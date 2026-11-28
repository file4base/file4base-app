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
that will not convert is reported with the row and the field it came from
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
dialog asks, and the report names the order it tried.

## What an import does

| Action | |
| --- | --- |
| **Add** | Every row becomes a new record. |
| **Update matching** | The record whose match fields hold the same values is updated. Matching is case-insensitive, as a find is, and an empty key matches nothing. A date or timestamp key matches the day or instant it names, whatever text the file writes it in (`2026-01-02 03:04:05`, `2026-01-02T03:04:05Z`, or with an offset). Rows matching no record are added or passed over, as asked. |
| **Replace found set** | FileMaker's *Replace field contents in found set, in order* (`#191`): the n-th row is written into the n-th record of the found set. Rows left once it runs out are added or passed over, as asked. It matches on nothing, so match fields are refused. |

The import works on the **found set** it is given (`found_set`, record ids in
order). *Replace found set* needs one and is refused without it, never widened
to the whole table; the other methods work on the whole table without one.
Given a found set, *Update
matching* updates every record of it that matches a row, and only those, as
FileMaker does; without one it updates the first matching record of the table.
A row whose match fields are all empty is neither matched nor added, as
FileMaker imports it. A number or boolean key is compared as the number or
boolean the column holds.
*Perform auto-enter options* can be limited to some fields
(`auto_enter_fields`), as FileMaker chooses them field by field in the import
field map. `return_record_ids` answers the records added or updated, which
FileMaker makes the found set afterwards; the import endpoint answers them as
`record_ids`.

A column with no field is left out. Two columns cannot go to the same field,
and a column cannot be sent to a **calculation** field, whose formula owns its
value, or a **summary** field, which has no value in a record — both are
refused with a reason rather than ignored.

Calculations **are** worked out for imported records, so an imported row
arrives with the same computed fields a typed one would have.

A **repeating field** takes its repetitions from one cell, separated by the
group separator (0x1D) as FileMaker writes them; each is read as the field's
type, and values past the field's maximum are dropped. In a field that does
not repeat, the separators become line breaks.

## Skipped records and values left out (#231)

An import skips what FileMaker's import skips, imports the rest, and reports
both in the Import Summary: **records skipped due to errors** and **fields
skipped due to errors**, with each one's row, field and rule.

The field validation rules validated *Always* apply; those validated only
during data entry do not. FileMaker checks some options as a value is entered
and the others as the record is committed (FileMaker Pro help, *About
validating data during import*), and an import follows the same line:

| Broken | What is skipped |
| --- | --- |
| strict data type, range, maximum length, unique, existing value | the value: the record is imported without it |
| not empty, validation by calculation | the whole record |
| the field's type (text sent to a number, an unreadable date) | the value |

A value left out of a new record leaves its field empty; of an updated record,
the field keeps what it held. The calculations reading it are worked out
without it, and a field that must not be empty and lost its value skips its
record, reported with the cause (`unique`, say); so does a value left out of
a new record whose column is not nullable, which cannot be stored empty. An
update validates the
fields it writes, not those it leaves alone, and a row whose match field cannot
be read, or breaks a rule, is skipped: it cannot be matched, and added without
its key it would be added again at the next import. A value is left out before
the auto-enter options are performed, so an auto-entered calculation reads the
record without it, as FileMaker never enters it; a value an auto-enter option
gives is then validated in turn.

**A value the field's type cannot hold.** FileMaker keeps text typed into a
number field as typed, and `IsValid` reports it; a typed column cannot hold
it. The import leaves it out and keeps the record, as FileMaker leaves out a
value that breaks *Strict data type* validated *Always*, so one bad cell costs
that cell, not the other fields of its record. Converting it the way
`GetAsNumber` would (`12abc` → `12`) was rejected: it writes a value the file
never held.

```
row 2  fee_paid  type  "not a number"  fee_paid is not a number
```

**All or nothing, when asked.** `abort_on_error` (a transfer's
`on_rejected: "abort"`) writes nothing when anything would be skipped, and
the answer names the first problem.

**Validated before it is written.** Each record is validated in the import's
transaction before it is written, so a skipped record writes nothing to undo.
A savepoint per row would have let the database refuse a row instead, but it
made a 10 000-row import 2.2 times slower on PostgreSQL (0.64 s → 1.41 s) and
1.3 times on MariaDB.

## One transaction

The import is one transaction. A row that cannot be stored for another reason
— a database constraint — takes the whole file with it, and the error names
the row. Nothing is written when that happens — not even the rows before it.

## Exporting

**File > Export Records** writes the found set, or the whole table, in creation
order unless it is sorted. CSV,
tab-separated text, JSON, XML and HTML are written by the client; an **Excel
workbook** is written by the server, which is the one format needing a library.

A repeating field is written as one cell, its repetitions separated by the
group separator (0x1D) and trailing empty ones left out, as FileMaker exports
them; *Export only the first repetition* writes repetition 1 alone. A JSON
export keeps each repetition under its own key (`field[2]`).

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
  operator keeps the server's environment. The name must start with
  `FILE4BASE_DATASOURCE_`: the server sends that password to the host the
  source names, and a database owner registers sources, so any other name
  would let an owner have the server send one of its own secrets
  (`DATABASE_URL`, a signing key) to a host of theirs. A variable without the
  prefix, in a source stored earlier or a solution file, is never read.

An import names a **registered** source, never a host of its own, so a request
cannot point the server at an arbitrary machine. Registering is owner-only for
the same reason; an admin can read the list and test a connection.

### What is read, and what is refused

- A **table** picked from the source's own catalog, or a **single SELECT**
  typed by an **owner** (the source reads with the credentials the owner
  stored; anyone else who may import picks a table). Nothing else: a
  statement that is not one `SELECT` (or `WITH … SELECT`), that holds a
  second statement, or that contains a write or execution keyword (`INSERT`,
  `DELETE`, `INTO`, `EXEC`, `DROP`, … outside quoted text, quoted names and
  comments) is refused before a connection is opened. PostgreSQL and
  MySQL/MariaDB sources are read in a read-only transaction as well; a SQL
  Server source should use a read-only login, since its driver has no
  read-only transactions. File4Base never writes to a data source.
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
