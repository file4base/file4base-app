# File4Base Database Abstraction Layer (DBAL) & Multi-Engine Specification

## 1. Vision & Concept

File4Base provides an integrated relational database engine, visual layout builder, and business-logic workspace—comparable in functional scope to Claris **FileMaker Pro** (see [`docs/PROVENANCE.md`](../PROVENANCE.md))—implemented on an open modern stack:
- **Backend**: Go (1.26+) implementing a database-agnostic metadata engine, DBAL/query builder, and generic CRUD/DDL handlers.
- **Frontend**: Flutter (desktop target: macOS/Windows/Linux) providing a visual schema designer (Relationship Graph), Drag-and-Drop Form/Layout builder, and block-based Script Workspace.
- **Primary Database Engine**: PostgreSQL 16+.
- **Secondary / Swappable Engines**: MariaDB 11, and SQLite: one file per solution database, no database server (see [sqlite_engine.md](sqlite_engine.md)).

To achieve database portability, File4Base introduces an internal **DBAL (Database Abstraction Layer)** and schema dialect engine, separating agnostic models and queries from database-specific SQL generation.

---

## 2. Two-Layer DBAL Architecture

```
┌────────────────────────────────────────────────────────┐
│                   Flutter Client                       │
│    (Visual Relationship Graph, Layouts, Data Grid)     │
└───────────────────────────┬────────────────────────────┘
                            │ REST / WebSocket JSON
┌───────────────────────────▼────────────────────────────┐
│                    Go Backend Core                     │
│                                                        │
│  [Layer 1: Agnostic Metadata & Dynamic Model Engine]   │
│   - sys_tables, sys_columns, sys_relationships         │
│   - Agnostic Query Model (Filter AST, Sort, Pagination)│
│   - Calculated Fields & Validation Rules               │
│                                                        │
│  [Layer 2: DBAL Gateway & Dialect Driver Interface]    │
│   - Dialect Interface (DDL generator, SQL formatter)   │
│   - Type Mapping (Agnostic types <-> Engine types)     │
│   - Connection Pool & Driver Abstraction               │
└──────────────┬─────────────────┬───────────────────────┘
               │                 │
      ┌────────▼───────┐ ┌───────▼────────┐ ┌───────────────────┐
      │ PostgreSQL     │ │ MariaDB/MySQL  │ │ SQLite            │
      │ (Default Engine│ │ (Alternative)  │ │ (File per database│
      └────────────────┘ └────────────────┘ └───────────────────┘
```

### Layer 1: Agnostic Model & Query Definitions
The application domain interacts only with abstract definitions:
- **Tables & Columns**: Agnostic column types (`TEXT`, `NUMBER`, `DATETIME`, `BOOLEAN`, `CONTAINER`/blob, `CALCULATION`).
- **Query Representation**: AST (Abstract Syntax Tree) representing searches (`Find Requests`), logical operators (`AND`, `OR`, `NOT`), wildcards, and relationship joins.
- **Transactions & Units of Work**: Standardized interfaces for begin, commit, rollback, and savepoints.

### Layer 2: Dialect Translation & Drivers (The Gateway)
The DBAL driver encapsulates all database engine peculiarities:
- **SQL Syntax Differences**:
  - PostgreSQL: `LIMIT $1 OFFSET $2`, identifier quotes `"column"`, `RETURNING id`, schemas (`public`).
  - MariaDB / MySQL: `LIMIT ? OFFSET ?`, identifier quotes `` `column` ``, `AUTO_INCREMENT`, `LAST_INSERT_ID()`.
  - SQLite: `$N` placeholders and quotes as PostgreSQL; type `CHECK`s on every column, the column changes SQLite lacks emulated by the connection, Unicode `lower`/`upper` and the `F4B` sort collation registered on the driver ([sqlite_engine.md](sqlite_engine.md)).
- **DDL Execution**:
  - Dynamically running `CREATE TABLE`, `ALTER TABLE ADD COLUMN`, `DROP COLUMN`, `CREATE INDEX` according to the target engine dialect.
- **Type Casting & Native Conversions**:
  - Mapping agnostic `NUMBER` to `NUMERIC` / `DECIMAL` / `BIGINT`.
  - Mapping agnostic `CONTAINER` to `BYTEA` (Postgres) or `LONGBLOB` (MariaDB).
- **Dates and timestamps**:
  - Every record write binds a timestamp as the instant it names, in UTC, and a date as its `YYYY-MM-DD` day (`dbal.TemporalValue`). MariaDB is given a timestamp as a UTC `DATETIME(6)` literal (`2026-01-02 03:04:05.000000`), not a `time.Time`, which its driver writes in the connection's zone and, for midnight on 1 January 0001, as `0000-00-00`.
  - A find criterion on a date or timestamp compares the column with the edges of the span its text names, never the column's text, which each engine writes its own way: `2026` is the year, `2026-01` or `01/2026` the month, `2026-01-02`, `02 Jan 2026`, `31/01/2026` or `01/31/2026` the day, `2026-01-02 03:04` the minute, `15:30` a time of day (on 1 January 0001, where a Time field keeps one). `=`, a typed value and `==` find the span; `<`, `<=`, `>`, `>=` and `...` (either end may be open) compare with its edges, so `2026-01-01...2026-01-31` takes in all of the 31st on a timestamp field. A text without a zone is in UTC. A day such as `02/01/2026` that could be read day first or month first is refused with `400`, naming both readings. A wildcard pattern, or text that names no date or time, is still compared as text.

---

## 3. Core Go Interface Definitions

```go
package dbal

import (
	"context"
	"database/sql"
)

// EngineType represents supported database engines
type EngineType string

const (
	EnginePostgres EngineType = "postgres"
	EngineMariaDB  EngineType = "mariadb"
	EngineSQLite   EngineType = "sqlite"
)

// AgnosticFieldType abstracts cross-engine types
type AgnosticFieldType string

const (
	FieldTypeText        AgnosticFieldType = "TEXT"
	FieldTypeNumber      AgnosticFieldType = "NUMBER"
	FieldTypeDate        AgnosticFieldType = "DATE"
	FieldTypeTimestamp   AgnosticFieldType = "TIMESTAMP"
	FieldTypeBoolean     AgnosticFieldType = "BOOLEAN"
	FieldTypeContainer   AgnosticFieldType = "CONTAINER"   // Blobs, files, images
	FieldTypeCalculation AgnosticFieldType = "CALCULATION" // Dynamic/Stored formulas
	FieldTypeSummary     AgnosticFieldType = "SUMMARY"     // Aggregations
)

// Dialect handles engine-specific SQL formatting and DDL generation
type Dialect interface {
	Name() EngineType
	QuoteIdentifier(name string) string
	Placeholder(index int) string
	MapType(agnosticType AgnosticFieldType, options ColumnOptions) string
	
	// DDL Generators
	BuildCreateTableSQL(table *TableDefinition) (string, error)
	BuildAddColumnSQL(table string, col *ColumnDefinition) (string, error)
	BuildDropColumnSQL(table string, colName string) (string, error)
	BuildCreateIndexSQL(table string, index *IndexDefinition) (string, error)
	
	// DML & Query Generators
	BuildSelectSQL(query *AgnosticQuery) (string, []interface{}, error)
	BuildInsertSQL(table string, record map[string]interface{}) (string, []interface{}, error)
	BuildUpdateSQL(table string, id interface{}, record map[string]interface{}) (string, []interface{}, error)
	BuildDeleteSQL(table string, id interface{}) (string, []interface{}, error)
}

// DatabaseDriver is the runtime connection gateway
type DatabaseDriver interface {
	Dialect() Dialect
	Exec(ctx context.Context, sql string, args ...interface{}) (sql.Result, error)
	Query(ctx context.Context, sql string, args ...interface{}) (*sql.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) *sql.Row
	BeginTx(ctx context.Context) (Transaction, error)
	Close() error
}

// Transaction abstracts atomic operations
type Transaction interface {
	Exec(ctx context.Context, sql string, args ...interface{}) (sql.Result, error)
	Query(ctx context.Context, sql string, args ...interface{}) (*sql.Rows, error)
	Commit() error
	Rollback() error
}
```

---

## 4. System Metadata Engine (`sys_*`)

To allow dynamic table and field creation without code recompilation, metadata is maintained in dedicated system tables:

### 1. `sys_tables`
- `id` (UUID / Serial, PK)
- `name` (Identifier used in SQL)
- `display_name` (Human-readable name in File4Base)
- `description` (Text)
- `created_at`, `updated_at`

### 2. `sys_columns`
- `id` (UUID / Serial, PK)
- `table_id` (FK -> `sys_tables.id`)
- `name` (Identifier in SQL)
- `display_name` (Label shown in forms)
- `field_type` (`TEXT`, `NUMBER`, `DATE`, `TIMESTAMP`, `BOOLEAN`, `CONTAINER`, `CALCULATION`, `SUMMARY`)
- `is_nullable` (Boolean)
- `is_primary_key` (Boolean)
- `default_value` (Text)
- `calculation_formula` (Text, if `CALCULATION`)
- `validation_rules` (JSONB / JSON text: range, regex, required, unique)

### 3. `sys_relationships` (Relationship Graph)
- `id` (UUID / Serial, PK)
- `name` (Identifier of occurrence)
- `left_table_id` (FK -> `sys_tables.id`)
- `left_column_id` (FK -> `sys_columns.id`)
- `right_table_id` (FK -> `sys_tables.id`)
- `right_column_id` (FK -> `sys_columns.id`)
- `operator` (`=`, `!=`, `<`, `<=`, `>`, `>=`)
- `cascade_delete` (Boolean: deleting a left record deletes the right records it matches)
- `allow_creation` (Boolean: right records may be created from a left record, as a portal's last row does)
- `sort_related` (Text, nullable: the order of the right records read from a left record)
- `left_cascade_delete`, `left_allow_creation` (Boolean, default false), `left_sort_related` (Text, nullable): the same options of the left side, acting on the left records from a right one (`server/migrations/006_relationship_left_options.sql`)
- `left_column_name`, `right_column_name` (field names kept beside the ids; the only name of a side on an occurrence of another solution's table, whose `*_column_id` is NULL)

### 4. `sys_relationship_predicates` (Further Match Criteria)
A relationship's first match criterion is the row above; each further one is a row here, and a record is related when it meets all of them.
- `relationship_id` (FK -> `sys_relationships.id`, cascade delete)
- `sequence_idx` (Integer, from 1, unique per relationship)
- `left_column_id`, `right_column_id` (FK -> `sys_columns.id`, cascade delete), with `left_column_name`, `right_column_name`
- `operator` (`=`, `≠`, `<`, `≤`, `>`, `≥`, `×`)
- `left_column_id`, `right_column_id` are NULL on a side on an occurrence of another solution's table, named by `*_column_name`.

### 5. `sys_table_occurrences` and `sys_file_references` (Other Solutions' Tables)
An occurrence of a table of **another solution** (`#107`) has a NULL `base_table_id` and instead:
- `file_reference_id` (`sys_file_references.id`): the reference it goes through;
- `external_table` (the table's name in that solution, NULL until bound; names, not ids, because ids differ between databases).

A file reference keeps `paths` (TEXT, one FileMaker path per line) beside its `name` and `database_name`. A value list taken from a field of another solution's table has NULL `source_table_id`/`source_column_id`, its external occurrence in `source_occurrence_id`, the field in `source_column_name` and its second field, if any, in `second_column_name` (VARCHAR(64), migration 008).

`sys_columns.source_field_id` (VARCHAR(32), digits, NULL for a column created here) keeps the FileMaker id of the field a migrated column comes from. A field of another solution that the source document names by id only is stored as the marker `#<id>` in `*_column_name` and `source_column_name` (an identifier never contains `#`), and matched against the other solution's `source_field_id` when the reference is bound.

A catalog created before these columns existed is upgraded by `EnsureSystemTables`, and the first time an occurrence, a file reference, a value list or the columns of a table are used on it (`server/migrations/005_external_occurrences.sql`): the catalog is read first, so an upgraded one is only read.

### 6. `sys_custom_functions` (Custom Functions)
A solution's own functions, as FileMaker's Manage Custom Functions defines them, which every formula of the solution may call (`server/migrations/007_custom_functions.sql`):
- `name` (VARCHAR(128), unique whatever its case: a unique index on `LOWER(name)` on PostgreSQL, the case-insensitive collation on MariaDB; never the name of a built-in function, which the server refuses)
- `parameters` (TEXT, a JSON array of the parameter names, in order)
- `formula` (TEXT, reading its parameters; it may call itself and the other custom functions)
- `access` (`all`, or `full_access` for a function only full-access accounts see in the design, FileMaker's *Availability*)

A catalog without the table has no custom functions; `EnsureSystemTables` creates it.

---

## 5. Configuration & Switching Database Engines

Switching between engines is controlled entirely through configuration:

### Environment Variables
```env
# Default: PostgreSQL
DB_ENGINE=postgres
DATABASE_URL=postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable

# Alternative: MariaDB / MySQL
# DB_ENGINE=mariadb
# DATABASE_URL=file4base:dev_password@tcp(localhost:3306)/file4base_dev?parseTime=true

# Alternative: SQLite (DATABASE_URL is the data directory, one <name>.sqlite per database)
# DB_ENGINE=sqlite
# DATABASE_URL=./data/solutions
# SQLITE_WRITER_WAIT=60s
# SQLITE_BUSY_TIMEOUT=5s
```

The Go server initializes the corresponding `Dialect` and connection pool at bootstrap based on `DB_ENGINE`. Without `DB_ENGINE` it uses `dbal.DefaultEngine` (PostgreSQL), the one place the default is set.
