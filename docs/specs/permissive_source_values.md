# Permissive source values (#124)

Status: implemented (phase 1: migration, storage, reading, display, export
and data files). Phase 2 is listed at the end.

## The problem

FileMaker fields are permissive. A Number field can hold `12-A`, `(4x2)` or
`N/A`; a Date field can hold text that is no date; a Time field can hold a
duration past 24 hours. FileMaker keeps the text exactly as it was entered and,
whenever a number, date or time is needed, reads it its own way:
`GetAsNumber("12-A")` is 12.

File4Base fields are typed SQL columns. Until this change a migration refused
such values and reported them, which is correct but meant that a real
application with even a few of them could not be migrated at all
(`records_ready` was 0).

## The decision (option A)

A migrated value keeps **both**:

- **the typed value**, FileMaker's reading of the text, in the field's own
  column. Sorting, finds, summaries, calculations and scripts use it, so they
  behave as in FileMaker.
- **the original text**, kept beside it, which is what the record shows and
  what an export writes.

The original text is kept only when it differs from what the typed value says.
A Number `7,125` in a French file is the number 7.125 written the French way,
so it needs nothing kept; `7.125` in the same file is FileMaker's 7125 and
keeps its text.

Two other options were considered and rejected: migrating such fields as Text
(numeric sorting, aggregates and finds break, and the field type no longer
matches the source), and keeping only the typed value with the text in an
audit trail (the record no longer shows what the source showed).

### Storage

The original texts live in one system table per database:

```sql
CREATE TABLE sys_source_values (
  table_name  VARCHAR(64) NOT NULL,
  record_id   VARCHAR(64) NOT NULL,
  field_name  VARCHAR(64) NOT NULL,
  source_text TEXT        NOT NULL,
  PRIMARY KEY (table_name, record_id, field_name)
);
```

A side table rather than one extra column per field: user tables, their DDL
and every schema operation on them stay exactly as they were, the same table
works on PostgreSQL and MariaDB, and a database without permissive values pays
nothing for the feature. It is created with the other system tables, and on
first use in a database created before it existed (reads treat a missing
table as "no original texts").

The code is in `server/internal/sourcetext`.

### Lifecycle

| Event | Effect on original texts |
|---|---|
| Migration apply with `source_locale` | Written in the same transaction as the records. |
| `PUT /api/v1/data/{table}/{id}` | The original text of every field written is dropped: the field now holds exactly what it says. Other fields keep theirs. |
| Record import "update matching" | Same as above, inside the import transaction. |
| `DELETE /api/v1/data/{table}/{id}` (and cascades) | The record's original texts are deleted. |
| Delete field | The field's original texts are deleted, in the same transaction. |
| Delete table / delete all records | The table's original texts are deleted. |
| `.f4data` export (backups, transfers) | Each record carries its original texts under `__source_text`. |
| `.f4data` import | Original texts carried by a record are restored with it, in the same transaction; a record skipped because its id exists keeps the existing ones. |

A client can never set an original text: `__source_text` sent with `POST` or
`PUT` is ignored. Only a migration or a restored data file writes them.

## Migration

The migration request gains `source_locale`, the locale the FileMaker file
writes numbers and dates in:

`ca-ES`, `de-AT`, `de-DE`, `en-AU`, `en-GB`, `en-IE`, `en-NZ`, `en-US`,
`es-ES`, `fr-BE`, `fr-FR`, `it-IT`, `nl-BE`, `nl-NL`, `pt-BR`, `pt-PT`.

Any other value is refused (`400`), never guessed. The locale sets the decimal
separator and, when `native_date_order` is empty, the native date order.

Without `source_locale` the migration stays strict exactly as before: the
values are refused and reported, and the report now says that naming the
locale would migrate them.

With it, for native (`.fmp12`) and `FMPXMLRESULT` records:

| Field type | Value | Typed value | Original text kept |
|---|---|---|---|
| Number | a plain decimal in the locale (`7,125` in fr-FR, `7.125` in en-US) | the number | no |
| Number | anything else (`12-A`, `(4x2)`, `7.125` in fr-FR) | FileMaker's reading (`12`, `-42`, `7125`) | yes |
| Number | text without a digit (`N/A`) | empty | yes |
| Date | a valid date in the file's order | the date | no |
| Date | anything else | empty | yes |
| Time / Timestamp | a valid time of day or timestamp | the value | no |
| Time / Timestamp | a duration past 24 h, a negative or fractional duration, anything else | empty | yes |
| any typed field | several repetitions | — | refused (repetitions remain a separate gate) |
| Text | anything | unchanged | no |

FileMaker's numeric reading, as observed on FileMaker Pro 26.0.2 with French
settings (`server/internal/migration/testdata/claris26-fr-scalars`, checked by
`TestFileMakerNumberMatchesTheFrenchSourceOracle`):

- every character other than a digit and the locale's decimal separator is
  ignored: `12-A` → 12, `1/2` → 12, `12-34` → 1234, `7.125` (fr-FR) → 7125;
- a minus sign or an opening parenthesis before the first digit makes the
  number negative: `-3a7` → -37, `(4x2)` → -42;
- text without a digit is empty;
- the number is exact, never a float: `9007199254740993,125` stays exactly that.

Chosen here and not yet observed: a second decimal separator is ignored like
any other character.

The report counts the values kept with their original text in
`counts.source_text_values` and lists, per dataset, the fields they are in
(a `translated` item). `records_ready` then equals `records_detected` unless
something else is refused.

## Reading and display

The data API (`GET` list and record, `POST …/find`, `PUT` response, related
records) returns each record with its typed values as before and, when it has
any, its original texts under the reserved key `__source_text`:

```json
{
  "id": "6d0c…",
  "amount": "12",
  "visited": null,
  "doubled": "24",
  "__source_text": { "amount": "12-A", "visited": "soon" }
}
```

Field names start with a letter, so `__source_text` can never name a field.
Reads inside the server (calculations, summaries, scripts on the server) see
the typed values only.

The client shows the original text wherever a record's field is displayed
(form, list and table views, layout fields) and leaves it unformatted.
Leaving a field without changing it writes nothing, so it never replaces an
original text with its reading. Typing a new value writes it, and the original
text of that field is gone.

`POST /api/v1/data/{table}/export` writes the original text of a value that
has one, the way the record shows it.

## Not in phase 1

- **Entering permissive values.** A Number field still only accepts a number
  when edited in File4Base; typing `12-B` is refused as before. FileMaker
  accepts it.
- **Exact decimal arithmetic in calculations.** The typed column is exact
  (NUMERIC), but the calculation engines compute in floating point.
- **Durations.** A Time field is a TIMESTAMP column, which holds a time of day;
  a duration keeps its text and has no typed value, so a calculation sees it
  empty where FileMaker sees seconds.
- **Locale-aware number display.** A plain decimal of a decimal-comma file is
  stored as a number without original text and shown with a decimal point.
- **Merge fields and print previews** show the typed value.
- **NUL characters in Text** remain refused (PostgreSQL text cannot hold them).
