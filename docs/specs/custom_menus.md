# Custom Menus (`#52`)

Custom menu sets allow a solution to tailor the top application menu bar seen by end users: **Tools > Custom Menus** and **File > Manage > Custom Menus...** (`#52`).

## 1. Overview & Capabilities

A custom menu set (`sys_menu_sets`) defines overrides for the standard application menus (`File`, `Edit`, `View`, `Insert`, `Format`, `Records`, `Requests`, `Scripts`, `Tools`, `Window`). Each configured menu in a set uses one of three modes:

| Mode | Constant | Behavior |
|---|---|---|
| **Append to standard menu** | `extend` | Keeps the standard items of that menu and appends the custom items below a divider. (`File` only allows `extend` so `Sign Out` and `Open Solution` can never be removed.) |
| **Replace standard menu** | `replace` | Replaces the standard items of that menu with the custom item list. |
| **Hide entire menu** | `hide` | Completely hides the menu from the top menu bar when the custom menu set is active. |

### Standard Item Preset & Selective Removal
In **Manage Custom Menus** (`ManageCustomMenusDialog`), selecting **Replace standard menu** exposes a **Load Standard Items** preset for the target menu (for example, loading `New Record`, `Duplicate Record`, `Delete Record...`, `Show All Records`, `Sort Records...`, and `Replace Field Contents...` for the `Records` menu). Designers can then:
- Remove specific sensitive commands (such as `Delete Record...` or `Replace Field Contents...`) so end users cannot invoke them from the menu bar;
- Rename any item with a custom label;
- Assign a custom keyboard shortcut (`Cmd/Ctrl` + `[Shift]` + `[Alt]` + key);
- Rebind an item so it runs a custom script (`perform_script`) instead of its standard built-in command (`standard_command`).

## 2. Action Types

Each item in a custom menu specifies an `action`:
- `perform_script`: Executes a solution script by `script_id` (with `script_name` fallback).
- `go_to_layout`: Switches the active layout to `layout_id` (with `layout_name` fallback).
- `open_url`: Opens an `http://` or `https://` URL.
- `standard_command`: Invokes a built-in application command (`new_record`, `duplicate_record`, `delete_record`, `show_all_records`, `sort_records`, `enter_browse_mode`, `enter_find_mode`, `enter_layout_mode`, `enter_preview_mode`).
- `separator`: Renders a horizontal divider line inside the menu.

## 3. Resolution Order & Safety Guardrails

1. **Menu Bar Resolution Order:**
   - **Manual Session Override:** Choosing a set (or `[Standard File4Base Menus]`) under **Tools > Custom Menus** overrides the menu bar for the current session.
   - **Layout Default:** If no manual session override is set and the active layout defines `definition.menu_set_id`, that menu set is active while viewing the layout.
   - **Solution Default:** Otherwise, the menu set marked `is_default: true` in `sys_menu_sets` is used.
   - **Standard Fallback:** If none of the above apply, `[Standard File4Base Menus]` is shown.
2. **Escape Hatch Always Reachable:**
   - **File** is always `extend`-only so session sign-out and file operations remain accessible.
   - **Tools > Custom Menus** is always appended if `Tools` is replaced, ensuring an administrator or developer can always switch back to `[Standard File4Base Menus]`.
3. **Reserved Shortcut Validation:**
   - Custom shortcuts cannot shadow core operational mode or quit shortcuts (`Cmd/Ctrl+B`, `F`, `L`, `U`, `Q`) and cannot duplicate another shortcut within the same menu set (`422 Unprocessable Entity`).

## 4. Persistence, Bundles & DDR

- Stored in `sys_menu_sets` (`id`, `name`, `is_default`, `menus` JSONB/LONGTEXT) and exposed at `GET/POST /api/v1/menu-sets` and `GET/PUT/DELETE /api/v1/menu-sets/{id}`.
- Exported and imported in `.f4p` solution bundles (`menu_sets`), with automatic remapping of `script_id`, `layout_id`, and layout `menu_set_id` UUIDs on import.
- Included in the Database Design Report (`#49`) across HTML, XML, and JSON formats.
