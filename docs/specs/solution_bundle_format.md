# Solution and Data Bundle Format (version 2.0)

File4Base packages a solution's design in a **solution bundle** (`.f4p`; `.f4b` is accepted as a legacy extension) and its records in a **data bundle** (`.f4data`). Both are a single MessagePack map. This document is the contract between the Go server, which produces and restores bundles, and the Flutter client, which reads them and writes the initial bundle of a new database.

Golden fixtures produced by each side are read by the other side's tests:

- `server/internal/schema/testdata/server_solution_v2.f4p`: written by the server, read by the client tests.
- `server/internal/schema/testdata/client_new_database_v2.f4p` and `client_legacy_v1.f4p`: written by the client, imported by the server tests.

## Encoding rules

- Every map key is a snake_case string, at every level.
- Timestamps are RFC 3339 strings in UTC (`2026-10-06T09:00:00.123456Z`), never the MessagePack timestamp extension, in solution bundles.
- Layout definitions and script step parameters are nested maps (the same JSON object the API uses), not strings or byte arrays.
- Optional keys may be absent or nil. Unknown keys are ignored, so later minor versions can add keys.
- **No secret is ever written**: no account password, password hash, connection password or remembered sign-in password.

## Solution bundle

| Key | Type | Notes |
|---|---|---|
| `format` | string | `"file4base_solution"` |
| `version` | string | `"2.0"`. `"1.0"` files written by the client are read with the same keys; their password is ignored. |
| `solution_name` | string | |
| `exported_at` | string | RFC 3339 |
| `database_connection` | map | `engine`, `host`, `port`, `database`, `user`, `ssl_mode`. `user` is the account that saved the file, used to prefill sign-in. No password. |
| `tables` | array of table | |
| `table_occurrences` | array of occurrence | |
| `relationships` | array of relationship | |
| `layouts` | array of layout | |
| `scripts` | array of script | |
| `value_lists` | array of value list | Optional. |
| `users` | array of account | |
| `saved_finds` | array of saved find | Optional. |
| `privileges` | array of role privileges | Optional. Absent in files written before extended privileges existed, which leaves the destination's own grants untouched. |
| `file_options` | map | Client settings (File Options), opaque to the server. Never contains `default_password`. |
| `page_setup` | map | Client settings (Page Setup), opaque to the server. |

**table**: `id`, `name`, `display_name`, `description`, `columns` (array of column).

**column**: `id`, `name`, `display_name`, `field_type`, `is_nullable`, `is_primary_key`, and the optional strings `default_value` (field options JSON), `calculation_formula`, `validation_rules`.

**occurrence**: `id`, `base_table_id`, `name`, `x_pos`, `y_pos`.

**relationship**: `id`, `name`, `left_occurrence_id`, `left_column_id`, `right_occurrence_id`, `right_column_id`, `operator`, `allow_creation`, `cascade_delete`, `sort_related`.

**layout**: `id`, `name`, `table_occurrence_id`, `definition` (map; see `layout_schema.json`).

**script**: `id`, `name`, `context_table`, `folder_id` (optional), `is_active`, `steps` (array of step).

**step**: `id`, `sequence_idx`, `step_type`, `params` (map), `is_enabled`, `parent_step_id` (optional).

**account**: `id`, `username`, `role` (`owner`, `admin` or `user`), `is_active`, `permissions` (array of `{layout_id, access_level}`, `access_level` being `read_write`, `read_only` or `none`).

**value list**: `id`, `name`, `kind`, `custom_values` (optional), `source_table_id` and `source_column_id` (optional, remapped on import).

**saved find**: `name`, `table_name`, `requests` (array of `{values, omit}`, `values` being field name to the criterion as typed). Matched by table and name on import; the account that saved it is not carried, so a restored find has no owner.

**role privileges**: `role` (`owner`, `admin` or `user`), `bulk_export`, `bulk_import`. Keyed by role, so nothing is remapped. See [security_model.md](security_model.md).

IDs only link objects inside the bundle. They are never assumed to exist in the destination database.

## Restoring a solution bundle

`POST /api/v1/solutions/import` (owner only) merges the bundle into the signed-in database:

1. **Validation first.** The whole bundle is checked before anything is written: structure (see `msgpackguard`), names, field options, and every reference (column to table, occurrence to table, relationship to occurrences and columns, layout to occurrence, step to parent step, permission to layout). A bundle with a dangling reference is rejected with nothing changed.
2. **Matching.** Objects that already exist are matched by name: tables by `name`, columns by table and `name`, occurrences by `name`, layouts and scripts by `id` and then by `name`, accounts by `username` (case-insensitive). Relationships are matched by their two endpoints and operator.
3. **Remapping.** Every reference is rewritten to the destination's IDs: occurrence base tables, relationship occurrences and columns, layout occurrences, step parents, permission layouts, and the `script_id` of button actions and script triggers inside layout definitions.
4. **Writing.** Missing tables, columns, occurrences, relationships, scripts and accounts are created; matched layouts and scripts are updated with the bundle's version; existing tables, columns and accounts are left as they are.
5. **Accounts.** A missing account is created **disabled**, with a random password nobody knows, with its role and its layout permissions. An owner enables it and sets its password in Manage Security. Existing accounts are never changed. Restoring never widens access: a disabled account stays disabled, and an `owner` account can only be restored by an owner.
6. **Saved finds.** A find is created when the destination's table has none with that name; the fields it searches must exist there. Existing finds are left as they are.
7. **Extended privileges.** The grants in `privileges` replace the destination's, by role; the owner's are always stored fully granted. A bundle without the key changes nothing.
8. **Failure.** If any write fails, everything the import created is removed and every layout or script it updated gets its previous version back, and the request fails with the reason.

The response reports what happened: `tables_created`, `columns_created`, `occurrences_created`, `relationships_created`, `layouts_created`, `layouts_updated`, `scripts_created`, `scripts_updated`, `accounts_created`, `saved_finds_created`, `privileges_updated` and `accounts_pending_password` (usernames that need a password before they can sign in).

Legacy `1.0` bundles exported by older servers (PascalCase keys and MessagePack timestamps) are converted to this format before validation.

## Data bundle

| Key | Type | Notes |
|---|---|---|
| `format` | string | `"file4base_data"` |
| `version` | string | `"2.0"` (`"1.0"` accepted) |
| `database_name` | string | |
| `exported_at` | timestamp | |
| `tables_data` | map | table name → array of records (field name → value) |

Values keep their type: text is a string, CONTAINER fields are MessagePack **binary** (byte-exact), numbers and booleans are native, dates and timestamps are MessagePack timestamps.

`POST /api/v1/solutions/import-data` restores records in **one transaction**: every table and field is validated first, then all rows are inserted, and the first failing row rolls everything back and is reported with its table and position. A record whose `id` already exists is skipped, not overwritten. Tables of the file that are not in the destination are reported in `skipped_tables`. The response counts `records_inserted` and `records_skipped` (records_count equals records_inserted).

## JSON data API and CONTAINER fields

`GET /api/v1/data/{table}` returns CONTAINER values as base64 strings (standard alphabet, with padding). `POST` and `PUT` accept base64 strings for CONTAINER fields and store the decoded bytes; a value that is not valid base64 is rejected with `400`.
