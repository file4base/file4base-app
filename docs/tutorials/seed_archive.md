# File4Base Tutorial — Boreal Seed & Herbarium Archive

This tutorial walks through building a complete relational database solution in
**File4Base** from an empty workspace: **Boreal Seed Archive**, a botanical
repository that tracks seed accessions, storage vault locations, annual
preservation upkeep costs, and the field stations that collect specimens.

By completing the 21 lessons below, you will build:

- two tables (`Accessions` and `Stations`) connected by a one-to-many relationship;
- custom form, list, report, dashboard, label, and dispatch-notice layouts;
- multi-criteria, multi-request, omit, and relational saved finds;
- multi-level sort rules;
- a server-evaluated calculation field (`Annual Upkeep`) and summary fields;
- custom and field-driven value lists;
- a grouped sub-summary report and live column/pie charts;
- Avery L7160 specimen labels and a form notice with collapsible merge fields;
- CSV/Excel/SQL record imports and exports;
- an interactive portal for viewing, adding, and editing related accessions;
- a Script Workspace automation script and granular table/layout security roles.

**Every step in this guide was executed in File4Base and every record count,
subtotal, find result, and sort order reflects actual application output.**
Where a capability is not yet implemented, see
[Verified limitations](#verified-limitations) at the end of this document.

---

## Contents

- [Before you start](#before-you-start)
- [Lesson 1 — The File4Base workspace and modes](#lesson-1--the-file4base-workspace-and-modes)
- [Lesson 2 — Create the solution and database](#lesson-2--create-the-solution-and-database)
- [Lesson 3 — Define the Accessions table and fields](#lesson-3--define-the-accessions-table-and-fields)
- [Lesson 4 — Enter and commit records](#lesson-4--enter-and-commit-records)
- [Lesson 5 — Navigate Form, List, and Table views](#lesson-5--navigate-form-list-and-table-views)
- [Lesson 6 — Find records and save searches](#lesson-6--find-records-and-save-searches)
- [Lesson 7 — Sort records across multiple fields](#lesson-7--sort-records-across-multiple-fields)
- [Lesson 8 — Arrange objects in Layout mode](#lesson-8--arrange-objects-in-layout-mode)
- [Lesson 9 — Configure layout properties and add a second layout](#lesson-9--configure-layout-properties-and-add-a-second-layout)
- [Lesson 10 — Compute values with a calculation field](#lesson-10--compute-values-with-a-calculation-field)
- [Lesson 11 — Constrain input with value lists](#lesson-11--constrain-input-with-value-lists)
- [Lesson 12 — Group and subtotal with a summary report](#lesson-12--group-and-subtotal-with-a-summary-report)
- [Lesson 13 — Visualize summaries with charts](#lesson-13--visualize-summaries-with-charts)
- [Lesson 14 — Build specimen labels and a dispatch notice with merge fields](#lesson-14--build-specimen-labels-and-a-dispatch-notice-with-merge-fields)
- [Lesson 15 — Preview pages and export to PDF](#lesson-15--preview-pages-and-export-to-pdf)
- [Lesson 16 — Import and export data](#lesson-16--import-and-export-data)
- [Lesson 17 — Add the Stations table and define a relationship](#lesson-17--add-the-stations-table-and-define-a-relationship)
- [Lesson 18 — Display related fields and portals](#lesson-18--display-related-fields-and-portals)
- [Lesson 19 — Automate tasks in the Script Workspace](#lesson-19--automate-tasks-in-the-script-workspace)
- [Lesson 20 — Manage accounts, table permissions, and privileges](#lesson-20--manage-accounts-table-permissions-and-privileges)
- [Lesson 21 — Back up the database and solution bundle](#lesson-21--back-up-the-database-and-solution-bundle)
- [Reference: Sample dataset](#reference-sample-dataset)
- [Verified limitations](#verified-limitations)

---

## Before you start

Launch the containerized stack from the repository root:

```bash
docker compose up -d
```

This starts three containers: `file4base-postgres`, `file4base-api`, and
`file4base-web`. Confirm that the API server is healthy:

```bash
curl -s http://127.0.0.1:8080/healthz
```

The endpoint responds with `"status":"pass"` and the active database engine.

Open **http://localhost:3000** in a browser to launch **File4Base Web Client**,
or start the native desktop application (macOS, Linux, or Windows). Both clients
connect to the same API (`http://127.0.0.1:8080` by default) over an
AES-256-GCM encrypted payload channel and behave identically.

To run against MariaDB instead of PostgreSQL, start the stack with
`docker compose -f docker-compose.mariadb.yml up -d`.

---

## Lesson 1 — The File4Base workspace and modes

File4Base operates as a four-mode state machine:

| Mode | Purpose | Keyboard Shortcut |
| --- | --- | --- |
| **Browse** | View, enter, edit, and delete records | `Cmd/Ctrl + B` |
| **Find** | Build and run search requests to filter the found set | `Cmd/Ctrl + F` |
| **Layout** | Design forms, reports, labels, charts, and portals | `Cmd/Ctrl + L` |
| **Preview** | Inspect paginated output before printing or PDF export | `Cmd/Ctrl + U` |

Switch between modes from the **View** menu or the mode selector in the bottom
status bar.

The workspace is organized into four regions:

- **Top Menu Bar**: `File`, `Edit`, `View`, `Insert`, `Format`, `Records` (or
  `Requests` in Find mode), `Scripts`, `Tools`, `Window`, and `Help`.
- **Context Toolbar**: Mode-specific actions and view switchers (`Form`, `List`,
  `Table` / `Report`).
- **Left Status Sidebar**: Active layout selector, record navigation controls,
  andfound-set indicators.
- **Bottom Status Bar**: Zoom level, operational mode, project/database name,
  save indicator, and signed-in account badge.

Commands that are not applicable to the current mode or not yet implemented are
shown disabled (greyed out).

---

## Lesson 2 — Create the solution and database

1. Open the **File4Base Connect & Login** dialog.
   - Verify that **API Server URL** points to `http://127.0.0.1:8080` (or click
     **Connect** after entering a custom host).
2. Click the **+** button next to the **Database / Solution** selector to open
   the **New Database Solution** dialog.
3. In **Solution Name**, enter `Boreal Seed Archive`.
   The **Base File Name** and **Database Name** populate automatically as
   `boreal_seed_archive.f4p` and `boreal_seed_archive_db`.
4. In **User**, enter `curator`. In **Password**, enter `Boreal2026!`.
   - The initial owner password must be at least 8 characters and distinct from
     the username and database name.
5. Leave **Create physical database in PostgreSQL if it does not exist**
   checked.
6. Optionally click the folder icon next to **Save location on hard drive** to
   pick a local reference directory. Once chosen, File4Base remembers that
   folder as the persistent destination for future `.f4p` saves and data
   exports.
7. Click **Create & Save Solution**.

Back in the login dialog, `boreal_seed_archive_db` is selected and registered in
the API control store alongside its project metadata. Enter `Boreal2026!` and
click **Connect & Sign In**.

The workspace opens on the welcome screen with a **Create First Database Table**
button, and the status bar displays `boreal_seed_archive_db` and
`curator (owner)`.

---

## Lesson 3 — Define the Accessions table and fields

### Create the `Accessions` table

1. Click **Create First Database Table** (or choose
   **File > Manage > Database...**).
2. Select the **Tables** tab and click **Create First Table**.
3. For **Display Name**, type `Accessions`. Leave **SQL Name** blank so
   File4Base derives `accessions`.
4. Click **Create**.

File4Base creates the table in PostgreSQL/MariaDB with an automatic `ID` primary
key column.

### Add the columns

Switch to the **Fields** tab. For each entry below, click **New Field...**,
enter the **Field Label**, select the **Field Type**, and click **Add Field**:

| Field Label | Column Name | Field Type |
| --- | --- | --- |
| Specimen Code | `specimen_code` | Text |
| Genus | `genus` | Text |
| Species | `species` | Text |
| Station | `station` | Text |
| Storage Tier | `storage_tier` | Text |
| Vault Corridor | `vault_corridor` | Text |
| Tray Slot | `tray_slot` | Text |
| Bioregion | `bioregion` | Text |
| Country | `country` | Text |
| Curator Phone | `curator_phone` | Text |
| Upkeep Grant | `upkeep_grant` | Number |
| Last Tested | `last_tested` | Date |
| Cataloged Date | `cataloged_date` | Date |
| Seed Scan | `seed_scan` | Container |

Close **Manage Database**. File4Base automatically generates the default layout
**Accessions Form** containing each field and its human-readable label.

---

## Lesson 4 — Enter and commit records

1. In **Browse** mode, click **New** in the toolbar.
   An uncommitted draft record opens and the top-right status badge reads
   **Not committed**.
2. Enter the first row from the [Sample dataset](#reference-sample-dataset):
   - **Specimen Code**: `BSA-101`
   - **Genus**: `Betula`
   - **Species**: `nana`
   - **Station**: `Nordic Vault`
   - **Storage Tier**: `Standard`
   - **Vault Corridor**: `North Wing A`
   - **Tray Slot**: `Rack 2`
   - **Bioregion**: `Bergen`
   - **Country**: `Norway`
   - **Curator Phone**: `470-1100`
   - **Upkeep Grant**: `75`
   - **Last Tested**: `2025-01-14`
   - **Cataloged Date**: `2021-04-10`
3. Choose **Records > Commit Record** (or click **New** to commit and start the
   next record). The status indicator turns to **Saved**.
4. Enter the remaining 15 accession records from the
   [Sample dataset](#reference-sample-dataset).
   - If you start a draft record by mistake, choose **Records > Revert Record**
     before committing to discard it.

---

## Lesson 5 — Navigate Form, List, and Table views

With all 16 records entered, the navigation bar reports `Record 1 of 16` and
`16 records · Unsorted`.

- Use the `|<`, `<`, `>`, and `>|` buttons, drag the slider, or type a record
  number and press **Enter** to jump directly to a record.
- Switch between the three representations in the toolbar:
  - **Form**: Displays a single record at a time using the full canvas layout.
  - **List**: Renders records vertically one below another.
  - **Table**: Displays a spreadsheet grid with one column per field.

---

## Lesson 6 — Find records and save searches

1. Click **Find** in the toolbar (or choose **View > Find Mode**).
   The form clears to accept search criteria and the operator bar appears at the
   top of the canvas.
2. Type `Bergen` into **Bioregion** and click **Perform Find**.
   File4Base returns to Browse mode with `4 found of 16 · Unsorted`.
3. Click **Show All** in the toolbar to restore all 16 records.

### Combining criteria (AND) and multiple requests (OR)

- **AND within one request**: Enter `Bergen` in **Bioregion** and `Standard` in
  **Storage Tier**. All 4 Bergen accessions use Standard storage, so **4**
  records are returned.
- **OR across multiple requests**:
  1. Enter **Find Mode** and type `Bergen` in **Bioregion**.
  2. Click **+** in the request bar (or **Requests > New Request**) to create
     `Request 2 of 2`.
  3. Type `Uppsala` in **Bioregion** and click **Perform Find**.
  4. File4Base returns **6** records (4 from Bergen + 2 from Uppsala).

### Omitting records (`NOT`)

To exclude a subset from your results, add a third request, enable **Omit
(NOT)**, type `Cryogenic` in **Storage Tier**, and run the find. One of the
Uppsala accessions (`Pinus sylvestris`) is Cryogenic, leaving **5** records.

### Saving a find

1. While those three requests are active in Find mode, choose
   **Requests > Saved Finds > Save Current Find...**.
2. Name the saved find `Bergen or Uppsala Standard` and click **Save**.
3. At any time in Browse mode, open **Records > Saved Finds** and click
   `Bergen or Uppsala Standard` to re-run the exact search.

### Verified find results on the 16-record dataset

| Search Criteria | Matching Records |
| --- | --- |
| Bioregion = `Bergen` | 4 |
| Bioregion = `Uppsala` | 2 |
| Bioregion = `Geneva` | 4 |
| Country = `Sweden` | 2 |
| Storage Tier = `Cryogenic` | 6 |
| Storage Tier = `Standard` | 10 |
| Upkeep Grant `>=180` | 6 |
| Genus `Bet*` | 2 |
| Bioregion `Bergen` OR Bioregion `Uppsala` (2 requests) | 6 |
| Bioregion `Bergen` OR `Uppsala`, omitting Storage Tier `Cryogenic` | 5 |

---

## Lesson 7 — Sort records across multiple fields

1. Switch to **Table** view in Browse mode and click **Sort** in the toolbar.
2. In **Add a field**, choose **Genus** (ascending) and click **Sort**.
   The status bar updates to `Sorted by Genus ↑`.
3. Notice that two accessions share the genus **Betula** (`nana` and `pendula`)
   and two share **Pinus** (`cembra` and `sylvestris`).
4. Open **Sort** again, add **Species** (ascending) as a second sort level below
   **Genus**, and click **Sort**.
   Ties within the same genus are now broken alphabetically by species:
   `Betula nana` precedes `Betula pendula`, and `Pinus cembra` precedes
   `Pinus sylvestris`.
5. Choose **Records > Unsort** to return to insertion order.

---

## Lesson 8 — Arrange objects in Layout mode

Choose **View > Layout Mode** (`Cmd/Ctrl + L`). The studio workspace displays:

- **Top Toolbars**: Layout creation, drawing and control tools, layout/theme
  selectors, grid snapping (`Snap 8px`), undo/redo, and the **Done / Browse**
  (`Exit Layout`) action button.
- **Left Dock (`Fields` & `Objects`)**: Drag columns onto the canvas or inspect
  the hierarchy of canvas elements.
- **Canvas & Horizontal Part Badges**: Resizable bands for `Header`, `Body`, and
  `Footer` with horizontal pill badges in the left gutter.
- **Right Inspector**: Four tabs covering geometry/anchors, fill and border
  styling, data bindings, and typography.

Select any field box to resize it via its 8 handles or nudge it with the arrow
keys. Click **Exit Layout** to save and return to Browse mode.

---

## Lesson 9 — Configure layout properties and add a second layout

1. In **Layout Mode**, click an empty area of the canvas to inspect layout-level
   properties: canvas width, part heights, background fill/image, transition
   effects, and `OnLayoutEnter` / `OnLayoutExit` script triggers.
2. Click **New Layout / Report** in the toolbar, name the layout
   `Storage Tier Directory`, keep **Accessions** as the context table, and click
   **Create Layout**.

---

## Lesson 10 — Compute values with a calculation field

Accessions stored in **Cryogenic** tanks cost **180** per year to maintain,
whereas **Standard** vault accessions cost **75**.

1. Open **File > Manage > Database...**, select **Accessions**, and switch to
   the **Fields** tab.
2. Click **New Field...**, enter `Annual Upkeep` as the label, choose
   **Calculation**, and click **Add Field**.
3. Click **Options...** on the `Annual Upkeep` row and enter the formula:

   ```
   IF(storage_tier = "Cryogenic"; 180; 75)
   ```

4. Set **Calculation result is** to **Number** and click **OK**.

File4Base evaluates the formula on the server and stores the computed value on
every record. In Browse mode, `Annual Upkeep` displays `75` for the 10 Standard
accessions and `180` for the 6 Cryogenic accessions.

Other useful formulas supported by the calculation engine for this table include:

```
UPPER(genus) & " " & species & " [" & specimen_code & "]"
```

```
IF(ISEMPTY(tray_slot); vault_corridor; vault_corridor & " / " & tray_slot)
```

```
YEAR(TODAY()) - YEAR(cataloged_date)
```

---

## Lesson 11 — Constrain input with value lists

1. Choose **File > Manage > Value Lists...** and click **New Value List...**.
2. Name the list `Storage Tiers`, keep **Custom values** selected, and enter:

   ```
   Standard
   Cryogenic
   ```

3. Click **Save**.
4. Create a second value list named `Active Bioregions`, select **From a field**,
   and pick table **Accessions** → field **Bioregion**.
5. In **Layout Mode**, select the **Storage Tier** field on **Accessions Form**,
   open the Inspector's **Data** tab, set **Control Style** to
   **Radio button set**, and bind **Values from** to `Storage Tiers`.
6. Select the **Bioregion** field, set **Control Style** to **Drop-down list**,
   and bind **Values from** to `Active Bioregions`.
7. Click **Exit Layout** to use the controls in Browse mode.

---

## Lesson 12 — Group and subtotal with a summary report

### Add summary fields

In **Manage Database > Fields** for **Accessions**, create two **Summary**
fields:

| Field Label | Column Name | Summary Operation |
| --- | --- | --- |
| Total Annual Upkeep | `upkeep_total` | *Total of* → **Annual Upkeep** |
| Accession Count | `accession_count` | *Count of* → **Genus** |

### Build the grouped report layout

1. In **Layout Mode**, click **New Layout / Report**, name it
   `Upkeep Summary Report`, and create it.
2. Select the **Part Tool** in the toolbar and click the canvas to add a
   **Sub-summary** part grouped **When sorted by** `Storage Tier`.
3. Use the **Part Tool** again to add a **Trailing grand summary** part.
4. Place objects into the bands as follows:
   - **Header**: Report title and column labels (`Genus`, `Bioregion`,
     `Annual Upkeep`).
   - **Sub-summary by Storage Tier**: `Storage Tier`, `Accession Count`, and
     `Total Annual Upkeep`.
   - **Body**: `Genus`, `Bioregion`, and `Annual Upkeep`.
   - **Trailing Grand Summary**: Label `All Accessions Total` and
     `Total Annual Upkeep`.
5. Save with **Exit Layout** and switch to **Report** view in the toolbar.
6. Click **Sort for this report** on the banner prompt. The report groups and
   subtotals all 16 records:

| Group / Genus | Bioregion / Count | Annual Upkeep |
| --- | --- | --- |
| **Cryogenic** | **6** | **1080** |
| **Standard** | **10** | **750** |
| **All Accessions Total** | **16** | **1830** |

If you filter the found set in Find mode to `Bioregion = Geneva` (4 records:
3 Cryogenic at 180 and 1 Standard at 75), the report dynamically recomputes over
the found set: **Cryogenic: 540**, **Standard: 75**, **Total: 615**.

---

## Lesson 13 — Visualize summaries with charts

1. In **Layout Mode**, create a **Blank layout** named `Upkeep Charts`.
2. Select the **Chart Tool** and drag a chart frame onto the Body part.
3. Double-click the chart to open **Chart Setup** and configure:
   - **Chart type**: `Column`
   - **Title**: `Annual Upkeep by Storage Tier`
   - **Group the records by**: `Storage Tier`
   - **Series**: `Total Annual Upkeep`, `Accession Count`
4. Add a second chart beside it configured as **Pie**, grouped by
   `Storage Tier`, with series `Total Annual Upkeep` and **Show each slice as a
   percentage** enabled (`Cryogenic: 1080 (59%)`, `Standard: 750 (41%)`).
5. Click **Exit Layout** to view the live charts in Browse mode.

---

## Lesson 14 — Build specimen labels and a dispatch notice with merge fields

### Specimen jar labels

1. In **Layout Mode**, click **New Layout / Report** and choose **Labels**.
2. Name the layout `Accession Jar Labels`, select fields `Specimen Code`,
   `Genus`, `Species`, `Vault Corridor`, `Tray Slot`, and `Bioregion`, and keep
   stock **Avery L7160 (A4)** (`63 × 38 mm · 3 across, 7 down`).
3. Click **Create Layout**. File4Base places a merge block on the label body:

   ```
   {{specimen_code}}
   {{genus}}
   {{species}}
   {{vault_corridor}}
   {{tray_slot}}
   {{bioregion}}
   ```

4. Switch to **View > Preview Mode**. The toolbar reports
   `16 label(s) on 1 sheet(s)`. Accessions whose `tray_slot` is empty collapse
   that line automatically so `{{bioregion}}` prints directly beneath
   `{{vault_corridor}}` without a blank gap.

### Curator dispatch notice

Create a **Blank layout** named `Annual Upkeep Notice` with a text block
containing:

```
{{CurrentDate}}

{{genus}} {{species}} ({{specimen_code}})
{{vault_corridor}}
{{tray_slot}}
{{bioregion}}, {{country}}

Annual preservation upkeep for this accession ({{storage_tier}} tier)
is scheduled at {{annual_upkeep}} EUR.
```

---

## Lesson 15 — Preview pages and export to PDF

Choose **View > Preview Mode** (`Cmd/Ctrl + U`). The left sidebar displays paper
dimensions, orientation, and margins from **File > Page Setup...**, and the
toolbar's **Print / Export PDF** button renders the paginated PDF document.

---

## Lesson 16 — Import and export data

### Exporting records

Choose **File > Export Records...**, pick **CSV** or **Excel Workbook (.xlsx)**,
and click **Export**. If you have already selected a reference folder on disk,
File4Base writes the exported file directly into that folder without prompting
again or redirecting to the browser's `Downloads` folder.

### Importing a CSV file

Create `new_accessions.csv`:

```csv
Specimen Code,Genus,Species,Bioregion,Country,Storage Tier,Upkeep Grant,Last Tested
BSA-117,Salix,polaris,Tromso,Norway,Cryogenic,180,2026-02-11
BSA-118,Empetrum,nigrum,Oulu,Finland,Standard,75,2026-03-02
```

Choose **File > Import Records > File...**, select `new_accessions.csv`, verify
the automatic column mapping and `Year first` date order, and click **Import**.
Both rows are inserted in a single atomic transaction and their `Annual Upkeep`
calculation field is evaluated automatically (`180` and `75`).

---

## Lesson 17 — Add the Stations table and define a relationship

### Create the `Stations` table

Open **File > Manage > Database...**, create a second table named `Stations`
(`stations`), and add these fields:

| Field Label | Column Name | Field Type |
| --- | --- | --- |
| Station | `station` | Text |
| Station Address | `station_address` | Text |
| Bioregion | `bioregion` | Text |
| Country | `country` | Text |

In Browse mode on **Stations Form**, enter three collecting stations:

| Station | Station Address | Bioregion | Country |
| --- | --- | --- | --- |
| Nordic Vault | 12 Fjordveien | Bergen | Norway |
| Alpine Lab | 44 Rue du Mont-Blanc | Geneva | Switzerland |
| Coastal Botanical | 88 Kyrkogatan | Uppsala | Sweden |

### Link `Stations` and `Accessions`

1. Open **Manage Database > Relationships Graph**.
2. Click **Relationship...**.
3. Select `stations` on the left and `accessions` on the right, matching
   `station` = `station`.
4. Tick **Allow records to be created in "accessions" through this
   relationship** and click **OK**.

---

## Lesson 18 — Display related fields and portals

### Related field on `Accessions Form`

1. Open **Accessions Form** in **Layout Mode** and drag a **Field** object next
   to `Station`.
2. In the Inspector's **Data** tab, set **Table Occurrence** to
   `accessions · through "stations_to_accessions"` and **Bound Column** to
   `Station Address`.
3. Return to **Browse Mode**: records assigned to `Nordic Vault` display
   `12 Fjordveien`, `Alpine Lab` displays `44 Rue du Mont-Blanc`, and
   `Coastal Botanical` displays `88 Kyrkogatan`.
4. In **Find Mode**, searching `*Mont-Blanc*` in the related `Station Address`
   field returns the **5** accessions belonging to `Alpine Lab`.

### Portal of accessions on `Stations Form`

1. Open **Stations Form** in **Layout Mode** and draw a **Portal** below the
   station fields.
2. In **Portal Setup**, choose `accessions · through "stations_to_accessions"`,
   set **Rows** to `6`, set **Sort rows by** to `genus,species`, and enable
   vertical scrolling, record creation, and deletion.
3. Drag `Specimen Code`, `Genus`, `Species`, and `Annual Upkeep` into the top
   row of the portal and click **Exit Layout**.
4. In **Browse Mode**, the first station (**Nordic Vault**) lists its **6**
   accessions sorted by genus and species (`Alpine Lab` lists **5**, and
   `Coastal Botanical` lists **5**).

---

## Lesson 19 — Automate tasks in the Script Workspace

Choose **Scripts > Script Workspace...**, click **+ Script**, and assemble steps
from the right-hand catalog (*Go to Layout*, *Enter Find Mode*, *Set Field*,
*Perform Find*, *Sort Records*, *Show Custom Dialog*). Bind the script to a
button on any layout via **Button Setup...**.

---

## Lesson 20 — Manage accounts, table permissions, and privileges

Choose **File > Manage > Security...**:

- **Accounts & Granular Permissions**: Create user accounts (`owner`, `admin`,
  `user`, `read_only`) and configure both **Table Access & Modification
  Privileges** (`Read/Write`, `Read-Only`, `No Access`, plus granular `Create
  records`, `Edit records`, `Delete records`, and `Modify schema` flags) and
  **Layout Access Privileges**.
- **Extended Privileges**: Toggle `Bulk record export` (`fmpexport`) and
  `Bulk record import` (`fmpimport`) per role.
- All project-to-database mappings, options, and account/table/layout
  permissions are persisted in the API's embedded SQLite control store and
  bundled inside `.f4p` solution files.

---

## Lesson 21 — Back up the database and solution bundle

- **Solution Bundle (`.f4p`)**: Choose **File > Save a Copy As...** to write a
  complete `.f4p` bundle (including tables, records, layouts, scripts, value
  lists, accounts, and table/layout permissions) to your configured reference
  folder.
- **PostgreSQL Cluster Backup**: Run `./scripts/backup_postgres.sh backup` from
  the host terminal to create a full `pg_dumpall` archive in `./backups`.

---

## Reference: Sample dataset

The 16 initial accessions used across this tutorial (`Curator Phone` is
`470-1100` for all rows):

| Specimen Code | Genus | Species | Station | Storage Tier | Vault Corridor | Tray Slot | Bioregion | Country | Upkeep Grant | Last Tested | Cataloged Date |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| BSA-101 | Betula | nana | Nordic Vault | Standard | North Wing A | Rack 2 | Bergen | Norway | 75 | 2025-01-14 | 2021-04-10 |
| BSA-102 | Betula | pendula | Nordic Vault | Standard | North Wing A | | Uppsala | Sweden | 75 | 2025-02-09 | 2020-09-18 |
| BSA-103 | Pinus | sylvestris | Coastal Botanical | Cryogenic | Cryo Tank 1 | | Uppsala | Sweden | 180 | 2025-03-04 | 2025-03-04 |
| BSA-104 | Gentiana | verna | Alpine Lab | Standard | South Vault B | | Bergen | Norway | 75 | 2025-02-27 | 2019-06-14 |
| BSA-105 | Dryas | octopetala | Nordic Vault | Cryogenic | Cryo Tank 2 | | Geneva | Switzerland | 180 | 2025-04-18 | 2025-04-18 |
| BSA-106 | Saxifraga | aizoides | Coastal Botanical | Standard | West Annex C | Shelf 3 | Reykjavik | Iceland | 75 | 2025-05-22 | 2022-01-09 |
| BSA-107 | Leontopodium | nivale | Alpine Lab | Standard | South Vault B | | Geneva | Switzerland | 75 | 2025-03-30 | 2021-09-18 |
| BSA-108 | Pinus | cembra | Alpine Lab | Cryogenic | Cryo Tank 2 | | Geneva | Switzerland | 180 | 2025-06-11 | 2025-06-11 |
| BSA-109 | Calluna | vulgaris | Nordic Vault | Standard | North Wing A | | Edinburgh | Scotland | 75 | 2025-01-27 | 2018-04-03 |
| BSA-110 | Vaccinium | myrtillus | Coastal Botanical | Standard | West Annex C | Tray 4 | Bergen | Norway | 75 | 2025-02-02 | 2022-07-30 |
| BSA-111 | Primula | farinosa | Alpine Lab | Cryogenic | Cryo Tank 3 | | Tartu | Estonia | 180 | 2025-07-19 | 2025-07-19 |
| BSA-112 | Juniperus | communis | Nordic Vault | Standard | North Wing A | | Tromso | Norway | 75 | 2025-03-14 | 2020-02-11 |
| BSA-113 | Rubus | chamaemorus | Coastal Botanical | Cryogenic | Cryo Tank 1 | | Oulu | Finland | 180 | 2025-08-08 | 2025-08-08 |
| BSA-114 | Angelica | archangelica | Alpine Lab | Standard | South Vault B | | Bergen | Norway | 75 | 2025-05-06 | 2019-12-01 |
| BSA-115 | Rhodiola | rosea | Nordic Vault | Standard | North Wing A | | Reykjavik | Iceland | 75 | 2025-04-02 | 2021-05-25 |
| BSA-116 | Campanula | thyrsoides | Coastal Botanical | Cryogenic | Cryo Tank 2 | | Geneva | Switzerland | 180 | 2025-09-12 | 2025-09-12 |

---

## Verified limitations

The following items are tracked in open or documented GitHub issues so users do
not spend time looking for them:

- **Merge variables (`<<$var>>`) on layout text blocks**
  ([#33](https://github.com/file4base/file4base-app/issues/33)): *Insert > Merge
  Variable...* is disabled. Merge *fields* (`{{field_name}}`) and system symbols
  (`{{CurrentDate}}`, `{{CurrentTime}}`, `{{CurrentUser}}`, `{{PageNumber}}`)
  work in Browse and Preview modes (Lesson 14).
- **Per-group page breaks in summary reports**
  ([#32](https://github.com/file4base/file4base-app/issues/32)): Summary reports
  flow continuously across pages rather than forcing a page break after each
  sub-summary group.
- **Folder image import and host ODBC bridge**
  ([#47](https://github.com/file4base/file4base-app/issues/47)): *Import
  Records > Folder...* is disabled. Direct SQL imports use File4Base's built-in
  PostgreSQL, MySQL/MariaDB, and SQL Server drivers rather than an OS ODBC
  manager (Lesson 16).
- **Per-client transport toggles**
  ([#39](https://github.com/file4base/file4base-app/issues/39)): Because desktop
  and web clients authenticate against the same REST API, access is governed by
  account role, table privileges, layout privileges, and extended privileges
  rather than client-type switches (Lesson 20).
