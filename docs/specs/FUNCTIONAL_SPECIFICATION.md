# File4Base Functional Specification: Core Feature Mapping

This specification maps functional database, layout, calculation, and scripting workflows—informed by functional observation of Claris *FileMaker Pro 14 User's Guide* and *FileMaker Pro 12–19 Help* (see [`docs/PROVENANCE.md`](../PROVENANCE.md), [`docs/LEGAL.md`](../LEGAL.md), and [`CAPABILITY_PARITY_MATRIX.md`](CAPABILITY_PARITY_MATRIX.md))—into technical requirements for **File4Base**, built with **Go** (Backend DBAL, Control Store & Metadata Engine) and **Flutter** (Desktop & Web Client Layout & Script Engine).

---

## 1. Architectural Matrix: Functional Reference to File4Base Stack

| Domain | Reference Feature (FileMaker Pro 14) | File4Base Technical Implementation | Target Layer |
|---|---|---|---|
| **Data Storage & Schema** | Tables, Fields, Types, Auto-Enter, Validation, Summary fields | Dynamic Metadata (`sys_tables`, `sys_columns`, `sys_table_occurrences`, `sys_value_lists`), DBAL (`internal/dbal`), dynamic SQL migrations | Go + PostgreSQL / MariaDB |
| **Relational Model** | Relationships Graph, Match Fields, Comparative operators, Cascading delete, Related creation, Lookups | Relationship Catalog (`sys_relationships`), DBAL JOIN builder, Lookup Triggers, Flutter GraphView UI (`custom_painter`) | Go + Flutter |
| **Operational Modes** | 4 Modes: Browse (`Cmd+B`), Find (`Cmd+F`), Layout (`Cmd+L`), Preview (`Cmd+U`) | Mode State Machine (Riverpod), dynamic toolbar rendering, canvas mode switching, custom menu sets (`sys_menu_sets`) | Flutter Client |
| **Data Views** | Form View, List View, Table View | Dynamic Grid/Table Widget, Paginated/Virtual ListView, Form Canvas with data binding | Flutter Client |
| **Find & Search Engine** | Quick Find, Find Requests, Omit / Include, Wildcards (`*`, `...`, `=`, `!`, `<`, `>`), Found Sets, Saved Finds | Query AST Parser in Go (`internal/data`) translating criteria (including related fields) to SQL `WHERE` clauses; `sys_saved_finds` | Go Backend + Flutter |
| **Layout & Visual Forms** | Drag-and-drop Layout Designer, Parts (Header, Body, Subsummary, Footer, Nav), Inspector, Layout Themes | JSON Layout Engine (`docs/specs/layout_schema.json`), `InteractiveViewer` Canvas, Horizontal Part Rail, Property Inspector Panel | Flutter Client |
| **Form Controls & Objects** | Fields (Edit Box, Dropdown, Pop-up, Checkbox, Radio, Calendar), Portals (1:N), Popovers, Tabs/Slides, Charts | Native Flutter component library dynamically rendered from JSON layout nodes | Flutter Client |
| **Calculation Engine** | Calculation Fields, Formulas, 20 Built-in Functions (Text, Math, Date, Logical, Row Aggregates) | Deterministic Go Expression Evaluator (`internal/calc`), Stored calculation columns & `internal/schema/summaries.go` | Go Backend |
| **Automation (Scripts)** | Script Workspace, Script Steps, Triggers (`OnRecordLoad`, `OnLayoutEnter`, etc.), Debugger, Data Viewer | Ordered step catalog (`sys_scripts`, `sys_script_steps`), client `LayoutActionRunner`, step-by-step `ScriptDebugSession`, live Data Viewer | Go + Flutter |
| **Security & Access** | Accounts, Roles & Privileges, Extended Privileges, Granular Table & Layout Access, Encrypted Web Channel | Per-session auth (`internal/auth`), SQLite API control store (`internal/control`), `sys_users` / `sys_role_privileges`, AES-256-GCM payload cipher (`internal/crypto`) | Go Backend + SQLite Control Store |
| **Import / Export & Migration** | CSV, TSV, XLSX, `FMPXMLRESULT` XML, External SQL Sources, Solution Bundles, Assisted FileMaker Migration | Tabular pipeline (`internal/dataio`), External SQL (`internal/extsource`), Assisted DDR XML & `FMPXMLRESULT` converter (`internal/migration`) | Go Backend + Flutter |

---

## 2. Core Functional Modules

### 2.1 The Four Operational Modes
File4Base enforces the exact 4-mode operational paradigm:

1. **Browse Mode (`Ctrl+B` / `Cmd+B`)**:
   - Primary data interaction mode (Form, List, and Table views).
   - Real-time record navigation slider, record counter, found-set indicator.
   - Record bar above the layout: first / previous / next / last, an editable "Record N of M" box, a slider (the record changes on release), the found set summary ("48 found of 230 · Sorted by City ↑") and the save status. `Cmd/Ctrl+↑/↓` moves to the previous / next record.
   - Record actions: New, Duplicate, Delete (with confirmation), Find, Sort (by one field, ascending or descending, or unsorted) and Show All; the Records menu offers the same.
   - Views: Form (the layout), List (one row per record) and Table (spreadsheet grid; click a header to sort, double click a row to open it in Form view).
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
   - Drawing tools: Line, Rectangle, Rounded Rectangle and Oval are drawn by dragging on the canvas (or placed with a click). Lines are horizontal or vertical, following the longer side of the drag. Line color and width (status sidebar stroke control or inspector) apply to lines and to the border of shapes.
   - Text: labels, buttons and shapes hold text, edited in place with a double click (or Enter) or in the inspector. Insert > Current Date / Current Time / Current User Name / Page Number / Merge Field add merge symbols (`{{CurrentDate}}`, `{{field_name}}`, ...) resolved in Browse and Preview modes.
   - Insert > Picture / PDF / Audio-Video / File embeds the file (up to 2 MB, stored in the layout) inside the selected shape, or in a new media object.
   - Button Setup (double click a button, press Enter, right click > Button Setup..., or the inspector Data tab): the button label and its click action. Under each button the canvas shows the action it runs. "New script..." creates a script, assigns it and opens the Script Workspace to add its steps. A button runs a single step (New/Duplicate/Delete/Commit/Revert Record, Go to Record, Enter Find Mode, Perform Find, Show All Records, Enter Preview Mode, Go to Layout, Set Field, Show Custom Dialog, Open URL) or performs a stored script with an optional parameter.
   - Set Tab Order tool: click fields and buttons in the order the Tab key visits them in Browse and Find modes; "Auto" numbers them in reading order, "Clear" removes the order.

4. **Preview Mode (`Ctrl+U` / `Cmd+U`)**:
   - Printable page simulation with exact margins, pagination breaks, and multi-column printing flow.
   - Evaluates and renders subsummary aggregations (subtotals grouped by break field).
   - Export to PDF / Print dialog: "Print / Export PDF" prints or saves as PDF the previewed sheet only (this record, or all records with one sheet per record) at the Page Setup paper size; File > Print prints the previewed page.

---

## 3. Relational & Calculation Engine Specification

### 3.1 Relationships Graph
- Visual node graph where each box represents a **Table Occurrence (TO)** (allowing multiple instances/aliases of the same underlying base table to prevent circular reference cycles).
- Connectors between match fields support:
  - Equality (`=`)
  - Comparative (`<`, `≤`, `>`, `≥`)
  - Non-equality (`≠`)

  The graph stores the mathematical signs; the plain spellings (`<>`, `!=`,
  `<=`, `>=`) are accepted too. The Cartesian product (`x`) is not implemented.
- A relationship is drawn **left to right**, and that direction is read as
  parent to child: all three options act on the right-hand side.
  - `allow_creation`: a record may be created on the right-hand side through
    this relationship, which is what the empty row at the end of a portal does.
  - `cascade_delete`: deleting a left-hand record deletes the right-hand
    records that match it, and theirs, down the whole chain.
  - `sort_related`: the order the right-hand side's records come back in when
    the caller asks for none. A portal's own sort order supersedes it.

  See [relationships_and_portals.md](relationships_and_portals.md).

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
