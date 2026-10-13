# Cross-Solution Structure & Theme Copy/Paste (`#54`)

File4Base supports copying and pasting both **structural schema/design objects** (`#54`) and **visual Layout Themes** across different solutions using deterministic, human-reviewable JSON envelopes.

## 1. Structure Clipboard (`f4b.structure_clipboard` v1)

From **Manage Database** (`Tables` and `Fields` tabs), **Manage Layouts**, and **Script Workspace**, developers can copy individual design objects to the system clipboard and paste them into the same or another solution:

| Object Kind | Copy Source | Paste Destination | Remapped References |
|---|---|---|---|
| `table` | Manage Database > Tables | Manage Database > Tables | Creates table, all columns, default Table Occurrence, and layout. |
| `columns` | Manage Database > Fields | Manage Database > Fields (into selected `target_table_id`) | Preserves calculation formulas, auto-enter options, and validation rules. |
| `layout` | Manage Layouts | Manage Layouts | Remaps `table_occurrence_id`, `script_id`, and `value_list_id` by name. |
| `script` | Script Workspace | Script Workspace | Remaps step target `layout_id` and `script_id` references by name. |

### Pre-Paste Collision & Dependency Preview
Before modifying the destination database, the client calls `POST /api/v1/solutions/clipboard/preview` and opens `StructurePastePreviewDialog`:
- **Name Collisions:** Offers **Keep both and rename** (`on_collision: "rename"`, default) or **Update / replace existing** (`on_collision: "replace"`).
- **Missing Dependencies:** Lists any referenced table occurrences, fields (`Occurrence::Field`), scripts, layouts, or value lists that do not yet exist in the target solution so the developer knows what to copy first or rebind after pasting.

## 2. Layout Themes (`file4base_theme_v1`)

Accessible from **Tools > Layout Themes...** and **File > Manage > Themes...** (`ManageThemesDialog`), Layout Themes define the visual styling of a solution's layouts (`canvasBackgroundColor`, `partBackgroundColor`, `partHeaderTextColor`, `controlFillColor`, `controlBorderColor`, `controlBorderRadius`, `textColor`, `labelColor`, `primaryAccentColor`, `fontFamily`, `baseFontSize`).

- **Built-in Presets:** *Minimalist Light*, *Apex Blue*, *Dark Obsidian*, and *Warm Paper*.
- **Cross-Solution Theme Copy & Paste:**
  - **Copy Theme** serializes any selected theme into a `file4base_theme_v1` JSON payload on the system clipboard.
  - **Export (.json)** saves the theme as a `<theme_name>.theme.json` file in the configured destination folder.
  - **Paste / Import Theme...** imports a `file4base_theme_v1` JSON payload from another solution (or applies it immediately to every layout in the active solution via **Apply to All Layouts**), allowing corporate visual styles to be shared across solutions without restyling elements one by one.
