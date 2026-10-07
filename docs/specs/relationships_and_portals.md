# Related fields and portals

A **relationship** joins two table occurrences on a match field. A layout uses
it in two ways:

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
the end creates one, with its match field filled from the record in hand — the
client never sends the match field, and a value sent for it would be
overwritten anyway, so a portal row cannot be aimed at a different parent.

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

## What is not here yet

- **Finding on a related field** ([#46](https://github.com/file4base/file4base-app/issues/46)).
  Find mode takes criteria on the record's own fields. A related field says so
  in Find mode rather than offering a box that would do nothing.
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
