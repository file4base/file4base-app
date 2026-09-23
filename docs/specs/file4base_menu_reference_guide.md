# File4Base Menu Bar & Functional Command Reference

This document details the canonical structure of menus, submenus, and commands for **File4Base** across its four operational modes (`Browse`, `Find`, `Layout`, `Preview`), modeled after File4base Pro. It serves as an implementation and compatibility reference for File4Base.

---

## Canonical Menu Schema (YAML)

```yaml
file_4_base_menu_bar:
  - menu: File
    items:
      - New Database...
      - Open...
      - Open Remote...
      - Open Favorite
      - Open Recent
      - Close
      - Manage:
          - Database...
          - Security...
          - Value Lists...
          - Custom Functions...
          - External Data Sources...
          - Layouts...
          - Scripts...
          - Containers...
          - Themes...
      - Sharing:
          - Share with FileMaker Clients...
          - Enable FileMaker WebDirect...
          - Share with ODBC/JDBC...
      - File Options...
      - Change Password...
      - Page Setup...
      - Print...
      - Import Records:
          - File...
          - Folder...
          - XML Data Source...
          - ODBC Data Source...
      - Export Records...
      - Save/Send Records As:
          - Excel...
          - PDF...
          - Snapshot Link...
      - Send Mail...
      - Save a Copy As...
      - Recover...

  - menu: Edit
    items:
      - Undo
      - Redo
      - Cut
      - Copy
      - Paste
      - Clear
      - Select All
      - Find/Replace:
          - Find/Replace...
          - Find Next
          - Find Previous
      - Spelling:
          - Check Selection...
          - Check Record...
          - Correct Word...
          - Dictionaries...
          - Edit User Dictionary...
          - Options...
      - Preferences...

  - menu: View
    items:
      - Browse Mode
      - Find Mode
      - Layout Mode
      - Preview Mode
      - Page Margins
      - Status Toolbar
      - Formatting Bar
      - Text Ruler
      - Zoom In
      - Zoom Out

  - menu: Insert
    items:
      - Picture...
      - Audio/Video...
      - PDF...
      - QuickTime...
      - File...
      - Current Date
      - Current Time
      - Current User Name
      - From Index...
      - From Last Visited Record
      - Merge Field...
      - Merge Variable...

  - menu: Format
    items:
      - Font
      - Size
      - Style
      - Align Text
      - Line Spacing
      - Text Color
      - Align Object (en Layout Mode)
      - Distribute (en Layout Mode)
      - Resize (en Layout Mode)

  - menu: Records
    items:
      - New Record
      - Duplicate Record
      - Delete Record...
      - Delete All Records...
      - Go to Record:
          - First
          - Previous
          - Next
          - Last
          - By Number...
      - Show All Records
      - Show Omitted Only
      - Omit Record
      - Omit Multiple...
      - Sort Records...
      - Unsort
      - Replace Field Contents...
      - Relookup Field Contents
      - Revert Record

  - menu: Scripts
    items:
      - Script Workspace...
      - "[Custom User Scripts List]"

  - menu: Tools
    items:
      - Script Debugger
      - Data Viewer
      - Custom Menus:
          - Manage Custom Menus...
      - Database Design Report...
      - Developer Utilities...

  - menu: Window
    items:
      - Minimize
      - Zoom
      - Tile Horizontally
      - Tile Vertically
      - Cascade
      - New Window
      - Show Window
      - "[Active Document Windows List]"

  - menu: Help
    items:
      - File4base  Help
      - Resource Center
      - Product Documentation
      - File4base Community
      - Service & Support
      - Check for Updates...
      - About File4base
```

---

## 1. Execution Modes

File4Base dynamically adjusts the contents of the menu bar based on the active mode:
1. **Browse Mode (`Ctrl+B` / `Cmd+B`)**: Record viewing, editing, and data entry mode.
2. **Find Mode (`Ctrl+F` / `Cmd+F`)**: Search and filter mode based on find request criteria (*Find Requests*).
3. **Layout Mode (`Ctrl+L` / `Cmd+L`)**: Visual designer mode for forms, lists, and views.
4. **Preview Mode (`Ctrl+U` / `Cmd+U`)**: Print and pagination preview mode.

---

## 2. "File" Menu

Available across all modes; manages persistence, server connectivity, import/export, and schema administration.

* **New Solution / Database...** (`Ctrl+N` / `Cmd+N`)
* **Open...** (`Ctrl+O` / `Cmd+O`)
* **Open Favorites**
  * *[List of favorite solutions and hosts]*
* **Open Recent**
  * *[Recent solutions history]*
* **Open Remote / Hosts...** (`Ctrl+Shift+O` / `Cmd+Shift+O`)
  * Server discovery dialog (LDAP / mDNS / direct IP).
* ---
* **Close** (`Ctrl+W` / `Cmd+W`)
* **File Options...** (Startup behavior, default credentials, file triggers).
* ---
* **Manage** (Centralized administration)
  * **Database...** (`Ctrl+Shift+D` / `Cmd+Shift+D`) -> Opens Tables, Fields, and Relationship Graph manager.
  * **Security...** (Accounts, Privilege Sets, Access Roles).
  * **Value Lists...** (Static and dynamic value lists via relationships).
  * **Layouts...** (View and layout management).
  * **Scripts...** (Script Workspace).
  * **Custom Functions...** (Calculations and reusable recursive functions).
  * **Custom Menus...** (Application menu bar overrides).
  * **External Data Sources...** (ODBC/SQL connections and external origins).
  * **Containers...** (Secure and open blob storage configuration).
* ---
* **Sharing**
  * **Share with File4Base Clients...** (Local network sharing configuration).
  * **Configure File4Base WebDirect...**
  * **Enable ODBC/JDBC...**
* ---
* **Save a Copy As...** (Full copy, clone with no records, compacted copy).
* **Recover...** (Index repair and file integrity check).
* ---
* **Import Records**
  * **File...** (CSV, XLSX, XML, JSON).
  * **Folder...** (Batch import of images or text files into records).
  * **ODBC Data Source...**
* **Export Records...**
* ---
* **Page Setup...** (`Ctrl+Shift+P` / `Cmd+Shift+P`)
* **Print...** (`Ctrl+P` / `Cmd+P`)
* ---
* **Exit / Quit** (`Ctrl+Q` / `Cmd+Q`)

---

## 3. "Edit" Menu

* **Undo** (`Ctrl+Z` / `Cmd+Z`)
* **Redo** (`Ctrl+Y` / `Cmd+Shift+Z`)
* ---
* **Cut** (`Ctrl+X` / `Cmd+X`)
* **Copy** (`Ctrl+C` / `Cmd+C`)
* **Paste** (`Ctrl+V` / `Cmd+V`)
* **Clear** (Delete current selection)
* **Select All** (`Ctrl+A` / `Cmd+A`)
* ---
* **Find/Replace...** (Find and replace across active fields or all record fields).
* **Spelling** (Spell checker).
* ---
* **Preferences...** (General user and editor preferences).

---

## 4. "View" Menu

Defines the active perspective and visibility of auxiliary controls.

* **Browse Mode** (`Ctrl+B` / `Cmd+B`)
* **Find Mode** (`Ctrl+F` / `Cmd+F`)
* **Layout Mode** (`Ctrl+L` / `Cmd+L`)
* **Preview Mode** (`Ctrl+U` / `Cmd+U`)
* ---
* **View as Form** (Single record / detail view).
* **View as List** (Continuous vertical list view).
* **View as Table** (Spreadsheet-style matrix grid).
* ---
* **Status Toolbar** (Toggle top record navigation toolbar).
* **Formatting Bar** (Typography, fonts, and alignment toolbar).
* **Zoom In** (`Ctrl++` / `Cmd++`)
* **Zoom Out** (`Ctrl+-` / `Cmd+-`)
* **Actual Size (100%)**

---

## 5. "Records" Menu *(Active in Browse Mode)*

* **New Record** (`Ctrl+N` / `Cmd+N`)
* **Duplicate Record** (`Ctrl+D` / `Cmd+D`)
* **Delete Record...** (`Ctrl+E` / `Cmd+E`)
* **Delete All Records...** / **Delete Found Records...**
* ---
* **Go to Record**
  * **First**
  * **Previous**
  * **Next**
  * **Last**
  * **By Number...**
* ---
* **Show All Records** (`Ctrl+J` / `Cmd+J`)
* **Show Omitted Only** (Invert currently found set).
* **Omit Record** (`Ctrl+M` / `Cmd+M`) (Temporarily exclude from view without deleting).
* **Omit Multiple...**
* ---
* **Sort Records...** (`Ctrl+S` / `Cmd+S`) (Multi-criteria sorting by local or related fields).
* **Unsort**
* ---
* **Replace Field Contents...** (`Ctrl+=` / `Cmd+=`) (Batch update across all found records).
* **Relookup Field Contents** (Force recalculation of lookups).
* **Revert Record** (Discard unsaved modifications before row commit).
* **Save/Commit Record** (`Enter`)

---

## 6. "Requests" Menu *(Active in Find Mode)*

Replaces the *Records* menu when entering Find Mode.

* **Perform Find** (`Enter`) -> Executes the corresponding SQL query / filter.
* **New Request** (`Ctrl+N` / `Cmd+N`) (Allows disjunctive `OR` queries).
* **Duplicate Request** (`Ctrl+D` / `Cmd+D`)
* **Delete Request** (`Ctrl+E` / `Cmd+E`)
* **Delete All Requests**
* ---
* **Include / Omit** (Toggles whether the request matches criteria or excludes matches like `NOT`).
* ---
* **Go to Request** (First, Previous, Next, Last).
* **Show All Records** (`Ctrl+J` / `Cmd+J`) (Cancels the find operation and restores the entire dataset).

---

## 7. "Layouts" & "Arrange" Menus *(Active in Layout Mode)*

When entering UI designer mode, the menus change significantly to provide visual layout composition tools:

### "Layouts" Menu
* **New Layout/Report...** (`Ctrl+N` / `Cmd+N`) (Layout wizard: Form, List/Report, Mobile device).
* **Duplicate Layout**
* **Delete Layout**
* ---
* **Layout Setup...** (Configure underlying table, page dimensions, and triggers).
* **Save Layout** (`Ctrl+S` / `Cmd+S`)
* **Revert Layout**

### "Insert" Menu (Placing components on canvas)
* **Field...** (Place control bound to a schema column).
* **Button / Button Bar...**
* **Portal...** (Sub-table / grid for 1:N related records).
* **Tab Control / Slide Control** (Tabs and sliding containers).
* **Web Viewer...** (Embedded browser container for micro-frontends or interactive charts).
* **Chart...** (Integrated analytics charts).
* **Part...** (Structural zones: Top Navigation, Header, Body, Sub-summary, Footer, Bottom Navigation).

### "Arrange" Menu (Canvas alignment and z-order)
* **Group** (`Ctrl+G` / `Cmd+G`) / **Ungroup** (`Ctrl+Shift+G` / `Cmd+Shift+G`)
* **Lock** / **Unlock**
* **Bring to Front** / **Send to Back** / **Bring Forward** / **Send Backward**
* **Align** (Left, Right, Top, Bottom, Centers).
* **Distribute** (Horizontal, Vertical).
* **Size** (Match width, height, or both).

---

## 8. "Scripts" Menu

* **Script Workspace...** (`Ctrl+Shift+S` / `Cmd+Shift+S`) (Block-based script programming environment).
* **Enable Script Debugger** (Developer tool: step-by-step script execution).
* **Data Viewer** (Inspector for global variables `$`, local variables `$$`, and watch expressions).
* ---
* *[Dynamic list of scripts marked as "Include in menu"]* (Automatic shortcuts `Ctrl+1` .. `Ctrl+8` / `Cmd+1` .. `Cmd+8`).

---

## 9. "Window" Menu

* **New Window** (Open independent parallel view with its own context state).
* **Show/Hide Status Toolbar**
* **Tile / Cascade Windows**
* *[List of active system windows]*

---

## 10. "Help" Menu

* **File4Base Help**
* **Product Documentation & API Specs**
* **About File4Base**

---

## Priority Mapping for File4Base (Implementation Roadmap)

| Menu / Feature | File4Base Phase | Backend Equivalence (Go + PostgreSQL) | Frontend Equivalence (Flutter) |
|---|---|---|---|
| **Manage > Database** | **Phase 2** | `sys_tables`, `sys_columns`, DDL (`CREATE/ALTER TABLE`) | Visual Schema Graph & Table Inspector |
| **View > Modes** | **Phase 3** | Layout metadata endpoints | Mode State Machine (`Browse`, `Layout`, `Find`, `Preview`) |
| **Records (CRUD)** | **Phase 4** | Generic row endpoints (`/api/v1/data/{table}`) | Form & Grid data binding with validation |
| **Requests (Find Mode)** | **Phase 4** | Query Builder translated to SQL `WHERE` clauses | Wildcard parser (`*`, `...`, `=`, `!`) |
| **Layouts > Insert Parts** | **Phase 5** | JSON schema specifications with positioning | Drag-and-drop canvas with `InteractiveViewer` |
| **Scripts > Workspace** | **Phase 6** | Embedded rules engine / Lua / JS in Go | Action-block IDE with debugger |