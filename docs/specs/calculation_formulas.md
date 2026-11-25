# Calculation formulas

A **calculation field** holds a value that File4Base works out from the record
instead of a value somebody types. You give the field a formula and a result
type; when a record is written, the formula is evaluated and the answer is
stored in the field's column.

Formulas are evaluated by the server, in Go, not translated to SQL, so a
calculation gives the same answer on PostgreSQL and on MariaDB.

- Implementation: [`server/internal/calc`](../../server/internal/calc)
- Issue: [#30](https://github.com/file4base/file4base-app/issues/30)

---

## When a formula runs

| Moment | What happens |
| --- | --- |
| A record is created | Every calculation field of the table is computed and stored. |
| A record is updated | The whole record is read, the update applied on top, and every calculation recomputed. Changing a field a formula reads therefore updates the result. |
| A formula is saved | The formula is checked (see [Validation](#validation)), and every record of the table is recomputed so the stored rows match the new formula. |

Because the answer is **stored in the field's own column**, a calculation field
is found on and sorted by exactly like any other field.

A value sent for a calculation field is ignored: the formula owns it.

## Unstored calculations and related fields

A calculation saved with `"stored": false` (Field Options: *Store the result*
unchecked; FileMaker's *Do not store calculation results*) is **computed each
time a record is read** instead of when it is written. Its column stays empty.
Every read of the data service computes it: one record, a list or page, a
find, the related records of a portal, an export, and the Data Viewer.

```json
{"formula": "Sum ( ${Invoice Items}::line_amount )", "result_type": "Number",
 "stored": false, "context": "Invoices"}
```

Only an unstored calculation may read **related fields**, `Occurrence::Field`,
as in FileMaker. Adding, changing or deleting a related record changes it
without its own record being written.

- The records are reached from the calculation's **context** occurrence
  (`context`; empty means the occurrence named like the table, else the
  table's first) along the shortest chain of relationships to the occurrence
  named, across as many relationships as it takes. The last relationship's
  sort order is the order `Occurrence::Field` takes its first value from and
  `List` lists them in.
- An empty match field relates nothing. A text match field holding several
  lines is a multi-key: it matches records holding any of them. `<`, `≤`, `>`,
  `≥` and `≠` relationships read the right records from either side.
- Related records are read **as the caller**: a related field of a table they
  may not read evaluates to `<No Access>` (an error, so the field is empty and
  the Data Viewer shows it), never to its value. Every table the path crosses
  must be readable.
- A formula that fails, or a related occurrence no relationship reaches any
  more, leaves the field empty for that record — FileMaker's `?` — rather than
  refusing to show the record. Formulas that read each other through
  relationships in a ring stop there with an empty value.
- Only what is read is computed: a related field, and the unstored fields it
  reads. Each record's unstored field is computed once per request; chains
  are followed up to 32 levels.
- Each request loads the relationship graph once. Before the records of a
  page are computed, what their formulas read is read ahead for all of them:
  each relationship of each path in one statement for the whole page (a
  branch per match value, in chunks of 500), then, the same way, what the
  calculations of the records reached read in their turn. A page therefore
  takes a fixed number of statements, however many records it shows.
- A formula reads no more related records than it needs, and only the
  fields it uses: `Occurrence::Field` reads the first related record in the
  relationship's sort; `Count`, `Sum`, `Average`, `Min` and `Max` of a stored
  field of the records one relationship away are totalled by the database,
  without carrying the records back; `Last` and `GetNthRecord` read the one
  record they give, the last holding a value or the nth, when the
  relationship sorts its records (then by id, so that records sorting alike
  keep one order) and they are one relationship away; `List` and the other
  reads of every value take at most 10,000 related records of one record. A
  formula reading more fails (the field is empty) rather than loading them
  all. A relationship that does not sort has no record order to count from,
  so its `Last` and `GetNthRecord` read every record, within that limit. A
  `GetNthRecord` whose number is computed for each record reads every
  record ahead for the page, or, for a record with more related records
  than that, its one record in a statement of its own.
- An occurrence of **another solution's table** (#107) is read the same way,
  in that solution, as the account the session holds there; that solution's
  own unstored calculations are computed there. See
  [relationships_and_portals.md](relationships_and_portals.md#occurrences-of-another-solutions-table).

Saving checks that a **stored** calculation reads no related field ("make it
unstored") and no unstored calculation, since what it stores would go stale;
and that each related field of an unstored one names an occurrence and a field
that exist, reached by relationships from its context.
An unstored calculation's column holds nothing, so it cannot be required
(not nullable), and a relationship cannot match on it: making a match field
unstored, or drawing a relationship on an unstored one, is refused. A
solution import checks its formulas once all its fields and relationships
exist, whatever order they are declared in. Keyed imports, like inserts and
updates, never write an unstored calculation.

An unstored calculation **cannot be found on, sorted by or summarized**: its
value does not exist in the database. Such a request is refused with
`422 Unstored Calculation`, rather than searched or sorted in memory.

A migration keeps FileMaker's choice (`storeCalculationResults`) and the
formula's context occurrence, binds `Occurrence::Field` to the imported
occurrence and column, and makes unstored (and nullable) any calculation that
reads a related field or an unstored calculation, as FileMaker requires. A
related field no relationship reaches is quarantined and reported. A
calculation a relationship matches on stays stored when it reads only its own
record, and is quarantined when it reads related records.

## Result types

The result type decides what the answer is stored as, and therefore how the
field sorts and compares.

| Result type | Stored as | Notes |
| --- | --- | --- |
| Text | text | The default. |
| Number | number | Sorts and compares numerically. |
| Date | date | The formula must produce a date. |
| Time, Timestamp | timestamp | Both spellings mean a timestamp. |
| Boolean | true / false | |
| Container | a reference to a container field | See [Containers](#containers). |

An empty answer is stored as nothing (SQL `NULL`), not as an empty string.

## Containers

A formula sees a container field as the file it holds, never its bytes:

| Formula | Answer |
| --- | --- |
| `GetAsText ( Photo )`, `Photo & ""` | The file's name, as stored with it; empty for a file stored without one. |
| `IsEmpty ( Photo )` | 1 when the field holds no file. |
| `GetContainerAttribute ( Photo ; "filename" )` | The file's name. |
| `GetContainerAttribute ( Photo ; "fileSize" )` | Its size in bytes. Other attributes are empty. |
| `If ( … ; Front ; Back )`, `Case`, `Choose`, `Let`, `GetRepetition ( Photo ; 2 )`, `Photo[2]`, `Related::Photo` | The container itself, so a Container result returns that file. |

A calculation whose result type is **Container** answers a container. Its
column stores where the file is (`{"table", "record", "field"}`, the field
being `field[n]` for a repetition, and `database` for a record of another
solution's table, read there as the session's account), not a copy of it, stored or unstored; a
read returns the file the reference names, as base64 with its metadata under
`__container_meta`, exactly as a container field reads, and an export writes
it the same way. The reference follows the field: a record whose file changes
shows the new one. Within one write, a calculation reading a Container result
of the same record sees the file that write stores, and a record added by an
import refers to itself. A reference whose record or field is gone is empty:
`IsEmpty` is 1 and a read gives nothing. A Container calculation whose answer is not a container
(text, a number) is empty, as FileMaker shows no file. A file in a table the
reader may not read is empty for that reader.

## Repeating fields

A field can hold up to 32,000 **repetitions** (its *maximum number of
repetitions*, 1 when it does not repeat), as in FileMaker. Formulas see them
as FileMaker does:

| Formula | Answer |
| --- | --- |
| `Readings[3]`, `GetRepetition ( Readings ; 3 )` | Repetition 3; empty past the field's maximum, and for a field that does not repeat past repetition 1. `Related::Field[2]` reads repetition 2 of the first related record. |
| `Readings` | Repetition 1, or in a repeating calculation the repetition being computed. |
| `Sum ( Readings )`, `Count`, `Average`, `Min`, `Max`, `List`, `Last` | Every non-empty repetition of a single repeating field, as if each were an argument. `Sum ( Readings[2] )` is that repetition only. |
| `Get ( CalculationRepetitionNumber )` | The repetition being computed: 1 in a calculation that does not repeat. |
| `Extend ( Rate )` | The first repetition of a field that does not repeat, in every repetition of a repeating calculation. |

A **repeating calculation** (a calculation field whose maximum is above 1) is
evaluated once per repetition. Repetition *n* reads repetition *n* of every
repeating field it names without an index, and nothing from a field that does
not repeat unless it is wrapped in `Extend`. Each answer is stored in
repetition *n* of the field; an unstored one is computed when the record is
read.

Limits: a related repeating field named without an index inside a repeating
calculation is empty past repetition 1 (use `Related::Field[n]` or `Extend`);
an unstored repeating calculation of a related table is computed for
repetition 1 only when it is read through a relationship.

## FileMaker compatibility

The engine reads FileMaker's calculation language and gives FileMaker's
answers. Its behaviour is pinned by
[`testdata/conformance.json`](../../server/internal/calc/testdata/conformance.json):
713 synthetic formulas evaluated in FileMaker Pro 26.0.2 (French), with the
exact text FileMaker returned or the error it raised. `TestConformance`
checks every one; the few deliberate differences carry a `note`. See
[`testdata/README.md`](../../server/internal/calc/testdata/README.md) for how
the answers were collected.

## Grammar

```
expression     := orExpr
orExpr         := andExpr ( ( "or" | "xor" ) andExpr )*
andExpr        := notExpr ( "and" notExpr )*
notExpr        := "not" notExpr | comparison
comparison     := concat ( ( "=" | "≠" | "<>" | "<" | "≤" | "<=" | ">" | "≥" | ">=" ) concat )*
concat         := additive ( "&" additive )*
additive       := multiplicative ( ( "+" | "-" ) multiplicative )*
multiplicative := power ( ( "*" | "/" ) power )*
power          := unary ( "^" unary )*
unary          := ( "-" | "+" ) unary | primary
primary        := NUMBER | TEXT | FIELD | VARIABLE | "¶"
                | FUNCTION "(" [ expression ( ";" expression )* ] ")"
                | "Let" "(" ( binding | "[" binding ( ";" binding )* "]" ) ";" expression ")"
                | "(" expression ")"
```

- **Fields** are written as FileMaker writes them: `Amount`, `Inscrit Zapping`
  (names with spaces, accents or operators are matched against the fields the
  table really has, longest first), `Invoices::Total` for a related field,
  `${A + B}` to force a name, `field[2]` for a repetition.
- **Variables**: `$local`, `$$global`, and the names `Let` binds.
- **Text** is in double quotes; `\"`, `\\` and `\¶` escape, and `¶` is a
  return. Comments are `//` and `/* */`.
- **Numbers follow the file's locale.** A French file writes `1,5` and
  separates arguments with `;` only; an ISO file writes `1.5`.
- Function names are case-insensitive, in English as FileMaker stores them.

## Values

| Type | Held as | Written as text (French locale) |
| --- | --- | --- |
| Number | exact decimal | `,5`, `1234,5`; with an exponent below 1e-9 (`1,0E-10`) and from 1e19 when it ends in zeros (`1,0E+19`). Results are kept to 16 places, or to `SetPrecision`'s; a division keeps two more until it is used, so `1/3*3` is 1 |
| Text | Unicode text | itself |
| Date | day number, 1 = 01/01/0001, up to 31/12/4000 | `24/11/2026` |
| Time | seconds, may pass 24 h or be negative | `01:02:03`, `49:30:00`, `-00:30:00` |
| Timestamp | seconds since 01/01/0001 | `24/11/2026 13:45`, seconds shown only when not zero |

FileMaker's rules apply throughout:

- **Text as a number** keeps the digits and the decimal separator and ignores
  the rest: `"12-A" * 1` is 12, `"Mary" * 2` is 0, `"(4x2)"` is -42. An empty
  value is empty, not 0, for `Abs`, `Round`, `-` and the like.
- **Comparison** is as text when either side is text, ignoring case and
  accents (`"é" = "E"`), so `"10" < 9`. Two numbers, dates or times compare as
  numbers. `Max` and `Min` compare as text only when both values are text.
- **Arithmetic** is exact: `0,1 + 0,2 = 0,3`. Division keeps 18 places,
  `SetPrecision ( x ; n )` up to 400. `Sqrt`, `Exp`, `Ln`, `Log` and fractional
  powers are exact to the current precision; trigonometry uses binary floating
  point, as FileMaker does.
- **Dates and times combine** as in FileMaker: date + number is a date, date +
  time a timestamp, timestamp - timestamp a time.
- **Errors** (`1/0`, `Sqrt ( -1 )`, `Date` from text that is no date) stop the
  formula; `IsValid` and `If`/`Case` branches not taken are never evaluated.

## Functions

The engine implements the FileMaker functions measured in real solutions
and the families around them:

| Family | Functions |
| --- | --- |
| Logical | `If`, `Case`, `Choose`, `Let`, `While`, `IsEmpty`, `IsValid`, `IsValidExpression`, `Evaluate`, `GetField`, `Get ( … )`, `GetNthRecord` (related fields) |
| Text | `Left`, `Right`, `Middle`, `Length`, `Position`, `PatternCount`, `Substitute`, `Replace`, `Filter`, `Trim`, `TrimAll`, `Upper`, `Lower`, `Proper`, `Exact`, `Quote`, `Char`, `Code`, `SerialIncrement`, `LeftWords`, `MiddleWords`, `RightWords`, `WordCount` |
| Lists | `List`, `ValueCount`, `GetValue`, `LeftValues`, `MiddleValues`, `RightValues`, `FilterValues`, `UniqueValues`, `SortValues` |
| Numbers | `Abs`, `Int`, `Floor`, `Ceiling`, `Round`, `Truncate`, `Mod`, `Div`, `Sign`, `Sqrt`, `Exp`, `Ln`, `Log`, `Lg`, `Sin`, `Cos`, `Tan`, `Asin`, `Acos`, `Atan`, `Degrees`, `Radians`, `Pi`, `Random`, `SetPrecision` |
| Aggregates | `Sum`, `Average`, `Count`, `Min`, `Max`, `List`, `Last` — over several values, or over one field's related records or repetitions |
| Dates and times | `Date`, `Time`, `Timestamp`, `Year`, `Month`, `Day`, `Hour`, `Minute`, `Seconds`, `DayName`, `DayOfWeek`, `DayOfYear`, `MonthName`, `WeekOfYear`, `WeekOfYearFiscal` |
| Conversion | `GetAsText`, `GetAsNumber`, `GetAsDate`, `GetAsTime`, `GetAsTimestamp`, `GetAsBoolean` |
| Containers | `GetContainerAttribute` (`filename`, `fileSize`) |
| Formatting | `TextFormatRemove`, `TextColor`, `TextFont`, `TextSize`, `TextStyleAdd` and their `…Remove` forms return the plain text; `RGB` |

Custom functions are evaluated, recursion included, up to a depth limit.
They belong to the solution (`sys_custom_functions`): every formula of the
solution may call them, a function may call itself, one defined after it and
one that calls it back, and a formula reads, for its dependencies, what the
functions it calls read. A function whose formula does not read, or named
like a built-in function, is left out, with every function that calls it; a
custom function never replaces a built-in one. Calling one with another
number of arguments than it takes is an error naming it. Solution files carry them, and the
FileMaker migration imports them; there is no editor for them yet.
`TODAY()`, `NOW()`, `COALESCE` and `CONCAT` remain for formulas written in
File4Base before.

## Validation

A formula is checked when it is saved. It is refused with `422` and the
position of the problem when:

- it does not parse, or is empty;
- it calls a function that does not exist, or with the wrong number of
  arguments;
- it reads a field the table does not have;
- it would make the table's calculation fields depend on each other, including
  a field that reads itself. The error names the ring.

A formula may read **another calculation field**. File4Base works out the order
to compute them in, so a formula always sees the values it depends on already
computed.

## Examples

```
IF(customer_type = "Continuing"; 100; 200)
```
The tutorial's annual fee: continuing customers pay 100, everyone else 200.
Result type: Number.

```
UPPER(last_name) & ", " & first_name
```
A name for a list. Result type: Text.

```
ROUND(price * quantity * (1 + tax_rate / 100); 2)
```
A line total with tax. Result type: Number.

```
IF(ISEMPTY(home_address_2); home_address_1; home_address_1 & ", " & home_address_2)
```
An address that does not leave a dangling comma. Result type: Text.

```
YEAR(TODAY()) - YEAR(customer_since)
```
Roughly how many years a customer has been with you. Result type: Number.

## What is not here

- **Aggregates across a found set.** That is a summary field:
  [#32](https://github.com/file4base/file4base-app/issues/32).
- **Finding, sorting or summarizing by an unstored calculation** (FileMaker
  does it by computing every record); store the result, or use the fields it
  reads.
- `ExecuteSQL`, JSON, cryptographic and plug-in functions, and container
  functions other than `GetContainerAttribute` ( `filename`, `fileSize` ),
  are reported as unsupported.
- **Finding or sorting by a Container calculation** works on the stored
  reference, not on the file's name.
