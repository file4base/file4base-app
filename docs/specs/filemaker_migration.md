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
   - File4Base **Design Report XML** (`<design format="file4base_design">`).

   **Not supported:** the `<FMSaveAsXML>` document that **FileMaker Pro 20 / Claris 26** (`FMDeveloperTool --saveAsXML`) writes. Its grammar is a different one — fields live in `FieldsForTables/FieldCatalog` with lower-case `datatype`/`fieldtype`, occurrences carry a nested `BaseTableSourceReference`, and script steps live under `StepsForScripts` — so renaming its root element to `<FMPSaveAsXML>` produces tables with no fields and no relationships. The converter refuses that root element by name and points at the formats above. A design whose base tables arrive without a single field is refused for the same reason, rather than reported as a successful conversion (#87).
2. **Record & Container Datasets (`data_files` / `fmp12_base64`, optional):**
   - One or more **`FMPXMLRESULT`** (or repeating-element XML) files mapped by base table name. Values are converted through the same conversion an ordinary import runs: a number keeps every digit it was exported with, text keeps its spaces, and a record whose every exported value is blank is still imported as a record. A value that cannot be converted is listed under `rejected` with its row and column, instead of failing the whole apply (#90).
   - A binary **`.fmp12`** container (`fmp12_base64`) paired with an XML/HTML DDR (`design_xml`) to extract table records, relational UUID keys, calculated/summary fields, and embedded `JPEG`/`PNG` `CONTAINER` images (`server/internal/migration/fmp12_records.go`). Records in a `.fmp12` are addressed by the numeric table and field ids of the design, so the design document must carry them: the HTML report always does, and an `<FMPReport>` XML DDR does when its `<BaseTable>` and `<Field>` elements have `id` attributes. An attachment that cannot be addressed, or that yields no records, is reported under `rejected` and `manual_action_required`; invalid base64 fails the request before any destination database is created (#91).

> **Step-by-Step Case Study:** For a complete real-world walkthrough (migrating `recipes.fmp12` + `recipes.html` with portals, self-join disambiguation, `CONTAINER` photographs, `Group Buttons`, and scripts), see the [**FileMaker to File4Base Migration Guide (`docs/tutorials/filemaker_migration_guide.md`)**](../tutorials/filemaker_migration_guide.md).

---

## 3. Preflight Dry-Run & Conversion Report Contract

Before any database table, column, layout, script, account, or row is written, `POST /api/v1/solutions/migrate/preflight` performs a read-only analysis and returns a deterministic `ConversionReport`:

| Category | Meaning | Examples |
|---|---|---|
| `preserved` | Imported 1:1 with identical semantics. | Base tables, `Text`/`Number`/`Date`/`Timestamp`/`Container` fields, Table Occurrences, `=`, `≠`, `<`, `≤`, `>`, `≥` relationships, custom & field-backed value lists, layouts & parts, compatible calculations, compatible script steps, and `FMPXMLRESULT` rows. |
| `translated` | Mapped to the closest safe File4Base construct and documented in the report. | `Time` fields → `TIMESTAMP`; `Get(CurrentDate)` → `TODAY()`; `Get(CurrentTimestamp)` → `NOW()`; global fields → standard columns; catalog-only control-flow script steps (`If`, `Loop`, `Set Variable`); FileMaker privilege sets → `owner`/`admin`/`user` roles. |
| `unsupported` | Cannot be executed by File4Base's runtime; preserved safely as inert data/comments so the solution imports cleanly. | Unsupported calculation functions (`ExecuteSQL`, `Let`, `While`) downgraded to stored columns; unsupported script steps (`Perform AppleScript`, plug-in steps) converted to disabled `Comment` steps (`[UNSUPPORTED FM STEP: ...]`); repeating fields (`maxRepetition > 1`) collapsed to single column; Cartesian (`x`) relationships skipped. |
| `rejected` | Invalid or unsafe items excluded from import. | `[Guest]` anonymous accounts; relationships referencing non-existent tables or columns; malformed XML datasets. |
| `manual_action_required` | Explicit post-migration checklist for the solution owner. | Reviewing quarantined script triggers; setting passwords for migrated accounts; rewriting unsupported calculation formulas or script steps; normalizing repeating fields. |

---

## 4. Security & Quarantine Guarantees

1. **Quarantined Triggers (`triggers_quarantined: true`):**
   - Imported scripts and script triggers (`OnFirstWindowOpen`, `OnLayoutEnter`, `OnRecordLoad`, etc.) are **never executed automatically** during or immediately after import. Layout and file triggers are stripped/disabled on import and listed under `manual_action_required` so an administrator can inspect the scripts first.
2. **Disabled Accounts & Zero Default Passwords (`accounts_disabled: true`):**
   - The `[Guest]` account is always rejected.
   - All migrated accounts are created with `is_active: false` and an unguessable random secret, recorded in `accounts_pending_password` and `manual_action_required`. An owner must explicitly assign a password and enable the account in `File > Manage > Security...` before anyone can sign in with it.
3. **Atomic Target Isolation:**
   - `POST /api/v1/solutions/migrate/apply` can either target a newly created `target_database` (rolling back and dropping the database on any schema or data error) or merge into the active session database with full rollback on failure.
