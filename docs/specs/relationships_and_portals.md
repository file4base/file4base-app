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

A relationship is drawn from a **left** side to a **right** side, and File4Base
reads that direction as **parent to child**. That is what gives its three
options a side to act on:

| Option | Means |
| --- | --- |
| Allow creation | A record may be created on the **right** side through this relationship. |
| Delete related records | Deleting a **left** record deletes the **right** records that match it. |
| Sort related records | The order the **right** side's records come back in. |

Reading goes both ways, so "Sort related records" is read as belonging to the
right side too: a Companies layout showing its customers uses it, a Customers
layout showing its company does not, because the fields it names are the right
side's. A field that side does not have is dropped rather than refused — a
stale setting on the relationship must not leave a portal blank. A sort typed
into **Portal Setup** is the caller's own and *is* checked, so a typo there is
reported.

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
- **Multi-keys.** A field holding several lines (separated by returns) matches
  through any of them, criterion by criterion.
- **Empty fields.** A record with an empty field in any criterion other than
  `×` relates to nothing, on either side: an account with no region has no
  visits, and a visit with no region is no account's, even through `≠`, where
  SQL alone would find an empty text different from `north`. Portals, finds,
  sorts and cascading deletes all leave it out.
- **Everything that follows a relationship** uses all its criteria: portals,
  related fields, unstored calculations reading `Occurrence::Field`, finds on
  related fields, cascading deletes and the relationship's sort. "Sort related
  records" set to ascending or descending sorts by the right field of the
  first criterion.
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
must also allow it, or the server refuses with `403`. Portal Setup says so when
the relationship does not.

## An empty match field relates to nothing

A record with nothing in its match field has no related records, rather than
matching every record whose own match field is also empty. The same holds for
creating: there would be no value to match the new record by, and the server
answers `422`.

## Cascading deletes

Deleting a record deletes the records that belong to it, following every
relationship of its table that asks for it, and then theirs, all the way down.
A ring of such relationships deletes each record once rather than recursing
forever.

This is integrity defined by the schema, so it applies whatever access the
caller has on the related table — the same way a foreign key would.

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

**Records are not read across solutions yet.** Related records and portals
through an external occurrence, a value list of one of its fields, and a
cascading delete that reaches one (checked over the whole cascade before
anything is deleted, so nothing is) are refused with `424 Failed Dependency`,
problem type
`https://file4base.org/errors/external-unavailable`, `code`
`external_unavailable`, the `reference` and `database` to fix, and `reason`
`missing`. Nothing through an external occurrence is answered from a table of
this solution with the same name: a find or a saved find on one of its fields
is refused too. An unstored calculation reading through one evaluates to empty. A
layout's default occurrence is always local. A migrated layout on an external
occurrence opens with a placeholder naming the file reference, bound to no
local table and with record commands disabled; a field object of an external
occurrence on a local layout keeps its occurrence and is shown as
unavailable, never bound to a local field of the same name.

## What is not here yet

- **Editing a related field in place.** Related records are edited in a portal.
- **Creating the related record from a related field.** A related field with no
  matching record stays empty; it does not create one even when the
  relationship allows creation.
- **Reading a related field in a formula.** A calculation reads the record's own
  fields; see [calculation_formulas.md](calculation_formulas.md).
- **Per-side options.** Allow creation, cascade delete and sort are one setting
  each per relationship, acting left to right as above. FileMaker has them
  separately for each side.
- **Portals of portals**, and relationships followed through more than one hop.
- **Records of another solution.** External occurrences are stored, bound and
  verified, but reading their records, layouts and portals on them, and the
  second-field and related options of a value list on one come later.
