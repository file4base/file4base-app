# UI Specification: Script Workspace (File4Base)

## 1. Overview
The Script Workspace is a visual script development canvas inspired by canonical visual low-code script workspaces.
- **Frontend Target**: Flutter Desktop (macOS, Linux, Windows) with keyboard shortcut support, panel resizing, and WebDirect support.
- **Theme & Palette**: Dark mode styling:
  - Main background: `#0b1120`
  - Panel surface: `#111827`
  - Accent / Highlights: Cyan `#38bdf8` and Emerald `#10b981`
  - Control / Conditionals: Amber `#f59e0b`
  - Navigation: Purple `#a855f7`
- **UI Architecture**: Three-column adjustable split layout:
  1. Left: Scripts Tree Explorer
  2. Center: Sequential Step Editor & Bottom Parameter Inspector
  3. Right: Action Step Catalog

Visual reference mockup: [script_workspace_mockup.png](../../assets/mockups/script_workspace_mockup.png)

---

## 2. Panel Structure

### A. Left Panel: Scripts Explorer (Tree View)
- Component: `TreeView` supporting nested collapsible folders and script files.
- Top quick-search filter bar (`TextField` with real-time substring filtering by script name or folder).
- Node presentation:
  - Item type icon: Folder icon for groupings, script file icon for scripts.
  - Script name and active status badge.
- Contextual menu (right-click / secondary click):
  - *New Script* (`Nuevo Script`)
  - *New Folder* (`Nueva Carpeta`)
  - *Duplicate Script* (`Duplicar`)
  - *Delete* (`Eliminar`)

### B. Center Panel: Sequential Step Editor & Parameter Inspector
- **Top Tab Bar**: Multi-tab management for concurrently opened scripts (with active indicator and close buttons).
- **Step List (`ReorderableListView`)**:
  - Reorderable list supporting drag-and-drop step rearrangement.
  - Two-digit formatted sequence index (`01`, `02`, `03`...).
  - Category-colored instruction labels:
    - *Navigation*: Violet / Purple (`#a855f7`)
    - *Fields & Variables*: Cyan / Teal (`#06b6d4`)
    - *Conditional Control*: Amber / Yellow (`#f59e0b`)
    - *Records & Commit*: Emerald / Green (`#10b981`)
    - *Integration & Dialogs*: Pink / Rose (`#f43f5e`)
  - Clear monospace parameter preview (e.g. `[Invoices::Total > 5000]`, `[$subtotal = Sum(Items.price)]`).
  - Active / Inactive step toggle (checkbox or visual switch to disable steps without deletion).
  - Vertical indentation guidelines connecting block pairs:
    - `If` / `Else` / `End If` (`Si / Sino / Fin Si`)
    - `Loop` / `Exit Loop If` / `End Loop` (`Bucle / Fin de Bucle`)
- **Bottom Zone: Contextual Parameter Inspector**:
  - Contextual configuration panel dynamically changing based on the focused step.
  - Input fields for variable names, target fields, and formula parameters.
  - `[ fx Specify... ]` (`[ fx Especificar... ]`) button opening the formula and calculation builder dialog.

### C. Right Panel: Step Catalog (Action Palette)
- Categorized accordion or grouped list:
  - **Navigation**: Go to Layout (`Ir a Presentación`), Go to Record/Request (`Ir a Registro`), Enter Find Mode (`Entrar en Modo Buscar`).
  - **Records**: New Record (`Nuevo Registro`), Commit Records (`Guardar Registros`), Delete Record (`Eliminar Registro`), Revert Record (`Revertir Registro`).
  - **Control & Logic**: If / Else / End If (`Si / Sino / Fin Si`), Loop / Exit Loop If / End Loop (`Bucle`), Set Variable (`Establecer Variable`), Perform Script (`Ejecutar Script`), Pause/Continue Script (`Pausar / Continuar Script`).
  - **Integration & Data**: Perform REST API cURL (`Ejecutar API REST cURL`), Insert from URL (`Insertar desde URL`), Send Webhook (`Enviar Notificación Webhook`).
- Action invocation: Double-click to insert at cursor or drag-and-drop into the central step list.

---

## 3. Script Data Model (JSON Schema)

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "File4BaseScript",
  "type": "object",
  "required": ["id", "name", "steps"],
  "properties": {
    "id": {
      "type": "string",
      "format": "uuid",
      "description": "Unique identifier for the script"
    },
    "name": {
      "type": "string",
      "description": "Name of the script"
    },
    "context_table": {
      "type": "string",
      "description": "Base table occurrence context for calculation resolution"
    },
    "folder_id": {
      "type": ["string", "null"],
      "description": "Parent folder ID if organized in a hierarchy"
    },
    "is_active": {
      "type": "boolean",
      "default": true
    },
    "steps": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["step_id", "type", "enabled"],
        "properties": {
          "step_id": { "type": "integer" },
          "type": { "type": "string" },
          "enabled": { "type": "boolean", "default": true },
          "params": {
            "type": "object",
            "additionalProperties": true
          },
          "children": {
            "type": "array",
            "items": { "$ref": "#/properties/steps/items" }
          }
        }
      }
    }
  }
}
```

### Example Script Payload
```json
{
  "id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "name": "on_invoice_created",
  "context_table": "invoices",
  "steps": [
    {
      "step_id": 1,
      "type": "go_to_layout",
      "params": { "layout_name": "Invoices_Detail" },
      "enabled": true
    },
    {
      "step_id": 2,
      "type": "set_variable",
      "params": { "variable": "$subtotal", "calc": "Sum(Items.price)" },
      "enabled": true
    },
    {
      "step_id": 3,
      "type": "if",
      "params": { "condition": "Invoices::Total > 5000" },
      "enabled": true,
      "children": [
        {
          "step_id": 4,
          "type": "set_field",
          "params": { "field": "Invoices::RequiresApproval", "value": "TRUE" },
          "enabled": true
        }
      ]
    }
  ]
}
```

---

## 4. Execution Engine Architecture
- **Server-Side Execution**: Go engine with Google CEL (Common Expression Language) for deterministic formula evaluation.
- **Client-Side Visual Runner**: Step-by-step debugger with breakpoint support, variable inspection stack (`$local`, `$$global`), and error handling (`Set Error Capture`).
