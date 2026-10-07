# Summary fields and reports

A **summary field** works out a figure over a set of records: a total, an
average, a count. A **report** is a layout that groups the found set and draws
those figures: the fees of each customer type subtotalled, with a grand total
at the end.

- Server: [`server/internal/data/summary.go`](../../server/internal/data/summary.go)
- Client: [`client/lib/features/data_browser/report_view.dart`](../../client/lib/features/data_browser/report_view.dart)
- Issue: [#32](https://github.com/file4base/file4base-app/issues/32)

---

## A summary has no value in a record

A calculation field is worked out **per record** and stored in that record's
column ([calculation_formulas.md](calculation_formulas.md)). A summary is not:
it is worked out over whatever set of records is in front of you, so the same
field shows a different figure in a group than it does over the whole found
set.

Its column therefore stays empty, and a value sent for it is dropped rather
than stored where nothing would read it. An update of summary fields alone is
an update of nothing.

## What a summary can be

| Kind | Works out | Needs |
| --- | --- | --- |
| Total | The sum. | a number field |
| Average | The mean. | a number field |
| Count | How many records hold a value in the field. | any field |
| Minimum, Maximum | The smallest and largest. | any field |
| Standard deviation | The sample standard deviation. | a number field |
| Fraction of total | This group's share of the grand total, shown as a percentage. | a number field |

"A number field" means a field that **stores** numbers, so a calculation whose
result type is Number counts — which is what the tutorial's Annual Fee is.

A summary cannot be taken over another summary: there is no per-record value to
aggregate. Saving one is refused with `422`, as is totalling text.

**Running total** adds up down the records as they are drawn rather than giving
one figure per group. It depends on the order they are in, so it is worked out
by the client as it draws, not by the database.

## How a summary field is stored

In the field's `calculation_formula`, the column that already means "how this
field is computed":

```json
{"summary_type": "total", "field": "annual_fee", "running": false}
```

The shape saved before summary fields were computed — `{"operation": "SUM",
"target_column": "annual_fee"}` — is **read as well**, so a field defined then
works now instead of having to be set up again.

## The parts of a report

A layout is a stack of bands. A report uses these, in this order:

| Part | Drawn |
| --- | --- |
| Header | Once, at the top. |
| Leading grand summary | Once, before the records. |
| Sub-summary (leading) | Once before the records of each group. |
| Body | Once per record. |
| Sub-summary (trailing) | Once after the records of each group. |
| Trailing grand summary | Once, after the records. |
| Footer | Once, at the bottom. |

Whether a sub-summary is leading or trailing is decided by **where it sits** —
above the body or below it — as it is in FileMaker, rather than by two part
types. Each one names the field it breaks on: the band is drawn again whenever
that field's value changes.

The fields in a band are the layout objects drawn inside it, which is what
putting a summary field in a sub-summary means.

**What a field shows** depends on the band it is in:

| In | A summary field shows | A plain field shows |
| --- | --- | --- |
| Body | its running value, when it is a running total | the record |
| Sub-summary | the group's figure | the value the group broke on |
| Grand summary | the figure over the whole found set | nothing |

## Nesting

More than one sub-summary makes a nested report. Groups open outermost first
and close innermost first, so a report that breaks on Customer Type and then on
City reads:

```
Continuing
  Dublin        …records…  Dublin subtotal
  London        …records…  London subtotal
              Continuing subtotal
New
  Toronto       …records…  Toronto subtotal
              New subtotal
Grand total
```

## The order a report needs

A report only reads correctly when the found set is sorted by its break fields,
outermost first. Sorted any other way, a group opens, closes and opens again
further down.

File4Base does not quietly re-sort: the report says so, and offers to sort the
found set the way it needs. That keeps the found set the reader's, the way
FileMaker does, while making the problem visible instead of printing nonsense.

## Where the figures come from

`POST /api/v1/data/{table}/summary`, which takes the found set as find requests
— the same ones a find takes — and works the figures out in SQL over exactly
those records. A report asks once per break level: level *n* is grouped by the
first *n* break fields, and level 0 is the grand totals.

The figures are worked out over the **whole found set**, so a report reads it
all rather than the first page, and its totals match the rows underneath them.

Figures come back as decimal text rather than through a float, so nothing is
lost on the way (#18). An average or a standard deviation arrives at the
engine's own precision and is shown to two decimals; a fraction of the total is
shown as a percentage.

## Printing

Preview draws the report, and the printed sheet is that preview. A report is
drawn **once** over the found set, so "print all records" prints one report
rather than one copy per record.

A report wider than the paper is scaled down to the printable width, so its
right-hand columns print instead of falling off the sheet.

## What is not here yet

- **Page breaks per group.** The report flows continuously and is cut wherever
  the page ends, rather than starting each group on a fresh page.
- **Repeating the header on each page**, and page numbers inside a report.
- **Summaries of related records.** A summary covers the found set of its own
  table; totalling a portal is not the same thing.
- **A report assistant.** Parts are added by hand with the Part tool;
  [#33](https://github.com/file4base/file4base-app/issues/33) is the assistant
  that would lay a report out for you.
- **Number formatting per field.** A figure is shown as the engine gives it,
  trimmed; there is no currency or thousands formatting yet.
