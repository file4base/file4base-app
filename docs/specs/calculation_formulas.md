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

An empty answer is stored as nothing (SQL `NULL`), not as an empty string.

## FileMaker compatibility

The engine reads FileMaker's calculation language and gives FileMaker's
answers. Its behaviour is pinned by
[`testdata/conformance.json`](../../server/internal/calc/testdata/conformance.json):
552 synthetic formulas evaluated in FileMaker Pro 26.0.2 (French), with the
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
| Number | exact decimal | `,5`, `1234,5`, `1,0E+20`; computed values rounded to 16 places, or to `SetPrecision`'s |
| Text | Unicode text | itself |
| Date | day number, 1 = 01/01/0001 | `24/11/2026` |
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
| Formatting | `TextFormatRemove`, `TextColor`, `TextFont`, `TextSize`, `TextStyleAdd` and their `…Remove` forms return the plain text; `RGB` |

Custom functions are evaluated, recursion included, up to a depth limit.
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
- **Unstored calculations.** Every calculation is still computed when its
  record is written and stored; one that reads related records is not yet
  recomputed when those change.
- `ExecuteSQL`, JSON, cryptographic, container and plug-in functions are
  reported as unsupported.
