# FileMaker Application Compatibility — Declared Scope and Acceptance Fixture

This document answers the two things [#112](https://github.com/file4base/file4base-app/issues/112) asks the project to state: **which input path is supported**, and **what the first acceptance fixture covers**. It is a statement of scope and of what has and has not been demonstrated — not a claim that the acceptance gates in that issue pass.

The adoption target behind [#26](https://github.com/file4base/file4base-app/issues/26) is to migrate an existing FileMaker application and go on using its workflows with equivalent behaviour. #112 turns that into an executable milestone. Individual parser and runtime defects keep their own reports; this document tracks application-level compatibility.

---

## 1. The supported input path

One path, stated exactly:

| Step | What is required |
|---|---|
| 1. Export the design | In FileMaker Pro, **Tools → Database Design Report**, as **XML** (`<FMPReport>`) or **HTML**. A `<FMPSaveAsXML>` design document is also read. |
| 2. Export the records | One `FMPXMLRESULT` export per table, or the `.fmp12` itself supplied as `fmp12_base64` for record extraction. |
| 3. Convert, read-only | `POST /api/v1/solutions/migrate/preflight` returns the `ConversionReport` and writes nothing. |
| 4. Apply | `POST /api/v1/solutions/migrate/apply`, into a fresh `target_database` or merged into the session's database. Either way a failure leaves nothing behind (`#97`). |
| 5. Provision | Assign passwords and enable the migrated accounts; review the quarantined triggers. Nothing imported runs until someone turns it on. |

Versions and languages this path is written against:

- **Source**: FileMaker Pro **12 to 19**. Claris 26's *Save a Copy as XML* writes `<FMSaveAsXML>`, a different grammar, which is **refused by name** rather than half-read (`#87`).
- **Report languages**: **English** and **French** (`migration.SupportedDDRLocales`). The readers key off the anchors, table shapes and XML element names that every language shares, and translate only names that can mean one operation; anything else is quarantined with its own name (`#100`, `#103`).
- **Encodings**: UTF-8 and UTF-16 with or without a byte order mark, as FileMaker on Windows writes them. UTF-32 and undecodable bytes are refused with what to do about it (`#99`).
- **Target**: this revision of File4Base, server and client from one release.

Renaming an XML root is not a conversion path. Where a different authentic export is needed, the procedure above says which one.

---

## 2. What the first fixture scope covers

The fixture #112 asks for is **two linked FileMaker files**, Dispatch and Results, with parent surveys and child answers, a recorded source run, and a source-generated expected-results manifest. The declared scope of that first fixture is the subset below — chosen because each item is already implemented and tested at unit level, so the fixture measures whether it survives a real migration rather than whether it exists:

- Base tables, fields of every mapped type, table occurrences including a self-join, and a multi-predicate join (`#101`).
- Field behaviour: calculations whose references follow renamed fields (`#111`), validation and auto-enter options read from their nested elements (`#102`), summary fields, and global fields as columns.
- Identifier collisions: two source names that normalize alike, and a source field named `id` (`#89`).
- Scripts with their operands, order, enabled state, parameters, variables, conditions, loops, finds, `Exit Script` and a nested `Halt Script` (`#88`, `#93`, `#94`, `#95`), executed both by the runner and step by step in the debugger.
- Records: exact numbers, untrimmed text, Unicode, blank records, dates and times, and container bytes compared by hash (`#90`, `#91`).
- Cross-file behaviour: a transfer with explicit mappings and match keys, replayed, with a rejected row and an interrupted run (`#96`), and `Perform Script` into another file (`#107`).
- Accounts and triggers imported inactive, enabled explicitly, and then exercised.

### Declared boundaries

These are capability limits, stated so that an application depending on them is known to be unqualified rather than quietly mis-migrated:

- An external script stops at operations that depend on a record or window context.
- `$$` variables are shared across solutions rather than scoped per source file.
- A transfer schedule runs a transfer, not an arbitrary migrated server script.
- Repeating fields collapse to one column; `ExecuteSQL`, `Let` and `While` are not executed.

---

## 3. The fixture, and what it has already found

The fixture is executable. `server/internal/api/testdata/acceptance/dispatch_results` holds the two linked files, their record exports and an expected-results manifest, and `server/internal/api/acceptance_fixture_test.go` imports the whole application through the documented HTTP path — preflight, then apply, with nothing but the credential binding — reads the result back over the API as a client would, and compares it against the manifest. It collects every difference as a machine-readable finding instead of failing on the first one, so one run says how far the application actually migrated, and it never creates a table, field, relationship, script or account of its own: if the import did not produce it, the finding says it is missing.

Three defects it found on its first run are fixed:

- A `Set Variable` step's operand was sanitized as if it were a field name, so `$$Batch` became `batch`. The sigil is the whole of the global-versus-local distinction, and the client runner reads a variable's name from a different parameter, so the step wrote a local variable nobody read while the rest of the script went on reading an empty `$$Batch`.
- Every migrated calculation field was empty until someone edited the record. FileMaker evaluates a calculation and writes no column for it in a record export, and the bulk record import stores the values it is given rather than computing any, so a calculation reported as preserved held nothing. The formulas are now computed over the records the migration just imported.
- A table the import created came with a default occurrence named after the table, which the source file never declared. A FileMaker file with two occurrences over one base table arrived with four boxes in the graph, and a comparison of the migrated graph against the source's failed on objects the source never had. The default is now removed where the file declares that table's occurrences.

The gates the harness covers are definition identity, data fidelity by business key, field behaviour, access and activation, the read-only preflight and the all-or-nothing apply, and the cross-file transfer with a replay and an injected rejection. The manifest states which gate each check belongs to, and the fixture's own `gates` block is reported with every run.

Two gates it cannot cover, and records as unmet on every run rather than skipping in silence:

- **Workflow execution.** File4Base runs script steps in the Flutter client, so no Go harness can execute the migrated workflow. The steps' identity, order, operands and enabled state are compared instead, and `TestAcceptanceFixture_WorkflowExecutionGate` names the gap.
- **Deriving the transfer from the source.** A FileMaker `Import Records` step is quarantined with a manual action rather than converted into a transfer definition, so the fixture writes the definition itself to measure the rest of the cross-file leg. Until that step converts, an application whose workflow depends on it is not qualified.

### What the fixture's inputs still are

The fixture's design documents, record exports and manifest were **written by hand** against the published grammars, and `expected.json` declares that in its `provenance` field. The harness refuses to treat the authentic-input gate as met while that field says anything other than `filemaker`: `TestAcceptanceFixture_AuthenticInputGate` skips with the exact procedure to follow, so the gate's state is recorded on every run instead of being argued about. A handwritten input can only contain what we already believed FileMaker writes, which is why it cannot qualify a compatibility claim however much it exercises.

**Two things cannot be produced here**, and both need an actual FileMaker installation:

1. **The working two-file solution and its authentic exports.** `recipes_html_ddr.html` shows the shape of the evidence required: a genuine report, generated by FileMaker's own feature, with its provenance recorded as [`docs/LEGAL.md`](../LEGAL.md#reference-and-asset-provenance) requires. An original Dispatch/Results pair has to be built and exported in FileMaker Pro 12–19 — and, for the French gate, exported again from a French installation.
2. **The source-generated expected-results manifest.** Gate 1 of the fixture is *running the source workflow* and recording what FileMaker itself produced: final data, calculation results, script results and errors, and container hashes. Those numbers are the comparison; they cannot be written by hand without turning the test into a restatement of File4Base's own behaviour.

Until both exist, this document declares the scope above, the harness measures everything it can against the handwritten fixture, and #112 stays open on the authentic-input and workflow-execution gates. A Claris-authorized trial installation is enough to produce them; so is any suitably licensed public FileMaker solution, provided its workflow can be run and its outcomes recorded.
