# Auto-Enter Field Options

File4Base enters values into a field automatically the way FileMaker's
*Field Options > Auto-Enter* tab does: creation and modification stamps,
serial numbers, a constant, a calculated value and a looked-up value, and it
can prohibit modifying the value during data entry. The options are applied by
the server, on every write path, so a record created through the API, a
portal row, a script, a duplicate or an import gets the same values.

## 1. Where the options are kept

The options are one JSON object in `sys_columns.default_value`, together with
the Storage tab's options (`storage_global`, `indexing`, `auto_index`,
`container_storage`), which are kept as they are. The schema API exchanges the
object as a string (`default_value` on `POST` and `PUT
/api/v1/schema/tables/{id}/columns…`), and a solution bundle carries it the
same way, so the options survive a bundle export and import unchanged.

Every key is optional and an absent key is the option being off, so a column
saved before an option existed keeps working. A `default_value` that is not a
JSON object (an older plain text) enables nothing.

| Key | Type | Meaning |
| --- | --- | --- |
| `creation_enabled`, `creation_type` | bool, string | **Creation**: `Date`, `Time`, `Timestamp`, `Name` or `Account Name`, entered when the record is created. |
| `modification_enabled`, `modification_type` | bool, string | **Modification**: the same values, entered when the record is created and again each time it is modified. |
| `serial_enabled` | bool | **Serial number**. |
| `serial_generate` | `"creation"` \| `"commit"` | *Generate: On creation / On commit* (default `creation`). |
| `serial_next` | string or number | *next value*: the value the next record gets. A whole number, or a text whose last run of digits is incremented (`INV0009` → `INV0010`, `A99` → `A100`); default `1`. |
| `serial_increment` | integer ≥ 1 | *increment by* (default `1`). |
| `value_last_visited` | bool | **Value from last visited record**: kept for the dialog; see §7. |
| `data_enabled`, `data_value` | bool, string | **Data**: a constant. |
| `calculated_enabled`, `calculated_formula` | bool, string | **Calculated value**, a FileMaker formula. |
| `calculated_context` | string | The table occurrence the formula is evaluated from (one of the field's table); empty is the occurrence named like the table, else its first. |
| `do_not_replace` | bool | *Do not replace existing value of field (if any)*. |
| `calculated_skip_if_empty` | bool | *Do not evaluate if all referenced fields are empty*; absent is `true`, FileMaker's default. |
| `looked_up_enabled` | bool | **Looked-up value**. |
| `looked_up_context` | string | *Starting with table*: the occurrence of the field's table the lookup starts from (empty as for `calculated_context`). |
| `looked_up_occurrence`, `looked_up_field` | string | *Lookup from related table* and *Copy value from field*: the occurrence and the field (its column name) the value is copied from. |
| `looked_up_dont_copy_if_empty` | bool | *Don't copy contents if empty*. |
| `looked_up_no_match` | string | *If no exact match, then*: `do_not_copy` (default), `copy_next_lower`, `copy_next_higher` or `use_constant`. |
| `looked_up_no_match_value` | string | The constant of `use_constant`. |
| `prohibit_modification` | bool | *Prohibit modification of value during data entry*. |
| `looked_up_target` | string | Legacy `Occurrence::Field` text written by older clients, read when `looked_up_occurrence` is absent. |

The options are checked when they are saved (`schema.ValidateFieldOptionsFor`,
package `internal/autoenter`), against the field's type: an unknown stamp, a
stamp the field cannot hold (a name in a date field, a date in a number field),
a serial in a field that is neither text nor number, a non-integer next value
for a number field, a next value with no digit, an increment below 1, an
auto-enter formula that does not parse and a lookup without its occurrence and
field are refused with `422 Invalid Field Options`.

### The next serial value

The next value lives in the options, and is allocated atomically: a new record
takes it and writes the following one in the same transaction, under a row
lock of the field's catalog row (`SELECT … FOR NO KEY UPDATE` on PostgreSQL,
`FOR UPDATE` on MariaDB), so concurrent writers never get the same value. A
serial taken by a write that then fails is not given back, as in FileMaker.

Saving the options **without** `serial_next` keeps the live next value; saving
them **with** it sets it. The Field Options dialog sends it only when it was
edited, so a dialog left open while records are created does not move the
counter back. `GET`/`PUT /api/v1/data/{table}/serials/{field}` read and set it
directly (FileMaker's `GetNextSerialValue` and *Set Next Serial Value*): both
answer `{"field", "next_value"}`, and `PUT` takes `{"next_value": …}`, a number
or a text, and needs the edit privilege.

## 2. When each option applies

Options are applied in this order, each seeing what the earlier ones entered:
creation and modification stamps, serial numbers, constants, then, field by
field in the order the fields were created, calculated values and looked-up
values (a field with both computes the formula, then looks up). Calculation
fields are computed after, so a calculation reading a serial number sees it.

| Write | Stamps | Serial | Data | Calculated value | Looked-up value |
| --- | --- | --- | --- | --- | --- |
| New record (API, layout, portal row, script) | creation and modification entered | allocated | entered | evaluated | copied when a match field has a value |
| … with a non-empty value given for the field | the value given wins | the value given wins | the value given wins | the value given wins | the value given wins |
| Record modified | modification stamps entered when a stored, non-global field changed | — | — | re-evaluated when a field it reads changes (see below) | copied again when a match field changes |
| Duplicate record | entered anew | allocated anew | entered anew | evaluated anew, unless *Do not replace* (copied) | copied from the original |
| Import, *Perform auto-enter options* on, added record | entered, over the imported value | allocated, over the imported value | entered, over the imported value | evaluated, over the imported value unless *Do not replace* and a value was imported | copied when a match field has a value |
| Import, on, updated record | modification stamps entered | — | — | as for a modified record | as for a modified record |
| Import, off; bundle restore; migration records; recalculation after a formula change | nothing is entered | nothing is allocated | — | — | — |

A new record given an empty value for a field is as one given none: the
options enter it.

**Modification.** A record is modified when a write changes a field that is
stored with it: not a calculation, a summary or a global field
(`storage_global`). A write that changes only those leaves the stamps alone.

**Calculated value.** It is evaluated with the calculation engine, in the
record's context (`calculated_context`), and it may read related fields:
`Occurrence::Field`, `Sum`, `List` and the other aggregates use the related
reads of unstored calculations (`internal/data/related_reads.go`), through
external occurrences too. On a modified record it is evaluated again when a
field it reads changes, including the match fields of the relationships its
related references start from, and a field another option entered in the
same write; with *Do not replace* it is evaluated only while the field is
empty. Without *Do not replace* it is re-evaluated when its own field is
changed, which is how a formula reading `Self` or its own field makes it
self-modifying. With *Do not evaluate if all referenced fields are empty* a
formula reading fields of the record is not evaluated while every one of them
is empty (a formula reading none, such as `Get ( UUID )`, always is).
`Get ( AccountName )` and `Get ( UserName )` answer the account writing.
Neither a calculated nor a looked-up value is entered in a container field.

**Looked-up value.** The value is the field `looked_up_field` of the first
record related through `looked_up_occurrence`, reached from
`looked_up_context`, in the relationship's sort order, the way a related field
reads; external occurrences work the same way. It is copied when the record is
created with a value in a match field of the first relationship of that path,
and when such a match field changes. When no record matches, `do_not_copy`
leaves the field as it is, `use_constant` enters `looked_up_no_match_value`,
and `copy_next_lower` / `copy_next_higher` copy from the related record whose
match field holds the closest value below / above the record's: this needs a
path of one relationship whose first criterion is `=`, in this solution, and
does nothing otherwise. *Don't copy contents if empty* leaves the field as it
is when the matching record's value is empty. **Relookup Field Contents**
(`POST /api/v1/data/{table}/relookup` with `{"field", "record_ids"}`, at most
10,000 records, the script step and the Records menu) copies again every
lookup of the given records that starts from the match field `field`, and
modifies the records it changes; it answers `{"updated": n}` and needs the
edit privilege. A match field no lookup starts from is refused with `400`.

**Stamps.** `Date`, `Time` and `Timestamp` are the server's clock, as
`Get ( CurrentDate )`, `Get ( CurrentTime )` and `Get ( CurrentTimestamp )`
are; a time entered in a timestamp field is on 0001-01-01, as the calculation
engine converts a time to a timestamp. `Name` and `Account Name` are both the account the
session signed in as (`Get ( AccountName )`): File4Base has no separate user
name, which FileMaker takes from the client's preferences.

## 3. Prohibit modification of value during data entry

Data entry is a write through `POST /api/v1/data/{table}` or `PUT
/api/v1/data/{table}/{id}` (and the occurrence routes) **without** the header
`X-File4Base-Entry: script`. A data-entry write that gives a prohibited field a
value — a non-empty one on a new record, a different one on an existing
record — is refused with `422 Prohibited Modification`, naming the field.
Sending back a record's unchanged value is accepted.

A script step sends `X-File4Base-Entry: script`, and may set the field, as in
FileMaker; so do imports, the duplicate and relookup endpoints and the
server's own writes. This is a data-entry rule, not a privilege: privileges
decide what an account may change, and the header cannot widen them.

In Browse mode the File4Base client shows such a field read-only.

## 4. Duplicate record

`POST /api/v1/data/{table}/{id}/duplicate` creates a copy of a record, as
FileMaker's *Duplicate Record*: every stored field is copied, except that the
auto-enter options enter new values (§2) — a looked-up value and a calculated
value with *Do not replace* are copied. It needs the create privilege, answers
`201` with the new record, and is what the Records menu, `Cmd+D` and the
*Duplicate Record/Request* step use. A container is copied with its file's
name and type.

## 5. Imports

`POST /api/v1/data/{table}/import` takes `options.perform_auto_enter` (default
`false`, which writes the imported values as they are). The client's import
dialog offers it as *Perform auto-enter options while importing*, checked by
default as in FileMaker. Record transfers between solutions (#96) run with it
off. Prohibit-modification never stops an import.

## 6. Scripts and functions

| FileMaker | File4Base |
| --- | --- |
| *Set Next Serial Value* [field; value] | Script step `set_next_serial_value`, `PUT /api/v1/data/{table}/serials/{field}`. |
| `GetNextSerialValue ( fileName ; fieldName )` | Calculation function, in auto-enter calculations, unstored calculations and the Data Viewer (a stored calculation answers an error: its value would go stale). `fileName` is not read: the field is `Table::Field` or a field of the formula's table. |
| *Relookup Field Contents* [field] | Script step `relookup_field_contents` and Records > Relookup Field Contents: the current found set, from the active (or named) match field. |
| *Duplicate Record/Request* | The duplicate endpoint (§4). |
| *Set Field* on a prohibited field | Allowed (§3). |

## 7. What is not the same as FileMaker

- *On creation* and *On commit* serials, and creation stamps, are entered when
  the new record is first saved to the server: a new record in Browse mode is
  a draft until it is committed, so it shows its serial number then.
- *Value from last visited record* is kept and shown in the dialog but not
  entered.
- `Name` is the account name (§2).
- `copy_next_lower` / `copy_next_higher` need a one-relationship path whose
  first criterion is `=` (§2).
- `GetNextSerialValue` is not available in the client's script evaluator
  (script steps evaluate their own calculations on the client); use it in a
  field calculation, or read `GET /api/v1/data/{table}/serials/{field}`.
- Transfers between solutions do not perform auto-enter options (§5).
- A stored calculation cannot call `GetNextSerialValue` (§6).
- The HTML Database Design Report is read in English only, and its lookups
  are not migrated: it does not name the relationship path well enough. A
  migration from Save as XML or the XML DDR keeps them.
