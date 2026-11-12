# Acceptance fixture: Dispatch and Results

This directory is the end-to-end application fixture of
[#112](https://github.com/file4base/file4base-app/issues/112): two linked
FileMaker files, imported whole and compared against an expected-results
manifest. `server/internal/api/acceptance_fixture_test.go` runs it.

## Provenance, stated plainly

**These artefacts were written by hand, against the published `<FMPReport>` and
`FMPXMLRESULT` grammars. FileMaker did not produce them, and no FileMaker
installation was involved.** `expected.json` says the same thing in its
`provenance` field, and the harness refuses to call the authentic-input gate
met while it says anything other than `filemaker`:
`TestAcceptanceFixture_AuthenticInputGate` skips with the procedure below.

They are useful as they stand — the fixture already measures the whole import
path and has found real defects — but a handwritten input cannot qualify a
compatibility claim, because it can only contain what we already believed
FileMaker writes. That is the point of the gate.

## What the fixture contains

| File | What it is |
|---|---|
| `dispatch_ddr.xml` | Dispatch design document: Surveys and Answers, a renamed identifier (`_Code`), a calculation reading it, a summary field, a global field, a repeating field, an occurrence alias, a self-join, a multi-predicate join, a value list, a privilege reference, and a script with a loop, a find, a nested `Halt Script`, an `Exit Script` result, a disabled step and a `Perform Script` into the other file. |
| `results_ddr.xml` | Results design document. It repeats the base table name `Surveys`, the script name `Send Batch` and the `$$Batch` variable on purpose, so a same-named local object can be shown never to stand in for the external one. |
| `dispatch_surveys.xml`, `dispatch_answers.xml`, `results_surveys.xml` | `FMPXMLRESULT` record exports: precise decimals, Unicode, untrimmed whitespace and blank values. |
| `expected.json` | The expected-results manifest and the fixture's own declaration of which acceptance gates it covers. |

## What the harness checks, and what it cannot

Covered here: definition identity (tables, fields, occurrences including the
alias and the self-join, join edges with their operator and side options, value
lists, layout bindings, script steps with their operands and enabled state,
external file references), data fidelity by business key, field behaviour,
access and activation, the read-only preflight, and the cross-file transfer
with a replay and an injected rejection.

Not covered here, and recorded as such rather than quietly skipped:

- **Authentic input.** See above.
- **Workflow execution.** File4Base runs script steps in the Flutter client, so
  no Go test can execute the migrated workflow. The harness compares the
  migrated steps' identity, order, operands and enabled state;
  `TestAcceptanceFixture_WorkflowExecutionGate` records the gap on every run.
- **Container bytes.** This fixture has no container field yet.

## Replacing this with an authentic fixture

1. In FileMaker Pro 12–19, build the two-file solution the manifest describes:
   Dispatch with parent surveys and child answers, Results with the returned
   records, linked by the business key `_Code`.
2. Run the source workflow and **record what FileMaker produced**: the final
   data, the calculation results, the script results and errors, and the
   container hashes. Those numbers become `expected.json`. They must come from
   the source run, not from File4Base, or the fixture only restates File4Base's
   own behaviour.
3. Export the design of each file with **Tools → Database Design Report** as
   XML, and each table's records as `FMPXMLRESULT`. For the French gate, export
   the report again from a French installation and add it as a second fixture.
4. Record the provenance as [`docs/LEGAL.md`](../../../../../../docs/LEGAL.md)
   requires, and set `"provenance": "filemaker"` with the real product version
   in `expected.json`.

A Claris-authorized trial installation is enough, and so is any suitably
licensed public FileMaker solution whose workflow can be run and recorded.
