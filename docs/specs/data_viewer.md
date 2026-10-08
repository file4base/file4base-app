# The Data Viewer

What the values actually are while you work: **Tools > Data Viewer** (#50).

Before it, a formula was only visible through its field. Finding out why a
calculation came out wrong meant changing the formula, saving the field,
looking at a record, and changing it back.

## What it shows

**Current record** — every field of the table with its name, its type and
what the record in hand holds. A field holding nothing says `empty` rather
than showing a blank line, because those are different things.

**Watch** — expressions you type, evaluated against that record. Each one
shows its value, or its own error. The list survives closing the dialog.

**Variables** — nothing yet, and it says so: *Set Variable*, *If* and *Loop*
stop a script with a message today, so there are no variables to show. The tab
says that rather than showing an empty list, which would read as "none are
set". When the script runner takes those steps, this is where what a script
holds goes, and the Script Debugger
([#51](https://github.com/file4base/file4base-app/issues/51)) steps through
it.

## The same engine as the fields

A watched expression is evaluated by `internal/calc` — the engine that fills
the calculation fields — against the record read from the server. The viewer
and the field therefore agree by construction: there is no second
implementation to drift.

`POST /api/v1/data/{table}/evaluate` takes the expressions, the record and the
result type, and answers a value for each. It **only reads**: evaluating a
formula is all the engine can do, and the caller needs read access to the
table, nothing more.

## Rules worth knowing

- **One bad expression is one bad line.** An expression that does not parse
  answers with its own error, including the position the parser stopped at,
  and the other expressions still answer. A viewer shows a list, and a typo in
  one line must not blank the rest.
- **A field the table does not have reads as empty**, which is the engine's
  rule for a missing field — and the viewer says which field it was, in so
  many words, rather than showing a quietly wrong answer.
- **Without a record** every field reads as empty, so `UPPER("ab")` still
  answers. That is what the viewer does before a record is chosen.
- **The result type is yours to choose.** The same expression read as Text,
  Number, Date, Timestamp or Boolean answers in that shape, exactly as a
  calculation field with that result type would store it.
- **At most 50 expressions** in one request, since each one reads the record.

## What is not here yet

- **The record is the one in hand when the viewer opens.** The dialog is
  modal, so it cannot change underneath; *Re-evaluate* reads that record
  again. A viewer that floats beside the window and follows the record as you
  browse is a larger change to the workspace.
- **No variables**, as above.
- **No related fields.** An expression reads the record's own fields, the same
  limit calculations have; see
  [calculation_formulas.md](calculation_formulas.md).
- **Nothing is written.** The viewer evaluates; it does not set a field.
