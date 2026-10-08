# File4Base Implementation Roadmap

## Phase 1: Environment, DBAL Scaffolding & Foundation
- [x] Create root `docker-compose.yml` (PostgreSQL 16) and `docker-compose.mariadb.yml` (MariaDB 11); the Go test suite runs against both engines in CI.
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
  - [x] `GET /api/v1/schemas/relationships`

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
- [x] Several find requests, unioned, with omitting requests subtracting, and saved finds: a named set of requests, stored in the catalog, carried in the solution file and run again from the Records or Requests menu ([#34](https://github.com/file4base/file4base-app/issues/34)).
- [x] Multi-field sort orders ([#35](https://github.com/file4base/file4base-app/issues/35)).
- [x] Find on a related field: a criterion on a field of a related record finds the records whose related records match it ([#46](https://github.com/file4base/file4base-app/issues/46), [relationships_and_portals.md](relationships_and_portals.md)).
- [x] Import records from CSV, tab-separated text, Excel workbooks and XML, with field mapping and one-transaction atomicity; export the found set as CSV, TSV or a workbook ([#38](https://github.com/file4base/file4base-app/issues/38), [docs/specs/importing_and_exporting_records.md](importing_and_exporting_records.md)). Records also import from another SQL database — PostgreSQL, MySQL/MariaDB or SQL Server — through a connection an owner registers, which stores no password ([#47](https://github.com/file4base/file4base-app/issues/47)).

## Phase 5: Dynamic Layout Engine & 4 Execution Modes
- [x] Define JSON layout schema specification (`docs/specs/layout_schema.json`).
- [x] Implement Flutter Mode State Machine:
  - [x] **Browse Mode** (`Cmd+B` / `Ctrl+B`): Form & Grid data viewing and inline editing.
  - [x] **Find Mode** (`Cmd+F` / `Ctrl+F`): Find requests, omit criteria, multi-request OR logic.
  - [x] **Layout Mode** (`Cmd+L` / `Ctrl+L`): Visual drag-and-drop WYSIWYG designer for forms and portals.
    - [x] Multiple selection (Shift-click, marquee), 8-handle resizing.
    - [x] Layout properties: size, background color and picture, OnLayoutEnter / OnLayoutExit script triggers, transition effect.
  - [x] **Preview Mode** (`Cmd+U` / `Ctrl+U`): Print and pagination preview, Page Setup dialog, paper formats, and printable boundaries calculation.
  - [x] **File Options** (`File > File Options...`): Startup credentials, startup layout selection, full-screen/toolbar suppression, and lifecycle script triggers.
- [x] Build Flutter Layout Renderer & Designer:
  - [x] Field inputs (Text, Number, Date, Dropdown/Value Lists, Checkbox).
  - [x] Portals (sub-table for 1:N related records).
  - [x] Drawn objects (line, rectangle, rounded rectangle, oval) with fill and line color/width, text inside shapes and embedded pictures/files (Insert menu).
  - [x] Button actions (single step or Perform Script) and Set Tab Order for Browse/Find keyboard navigation.
  - [x] Parts (Header, Body, Footer, and the sub-summary and grand summary parts a report is made of).
  - [x] Summary fields and grouped reports with subtotals and a grand total ([#32](https://github.com/file4base/file4base-app/issues/32)).
  - [x] The New Layout assistant: form, list, report, labels and blank, with a field picker, layout themes and label stock ([#33](https://github.com/file4base/file4base-app/issues/33)).
  - [x] Merge fields in layout text, with an empty line collapsing ([#33](https://github.com/file4base/file4base-app/issues/33)).
  - [x] Charts: column, bar, line, area, pie and scatter ([#37](https://github.com/file4base/file4base-app/issues/37)).
  - [x] Related fields on a layout, through a named relationship ([#36](https://github.com/file4base/file4base-app/issues/36)).
  - [x] Layout backend REST persistence (`/api/v1/schemas/layouts`).
  - [x] Official branding integration across macOS, Windows, Linux, and Web Client.

## Security & Multi-User Foundations (prerequisite for Phase 6)
- [x] Session-based API authentication (`POST /api/v1/auth/login` bearer tokens, logout, session introspection).
- [x] Per-session database binding (no server-wide active database).
- [x] Server-side enforcement of roles (`owner`, `admin`, `user`) and per-layout permissions.
- [x] Data API restricted to catalog tables and fields; system tables unreachable.
- [x] Extended privileges: bulk record export and import granted per account role and enforced on every request ([#39](https://github.com/file4base/file4base-app/issues/39), [security_model.md](security_model.md)). Access is not restricted by client, and nothing claims it is.
- [x] No default accounts; database creation provisions the first owner explicitly.
- [x] Go test suite and `go vet` in CI.
- [ ] Sign-in rate limiting and account lockout.
- [ ] Sessions shared across server replicas (required for horizontal scaling).

## Phase 6: Real-Time Sync & Multi-User Collaboration
- [ ] Set up WebSocket hub in Go server.
- [ ] Implement DB event notifications (PostgreSQL `LISTEN/NOTIFY` with fallback to Go pub/sub for MariaDB).
- [ ] Connect Flutter client to WebSockets to refresh layouts and records in real-time.

## Phase 7: Calculation Engine & Script Workspace
- [x] Implement script metadata and ordered step persistence in the system catalog (`sys_scripts`, `sys_script_steps`).
- [x] Build the Script Workspace UI with script listing, create, duplicate, delete, and step editing.
- [x] Add a calculation formula builder to the client for calculation fields.
- [ ] Complete the three-panel Script Workspace described in `docs/specs/script_workspace_spec.md`:
  - [x] Scripts Explorer with create, duplicate, and delete actions.
  - [x] Sequential step editor with enabled/disabled steps.
  - [ ] Contextual parameter inspector with formula constructor.
  - [ ] Categorized step catalog for Navigation, Records, Control & Logic, and Integration & Data.
- [ ] Implement drag-and-drop step reordering and visual conditional indentation (`If/Else/End If`, `Loop/End Loop`).
- [x] Formula calculation engine in Go: arithmetic, text joining, logical tests, comparisons and 20 functions, evaluated by the server so both engines agree. Written by hand rather than with Google CEL, whose grammar is not FileMaker's ([#30](https://github.com/file4base/file4base-app/issues/30), [docs/specs/calculation_formulas.md](calculation_formulas.md)).
- [ ] Script step runner executing actions deterministically with step-by-step debugging.
  - [x] Client-side runner for layout buttons: single step actions and Perform Script for record, navigation, Set Field, dialog and URL steps. Control flow (`If`, `Loop`), variables and integration steps stop the script with a message until the calculation engine and server runner exist.
