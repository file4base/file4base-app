# The layout assistant, and merge fields

**New Layout / Report** used to ask for a name and a table and then make a copy
of the standard form, whatever you picked. It now asks what kind of layout,
which fields and in which order, which theme, and — for the kinds that need
them — the field to group by or the label stock, and builds that.

Separately, layout text can hold **merge fields**, and a line whose fields are
all empty takes its line with it, which is what makes a mailing label or a form
letter print properly.

- Assistant: [`client/lib/features/layout_engine/new_layout_assistant.dart`](../../client/lib/features/layout_engine/new_layout_assistant.dart)
- What it builds: [`models/layout_blueprint.dart`](../../client/lib/features/layout_engine/models/layout_blueprint.dart)
- Labels on a sheet: [`label_sheet_view.dart`](../../client/lib/features/layout_engine/label_sheet_view.dart)
- Issue: [#33](https://github.com/file4base/file4base-app/issues/33)

---

## The five kinds

| Kind | What it makes | Asked for |
| --- | --- | --- |
| **Form** | One record at a time, each field labelled. | fields, theme |
| **List** | One row per record, column headings in the header. | fields, theme |
| **Report with grouped data** | A list that breaks into groups, with a subtotal under each and a grand total at the end. | fields, **break field**, summary fields, theme |
| **Labels** | One label, repeated across and down a sheet. | fields, **label stock**, theme |
| **Blank layout** | The header, body and footer, empty. | theme |

A report is built out of the parts [#32](https://github.com/file4base/file4base-app/issues/32)
added: a sub-summary on the break field above the body, and a trailing grand
summary. The assistant places the subtotals under the columns they total, so
the figures line up with the rows above them.

## Which fields, and in which order

The field list is the layout's **order**: the column order of a list, the row
order of a form, the line order of a label. Fields are dragged to reorder,
removed with the button on the row, and added back from the chips below.

Two kinds of field are never offered as columns:

- the **primary key**, which is not something to put on a layout;
- **summary fields**, which belong in a report's own bands rather than in a
  column — a report asks for them separately.

The assistant says what is missing rather than leaving the Create button
mysteriously dead: no name, no fields, or a report with nothing to group by.

## Themes

A **layout theme** is how the generated layout looks: header fill and text
colour, heading and body sizes, whether fields are drawn with a box, and the
fill of a summary band.

| Theme | |
| --- | --- |
| Enlightened | Blue headings on white, boxed fields. The default. |
| Cool Grey | A grey header band with white text, quiet field borders. |
| Classic | Heavier boxes, larger headings. Looks like a printed form. |
| Minimal | No field boxes at all. Best for labels and printed reports. |

These are **not** the themes in Manage Themes, which colour the application
window itself. They are two different things with the same word, and the
assistant offers the layout ones.

The theme is stored on the layout (`theme`), which until now was a string
nothing read.

## Labels

A labels layout **is one label**: its width and height are the label's, and it
has a body and no header or footer, because a sheet of labels is nothing but
labels. Preview lays them out across and down the page, one record each, and
pages the found set into sheets.

Stock sizes offered: Avery L7160 and L7163 (A4), Avery 5160 and 5163 (US
Letter), and a custom size in millimetres, which is fitted to an A4 sheet.

The fields go on as a **block of merge text**, one per line, rather than as
separate field objects — that is what lets an empty line collapse.

Printing a labels layout prints one page per sheet.

## Merge fields

A text object can hold `{{field_name}}`, matched against the record without
regard to case, as well as `{{CurrentDate}}`, `{{CurrentTime}}`,
`{{CurrentUser}}` and `{{PageNumber}}`.

A value is written the way the field is shown, so a DATE reads as a date rather
than the full timestamp the API returns.

A symbol nothing answers is **left as it is**, so a mistyped field name is
visible on the layout instead of silently blank.

### Collapsing

A line that **held merge fields and came out with nothing in it** is removed.
That is what makes an address print properly:

```
{{first_name}} {{last_name}}      Marie Durand
{{home_address_1}}                14 Avenue Foch
{{home_address_2}}        ───▶    Paris, France
{{city}}, {{country}}
```

The rules:

- A line keeps its place when **any** of its merge fields has a value.
- A line left holding only spaces and punctuation — `", "` when both City and
  Country are empty — goes too.
- A line carrying **words of its own** is kept: `Phone: {{phone}}` prints as
  `Phone: ` because the word was meant to be there.
- A blank line the author typed, with no merge fields on it, is spacing and
  stays.

Collapsing applies in Browse, Preview and print.

## What is not here yet

- **A letter kind.** A form letter is a Form or Blank layout with a block of
  merge text on it, which works; the assistant does not offer a letter template
  with a body already written.
- **Merge variables.** `Insert > Merge Variable...` has no implementation and
  stays disabled.
- **Field formatting in the assistant** — number, date and time formats per
  field are not asked for.
- **Label stock beyond the four sizes and a custom one**, and no gutter or
  margin editing for a custom size.
