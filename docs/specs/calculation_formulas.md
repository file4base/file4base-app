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

## Grammar

```
expression     := orExpr
orExpr         := andExpr ( "OR" andExpr )*
andExpr        := notExpr ( "AND" notExpr )*
notExpr        := "NOT" notExpr | comparison
comparison     := concat ( ( "=" | "==" | "<>" | "!=" | "<" | "<=" | ">" | ">=" ) concat )?
concat         := additive ( "&" additive )*
additive       := multiplicative ( ( "+" | "-" ) multiplicative )*
multiplicative := unary ( ( "*" | "/" ) unary )*
unary          := ( "-" | "+" ) unary | primary
primary        := NUMBER | TEXT | "true" | "false"
                | FIELD
                | FUNCTION "(" [ expression ( ( "," | ";" ) expression )* ] ")"
                | "(" expression ")"
```

- **Fields** are written as their column name: `fee_paid`, `home_address_1`.
  Names are case-sensitive, because column names are.
- **Text** is in double or single quotes. The quote that opened the text closes
  it, and doubling it means the quote itself: `"say ""hi"""`.
- **Arguments** are separated by `;` (as in FileMaker) or by `,`. Both work.
- Keywords and function names are case-insensitive: `IF`, `if` and `If` are the
  same.

## Operators

| Operator | Meaning |
| --- | --- |
| `+` `-` `*` `/` | Arithmetic. Division by zero is an error. |
| `&` | Joins text: `first_name & " " & last_name`. |
| `=` `==` | Equal. |
| `<>` `!=` | Not equal. |
| `<` `<=` `>` `>=` | Comparison. |
| `AND` `OR` `NOT` | Logic. `AND` and `OR` stop as soon as the answer is known, so the other side is not evaluated. |

**How values compare.** Two moments compare as moments. Two values that are
both numbers (or text that reads as a number) compare as numbers. Otherwise
they compare as text, **ignoring case** — which is how Find mode matches text,
so a formula agrees with a find.

## Functions

| Function | Result |
| --- | --- |
| `IF(test; whenTrue [; whenFalse])` | One branch or the other. Only the branch taken is evaluated. Without a false branch the answer is empty. |
| `UPPER(text)`, `LOWER(text)`, `TRIM(text)` | The text, changed. Empty stays empty. |
| `LENGTH(text)` | How many characters. |
| `CONCAT(a; b; …)` | The arguments joined, like `&`. |
| `LEFT(text; n)`, `RIGHT(text; n)` | The first or last `n` characters. |
| `MIDDLE(text; start; count)` | `count` characters from `start`, counting from 1. |
| `ROUND(n [; places])` | Rounded; `places` defaults to 0. |
| `ABS(n)` | Without its sign. |
| `SUM(a; b; …)`, `MIN(…)`, `MAX(…)`, `AVERAGE(…)` | Over the **arguments given**, not over the records. An aggregate across a found set is a summary field, which is [#32](https://github.com/file4base/file4base-app/issues/32). |
| `ISEMPTY(value)` | True when the value is empty. |
| `COALESCE(a; b; …)` | The first argument that is not empty. |
| `TODAY()`, `NOW()` | Today's date, and the current moment, in UTC. |
| `YEAR(date)`, `MONTH(date)`, `DAY(date)` | A part of a date, as a number. |

## How values are coerced

| Used as | Empty | Number | Text | Boolean | Date |
| --- | --- | --- | --- | --- | --- |
| Number | `0` | itself | parsed, **error** if it is not a number | `1` / `0` | error |
| Text | `""` | without a trailing `.0`, so 100 reads as `100` | itself | `true` / `false` | `YYYY-MM-DD`, or RFC 3339 with a time |
| Boolean | false | non-zero | not blank | itself | not the zero time |

Text that is not a number is an **error** rather than zero, so a typo in a
field is reported instead of quietly producing a wrong total.

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

- **Aggregates across records** (totals, counts, averages over a found set).
  That is a summary field: [#32](https://github.com/file4base/file4base-app/issues/32).
- **Related fields.** A formula reads the record's own fields. Reading a field
  through a relationship needs [#36](https://github.com/file4base/file4base-app/issues/36).
- **Custom functions.** Writing your own reusable functions is a roadmap item.
- **Repetitions, globals and unstored calculations.** Every calculation is
  stored.
