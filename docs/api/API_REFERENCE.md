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

## Authentication & Authorization

Every endpoint requires a session, except the health probes, Swagger UI, `POST /api/v1/auth/login` and the database selector endpoints used before sign-in (`GET /api/v1/databases`, `POST /api/v1/databases`, `POST /api/v1/databases/switch`).

1. Sign in with `POST /api/v1/auth/login`. The response carries an opaque `token`.
2. Send it on every request: `Authorization: Bearer <token>`.
3. Sign out with `POST /api/v1/auth/logout`.

**A session is bound to one user and one database.** All schema, data, security and solution endpoints operate on the database the session signed in to. The server has no global "active database": two clients signed in to different databases never affect each other. A request that names another database (`?database=` or `X-Database-Name`) is rejected with `403`.

Sessions live in server memory, expire after `SESSION_TTL` of inactivity (default `12h`) and do not survive a server restart. Changing a user's password or role, deactivating or deleting the user, or dropping the database revokes the affected sessions immediately.

| Status | Meaning |
|---|---|
| `401 Unauthorized` | Missing, invalid, expired or revoked token. Sign in again. |
| `403 Forbidden` | The session's role or layout permissions do not allow the operation. |

### Roles

| Capability | `owner` | `admin` | `user` |
|---|---|---|---|
| Read the schema catalog (tables, occurrences, relationships, layouts, scripts) | yes | yes | yes (layouts with `none` access are hidden) |
| Create / change / delete tables, fields, occurrences, relationships, scripts; create / delete layouts | yes | yes | no |
| Save an existing layout (`PUT /layouts/{id}`) | yes | yes | only with `read_write` on that layout |
| Record CRUD and Find (`/api/v1/data/{table}`) | yes | yes | according to layout permissions (see below) |
| List / create / update / delete users, set layout permissions | yes | yes, except owner accounts | own password only |
| Export solution, export / import data | yes | yes | no |
| Import solution (may create accounts) | yes | no | no |
| Drop the database | yes | no | no |

### Layout permissions on the data API
Permissions are granted per layout (`read_write`, `read_only`, `none`), while the data API works on tables. For a `user`, the level on a table is the most permissive level across all layouts built on that table. A layout without an explicit permission row counts as `read_write`, and so does a table without layouts. `none` blocks reads and writes; `read_only` blocks inserts, updates and deletes.

### Server configuration

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | local development DSN | Connection to the engine's administrative database. Always set it outside development. |
| `SESSION_TTL` | `12h` | Idle lifetime of a session (Go duration). |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated list of browser origins allowed to call the API. |
| `ALLOW_PUBLIC_DATABASE_CREATION` | `true` | When `false`, only a signed-in owner can create databases. |

---

## 1. Documentation & Swagger UI

### `GET /swagger/`
Interactive Swagger UI embedded directly inside the API service container (`file4base-app-api`), enabling exploration and testing of all API endpoints. (Also accessible via `/swagger` and `/docs`).

### `GET /swagger/openapi.json`
Full OpenAPI 3.0.3 specification in JSON format. Also accessible at `/openapi.json` and `/api/v1/openapi.json`.

---

## 2. System & Health Probes

### `GET /healthz`
IETF draft & diagnostic health check. Returns comprehensive system status, version, uptime, and database connectivity.

#### Response `200 OK`
```json
{
  "status": "pass",
  "version": "0.4.7",
  "release_id": "file4base-api-v0.4.7",
  "service_id": "file4base-api",
  "description": "File4Base API Engine Health",
  "uptime_seconds": 3600,
  "checks": {
    "database": {
      "status": "pass",
      "active_database": "file4base_dev"
    }
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

The name (`custom_name`, or `display_name` with spaces turned into underscores) is lower-cased and must start with a letter, contain only letters, digits and underscores, and be at most 63 characters long (the same rule applies to column names). Names starting with `sys_` or `pg_`, and `information_schema`, `mysql`, `performance_schema` and `sys`, are reserved. A name already in use is never reused, whether it is a catalog table or another physical table.

#### Request Body
```json
{
  "display_name": "Invoices",
  "custom_name": "invoices"
}
```

#### Response `201 Created`
Returns created `TableMetadata`.

#### Errors
- `409 Conflict`: a table with that name is already registered.
- `422 Unprocessable Entity`: invalid or reserved name, or a name longer than 63 characters.
- `400 Bad Request`: the database refused the table (for example, a physical table outside the catalog already has that name).

---

### `DELETE /api/v1/schemas/tables/{id}`
Permanently drops the physical database table and deletes its catalog records in `sys_tables`, cascading to columns and table occurrences.

#### Response `204 No Content`

---

### `PUT /api/v1/schemas/tables/{id}/rename`
Updates the display name of an existing table.

#### Request Body
```json
{
  "display_name": "Clients"
}
```

#### Response `200 OK`
Returns updated `TableMetadata`.

---

### `POST /api/v1/schemas/tables/{id}/duplicate`
Duplicates a table structure (columns and options, without row data) with an auto-generated unique name (`<name>_copy`, shortened to fit 63 characters). If a column cannot be copied, the incomplete copy is removed and the request fails.

#### Response `201 Created`
Returns duplicated `TableMetadata`.

---

### `POST /api/v1/schemas/tables/{id}/truncate`
Permanently empties all rows and data from the table while preserving its schema structure and columns.

#### Response `204 No Content`

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
Updates column metadata and options (display name, auto-enter rules, default values, calculation formulas, and validation rules).

`default_value` holds the field's auto-enter and storage options as a JSON object (written by the Fields dialog), or `null`. It is metadata only: it is never placed in SQL, so the physical column has no `DEFAULT`. Any other value, including SQL expressions, answers `422 Unprocessable Entity`; the same applies to `default_value` when adding a column. When `data_enabled` is `true`, `data_value` is the constant entered in that field by `POST /api/v1/data/{table}` when the request does not include the field.

#### Request Body
```json
{
  "display_name": "Updated Column Name",
  "default_value": "{\"creation_enabled\":true,\"creation_type\":\"Date\"}",
  "calculation_formula": "{\"formula\":\"UPPER(first_name)\",\"result_type\":\"Text\"}",
  "validation_rules": "{\"not_empty\":true,\"unique\":true}"
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

### `POST /api/v1/schemas/occurrences`
Creates a new Table Occurrence tied to a base table.
```json
{
  "base_table_id": "table-uuid",
  "name": "CUSTOMERS 2",
  "x_pos": 250,
  "y_pos": 180
}
```

### `PUT /api/v1/schemas/occurrences/{id}`
Updates an occurrence name and/or its canvas coordinates `(x_pos, y_pos)`.
```json
{
  "name": "CUSTOMERS 2",
  "x_pos": 320,
  "y_pos": 220
}
```

### `DELETE /api/v1/schemas/occurrences/{id}`
Deletes a Table Occurrence. Cascades to any relationships connecting to it.

---

### `GET /api/v1/schemas/relationships`
Lists all relationships between Table Occurrences in the relational graph.

### `POST /api/v1/schemas/relationships`
Creates a new relationship between two Table Occurrence fields.
```json
{
  "name": "Cust_to_Invoices",
  "left_occurrence_id": "occ-1-uuid",
  "left_column_id": "col-1-uuid",
  "right_occurrence_id": "occ-2-uuid",
  "right_column_id": "col-2-uuid",
  "operator": "=",
  "allow_creation": true,
  "cascade_delete": false,
  "sort_related": ""
}
```

### `PUT /api/v1/schemas/relationships/{id}`
Updates a relationship definition.
```json
{
  "name": "Cust_to_Invoices",
  "operator": "=",
  "allow_creation": true,
  "cascade_delete": true,
  "sort_related": ""
}
```

### `DELETE /api/v1/schemas/relationships/{id}`
Deletes a relationship definition.

---

## 3. Visual Layouts

### `GET /api/v1/schemas/layouts`
Retrieves all visual layout definitions.

### `POST /api/v1/schemas/layouts`
Creates a visual layout. `table_occurrence_id` may be a table occurrence ID, a table ID or a table name; one that matches nothing answers `400` (the layout is never bound to another table). Without it, the first table occurrence is used.

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

> Only user tables registered in the catalog are reachable. System tables (`sys_*`) and any other physical table answer `404 Table Not Found`, for every role. Field names (`sort_by`, record keys, find criteria) must be fields registered for the table; unknown fields answer `400 Unknown Field`.

### `GET /api/v1/data/{table}`
Queries rows with optional pagination and sorting.

#### Query Parameters
- `limit` (integer, default `100`, maximum `1000`; larger values are clamped to `1000`)
- `offset` (integer, default `0`)
- `sort_by` (string, optional)
- `sort_asc` (boolean, default `true`)

CONTAINER fields are returned as base64 strings (standard alphabet, with padding); `POST` and `PUT` accept base64 strings for them and store the decoded bytes, and a value that is not valid base64 answers `400 Invalid Value`.

To read every row, page with `limit=1000` and `sort_by=id` (a stable order), advancing `offset` by the number of rows received, until a page returns fewer than `limit` rows.

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
*Public.* Lists the solution databases on the server (engine-internal databases such as `postgres` are never listed). `active` is the database of the caller's own session, or an empty string when the request carries no session.

#### Response `200 OK`
```json
{
  "databases": [
    "invoices_db",
    "contacts_db"
  ],
  "active": "invoices_db"
}
```

---

### `POST /api/v1/databases`
Creates a new physical database, initializes the system catalog tables (`sys_*`) and provisions its first `owner` account with the credentials in the request. There are no default accounts: `user` and `password` are mandatory. The owner password must have at least 8 characters and cannot be the username or the database name (database names are listed publicly).

*Public* while `ALLOW_PUBLIC_DATABASE_CREATION=true` (default, needed by the desktop "New Database" flow, which runs before sign-in). With `false`, it requires an `owner` session.

An existing database is never modified: if the name is taken the response is `409 Conflict`.

#### Request Body
```json
{
  "database": "invoices_db",
  "user": "alice",
  "password": "a-strong-password"
}
```

#### Response `201 Created`
```json
{
  "database": "invoices_db",
  "status": "created",
  "owner_user": "alice",
  "active": ""
}
```

#### Errors
- `409 Conflict`: a database with that name already exists.
- `422 Unprocessable Entity`: missing database name or owner user, or an owner password that is missing, shorter than 8 characters, or equal to the username or the database name.

---

### `POST /api/v1/databases/switch` *(deprecated)*
*Public.* Kept for backwards compatibility. It no longer switches anything: it only reports whether the database exists (`404` otherwise). To work on another database, sign in to it with `POST /api/v1/auth/login`.

#### Request Body
```json
{
  "database": "invoices_db"
}
```

#### Response `200 OK`
```json
{
  "database": "invoices_db",
  "active": "",
  "status": "available"
}
```

---

### `DELETE /api/v1/databases/{name}`
Drops a database and revokes every session bound to it. The caller must prove ownership of the database being dropped:
- the session is an `owner` session on that same database, **or**
- the request body carries the credentials of one of its owners.

#### Request Body (only when dropping a database other than the session's)
```json
{
  "username": "bob",
  "password": "owner-password-of-that-database"
}
```

#### Response `200 OK`
```json
{
  "database": "old_db",
  "status": "deleted",
  "active": "invoices_db"
}
```

---

## 6. MessagePack Solutions & Database Data Persistence

### `GET /api/v1/solutions/export` and `POST /api/v1/solutions/export`
Packages the design of the session's database into a solution file (`.f4p`, MessagePack): tables with their field options, table occurrences, relationships, layouts, scripts with their steps, and accounts with their role, active state and layout permissions. The format is specified in [`docs/specs/solution_bundle_format.md`](../specs/solution_bundle_format.md).

**No password is ever written**: no account password or hash, no connection password, no remembered sign-in password. The connection part names the database and the account that exported it.

`GET` takes the solution name in `?name=`. `POST` takes a JSON body with client settings that are stored in the file as they are (keys containing `password` are dropped):

```json
{
  "solution_name": "Invoices Pro",
  "file_options": { "auto_login_enabled": false, "startup_layout_name": "Invoices" },
  "page_setup": { "paper": "A4" }
}
```

Requires the `owner` or `admin` role.

#### Response `200 OK`
- `Content-Type: application/x-msgpack`
- Binary MessagePack payload (`.f4p`)

---

### `POST /api/v1/solutions/import`
Merges a solution file into the session's database (owner only). Version `2.0` files, files written by the 1.0 client and solution files exported by 1.0 servers are accepted.

1. The whole file is validated first (structure, names, field options and every internal reference); a file with a dangling reference answers `400` and changes nothing.
2. Existing objects are matched by name (tables, fields, occurrences, layouts and scripts, accounts by username) and every reference is rewritten to the database's own IDs, including the `script_id` of buttons and script triggers in layout definitions.
3. Missing objects are created; matching layouts and scripts are updated; existing tables, fields and accounts are left unchanged. Importing the same file twice creates nothing new.
4. Missing accounts are created **disabled**, with a random password nobody knows, keeping their role and layout permissions: an owner enables them and sets their password in Manage Security.
5. If a write fails, everything the import created is removed and every updated layout or script gets its previous version back; the response is `400` with the reason.

Before decoding, the payload's structure is checked: a collection or string declaring more elements or bytes than the payload holds, nesting deeper than 32 levels or more than 5,000,000 collection entries in total answers `400 Bad Request` and changes nothing (also for `import-data`).

#### Request Body
Binary MessagePack payload (`Content-Type: application/x-msgpack`).

#### Response `200 OK`
```json
{
  "status": "imported",
  "solution_name": "Invoices Pro",
  "tables_created": 2,
  "columns_created": 5,
  "occurrences_created": 1,
  "relationships_created": 1,
  "layouts_created": 2,
  "layouts_updated": 0,
  "scripts_created": 1,
  "scripts_updated": 0,
  "accounts_created": 1,
  "accounts_pending_password": ["bob"],
  "tables_count": 2,
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
Restores records into the session's database from a MessagePack `.f4data` payload, in **one transaction**.

Only catalog tables receive rows (internal `sys_*` tables never do, even if a catalog row names one), and every field of every record must be a field of its table: otherwise the request answers `400 Bad Request` before any row is written. The first record the database rejects (for example, text in a NUMBER field) rolls back the whole import and is reported with its table and position. A record whose `id` already exists is skipped, not overwritten. Tables of the file that the database does not have are listed in `skipped_tables`.

CONTAINER fields are MessagePack binary values in `.f4data` files, so they are restored byte for byte.

#### Request Body
Binary MessagePack payload (`Content-Type: application/x-msgpack`).

#### Response `200 OK`
```json
{
  "status": "restored",
  "database": "invoices_db",
  "tables_restored": 3,
  "records_inserted": 140,
  "records_skipped": 2,
  "records_count": 140,
  "skipped_tables": []
}
```
`records_count` equals `records_inserted`.

---

## 7. Authentication & Security Management

### `POST /api/v1/auth/login`
*Public.* Authenticates a user against one database and opens a session bound to that database. `database`, `username` and `password` are all required, and the username and password must belong to the same account.

Signing in never initializes a database or creates accounts.

#### Request Body
```json
{
  "username": "alice",
  "password": "a-strong-password",
  "database": "invoices_db"
}
```

#### Response `200 OK`
```json
{
  "status": "ok",
  "database": "invoices_db",
  "user": {
    "id": "u-0001",
    "username": "alice",
    "role": "owner",
    "is_active": true,
    "permissions": []
  },
  "token": "5f1c…64 hex characters",
  "token_type": "Bearer",
  "expires_at": "2026-10-06T20:00:00Z"
}
```

#### Errors
- `401 Unauthorized`: invalid username or password, deactivated account, or the database has no File4Base catalog.
- `404 Not Found`: the database does not exist.
- `422 Unprocessable Entity`: `database` is missing.

---

### `POST /api/v1/auth/logout`
Revokes the session of the bearer token. Response `204 No Content`.

---

### `GET /api/v1/auth/session`
Returns the user, role, layout permissions and database of the current session.

#### Response `200 OK`
```json
{
  "database": "invoices_db",
  "user": {
    "id": "u-0001",
    "username": "alice",
    "role": "owner",
    "is_active": true,
    "permissions": []
  },
  "expires_at": "2026-10-06T20:00:00Z"
}
```

---

### `GET /api/v1/security/users`
Lists the user accounts of the session's database. Requires the `owner` or `admin` role. User accounts are strictly isolated per database.

The `?database=<name>` query parameter and the `X-Database-Name` header are still accepted for compatibility, but they must name the session's own database; any other value answers `403`.

#### Response `200 OK`
```json
[
  {
    "id": "u-0001",
    "username": "alice",
    "role": "owner",
    "is_active": true,
    "created_at": "2026-09-23T10:00:00Z",
    "updated_at": "2026-09-23T10:00:00Z"
  }
]
```

---

### `POST /api/v1/security/users`
Creates a new user account with hashed password (`bcrypt`). Requires the `owner` or `admin` role; only an `owner` can create another `owner`.

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
Updates password, role and/or `is_active` of an existing user account.

- `owner`: any account.
- `admin`: `admin` and `user` accounts; cannot modify owner accounts nor promote to `owner`.
- `user`: only their own password (role and status must stay unchanged).

The last active owner cannot be demoted or deactivated. When the password, role or status changes, the other sessions of that user are revoked.

#### Request Body
```json
{
  "password": "newpassword",
  "role": "admin"
}
```

---

### `DELETE /api/v1/security/users/{id}`
Deletes a user account and revokes its sessions. Requires the `owner` or `admin` role (only an `owner` can delete an owner). You cannot delete your own account nor the only remaining owner account.

---

### `GET /api/v1/security/users/{id}/permissions`
Retrieves granular per-layout permissions for a user. Admins can read anyone's; a `user` can only read their own.

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
Replaces the per-layout permissions of a user. Requires the `owner` or `admin` role. `access_level` is one of `read_write`, `read_only`, `none`; every level is stored explicitly (a layout without a row defaults to `read_write`).

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

---

## 8. Script Workspace & Automation

### `GET /api/v1/schemas/scripts`
Lists all scripts in the active database, including their complete sequential step hierarchy.

#### Response `200 OK`
```json
[
  {
    "id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "name": "on_invoice_created",
    "context_table": "Invoices",
    "is_active": true,
    "steps": [
      {
        "id": "step-1",
        "script_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
        "sequence_idx": 1,
        "step_type": "go_to_layout",
        "params": { "layout_name": "Invoices_Detail" },
        "is_enabled": true
      },
      {
        "id": "step-2",
        "script_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
        "sequence_idx": 2,
        "step_type": "set_variable",
        "params": { "variable": "$subtotal", "calc": "Sum(Items.price)" },
        "is_enabled": true
      }
    ]
  }
]
```

---

### `POST /api/v1/schemas/scripts`
Creates a new script with optional initial step instructions.

#### Request Body
```json
{
  "name": "monthly_report_generation",
  "context_table": "Reports",
  "is_active": true,
  "steps": [
    {
      "sequence_idx": 1,
      "step_type": "go_to_layout",
      "params": { "layout_name": "Reports_Summary" },
      "is_enabled": true
    }
  ]
}
```

---

### `GET /api/v1/schemas/scripts/{id}`
Retrieves a single script definition with all associated steps ordered by `sequence_idx`.

---

### `PUT /api/v1/schemas/scripts/{id}`
Updates script metadata and atomically synchronizes the sequential steps list.

#### Request Body
```json
{
  "name": "monthly_report_generation_v2",
  "context_table": "Reports",
  "is_active": true,
  "steps": [
    {
      "sequence_idx": 1,
      "step_type": "go_to_layout",
      "params": { "layout_name": "Reports_Summary" },
      "is_enabled": true
    },
    {
      "sequence_idx": 2,
      "step_type": "commit_records",
      "params": { "validate": true },
      "is_enabled": true
    }
  ]
}
```

---

### `DELETE /api/v1/schemas/scripts/{id}`
Deletes a script and cascades removal of all associated steps.

---

### `POST /api/v1/schemas/scripts/{id}/duplicate`
Clones an existing script and all its steps, appending `_copy` to the name.


