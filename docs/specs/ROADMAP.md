# File4Base Implementation Roadmap

## Phase 1: Environment, DBAL Scaffolding & Foundation
- [ ] Create root `docker-compose.yml` supporting both PostgreSQL 16 and optional MariaDB profiles.
- [ ] Initialize Go server module (`server/go.mod`) with clean package structure.
- [ ] Implement DBAL interface (`internal/dbal`):
  - [ ] Agnostic types (`TEXT`, `NUMBER`, `DATE`, `TIMESTAMP`, `BOOLEAN`, `CONTAINER`, `CALCULATION`).
  - [ ] Dialect interface for DDL and DML generation.
  - [ ] PostgreSQL dialect implementation (`internal/dbal/postgres`).
  - [ ] MariaDB dialect implementation (`internal/dbal/mariadb`).
  - [ ] Driver factory switching via `DB_ENGINE` configuration.
- [ ] Initialize Flutter desktop project skeleton in `client/` (macOS/Linux/Windows).

## Phase 2: Schema Engine & DDL (Backend Go)
- [ ] Implement system catalog tables (`sys_tables`, `sys_columns`, `sys_relationships`).
- [ ] Implement Go dynamic DDL service:
  - [ ] Create Table (generates metadata + executes dynamic DDL on target engine).
  - [ ] Add Column / Drop Column (with type mapping per dialect).
  - [ ] Define Relationships between occurrences.
- [ ] Expose REST API:
  - [ ] `GET /api/v1/schemas/tables`
  - [ ] `POST /api/v1/schemas/tables`
  - [ ] `POST /api/v1/schemas/tables/{id}/columns`
  - [ ] `GET /api/v1/schemas/relationships`

## Phase 3: Visual Schema Designer (Flutter Client)
- [ ] Build **Manage Database** Dialog in Flutter:
  - [ ] **Tables Tab**: Create, edit, and delete tables.
  - [ ] **Fields Tab**: Define field types, auto-enter options, and validation rules.
  - [ ] **Relationships Graph Tab**: Visual canvas showing table occurrences and draggable link lines connecting keys.
- [ ] Wire Flutter Manage Database UI to Go Schema REST API.

## Phase 4: Dynamic CRUD & Query Builder (Find Mode)
- [ ] Implement generic dynamic CRUD endpoints in Go:
  - [ ] `GET /api/v1/data/{table}` (with AST query filters, sorting, pagination).
  - [ ] `POST /api/v1/data/{table}` (insert row with validation check).
  - [ ] `PUT /api/v1/data/{table}/{id}` (update row).
  - [ ] `DELETE /api/v1/data/{table}/{id}` (delete row with cascade check).
- [ ] Implement Find Mode query translator in Go:
  - [ ] Translate file4base find operators (`*`, `...`, `=`, `!`, `>`, `<`) into agnostic SQL AST.
- [ ] Connect Flutter Grid / List view to generic CRUD endpoints.

## Phase 5: Dynamic Layout Engine & 4 Execution Modes
- [ ] Define JSON layout schema specification (`docs/specs/layout_schema.json`).
- [ ] Implement Flutter Mode State Machine:
  - [ ] **Browse Mode** (`Cmd+B` / `Ctrl+B`): Form & Grid data viewing and inline editing.
  - [ ] **Find Mode** (`Cmd+F` / `Ctrl+F`): Find requests, omit criteria, multi-request OR logic.
  - [ ] **Layout Mode** (`Cmd+L` / `Ctrl+L`): Visual drag-and-drop WYSIWYG designer for forms and portals.
  - [ ] **Preview Mode** (`Cmd+U` / `Ctrl+U`): Print and pagination preview.
- [ ] Build Flutter Layout Renderer:
  - [ ] Field inputs (Text, Number, Date, Dropdown/Value Lists, Checkbox).
  - [ ] Portals (sub-table for 1:N related records).
  - [ ] Parts (Top Navigation, Header, Body, Footer).

## Phase 6: Real-Time Sync & Multi-User Collaboration
- [ ] Set up WebSocket hub in Go server.
- [ ] Implement DB event notifications (PostgreSQL `LISTEN/NOTIFY` with fallback to Go pub/sub for MariaDB).
- [ ] Connect Flutter client to WebSockets to refresh layouts and records in real-time.

## Phase 7: Calculation Engine & Script Workspace
- [ ] Embed formula parser in Go (arithmetic, string concatenation, logical tests, date math).
- [ ] Build **Script Workspace** in Flutter:
  - [ ] Action block palette (Navigation, Records, Control flow, Dialogs).
  - [ ] Script step runner executing actions deterministically.
