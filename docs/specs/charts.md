# Charts

The Chart Tool was in the layout toolbar and drew nothing: there was no chart
object, no setup and no renderer. A chart now draws the figures
[#32](https://github.com/file4base/file4base-app/issues/32) works out — the
found set grouped by a field — on a layout, in Preview and in print.

- Model: [`models/chart_definition.dart`](../../client/lib/features/layout_engine/models/chart_definition.dart)
- Drawing: [`chart_view.dart`](../../client/lib/features/layout_engine/chart_view.dart)
- Issue: [#37](https://github.com/file4base/file4base-app/issues/37)

---

## Where a chart's numbers come from

| Kind | Reads |
| --- | --- |
| Column, Bar, Line, Area, Pie | The **figures**: the found set grouped by the category field, one summary field per series. |
| Scatter | The **records**: two number fields per record, one along the bottom and one up the side. |

A grouped chart asks `POST /api/v1/data/{table}/summary` for its own grouping,
which need not be the one the layout's report uses. Its series must therefore
name **summary fields** — a plain field has no figure over a group — and the
setup dialog only offers those.

A scatter is the exception. It plots one point per record, so it reads the
found set the browser already holds and its series names a plain number field.
A record missing either number is **left out**, not drawn at zero, which would
be a lie about the data.

A chart draws over the **found set**: narrow it with a find and the chart
follows.

## Setting one up

Double-click a chart on the canvas, or use **Chart Setup...** in the
inspector's Data tab. The dialog asks for the type, the title, the field to
group by, the series, and whether to show the figures, the legend and — for a
pie — each slice as a percentage. Beside it is a preview drawn with made-up
figures, so the shape and the labels can be judged without leaving Layout mode.

A **pie shows one series**: choosing a second replaces the first, because a pie
divides one quantity into shares.

Series take the next colour of the palette unless one is chosen.

## What it looks like when it cannot draw

A chart object is never silently blank:

| | Shows |
| --- | --- |
| No category field or no series | *Chart: choose a field and a series in Chart Setup* |
| Figures not read yet | *Reading the figures…* |
| Read, but nothing to draw | *Nothing to chart yet* |
| Find mode | *A chart shows nothing in Find mode* |

A chart object can be dragged small. The title goes first, then the legend, so
what is left is still a chart rather than an overflow.

## How a figure is written

Whole numbers without decimals, the rest to two. A pie slice can be labelled
with its share as well.

## Printing

Preview draws the same chart over the same found set, and the printed sheet is
that preview.

## Storage

The setup lives in the object's `chart_config`, inside the layout definition,
so it goes into the solution file and comes back with it. Series name **fields**
rather than ids, so nothing needs remapping on import.

See [layout_schema.json](layout_schema.json).

## What is not here yet

- **Stacked and percentage-stacked** columns and bars.
- **A second value axis**, and axis titles or gridlines beyond the scale marks.
- **Charting related records** — a chart covers its own table's found set.
- **Delineated data**: a chart of values typed into the setup rather than taken
  from records.
- **Clicking a chart** to find the records behind a column.
