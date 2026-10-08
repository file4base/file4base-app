# Importing and exporting records

File4Base could only read its own files. It now reads records out of the
formats other programs write — comma- and tab-separated text, Excel workbooks
and XML — and writes a found set back out.

- Parsers and writers: [`server/internal/dataio`](../../server/internal/dataio)
- The import itself: [`server/internal/data/import.go`](../../server/internal/data/import.go)
- Issue: [#38](https://github.com/file4base/file4base-app/issues/38)

---

## Everything arrives as text

A parser reads cells, not values. Turning `"1.234,56"` into a number or
`03/04/2011` into a date is the **import's** job, not the parser's, so a value
that will not convert is reported with the row and the column it came from
rather than quietly becoming zero.

| Format | Read | Written |
| --- | --- | --- |
| CSV | yes | yes |
| Tab-separated | yes | yes |
| Excel `.xlsx` / `.xlsm` | yes | `.xlsx` |
| XML | yes | no |
| JSON, HTML | no | yes (written by the client) |

**Delimited text.** The separator is worked out from the first few lines when
it is not given: the character that splits them into the same number of
columns every time. A byte order mark is stripped, quoted values keep their
commas and newlines, short rows are rows with empty cells, and blank lines are
skipped.

**Excel.** The first sheet is read and the others are offered. Cells are read
as the text the sheet shows, so a date formatted as a date arrives as a date
rather than as Excel's day count.

**XML.** Two shapes: FileMaker's own `FMPXMLRESULT`, where the field names are
in `METADATA`; and a repeating element, worked out as whichever element appears
most often while having children of its own. Attributes of a record are fields
too, named `@attr`, so an `id="3"` is not lost. A repeating field's several
`DATA` elements become one value per line, which is how File4Base holds several
values in a field.

## Guards

Text and spreadsheets get the equivalent of what `msgpackguard` gives the
MessagePack path:

| | |
| --- | --- |
| File size | 25 MiB, checked before the file is read |
| Rows | 100 000; the rest are left out and the answer **says so** |
| Columns | 256 |

## Turning text into values

| Field | Reads |
| --- | --- |
| Text | exactly what it was given, spaces and all |
| Number | `-1234.56`, `1,234.56`, `1.234,56`, `1 234,56` — whichever of `.` and `,` comes last is the decimal point, the other groups digits |
| Boolean | true/false, yes/no, y/n, 1/0, t/f, any case |
| Date | ISO always; `03/04/2011` only in the order the caller **said** |
| Timestamp | RFC 3339, or `YYYY-MM-DD HH:MM[:SS]` |
| Container | base64 |

**An empty cell is nothing** (SQL `NULL`) for every field — never a zero, never
a blank date.

**A date is not guessed.** `03/04/2011` is 3 April or 4 March depending on
where the file came from, and nothing in the value says which. The import
dialog asks, and the error names the order it tried.

## What an import does

| Action | |
| --- | --- |
| **Add** | Every row becomes a new record. |
| **Update matching** | The record whose match fields hold the same values is updated. Matching is case-insensitive, as a find is, and an empty key matches nothing. Rows matching no record are added or passed over, as asked. |

A column with no field is left out. Two columns cannot go to the same field,
and a column cannot be sent to a **calculation** field, whose formula owns its
value, or a **summary** field, which has no value in a record — both are
refused with a reason rather than ignored.

Calculations **are** worked out for imported records, so an imported row
arrives with the same computed fields a typed one would have.

## One transaction

The first row that cannot be read or stored takes the whole file with it, and
the error names the row and the column:

```
import failed: row 2, Fee Paid: "not a number" is not a number
```

Nothing is written when that happens — not even the rows before it. The field
validation rules apply as they do to any other write.

## Exporting

**File > Export Records** writes the found set, or the whole table. CSV,
tab-separated text, JSON, XML and HTML are written by the client; an **Excel
workbook** is written by the server, which is the one format needing a library.

A date is written as a date rather than as the timestamp the column holds, and
a CSV is given a byte order mark so Excel opens it as UTF-8 and accented names
survive.

Column headings are the field labels by default, or the field names, which
reimport without being matched by hand.

## What is not here yet

- **ODBC**, which is [#47](https://github.com/file4base/file4base-app/issues/47):
  a network client, a credential store and probably a cgo dependency, each
  worth its own argument. The menu item is shown disabled.
- **Importing a folder** of pictures into container fields. Also disabled.
- **Making a table from a file** — the table and its fields have to exist
  first.
- **Writing XML**, and importing into more than one table in one go.
- **A saved import order**, so the same file can be reimported without being
  mapped again.
