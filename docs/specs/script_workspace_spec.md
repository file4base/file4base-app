# UI Specification: Script Workspace (File4Base)

## 1. Overview
The Script Workspace is a visual script development canvas inspired by canonical visual low-code script workspaces.
- **Frontend Target**: Flutter Desktop (macOS, Linux, Windows) with keyboard shortcut support, panel resizing, and Web Client support.
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
  - **Integration & Data**: Perform REST API cURL (`Ejecutar API REST cURL`), Insert from URL (`Insertar desde URL`), Send Webhook (`Enviar Notificación Webhook`), Import Records (`Importar Registros`), Export Records (`Exportar Registros`).
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

Normal execution and the debugger advance **one** state machine, `ScriptExecution` (program counter, `If` frames, `Loop` frames, local variables, halt state), so stepping through a script has the same effects, in the same order, as running it (`#94`).

---

## 5. `Import Records` / `Export Records` (`#96`)

Both steps run a **transfer definition**: a named, persisted object recording the two solutions and tables, the explicit field mappings, the stable match keys, the add/update mode and the unmatched- and rejected-row policies. The step names the definition and nothing else; the definition is managed over `/api/v1/transfers` (see [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 17).

| Step parameter | Meaning |
|---|---|
| `transfer` (also read as `transfer_name` or `name`) | The definition's name, or a `$variable` / `{{merge}}` token holding it. |

`Import Records` requires a definition whose **destination** is this solution, `Export Records` one whose **source** is. A step pointed the other way is refused by name rather than run backwards.

### Where execution happens

The step calls the server, which runs the transfer. Execution is therefore **client-triggered and server-performed**, and it is available wherever a script runs today: the desktop client, the Web Client and the Script Debugger. **There is no scheduled or server-side script runner in this release**, so a transfer never runs unattended on File4Base's own initiative; any scheduler must call the endpoint itself with a session token for each end.

### What stops the script

A transfer moves records between two solutions, so the step is deliberately unforgiving. Each of these stops the script and says which it was:

- the step names **no transfer** — a step converted from FileMaker carries no target, by design;
- **no transfer of that name** exists in this solution. It is never answered with a *script* of the same name;
- the variable naming the transfer **holds nothing**;
- the step is pointed the **wrong way** for the definition it names;
- the transfer reaches **another solution** and this session is not signed in to it. The message names the solution. A definition carries no credential, so authority comes from a session of each end;
- the transfer ran and was **refused**: nothing was written, and the message carries the counts and the first rejected rows, each with its row number, the two fields and the value.

### What a script can read afterwards

A committed run leaves its counts in global variables, so the steps after it can test what happened with an ordinary `If`:

| Variable | Holds |
|---|---|
| `$$transferAdded` | Records created in the destination. |
| `$$transferUpdated` | Destination records a match key found and updated. |
| `$$transferSkipped` | Records passed over for want of a match. |
| `$$transferRejected` | Rows the destination could not hold. Non-zero only under `on_rejected: "skip"`, since `abort` stops the script. |
| `$$transferCommitted` | `1` when the destination transaction was committed. |

---

## 6. `Perform Script` Into Another Solution (`#107`)

`Perform Script` takes an optional `file_reference` parameter: the name of a **file reference**, a design object that maps a name to another solution (see [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 18). Without it, the step performs a script of this solution, as always.

| Step parameter | Meaning |
|---|---|
| `script_name` | The script to perform. |
| `file_reference` | Optional. The reference naming the solution the script belongs to. |

With a `file_reference`, the script is looked up **only** in the solution the reference is mapped to — never in this one, whatever shares its name. The step stops the script when the reference is unknown, unmapped, or mapped to a solution this session is not signed in to, and names which.

The called script runs on the session the user opened in that solution, and **without a window**: the window on screen belongs to the caller. Its first step that acts on a window (`New Record`, `Set Field`, `Go to Layout`, `Perform Find`, `Commit Records`, …) stops the script and names itself; a field reference is not answered from the record on screen. Everything else runs — transfers, variables, `If` / `Loop`, `Perform Script` within that solution, `Exit Script`, `Halt Script`, dialogs. `Halt Script` there stops every caller here. The Script Debugger runs the same state machine, so it does the same.
