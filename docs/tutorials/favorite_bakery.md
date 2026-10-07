# File4Base tutorial — Favorite Bakery

This tutorial builds a working database from nothing: the customer list of a
fictitious business called **Favorite Bakery**, which lets customers pay an
annual fee in exchange for a discount. By the end you will have two tables, a
relationship between them, several layouts, a found set, a sorted list, a
report that subtotals, a portal, a printable sheet and a second account.

It takes about an hour.

**Every step below was carried out in File4Base 0.9.3 and the results are the
ones the application actually produced.** Where File4Base does not do something,
this tutorial says so instead of describing it — see
[What is not here yet](#what-is-not-here-yet) at the end.

---

## Contents

- [Before you start](#before-you-start)
- [Lesson 1 — The File4Base window](#lesson-1--the-file4base-window)
- [Lesson 2 — Create the database](#lesson-2--create-the-database)
- [Lesson 3 — Create a table and its fields](#lesson-3--create-a-table-and-its-fields)
- [Lesson 4 — Enter records](#lesson-4--enter-records)
- [Lesson 5 — Browse the records](#lesson-5--browse-the-records)
- [Lesson 6 — Find records](#lesson-6--find-records)
- [Lesson 7 — Sort records](#lesson-7--sort-records)
- [Lesson 8 — Customize a layout](#lesson-8--customize-a-layout)
- [Lesson 9 — Layout properties and a second layout](#lesson-9--layout-properties-and-a-second-layout)
- [Lesson 10 — A calculated field](#lesson-10--a-calculated-field)
- [Lesson 11 — A value list](#lesson-11--a-value-list)
- [Lesson 12 — A summary report](#lesson-12--a-summary-report)
- [Lesson 13 — Preview and print](#lesson-13--preview-and-print)
- [Lesson 14 — A second table and a relationship](#lesson-14--a-second-table-and-a-relationship)
- [Lesson 15 — Show related records](#lesson-15--show-related-records)
- [Lesson 16 — Scripts](#lesson-16--scripts)
- [Lesson 17 — Accounts and privileges](#lesson-17--accounts-and-privileges)
- [Lesson 18 — Backups](#lesson-18--backups)
- [Reference: the sample data](#reference-the-sample-data)
- [What is not here yet](#what-is-not-here-yet)

---

## Before you start

Start the stack from the repository root:

```bash
docker compose up -d
```

Three containers come up: `file4base-postgres`, `file4base-api` and
`file4base-web`. Check the API is ready:

```bash
curl -s localhost:8080/healthz
```

It answers with `"status":"pass"` and the engine it is using.

Then open **http://localhost:3000** in a browser to use File4Base Web Client.
The desktop builds (macOS, Linux, Windows) look and behave the same;
everything in this tutorial applies to both.

To run the whole thing on MariaDB instead of PostgreSQL, use
`docker compose -f docker-compose.mariadb.yml up -d`. The tutorial is identical.

---

## Lesson 1 — The File4Base window

File4Base always works in one of **four modes**, like FileMaker:

| Mode | What it is for | Shortcut |
| --- | --- | --- |
| **Browse** | Enter and read records | ⌘B |
| **Find** | Look for a subset of the records | ⌘F |
| **Layout** | Design how the records are shown | ⌘L |
| **Preview** | See the printed page | ⌘U |

You switch with the **View** menu, or with the mode shown at the bottom left of
the window. In a browser the ⌘ shortcuts may be taken by the browser itself, so
use the View menu there.

The window has:

- the **menu bar** at the top (File, Edit, View, Insert, Format, Records,
  Scripts, Tools, Window, Help);
- the **toolbar** under it, whose buttons change with the mode;
- the **left sidebar**, which shows the current layout and the controls of the
  current mode;
- the **status bar** at the bottom: zoom, current mode, solution file, database,
  save state and the signed-in account.

A command that File4Base cannot carry out yet appears **greyed out** in the
menus. If a menu item is black, it does something.

---

## Lesson 2 — Create the database

1. At **http://localhost:3000** the **File4Base Connect & Login** dialog opens.
2. Click the **+** button to the right of the database list. The **New Database
   Solution** dialog appears.
3. For **Solution Name**, type `Favorite Bakery`.
   The **Base File Name** and the **Database Name** fill in by themselves:
   `favorite_bakery.f4p` and `favorite_bakery_db`.
4. Leave **Host / Access** and **Port** alone. They are filled in for
   information; the server uses its own configured connection.
5. For **User**, type `bakery`. For **Password**, type `Bakery2026!`.

   The owner password must be at least 8 characters and must not be the same as
   the username or the database name. These are the credentials of the first
   **owner** of this database, not of PostgreSQL.
6. Leave **Create physical database in PostgreSQL if it does not exist** ticked.
7. Click **Create & Save Solution**.

   **Save location on hard drive** is optional. In the web client the browser cannot
   choose a folder, so leave it empty: the database is created on the server
   either way and you can write the `.f4p` file later with **File > Save As**.

You are returned to the sign-in dialog with the new database already selected
and the username filled in. Type the password again and click
**Connect & Sign In**.

The window now shows **Welcome to File4Base** with a **Create First Database
Table** button, and the status bar reads `favorite_bakery_db` and
`bakery (owner)`.

---

## Lesson 3 — Create a table and its fields

A **table** holds records. A **record** is a set of **fields**. Each field has a
type, and the type should match the data: text, number, date, and so on.

### Create the table

1. Click **Create First Database Table** (or **Manage Database...** at the
   bottom of the left sidebar at any other time). The **Manage Database** dialog
   opens with four tabs: *Databases*, *Tables*, *Fields*, *Relationships Graph*.
2. Go to the **Tables** tab and click **Create First Table**.
3. For **Display Name**, type `Customers`. Leave **SQL Name** empty — File4Base
   derives `customers` from the display name.
4. Click **Create**.

The table appears in the list with its SQL name and one field: `ID`, the primary
key, which File4Base creates and maintains itself.

### Add the fields

Go to the **Fields** tab (or click **Fields** on the table's row). Then, for
each field below, click **New Field...**, type the **Field Label**, choose the
**Field Type** and click **Add Field**.

The **Column Name** fills in by itself: `Home Address 1` becomes
`home_address_1`. You can override it. Column names may contain letters, digits
and underscores only, and are limited to 63 characters.

| Field Label | Column Name | Field Type |
| --- | --- | --- |
| First Name | `first_name` | Text |
| Last Name | `last_name` | Text |
| Company | `company` | Text |
| Customer Type | `customer_type` | Text |
| Home Address 1 | `home_address_1` | Text |
| Home Address 2 | `home_address_2` | Text |
| City | `city` | Text |
| Country | `country` | Text |
| Phone | `phone` | Text |
| Fee Paid | `fee_paid` | Number |
| Date Paid | `date_paid` | Date |
| Customer Since | `customer_since` | Date |
| Photo | `photo` | Container |

The field types File4Base offers are:

| Type | Stores |
| --- | --- |
| Text | Any text |
| Number | Numbers, with decimals |
| Date | A calendar date |
| Timestamp | A date and a time |
| Boolean | True or false |
| Container | A file: a picture, a PDF, any binary |
| Calculation | *See the note below* |
| Summary | *See the note below* |

> **Calculation** fields are filled in by a formula — lesson 10 builds one.
> **Summary** fields total a column across the found set rather than holding a
> value per record — lesson 12 builds a report with them.

Close **Manage Database**. File4Base has already built a first layout for the
table — **Customers Form** — with one row per field, each labelled with the
field label you typed.

---

## Lesson 4 — Enter records

You are in **Browse** mode. The toolbar has **New**, **Duplicate**, **Delete**,
**Find**, **Sort**, **Show All** and the three views: **Form**, **List**,
**Table**.

### Create the first record

1. Click **New**. An empty record appears and the status at the top right turns
   to a yellow **Not committed**.

   A new record lives only in your window until you commit it. "Not committed"
   is telling you it has not reached the server yet.
2. Click in **First Name** and type `Mary`.
3. Click in **Last Name** and type `Smith`.
4. Fill in the rest from the first row of
   [the sample data](#reference-the-sample-data).

   In a **Date** field type the date as `2011-01-15`. `15/01/2011` and
   `1/15/2011` are also understood. The field shows the date back to you as
   `2011-01-15`.
5. Choose **Records > Commit Record**.

   The badge turns to a green **Saved** and the record now exists on the server.
   Clicking **New** again, moving to another record, or sorting also commits the
   record you are leaving.

> **Commit the record before you close the window.** Unlike FileMaker,
> File4Base does not commit when you simply click outside the field. Until you
> commit, the record is only in your browser.

### Create the rest

Click **New** and repeat for the other 15 customers. To throw away a record you
have started and not committed, choose **Records > Revert Record**.

### Change a record

Click in a field, edit it and click outside. An edit to an existing record is
sent as you type (the badge flicks to **Saving...** and back to **Saved**).

### Delete a record

Select the record and click **Delete** in the toolbar, then confirm. Deleting a
record cannot be undone.

---

## Lesson 5 — Browse the records

The record bar at the top shows `Record 1 of 16`, the arrows `|<  <  >  >|`, a
slider, and a summary: `16 records · Unsorted`.

- Click the arrows to move one record at a time, or drag the slider.
- Type a number in the **Record** box to jump to it.
- **Records > Go to Record** has **First**, **Previous**, **Next**, **Last** and
  **By Number...**

Three views show the same layout differently:

- **Form** — one record at a time, as the layout draws it.
- **List** — the records one under another.
- **Table** — a grid: one row per record, one column per field, with the field
  labels as column headings.

Switching views does not change the data.

---

## Lesson 6 — Find records

Finding is how you work with a subset of the records: the **found set**.

1. In Browse mode, click **Find** in the toolbar (or **View > Find Mode**).

   The window becomes a blank form with one box per field, labelled with the
   field labels. The left sidebar shows the **FIND** card and the toolbar shows
   **Perform Find (Enter)**, **Omit (NOT)**, **Operators**, **Clear Criteria**
   and **Cancel Find**.
2. Click in **City** and type `New York`.
3. Click **Perform Find**.

   File4Base returns to Browse mode and reports **Found 4 record(s) matching
   criteria**. The summary reads `4 found of 16 · Unsorted`, and a funnel icon
   marks the found set.

Click **Show All** to come back to all 16 records.

### Criteria in several fields

Criteria in different fields are combined with AND. A find with `New York` in
**City** and `Continuing` in **Customer Type** returns 4 records: all four
New York customers are continuing ones.

### Operators

The row of chips above the form inserts the operators, and each field shows the
form its type expects:

| Operator | Means | Example |
| --- | --- | --- |
| `*` | Any text | `Sm*` finds Smith |
| `=` | Exactly this value | `=New York` |
| `==` | The whole field is exactly this | |
| `>` `<` `>=` `<=` | Comparison | `>100` in Fee Paid |
| `...` | A range | `2011-01-01...2011-06-30` in Date Paid |
| `!` | Exclude | |
| `//` | Today (date fields) | |
| `= (empty)` | The field is empty | |
| `* (non-empty)` | The field has something | |

### Several requests

One request finds the records that match **all** its criteria. For records that
match one thing **or** another, use a second request.

To find the customers in New York or in London:

1. In Find mode, type `New York` in **City**.
2. Click **+** in the request navigator (or choose **Requests > New Request**).
   The toolbar now reads `Request 2 of 2`.
3. Type `London` in **City**.
4. Click **Perform Find**. The found set has 6 records.

Move between requests with the arrows in the navigator or with
**Requests > Go to Request**. **Duplicate Request** copies the one you are on,
which saves retyping the criteria that stay the same.

### Omitting

Tick **Omit** in the sidebar (or click **Omit (NOT)**, or choose
**Requests > Include / Omit**) and the request *subtracts* what it matches from
what the other requests found, instead of adding to it.

To find the customers who paid in 2011 except those who paid in March:

1. Type `2011-01-01...2011-12-31` in **Date Paid**.
2. Add a second request, tick **Omit**, and type `2011-03-01...2011-03-31` in
   **Date Paid**.
3. Click **Perform Find**.

A find made only of omitting requests starts from every record, so a single
omitting request with `New York` in **City** finds everyone who does not live
there.

### What a find in this database gives you

| Criterion | Found |
| --- | --- |
| City = `New York` | 4 |
| City = `London` | 2 |
| City = `Paris` | 4 |
| Country = `UK` | 2 |
| Customer Type = `New` | 6 |
| Customer Type = `Continuing` | 10 |
| Fee Paid `>=200` | 6 |
| Last Name `Sm*` | 2 |
| City `New York` or City `London` (two requests) | 6 |
| City `New York` or `London`, omitting Customer Type `New` | 5 |

---

## Lesson 7 — Sort records

1. In Browse mode, switch to **Table** view so the order is easy to read.
2. Click **Sort** in the toolbar.

   The dialog says *Records are ordered by the first field; ties are broken by
   the next one*, and starts with *No sort order yet*.
3. In **Add a field**, choose **Last Name**. It becomes the first level, with
   an arrow showing it sorts ascending.
4. Click **Sort**.

The records are now in alphabetical order by last name, the summary reads
`Sorted by Last Name ↑`, an arrow appears on the column heading, and the record
you were on follows its data to its new position.

### Break the ties with a second field

Two customers share the last name **Smith**, and two share **Durand**. Add a
second level to decide their order:

1. Open **Sort** again. **Last Name** is still there.
2. In **Add a field**, choose **First Name**. It joins below Last Name.
3. Click **Sort**.

The two Smiths are now **John** then **Mary**, and the two Durands **Jean**
then **Marie**. Each level has buttons to move it up or down, flip it between
ascending and descending, and remove it; **Clear All** empties the order.

To get back to the stored order, choose **Records > Unsort**, or open the Sort
dialog again and click **Unsort**.

---

## Lesson 8 — Customize a layout

A **layout** decides what you see. Each layout belongs to one table and shows
records from it.

Choose **View > Layout Mode**. The window changes:

- the **layout tools** are in the toolbar: selection, text, line, rectangle,
  rounded rectangle, oval, field/control, button, popover button, button bar,
  portal, chart, web viewer and the format painter;
- the **Fields** palette on the left lists the table's fields with their types;
  the **Objects** tab lists what is on the layout;
- the **inspector** on the right has four tabs: position, appearance, the object
  list and text;
- the canvas shows the layout's **parts**: *Header*, *Body* and *Footer*.

### Select and resize a field

1. Click the **First Name** field on the canvas. Handles appear on its corners
   and edges, and the inspector shows **Left (X)**, **Top (Y)**, **Right**,
   **Bottom**, **Width** and **Height**.
2. Drag a side handle. The numbers in the inspector follow as you drag. You can
   also type the number you want straight into the inspector.
3. The state at the top right turns to **Unsaved**.

### Select several objects

**Shift-click** a second object to add it to the selection, or drag a selection
rectangle over an empty part of the canvas to take everything it touches.
Several selected objects move, nudge, duplicate and delete together.

### Move things

Drag an object, or nudge the selection with the **arrow keys**. **Snap 8px** in
the toolbar snaps to the grid; turn it off for fine positioning.

To undo, use the **↩** arrow in the layout toolbar. (**Edit > Undo** does not
act on the layout.)

### Add a field to the layout

Drag a field from the **Fields** palette onto the canvas.

### Save

Click **Exit Layout** to go back to Browse mode. The layout toolbar's save state
tells you whether there is anything unsaved.

---

## Lesson 9 — Layout properties and a second layout

### The layout's own properties

In Layout mode, click an empty part of the canvas so nothing is selected. The
inspector then shows the properties of the **layout itself**:

- **Layout size** — Width and Height, and the heights of the Header, Body and
  Footer. The layout ends at the bottom of the footer; changing the height
  changes the body.
- **Background color** — a palette, or a hex value.
- **Background picture** — **Choose Picture...**, then fill, fit, stretch or
  tile.
- **Script triggers** — **OnLayoutEnter** and **OnLayoutExit**, run in Browse
  mode when the layout is shown and when you leave it.
- **Transition** — the effect played when the layout appears.

Browse and Preview mode paint the background and play the transition.

### A second layout

1. Click **+ New Layout / Report** in the toolbar.
2. For **Layout Name**, type `Customer Type List`.
3. Leave **Show records from table** on **Customers**.
4. Click **Create Layout**.

The new layout starts as a copy of the standard form for the table. Delete the
fields you do not want, arrange the rest, and use it from the **Layout** list in
the top left.

> The button is labelled *New Layout / Report*, but there is no report
> assistant: no list/labels/report/blank types, no field picker and no themes
> chooser. You build the layout by hand — including a report's sub-summary and
> grand summary parts, which lesson 12 adds with the Part tool.

---

## Lesson 10 — A calculated field

Customers of Favorite Bakery pay an annual fee: new customers pay 200,
continuing ones pay 100. Rather than typing it into every record, let File4Base
work it out.

1. Open **Manage Database > Fields** for **Customers**.
2. Click **New Field...**, type `Annual Fee` for the label, and choose
   **Calculation** for the type. Click **Add Field**.
3. On the new field's row, click **Options...** and open the
   **Calculation & Summary** tab.
4. In the formula box, type:

   ```
   IF(customer_type = "Continuing"; 100; 200)
   ```

5. Set **Calculation result is** to **Number**.
6. Click **OK**.

Every record is recomputed as soon as you save the formula. Go back to Browse
mode: **Annual Fee** shows 100 for continuing customers and 200 for new ones,
and the field cannot be typed into — it carries a function mark to say the
formula fills it.

Change a customer's **Customer Type** from `New` to `Continuing` and the
**Annual Fee** follows.

### What you can write

Formulas read the record's own fields by their column name, and may use
arithmetic, text joining with `&`, comparisons, `AND` / `OR` / `NOT`, and
functions such as `IF`, `UPPER`, `ROUND`, `LEFT`, `COALESCE`, `TODAY` and
`YEAR`. Arguments are separated by `;` or by `,`.

A few that fit this database:

```
UPPER(last_name) & ", " & first_name
```

```
IF(ISEMPTY(home_address_2); home_address_1; home_address_1 & ", " & home_address_2)
```

```
YEAR(TODAY()) - YEAR(customer_since)
```

A formula may read another calculation field; File4Base works out the order to
compute them in. A formula that does not parse, that reads a field the table
does not have, or that would make two calculations depend on each other is
refused when you save it, and the message says where the problem is.

The whole language is in
[docs/specs/calculation_formulas.md](../specs/calculation_formulas.md).

Because the answer is stored in the field's own column, you can **find** and
**sort** on a calculation field like any other: a find for `Annual Fee` `>=200`
returns the 6 new customers.

---

## Lesson 11 — A value list

`Customer Type` is either `New` or `Continuing`, and typing it by hand invites
typos. A **value list** turns the field into a set of radio buttons.

### Make the list

1. Choose **File > Manage > Value Lists...**
2. Click **New Value List...**
3. For the name, type `Customer Types`. Leave **Custom values** selected.
4. In the box, type the values one per line:

   ```
   New
   Continuing
   ```

5. Click **Save**, then **Close**.

### Put it on the layout

1. Switch to **Layout mode** and select the **Customer Type** field.
2. In the inspector, under **CONTROL STYLE**, choose **Radio button set**.
3. A **Values from** box appears. Choose **Customer Types**.
4. Click **Exit Layout**.

In Browse mode the field is now two radio buttons. Choosing one writes it to
the record; the field can no longer be mistyped.

The other control styles are **Drop-down list**, **Pop-up menu** and
**Checkbox set**. A checkbox set lets a record hold more than one of the
values; they are kept in the field one per line.

### A list that grows with the data

The second kind of list takes its values from a field instead of being typed:

1. **File > Manage > Value Lists... > New Value List...**
2. Name it `Cities In Use` and choose **From a field**.
3. Pick the **Customers** table and the **City** field.
4. Save.

That list offers every city your records already contain, so it grows as you
add customers. Bind it to **City** as a drop-down and new records can be filled
from the cities you already use, while still accepting a new one typed in.

> **A value list offers values; it does not enforce them.** A record can still
> end up holding something that is not on the list — through the data API, or
> from before the list existed. To refuse anything else, add an **existing
> value** rule in **Field Options > Validation**. Keeping the two separate is
> deliberate, and it is what FileMaker does.

If you delete a value list, fields that used it go back to being plain edit
boxes. No record data changes.

---

## Lesson 12 — A summary report

Lesson 10 gave each customer an **Annual Fee**. This lesson answers the question
that fee is for: *how much do we take, and from whom?*

That needs two things: a field that totals over a set of records, and a layout
that groups them.

### Make the summary fields

Open **Manage Database > Fields** on **Customers** and add these, choosing
**Summary** as the field type:

| Field Label | Column Name |
| --- | --- |
| Total Annual Fees | `fee_total` |
| Customers | `fee_count` |

A summary field has no value of its own yet — it needs to know what it totals.
Click the row's **Options...** and go to the last tab:

- For **Total Annual Fees**: *Total of* → **Annual Fee**.
- For **Customers**: *Count of* → **Last Name**.

The list of fields only offers what the kind of summary can be taken over.
*Total of* needs numbers, so it offers Fee Paid and Annual Fee — Annual Fee is
a calculation, but one whose result is a number, so it totals like any other.
*Count of* counts records that hold a value, so it offers everything.

> **A summary has no value in a record.** Look at a customer in Browse mode and
> Total Annual Fees is empty, and stays empty. It is not a thing each customer
> has; it is worked out over whatever set of records you are looking at. That
> is why it only means something on a report.

### Build the report layout

Choose **New Layout...**, name it `Annual Fee Report`, and go into **Layout
Mode**.

A layout is a stack of bands, shown as tabs down the left: **Header**, **Body**,
**Footer**. A report adds more.

1. Choose the **Part Tool** and click on the canvas. Pick **Sub-summary**.
2. Its setup opens. Under **When sorted by**, choose **Customer Type**, and set
   the height to about 34 pt.

   A sub-summary is drawn once for each group of records that share that field.
3. Choose the **Part Tool** again and pick **Trailing grand summary**. That band
   is drawn once, after all the records.

The gutter now reads: Header, **Sub-summary by Customer Type**, Body,
**Trailing Grand Summary**, Footer.

4. Put the fields in. What a field shows depends on the band it is drawn in, so
   where you put it is the point:

   | Band | Put in it |
   | --- | --- |
   | Header | A title, and column headings: `Customer`, `City`, `Annual Fee` |
   | Sub-summary | **Customer Type**, **Customers**, **Total Annual Fees** |
   | Body | **Last Name**, **City**, **Annual Fee** |
   | Trailing grand summary | A label `Total, all customers`, and **Total Annual Fees** again |

   The same field, Total Annual Fees, appears twice. In the sub-summary it is
   the group's total; in the grand summary it is the whole found set's. The
   field says *what* to total; the band says *over what*.

5. Save and go back to Browse mode.

### Read the report

The view switcher now offers **Report** where it used to say List — a layout
with summary bands lists its records as a report. Click it.

The first thing you see is a warning:

> This report groups by Customer Type. The records are not in that order, so the
> groups below are split up rather than totalled once each.

That is true: the records are in the order they were entered, so **Continuing**
opens, closes and opens again further down. File4Base does not quietly re-sort
your found set; it tells you and offers to.

Click **Sort for this report**. The report comes together:

| | | |
| --- | --- | --- |
| **Continuing** | 9 | **900** |
| Common | Toronto | 100 |
| Murphy | Dublin | 100 |
| Nguyen | New York | 100 |
| Smith | New York | 100 |
| Smith | San Francisco | 100 |
| Alvarez | New York | 100 |
| Williams | New York | 100 |
| Durand | Paris | 100 |
| Wilson | Melbourne | 100 |
| **New** | 7 | **1400** |
| LeFranc | Paris | 200 |
| Durand | Paris | 200 |
| Tang | Hong Kong | 200 |
| Johnson | London | 200 |
| Ogawa | Tokyo | 200 |
| Cannon | Paris | 200 |
| Lee | Paris | 200 |
| **Total, all customers** | | **2300** |

Nine continuing customers at 100 and seven new ones at 200: **2300**. The
toolbar reads `16 records · Sorted by Customer Type ↑`.

### A report is over the found set

Click **Find**, type `=Paris` in **City**, and **Perform Find**.

Five records are found, and the report follows them: **New** 4 = 800,
**Continuing** 1 = 100, total **900**. The figures are worked out over the
records the find returned, not over the table.

**Show All** brings back 2300.

### Other kinds of summary

In Field Options the **Operation** list offers more than a total:

| Kind | Over the bakery's 16 customers |
| --- | --- |
| Total of | 2300 |
| Average of | 143.75 |
| Count of | 16 |
| Minimum of, Maximum of | 100 and 200 |
| Standard deviation of | the spread of the fees |
| Fraction of total of | Continuing 39.1%, New 60.9% |

**Running total** is different: instead of one figure per group it adds up down
the records as they are drawn, so a body row shows the total *so far*.

### Print it

**View > Preview Mode** shows the report on paper — one sheet, not one per
record, because a report is drawn once over the found set. A report wider than
the paper is scaled down so its right-hand column prints instead of falling off
the sheet.

**Print / Export PDF** prints what you see. Lesson 13 covers Page Setup.

---

## Lesson 13 — Preview and print

Choose **View > Preview Mode**.

The left sidebar shows a **PAPER** card with the real page setup — paper size
and orientation, the printable area in millimetres, and the four margins — and a
**Page Setup...** button. The toolbar shows how many records will print, which
record you are looking at, the paper size and the print button.

The sheet in the middle is what will come out of the printer: a header with the
layout and occurrence names, the layout itself drawn at the paper's scale, and a
footer with the date and the record number.

Change the paper with **Page Setup...** (also on **File > Page Setup...**), and
print or save a PDF from the print button. A layout wider than the paper is
scaled down to fit.

Choose **View > Browse Mode** to come back.

---

## Lesson 14 — A second table and a relationship

Customers work for companies, and several customers work for the same one. That
is a second table.

### Create the Companies table

Open **Manage Database > Tables**, click **Create Table...**, name it
`Companies`, and add these fields on the **Fields** tab:

| Field Label | Column Name | Field Type |
| --- | --- | --- |
| Company | `company` | Text |
| Company Address | `company_address` | Text |
| City | `city` | Text |
| Country | `country` | Text |

Then enter three records in Browse mode:

| Company | Company Address | City | Country |
| --- | --- | --- | --- |
| ABC Company | 1200 Industrial Way | New York | USA |
| DEF Ltd. | 8 Threadneedle Street | London | UK |
| XYZ Inc. | 42 Rue de la Paix | Paris | France |

### Relate the two tables

1. Open **Manage Database** and go to the **Relationships Graph** tab.

   Each table has a card listing its fields. Drag the cards where you want them;
   their positions are saved.
2. Click **Relationship...**.
3. In **Specify Relationship**, pick the occurrence on each side — `companies`
   on the left, `customers` on the right — then click the **Company** field in
   each list.
4. Tick **Allow records to be created in "customers" through this
   relationship**. Lesson 14 uses it to add a customer from the company's own
   layout.

   The options name both tables, because they act on one side: the side the
   relationship points at, which is the one on the right.

   Leave the other two unticked:
   - *Delete the matching records in "customers" when a record is deleted in
     "companies"* — deleting a company would delete its customers. Lesson 14
     comes back to it.
   - *Sort the related records of "customers" by their match field* — that
     would order them by company, which is the same for all of them. A portal
     sorts its own rows, which lesson 15 does instead.
5. Click **OK**.

A line now joins the two cards, anchored on the two **Company** fields, with an
**=** badge in the middle you can click to select the relationship. The counter
reads `2 occurrences · 1 relationships`.

---

## Lesson 15 — Show related records

The relationship from lesson 14 is drawn, but nothing uses it yet. A layout can
read the other side of it in two ways: **one field** of the related record, and
a **portal**, which is the list of them.

### Show the company's address on the customer layout

A customer record holds the company's name, not its address. The address lives
in Companies, and a related field reads it from there.

1. Go to the **Customers Form** layout and choose **View > Layout Mode**.
2. Drag a **Field** onto the layout, to the right of the **Company** box.
3. In the inspector, on the **Data** tab, open **Table Occurrence**.

   The list now has more than this layout's own table: every occurrence the
   relationship reaches. Pick **customers · through "companies_to_customers"**
   — that is Companies, named by the relationship followed to get there.
4. In **Bound Column**, pick **Company Address**. The field on the canvas reads
   `:: companies::company_address`, with a link badge.
5. Add a **Text** label beside it reading `Company Address`.
6. Save, and choose **View > Browse Mode**.

The first record is **Marie Durand**, who works at **XYZ Inc.**, and the new
field shows **42 Rue de la Paix**. Move to the next record — **Michelle
Cannon**, **ABC Company** — and it changes to **1200 Industrial Way**. The
address is never typed into a customer record; it is read from the one company
record that holds it.

A related field is **shown, not typed into**. A relationship can match several
records, and it would not be clear which one an edit changed. The next section
is where related records are edited.

### Put a portal of customers on the company layout

1. Go to the **Companies Form** layout and choose **View > Layout Mode**.
2. Make the body taller — drag the **Footer** divider down — so there is room
   under the four fields.
3. Choose the **Portal Tool** from the toolbar and drag a rectangle about 600
   wide and 180 tall under the fields.
4. In the inspector, on the **Data** tab, **PORTAL SETUP** appears:

   | Setting | Value |
   | --- | --- |
   | Show related records from | `customers · through "companies_to_customers"` |
   | Initial row | `1` |
   | Rows | `6` |
   | Sort rows by | `last_name,first_name` |
   | Show vertical scroll bar | ticked |
   | Allow creation of records in this portal | ticked |
   | Allow deletion of records in this portal | ticked |

   **Sort rows by** takes field names in the order they sort, with `-` in front
   of one to sort it descending. Leave it empty to use the relationship's own
   *Sort related records*.
5. Now put the fields **inside** the portal. Drag three **Field** objects into
   its **top row** — that band is the row, and every row below repeats it:

   | Field | Where |
   | --- | --- |
   | First Name | left of the row |
   | Last Name | middle |
   | Annual Fee | right |

   Drawing them inside the portal is what puts them in it. A field that
   overhangs its edge is not in the portal and stays on the layout.
6. Add a **Text** label above the portal reading `Customers of this company`.
7. Save, and choose **View > Browse Mode**.

The first company, **ABC Company**, now lists its **6** customers, by last name:

| First Name | Last Name | Annual Fee |
| --- | --- | --- |
| Michelle | Cannon | 200 |
| John | Lee | 200 |
| Patrick | Murphy | 100 |
| John | Smith | 100 |
| Mary | Smith | 100 |
| Betty | Wilson | 100 |

**Annual Fee** is the calculation field from lesson 10, computed for each
related record. The two features work together: a portal shows whatever the
related table holds, calculated fields included.

Step to the next company and the rows change with it: **DEF Ltd.** has 5 and
**XYZ Inc.** has 5.

### Edit, add and remove through the portal

- **Edit.** Click a cell and type. It is saved to that related record — the
  customer's own record, not the company's. The row stays where it is until the
  portal next reads; it does not jump to a new sorted position while you are
  typing in it.
- **Add.** The empty row at the end reads *Add a customers record…*. Type a
  name and press **Enter**. A customer record is created and its **Company** is
  filled from the company you are on, so it belongs to it straight away. You
  never type the company name — the relationship owns that field, and a value
  sent for it would be overwritten.

  This needs *Allow records to be created…* on the relationship, which lesson 14
  ticked. Without it the row is still offered but the server refuses, and the
  Portal Setup panel warns you.
- **Remove.** The button at the right of each row deletes that customer record.
  It deletes the **related record**, not the row: there is no such thing as
  removing a customer from a company without either deleting the customer or
  clearing their Company field.

### A company with no company name

Create a company record and leave **Company** empty. Its portal is empty, and
the *Add a customers record…* row refuses with *the record has no value in the
match field*.

That is deliberate. An empty match field relates to **nothing**, rather than to
every customer whose Company is also empty — otherwise one blank record would
appear to own every unattached one. And a record created against it would have
nothing to be matched by.

Delete that test company before going on.

### Deleting a company

By default, deleting a company leaves its customers alone. They keep the
company name, and no portal shows them any more.

Tick **Delete the matching records in "customers" when a record is deleted in
"companies"** in Specify Relationship and it changes: deleting a company
deletes its customers too, and anything that belongs to *those* records, all
the way down.

Leave it **unticked** for this tutorial. It is the right setting for records
that cannot exist on their own — invoice lines without an invoice — and the
wrong one for customers, who outlive the company they happen to work for.

---

## Lesson 16 — Scripts

Choose **Scripts > Script Workspace...**

A database with no scripts says so. Click **+ Script** to write one. The
workspace has:

- the **script list** on the left;
- the **steps** of the open script in the middle, numbered, each one switchable
  on and off;
- the **step catalog** on the right, grouped into Navigation, Records,
  Control & Logic and so on: *Go to Layout*, *Go to Record*, *Enter Find Mode*,
  *Go to Field*, *New Record*, *Commit Records*, *Delete Current Record*,
  *Revert Record*, *If / Else / End If*, *Loop*, and more;
- a **parameters** panel at the bottom for the selected step.

Click a step in the catalog to add it, drag the steps to reorder them, and click
**Save**. Closing with unsaved changes asks first.

**Scripts run from layout buttons in Browse mode**, as the workspace footer
says. Draw a button with the button tool in Layout mode and point it at the
script. The **Run** button in the workspace is deliberately disabled: the
workspace is an editor, and **Step Preview** shows what the steps would do
without executing them.

---

## Lesson 17 — Accounts and privileges

Choose **File > Manage > Security...**

The dialog has three tabs.

### Accounts

The list shows every account with its status, name, privilege set and how many
layouts it can reach. Right now there is one: `bakery`, **[Full Access] Owner**.

Click **New account...** to add one. You give it a name, a password, a privilege
set and a status. The row buttons reset the password, edit, duplicate and delete
the account. File4Base refuses to leave the database without an active owner.

A change to an account takes effect at once: a session that was opened before
the account was disabled, demoted or given a new password is rejected and
revoked on its next request.

### Layout privileges

Grants an account access to specific layouts rather than all of them.

### Extended privileges

Lists the access methods — desktop client, web client, REST API, bulk export.
The web-client row currently reads **Access via File4Base WebDirect**; its
display-name change is tracked in [#8](https://github.com/file4base/file4base-app/issues/8).
**They are not enforced yet**, and the tab says so: the server does not restrict
access by method, so any account that can sign in can use all of them. Use
account roles and layout privileges to restrict access today.

---

## Lesson 18 — Backups

The database lives in PostgreSQL (or MariaDB), so backing it up means backing up
the engine's data.

The repository ships a script for PostgreSQL. It works against the running
`file4base-postgres` container and writes to `./backups` (or `BACKUP_DIR`):

```bash
./scripts/backup_postgres.sh backup
```

```bash
./scripts/backup_postgres.sh list
```

```bash
./scripts/backup_postgres.sh restore backups/<file>
```

The backup dumps every database and its roles with `pg_dumpall`, and the archive
is kept only when the dump finished. The restore checks the archive **before**
it stops anything, aborts if the API cannot be stopped, stops at the first SQL
error, leaves the API stopped after a failed restore, and reports what happened
with its exit status: `0` the whole backup was applied, `1` nothing was changed,
`2` the restore failed part-way and the API is left stopped, `3` the backup was
applied but the API could not be restarted.

Back up:

- as often as you need to be able to restore everything;
- before anything large or irreversible — deleting records, deleting a field,
  importing over existing data.

Keep more than one backup, and never write over the only good copy.

---

## Reference: the sample data

The 16 customers used throughout this tutorial. Phone is `555-1234` for all of
them.

| First Name | Last Name | Company | Customer Type | Home Address 1 | Home Address 2 | City | Country | Fee Paid | Date Paid | Customer Since |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Mary | Smith | ABC Company | Continuing | 100 First Street | Apt 2 | New York | USA | 100 | 2011-01-15 | 2009-03-02 |
| John | Lee | ABC Company | Continuing | 12 St. Johns Circle | | London | UK | 100 | 2011-02-10 | 2008-11-20 |
| William | Johnson | DEF Ltd. | New | 45 Baker Street | | London | UK | 200 | 2011-03-05 | 2011-03-05 |
| Juanita | Alvarez | XYZ Inc. | Continuing | 300 Third Avenue | | New York | USA | 100 | 2011-02-28 | 2007-06-14 |
| Michelle | Cannon | ABC Company | New | 9 Rue de Rivoli | | Paris | France | 200 | 2011-04-18 | 2011-04-18 |
| Andre | Common | DEF Ltd. | Continuing | 87 Queen Street | Floor 3 | Toronto | Canada | 100 | 2011-05-22 | 2010-01-09 |
| Marie | Durand | XYZ Inc. | Continuing | 14 Avenue Foch | | Paris | France | 100 | 2011-03-30 | 2009-09-18 |
| Jean | Durand | XYZ Inc. | New | 14 Avenue Foch | | Paris | France | 200 | 2011-06-11 | 2011-06-11 |
| Patrick | Murphy | ABC Company | Continuing | 5 Grafton Street | | Dublin | Ireland | 100 | 2011-01-27 | 2006-04-03 |
| Le | Nguyen | DEF Ltd. | Continuing | 220 Second Street | Suite 400 | New York | USA | 100 | 2011-02-02 | 2010-07-30 |
| Kentaro | Ogawa | XYZ Inc. | New | 3-1 Marunouchi | | Tokyo | Japan | 200 | 2011-07-19 | 2011-07-19 |
| John | Smith | ABC Company | Continuing | 51 Market Street | | San Francisco | USA | 100 | 2011-03-14 | 2008-02-11 |
| Sophie | Tang | DEF Ltd. | New | 77 Nathan Road | | Hong Kong | China | 200 | 2011-08-08 | 2011-08-08 |
| Steve | Williams | XYZ Inc. | Continuing | 789 Ninth Avenue | | New York | USA | 100 | 2011-05-06 | 2007-12-01 |
| Betty | Wilson | ABC Company | Continuing | 16 Collins Street | | Melbourne | Australia | 100 | 2011-04-02 | 2009-05-25 |
| Gerard | LeFranc | DEF Ltd. | New | 28 Rue Saint-Honore | | Paris | France | 200 | 2011-09-12 | 2011-09-12 |

All names, companies and addresses are fictitious.

---

## What is not here yet

These are things a FileMaker user will look for and not find in File4Base 0.9.3.
They are listed so you do not go looking, and each one has an issue.

**No report assistant** ([#33](https://github.com/file4base/file4base-app/issues/33))**.**
*New Layout / Report* only asks for a name and a table: there are no list,
labels, report or blank layout types, no field picker and no theme chooser. The
parts a report is made of do work — lesson 12 adds them by hand.

**A report does not start each group on a new page** ([#32](https://github.com/file4base/file4base-app/issues/32))**.**
It flows continuously and is cut wherever the page ends, and the header is not
repeated at the top of each printed page.

**No merge fields in text** ([#33](https://github.com/file4base/file4base-app/issues/33))**.** Form letters and mailing labels, which depend on
merge fields that collapse when empty, cannot be built.

**No saved finds** ([#34](https://github.com/file4base/file4base-app/issues/34))**.**
A find cannot be named and run again later.

**No find on a related field** ([#46](https://github.com/file4base/file4base-app/issues/46))**.**
Find mode takes criteria on the record's own fields. A related field says so in
Find mode rather than offering a box that would do nothing. Portals and related
fields themselves do work — see lesson 15.

**No charts** ([#37](https://github.com/file4base/file4base-app/issues/37))**.** The chart tool is in the layout toolbar but there is no chart
setup.

**No import of foreign files** ([#38](https://github.com/file4base/file4base-app/issues/38))**.** *File > Import Records* (file, folder, XML,
ODBC) is a roadmap item. File4Base reads its own `.f4p` solutions and `.f4data`
data files.

**Extended privileges are not enforced** ([#39](https://github.com/file4base/file4base-app/issues/39))**.** See lesson 17.

**Some menu commands are greyed out.** Those are commands that do not exist yet.
A greyed item is the application telling you the truth; it used to show a
message as though the command had run.

---

*The structure of this tutorial follows the FileMaker Pro tutorial that inspired
File4Base. The content is File4Base's own: every instruction was performed in
the application and every number quoted is one it produced.*
