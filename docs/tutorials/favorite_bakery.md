# File4Base tutorial — Favorite Bakery

This tutorial builds a working database from nothing: the customer list of a
fictitious business called **Favorite Bakery**, which lets customers pay an
annual fee in exchange for a discount. By the end you will have two tables, a
relationship between them, several layouts, a found set, a sorted list, a
printable sheet and a second account.

It takes about an hour.

**Every step below was carried out in File4Base 0.9.2 and the results are the
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
- [Lesson 10 — Preview and print](#lesson-10--preview-and-print)
- [Lesson 11 — A second table and a relationship](#lesson-11--a-second-table-and-a-relationship)
- [Lesson 12 — Scripts](#lesson-12--scripts)
- [Lesson 13 — Accounts and privileges](#lesson-13--accounts-and-privileges)
- [Lesson 14 — Backups](#lesson-14--backups)
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

Then open **http://localhost:3000** in a browser. That is WebDirect, the web
client. The desktop builds (macOS, Linux, Windows) look and behave the same;
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

   **Save location on hard drive** is optional. In WebDirect the browser cannot
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

> **Calculation and Summary are not computed yet.** You can create the field and
> write a formula in its options, and the formula is stored, but nothing
> evaluates it: the column behaves as plain text that you fill in yourself. The
> New Field dialog says so when you pick one of these two types. Do not use them
> for this tutorial.

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

### Omitting

Tick **Omit** in the sidebar (or click **Omit (NOT)**) and the find returns the
records that do **not** match. It applies to the whole request.

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

---

## Lesson 7 — Sort records

1. In Browse mode, switch to **Table** view so the order is easy to read.
2. Click **Sort** in the toolbar.
3. In **Sort by**, choose **Last Name**. Leave **Ascending** selected.
4. Click **Sort**.

The records are now in alphabetical order by last name, the summary reads
`Sorted by Last Name ↑`, an arrow appears on the column heading, and the record
you were on follows its data to its new position.

To get back to the stored order, choose **Records > Unsort**, or open the Sort
dialog again and click **Unsort**.

> File4Base sorts by **one** field. There is no multi-field sort order.

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
> assistant: no list/labels/report/blank types, no field picker, no themes
> chooser and no sub-summary parts. You build the layout by hand.

---

## Lesson 10 — Preview and print

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

## Lesson 11 — A second table and a relationship

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
4. Leave the three options unticked for now:
   - *Allow records to be created in this table through this relationship*
   - *Delete related records in this table when a record is deleted in the other table*
   - *Sort related records*
5. Click **OK**.

A line now joins the two cards, anchored on the two **Company** fields, with an
**=** badge in the middle you can click to select the relationship. The counter
reads `2 occurrences · 1 relationships`.

---

## Lesson 12 — Scripts

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

## Lesson 13 — Accounts and privileges

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

Lists the access methods — desktop client, WebDirect, REST API, bulk export.
**They are not enforced yet**, and the tab says so: the server does not restrict
access by method, so any account that can sign in can use all of them. Use
account roles and layout privileges to restrict access today.

---

## Lesson 14 — Backups

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

These are things a FileMaker user will look for and not find in File4Base 0.9.2.
They are listed so you do not go looking.

**No calculation engine.** Calculation and Summary fields can be created and a
formula can be written, but nothing evaluates it. There is no *Annual Fee*
computed from the customer type, and no summary field totalling a column.

**No value lists.** A field cannot be presented as a pop-up, a checkbox set or a
radio button set from a list of allowed values. *File > Manage > Value Lists* is
a roadmap item and says so.

**No report assistant.** *New Layout / Report* only asks for a name and a table.
There are no list, labels, report or blank layout types, no field picker, no
theme chooser, no sub-summary parts, no break fields and no grand totals.

**No merge fields in text.** Form letters and mailing labels, which depend on
merge fields that collapse when empty, cannot be built.

**No OR searches.** Find mode has a single request. *Requests > New Request* is
not implemented, so "New York **or** London" has to be two separate finds.

**No saved finds.**

**No multi-field sort.** The Sort dialog takes one field.

**No portals.** A layout cannot show a list of the related records of another
table. The relationship itself works; displaying related records on a layout
does not.

**No charts.** The chart tool is in the layout toolbar but there is no chart
setup.

**No import of foreign files.** *File > Import Records* (file, folder, XML,
ODBC) is a roadmap item. File4Base reads its own `.f4p` solutions and `.f4data`
data files.

**Extended privileges are not enforced.** See lesson 13.

**Some menu commands are greyed out.** Those are commands that do not exist yet.
A greyed item is the application telling you the truth; it used to show a
message as though the command had run.

---

*The structure of this tutorial follows the FileMaker Pro tutorial that inspired
File4Base. The content is File4Base's own: every instruction was performed in
the application and every number quoted is one it produced.*
