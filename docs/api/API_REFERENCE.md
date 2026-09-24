# File4Base REST API Reference

Base URL (Default): `http://localhost:8080`

> For enterprise architecture guidelines, OpenTelemetry standards, health check probe rules, and RFC 9457 error formatting, see the [API Best Practices Specification](../specs/API_BEST_PRACTICES.md).

---

## Global Headers & Observability

### Request Headers
- `traceparent` *(optional)*: W3C standard trace context (`00-{trace_id}-{span_id}-01`). Injected automatically by the Web client and desktop clients.
- `tracestate` *(optional)*: W3C vendor state for distributed tracing.
- `X-Trace-ID` *(optional)*: Trace identifier alias.

### Response Headers
- `traceparent`: W3C trace context matching the active span.
- `X-Trace-ID`: Active trace identifier for correlation in structured logs.
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: SAMEORIGIN`
- `X-XSS-Protection: 1; mode=block`
- `Strict-Transport-Security: max-age=31536000; includeSubDomains`

### RFC 9457 Problem Details Error Envelope
When an error occurs (HTTP 4xx or 5xx), the response body is encoded as `application/problem+json`:
```json
{
  "type": "https://tools.ietf.org/html/rfc9110#section-15.5.5",
  "title": "Not Found",
  "status": 404,
  "detail": "Table with id 'unknown-id' was not found",
  "instance": "/api/v1/schemas/tables/unknown-id",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

---

## 1. System & Health Probes

### `GET /healthz`
IETF draft & diagnostic health check. Returns comprehensive system status, version, uptime, and database connectivity.

#### Response `200 OK`
```json
{
  "status": "pass",
  "version": "0.4.6",
  "releaseId": "0.4.6",
  "serviceId": "file4base-server",
  "description": "File4Base Core Backend Service",
  "uptime": "1h23m45s",
  "checks": {
    "database": [
      {
        "componentType": "datastore",
        "observedValue": "connected",
        "status": "pass",
        "time": "2026-09-24T10:45:00Z"
      }
    ]
  },
  "engine": "postgres",
  "database": "connected",
  "active_database": "file4base_dev"
}
```

### `GET /healthz/liveness` (Alias: `GET /livez`)
Kubernetes / Docker container liveness probe. Indicates if the process is alive. If this fails, the container orchestrator restarts the pod/container.

#### Response `200 OK`
```json
{
  "status": "pass",
  "probe": "liveness"
}
```

### `GET /healthz/readiness` (Alias: `GET /readyz`)
Kubernetes / Docker container readiness probe. Indicates whether the service is ready to accept incoming user traffic. Returns `503 Service Unavailable` during startup, during database disconnections, or when a graceful shutdown signal (SIGTERM/SIGINT) is received.

#### Response `200 OK` (Healthy)
```json
{
  "status": "pass",
  "probe": "readiness",
  "database": "connected"
}
```

#### Response `503 Service Unavailable` (Draining / Unhealthy)
```json
{
  "status": "fail",
  "probe": "readiness",
  "database": "disconnected"
}
```

### `GET /healthz/startup` (Alias: `GET /startupz`)
Kubernetes startup probe. Used for slow-starting containers to protect liveness probes during DB migrations or initialization.

#### Response `200 OK`
```json
{
  "status": "pass",
  "probe": "startup"
}
```


---

## 2. Schema Engine & Metadata Catalog

### `GET /api/v1/schemas/tables`
Retrieves all registered user database tables and their column definitions.

#### Response `200 OK`
```json
[
  {
    "id": "366f22cf-0054-4a84-a8ba-21cc90bbedae",
    "name": "contacts",
    "display_name": "Contacts",
    "description": "",
    "columns": [
      {
        "id": "de9c04e4-03e8-4473-9fa4-9e356f75dc48",
        "table_id": "366f22cf-0054-4a84-a8ba-21cc90bbedae",
        "name": "id",
        "display_name": "ID",
        "field_type": "TEXT",
        "is_nullable": false,
        "is_primary_key": true
      },
      {
        "id": "c1cc6a87-36e6-4182-9a08-5322536fe567",
        "table_id": "366f22cf-0054-4a84-a8ba-21cc90bbedae",
        "name": "first_name",
        "display_name": "First Name",
        "field_type": "TEXT",
        "is_nullable": false,
        "is_primary_key": false
      }
    ],
    "created_at": "2026-09-23T09:21:47Z",
    "updated_at": "2026-09-23T09:21:47Z"
  }
]
```

### `POST /api/v1/schemas/tables`
Creates a new physical table and registers it in `sys_tables` and `sys_table_occurrences`.

#### Request Body
```json
{
  "display_name": "Invoices",
  "custom_name": "invoices"
}
```

#### Response `201 Created`
Returns created `TableMetadata`.

---

### `POST /api/v1/schemas/tables/{id}/columns`
Adds a new column to the specified table, executing physical DDL via the active DBAL dialect.

#### Request Body
```json
{
  "name": "total_amount",
  "display_name": "Total Amount",
  "field_type": "NUMBER",
  "is_nullable": true
}
```

#### Response `201 Created`
Returns created `ColumnMetadata`.

---

### `PUT /api/v1/schemas/tables/{id}/columns/{columnId}`
Updates column metadata (e.g. display name).

#### Request Body
```json
{
  "display_name": "Updated Column Name"
}
```

#### Response `200 OK`
Returns updated `ColumnMetadata`.

---

### `DELETE /api/v1/schemas/tables/{id}/columns/{columnId}`
Drops the column from the physical database table and removes it from `sys_columns`. Cannot delete primary key.

#### Response `204 No Content`

---

### `GET /api/v1/schemas/occurrences`
Lists all Table Occurrences in the relationship graph.

---

## 3. Visual Layouts

### `GET /api/v1/schemas/layouts`
Retrieves all visual layout definitions.

### `POST /api/v1/schemas/layouts`
Creates a visual layout.

#### Request Body
```json
{
  "name": "Customers Form",
  "table_occurrence_id": "to-uuid",
  "definition": {
    "theme": "Enlightened",
    "width": 1024,
    "parts": [],
    "objects": []
  }
}
```

### `GET /api/v1/schemas/layouts/{id}`
Retrieves a single layout by ID.

### `PUT /api/v1/schemas/layouts/{id}`
Updates a layout definition and name.

### `DELETE /api/v1/schemas/layouts/{id}`
Deletes a layout.

---

## 4. Dynamic Data & Find Mode

### `GET /api/v1/data/{table}`
Queries rows with optional pagination and sorting.

#### Query Parameters
- `limit` (integer, default `100`)
- `offset` (integer, default `0`)
- `sort_by` (string, optional)
- `sort_asc` (boolean, default `true`)

---

### `POST /api/v1/data/{table}`
Dynamically inserts a row into any table. Generates a UUID `id` if not provided.

#### Request Body
```json
{
  "first_name": "Alice",
  "email": "alice@example.com"
}
```

---

### `PUT /api/v1/data/{table}/{id}`
Updates an existing record by primary key ID.

---

### `DELETE /api/v1/data/{table}/{id}`
Deletes a record by primary key ID.

---

### `POST /api/v1/data/{table}/find`
Executes File4Base-style Find requests with operator translation.

#### Request Body
```json
{
  "requests": [
    {
      "criteria": [
        {
          "field_name": "first_name",
          "operator": "LIKE",
          "value": "%Ali%"
        }
      ],
      "omit": false
    }
  ],
  "options": {
    "limit": 100,
    "offset": 0
  }
}
```
Supported operators: `=`, `!=`, `>`, `<`, `>=`, `<=`, `LIKE`, `RANGE`.

---

## 5. Multi-Database Management

### `GET /api/v1/databases`
Lists all available user databases on the PostgreSQL server and indicates the current active database.

#### Response `200 OK`
```json
{
  "databases": [
    "file4base_dev",
    "invoices_db",
    "contacts_db"
  ],
  "active": "file4base_dev"
}
```

---

### `POST /api/v1/databases`
Creates a new physical database in PostgreSQL and initializes the system catalog tables (`sys_*`).

#### Request Body
```json
{
  "database": "invoices_db"
}
```

#### Response `201 Created`
```json
{
  "database": "invoices_db",
  "status": "created",
  "active": "invoices_db"
}
```

---

### `POST /api/v1/databases/switch`
Switches the active database context for future schema and data operations.

#### Request Body
```json
{
  "database": "invoices_db"
}
```

#### Response `200 OK`
```json
{
  "active": "invoices_db",
  "status": "connected"
}
```

---

## 6. MessagePack Solutions & Database Data Persistence

### `GET /api/v1/solutions/export`
Packages the active solution (layouts, schemas, table occurrences, relationships, users, and DB connection parameters) into a binary MessagePack solution file (`.f4b`).

#### Query Parameters
- `name` (string, optional, default: `file4base_solution`)
- `host` (string, optional, default: `localhost`)
- `port` (integer, optional, default: `5432`)
- `user` (string, optional, default: `file4base`)
- `password` (string, optional, default: `dev_password`)

#### Response `200 OK`
- `Content-Type: application/x-msgpack`
- Binary MessagePack payload (.f4b)

---

### `POST /api/v1/solutions/import`
Restores a complete solution from a MessagePack `.f4b` binary payload. Re-creates missing tables, adds columns, and persists layouts.

#### Request Body
Binary MessagePack payload (`Content-Type: application/x-msgpack`).

#### Response `200 OK`
```json
{
  "status": "imported",
  "solution_name": "Invoices Pro",
  "tables_count": 3,
  "layouts_count": 2
}
```

---

### `GET /api/v1/solutions/export-data`
Dumps all records and table rows from the active database into a binary MessagePack data file (`.f4data`).

#### Response `200 OK`
- `Content-Type: application/x-msgpack`
- Binary MessagePack payload (.f4data)

---

### `POST /api/v1/solutions/import-data`
Restores physical records into the active database from a MessagePack `.f4data` payload.

#### Request Body
Binary MessagePack payload (`Content-Type: application/x-msgpack`).

#### Response `200 OK`
```json
{
  "status": "restored",
  "database": "invoices_db",
  "tables_restored": 3,
  "records_count": 142
}
```

---

## 7. Authentication & Security Management

### `POST /api/v1/auth/login`
Authenticates a user with username and password, optionally switching active database context.

#### Request Body
```json
{
  "username": "admin",
  "password": "admin",
  "database": "file4base_dev"
}
```

#### Response `200 OK`
```json
{
  "status": "authenticated",
  "user": {
    "id": "u-0001",
    "username": "admin",
    "role": "owner"
  },
  "database": "file4base_dev"
}
```

---

### `GET /api/v1/security/users`
Lists all user accounts in the active database.

#### Response `200 OK`
```json
[
  {
    "id": "u-0001",
    "username": "file4base_dev",
    "role": "owner",
    "created_at": "2026-09-23T10:00:00Z",
    "updated_at": "2026-09-23T10:00:00Z"
  }
]
```

---

### `POST /api/v1/security/users`
Creates a new user account with hashed password (`bcrypt`).

#### Request Body
```json
{
  "username": "editor1",
  "password": "secretpassword",
  "role": "user"
}
```

#### Response `201 Created`
```json
{
  "id": "u-0002",
  "username": "editor1",
  "role": "user",
  "created_at": "2026-09-23T10:05:00Z",
  "updated_at": "2026-09-23T10:05:00Z"
}
```

---

### `PUT /api/v1/security/users/{id}`
Updates password and/or role of an existing user account.

#### Request Body
```json
{
  "password": "newpassword",
  "role": "admin"
}
```

---

### `DELETE /api/v1/security/users/{id}`
Deletes a user account. Cannot delete the only remaining owner account.

---

### `GET /api/v1/security/users/{id}/permissions`
Retrieves granular per-layout permissions for a user.

#### Response `200 OK`
```json
[
  {
    "layout_id": "layout_1",
    "access_level": "read_write"
  },
  {
    "layout_id": "layout_2",
    "access_level": "read_only"
  }
]
```

---

### `PUT /api/v1/security/users/{id}/permissions`
Saves per-layout permissions for a user.

#### Request Body
```json
{
  "permissions": [
    {
      "layout_id": "layout_1",
      "access_level": "read_write"
    },
    {
      "layout_id": "layout_2",
      "access_level": "read_only"
    }
  ]
}
```

