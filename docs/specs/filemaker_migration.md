# Assisted FileMaker Migration (`#26`)

File4Base provides an **Assisted FileMaker XML / DDR & `FMPXMLRESULT` Migration** workflow (`server/internal/migration`, `POST /api/v1/solutions/migrate/preflight`, `POST /api/v1/solutions/migrate/apply`, and `FileMakerMigrationDialog` in the client).

---

## 1. Binary `.fmp12` Policy & Detection

A `.fmp12` (or `.fp7`) file is Claris FileMaker's proprietary binary storage container (`HBAM2`). File4Base does **not** execute or silently guess proprietary binary `.fmp12` page structures.

Instead:
1. **Server Detection:** Uploading a `.fmp12` file (detected by `.fmp12`/`.fp7` extension or the `\x00\x01\x00\x00\x00` / `HBAM2` binary header) to `POST /api/v1/solutions/import`, `POST /api/v1/solutions/import-data`, or `/api/v1/solutions/migrate/*` returns `422 Unprocessable Entity` (`Unsupported Binary .fmp12 Format`) with step-by-step instructions to export a FileMaker XML design report.
2. **Client Detection:** Selecting a `.fmp12` file in `OpenSolutionDialog` (`File > Open Solution...`) detects the binary signature and immediately opens `FileMakerMigrationDialog` with guidance.

---

## 2. Supported Source Formats

The Assisted Migration converter accepts:
1. **Design Structure (`design_xml`):**
   - FileMaker **Database Design Report (DDR) XML** (`<FMPReport>`), as exported by **FileMaker Pro 12 through 19** (`Tools > Database Design Report...`, XML output).
   - FileMaker **HTML Database Design Report (DDR)** (`recipes.html` / `Summary.html`) from the same versions.
   - A **`<FMPSaveAsXML>`** design document.
   - A **`<FMSaveAsXML>`** *Save a Copy as XML* document, as written by **FileMaker Pro 20 / Claris 26** (`FMDeveloperTool --saveAsXML`), **exported with *Include details for analysis tools***.
   - File4Base **Design Report XML** (`<design format="file4base_design">`).

   **Supported report languages: English and French** (`server/internal/migration/locales.go`, `migration.SupportedDDRLocales`). A DDR is localized, and the readers key off what is the same in every language — the HTML report's anchors (`BaseTableAnchor_`, `valScriptsSectionAnchor_`), the shape of its tables, and the element names of the XML DDR — treating the visible label as decoration. The words that do decide something (a field's type, a script step's name) are translated from a table that only holds names with one possible meaning; a step whose localized name is not in it stays quarantined as a disabled comment with its own name, its instruction id (`filemaker_step_id`) and a report entry, rather than being guessed onto another operation. An HTML report that converts to no table, or to tables with no field, is **refused** naming the supported languages, instead of returning a successful preflight that imports nothing (#100, #103).

   > FileMaker writes a numeric instruction id beside each step name (`<Step id="9" name="…">`), which would be a language-free key. Claris publishes no id table, so File4Base records the id for diagnosis and does not decide an operation from a number it cannot verify.

   **How `<FMSaveAsXML>` is read, and what that claim is worth.** Its grammar is not the DDR's: a base table's fields live in `FieldsForTables/FieldCatalog` and point back at the table rather than being nested in it, script steps live under `StepsForScripts`, and attribute names are lower-cased (`datatype`, `fieldtype`). Renaming the root element to `<FMPSaveAsXML>` is therefore not a conversion path and never was — it produced tables with no fields and no relationships. Instead, `server/internal/migration/save_as_xml.go` **relocates** each field catalog under the base table it names and each step list under the script it names, before the ordinary converter runs; `firstAttr` falls back to a case-insensitive attribute match so the lower-cased names resolve. A catalog that names a table the document does not declare is **reported**, not dropped; a catalog that names no table at all is read as the only table's when the document declares exactly one, and reported as unplaced when it declares more, because a field put on the wrong table is worse than a field reported as unplaced.

   > **Claris publishes no grammar for this format.** Its own help calls the XML a preview that "may change significantly in future versions" and is "not intended for use in production systems", and from FileMaker Pro 2026 the root carries `Has_DDR_INFO`, which is `True` only when the file was exported with *Include details for analysis tools* — the option that puts the schema detail in the document at all. A document carrying `Has_DDR_INFO="False"` is converted as far as it goes and the report names the option to re-export with. This reader is written against the structure an authentic export was **reported** to have (#87); until an authentic export is committed as a fixture, treat the path as provisional. See `docs/specs/filemaker_compatibility_scope.md`.

   A design whose base tables arrive without a single field is **refused** rather than reported as a successful conversion, whatever its root element says — the check that makes any grammar mismatch fail loudly instead of importing empty tables (#87).
2. **Record & Container Datasets (`data_files` / `fmp12_base64`, optional):**
   - One or more **`FMPXMLRESULT`** (or repeating-element XML) files mapped by base table name. Values are converted through the same conversion an ordinary import runs: a number keeps every digit it was exported with, text keeps its spaces, and a record whose every exported value is blank is still imported as a record. A value that cannot be converted is listed under `rejected` with its row and column, instead of failing the whole apply (#90).
   - A binary **`.fmp12`** container (`fmp12_base64`) paired with an XML/HTML DDR (`design_xml`) to extract table records, relational UUID keys, calculated/summary fields, and embedded `JPEG`/`PNG` `CONTAINER` images (`server/internal/migration/fmp12_records.go`). A cell a native file still holds for an unstored calculation (*Do not store calculation results*) is stale, since FileMaker computes the field whenever it is shown, and is not read whatever the field's type, so a leftover container reference cannot refuse the record; an HTML report marks such a calculation with *Do not store*. The files FileMaker kept for a Container calculation are its own answers and are not read either: File4Base computes the calculation as a reference, and one it cannot convert (an HTML report's calculation reading another table) becomes an empty container field reported under `manual_action_required`. Containers in FileMaker's open external storage are read from the storage folder supplied with the request and checked against the size and MD5 FileMaker recorded; containers in secure external storage are encrypted by FileMaker and are refused until the field is changed to open or embedded storage in FileMaker. Records in a `.fmp12` are addressed by the numeric table and field ids of the design, so the design document must carry them: the HTML report always does, and an `<FMPReport>` XML DDR does when its `<BaseTable>` and `<Field>` elements have `id` attributes. An attachment that cannot be addressed, or that yields no records, is reported under `rejected` and `manual_action_required`; invalid base64 fails the request before any destination database is created (#91).

> **Step-by-Step Case Study:** For a complete real-world walkthrough (migrating `recipes.fmp12` + `recipes.html` with portals, self-join disambiguation, `CONTAINER` photographs, `Group Buttons`, and scripts), see the [**FileMaker to File4Base Migration Guide (`docs/tutorials/filemaker_migration_guide.md`)**](../tutorials/filemaker_migration_guide.md).

---

## 3. Preflight Dry-Run & Conversion Report Contract

Before any database table, column, layout, script, account, or row is written, `POST /api/v1/solutions/migrate/preflight` performs a read-only analysis and returns a deterministic `ConversionReport`:

| Category | Meaning | Examples |
|---|---|---|
| `preserved` | Imported 1:1 with identical semantics. | Base tables, `Text`/`Number`/`Date`/`Timestamp`/`Container` fields, Table Occurrences, `=`, `≠`, `<`, `≤`, `>`, `≥` relationships, custom & field-backed value lists (with their second field when it is on the same table occurrence as the first, "show values only from second field", sort field and "include only related values" options), layouts & parts, compatible calculations (Container result calculations included, kept as calculations that refer to the container they answer), compatible script steps, and `FMPXMLRESULT` rows. |
| `translated` | Mapped to the closest safe File4Base construct and documented in the report. | `Time` fields → `TIMESTAMP` (a time of day on 1 January 0001); `Get(CurrentDate)` → `TODAY()`; `Get(CurrentTimestamp)` → `NOW()`; global fields → standard columns; catalog-only script steps (`Set Variable`, `Print`, `Insert from URL`); FileMaker privilege sets → `owner`/`admin`/`user` roles. |
| `unsupported` | Cannot be executed by File4Base's runtime; preserved safely as inert data/comments so the solution imports cleanly. | Calculations calling a function File4Base does not run (`ExecuteSQL is not supported`, a plug-in function shown as `<Function Missing>`, a custom function that could not be migrated) downgraded to stored columns; unsupported script steps (`Perform AppleScript`, plug-in steps) converted to disabled `Comment` steps (`[UNSUPPORTED FM STEP: ...]`); Cartesian (`x`) relationships skipped. |
| `rejected` | Invalid or unsafe items excluded from import. | `[Guest]` anonymous accounts; relationships referencing non-existent tables or columns; malformed XML datasets. |
| `manual_action_required` | Explicit post-migration checklist for the solution owner. | Reviewing quarantined script triggers; setting passwords for migrated accounts; rewriting unsupported calculation formulas or script steps; normalizing repeating fields; recreating a value list whose field cannot be indexed (an unstored calculation or a summary field, which FileMaker refuses for a value list and which would offer nothing), which is not migrated; an unindexable second field is dropped and reported as `unsupported`. |

### Custom functions

A solution's custom functions (*File > Manage > Custom Functions*) are read from the XML DDR (`File/CustomFunctionCatalog/CustomFunction`, with `parameters="a;b"` and `visible`), Save a Copy as XML (`Structure/AddAction/CustomFunctionsCatalog`, with its `Parameter` objects and `access`) and the HTML DDR (its *Custom Functions* section), and imported into `sys_custom_functions` before any field, so a calculation calling one is read against it:

- A function may call itself, call one that calls it back, and call one listed after it: they are all read together, whatever the catalog order.
- A function named like a built-in function (whatever its case) is not imported, as FileMaker does not allow it, with every function and calculation that calls it; the report asks to rename it.
- Its formula is rewritten like a field calculation's, against every table occurrence (the HTML DDR's too, read once its occurrences are), in the `source_locale`: field references renamed to the imported names, numbers written with `.`. Its parameters are left alone.
- A field whose imported name is a custom function's (`_rate` imported as `rate` beside a function `Rate`) is written `${rate}` in the formulas that read it, so it still reads the field.
- Its *Availability* is kept: `all` (*All accounts*) or `full_access`.
- Each function is reported once: `preserved`, or `translated` when its formula had to be rewritten, naming the function, its parameters, its availability and its formula.
- A function that does not read (a syntax error, a field no occurrence has, a plug-in function, `ExecuteSQL`) is `unsupported`, with a `manual_action_required` item to rewrite it; so is every function that calls it. The other functions still import.
- A calculation calling a function that could not be migrated is quarantined like any formula that does not read, and says which function it calls and why: `calls ExecuteSQL: ExecuteSQL is not supported`, `calls a function FileMaker shows as <Function Missing>` (a plug-in function), or `calls the custom function X, which could not be migrated`.

`ExecuteSQL` is not implemented: File4Base calculations do not run SQL queries. A French DDR writes the built-in functions inside a custom function in French, as it does in every formula; those are not translated yet.

### Auto-enter options

A field's auto-enter options are read from Save as XML (the `type` attribute and each option's `enable`), from the XML DDR and the clipboard (the `constant`, `calculation`, `lookup` and `allowEditing` flags, the stamp in `value`, `<Serial nextValue increment generate>`), and from the English HTML DDR's *Auto-Enter* list. They are kept in the column's `default_value` ([Auto-Enter Field Options](auto_enter_options.md)) and reported once per field: `preserved` lists what was kept (*Auto-enter options: serial number on creation, next value 1001, increment 1; …*), and each option that could not be is `unsupported`, with the reason, plus a `manual_action_required` step:

| Option | Kept | Dropped, with the reason |
| --- | --- | --- |
| Creation / modification stamps | Date, Time, Timestamp, Name, Account Name | A stamp the field's type cannot hold (a name in a number field). |
| Serial number | Next value, increment, on creation / on commit | A serial in a field that is neither text nor number, a number field whose next value is not a whole number, a next value with no digit. |
| Data | The constant | — |
| Calculated value | The formula bound to the imported names, as calculations are (below), its context, *Do not replace* and *Do not evaluate if all referenced fields are empty*; it may call the solution's custom functions | A formula that cannot be bound (a field or an occurrence no relationship reaches, a custom function that could not be migrated), a container field. The HTML DDR keeps only formulas reading plain fields of their own table and custom functions. |
| Looked-up value | The starting and source occurrences, the field, *Don't copy contents if empty* and the no-match option. The source occurrence may be several relationships away from the starting one. When the source names no starting occurrence, or one that was not migrated, the lookup starts from an occurrence of the field's table a path leads from. | A source occurrence no path of relationships reaches from where the lookup starts (the report names both), a field it does not have. The HTML DDR does not keep lookups. |
| Value from last visited record | Kept for the dialog | Not entered: always reported. |
| Prohibit modification | Kept | — |

A migration's records are written as they are: no option enters a value into them, and the next serial value is the source's.

### Field names that had to change, and the formulas that read them (`#111`)

An imported field does not always keep its source name: `sanitizeIdentifier` strips what a SQL identifier cannot carry, so `_code` becomes `code`, and two source names that normalize alike are told apart as `code` and `code_2` (`#89`). A calculation written against the source names would then read a column the table does not have.

The converter therefore builds, per table, the complete map from every name a field can be referred to — what the source called it and what it was imported as — and resolves each calculation against that map once all of the table's fields are known:

- A formula whose references all resolve is **rewritten** to the imported names and reported as `translated`, naming the formula before and after. Quoted text and function names are never touched, so `IF(_code = "_code"; UPPER(_code); "_code")` becomes `IF(code = "_code"; UPPER(code); "_code")`.
- A name is matched by its exact spelling before it is matched case-insensitively, so a `Code`/`code` pair — two fields, imported as `code` and `code_2` — stays unambiguous.
- Parsing is not proof. The formula parser accepts any identifier, so a reference to a field that does not exist parses perfectly. A formula naming something the map cannot place is **quarantined**: the column keeps the type the calculation would have produced and stores a plain value, `unsupported` names the reference that could not be placed, and `manual_action_required` says to repoint it. The alternative — reporting the field as preserved and failing the apply on it — is what `#111` reported.

---

## 4. Security & Quarantine Guarantees

1. **Quarantined Triggers (`triggers_quarantined: true`, #26):**
   - Imported scripts and script triggers are **never executed automatically** during or after import. Every layout and layout-object trigger of a Save a Copy as XML (`<ScriptTriggers>`) is kept in its layout's `script_triggers` list with its event, the imported script it runs (found by its FileMaker id, then by a name only one imported script has; one not found, one whose name several imported scripts have, and one in another FileMaker file — kept with its `file_reference` and never bound to a script of this solution with the same id — keep their name, unbound), its parameter rewritten like any calculation, `enabled: false` and `quarantined_by_migration: true`. A client runs the layout's `on_layout_enter` / `on_layout_exit` actions that are not disabled, and only the `script_triggers` entries marked `enabled: true`, so a migrated trigger never fires until someone enables it.
   - The report has one `manual_action_required` item per solution (`N script trigger(s) were imported disabled (… on layouts, … on layout objects) … Review them in File > Manage > Script Triggers`) instead of one per trigger, plus one item per trigger with a problem: a script not found, a script name several scripts have, a script in another FileMaker file, a parameter that does not read, a trigger FileMaker runs in Find mode only. Triggers that name no script, and triggers of each event File4Base does not run (`unsupported`, one item per event), are counted in one item each.
   - File > Manage > Script Triggers lists every trigger by layout, with its object, event, script, parameter and status, filters and searches them, and enables or disables a selection or every trigger without a problem in one atomic change (`GET` / `PUT /api/v1/schemas/script-triggers`). File4Base runs `OnLayoutEnter`, `OnLayoutExit`, `OnRecordLoad`, `OnRecordCommit`, `OnRecordRevert`, `OnModeEnter` and `OnModeExit` set on a layout, and `OnObjectEnter`, `OnObjectExit`, `OnObjectModify`, `OnObjectValidate` and `OnObjectSave` set on a field, in the modes FileMaker ran them in; triggers of other events, and object triggers set on anything but a field (`unsupported_object`), are kept, disabled, and cannot be enabled. Layout Setup shows the layout's triggers with a switch (owner or admin, through the same API), and the object inspector those of the selected object. A copied layout gets new trigger ids; an id naming two triggers is refused with `409`.
   - File triggers (`OnFirstWindowOpen`, `OnLastWindowClose`, …) are not imported, as File4Base does not run them; the report lists them in one `unsupported` item per solution.
   - Solution files carry the triggers and their `enabled` flag.
2. **Disabled Accounts & Zero Default Passwords (`accounts_disabled: true`):**
   - The `[Guest]` account is always rejected.
   - All migrated accounts are created with `is_active: false` and an unguessable random secret, recorded in `accounts_pending_password` and `manual_action_required`. An owner must explicitly assign a password and enable the account in `File > Manage > Security...` before anyone can sign in with it.
3. **Atomic Target Isolation:**
   - `POST /api/v1/solutions/migrate/apply` can either target a newly created `target_database` (rolling back and dropping the database on any schema or data error) or merge into the active session database with full rollback on failure. A merge into an existing database records its design before the first write (`schema.Service.SnapshotDesign`) and, on any later failure, deletes every design object the migration added and re-imports the recorded design, so a definition the merge changed is the one it was (`RestoreDesign`). Records are not part of the rollback because none are written: the records are imported after the schema, in one transaction of their own. If the restore is itself incomplete, the problem report says so rather than implying nothing was left behind (#97).

---

## 5. `Import Records` / `Export Records` and Transfer Definitions (`#96`)

FileMaker's `Import Records` and `Export Records` steps name a **file** and a mapping of their own. File4Base's equivalent is a **transfer definition**: a named, persisted object that records the source and destination solutions and tables, explicit field mappings, stable match keys, the add/update mode, the unmatched-record policy, the rejected-row policy and the date interpretation. The script step names the definition; the definition holds everything else. See [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 17 for the stored shape and the endpoint.

The two steps are therefore **converted but not configured**:

- the step is imported as an enabled `Import Records` / `Export Records` step, so the script reads as it did and nothing disappears out of its middle;
- it carries **no target**, because the FileMaker file reference and its mapping are not a File4Base transfer definition and cannot be turned into one automatically: the field names, the match keys and the policies are decisions about two solutions that the source document does not state;
- each one is reported under **`manual_action_required`**, naming the script, the step and its position, and saying that a transfer has to be created and named in it;
- until one is, the step **stops the script** when it runs, reporting that it names no transfer. It is never answered with a *script* of the same name — a transfer and a script are different objects, and falling back from one to the other would run something nobody asked for.

A converted step whose transfer reaches another solution also stops the script when the session is not signed in to that solution, naming the solution it needs. A transfer definition carries no credential, so authority comes from a session of each end and from nothing else.

`Loop`, `Exit Loop If` and `End Loop` convert to steps the client runner really executes (`#96`), so a migrated loop runs rather than halting the script.

### What is still not migrated

- **FileMaker Server schedules.** A FileMaker Server schedule that ran a script is not converted: it lives on the server, not in the design document. Its File4Base counterpart for moving records is a **transfer schedule** (`#108`, [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 19), created by an administrator of each solution and run by the File4Base server. A schedule runs one transfer definition; it does not run a script.

---

## 6. `Perform Script` Into Another File and File References (`#107`)

A FileMaker `Perform Script` step can call a script in **another file**. The XML route keeps that file's name as the step's `file_reference` parameter. File4Base turns each such file into a **file reference**: a named design object (`sys_file_references`) that says which File4Base solution the file became. See [`API_REFERENCE.md`](../api/API_REFERENCE.md) § 18.

> **Correction.** Until `#107` the client runner ignored `file_reference` and looked the script up by name in the *current* solution, so a migrated step calling "Receive" in another file ran this solution's "Receive" if one existed. The `#96` version of this document said such a step was "reported rather than matched to a same-named local script"; that was not what the runner did. It is now.

What the migration does:

- every external file a converted `Perform Script` step calls becomes **one** file reference, matched case-insensitively, created **unmapped** (no solution) because nothing in the design document says which solution the file is;
- each one is reported under **`manual_action_required`**, naming the steps that use it;
- the step keeps its `file_reference` and its `script_name` exactly as the source wrote them.

What the runner does with such a step:

| Situation | Result |
|---|---|
| The reference is unmapped, or this solution has no reference of that name | The script **stops** and says which. |
| The reference is mapped to another solution this session is not signed in to | The script **stops** and names the solution. |
| The reference is mapped to this very solution | An ordinary `Perform Script`, with this solution's window. |
| The reference is mapped to another solution and the user is signed in to it | The script runs **in that solution**, on that session, **without a window** (below). |

In none of these cases is the script looked up in the calling solution. A same-named local script is a different script, and running it is worse than stopping.

**A script performed in another solution has no window here.** The window on screen belongs to the calling solution, so a step that acts on a window — `New Record`, `Set Field`, `Go to Layout`, `Perform Find`, `Commit Records`, … — would act on the wrong solution's table. The first such step stops the script and names itself. Steps that need no window run: `Import Records` / `Export Records`, `Set Variable`, `If` / `Loop`, `Perform Script` within that solution, `Exit Script`, `Halt Script`, `Show Custom Dialog`, `Open URL`, `Comment`. A field reference is not answered from the record on screen, so an expression that reads one stops the script. A transfer in the called script that reaches back to the calling solution uses the session the user is working in there. `Halt Script` in the called script stops every caller (`#95`).

`$$global` variables are one set per client session, shared by every solution a script reaches — unlike FileMaker, which keeps them per file.

### External table occurrences

An occurrence of a table of another FileMaker file — through an External Data Source of type FileMaker — is migrated as an **external occurrence** instead of being refused (see [`relationships_and_portals.md`](relationships_and_portals.md#occurrences-of-another-solutions-table)):

- its data source becomes a file reference, created **unmapped**, with the source's paths (`UniversalPathList` in Save a Copy as XML, `pathList` in an XML DDR, whose paths are split at each `file:`, `fmnet:`, `filemac:`, `filewin:`… prefix so a path with a space stays whole);
- the occurrence keeps that reference and, when the document names it (an XML DDR's `baseTable`), the table; Save a Copy as XML does not, so the occurrence starts unbound and its table is resolved when the reference is bound, never guessed;
- relationships, value lists and calculations that go through it name its fields as the other file's own migration names them (the same sanitization; a field named `id` becomes `id_2`). A value list on an external field keeps its first field only; a second field or related values are reported as unsupported;
- a layout on an external occurrence keeps that occurrence and is bound to no local table; once the reference is bound, the client browses that solution's records on it, writing them as far as the account held there may, and until then shows a placeholder naming the file reference and what to do;
- a field of the other file that the document names **by id only** — FileMaker writes `name=""` and keeps the field's `id` when that file was not reachable as it saved the design — is kept as the marker **`#<id>`** wherever its name would go: a relationship criterion, a value list, a calculation (`Occurrence::<Field Missing>` in the text, matched to the `FieldRef` chunk that gives the id, becomes `${Occurrence}::${#<id>}`) and a layout field object (`field_name: "#<id>"` with its `table_occurrence`). It is reported under `manual_action_required` with the ids, never refused, and resolved by that id in the other solution's table when the reference is bound;
- each occurrence is listed under **`manual_action_required`**, and in the report's **`external_occurrences`**: `occurrence`, `data_source`, `paths`, `table` (when known), `fields` (used through it), `suggested_database` (the one installed solution whose database or solution name matches a path of the data source, when exactly one does) and `cycle` (signed-in callers only: that solution has an external occurrence leading back to the file being migrated — install both, then bind each). A suggestion is never applied.

An occurrence through an ODBC data source, or through a data source the document does not describe, is still **rejected**.

Every migrated column keeps the **FileMaker id** of its field, from Save a Copy as XML, an XML DDR or an HTML DDR, as `sys_columns.source_field_id` (`source_field_id` in the solution bundle, absent for a column created in File4Base). That id is what another file's markers are resolved against, so a file read by another file's external occurrences is migrated with File4Base as well rather than recreated by hand.

Two fields of the other file whose names sanitize to the same name are renamed by that file's own migration in a way this file cannot know; binding then reports the field as missing, and the reference is verified again after the field is fixed.

## 7. `Save Records as PDF` and `Send Mail` (`#133`)

Both steps convert as runnable steps with their options (paths, records, append, auto-open, create e-mail; addresses, subject, message, attachments, one e-mail per record, collected addresses, and the *via SMTP server* sender name, sender address and reply-to). What runs is in the [Script Workspace spec](script_workspace_spec.md) § 8. A step gets a migration note, and a `manual_action_required` item, only when it needs something from the administrator or will not run as written:

| Step | Migration note |
|---|---|
| `Send Mail` via SMTP server, one e-mail per record, or with attachments | Needs an SMTP profile in Server Tools > Send Mail (SMTP); without one the step fails with `1506` (attachments with dialog: a draft and a notice instead). The step's own SMTP server, port and password are **never migrated**: the report records only that the step named one (`smtp_server_in_step`). |
| `Send Mail` via e-mail client without dialog | Documented deviation: sent through the SMTP profile when there is one, a draft otherwise. |
| `Send Mail` via e-mail client with dialog, no attachment | No note: it runs as written (a draft in the user's e-mail client). |
| `Save Records as PDF` into a variable | Not supported, error 3. |
| `Save Records as PDF` with security options (a password to open, print or edit) | Not supported: the step fails with error 3 rather than write an unprotected PDF. The password is not migrated. |
| Either step naming a **fixed path** outside the script folders (`filemac:/Users/…/Desktop/…`, `filewin:/C:/…`) | Change it to `Get ( TemporaryPath )` or `Get ( DocumentsPath )`; as written the step fails with `800` (PDF) or `100` (attachment). A path in a `$variable` is not flagged: it is checked when the step runs. |

**Passwords in step options.** The raw options of every converted step (`filemaker_parameters`) never hold a password: any option whose element, `type` or attribute names a password (an SMTP password, a PDF password) is stored as `[redacted]`.
