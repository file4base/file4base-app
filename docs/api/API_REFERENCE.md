# File4Base REST API Reference

Base URL (Default): `http://localhost:8080`

---

## 1. System & Health

### `GET /healthz`
Returns system status, active database engine, and connection state.

#### Response `200 OK`
```json
{
  "app": "File4Base Server",
  "version": "0.2.0",
  "status": "ok",
  "database": "connected",
  "engine": "postgres",
  "timestamp": "2026-09-23T09:30:10Z"
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
Executes FileMaker-style Find requests with operator translation.

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
