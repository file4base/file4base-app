# File4Base Database Abstraction Layer (DBAL) & Multi-Engine Specification

## 1. Vision & Concept

File4Base aims to replicate the unified, integrated database experience of **File4base Pro** (database engine + UI builder + business logic) using an open modern stack:
- **Backend**: Go (1.22+) implementing a database-agnostic metadata engine, DBAL/query builder, and generic CRUD/DDL handlers.
- **Frontend**: Flutter (desktop target: macOS/Windows/Linux) providing a visual schema designer (Relationship Graph), Drag-and-Drop Form/Layout builder, and block-based Script Workspace.
- **Primary Database Engine**: PostgreSQL 16+.
- **Secondary / Swappable Engines**: MariaDB / MySQL 8.0+, SQLite (embedded / single-file offline mode).

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
      │ PostgreSQL     │ │ MariaDB/MySQL  │ │ SQLite (Optional) │
      │ (Default Engine│ │ (Alternative)  │ │ (Embedded/Local)  │
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
  - SQLite: Typeless affinity, different date functions, PRAGMA statements.
- **DDL Execution**:
  - Dynamically running `CREATE TABLE`, `ALTER TABLE ADD COLUMN`, `DROP COLUMN`, `CREATE INDEX` according to the target engine dialect.
- **Type Casting & Native Conversions**:
  - Mapping agnostic `NUMBER` to `NUMERIC` / `DECIMAL` / `BIGINT`.
  - Mapping agnostic `CONTAINER` to `BYTEA` (Postgres) or `LONGBLOB` (MariaDB).

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
- `cascade_delete` (Boolean)
- `allow_creation` (Boolean, allow creating related records via portal)

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

# Alternative: SQLite
# DB_ENGINE=sqlite
# DATABASE_URL=file:file4base.db?cache=shared&mode=rwc
```

The Go server initializes the corresponding `Dialect` and connection pool at bootstrap based on `DB_ENGINE`.
