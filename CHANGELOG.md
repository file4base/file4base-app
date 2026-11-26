# Changelog

All notable changes to the **File4Base** project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- **SQLite as an engine for solution databases (#199).** `DB_ENGINE=sqlite` keeps each solution database in one file of a data directory (`DATABASE_URL`, default `./data/solutions`), with no database server; `docker-compose.sqlite.yml` is that stack. Every connection runs in WAL mode with `synchronous=FULL`, `BEGIN IMMEDIATE`, a 5 s `busy_timeout` (`SQLITE_BUSY_TIMEOUT`) and a 64 MB `journal_size_limit`, and the writers of a database take turns in arrival order. Finds are case-insensitive and accent-sensitive as on PostgreSQL; sorts follow a calculation's order (`calc.CompareText`) where the default PostgreSQL image sorts by code point. Each column keeps to its type with a `CHECK`; the column changes SQLite lacks (`ALTER COLUMN TYPE`, `ADD COLUMN IF NOT EXISTS`, dropping an indexed column) are emulated in a savepoint. A network share, FUSE, Docker Desktop or WSL mount is refused (`SQLITE_ALLOW_UNSAFE_STORAGE` downgrades it to a warning). `server sqlite-backup` / `sqlite-restore` take a `VACUUM INTO` backup while the server runs and restore it as a new database; `GET /api/v1/server/maintenance` reports the SQLite settings and what differs from them. PostgreSQL stays the default (`dbal.DefaultEngine`); the server suite also runs on SQLite in CI, and `server/cmd/f4b-bench` compares the three engines. See `docs/specs/sqlite_engine.md`.
- **A FileMaker migration ends on its results, and the accounts it created without a password can be given one (#198).** After *Apply Migration*, the migration window shows what was created (tables, fields, layouts, scripts, value lists, accounts, records), then what to review: the conversion report's rejected, manual-action and unsupported items and the import's warnings, grouped by kind (script triggers, scripts, fields, value lists...), folded, selectable and copyable one group at a time or all together, with a button that opens File > Manage > Script Triggers on the triggers the migration switched off. The accounts migrated without a password are listed as unable to sign in, each with *Set password...*, which sets it and, by default, activates the account. The server now marks such accounts (`password_pending` in `GET /api/v1/security/users`, cleared when a password is set), so File > Manage > Security still shows them after the migration window is closed: a *No password* badge, a notice and a filter. The account's password dialog is shared by both.
- **Save Records as PDF writes a real PDF, and Send Mail sends through SMTP (#133).** *Save Records as PDF* draws the layout of the window for the current record, the found set or a blank record — the pages *Print* draws, with the window's page setup — and writes the PDF at the step's path; *Append* adds the pages to a PDF a script saved earlier in the same run, *Automatically open* opens it, and *Create email* hands it to Send Mail. A script reads and writes files in two folders of its own only, `Get ( TemporaryPath )` (a folder of this run) and `Get ( DocumentsPath )` / `Get ( DesktopPath )` (`Documents/File4Base`; in memory in the browser); a FileMaker path (`file:`, `filemac:`, `filewin:`, path lists, relative paths) resolves into them, and any other place fails with FileMaker's error 800 (PDF) or 100 (attachment). *Send Mail* sends through an **SMTP profile** a server administrator sets in **Server Tools > Send Mail (SMTP)** — one per solution, or one for the server — with `POST /api/v1/mail/send`: *via SMTP server* (its sender, sender address and reply-to; never its own server or password), one e-mail per record (in batches), attachments (with the dialog, in a File4Base compose window), and — a documented deviation — *via e-mail client* without dialog. With its dialog and no attachment the step still opens a draft in the e-mail client; without a profile a draft is opened where a client can do it, saying which files to attach, and the rest fails with `1506`. A profile never holds its password: it names a server environment variable starting with `FILE4BASE_SMTP_`, which no endpoint answers (only `password_available`) and nothing logs; the audit log counts messages and never records an address. The server limits each account per hour (`max_per_hour`, `429`), each request (100 messages, 32 MiB) and each message (50 recipients, 20 MiB of attachments), refuses CR/LF in addresses, sender name, subject and attachment names, sends always as the profile's address (a step sender of another domain becomes the reply-to), and answers SMTP failures with FileMaker's errors `1501`–`1506`. `GET/PUT/DELETE /api/v1/server/mail-profiles`, `POST /api/v1/server/mail-profiles/test` and `GET /api/v1/mail/status` complete the API. Migration: Save Records as PDF no longer carries a note or fails with error 3 (except into a variable), and Send Mail notes only what needs a profile, deviates, or names a fixed path outside the script folders; a step's own SMTP server and password are never imported. `docker-compose.yml` passes `FILE4BASE_SMTP_PASSWORD` to the API. The server profile serves only the solutions a server administrator allows (`PUT /api/v1/server/mail-profiles/server-access`, off by default); quota and password are checked before attachments are decoded, and messages are streamed to the SMTP server. A step with a dialog never sends without it (no window: a draft, or error 1 for several messages); a protected PDF fails with error 3 instead of being written unprotected; passwords in a step's options are stored as `[redacted]` in `filemaker_parameters`; a desktop path through a symbolic link out of the script folders is refused; the script folders are emptied on sign-out.
- **File > Manage > Custom Functions edits the solution's custom functions, and script steps call them.** The new dialog lists them and creates, edits, duplicates and deletes them: name, parameters, a formula written with *Specify Calculation* and *Available to* all accounts or full access only. Renaming one rewrites every stored formula that calls it — calculation fields, auto-enter calculations, validation formulas, other custom functions, script steps, layout buttons and triggers — as FileMaker does; a change of parameters that would break formulas, or deleting one that formulas call, lists them and is refused unless forced. A function that calls itself before any condition could stop it is refused. *Specify Calculation* lists the custom functions as a category and has the server check the formula on OK (`POST /api/v1/formulas/check`), keeping the editor open on a syntax error. A script step or button parameter whose formula the client's evaluator cannot evaluate — a custom function, a related field, any other function — is evaluated by the server in one request with the script's `$`/`$$` variables and the `Get ( … )` answers only the client knows, and the variables it sets with `Let` are kept; a formula that fails stops the script naming the step and the formula. The server keeps each solution's compiled custom functions, dropped when one changes. *Replace Field Contents* has the server evaluate its calculation for each record when the client cannot, and a new record not committed yet sends its field values with the request (`values`). A function change under which calculation fields would read each other in a ring through it is refused, as is a calculation field written into such a ring; a rename that finds a rewritten formula saved meanwhile writes nothing and answers `412`; literal step text (`value_is_calculation` false) is neither rewritten nor listed as a use. Stored results calling a changed function are not recomputed, as in FileMaker. New endpoints: `/api/v1/custom-functions` (CRUD, `usages`) and `/api/v1/formulas/check`; `POST /api/v1/data/{table}/evaluate` takes `variables`, `get` and `values` ([custom_functions.md](docs/specs/custom_functions.md)).
- **Validation by calculation, with FileMaker's override.** A field's *Validated by calculation* formula (with `Self`, the record as it is about to be saved, and the solution's custom functions) is enforced by the server on commit, when the field is modified or, with *Validate only if field has been modified* off, on every save. *Allow user to override during data entry* makes a failed rule answer `overridable`, Browse mode asks "Allow this value?", and Yes sends the write again with `X-File4Base-Validation: override`. Field Options > Validation has the three options. The formula reads the unstored calculations as the write makes them. Restoring a `.f4data` file does not check again the rules that allow override, so a value kept over one (a duplicate under *unique* included) is restored. The FileMaker migration reads the rule from the XML export with its formula bound to the imported names.

### Changed
- **The FileMaker migration reads *Validate data in this field: Always* from `type="Always"`**, not only from `alwaysValidate`, and honours *Allow user to override*: a rule set that allows override no longer makes a *not empty* column `NOT NULL` nor creates a unique index for a *unique* rule.
- **Field Options keeps the validation keys it does not show** when it saves a field, instead of dropping them.

### Security
- **A data source reads its password only from a server variable starting with `FILE4BASE_DATASOURCE_`.** A database owner registers data sources, and the server sends a source's password to the host the source names, so a `password_env` naming any variable (`DATABASE_URL`, a signing key) let an owner have the server send that secret to a host of theirs. `POST`/`PUT /api/v1/data-sources` now refuse another name with `422`, and a source stored earlier or imported from a solution file that names one reads no password from it, so the connection fails to sign in unless a password is typed in the import dialog. **Upgrade:** rename such a variable in the server's environment to start with `FILE4BASE_DATASOURCE_` and edit the source to name it.

## [0.22.0] - 2026-11-25

### Added
- **Multi-language application menus and menu language management in Preferences.** All top-level menus, submenus, menu items, color labels, and menu prompts in `File4BaseMenuBar` are now localized via Flutter ARB bundles (`client/lib/l10n/app_*.arb`) across **English (`en`)**, **French (`fr`)**, **Spanish (`es`)**, **German (`de`)**, **Italian (`it`)**, **Dutch (`nl`)**, and **Portuguese (`pt`)**. Opening **Edit > Preferences...** (`Cmd/Ctrl+,`) now presents a dedicated `PreferencesDialog` to switch the menu language with live preview, Cancel rollback, one-click reset to English, and quick access to Server Settings; the selected language persists across sessions (JSON preferences file on desktop, `localStorage` on the Web Client). Solution Custom Menu Sets continue to match canonical menu identifiers regardless of the active display language.
- **Record, mode and object script triggers run, and Go to Portal Row works in Browse mode (#26).** Besides `OnLayoutEnter` / `OnLayoutExit`, File > Manage > Script Triggers and Layout Setup now enable, and a client runs, `OnRecordLoad`, `OnRecordCommit`, `OnRecordRevert`, `OnModeEnter` and `OnModeExit` set on a layout, and `OnObjectEnter`, `OnObjectExit`, `OnObjectModify`, `OnObjectValidate` and `OnObjectSave` set on a field, one script per event of a layout or object, with the parameter evaluated as the script's `Get ( ScriptParameter )`. They run as in FileMaker: `OnObjectEnter` after a field is entered, `OnObjectModify` after each change, `OnObjectValidate` before a changed field is saved and `OnObjectSave` after, `OnObjectExit` before it is left, `OnRecordLoad` when a record is shown, `OnRecordCommit` before and `OnRecordRevert` before a revert, `OnModeExit` before a mode is left and `OnModeEnter` after one is entered; the pre-event ones (`OnObjectExit`, `OnObjectValidate`, `OnRecordCommit`, `OnRecordRevert`, `OnModeExit`) cancel the event when their script ends with `Exit Script [ False ]` (a result that is `0` or not a number), so the field stays active, the value is not saved, the record is not committed or reverted, or the mode is not changed. A trigger runs in the modes FileMaker ran it in (`modes`, Browse when absent; Find mode for the object enter/exit/modify and mode events). A trigger set off again by its own script does not start again, and triggers nest 8 deep at most, so a script that sets the field its trigger watches does not loop; one set off by something else while its script runs (a second commit) waits and gets the same answer. `OnObjectValidate` reads the value typed in the field; after `OnRecordCommit` returns False, what is typed is saved with the record only, not by the field as it is typed or left; a script's *Perform Find* asks `OnModeExit` before the found set is replaced (error `20` when it refuses); `OnRecordLoad` on a layout of another table waits for its records. Removing the last trigger of a layout in Layout mode saves an empty `script_triggers` list. Go to Field and Go to Portal Row run the object triggers of the fields they leave and enter. The object inspector in Layout mode adds, edits and removes a field's triggers (event, script, parameter). The review lists `unsupported_object` (an object trigger on something other than a field) and `no_mode` (FileMaker runs it in no mode). *Go to Portal Row* moves a portal's active row (first, last, previous, next, by calculation, *Exit after last*, *Select entire contents*; `101` past either end), keeping the active field in its column or entering the row's first field; clicking a row makes it active, and `Get ( ActivePortalRowNumber )` and `Get ( ActiveFieldTableName )` answer it. It is the row a script step writing a field of the portal's occurrence writes (#178), and only while the portal shows the record in hand: a row of the record left is never gone to, written or deleted.
- **FileMaker script triggers migrate, switched off, and File > Manage > Script Triggers reviews them (#26).** A migration from Save a Copy as XML now keeps every layout and layout-object trigger instead of dropping it: its event, the imported script it runs (found by its FileMaker id, then by a name only one imported script has; one not found, one whose name several scripts have, and one in another FileMaker file — never bound to a local script of the same id — keep their name, unresolved, with a problem saying which), its parameter rewritten like any calculation, and the object it is set on, in the layout's `script_triggers` list with `enabled: false` and `quarantined_by_migration: true`. Nothing in that list runs, and a client skips an `on_layout_enter` / `on_layout_exit` action with `enabled: false`, so no imported script runs on its own. The report says so in one `manual_action_required` item per solution instead of one item per trigger, plus one item per trigger whose script or parameter could not be bound or that FileMaker runs in Find mode only; triggers naming no script, and those of each event File4Base does not run yet, are counted in one item each. File triggers (`OnFirstWindowOpen`, …), which were silently dropped, are listed in one `unsupported` item. File > Manage > Script Triggers lists every trigger by layout (object, event, script, parameter, status), filters them by status, event, script and layout, searches them, and enables or disables a selection, or every trigger without a problem after saying how many and that their scripts will then run on user actions; a problem reads in plain words, with what to do, and a row opens its script in the Script Workspace or its object in Layout mode. `GET /api/v1/schemas/script-triggers` lists them and `PUT` switches them, all or none, in one transaction that gives each layout changed a new version (owner or admin; unlocked design). File4Base runs `OnLayoutEnter` and `OnLayoutExit` set on a layout, and (see below) the record, mode and object events; triggers of other events are kept, disabled, and cannot be enabled. Layout Setup shows the layout's triggers with a switch, which goes through the same owner-or-admin API, and a link to the review, and the object inspector those of the selected object; `PUT /api/v1/schemas/layouts/{id}` refuses with `403` anyone else switching on a trigger that was off, and keeps the stored triggers when a definition leaves them out. A layout copied — duplicated, pasted, or imported from a solution file — gets new trigger ids, and the review lists ids that name more than one trigger with a `duplicate_id` problem and `PUT` refuses them with `409`. Numbers a switch does not touch keep their exact value in the definition. Closing the review, however it is closed, reloads the triggers of the layouts it changed only, without reopening the current one. A script parameter that reads a field of another occurrence (`Occurrence::Field`), on a button, a trigger or Perform Script, is evaluated by the server in the related context, as a calculation field is, instead of reading the field of the same name in the record; one that cannot be evaluated stops the script. Solution files carry the triggers and their `enabled` flag.
- **FileMaker script step options that a migration used to leave under `filemaker_parameters` are now read** (Save a Copy as XML). *Import Records*: the source (FileMaker file, file on disk, folder, or a path computed from a variable), the target table, the method (*Add*, *Update* on match fields, *Replace*), *add remaining records*, *auto-enter*, the character set and every mapped target field with its source position and whether it is imported or matched on (`import_source`, `import_method`, `field_mappings`...). FileMaker saves the source fields of a FileMaker file by position only and not the source table, so no transfer is built by guessing: the report item and the step now say exactly what the import did and which transfer to create (target table, match keys, `source field N -> field` pairs). *Perform Script* of a script FileMaker wrote as `<unknown>`: a deleted script fails with error `104` as in FileMaker (stopping the script unless error capture is on); a script of another file the document does not name keeps its file reference and says so. *Send Mail*: To/CC/BCC/Subject/Message calculations, *With dialog*, the e-mail client or SMTP route, one e-mail per record, collected addresses and attachments. *Save Records as PDF*: save to file, records being browsed / current record, path and *Create email*. *Go to Portal Row*: Last, By Calculation and *Select*. *Replace Field Contents*: current contents, serial numbers or calculation, and *Skip auto-enter*. *Show Custom Dialog*: input fields (field or variable, label, password).
- **A step converted from FileMaker that cannot run as written, or runs differently, carries a `migration_note`**, shown next to the step and in its inspector in the Script Workspace, listed in the report, and given by the runner when the step stops. *Send Mail* via the e-mail client opens a draft that the user sends: it does not send unattended. *Save Records as PDF* produces no PDF yet.
- **Migrated FileMaker layouts keep their look.** A *Save a Copy as XML* layout now migrates with its parts at their own heights and fills, and its objects at their FileMaker bounds: text (merge fields of the layout's table become `{{column}}`), field objects with their control style (drop-down calendar included), value list, repetitions (from the first one the box shows, so a box showing only repetition 3 shows and edits repetition 3), whether the field takes entry in Browse and in Find mode, placeholder and number/date/time format (a date typed and shown in the order of the source file's locale, month first or day first, with a date picker on a drop-down calendar), rectangles, rounded rectangles, ovals, lines and library pictures, grouped objects one by one, and the front panel of a tab or slide control. The look comes from the layout's theme and each object's local CSS (fill, line, corner radius, font, colour, alignment, indents). Each object has an id stable across migrations of the same file, a related field reached through more than one relationship is shown as unreachable rather than read from a wrong column, a related field of an external occurrence keeps its relationship, and a *Hide object when* calculation is kept as `hide_when` and evaluated in Browse mode, in Form and List view, from the layout's occurrence. Preview mode reads a related field through its relationship and shows a field it cannot read as its name, never as a column of the layout's table with the same name. Before, a Save-as-XML layout migrated with no object and default part heights. The report counts the objects of each FileMaker kind migrated (`counts.layout_objects`) and has at most one `unsupported` and one `manual_action_required` item per layout; buttons, portals, popovers, web viewers and charts are listed there until they are migrated. Browse and List views draw part fills, label boxes, placeholders and fields without a line; List view draws every part above the body once as its header and every part below it once as its footer. The Layout mode inspector edits *Hide object when* and the placeholder.
- **FileMaker custom functions migrate, so the calculations calling them migrate as calculations.** A solution now has its own custom functions (`sys_custom_functions`: name, parameters, formula, and *Availability* as `all` or `full_access`), which every calculation of the solution may call, and which may call themselves, call each other, and call one defined after them, whatever the order they are listed in. A migration reads them from the XML DDR (`CustomFunctionCatalog`), Save a Copy as XML (`CustomFunctionsCatalog`) and the HTML DDR, imports them before the fields, and rewrites them like a field calculation: field references renamed, a decimal comma written `.`. The report has one `preserved` item per function (`translated` when its formula was rewritten), and a function that does not read is `unsupported` with a `manual_action_required` item, as is every function and calculation that calls it, while the others import. A function named like a built-in function is not imported, as FileMaker does not allow it, and a field whose imported name is a custom function's is quoted (`${rate}`) in the formulas reading it. A calculation calling a function File4Base does not run says which one: `ExecuteSQL is not supported`, a plug-in function (`<Function Missing>`), or the custom function that could not be migrated. Solution files carry the custom functions; importing one adds those the solution does not have yet (a name is unique whatever its case) and keeps the definition of those it has, refuses one named like a built-in function, and a failed migration into an existing solution removes those it added. A formula's dependencies include what the functions it calls read. There is no editor for custom functions yet, and `ExecuteSQL` is not implemented.
- **Auto-enter field options work as in FileMaker: stamps, serial numbers, calculated and looked-up values, and prohibit modification.** Creation and modification stamps (Date, Time, Timestamp, Name, Account Name) are entered by the server, and a modification stamp only when a stored, non-global field changes. Serial numbers, on creation or on commit, number or text (`INV0009` → `INV0010`), are allocated atomically under a row lock on PostgreSQL and MariaDB; `GET`/`PUT /api/v1/data/{table}/serials/{field}`, the *Set Next Serial Value* step and `GetNextSerialValue` read and set the next value. A calculated value honours *Do not replace existing value* and *Do not evaluate if all referenced fields are empty*, is evaluated again when a field it reads changes (related references included), may read related fields and `Self` and call the solution's custom functions; a looked-up value copies from the first related record, however many relationships away, with *Don't copy contents if empty* and the no-match options (do not copy, next lower or next higher among the records meeting every other criterion of the relationship, a constant), and *Relookup Field Contents* (`POST /api/v1/data/{table}/relookup`, step and Records menu) copies again over the found set, which stays as it is. *Prohibit modification of value during data entry* refuses a data-entry write with `422 Prohibited Modification` and shows the field read-only in Browse mode, while script steps (header `X-File4Base-Entry: script`), imports and duplicates may still set it. `POST /api/v1/data/{table}/{id}/duplicate` is FileMaker's Duplicate Record, and imports take `perform_auto_enter`; on another solution's occurrence, duplicating, the serial routes and relookup run in that solution (`/api/v1/occurrences/{occurrence}/records/…`), with its own serial numbers. The options stay a JSON object in `default_value`, so older columns and bundles keep working; the Auto-Enter tab of Field Options mirrors FileMaker's, with a lookup dialog and the formula editor. A migration from Save as XML, the XML DDR or the HTML DDR now keeps every option it can and reports the others with the reason ([auto_enter_options.md](docs/specs/auto_enter_options.md)).
- **Portal Setup chooses the end of a relationship joining an occurrence to itself.** Such a relationship (`Tasks::code = Tasks::parent_code`) has the same occurrence at both ends, and a portal always showed the right one. Portal Setup now offers *Left end* / *Right end* for it, the layout keeps the choice as `relationship_side` on `portal_config` (and on a related field's `field_binding`), and Browse mode sends it as `side` with the portal's list, creation and batch requests. A layout naming no end shows the right one, as before. Picking the left end of such a relationship for a related field is now kept too: the field list offered both ends under the same entry and always stored the right one. Find mode sends the end with a criterion typed into such a related field (`side` on a find criterion, new on the server), so it searches that end rather than always the right one, and a field shown from both ends has two criterion boxes.
- **Multi-keys match on either side of a relationship.** A text match field holding several lines was split only on the side of the record in hand, so a related record keyed `A¶B` was never found from a record keyed `A`. Each line is now a key on both sides, as FileMaker indexes it, for every operator and criterion by criterion, and a CR, an LF or a CRLF ends a line, so migrated text (whose returns become LFs) and text a client writes with CRs match alike. A line is a key when it is not empty (a line of spaces is one), the same rule in the server and in SQL, and a date or timestamp meeting the lines of a text field is written as an ISO date (and time, in UTC), so a text multi-key of dates relates to a date field. It applies to portals, batched related reads, related fields and unstored calculations, finds on related fields (local and across solutions), cascading deletes and value lists of related values. On PostgreSQL an `=` criterion is an array overlap a GIN index on the same expression can serve; see the performance note in `relationships_and_portals.md`.
- **Records nothing sorts are in creation order, as in FileMaker.** Every user table numbers its records as they are created, in an engine-filled column (`f4b$creation_order`, not a field), and that order lists a found set without a sort, the records of a relationship without "Sort records" (portals, related fields, `Last`, `GetNthRecord`), and breaks the ties of every sort, so records that sort alike keep it. Records reached across several relationships are ordered all together, not related record by related record. The client's whole-table and whole-found-set reads (List view, reports, exports) no longer force a sort by id, so they come in that order too. They came back in whatever order the engine read them, or by their random UUID. Existing tables get the column at sign-in or on restore, their records numbered in physical order on PostgreSQL (a migrated file's record order) and in id order on MariaDB, without waiting more than 5 seconds for a busy table; `server/migrations/007_creation_order.sql` does the same by hand. Solution exports list records in that order (by id in a table not upgraded yet, as it is read).
- **Portals sort by unstored calculations.** A portal or related read sorted by an unstored calculation, through the relationship's "Sort records" or the caller's sort, was refused with `422 Unstored Calculation`. Its related records are now read in creation order, up to 10,000, computed, stably sorted and then paged; more answer `422 Too Many Related Records`. A found set is still never sorted by one.
- **Five more FileMaker script steps run: `Insert Current Date`, `Select All`, `Refresh Portal`, `Set Error Logging` and `Open File`.** A migration converts them by instruction id (`13`, `50`, `180`, `200`, `33`), so a localized report converts them too, with their options from the XML DDR and Save a Copy as XML: target field (with its table occurrence) or variable, including the DDR's `<Field>$var</Field>` form, and *Select entire contents*; portal object name or calculation; on/off (`<Option state="False"/>` is Off) and *custom debug info*; file reference and *Open hidden*. An `Open File` file reference is created unmapped and listed for mapping, like `Perform Script`'s. In the client runner and debugger, `Insert Current Date` follows the field's type (a date replaces a date field, a timestamp gets `00:00:00`, a calculation, summary or container field is refused with error `201`); otherwise it replaces the content with *Select entire contents*, and without it the active field's selection or caret, as typing would. In Find mode it fills the find request and never saves the record. A field of another table occurrence than the layout's, or a target the migration could not read, is refused with error `102` and nothing is written. `Select All` selects the active field's content. `Refresh Portal` reloads the named portal's related records (error `116` when the layout has no such object); on another object, and in `Refresh Window`, which now reloads portals too, the record in hand is read again and the found set and current record are kept. `Set Error Logging` records each nonzero error once, with script, step, line, code and the debug calculation's value, and stays on across script runs for the solution session until `Off`; it writes no `ScriptErrors.log` file yet. `Open File` checks that the referenced solution is installed on this server and that the user is signed in to it with their own account (errors `114`, `100`, `9`). As in FileMaker, the steps after it still run in the calling solution. Known gaps: buttons, triggers, menus and the debugger do not wire the peer session yet, so an `Open File` naming another solution fails with `9` from them, and the opened solution's opening script is not run.
- **Relationships match on several criteria.** A relationship now holds an ordered list of criteria, each a left field, an operator (`=`, `≠`, `<`, `≤`, `>`, `≥`, or `×`, which always matches) and a right field, and a record is related when it meets every one, as in FileMaker. Portals, related fields, unstored calculations reading `Occurrence::Field`, finds on related fields, cascading deletes and the relationship's sort all apply every criterion, in the right direction from either side, with multi-key fields matching through any of their lines; a record whose match field is empty is related on neither side, whatever the operator. Records can be created through the relationship only when every criterion is `=`, and then every right-hand match field is set from the left record; otherwise the request answers `403 Creation Not Allowed`. The first criterion stays the relationship's own `left_column_id` / `operator` / `right_column_id`, so existing relationships and clients that send a single pair are unchanged; the API returns and accepts the whole list as `predicates`, each side naming its field beside its id. The criteria after the first are kept in a new `sys_relationship_predicates` table (`server/migrations/002_relationship_predicates.sql`, created on start-up). Each criterion's fields are checked against its side's table, an empty list is refused, and deleting a field used by any criterion deletes the relationship rather than widening it. Solution files carry the list (`predicates`, omitted for a single criterion, so existing files are byte-identical). Migration maps every join predicate from the XML DDR and Save a Copy as XML, `×` included and criteria written from the other side turned round, instead of keeping the first and reporting the rest; a predicate whose field cannot be found or whose operator is not known (it used to be read as `=`) refuses the relationship and is named in the report. The Specify Relationship dialog edits the list with Add, Change and Delete and reorders it, and the graph draws a line per criterion with every operator in the badge.
- **Value lists from a field have FileMaker's options: a second field, sorting, and related values only.** A list may show a second field of the same table beside the first (`second_column_id`), or in its place (`show_second_only`), sort by either field (`sort_by`: `first` or `second`), and include only the values of the records related to the current record of a table occurrence (`related_occurrence_id`, read through the list table's occurrence `source_occurrence_id` along the existing relationships, as the caller). Choosing an entry always stores the first field's value; pop-up menus, drop-down lists, checkbox and radio button sets show the second field's text, also for a value already stored. `GET /api/v1/schemas/value-lists/{id}/values` returns `items` (`value`, `second`) beside `values`, and takes `?table=&record_id=` for a related list, which gives no values without them. Each entry is offered once: each value, each distinct pair of values with a second field, or each text when only the second field is shown (storing the least value of the records showing it, since FileMaker too shows each text once). A list of more than 500 entries offers the first 500 in its order, counted after merging repeats, and says so (`truncated`, "More values than shown" in the controls). A checkbox set keeps, checked, a stored value the list does not offer, and a list of related values is read again when a field of the record is saved. Saving refuses a second field of another table, an occurrence that does not exist, and a source occurrence of another table (`422`). Deleting the second field clears it; deleting either occurrence deletes the list. The options travel in solution bundles and design reports, and the Manage Value Lists dialog edits them. Lists without them behave as before. The columns are added to `sys_value_lists` on start-up and on first use by an older database (`server/migrations/004_value_list_field_options.sql`, with the foreign key names the server uses). Migration maps the options from Save a Copy as XML and the XML DDR; a second field of another table occurrence (also one of the same table, such as a self-join, whose value is a related record's), or a related occurrence that was not migrated, is still reported.
- **Repeating fields.** A field can hold up to 32,000 repetitions, set as *Maximum number of repetitions* in Field Options > Storage (`max_repetitions` on the schema API; a summary field or the primary key cannot repeat). Repetition 1 stays in the field's own typed, indexed column, so finding, sorting, summaries, relationships and validation rules use it and a client that knows nothing of repetitions is unaffected; repetitions 2 to N are kept together in a companion text column holding a JSON array, the same on PostgreSQL and MariaDB. The data API reads and writes them under `"field[n]"` keys, returning only those that hold a value; a `PUT` changes only the repetitions it names. Lowering the maximum, even to 1, hides the repetitions past it without deleting them: writing the other repetitions keeps them, `.f4data` bundles and backups carry them, and raising the maximum shows them again, as in FileMaker. Formulas read `Field[n]`, `Related::Field[n]` and `GetRepetition`; `Sum`, `Count`, `Average`, `Min`, `Max`, `List` and `Last` take every repetition of a single repeating field; a repeating calculation is evaluated once per repetition, with `Get ( CalculationRepetitionNumber )` and `Extend`. Tab-separated, CSV and workbook exports write a repeating field as one cell separated by the group separator (0x1D), or its first repetition only (`first_repetition_only`); imports split such cells into the repetitions; `.f4data` bundles and solution bundles carry every repetition and the maximum. Migration keeps the maximum from XML and HTML design reports and every repetition from FMPXMLRESULT exports and native `.fmp12` files, each typed on its own (a native two-byte record key that reads both as a repetition of a repeating field and as a field id from 384 up goes to the repetition when the field declares it), instead of reporting the field as collapsed to one column. On a layout, a repeating field shows its repetitions stacked (or side by side with `repetition_orientation: horizontal`, `repetitions_shown` limiting how many), each editable. A find matches repetition 1 only, only repetition 1 keeps a migrated original text, and container repetitions past the first are not yet read from native `.fmp12` files.
- **Containers in calculations, and Container result calculations.** A formula sees a container field as its file: `GetAsText ( Photo )` and `Photo & ""` give the file's name, `IsEmpty` is true when there is no file, and `GetContainerAttribute ( Photo ; "filename" | "fileSize" )` answers its name and size; `If`, `Case`, `Choose`, `Let`, `GetRepetition`, `Photo[n]` and `Related::Photo` pass the container itself on. A calculation can now have the result type *Container* (Field Options, and `"result_type": "Container"`): stored or unstored, its column holds a reference to the container field it answered, not a copy of the file, and the data API returns the referenced file with its metadata exactly as it returns a container field, so the client shows it like one and exports write it like one. The value follows the field it refers to. Migration keeps FileMaker's Container (binary) result calculations as calculations instead of converting them to stored container columns reported as unsupported. Finding or sorting by a Container calculation works on the reference, not the file name. Within one insert, update or import, a Container result read by another calculation is the file being written, and an added record's result refers to that record; a reference whose record or field is gone is empty (`IsEmpty` is 1), as reading it gives `null`.
- **FileMaker files whose relationship graph uses another file's tables migrate instead of being refused (#107).** An occurrence of a table of another FileMaker file, through an External Data Source of type FileMaker, becomes an **external occurrence**: it goes through a file reference, created unmapped with the source's paths (`UniversalPathList` in Save a Copy as XML, `pathList` in an XML DDR), and names the table in the other solution when the document does (a DDR) or stays unbound when it does not (Save a Copy as XML). Relationships, value lists and calculations through it name its fields as the other file's own migration names them; a field the document names **by id only** (FileMaker writes `name=""` when the other file was not reachable as it saved the design) is kept as the marker `#<id>` in relationships, value lists, calculations and layouts, listed under `manual_action_required` rather than refused, and resolved at bind time by the FileMaker id every migrated column now keeps (`source_field_id`, from Save a Copy as XML and both DDRs): a candidate table must have every such id, and the bind report's `resolved` names the field each marker was found as. The preflight and apply reports list each one under `manual_action_required` and in `external_occurrences`, with `suggested_database` (the one installed solution matching a path) and `cycle` (signed-in callers: that solution leads back to this file). ODBC data sources are still rejected. `POST /api/v1/file-references/{id}/bind` maps a reference to a solution and resolves each occurrence's table there, with a `peer_token` of that solution, never guessing between several candidate tables; `POST /api/v1/file-references/{id}/verify` reports each occurrence as `unbound`, `bound` or `mismatch`. Both are owner/admin operations (`403` otherwise); bind checks the whole request before writing (`422`, nothing changed) and saves the mapping and the tables in one transaction, and an id that several fields share is a `mismatch` naming them, never resolved to one. Only a field reference with an empty name becomes a marker: a FileMaker field actually named `#4` stays a name. Neither reads a record. What cannot be read through an external occurrence answers `424 Failed Dependency` with `code: "external_unavailable"`, the `reference` and `database` to fix and a `reason`, and is never answered from a local table (reading and writing records, and calculations and finds, across solutions: see the next three entries). A file reference in use cannot be deleted (`409`). The catalog gains nullable `sys_table_occurrences.file_reference_id` and `external_table`, `sys_file_references.paths`, `sys_value_lists.source_column_name` and `sys_columns.source_field_id` (`server/migrations/005_external_occurrences.sql`), applied on sign-in or on first use of an older database; the solution bundle carries `external`, the side and value list field names, the reference paths and each column's `source_field_id`, all omitted for a solution without external occurrences, whose file is unchanged; restoring one never retargets an existing occurrence of the same name to another table (a conflict). In the client, the relationship graph draws an external occurrence with an italic title naming its data source and lists the fields its relationships match on; Specify Table shows where its records live, read-only; and Edit Relationship keeps its criteria as they are, by field name. A layout on an external occurrence is bound to no local table; a field of an external occurrence on a local layout keeps its occurrence and is shown as unavailable, never bound to a local field of the same name.
- **Related fields, portals, value lists and layouts read another solution's records, as the account held there (#107).** A session now belongs to a **session group**: signing in to a solution tries the same username and password in each solution its external occurrences read, and in those they read, once each (a cycle links each solution once, at most 16), and lists the outcome in the sign-in's `linked` (`linked`, `refused`, `mfa_required`, `throttled`, `missing`). Every attempt counts against that account's own lockout, and none can fail the sign-in; a link does not walk the other solution's accounts, so a sign-in takes no longer for their number. Solutions are matched by their exact name (on PostgreSQL `Sales` and `sales` are two), in links, in a group's reads and when binding a file reference. `POST /api/v1/auth/link` signs in to one of them with an account of that solution (`DELETE /api/v1/auth/link/{database}` signs out of it); `GET /api/v1/auth/session` lists `linked`. Membership is kept by the server: a linked session is checked against its own account, so a disabled account or a changed password signs it out of the group, and signing out ends every session of the group. A linked session's own token ends with the group's session: once that one expired or signed out it is rejected, and using it does not extend the group's. The other solution is always read as that account, with its privileges and table access there, never with the signed-in one's. Related records and portals follow this solution's relationship with every criterion (#160) and read the related records there, with no SQL joining the two databases; a value list of a field of the other table offers its values; a field the catalog knows only by its FileMaker id (`#<id>`) in a predicate or value list is read as the one field bound to that id (`source_field_id`), as binding resolved it, and as `schema_mismatch` when none or several have it; `/api/v1/occurrences/{occurrence}/records` reads and finds the records of an occurrence wherever its table is, and `/api/v1/occurrences/{occurrence}/table` gives its fields from that solution's catalog. `POST /api/v1/data/{table}/related` reads the related records of up to 100 records at once, in the order `sort` gives, in a fixed number of statements however many records (no N+1), for local and external relationships alike. What cannot be read answers `424 external_unavailable` with `reason` `not_signed_in`, `no_access`, `schema_mismatch` or `missing`. Writing their records and cascading into them, and calculations and finds reading them, come with the next two entries. In the client, portals, related fields and list rows show the other solution's records; a portal on a list row shows that row's related records, not the selected record's, and the related fields and portals of all rows are read in one request per relationship; and a layout on an external occurrence browses them; when a solution is not signed in to, the sign-in dialog opens once, titled *Open <solution> (used by <this solution>)*, and the field, portal or value list offers to sign in afterwards (a value list of the other solution no longer turns silently into an edit box); a layout on an external occurrence whose records need a sign-in opens that dialog and reads them once more once signed in; another reason is shown in place, naming the file reference and what to do. Solutions without external occurrences behave as before.
- **Portals, layouts and relationships write another solution's records, and cascading deletes cross solutions, as the account held there (#107).** A layout on an external occurrence, and a portal row of one, create, edit and delete that solution's records through `/api/v1/occurrences/{occurrence}/records` (`POST`, `PUT`, `PATCH`, `DELETE`), run there by that solution's data service as the account the session group holds there, with its validation, auto-enter values, stored calculations and record privileges. Creating a related record through a relationship into another solution (a portal's last row, `POST /api/v1/data/{table}/{id}/related`, or `POST /api/v1/occurrences/{occurrence}/records/{id}/related` from a layout on one) reads the match values here and sets every criterion's field there (every criterion `=`, an empty key answering `422`). A write the account may not make answers `403`, naming the occurrence, the table, the solution, the file reference and the account, and the remedy; no access answers `424 no_access`. A cascading delete that reaches another solution follows each solution's own cascading relationships from its own records, keyed by solution so a ring of solutions reading each other deletes each record once; it is planned in full first, a level at a time with one statement per table and batch (no N+1, no statement joining two databases), checking that the account held in every other solution may delete each table it reaches there, and refuses the whole delete (`403` or `424`) before anything is deleted. Each other solution's records are then deleted in one transaction there, and this solution's last, in one transaction; each related record is deleted only while its match fields still hold the key of a record being deleted, so one moved to another record after the plan is kept. When that last part fails after another solution committed, the request answers `500` with `code: "partial_delete"`, `partial: true` and the solutions already done (`committed`), and deleting again finishes it, though the re-run may leave records reachable only through the committed part. A local cascade is now one transaction too, decided in full before any record is deleted: a delete refused or failing part-way no longer leaves some related records deleted. Other solutions' clients get the realtime change event of what was written or deleted there. `GET /api/v1/occurrences/{occurrence}/table` returns `privileges` (`read`, `create`, `edit`, `delete`), and the client enables New, Duplicate, Delete, field entry, and a portal's new row and delete buttons by them; a write into a solution not signed in to opens the sign-in dialog and is made once more once signed in. Related fields of another solution are shown, not typed into, as local related fields are; calculations and finds reading them: see the next entry.
- **External occurrences can be designed and repaired from the client (#107).** The relationship graph's card of an occurrence of another solution's table lists that table's fields as its solution has them, strikes through any a criterion names that it no longer has, and carries a badge (*Unbound*, *Not signed in*, *Missing*, *No access*, *Mismatch*, *OK*) whose tooltip names the file reference, the solution and the remedy; clicking it signs in or opens the new **File > Manage > File References...**. That dialog lists, adds, edits (name, solution, FileMaker paths, a path naming one installed solution being suggested, never chosen), binds, verifies and deletes file references, with the bind or verify report of each occurrence and a choice of that solution's tables for one left unbound; binding needs an owner or administrator account in the other solution, and the dialog says so and offers to sign in there as another account. Specify Table's *Data Source* offers each file reference and *Add File Reference…*, and lists the tables of its solution through the session held there; Specify Relationship reads an external side's fields there and names them. After a sign-in, a banner lists the solutions the account could not be linked to (`refused`, `mfa_required`, `throttled`), each with *Sign in…*. Perform Script, Open File and Import/Export Records into another solution use the session held there, or ask for an account first. Layout mode designs a layout of an external occurrence with that table's fields. `POST /api/v1/auth/link` now also signs in to the solution a mapped file reference names before any occurrence goes through it, and to a transfer's far solution. A value list taken from a field of another solution's table has a second field (`second_column_name`, catalog migration 008), shown beside the first or in its place and sorted by either, and can include only the values related to a record of this solution.
- **Unstored calculations and finds on related fields reach another solution's records (#107).** An unstored calculation reading through an external occurrence — `Occurrence::Field`, `Count`, `Sum`, `List` and the other aggregates, along a path that may cross both solutions — is now computed from that solution's records, read there as the account the session group holds in it, with its privileges, instead of evaluating empty. That solution's own unstored calculations are computed there with its graph, and may read the first solution back; a ring of formulas across solutions stops as a local one does. When the other solution cannot be read (not signed in, unmapped, a field it no longer has) the field is empty, as a formula that fails is, and the record is still read; a table the account there may not read gives `<No Access>`, as a local one does. Unstored calculations of a page, local or not, no longer read related records one record at a time: what the page's formulas read is read ahead in one statement per relationship for the whole page (a branch per match value, in chunks of 500), so a list takes a fixed number of statements however many records it shows. They read no more than the formula needs, and only the columns it uses: `Occurrence::Field` reads the first related record in the relationship's sort; `Count`, `Sum`, `Average`, `Min` and `Max` of a stored field of the records one relationship away are totalled by the database where the records are, without carrying them back; `Last` and `GetNthRecord` read the one record they give (the last holding a value, or the nth, with `LIMIT 1` and `OFFSET`) when the relationship sorts its records and they are one relationship away; `List`, and the other reads of every value, take at most 10,000 related records of one record, and a formula reading more fails (the field is empty) rather than loading them all. A relationship's sort order now ends with the records' ids, so records that sort alike come in the same order in every read. A Container result taken from another solution's record keeps that solution: it names and serves that record's file, never the file of a record of this solution with the same table and id, and its name is read only with access to that table. A find (or summary) with a criterion on a related field of another solution's table no longer answers `424 missing`: the criterion is run in that solution, as that account, and the records searched are filtered with the match values it finds, with no SQL joining the two databases — the same records a local related find finds, for every find operator, every relationship operator and several criteria. For a single `<`, `≤`, `>` or `≥` criterion only the bound is carried back (Boolean match fields included); otherwise at most 10,000 match values for the whole find, all its criteria together, beyond which it answers `422 Find Too Broad`. The values are bound once in the statement, however many requests read them, and an omitting request before an including one binds its values in order on MariaDB too (a local find with that order compared the wrong values there). Not signed in, no access or a missing field answers `424 external_unavailable` as reads do. A find through a relationship from a record of another solution, and sorting by a related field, are still not done.
- **`POST /api/v1/data/{table}/evaluate` evaluates from an occurrence, and for several records at once.** `occurrence` (an id or a name) evaluates the expressions from that occurrence of the table, as a layout on it does, so a related field is read through its relationships; `record_ids` (at most 100) answers `{"records": {id: results}}`, `null` for a record not found, in one request that reads the formulas and fields once.
- **Migrated layout fields several relationships away show their related record (#26).** In an anchor–buoy graph most fields of a layout are on an occurrence reached through two or three relationships; a migration now keeps the chain of occurrences (`occurrence_path`) instead of marking the field unreachable, and Browse, List and Preview modes show the first record at the end of the chain, as FileMaker does, read for every row of a list in one request (`POST /api/v1/data/{table}/related-first`, starting from the layout's occurrence). In Browse mode a text or number field there is typed into and writes that record, in its own table, unless it takes no entry in Browse mode; the record is read again before it is written, and again whenever the record in hand is saved, so a changed match field never leaves the edit on the record it used to reach. A path whose two occurrences more than one relationship joins is refused rather than followed through either. In Layout mode such a field is rebound to another field of the path's end, never of the layout's table. Only a field no relationship joins to the layout's is still shown as unreachable.
- **Migrated layouts keep their buttons, button bars and grouped buttons (#26).** Each runs what it ran in FileMaker: a script, bound to the imported scripts by its FileMaker id and then by a name only one script has, with its parameter, or a single step converted as a script step is. An icon-only button shows its icon; a grouped button is drawn by its members with the click over them. A step's fields are bound to the columns they were imported as (two fields named alike, such as `Unit Price` and `Unit_Price`, are told apart), a protected Save Records as PDF stays protected, and a script of another file is never bound to a local script with the same id. A button whose script cannot be told or is in another file, whose step does not run or names a field that was not imported, or whose parameter cannot be bound is kept switched off, says so when clicked, and is reported. A button of a list row acts only once its row has become the current record. Changing only the label of a switched-off button in Button Setup keeps it switched off.
- **Migrated tab and slide controls keep all their panels (#26).** The control is drawn with its tab labels, and the objects of every panel are migrated, each tagged with its panel; Browse, Find and Layout modes show the panel in front (the one FileMaker opens on) and bring another forward when its tab is clicked, and a control on a panel follows its panel. Before, only the front panel's objects were kept, without the control.
- **Migrated layouts keep their portals (#26).** A portal of an occurrence one relationship from the layout's keeps its relationship, initial row, number of rows, sort and filter, and the objects of its row; a layout portal can now filter its records with a calculation (`portal_config.filter`), evaluated from the portal's occurrence against each related record. A filter the migration cannot bind shows no record and is reported. A field of another occurrence placed in a portal row shows its name instead of reading a same-named column of the row's record, and a button in a row is drawn on every row (it says it does not run there yet when clicked). A portal of an occurrence several relationships away keeps its chain of occurrences (`portal_config.occurrence_path`) and lists every record reached at the end (`POST /api/v1/data/{table}/related-along`); no record is created through it. A portal that read only its first 1,000 records refuses Go to Portal Row to its last row or past those it has (error 3), leaving no row active.
- **Conditional formatting by calculation (#26).** A field's conditional format may be a calculation (`operator: "formula"`, "formula is true" in the conditional formatting dialog), evaluated by the server against the record shown with the layout's hide conditions, in the same requests; a migration keeps FileMaker's "Formula is" formats of fields with their text colour, fill, bold, italic and underline, and reports the others.
- **Migrated layouts keep their popovers (#26).** A popover button is migrated with its popover, the panel and its objects drawn over the rest of the layout; the popover is closed when the layout opens, and the button opens and closes it in Browse mode.
- **Layout calculations open in the calculation editor.** In Layout mode the inspector's "Hide object when", Portal Setup's new "Filter portal records" and a "formula is true" rule of Conditional Formatting each have a Specify Calculation button opening the calculation editor with the fields of every occurrence, from the layout's occurrence (from the portal's for a filter).

### Fixed
- **A script step that writes a field of another table occurrence writes the related record, never the field of the same name of the record in hand.** Set Field, Set Field By Name, Insert Calculated Result, Insert Current Date, Insert from URL, Paste, Clear, Cut, Copy and the input fields of Show Custom Dialog keep the occurrence a field is named with (`Occurrence::Field`, or the `field_table` of a migrated step) instead of dropping it. A field of the layout's own occurrence is the record in hand's (only that occurrence: another occurrence of the same table, a self-join, is a related one, and the table's name stands for the layout's occurrence only where no occurrence has that name); one of another occurrence is reached through the relationship graph from the layout's occurrence and written, as FileMaker does, in the active portal row of that occurrence (the portal row last clicked into, in a list on the record of its own row, until a field of the record is entered, and only while it is still related), otherwise in the first related record through the relationship, in the relationship's sort order, and, when there is none, in a record created through the relationship if it allows creation; otherwise the step fails with error 101 and writes nothing (102 for a field or occurrence that does not exist, 103 when no relationship leads to it, 201 for a calculation, summary or key field). A new record is committed before its related field is read or written, so Cut and Copy put the related value on the clipboard. The input fields of Show Custom Dialog show and commit the related field. Insert Current Date reads the type of a related field from its own table, so a related date field gets the date in place of its content. A related record of another solution is written through its occurrence, as the account signed in there. Set Next Serial Value and Relookup Field Contents get the occurrence of a migrated step too, and Relookup Field Contents and Insert Picture / Insert File refuse a field of another occurrence instead of using the layout's field of that name.
- **Dates and timestamps written as text are stored on MariaDB, and a migrated time of day is stored on PostgreSQL.** MariaDB's `DATETIME` and `DATE` refused RFC 3339 text (`2026-01-02T03:04:05Z`, or with an offset), which the import, transfers, a data file, a migration's records, a stored timestamp calculation and the data API all write, so those writes failed there. A migration also wrote a FileMaker Time field's time of day on 1 January of year 0 (`0000-01-01T15:04:05Z`), which PostgreSQL does not have, and dropped its fraction of a second. Every record write now binds a timestamp given as text as the instant it names, in UTC, and a date as its `YYYY-MM-DD` day, on both engines and in every repetition; text that names no date or instant is still refused. A formula and the auto-enter options read that value rather than the text sent, so `Hour` of a timestamp sent with an offset is the hour in UTC. On MariaDB a timestamp is bound as a UTC literal: the driver wrote a `time.Time` in the connection's zone, and a Time field's midnight (Go's zero time) as `0000-00-00`. A date a data file holds as a time keeps its UTC day on a server west of UTC. A time of day is placed on 1 January 0001, as the calculation engine and FileMaker place one, and a value already written on year 0 is moved there when it is written again.
- **Finding and matching on dates and timestamps compares days and instants, not text.** A find compared each engine's text of the column (`2026-01-02 03:04:05+00` on PostgreSQL, with microseconds on MariaDB) with what was typed, so a date typed as File4Base shows it (`31/01/2026`, `2 Jan 2026`), a timestamp typed as RFC 3339 or with a date only, a time of day, and a range whose end day holds later instants (`2026-01-01...2026-01-31`) found nothing or too little; "update matching" on a date or timestamp match field matched nothing. What is typed now names a span (the year `2026`, the month `2026-01`, a day, a minute, a second, a time of day `15:30` or `3:30 PM`), which `=`, a typed value and `==` find, and whose edges `<`, `<=`, `>`, `>=` and `...` compare with. A day that reads both ways (`02/01/2026`) is refused with `400` naming both, rather than guessed. Wildcards still compare text. An import matches a date or timestamp key as the day or instant it names.
- **Go to Record/Request/Page and Go to Portal Row read from Save a Copy as XML went to the wrong record.** The target code in `<List name="Next" value="4">` was taken for a record number (record 4 instead of the next one); the target word is now read first, with FileMaker's codes and the *By Calculation…* ellipsis understood.
- **Replace Field Contents with a calculation wrote the current record's value into every record.** The calculation of a converted step was not read, so the step replaced with the current contents; it is now read and, as in FileMaker, evaluated for each record of the found set (serial numbers likewise counted per record). The replace now covers the whole found set, including the records the window has not read yet (a browse reads its first 100 records, a find its first 500), less the records omitted from it, in its sort order. A write the server rejects stops the replace with a non-zero error (`504` not unique, `509` other validation, `200` no access, `101` record missing), the records before it staying replaced, where it used to be skipped silently. A field of another table occurrence than the layout's is refused with error `102` and nothing is written, where the layout's own field of that name used to be written. With dialog, the runner asks before rewriting the found set; Cancel, or closing the dialog, leaves it unchanged (error `1`).
- **A script dialog closed without a button (Escape, a click outside it) no longer answers its first button.** It answers no button (`Get(LastMessageChoice)` is `0`) and commits no input field.
- **Show Custom Dialog buttons read from Save a Copy as XML have their labels and their Commit option.** The label is the parameter's `value` and *Commit* its `<Boolean type="Commit">`; both were missed, so every button was unlabelled and committed the input fields, Cancel included. Unlabelled buttons after the last labelled one are dropped, as FileMaker does not show them.
- **Save Records as PDF reports error `3` (command unavailable) instead of success**, since no PDF is produced yet; the script stops there unless error capture is on.
- **Send Mail never sends a calculation it cannot evaluate as its own text**, and refuses what a draft opened in the e-mail client cannot do (SMTP, one e-mail per record, collected addresses, attachments) with a message, instead of dropping it silently.
- **A migrated field offers the value list it was given.** Importing a solution created the value lists after the layouts and left each field pointing at the list's id in the file; the lists are now created first and the fields rewritten to the ids they have in the database. Selecting a layout also finds its table through its table occurrence, rather than comparing the occurrence's id with table ids.
- **Evaluating a formula no longer tells other clients the table changed.** `POST /data/{table}/evaluate` is a read, but it was announced over the realtime channel as a change to the table, so every client showing it reloaded; with layouts evaluating their *Hide object when* conditions as records are shown, two clients on one table could keep each other reloading.
- **A value list showing only its second field works on PostgreSQL when its first field is a Boolean or a container.** `GET /api/v1/value-lists/{id}/values` failed there (`function min(boolean)` and `min(bytea) does not exist`). A Boolean entry now stores the least value of the records that show its text (`false` before `true`), on both engines. A container is offered, in every list taken from a field and as a first or second field, as the name of its file, which is the text FileMaker and a calculation give a container, rather than its bytes in base64, which matched nothing and grouped by content; a container whose file name is not known (the data API keeps none for an uploaded file) offers nothing.
- **A value list cannot take its values from a field that cannot be indexed.** A list of an unstored calculation or a summary field, as its first or second field, was accepted and was always empty, since neither keeps a value in the record. As in FileMaker ("This field cannot be indexed"), creating or updating one now answers `422 Invalid Field Options` naming the field and why, and so does making unstored a calculation a list reads; Manage Value Lists refuses it before saving. A migration reports such a list as a manual action instead of creating it empty, and drops an unindexable second field with a report, as it does a second field it cannot bind. Restoring a solution file still restores such a list as it was saved, so that a backup taken before this check stays restorable, and lists it in the import report's new `warnings`.
- **`Last` and `GetNthRecord` across a relationship that does not sort read past 10,000 related records.** With no sort there was no order to count from, so they read every related record and failed beyond 10,000; they now read the one record they give, in creation order, as for a sorted relationship.
- **Creating a related record needs the create privilege on the related table.** `POST /api/v1/data/{table}/{id}/related` accepted any write privilege there, so an account that may only edit the related table created records in it; it now answers `403` unless it may create there, for a table of this solution as for another's.
- **Relationship options are set per side, as in FileMaker.** FileMaker gives each side of a relationship its own *Allow creation of records in this table via this relationship*, *Delete related records in this table when a record is deleted in the other table* and *Sort records*, each acting on that side's records. File4Base had one of each, on the right side: migration dropped the left side's options from XML designs, and merged both sides' from an HTML DDR, so a left side's "delete related records", set to delete a parent's children when the child table is drawn on the left, deleted the parent with any of its children. The left side now has its own `left_allow_creation`, `left_cascade_delete` and `left_sort_related`, beside the right side's `allow_creation`, `cascade_delete` and `sort_related`: a record of the right side creates records of the left through it when the left side allows it, deleting a right record deletes the left records it matches when the left side's option is on (never the other way round), and a portal showing the left side uses its sort. Migration keeps each side's options from the XML DDR (`cascadeCreate`, `cascadeDelete` and the sort on `<LeftTable>` and `<RightTable>`), Save a Copy as XML (`<SortSpecification>`) and the HTML DDR (each side's column), and the report says which side's records each acts on; a sort level on a field it cannot keep is listed, and an HTML report's *Delete* that names no side is left to the operator instead of guessed. The Specify Relationship dialog shows the options in two columns, one per side, named by its occurrence; Portal Setup's *Allow creation* follows the side the portal shows; design reports list each side's options. The API, `openapi.json` and solution files carry the left side's options (omitted when off, so existing files are unchanged and open with none); the columns are added to `sys_relationships` on start-up and on first use of an older database (`server/migrations/006_relationship_left_options.sql`), off for every existing relationship, which so acts as before. Importing a solution file onto a solution that already has one of its relationships (same occurrences, same criteria) now gives it the file's options for each side instead of keeping its own, so a design migrated again corrects options an earlier import set on the wrong side (`relationships_updated` in the import report). On a relationship joining an occurrence to itself, where both ends are that occurrence, a cascade now follows the end whose option is on instead of the end the occurrence names, so a left-side option deletes a report's boss rather than its reports; related reads and creation take `side` (`left` or `right`) to name the end. Creating a related record and planning a cascade no longer apply or check the relationship's sort, so a sort on an unstored calculation no longer refuses them.
- **The HTML DDR keeps each relationship criterion's operator.** Every criterion was read as `=`, and only the last of several was kept, so a relationship matching on `≠`, `<`, `≤`, `>`, `≥` or `×`, or on several criteria, related other records than in FileMaker. Each "Field" row is now a criterion with the operator of its middle cell (also from a report writing the criterion as one text); an operator that is not one of FileMaker's refuses the relationship, named in the report, instead of being read as `=`.
- **A related record is created from the left side of its relationship only.** Allow-creation creates records of the right side from a record of the left, as documented and as FileMaker's per-side option; a request from a right record created a left one, and now answers `403 Creation Not Allowed`, naming the relationship and the side to create from.
- **Delete Portal Row no longer deletes the record the portal is on.** The script step deleted the current record, the portal's parent, with its cascade. It now deletes the related record of the active portal row (see *Go to Portal Row* above), after the confirmation FileMaker asks for unless *With dialog* is off, and with no active row deletes nothing and sets `Get ( LastError )` to `101`.
- **Deleting a record that is not there answers `404 Record Not Found`.** It answered `400 Data Delete Error`, through `/api/v1/data/{table}/{id}` and the occurrence routes alike.
- **A value list taken from a field no longer shows a table the user may not read.** `GET /api/v1/value-lists/{id}/values` read the list's table directly, so a user without access to it got its values. Every field list is now read with the caller's privileges, and a table they may not read offers nothing.
- **Value lists from a date field offer dates.** On PostgreSQL a DATE value was offered as `2026-06-15T00:00:00Z`, which matched no stored value and was not saved when chosen. Values are now written as the field shows them (`2026-06-15`; numbers without padding zeros).
- **A backup no longer succeeds empty.** Every part of a manual or pre-update backup was read with its error ignored, so a database whose catalog could not be read (another database nobody had signed in to since an upgrade, whose catalog migrations had not run) was saved as an empty snapshot answered with `201`, and a container update went ahead after one. The catalog migrations are now applied before a backup, and a part that cannot be read or exported answers `500 Backup Failed` (`500 Update Failed` before an update, which then does not happen) and writes nothing. Database maintenance applies the migrations too.
- **A restore whose records cannot be put back fails.** Restoring emptied the tables and then ignored a failed record import, answering `"status": "restored"` over an empty database. It now answers `500 Restore Failed`, saying the tables were emptied; a snapshot whose records cannot be decoded is refused with `400 Corrupt Backup` before anything changes. An existing calculation field also gets back the maximum number of repetitions it had in the snapshot, which its records need.
- **Stale cells of unstored calculations no longer refuse native records.** A native `.fmp12` file can still hold a cell for a calculation set to *Do not store calculation results*, for instance a container reference to an object of another table. It was read like a stored value, failed as a container with no reconstructable payload, and refused every record of the file. FileMaker recomputes such a field whenever it is shown, so its cells are now ignored whatever the field's type; an HTML design report's *Do not store* option is now read too. The files FileMaker kept inline for a Container calculation, stored or not, are FileMaker's own answers: they are ignored instead of refusing the whole file, and a Container calculation that cannot be converted becomes an empty container field reported under `manual_action_required` rather than a container field holding FileMaker's copy.
- **A transfer reads its source with the privileges of the account that runs it.** The export side of a transfer, run by a request or on a schedule, read every table, so a calculation reaching a related table, or a Container result referring to a file in one, carried data the account may not read into the destination. It now reads as that account, as the data API does.
- **Table occurrences on a deleted table no longer block a migration.** FileMaker keeps an occurrence whose base table was deleted, with an empty source reference, and shows it as `<Table Missing>`. It was refused as "Base table not found", and so was every relationship and layout using it, which blocked the whole solution (4 of 25 real solutions). The occurrence, its relationships and its layouts are now reported as not migrated, since they have no records in FileMaker either.
- **Pictures in open external storage migrate.** For a picture, FileMaker writes the stored-file descriptor (size, MD5, name) in the image fork, such as `jpeg`, rather than in a `file` fork. The preflight only read `file` and refused the cell as having an incomplete descriptor, which blocked every record of the solution.
- **A container that only referenced a file on a user's computer no longer blocks its table.** FileMaker can store a reference (`filewin:`, `imagewin:`, `file:`, `image:` and similar) instead of the file. When that file is not supplied, the record now migrates with the cell empty and the reference is listed as unsupported; missing files from FileMaker's own external storage still block the table.
- **Value lists from a field migrate from an XML DDR.** The DDR names the list's field as `<PrimaryField><Field table=…>` directly under `<ValueList>`, which was not read, so every such list was reported as taking its values from `::` and not migrated. It is now read, with its options.
- **Value lists from Save a Copy as XML migrate with their values.** A custom list is written as one `<Text>` with one value per line, separated by carriage returns, and was imported as a single entry (302 of 367 lists in real solutions). A list that takes its values from a field nests the field as `<PrimaryField><FieldReference>` on a table occurrence, and was skipped as an empty custom list (228 lists). Both are now read, with the options of a field list (see Added). A list that uses another FileMaker file's list is reported as needing to be recreated.
- **Design exports with raw control characters migrate.** FileMaker copies characters such as ETX (U+0003) from stored text, for example a layout label, into its XML export without escaping them, and XML 1.0 forbids them, so the whole design was refused. They are now removed before parsing and the count is reported as a translation.
- **Layout and script menu separators no longer block a migration.** FileMaker writes each "-" separator of the layout and script menus as an item with `isSeparatorItem="True"` on the deleted-occurrence placeholder, and every one was refused as a layout on an unresolved table occurrence (104 in 47 real solutions). They are now skipped. A real layout whose occurrence was deleted in FileMaker shows no records there; it is now reported as not migrated instead of being refused.
- **Native containers are no longer attached to another record's file.** A container cell refers to its embedded object by the hexadecimal form of the object's path key (`12` is object 0x12, `805A` is object 218), but the reference was read as a decimal number first, then by two looser fallbacks. A cell whose key also spelled an existing decimal id received another record's file, and every object past 127 was missed. On four production files, 531 of 1,842 cells had been attached to the wrong file and 1,108 were refused; all now resolve to their own object. A reference that does not decode to an existing object is refused instead of guessed. A container that stores only a reference to a picture, whose text starts with its `size:` line, now follows that reference instead of reporting no payload.
- **A script's find requests survive the switch to Find mode.** `Enter Find Mode [Restore]`, and any step right after `Enter Find Mode`, wrote into the find requests before the window rebuilt in Find mode, and that rebuild cleared them, so the find that followed ran without them. The window now sets up the blank request when the script asks for Find mode and keeps what the script puts in it.
- **Externally stored containers no longer migrate as their storage descriptor (#139).** In a file created by FileMaker Pro 26.0.2, a container in open external storage keeps a `REMO` fork (its folder) and a `file` fork (size, MD5, name), and one in secure storage an `ENCR` fork: none of them is the file, but the first one was imported as its content and counted as reconstructed. Open storage is now read from `Table/<field>/<name>` through `external_files` or `migration-preflight -external-containers`, and accepted only when its size and MD5 match what FileMaker recorded; without the file it is reported missing. Secure storage, whose files only FileMaker can decrypt, is refused with the steps to migrate it (change the field to open or embedded storage and transfer its data in FileMaker). A fixture created with FileMaker, `server/internal/migration/testdata/containers`, covers embedded, open and secure storage.
- **Native containers with stale segments or an EPS original migrate (#142).** When a fork's segments run past its declared length, the consecutive segments that end exactly at that length are kept and the rest are ignored as leftovers of an earlier version; any other length mismatch is still refused. An `EPS ` fork is taken as the original over its `JPEG`/`PICT` previews (`application/postscript`, `.eps`). On a production file this took container reconstruction from 61 to 69 of 70 cells.
- **Comment steps from Save a Copy as XML are no longer reported as contradicting their instruction id.** FileMaker writes the comment step as `# (comment)` with id `89`, and every one was listed as a manual action ("Step name disagrees with its instruction id"), 48 in five real solutions. The id check now compares the canonical step, so only a real contradiction is reported.
### Changed

- **Calculations follow FileMaker's language and answers.** The engine (`server/internal/calc`) now reads FileMaker formulas — field names with spaces and accents, `Table::Field`, `${…}`, `$variables`, `Let`, `While`, comments, `¶`, `≠ ≤ ≥ ^ xor`, the file's decimal comma — and implements about 110 FileMaker functions across text, lists, numbers, dates, times, aggregates and conversion. Numbers are exact decimals (`0,1 + 0,2 = 0,3`), text reads as a number the permissive FileMaker way (`"Mary" * 2` is 0, no longer an error), comparison ignores case and accents, and results are written as FileMaker writes them (`,5`, `01:00:00`, `24/11/2026 13:45`). 552 formulas evaluated in FileMaker Pro 26.0.2 (`testdata/conformance.json`) pin the behaviour. A Number result is stored exactly instead of through a float64, and values read back from SQL keep their declared type.
- **Migrated calculations are read as FileMaker wrote them.** A calculation field is now bound once every table and table occurrence of the design is known: its formula is read with the source's own field and occurrence names (`Prix HT * 2`, `${Prix HT}`) and, when the migration names `source_locale`, with the file's decimal comma (`Amount * 1,5`), then stored with each field reference repointed at the imported column and numbers written with `.`. Everything else — `Get ( CurrentDate )`, `≠ ≤ ≥`, comments, spacing — is kept as written instead of being rewritten by pattern (`TODAY()`, `<>`). A formula that cannot be applied is still kept as a stored value of its result type, and the report now says why: it does not read (with a hint to name `source_locale` when it writes `1,5`), it reads a field the table does not have, a field deleted in FileMaker (`<Field Missing>`), or a related field no relationship reaches.
- **Unstored calculations, and calculations that read related fields.** A calculation saved with `"stored": false` (the new *Store the result* checkbox in Field Options) is computed each time a record is read — one record, a list or page, a find, portal rows, an export, the Data Viewer — and its column stays empty. Only such a calculation may read `Occurrence::Field`: the records are reached from its context occurrence (`"context"`) along the shortest chain of relationships, in the last relationship's sort order, so `Sum ( Lines::Amount )` changes when a line is added without the parent being written. Saving refuses a stored calculation that reads a related field ("make it unstored") or an unstored calculation, and an unstored one naming an occurrence, field or path that does not exist. Related records are read as the caller: a table they may not read gives `<No Access>`, never its values. Only the field read and the unstored fields it reads are computed, each once per request, and a ring of formulas across tables stops. An unstored calculation cannot be required or be a relationship's match field. Finding, sorting or summarizing by an unstored calculation is refused with `422 Unstored Calculation`. Migration keeps FileMaker's `storeCalculationResults` and context occurrence, binds related references to the imported occurrence and column, and makes unstored every calculation that reads a related field or an unstored calculation, as FileMaker does; such calculations were previously quarantined as stored values. A match field stays stored, or is quarantined when it reads related records. A solution import now checks formulas once every field and relationship exists, so a calculation declared before the field it reads no longer rolls the import back.
- **Relationships with `<`, `≤`, `>`, `≥` read the right records from the left side, and multi-keys match.** Portals (and related finds) compared the related record's match field the wrong way round when read from the left side of an inequality relationship, returning the opposite records. A text match field holding several lines now matches a record holding any of them, as FileMaker's multi-key does.

## [0.21.0] - 2026-11-24

### Added
- **Remaining FileMaker script steps and options execute in the client runner and debugger (#133).** All 62 FileMaker instruction IDs observed in real solutions (plus `Show Omitted Only`, `Allow User Abort`, `Unsort Records`, `Move/Resize Window`, `Cut`, `Clear`, `Omit Multiple Records`, `Constrain Found Set`, `Extend Found Set`, `Modify Last Find`, and `Save Records as Excel`) now convert as executable steps instead of disabled comments, and both XML DDR and native `<FMSaveAsXML>` decoders read their options (`LayoutDestination`, `Repetition`, `DisplayCalculation`, `Restore`, `Query`/`FindRequest`, `SortList`/`SortSpecification`, `PrintSettings`, `ImportOptions`/`ExportOptions`, `Show Custom Dialog` `Title`/`Message`/`Buttons`/`InputFields`, `Go to Record/Request/Page` `RowPageLocation`/`NoInteract`/`Exit`, `Commit Records` `NoInteract`/`Option`/`ESSForceCommit`, and `New`/`Select`/`Close Window` options) while ignoring presentation-only `StepText`, `DisableStepCollapsed`, and `Animation` metadata. On the client, `LayoutActionRunner`, `ScriptExecution`, `ScriptDebugSession`, and `DataBrowserWidgetState` (via `ExtendedLayoutActionHost`) now execute `Set Error Capture` with `Get(LastError)`, `Go to Record/Request/Page` and `Go to Portal Row` with `Exit after last` loop termination, dynamic `Go to Layout` (`original`, `by_name`, `by_number`), `Set Variable` repetitions (`$v[n]`/`$$v[n]`), `Set Field By Name`, `Insert Calculated Result`, `Replace Field Contents`, `Sort Records`, `Unsort Records`, `Omit Record`, `Show Omitted Only`, `Delete All Records`, `Go to Field`/`Next Field`/`Object`/`Related Record`, `Copy`/`Paste`/`Cut`/`Clear`, `Insert from URL`, `Export Field Contents`, `Insert Picture`/`File`, `Show Custom Dialog` buttons/inputs with `Get(LastMessageChoice)`, `Print`/`Print Setup`/`Save Records as PDF`/`Send Mail`/`Beep`/`Install Menu Set`/`Show/Hide Toolbars|Menubar|Text Ruler`, and a deterministic multi-window model (`New`/`Select`/`Close Window`, `Freeze`/`Refresh Window`, `Get(WindowName)`). `ScriptDebugSession._advance()` now syncs its step index to `_execution.pc` so debugger stepping follows multi-turn loops and early loop exits identically to normal execution.
- **Container field data migrates from native `.fmp12` files (#125).** The HBAM7 reader now reconstructs embedded container payloads from both Section 31 shared objects (`[tableID].[31].[5].[objID]`) and inline record forks (`[tableID].[5].[row].[fid]`), choosing the original file (`FILE`, `PDF `, `FORK`, or the extension-matched media fork) over FileMaker-generated previews/thumbnails (`JPEG`/`PNGf`/`PICT`), verifying exact declared fork byte lengths, and computing SHA-256 hashes. Original filename (from `FNAM`), MIME type, SHA-256 digest and byte size are kept in a new `sys_container_meta` system table, attached on Data API reads under the reserved `__container_meta` key, preserved across `.f4data` export/restore, cleared when the container field is overwritten, and removed when the record, field or table is deleted. Externally stored container references (`remote:`, `filemac:`, `filewin:`, `file:`, …) resolve against `external_files` (or `migration-preflight -external-containers` with path-traversal protection) or emit an explicit `rejected` report entry naming the missing file. Migration reports now count `container_cells_detected`, `container_files_reconstructed` and `container_files_missing`.
- **Permissive FileMaker values migrate with their original text (#124).** A migration request can name the source file's locale (`source_locale`, e.g. `fr-FR`; 16 locales, anything else is refused rather than guessed). With it, a Number holding `12-A`, `(4x2)` or `N/A`, a Date holding text that is no date, or a Time holding a duration past 24 hours is no longer refused: the field holds FileMaker's reading of the text (`12`, `-42`, empty where there is none) — every character but digits and the locale's decimal separator ignored, a leading minus or parenthesis negative, exact rather than a float, as observed on FileMaker Pro 26.0.2 fr-FR — and the original text is kept beside it in a new `sys_source_values` system table. Sorting, finds, summaries and calculations use the reading; the data API returns the text under a reserved `__source_text` key of the record (list, record, find, update, related records), exports write it, `.f4data` files carry and restore it, and the client shows it in form, list, table and layout views. Writing a field drops its original text; deleting the record, field or table deletes theirs; a client cannot set one. The report counts them (`counts.source_text_values`). Without `source_locale` migrations stay strict and the refusal says how to migrate the values. The migration dialog has a source-locale selector. Entering permissive values in File4Base, exact decimal arithmetic in calculations and durations remain phase 2 (`docs/specs/permissive_source_values.md`).
- **Native FileMaker migrations above 64 MiB can be applied (#126).** `POST /api/v1/solutions/migrate/preflight` and `/apply` also accept `multipart/form-data`: the JSON request as the `request` part, the native `.fmp12` copy as a raw `native` part and, optionally, the design as a `design` part. The copy is spooled to a private temporary file as it arrives and read back once into an exactly-sized buffer, instead of being held as base64 inside a JSON body capped at 64 MiB, and the conversion, report and `422` refusals are those of the JSON form. A signed-in admin or owner may upload up to `MIGRATION_MAX_UPLOAD_BYTES` (default `2GiB`); callers without a session (possible only with `ALLOW_PUBLIC_DATABASE_CREATION`) keep the 64 MiB ceiling, so public database creation never admits anonymous gigabyte uploads. `MIGRATION_UPLOAD_DIR` chooses where the upload is spooled. The client's migration dialog sends an attached `.fmp12` this way. A JSON request above 64 MiB is now `413 Migration Request Too Large` pointing at the multipart form, where it used to be cut silently and reported as invalid JSON.
- **Migration requests have their own time limit.** Every request was cancelled after 60 seconds, which a large upload and import cannot meet; the two migration routes now run for up to `MIGRATION_REQUEST_TIMEOUT` (default `30m`) and every other route keeps 60 seconds. The Web Client's Nginx streams `/api/v1/solutions/migrate/` to the API without buffering, with a 2 GB body limit and a 30-minute timeout to match.

### Fixed
- **NUL characters in native Text values no longer block a permissive migration (#135).** With a `source_locale`, a Text value holding NUL characters (which PostgreSQL text cannot store) is migrated with them removed and counted in a `translated` report item, instead of refusing the whole dataset. Without a locale it stays refused. `migration-preflight` gains `-source-locale` and prints `source_text_values`.
- **Native `.fmp12` migrations verify the binary's internal catalog against the attached design before reading records (#130).** FileMaker starts every file's base tables at id `129` and fields at `1`, so pairing solution A's design with solution B's `.fmp12` previously imported B's records under A's table and field names. Before decoding any row, the HBAM7 reader now extracts the binary's own base table catalog (`[3].[16].[5].[tableID]`) and field catalog (`[tableID].[3].[5].[fieldID]`, names plus field class/data type) and compares every table ID/name and field ID/name/type in both directions. Missing, extra, renamed or retyped tables and fields — or a catalog-less binary — are refused with a clear `rejected` report listing each difference (and the attached vs. declared file names when they differ), `records_ready: 0`, exit code `2` from `migration-preflight`, and `422 Native Data Import Blocked` from `/apply` before any target database change.
- **Script steps are identified by their FileMaker instruction id.** A localized design report names each step in the client's language, and File4Base's French labels were written by hand: FileMaker Pro 26.0.2 actually writes `Ouvrir boîte dial. person.`, `Afficher enreg/requête/page`, `Afficher tous les enreg.`, `Importer enregistrements`… so Show Custom Dialog, Go to Record, Show All Records, Import/Export Records and others were quarantined as unsupported although the runner executes them. Each step is now resolved by its id through a table of 62 id/English-name pairs observed in FileMaker's own output (`step_ids.go`), and the French labels FileMaker actually writes are recognized when no id is present. An id that contradicts an English name is reported and the name wins; an id outside the table falls back to the name. Steps the runner cannot execute are reported with their English name.
- **Authentic XML DDR imports no longer invent or rebind objects.** Object definitions are read from their owning catalogs and nested groups, so privilege, trigger and layout references no longer create phantom tables, scripts, layouts or value lists (#88). Native graph joins resolve source table/field IDs, and external or missing occurrences can no longer be substituted with same-named local tables or bound to unrelated layouts. Record exports use the complete field-name map, preserving normalized name collisions, source `id` values and whitespace-only text. Quarantined instructions keep their instruction ID and enabled state.
- **French HTML DDR keeps its script operands (#100).** Operands are extracted after the instruction name is canonicalized; native French Go to Layout, Exit/Halt, Exit Loop If and comment labels are recognized.
- **Claris 26 Save a Copy as XML designs convert (#116 follow-up).** `Structure/AddAction`, field `ObjectList` catalogs and referenced script bodies are decoded, folder/end markers are excluded, nested `Calculated` fields remain calculations, typed native operands are read, binary calculations are no longer reported as text, and auto-enter constants populate new records.
- **The native `.fmp12` record reader finds the records.** HBAM7 records follow the live leaf chain and length-delimited paths instead of guessed byte markers; whole record tokens stay distinct instead of merging, and SCSU Unicode, whitespace and sparse repetitions are retained. Native date order is explicit (`native_date_order`). `records_detected` and `records_ready` are reported separately, and an attached native or XML dataset that cannot be read or represented refuses apply with `422` before any target write instead of succeeding schema-only. Permissive Number text is no longer guessed into a rounded fraction. The new local `migration-preflight` command audits copies larger than the HTTP request limit.
- UUID-shaped source Text values keep their case and surrounding whitespace. Migration capability reports recognize client-supported conditionals and Set Variable and flag Sort/Omit steps the client cannot execute.
- **Script parameters and results flow through Perform Script.** Arguments and explicit return values pass through local, peer and self-reference calls; each call keeps its own parameter and local variables, and an undefined local never falls back to a global. Normal execution and the debugger share the same result handling, including retaining a prior subscript result when no explicit value is returned.
- **Script `$$` variables belong to the authenticated solution session.** They persist across repeated peer calls and clear on sign-out, session expiry, replacement, server change or client closure; same-named variables in another file or session no longer overwrite one another. Imported Set Field/dialog calculations evaluate supported expressions and stop before acting on unsupported ones.
- **Two runs no longer race to publish the same GitHub Release.** Cutting a release pushes the version bump to `main` and then the `vX.Y.Z` tag, and each of those starts a run of `desktop_release.yml` whose `publish-release` job publishes the release for the same `VERSION`. On v0.20.0 both ran at once and landed on the same release id: one answered `HTTP 403` and the other `HTTP 404` uploading assets to it, because `gh release create` deletes the release it created when an upload fails. Every build job was green and **no release existed at all**. The job now takes a repository-wide concurrency group, queued rather than cancelled, so the second run finds the release already published and re-uploads with `--clobber` as the step was always written to do.

## [0.20.0] - 2026-11-16

### Added
- **The migration reads a `<FMSaveAsXML>` *Save a Copy as XML* export (#87).** FileMaker Pro 20 / Claris 26 write that root, and it was refused by name: the format advertised as supported was the older `<FMPSaveAsXML>`. Its grammar is not the Database Design Report's — a base table's fields live in `FieldsForTables/FieldCatalog` and point back at the table instead of being nested in it, script steps live under `StepsForScripts`, and attribute names are lower-cased — so renaming the root was never a conversion path; it produced tables with no fields and no relationships. The document is now **adapted** before conversion: each field catalog is moved under the base table it names and each step list under the script it names, and `firstAttr` falls back to a case-insensitive attribute match so `datatype` and `fieldtype` resolve. A catalog naming a table the document does not declare is reported rather than dropped, and one naming no table is read as the only declared table's when there is exactly one and reported as unplaced when there are more — a field on the wrong table being worse than a field reported as unplaced. A document exported without *Include details for analysis tools* (`Has_DDR_INFO="False"`) is named as such in the report, and the refusal of a document whose tables arrive with no fields now says which export option to use.
  - **Claris publishes no grammar for this format**, and its own help calls the XML a preview that "may change significantly in future versions" and is "not intended for use in production systems". The reader is written against the structure an authentic export was reported to have, and `docs/specs/filemaker_compatibility_scope.md` marks the path **provisional** until an authentic export is committed as a fixture. The empty-conversion refusal is what keeps a grammar mismatch failing loudly instead of importing empty tables.
- **The acceptance fixture of #112 is executable.** `server/internal/api/testdata/acceptance/dispatch_results` holds two linked FileMaker files — Dispatch and Results, parent surveys and child answers — with their `FMPXMLRESULT` record exports and an expected-results manifest, and `server/internal/api/acceptance_fixture_test.go` imports the whole application through the documented HTTP path (preflight, then apply, with nothing but the credential binding), reads the result back over the API as a client would, and compares it against the manifest. It collects every difference as a machine-readable finding instead of failing on the first one, so one run says how far the application actually migrated, and it creates no table, field, relationship, script or account of its own: if the import did not produce it, the finding says it is missing. The fixture covers definition identity (including an occurrence alias, a self-join and a multi-predicate join), data fidelity by business key with exact decimals, Unicode and untrimmed whitespace, field behaviour, access and activation, the read-only preflight, and the cross-file transfer with a replay and an injected rejection. The two gates it cannot meet are recorded as unmet on every run rather than skipped in silence: **authentic input**, because these inputs were written by hand and `expected.json` declares that provenance, and **workflow execution**, because File4Base runs script steps in the Flutter client. `docs/specs/filemaker_compatibility_scope.md` § 3 says what it found and what is still missing.

- **The declared FileMaker compatibility scope (#112).** `docs/specs/filemaker_compatibility_scope.md` states the one supported input path — Database Design Report (`<FMPReport>` XML or HTML) or `<FMPSaveAsXML>` from FileMaker Pro 12–19, records as `FMPXMLRESULT` or `fmp12_base64`, in English or French, UTF-8 or UTF-16 — together with the subset the first acceptance fixture covers, the declared capability boundaries, and the two artefacts that cannot be produced without an actual FileMaker installation: the working two-file solution with its authentic exports, and the source-generated expected-results manifest.

- **Scripted record transfers between solutions (#96), first milestone.** Sending records from one solution to another and getting results back had no scripted form at all: the client runner stopped on `Import Records`, `Export Records`, `Loop` and `End Loop`, and a manual CSV round trip was the only way to move a keyed found set. A **transfer definition** is now a design object of its own — stored in `sys_transfers`, exported in the solution bundle, listed in the Database Design Report — recording the source solution and table (optionally narrowed to a found set), the destination solution and table, explicit field mappings, stable match keys, the add/update mode, the unmatched-record policy, the rejected-row policy and the date interpretation. It is managed and run over `/api/v1/transfers` (`docs/api/API_REFERENCE.md` § 17).
  - **A definition never holds a credential.** The session the request is made with authorizes its own solution, and the far end is authorized by `peer_token`, a session token obtained by signing in to *that* solution; a token for any third solution is refused whatever role it holds there, the peer account must still be in the state its session was opened with, and the source needs `bulk_export` with read access while the destination needs `bulk_import` with read-write access. The invariant that a session reaches exactly the database it signed in to is unchanged.
  - **One destination table, one transaction, accurate counts.** A run reports `rows`, `added`, `updated`, `skipped` and every rejected row with its position, both fields, the value and the reason. `on_rejected: "abort"` (the default) refuses the whole transfer and leaves the destination exactly as it was — the all-or-nothing contract a manual import already follows (#38) — while `on_rejected: "skip"` quarantines the rejected rows and commits the rest. A mapping or table that cannot be resolved in either solution blocks the run before a single record moves. Refusals answer `422` with the same report shape and `committed: false`, so a caller parses one object either way.
  - **Replaying is the retry.** With `update_matching` and stable match keys, running a transfer again updates the same records instead of duplicating them, so parents then children neither duplicate a parent nor orphan a child; `add` is deliberately not replayable and the parity matrix says so. The transfer reuses the export and import every manual one runs (`data.ImportRecords`), so a keyed update evaluates its calculations over the stored record merged with the mapped values (#92), and exact decimals, accented text and containers cross unchanged.
  - **The script steps run.** `Import Records` and `Export Records` name a transfer and are wired into the shared execution state machine (`ScriptExecution`), so the Script Debugger steps them exactly as a run performs them. The counts land in `$$transferAdded`, `$$transferUpdated`, `$$transferSkipped`, `$$transferRejected` and `$$transferCommitted` for the steps after them to test. A step that names no transfer, names one this solution does not have, is pointed the wrong way, or reaches a solution this session is not signed in to **stops the script and says which** — and is never answered with a *script* of the same name, the fallback the issue asks us not to have.
  - **`Loop` / `Exit Loop If` / `End Loop` execute**, with the same frame discipline as `If`: a nested `Exit Loop If` leaves only its own loop, branches opened inside a turn are closed on the next, a loop inside a branch that was not taken is passed over whole, an unbalanced `End Loop` stops the script, and a loop with no way out is stopped and says so rather than hanging the application.
  - **Execution is client-triggered and server-performed**, available to any API caller holding a session for each end — and, since #108 below, on a schedule run by the server. Scripts themselves still run only in a client.
- **Perform Script into another solution (#107).** A `Perform Script` step can name a **file reference**, a new design object (`sys_file_references`, `/api/v1/file-references`, exported in the solution bundle and listed in the Database Design Report) that maps a name to another solution explicitly. The step then performs the script of *that* solution, on a session the user opened there and with no window of its own: steps that need no window — transfers, variables, `If` / `Loop`, `Perform Script` within that solution, `Exit` / `Halt Script`, dialogs — run, and the first step that acts on a window stops the script and names itself, because the window on screen belongs to the calling solution. A transfer in the called script that reaches back to the caller uses the session the user is working in there, and `Halt Script` over there stops every caller here. An unknown or unmapped reference, or one to a solution the session is not signed in to, stops the script and says which. The assisted migration turns every external file a converted `Perform Script` step calls into an unmapped reference and reports it under `manual_action_required`; a restored bundle never repoints a reference the destination has already mapped.
- **Scheduled transfers (#108).** A transfer definition can now run unattended on a UTC cron schedule, run by the File4Base server itself (`/api/v1/transfer-schedules`, `sys_transfer_schedules`, with the 50 most recent runs of each kept in `sys_transfer_schedule_runs`).
  - **No stored credential.** A schedule names an account in each solution to run as and keeps no password or token. Creating one proves everything now — the creator administers this solution, an owner or administrator of the other solution vouches for the account named there with a `peer_token`, and both accounts already hold their privilege and table access — and **every run proves it again**: an account disabled, deleted, demoted or stripped of `bulk_export` / `bulk_import` or of its table refuses the run, recorded as `refused`, with nothing read or written.
  - **Authorization is pinned to the solutions it was given for.** A schedule keeps the two solutions its transfer resolved to; if the definition is changed to point at another solution, the schedule refuses to run rather than carry its authorization over.
  - **Each occurrence runs once.** Due schedules are claimed in the catalog before they run, so several server processes over the same databases never run the same occurrence twice, and a schedule still running is not started again. `FILE4BASE_TRANSFER_SCHEDULER=false` keeps a process from running any. Runs are recorded as `committed`, `refused` or `failed`, with the transfer's report; `POST …/{id}/run` runs one at once, exactly as the scheduler would.
  - Cron expressions are read by a small parser of the classic five-field dialect (`internal/cronexpr`) rather than a new dependency; anything it does not read is refused when the schedule is saved.
  - A schedule runs a transfer definition, **never a script**: scripts still run only in a client.

### Changed
- **The File4Base Web Client is compiled to WebAssembly.** The browser bundle was built with `flutter build web --release`, which compiles Dart to JavaScript; the only `.wasm` files it served were CanvasKit's and Skwasm's, the renderer's own modules, so the application code itself ran as 5.1 MB of JavaScript. The image now builds with `flutter build web --wasm --release`: Dart is compiled to WasmGC (`main.dart.wasm`, 4.6 MB, loaded through `main.dart.mjs`), and Flutter emits the JavaScript bundle alongside it so the bootstrap falls back for a browser without WasmGC — no browser loses the client. Verified against the built image: a Chromium-based browser fetches `main.dart.mjs` and `main.dart.wasm` and never requests `main.dart.js`, the client renders and reaches the API. The image grows from 155 MB to 161 MB because both bundles ship, while the browser downloads slightly less than before.
  - **Nginx had to be told about `.mjs`.** Its `mime.types` maps `.js` and `.wasm` but not `.mjs`, so the ES module that loads the WebAssembly would have been sent as `application/octet-stream` — and with `X-Content-Type-Options: nosniff`, which this client sets, the browser refuses to execute it and the application never starts. The location that revalidates the bundle's code now covers `.js`, `.mjs` and `.wasm` together, naming the type of `.mjs` explicitly: the three are replaced as a set on every deploy, and one of them served from cache beside a fresh one is a broken client.
  - The page is not cross-origin isolated, so the renderer loads single-threaded `skwasm`. Enabling COOP/COEP would unlock the multi-threaded variant and is not part of this change.

- **The integration tests no longer collide on their own table names.** They suffixed each table with `time.Now().UnixNano() % 1000000`; on a host whose clock has microsecond resolution — macOS among others — every nanosecond reading is a whole multiple of 1000, leaving that expression only a thousand possible values, and nothing drops the tables afterwards. A developer's scratch database filled up until the suite failed on `table already exists` for reasons unrelated to the code under test. All fourteen test files now take their suffix from `uniqueTestStamp()`, the full nanosecond reading with random bits mixed in.

### Fixed
- **A migrated `Set Variable` keeps its variable, sigil and all (#112).** The step's operand was sanitized as if it were a field name, so `$$Batch` became `batch` — and the `$$` is the whole of the global-versus-local distinction, while the client runner reads a variable's name from a different parameter altogether. A migrated step therefore wrote a local variable nobody read, or stopped the script outright, while every later `$$Batch` in the same script read an empty global. A `$`-prefixed operand is now carried through verbatim as the variable it is; a real field operand is still pointed at the imported column and keeps the source spelling as its label.
- **A migrated calculation field holds its value from the start (#112).** FileMaker evaluates a calculation and writes no column for it in a record export, and the bulk record import stores the values it is given rather than computing any, so every calculation a migration reported as preserved was empty until someone edited the record — including the references #111 had just repointed. The formulas are now computed over the records the migration just imported, the same way a changed formula is applied to the rows already stored, and a formula that cannot be computed is named under `manual_action_required` instead of being swallowed.
- **An import no longer invents table occurrences (#112).** A table the import creates comes with a default occurrence named after the table, which a FileMaker file never declared: a file with the occurrences `Surveys` and `SurveysByTitle` over one base table arrived with four boxes in the relationship graph instead of two, so comparing the migrated graph against the source's failed on objects the source never had. Where a solution file declares a table's occurrences, the default one is now removed; a file that carries no occurrences at all keeps the defaults it relies on.
- **A migrated calculation reads the fields as the import named them (#111).** `sanitizeIdentifier` strips a leading underscore and two names that normalize alike are told apart as `code` and `code_2` (#89), but a calculation's formula was stored exactly as the source wrote it. A design with a text field `_code` and a calculation `Mirror = _code` therefore passed preflight with two preserved fields and nothing unsupported, and the apply then failed with *the formula of "mirror" reads "_code", which this table does not have* — a conversion reported as faithful that could not be applied. Every calculation of a table is now resolved against that table's complete source-to-target field map once all of its fields are known: references are repointed at the imported names and the change is reported as translated, while quoted text and function names are left exactly as they were. Because the parser accepts any identifier, a successful parse no longer passes for proof that the fields exist — a formula naming something the table does not have is **quarantined** as a stored column of the type the calculation would have produced, and the report names the reference it could not place. Names that differ only in case are resolved by their exact spelling first, so a `Code`/`code` pair stays unambiguous.

- **A Perform Script that calls another file never runs a same-named script of this one (#107).** The XML route of the migration kept a step's external file as `file_reference`, but the client runner ignored it and looked the script up by name in the *current* solution — so a migrated step calling "Receive" in another file ran this solution's "Receive" if one existed, exactly the fallback #96 rules out. The `#96` migration spec also described this wrongly, saying such a step was "reported rather than matched to a same-named local script"; it now says what the runner does.
- **Server Tools no longer reports scheduled jobs that never ran (#108).** The Server Tools schedules are kept in memory and executed by nothing, yet the panel showed invented last-run times with `success`, and `Run Now` stamped a fresh `success` without running anything — so a nightly backup could look healthy for months without one ever being taken. Every job now reports `executed: false` and `last_status: "not executed"` with no last run, `Run Now` answers `409` saying nothing was run (and the audit log records the request as a warning), and the docs that described these jobs as an executing scheduler say what they are.
- **An imported container value is the file that went in.** `ImportRecords` stored the base64 *text* of a container cell instead of the bytes it encodes, so reading the record back encoded it a second time and an imported image or document came out doubly encoded and no longer itself. The import now decodes the cell, as every other write path already did; the manual CSV/XLSX import of a container column is fixed by the same change. Found while proving that a container crosses a transfer unchanged (#96).

## [0.19.4] - 2026-11-10

### Fixed
- **"Check for Updates..." opens its dialog.** The Help menu item read the installed version from the platform package plugin and opened the dialog afterwards, which left it opening nothing on two counts: the item that asked for it is a menu entry, so the tap closes the menu and unmounts the context the dialog was asked of, and a platform that answers the package query slowly — or never — held the dialog back indefinitely. The dialog now opens immediately with the version the app was built with, which the release script keeps in step with the root `VERSION` file and the About dialog already shows.
- **A report asks the server for its figures once.** Switching to the Report view re-reads the records, because the first read may have been a single page while a report totals the whole found set, and reading records asks for the report figures — so every report issued its one-request-per-break-level summary twice. The figures depend on the table, the summary fields, the break levels and the found set rather than on the rows, so a load that would ask the identical question is now skipped; a failed load is not remembered as asked, so Retry still reaches the server.
- **A failed migration into an existing database leaves nothing behind (#97).** Apply imports the converted design first and the records second; on a data failure only a *newly created* target database was dropped, so merging into the active database committed the imported tables, layouts, scripts and accounts although the migration contract promises full rollback — and a retry then built on that half-applied design. A merge now records the current design before the first write (`schema.Service.SnapshotDesign`) and, on any later failure, deletes every object the migration added and re-imports the recorded design (`RestoreDesign`), so a definition the merge changed is the one it was. A design that cannot be recorded stops the migration before it starts, and a restore that is itself incomplete is said out loud in the problem report instead of being passed off as a clean rollback.
- **A French Database Design Report converts like an English one (#100, #103).** The HTML reader matched the English labels `Table Name:`, `Script Name` and `Script Steps`, and both readers classified script steps by their English names, so a real French report of a working solution passed preflight with **zero tables, fields, occurrences, relationships and scripts** — leaving layouts bound to occurrences that were never imported — and every French step name arrived as a disabled comment. The readers now key off what every language shares (the report's anchors, the shape of its tables, the XML element names) and translate only the words that decide something, from a table of names with one possible meaning (`server/internal/migration/locales.go`). Supported report languages are **English and French**, stated in `migration.SupportedDDRLocales` and in the spec. A step name no supported locale carries stays quarantined with its own name, its instruction id and a report entry; an unrecognized field type is reported rather than quietly becoming TEXT; and an HTML report that converts to no table, or to tables with no field, is **refused** naming the languages that are read.
- **A UTF-16 design report is read as the document it is (#99).** FileMaker Pro on Windows writes its XML Database Design Report as UTF-16 with a byte order mark; the migration dialog decoded every picked file with `utf8.decode(..., allowMalformed: true)`, which turned a perfectly good export into replacement characters with the file's NUL bytes between them — rejected by the server as invalid FileMaker XML, and large enough to overrun the request limit. Design files and FMPXMLRESULT attachments now go through one decoder (`core/utils/xml_encoding.dart`) that honors the byte order mark (UTF-8, UTF-16LE, UTF-16BE), recognizes markless UTF-16 by its NUL bytes, falls back to the XML declaration's `encoding` for Latin-1/Windows-1252, keeps characters outside the basic plane, and **refuses** UTF-32 or undecodable bytes with a message saying to re-save as UTF-8 or UTF-16 rather than submitting mangled text.
- **Native DDR table occurrences and relationships are read (#101).** A real `<FMPReport>` writes its graph as `RelationshipGraph/TableList/Table` and `RelationshipList/Relationship`, with the two sides as `<LeftTable>` / `<RightTable>` elements and the join in a `<JoinPredicateList>` whose predicates are not direct children. The converter looked only for `TableOccurrence` nodes and a direct `JoinPredicate`, so every such relationship was rejected as unresolvable and the source aliases were replaced by invented default occurrences. The graph's `<Table>` elements are now the solution's occurrences, aliases intact, no default occurrence is invented for a base table the graph already places, per-side `cascadeCreate` / `cascadeDelete` are honored, and a multi-predicate join migrates its first predicate and reports the rest instead of dropping them in silence.
- **A field's nested Storage, Validation and AutoEnter options are read (#102).** The converter checked `global` and `maxRepetition` on the `<Field>` element, while a native DDR keeps them under `<Storage>`, and never read `<Validation>` or `<AutoEnter>` at all — so a not-empty, unique field arrived accepting blanks and duplicates, an auto-enter constant vanished, and a global or repeating field was reported as preserved with no warning. Not-empty (which also makes the column non-nullable), unique, member-of-existing, maximum length, range and strict data type now become File4Base validation rules; an auto-enter constant becomes the field's default value; and validation by formula or value list, auto-entered serials, lookups, creation and modification stamps are each reported with their source field and what to do instead.
- **No HTML DDR script step is deleted any more (#104).** The HTML reader had no branch for an unsupported step, so `Send Mail` in the middle of a script simply disappeared and the script was reported as preserved with nothing unsupported — leaving a script that looked complete and enabled. Unsupported steps now become disabled `Comment` steps keeping the original DDR line and the step name, exactly as the XML route has always done, and each is reported under `unsupported` with a `manual_action_required` entry naming the script, the step and its position.
- **Numeric script conditions pick the branch their arithmetic picks, and a numeric Set Variable value is no longer lost (#93).** The client runner resolved anything it did not recognize — a numeric literal included — as a field name, and a missing field as an empty string, so `If [1 = 0]` compared two empty strings and ran its branch, `If [2 > 1]` ran nothing because ordering comparisons were never evaluated, and `Set Variable [$count; 2]` stored nothing. The runner now reads numeric literals as numbers, compares `=`, `<>` (`≠`, `!=`), `<`, `<=` (`≤`), `>`, `>=` (`≥`) numerically when both sides are numbers and as text otherwise, reads `<=` as one operator rather than as `<` or `=`, and treats a bare value as true when it is a non-zero number or non-empty text. An expression it cannot evaluate — arithmetic, a function call, a field the context does not have — **stops the script and names itself** instead of being guessed as empty.
- **The debugger steps the script the runner runs (#94).** `ScriptDebugSession` ran each step in isolation, so a conditional script that ran perfectly well stopped the debugger at its first `If` ("not supported on a button"), and no step could see the variables another had set. Normal execution and the debugger now advance one shared state machine (`ScriptExecution`: program counter, branch frames, local variables, halt state), so Step and Continue have the same effects, in the same order, as running the script; a branch not taken is marked skipped, and `localVariables` shows what the run holds.
- **Halt Script in a called script stops its callers (#95).** `Halt Script` and `Exit Script` were both a local `break` returning ordinary success, so a parent carried on writing records after a child halted. A halt is now its own outcome (`LayoutActionResult.halted`) that travels up through every caller and the debugger — no later step of any ancestor runs, at any nesting depth — while `Exit Script` keeps returning to its caller.
- **The authentic Claris 26 "Save a Copy as XML" document is refused by name instead of half-read (#87).** `FMDeveloperTool 20+` writes `<FMSaveAsXML>`, whose grammar keeps fields in `FieldsForTables/FieldCatalog` and script steps under `StepsForScripts`; the converter only recognized `<FMPSaveAsXML>`. The refusal now names the root element and the formats that do work (a DDR from FileMaker Pro 12–19 as XML or HTML, `<FMPSaveAsXML>`, File4Base design XML), and a design whose base tables arrive without a single field is refused on the same grounds rather than reported as a successful conversion with empty tables.
- **XML script steps keep their operands, and a step's references are no longer imported as definitions (#88).** FileMaker writes a step's field target, formula, layout destination, called script, external file and parameter as child elements, not attributes, so every imported parameter map was empty while the report called the script preserved. Each `<Step>` is now decoded into the parameters the Script Workspace and the client runner read (`field`, `value`, `layout_name`, `script_name`, `script_parameter`, `condition`, `name`, `url`, `message`), its whole child structure is kept under `filemaker_parameters`, and any operand File4Base does not run is listed under `manual_action_required` by element name. The `<Script>` and `<Layout>` elements nested in a step are references: they no longer produce an empty script and a layout the source never declared.
- **Normalized name collisions keep both definitions (#89).** `Visit Code` and `Visit-Code` both normalize to `visit_code`, and the second was silently dropped; a source field named `id` disappeared behind the generated primary key. Each collision now takes the next free `name_2`, `name_3` … and is reported under `translated`, with a `manual_action_required` entry for a replaced `id`, so no source table or field definition disappears without a line in the report.
- **A record export is converted exactly (#90).** The migration trimmed every cell, dropped records whose exported values were all blank, and coerced numbers through `float64`, which rounded `9007199254740993` to its even neighbour. Conversion now runs through `data.CoerceImportValue`, the conversion every import uses, so a number keeps its digits as the exact decimal it was written as and text keeps its spaces; a blank record is still a record; FileMaker's `MM/DD/YYYY` dates and timestamps are read; and a value that will not convert is listed under `rejected` with its row and column instead of failing the whole apply at the database.
- **An attached `.fmp12` is no longer ignored on the XML design route (#91).** `fmp12_base64` was read only on the HTML path, so an XML DDR plus a binary attachment — valid or `%%%` — reported a successful migration with zero records. Both routes now go through the same reader: invalid base64 fails the request before any destination database is created, the numeric table and field ids of an `<FMPReport>` DDR are used to address the binary's records, and an attachment that cannot be addressed or yields nothing is reported under `rejected` and `manual_action_required`.
- **Imported script steps are numbered from 1, uniquely (#98).** The XML converter assigned zero-based `sequence_idx` and the bundle importer repaired only non-positive values, so a three-step script was stored as `[1, 1, 2]` and the first two steps collided — leaving no total order for the workspace to sort by or an export to round-trip. The converter is 1-based and the importer now renumbers a repeated position as well as a missing one.
- **A keyed import no longer recomputes a calculation field from the file alone (#92).** `update_matching` looked up the matching record *after* evaluating the formulas, so a row carrying only its match key and one column computed every calculation over that partial map: `total = quantity * rate` became `0` when the file sent `quantity` and the stored `rate` was never read. The match is now found first, the stored row is read inside the import transaction, and the formulas are evaluated over that row merged with the mapped values — the same rule a partial `PUT` follows. Adding a row is unchanged: its formulas are computed over the new record alone.

## [0.19.3] - 2026-11-08

### Fixed
- Normalize complete day/month/year and ISO date input before saving, avoiding failed requests for incomplete date typing.
- Load every row when opening the record list, including all rows in an active found set.
- Read the installed version and build number in the Check for Updates dialog instead of using stale hardcoded text.

### Added
- **In-app updater for the installed desktop client** (`server/internal/api/desktop_update_handler.go`, `client/lib/core/services/desktop_update_service.dart`, `client/lib/features/server_tools/client_update_dialog.dart`, `client/lib/main.dart`):
  - **`GET /api/v1/updates/desktop?platform=macos|windows|linux&client_version=…`** reports the newest published desktop client, its release notes, and the asset to download with the `sha256` to verify it against. It needs **no session** (a build too old to sign in must still find its own update) and mirrors only the public GitHub release; release metadata and the asset list are each cached for ten minutes, so a fleet of polling clients costs one upstream request. Per platform the native installer is preferred and the plain bundle is the fallback (`.dmg` → `.zip`, `Setup.exe` → `.zip`, `.deb` → `.tar.gz`).
  - **Download, verification and installer handoff inside the app**: the client streams the asset to a private temporary directory, hashes it as it arrives, and only then opens it with the platform's own installer (`open` for the `.dmg`, the Setup wizard, `xdg-open` for the `.deb`). A checksum mismatch, an interrupted download or a release-server error deletes the partial file, so nothing unverified is ever left for someone to run. An update with no published digest is **not** offered as an in-app download at all: the user is sent to the release page instead.
  - **This is a second, independent update axis.** `docker compose pull` replaces `file4base-api` and `file4base-web` but never the natively compiled binary a user installed, and the existing `Server Tools` container flow is administrator-only and ends in a shell command. The new `App Update Available` pill and `ClientUpdateDialog` never mention a terminal, so a user who only ever sees a program can stay current.
  - **`client_update_policy` in File Options** (`Tools > Developer Utilities...`): `prompt` (default, any signed-in account), `admin-only`, or `off` for fleets rolled out by device management. **Kiosk Mode overrides all three** — a session in Kiosk lockout is never offered an update, since it has no menu bar, status badges or developer shortcuts with which to authorize one.
  - **`SHA256SUMS.txt` is now published with every release** (`.github/workflows/desktop_release.yml`), which is what the in-app verification checks against.
- **HTTPS out of the box with a persisted local certificate authority (`server/internal/localca`):** the API creates a long-lived **File4Base Local CA** and a leaf certificate it signs under `/app/data/tls` (the `file4base-tls` volume, `FILE4BASE_TLS_DIR`), covering `localhost`, `127.0.0.1`, `::1`, the stack's container names, and the domain and addresses configured through `FILE4BASE_TLS_DOMAIN` / `FILE4BASE_TLS_SANS` or Server Tools. The material is generated once and reused on every later start, so trust an administrator installed keeps working; private keys are written with mode `0600`.
- **`GET /api/v1/server/tls/ca.crt` and `GET /api/v1/server/tls/bundle.pem`** (server administrators only): download the authority's **public** certificate to install once per machine, or the served leaf plus the authority for another reverse proxy. No private key is ever served.
- **Server Tools > Domain & SSL/TLS: "File4Base Local CA — install once per machine" card** with both downloads and per-platform installation steps (macOS Keychain, Windows `certlm.msc`, Linux `update-ca-certificates`, and Firefox's own trust store).
- **HTTPS listener on the Web Client (`file4base-web`, port `3443` → `443`):** Nginx serves the leaf certificate from the shared `file4base-tls` volume, mounted read-only. The listener is installed at container start only when the certificate exists (`client/docker-entrypoint.d/25-file4base-tls.sh`), so a first start on an empty stack still serves HTTP instead of failing; `FILE4BASE_FORCE_HTTPS=true` redirects HTTP to HTTPS.
- **The HTML DDR reader is now tested against a design report of a real FileMaker solution** (`server/internal/migration/testdata/recipes_html_ddr.html`, `TestConvertHTMLDDR_RecipeDatabase`): 3 base tables, 24 fields, 7 layouts and 4 table occurrences over those 3 tables, because the solution reaches one table twice — the second time through a sorted self-join, whose relationship the test pins so that collapsing an alias onto its base table cannot pass unnoticed. The fixture also covers the account policy end of an import: a built-in `[Guest]` account is rejected and `Admin` arrives needing a password. The sample is Peter Hebert's public domain (Unlicense) `FileMaker-recipe-database`; its provenance is recorded in `server/internal/migration/testdata/README.md` and the ledger entry in `docs/PROVENANCE.md` was updated, since that sample was previously cited by URL and not stored. The binary `recipes.fmp12` is still not vendored, so `fmp12_records.go` remains covered only by synthesised HBAM pages.

### Changed
- **The macOS `.dmg` now installs by dragging** (`.github/workflows/desktop_release.yml`, `scripts/build_desktop.sh`): the disk image is built from a staging folder holding `File4Base.app` beside a symlink to `/Applications`, so the mounted volume shows both icons and the user drags one onto the other. Previously the image carried the bare `.app`, leaving a single icon and no indication of what to do with it.
- The shared solution-bundle fixtures (`server/internal/schema/testdata/client_new_database_v2.f4p`, `server_solution_v2.f4p`) were regenerated for the new `client_update_policy` File Options key; the server stores File Options as given, so nothing was lost.
- **The TLS panel now describes the certificate the server really serves.** `GET /api/v1/server/tls` re-reads the material from disk and reports `cert_source`, `ca_available`, `ca_subject`, `ca_not_after` and `ca_sha256`; `POST /api/v1/server/tls/renew` reissues only the leaf and leaves the authority (and therefore installed trust) untouched, and returns `409` for an uploaded custom certificate; `PUT /api/v1/server/tls` reissues the leaf for the new `domain` plus `extra_sans` under the same authority.
- **The Web Client's security headers reach every response.** They moved to `client/nginx/security-headers.conf`, included both in the server blocks and in the cache-control locations, because Nginx drops inherited `add_header` directives in a context that sets its own; `Strict-Transport-Security` is announced over HTTPS only.
- **The API's HTTPS listener serves the same persisted certificate.** With `TLS_ENABLED=true` and no explicit `TLS_CERT_FILE` / `TLS_KEY_FILE`, `main.go` now serves the leaf signed by the local authority instead of an unrelated in-memory certificate; explicit certificate files still take precedence.

### Fixed
- Each process start no longer generated a throw-away certificate that never reached any listener: the Server Tools panel, the HTTPS listener and the exported certificate are now one and the same material.
- `docs/specs/server_tools_and_mfa.md` no longer documents the `POST /api/v1/server/tls/acme/issue` and `POST /api/v1/server/tls/custom` endpoints, which have never existed in the router.
- **Assisted FileMaker Migration rejected valid record exports as binary databases** (`server/internal/migration/filemaker.go`): the signature scan searched the payload for the text `FileMaker Pro`, and every `FMPXMLRESULT` export carries a `<PRODUCT>` element naming the product it came from — spelled `NAME="FileMaker Pro"` by FileMaker 7 through 12, and split as `NAME="FileMaker" VERSION="Pro 13.0v1"` only from 13 onwards. Exports from the older versions therefore failed with `422 Unsupported Binary .fmp12 Format`. An XML payload is now recognised as an export before the content is scanned; a `.fmp12` / `.fp7` / `.fp5` filename and the `HBAM2` binary header still identify a real database.
- **A custom value list in a FileMaker DDR arrived empty and rolled the whole import back** (`server/internal/migration/filemaker.go`): a DDR writes one `<Text>` child per entry inside `<CustomValues>`, but only `<Value>` children and the element's own character data were read, so `Preflight` reported "Custom value list with 0 entries" as converted and `Apply` then failed validation with *a custom value list needs at least one value* and discarded the entire solution. The `<Text>` entries are now read, and a value list that genuinely carries none is reported as requiring manual action and skipped instead of reaching the import.

## [0.19.2] - 2026-10-17

### Added
- **Comprehensive File4Base v0.19.2 Manual (`docs/manual-file4base-0.19.2.md`):** Complete end-user, visual layout designer, schema/script engineer, and server administrator manual in English illustrated with 20 high-resolution UI screenshots (`docs/images/manual-0.19.2/01_login_dialog.png` – `20_about_file4base.png`) and architectural diagrams.

### Changed
- Version bump to demonstrate the in-app new-version banner live (no functional changes).
- Wrapped account badges in `ManageSecurityDialog` and header text in `ServerToolsDialog` (`Containers & Services`) to prevent horizontal row overflow on narrow viewports.

## [0.19.1] - 2026-10-17

### Fixed
- **The in-app new-version banner (0.18.0) never appeared.** It watched the service-worker update lifecycle, but modern Flutter ships a self-unregistering service worker (no caching worker), so those events never fired. The detector now records the version the page loaded with and polls the deployed `version.json`; when the deployed version differs, it shows the Actualizar/Cancelar bar (verified end-to-end).

## [0.19.0] - 2026-10-17

### Changed
- Version bump to validate the in-app new-version detection added in 0.18.0 (no functional changes).

## [0.18.0] - 2026-10-17

### Added
- **The "Manage" menu is now also in the Server Tools top menu**, at the end (Database, Security, Value Lists, External Data Sources, Layouts, Scripts) — the same menu as File ▸ Manage, with "Containers" excluded (it is a Server Tools section). It is in both the Server Tools menu-bar dropdown and the console dialog.
- **New-version banner (web PWA).** The web client caches itself via a service worker, so after a deploy the browser kept serving the old build until fully closed. It now checks for a newer version while open and, when one is ready, shows a bar offering **Actualizar** (unregisters the worker, clears caches and reloads for a guaranteed-fresh version) or **Cancelar**.

### Changed
- **The Super-owner switch now works on any account, not only existing owners.** Super-owner is a privilege on top of Owner, so turning it on for a non-owner account promotes it to Owner first and then grants super-owner (a single toggle). It still never appears on your own account.

## [0.17.0] - 2026-10-17

### Added
- **The "Manage" menu is now also at the end of the Server Tools console.** A super-owner/administrator can reach solution management (Database, Security, Value Lists, External Data Sources, Layouts, Scripts) from one place; each opens its own dialog. "Containers" was removed from the Manage menu (in both File ▸ Manage and here) — it belongs to Server Tools proper, not to solution management.
- **A clear Super-owner on/off switch in the account editor** (Manage Security ▸ edit an owner account). A super-owner now has an explicit, labelled toggle — ON = full, server-wide access to every Server Tool; OFF = normal owner — instead of only the small shield icon on the row. It appears only to a super-owner, only on owner accounts, and never on your own account; toggling takes effect immediately and signs the target out.

## [0.16.2] - 2026-10-17

### Added
- **The login screens now show the loaded client version** (a `v0.16.2` badge on the "Connect & Login" dialog and a line on the "Protected Solution / Sign In" screen). Because the web client is a PWA cached by a service worker, after an update the browser can keep serving an older build; showing the version makes a stale cache obvious at a glance (do a hard reload, or unregister the service worker, if it does not match the published version).

## [0.16.1] - 2026-10-17

### Fixed
- **The super-owner switch could not be used in the default setup (`FILE4BASE_ADMIN_DATABASE` set, no `FILE4BASE_SUPER_OWNER`).** Granting super-owner required already being a super-owner, but the only bootstrap was the env seed — so with an admin database and no seed there was no way to grant server-wide (Server Tools) access from the UI: a dead-end. An owner of the admin database is already a server administrator, so it now signs in as an **effective super-owner** and therefore sees and can use the super-owner switch (the shield on owner rows in Manage Security) to delegate server-wide access to other owners — no `FILE4BASE_SUPER_OWNER` seed needed. No endpoint guard was loosened: the effective super-owner is computed at sign-in (like the seed), alongside the existing `FILE4BASE_ADMIN_DATABASE` compatibility. Sign in again after updating for it to take effect.

## [0.16.0] - 2026-10-16

### Added
- **In-app "update available" notice with one-click guided deploy** (web client). While a server administrator is signed in, the app checks the running version against the latest File4Base release every 30 minutes (and at sign-in), and when a newer version is published it shows a notice with the current→latest versions and the release notes. Confirming takes an automatic pre-update backup (server side) and shows the exact command to run on the Docker host (`docker compose pull && docker compose up -d`) with a copy button — because the API container has no access to the host's Docker, it cannot pull and recreate the stack itself, so the operator runs that one command to finish. The amber toolbar badge now opens this notice instead of pointing to Server Tools, and the check stops polling for accounts that are not server administrators. Reuses the existing `GET /api/v1/server/containers` version check and `POST /api/v1/server/containers/update` (no server changes).

## [0.15.2] - 2026-10-16

### Fixed
- **Real-time sync never connected in the deployed server (WebSocket upgrade failed with `501 Not Implemented`).** The global telemetry/logging middleware wraps every response in a status-recording writer, but that wrapper embedded `http.ResponseWriter` without forwarding `Hijack`, so it did not satisfy `http.Hijacker`. The WebSocket upgrade at `GET /api/v1/realtime` needs to hijack the connection, so it was refused for every client — each browser only ever saw its own edits and picked up others' changes solely on a manual reload or when navigating between records (the database was always shared; only the live push was broken). The wrapper now forwards `Hijack` (and `Flush`). This passed the unit tests before because they mount the API router without the production middleware stack; added a telemetry test that the wrapped writer supports hijacking.

## [0.15.1] - 2026-10-16

Security-review follow-ups deferred during 0.15.0. Each item was developed and reviewed as its own pull request.

### Security
- **Dependency and Go toolchain updates to clear all known vulnerabilities** (`server/go.mod`, `server/go.sum`). `govulncheck ./...` reported **23 called vulnerabilities** (Go standard library plus `github.com/go-chi/chi/v5`); it now reports **0**. The build toolchain is pinned to **go1.26.9** (new `toolchain` directive, keeping the `go 1.26.0` minimum), `github.com/go-chi/chi/v5` is updated `5.2.1 → 5.5.0` (fixes the host-header/open-redirect in `RedirectSlashes` and the request-parsing advisories `GO-2026-5774/5775/5777`), and `golang.org/x/net` is updated `0.58.0 → 0.61.0` (fixes `GO-2026-6603/6610/6611/6612/6617`). One advisory remains and is accepted: `GO-2026-5932` (the unmaintained `golang.org/x/crypto/openpgp` package) has no fixed version, and File4Base neither imports nor calls it.
- **The audit log, session list and login throttle no longer trust a client-supplied IP by default.** `X-Forwarded-For` / `X-Real-IP` were honored from any caller (and chi's `middleware.RealIP` rewrote `RemoteAddr` from them unconditionally), so a direct client could forge the address recorded against it — or, since the login throttle keys on that address, interfere with another account's lockout state. Those forwarding headers are now trusted only when the request's direct TCP peer is a configured reverse proxy, set via the new **`FILE4BASE_TRUSTED_PROXIES`** setting (comma-separated IPs and CIDR ranges); the real client is then recovered by walking `X-Forwarded-For` right-to-left past trusted hops. With no trusted proxies configured (the default) the direct peer address is always used. The resolution is shared by the audit/session metadata, the request telemetry log and the login-throttle key (new `internal/clientip` package). Documented in `.env.example` and `docs/api/API_REFERENCE.md`.
- **Hardening from a review of the file importers, the encrypted channel and the control store** (documented in `docs/specs/security_model.md`). The encrypted payload channel (`server/internal/crypto`) now (a) logs a startup warning when `FILE4BASE_CHANNEL_SECRET` is unset — the fallback key is a public built-in default that provides no confidentiality on its own — and (b) caps the in-memory read of an encrypted request body at 512 MiB so a hostile oversized payload cannot exhaust server memory. The review confirmed the `.fmp12` / HTML-DDR importers whitelist (`[a-z0-9_]`) and quote every imported identifier and bound every binary read against the input length (no SQL injection, no out-of-bounds), and that the SQLite control store is fully parameterised with transactional writes; those needed no code change.

### Fixed
- **Saving a script now runs inside a database transaction, so a failing step can no longer corrupt it** (`server/internal/schema/script_service.go`). `CreateScript` and `UpdateScript` wrote the script row and each of its steps as separate statements; `UpdateScript` first deleted all existing steps and then re-inserted the new ones one at a time. A failure partway through (for example a step violating a column constraint) left the script with its steps wiped out and only a prefix of the new sequence written — and `CreateScript` could leave a half-created script. Both now wrap the whole operation in a single transaction that rolls back on any failure, leaving the previously stored script untouched. Added `internal/schema` tests that a failing step rolls back both the create and the update.

### Changed
- **Audited and annotated the client's unhandled async futures** (`client/lib`, 135 `discarded_futures` / `unawaited_futures` from `flutter analyze`, now 0). The audit found that the flagged fire-and-forget calls target methods that already surface their own failures to the user (an error `SnackBar`, an in-dialog error state, or a `_report(...)`) or are benign lifecycle cleanup (`cancel()` / `close()`); none was silently swallowing an error in a user-facing flow. Each is now wrapped in `unawaited(...)` (the idiom already used in the codebase) so the intentional discard is explicit — behaviour is unchanged, errors still reach the zone handler, and the lint is cleared. All 427 Flutter tests pass. (The only remaining findings are in test files and are benign.)

## [0.15.0] - 2026-10-16

### Added
- **Super-owner accounts**: a per-account `super_owner` flag that grants full, server-wide control independent of the database a session signed in to — the account-based way to administer the whole server, alongside the `FILE4BASE_ADMIN_DATABASE` setting (an owner of that database is still a server administrator, for backward compatibility). A super-owner reaches every Server Tool of every database. The first super-owner is bootstrapped with `FILE4BASE_SUPER_OWNER=database:username` (comma-separated for several); a seeded account signs in as a super-owner even before any flag is stored, so it can then grant the privilege to other owners. Grant/revoke with `PUT /api/v1/security/users/{id}/super-owner` (super-owners only; the target must be an owner; a super-owner cannot strip their own privilege). Manage Security shows a shield control on owner rows for super-owners. Changing the flag re-stamps the account, so the target's existing sessions end at once.
- **Real-time sync (server)**: a change-notification WebSocket at `GET /api/v1/realtime`. A client authenticates with its bearer token as the first message and then receives a small event (`{type, resource, database, origin}`) whenever the records or schema of its database change in another session — so every open window can reload instead of going stale. Events carry no record data (the client reloads what it shows) and are scoped to the client's database; the `origin` id lets a client ignore the changes it made itself. In-memory per process (a future multi-replica setup would use Postgres LISTEN/NOTIFY). The Flutter client subscribes to it (below).
- **Real-time sync (client)**: while signed in, the app keeps a WebSocket to `/api/v1/realtime` (auto-reconnecting). When another session changes the table you are viewing, Browse reloads and keeps you on the same record — and it waits until you leave a field so it never interrupts your typing; schema/layout changes reload the tables and layouts. The app ignores the changes it made itself.
- Server Tools: a backup file (`.tar.gz` or `.f4b.json`) can be uploaded (`POST .../backups/upload`, "Upload Backup..." button) and stored as a server-side backup, then restored — the counterpart of download. A non-administrator's upload can only ever target their own database.

## [0.14.0] - 2026-10-15

### Added
- Server Tools: a backup can be downloaded as a gzip-compressed tar (`.tar.gz`, `GET .../download?format=targz`) and saved to disk; the window's download button now saves the file (a separate button still copies the JSON to the clipboard).

### Security
- **Server Tools could be used to read and change other databases and to control the whole server.** Every `/api/v1/server/*` route only required the `admin` role of *some* database, and by default anyone who can reach the server can create a database and become its owner. Such an account could back up, download and restore any other database, list and revoke every session, read and clear the audit log, change TLS and restart or update the containers. Now:
  - **Server-wide tools** (containers, TLS, sessions, audit log, schedules, notifications, telemetry) require a **server administrator**: an owner of the database named by the new `FILE4BASE_ADMIN_DATABASE` setting. Without it they are disabled.
  - **Database tools** (backups, maintenance) stay available to a database's owners and admins, **for their own database only**; another database's backups are not listed and are reported as not found.
  - New `GET /api/v1/server/access`; the Server Tools window only shows the modules the session may use and explains how the server-wide ones are enabled.
  - **Upgrade note:** set `FILE4BASE_ADMIN_DATABASE` (in `.env`) to keep using the server-wide tools.
- **External data sources could be made to write.** Reading a source with a statement only checked that it began with `SELECT` or `WITH` and held no `;`, and anyone with the `bulk_import` capability could send one, run with the credentials the owner stored. SQL Server accepts a write after `WITH`, several statements without `;`, and `SELECT … INTO` creates a table. Now:
  - A statement of one's own (`query`) is for **owners** only (`403` otherwise); everyone else who may import picks one of the source's tables. The import window only offers "A SELECT" to owners.
  - Statements containing a write or execution keyword (`INSERT`, `UPDATE`, `DELETE`, `MERGE`, `INTO`, `EXEC`, `CREATE`, `DROP`, …) outside quoted text, quoted names and comments are refused (`422`).
  - PostgreSQL and MySQL/MariaDB sources are read in a **read-only transaction**. SQL Server's driver has none: give a SQL Server source a read-only login.
- **No limit on password guessing.** Sign-in (`POST /api/v1/auth/login`) and credential-based database drops accepted unlimited password attempts. Now a database + username + client is locked out (`429 Too Many Requests`, with `Retry-After`) after `FILE4BASE_LOGIN_MAX_ATTEMPTS` failures in a row (default 10), for `FILE4BASE_LOGIN_LOCKOUT_SECONDS` (default 15 min). A missing second factor does not count; a success clears the counter. Keyed on the client address too, so one attacker cannot lock a victim out.

---

## [0.13.1] - 2026-10-15

### Fixed
- Browse mode: one click on a field now puts the cursor in it, ready to type. It took two: every field was treated as formatted (unset number/date/time formats and the default text case `none` counted as formatting), and a formatted field was rebuilt as a new text field when focused, so it got the focus but no text input until a second click.
- Browse mode, List and Table views: one click on a field of another record makes it the current record and starts editing that field. The field was focused before the record changed, and the double click on the whole row (open in Form view) delayed every single click; it moved to the record number (Table) and an open button at the end of each row (List).
- Server Tools reported a hard-coded stack version (0.13.0) that `scripts/bump_version.sh` does not update, so after an upgrade it showed the old version and a stale "update available". The server now reports its own version (`AppVersion`, passed as `api.Options.Version`; `FILE4BASE_VERSION` still overrides it) and the client uses its single version constant.
- Client tests: refreshed the shared new-database fixture (it lacked the File Options `kiosk_mode` key added in 0.13.0; the server stores File Options as given, so nothing was lost), and updated the Script Workspace and Browse record bar tests to the current catalog and inline-editing table.

## [0.13.0] - 2026-10-14

### Added
- **Server Tools Menu & Administration Console** (`client/lib/features/server_tools/server_tools_dialog.dart`, `client/lib/core/widgets/file4base_menu_bar.dart`, `server/internal/api/server_admin_handler.go`, `server/internal/telemetry/collector.go`, [`docs/specs/server_tools_and_mfa.md`](docs/specs/server_tools_and_mfa.md)):
  - Added top-level **Server Tools** menu right after `Tools` with 9 integrated server administration modules:
    1. **Containers & Services (with Automatic Version & Update Watcher)**: Live health, version (`current_version` vs `latest_version`), uptime, resource usage, log viewer, graceful service restart, upstream release check (`POST /api/v1/server/containers/check-updates` against GitHub Releases & Docker Hub `cloudresources/file4base-api`), update simulation, top menu bar & bottom status bar **"Update Available (vX.Y.Z)"** alert badges, and 1-click **"Pre-Backup & Update Now"** (`POST /api/v1/server/containers/update` with automatic `pre_update` snapshot in `./data/backups/`) for `file4base-api`, `file4base-web`, and `file4base-postgres`.
    2. **Backups & Snapshots**: On-demand server snapshots (`.f4b.json`) persisted in `./data/backups/` (mounted to `/app/data/backups/` inside `file4base-api`) capturing tables, columns, occurrences, relationships, layouts, scripts, value lists, accounts, and table rows, with 1-click download, true point-in-time DDL/DML/layout restoration (dropping extra tables/columns/layouts added after the snapshot and restoring modified/deleted items), live workspace reload, and deletion.
    3. **Domain & SSL/TLS Certificates (ACME / Custom PEM)**: Configure the public Web/API domain, automatic Let's Encrypt / ZeroSSL ACME certificate provisioning (`http-01` / `tls-alpn-01`), or custom PEM certificate chain + private key upload with X.509 issuer/expiry inspection.
    4. **API Telemetry & Live Metrics**: Rolling 60-minute in-memory telemetry ring buffer with live Requests Per Minute (RPM) bar charts, Average vs P95 Latency line charts, HTTP `2xx`/`4xx`/`5xx` status distribution, Go runtime memory/goroutines, DB ping latency, and top endpoints table.
    5. **Connected Clients & Sessions**: Real-time table of authenticated sessions across databases with 1-click session revocation (`Force Disconnect`).
    6. **Scheduled Tasks & Cron Scripts**: Cron scheduler (`0 2 * * *`, `*/15 * * * *`) for automated backups and server-side script execution with `Run Now` support.
    7. **Audit & Security Logs**: Filterable security and administrative event log (`auth`, `mfa`, `backup`, `tls`, `container`, `maintenance`, `schedule`).
    8. **Database Maintenance & Optimizer**: Live per-table column and row counts with 1-click `VACUUM ANALYZE`, `ANALYZE`, and `REINDEX DATABASE` execution.
    9. **Server Notifications, SMTP & Webhooks**: Outbound SMTP email and Slack/Discord/Teams/Generic JSON webhook alerting with threshold triggers and test dispatch.
- **Multi-Factor Authentication (MFA / 2FA) with Google Authenticator (RFC 6238 TOTP)** (`server/internal/schema/mfa.go`, `server/internal/api/security_handler.go`, `client/lib/features/security/manage_security_dialog.dart`, `client/lib/features/auth/database_login_dialog.dart`):
  - Added an **MFA (Google Authenticator)** action icon and `2FA` status badge in the `OPTIONS` column of the `Accounts` table in **Manage Security**.
  - Generates a 160-bit Base32 TOTP secret and renders an offline `otpauth://totp/` QR code (`package:qr`) for scanning with Google Authenticator, Authy, or 1Password, requiring a 6-digit verification code to activate.
  - Enforces 6-digit TOTP verification during sign-in (`POST /api/v1/auth/login` returns `mfa_required: true` and reveals the 6-digit authenticator input in `DatabaseLoginDialog`).
- **Functional `Edit` Menu (`Undo`, `Redo`, `Cut`, `Copy`, `Paste`, `Clear`, `Select All`)** (`client/lib/core/widgets/file4base_menu_bar.dart`, `client/lib/features/layout_engine/layout_designer_widget.dart`, `client/lib/main.dart`):
  - Added `EditMenuFocusTracker` to preserve the active `EditableTextState` and per-controller undo/redo stacks when clicking the top menu bar in Browse and Find modes, with automatic fallback to `revertRecord()` on `Undo`.
  - Added `cutSelected()`, `clearSelected()`, and `selectAllObjects()` in `LayoutDesignerWidgetState` so all 7 `Edit` menu commands work seamlessly on layout objects in Layout mode and text inputs across the application.
- **View Menu Design Tools (`Page Margins`, `Formatting Bar`, `Text Ruler`) & Full `Format` Menu** (`client/lib/features/layout_engine/view_format_tools.dart`, `client/lib/features/layout_engine/models/layout_definition.dart`, `client/lib/features/layout_engine/layout_object_visuals.dart`, `client/lib/core/widgets/file4base_menu_bar.dart`, `client/lib/features/layout_engine/layout_designer_widget.dart`, `client/lib/features/data_browser/data_browser_widget.dart`, `client/lib/main.dart`):
  - **View > Formatting Bar (`File4BaseFormattingBar`)**: Horizontal quick-formatting toolbar at the top of the workspace for editing font family, size, style (`Bold`, `Italic`, `Underline`, `Strikethrough`), text/fill/border colors, alignment, line spacing, and launching data/conditional format dialogs without opening the right Inspector.
  - **View > Text Ruler (`File4BaseTextRuler`)**: Graduated ruler (`pt` / `in`) above the canvas allowing interactive mouse drag adjustment of Left/Right paragraph indents (`indentLeft`, `indentRight`) and click-to-toggle Tab Stops (`tabStopsPt`).
  - **View > Page Margins (`PageMarginsOverlay`)**: Dashed printable area boundaries and page break guides across the canvas based on the active `PageSetupModel`.
  - **Format Menu**: Enabled all typography (`Font`, `Size`, `Style`, `Align Text`, `Line Spacing`, `Text Color`, `Text...`), non-destructive data display formatting (`Number...`, `Date...`, `Time...`, `Timestamp...`), `Conditional...` formatting rules (e.g., negative values highlighted in red/bold), and Layout Mode object arrangement (`Align Object`, `Distribute`, `Resize`).
- **Complete Kiosk Mode (`Tools > Developer Utilities...`) & Runtime UI Lockout** (`client/lib/features/tools/developer_utilities_dialog.dart`, `client/lib/core/models/file_options_model.dart`, `client/lib/core/models/script_models.dart`, `client/lib/features/layout_engine/layout_action_runner.dart`, `client/lib/features/data_browser/data_browser_widget.dart`, `client/lib/features/layout_engine/layout_preview_widget.dart`, `client/lib/main.dart`):
  - **Full Interface Lockout (`Bloqueo de Interfaz`)**: When Kiosk Mode is active, the top Menu Bar (`File4BaseMenuBar`), Formatting Bar (`File4BaseFormattingBar`), Text Ruler (`File4BaseTextRuler`), Left Status Sidebar (`File4BaseStatusSidebar`), and Bottom Status Bar completely disappear, and developer keyboard shortcuts (`Cmd+L`, `Cmd+N`, `Cmd+O`, `Cmd+S`) are blocked.
  - **Conditional Privilege (`Privilegio Condicional`)**: Accounts with `[Full Access]` (`owner` / `admin`) or layout/script modification privileges open the solution normally so developers can continue programming, while non-admin accounts enter full Kiosk lockout automatically. Added a **"Test Kiosk Mode Now (Simulate Non-Admin)"** button in `Developer Utilities` with `Esc` / top-right badge exit so developers can preview Kiosk Mode live.
  - **Primary / Home Layout, Forced Login & Readiness Auditor**: Added Primary/Home Layout selector (`kiosk_primary_layout_id`), forced credential prompt (`kiosk_require_login` disables `"Log in using..."`), a **Kiosk Navigation & Exit Readiness Check** that detects dead-end secondary layouts or missing exit buttons, and a 1-click **"Auto-Add Home & Exit Buttons"** action.
  - **Manual Navigation, Window, Session & Preview Auto-Return Script Steps**: Added `Enter Browse Mode` (`enter_browse_mode`), `Adjust Window` (`adjust_window`), `Re-Login` (`re_login`), `Close File` (`close_file`), and `Exit Application` (`exit_application`) to the Script Workspace and Button Action Setup, plus automatic countdown return from `Preview Mode` to `Browse Mode` during Kiosk Mode.
- **Full Solution (`.f4p`) & Database Data (`.f4data`) Roundtrip Import** (`server/internal/schema/solution_service.go`, `client/lib/features/solution_manager/open_solution_dialog.dart`, `client/lib/core/widgets/file4base_menu_bar.dart`, `client/lib/main.dart`):
  - Preserves default Table Occurrence Relationship Graph coordinates (`x_pos`, `y_pos`), remaps `startup_layout_id`, `kiosk_primary_layout_id`, `layout_id`, and `target_layout_id` across layouts and `file_options`, restores layout/table permissions for existing accounts on `.f4p` import, normalizes empty string values for numeric/date/timestamp columns on `.f4data` import, and adds `File > Import Data (.f4data)...` plus an optional companion `.f4data` selector in `Open Solution`.
- **Complete OpenAPI 3.0.3 / Swagger UI & Web-to-API Nginx Reverse Proxy Synchronization** (`client/nginx.conf`, `server/internal/api/swagger/ui/openapi.json`, `docs/api/API_REFERENCE.md`):
  - Synchronized all **103 REST API paths** under `/api/v1/...` called by `ApiClient` (`client/lib/core/api/api_client.dart`) into `server/internal/api/swagger/ui/openapi.json` and `docs/api/API_REFERENCE.md`.
  - Updated `client/nginx.conf` in `file4base-web` (`client_max_body_size 64m`, prioritized `location ^~ /api/`, `location ~ ^/(swagger|docs|openapi\.json)(/|$)`, and `location ~ ^/(healthz|livez|readyz|startupz)(/|$)` before static `.js` regex rules) so Swagger UI assets (`/swagger/swagger-ui-bundle.js`, `/swagger/swagger-ui-standalone-preset.js`) and large `.f4p`/`.f4data` uploads proxy cleanly to `http://api:8080`.

### Fixed
- **Browse Mode Field Entry, Formatted Field Focus, Inline List/Table Editing & Default Form Sync** (`client/lib/features/data_browser/data_browser_widget.dart`, `client/lib/features/schema_manager/manage_database_dialog.dart`, `client/lib/main.dart`):
  - Fixed formatted fields (`Fee Paid`, currency/date/time/text formatted fields) so the underlying `TextField` stays mounted in a `Stack` while unfocused, allowing `FocusNode.requestFocus()` to immediately enter edit mode when clicked.
  - Enabled direct inline `TextField` data entry in **Table View** (`View as Table`) and **List View** (`View as List`) in addition to **Form View**, and added dynamic vertical padding for compact field boxes (`height < 32`).
  - Automatically places newly created columns in **Manage > Database** onto the table's default `<Table> Form` layout and prefers editable Form layouts (or `startup_layout_id`) over Report/Chart/Label layouts when opening a database.
- **Wildcard (`*`, `@`, `?`) and Date (`//`) Find Expression Routing (`server/internal/data/service.go`)**: Fixed `buildFindClauses` so find requests containing FileMaker wildcard tokens (`Sm*`, `*Rue de la Paix*`, `@`, `?`, `//`) without pre-converted SQL `%` wildcards are routed through `parseFindValueExpr` instead of matching literal asterisks.
- **Script Step Primary Key Collision Across Scripts (`server/internal/schema/script_service.go`)**: Fixed `CreateScript` and `UpdateScript` to detect when a caller-supplied step ID (such as `"step_1"`) already exists in `sys_script_steps` across another script, automatically generating a fresh UUID and remapping `parent_step_id` references.

---

## [0.12.1] - 2026-10-13

### Added
- **FileMaker HTML DDR & Binary `.fmp12` Record/Container Migration**:
  - **HTML DDR Parser (`server/internal/migration/html_ddr.go`)**: Parses full and compact FileMaker HTML Database Design Reports (`recipes.html`, `Summary.html`), extracting base tables, columns, calculations, summaries, table occurrences, pair-keyed relationships (`layout_occurrence|target_occurrence`), value lists, scripts, and layouts (`768pt` and `1024pt` canvas bounds, portals with absolute child offsets, multiline merge text preserving `&#13;` line breaks, `Group Buttons` attached to fields and merge links, and `web_viewer` controls).
  - **Binary `.fmp12` Record & `CONTAINER` Image Extractor (`server/internal/migration/fmp12_records.go`)**: Scans `HBAM2` pages when a `.fmp12` file is paired with an XML/HTML DDR (`fmp12_base64`), extracting table rows, relational UUID keys, embedded `JPEG`/`PNG` `CONTAINER` images (encoded as base64), and pre-evaluated calculation/summary fields (`qty_metric`, `unit_metric`, `yield_calc`, `ingredients_formatted`, `tags`).
  - **Layout Canvas, List View, Container Image, Web Viewer & Script Runner Enhancements** (`client/lib/features/data_browser/data_browser_widget.dart`, `client/lib/features/data_browser/report_view.dart`, `client/lib/features/data_browser/related_records.dart`, `client/lib/features/layout_engine/layout_action_runner.dart`, `client/lib/features/layout_engine/layout_object_visuals.dart`):
    - Renders `CONTAINER` fields (`recipes::Photo`) as decoded images (`Image.memory`) across Form, List, and Preview/Report views.
    - Renders custom list layouts (`defaultView == 'list'`, e.g., `Recipe List`) with a fixed header band and repeating clickable record rows.
    - Renders `web_viewer` objects (`Web View`) with address bar, recipe preview card, and external browser launch.
    - Enables multiline `TextField` inputs when `height > 44` (`Description`, `Instructions`).
    - Resolves `{{LayoutName}}` and `snake_case` merge tokens across Form, List, and Preview modes.
    - Executes Title Case and `snake_case` script steps including `If` / `Else If` / `Else` / `End If` (`Get ( LayoutName )`, `not IsEmpty(...)`, `and`/`or`, `≠`), `Set Variable` (`$local` and `$$global` including `$$lastLayout`), and `Go to Layout`.
    - Disambiguates self-join relationships in portals (`RelatedContext.resolve` and `relIDByOccurrence` pair keys) so parent-to-child portals (`recipe_ingredients`) are never shadowed by auxiliary self-joins (`recipe_ingredients = recipe_ingredients ordered`).
- **FileMaker-to-File4Base Migration Guide & Case Study** ([`docs/tutorials/filemaker_migration_guide.md`](docs/tutorials/filemaker_migration_guide.md), [`docs/specs/filemaker_migration.md`](docs/specs/filemaker_migration.md), `assets/mockups/filemaker_migration_pipeline.svg`, `assets/mockups/filemaker_migration_layouts.svg`, `assets/mockups/file4base_edit_recipe_portal_error.png`, `assets/mockups/file4base_edit_recipe_resolved.png`): Step-by-step migration documentation, architecture diagrams, Before/After portal resolution screenshots, and engineering case study based on `recipes.fmp12` + `recipes.html`.
- **Flutter Static Analysis with `very_good_analysis`** (`client/analysis_options.yaml`, `client/pubspec.yaml`, `.github/workflows/code_quality.yml`, `scripts/flutter_analyze_fix.sh`): Configured `very_good_analysis: ^10.2.0` for automated lint checking and `dart fix --apply` remediation locally and in GitHub Actions CI.

---

## [0.12.0] - 2026-10-13

### Added
- **Tools Menu Advanced Capabilities (`Custom Menus`, `Developer Utilities`, `Database Design Report`, `Save a Copy as XML`, `Layout Themes`)**:
  - **Custom Menus (`Tools > Custom Menus`)**: Added `hide` menu mode (`MenuModeHide`) to completely hide non-essential standard menus from the menu bar, plus a **Load Standard Items** preset in `ManageCustomMenusDialog` so designers can selectively remove individual commands (such as *Delete Record*), rename menu labels, assign custom keyboard shortcuts, or bind menu commands to custom scripts ([`docs/specs/custom_menus.md`](docs/specs/custom_menus.md)).
  - **Developer Utilities (`Tools > Developer Utilities...`)**: Added **Kiosk Mode** (`kiosk_mode` in `file_options`, hiding status toolbars and menus for non-admin accounts) and **AES-256-GCM Solution Encryption** configuration alongside solution renaming, zero-record database cloning, and permanent design lock ([`docs/specs/developer_utilities.md`](docs/specs/developer_utilities.md)).
  - **Database Design Report & Save a Copy as XML (`Tools > Database Design Report...`, `Tools > Save a Copy as XML...`)**: Exposed direct structured XML design export (`Save a Copy as XML...`) and full HTML/XML/JSON DDR generation in the `Tools` menu, prompting sign-in automatically when invoked from an unauthenticated session ([`docs/specs/design_report.md`](docs/specs/design_report.md)).
  - **Layout Themes (`Tools > Layout Themes...` & `File > Manage > Themes...`)**: Added `Layout Themes...` to the `Tools` menu and added **Copy Theme**, **Paste / Import Theme...**, and **Export (.json)** (`file4base_theme_v1`) in `ManageThemesDialog` so custom corporate themes can be copied and imported across different solutions without configuring elements one by one ([`docs/specs/copy_paste_structure.md`](docs/specs/copy_paste_structure.md)).
- **Assisted FileMaker Migration Path & Capability Parity Matrix**
  ([#26](https://github.com/file4base/file4base-app/issues/26),
  [#27](https://github.com/file4base/file4base-app/issues/27)):
  - **Assisted FileMaker XML / DDR & `FMPXMLRESULT` Converter** (`server/internal/migration`, `POST /api/v1/solutions/migrate/preflight`, `POST /api/v1/solutions/migrate/apply`, `FileMakerMigrationDialog`, [`docs/specs/filemaker_migration.md`](docs/specs/filemaker_migration.md)): converts FileMaker Database Design Report (`<FMPReport>`) and Save-as-XML (`<FMPSaveAsXML>`) schemas, occurrences, relationships, value lists, layouts, scripts, accounts, and `FMPXMLRESULT` record exports into File4Base solutions with a read-only dry-run preflight `ConversionReport` (`preserved`, `translated`, `unsupported`, `rejected`, `manual_action_required`), automatic script-trigger quarantine (`triggers_quarantined: true`), and disabled-by-default migrated accounts (`accounts_disabled: true`).
  - **Binary `.fmp12` detection and actionable guidance**: uploading or picking a proprietary binary `.fmp12` container (`HBAM2` header or `.fmp12` extension) in `POST /api/v1/solutions/import`, `POST /api/v1/solutions/import-data`, or `OpenSolutionDialog` returns `422 Unsupported Binary .fmp12 Format` / opens `FileMakerMigrationDialog` with clear DDR XML export instructions instead of an opaque MessagePack decoding error ([#26](https://github.com/file4base/file4base-app/issues/26)).
  - **Measurable Capability Parity Matrix & Reconciled Roadmap** ([`docs/specs/CAPABILITY_PARITY_MATRIX.md`](docs/specs/CAPABILITY_PARITY_MATRIX.md), [`docs/specs/ROADMAP.md`](docs/specs/ROADMAP.md), [`docs/specs/FUNCTIONAL_SPECIFICATION.md`](docs/specs/FUNCTIONAL_SPECIFICATION.md)): documents verified UI/backend/test coverage across all domains, supported 20-function calculation catalog, executed vs. catalog-only script steps, script triggers, and explicit non-goals ([#27](https://github.com/file4base/file4base-app/issues/27)).
- **Independent Tutorial Scenario, Provenance Ledger & Studio UI Redesign**
  ([#40](https://github.com/file4base/file4base-app/issues/40),
  [#41](https://github.com/file4base/file4base-app/issues/41),
  [#42](https://github.com/file4base/file4base-app/issues/42),
  [#44](https://github.com/file4base/file4base-app/issues/44)):
  - **Independent *Boreal Seed & Herbarium Archive* Tutorial** ([`docs/tutorials/seed_archive.md`](docs/tutorials/seed_archive.md)):
    replaces the retired *Favorite Bakery* / `ABC Company` / `XYZ Inc.` scenario
    with an original 21-lesson botanical repository walkthrough (`Accessions` and
    `Stations`), verified by automated dataset tests in `client/test/tutorial_walkthrough_test.dart`
    and updated test fixtures across `server/` and `client/test/` ([#41](https://github.com/file4base/file4base-app/issues/41)).
  - **Consolidated Reference, Asset & Design Provenance Ledger** ([`docs/PROVENANCE.md`](docs/PROVENANCE.md)):
    restores accurate external reference citations in `docs/specs/FUNCTIONAL_SPECIFICATION.md`,
    `docs/specs/file4base_menu_reference_guide.md`, and `docs/specs/DBAL_SPECIFICATION.md`,
    and inventories all shipped branding SVG/PNG/ICO assets and wireframes ([#42](https://github.com/file4base/file4base-app/issues/42), [#44](https://github.com/file4base/file4base-app/issues/44)).
  - **File4Base Slate & Sky-Cyan Studio Chrome and Horizontal Part Rail**
    (`client/lib/features/layout_engine/layout_designer_widget.dart`,
    `client/lib/features/schema_manager/widgets/relationship_graph_widget.dart`,
    `client/lib/features/schema_manager/manage_database_dialog.dart`):
    replaces narrow rotated part tabs with a 96px Horizontal Part Rail showing
    live point-height badges, replaces screenshot-derived amber graph halos with
    File4Base Slate & Sky-Cyan (`#0284C7` / `#38BDF8`) selection rings and pill
    operator badges, and updates `Exit Layout` studio styling ([#40](https://github.com/file4base/file4base-app/issues/40)).
- **API SQLite Control Store, Granular Table Permissions, Encrypted Channel & Configurable API Server**:
  - **Embedded SQLite control store in `file4base-api`** (`server/internal/control`, persisted at `CONTROL_DB_PATH` / Docker volume `file4base-api-data`) storing the project-to-database registry (`GET /api/v1/projects`), project options (`GET/PUT /api/v1/solutions/options` for `file_options` and `page_setup`), accounts, per-table permissions, and per-layout permissions.
  - **Granular per-table access & modification permissions** (`sys_table_permissions` and `GET/PUT /api/v1/security/users/{id}/table-permissions`): owners and admins can configure per-table `access_level` (`read_write`, `read_only`, `none`) and modification flags (`can_create`, `can_edit`, `can_delete`, `can_modify_schema`) in **Manage Security**, enforced by the data API on record reads, inserts, updates, and deletes, and exported/imported inside `.f4p` solution bundles (`table_permissions` on `BundleAccount`).
  - **AES-256-GCM encrypted payload channel & optional HTTPS listener** (`server/internal/crypto/envelope.go` and `client/lib/core/crypto/payload_cipher.dart`): authenticates and encrypts HTTP request/response payloads (including `.f4p` and `.f4data` exports/imports) using the `F4BE\x01` AES-256-GCM envelope (`X-File4Base-Encrypted: aes-256-gcm`, `Accept-Encryption: aes-256-gcm`, `GET /api/v1/crypto/handshake`), plus optional TLS (`TLS_ENABLED=true`) in `file4base-api`.
  - **API Server URL selector in the Sign-In dialog** (`DatabaseLoginDialog`): defaults to `http://127.0.0.1:8080` and allows switching the target API server directly from the login screen, reloading the server's registered projects and databases.
- **Copy and paste structure between solutions**
  ([#54](https://github.com/file4base/file4base-app/issues/54)). Tables, fields,
  layouts, and scripts can now be copied to the system clipboard as reviewable
  `f4b.structure_clipboard` (`v1`) JSON text (`POST /api/v1/solutions/clipboard/copy`)
  and pasted into the same or another solution from **Manage Database** (Tables
  and Fields tabs), **Manage Layouts**, and **Script Workspace**:
  - **Pre-paste collision & dependency preview**
    (`POST /api/v1/solutions/clipboard/preview` and `StructurePastePreviewDialog`)
    flags name collisions with a choice to **Keep both and rename** (`rename`)
    or **Update / replace** (`replace`), and lists any missing table
    occurrences, fields, scripts, layouts, or value lists in the destination
    solution.
  - **Name-based reference remapping** (`POST /api/v1/solutions/clipboard/paste`)
    automatically remaps `table_occurrence_id`, `script_id`, and `value_list_id`
    references when matching names exist in the destination solution, while
    keeping unresolved references editable instead of failing silently.
- **Developer Utilities: clone without records, rename, and permanently lock design**
  ([#53](https://github.com/file4base/file4base-app/issues/53)). Owners can now
  open **Tools > Developer Utilities...** to prepare a solution for deployment
  or distribution:
  - **Clone without records** (`POST /api/v1/solutions/clone`) creates a new
    physical database on the server with all tables, fields, occurrences,
    relationships, layouts, scripts, value lists, custom menus, accounts, and
    privileges, and zero user records.
  - **Rename solution** (`POST /api/v1/solutions/rename`) updates the solution's
    logical name in `sys_solution_meta` and rewrites layout and script step
    references to the previous solution name.
  - **Lock solution design** (`POST /api/v1/solutions/lock-design`, or
    `lock_design: true` when cloning) permanently and irreversibly disables
    schema, layout, script, value list, custom menu, and data source mutations
    (`403 Forbidden` via `RequireDesignUnlocked`) while keeping record CRUD,
    searches, exports, and sign-in working.
- **Custom Menus defined by the solution**
  ([#52](https://github.com/file4base/file4base-app/issues/52)). Solutions can
  now store custom menu sets in `sys_menu_sets` (`/api/v1/menu-sets`), mark one
  set as the solution default or assign a menu set per layout
  (`definition.menu_set_id`), and manage them from **Tools > Custom Menus >
  Manage Custom Menus...**.
  - Custom menus can **extend** or **replace** standard menus, except **File**
    (which is extend-only so `Sign Out` and `Open Solution` remain reachable)
    and **Tools > Custom Menus** (which stays reachable so the standard menu
    bar can always be restored).
  - Keyboard shortcuts on custom menu items are checked against duplicates
    within the set and reserved mode/quit shortcuts (`Cmd/Ctrl+B`, `F`, `L`,
    `U`, `Q`).
  - `.f4p` solution bundles and the Database Design Report (`#49`) carry custom
    menu sets, remapping `script_id`, `layout_id`, and layout `menu_set_id`
    references on import.

### Fixed
- **Persistent destination folder for solution saves and record exports** (`SolutionStorageService.defaultDirectory`, `ExportRecordsDialog`, `SaveCopyDialog`, `NewDatabaseDialog`): once a destination folder is chosen on disk (or via the File System Access API on Web), subsequent solution saves (`.f4p`), database data exports (`.f4data`), and record exports (`.csv`, `.tab`, `.xlsx`) write directly into that folder instead of re-prompting or falling back to the browser's `Downloads` directory.
- **Release workflow publishes multi-architecture Docker Hub images**
  ([#55](https://github.com/file4base/file4base-app/issues/55)). Tagging a `v*`
  release now builds and pushes `file4base-api` and `file4base-web` for
  `linux/amd64`, `linux/arm64` and `linux/arm/v7` in
  `.github/workflows/desktop_release.yml` after the PostgreSQL and MariaDB
  server test suites pass, and before the GitHub Release is published.
  `scripts/publish_images.sh` verifies that `EXPECTED_TAG` matches the root
  `VERSION` file and automatically sets `NO_LATEST=1` when publishing a hotfix
  tag older than the highest existing semver tag.
- **PDFium redistribution notices in desktop release archives**
  ([#43](https://github.com/file4base/file4base-app/issues/43)). Linux and
  Windows desktop builds (which bundle `libpdfium.so` / `pdfium.dll` via
  `printing 5.15.1`, `chromium/5200`) now install the complete upstream
  PDFium `LICENSE` and `VERSION` under `THIRD_PARTY_NOTICES/pdfium/` alongside
  the root `LICENSE` and Flutter's `NOTICES.Z`. Added
  `scripts/verify_desktop_archive.sh` to verify binary/license SHA-256
  correspondence in both `scripts/build_desktop.sh` and CI, added the
  corrective notice bundle in `THIRD_PARTY_NOTICES/pdfium/` for existing
  `v0.9.1`–`v0.11.0` downloads, and surfaced third-party notice locations and
  Flutter's license viewer in the About dialog (`Credits` tab).
- **Consistent `File4Base Web Client` naming across UI, OpenAPI, and Docker**
  ([#8](https://github.com/file4base/file4base-app/issues/8)). Replaced the
  remaining `WebDirect` display strings and comments in
  `client/lib/features/about/about_dialog.dart`,
  `client/lib/features/help/issues_guide_dialog.dart`,
  `server/internal/api/swagger/ui/openapi.json`, `client/Dockerfile`, and
  `docker-compose.yml` with **File4Base Web Client** (`[Web Client]`), leaving
  `WebDirect` only in Claris trademark attributions and negative tests.

---

## [0.11.0] - 2026-10-08

### Added
- **Script Debugger** ([#51](https://github.com/file4base/file4base-app/issues/51)).
  A script that misbehaved could only be investigated by reading it. **Tools >
  Script Debugger** runs one step at a time against the records on screen,
  with Step, Continue, Stop, Start over and breakpoints on any step.
  [docs/specs/script_debugger.md](docs/specs/script_debugger.md) is the
  specification.
  - **It drives the runner rather than imitating it.** Each step goes through
    `LayoutActionRunner`, the one a layout button uses, so what the debugger
    shows is what running the script does.
  - **A step the runner does not take stops the script** and shows the
    runner's own message, with that step marked — *Set Variable*, *If* and
    *Loop* among them. A debugger that pretended to run what the runner
    refuses would be worse than none.
  - A disabled step is skipped and marked as skipped; *Halt Script* ends the
    run where it is; the steps are listed in the order they run.
  - Opened without a layout in Browse mode it says where to start it from,
    rather than offering buttons that would do nothing.
- **Data Viewer** ([#50](https://github.com/file4base/file4base-app/issues/50)).
  A formula was only ever visible through its field: finding out why a
  calculation came out wrong meant changing the formula, saving the field,
  looking at a record and changing it back. **Tools > Data Viewer** shows the
  fields of the record in hand with their types and values, and a watch list
  of expressions evaluated against that record.
  [docs/specs/data_viewer.md](docs/specs/data_viewer.md) is the specification.
  - **The same engine as the fields.** A watched expression is evaluated by
    `internal/calc`, the one that fills the calculation fields, through a new
    `POST /api/v1/data/{table}/evaluate`. The viewer and the field agree by
    construction: there is no second implementation to drift. It only reads.
  - **One bad expression is one bad line**: it answers with its own error and
    the position the parser stopped at, and the others still answer.
  - **A field the table does not have reads as empty** — the engine's rule —
    and the viewer names it rather than showing a quietly wrong answer.
  - The result type is chosen per watch, so the same expression read as Text,
    Number, Date, Timestamp or Boolean answers in that shape.
  - **The Variables tab says no script sets a variable yet**, because *Set
    Variable*, *If* and *Loop* stop a script with a message today. An empty
    list would have read as "none are set".

- **The Tools menu is honest, and its first tool is real**
  ([#48](https://github.com/file4base/file4base-app/issues/48),
  [#49](https://github.com/file4base/file4base-app/issues/49)). Every item of
  the Tools menu opened a "roadmap" dialog that described the tool as though
  it were planned rather than doing anything — the same shape as the extended
  privileges of #39. What does not exist is now **shown disabled**, and each
  of the five has its own issue
  ([#50](https://github.com/file4base/file4base-app/issues/50),
  [#51](https://github.com/file4base/file4base-app/issues/51),
  [#52](https://github.com/file4base/file4base-app/issues/52),
  [#53](https://github.com/file4base/file4base-app/issues/53),
  [#54](https://github.com/file4base/file4base-app/issues/54)).
- **Database Design Report, and the design as text for version control**
  ([#49](https://github.com/file4base/file4base-app/issues/49)).
  [docs/specs/design_report.md](docs/specs/design_report.md) is the
  specification.
  - **Tools > Database Design Report...** writes one self-contained HTML page:
    tables and fields with their options, validation and formulas; the
    relationships graph; every layout with its parts, the fields it binds —
    naming the relationship a related field is read through — its portals and
    the scripts it runs; every script and step; value lists; accounts with
    their layout permissions; extended privileges; saved finds; and data
    sources. No script in the page and nothing to fetch, so it opens from a
    file and prints.
  - **Tools > Save Design as XML...** writes the same design as XML or JSON.
    A `.f4p` file is MessagePack and cannot be diffed; this can, and it
    carries each layout's definition exactly as it is stored, so a change to a
    layout is a readable diff. The text forms have **no timestamp** and a
    fixed order throughout, so writing an unchanged solution twice produces
    the same bytes and no commit noise.
  - **No records and no secrets in any format** — no password, no password
    hash, no stored credential — and the report says so about itself, so what
    is absent from it is not read as absent from the solution.
  - One walk of the catalog, the one the solution export already does, so the
    report and the `.f4p` file cannot describe different solutions. New
    endpoint `GET /api/v1/solutions/design-report`, owner or admin.
- **Import records from another SQL database**
  ([#47](https://github.com/file4base/file4base-app/issues/47)). *File >
  Import Records > ODBC Data Source...* was greyed out. It is now **External
  SQL Data Source...**, and it works: the records are read straight out of
  another database and arrive at the same dialog, with the same preview,
  mapping, coercion and one-transaction import a file goes through.
  [docs/specs/importing_and_exporting_records.md](docs/specs/importing_and_exporting_records.md)
  has the specification.
  - **Not ODBC, and the menu says so.** Go has no ODBC in its standard
    library and the cgo bridges pull unixODBC and third-party drivers into
    the API image, which would end the single static binary the server is.
    File4Base reads **PostgreSQL**, **MySQL / MariaDB** and **Microsoft SQL
    Server** with Go drivers instead (one new dependency,
    `microsoft/go-mssqldb`, pure Go).
  - **A registered connection holds no password.** There is no key management
    to protect one with, so rather than pretend: the password is typed for
    that import and never stored or logged, or read from an environment
    variable of the server whose name the connection records.
  - **An import names a registered source, never a host of its own**, so a
    request cannot point the server at an arbitrary machine. Registering one
    is owner-only; an admin can read the list and test a connection.
  - **Only a single SELECT is ever run.** A statement that is not one
    `SELECT`, or that holds a second one, is refused before a connection is
    opened, and a table name from the picker is quoted rather than
    concatenated. File4Base never writes to a data source.
  - The same guards a file gets: the row cap applied by the source database,
    a source that was cut says so, more columns than the limit is refused
    rather than cut, and a statement timeout.
  - Connections travel in the solution bundle, without a password. New
    endpoints under `/api/v1/data-sources`, and `data_source` on the import
    and preview endpoints.
- **Find on a related field** ([#46](https://github.com/file4base/file4base-app/issues/46)).
  Since #36 a layout can show a field of a related record; in Find mode that
  field drew a notice saying finding on it was not supported. It is a criterion
  box now: what is typed there finds the records whose **related** records match
  it — "every customer whose company is in Paris" finds customers, not
  companies.
  - A criterion names the relationship it reaches through (`relationship_id`,
    plus `occurrence` when the relationship joins a table to itself) and
    becomes an `EXISTS` over the related table. It takes the same operators as
    a criterion on an own field, combines with them with AND, and composes with
    the OR and omit semantics of several requests.
  - **A record with an empty match field relates to nothing**, so it never
    matches a related criterion — the rule the related reads already follow.
  - **The related table's access is enforced**: a user who cannot see Companies
    cannot search Customers through it either, on `/find` and on `/summary`.
  - Saved finds hold related criteria too, checked against the related table.
  - A related field whose *allow find entry* option is off says it takes no
    criteria, instead of a box that would do nothing.
- **Saved finds** ([#34](https://github.com/file4base/file4base-app/issues/34)).
  Several find requests with OR and omit landed in `2779dbe`; what was missing
  was naming a find and running it again. A find is now saved on its table in a
  new `sys_saved_finds` catalog table, listed in **Records > Saved Finds** and
  in the same submenu of **Requests** while the criteria are being typed, and
  run with one click.
  - **The criteria are stored as they were typed** — `>100`, `=Paris`,
    `2011-01-01...2011-06-30` — not as the operators they parse into, so a
    saved find can be opened in Find mode, read and changed, and is parsed
    again every time it runs.
  - **Checked against the catalog before it is stored**, so a saved find cannot
    fail with "unknown field" the first time someone runs it. An empty request
    is dropped rather than stored, since it would find everything.
  - **Saved on the table, not on the account**: everyone who can search the
    table can run it, and whoever saved it — or an admin — can rename or delete
    it. A find on a table an account cannot reach is not even listed to them.
  - **They travel in the solution bundle** and are restored by table and name,
    so importing the same file twice does not make a second copy.
  - New endpoints: `GET`, `POST /api/v1/saved-finds`, `PUT` and `DELETE
    /api/v1/saved-finds/{id}`, also mounted under `/api/v1/schemas/`.
- **Extended privileges are real privileges now**
  ([#39](https://github.com/file4base/file4base-app/issues/39)). Manage Security
  listed four "access methods" — desktop, WebDirect, REST API, bulk export —
  that nothing enforced: the server had no concept of them, so every account
  that could sign in could use all of them. The tab now holds two privileges
  that the server does enforce, and states plainly what File4Base does not
  restrict. [docs/specs/security_model.md](docs/specs/security_model.md) is the
  specification.
  - **Bulk record export** and **bulk record import** are granted per account
    role in a new `sys_privileges` catalog table. Without the privilege,
    `POST /api/v1/data/{table}/export`, `/import`, `/import/preview` and the
    `.f4data` endpoints answer `403 Forbidden`, whatever the caller's table
    access is and whichever client calls them.
  - **A role with nothing stored holds every privilege**, so upgrading a
    database restricts nothing: a restriction only appears because an owner
    asked for one.
  - **An owner always holds every privilege** and the switches say so, so that
    an owner cannot lock themselves out of their own database. Only an owner can
    change the grants; an admin can read them.
  - **The switches store what they show.** Before `e7473a4` the tab had
    switches that wrote to local variables, were never sent to the server and
    were lost when the dialog closed. Each switch is now written as it is moved,
    and applies to sessions that are already open.
  - **Clients are told what their role may do**, in the `capabilities` of the
    sign-in and session responses, so *File > Import/Export Records...* says a
    privilege is withheld instead of letting the request come back refused.
    That is a courtesy; the server checks every call.
  - **The grants travel in the solution bundle** and are restored on import,
    keyed by role.
- **No privilege per access method, and no claim of one.** Every File4Base
  client is a REST client: the desktop application, the Web Client behind Nginx
  and a direct `curl` all call the same endpoints with the same session token,
  so what would identify the client is something the request itself claims and
  anyone with valid credentials can claim it. A switch built on that would read
  as a restriction and behave as a suggestion. The tab, the specification and
  the tutorial now say what is enforced — account role, table and layout,
  these privileges — and why the method is not among them. Binding the method to
  the token at sign-in, with a secret shared with the Web Client's proxy, is
  what building it would take.

### Fixed
- **A layout created without naming a table occurrence took an arbitrary
  one.** The fallback read the first row of `sys_table_occurrences` with no
  order, so which table a layout ended up bound to depended on the engine —
  PostgreSQL and MariaDB disagreed. It is the first occurrence **by name**
  now. Found while testing the Data Viewer against both engines; the client
  always names the occurrence, so this only reached callers of the API that
  left it out.

---

## [0.10.0] - 2026-10-08

### Added
- **Summary fields and reports** ([#32](https://github.com/file4base/file4base-app/issues/32)).
  File4Base could show records and sort them, but not group them, total them or
  produce a report. `SUMMARY` was a field type that mapped to a plain column and
  was never computed, and a layout part could call itself a sub-summary but
  nothing created one, sorted by it or drew it. Lesson 12 of the tutorial is new
  and builds the Annual Fee Report;
  [docs/specs/summary_fields_and_reports.md](docs/specs/summary_fields_and_reports.md)
  is the specification.
  - **Seven kinds of summary**: total, average, count, minimum, maximum,
    standard deviation and fraction of total, with a running-total option. They
    are worked out in SQL over the found set, which is given as the same find
    requests a find takes, so a report totals the records the find returned.
  - **A summary has no value in a record**, so its column is never written and a
    value sent for one is dropped. The same field shows the group's figure in a
    sub-summary and the whole found set's in a grand summary: the field says
    what to total, the band says over what.
  - **Sub-summary and grand summary parts.** A sub-summary names the field it
    breaks on and is drawn once per group; leading or trailing is decided by
    where it sits, as in FileMaker. Several of them nest, opening outermost
    first and closing innermost first.
  - **The designer handles parts properly**: they are addressed by id rather
    than by type, so a report can have more than one sub-summary. The Part tool
    adds them, Part Setup sets the height and the break field, and adding or
    removing a band moves the objects below it so each stays in the band it was
    drawn in.
  - **A layout with summary parts lists its records as a report.** When the
    found set is not in the order the report needs, it says so and offers to
    sort rather than printing nonsense — the found set stays the reader's.
  - Preview draws the report and the printed sheet is that report, **once** over
    the found set rather than once per record, scaled down when the layout is
    wider than the paper so its right-hand columns do not fall off the sheet.
  - A new endpoint, `POST /api/v1/data/{table}/summary`, documented in
    [docs/api/API_REFERENCE.md](docs/api/API_REFERENCE.md).
  - A summary field defined before this release, when the Field Options dialog
    saved a SQL aggregate name, is **read** rather than ignored, so it works
    without being set up again.

- **The layout assistant** ([#33](https://github.com/file4base/file4base-app/issues/33)).
  **New Layout / Report** asked for a name and a table and then made a copy of
  the standard form, whatever you picked — the button said one thing, the
  dialog another, and it made neither. It now asks what kind of layout, which
  fields and in which order, which theme, and the break field or the label
  stock, and builds that. Lesson 13 of the tutorial is new;
  [docs/specs/layout_assistant_and_merge_fields.md](docs/specs/layout_assistant_and_merge_fields.md)
  is the specification.
  - **Five kinds**: form, list, report with grouped data, labels and blank. A
    report is built out of the parts [#32](https://github.com/file4base/file4base-app/issues/32)
    added, with the subtotals placed under the columns they total.
  - **A field picker** whose order is the layout's: the column order of a list,
    the line order of a label. The primary key and the summary fields are not
    offered as columns, because neither belongs in one.
  - **Layout themes** — Enlightened, Cool Grey, Classic and Minimal — which
    decide how the generated layout looks. These are the *layout's* themes, not
    the application themes in Manage Themes, which colour the window; the two
    are different things sharing a word. `theme` on a layout was a string
    nothing read and now means something.
  - **Labels**: Avery L7160, L7163, 5160 and 5163, or a custom size in
    millimetres fitted to an A4 sheet. A labels layout **is one label** — a body
    and no header or footer — and Preview lays them out across and down the
    page, paging the found set into sheets. Printing prints one page per sheet.
  - Both ways into a new layout — the Layout mode button and Manage Layouts —
    used to carry their own copy of the same dialog. There is one assistant now.
- **Merge fields collapse an empty line** ([#33](https://github.com/file4base/file4base-app/issues/33)).
  `{{field}}` in layout text already took the record's value, but a line whose
  fields were empty printed blank, which makes a mailing label or a form letter
  unusable. A line that held merge fields and came out with nothing in it is now
  removed, so a three-line address prints as two. A line carrying words of its
  own is kept — `Phone: {{phone}}` is still `Phone: ` — and a blank line the
  author typed is spacing and stays. A value is also written the way its field
  is shown, so a DATE reads as a date rather than the full timestamp the API
  returns.

- **Charts** ([#37](https://github.com/file4base/file4base-app/issues/37)).
  The Chart Tool was in the layout toolbar and drew nothing: no chart object,
  no setup, no renderer. A chart now draws the figures
  [#32](https://github.com/file4base/file4base-app/issues/32) works out, on a
  layout, in Preview and in print. Lesson 13 of the tutorial is new;
  [docs/specs/charts.md](docs/specs/charts.md) is the specification.
  - **Six kinds**: column, bar, line, area, pie and scatter. A grouped chart
    asks for its own grouping over the found set, so narrowing with a find
    redraws it; its series name summary fields, because a plain field has no
    figure over a group. A **scatter** is the exception — it plots two numbers
    per record and reads the records themselves, leaving out any record missing
    either number rather than drawing it at zero.
  - **Chart Setup**, from a double click on the canvas or the inspector, with a
    preview drawn from made-up figures beside it so the shape can be judged
    without leaving Layout mode. A pie draws one series, so choosing a second
    replaces the first.
  - A chart object is never silently blank: it says whether it has no setup,
    is still reading, has nothing to draw, or is in Find mode. Dragged small it
    drops its title and legend rather than overflowing.
  - `docs/specs/layout_schema.json` gained `chart_config`, and was brought up to
    date with the `portal_config` and `field_binding` of
    [#36](https://github.com/file4base/file4base-app/issues/36), the leading
    grand summary part of [#32](https://github.com/file4base/file4base-app/issues/32)
    and the layout themes and label stock of
    [#33](https://github.com/file4base/file4base-app/issues/33).

- **Importing records from CSV, tab-separated text, Excel and XML**
  ([#38](https://github.com/file4base/file4base-app/issues/38)). File4Base
  could only read its own files, so an existing data set could not be loaded
  into a new database without going through the REST API by hand; all four
  **File > Import Records** entries opened a roadmap dialog. Lesson 16 of the
  tutorial is new;
  [docs/specs/importing_and_exporting_records.md](docs/specs/importing_and_exporting_records.md)
  is the specification.
  - A new `dataio` package parses the formats other programs write. The
    separator of a delimited file is worked out when it is not given, a byte
    order mark is stripped, quoted values keep their commas and newlines, and
    a short row is a row with empty cells. An Excel workbook is read a sheet
    at a time, as the text the sheet shows. XML is read either as a repeating
    element, worked out from the file, or as FileMaker's own `FMPXMLRESULT`.
  - **Everything arrives as text**, and turning it into a value is the
    import's job, so a value that will not convert is reported with the row
    and column it came from rather than quietly becoming zero. An empty cell
    is nothing, never a zero or a blank date.
  - **A date is not guessed**: `03/04/2011` is 3 April or 4 March depending on
    where the file came from, so the dialog asks and the error names the order
    it tried.
  - **A field mapping** with a preview of the file, matching each column to
    the field whose name or label it looks like and leaving the rest out
    rather than guessing. A column cannot be sent to a calculation field,
    whose formula owns its value, or a summary field, which has no value in a
    record.
  - **Add** every row, or **update the records that match** on chosen fields,
    adding or passing over the rows that match nothing. Matching ignores case
    as a find does, and an empty key matches nothing.
  - **One transaction.** The first row that cannot be read or stored takes the
    whole file with it — not even the rows before it are written — and the
    error names the row and the column. The field validation rules apply, and
    calculation fields are worked out for imported records.
  - Size, row and column guards for text and spreadsheets, equivalent to what
    `msgpackguard` gives the MessagePack path. A file read in part says so
    rather than importing quietly.
  - Three endpoints: `POST /api/v1/data/{table}/import/preview`, `/import` and
    `/export`, documented in
    [docs/api/API_REFERENCE.md](docs/api/API_REFERENCE.md).
  - **ODBC is now [#47](https://github.com/file4base/file4base-app/issues/47)**,
    as the issue itself suggested: a network client, a credential store and
    probably a cgo dependency, each worth its own argument. That menu item and
    **Folder...** are shown disabled rather than opening a dialog that does
    nothing.

### Changed
- **Export Records** writes an **Excel workbook** as well as the formats it
  already wrote, and can write the **found set** rather than the whole table.
  The workbook is written by the server, which is the one format needing a
  library ([#38](https://github.com/file4base/file4base-app/issues/38)).
- Preview prints the **found set**, not the whole table. It had always read
  every record regardless of the find, which only became visible once a report
  could be printed ([#32](https://github.com/file4base/file4base-app/issues/32)).

### Fixed
- A summary could not be taken over a **calculation field whose result is a
  number**, because the check read the field's declared type rather than what
  its column stores — so the tutorial's own Annual Fee could not be totalled
  ([#30](https://github.com/file4base/file4base-app/issues/30),
  [#32](https://github.com/file4base/file4base-app/issues/32)).
- A report over records that were not in its order crashed instead of drawing
  with its warning: a group that opened twice gave two bands the same key
  ([#32](https://github.com/file4base/file4base-app/issues/32)).
- An exported DATE came out in Go's default time format
  (`2011-03-01 00:00:00 +0000 UTC`) on PostgreSQL, which returns a time rather
  than text. A date is written as a date on both engines
  ([#38](https://github.com/file4base/file4base-app/issues/38)).
- The two radio buttons choosing what an import does were built with the
  per-tile `groupValue` and `onChanged`, which newer Flutter ignores without a
  `RadioGroup` ancestor, so the taps did nothing
  ([#38](https://github.com/file4base/file4base-app/issues/38)).
- Switching to a layout carrying charts never read their figures, so they sat
  on "Reading the figures…" for good: nothing but fetching records loaded them,
  and switching layouts does not fetch records
  ([#37](https://github.com/file4base/file4base-app/issues/37)).
- The layout inspector's part-height boxes named the three standard bands and
  edited nothing, so a report's sub-summary could not be resized there and a
  sheet of labels was shown as having a header and a footer it does not have.
  They are now one box per band the layout actually has
  ([#32](https://github.com/file4base/file4base-app/issues/32),
  [#33](https://github.com/file4base/file4base-app/issues/33)).

---

## [0.9.3] - 2026-10-07

### Added
- **Related fields and portals** ([#36](https://github.com/file4base/file4base-app/issues/36)).
  A layout can show the other side of a relationship, which until now was stored
  metadata the application never acted on. Lesson 14 of the tutorial is new and
  builds both; [docs/specs/relationships_and_portals.md](docs/specs/relationships_and_portals.md)
  is the specification.
  - A **related field** shows a field of the first matching record —
    `Companies::company_address` on a Customers layout. The inspector's **Table
    Occurrence** picker offers every occurrence a relationship reaches, named by
    the relationship followed to get there, and the Bound Column list follows it
    to that table's fields.
  - A **portal** shows the related records, one row each. The fields in a row
    are the layout objects drawn **inside** the portal, as in FileMaker: its
    first row is the band they are placed in, and every row repeats it. **Portal
    Setup** holds the relationship, the initial row, the row count, the sort
    order, the scroll bar, and whether rows may be added or removed.
  - **Writing through a portal**: typing in a row saves to that related record;
    the empty row at the end creates one with its match field filled from the
    record in hand. The match field belongs to the relationship — a value sent
    for it is overwritten — so a portal row cannot be aimed at another parent.
    Creation is refused with `403` unless the relationship allows it, and Portal
    Setup says so when it does not.
  - **Cascading deletes** are honoured: deleting a record deletes the records
    that belong to it through every relationship whose *Delete related records*
    is set, down the whole chain, with a ring of them deleting each record once.
  - **Sort related records** now means something: it is the order a portal falls
    back to when it asks for none of its own.
  - Two new endpoints, `GET` and `POST /api/v1/data/{table}/{id}/related`,
    documented in [docs/api/API_REFERENCE.md](docs/api/API_REFERENCE.md).
  - Portals and related fields survive export and import: a layout's
    `relationship_id` is rewritten to the id the relationship has in the
    destination, including when the import matched one that was already there.
- **Value lists** ([#31](https://github.com/file4base/file4base-app/issues/31)).
  A field can be filled from a named set of values instead of being typed into.
  **File > Manage > Value Lists...**, which used to be a roadmap dialog, now
  manages them, and lesson 11 of the tutorial turns Customer Type into a pair of
  radio buttons with one.
  - Two kinds: **custom values**, typed one per line, and **from a field**,
    which offers the distinct values a field already holds so the list grows
    with the data (capped at 500).
  - A layout's field object chooses its control — edit box, drop-down list,
    pop-up menu, checkbox set or radio button set — and which list fills it. A
    control whose list is missing or was deleted falls back to an edit box, so a
    layout cannot break. A checkbox set keeps the chosen values in the field one
    per line.
  - Value lists go into the solution file and are matched by name on import, so
    re-importing the same file updates them instead of making copies; a list
    taken from a field has its table and column rewritten to the destination's
    ids.
  - A list offers values; it does not restrict what may be stored. Refusing
    anything else is the "existing value" validation rule, so the two choices
    stay separate as they do in FileMaker. Both the dialog and the tutorial say
    so.
- **Calculation fields are computed** ([#30](https://github.com/file4base/file4base-app/issues/30)).
  The field type and its formula editor existed, but nothing ever evaluated a
  formula: the column was created as plain text and stayed empty. A formula is
  now parsed and evaluated by the server when a record is written, and the
  answer is stored in the field's own column, so a calculation field is sorted
  and found on like any other field. The whole language — grammar, operators,
  the 20 functions, how values are coerced and what is validated — is in
  [docs/specs/calculation_formulas.md](docs/specs/calculation_formulas.md), and
  lesson 10 of the tutorial builds the annual fee of the Favorite Bakery
  example with it.
  - Formulas are evaluated in Go, not translated to SQL, so they give the same
    answer on PostgreSQL and on MariaDB; the suite runs against both.
  - A calculation is stored in the column its result type needs, so a numeric
    result sorts and compares as a number. Changing the result type re-types the
    stored column.
  - Changing a field a formula reads updates the result, and saving a new
    formula recomputes the records already stored instead of leaving them with
    the old answer.
  - A formula may read another calculation field; File4Base works out the order
    to compute them in.
  - A formula that does not parse, calls an unknown function, reads a field the
    table does not have, or would make two calculations depend on each other is
    refused with `422` and the position of the problem. A ring of formulas names
    the fields in it.
  - Calculation fields are no longer typed into: Browse mode shows them filled
    in and marked with a function sign, and a value sent for one through the API
    is ignored.
  - Summary fields are a different problem — an aggregate over the found set —
    and are still not computed; the New Field dialog now says so for that type
    alone ([#32](https://github.com/file4base/file4base-app/issues/32)).
- A hands-on tutorial that builds a complete database from nothing,
  [docs/tutorials/favorite_bakery.md](docs/tutorials/favorite_bakery.md): the
  Favorite Bakery customer list, with its tables and fields, data entry, finds,
  sorting, layouts, preview, a relationship, scripts, accounts and backups. Every
  step was carried out in the application and every figure it quotes is one
  File4Base produced. It ends with the list of things a FileMaker user will look
  for and not find yet, each linked to its issue ([#30](https://github.com/file4base/file4base-app/issues/30)
  calculations, [#31](https://github.com/file4base/file4base-app/issues/31) value lists,
  [#32](https://github.com/file4base/file4base-app/issues/32) grouped reports,
  [#33](https://github.com/file4base/file4base-app/issues/33) layout assistant and merge fields,
  [#34](https://github.com/file4base/file4base-app/issues/34) saved finds,
  [#37](https://github.com/file4base/file4base-app/issues/37) charts,
  [#38](https://github.com/file4base/file4base-app/issues/38) importing foreign files,
  [#39](https://github.com/file4base/file4base-app/issues/39) extended privileges).
  The defects below were all found while writing it.
- Contribution and legal guidance for reference/asset provenance, independent
  UI and tutorial development, and final-artifact license review. These practices
  support the open [IP audit actions](https://github.com/file4base/file4base-app/issues/44);
  they do not constitute legal clearance or resolve historical provenance.
- Trademark attribution and independent-project notices in the README and
  documentation website. Active documentation calls the browser component
  File4Base Web Client; application display labels remain tracked in
  [#8](https://github.com/file4base/file4base-app/issues/8).

### Changed
- Records can be sorted by more than one field ([#35](https://github.com/file4base/file4base-app/issues/35)).
  The Sort dialog now builds a sort order: fields are added, reordered, given a
  direction of their own and removed, and the found-set summary names the whole
  order. Ties on the first field are broken by the next one, so "by Company, then
  by Last Name within each company" can be expressed. The data API takes the
  order in a `sort` parameter, repeatable or comma-separated, with a leading `-`
  for descending (`?sort=company,-fee_paid`); `sort_by`/`sort_asc` still work for
  callers that only sort by one field. Every field in the order is checked
  against the table's registered fields, so an unknown one answers `400` instead
  of reaching the SQL.
- Find mode holds several requests ([#34](https://github.com/file4base/file4base-app/issues/34)).
  The records that match any request are found together, so "in New York **or** in
  London" is one find; a request marked Omit subtracts what it matches from what
  the others found, so "paid in 2011, except in March" is another. The toolbar
  shows "Request n of N" with its own navigation, and the Requests menu — New,
  Duplicate, Delete, Delete All, Include / Omit and Go to Request — now acts on
  that list instead of being disabled. Saved finds are still to come.
- The three options of **Specify Relationship** now name both tables instead of
  saying "this table", which said nothing in a dialog showing two. They act on
  the side the relationship points at — the right-hand one — and that is now
  written down ([#36](https://github.com/file4base/file4base-app/issues/36)).
- The "Extended privileges" tab of Manage Security no longer offers switches that
  do nothing. The four toggles (desktop, WebDirect, REST API, bulk export) wrote
  to local variables, were never sent to the server and were lost when the dialog
  closed, while the tab said it controlled the access methods allowed for each
  privilege set — an owner could switch REST access off and believe access was
  restricted. The tab now lists the planned methods and states that they are not
  enforced yet.
- Lesson 7 of the tutorial describes the Sort dialog as it is, with levels and
  a tie-break, rather than the single-field dialog it replaced in
  [#35](https://github.com/file4base/file4base-app/issues/35).

### Fixed
- An omitting find request was OR-ed into the result rather than subtracting from
  it, so "paid in 2011, except in March" meant "paid in 2011 OR not in March",
  which is almost every record. The requests that match are unioned and the
  omitting ones subtract; with only omitting requests the starting point is every
  record.
- A found set came back in whatever order the engine chose: `ExecuteFind` built
  no `ORDER BY` at all, so the sort options were silently dropped after a find.
- 62 menu commands showed a message saying they had run and did nothing: "Omit
  Record: Temporarily hide current row from found set." appeared while the found
  set stayed exactly as it was, and the same for Unsort, Show Omitted Only,
  Delete All Records, Go to Record, Revert Record, the whole Requests menu, Undo,
  Redo, Cut, Copy, Paste, Select All, the Format menu, the Window menu and
  others. Commands that are implemented are now wired up — Go to Record
  (First/Previous/Next/Last/By Number), Revert Record and Unsort already existed
  in Browse mode but the menu never called them — and the rest are shown
  disabled, so a greyed item now means the command does not exist yet. (The
  fourteen items that opened a "Roadmap Phase N" dialog were already honest and
  are unchanged.)
- Browse mode had no way to commit a record, and called an uncommitted one
  "Saved". A record created with New exists only in the window until it is
  committed, yet the status badge showed a green "Saved" and no menu or toolbar
  item committed it: clicking outside the field does not commit (it does in
  FileMaker), and the only ways out were clicking New again, moving to another
  record, sorting, or a script step. Typing the first record of a new database
  and reloading lost it. There is now **Records > Commit Record**, the badge
  reads "Not committed" until the record reaches the server, and Revert Record
  discards it.
- The first layout of a table labelled every field with its SQL column name:
  New Field asks for a "Field Label" and stores it, but the layout showed
  `home_address_1` instead of "Home Address 1" (Table view showed the label, so
  the same field had two names). The layout now uses the label and still binds to
  the column.
- The first layout of a table always had a 400 pt body, whatever the table held.
  With more than about seven fields the lower fields fell inside the footer part
  and below the end of the layout. The body is now sized to the rows it holds.
- The three "Fields" shortcuts in Manage Database (the chip on a table's row, the
  row menu and the "Manage Fields ->" button) switched to the Tables tab instead
  of the Fields tab, so they looked dead.
- "Add Field" did nothing, silently, when the label or the column name was empty:
  the dialog stayed open, no request was sent and nothing said why. It now says
  what is missing.
- DATE fields were shown and edited as RFC3339 timestamps: a date entered as
  11/11/2011 came back as `2011-11-11T00:00:00Z`, in Browse, List and Table view.
- The Script Workspace invented three scripts for every database that had none —
  `on_invoice_created`, `navigate_to_client_record` and `batch_update_products` —
  presented as the user's own and bound to tables (Invoices, Items, Clients,
  Products) and a layout that did not exist; saving would have written them into
  the database. An empty database now says it has no scripts.
- The Script Workspace name box kept showing the previously selected script's
  name: creating a script opened a tab called "New_Script_4" whose name read
  `on_invoice_created`.
- Every table occurrence was created at the same point of the relationships
  graph, so in a database with more than one table the cards sat exactly on top
  of each other: the graph read "2 occurrences" and showed one, with the
  relationship line hidden in the gap between them. New occurrences are now laid
  out in rows, each taking the first free place, so a table created after
  another was deleted no longer lands on a card that is still there
  ([#36](https://github.com/file4base/file4base-app/issues/36)).
- Toggling **Allow Browse mode entry** or **Allow Find mode entry** in the
  layout inspector rebuilt the field's binding from scratch and silently dropped
  its value list ([#31](https://github.com/file4base/file4base-app/issues/31)).
- Two Spanish strings in the English interface: the sign-in button ("Conectar y
  Entrar") and "Conectar a Servidor Remoto...".
- "1 tables in database" and "1 fields".

---

## [0.9.2] - 2026-10-07

### Fixed
- MariaDB mode could not start a usable application ([#19](https://github.com/file4base/file4base-app/issues/19)). The `mariadb` Compose profile left the API wired to PostgreSQL; MariaDB connection strings were parsed as URLs (`first path segment in URL cannot contain colon`); the system catalog was created with PostgreSQL-only types; and find queries used `CAST(... AS TEXT)`, `ILIKE` and `~`. MariaDB DSNs are now parsed with the MySQL driver's own grammar, the catalog and the find queries are generated per dialect, and `docker-compose.mariadb.yml` runs the whole stack on MariaDB 11 (with `deploy/mariadb/init.sql` granting the API the privileges it needs). The same Go test suite runs against both engines, in CI too.
- Other defects this uncovered on MariaDB: a text primary key could not be created (`BLOB/TEXT column used in key specification`); `DROP TABLE`/`DELETE FROM` quoted identifiers the PostgreSQL way, so deleting a table failed; timestamps came back unparsed; an update that stored the same value reported "record not found"; re-importing a data file counted skipped records as inserted; unique validation rules had no database constraint, so concurrent duplicates both succeeded; and NUMBER and BOOLEAN values read back as `1500.0000000000` and `0` instead of `1500` and `false`.

## [0.9.1] - 2026-10-06

### Fixed
- Preview mode: the left sidebar showed "Page: 1 of 1" and "Margins: 0.5 in", both fixed values that ignored Page Setup. It now shows the paper size and orientation, the real printable area and the four margins, with a Page Setup button, in the same card as the other modes. Which record is shown is reported by the preview's own toolbar.
- Find mode: the left sidebar did not match the rest of the interface. Its controls floated on the background with mixed alignment (a centred notebook drawing above left-aligned text), and it showed a "Requests: 1" counter that was always 1. The Find controls now sit in the same titled card as the Layout selector above them, the Omit checkbox toggles from its label too, the buttons say "Perform Find" and "Cancel Find", and the fake counter is gone. The Perform Find button is now readable in dark mode.

## [0.9.0] - 2026-10-06

### Changed
- Browse mode creates a new record on the server when it is committed (leaving it, New Record again, Commit Records), like FileMaker, instead of storing an empty record at once: validation rules apply to the record as a whole, fields left empty get their auto-enter values, and a rejected record stays open with the reason. Reverting or deleting an uncommitted new record discards it.
- Solution files follow a versioned format shared by the server and the client ([docs/specs/solution_bundle_format.md](docs/specs/solution_bundle_format.md), version 2.0), checked by golden files that each side writes and the other reads ([#5](https://github.com/file4base/file4base-app/issues/5)). File > Save, Save As and Save a Copy now ask the server for the file (`POST /api/v1/solutions/export`), so a saved file is always one that File > Open restores; before, the client wrote its own format, which the server could not import. Files written by the 1.0 client and solution files exported by 1.0 servers are still read.

### Security
- A sign-in that was in progress while an owner changed the account's password or role, disabled it or deleted it could still create a usable session ([#12](https://github.com/file4base/file4base-app/issues/12)). Sessions now carry the account state they were opened with, and every request checks it against the current account: sessions of deleted, disabled, demoted or re-passworded accounts are rejected and revoked, whatever their creation order. The session used to change one's own password stays valid.
- Solution files no longer contain passwords ([#10](https://github.com/file4base/file4base-app/issues/10)): the new-database file stored the owner password with a reversible fixed-key encoding, File Options wrote the remembered sign-in password in plain text, and the export endpoint accepted a password in the URL. None is written any more; a password in an old file is ignored when it is opened, and Open Solution asks for the password instead of prefilling it. The remembered File Options password stays in memory for the session. If you shared solution files saved by earlier versions, change the passwords they contained.
- Restoring a solution no longer widens access ([#11](https://github.com/file4base/file4base-app/issues/11)): accounts lost their disabled state and layout permissions and all received the same password. Files now carry each account's role, active state and layout permissions (never a password); missing accounts are restored disabled with an unknown random password and their permissions, to be enabled by an owner who sets their password. Existing accounts are never changed, and the client no longer recreates accounts itself when loading a file.
- Column options could run SQL with the server's database role ([#1](https://github.com/file4base/file4base-app/issues/1)): `default_value` was pasted into the table DDL as `DEFAULT <text>`. It is now metadata only (a JSON object of field options, or nothing), never part of any SQL statement; anything else, such as an SQL expression, is rejected with `422` when adding or updating a column, and through solution import. Physical columns no longer get a `DEFAULT` clause.
- Reserved and colliding table names ([#2](https://github.com/file4base/file4base-app/issues/2)): an internal table such as `sys_users` could be registered as a user table (making password hashes readable through the data API), and names longer than 63 characters were truncated by PostgreSQL into the same physical table. Table and column names now follow one policy (letters, digits and underscores, at most 63 characters; `sys_`, `pg_` and engine schema names reserved), user tables are created without `IF NOT EXISTS` so an existing table is never adopted (`409` for a catalog name in use), and the data API and data import refuse internal tables even if a catalog row names one. Data import also rejects fields that are not registered for their table before writing any row.
- Malformed import files could exhaust the server's memory ([#13](https://github.com/file4base/file4base-app/issues/13)): the MessagePack decoder allocates from declared collection sizes, and its allocation limit is never applied. Solution and data imports now check the file's structure first (declared sizes against the bytes present, nesting depth, total entries) and reject a malformed file with `400` without allocating or changing anything.
- New Database no longer provisions predictable owner credentials ([#9](https://github.com/file4base/file4base-app/issues/9)): the password field starts empty, an empty username or password is no longer replaced by the database name, and the server rejects owner passwords shorter than 8 characters or equal to the username or the database name. Databases created earlier with `admin`/`admin` or with the database name as password keep working: change that password from the account's Change Password dialog.
- File Options no longer reports a password change as successful when the server rejected it, could not be reached, or the new password was blank ([#20](https://github.com/file4base/file4base-app/issues/20)). It now uses the shared Change Password dialog, and the remembered sign-in password changes only after the server accepted the new one.

### Fixed
- Field validation rules were saved but never enforced ([#15](https://github.com/file4base/file4base-app/issues/15)). Record writes now apply not empty, unique, existing value, strict type, range, maximum length and custom messages; violations answer `422` with the field and rule, and nothing is written. Unique rules get a unique index, so concurrent duplicates cannot both succeed. Data import applies the rules whose timing is "Always". Malformed rules are rejected when saved.
- Editing records right after adding a field could fail with "cached plan must not change result type": record reads used `SELECT *`, whose prepared statement PostgreSQL refuses once the table has another column. Reads now name the table's registered fields.
- The PostgreSQL restore script reported success after SQL errors and restarted the API into a partially restored database ([#21](https://github.com/file4base/file4base-app/issues/21)). It now checks the archive before any downtime, aborts if the API cannot be stopped, stops at the first SQL error (only the statements recreating its own role are skipped), leaves the API stopped after a failed restore, starts it again only if it was running, and reports each outcome with its own exit status. Backups are only kept when `pg_dumpall` completed.
- Solution restore lost relationships and custom table occurrences and bound layouts to the wrong table ([#3](https://github.com/file4base/file4base-app/issues/3)). The import now validates the whole file first, matches existing objects by name, rewrites every reference (occurrences, relationship fields, layout tables, script steps, permissions, and the scripts run by buttons and triggers) to the destination's IDs, and, if a write fails, removes what it created and restores what it updated. Importing the same file twice creates nothing new. Creating a layout for a table occurrence that does not exist now fails instead of using another table. The response reports what was created and updated.
- Solution files did not include scripts ([#17](https://github.com/file4base/file4base-app/issues/17)). Scripts and their ordered steps (parameters, disabled steps, nesting) are now saved and restored.
- Data import reported rejected records as restored ([#4](https://github.com/file4base/file4base-app/issues/4)). It now restores all records in one transaction: the first record the database rejects rolls everything back and is reported with its table and position. Records whose id already exists are counted as skipped, and tables the database does not have are listed (`records_inserted`, `records_skipped`, `skipped_tables`).
- CONTAINER (binary) values were exported as text and could not be restored ([#14](https://github.com/file4base/file4base-app/issues/14)). Data files keep them as MessagePack binary, byte for byte, and the JSON data API returns and accepts them as base64.
- Field options broke table duplication and left an incomplete copy ([#16](https://github.com/file4base/file4base-app/issues/16)): the options JSON reached the DDL as a default. Duplication now copies columns and options, and removes the copy if a column cannot be copied. A constant auto-enter value ("Data" in the Fields dialog) now fills new records that do not supply the field; an explicit value still wins.
- Export Records saved only the first 100 records ([#6](https://github.com/file4base/file4base-app/issues/6)): it asked for 10,000 rows, which the server silently replaced by its 100-row default. Exports now read every record page by page (in `id` order) and save the file only when all pages arrived. The data API clamps `limit` to its 1,000-row maximum instead of falling back to 100.
- JSON numbers sent to the data API kept only float64 precision ([#18](https://github.com/file4base/file4base-app/issues/18)): `9007199254740993` was stored as `9007199254740992`. Numbers now reach the database exactly as written, on create and update.
- Creating a database from the sign-in dialog filled the username with the database name ([#23](https://github.com/file4base/file4base-app/issues/23)); it now fills the owner username.
- Script Workspace Run and Debug showed a successful execution with invented results without running anything ([#24](https://github.com/file4base/file4base-app/issues/24)). Run is disabled until the workspace can run scripts (scripts run from layout buttons in Browse mode), and Debug is now Step Preview, labelled as a preview that executes nothing. The footer no longer names a script engine that does not exist.

### Legal
- The vendored Swagger UI 5.33.0 ships with its Apache-2.0 `LICENSE` and `NOTICE` and the license files referenced by its script banners, served next to the scripts; `VENDOR.md` records the version and checksums ([#7](https://github.com/file4base/file4base-app/issues/7)).
- The API image contains `/usr/share/doc/file4base/THIRD_PARTY_NOTICES` with the license texts of Go and of every Go module linked into the server, generated at build time from the binary (`server/tools/third_party_notices.sh`); the build fails if a module has no license file ([#22](https://github.com/file4base/file4base-app/issues/22)).

## [0.8.0] - 2026-10-06

### Added
- Layout mode: with no object selected, the inspector shows the layout's own properties: width and height (and the header, body and footer heights), background color, background picture (fill, fit, stretch or tile), OnLayoutEnter / OnLayoutExit script triggers and a transition effect (fade, slide or zoom). New layouts get them too. Browse mode paints the background, plays the transition when a layout is shown and runs the triggers: OnLayoutEnter when the layout is shown, OnLayoutExit then OnLayoutEnter when going to another layout. Preview mode paints the background as well.
- Layout mode: select several objects with Shift-click or by dragging a selection rectangle on the empty canvas. Selected groups can be dragged, nudged with the arrow keys, duplicated and deleted together.

### Fixed
- Layout mode: the canvas kept a blank area below the footer that could not be removed (it was always at least 600 pt tall). The layout now ends at the bottom of the footer, or of the lowest object. Browse and Preview modes also use the layout's width and height instead of a minimum size.
- Layout mode: the resize handles of a selected object did not resize it, and Shift-click did not add to the selection. A mouse press held longer than 100 ms made the canvas deselect everything even when the click landed on an object or a handle; the canvas now only deselects on a click on empty space. The handles are also larger and fully clickable (half of each one sat outside the object's hit-test area).

### Changed
- Docker images moved to the `cloudresources` Docker Hub organization: `cloudresources/file4base-api` and `cloudresources/file4base-web` (all published versions were copied). `docker-compose.yml` and `scripts/publish_images.sh` use it by default; `FILE4BASE_REGISTRY` still overrides it.

## [0.7.1] - 2026-10-06

### Fixed
- Preview mode "Export PDF / Print" printed a capture of the whole browser window (toolbars included) instead of the previewed page, and did nothing in the desktop app. It now builds a PDF of the previewed sheet only, at the Page Setup paper size and orientation, and opens the print dialog with it (web and desktop). Content taller than one sheet continues on the next pages.
- File > Print prints the page shown in Preview mode in the same way.
- Preview mode cut off layouts wider than the paper (and printed them cut off); they are now scaled down to fit the printable width.
- Script Workspace: closing with unsaved scripts asks again to save them (the confirmation was dropped while merging the English UI change).

### Added
- Preview mode "Print / Export PDF" menu: print this page, print all records (one sheet per record), and save this page or all records as a PDF file.
- Client dependencies `pdf` and `printing`.

### Changed
- Use English throughout the client interface, including security, scripts, relationships, themes, file options, page setup, and help dialogs.
- Set English as the default client locale regardless of the host language, including standard Material controls.

## [0.7.0] - 2026-10-06

### Added
- Button Setup window in Layout mode, opened with a double click on a button, Enter, right click > Button Setup... or the inspector: set the label and choose what a click runs in Browse mode (nothing, a single step or a script). "New script..." creates a script, assigns it and opens the Script Workspace; "Edit scripts..." reloads the list when the workspace closes.
- Layout mode canvas shows under each button the action it runs ("Perform Script: New contact"), and objects have a context menu (Button Setup, Edit text, Duplicate, Bring to front, Send to back, Delete).
- Browse mode record bar: first, previous, next and last record, an editable "Record N of M" box, a slider, the found set summary ("N found of M · Sorted by ...") and the save status.
- Browse mode actions: New, Duplicate, Delete (with confirmation), Find, Sort and Show All; Records > Sort Records... opens the same sort dialog.
- Browse mode List and Table views (Table: click a header to sort, double click a row to open it in Form view).
- `Cmd/Ctrl+↑` / `Cmd/Ctrl+↓` move to the previous / next record in Browse mode.
- Docker images are also published for `linux/arm/v7` (Raspberry Pi with a 32-bit OS), besides `linux/amd64` and `linux/arm64`.

### Changed
- The status sidebar no longer shows the notebook, record counter and slider in Browse mode (they moved to the record bar), and the duplicate record header above the form was removed.
- The inspector Data tab of a button shows a summary of its action and a Button Setup... button instead of inline action fields.

### Fixed
- Records > Duplicate Record created an empty record instead of duplicating the current one.
- Dragging the record slider could save field edits into the wrong record.

## [0.6.1] - 2026-10-06

### Fixed
- Scripts created in the Script Workspace did not appear in the button "Perform Script" list: the layout designer loaded the list once and never refreshed it. It now reloads when a button is selected and when the Script Workspace is closed (from the inspector, the Manage menu or the Scripts menu), and shows load errors instead of "No scripts yet".
- Script Workspace: saving created a new copy of every new script on each save, because scripts kept their local id after the server assigned another one. Created scripts now adopt the server id.
- Script Workspace: closing the window with unsaved scripts discarded them silently; it now asks to save, discard or cancel.

## [0.6.0] - 2026-10-06

### Added
- Layout mode: Line, Rectangle, Rounded Rectangle and Oval are drawn by dragging on the canvas (a click still places a default size). Lines run horizontally or vertically along the longer side of the drag.
- Layout mode: line (border) color swatches and hex field, line width slider, fill "none", and text color swatches in the inspector; the status sidebar stroke control sets the line width of the selected object and of new drawings.
- Layout mode: text inside rectangles, rounded rectangles and ovals, edited in place with a double click or Enter (Esc cancels); labels accept several lines.
- Layout mode: Insert > Picture, PDF, Audio/Video, QuickTime and File embed the file (up to 2 MB) inside the selected shape or in a new `media` object; pictures can fit, fill-and-crop or stretch.
- Layout mode: Insert > Current Date, Current Time, Current User Name, Page Number and Merge Field add merge symbols (`{{CurrentDate}}`, `{{field_name}}`, ...) to the selected text or a new label; Browse and Preview modes show their values.
- Button setup in the inspector Data tab: a button runs a single step (New, Duplicate, Delete, Commit or Revert Record, Go to Record, Enter Find Mode, Perform Find, Show All Records, Enter Preview Mode, Go to Layout, Set Field, Show Custom Dialog, Open URL) or performs a stored script with an optional parameter.
- Client-side runner for button actions and Perform Script (record, navigation, Set Field, dialog and URL steps). Steps that need the calculation engine (`If`, `Loop`, `Set Variable`, REST calls) stop the script with a message.
- Set Tab Order tool (layout toolbar and status sidebar, replacing the Tab Control and Rotate tools): click fields and buttons in Tab key order, or number them automatically in reading order. Browse and Find modes follow that order.
- Layout mode keyboard: arrow keys move the selected object (Shift: 8 pt), Esc returns to the pointer.
- Layout JSON schema: `rect`, `rounded_rect`, `oval`, `line` and `media` objects and the `text`, `tab_order`, `action` and `media` properties.

### Fixed
- Tools picked in the status sidebar palette (Line, Rectangle, Oval, ...) did nothing: the selection never reached the layout designer.
- Typing in the inspector text fields reset the cursor and lost input on every rebuild, and Backspace/Delete deleted the selected object while typing.
- The border color could only be typed as hex, the "None" fill swatch did not clear the fill, and malformed colors broke the canvas.
- Ovals were drawn as circles, and lines, ovals and shapes rendered as plain rectangles in Browse and Preview modes.
- Button text color and size ignored the object style; buttons only acted according to words in their label (kept for buttons without an action).
- Quick consecutive inspector edits (a color, then a width) could undo each other.

## [0.5.1] - 2026-10-06

### Added
- Docker Hub distribution: `marioezquerro/file4base-api` and `marioezquerro/file4base-web` multi-arch images (`linux/amd64`, `linux/arm64`), published with `scripts/publish_images.sh` under the version tag and `latest`.
- `scripts/backup_postgres.sh` to back up and restore every PostgreSQL database of the `file4base-postgres-data` volume (`backup`, `list`, `restore`).

### Changed
- Docker images are tagged with the application version instead of `latest`; `docker-compose.yml` references the Docker Hub images (`docker compose pull`) and still builds them locally with `--build`. `FILE4BASE_VERSION` and `FILE4BASE_REGISTRY` override the tag and namespace.
- Dockerfiles cross-compile on the build host's platform, so multi-arch builds need no emulation.
- The web client image is renamed from `file4base-app-web` to `file4base-web`, following the `file4base-` naming convention.
- `scripts/bump_version.sh` also synchronizes the About dialog, the OpenAPI spec and the image tags in `docker-compose.yml`.
- Documented data persistence of the PostgreSQL volume and the backup workflow.

## [0.5.0] - 2026-10-06

### Security
- **API authentication (breaking)**: every schema, data, security and solution endpoint now requires a session. `POST /api/v1/auth/login` returns a bearer token (`Authorization: Bearer <token>`); added `POST /api/v1/auth/logout` and `GET /api/v1/auth/session`. Only the health probes, Swagger UI, sign-in and the database selector remain public.
- **Server-side authorization**: `owner` / `admin` / `user` roles are enforced on every route. Per-layout permissions are enforced by the server too (hidden layouts, layout saving, and record access through the data API), instead of only in the client.
- **Per-session database (breaking)**: removed the server-wide "active database". A session is bound to the database it signed in to, so one client can no longer redirect the requests of another. `POST /api/v1/databases/switch` is deprecated and only reports whether a database exists.
- **Data API restricted to user tables**: `/api/v1/data/{table}` only serves tables registered in `sys_tables` and fields registered in `sys_columns`. System tables such as `sys_users` (password hashes) are no longer reachable. Data imports (`.f4data`) are restricted to catalog tables as well.
- **Login requires a matching username and password**: removed the fallback that accepted any owner's password regardless of the username. Response timing no longer reveals whether a username exists.
- **No default accounts**: removed the automatic `admin` / `admin` owner. `POST /api/v1/databases` requires the credentials of the first owner, returns `409` for an existing database instead of adding an owner to it, and can be limited to signed-in owners with `ALLOW_PUBLIC_DATABASE_CREATION=false`.
- **Dropping a database requires proof of ownership**: an owner session on that database, or the credentials of one of its owners.
- Sessions are revoked when a user's password, role or status changes, when the user is deleted, and when the database is dropped. The last active owner can no longer be demoted or deactivated.
- Explicit `none` layout permissions are now stored (previously they were dropped and silently became `read_write`).
- CORS origins are configurable through `CORS_ALLOWED_ORIGINS`; solution and data imports are size-limited; engine-internal databases (`postgres`, `mysql`, ...) cannot be signed in to, created or dropped through the API.

### Added
- `internal/auth` session store and `api.Mount` route assembly with public and protected groups.
- Client: the API client keeps the session token, sends it on every request, signs out on the server, and locks the workspace when the session expires.
- Client: the "Drop Database" confirmation asks for the owner credentials of the databases being dropped.
- CI: new `test-server` job running `go vet` and the Go test suite against a PostgreSQL service; releases depend on it.
- `.env.example` and environment-driven credentials, bind address and server options in `docker-compose.yml`.
- End-to-end API tests covering authentication, database isolation, roles, layout permissions and the data API restrictions.

### Changed
- `client/Dockerfile` is now multi-stage and builds the Flutter web bundle itself, so `docker compose up` works on a clean clone.
- Database ports in `docker-compose.yml` are published on `127.0.0.1` by default.
- Health responses no longer expose `active_database` and report the configured engine.
- Removed the hard-coded `dev_password` defaults from the solution export endpoint and the client's connection model; the server warns when it falls back to the development DSN.
- Manage Database: the "switch" action explains that another database is opened by signing in to it; creating a database requires an owner user and password (no `admin` / `admin` prefill).
- Opening a solution file signs in first and imports afterwards; packaged users are synchronized after sign-in.
- Updated client and roadmap documentation to match the implemented application and current feature status.
- Clarified empty-database startup behavior and existing Docker volume persistence in the deployment guide.
- Aligned database login empty-state messages with the repository's English language policy.
- Clarified that `AGENTS.md` and its referenced rules are authoritative over overlapping Antigravity guidance.
- Documentation aligned with the code: Go 1.26, repository structure, WebSockets and the calculation engine marked as planned, OpenAPI spec updated with the bearer scheme and the auth and database endpoints.

### Removed
- Unused duplicate `messagepack` Dart dependency (`msgpack_dart` is the one in use).

### Fixed
- `DROP DATABASE` used a hard-coded `postgres` administrative connection; it now uses the configured administrative database.
- The Go test suite no longer depends on pre-existing accounts or on the order in which packages run.

## [0.4.26] - 2026-10-01

### Changed
- **Zero-State Clean Database Initialization & Connection**:
  - Removed default database `file4base_dev` from `docker-compose.yml`, starting PostgreSQL purely with administrative database `postgres` and 0 application databases.
  - Initialized `ListDatabases` in DBAL with an empty slice `[]` (instead of null JSON) when no user databases exist.
  - Removed `file4base_dev` pre-selection in `DatabaseLoginDialog`: displays placeholder *"Selecciona una base de datos"* or *"Ninguna base de datos disponible"* when no databases exist.
  - Added strict validation preventing login without selecting or creating a database via the `+` button or importing an `.f4p` solution file.
  - Cleaned up all fallback references to `file4base_dev` in solution models, status bar, and security dialogs.

## [0.4.25] - 2026-10-01

### Added
- **File Menu Quit Action with Auto-Save Safety**:
  - Added `Quit` action at the bottom of the `File` menu (with shortcut `Cmd+Q` / `Ctrl+Q`).
  - Automatically commits pending layout edits when in Layout Mode.
  - Flushes debounced auto-save immediately to persist all solution configuration and metadata to disk.
  - Prompts a confirmation dialog before terminating the process on Desktop or closing the tab/window in WebDirect mode (`SolutionStorageService.triggerQuit()`).

## [0.4.24] - 2026-10-01

### Fixed
- **Desktop Environment Preflight & Database Engine Validation**:
  - **macOS Docker App & Socket Detection**: Enhanced `EnvironmentChecker` to inspect symlinks, check `/Applications/Docker.app`, and detect `/var/run/docker.sock` and `~/.docker/run/docker.sock` so Docker Desktop is recognized even if CLI binaries are not in minimal GUI launcher `PATH`.
  - **macOS App Sandbox Disabled for Low-Code Docker Orchestration**: Disabled `com.apple.security.app-sandbox` in `Release.entitlements` and `DebugProfile.entitlements` so that standalone macOS desktop builds distributed via DMG/ZIP can execute Docker CLI commands and communicate with the local Docker daemon socket.
  - **Single Active Database Engine Validation**: Corrected the preflight check so that having either PostgreSQL OR MariaDB active (with the API and Web containers or healthy `/healthz` returning 200) satisfies startup requirements without failing or demanding both engines concurrently.
  - **Automated Docker Desktop & Container Launch**: Added `startDockerAndContainers` in `EnvironmentChecker` and integrated it with `PreflightDialog`. When Docker Desktop is closed or stopped, users can click "Launch Docker Desktop" to automatically launch Docker Desktop, poll for the daemon to become responsive, and start the project containers (`postgres`, `api`, `web`) without requiring manual terminal intervention.
  - **Container State Discovery**: Added `checkProjectContainers` using `docker ps` to verify running container state (`file4base-postgres` or `file4base-mariadb` + `file4base-api` + `file4base-web`) so the desktop application immediately identifies ready local services.
  - **Dynamic Engine Status**: Updated desktop status bar and authentication workflow to dynamically show the active database engine (`PostgreSQL` or `MariaDB`) rather than assuming PostgreSQL.

## [0.4.23] - 2026-10-01

### Changed
- **File Menu Streamlining & Enhancements**:
  - **Removed "Close"**: Removed the redundant "Close" option from the `File` menu.
  - **Removed "Sharing" Submenu**: Cleaned up the non-functional `Sharing` submenu (`Share with File4Base Clients...`, `Enable File4Base WebDirect...`, `Share with ODBC/JDBC...`).
  - **Removed "Save/Send Records As" Submenu**: Removed the non-functional `Save/Send Records As` submenu (which contained Excel, PDF, Snapshot Link placeholders).
  - **Active User Password Modification**: The `File -> Change Password...` menu option now opens a dedicated `ChangePasswordDialog` that allows the authenticated user to update their account password directly on the active database.
  - **Comprehensive Record Export**: The `File -> Export Records...` menu option now launches an `ExportRecordsDialog` supporting export to standard formats: CSV, TSV/Excel, JSON, XML, and HTML table with optional header fields.
  - **Native Platform Printing**: Wired `File -> Print...` and the preview toolbar's `Export PDF / Print` button to trigger native browser printing (`window.print()`) in WebDirect mode and platform preview printing.
  - **Universal Password Visibility Toggles**: Added interactive visibility toggles (`Icons.visibility` / `Icons.visibility_off`) to all password fields across the application, allowing users to temporarily reveal or conceal entered passwords in:
    - `DatabaseLoginDialog` (Main database authentication & login screen).
    - `ManageSecurityDialog` (User account creation/edit, duplicate account dialog, and change password dialogs).
    - `ChangePasswordDialog` (New password and confirm password fields).
    - `NewDatabaseDialog` (New solution database owner password).
    - `OpenSolutionDialog` (Server connection password and encrypted file credentials).
    - `ManageDatabaseDialog` (Create database on server modal).
    - `FileOptionsDialog` (Auto-login credentials and change password confirmation).

### Added
- **Manage Database Global "Databases" Tab & Lifecycle Management**:
  - Added a new primary **Databases** tab in `ManageDatabaseDialog` displaying all physical databases registered on the PostgreSQL / MariaDB server.
  - Implemented multi-database selection with checkboxes, master header checkbox ("Select All" with tristate support), and real-time selected database count badge with quick-clear button.
  - Added individual database actions:
    - **Active (Connected) badge**: clearly distinguishes the currently connected database context.
    - **Switch**: quickly switch active database context directly from the table.
    - **Drop Database**: single database deletion with safety checks.
  - Added batch database operations:
    - **Drop (N) Selected...**: permanently drop multiple selected databases in a single operation, protected by a consolidated safety confirmation dialog.
    - Protected system and development databases (`postgres`, `file4base_dev`) and active database from accidental deletion.
  - Added **New Database...** quick action button to create databases on the server directly from the dialog with initial owner credentials.
  - Added backend database deletion:
    - Implemented `MultiDatabaseManager.DropDatabase(ctx, dbName)` with session termination (`pg_terminate_backend`) and connection pool cleanup.
    - Registered `DELETE /api/v1/databases/{name}` REST API endpoint in `SolutionHandler`.
    - Added `deleteDatabase` method to Flutter `ApiClient`.

## [0.4.21] - 2026-09-30

### Fixed
- **Strict Database and Solution Isolation for Security Users**:
  - Fixed an issue where creating a new database or switching database context automatically provisioned development default accounts (`admin`, `file4base`, `file4base_dev`) into `sys_users`.
  - In `server/internal/schema/service.go`:
    - Removed automatic provisioning of development credentials (`file4base`, `file4base_dev`).
    - When a new database is created with an explicit initial owner, only that specific owner user is provisioned.
    - Default fallback to `admin` only occurs if `sys_users` is entirely empty and no owner was requested.
  - In `client/lib/features/solution_manager/new_database_dialog.dart`:
    - Added the `user` property to `NewDatabaseDialogResult` and configured the initial `SolutionPackage` to only include the designated owner user for that new database/solution.
  - In `client/lib/main.dart`:
    - Fixed `_handleNewDatabase` authentication logic to log in using the selected owner username (`result.user`) instead of accidentally using the database name (`result.databaseName`).
    - Fixed solution export so that it strictly exports the active database's user roster without injecting foreign accounts.
  - Verified that switching databases and accessing **Manage Security** only queries and displays the users belonging to the active database.

## [0.4.20] - 2026-09-30

### Added
- **Manage Database Multi-Table Selection & Batch Operations**:
  - Added checkboxes to the **Tables** tab in `ManageDatabaseDialog` allowing multi-selection of database tables.
  - Added a **Select All** header checkbox with tristate support (selected, unselected, partially selected) and a real-time selection badge showing the number of selected tables with a quick-clear button.
  - Added batch operations in the bottom action bar:
    - **Batch Duplicate (`Duplicate (N)`)**: duplicates multiple selected tables in one step.
    - **Batch Truncate / Empty (`Empty (N)...`)**: empties records from all selected tables while preserving table structures and columns, with a consolidated warning dialog.
    - **Batch Delete (`Delete (N)...`)**: permanently drops multiple selected tables and their columns in a single confirmation flow.
  - Single-table actions (`Rename...` and `Manage Fields ->`) gracefully adapt when single or multiple tables are checked.

### Added
- **GitHub Pages Documentation Website (`https://file4base.github.io/file4base-app/`)**:
  - Published comprehensive single-page documentation app at `docs/index.html` featuring responsive sidebar navigation, real-time search, dark modern theme, and detailed guides for all 4 operational modes, schema management, calculations, script workspace, database-isolated security, and REST API.
  - Added automated CI/CD deployment workflow `.github/workflows/deploy_docs.yml` using `actions/deploy-pages@v4` to continuously build and publish documentation upon pushes to `main`.
- **Help Menu Activation & Interactive Service & Support Guide**:
  - Activated all Help menu items in `File4BaseMenuBar`:
    - `File4Base Help`: opens the official GitHub Pages documentation website.
    - `Resource Center`: opens documentation tutorials and template guides.
    - `Product Documentation`: links directly to API references and architecture specifications.
    - `File4Base Community`: navigates to GitHub Discussions for community collaboration.
    - `Service & Support...`: presents new modal `IssuesGuideDialog` educating users on best practices (searching existing tickets, title conventions, step-by-step reproduction, version/environment logs, and credential privacy) before opening GitHub Issues.
    - `Check for Updates...`: opens `CheckUpdatesDialog` displaying current version (`v0.4.19`) with release links.
    - `About File4Base`: opens detailed system information and credits modal.

### Fixed
- **macOS Desktop Connectivity & App Transport Security (ATS)**:
  - Added `NSAppTransportSecurity` to `client/macos/Runner/Info.plist` with `NSAllowsArbitraryLoads` and `NSAllowsLocalNetworking` enabled, preventing macOS network security from blocking cleartext HTTP requests to `http://localhost:8080`.
  - Added `com.apple.security.network.server` and `com.apple.security.files.user-selected.read-write` to `Release.entitlements` and `DebugProfile.entitlements` for smooth local networking and file operations.
  - Fixed false-positive Docker errors in macOS GUI launches: `EnvironmentChecker` now scans standard installation paths (`/usr/local/bin/docker`, `/opt/homebrew/bin/docker`, etc.) since macOS GUI apps do not inherit shell `$PATH`.
  - Added fast-path health check to `EnvironmentChecker`: if the File4Base API server is already running and healthy, preflight checks pass immediately without requiring host Docker CLI invocations.

### Changed
- **Docker Infrastructure Naming Convention Enforcement**:
  - Enforced mandatory `file4base-` prefix across all containers, images, volumes, and networks.
  - Renamed backend container and image to `file4base-api` in `docker-compose.yml`, `docs/specs/ARCHITECTURE.md`, and `AGENTS.md`.

## [0.4.18] - 2026-09-30

### Fixed
- **Unauthenticated Access Leak on Login Cancellation**:
  - Fixed security vulnerability where dismissing or cancelling the initial database authentication dialog allowed unauthenticated access to Browse Mode, exposing database tables, field schemas, and records.
  - Implemented strict unauthenticated state handling: cancelling authentication immediately purges all in-memory database context (`_tables`, `_selectedTable`, `_activeLayout`, `_serverLayouts`, and sets `_currentUser = null`).
  - Protected all table loading and background health checks so unauthenticated sessions are completely blocked from querying table schemas or records.
  - Added dedicated Protected Workspace interface displaying a security shield badge, active database indicator, and quick actions (`Sign In to Database`, `Open Solution (.f4p)`, `New Solution...`, `Connect Remote Server...`).
  - Added session locking actions in `File4BaseMenuBar`: toggles between `Sign In...` and `Sign Out (Lock Session)`. All mode changes and database management dialogs now require an active authenticated session.

### Changed
- **Database & Solution-Scoped User Security (`Manage > Security`)**:
  - Replaced global user sharing with strict per-database user isolation. Accounts and permissions now belong exclusively to the specific database/solution and are non-transferable across databases.
  - Backend `SecurityHandler` now routes all user administration (`ListUsers`, `CreateUser`, `UpdateUser`, `DeleteUser`, `GetUserPermissions`, `SetUserPermissions`) directly to the target database driver resolved via `?database=...` query parameter, `X-Database-Name` header, or payload body.
  - Solution Package (`.f4p`) synchronization:
    - `ExportSolution` exports user accounts from `sys_users` into the MessagePack solution bundle (`bundle.Users`).
    - `ImportSolution` syncs bundle users into the target database's `sys_users`.
    - User mutations in `ManageSecurityDialog` trigger `AutoSaveService.instance.markDirty()` so edits persist to the active `.f4p` solution file.
  - Client `ApiClient` and `ManageSecurityDialog` now pass `database` parameter across all security requests.

## [0.4.17] - 2026-09-30

### Added
- **FileMaker Pro Layout Mode Studio Redesign (`OperationalMode.layout`)**:
  - **Eliminated Duplicate & Unwired UI**: Removed narrow 120px browse sidebar during Layout Mode to prevent duplicate tools and reclaim full-screen canvas width.
  - **Top Bar 1 (Layout Tools Palette Ribbon)**:
    - **`[ + ] New Layout / Report`**: Dedicated shortcut to trigger the layout creation workflow.
    - **Centered Ribbon with 16 Interactive Tools**: Selection Arrow (`Pointer`), Text Tool (`T`), Line Tool (`\`), Rectangle (`▢`), Rounded Rectangle (`▢`), Oval (`○`), Field/Control Tool, Button Tool, Popover Button Tool, Button Bar Tool, Tab Control Tool, Portal Tool, Chart Tool, Web Viewer Tool, Part Tool, and Format Painter Tool.
    - **Manage Dropdown Menu**: Direct access to Database, Layouts, Security, Script Workspace, and Themes.
    - **Show/Hide Panes Toggles**: Quick controls to toggle the Left Fields/Objects Pane and the Right Inspector Pane.
  - **Top Bar 2 (Layout Context Bar)**:
    - **Layout Selector & Quick Rename**: Dropdown listing all server layouts with pencil shortcut to rename inline.
    - **Table Occurrence Indicator**: Pill badge showing underlying table occurrence context.
    - **Theme Selector**: Dropdown to switch themes (`Enlightened`, `Enlightened Touch`, `Enlightened Print`, `Minimalist`).
    - **Snap 8px Grid & Quick Controls**: Snap-to-grid toggle, Undo / Redo history stack, auto-save status indicator dot.
    - **Prominent `[ Exit Layout ]` Button**: Green-accented button that flushes pending changes, commits layout to API, and seamlessly returns to Browse Mode.
  - **Left Pane (Fields & Objects Panel)**:
    - **Fields Tab**: Table occurrence picker, real-time search filter, A-Z sort toggle, field rows with data type badges (`TT`, `#`, `📅`, `⏱`, `⏱📅`, `🖼`, `fx`, `∑`).
    - **Direct Drag & Drop**: Dragging fields drops them directly onto the canvas at precise cursor coordinates.
    - **`[ + New Field ]` Creator**: In-place modal dialog to add new database columns via REST API without leaving Layout Mode.
    - **Collapsible Drag Preferences**: Customization for Field Placement (Horizontal vs Vertical), Include Label toggle, and Control Style (Edit Box, Dropdown, Popup Menu, Checkbox Set, Radio Set, Calendar).
    - **Objects Tab**: Hierarchical tree of canvas objects grouped by part (`HEADER`, `BODY`, `FOOTER`) with selection, z-order, and delete actions.
  - **Interactive Canvas with Dynamic Part Divider Resizing**:
    - **Left Gutter Part Tabs**: Vertical tabs for `Header`, `Body`, and `Footer` with double-click "Part Setup" dialog for exact point height entry.
    - **Draggable Part Divider Lines**: Horizontal divider lines with handle tabs between Header, Body, and Footer allowing interactive dragging with live point height tooltip badges (`Header: 80 pt`, `Body: 520 pt`, `Footer: 50 pt`).
    - **8-Handle Interactive Object Resizing**: Top-left, top-center, top-right, middle-right, bottom-right, bottom-center, bottom-left, middle-left resize handles with live bounds update and 8pt grid snapping.
  - **Right Inspector (4 Tabs)**:
    - **Position & Geometry**: Coordinates (Left, Top, Right, Bottom), Dimensions (Width, Height), FileMaker 4-pin autosizing anchor box, Arrange & Align tools (Left, Center, Right, Top, Middle, Bottom, Bring Front, Send Back).
    - **Appearance & Styles**: Fill color palette swatches and custom hex code input, border color, border width, and corner radius slider.
    - **Data Binding**: Table occurrence, bound column picker, control style, and Browse/Find entry permission checkboxes.
    - **Typography & Text**: Display text editor, font family, font size, bold toggle, text color, and alignment options (Left, Center, Right, Justify).
  - **Automated Tests**:
    - Added `client/test/layout_designer_filemaker_studio_test.dart` asserting all tools, left pane tabs, canvas parts, object selection, and inspector tab switching (54 client tests passing).

## [0.4.16] - 2026-09-25

### Added
- **Table Operations in Schema Manager (`File > Manage > Database > Tables`)**:
  - Added dedicated action buttons for each table row in `ManageDatabaseDialog`:
    - **Fields Shortcut (`Icons.view_column_outlined`)**: Quick navigation to inspect and manage fields for the selected table.
    - **Rename Table (`Icons.edit_outlined`)**: Dialog allowing editing of human-readable display names with immediate schema synchronization.
    - **Duplicate Table (`Icons.copy_outlined`)**: Creates a clone of table definitions including non-primary-key columns, field types, and calculation formulas with automatic `_copy` naming.
    - **Empty Table (`Icons.cleaning_services_outlined`)**: Truncates all row data from the physical table while preserving schema structure, with safety confirmation prompt.
    - **Delete Table (`Icons.delete_outline`)**: Permanently drops the physical database table and cascades removal across system catalogs (`sys_tables`, `sys_columns`, `sys_table_occurrences`), with strict confirmation dialog.
- **Backend Table REST APIs**:
  - `DELETE /api/v1/schemas/tables/{id}`: Drops physical table and cleans up catalog entries.
  - `PUT /api/v1/schemas/tables/{id}/rename`: Updates table display name.
  - `POST /api/v1/schemas/tables/{id}/duplicate`: Clones table schema structure.
  - `POST /api/v1/schemas/tables/{id}/truncate`: Deletes all data rows from the target table.
- **Docker Compose Custom Volume Names**:
  - Configured explicit custom volume names in `docker-compose.yml`:
    - `file4base-postgres-data` for PostgreSQL persistence.
    - `file4base-mariadb-data` for MariaDB profile persistence.
    - `file4base-net` for the bridge network.
  - Prevents volume orphaning or duplication when cloning into different directory names or running with custom project flags.
  - Migrated existing local data from legacy auto-prefixed volume `file4base-app_postgres_data` to `file4base-postgres-data`.
- **Layout Unification Across All Operational Modes (Browse, Find, Layout, Preview)**:
  - **Layout Mode (Modo Presentación)**: Added `commitAndSave()` and `onLayoutChanged` in `LayoutDesignerWidgetState` so layout changes are committed and synced to `_activeLayout` in `main.dart` and persisted via API immediately on switching modes, eliminating debounce timer loss.
  - **Browse Mode (Modo Hojear)**: Active layout is rendered in Form view with real-time bound record values. Even when a table has 0 records, the designed layout canvas is preserved with interactive click-to-create fields and a top banner with `[Create First Record]`.
  - **Find Mode (Modo Buscar)**: Renders the active layout canvas with criteria input fields and query operators (`*`, `...`, `=`, `==`, `!`, `>`, `<`), enabling visual query design directly on the layout.
  - **Preview Mode (Modo Vista previa)**: Replaced mock tabular view with `_buildLayoutPreviewCanvas`, rendering the exact designed layout objects (labels, bound fields, buttons, portals, shapes) on a simulated sheet of paper honoring margins, paper size, orientation, and record pagination.
  - Added automated widget tests in `client/test/layout_unification_test.dart` validating layout rendering in Browse, Find, and Preview modes.

## [0.4.15] - 2026-09-25

### Added
- **Visual Relationships Graph (`File > Manage > Database > Relationships Graph`)**:
  - Implemented the visual relational schema graph in Tab 3 of `ManageDatabaseDialog`, matching the canonical low-code relational specification:
    - **Draggable Table Occurrence (TO) Boxes**: Interactive cards showing occurrence names (`CLIENTES`, `FACTURAS`, `CLIENTES 2`), header bar with collapse/expand toggle, and field lists with key icons.
    - **Selection State & Amber Glow**: Selected Table Occurrence displays an amber/gold halo glow outline matching the visual design.
    - **Relational Match Connector Lines**: Smooth orthogonal cubic bezier curves linking connected fields across occurrences.
    - **Interactive Operator Badges**: Centered badges on relationship lines displaying criteria comparison operators (`=`, `≠`, `<`, `≤`, `>`, `≥`). Clicking selects the relationship, double-clicking opens relationship editor.
    - **"Especificar tabla" Modal Dialog (`SpecifyTableDialog`)**:
      - Exact replication of the canonical dialog: source dropdown ("Archivo actual"), table list view with selection highlight, and automatic occurrence name generator (suggesting e.g. `CLIENTES 2` when `CLIENTES` already exists).
    - **"Especificar relación" Modal Dialog (`SpecifyRelationshipDialog`)**:
      - Two-column table occurrence and field selectors with middle comparison operator dropdown.
      - Canonical relational checkboxes:
        - `Permitir la creación de registros en esta tabla a través de esta relación` (`allow_creation`)
        - `Eliminar registros relacionados en esta tabla cuando se elimine un registro en la otra tabla` (`cascade_delete`)
        - `Ordenar registros relacionados` (`sort_related`)
      - Relationship deletion and criteria update actions.
    - **Canvas Toolbar & Navigation**: Buttons to add occurrence (`+ Tabla...`), add relationship (`+ Relación...`), delete selection, and center/reset view (`Centrar vista`).
- **Backend Schema & REST APIs**:
  - Added full CRUD endpoints for Table Occurrences:
    - `GET /api/v1/schemas/occurrences`: Lists all occurrences.
    - `POST /api/v1/schemas/occurrences`: Creates a new table occurrence.
    - `PUT /api/v1/schemas/occurrences/{id}`: Updates occurrence name and/or coordinates `(x_pos, y_pos)`.
    - `DELETE /api/v1/schemas/occurrences/{id}`: Deletes occurrence with cascading cleanup.
  - Added full CRUD endpoints for Relationships:
    - `GET /api/v1/schemas/relationships`: Lists all relationships.
    - `POST /api/v1/schemas/relationships`: Creates a new relationship.
    - `PUT /api/v1/schemas/relationships/{id}`: Updates relationship operator, name, or cascade options.
    - `DELETE /api/v1/schemas/relationships/{id}`: Deletes relationship.
  - Cross-engine support for both PostgreSQL and MariaDB dialects.
  - Solution bundle export and import now packages both Table Occurrences and Relationships.
- **Automated Tests**:
  - Added 6 Flutter tests in `client/test/relationships_graph_test.dart` (model serialization, `SpecifyTableDialog`, `SpecifyRelationshipDialog`, and `RelationshipGraphWidget` canvas).
  - Added Go tests in `server/internal/schema/relationship_service_test.go` and `server/internal/api/schema_handler_test.go`.

## [0.4.14] - 2026-09-25

### Added
- **Interactive Canvas Zoom Controls & Scaling Engine**:
  - Implemented interactive zoom magnification controls in the bottom-left corner of the workspace:
    - **Percentage Magnification Indicator**: Displays current zoom level (e.g. `100`, `150`, `75`) with interactive popup menu offering standard low-code zoom presets (`400%`, `300%`, `200%`, `150%`, `100% (Actual Size)`, `75%`, `50%`, `25%`).
    - **Zoom Out Button (`Icons.zoom_out`)**: Magnifying glass button with minus (`-`) stepping down through preset zoom levels with bounds checking (min 25%).
    - **Zoom In Button (`Icons.zoom_in`)**: Magnifying glass button with plus (`+`) stepping up through preset zoom levels with bounds checking (max 400%).
  - **View Menu Integration (`File4BaseMenuBar`)**:
    - Wired `View > Zoom In` (`Cmd/Ctrl + =`), `View > Zoom Out` (`Cmd/Ctrl + -`), `View > Actual Size (100%)` (`Cmd/Ctrl + 0`), and `View > Zoom Level` presets submenu.
  - **Global Keyboard Shortcuts (`CallbackShortcuts`)**:
    - Registered `Cmd/Ctrl +`, `Cmd/Ctrl -`, `Cmd/Ctrl 0` to quickly adjust canvas zoom level from anywhere in the window.
  - **Workspace Canvas Scaling (`_buildZoomableBody`)**:
    - Dynamic bi-directional horizontal and vertical scrollbars automatically wrapping the scaled workspace when zoom exceeds viewport size.
    - Hit-testing and interactive record editing fully preserved via Flutter inverse transformation matrix.
  - **Automated Tests**: Added unit and widget tests in `client/test/zoom_test.dart` (6 tests covering zoom stepping, limits, menu triggers, and reset).

## [0.4.13] - 2026-09-25

### Added
- **Themes Management System (`File > Manage > Themes...` / `ManageThemesDialog`)**:
  - Implemented free theme manager modal dialog allowing real-time discovery, inspection, and switching between canonical developer and design palettes:
    - **Light (Clean Slate)**: Default theme featuring pure crisp white canvas (`#FFFFFF` surface, `#F8FAFC` background) with slate typography and cyan accents.
    - **Dark Modern (Sophisticated)**: Midnight dark workspace with graphite panels and cyan highlights (`#0B1120` / `#111827`).
    - **Monokai Pro (Code Studio)**: Iconic developer palette with charcoal background (`#272822`) and vibrant neon yellow/green/pink step accents.
    - **Dracula Midnight**: Rich dark purple palette (`#282A36`) with soft lavender borders and emerald highlights.
    - **Nordic Frost (Nord)**: Minimalist arctic palette (`#2E3440` / `#3B4252`) with calm frost cyan accents.
    - **Solarized Light & Solarized Dark**: Low-contrast precision palettes (`#FDF6E3` / `#002B36`).
  - **Live Preview Panel**: Split-view with interactive mockups (menu bar, form cards, script editor with syntax highlighting, and color hex swatches) reacting instantaneously to selected themes.
  - **Fast Switch Toolbar**: Quick buttons for one-click switching directly from the dialog footer.
  - **System-Wide Reactivity**: Script Workspace dialog, Calculation Builder dialog, and root MaterialApp react to theme changes without reloading or losing state.
  - **Automated Tests**: Added 9 unit and widget tests in `client/test/manage_themes_test.dart`.



## [0.4.12] - 2026-09-25

### Added
- **Script Workspace IDE & Execution Engine (`Scripts > Script Workspace...` / `Manage > Scripts...`)**:
  - Implemented 3-panel visual scripting canvas matching canonical visual IDE architecture:
    - **Header & Controls**: Active database badge (`file4base_dev`), real-time unsaved changes indicator, `▶ Ejecutar` (Run script) button with execution simulation and trace inspector, `🐞 Depurar` (Debug script) button with step-by-step visual execution and live variable inspection stack (`$subtotal`, `$counter`, `Get(LastError)`), and `💾 Guardar` button persisting script definitions.
    - **Left Panel (Scripts Explorer)**: Real-time search filter `TextField`, `+ Guión` quick creation, active status switches, sequence step badges, and context menu (`Activar/Desactivar`, `Duplicar`, `Eliminar`).
    - **Center Panel (Sequential Step Editor)**: Multi-tab editor for concurrently open scripts with close buttons (`✕`), inline script renaming, context table occurrence selector (`Invoices`, `Customers`, etc.), `ReorderableListView` supporting drag-and-drop step rearrangement, 2-digit indices (`01`, `02`...), step activation checkboxes, category badges (Violet for Navigation, Emerald for Records, Amber for Control & Logic, Cyan for Fields/Variables, Rose for Integration), monospace parameter preview brackets, and hierarchical vertical nesting lines for `If/Else/End If` and `Loop/End Loop`.
    - **Bottom Zone (Contextual Parameter Inspector)**: Context-aware inspector adapting to the active instruction type (`set_variable`, `set_field`, `go_to_layout`, `if`, `exit_loop_if`, `perform_rest_api`, etc.) with variable/target field inputs and `[ fx Especificar... ]` calculation launcher.
    - **Right Panel (Step Catalog)**: Categorized catalog with 4 major categories (Navegación, Registros, Control y Lógica, Integración y Datos), description tooltips, and click/double-click instant insertion.
    - **Bottom Status Bar**: Context table occurrence indicator, total steps counter, and execution engine status badge (`File4Base Script Engine v1.0 (Go/CEL)`).
- **Calculation & Formula Builder Dialog (`CalculationBuilderDialog`)**:
  - Canonically styled formula editor with table occurrence and field browser (`Table::Field`), mathematical and logical operator palette (`+`, `-`, `*`, `/`, `^`, `&`, `=`, `≠`, `>`, `<`, `≥`, `≤`, `AND`, `OR`, `NOT`, `( )`, `" "`), categorized functions list (Agregación, Lógica, Texto, Fecha & Hora, Sistema), search filters, and monospace syntax editor.
- **Backend Schema & Script Service (Go & PostgreSQL/MariaDB)**:
  - Added DDL for `sys_scripts` and `sys_script_steps` system tables in `server/internal/schema/service.go`.
  - Added CRUD and step management in `server/internal/schema/script_service.go` (`ListScripts`, `GetScript`, `CreateScript`, `UpdateScript`, `DeleteScript`, `DuplicateScript`).
  - Added REST API endpoints under `/api/v1/schemas/scripts` and `/api/v1/scripts` in `server/internal/api/schema_handler.go`.
  - Added unit test suite in `server/internal/schema/script_service_test.go` (`TestScriptService_CRUD`).
- **Solution Packaging (`.f4b`)**:
  - Updated `SolutionPackage` in `client/lib/core/models/solution_models.dart` to serialize and deserialize scripts alongside layouts and tables.
- **Automated Tests**:
  - Added widget and model test suite in `client/test/script_workspace_test.dart` (5 tests covering model serialization, formula dialog, and 3-panel workspace layout).

## [0.4.11] - 2026-09-24

### Added
- **Database-Scoped Manage Security Dialog (`Manage > Security` / Gestionar seguridad)**:
  - Redesigned `ManageSecurityDialog` (`client/lib/features/security/manage_security_dialog.dart`) into a comprehensive database-level table/list interface scoped to the active database:
    - **Header & Scope**: Active database badge indicator (`file4base_dev` / custom database), metric badges (Total accounts, Active, Inactive).
    - **Sequential Indexing (`#`)**: 1-based sequential number per row for clear tabular identification.
    - **Interactive Account Activation Toggle**: Direct Switch control to activate or deactivate accounts in real-time (`is_active` column in PostgreSQL `sys_users` table), with safety protections preventing self-deactivation and deactivating the last active Owner account.
    - **Account Name & Identity**: User avatar with initial, username, current active session badge (`(Tú / Activa)`), and creation date tooltip.
    - **Privilege Sets (Conjuntos de privilegios)**: Formatted, color-coded role badges:
      - `[Acceso total] Owner`: Full administrative and schema control.
      - `[Entrada datos y diseño] Admin`: Layout designer and data entry permissions.
      - `[Acceso restringido] User`: Restricted access controlled by layout-level permissions.
    - **Per-Row Action Icon Buttons**:
      - 🔑 **Cambiar contraseña**: Fast modal dialog to update account password.
      - ✏️ **Editar cuenta y privilegios**: Full account configuration dialog with role selector, active status switch, and per-layout permissions matrix (Read & Write / Read Only / No Access).
      - 📋 **Duplicar cuenta**: Clone existing account with `_copia` suffix, pre-copying role and all layout permissions.
      - 🗑️ **Eliminar cuenta**: Permanent deletion with confirmation dialog, protected against deleting self or the sole Owner.
    - **Search & Filter Toolbar**: Instant search bar filtering by username or privilege role, plus status filter chips (`Todas`, `Activas`, `Inactivas`).
    - **Layout Privileges Matrix Tab («Privilegios de presentaciones»)**: Solution-wide layout audit view inspecting user access levels across each layout in the database.
    - **Extended Privileges Tab («Privilegios ampliados»)**: Network and protocol privileges configuration (`f4bapp`, `f4bwebdirect`, `f4brest`, `f4bexport`) per privilege set.

### Changed
- **Backend Schema & Security Service**:
  - Added `is_active BOOLEAN NOT NULL DEFAULT TRUE` column to `sys_users` table and migration in `server/internal/schema/service.go`.
  - Updated `Authenticate`, `ListUsers`, `CreateUser`, and `UpdateUser` in `server/internal/schema/security_service.go` to enforce account activation status and reject deactivated accounts with `user account is deactivated`.
  - Updated REST API endpoint `/api/v1/security/users` (`server/internal/api/security_handler.go`) to accept `is_active` in user creation and update requests.
- **Frontend ApiClient & Models**:
  - Added `isActive` property to `UserModel` with `copyWith`, `toJson`, and updated `fromJson`.
  - Added `isActive` parameter to `ApiClient.createUser` and `ApiClient.updateUser`.


### Removed
- **"Send Mail..." File Menu Item**:
  - Removed "Send Mail..." from `File4BaseMenuBar` under the File menu.
- **Duplicate "Save a Copy As..." Menu Item**:
  - Removed the duplicate "Save a Copy As..." menu entry that was placed immediately above "Recover...".

### Added
- **File Options Dialog (`File > File Options...`)**:
  - Implemented `FileOptionsDialog` (`client/lib/features/solution_manager/file_options_dialog.dart`) and `FileOptionsModel` (`client/lib/core/models/file_options_model.dart`) adhering to canonical File4Base File Options:
    - **Tab 1: «Abrir» (Open)**:
      - *Iniciar sesión como*: Configure automatic login using a default account and password, or entering via the Guest account.
      - *Cambiar contraseña*: Integrated password management modal to update account credentials.
      - *Presentación de inicio*: Select the exact layout (`startup_layout`) to display upon opening the database solution.
      - *Ocultar todas las barras de herramientas*: Full-screen or clean view hiding native toolbars and sidebars.
    - **Tab 2: «Activadores de guión» (Script Triggers)**:
      - Assign automatic script execution to file lifecycle events: `OnFirstWindowOpen`, `OnLastWindowClose`, `OnWindowOpen`, `OnWindowClose`, and `OnFileAVPlayerChange` with optional script parameters.
- **Page Setup Dialog (`File > Page Setup...` / Configurar impresión / Ajustar página)**:
  - Implemented `PageSetupDialog` (`client/lib/features/solution_manager/page_setup_dialog.dart`) and `PageSetupModel` (`client/lib/core/models/page_setup_model.dart`):
    - *Selección de impresora*: Format for "Any Printer" / "Cualquier impresora" (recommended for multi-machine interoperability), system default printer, or PDF virtual writer.
    - *Tamaño del papel*: Standard formats (A4, US Letter, US Legal, A3, A5, B5, Custom) with exact dimensions in millimeters and PostScript points (`pt`).
    - *Orientación*: Interactive toggle for Vertical (Portrait) vs Horizontal (Landscape).
    - *Cálculo de márgenes*: Configurable top, bottom, left, and right margins in mm.
    - *Límites de página y ancho útil*: Live schematic diagram showing printable area and exact metric/point dimensions for report layout and printing.
  - Added "Page Setup..." button and paper configuration badges in Preview Mode toolbar (`LayoutPreviewWidget`).
  - Added shortcut `Shift+Cmd+P` / `Shift+Ctrl+P` for quick Page Setup access.
- **Solution Persistence**:
  - Integrated `fileOptions` and `pageSetup` serialization into `SolutionPackage` and `.f4p` binary packages, automatically preserving startup options and print configurations.


### Removed
- **Redundant "Switch Database / Login..." Menu Item**:
  - Removed `Switch Database / Login...` from `File4BaseMenuBar` under the File menu and deprecated the `onSwitchDatabaseOrLogin` callback.

### Added
- **Load from Disk and Cancel in Initial Connection Dialog (`DatabaseLoginDialog`)**:
  - Added "Cargar de disco duro..." (`Icons.folder_open`) button to `DatabaseLoginDialog`, allowing immediate opening of a `.f4p` / `.f4b` solution directly from local disk upon connecting.
  - Added "Cancelar" action button and window close button (`Icons.close`), making the dialog dismissible (`barrierDismissible: true`) without trapping the user.
- **Strict Database Access Password Enforcement & Owner Password Fallback**:
  - Removed insecure `'admin'` / `'admin'` default prefilling from `DatabaseLoginDialog` and `OpenSolutionDialog`.
  - The password field is always blank and strictly required upon database selection.
  - Enhanced backend `Authenticate` in `security_service.go` to support authenticating with the database password against database owner accounts when logging in to a database with no additional users.
- **Solution-Level User & Permission Packaging**:
  - Updated `_exportCurrentSolutionBytes` in `main.dart` and `SolutionPackage.fromLiveData` in `solution_models.dart` to export the database's users and layout permissions into the `.f4p` solution package.
  - When loading a solution from disk, its packaged users are automatically restored into `sys_users`.
  - `Manage > Security...` manages accounts and layout privileges specifically for the active database/solution independently from other databases.

## [0.4.8] - 2026-09-24

### Added
- **Unified Layout Selector Across Modes (Browse, Layout, Preview)**:
  - Replaced the erroneous top table selector in the left status sidebar (`File4BaseStatusSidebar`) with a dedicated Layout Selector dropdown (`Icons.view_quilt`).
  - Restored canonical low-code database architecture where records are always viewed, found, and printed through a Layout representation (Presentación).
  - Supported switching between all layouts in the active solution with instant synchronization across Browse Mode, Find Mode, Layout Mode, and Preview Mode.
  - Included quick actions in the layout dropdown: **New Layout...** and **Manage Layouts...**.
- **Comprehensive Solution Layout Management (`ManageLayoutsDialog`)**:
  - Implemented `ManageLayoutsDialog` in `client/lib/features/layout_engine/manage_layouts_dialog.dart`.
  - Displays all layouts across the database solution with associated table occurrences, visual object counts, and internal IDs.
  - Added full CRUD management for layouts: Create New Layout, Rename Layout, Duplicate Layout, Delete Layout, and Open / Switch to Layout.
  - Added canonical `Manage > Layouts...` menu entry in `File4BaseMenuBar`.
- **Layout Renaming & Layout Setup Dialog**:
  - Added layout renaming capability in `LayoutDesignerWidget` via editable layout title badge, inline rename modal, and `Layout Setup...` dialog (`_showLayoutSetupDialog`).
  - Resolved bug where renaming a layout reset it to default due to mismatched table occurrence IDs; layouts are now referenced deterministically by `id` with table occurrence linkage.
  - Layout renames persist immediately to `PUT /api/v1/layouts/{id}` and trigger visual updates in the designer and sidebar.
- **Visual Layout Canvas View in Browse Mode (`DataBrowserWidget`)**:
  - Added visual Layout Canvas view rendering (`_buildLayoutCanvasView`) in `DataBrowserWidget` based on the active layout definition (`_activeLayout`).
  - Renders custom fields, labels, buttons, and visual objects at their exact layout coordinates.
  - Integrated field inputs directly into record editing with debounced auto-save, primary key badges, and validation error states.
  - Connected layout buttons to File4Base record actions (New Record, Delete Record, Perform Find, Print/Preview, Record Navigation).
  - Added a toggle between `Form` (custom layout canvas) and `Cards` (standard grid/card view) in the records toolbar.

### Changed
- Rebuilt Flutter WebDirect client bundle (`build/web`) and updated `file4base-web` Docker container.
- Updated version metadata to `0.4.8` across `VERSION`, `server/cmd/server/main.go`, `client/pubspec.yaml`, and `client/lib/features/about/about_dialog.dart`.

## [0.4.7] - 2026-09-24

### Added
- **Field Options Modal ("Options for Field <name>")**:
  - Implemented `FieldOptionsDialog` in `client/lib/features/schema_manager/field_options_dialog.dart` faithfully matching the native field options dialog.
  - **Auto-Enter Tab**: Automatic population rules for Creation/Modification (Date, Time, Timestamp, Name, Account Name), serial number generation (on creation / on commit, next value, increment), value from last visited record, default static data, calculated values (`Specify...`), looked-up values, and option to prohibit modification during data entry.
  - **Validation Tab**: Strict data validation options including Not Empty (Always / During Entry), Unique Value, Existing Value, Strict Data Type (Date, Time of Day, 4-Digit Year, Numeric, Text), Range validation (min/max), Maximum length, and custom error message text.
  - **Storage & Indexing Tab**: Global field storage (single value shared across all records for logos, VAT rates, session state), indexing options (None, Minimal, All full-text & fast search, auto-index), and container storage selection (internal database bytea blob vs external filesystem uploads).
  - **Calculation & Summary Tab**: Formula editor with quick-insert function buttons (`UPPER()`, `LOWER()`, `CONCAT()`, `SUM()`, `ROUND()`, `IF()`, `CURRENT_DATE`), calculation result type selector, and Summary aggregate operations (Sum, Average, Count, Minimum, Maximum, target column selector, and running total option).
  - Added "Options..." button with `Icons.tune` and row tap navigation in the Fields tab of `ManageDatabaseDialog` (`client/lib/features/schema_manager/manage_database_dialog.dart`).
- **Embedded Swagger UI & OpenAPI Specification in API Container (`file4base-app-api`)**:
  - Embedded Swagger UI and OpenAPI 3.0.3 specification directly inside the Go backend binary using `embed.FS` (`server/internal/api/swagger_handler.go`).
  - Available at `/swagger/` (interactive documentation), `/swagger/openapi.json` and `/openapi.json` (raw specification), and redirected from `/swagger` and `/docs`.
  - 100% self-contained and offline-ready with zero external CDN runtime dependencies.
- **Docker Service & Container Renaming (`file4base-app-api`)**:
  - Renamed backend container and image from `file4base-server` / `file4base-app-server` to `file4base-app-api` in `docker-compose.yml`.
  - Service renamed to `api` with dual network aliases (`api`, `server`) preserving backwards compatibility for internal container networking.
  - Updated `client/nginx.conf` reverse proxy to route `/api/`, `/swagger/`, `/docs`, and `/openapi.json` to `http://api:8080`.
- **Enhanced Column Options Persistence in Backend (`server/`)**:
  - Enhanced `Service.UpdateColumn` and `SchemaHandler.UpdateColumn` to support updating `default_value`, `calculation_formula`, and `validation_rules` in `sys_columns` using dynamic SQL for PostgreSQL and MariaDB.
  - Updated `ApiClient.updateColumn` in Flutter client to serialize and transmit field options.

### Fixed
- **Find Mode Search Execution and Navigation Transitions**:
  - Resolved issue where executing a search ("Perform Find") left the user trapped on the Find input form without transitioning to the found records.
  - Implemented proper operational mode transition from `OperationalMode.find` to `OperationalMode.browse` upon search execution, showing a found set notification badge and "Show All" action.
  - Handled zero-results scenario with an informative dialog allowing the user to either modify their criteria or return to showing all records.
  - Replaced muddy amber card backgrounds in Find Mode with clean, high-contrast Material 3 cards, banner headers, search icons, and field type indicators.
  - Implemented comprehensive formula & wildcard search syntax in backend `ExecuteFind` and `ParseFile4BaseFindCriteria`:
    - Wildcard `*` (`%`) and single character `@`/`?` (`_`).
    - Comparison operators: `>`, `<`, `>=`, `<=`.
    - Exact match `=` and strict exact match `==`.
    - Negation / exclusion: `!=` or `!`.
    - Range queries: `min...max`.
    - Today's date shorthand: `//`.
    - Empty field search (`=`) and non-empty field search (`*`).
    - Wrapped column references in `CAST(col AS TEXT)` to avoid PostgreSQL type operator mismatches (`numeric ~~* text`).

## [0.4.6] - 2026-09-24

### Added
- **API Best Practices Implementation in Go Backend (`server/`)**:
  - Implemented cloud-native health probes: `/healthz/liveness` (`/livez`), `/healthz/readiness` (`/readyz`), `/healthz/startup` (`/startupz`), and comprehensive IETF-draft diagnostic `/healthz`.
  - Implemented graceful shutdown lifecycle: failing readiness (HTTP 503) on SIGTERM/SIGINT, draining in-flight requests, and closing database pools cleanly.
  - Implemented OpenTelemetry and W3C TraceContext middleware (`server/internal/telemetry/middleware.go` and `trace.go`): automatic generation and propagation of `traceparent`, `tracestate`, `X-Trace-ID`, and structured JSON request logs.
  - Implemented RFC 9457 Problem Details (`application/problem+json`) error handling across all schema, data, security, and solution handlers (`server/internal/telemetry/problem.go`).
  - Added security headers middleware: `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `X-XSS-Protection: 1; mode=block`, `Strict-Transport-Security`.
  - Added Docker container healthchecks in `server/Dockerfile` and `docker-compose.yml` (`depends_on: condition: service_healthy`).
- **Web Client Observability & Error Handling (`client/`)**:
  - Implemented W3C `TraceContext` in Flutter Web client (`client/lib/core/api/api_client.dart`), generating and injecting `traceparent` and `X-Trace-ID` headers into all outgoing requests.
  - Implemented `ApiException` parsing RFC 9457 `application/problem+json` error responses (exposing `title`, `detail`, `status`, `type`, `instance`, and `traceId`).
  - Added cloud-native probe methods to `ApiClient`: `checkLiveness()`, `checkReadiness()`, `checkStartup()`.
  - Updated `client/nginx.conf` reverse proxy to forward W3C `traceparent` and `tracestate`, apply security headers, and proxy cloud-native probe endpoints.
- **API Best Practices & Observability Specification**:
  - Published comprehensive enterprise specification in [docs/specs/API_BEST_PRACTICES.md](docs/specs/API_BEST_PRACTICES.md).
  - Defined standards for API Semantic Versioning (SemVer 2.0.0), non-breaking evolution, and RFC 8594 deprecation/sunset headers.
  - Specified cloud-native three-probe health architecture (Liveness, Readiness, Startup) and IETF Draft `/healthz` diagnostics.
  - Defined OpenTelemetry (OTel v1.26+) distributed tracing conventions, W3C TraceContext (`traceparent`), RED metrics, and JSON structured log correlation.
  - Documented REST resource modeling, idempotency matrix, cursor/keyset pagination, and RFC 9457 (`application/problem+json`) error handling.

### Changed
- **Browse Mode Cleaner Record Form ("Ficha")**:
  - Removed the out-of-place 3-dots popup menu ("Rename Field...", "Delete Field...") and the "+ Add Field to Table..." button from Browse mode records, keeping schema modifications strictly in **Manage Database** (`File -> Manage -> Database... -> Fields` tab).
  - Browse mode is now focused entirely on data entry, validation, record navigation, and live auto-saving.
- **About File4Base Dialog Redesign**:
  - Completely redesigned `AboutFile4BaseDialog` in `client/lib/features/about/about_dialog.dart` from the retro 1990s beveled style to a modern Material 3 interface aligned with the application design system.
  - Added modern header with app badge, title, subtitle, and close button.
  - Added clean top `TabBar` with icons for *About*, *System Info*, and *Credits* tabs.
  - Modernized bottom action bar with responsive `About`, `Info`, `Credits` tab switch buttons and a primary `OK` button (`FilledButton.icon`).
  - Added structured key-value cards with icons for system runtime information and open-source technology badges.
  - Fixed responsive wrapping to eliminate any `RenderFlex` overflow across compact viewports and automated test runners.

## [0.4.5] - 2026-09-24

### Fixed
- **Web File Picker in Open Solution Dialog**:
  - Implemented `platformPickFile()` in `client/lib/core/services/solution_storage_web.dart` using standard HTML5 file chooser (`<input type="file">` and `FileReader`).
  - Replaced unsupported `FilePicker.pickFiles` call on Web which previously threw `UnimplementedError` when trying to browse and select `.f4p` / `.f4b` files from disk.
  - Seamless file picking across macOS Finder, Windows Explorer, and Linux file managers via browser file dialog.

## [0.4.4] - 2026-09-24

### Added
- **Dynamic Field & Record Management in Record Form ("Ficha")**:
  - Live auto-save on every field keystroke (800ms debounce), on focus lost (blur), on Enter key, and before record navigation.
  - Added "+ Add Field to Table..." action directly at the bottom of the record card in Browse mode, allowing instant addition of new columns without leaving the record view.
  - Added per-field action menu (3 dots) on every column in the card with "Rename Field..." and "Delete Field..." actions.
  - Added "Edit Field" (rename) and "Delete Field" (drop column) buttons to the Fields tab in `ManageDatabaseDialog`.
- **Backend Column Management API**:
  - Added `PUT /api/v1/schemas/tables/{id}/columns/{columnId}` to update column metadata (display names).
  - Added `DELETE /api/v1/schemas/tables/{id}/columns/{columnId}` to physically drop columns and unregister them from `sys_columns`.
- **Live State Synchronization**:
  - Fixed `_loadTables()` in client `main.dart` to correctly refresh the active `_selectedTable` instance and its columns whenever schema modifications occur.
  - Enhanced `DataBrowserWidget.didUpdateWidget` to detect column changes and immediately rebuild controllers and reload rows.
  - Added `await _saveCurrentRecord()` before creating a new record in `DataBrowserWidget` to ensure no in-flight edits are lost.

## [0.4.3] - 2026-09-24

### Added
- **Auto-Save Service (`AutoSaveService`)**:
  - New `client/lib/core/services/auto_save_service.dart` singleton that debounces structural
    solution saves every 3 seconds using a background timer.
  - Saves only layout structure, schemas, tables, and workspace config — **never** row data.
  - Exposes a `Stream<AutoSaveStatus>` (`idle`, `dirty`, `saving`, `saved`, `error`) consumed
    by a live indicator in the bottom status bar.
- **File Extension Migration `.f4b` → `.f4p`**:
  - New solutions default to the `.f4p` extension (File4Base Project).
  - All file pickers now accept both `.f4p` (new) and `.f4b` (legacy) for backward compatibility.
  - `SolutionStorageService` gains a new `saveSolutionFile()` method for single-file structure
    writes, separate from the dual-file export path.
- **Export Data Command (`File → Export Data...`)**:
  - New menu item that explicitly exports PostgreSQL row data to `.f4data` (MessagePack).
  - Row data is **never** part of auto-save; this is the only path to persist database rows locally.
- **Explicit Save Warning Dialog**:
  - Pressing `Cmd+S` now flushes the auto-save immediately and shows a two-panel dialog:
    - Green panel: confirms the `.f4p` file was written (layouts/schemas/config).
    - Amber panel: warns that database row data was **not** saved and offers a shortcut to
      `Export Data Now`.
- **Auto-Save Status Indicator in Bottom Status Bar**:
  - Real-time chip showing `Auto-save`, `Unsaved`, `Saving...`, `Saved`, or `Save Error`
    with colour-coded icon and a tooltip showing the last save timestamp.
- **`onAutoSaveDirty` callback in `LayoutDesignerWidget`**:
  - Fires after each successful layout server-save, notifying `AutoSaveService` to mark the
    solution dirty and schedule a debounced file write.

### Changed
- `NewDatabaseDialog` no longer auto-exports an empty `.f4data` on creation; only the `.f4p`
  structural file is written. Users must use `File → Export Data...` to capture row data.
- `_handleSaveAs()` in `main.dart` updated to produce `.f4p` files, update `AutoSaveService`
  directory/base-name, and display the improved save dialog.
- `SaveCopyDialog` labels updated from `.f4b` → `.f4p`.

## [0.4.2] - 2026-09-23

### Fixed
- **GitHub Actions Node.js 20 Deprecation Warning**:
  - Upgraded CI/CD workflow actions from `@v4` to `@v7`/`@v8` (`actions/checkout@v7`, `actions/upload-artifact@v7`, `actions/download-artifact@v8`).
  - All workflow actions now natively target `node24`, eliminating the runner deprecation warning.
- **Desktop Single-Compression Distribution**:
  - Resolved double-compression issue where downloading desktop artifacts from GitHub Actions resulted in nested archives (`.zip` containing `.tar.gz` or `.zip`).
  - Enabled `archive: false` on `actions/upload-artifact@v7`, allowing pre-packaged archives to be uploaded as direct single files without GitHub Actions adding a redundant outer `.zip` container.
  - Decompressing downloaded desktop artifacts now immediately extracts the application (`File4Base.app` on macOS, binary bundle on Linux, executable folder on Windows) in a single step.
  - Added native Apple Disk Image (`File4Base-macOS.dmg`) generation via `hdiutil` for seamless macOS drag-and-drop installation.

### Added
- **Automated GitHub Releases Publishing**:
  - Added `publish-release` workflow job triggered upon version tags (`v*`).
  - Automatically compiles desktop bundles across macOS, Linux, and Windows and attaches release assets directly to GitHub Releases via `gh release create`.

### Added
- **Unified Open Solution / Database Dialog (`OpenSolutionDialog`)**:
  - Implemented comprehensive `File -> Open...` (`⌘O`) modal supporting dual opening pathways:
    1. **Server Databases (API)**: Queries `GET /api/v1/databases`, lists all databases created on the server API, displays the active database badge, and prompts for username and password to authenticate.
    2. **Local Solution File (.f4b)**: Allows selecting a `.f4b` file from the computer, inspects package metadata (solution name, target database), and prompts for database username and password before restoring and opening.

### Fixed
- **Decoupled Database Name vs User Credentials**:
  - Resolved conflation between PostgreSQL physical database names and application user accounts:
    - Database Name (`file4base_dev`, `my_solution_db`, etc.) defines the database catalog.
    - Username (`admin`, `file4base`, or custom) defines the user identity in `sys_users`.
  - In `NewDatabaseDialog`, database name changes no longer overwrite username or password fields.
  - In `DatabaseLoginDialog`, selecting a database from the dropdown preserves user credentials.
  - In backend Go server, default administrative accounts (`admin`, `file4base`, `file4base_dev`) are provisioned with role `owner` upon catalog initialization.

## [0.4.0] - 2026-09-23

### Added
- **Centralized Layout Persistence in Intermediate Server**:
  - Centralized layout definitions directly in PostgreSQL `sys_layouts` table via backend Go service and REST API (`/api/v1/layouts`).
  - Enables multiple connected web client instances (`file4base-web`) to share, synchronize, and edit layouts collaboratively without local desynchronization.
- **Default Administrative `owner` User**:
  - Auto-provisioned default administrative account `owner` (password `owner`, role `owner`) upon database catalog initialization in `EnsureSystemTables`.
  - Passwords securely hashed with `bcrypt` (DefaultCost).
- **Manage -> Security (`Manage -> Security...`)**:
  - Full administrative dialog under `File -> Manage -> Security...` allowing owners and admins to manage user accounts and assign roles (`owner`, `admin`, `user`).
  - Granular per-layout permission matrix (`read_write`, `read_only`, `none`) stored in `sys_user_permissions`.
  - Automatic restriction of layout designer mode (`OperationalMode.layout`) for users with `read_only` or `none` layout access.
- **Startup Database Selection & Authentication Flow**:
  - Initial cold start dialog (`DatabaseLoginDialog`) that queries `/api/v1/databases`, allows selecting or creating PostgreSQL databases, and authenticates the user before entering the workspace.
  - Added user account and role chip to bottom status bar with one-click database/user switching.
  - Added `File -> Switch Database / Login...` shortcut in canonical menu bar.

## [0.3.1] - 2026-09-23

### Added
- **Hard Drive Destination Folder Selection**:
  - Implemented `SolutionStorageService.pickDirectory()` using modern Web **File System Access API** (`window.showDirectoryPicker()`) on Chromium/Chrome and native OS folder picker (`FilePicker.getDirectoryPath()`) on Desktop (macOS, Windows, Linux).
  - Enables users to browse their disk and select the exact folder where solution files are saved.
- **Dual-File Synchronized Persistence (`.f4b` + `.f4data`)**:
  - Unified saving across `New Database...`, `Save` (`⌘S`), `Save As...` (`⇧⌘S`), and `Save a Copy As...` to automatically record both companion files in the selected folder:
    1. `<name>.f4b`: UI definitions, layouts, schemas, table occurrences, and secure encoded credentials.
    2. `<name>.f4data`: Active PostgreSQL table rows and database records.
- **Encrypted / Obfuscated Connection Credentials**:
  - Implemented reversible symmetric key-stream obfuscation/encryption (`enc:<base64>`) for database credentials (`user` and `password`).
  - Completely prevents plaintext credential leaks inside `.f4b` MessagePack bundles.
  - Implemented identically across Dart client (`DatabaseConnectionConfig`) and Go server (`solution_service.go`) with automated test coverage.
- **Desktop Application Distribution**:
  - Documented and unpacked native desktop applications for macOS (`File4Base.app`), Windows, and Linux in `dist/`.

## [0.3.0] - 2026-09-23

### Added
- **MessagePack Solution & Dual-File Architecture**:
  - Implemented binary MessagePack (`msgpack`) serialization for File4Base application files (`.f4b`), preserving layouts, schemas, table definitions, relationships, users, and database credentials.
  - Implemented secondary MessagePack database data file (`.f4data`) containing all physical table rows and records for database backup and restore.
- **Multi-Database PostgreSQL Architecture**:
  - Created `MultiDatabaseManager` in Go backend (`server/internal/dbal/multi_db.go`) supporting dynamic connection pools, runtime database creation, and seamless database switching.
  - Added REST API endpoints at `/api/v1/databases` (list, create, switch) and `/api/v1/solutions` (export/import solution, export/import data).
- **File Menu Save & Database Lifecycle**:
  - Wired full persistence workflows in `File4BaseMenuBar`: `New Database...` (`⌘N`), `Open...` (`⌘O`), `Save` (`⌘S`), `Save As...` (`⌘⇧S`), and `Save a Copy As...`.
  - Added `NewDatabaseDialog` wizard configuring solution file name, PostgreSQL database name, and credentials with automatic database creation.
  - Added `SaveCopyDialog` enabling dual-file export: `.f4b` (solution clone) vs `.f4data` (database records dump).
  - Added real-time active solution file and active PostgreSQL database chips to the status bar.
- **Layout Persistence Foreign Key Fix**:
  - Enhanced `CreateLayout` in `server/internal/schema/layout_service.go` to flexibly resolve table occurrence IDs when passed base table IDs or table names, eliminating foreign key violation errors on save.
- **Canonical Top Menu Bar (`File4BaseMenuBar`)**:
  - Implemented the complete 10-menu system (`File`, `Edit`, `View`, `Insert`, `Format`, `Records` / `Requests`, `Scripts`, `Tools`, `Window`, `Help`) pinned to the top-left margin across Desktop and WebDirect.
  - Dynamically swaps between `Records` and `Requests` menus based on operational mode (`Browse` vs `Find`).
  - Added unit test suite in `client/test/menu_bar_test.dart`.
- **Compiled Multiplatform Desktop Packages**:
  - Downloaded compiled release packages for macOS (`File4Base-macOS-universal.zip`), Windows (`File4Base-Windows-x64.zip`), and Linux (`File4Base-Linux-x64.tar.gz`) into the root `dist/` directory.

### Fixed
- **Menu Bar Alignment**:
  - Pinned top `MenuBar` to the left screen margin (`CrossAxisAlignment.stretch` and `Alignment.centerLeft`) instead of centering.
- **WebDirect Tab Icon & Branding**:
  - Replaced default Flutter browser tab favicon and application metadata in `client/web/` (`favicon.ico`, `favicon.png`, `manifest.json`, `index.html`) with official File4Base branding icons.
  - Added cache-busting queries (`?v=2`) and multi-resolution ICO/PNG declarations to bypass aggressive browser favicon caching.
- **API Connection & Browser CORS**:
  - Added `corsMiddleware` in Go server (`server/cmd/server/main.go`) handling `OPTIONS` preflight and `Access-Control-Allow-Origin: *`.
  - Added Nginx reverse proxy configuration (`client/nginx.conf`) proxying `/api/` and `/healthz` directly to the `server:8080` backend container.
  - Enabled intelligent browser origin fallback in `client/lib/main.dart` (`ServerUrlNotifier`) preventing cross-origin errors in WebDirect mode.

---

## [0.2.0] - 2026-09-23

### Added
- **Phase 4: Dynamic CRUD & Find Mode**:
  - Generic CRUD backend service (`server/internal/data/service.go`) executing dynamic SQL across DBAL engines.
  - File4Base Find Mode operator translator (`ParseFile4BaseFindCriteria` and `ExecuteFind`) supporting wildcards (`*`), ranges (`...`), exact equality (`=`), negations (`!`), and inequalities (`>`, `<`).
  - REST endpoints at `/api/v1/data/{table}` (GET, POST, PUT, DELETE, and POST `/find`).
  - Frontend record browser and stepper (`DataBrowserWidget`) with record creation, editing, deletion, and Find Mode request input.
- **Phase 5: Dynamic Layout Engine & 4 Execution Modes**:
  - Layout persistence service (`server/internal/schema/layout_service.go`) and REST API at `/api/v1/schemas/layouts`.
  - Deterministic JSON layout models adhering to `docs/specs/layout_schema.json`.
  - Interactive WYSIWYG Layout Designer (`LayoutDesignerWidget`) for **Layout Mode** (`⌘L`): 8px grid snapping, coordinate rulers, structural parts (Header, Body, Footer), draggable element placement, and real-time Object Inspector.
  - Page print preview simulator (`LayoutPreviewWidget`) for **Preview Mode** (`⌘U`): page margin boundaries, tabular data layout, totals, and pagination footer.
  - Active Table selector dropdown directly in the main top navigation bar.
- **Multiplatform Desktop & Branding Integration**:
  - Integrated official user-provided icons `file4base-dark.ico` and `file4base-light.ico`.
  - Extracted high-resolution PNG variants (`64px` through `1024px`) in `assets/branding/` and `client/assets/branding/`.
  - Configured native macOS desktop icons in `client/macos/Runner/Assets.xcassets/AppIcon.appiconset/`.
  - Configured native Windows desktop resources in `client/windows/runner/resources/app_icon.ico` and `Runner.rc`.
  - Configured WebDirect web favicon and PWA icons.
  - Added macOS network client sandbox entitlements (`com.apple.security.network.client`) to `Release.entitlements` and `DebugProfile.entitlements`.
  - Added configurable Server Host & Port Settings dialog (`ServerConnectionDialog`) allowing desktop clients to connect to custom host/ports with generic connectivity diagnostics.
  - Created desktop build and packaging script (`scripts/build_desktop.sh`) for macOS, Windows, and Linux.
  - Added GitHub Actions multiplatform desktop CI/CD workflow (`.github/workflows/desktop_release.yml`).
- **Governance & Version Management**:
  - Added `.agents/rules/versioning.md` establishing mandatory SemVer synchronization and living documentation rules for AI assistants and developers.
  - Added root `VERSION` file as the canonical single source of truth.
  - Added `docs/VERSIONING.md` describing release process, git workflow, and tagging.
  - Added `docs/api/API_REFERENCE.md` documenting all available REST API endpoints.

---

## [0.1.1] - 2026-09-23

### Added
- Automated Preflight Environment Checker (`client/lib/core/system/environment_checker.dart`) verifying Docker CLI, engine daemon status, and Apple Silicon / Intel architecture.
- Preflight warning dialog with one-click Docker Desktop launch or download links.
- WebDirect web container in `docker-compose.yml` serving client bundle on port `3000`.
- GNU General Public License v3.0 (`LICENSE`).

---

## [0.1.0] - 2026-09-22

### Added
- Clean Architecture repository structure (Decoupled Go backend + Flutter desktop client).
- Database Abstraction Layer (`internal/dbal`) supporting PostgreSQL 16 primary and MariaDB 11 profile.
- System catalog metadata tables (`sys_tables`, `sys_columns`, `sys_table_occurrences`, `sys_relationships`, `sys_layouts`).
- Dynamic DDL schema service (`server/internal/schema/service.go`) and REST API at `/api/v1/schemas/tables`.
- Manage Database visual dialog with Tables, Fields, and Relationship Graph canvas.
- Operational Mode state machine with 4 canonical File4Base modes (`Browse`, `Find`, `Layout`, `Preview`) and keyboard shortcuts (`⌘B`, `⌘F`, `⌘L`, `⌘U`).
