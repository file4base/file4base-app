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
| Summary Fields (`SUM`, `COUNT`, `AVERAGE`, `MIN`, `MAX`) & Sub-Summary Grouped Reports | New Layout Assistant + Sub-Summary layout parts | Server-side summary aggregation (`internal/schema/summaries.go`) + Preview/List renderer | **Verified** | [`summary_fields_and_reports.md`](summary_fields_and_reports.md) (`#32`) |
| New Layout Assistant (Form, List, Report, Labels, Blank), Merge Fields (`{{field}}`), and Layout Themes | `Layouts > New Layout / Report...`, `Tools > Layout Themes...` | `new_layout_wizard_dialog.dart`, `manage_themes_dialog.dart` (`file4base_theme_v1` JSON copy/paste/export) | **Verified** | [`layout_assistant_and_merge_fields.md`](layout_assistant_and_merge_fields.md), `client/test/new_layout_wizard_test.dart` (`#33`) |
| Native Charts (Column, Bar, Line, Area, Pie, Scatter) | Layout tool palette & Chart Setup inspector | `ChartWidget` rendered from found-set or related records | **Verified** | [`charts.md`](charts.md), `client/test/chart_widget_test.dart` (`#37`) |
| Interactive Containers (Images, PDF preview, Audio/Video metadata, Binary download) | Container field control & context menu | Base64 data URI storage with MIME sniffing | **Verified** | `client/test/` (`#19`) |
| Horizontal Part Rail & Slate/Sky-Cyan Studio Tokens | Layout Mode canvas part divider badges | `_HorizontalPartDivider` in `layout_canvas.dart` | **Verified** | `client/test/layout_canvas_ux_test.dart` (`#40`) |

### 2.4 Developer Tools, Packaging & Interoperability
| Capability | UI / Config | Backend / Runtime | Status | Specification & Test Evidence |
|---|---|---|---|---|
| Solution Bundle Export/Import (`.f4p` / `.f4b`) & Data Snapshot (`.f4data`) with Persistent Export Directory | `File > Save a Copy As...`, `Export Solution`, `Open Solution` | `SolutionStorageService` (persists chosen directory), `internal/schema/solution_service.go` | **Verified** | [`solution_bundle_format.md`](solution_bundle_format.md), `bundle_golden_test.go` (`#12`, `#14`) |
| Tabular Record Import/Export (CSV, TSV, XLSX, `FMPXMLRESULT` / repeating XML) | `File > Import Records`, `File > Export Records...` | `internal/dataio` with dry-run preview & single-transaction atomic import | **Verified** | [`importing_and_exporting_records.md`](importing_and_exporting_records.md), `dataio_test.go` (`#38`) |
| Perform Script Into Another Solution (file references mapped explicitly to a solution, windowless execution on that solution's session, never a same-named local fallback) | `Perform Script` step with `file_reference` | `sys_file_references`, `internal/schema/file_reference_service.go`, `/api/v1/file-references/{name}/resolve`; client `LayoutActionRunner` with a peer session and no window | **Verified** | [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 18, [`filemaker_migration.md`](filemaker_migration.md) § 6, `internal/api/file_references_test.go`, `client/test/external_perform_script_test.dart` (`#107`) |
| Scripted Record Transfers Between Solutions (persisted mappings, stable match keys, add/update mode, unmatched & rejected-row policies, idempotent replay) | `Import Records` / `Export Records` script steps naming a transfer definition | `sys_transfers`, `internal/schema/transfer_service.go`, `internal/transfer` over `data.ImportRecords`; `/api/v1/transfers` with a session for **each** end (`peer_token`), zero stored credentials | **Verified** | [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 17, `internal/transfer/transfer_integration_test.go`, `internal/api/transfers_test.go`, `client/test/script_execution_test.dart` (`#96`) |
| Scheduled Transfers (unattended runs on a UTC cron schedule, run-as account per solution re-checked every run, solutions pinned at authorization, claimed once across server processes, bounded run history) | `/api/v1/transfer-schedules` (no client dialog yet) | `sys_transfer_schedules`, `sys_transfer_schedule_runs`, `internal/transfer/scheduler.go`, `internal/cronexpr`; zero stored credentials; `FILE4BASE_TRANSFER_SCHEDULER` | **Verified** | [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 19, `internal/transfer/scheduler_integration_test.go`, `internal/api/transfer_schedules_test.go`, `internal/schema/transfer_schedule_service_test.go`, `internal/cronexpr/cronexpr_test.go` (`#108`) |
| External SQL Data Sources (PostgreSQL, MySQL/MariaDB, SQL Server) | `File > Manage > External Data Sources...` | `internal/extsource` read-only bounded query importer (zero stored passwords) | **Verified** | [`external_sql_sources.md`](external_sql_sources.md) (`#47`) |
| Database Design Report (DDR) & Save a Copy as XML | `Tools > Database Design Report...`, `Tools > Save a Copy as XML...` | `internal/designreport` (`GET /api/v1/solutions/design-report?format=html\|xml\|json`) | **Verified** | [`design_report.md`](design_report.md) (`#49`) |
| Data Viewer (`Current` record fields & live `Watch` expression evaluator) | `Tools > Data Viewer` | `POST /api/v1/data/{table}/evaluate` using `internal/calc` | **Verified** | [`data_viewer.md`](data_viewer.md) (`#50`) |
| Script Debugger (step-by-step execution, breakpoints, skip disabled, halt on unsupported) | `Tools > Script Debugger` | `ScriptDebugSession` driving `LayoutActionRunner` | **Verified** | [`script_debugger.md`](script_debugger.md), `client/test/script_debugger_test.dart` (`#51`) |
| Custom Menus (`extend`, `replace` with standard items preset, `hide` entire menu, custom shortcuts, script actions, `[Standard File4Base Menus]`) | `Tools > Custom Menus`, `File > Manage > Custom Menus...` | `sys_menu_sets`, `internal/schema/menu_set_service.go` | **Verified** | [`custom_menus.md`](custom_menus.md), `client/test/custom_menus_test.dart` (`#52`) |
| Developer Utilities (Clone without records, Rename solution, Kiosk Mode, AES-256-GCM encryption, Permanent Design Lock) | `Tools > Developer Utilities...` | `internal/schema/developer_utilities.go`, `PUT /api/v1/solutions/options`, `RequireDesignUnlocked` | **Verified** | [`developer_utilities.md`](developer_utilities.md) (`#53`) |
| Cross-Solution Structure & Theme Copy/Paste (`f4b.structure_clipboard` & `file4base_theme_v1` JSON) | Copy/Paste in Manage Database, Layouts, Scripts, and `Tools > Layout Themes...` | `internal/schema/clipboard_service.go` (`/clipboard/copy`, `/preview`, `/paste`), `ManageThemesDialog` | **Verified** | [`copy_paste_structure.md`](copy_paste_structure.md) (`#54`) |
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
| **Control & Dialogs** | `Perform Script` | Executes a named sub-script from the catalog. With a `file_reference`, executes the script of **another solution** — the one the reference is mapped to — on a session the user opened there, with no window of its own; it is never resolved against the calling solution (`#107`). |
| **Control & Dialogs** | `Exit Script`, `Halt Script` | Cleanly terminates the current script or halts the entire execution session. A halt reaches every caller (`#95`). |
| **Control & Dialogs** | `If`, `Else If`, `Else`, `End If` | Evaluates the condition and takes the branch it picks; a condition File4Base cannot evaluate stops the script rather than guessing (`#93`). |
| **Control & Dialogs** | `Loop`, `Exit Loop If`, `End Loop` | Repeats the steps between them until an `Exit Loop If` holds. Nested loops leave only the loop they are in, branches opened inside a turn are closed on the next, and a loop with no way out is stopped and says so rather than hanging (`#96`). |
| **Control & Dialogs** | `Set Variable` | Assigns to a local `$` or global `$$` variable; a value it cannot evaluate stops the script (`#93`). |
| **Control & Dialogs** | `Show Custom Dialog` | Displays a modal message dialog with configurable title and body. |
| **Integration** | `Open URL` | Validates and opens an `http`/`https`/`mailto` URL. |
| **Integration** | `Import Records`, `Export Records` | Runs the **transfer definition** named in the step: records cross between two solutions by explicit mappings and stable match keys, in one transaction per destination table (`#96`). The counts land in `$$transferAdded`, `$$transferUpdated`, `$$transferSkipped`, `$$transferRejected` and `$$transferCommitted`, so the steps after it can test what happened with an ordinary `If`. See [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 17. |
| **Documentation** | `Comment` (`#`) | No-op annotation step recorded in the execution trace. |

### 4.2 Catalog-Only / Halting Script Steps
Steps that are stored in the catalog or imported for inspection but **not executed** — `Insert from URL` and `Print` — are **never silently ignored**: when one is reached during execution or debugging, `LayoutActionRunner` stops the script and reports the exact step name that needs support it does not yet have (`#20`, `#51`).

An `Import Records` or `Export Records` step that names **no transfer**, or one that names a transfer this solution does not have, stops the script the same way. In particular it is never answered with a *script* of the same name: a transfer and a script are different objects, and falling back from one to the other would run something nobody asked for (`#96`).

A transfer whose far end is another solution also stops the script when this session is not signed in to that solution. The step reports which solution it needs, because File4Base moves records only on authority it has been given — the definition carries no credential of its own.

A `Perform Script` that names a **file reference** stops the script when the reference is unknown, unmapped, or mapped to a solution this session is not signed in to — and in none of those cases is it answered with a script of the calling solution that shares the name (`#107`). A script performed in another solution stops at its first step that acts on a window (`New Record`, `Set Field`, `Go to Layout`, `Perform Find`, `Commit Records`, …), because the window on screen belongs to the calling solution.

### 4.3 Script Triggers
| Trigger Scope | Supported Triggers | Migration Quarantine Policy (`#26`) |
|---|---|---|
| **File Lifecycle** (`File > File Options...`) | `OnFirstWindowOpen`, `OnLastWindowClose` | Quarantined (disabled) on FileMaker XML import until reviewed by an administrator. |
| **Layout Lifecycle** (`Layout Setup > Script Triggers`) | `OnRecordLoad`, `OnRecordCommit`, `OnRecordRevert`, `OnLayoutEnter`, `OnLayoutExit` | Quarantined (disabled) on FileMaker XML import until reviewed by an administrator. |
| **Object Events** (Inspector) | Button `OnClick` (`Perform Script` or single action) | Preserved with target script reference; never auto-executed during import. |
