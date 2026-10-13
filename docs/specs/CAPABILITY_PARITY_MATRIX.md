# Capability Parity Matrix & Scope Reference

This document provides the measurable engineering reference for **File4Base** capability parity against its functional baseline (informed by functional observation of Claris *FileMaker Pro 14 User's Guide* and *FileMaker Pro / Server 14–19 Help*; see [`docs/PROVENANCE.md`](../PROVENANCE.md)).

Every feature in this matrix distinguishes between:
1. **UI / Config Availability** — Whether the desktop/web client exposes the configuration surface.
2. **Backend / Runtime Execution** — Whether the Go server, DBAL, or Flutter runtime executes and enforces the behavior.
3. **Verification Evidence** — The automated test suites and specifications that verify the behavior across PostgreSQL 16+ and MariaDB 10.11+.

---

## 1. Target Reference Scope & Supported Platforms

### 1.1 Supported Platforms & Runtimes
| Layer | Supported Targets | Notes |
|---|---|---|
| **Desktop Client** | macOS, Linux, Windows (Flutter Desktop) | Full 4-mode studio, local `.f4p` / `.f4b` / `.f4data` file workflows, persistent export directory selection, and server connection selector. |
| **Browser Client** | File4Base Web Client (Flutter Web via Nginx) | Connects to any configured API endpoint (defaults to `http://127.0.0.1:8080`) over an AES-256-GCM encrypted payload channel (`F4BE\x01`). |
| **Backend Server** | Go 1.24+ (`file4base-api`) | Stateless REST API with per-session database binding and an embedded SQLite control store (`internal/control`) for project registry and granular permissions. |
| **Database Engines** | PostgreSQL 16+ (primary), MariaDB 10.11+ (supported) | Identical Go calculation and validation evaluation across both SQL engines. |

### 1.2 Explicit Non-Goals & Out-of-Scope Features
The following features from proprietary desktop/server ecosystems are **explicitly excluded** from File4Base's scope:
- **Direct binary `.fmp12` file execution or silent binary parsing** — `.fmp12` is a proprietary binary container. File4Base detects `.fmp12` uploads and guides users to the **Assisted FileMaker XML / DDR & `FMPXMLRESULT` Migration** workflow ([`docs/specs/filemaker_migration.md`](filemaker_migration.md), `#26`).
- **AI / LLM script steps and calculation functions** (e.g., FileMaker 2024+ vector embeddings, semantic search steps).
- **Mobile-only hardware sensors** (iOS/FileMaker Go location, beacon, barcode camera capture, Touch ID).
- **Locale-specific phonetic functions** (`Furigana`, Japanese kana conversion).
- **OS-level ODBC DSN Driver Manager integration** — File4Base uses server-side External SQL Data Sources (`#47`) over native PostgreSQL, MySQL/MariaDB, and SQL Server drivers without storing plaintext passwords in the catalog.
- **Proprietary plug-in SDK (`.fmplugin` C/C++ binaries)** and AppleEvents / ActiveX OS automation.

---

## 2. Domain Capability Matrix

### 2.1 Storage, Schema & Relational Integrity
| Capability | UI / Config | Backend / Runtime | Status | Specification & Test Evidence |
|---|---|---|---|---|
| Base Tables & Agnostic Field Types (`TEXT`, `NUMBER`, `DATE`, `TIMESTAMP`, `BOOLEAN`, `CONTAINER`, `CALCULATION`, `SUMMARY`) | Manage Database (`Tables` & `Fields` tabs) | Dynamic DDL in `internal/dbal` & `internal/schema` | **Verified** | `server/internal/schema/service_test.go` |
| Auto-Enter Options (serial numbers, creation/modification timestamps & account names, default data, calculated value, looked-up value) | Field Options > Auto-Enter tab | Enforced on `POST /api/v1/data/{table}` and `PUT` | **Verified** | `server/internal/data/service_test.go` (`#3`, `#4`) |
| Field Validation Rules (not empty, unique, existing value from value list, numeric range, max length, type check) | Field Options > Validation tab | Enforced in `internal/validation` & `internal/data` (`422 Unprocessable Entity`) | **Verified** | `server/internal/data/service_test.go` (`#4`, `#31`) |
| Table Occurrences (TO) & Visual Relationships Graph | Manage Database > Relationships tab | `sys_table_occurrences`, `sys_relationships`, persisted node coordinates | **Verified** | `server/internal/schema/occurrence_position_test.go`, `relationship_service_test.go` (`#24`) |
| Comparative Relationship Operators (`=`, `≠`, `<`, `≤`, `>`, `≥`), Allow Creation, Cascade Delete, Sort Related | Edit Relationship dialog | Recursive cascade delete & related record creation in `internal/data` | **Verified** | [`relationships_and_portals.md`](relationships_and_portals.md) (`#46`) |
| Value Lists (Custom static lists & Dynamic field-backed lists) | `File > Manage > Value Lists...` | `sys_value_lists`, `GET /api/v1/schemas/value-lists/{id}/values` | **Verified** | `server/internal/schema/value_list_test.go` (`#31`) |
| Repeating Fields & Global Storage Fields | Not exposed as separate storage checkboxes | Imported FileMaker repetitions/globals are converted to standard columns and flagged in the migration report | **Excluded / Flagged on Migration** | [`filemaker_migration.md`](filemaker_migration.md) (`#26`) |

### 2.2 Authorization, Security & Multi-User Isolation
| Capability | UI / Config | Backend / Runtime | Status | Specification & Test Evidence |
|---|---|---|---|---|
| Per-Session Database Binding & Bearer Token Auth | `DatabaseLoginDialog`, `OpenSolutionDialog` | `internal/auth` session store; no global active database | **Verified** | `server/internal/api/security_integration_test.go` (`#9`) |
| SQLite API Control Store (project ↔ database registry, options, granular permissions) | `DatabaseLoginDialog` API Server URL & Project selector | `internal/control/store.go` (`sys_projects`, `sys_table_permissions`, `sys_layout_permissions`) | **Verified** | `server/internal/control/store_test.go` |
| Encrypted Web-to-API Payload Channel (`F4BE\x01` AES-256-GCM) | Automatic in `ApiClient` (`PayloadCipher`) | `internal/crypto/channel.go` middleware & `/api/v1/crypto/handshake` | **Verified** | `server/internal/crypto/channel_test.go`, `client/test/payload_cipher_test.dart` |
| Role-Based Access (`owner`, `admin`, `user`) & Extended Capabilities (`bulk_export`, `bulk_import`, `edit_value_lists`, `edit_scripts`, `manage_external_sql`) | `File > Manage > Security...` (Accounts, Roles & Privileges, Table Access tabs) | Enforced on every schema, data, and solution route | **Verified** | [`security_model.md`](security_model.md), `server/internal/schema/privileges_test.go` (`#10`, `#11`, `#39`) |
| Granular Per-Table CRUD & Schema Permissions (`can_create`, `can_edit`, `can_delete`, `can_modify_schema`) | Manage Security > Table Access tab | Enforced in `DataHandler` and `SchemaHandler` via `internal/control` | **Verified** | `server/internal/api/security_integration_test.go` |
| Per-Layout Access Levels (`full`, `view`, `none`) | Manage Security > Layout Access | Enforced on layout reads and data browser rendering | **Verified** | `server/internal/schema/security_service_test.go` |
| Row-Level Security (RLS) custom boolean predicates | Planned | Not yet evaluated per row | **Planned** | Tracked in [`ROADMAP.md`](ROADMAP.md) |

### 2.3 Operational Modes, Layout Engine & Reporting
| Capability | UI / Config | Backend / Runtime | Status | Specification & Test Evidence |
|---|---|---|---|---|
| Four Operational Modes (`Browse`, `Find`, `Layout`, `Preview`) | Mode switcher, menu bar, shortcuts (`Cmd+B/F/L/U`) | Flutter mode state machine & layout canvas renderer | **Verified** | `client/test/phase5_modes_test.dart`, `layout_canvas_ux_test.dart` |
| Form, List, and Table Views with inline editing & multi-column sort | View switcher bar & `Records > Sort Records...` | `POST /api/v1/data/{table}/find` with multi-field sort | **Verified** | `server/internal/data/find_test.go` (`#35`) |
| Disjunctive Multi-Request Find, Omit Criteria, Wildcards (`*`, `...`, `=`, `!`, `<`, `≤`, `>`, `≥`) & Saved Finds | Find Mode toolbar & `Records > Saved Finds` | `sys_saved_finds`, AST query compiler in `internal/data` | **Verified** | `server/internal/schema/saved_find_service.go` (`#34`) |
| Find & Sort on Related Fields (`Occurrence::Field`) | Related field on layout in Find/Browse mode | Joined subquery evaluation in `internal/data` | **Verified** | [`relationships_and_portals.md`](relationships_and_portals.md) (`#36`, `#46`) |
| Summary Fields (`SUM`, `COUNT`, `AVERAGE`, `MIN`, `MAX`) & Sub-Summary Grouped Reports | New Layout Assistant + Sub-Summary layout parts | Server-side summary aggregation (`internal/schema/summaries.go`) + Preview/List renderer | **Verified** | [`summaries_and_reports.md`](summaries_and_reports.md) (`#32`) |
| New Layout Assistant (Form, List, Report, Labels, Blank), Merge Fields (`{{field}}`), and Layout Themes | `Layouts > New Layout / Report...` | `client/lib/features/layout_editor/new_layout_wizard_dialog.dart` | **Verified** | `client/test/new_layout_wizard_test.dart` (`#33`) |
| Native Charts (Column, Bar, Line, Area, Pie, Scatter) | Layout tool palette & Chart Setup inspector | `ChartWidget` rendered from found-set or related records | **Verified** | [`charts.md`](charts.md), `client/test/chart_widget_test.dart` (`#37`) |
| Interactive Containers (Images, PDF preview, Audio/Video metadata, Binary download) | Container field control & context menu | Base64 data URI storage with MIME sniffing | **Verified** | `client/test/` (`#19`) |
| Horizontal Part Rail & Slate/Sky-Cyan Studio Tokens | Layout Mode canvas part divider badges | `_HorizontalPartDivider` in `layout_canvas.dart` | **Verified** | `client/test/layout_canvas_ux_test.dart` (`#40`) |

### 2.4 Developer Tools, Packaging & Interoperability
| Capability | UI / Config | Backend / Runtime | Status | Specification & Test Evidence |
|---|---|---|---|---|
| Solution Bundle Export/Import (`.f4p` / `.f4b`) & Data Snapshot (`.f4data`) with Persistent Export Directory | `File > Save a Copy As...`, `Export Solution`, `Open Solution` | `SolutionStorageService` (persists chosen directory), `internal/schema/solution_service.go` | **Verified** | [`solution_bundle_format.md`](solution_bundle_format.md), `bundle_golden_test.go` (`#12`, `#14`) |
| Tabular Record Import/Export (CSV, TSV, XLSX, `FMPXMLRESULT` / repeating XML) | `File > Import Records`, `File > Export Records...` | `internal/dataio` with dry-run preview & single-transaction atomic import | **Verified** | [`importing_and_exporting_records.md`](importing_and_exporting_records.md), `dataio_test.go` (`#38`) |
| External SQL Data Sources (PostgreSQL, MySQL/MariaDB, SQL Server) | `File > Manage > External Data Sources...` | `internal/extsource` read-only bounded query importer (zero stored passwords) | **Verified** | [`external_sql_sources.md`](external_sql_sources.md) (`#47`) |
| Database Design Report (DDR) in HTML, XML, and JSON | `Tools > Database Design Report...` | `internal/designreport` (`GET /api/v1/solutions/design-report`) | **Verified** | [`design_report.md`](design_report.md) (`#49`) |
| Data Viewer (`Current` record fields & live `Watch` expression evaluator) | `Tools > Data Viewer` | `POST /api/v1/data/{table}/evaluate` using `internal/calc` | **Verified** | [`data_viewer.md`](data_viewer.md) (`#50`) |
| Script Debugger (step-by-step execution, breakpoints, skip disabled, halt on unsupported) | `Tools > Script Debugger` | `ScriptDebugSession` driving `LayoutActionRunner` | **Verified** | [`script_debugger.md`](script_debugger.md), `client/test/script_debugger_test.dart` (`#51`) |
| Custom Menus (`MenuSet`, role restriction, per-layout override, `[Standard File4Base Menus]`) | `File > Manage > Custom Menus...` | `sys_menu_sets`, `internal/schema/menu_set_service.go` | **Verified** | [`custom_menus.md`](custom_menus.md), `client/test/custom_menus_test.dart` (`#52`) |
| Developer Utilities (Clone without records, Rename solution, Permanent Design Lock) | `Tools > Developer Utilities...` | `internal/schema/developer_utilities.go`, `RequireDesignUnlocked` middleware | **Verified** | [`developer_utilities.md`](developer_utilities.md) (`#53`) |
| Cross-Solution Structure Copy & Paste (`file4base_clipboard` JSON with dry-run dependency preview) | Copy/Paste buttons in Manage Database, Value Lists, Layouts, Scripts | `internal/schema/clipboard_service.go` (`/clipboard/copy`, `/preview`, `/paste`) | **Verified** | [`copy_paste_structure.md`](copy_paste_structure.md) (`#54`) |
| Assisted FileMaker Migration (`.fmp12` detection + DDR XML & `FMPXMLRESULT` converter with preflight report) | `File > Migrate FileMaker Solution...` & `OpenSolutionDialog` | `internal/migration` (`/api/v1/solutions/migrate/preflight` & `/apply`) | **Verified** | [`filemaker_migration.md`](filemaker_migration.md) (`#26`) |

---

## 3. Calculation Engine Catalog (`internal/calc`)

Formulas are evaluated deterministically in Go by `server/internal/calc` ([`calculation_formulas.md`](calculation_formulas.md), `#30`) so PostgreSQL and MariaDB always produce identical results.

### 3.1 Supported Operators & Grammar
- **Arithmetic:** `+`, `-`, `*`, `/`, unary `+` / `-`
- **Text Concatenation:** `&`
- **Comparison:** `=`, `==`, `<>`, `!=`, `<`, `<=`, `>`, `>=` (case-insensitive text comparison matching Find mode; numeric coercion with strict error on invalid numeric text)
- **Logical (short-circuiting):** `AND`, `OR`, `NOT`, boolean literals `true` / `false`
- **Argument Separators:** Both `;` (FileMaker style) and `,` are accepted.

### 3.2 Supported Calculation Functions (20 Built-In Functions)
| Category | Supported Functions | Behavior |
|---|---|---|
| **Logical & Control** | `IF(test; whenTrue [; whenFalse])`, `ISEMPTY(value)`, `COALESCE(a; b; ...)` | Short-circuit conditional evaluation and empty/null checks. |
| **Text Manipulation** | `UPPER(text)`, `LOWER(text)`, `TRIM(text)`, `LENGTH(text)`, `CONCAT(a; b; ...)`, `LEFT(text; n)`, `RIGHT(text; n)`, `MIDDLE(text; start; count)` | Unicode-aware character counting and slicing (1-based indexing). |
| **Numeric & Row Math** | `ROUND(n [; places])`, `ABS(n)`, `SUM(a; b; ...)`, `MIN(a; b; ...)`, `MAX(a; b; ...)`, `AVERAGE(a; b; ...)` | Exact numeric rounding and multi-argument row calculations. |
| **Date & Time** | `TODAY()`, `NOW()`, `YEAR(date)`, `MONTH(date)`, `DAY(date)` | UTC date/timestamp evaluation and component extraction. |

### 3.3 Translation & Unsupported Function Policy
- **Translated Aliases during Migration (`#26`):** Common FileMaker expressions such as `Get ( CurrentDate )` → `TODAY()`, `Get ( CurrentTimestamp )` → `NOW()`, and `Case(c1; v1; default)` → nested `IF(...)` are automatically translated or flagged during assisted migration.
- **Unsupported Function Families:** `Let()`, `Evaluate()`, `ExecuteSQL()`, `While()`, `GetNthRecord()`, JSON/Cryptographic functions, and financial functions (`PMT`, `FV`, `NPV`) are rejected at formula save time with `422 Unprocessable Entity` and flagged as `manual_action_required` in the FileMaker Migration Preflight Report.

---

## 4. Script Workspace, Step Catalog & Trigger Matrix

Scripts are persisted in `sys_scripts` and `sys_script_steps` and executed by `LayoutActionRunner` and `ScriptDebugSession` ([`script_debugger.md`](script_debugger.md), `#20`, `#51`).

### 4.1 Executed Script Steps (Supported in Client Runner & Debugger)
| Category | Step Type | Runner & Debugger Behavior |
|---|---|---|
| **Navigation** | `Go to Layout` | Switches the active layout by name or ID; halts with an error if the target layout does not exist. |
| **Navigation** | `Go to Record/Request/Page` | Navigates to `First`, `Previous`, `Next`, `Last`, or a specific record number. |
| **Navigation** | `Enter Browse Mode`, `Enter Find Mode`, `Enter Preview Mode` | Transitions the operational mode state machine. |
| **Records** | `New Record/Request`, `Duplicate Record/Request`, `Delete Record/Request` | Creates, duplicates, or deletes the active record via the REST API. |
| **Records** | `Commit Records/Requests`, `Revert Record/Request` | Commits pending field edits to the server or reverts unsaved changes. |
| **Records** | `Set Field` | Updates a target field on the current record. |
| **Found Sets** | `Perform Find`, `Show All Records`, `Show Omitted Only`, `Omit Record`, `Sort Records` | Executes find queries, restores all records, or sorts the found set. |
| **Control & Dialogs** | `Perform Script` | Executes a named sub-script from the catalog. |
| **Control & Dialogs** | `Exit Script`, `Halt Script` | Cleanly terminates the current script or halts the entire execution session. |
| **Control & Dialogs** | `Show Custom Dialog` | Displays a modal message dialog with configurable title and body. |
| **Integration** | `Open URL` | Validates and opens an `http`/`https`/`mailto` URL. |
| **Documentation** | `Comment` (`#`) | No-op annotation step recorded in the execution trace. |

### 4.2 Catalog-Only / Halting Script Steps
Steps that can be stored in the catalog or imported for inspection (`If`, `Else If`, `Else`, `End If`, `Loop`, `Exit Loop If`, `End Loop`, `Set Variable`, `Insert from URL`, `Import Records`, `Export Records`, `Print`) are **never silently ignored**: when encountered during execution or debugging, `LayoutActionRunner` halts the script and reports the exact step name that requires server-side/expression runner support (`#20`, `#51`).

### 4.3 Script Triggers
| Trigger Scope | Supported Triggers | Migration Quarantine Policy (`#26`) |
|---|---|---|
| **File Lifecycle** (`File > File Options...`) | `OnFirstWindowOpen`, `OnLastWindowClose` | Quarantined (disabled) on FileMaker XML import until reviewed by an administrator. |
| **Layout Lifecycle** (`Layout Setup > Script Triggers`) | `OnRecordLoad`, `OnRecordCommit`, `OnRecordRevert`, `OnLayoutEnter`, `OnLayoutExit` | Quarantined (disabled) on FileMaker XML import until reviewed by an administrator. |
| **Object Events** (Inspector) | Button `OnClick` (`Perform Script` or single action) | Preserved with target script reference; never auto-executed during import. |
