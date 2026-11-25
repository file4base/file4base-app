# Custom Functions

A solution's custom functions are FileMaker's *File > Manage > Custom
Functions*: named formulas with parameters that every formula of the
solution may call. They live in `sys_custom_functions` (name, parameters,
formula, access), travel in solution files, and are migrated from a
FileMaker XML export (see [FileMaker Migration](filemaker_migration.md#custom-functions)).
The API is in [API Reference](../api/API_REFERENCE.md#custom-functions).

## 1. Where they are called

One engine evaluates every formula that may call a custom function, so the
rules (built-in names win, the number of arguments must match, recursion)
are the same everywhere:

| Formula | Evaluated by |
| --- | --- |
| Calculation fields, stored and unstored | the server, when the record is written or read |
| Auto-enter calculated values | the server, on write |
| Validation by calculation | the server, on commit (section 4) |
| Script steps: Set Variable, Set Field, If, Else If, Exit Loop If, Exit Script, Insert Calculated Result, Go to Layout or Record by calculation, and every option marked as a calculation (`<key>_is_calculation`) | the client's evaluator when it can, the server otherwise (section 2) |
| Script parameters of buttons, layout triggers and Perform Script | the same |
| The Data Viewer | the server |

Layout calculations (hide object, tooltip, conditional formatting, portal
filter) are part of the layout work, not of this one. Value-list filters do
not exist yet. *Replace Field Contents* by calculation is evaluated for each
record by the client's evaluator only.

## 2. Script steps: the client's evaluator, then the server

The client evaluates a step's formula itself when it can: literals,
variables, fields of the record on the layout, `&`, arithmetic,
comparisons, `and`/`or`/`not` and the `Get ( … )` functions it knows. That
covers most steps and costs nothing.

Any other formula — a custom function, a related field, a function the
client does not have — is sent to `POST /api/v1/data/{table}/evaluate`
with the record in hand, the script's `$` and `$$` variables, and the
answers of the `Get ( … )` functions only the client knows (`ScriptParameter`,
`ScriptResult`, `ScriptName`, `LastError`, `LastMessageChoice`, `LayoutName`,
`WindowName`, `ActiveFieldName`, `ActiveLayoutObjectName`, `FoundCount`,
`RecordNumber`, `TotalRecordCount`, `AccountName`). The variables the
formula sets with `Let` come back and the script keeps them.

This was chosen over sending the library to the client and compiling it
with the client's evaluator:

- **One engine.** The server's evaluator has the full function set,
  FileMaker's number and date rules, related fields and the custom
  functions' recursion; the client's is a subset. Shipping the library would
  give a custom function calling `Substitute` or a related field nowhere to
  run, and two evaluators to keep identical.
- **Fast enough, and lean.** A step whose formula the client can evaluate
  makes no request. One that it cannot makes exactly one request per
  evaluation — no lookup before it, nothing cached on the client to
  invalidate. The server keeps the solution's compiled library per database
  (`data.LoadLibrary`), dropped on every change to a custom function and
  otherwise kept for a minute, so the request parses one formula and
  evaluates it.
- **A loop** evaluates its formulas again on every turn, as FileMaker does:
  a value is never reused from an earlier turn.

A formula that fails stops the script with a message naming the step and
the formula: `Script "Totals" stopped at step 4: Set Field: the formula
"Tripled ( 2 )" could not be evaluated: …`.

## 3. Manage Custom Functions

*File > Manage > Custom Functions…* (and *Server Tools > Manage*) lists the
functions with their parameters and access, and creates, edits, duplicates
and deletes them. The editor takes:

- the **name**: no spaces or operators, not starting with a digit (checked
  by the editor), not a built-in function's and not another custom
  function's, whatever the case (checked by the server);
- the **parameters**, in order;
- the **formula**, written with *Specify Calculation*, whose function list
  has the custom functions as a category of their own, and whose OK has the
  server check the formula (`POST /api/v1/formulas/check`) first: a syntax
  error, an unknown function or a wrong number of arguments keeps the editor
  open on the position of the problem;
- **Available to**: all accounts, or only accounts with full access. A
  function kept for full access is not listed for other accounts; formulas
  calling it still run, as in FileMaker.

Writing custom functions is design: full access, with the design unlocked.

### Rename, change and delete

FileMaker refers to a custom function by id, so renaming one updates every
formula calling it. File4Base stores formulas as text, so a rename
**rewrites the calls** in every stored formula: calculation fields,
auto-enter calculations, validation formulas, the other custom functions,
script steps and layout buttons and triggers. Only the calls change; text,
comments and spacing stay as written. A formula in which the new name would
read a field of that name as a call is refused (quote the field as
`${name}`), rather than changing its meaning.

A change of parameters that would break a formula calling the function
(another number of arguments), and deleting a function formulas call, are
refused with the list of those formulas; the person may go ahead anyway, and
those formulas then no longer read until they are fixed.

A recursive function is allowed, as in FileMaker; one that calls itself
before any condition could stop it (outside a branch of `If`, `Case`,
`Choose` or the right side of `and`/`or`) is refused, since it would never
end.

## 4. Validation by calculation

A field's *Validated by calculation* rule is a formula that must be true,
read in its context occurrence with `Self` the field's value and the record
as it is about to be saved. It is enforced by the server on commit, for
data entry and for scripts alike:

- with *Validate only if field has been modified* (the default), when a write
  changes the field, and on a new record when the field is not empty;
- without it (`calculation_always`), on every save of the record.

A failed rule answers `422` with the field's custom message when one is set.
With *Allow user to override during data entry* (`allow_override`), the
answer says the rule may be overridden; the client asks "Allow this value?",
and Yes sends the write again with `X-File4Base-Validation: override`. A
script's write is refused as it is. Such a rule set is not backed by a unique
index, and a migrated *not empty* rule that allows override does not make
its column `NOT NULL`, since the person may keep an empty value.

The FileMaker migration reads the rule from the XML export (`<Validation>`
with `<Calculated enable="True">` or `validateCalculation="True"` and its
`<Calculation>`), binds the formula to the imported names as calculations
are, and reads `type="Always"`/*Only during data entry* and *Allow user to
override*. A disabled validation formula is not migrated. Restoring a
solution bundle restores its records as they were saved: validation by
calculation is not applied to them.
