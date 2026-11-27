# Related fields and portals

A **relationship** joins two table occurrences on one or more match criteria
(see [Several match criteria](#several-match-criteria)). A layout uses it in two
ways:

- a **related field** shows a field of a related record —
  `Companies::company_address` on a Customers layout;
- a **portal** shows the list of related records, one row each.

- Server: [`server/internal/data/related.go`](../../server/internal/data/related.go)
- Client: [`client/lib/features/data_browser/related_records.dart`](../../client/lib/features/data_browser/related_records.dart)
- Issue: [#36](https://github.com/file4base/file4base-app/issues/36)

---

## Direction

A relationship is drawn from a **left** side to a **right** side. As in
FileMaker, **each side has its own three options**, and each acts on the
records of the side it is set on, reached from a record of the other:

| Option, set on a side | Means |
| --- | --- |
| Allow creation of records in this table via this relationship | A record of this side may be created through the relationship from a record of the other side, by an account with the create privilege on this side's table. |
| Delete related records in this table when a record is deleted in the other table | Deleting a record of the other side deletes the records of this side that match it. |
| Sort records | The order this side's records come back in, read from a record of the other side. |

The right side's options are the relationship's `allow_creation`,
`cascade_delete` and `sort_related`; the left side's are `left_allow_creation`,
`left_cascade_delete` and `left_sort_related`. A relationship drawn as parent
to child sets them on the right side, the child's; nothing is set on the left
side unless asked, so a relationship created before the left side had options
acts as it did. The two sides are never merged: the left side's "delete related
records" deletes a left record with a right one, never a right record with a
left one.

A relationship may join an occurrence to itself, a hierarchy drawn as
`Staff::code = Staff::boss_code` (the left end the boss, the right end the
reports). Both ends are then the same occurrence, so a cascade follows the end
whose option is on, and a read or a creation names its end with `side`
(`left` or `right`); the occurrence alone names the right end.

A portal reads the sort of the side it shows: a Companies layout showing its
customers uses the customers side's, a Customers layout showing its company
uses the companies side's. A field that side does not have is dropped rather
than refused — a stale setting on the relationship must not leave a portal
blank. A sort typed into **Portal Setup** is the caller's own and *is* checked,
so a typo there is reported.

The Specify Relationship dialog names both tables in these labels rather than
saying "this table", because a dialog showing two tables makes that ambiguous.

Reading, unlike those options, works **both ways**: a Customers layout can show
its company, and a Companies layout can show its customers.

## Several match criteria

A relationship holds an ordered list of **criteria**, each a left field, an
operator and a right field, as FileMaker's Edit Relationship dialog does. A
record is related when it meets **every** criterion:

```
Accounts::code   =  Visits::account_code
Accounts::region ≠  Visits::region
Accounts::since  <  Visits::day
```

relates an account to its visits outside its own region after the date it
opened.

| Operator | A right record matches when |
| --- | --- |
| `=` | its field equals the left record's. |
| `≠`, `<`, `≤`, `>`, `≥` | the comparison holds with the left field on the left: `since < day`. |
| `×` | always. Every record matches, empty fields included. |

- **Both directions.** Read from the right side, each comparison is turned
  round, so the accounts a visit belongs to are the same pairs of records.
- **Multi-keys.** A text field holding several lines is a key per line, on
  either side, as FileMaker indexes it: a record keyed `A¶B` is related to a
  record keyed `A` and to one keyed `B`, whichever of the two holds the
  multi-key, and two multi-keys match when any line of one meets any line of
  the other. It holds for every operator, criterion by criterion (an `<`
  criterion holds when a line of one is before a line of the other), and for
  everything that follows a relationship: portals, batched reads, related
  fields and calculations, finds on related fields, cascading deletes and
  value lists of related values, for a table of this solution or of another
  one. A CR, an LF or a CRLF ends a line, so a field migrated from FileMaker
  (whose returns become LFs) and one a client wrote with CRs match alike.
  A line is a key when it is not empty: empty lines are no key, and a line
  of spaces is one (compared as the engine's collation compares it), by the
  same rule whether the line is split in the server or in SQL. A field of
  another type holds one value; a date or timestamp meeting the lines of a
  text field is written as an ISO date (`2026-03-05`), or for a timestamp
  as an ISO date and time in UTC (`2026-03-05 10:30:00`, no fraction of a
  second, a date alone at midnight exactly), so a text multi-key of dates
  relates to a date field.
  **Performance.** On PostgreSQL an `=` criterion on a text field is an
  overlap of arrays of lines, which a GIN index on the same expression
  serves; File4Base creates none, and a large table matched that way can be
  given one:
  `CREATE INDEX ON "table" USING gin (array_remove(string_to_array(replace("field", E'\r', E'\n'), E'\n'), ''));`.
  Other operators, and MariaDB, split each record's field as it is read
  (on MariaDB a one-line field is compared whole, as before).
- **Empty fields.** A record with an empty field in any criterion other than
  `×` relates to nothing, on either side: an account with no region has no
  visits, and a visit with no region is no account's, even through `≠`, where
  SQL alone would find an empty text different from `north`. Portals, finds,
  sorts and cascading deletes all leave it out.
- **Everything that follows a relationship** uses all its criteria: portals,
  related fields, unstored calculations reading `Occurrence::Field`, finds on
  related fields, cascading deletes and the relationship's sort. "Sort records"
  set to ascending or descending sorts by that side's field of the first
  criterion.
- **Creation** through the relationship is possible only when every criterion
  is `=`: each right-hand field of the new record is set from the left record,
  and the client cannot override any of them. With any other operator there
  is no single value to give the new record, so the server answers `403` even
  when the relationship allows creation; the Specify Relationship dialog warns
  about it. A left field that is empty answers `422`, as below.
- **Validation.** Each criterion's left field must belong to the left
  occurrence's table and its right field to the right one's; the error names
  the criterion (`criterion 2: the field "day" is not in the table of
  "Accounts"`). A relationship needs at least one criterion. An unstored
  calculation cannot be a match field, except in a `×` criterion, which reads
  no value.
- **Deleting a field** used by any criterion deletes the relationship, as for
  the first one: a relationship that lost a criterion would relate more
  records than it was defined to, and cascade its deletes to them.

The first criterion is the relationship's own `left_column_id`, `operator` and
`right_column_id`, so a client that knows only those keeps working: it creates
and reads single-criterion relationships as before. Every response also carries
the whole list as `predicates`; on `PUT`, `predicates` replaces every criterion
and leaving it out leaves them unchanged. The criteria after the first are kept
in `sys_relationship_predicates`. Each side of a criterion records the field's
name beside its id; the id is what it is resolved by.

In the Specify Relationship dialog, the field lists and the operator edit the
selected criterion: **Add** appends what they show, **Change** replaces the
selected criterion with it, **Delete** removes it, and the arrows reorder the
list. OK applies a pending change to the selected criterion. The graph draws a
line per criterion and shows every operator in the relationship's badge
(`= ≠ <`), with the criteria in its tooltip.

Migration maps every join predicate of a FileMaker relationship, from both the
XML DDR and Save a Copy as XML, `×` (cartesian product) included, with the
operator spelled as a sign or a name (`NotEqual`, `LessThanOrEqualTo`, …). A
predicate written from the relationship's other side is turned round. A
predicate whose field cannot be found, or whose operator is not known, refuses
the whole relationship and is named in the report, rather than migrating a
relationship that relates other records than the source did.

## How a side is named

A layout object names a relationship **and** an occurrence:

```json
{
  "type": "portal",
  "portal_config": {
    "relationship_id": "rel-1",
    "occurrence": "Customers",
    "row_count": 5
  }
}
```

```json
{
  "type": "field",
  "field_binding": {
    "relationship_id": "rel-1",
    "table_occurrence": "Companies",
    "field_name": "company_address"
  }
}
```

The relationship is needed because the same two tables can be joined more than
once, and the occurrence because a relationship may join a table **to itself**,
where there is no "other" table to infer. For every other relationship the
occurrence may be left out and the other side is taken.

A relationship joining an **occurrence** to itself (`Tasks::code =
Tasks::parent_code`) has the same occurrence at both ends, so the occurrence
cannot tell them apart. The object then names its end with
`relationship_side`, `left` or `right`:

```json
{
  "type": "portal",
  "portal_config": {
    "relationship_id": "rel-sub",
    "occurrence": "Tasks",
    "relationship_side": "left"
  }
}
```

Left out, or anything other than `left`, it is the right end, as before.
Browse mode sends it as `side` with every list, creation and batch request on
the portal or field, and Find mode with a criterion typed into such a related
field, so the find searches that end; it is ignored for a relationship between
two occurrences. A portal or field naming the relationship alone, with no
occurrence and no end, shows the right end too, as Portal Setup does. Portal Setup offers the choice (*Left end* / *Right end*) for such
a relationship only. Choosing another relationship clears it; choosing the same
one again keeps it. A related field
keeps the end it was picked from in the field list, which lists both.

A FileMaker migration never writes it. FileMaker always relates two table
occurrences, even when they are of the same table, and each is migrated to its
own occurrence, so the occurrence names the end.

The occurrence is held by **name**, which survives export and import as it is
(occurrences are matched by name). The relationship is held by **id**, which
import rewrites to the id it has in the destination.

## What a related field shows

The **first** matching record. It is shown rather than typed into: a
relationship can match several records, and it would not be clear which one an
edit changed. Related records are edited in a portal, where the row says which
record it is.

A related field whose relationship has been deleted draws its name in a warning
box (`<Companies::company_address>`) instead of disappearing, and the rest of
the layout is unharmed.

## What a portal shows

One row per related record. The fields in a row are the layout objects drawn
**inside** the portal, as in FileMaker: the portal's first row is the band they
are placed in, and every row redraws them against its own record. An object
that overhangs the portal's edge is not in it.

Portal Setup holds:

| Setting | Means |
| --- | --- |
| Show related records from | The relationship and occurrence the rows come from. |
| Show the records at its | For a relationship joining an occurrence to itself only: the end the rows come from (`relationship_side`). |
| Initial row | The first related record shown, counting from 1. |
| Rows | How many rows are drawn; the rest are scrolled to. |
| Sort rows by | `last_name,-fee_paid`. Empty uses the relationship's own "Sort related records". |
| Show vertical scroll bar | |
| Allow creation of records in this portal | Adds an empty row at the end that creates a related record. |
| Allow deletion of records in this portal | Adds a button on each row that deletes its related record. |

**Writing.** Typing in a row writes to that related record. The empty row at
the end creates one, with its match fields filled from the record in hand — the
client never sends them, and a value sent for one would be overwritten anyway,
so a portal row cannot be aimed at a different parent.

Allowing creation in Portal Setup is not enough on its own: the relationship
must also allow creation of records on the side the portal shows, or the server
refuses with `403`. Portal Setup says so when that side does not.

## Creation order

Records nothing sorts are listed in the order they were created, as FileMaker
lists them by record ID: a found set without a sort, the records of a
relationship without "Sort records" (a portal, a related field, `Last` and
`GetNthRecord`), and every page of them. A sort ends with that order too, so
records that sort alike keep it and a page never depends on the engine.
Records reached across several relationships (`List`, a related field,
`GetNthRecord` through a path) are ordered all together, in the last
relationship's sort order and then in creation order, not the related
records of each record on the way one after the other; that sort is done in
the server, its text comparison ignoring case and accents first.

Each user table numbers its records in a column the engine fills,
`f4b$creation_order` (an identity on PostgreSQL, `AUTO_INCREMENT` on MariaDB).
It is not a field: it is in no field list, no client reads or writes it, and
no field name can hold `$`. A solution export lists each table's records in
that order, and an import, like a migration from FileMaker, inserts them in
the order it reads them, so the order survives both.

A table created before the column existed is given it when its catalog is
brought up to date, at sign-in or on restore (`server/migrations/007` does
the same by hand). The records it holds are numbered in the order the engine
reads them: their physical order on PostgreSQL, close to the order they were
inserted in (a migrated FileMaker file's record order), and their id order on
MariaDB. Until then, or while another session holds the table (the upgrade
waits 5 seconds for it, then tries again at a later sign-in), its records are
ordered by id, as before, and an export lists them by id too. A server finds
out a table has the column again at most 30 seconds after it was found
without it, so one that did not run the upgrade itself picks the column up.

### Record ID and modification count

That number is also the record's **record ID**, FileMaker's `Get ( RecordID )`
and the Data API's `recordId` (#208): unique in its table, ascending and never
given twice, not even after the record holding it is deleted. The server
takes it from the table's allocator before writing any new record (on
PostgreSQL the column's identity sequence, on MariaDB a row of
`sys_record_ids`), whatever creates it: data entry, a script, a portal row, a
duplicate, an import, a related record, a restore. A create that fails, being
refused by validation for instance, spends its ID, as in FileMaker.

The **modification count** (`Get ( RecordModificationCount )`, the Data API's
`modId`) is a column of its own, `f4b$mod_count`, not a field either. A record
is created with 0, and every write that sets a field stored with the record
raises it by one, in the same statement, even when it sets the value the
field holds: FileMaker Server counts an edit sending the same value again
(recorded on 26.0.2). A write that sets no such field counts none: an empty
one, a recalculation, one of global fields or of calculations only, and a
write to a related record, which counts on that record. A value an
auto-enter option enters as a consequence counts as the write's. This rule
is not the modification stamps', which a value sent back as it is leaves
alone. The data service can write a record on condition that its
modification count is still the one a client read; otherwise it changes
nothing and says so (the Data API's error 306). A table not given both its
record IDs and its count yet refuses such a write rather than take the
condition as met; it is written and counted otherwise as any.

A migration keeps both. A record of a `.fmp12` file keeps the record ID the
file gives it and counts no modification, as FileMaker Server answers for a
record it hosts after a migration; a Save as XML (`FMPXMLRESULT`) export keeps
its `RECORDID` and `MODID`. A record whose ID the file does not give, or gives
to another record too, is given a new one after the table's others, and the
migration report says how many were. Tables migrated before this release keep
the numbering they were given then.

A solution export (`.f4data`) carries each record's identity, and restoring it
keeps both unless the table has given that record ID already, in which case
the record is given a new one and the import's `record_ids_renumbered` counts
it: a backup restored into the database it was taken from, its records still
there, keeps none of their IDs. The stored calculations that read the record
ID are computed again for the records given a new one. A restore that fails
gives back the record IDs it took unless another write has been given one
since, so restoring the file again keeps them. A file carries no
table's allocator: restored into a new database, the IDs of records deleted
before the export can be given again there.

A table created before the modification count existed is given its column
with the creation order's, at sign-in or on restore, counting no modification
for its records (`server/migrations/010` does the same by hand). Adding it
does not rewrite the table.

## Sorting by an unstored calculation

A portal, or any read of one record's related records, may be sorted by an
unstored calculation, through the relationship's "Sort records" or the
caller's sort, as in FileMaker. The engine cannot sort by a value it does not
hold, so such a read takes every related record of the record in hand, in
creation order, computes their calculations, sorts them (numbers as numbers,
dates in time, text ignoring case and accents first, records that sort alike
in creation order, an empty value where the engine puts an empty one), and
then keeps the page asked for. It costs the calculations of every related
record rather than of one page, and it reads at most 10,000 related records:
more are refused (`422`, "more than 10000 related records") rather than cut
short. A batched read sorts the related records of all its rows the same way,
within the same 10,000 in all.

A found set is still never sorted by an unstored calculation (`422 Unstored
Calculation`), since it may hold any number of records, nor are the records a
calculation reads through a relationship.

## An empty match field relates to nothing

A record with nothing in its match field has no related records, rather than
matching every record whose own match field is also empty. The same holds for
creating: there would be no value to match the new record by, and the server
answers `422`.

## Cascading deletes

Deleting a record deletes the records that belong to it, following every
relationship of its table whose other side asks for it — the right side's
option when the table is on the left, the left side's when it is on the right —
and then theirs, all the way down. A cascade, like creating a related
record, reads no page, so the relationship's sort is neither applied nor
checked there.
A ring of such relationships deletes each record once rather than recursing
forever.

This is integrity defined by the schema, so it applies whatever access the
caller has on the related table — the same way a foreign key would.

The whole cascade is worked out before anything is deleted, a level at a
time with one statement per table and batch of records, and then deleted in
one transaction: either every record goes, or none does. Deleting a record
that is not there answers `404`.

## Finding on a related field

A criterion typed into a related field finds the records whose **related**
records match it (#46): on a Customers layout showing `Companies::company_address`,
typing `*Paris*` there finds the customers whose company is in Paris — the
customers, not the companies.

The criterion names the relationship it reaches through, and the occurrence
when the relationship joins a table to itself:

```json
{"field_name": "company_address", "operator": "LIKE", "value": "%Paris%",
 "relationship_id": "a012…", "occurrence": "d97e…"}
```

It becomes an `EXISTS` over the related table joined on the match fields, so:

- it takes the same operators as a criterion on an own field;
- it combines with the record's own criteria with AND, and with the OR and
  omit semantics of several requests, exactly as those do;
- a record with an **empty match field relates to nothing**, so it never
  matches a related criterion — not even one like "the field is empty", which
  is about the related record's field rather than about having no related
  record at all;
- the relationship is read in either direction: the companies that have a
  customer called Tang is the same criterion the other way round;
- the caller must have access to the related table. A user who cannot see
  Companies cannot search Customers through it either; the request answers
  `403`.

**Through an occurrence of another solution's table** the related table is in
another database, which an `EXISTS` cannot reach. The criterion is run there
instead, through that solution's data service as the account the session
holds in it, and finds what this solution's records must hold to be related
to a record it matches; the records searched are then filtered with that
here. No statement joins the two databases, and the result is the one the
same find on a local copy of the table gives: every find operator, every
relationship operator, several criteria and the empty-match-field rule. What
is carried back depends on the relationship's criteria other than `×`: for one
`<`, `≤`, `>` or `≥`, the least or greatest match value found; for one `≠`, up
to two distinct values; for none, whether anything matched; otherwise the
distinct match values themselves, at most 10,000 for the whole find, all its
criteria together (a find matching more answers `422 Find Too Broad`: narrow
it). A Boolean match field is bounded as any other. The values are bound once
in the statement, however many requests read them. Not signed in to that solution, no
access to the table, or a field it no longer has answers `424
external_unavailable` as reads do.

A related field takes a criterion unless its **allow find entry** option is
off, in which case Find mode says the field takes none. Saved finds hold
related criteria too, keyed by the relationship (see
[solution_bundle_format.md](solution_bundle_format.md)).

## Occurrences of another solution's table

FileMaker's relationship graph can hold an occurrence of a table of another
file, reached through an External Data Source. In File4Base the other file is
another solution, the data source is a **file reference** (`#107`), and the
occurrence keeps the reference and the table's **name** in that solution in
place of a base table: ids differ between databases.

- An external occurrence is created with `external: {file_reference, table}`
  instead of `base_table_id` (`POST /api/v1/schemas/occurrences`). The table
  may be empty: Save a Copy as XML does not name it, so a migrated occurrence
  starts **unbound**.
- A relationship side, a predicate or a value list on an external occurrence
  names its field (`right_column_name`, `source_column_name`), since the field
  has no id here. A calculation names it as `Occurrence::field`.
- A field the source document names **by its FileMaker id only** (FileMaker
  writes an empty name when the other file was not reachable) is named
  `#<id>`, a marker no field name can collide with: `right_column_name: "#4"`,
  `source_column_name: "#4"`, `${Occurrence}::${#4}` in a calculation, a
  layout field object `field_name: "#4"`. It is found by the
  `source_field_id` the other solution's migration kept on each column; when
  several columns of a table have the id, the occurrence is a `mismatch`
  naming them, never bound to one. Only a reference with an empty name gets a
  marker: a FileMaker field actually named `#4` keeps its name (migrated as
  any other name).
- **Binding** (`POST /api/v1/file-references/{id}/bind`, admin, with a
  `peer_token` of an owner or admin session of the other solution) maps the
  reference to that solution and resolves the table of each occurrence through
  it: the table the request names for it, else the one table holding every
  field used through the occurrence (by name, and for a marker by
  `source_field_id`), preferring one named like the occurrence. When none or
  several qualify, the occurrence stays unbound and the candidates are listed:
  a table is **never guessed**. The report's `resolved` gives, for each
  marker, the field it was found as; the stored marker is kept. The request
  is checked in full before anything is written (an unknown occurrence or
  table answers `422` and changes nothing), then the mapping and the tables
  are saved in one transaction.
- **Verifying** (`POST /api/v1/file-references/{id}/verify`, admin, like
  binding: both are design operations) reports each
  occurrence as `unbound`, `bound` (the table and every field used through it
  exist there; match fields have the same type as their local counterpart) or
  `mismatch`, with the reason. Neither request reads a record.
- A file reference that occurrences go through cannot be deleted (`409`).

**Reading another solution's records.** The records of an external
occurrence are read in the solution its reference is mapped to, as the
account the session holds there (see
[security_model.md](security_model.md#session-groups-reading-another-solution)):
signing in links the solutions read with the same credentials, and one it
could not link is signed in to with `POST /api/v1/auth/link`.

- **Related fields and portals** follow this solution's relationship: the
  match values are read here, the related records there, with every criterion,
  the operator's direction and multi-key matching as for a local relationship.
  No statement joins the two databases. A list reads the related records of
  all its rows at once (`POST /api/v1/data/{table}/related`): one statement
  for the match values and one for the related records, however many rows;
  a portal on each row of a list shows that row's related records, read the
  same way.
- A **marker** (`#<id>`) in a predicate or a value list is read as the field
  binding resolved it to: the one field of that table whose `source_field_id`
  is the id. When no field, or more than one, has it, the read answers `424`
  with `schema_mismatch`.
- **Value lists** of a field of the other table offer that solution's values.
  Their second field is named too (`second_column_name`), shown beside the
  first or in its place and sorted by either, and *include only related
  values* starts from an occurrence of this solution (`related_occurrence_id`;
  one of another solution's table is refused with `422`): the match values
  are read here, the values there.
- **Unstored calculations** read it like any related table:
  `Occurrence::Field`, `Count`, `Sum`, `List` and the other aggregates, along
  a path that may cross both solutions. The other solution's own unstored
  calculations are computed there, with its graph, and may read this one
  back; a ring of formulas across the two stops as a local one does. A page
  of records reads each relationship once for all its records (see
  [calculation_formulas.md](calculation_formulas.md)). When that solution
  cannot be read the field is empty, as for a formula that fails, and the
  record is read all the same; a table the account there may not read is
  `<No Access>`, as a local one is.
- **A layout on an external occurrence** browses that solution's records
  through `/api/v1/occurrences/{occurrence}/records`, with the fields read
  from its catalog (`/api/v1/occurrences/{occurrence}/table`). Relationships
  followed from those records are this solution's.

What cannot be read is refused with `424 Failed Dependency`, problem type
`https://file4base.org/errors/external-unavailable`, `code`
`external_unavailable`, the `reference` and `database` to fix, and a
`reason`: `not_signed_in` (the client then asks for an account of that
solution with the usual sign-in dialog, titled *Open <solution> (used by
<this one>)*), `no_access`, `schema_mismatch` (the table or a field is no
longer there: verify the reference) or `missing` (unmapped, unbound, not
installed, or not done across solutions yet). Nothing through an external
occurrence is ever answered from a table of this solution with the same name.

**Writing another solution's records.** They are written there, by that
solution's data service, as the account the session holds there: with its
validation, auto-enter values and stored calculations, and its privileges,
never this session's.

- **A layout on an external occurrence**, and a portal row of one, write
  through `/api/v1/occurrences/{occurrence}/records` (`POST`, `PUT`, `PATCH`,
  `DELETE`). `GET /api/v1/occurrences/{occurrence}/table` says what the
  account may do (`privileges`: `read`, `create`, `edit`, `delete`), and the
  client enables New, Duplicate, Delete, field entry and the portal's new row
  and delete buttons by it.
- **Creating a related record** through a relationship into another solution
  (a portal's last row, or `POST /api/v1/data/{table}/{id}/related`) reads the
  match values here and creates the record there with them, every criterion
  `=` as for a local relationship; the account there must be allowed to
  create records.
- A write the account may not make answers `403`, naming the occurrence, the
  table, the solution, the file reference and the account, and the remedy
  (ask that solution's administrator for the privilege, or sign in to it with
  another account). An account with no access answers `424 no_access`, and a
  solution not signed in to `424 not_signed_in`: the client then opens the
  sign-in dialog and makes the write once more.
- **A cascading delete** that reaches another solution follows each
  solution's own cascading relationships, from its own records, so a ring of
  solutions that read each other deletes each record once. It is planned in
  full first, reading each solution as the account held there and checking
  that account may delete every table it reaches there; a refusal (`403`) or
  an unavailable solution (`424`) refuses the whole delete before anything is
  deleted. The other solutions' records are then deleted, each solution in one
  transaction of its own, and this solution's last, in one transaction. No
  transaction spans two databases: when this solution's delete fails after
  another's committed, the request answers `500` with `code`
  `partial_delete`, `partial: true` and the solutions whose records are
  already gone (`committed`). Deleting the record again once the cause is
  fixed finishes the cascade, but the re-run may leave records reachable only
  through the committed part, whose parents are already gone. Each related
  record is deleted only while its match fields still hold the key of a
  record being deleted: one moved to another record after the plan is kept.
- Other solutions' clients are told of the change, as for a write made there.

**In the client.** Every view says which file reference and which solution
a problem is about, and what to do, with no dead end:

- **Relationship graph.** An external occurrence's card has an italic header
  and a badge: *Unbound* (the reference is not mapped, or no table is chosen
  through it), *Not signed in*, *Missing*, *No access*, *Mismatch* or *OK*,
  its tooltip giving the reason and the remedy. Clicking it signs in to the
  solution (*Not signed in*) or opens Manage File References. The card lists
  the fields of the table as that solution's catalog has them
  (`GET /api/v1/occurrences/{occurrence}/table`), then, struck through, any
  field a criterion names that the table no longer has.
- **Specify Table.** *Data Source* offers the current file, each file
  reference (*Catalog (catalog_db)*, or *(not mapped)*) and *Add File
  Reference…*. A mapped reference lists that solution's tables, read with the
  session held there (`POST /api/v1/auth/link` signs in to it first, even
  before any occurrence goes through the reference); the new occurrence is
  created with `external: {file_reference, table}`.
- **Specify Relationship** reads the fields of an external side in its
  solution, and a criterion names them (`right_column_name`). Not signed in,
  the field list offers to sign in. A marker (`#<id>`) is shown as the field
  it resolves to and saved as it is stored; one no field resolves is shown
  as *field #<id> (unresolved)*, kept untouched, and never replaced by
  another field. Value lists do the same.
- **File > Manage > File References** lists the references, adds and edits
  them (name, solution, FileMaker paths; a path naming one installed solution
  is *suggested*, never chosen), binds, verifies and deletes them. Bind and
  Verify read the other solution's design, which needs an owner or
  administrator account there: the dialog signs in to it, and when the
  account held there is not one, says so and offers to sign in as another
  account. The report lists each occurrence (*bound*, *unbound* with its
  candidate tables, or *mismatch* with the reasons), and an unbound one
  with no candidate gets a choice of that solution's tables.
- **After a sign-in**, a banner lists the solutions this one reads that the
  account could not be signed in to (`refused`, `mfa_required`, `throttled`),
  each with *Sign in…*; signing in never fails because of them.
- **Script steps** that reach another solution (Perform Script and Open File
  through a file reference, Import and Export Records through a transfer) use
  the session held there, kept by the solution's exact name, or ask for an
  account of it first. A session that solution no longer accepts (`401`) is
  dropped and the user asked to sign in again.
- **Layout mode** on a layout of an external occurrence designs it with that
  table's fields; a new field is added in that solution. Until its fields can
  be read, the layout says why.
- **Manage Value Lists** offers the external occurrences as sources (in
  italics), with their fields, second field and related values.

Not done across solutions yet, refused with `424` and `reason` `missing`: a
find on a related field from a record of one (a layout on an external
occurrence). Unstored calculations reading through an external occurrence,
and finds on a related field through one, are described in
[calculation_formulas.md](calculation_formulas.md) and
[Finding on a related field](#finding-on-a-related-field). A related field of
one is shown, not typed into (as a local related field: related records are
edited in a portal). A layout's default occurrence is always local. A
field object of an external occurrence placed directly on a local layout keeps
its occurrence and is shown as unavailable, never bound to a local field of
the same name.

## What is not here yet

- **Editing a related field in place.** Related records are edited in a portal.
- **Creating the related record from a related field.** A related field with no
  matching record stays empty; it does not create one even when the
  relationship allows creation.
- **Reading a related field in a formula.** A calculation reads the record's own
  fields; see [calculation_formulas.md](calculation_formulas.md).
- **Portals of portals**, and relationships followed through more than one hop.
- A find through a relationship from a record of another solution comes
  later, and so do, in Layout mode, related fields and portals on a layout of
  an external occurrence and related fields of one placed on a local layout.
  The clients of this solution are not told when another
  solution's records they show change; they read them again when they reload.
