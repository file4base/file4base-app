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

## Reading another SQL database

Records can also come from a database rather than a file (#47):
**File > Import Records > External SQL Data Source...**, which arrives at the
same dialog, with the same preview, mapping and one-transaction import. Only
where the rows come from is different.

### Not ODBC, and the menu says so

Go has no ODBC in its standard library, and the cgo bridges available pull
unixODBC and third-party drivers into the API image, which would end the
single static binary the server is today. File4Base reads the engines it has
Go drivers for instead:

| Engine | Driver | Default port |
| --- | --- | --- |
| PostgreSQL | `pgx` (the one File4Base itself speaks) | 5432 |
| MySQL / MariaDB | `go-sql-driver/mysql` | 3306 |
| Microsoft SQL Server | `microsoft/go-mssqldb`, pure Go | 1433 |

The menu item is called *External SQL Data Source*, not ODBC, because that is
what it is.

### A registered connection, and no stored password

A connection is registered by an **owner** — `sys_data_sources`, managed in
**File > Manage > External Data Sources...** — and holds the engine, host,
port, database, user, schema and whether to use TLS.

**It holds no password.** There is no key management in File4Base to protect
one with, so rather than pretend, a password is either:

- typed in the import dialog, used for that request and never stored or
  logged; or
- read from an **environment variable of the server**, whose name the
  connection records (`password_env`). The secret then lives wherever the
  operator keeps the server's environment.

An import names a **registered** source, never a host of its own, so a request
cannot point the server at an arbitrary machine. Registering is owner-only for
the same reason; an admin can read the list and test a connection.

### What is read, and what is refused

- A **table** picked from the source's own catalog, or a **single SELECT**
  typed by the caller. Nothing else: a statement that is not one `SELECT`
  (or `WITH … SELECT`), or that holds a second statement, is refused before a
  connection is opened. File4Base never writes to a data source.
- A table name from the picker is quoted in the engine's own spelling, and
  anything that is not a plain name is refused rather than concatenated.
- The same guards a file gets: the row cap is applied by the source database
  where the engine allows it, a source with more rows is cut and says so, and
  one with more columns than the limit is refused rather than cut, since a
  half-read row would map to the wrong fields.
- A statement timeout, 30 seconds by default and 5 minutes at most.
- Every value arrives as **text**, exactly as from a file, so coercion stays
  the import's job and a value that will not convert is reported with its row
  and column.
- The connection is opened for one read and closed again.

Listing a source's tables is also how a connection is tested: one that answers
its catalog is one an import can read.

## What is not here yet

- **ODBC itself** ([#47](https://github.com/file4base/file4base-app/issues/47)).
  A source that is not PostgreSQL, MySQL/MariaDB or SQL Server needs its own
  Go driver, or an ODBC bridge and the cgo dependency that comes with it.
- **Importing a folder** of pictures into container fields. Also disabled.
- **Making a table from a file** — the table and its fields have to exist
  first.
- **Writing XML**, and importing into more than one table in one go.
- **A saved import order**, so the same file can be reimported without being
  mapped again.
