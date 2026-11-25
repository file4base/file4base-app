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

The step calls the server, which runs the transfer. Execution from a script is therefore **client-triggered and server-performed**, and it is available wherever a script runs today: the desktop client, the Web Client and the Script Debugger.

A transfer can also run **unattended, on a schedule** (`#108`): the File4Base server runs a **transfer schedule** at the times its cron expression names, as an account an administrator of each solution designated. That runs the transfer definition only. **Scripts still run only in a client** — there is no server-side script runner — so a schedule cannot run a script, and the steps around an `Import Records` in a script have no scheduled counterpart. See [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 19.

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

---

## 7. `Insert Current Date`, `Select All`, `Refresh Portal`, `Set Error Logging`, `Open File`

A FileMaker migration converts these five steps by instruction id (`13`, `50`, `180`, `200`, `33`) or by English name, from both the XML DDR and Save a Copy as XML, as runnable steps.

| Step | Parameters | What runs |
|---|---|---|
| `Insert Current Date` | `field` and `field_table` (or `target` / `name` for a `$variable`), `select` | Today's date, as `YYYY-MM-DD`, into the target, or the active field when the step names none. A date field holds one date, so the date replaces it; a timestamp field gets it at `00:00:00`. Elsewhere *Select entire contents* (`select`) replaces the content; without it the date replaces the field's selection (after `Select All`, say) or goes in at its caret when it is the active field, and is appended otherwise, and to a variable. In Find mode the date goes into the current find request, never into the record. A field of another table occurrence than the layout's (`field_table`) is not written yet: the step fails with `102` and writes nothing, rather than writing the layout table's field of that name. A target the migration could not read (`target_unresolved`) also fails with `102`, never falling back to the active field. A calculation, summary or container field fails with `201`; no target and no active field, or a field the table does not have, with `102`. |
| `Select All` | — | Selects the content of the active field. With no window on screen, or no active field, it does nothing. |
| `Refresh Portal` | `object_name`, `object_name_is_calculation` | Reloads the related records of the portal of that name, or of the active object, or of every portal when neither names one. A named object that is not a portal reads the record in hand again from the server, as FileMaker's Refresh Object does; the found set, its order and the current record stay as they are. An object the layout does not have fails with `116`. With no window on screen it does nothing. `Refresh Window` likewise reads the record in hand again and reloads the portals, without finding again. |
| `Set Error Logging` | `state`, `custom_debug_info` | `On` logs every nonzero error a later step reports, once, by the step that raised it: time, script, step and line, code, and the result of `custom_debug_info` evaluated at that moment (`?` when it cannot be evaluated). It stays on for the solution session, across every later script run (a button script after the startup script that turned it on, say), until `Off`. A step whose state was not read is `Off`. |
| `Open File` | `file_reference`, `hidden` | See below. |

**Set Error Logging — where the log goes.** FileMaker appends to a `ScriptErrors.log` file in the user's Documents folder, or to the server's log when the administrator enabled script logging. The File4Base client keeps the entries in memory with the solution session instead (`LayoutActionRunner.errorLogging`: the state and the last 1,000 entries, each with `toLogLine()` in FileMaker's tab-separated column order, without the session number, file name and account columns). The state and the entries go with the session at sign-out, as FileMaker's go when the file closes. Writing them to a file or to the server log is not implemented yet, and the step does not require full access as FileMaker's does.

**Open File.** The step takes a file reference, like `Perform Script` (§ 6), and fails with the same distinctions: an unknown reference (`114`), an unmapped one or a solution not installed on this server (`100`), a solution this session is not signed in to (`9`). A reference to this solution succeeds at once. Otherwise the step succeeds when the solution is installed and the user holds a session there, with their own account, whose privileges then govern every later step that reaches it. It opens nothing on screen: FileMaker, too, runs the steps after `Open File` in the file that holds the script. *Open hidden* (`hidden`) therefore changes nothing, and the step does not sign in for the user.

**Open File — known gaps.** No window that runs scripts today — a layout button, a script trigger, a custom menu item, the debugger — hands the runner a way to reach the user's session in another solution (the peer session `Perform Script` across file references uses too). From those entry points an `Open File` that names another solution therefore fails with `9`, even when the user is signed in there; only a reference to this solution succeeds. Where a peer session is available, the step opens no window and does not run the opened solution's `OnFirstWindowOpen` (opening) script, which FileMaker runs even for a file opened hidden.

## 8. `Save Records as PDF`, `Send Mail`, and the script folders (`#133`)

### Where a script reads and writes files

A FileMaker script names files by path. A File4Base script reaches **two folders of its own**, never the rest of the user's disk (`ScriptFileStore`, `client/lib/features/layout_engine/script_files.dart`):

| Folder | `Get (…)` | Desktop client | Browser |
|---|---|---|---|
| Temporary | `Get ( TemporaryPath )` | A folder made for this run of File4Base in the system's temporary folder (`file4base-…`). | In memory, `/File4Base/Temporary/`. |
| Documents | `Get ( DocumentsPath )`, `Get ( DesktopPath )` | `Documents/File4Base` in the user's home folder (`%USERPROFILE%\Documents\File4Base` on Windows), created on first write. | In memory, `/File4Base/Documents/`. |

- Both answers are FileMaker paths ending with a slash: `/C:/Users/…/` on Windows, `/home/…/` elsewhere. A script never writes on the real desktop; its desktop is the documents folder.
- A step's path is read as FileMaker writes it, optionally after `file:`, `filemac:`, `filewin:` or `filelinux:`. A `$variable` line stands for the variable's value. A **relative** path (`file:Report.pdf`) is in the documents folder, as FileMaker's "folder of the database file" has no counterpart. A **path list** (one path per line or `¶`) uses its first path that lies in one of the two folders.
- Any other place, and any `..` that would leave a folder, is refused: `Save Records as PDF` reports **800** (cannot create the file), `Send Mail` **100** (file missing). A script therefore never writes over, or sends, a file of the user's it was not given. Comparison is case-insensitive under a Windows drive.
- The browser's folders live in memory for the tab: a PDF saved there is opened as a download, and can be attached by a later Send Mail of the same session.

### `Save Records as PDF`

| Parameter | What runs |
|---|---|
| `path` / `paths` | Resolved as above; empty: `<layout name>.pdf` in the documents folder. Invalid: **800**. |
| `records` | `current` (the record in hand), `browsed` (every record of the found set, the default) or `blank`. No record: **401**. |
| `append` | Adds the pages to the PDF at that path **saved by a script earlier in this run of File4Base**; a PDF that is there but was not saved by this run cannot be appended to (**3**) and is left as it was. No file yet: a new one. |
| `auto_open` | Opens the PDF: in its application on the desktop, as a download in the browser. |
| `create_email` | Runs `Send Mail` with its dialog and the PDF attached (below). |
| `save_to: variable` | Not supported: **3**, with its migration note. |
| `dialog` | The *Save Records as PDF* dialog is not shown; the step saves as written. |

The pages are those `Print` draws (`LayoutPrintService.slicePages`): the layout of the window drawn off screen for those records with the window's page setup, one image per sheet, assembled as a PDF. A window that cannot draw (no data browser) answers **3**.

### `Send Mail`

| Step as written | File4Base |
|---|---|
| Via e-mail client, **with dialog**, no attachment | A draft in the user's e-mail client (`mailto:`), as FileMaker. |
| Via e-mail client, with dialog, **with attachments** | With an SMTP profile: File4Base's compose window (To, CC, BCC, Subject, Message, the attachments), then sent through the profile; Cancel sets `Get ( LastError )` to **1**. Without one: the draft, and the user is told which files to attach (a `mailto:` draft cannot carry a file). |
| Via e-mail client, **without dialog** | **Documented deviation**: FileMaker sends through the e-mail client without asking, which a browser and most desktops cannot do. With an SMTP profile the message is sent through it; without one, a draft as before. |
| **Via SMTP server** | Sent through the solution's SMTP profile (or the server's). The step's own server, port and password are **not** used and never migrated: only its sender name, sender address and reply-to are (`smtp_from_name`, `smtp_from`, `smtp_reply_to`). No profile: **1506**. |
| **One e-mail per record** | Each record of the found set is made current in turn and its message evaluated; records without a recipient are skipped; the messages go through SMTP in batches of 100. No profile: **1506**. |
| *Collect addresses across found set* (`collect_to` / `collect_cc` / `collect_bcc`) | The addresses of every record are joined, once each, into one message. |

- A calculation that does not evaluate stops the script; its text is never sent.
- An attachment that does not resolve, or a file that is not there, is **100**. A server refusal answers the FileMaker error the server reports (`1501`–`1506`, see the API reference, *Send Mail through SMTP*); nothing is drafted instead.
- Profiles, limits and header-injection refusal are the server's: see [Server Tools](server_tools_and_mfa.md) and `docs/api/API_REFERENCE.md`.

### Known gaps

- `Export Field Contents`, `Insert File` and `Insert Picture` still do not read or write the script folders.
- A path calculation that reads a related field is not evaluated on the client.
- Append works within one run of File4Base only; a PDF from an earlier run is not appended to.
