# File4Base Implementation Roadmap

## Phase 1: Environment, DBAL Scaffolding & Foundation
- [x] Create root `docker-compose.yml` supporting both PostgreSQL 16 and optional MariaDB profiles.
- [x] Initialize Go server module (`server/go.mod`) with clean package structure.
- [x] Implement DBAL interface (`internal/dbal`):
  - [x] Agnostic types (`TEXT`, `NUMBER`, `DATE`, `TIMESTAMP`, `BOOLEAN`, `CONTAINER`, `CALCULATION`).
  - [x] Dialect interface for DDL and DML generation.
  - [x] PostgreSQL dialect implementation (`internal/dbal/postgres`).
  - [x] MariaDB dialect implementation (`internal/dbal/mariadb`).
  - [x] Driver factory switching via `DB_ENGINE` configuration.
- [x] Initialize Flutter desktop project skeleton in `client/` (macOS/Linux/Windows).


## Phase 2: Schema Engine & DDL (Backend Go)
- [x] Implement system catalog tables (`sys_tables`, `sys_columns`, `sys_relationships`).
- [x] Implement Go dynamic DDL service:
  - [x] Create Table (generates metadata + executes dynamic DDL on target engine).
  - [x] Add Column / Drop Column (with type mapping per dialect).
  - [x] Define Relationships between occurrences.
- [x] Expose REST API:
  - [x] `GET /api/v1/schemas/tables`
  - [x] `POST /api/v1/schemas/tables`
  - [x] `POST /api/v1/schemas/tables/{id}/columns`
  - [ ] `GET /api/v1/schemas/relationships`

## Phase 3: Visual Schema Designer (Flutter Client)
- [x] Build **Manage Database** Dialog in Flutter:
  - [x] **Tables Tab**: Create, edit, and delete tables.
  - [x] **Fields Tab**: Define field types, auto-enter options, and validation rules.
  - [x] **Relationships Graph Tab**: Visual canvas showing table occurrences and draggable link lines connecting keys.
- [x] Wire Flutter Manage Database UI to Go Schema REST API.


## Phase 4: Dynamic CRUD & Query Builder (Find Mode)
- [x] Implement generic dynamic CRUD endpoints in Go:
  - [x] `GET /api/v1/data/{table}` (with AST query filters, sorting, pagination).
  - [x] `POST /api/v1/data/{table}` (insert row with validation check).
  - [x] `PUT /api/v1/data/{table}/{id}` (update row).
  - [x] `DELETE /api/v1/data/{table}/{id}` (delete row with cascade check).
- [x] Implement Find Mode query translator in Go:
  - [x] Translate file4base find operators (`*`, `...`, `=`, `!`, `>`, `<`) into agnostic SQL AST.
- [x] Connect Flutter Grid / List view to generic CRUD endpoints (`DataBrowserWidget`).

## Phase 5: Dynamic Layout Engine & 4 Execution Modes
- [x] Define JSON layout schema specification (`docs/specs/layout_schema.json`).
- [x] Implement Flutter Mode State Machine:
  - [x] **Browse Mode** (`Cmd+B` / `Ctrl+B`): Form & Grid data viewing and inline editing.
  - [x] **Find Mode** (`Cmd+F` / `Ctrl+F`): Find requests, omit criteria, multi-request OR logic.
  - [x] **Layout Mode** (`Cmd+L` / `Ctrl+L`): Visual drag-and-drop WYSIWYG designer for forms and portals.
  - [x] **Preview Mode** (`Cmd+U` / `Ctrl+U`): Print and pagination preview.
- [x] Build Flutter Layout Renderer & Designer:
  - [x] Field inputs (Text, Number, Date, Dropdown/Value Lists, Checkbox).
  - [x] Portals (sub-table for 1:N related records).
  - [x] Parts (Top Navigation, Header, Body, Footer).
  - [x] Layout backend REST persistence (`/api/v1/schemas/layouts`).
  - [x] Official branding integration across macOS, Windows, Linux, and WebDirect.

## Phase 6: Real-Time Sync & Multi-User Collaboration
- [ ] Set up WebSocket hub in Go server.
- [ ] Implement DB event notifications (PostgreSQL `LISTEN/NOTIFY` with fallback to Go pub/sub for MariaDB).
- [ ] Connect Flutter client to WebSockets to refresh layouts and records in real-time.

## Phase 7: Calculation Engine & Script Workspace
- [ ] Embed formula parser in Go (arithmetic, string concatenation, logical tests, date math).
- [ ] Build **Script Workspace** in Flutter:
  - [ ] Action block palette (Navigation, Records, Control flow, Dialogs).
  - [ ] Script step runner executing actions deterministically.
