# File4Base Functional Specification: Core file4base Feature Mapping

This specification translates the core features, workflows, and tools documented in the *file4base Pro User's Guide* into technical requirements for **File4Base**, built with **Go** (Backend DBAL & Metadata Engine) and **Flutter** (Desktop Frontend Layout & Script Engine).

---

## 1. Architectural Matrix: file4base Features to File4Base Stack

| Domain | file4base Pro 14 Feature | File4Base Technical Implementation | Target Layer |
|---|---|---|---|
| **Data Storage & Schema** | Tables, Fields, Types, Auto-Enter, Validation, Global storage, Indexes | Dynamic Metadata (`sys_tables`, `sys_columns`, `sys_table_occurrences`), DBAL (`internal/dbal`), dynamic SQL migrations | Go + PostgreSQL / MariaDB |
| **Relational Model** | Relationships Graph, Match Fields, Comparative operators, Cascading delete, Related creation, Lookups | Relationship Catalog (`sys_relationships`), DBAL JOIN builder, Lookup Triggers, Flutter GraphView UI (`custom_painter`) | Go + Flutter |
| **Operational Modes** | 4 Modes: Browse (`Cmd+B`), Find (`Cmd+F`), Layout (`Cmd+L`), Preview (`Cmd+U`) | Mode State Machine (Riverpod/Bloc), dynamic toolbar rendering, canvas mode switching | Flutter Client |
| **Data Views** | Form View, List View, Table View | Dynamic Grid/Table Widget, Paginated/Virtual ListView, Form Canvas with data binding | Flutter Client |
| **Find & Search Engine** | Quick Find, Find Requests, Omit / Include, Wildcards (`*`, `...`, `=`, `!`, `<`, `>`), Found Sets | Query AST Parser in Go translating criteria to SQL `WHERE` clauses; snapshot links (JSON payload) | Go Backend + Flutter |
| **Layout & Visual Forms** | Drag-and-drop Layout Designer, Parts (Header, Body, Subsummary, Footer, Nav), Inspector, Layout Themes | JSON Layout Engine (`docs/specs/layout_schema.json`), `InteractiveViewer` Canvas, Property Inspector Panel | Flutter Client |
| **Form Controls & Objects** | Fields (Edit Box, Dropdown, Pop-up, Checkbox, Radio, Calendar), Portals (1:N), Popovers, Tabs/Slides, Charts | Native Flutter component library dynamically rendered from JSON layout nodes | Flutter Client |
| **Calculation Engine** | Calculation Fields, Formulas, Built-in Functions (Text, Math, Date, Logical, Aggregates) | Embedded Go Expression Engine (`expr` / custom recursive AST evaluator), Stored & Unstored virtual fields | Go Backend |
| **Automation (Scripts)** | Script Workspace, Script Steps, Triggers (OnRecordLoad, OnFieldExit, etc.), Debugger, Data Viewer | Block-based action sequence runner in Go/Flutter, variable stack (`$local`, `$$global`), step-by-step debugger | Go + Flutter |
| **Security & Access** | Accounts, Privilege Sets, Extended Privileges, Record-Level Access, Field Restrictions | RBAC engine (`sys_accounts`, `sys_privilege_sets`), JWT/Session auth, SQL row-level security predicates | Go Backend |
| **Import / Export** | CSV, XLSX, JSON, XML, Merge, Matching Records Update | File import/export pipeline (`internal/importer`, `internal/exporter`) with column-mapping UI | Go Backend + Flutter |

---

## 2. Core Functional Modules

### 2.1 The Four Operational Modes
File4Base enforces the exact 4-mode operational paradigm:

1. **Browse Mode (`Ctrl+B` / `Cmd+B`)**:
   - Primary data interaction mode (Form, List, and Table views).
   - Real-time record navigation slider, record counter, found-set indicator.
   - Field data entry with instant auto-commit or deferred layout commit (`Enter`).
   - Portal interaction: adding/editing rows directly in 1:N related grids.

2. **Find Mode (`Ctrl+F` / `Cmd+F`)**:
   - Layout turns into an empty template with searchable badges on compatible fields.
   - Operators palette: `=`, `!`, `<`, `≤`, `>`, `≥`, `...` (range), `*` (wildcard), `""` (exact match).
   - Multi-request support (disjunctive `OR` requests via multiple find request cards).
   - "Omit" toggle for negation (`NOT`).
   - Compiles AST query sent to `POST /api/v1/data/{table}/find`.

3. **Layout Mode (`Ctrl+L` / `Cmd+L`)**:
   - WYSIWYG canvas with rulers, pixel grid, alignment guides, and dynamic snapping.
   - Layout Parts manager: Top Navigation, Title Header, Header, Body, Subsummary (with break field), Trailing Grand Summary, Footer, Bottom Navigation.
   - Field Picker dialog for dragging fields onto the canvas.
   - Floating/Dockable Inspector: Position, Styles (fill, stroke, corner radius, padding, shadows), Typography, and Data Binding (control style, value lists, behavior).

4. **Preview Mode (`Ctrl+U` / `Cmd+U`)**:
   - Printable page simulation with exact margins, pagination breaks, and multi-column printing flow.
   - Evaluates and renders subsummary aggregations (subtotals grouped by break field).
   - Export to PDF / Print dialog.

---

## 3. Relational & Calculation Engine Specification

### 3.1 Relationships Graph
- Visual node graph where each box represents a **Table Occurrence (TO)** (allowing multiple instances/aliases of the same underlying base table to prevent circular reference cycles).
- Connectors between match fields support:
  - Equality (`=`)
  - Comparative (`<`, `<=`, `>`, `>=`)
  - Non-equality (`!=`)
  - Cartesian product (`x`)
- Relationship options:
  - `allow_create_related`: Auto-creates child record on portal row entry.
  - `cascade_delete`: Auto-deletes child records when master record is deleted.
  - `sort_related`: Automatic sorting of portal records.

### 3.2 Calculation Fields & Virtual Formulas
- Formulas can reference:
  - Current record fields (`FieldName`)
  - Related table fields (`OccurrenceName::FieldName`)
  - System functions (`Get(CurrentDate)`, `Get(RecordID)`, `Get(FoundCount)`)
  - Global variables (`$$GLOBAL_VAR`) and local script variables (`$local_var`)
- Storage types:
  - **Stored**: Evaluated on record insert/update and cached in an indexed physical column.
  - **Unstored (Virtual)**: Evaluated dynamically during query execution (read-only).

---

## 4. Layout Engine & JSON Schema Architecture

Every layout is defined by a deterministic JSON document stored in `sys_layouts` matching [`docs/specs/layout_schema.json`](docs/specs/layout_schema.json):
- Root properties: `id`, `name`, `table_occurrence_id`, `width`, `theme`, `default_view` (`form`, `list`, `table`).
- `parts`: Array of layout parts with `type`, `height`, and `break_field_id` (for subsummaries).
- `objects`: Tree of components positioned with `x`, `y`, `width`, `height`, `anchors` (top, bottom, left, right for responsive resizing), `type` (field, label, button, button_bar, portal, tab_panel, slide_control, popover, chart, web_viewer), and styling attributes.

---

## 5. Automation, Script Workspace & Developer Utilities

### 5.1 Script Workspace & Execution Engine
- **Block-Based Programming**: Structured sequence of script steps arranged in order of execution.
- **Control Flow**: `If`, `Else If`, `Else`, `End If`, `Loop`, `Exit Loop If`, `End Loop`.
- **Navigation & Mode Control**: `Go to Layout`, `Enter Browse Mode`, `Enter Find Mode`, `Enter Preview Mode`, `Go to Record/Request/Page`.
- **Data Manipulation**: `Set Field`, `New Record/Request`, `Delete Record/Request`, `Commit Records/Requests`, `Revert Record/Request`.
- **Variables & Scope**:
  - Local variables: prefixed with `$` (scoped strictly to current script run).
  - Global variables: prefixed with `$$` (persist across sessions in client memory).
- **Script Triggers**: Event-driven hooks attaching scripts to layout and data lifecycle events:
  - `OnRecordLoad`, `OnRecordCommit`, `OnRecordRevert`
  - `OnFieldEnter`, `OnFieldKeystroke`, `OnFieldExit`, `OnFieldValidate`
  - `OnLayoutEnter`, `OnLayoutExit`, `OnModeEnter`, `OnModeExit`

### 5.2 Developer Utilities & Debugging
- **Script Debugger**: Step-through debugging (`Step Over`, `Step Into`, `Step Out`, `Pause on Error`, breakpoints).
- **Data Viewer**:
  - `Current Tab`: Inspects all active `$local` and `$$global` variables in execution scope.
  - `Watch Tab`: Evaluates live watch formulas and expressions.
- **Database Design Report (DDR)**: Generates complete schema audit and structure dump in XML / HTML / JSON.

---

## 6. Security, User Access & Multi-User Sharing

### 6.1 Accounts and Privilege Sets
- **Predefined Accounts**: `Admin` (Full Access) and `Guest` (Read-Only).
- **Privilege Sets**:
  - Granular permissions: Record CRUD (all, none, custom predicate per table), Layout design access, Script execution and modification.
  - Field-level security: View-only or restricted column access.
- **Extended Privileges**:
  - `fmrest`: Access via REST Data API.
  - `fmws`: Access via Real-Time WebSockets.
  - `fmxdbc`: External SQL ODBC/JDBC gateway access.

### 6.2 Multi-User Concurrency & Record Locking
- Row-level pessimistic locking when editing in Browse mode (prevents two users from simultaneously altering the same record).
- Real-time updates pushed to all active clients via WebSockets when rows or layouts commit.
