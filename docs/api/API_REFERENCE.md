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

Sessions live in server memory, expire after `SESSION_TTL` of inactivity (default `12h`) and do not survive a server restart. Changing a user's password or role, deactivating or deleting the user, or dropping the database revokes the affected sessions immediately. Every request also checks that the account is still in the state it signed in with (same password, role and active state, and not deleted), so a session opened by a sign-in that was still in progress during such a change is rejected too. The session used to change one's own password stays valid.

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

#### What the options mean

A relationship is drawn from a **left** side to a **right** side, and File4Base reads that direction as parent to child. The three options therefore act on the right side when a record of the left side is the one in hand:

| Option | Acts |
| --- | --- |
| `allow_creation` | `POST /api/v1/data/{table}/{id}/related` creates a record on the right side. Without it that request answers `403 Creation Not Allowed`. |
| `cascade_delete` | `DELETE /api/v1/data/{table}/{id}` on a left-side record also deletes the right-side records that match it, down the whole chain. |
| `sort_related` | The order the right side's records come back in when the caller asks for none. `asc` / `desc` mean the match field in that direction; anything else is read as a sort order (`last_name,-fee_paid`). Reading the relationship the other way round ignores it, since the fields it names are the right side's, and a field the side being read does not have is dropped rather than refused. |

The Specify Relationship dialog names both tables in these labels, so which side an option acts on is visible rather than implied.

Operators are stored as the mathematical signs the relationship graph writes — `=`, `≠`, `<`, `≤`, `>`, `≥` — and the plain spellings (`<>`, `!=`, `<=`, `>=`) are accepted too.

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
- `sort` (string, optional, repeatable) — orders by several fields in turn. Comma-separated or repeated; a leading `-` sorts that field descending. `?sort=company,-fee_paid` orders by Company ascending and breaks ties by Fee Paid descending. It supersedes `sort_by`/`sort_asc`, which stay for callers that only sort by one field. Every field named must be registered for the table; an unknown one answers `400 Unknown Field`.

CONTAINER fields are returned as base64 strings (standard alphabet, with padding); `POST` and `PUT` accept base64 strings for them and store the decoded bytes, and a value that is not valid base64 answers `400 Invalid Value`.

To read every row, page with `limit=1000` and `sort_by=id` (a stable order), advancing `offset` by the number of rows received, until a page returns fewer than `limit` rows.

---

### `POST /api/v1/data/{table}`
Dynamically inserts a row into any table. Generates a UUID `id` if not provided.

CALCULATION fields are filled in by their formula and are not writable: a value sent for one is ignored. The formula is evaluated by the server when the record is written, and the answer is stored in the field's column, so a calculation field is sorted and found on like any other field. A `PUT` recomputes them from the record as it will be after the update, so changing a field a formula reads updates the result. A formula that cannot be computed for a record (dividing by zero, or text where a number is needed) answers `400 Invalid Value` and nothing is written. See [docs/specs/calculation_formulas.md](../specs/calculation_formulas.md).

The fields' validation rules (`validation_rules`, set in the Fields dialog) are enforced on `POST` (every ruled field) and `PUT` (the fields being changed): not empty, unique, existing value, strict type (`Numeric Only`, `Date`, `4-Digit Year`, `Time of Day`, `Text Only`), range and maximum length, with the custom message when one is set. A broken rule answers `422 Unprocessable Entity` naming the field and the rule in `invalid_params`, and nothing is written. Unique rules are backed by a unique index on PostgreSQL, so concurrent duplicates cannot both succeed; a unique rule cannot be enabled while records already share a value. Data import applies the rules whose timing is "Always", not those for "Only during data entry"; malformed rules are rejected with `422` when saved.

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

Every relationship of the table whose `cascade_delete` is set and whose **left** side this table is takes its matching records with it, and so on down the chain; a ring of such relationships deletes each record once rather than recursing forever. This is schema-defined integrity, so it applies whatever access level the caller has on the related table.

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
Supported operators: `=`, `!=`, `>`, `<`, `>=`, `<=`, `LIKE`, `RANGE`, `IS_EMPTY`, `IS_NOT_EMPTY`.

**How several requests combine.** A record is found when it matches **any** request whose `omit` is false, and is then dropped when it matches **any** request whose `omit` is true. So `[{city: "New York"}, {city: "London"}]` finds the records in either city, and adding `{customer_type: "New", omit: true}` drops the new customers from that set. A find made only of omitting requests starts from every record.

`options` takes the same `limit`, `offset`, `sort_by`/`sort_asc` and `sort` as `GET /api/v1/data/{table}`; `sort` is the array form, `[{"field": "company"}, {"field": "fee_paid", "descending": true}]`.

---

### Importing and exporting records

File4Base reads records out of comma- and tab-separated text, Excel workbooks and XML, and writes a found set back out (#38). See [docs/specs/importing_and_exporting_records.md](../specs/importing_and_exporting_records.md).

The file travels **base64 in the JSON body**, beside the settings, rather than as a separate upload, so one request carries the file and what to do with it. The format is taken from `format`, or guessed from `file_name`.

Moving records in or out in bulk is a privilege an owner can withhold from a role (#39): without it, `import`, `import/preview` and `export` answer `403 Forbidden` whatever the caller's table access is. See [`/api/v1/security/privileges`](#get-apiv1securityprivileges).

### `POST /api/v1/data/{table}/import/preview`

Reads a file and reports what is in it. **Writes nothing**, but needs write access to the table, because it is a step on the way to writing.

#### Request Body
```json
{
  "file_name": "customers.csv",
  "content": "TGFzdCBOYW1lLENpdHkKRHVyYW5kLFBhcmlzCg==",
  "format": "csv",
  "has_header": true,
  "delimiter": ",",
  "sheet": "Customers",
  "record_element": "customer"
}
```
`format` is `csv`, `tsv`, `xlsx` or `xml`. `delimiter` empty means the parser works it out. `sheet` and `record_element` apply to workbooks and XML.

#### Response `200 OK`
```json
{
  "columns": ["Last Name", "City"],
  "sheets": ["Customers", "Notes"],
  "rows": [["Durand", "Paris"]],
  "row_count": 2,
  "truncated": false
}
```
`rows` is at most the first 20, for a preview. `truncated` is true when the file held more rows than the guards allow — the rest were not read, and saying so is the caller's job.

### `POST /api/v1/data/{table}/import`

Brings the records in. Same body, plus `options`:

```json
{
  "options": {
    "action": "add",
    "date_order": "dmy",
    "mappings": [{"column": 0, "field": "last_name"}, {"column": 1, "field": "city"}],
    "match_fields": ["last_name"],
    "add_unmatched": false
  }
}
```

- `action` — `add` makes every row a new record; `update_matching` updates the record whose `match_fields` hold the same values, case-insensitively, and adds or passes over the rest according to `add_unmatched`.
- `date_order` — `iso` (default), `dmy` or `mdy`. `03/04/2011` cannot be read without being told.
- `mappings` — which column goes to which field. A column not listed is left out.

**One transaction.** The first row that cannot be read or stored takes the whole file with it; nothing is written and the error names the row and the column. The field validation rules apply, and calculation fields are worked out for imported records.

A column sent to a **calculation** or a **summary** field is refused: the first's formula owns its value, the second has none in a record.

#### Response `200 OK`
```json
{"rows": 2, "added": 2, "updated": 0, "skipped": 0, "fields": ["last_name", "city"], "truncated": false}
```

#### Errors
- `422 Import Failed` — a bad mapping, or a row that could not be read. The detail names the row and the column.
- `422 Invalid Source File` — the file is not what it claims to be.
- `413 Source File Too Large` — past the 25 MiB, 100 000-row or 256-column guard.

### `POST /api/v1/data/{table}/export`

Writes a found set out and returns the file itself, with `Content-Disposition` and an `X-File4Base-Rows` count.

#### Request Body
```json
{
  "format": "xlsx",
  "name": "Customers",
  "requests": [{"criteria": [{"field_name": "city", "operator": "=", "value": "Paris"}]}],
  "fields": ["last_name", "city"],
  "headings": ["Last Name", "City"],
  "sort": [{"field": "last_name"}]
}
```

`format` is `csv`, `tsv` or `xlsx`; XML is read but not written. `requests` takes the same find requests a find does, so what is exported is what was found — empty exports the table. Empty `fields` writes every field by name. A date is written as a date rather than as the timestamp the column holds, and a CSV carries a byte order mark so Excel opens it as UTF-8.

---

### Criteria on a related field

A criterion may name a relationship instead of searching the record's own
field (#46). It then finds the records that have a related record matching it:

```json
{
  "requests": [{"criteria": [
    {"field_name": "company_address", "operator": "LIKE", "value": "%Paris%",
     "relationship_id": "a012b0fa-…", "occurrence": "d97e092c-…"}
  ]}]
}
```

`relationship_id` is the relationship to reach through; `occurrence` names the
side to read and is needed only when the relationship joins a table to itself.
The criterion takes the same operators as one on an own field and combines with
them, and with the OR and omit semantics of several requests, in the same way.

A record whose match field is empty relates to nothing, so it never matches a
related criterion. The caller must have access to the related table as well:
`POST /api/v1/data/{table}/find` and `/summary` answer `403 Forbidden` when they
do not, and `400 Unknown Field` when the related table has no such field.

### Saved finds

A saved find is a named set of find requests on one table (#34). The criteria
are stored **as they were typed** (`>100`, `Paris`, `a...b`), not as the
operators they parse into, so a saved find can be opened in Find mode, read and
changed, and is parsed again every time it runs. Running one is `POST
/api/v1/data/{table}/find` with those requests.

The finds of a table are visible to every account that can search it, and the
account that saved one — or any admin — may rename or delete it. A find
restored from a solution file has no owner account, so anyone with access to
the table may change it.

#### `GET /api/v1/saved-finds`
Lists the saved finds. `?table=<name>` limits them to one table; without it the
list holds every table the caller can reach. Also mounted at
`/api/v1/schemas/saved-finds`.

##### Response `200 OK`
```json
[
  {
    "id": "3f2a…",
    "name": "New York or London, not new",
    "table_name": "customers",
    "requests": [
      {"values": {"city": "=New York"}, "omit": false},
      {"values": {"city": "=London"}, "omit": false},
      {"values": {"status": "=New"}, "omit": true}
    ],
    "created_by": "u-0001",
    "created_at": "2026-10-08T09:12:00Z",
    "updated_at": "2026-10-08T09:12:00Z"
  }
]
```

#### `POST /api/v1/saved-finds`
Saves a find. The body is `name`, `table_name` and `requests`.

A criterion on a related field is held under the key
`rel:<relationship id>:<occurrence>:<field>` instead of a plain field name, and
is checked against the related table (#46).

Checked against the catalog before it is stored, so a saved find cannot fail
with "unknown field" the first time someone runs it: the table must exist, every
criterion must name one of its fields, and at least one request must have
something typed in it. A request with nothing in it is dropped rather than
stored, since it would find everything.

- `201 Created`: the find, as `GET` returns it.
- `403 Forbidden`: the caller cannot search that table.
- `409 Conflict`: the table already has a find with that name.
- `422 Unprocessable Entity`: no name, an unknown table or field, or nothing typed.

#### `PUT /api/v1/saved-finds/{id}`
Replaces the name and the requests. The table cannot change: that would be a
different find, and `table_name` in the body is ignored. Requires the account
that saved it, or an admin (`403` otherwise).

#### `DELETE /api/v1/saved-finds/{id}`
Removes the find; the records it finds are not affected. Same rule as `PUT`.
Answers `204 No Content`.

---

### `POST /api/v1/data/{table}/summary`

Works out the figures of a report: what each summary field comes to over the found set, and over each group of it (#32). See [docs/specs/summary_fields_and_reports.md](../specs/summary_fields_and_reports.md).

#### Request Body
```json
{
  "requests": [
    {"criteria": [{"field_name": "city", "operator": "=", "value": "Paris"}], "omit": false}
  ],
  "fields": ["fee_total", "fee_count"],
  "group_by": ["customer_type"]
}
```

- `requests` — the found set, in the same form `POST .../find` takes, so a report totals the records the find returned. Empty summarizes every record of the table.
- `fields` — the summary fields wanted. Empty returns every summary field the table has.
- `group_by` — the break fields, outermost first. Empty returns the grand totals alone. A report asks once per break level, because a sub-summary needs the group at its own level.

#### Response `200 OK`
```json
{
  "count": 16,
  "grand": {"fee_total": "2300", "fee_count": "16"},
  "group_by": ["customer_type"],
  "groups": [
    {"values": {"customer_type": "Continuing"}, "count": 9, "summaries": {"fee_total": "900", "fee_count": "9"}},
    {"values": {"customer_type": "New"}, "count": 7, "summaries": {"fee_total": "1400", "fee_count": "7"}}
  ],
  "summaries": {"fee_total": {"summary_type": "total", "field": "annual_fee", "running": false}}
}
```

Groups come back in the order their break fields sort, so a report reads top to bottom. Figures are decimal text rather than numbers, so nothing is lost through a float (#18); `fraction_of_total` is a share between 0 and 1.

#### Errors
- `422 Invalid Summary Field` — a field named in `fields` is not a summary field, or its definition cannot be used.
- `400 Unknown Field` — a `group_by` field the table does not have.

---

### Summary fields

A **summary field** (`field_type: "SUMMARY"`) works out a figure over a set of records rather than holding a value per record. Its definition goes in `calculation_formula`:

```json
{"summary_type": "total", "field": "annual_fee", "running": false}
```

`summary_type` is one of `total`, `average`, `count`, `minimum`, `maximum`, `standard_deviation`, `fraction_of_total`. The first two and the last two need a field that **stores** numbers — a calculation whose result type is Number counts. A summary over another summary, or over itself, is refused with `422`.

The older shape, `{"operation": "SUM", "target_column": "annual_fee"}`, is read as well, so a field defined before summaries were computed works without being set up again.

**A summary field's column is never written.** It has no value in a record, so a value sent for one in `POST` or `PUT` is dropped; an update of summary fields alone changes nothing and answers the record unchanged.

---

### Related records and portals

A layout can show the other side of a relationship: a **related field** (`Companies::company_address` on a Customers layout) and a **portal**, which is the list of related records. Both read these two endpoints. See [docs/specs/relationships_and_portals.md](../specs/relationships_and_portals.md).

### `GET /api/v1/data/{table}/{id}/related`

Returns the records of a related table that match record `{id}` of `{table}`.

#### Query Parameters
- `relationship` (string, **required**) — the relationship to follow. Missing answers `400 Missing Relationship`.
- `occurrence` (string, optional) — the occurrence whose records are wanted, by id or by name. It may be left out unless the relationship joins a table to itself, where there is no other side to infer and leaving it out answers `400`.
- `limit`, `offset`, `sort` — as in `GET /api/v1/data/{table}`. Without `sort` the relationship's own `sort_related` applies, but only when the side being read is the relationship's right side. A `sort` the caller gives is always checked: a field the related table does not have answers `400 Unknown Field`.

The caller needs read access to **both** tables.

An **empty match field relates to nothing**: a record with nothing in its match field answers `200 OK` with `[]`, rather than matching every related record whose own match field is also empty.

#### Errors
- `404 Relationship Not Found` — no such relationship, or it does not reach `{table}`, or the named `occurrence` is not one of its two sides.
- `400 Unknown Field` — a `sort` field the related table does not have.

#### Response `200 OK`
```json
[
  {"id": "cu-1", "company": "Favorite Bakery", "last_name": "Soto"},
  {"id": "cu-2", "company": "Favorite Bakery", "last_name": "Alvarez"}
]
```

---

### `POST /api/v1/data/{table}/{id}/related`

Creates a record on the other side of a relationship and fills its match field from record `{id}`, which is what typing into the last row of a portal does. Takes the same `relationship` and `occurrence` query parameters.

The match field belongs to the relationship: a value sent for it in the body is **overwritten** with the parent's, so a request cannot aim the new record at a different parent.

The caller needs read access to `{table}` and write access to the related table.

#### Errors
- `403 Creation Not Allowed` — the relationship's `allow_creation` is off, or it matches with something other than `=`, in which case there is no single value to put in the new record's match field.
- `422 No Match Value` — the parent's match field is empty, so a new record would have nothing to be matched by.
- `404 Relationship Not Found` — as above.

#### Response `201 Created`
The created record, as `POST /api/v1/data/{table}` returns it.

---

### Value lists

A **value list** is a named set of values a field can be filled from. It decides what a field *offers*; it does not restrict what may be stored — that is the `existing_value` validation rule, so the two choices stay separate.

A list is either `custom` (values typed in, one per line in `custom_values`) or `field` (the distinct values `source_column_id` already holds, which makes the list grow with the data). A field list is capped at 500 values.

| Method and path | Role | Does |
| --- | --- | --- |
| `GET /api/v1/value-lists` | any session | Lists the value lists, by name. |
| `GET /api/v1/value-lists/{id}/values` | any session | Resolves a list to the values it offers right now: `{"values": ["New", "Continuing"]}`. For a `field` list this reads the table, so the answer reflects the data as it is. |
| `POST /api/v1/value-lists` | admin | Creates one. |
| `PUT /api/v1/value-lists/{id}` | admin | Replaces its definition. |
| `DELETE /api/v1/value-lists/{id}` | admin | Removes it. Layout fields that pointed at it fall back to a plain edit box; no record data changes. |

The same routes are also mounted under `/api/v1/schemas/value-lists`.

#### Request body
```json
{
  "name": "Customer Types",
  "kind": "custom",
  "custom_values": "New\nContinuing"
}
```
or
```json
{
  "name": "Cities In Use",
  "kind": "field",
  "source_table_id": "…",
  "source_column_id": "…"
}
```

A list with no name, a `custom` list with no values, or a `field` list without its table and field answers `422 Invalid Field Options`. Two lists cannot share a name (`409 Value List Already Exists`). An id that is not there answers `404 Value List Not Found`.

Value lists are carried in the solution file and matched by name on import, so re-importing the same file updates them instead of making copies; a `field` list has its table and column rewritten to the destination's ids.

A layout's field object says which control it uses and which list fills it:

```json
{
  "field_name": "customer_type",
  "control_style": "radio_button_set",
  "value_list_id": "…"
}
```

`control_style` is `edit_box`, `drop_down_list`, `pop_up_menu`, `checkbox_set` or `radio_button_set`. Everything but `edit_box` needs a value list; without one the field is drawn as an edit box. A `checkbox_set` keeps the chosen values in the field as a newline-separated list.

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
Dumps all records and table rows from the active database into a binary MessagePack data file (`.f4data`). Requires the `owner` or `admin` role and the `bulk_export` privilege (#39).

#### Response `200 OK`
- `Content-Type: application/x-msgpack`
- Binary MessagePack payload (.f4data)

---

### `POST /api/v1/solutions/import-data`
Restores records into the session's database from a MessagePack `.f4data` payload, in **one transaction**. Requires the `owner` or `admin` role and the `bulk_import` privilege (#39).

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
  "capabilities": ["bulk_export", "bulk_import"],
  "expires_at": "2026-10-06T20:00:00Z"
}
```

`capabilities` lists the extended privileges the account's role holds (#39), so a client can say what the account may do before trying it. The server enforces them on every request regardless.

#### Errors
- `401 Unauthorized`: invalid username or password, deactivated account, or the database has no File4Base catalog.
- `404 Not Found`: the database does not exist.
- `422 Unprocessable Entity`: `database` is missing.

---

### `POST /api/v1/auth/logout`
Revokes the session of the bearer token. Response `204 No Content`.

---

### `GET /api/v1/auth/session`
Returns the user, role, layout permissions, extended privileges and database of the current session.

#### Response `200 OK`
```json
{
  "database": "invoices_db",
  "capabilities": ["bulk_export", "bulk_import"],
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

### `GET /api/v1/security/privileges`
Returns the extended privileges each account role holds (#39). Requires the `owner` or `admin` role.

An extended privilege is an **action** the server performs and can therefore refuse. File4Base does not restrict access by client: the desktop client, the Web Client and a direct REST call all speak to this API with the same session token, so the server cannot tell them apart, and a setting that claimed to would restrict nothing.

A role with nothing stored holds every privilege, so a database created before this table existed is not restricted by upgrading. An `owner` always holds every privilege and is reported as such whatever is stored.

#### Response `200 OK`
```json
{
  "capabilities": ["bulk_export", "bulk_import"],
  "privileges": [
    {"role": "owner", "bulk_export": true, "bulk_import": true},
    {"role": "admin", "bulk_export": true, "bulk_import": false},
    {"role": "user", "bulk_export": false, "bulk_import": false}
  ]
}
```

| Capability | Covers |
| --- | --- |
| `bulk_export` | `POST /api/v1/data/{table}/export` and `GET /api/v1/solutions/export-data`. |
| `bulk_import` | `POST /api/v1/data/{table}/import`, `POST /api/v1/data/{table}/import/preview` and `POST /api/v1/solutions/import-data`. |

Reading or writing one record at a time is not a bulk privilege: that is governed by the account role and by the per-table and per-layout access levels.

---

### `PUT /api/v1/security/privileges`
Replaces the extended privileges of the roles in the request. Requires the `owner` role: granting a privilege to one's own role would be no restriction at all.

Roles left out of the body keep what they hold. The `owner` entry is stored fully granted whatever it says, so that an owner cannot lock themselves out of their own database. The change applies to sessions that are already open, on the next request they make.

#### Request Body
```json
{
  "privileges": [
    {"role": "admin", "bulk_export": true, "bulk_import": false},
    {"role": "user", "bulk_export": false, "bulk_import": false}
  ]
}
```

#### Response `200 OK`
The same body as `GET /api/v1/security/privileges`.

#### Errors
- `403 Forbidden`: the caller is not an owner.
- `422 Unprocessable Entity`: a role File4Base does not have.

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


