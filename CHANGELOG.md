# Changelog

All notable changes to the **File4Base** project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [0.9.1] - 2026-10-06

### Fixed
- Preview mode: the left sidebar showed "Page: 1 of 1" and "Margins: 0.5 in", both fixed values that ignored Page Setup. It now shows the paper size and orientation, the real printable area and the four margins, with a Page Setup button, in the same card as the other modes. Which record is shown is reported by the preview's own toolbar.
- Find mode: the left sidebar did not match the rest of the interface. Its controls floated on the background with mixed alignment (a centred notebook drawing above left-aligned text), and it showed a "Requests: 1" counter that was always 1. The Find controls now sit in the same titled card as the Layout selector above them, the Omit checkbox toggles from its label too, the buttons say "Perform Find" and "Cancel Find", and the fake counter is gone. The Perform Find button is now readable in dark mode.

## [0.9.0] - 2026-10-06

### Changed
- Browse mode creates a new record on the server when it is committed (leaving it, New Record again, Commit Records), like FileMaker, instead of storing an empty record at once: validation rules apply to the record as a whole, fields left empty get their auto-enter values, and a rejected record stays open with the reason. Reverting or deleting an uncommitted new record discards it.
- Solution files follow a versioned format shared by the server and the client ([docs/specs/solution_bundle_format.md](docs/specs/solution_bundle_format.md), version 2.0), checked by golden files that each side writes and the other reads ([#5](https://github.com/file4base/file4base-app/issues/5)). File > Save, Save As and Save a Copy now ask the server for the file (`POST /api/v1/solutions/export`), so a saved file is always one that File > Open restores; before, the client wrote its own format, which the server could not import. Files written by the 1.0 client and solution files exported by 1.0 servers are still read.

### Security
- A sign-in that was in progress while an owner changed the account's password or role, disabled it or deleted it could still create a usable session ([#12](https://github.com/file4base/file4base-app/issues/12)). Sessions now carry the account state they were opened with, and every request checks it against the current account: sessions of deleted, disabled, demoted or re-passworded accounts are rejected and revoked, whatever their creation order. The session used to change one's own password stays valid.
- Solution files no longer contain passwords ([#10](https://github.com/file4base/file4base-app/issues/10)): the new-database file stored the owner password with a reversible fixed-key encoding, File Options wrote the remembered sign-in password in plain text, and the export endpoint accepted a password in the URL. None is written any more; a password in an old file is ignored when it is opened, and Open Solution asks for the password instead of prefilling it. The remembered File Options password stays in memory for the session. If you shared solution files saved by earlier versions, change the passwords they contained.
- Restoring a solution no longer widens access ([#11](https://github.com/file4base/file4base-app/issues/11)): accounts lost their disabled state and layout permissions and all received the same password. Files now carry each account's role, active state and layout permissions (never a password); missing accounts are restored disabled with an unknown random password and their permissions, to be enabled by an owner who sets their password. Existing accounts are never changed, and the client no longer recreates accounts itself when loading a file.
- Column options could run SQL with the server's database role ([#1](https://github.com/file4base/file4base-app/issues/1)): `default_value` was pasted into the table DDL as `DEFAULT <text>`. It is now metadata only (a JSON object of field options, or nothing), never part of any SQL statement; anything else, such as an SQL expression, is rejected with `422` when adding or updating a column, and through solution import. Physical columns no longer get a `DEFAULT` clause.
- Reserved and colliding table names ([#2](https://github.com/file4base/file4base-app/issues/2)): an internal table such as `sys_users` could be registered as a user table (making password hashes readable through the data API), and names longer than 63 characters were truncated by PostgreSQL into the same physical table. Table and column names now follow one policy (letters, digits and underscores, at most 63 characters; `sys_`, `pg_` and engine schema names reserved), user tables are created without `IF NOT EXISTS` so an existing table is never adopted (`409` for a catalog name in use), and the data API and data import refuse internal tables even if a catalog row names one. Data import also rejects fields that are not registered for their table before writing any row.
- Malformed import files could exhaust the server's memory ([#13](https://github.com/file4base/file4base-app/issues/13)): the MessagePack decoder allocates from declared collection sizes, and its allocation limit is never applied. Solution and data imports now check the file's structure first (declared sizes against the bytes present, nesting depth, total entries) and reject a malformed file with `400` without allocating or changing anything.
- New Database no longer provisions predictable owner credentials ([#9](https://github.com/file4base/file4base-app/issues/9)): the password field starts empty, an empty username or password is no longer replaced by the database name, and the server rejects owner passwords shorter than 8 characters or equal to the username or the database name. Databases created earlier with `admin`/`admin` or with the database name as password keep working: change that password from the account's Change Password dialog.
- File Options no longer reports a password change as successful when the server rejected it, could not be reached, or the new password was blank ([#20](https://github.com/file4base/file4base-app/issues/20)). It now uses the shared Change Password dialog, and the remembered sign-in password changes only after the server accepted the new one.

### Fixed
- Field validation rules were saved but never enforced ([#15](https://github.com/file4base/file4base-app/issues/15)). Record writes now apply not empty, unique, existing value, strict type, range, maximum length and custom messages; violations answer `422` with the field and rule, and nothing is written. Unique rules get a unique index, so concurrent duplicates cannot both succeed. Data import applies the rules whose timing is "Always". Malformed rules are rejected when saved.
- Editing records right after adding a field could fail with "cached plan must not change result type": record reads used `SELECT *`, whose prepared statement PostgreSQL refuses once the table has another column. Reads now name the table's registered fields.
- The PostgreSQL restore script reported success after SQL errors and restarted the API into a partially restored database ([#21](https://github.com/file4base/file4base-app/issues/21)). It now checks the archive before any downtime, aborts if the API cannot be stopped, stops at the first SQL error (only the statements recreating its own role are skipped), leaves the API stopped after a failed restore, starts it again only if it was running, and reports each outcome with its own exit status. Backups are only kept when `pg_dumpall` completed.
- Solution restore lost relationships and custom table occurrences and bound layouts to the wrong table ([#3](https://github.com/file4base/file4base-app/issues/3)). The import now validates the whole file first, matches existing objects by name, rewrites every reference (occurrences, relationship fields, layout tables, script steps, permissions, and the scripts run by buttons and triggers) to the destination's IDs, and, if a write fails, removes what it created and restores what it updated. Importing the same file twice creates nothing new. Creating a layout for a table occurrence that does not exist now fails instead of using another table. The response reports what was created and updated.
- Solution files did not include scripts ([#17](https://github.com/file4base/file4base-app/issues/17)). Scripts and their ordered steps (parameters, disabled steps, nesting) are now saved and restored.
- Data import reported rejected records as restored ([#4](https://github.com/file4base/file4base-app/issues/4)). It now restores all records in one transaction: the first record the database rejects rolls everything back and is reported with its table and position. Records whose id already exists are counted as skipped, and tables the database does not have are listed (`records_inserted`, `records_skipped`, `skipped_tables`).
- CONTAINER (binary) values were exported as text and could not be restored ([#14](https://github.com/file4base/file4base-app/issues/14)). Data files keep them as MessagePack binary, byte for byte, and the JSON data API returns and accepts them as base64.
- Field options broke table duplication and left an incomplete copy ([#16](https://github.com/file4base/file4base-app/issues/16)): the options JSON reached the DDL as a default. Duplication now copies columns and options, and removes the copy if a column cannot be copied. A constant auto-enter value ("Data" in the Fields dialog) now fills new records that do not supply the field; an explicit value still wins.
- Export Records saved only the first 100 records ([#6](https://github.com/file4base/file4base-app/issues/6)): it asked for 10,000 rows, which the server silently replaced by its 100-row default. Exports now read every record page by page (in `id` order) and save the file only when all pages arrived. The data API clamps `limit` to its 1,000-row maximum instead of falling back to 100.
- JSON numbers sent to the data API kept only float64 precision ([#18](https://github.com/file4base/file4base-app/issues/18)): `9007199254740993` was stored as `9007199254740992`. Numbers now reach the database exactly as written, on create and update.
- Creating a database from the sign-in dialog filled the username with the database name ([#23](https://github.com/file4base/file4base-app/issues/23)); it now fills the owner username.
- Script Workspace Run and Debug showed a successful execution with invented results without running anything ([#24](https://github.com/file4base/file4base-app/issues/24)). Run is disabled until the workspace can run scripts (scripts run from layout buttons in Browse mode), and Debug is now Step Preview, labelled as a preview that executes nothing. The footer no longer names a script engine that does not exist.

### Legal
- The vendored Swagger UI 5.33.0 ships with its Apache-2.0 `LICENSE` and `NOTICE` and the license files referenced by its script banners, served next to the scripts; `VENDOR.md` records the version and checksums ([#7](https://github.com/file4base/file4base-app/issues/7)).
- The API image contains `/usr/share/doc/file4base/THIRD_PARTY_NOTICES` with the license texts of Go and of every Go module linked into the server, generated at build time from the binary (`server/tools/third_party_notices.sh`); the build fails if a module has no license file ([#22](https://github.com/file4base/file4base-app/issues/22)).

## [0.8.0] - 2026-10-06

### Added
- Layout mode: with no object selected, the inspector shows the layout's own properties: width and height (and the header, body and footer heights), background color, background picture (fill, fit, stretch or tile), OnLayoutEnter / OnLayoutExit script triggers and a transition effect (fade, slide or zoom). New layouts get them too. Browse mode paints the background, plays the transition when a layout is shown and runs the triggers: OnLayoutEnter when the layout is shown, OnLayoutExit then OnLayoutEnter when going to another layout. Preview mode paints the background as well.
- Layout mode: select several objects with Shift-click or by dragging a selection rectangle on the empty canvas. Selected groups can be dragged, nudged with the arrow keys, duplicated and deleted together.

### Fixed
- Layout mode: the canvas kept a blank area below the footer that could not be removed (it was always at least 600 pt tall). The layout now ends at the bottom of the footer, or of the lowest object. Browse and Preview modes also use the layout's width and height instead of a minimum size.
- Layout mode: the resize handles of a selected object did not resize it, and Shift-click did not add to the selection. A mouse press held longer than 100 ms made the canvas deselect everything even when the click landed on an object or a handle; the canvas now only deselects on a click on empty space. The handles are also larger and fully clickable (half of each one sat outside the object's hit-test area).

### Changed
- Docker images moved to the `cloudresources` Docker Hub organization: `cloudresources/file4base-api` and `cloudresources/file4base-web` (all published versions were copied). `docker-compose.yml` and `scripts/publish_images.sh` use it by default; `FILE4BASE_REGISTRY` still overrides it.

## [0.7.1] - 2026-10-06

### Fixed
- Preview mode "Export PDF / Print" printed a capture of the whole browser window (toolbars included) instead of the previewed page, and did nothing in the desktop app. It now builds a PDF of the previewed sheet only, at the Page Setup paper size and orientation, and opens the print dialog with it (web and desktop). Content taller than one sheet continues on the next pages.
- File > Print prints the page shown in Preview mode in the same way.
- Preview mode cut off layouts wider than the paper (and printed them cut off); they are now scaled down to fit the printable width.
- Script Workspace: closing with unsaved scripts asks again to save them (the confirmation was dropped while merging the English UI change).

### Added
- Preview mode "Print / Export PDF" menu: print this page, print all records (one sheet per record), and save this page or all records as a PDF file.
- Client dependencies `pdf` and `printing`.

### Changed
- Use English throughout the client interface, including security, scripts, relationships, themes, file options, page setup, and help dialogs.
- Set English as the default client locale regardless of the host language, including standard Material controls.

## [0.7.0] - 2026-10-06

### Added
- Button Setup window in Layout mode, opened with a double click on a button, Enter, right click > Button Setup... or the inspector: set the label and choose what a click runs in Browse mode (nothing, a single step or a script). "New script..." creates a script, assigns it and opens the Script Workspace; "Edit scripts..." reloads the list when the workspace closes.
- Layout mode canvas shows under each button the action it runs ("Perform Script: New contact"), and objects have a context menu (Button Setup, Edit text, Duplicate, Bring to front, Send to back, Delete).
- Browse mode record bar: first, previous, next and last record, an editable "Record N of M" box, a slider, the found set summary ("N found of M · Sorted by ...") and the save status.
- Browse mode actions: New, Duplicate, Delete (with confirmation), Find, Sort and Show All; Records > Sort Records... opens the same sort dialog.
- Browse mode List and Table views (Table: click a header to sort, double click a row to open it in Form view).
- `Cmd/Ctrl+↑` / `Cmd/Ctrl+↓` move to the previous / next record in Browse mode.
- Docker images are also published for `linux/arm/v7` (Raspberry Pi with a 32-bit OS), besides `linux/amd64` and `linux/arm64`.

### Changed
- The status sidebar no longer shows the notebook, record counter and slider in Browse mode (they moved to the record bar), and the duplicate record header above the form was removed.
- The inspector Data tab of a button shows a summary of its action and a Button Setup... button instead of inline action fields.

### Fixed
- Records > Duplicate Record created an empty record instead of duplicating the current one.
- Dragging the record slider could save field edits into the wrong record.

## [0.6.1] - 2026-10-06

### Fixed
- Scripts created in the Script Workspace did not appear in the button "Perform Script" list: the layout designer loaded the list once and never refreshed it. It now reloads when a button is selected and when the Script Workspace is closed (from the inspector, the Manage menu or the Scripts menu), and shows load errors instead of "No scripts yet".
- Script Workspace: saving created a new copy of every new script on each save, because scripts kept their local id after the server assigned another one. Created scripts now adopt the server id.
- Script Workspace: closing the window with unsaved scripts discarded them silently; it now asks to save, discard or cancel.

## [0.6.0] - 2026-10-06

### Added
- Layout mode: Line, Rectangle, Rounded Rectangle and Oval are drawn by dragging on the canvas (a click still places a default size). Lines run horizontally or vertically along the longer side of the drag.
- Layout mode: line (border) color swatches and hex field, line width slider, fill "none", and text color swatches in the inspector; the status sidebar stroke control sets the line width of the selected object and of new drawings.
- Layout mode: text inside rectangles, rounded rectangles and ovals, edited in place with a double click or Enter (Esc cancels); labels accept several lines.
- Layout mode: Insert > Picture, PDF, Audio/Video, QuickTime and File embed the file (up to 2 MB) inside the selected shape or in a new `media` object; pictures can fit, fill-and-crop or stretch.
- Layout mode: Insert > Current Date, Current Time, Current User Name, Page Number and Merge Field add merge symbols (`{{CurrentDate}}`, `{{field_name}}`, ...) to the selected text or a new label; Browse and Preview modes show their values.
- Button setup in the inspector Data tab: a button runs a single step (New, Duplicate, Delete, Commit or Revert Record, Go to Record, Enter Find Mode, Perform Find, Show All Records, Enter Preview Mode, Go to Layout, Set Field, Show Custom Dialog, Open URL) or performs a stored script with an optional parameter.
- Client-side runner for button actions and Perform Script (record, navigation, Set Field, dialog and URL steps). Steps that need the calculation engine (`If`, `Loop`, `Set Variable`, REST calls) stop the script with a message.
- Set Tab Order tool (layout toolbar and status sidebar, replacing the Tab Control and Rotate tools): click fields and buttons in Tab key order, or number them automatically in reading order. Browse and Find modes follow that order.
- Layout mode keyboard: arrow keys move the selected object (Shift: 8 pt), Esc returns to the pointer.
- Layout JSON schema: `rect`, `rounded_rect`, `oval`, `line` and `media` objects and the `text`, `tab_order`, `action` and `media` properties.

### Fixed
- Tools picked in the status sidebar palette (Line, Rectangle, Oval, ...) did nothing: the selection never reached the layout designer.
- Typing in the inspector text fields reset the cursor and lost input on every rebuild, and Backspace/Delete deleted the selected object while typing.
- The border color could only be typed as hex, the "None" fill swatch did not clear the fill, and malformed colors broke the canvas.
- Ovals were drawn as circles, and lines, ovals and shapes rendered as plain rectangles in Browse and Preview modes.
- Button text color and size ignored the object style; buttons only acted according to words in their label (kept for buttons without an action).
- Quick consecutive inspector edits (a color, then a width) could undo each other.

## [0.5.1] - 2026-10-06

### Added
- Docker Hub distribution: `marioezquerro/file4base-api` and `marioezquerro/file4base-web` multi-arch images (`linux/amd64`, `linux/arm64`), published with `scripts/publish_images.sh` under the version tag and `latest`.
- `scripts/backup_postgres.sh` to back up and restore every PostgreSQL database of the `file4base-postgres-data` volume (`backup`, `list`, `restore`).

### Changed
- Docker images are tagged with the application version instead of `latest`; `docker-compose.yml` references the Docker Hub images (`docker compose pull`) and still builds them locally with `--build`. `FILE4BASE_VERSION` and `FILE4BASE_REGISTRY` override the tag and namespace.
- Dockerfiles cross-compile on the build host's platform, so multi-arch builds need no emulation.
- The web client image is renamed from `file4base-app-web` to `file4base-web`, following the `file4base-` naming convention.
- `scripts/bump_version.sh` also synchronizes the About dialog, the OpenAPI spec and the image tags in `docker-compose.yml`.
- Documented data persistence of the PostgreSQL volume and the backup workflow.

## [0.5.0] - 2026-10-06

### Security
- **API authentication (breaking)**: every schema, data, security and solution endpoint now requires a session. `POST /api/v1/auth/login` returns a bearer token (`Authorization: Bearer <token>`); added `POST /api/v1/auth/logout` and `GET /api/v1/auth/session`. Only the health probes, Swagger UI, sign-in and the database selector remain public.
- **Server-side authorization**: `owner` / `admin` / `user` roles are enforced on every route. Per-layout permissions are enforced by the server too (hidden layouts, layout saving, and record access through the data API), instead of only in the client.
- **Per-session database (breaking)**: removed the server-wide "active database". A session is bound to the database it signed in to, so one client can no longer redirect the requests of another. `POST /api/v1/databases/switch` is deprecated and only reports whether a database exists.
- **Data API restricted to user tables**: `/api/v1/data/{table}` only serves tables registered in `sys_tables` and fields registered in `sys_columns`. System tables such as `sys_users` (password hashes) are no longer reachable. Data imports (`.f4data`) are restricted to catalog tables as well.
- **Login requires a matching username and password**: removed the fallback that accepted any owner's password regardless of the username. Response timing no longer reveals whether a username exists.
- **No default accounts**: removed the automatic `admin` / `admin` owner. `POST /api/v1/databases` requires the credentials of the first owner, returns `409` for an existing database instead of adding an owner to it, and can be limited to signed-in owners with `ALLOW_PUBLIC_DATABASE_CREATION=false`.
- **Dropping a database requires proof of ownership**: an owner session on that database, or the credentials of one of its owners.
- Sessions are revoked when a user's password, role or status changes, when the user is deleted, and when the database is dropped. The last active owner can no longer be demoted or deactivated.
- Explicit `none` layout permissions are now stored (previously they were dropped and silently became `read_write`).
- CORS origins are configurable through `CORS_ALLOWED_ORIGINS`; solution and data imports are size-limited; engine-internal databases (`postgres`, `mysql`, ...) cannot be signed in to, created or dropped through the API.

### Added
- `internal/auth` session store and `api.Mount` route assembly with public and protected groups.
- Client: the API client keeps the session token, sends it on every request, signs out on the server, and locks the workspace when the session expires.
- Client: the "Drop Database" confirmation asks for the owner credentials of the databases being dropped.
- CI: new `test-server` job running `go vet` and the Go test suite against a PostgreSQL service; releases depend on it.
- `.env.example` and environment-driven credentials, bind address and server options in `docker-compose.yml`.
- End-to-end API tests covering authentication, database isolation, roles, layout permissions and the data API restrictions.

### Changed
- `client/Dockerfile` is now multi-stage and builds the Flutter web bundle itself, so `docker compose up` works on a clean clone.
- Database ports in `docker-compose.yml` are published on `127.0.0.1` by default.
- Health responses no longer expose `active_database` and report the configured engine.
- Removed the hard-coded `dev_password` defaults from the solution export endpoint and the client's connection model; the server warns when it falls back to the development DSN.
- Manage Database: the "switch" action explains that another database is opened by signing in to it; creating a database requires an owner user and password (no `admin` / `admin` prefill).
- Opening a solution file signs in first and imports afterwards; packaged users are synchronized after sign-in.
- Updated client and roadmap documentation to match the implemented application and current feature status.
- Clarified empty-database startup behavior and existing Docker volume persistence in the deployment guide.
- Aligned database login empty-state messages with the repository's English language policy.
- Clarified that `AGENTS.md` and its referenced rules are authoritative over overlapping Antigravity guidance.
- Documentation aligned with the code: Go 1.26, repository structure, WebSockets and the calculation engine marked as planned, OpenAPI spec updated with the bearer scheme and the auth and database endpoints.

### Removed
- Unused duplicate `messagepack` Dart dependency (`msgpack_dart` is the one in use).

### Fixed
- `DROP DATABASE` used a hard-coded `postgres` administrative connection; it now uses the configured administrative database.
- The Go test suite no longer depends on pre-existing accounts or on the order in which packages run.

## [0.4.26] - 2026-10-01

### Changed
- **Zero-State Clean Database Initialization & Connection**:
  - Removed default database `file4base_dev` from `docker-compose.yml`, starting PostgreSQL purely with administrative database `postgres` and 0 application databases.
  - Initialized `ListDatabases` in DBAL with an empty slice `[]` (instead of null JSON) when no user databases exist.
  - Removed `file4base_dev` pre-selection in `DatabaseLoginDialog`: displays placeholder *"Selecciona una base de datos"* or *"Ninguna base de datos disponible"* when no databases exist.
  - Added strict validation preventing login without selecting or creating a database via the `+` button or importing an `.f4p` solution file.
  - Cleaned up all fallback references to `file4base_dev` in solution models, status bar, and security dialogs.

## [0.4.25] - 2026-10-01

### Added
- **File Menu Quit Action with Auto-Save Safety**:
  - Added `Quit` action at the bottom of the `File` menu (with shortcut `Cmd+Q` / `Ctrl+Q`).
  - Automatically commits pending layout edits when in Layout Mode.
  - Flushes debounced auto-save immediately to persist all solution configuration and metadata to disk.
  - Prompts a confirmation dialog before terminating the process on Desktop or closing the tab/window in WebDirect mode (`SolutionStorageService.triggerQuit()`).

## [0.4.24] - 2026-10-01

### Fixed
- **Desktop Environment Preflight & Database Engine Validation**:
  - **macOS Docker App & Socket Detection**: Enhanced `EnvironmentChecker` to inspect symlinks, check `/Applications/Docker.app`, and detect `/var/run/docker.sock` and `~/.docker/run/docker.sock` so Docker Desktop is recognized even if CLI binaries are not in minimal GUI launcher `PATH`.
  - **macOS App Sandbox Disabled for Low-Code Docker Orchestration**: Disabled `com.apple.security.app-sandbox` in `Release.entitlements` and `DebugProfile.entitlements` so that standalone macOS desktop builds distributed via DMG/ZIP can execute Docker CLI commands and communicate with the local Docker daemon socket.
  - **Single Active Database Engine Validation**: Corrected the preflight check so that having either PostgreSQL OR MariaDB active (with the API and Web containers or healthy `/healthz` returning 200) satisfies startup requirements without failing or demanding both engines concurrently.
  - **Automated Docker Desktop & Container Launch**: Added `startDockerAndContainers` in `EnvironmentChecker` and integrated it with `PreflightDialog`. When Docker Desktop is closed or stopped, users can click "Launch Docker Desktop" to automatically launch Docker Desktop, poll for the daemon to become responsive, and start the project containers (`postgres`, `api`, `web`) without requiring manual terminal intervention.
  - **Container State Discovery**: Added `checkProjectContainers` using `docker ps` to verify running container state (`file4base-postgres` or `file4base-mariadb` + `file4base-api` + `file4base-web`) so the desktop application immediately identifies ready local services.
  - **Dynamic Engine Status**: Updated desktop status bar and authentication workflow to dynamically show the active database engine (`PostgreSQL` or `MariaDB`) rather than assuming PostgreSQL.

## [0.4.23] - 2026-10-01

### Changed
- **File Menu Streamlining & Enhancements**:
  - **Removed "Close"**: Removed the redundant "Close" option from the `File` menu.
  - **Removed "Sharing" Submenu**: Cleaned up the non-functional `Sharing` submenu (`Share with File4Base Clients...`, `Enable File4Base WebDirect...`, `Share with ODBC/JDBC...`).
  - **Removed "Save/Send Records As" Submenu**: Removed the non-functional `Save/Send Records As` submenu (which contained Excel, PDF, Snapshot Link placeholders).
  - **Active User Password Modification**: The `File -> Change Password...` menu option now opens a dedicated `ChangePasswordDialog` that allows the authenticated user to update their account password directly on the active database.
  - **Comprehensive Record Export**: The `File -> Export Records...` menu option now launches an `ExportRecordsDialog` supporting export to standard formats: CSV, TSV/Excel, JSON, XML, and HTML table with optional header fields.
  - **Native Platform Printing**: Wired `File -> Print...` and the preview toolbar's `Export PDF / Print` button to trigger native browser printing (`window.print()`) in WebDirect mode and platform preview printing.
  - **Universal Password Visibility Toggles**: Added interactive visibility toggles (`Icons.visibility` / `Icons.visibility_off`) to all password fields across the application, allowing users to temporarily reveal or conceal entered passwords in:
    - `DatabaseLoginDialog` (Main database authentication & login screen).
    - `ManageSecurityDialog` (User account creation/edit, duplicate account dialog, and change password dialogs).
    - `ChangePasswordDialog` (New password and confirm password fields).
    - `NewDatabaseDialog` (New solution database owner password).
    - `OpenSolutionDialog` (Server connection password and encrypted file credentials).
    - `ManageDatabaseDialog` (Create database on server modal).
    - `FileOptionsDialog` (Auto-login credentials and change password confirmation).

### Added
- **Manage Database Global "Databases" Tab & Lifecycle Management**:
  - Added a new primary **Databases** tab in `ManageDatabaseDialog` displaying all physical databases registered on the PostgreSQL / MariaDB server.
  - Implemented multi-database selection with checkboxes, master header checkbox ("Select All" with tristate support), and real-time selected database count badge with quick-clear button.
  - Added individual database actions:
    - **Active (Connected) badge**: clearly distinguishes the currently connected database context.
    - **Switch**: quickly switch active database context directly from the table.
    - **Drop Database**: single database deletion with safety checks.
  - Added batch database operations:
    - **Drop (N) Selected...**: permanently drop multiple selected databases in a single operation, protected by a consolidated safety confirmation dialog.
    - Protected system and development databases (`postgres`, `file4base_dev`) and active database from accidental deletion.
  - Added **New Database...** quick action button to create databases on the server directly from the dialog with initial owner credentials.
  - Added backend database deletion:
    - Implemented `MultiDatabaseManager.DropDatabase(ctx, dbName)` with session termination (`pg_terminate_backend`) and connection pool cleanup.
    - Registered `DELETE /api/v1/databases/{name}` REST API endpoint in `SolutionHandler`.
    - Added `deleteDatabase` method to Flutter `ApiClient`.

## [0.4.21] - 2026-09-30

### Fixed
- **Strict Database and Solution Isolation for Security Users**:
  - Fixed an issue where creating a new database or switching database context automatically provisioned development default accounts (`admin`, `file4base`, `file4base_dev`) into `sys_users`.
  - In `server/internal/schema/service.go`:
    - Removed automatic provisioning of development credentials (`file4base`, `file4base_dev`).
    - When a new database is created with an explicit initial owner, only that specific owner user is provisioned.
    - Default fallback to `admin` only occurs if `sys_users` is entirely empty and no owner was requested.
  - In `client/lib/features/solution_manager/new_database_dialog.dart`:
    - Added the `user` property to `NewDatabaseDialogResult` and configured the initial `SolutionPackage` to only include the designated owner user for that new database/solution.
  - In `client/lib/main.dart`:
    - Fixed `_handleNewDatabase` authentication logic to log in using the selected owner username (`result.user`) instead of accidentally using the database name (`result.databaseName`).
    - Fixed solution export so that it strictly exports the active database's user roster without injecting foreign accounts.
  - Verified that switching databases and accessing **Manage Security** only queries and displays the users belonging to the active database.

## [0.4.20] - 2026-09-30

### Added
- **Manage Database Multi-Table Selection & Batch Operations**:
  - Added checkboxes to the **Tables** tab in `ManageDatabaseDialog` allowing multi-selection of database tables.
  - Added a **Select All** header checkbox with tristate support (selected, unselected, partially selected) and a real-time selection badge showing the number of selected tables with a quick-clear button.
  - Added batch operations in the bottom action bar:
    - **Batch Duplicate (`Duplicate (N)`)**: duplicates multiple selected tables in one step.
    - **Batch Truncate / Empty (`Empty (N)...`)**: empties records from all selected tables while preserving table structures and columns, with a consolidated warning dialog.
    - **Batch Delete (`Delete (N)...`)**: permanently drops multiple selected tables and their columns in a single confirmation flow.
  - Single-table actions (`Rename...` and `Manage Fields ->`) gracefully adapt when single or multiple tables are checked.

### Added
- **GitHub Pages Documentation Website (`https://file4base.github.io/file4base-app/`)**:
  - Published comprehensive single-page documentation app at `docs/index.html` featuring responsive sidebar navigation, real-time search, dark modern theme, and detailed guides for all 4 operational modes, schema management, calculations, script workspace, database-isolated security, and REST API.
  - Added automated CI/CD deployment workflow `.github/workflows/deploy_docs.yml` using `actions/deploy-pages@v4` to continuously build and publish documentation upon pushes to `main`.
- **Help Menu Activation & Interactive Service & Support Guide**:
  - Activated all Help menu items in `File4BaseMenuBar`:
    - `File4Base Help`: opens the official GitHub Pages documentation website.
    - `Resource Center`: opens documentation tutorials and template guides.
    - `Product Documentation`: links directly to API references and architecture specifications.
    - `File4Base Community`: navigates to GitHub Discussions for community collaboration.
    - `Service & Support...`: presents new modal `IssuesGuideDialog` educating users on best practices (searching existing tickets, title conventions, step-by-step reproduction, version/environment logs, and credential privacy) before opening GitHub Issues.
    - `Check for Updates...`: opens `CheckUpdatesDialog` displaying current version (`v0.4.19`) with release links.
    - `About File4Base`: opens detailed system information and credits modal.

### Fixed
- **macOS Desktop Connectivity & App Transport Security (ATS)**:
  - Added `NSAppTransportSecurity` to `client/macos/Runner/Info.plist` with `NSAllowsArbitraryLoads` and `NSAllowsLocalNetworking` enabled, preventing macOS network security from blocking cleartext HTTP requests to `http://localhost:8080`.
  - Added `com.apple.security.network.server` and `com.apple.security.files.user-selected.read-write` to `Release.entitlements` and `DebugProfile.entitlements` for smooth local networking and file operations.
  - Fixed false-positive Docker errors in macOS GUI launches: `EnvironmentChecker` now scans standard installation paths (`/usr/local/bin/docker`, `/opt/homebrew/bin/docker`, etc.) since macOS GUI apps do not inherit shell `$PATH`.
  - Added fast-path health check to `EnvironmentChecker`: if the File4Base API server is already running and healthy, preflight checks pass immediately without requiring host Docker CLI invocations.

### Changed
- **Docker Infrastructure Naming Convention Enforcement**:
  - Enforced mandatory `file4base-` prefix across all containers, images, volumes, and networks.
  - Renamed backend container and image to `file4base-api` in `docker-compose.yml`, `docs/specs/ARCHITECTURE.md`, and `AGENTS.md`.

## [0.4.18] - 2026-09-30

### Fixed
- **Unauthenticated Access Leak on Login Cancellation**:
  - Fixed security vulnerability where dismissing or cancelling the initial database authentication dialog allowed unauthenticated access to Browse Mode, exposing database tables, field schemas, and records.
  - Implemented strict unauthenticated state handling: cancelling authentication immediately purges all in-memory database context (`_tables`, `_selectedTable`, `_activeLayout`, `_serverLayouts`, and sets `_currentUser = null`).
  - Protected all table loading and background health checks so unauthenticated sessions are completely blocked from querying table schemas or records.
  - Added dedicated Protected Workspace interface displaying a security shield badge, active database indicator, and quick actions (`Sign In to Database`, `Open Solution (.f4p)`, `New Solution...`, `Connect Remote Server...`).
  - Added session locking actions in `File4BaseMenuBar`: toggles between `Sign In...` and `Sign Out (Lock Session)`. All mode changes and database management dialogs now require an active authenticated session.

### Changed
- **Database & Solution-Scoped User Security (`Manage > Security`)**:
  - Replaced global user sharing with strict per-database user isolation. Accounts and permissions now belong exclusively to the specific database/solution and are non-transferable across databases.
  - Backend `SecurityHandler` now routes all user administration (`ListUsers`, `CreateUser`, `UpdateUser`, `DeleteUser`, `GetUserPermissions`, `SetUserPermissions`) directly to the target database driver resolved via `?database=...` query parameter, `X-Database-Name` header, or payload body.
  - Solution Package (`.f4p`) synchronization:
    - `ExportSolution` exports user accounts from `sys_users` into the MessagePack solution bundle (`bundle.Users`).
    - `ImportSolution` syncs bundle users into the target database's `sys_users`.
    - User mutations in `ManageSecurityDialog` trigger `AutoSaveService.instance.markDirty()` so edits persist to the active `.f4p` solution file.
  - Client `ApiClient` and `ManageSecurityDialog` now pass `database` parameter across all security requests.

## [0.4.17] - 2026-09-30

### Added
- **FileMaker Pro Layout Mode Studio Redesign (`OperationalMode.layout`)**:
  - **Eliminated Duplicate & Unwired UI**: Removed narrow 120px browse sidebar during Layout Mode to prevent duplicate tools and reclaim full-screen canvas width.
  - **Top Bar 1 (Layout Tools Palette Ribbon)**:
    - **`[ + ] New Layout / Report`**: Dedicated shortcut to trigger the layout creation workflow.
    - **Centered Ribbon with 16 Interactive Tools**: Selection Arrow (`Pointer`), Text Tool (`T`), Line Tool (`\`), Rectangle (`▢`), Rounded Rectangle (`▢`), Oval (`○`), Field/Control Tool, Button Tool, Popover Button Tool, Button Bar Tool, Tab Control Tool, Portal Tool, Chart Tool, Web Viewer Tool, Part Tool, and Format Painter Tool.
    - **Manage Dropdown Menu**: Direct access to Database, Layouts, Security, Script Workspace, and Themes.
    - **Show/Hide Panes Toggles**: Quick controls to toggle the Left Fields/Objects Pane and the Right Inspector Pane.
  - **Top Bar 2 (Layout Context Bar)**:
    - **Layout Selector & Quick Rename**: Dropdown listing all server layouts with pencil shortcut to rename inline.
    - **Table Occurrence Indicator**: Pill badge showing underlying table occurrence context.
    - **Theme Selector**: Dropdown to switch themes (`Enlightened`, `Enlightened Touch`, `Enlightened Print`, `Minimalist`).
    - **Snap 8px Grid & Quick Controls**: Snap-to-grid toggle, Undo / Redo history stack, auto-save status indicator dot.
    - **Prominent `[ Exit Layout ]` Button**: Green-accented button that flushes pending changes, commits layout to API, and seamlessly returns to Browse Mode.
  - **Left Pane (Fields & Objects Panel)**:
    - **Fields Tab**: Table occurrence picker, real-time search filter, A-Z sort toggle, field rows with data type badges (`TT`, `#`, `📅`, `⏱`, `⏱📅`, `🖼`, `fx`, `∑`).
    - **Direct Drag & Drop**: Dragging fields drops them directly onto the canvas at precise cursor coordinates.
    - **`[ + New Field ]` Creator**: In-place modal dialog to add new database columns via REST API without leaving Layout Mode.
    - **Collapsible Drag Preferences**: Customization for Field Placement (Horizontal vs Vertical), Include Label toggle, and Control Style (Edit Box, Dropdown, Popup Menu, Checkbox Set, Radio Set, Calendar).
    - **Objects Tab**: Hierarchical tree of canvas objects grouped by part (`HEADER`, `BODY`, `FOOTER`) with selection, z-order, and delete actions.
  - **Interactive Canvas with Dynamic Part Divider Resizing**:
    - **Left Gutter Part Tabs**: Vertical tabs for `Header`, `Body`, and `Footer` with double-click "Part Setup" dialog for exact point height entry.
    - **Draggable Part Divider Lines**: Horizontal divider lines with handle tabs between Header, Body, and Footer allowing interactive dragging with live point height tooltip badges (`Header: 80 pt`, `Body: 520 pt`, `Footer: 50 pt`).
    - **8-Handle Interactive Object Resizing**: Top-left, top-center, top-right, middle-right, bottom-right, bottom-center, bottom-left, middle-left resize handles with live bounds update and 8pt grid snapping.
  - **Right Inspector (4 Tabs)**:
    - **Position & Geometry**: Coordinates (Left, Top, Right, Bottom), Dimensions (Width, Height), FileMaker 4-pin autosizing anchor box, Arrange & Align tools (Left, Center, Right, Top, Middle, Bottom, Bring Front, Send Back).
    - **Appearance & Styles**: Fill color palette swatches and custom hex code input, border color, border width, and corner radius slider.
    - **Data Binding**: Table occurrence, bound column picker, control style, and Browse/Find entry permission checkboxes.
    - **Typography & Text**: Display text editor, font family, font size, bold toggle, text color, and alignment options (Left, Center, Right, Justify).
  - **Automated Tests**:
    - Added `client/test/layout_designer_filemaker_studio_test.dart` asserting all tools, left pane tabs, canvas parts, object selection, and inspector tab switching (54 client tests passing).

## [0.4.16] - 2026-09-25

### Added
- **Table Operations in Schema Manager (`File > Manage > Database > Tables`)**:
  - Added dedicated action buttons for each table row in `ManageDatabaseDialog`:
    - **Fields Shortcut (`Icons.view_column_outlined`)**: Quick navigation to inspect and manage fields for the selected table.
    - **Rename Table (`Icons.edit_outlined`)**: Dialog allowing editing of human-readable display names with immediate schema synchronization.
    - **Duplicate Table (`Icons.copy_outlined`)**: Creates a clone of table definitions including non-primary-key columns, field types, and calculation formulas with automatic `_copy` naming.
    - **Empty Table (`Icons.cleaning_services_outlined`)**: Truncates all row data from the physical table while preserving schema structure, with safety confirmation prompt.
    - **Delete Table (`Icons.delete_outline`)**: Permanently drops the physical database table and cascades removal across system catalogs (`sys_tables`, `sys_columns`, `sys_table_occurrences`), with strict confirmation dialog.
- **Backend Table REST APIs**:
  - `DELETE /api/v1/schemas/tables/{id}`: Drops physical table and cleans up catalog entries.
  - `PUT /api/v1/schemas/tables/{id}/rename`: Updates table display name.
  - `POST /api/v1/schemas/tables/{id}/duplicate`: Clones table schema structure.
  - `POST /api/v1/schemas/tables/{id}/truncate`: Deletes all data rows from the target table.
- **Docker Compose Custom Volume Names**:
  - Configured explicit custom volume names in `docker-compose.yml`:
    - `file4base-postgres-data` for PostgreSQL persistence.
    - `file4base-mariadb-data` for MariaDB profile persistence.
    - `file4base-net` for the bridge network.
  - Prevents volume orphaning or duplication when cloning into different directory names or running with custom project flags.
  - Migrated existing local data from legacy auto-prefixed volume `file4base-app_postgres_data` to `file4base-postgres-data`.
- **Layout Unification Across All Operational Modes (Browse, Find, Layout, Preview)**:
  - **Layout Mode (Modo Presentación)**: Added `commitAndSave()` and `onLayoutChanged` in `LayoutDesignerWidgetState` so layout changes are committed and synced to `_activeLayout` in `main.dart` and persisted via API immediately on switching modes, eliminating debounce timer loss.
  - **Browse Mode (Modo Hojear)**: Active layout is rendered in Form view with real-time bound record values. Even when a table has 0 records, the designed layout canvas is preserved with interactive click-to-create fields and a top banner with `[Create First Record]`.
  - **Find Mode (Modo Buscar)**: Renders the active layout canvas with criteria input fields and query operators (`*`, `...`, `=`, `==`, `!`, `>`, `<`), enabling visual query design directly on the layout.
  - **Preview Mode (Modo Vista previa)**: Replaced mock tabular view with `_buildLayoutPreviewCanvas`, rendering the exact designed layout objects (labels, bound fields, buttons, portals, shapes) on a simulated sheet of paper honoring margins, paper size, orientation, and record pagination.
  - Added automated widget tests in `client/test/layout_unification_test.dart` validating layout rendering in Browse, Find, and Preview modes.

## [0.4.15] - 2026-09-25

### Added
- **Visual Relationships Graph (`File > Manage > Database > Relationships Graph`)**:
  - Implemented the visual relational schema graph in Tab 3 of `ManageDatabaseDialog`, matching the canonical low-code relational specification:
    - **Draggable Table Occurrence (TO) Boxes**: Interactive cards showing occurrence names (`CLIENTES`, `FACTURAS`, `CLIENTES 2`), header bar with collapse/expand toggle, and field lists with key icons.
    - **Selection State & Amber Glow**: Selected Table Occurrence displays an amber/gold halo glow outline matching the visual design.
    - **Relational Match Connector Lines**: Smooth orthogonal cubic bezier curves linking connected fields across occurrences.
    - **Interactive Operator Badges**: Centered badges on relationship lines displaying criteria comparison operators (`=`, `≠`, `<`, `≤`, `>`, `≥`). Clicking selects the relationship, double-clicking opens relationship editor.
    - **"Especificar tabla" Modal Dialog (`SpecifyTableDialog`)**:
      - Exact replication of the canonical dialog: source dropdown ("Archivo actual"), table list view with selection highlight, and automatic occurrence name generator (suggesting e.g. `CLIENTES 2` when `CLIENTES` already exists).
    - **"Especificar relación" Modal Dialog (`SpecifyRelationshipDialog`)**:
      - Two-column table occurrence and field selectors with middle comparison operator dropdown.
      - Canonical relational checkboxes:
        - `Permitir la creación de registros en esta tabla a través de esta relación` (`allow_creation`)
        - `Eliminar registros relacionados en esta tabla cuando se elimine un registro en la otra tabla` (`cascade_delete`)
        - `Ordenar registros relacionados` (`sort_related`)
      - Relationship deletion and criteria update actions.
    - **Canvas Toolbar & Navigation**: Buttons to add occurrence (`+ Tabla...`), add relationship (`+ Relación...`), delete selection, and center/reset view (`Centrar vista`).
- **Backend Schema & REST APIs**:
  - Added full CRUD endpoints for Table Occurrences:
    - `GET /api/v1/schemas/occurrences`: Lists all occurrences.
    - `POST /api/v1/schemas/occurrences`: Creates a new table occurrence.
    - `PUT /api/v1/schemas/occurrences/{id}`: Updates occurrence name and/or coordinates `(x_pos, y_pos)`.
    - `DELETE /api/v1/schemas/occurrences/{id}`: Deletes occurrence with cascading cleanup.
  - Added full CRUD endpoints for Relationships:
    - `GET /api/v1/schemas/relationships`: Lists all relationships.
    - `POST /api/v1/schemas/relationships`: Creates a new relationship.
    - `PUT /api/v1/schemas/relationships/{id}`: Updates relationship operator, name, or cascade options.
    - `DELETE /api/v1/schemas/relationships/{id}`: Deletes relationship.
  - Cross-engine support for both PostgreSQL and MariaDB dialects.
  - Solution bundle export and import now packages both Table Occurrences and Relationships.
- **Automated Tests**:
  - Added 6 Flutter tests in `client/test/relationships_graph_test.dart` (model serialization, `SpecifyTableDialog`, `SpecifyRelationshipDialog`, and `RelationshipGraphWidget` canvas).
  - Added Go tests in `server/internal/schema/relationship_service_test.go` and `server/internal/api/schema_handler_test.go`.

## [0.4.14] - 2026-09-25

### Added
- **Interactive Canvas Zoom Controls & Scaling Engine**:
  - Implemented interactive zoom magnification controls in the bottom-left corner of the workspace:
    - **Percentage Magnification Indicator**: Displays current zoom level (e.g. `100`, `150`, `75`) with interactive popup menu offering standard low-code zoom presets (`400%`, `300%`, `200%`, `150%`, `100% (Actual Size)`, `75%`, `50%`, `25%`).
    - **Zoom Out Button (`Icons.zoom_out`)**: Magnifying glass button with minus (`-`) stepping down through preset zoom levels with bounds checking (min 25%).
    - **Zoom In Button (`Icons.zoom_in`)**: Magnifying glass button with plus (`+`) stepping up through preset zoom levels with bounds checking (max 400%).
  - **View Menu Integration (`File4BaseMenuBar`)**:
    - Wired `View > Zoom In` (`Cmd/Ctrl + =`), `View > Zoom Out` (`Cmd/Ctrl + -`), `View > Actual Size (100%)` (`Cmd/Ctrl + 0`), and `View > Zoom Level` presets submenu.
  - **Global Keyboard Shortcuts (`CallbackShortcuts`)**:
    - Registered `Cmd/Ctrl +`, `Cmd/Ctrl -`, `Cmd/Ctrl 0` to quickly adjust canvas zoom level from anywhere in the window.
  - **Workspace Canvas Scaling (`_buildZoomableBody`)**:
    - Dynamic bi-directional horizontal and vertical scrollbars automatically wrapping the scaled workspace when zoom exceeds viewport size.
    - Hit-testing and interactive record editing fully preserved via Flutter inverse transformation matrix.
  - **Automated Tests**: Added unit and widget tests in `client/test/zoom_test.dart` (6 tests covering zoom stepping, limits, menu triggers, and reset).

## [0.4.13] - 2026-09-25

### Added
- **Themes Management System (`File > Manage > Themes...` / `ManageThemesDialog`)**:
  - Implemented free theme manager modal dialog allowing real-time discovery, inspection, and switching between canonical developer and design palettes:
    - **Light (Clean Slate)**: Default theme featuring pure crisp white canvas (`#FFFFFF` surface, `#F8FAFC` background) with slate typography and cyan accents.
    - **Dark Modern (Sophisticated)**: Midnight dark workspace with graphite panels and cyan highlights (`#0B1120` / `#111827`).
    - **Monokai Pro (Code Studio)**: Iconic developer palette with charcoal background (`#272822`) and vibrant neon yellow/green/pink step accents.
    - **Dracula Midnight**: Rich dark purple palette (`#282A36`) with soft lavender borders and emerald highlights.
    - **Nordic Frost (Nord)**: Minimalist arctic palette (`#2E3440` / `#3B4252`) with calm frost cyan accents.
    - **Solarized Light & Solarized Dark**: Low-contrast precision palettes (`#FDF6E3` / `#002B36`).
  - **Live Preview Panel**: Split-view with interactive mockups (menu bar, form cards, script editor with syntax highlighting, and color hex swatches) reacting instantaneously to selected themes.
  - **Fast Switch Toolbar**: Quick buttons for one-click switching directly from the dialog footer.
  - **System-Wide Reactivity**: Script Workspace dialog, Calculation Builder dialog, and root MaterialApp react to theme changes without reloading or losing state.
  - **Automated Tests**: Added 9 unit and widget tests in `client/test/manage_themes_test.dart`.



## [0.4.12] - 2026-09-25

### Added
- **Script Workspace IDE & Execution Engine (`Scripts > Script Workspace...` / `Manage > Scripts...`)**:
  - Implemented 3-panel visual scripting canvas matching canonical visual IDE architecture:
    - **Header & Controls**: Active database badge (`file4base_dev`), real-time unsaved changes indicator, `▶ Ejecutar` (Run script) button with execution simulation and trace inspector, `🐞 Depurar` (Debug script) button with step-by-step visual execution and live variable inspection stack (`$subtotal`, `$counter`, `Get(LastError)`), and `💾 Guardar` button persisting script definitions.
    - **Left Panel (Scripts Explorer)**: Real-time search filter `TextField`, `+ Guión` quick creation, active status switches, sequence step badges, and context menu (`Activar/Desactivar`, `Duplicar`, `Eliminar`).
    - **Center Panel (Sequential Step Editor)**: Multi-tab editor for concurrently open scripts with close buttons (`✕`), inline script renaming, context table occurrence selector (`Invoices`, `Customers`, etc.), `ReorderableListView` supporting drag-and-drop step rearrangement, 2-digit indices (`01`, `02`...), step activation checkboxes, category badges (Violet for Navigation, Emerald for Records, Amber for Control & Logic, Cyan for Fields/Variables, Rose for Integration), monospace parameter preview brackets, and hierarchical vertical nesting lines for `If/Else/End If` and `Loop/End Loop`.
    - **Bottom Zone (Contextual Parameter Inspector)**: Context-aware inspector adapting to the active instruction type (`set_variable`, `set_field`, `go_to_layout`, `if`, `exit_loop_if`, `perform_rest_api`, etc.) with variable/target field inputs and `[ fx Especificar... ]` calculation launcher.
    - **Right Panel (Step Catalog)**: Categorized catalog with 4 major categories (Navegación, Registros, Control y Lógica, Integración y Datos), description tooltips, and click/double-click instant insertion.
    - **Bottom Status Bar**: Context table occurrence indicator, total steps counter, and execution engine status badge (`File4Base Script Engine v1.0 (Go/CEL)`).
- **Calculation & Formula Builder Dialog (`CalculationBuilderDialog`)**:
  - Canonically styled formula editor with table occurrence and field browser (`Table::Field`), mathematical and logical operator palette (`+`, `-`, `*`, `/`, `^`, `&`, `=`, `≠`, `>`, `<`, `≥`, `≤`, `AND`, `OR`, `NOT`, `( )`, `" "`), categorized functions list (Agregación, Lógica, Texto, Fecha & Hora, Sistema), search filters, and monospace syntax editor.
- **Backend Schema & Script Service (Go & PostgreSQL/MariaDB)**:
  - Added DDL for `sys_scripts` and `sys_script_steps` system tables in `server/internal/schema/service.go`.
  - Added CRUD and step management in `server/internal/schema/script_service.go` (`ListScripts`, `GetScript`, `CreateScript`, `UpdateScript`, `DeleteScript`, `DuplicateScript`).
  - Added REST API endpoints under `/api/v1/schemas/scripts` and `/api/v1/scripts` in `server/internal/api/schema_handler.go`.
  - Added unit test suite in `server/internal/schema/script_service_test.go` (`TestScriptService_CRUD`).
- **Solution Packaging (`.f4b`)**:
  - Updated `SolutionPackage` in `client/lib/core/models/solution_models.dart` to serialize and deserialize scripts alongside layouts and tables.
- **Automated Tests**:
  - Added widget and model test suite in `client/test/script_workspace_test.dart` (5 tests covering model serialization, formula dialog, and 3-panel workspace layout).

## [0.4.11] - 2026-09-24

### Added
- **Database-Scoped Manage Security Dialog (`Manage > Security` / Gestionar seguridad)**:
  - Redesigned `ManageSecurityDialog` (`client/lib/features/security/manage_security_dialog.dart`) into a comprehensive database-level table/list interface scoped to the active database:
    - **Header & Scope**: Active database badge indicator (`file4base_dev` / custom database), metric badges (Total accounts, Active, Inactive).
    - **Sequential Indexing (`#`)**: 1-based sequential number per row for clear tabular identification.
    - **Interactive Account Activation Toggle**: Direct Switch control to activate or deactivate accounts in real-time (`is_active` column in PostgreSQL `sys_users` table), with safety protections preventing self-deactivation and deactivating the last active Owner account.
    - **Account Name & Identity**: User avatar with initial, username, current active session badge (`(Tú / Activa)`), and creation date tooltip.
    - **Privilege Sets (Conjuntos de privilegios)**: Formatted, color-coded role badges:
      - `[Acceso total] Owner`: Full administrative and schema control.
      - `[Entrada datos y diseño] Admin`: Layout designer and data entry permissions.
      - `[Acceso restringido] User`: Restricted access controlled by layout-level permissions.
    - **Per-Row Action Icon Buttons**:
      - 🔑 **Cambiar contraseña**: Fast modal dialog to update account password.
      - ✏️ **Editar cuenta y privilegios**: Full account configuration dialog with role selector, active status switch, and per-layout permissions matrix (Read & Write / Read Only / No Access).
      - 📋 **Duplicar cuenta**: Clone existing account with `_copia` suffix, pre-copying role and all layout permissions.
      - 🗑️ **Eliminar cuenta**: Permanent deletion with confirmation dialog, protected against deleting self or the sole Owner.
    - **Search & Filter Toolbar**: Instant search bar filtering by username or privilege role, plus status filter chips (`Todas`, `Activas`, `Inactivas`).
    - **Layout Privileges Matrix Tab («Privilegios de presentaciones»)**: Solution-wide layout audit view inspecting user access levels across each layout in the database.
    - **Extended Privileges Tab («Privilegios ampliados»)**: Network and protocol privileges configuration (`f4bapp`, `f4bwebdirect`, `f4brest`, `f4bexport`) per privilege set.

### Changed
- **Backend Schema & Security Service**:
  - Added `is_active BOOLEAN NOT NULL DEFAULT TRUE` column to `sys_users` table and migration in `server/internal/schema/service.go`.
  - Updated `Authenticate`, `ListUsers`, `CreateUser`, and `UpdateUser` in `server/internal/schema/security_service.go` to enforce account activation status and reject deactivated accounts with `user account is deactivated`.
  - Updated REST API endpoint `/api/v1/security/users` (`server/internal/api/security_handler.go`) to accept `is_active` in user creation and update requests.
- **Frontend ApiClient & Models**:
  - Added `isActive` property to `UserModel` with `copyWith`, `toJson`, and updated `fromJson`.
  - Added `isActive` parameter to `ApiClient.createUser` and `ApiClient.updateUser`.


### Removed
- **"Send Mail..." File Menu Item**:
  - Removed "Send Mail..." from `File4BaseMenuBar` under the File menu.
- **Duplicate "Save a Copy As..." Menu Item**:
  - Removed the duplicate "Save a Copy As..." menu entry that was placed immediately above "Recover...".

### Added
- **File Options Dialog (`File > File Options...`)**:
  - Implemented `FileOptionsDialog` (`client/lib/features/solution_manager/file_options_dialog.dart`) and `FileOptionsModel` (`client/lib/core/models/file_options_model.dart`) adhering to canonical File4Base File Options:
    - **Tab 1: «Abrir» (Open)**:
      - *Iniciar sesión como*: Configure automatic login using a default account and password, or entering via the Guest account.
      - *Cambiar contraseña*: Integrated password management modal to update account credentials.
      - *Presentación de inicio*: Select the exact layout (`startup_layout`) to display upon opening the database solution.
      - *Ocultar todas las barras de herramientas*: Full-screen or clean view hiding native toolbars and sidebars.
    - **Tab 2: «Activadores de guión» (Script Triggers)**:
      - Assign automatic script execution to file lifecycle events: `OnFirstWindowOpen`, `OnLastWindowClose`, `OnWindowOpen`, `OnWindowClose`, and `OnFileAVPlayerChange` with optional script parameters.
- **Page Setup Dialog (`File > Page Setup...` / Configurar impresión / Ajustar página)**:
  - Implemented `PageSetupDialog` (`client/lib/features/solution_manager/page_setup_dialog.dart`) and `PageSetupModel` (`client/lib/core/models/page_setup_model.dart`):
    - *Selección de impresora*: Format for "Any Printer" / "Cualquier impresora" (recommended for multi-machine interoperability), system default printer, or PDF virtual writer.
    - *Tamaño del papel*: Standard formats (A4, US Letter, US Legal, A3, A5, B5, Custom) with exact dimensions in millimeters and PostScript points (`pt`).
    - *Orientación*: Interactive toggle for Vertical (Portrait) vs Horizontal (Landscape).
    - *Cálculo de márgenes*: Configurable top, bottom, left, and right margins in mm.
    - *Límites de página y ancho útil*: Live schematic diagram showing printable area and exact metric/point dimensions for report layout and printing.
  - Added "Page Setup..." button and paper configuration badges in Preview Mode toolbar (`LayoutPreviewWidget`).
  - Added shortcut `Shift+Cmd+P` / `Shift+Ctrl+P` for quick Page Setup access.
- **Solution Persistence**:
  - Integrated `fileOptions` and `pageSetup` serialization into `SolutionPackage` and `.f4p` binary packages, automatically preserving startup options and print configurations.


### Removed
- **Redundant "Switch Database / Login..." Menu Item**:
  - Removed `Switch Database / Login...` from `File4BaseMenuBar` under the File menu and deprecated the `onSwitchDatabaseOrLogin` callback.

### Added
- **Load from Disk and Cancel in Initial Connection Dialog (`DatabaseLoginDialog`)**:
  - Added "Cargar de disco duro..." (`Icons.folder_open`) button to `DatabaseLoginDialog`, allowing immediate opening of a `.f4p` / `.f4b` solution directly from local disk upon connecting.
  - Added "Cancelar" action button and window close button (`Icons.close`), making the dialog dismissible (`barrierDismissible: true`) without trapping the user.
- **Strict Database Access Password Enforcement & Owner Password Fallback**:
  - Removed insecure `'admin'` / `'admin'` default prefilling from `DatabaseLoginDialog` and `OpenSolutionDialog`.
  - The password field is always blank and strictly required upon database selection.
  - Enhanced backend `Authenticate` in `security_service.go` to support authenticating with the database password against database owner accounts when logging in to a database with no additional users.
- **Solution-Level User & Permission Packaging**:
  - Updated `_exportCurrentSolutionBytes` in `main.dart` and `SolutionPackage.fromLiveData` in `solution_models.dart` to export the database's users and layout permissions into the `.f4p` solution package.
  - When loading a solution from disk, its packaged users are automatically restored into `sys_users`.
  - `Manage > Security...` manages accounts and layout privileges specifically for the active database/solution independently from other databases.

## [0.4.8] - 2026-09-24

### Added
- **Unified Layout Selector Across Modes (Browse, Layout, Preview)**:
  - Replaced the erroneous top table selector in the left status sidebar (`File4BaseStatusSidebar`) with a dedicated Layout Selector dropdown (`Icons.view_quilt`).
  - Restored canonical low-code database architecture where records are always viewed, found, and printed through a Layout representation (Presentación).
  - Supported switching between all layouts in the active solution with instant synchronization across Browse Mode, Find Mode, Layout Mode, and Preview Mode.
  - Included quick actions in the layout dropdown: **New Layout...** and **Manage Layouts...**.
- **Comprehensive Solution Layout Management (`ManageLayoutsDialog`)**:
  - Implemented `ManageLayoutsDialog` in `client/lib/features/layout_engine/manage_layouts_dialog.dart`.
  - Displays all layouts across the database solution with associated table occurrences, visual object counts, and internal IDs.
  - Added full CRUD management for layouts: Create New Layout, Rename Layout, Duplicate Layout, Delete Layout, and Open / Switch to Layout.
  - Added canonical `Manage > Layouts...` menu entry in `File4BaseMenuBar`.
- **Layout Renaming & Layout Setup Dialog**:
  - Added layout renaming capability in `LayoutDesignerWidget` via editable layout title badge, inline rename modal, and `Layout Setup...` dialog (`_showLayoutSetupDialog`).
  - Resolved bug where renaming a layout reset it to default due to mismatched table occurrence IDs; layouts are now referenced deterministically by `id` with table occurrence linkage.
  - Layout renames persist immediately to `PUT /api/v1/layouts/{id}` and trigger visual updates in the designer and sidebar.
- **Visual Layout Canvas View in Browse Mode (`DataBrowserWidget`)**:
  - Added visual Layout Canvas view rendering (`_buildLayoutCanvasView`) in `DataBrowserWidget` based on the active layout definition (`_activeLayout`).
  - Renders custom fields, labels, buttons, and visual objects at their exact layout coordinates.
  - Integrated field inputs directly into record editing with debounced auto-save, primary key badges, and validation error states.
  - Connected layout buttons to File4Base record actions (New Record, Delete Record, Perform Find, Print/Preview, Record Navigation).
  - Added a toggle between `Form` (custom layout canvas) and `Cards` (standard grid/card view) in the records toolbar.

### Changed
- Rebuilt Flutter WebDirect client bundle (`build/web`) and updated `file4base-web` Docker container.
- Updated version metadata to `0.4.8` across `VERSION`, `server/cmd/server/main.go`, `client/pubspec.yaml`, and `client/lib/features/about/about_dialog.dart`.

## [0.4.7] - 2026-09-24

### Added
- **Field Options Modal ("Options for Field <name>")**:
  - Implemented `FieldOptionsDialog` in `client/lib/features/schema_manager/field_options_dialog.dart` faithfully matching the native field options dialog.
  - **Auto-Enter Tab**: Automatic population rules for Creation/Modification (Date, Time, Timestamp, Name, Account Name), serial number generation (on creation / on commit, next value, increment), value from last visited record, default static data, calculated values (`Specify...`), looked-up values, and option to prohibit modification during data entry.
  - **Validation Tab**: Strict data validation options including Not Empty (Always / During Entry), Unique Value, Existing Value, Strict Data Type (Date, Time of Day, 4-Digit Year, Numeric, Text), Range validation (min/max), Maximum length, and custom error message text.
  - **Storage & Indexing Tab**: Global field storage (single value shared across all records for logos, VAT rates, session state), indexing options (None, Minimal, All full-text & fast search, auto-index), and container storage selection (internal database bytea blob vs external filesystem uploads).
  - **Calculation & Summary Tab**: Formula editor with quick-insert function buttons (`UPPER()`, `LOWER()`, `CONCAT()`, `SUM()`, `ROUND()`, `IF()`, `CURRENT_DATE`), calculation result type selector, and Summary aggregate operations (Sum, Average, Count, Minimum, Maximum, target column selector, and running total option).
  - Added "Options..." button with `Icons.tune` and row tap navigation in the Fields tab of `ManageDatabaseDialog` (`client/lib/features/schema_manager/manage_database_dialog.dart`).
- **Embedded Swagger UI & OpenAPI Specification in API Container (`file4base-app-api`)**:
  - Embedded Swagger UI and OpenAPI 3.0.3 specification directly inside the Go backend binary using `embed.FS` (`server/internal/api/swagger_handler.go`).
  - Available at `/swagger/` (interactive documentation), `/swagger/openapi.json` and `/openapi.json` (raw specification), and redirected from `/swagger` and `/docs`.
  - 100% self-contained and offline-ready with zero external CDN runtime dependencies.
- **Docker Service & Container Renaming (`file4base-app-api`)**:
  - Renamed backend container and image from `file4base-server` / `file4base-app-server` to `file4base-app-api` in `docker-compose.yml`.
  - Service renamed to `api` with dual network aliases (`api`, `server`) preserving backwards compatibility for internal container networking.
  - Updated `client/nginx.conf` reverse proxy to route `/api/`, `/swagger/`, `/docs`, and `/openapi.json` to `http://api:8080`.
- **Enhanced Column Options Persistence in Backend (`server/`)**:
  - Enhanced `Service.UpdateColumn` and `SchemaHandler.UpdateColumn` to support updating `default_value`, `calculation_formula`, and `validation_rules` in `sys_columns` using dynamic SQL for PostgreSQL and MariaDB.
  - Updated `ApiClient.updateColumn` in Flutter client to serialize and transmit field options.

### Fixed
- **Find Mode Search Execution and Navigation Transitions**:
  - Resolved issue where executing a search ("Perform Find") left the user trapped on the Find input form without transitioning to the found records.
  - Implemented proper operational mode transition from `OperationalMode.find` to `OperationalMode.browse` upon search execution, showing a found set notification badge and "Show All" action.
  - Handled zero-results scenario with an informative dialog allowing the user to either modify their criteria or return to showing all records.
  - Replaced muddy amber card backgrounds in Find Mode with clean, high-contrast Material 3 cards, banner headers, search icons, and field type indicators.
  - Implemented comprehensive formula & wildcard search syntax in backend `ExecuteFind` and `ParseFile4BaseFindCriteria`:
    - Wildcard `*` (`%`) and single character `@`/`?` (`_`).
    - Comparison operators: `>`, `<`, `>=`, `<=`.
    - Exact match `=` and strict exact match `==`.
    - Negation / exclusion: `!=` or `!`.
    - Range queries: `min...max`.
    - Today's date shorthand: `//`.
    - Empty field search (`=`) and non-empty field search (`*`).
    - Wrapped column references in `CAST(col AS TEXT)` to avoid PostgreSQL type operator mismatches (`numeric ~~* text`).

## [0.4.6] - 2026-09-24

### Added
- **API Best Practices Implementation in Go Backend (`server/`)**:
  - Implemented cloud-native health probes: `/healthz/liveness` (`/livez`), `/healthz/readiness` (`/readyz`), `/healthz/startup` (`/startupz`), and comprehensive IETF-draft diagnostic `/healthz`.
  - Implemented graceful shutdown lifecycle: failing readiness (HTTP 503) on SIGTERM/SIGINT, draining in-flight requests, and closing database pools cleanly.
  - Implemented OpenTelemetry and W3C TraceContext middleware (`server/internal/telemetry/middleware.go` and `trace.go`): automatic generation and propagation of `traceparent`, `tracestate`, `X-Trace-ID`, and structured JSON request logs.
  - Implemented RFC 9457 Problem Details (`application/problem+json`) error handling across all schema, data, security, and solution handlers (`server/internal/telemetry/problem.go`).
  - Added security headers middleware: `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `X-XSS-Protection: 1; mode=block`, `Strict-Transport-Security`.
  - Added Docker container healthchecks in `server/Dockerfile` and `docker-compose.yml` (`depends_on: condition: service_healthy`).
- **Web Client Observability & Error Handling (`client/`)**:
  - Implemented W3C `TraceContext` in Flutter Web client (`client/lib/core/api/api_client.dart`), generating and injecting `traceparent` and `X-Trace-ID` headers into all outgoing requests.
  - Implemented `ApiException` parsing RFC 9457 `application/problem+json` error responses (exposing `title`, `detail`, `status`, `type`, `instance`, and `traceId`).
  - Added cloud-native probe methods to `ApiClient`: `checkLiveness()`, `checkReadiness()`, `checkStartup()`.
  - Updated `client/nginx.conf` reverse proxy to forward W3C `traceparent` and `tracestate`, apply security headers, and proxy cloud-native probe endpoints.
- **API Best Practices & Observability Specification**:
  - Published comprehensive enterprise specification in [docs/specs/API_BEST_PRACTICES.md](docs/specs/API_BEST_PRACTICES.md).
  - Defined standards for API Semantic Versioning (SemVer 2.0.0), non-breaking evolution, and RFC 8594 deprecation/sunset headers.
  - Specified cloud-native three-probe health architecture (Liveness, Readiness, Startup) and IETF Draft `/healthz` diagnostics.
  - Defined OpenTelemetry (OTel v1.26+) distributed tracing conventions, W3C TraceContext (`traceparent`), RED metrics, and JSON structured log correlation.
  - Documented REST resource modeling, idempotency matrix, cursor/keyset pagination, and RFC 9457 (`application/problem+json`) error handling.

### Changed
- **Browse Mode Cleaner Record Form ("Ficha")**:
  - Removed the out-of-place 3-dots popup menu ("Rename Field...", "Delete Field...") and the "+ Add Field to Table..." button from Browse mode records, keeping schema modifications strictly in **Manage Database** (`File -> Manage -> Database... -> Fields` tab).
  - Browse mode is now focused entirely on data entry, validation, record navigation, and live auto-saving.
- **About File4Base Dialog Redesign**:
  - Completely redesigned `AboutFile4BaseDialog` in `client/lib/features/about/about_dialog.dart` from the retro 1990s beveled style to a modern Material 3 interface aligned with the application design system.
  - Added modern header with app badge, title, subtitle, and close button.
  - Added clean top `TabBar` with icons for *About*, *System Info*, and *Credits* tabs.
  - Modernized bottom action bar with responsive `About`, `Info`, `Credits` tab switch buttons and a primary `OK` button (`FilledButton.icon`).
  - Added structured key-value cards with icons for system runtime information and open-source technology badges.
  - Fixed responsive wrapping to eliminate any `RenderFlex` overflow across compact viewports and automated test runners.

## [0.4.5] - 2026-09-24

### Fixed
- **Web File Picker in Open Solution Dialog**:
  - Implemented `platformPickFile()` in `client/lib/core/services/solution_storage_web.dart` using standard HTML5 file chooser (`<input type="file">` and `FileReader`).
  - Replaced unsupported `FilePicker.pickFiles` call on Web which previously threw `UnimplementedError` when trying to browse and select `.f4p` / `.f4b` files from disk.
  - Seamless file picking across macOS Finder, Windows Explorer, and Linux file managers via browser file dialog.

## [0.4.4] - 2026-09-24

### Added
- **Dynamic Field & Record Management in Record Form ("Ficha")**:
  - Live auto-save on every field keystroke (800ms debounce), on focus lost (blur), on Enter key, and before record navigation.
  - Added "+ Add Field to Table..." action directly at the bottom of the record card in Browse mode, allowing instant addition of new columns without leaving the record view.
  - Added per-field action menu (3 dots) on every column in the card with "Rename Field..." and "Delete Field..." actions.
  - Added "Edit Field" (rename) and "Delete Field" (drop column) buttons to the Fields tab in `ManageDatabaseDialog`.
- **Backend Column Management API**:
  - Added `PUT /api/v1/schemas/tables/{id}/columns/{columnId}` to update column metadata (display names).
  - Added `DELETE /api/v1/schemas/tables/{id}/columns/{columnId}` to physically drop columns and unregister them from `sys_columns`.
- **Live State Synchronization**:
  - Fixed `_loadTables()` in client `main.dart` to correctly refresh the active `_selectedTable` instance and its columns whenever schema modifications occur.
  - Enhanced `DataBrowserWidget.didUpdateWidget` to detect column changes and immediately rebuild controllers and reload rows.
  - Added `await _saveCurrentRecord()` before creating a new record in `DataBrowserWidget` to ensure no in-flight edits are lost.

## [0.4.3] - 2026-09-24

### Added
- **Auto-Save Service (`AutoSaveService`)**:
  - New `client/lib/core/services/auto_save_service.dart` singleton that debounces structural
    solution saves every 3 seconds using a background timer.
  - Saves only layout structure, schemas, tables, and workspace config — **never** row data.
  - Exposes a `Stream<AutoSaveStatus>` (`idle`, `dirty`, `saving`, `saved`, `error`) consumed
    by a live indicator in the bottom status bar.
- **File Extension Migration `.f4b` → `.f4p`**:
  - New solutions default to the `.f4p` extension (File4Base Project).
  - All file pickers now accept both `.f4p` (new) and `.f4b` (legacy) for backward compatibility.
  - `SolutionStorageService` gains a new `saveSolutionFile()` method for single-file structure
    writes, separate from the dual-file export path.
- **Export Data Command (`File → Export Data...`)**:
  - New menu item that explicitly exports PostgreSQL row data to `.f4data` (MessagePack).
  - Row data is **never** part of auto-save; this is the only path to persist database rows locally.
- **Explicit Save Warning Dialog**:
  - Pressing `Cmd+S` now flushes the auto-save immediately and shows a two-panel dialog:
    - Green panel: confirms the `.f4p` file was written (layouts/schemas/config).
    - Amber panel: warns that database row data was **not** saved and offers a shortcut to
      `Export Data Now`.
- **Auto-Save Status Indicator in Bottom Status Bar**:
  - Real-time chip showing `Auto-save`, `Unsaved`, `Saving...`, `Saved`, or `Save Error`
    with colour-coded icon and a tooltip showing the last save timestamp.
- **`onAutoSaveDirty` callback in `LayoutDesignerWidget`**:
  - Fires after each successful layout server-save, notifying `AutoSaveService` to mark the
    solution dirty and schedule a debounced file write.

### Changed
- `NewDatabaseDialog` no longer auto-exports an empty `.f4data` on creation; only the `.f4p`
  structural file is written. Users must use `File → Export Data...` to capture row data.
- `_handleSaveAs()` in `main.dart` updated to produce `.f4p` files, update `AutoSaveService`
  directory/base-name, and display the improved save dialog.
- `SaveCopyDialog` labels updated from `.f4b` → `.f4p`.

## [0.4.2] - 2026-09-23

### Fixed
- **GitHub Actions Node.js 20 Deprecation Warning**:
  - Upgraded CI/CD workflow actions from `@v4` to `@v7`/`@v8` (`actions/checkout@v7`, `actions/upload-artifact@v7`, `actions/download-artifact@v8`).
  - All workflow actions now natively target `node24`, eliminating the runner deprecation warning.
- **Desktop Single-Compression Distribution**:
  - Resolved double-compression issue where downloading desktop artifacts from GitHub Actions resulted in nested archives (`.zip` containing `.tar.gz` or `.zip`).
  - Enabled `archive: false` on `actions/upload-artifact@v7`, allowing pre-packaged archives to be uploaded as direct single files without GitHub Actions adding a redundant outer `.zip` container.
  - Decompressing downloaded desktop artifacts now immediately extracts the application (`File4Base.app` on macOS, binary bundle on Linux, executable folder on Windows) in a single step.
  - Added native Apple Disk Image (`File4Base-macOS.dmg`) generation via `hdiutil` for seamless macOS drag-and-drop installation.

### Added
- **Automated GitHub Releases Publishing**:
  - Added `publish-release` workflow job triggered upon version tags (`v*`).
  - Automatically compiles desktop bundles across macOS, Linux, and Windows and attaches release assets directly to GitHub Releases via `gh release create`.

### Added
- **Unified Open Solution / Database Dialog (`OpenSolutionDialog`)**:
  - Implemented comprehensive `File -> Open...` (`⌘O`) modal supporting dual opening pathways:
    1. **Server Databases (API)**: Queries `GET /api/v1/databases`, lists all databases created on the server API, displays the active database badge, and prompts for username and password to authenticate.
    2. **Local Solution File (.f4b)**: Allows selecting a `.f4b` file from the computer, inspects package metadata (solution name, target database), and prompts for database username and password before restoring and opening.

### Fixed
- **Decoupled Database Name vs User Credentials**:
  - Resolved conflation between PostgreSQL physical database names and application user accounts:
    - Database Name (`file4base_dev`, `my_solution_db`, etc.) defines the database catalog.
    - Username (`admin`, `file4base`, or custom) defines the user identity in `sys_users`.
  - In `NewDatabaseDialog`, database name changes no longer overwrite username or password fields.
  - In `DatabaseLoginDialog`, selecting a database from the dropdown preserves user credentials.
  - In backend Go server, default administrative accounts (`admin`, `file4base`, `file4base_dev`) are provisioned with role `owner` upon catalog initialization.

## [0.4.0] - 2026-09-23

### Added
- **Centralized Layout Persistence in Intermediate Server**:
  - Centralized layout definitions directly in PostgreSQL `sys_layouts` table via backend Go service and REST API (`/api/v1/layouts`).
  - Enables multiple connected web client instances (`file4base-web`) to share, synchronize, and edit layouts collaboratively without local desynchronization.
- **Default Administrative `owner` User**:
  - Auto-provisioned default administrative account `owner` (password `owner`, role `owner`) upon database catalog initialization in `EnsureSystemTables`.
  - Passwords securely hashed with `bcrypt` (DefaultCost).
- **Manage -> Security (`Manage -> Security...`)**:
  - Full administrative dialog under `File -> Manage -> Security...` allowing owners and admins to manage user accounts and assign roles (`owner`, `admin`, `user`).
  - Granular per-layout permission matrix (`read_write`, `read_only`, `none`) stored in `sys_user_permissions`.
  - Automatic restriction of layout designer mode (`OperationalMode.layout`) for users with `read_only` or `none` layout access.
- **Startup Database Selection & Authentication Flow**:
  - Initial cold start dialog (`DatabaseLoginDialog`) that queries `/api/v1/databases`, allows selecting or creating PostgreSQL databases, and authenticates the user before entering the workspace.
  - Added user account and role chip to bottom status bar with one-click database/user switching.
  - Added `File -> Switch Database / Login...` shortcut in canonical menu bar.

## [0.3.1] - 2026-09-23

### Added
- **Hard Drive Destination Folder Selection**:
  - Implemented `SolutionStorageService.pickDirectory()` using modern Web **File System Access API** (`window.showDirectoryPicker()`) on Chromium/Chrome and native OS folder picker (`FilePicker.getDirectoryPath()`) on Desktop (macOS, Windows, Linux).
  - Enables users to browse their disk and select the exact folder where solution files are saved.
- **Dual-File Synchronized Persistence (`.f4b` + `.f4data`)**:
  - Unified saving across `New Database...`, `Save` (`⌘S`), `Save As...` (`⇧⌘S`), and `Save a Copy As...` to automatically record both companion files in the selected folder:
    1. `<name>.f4b`: UI definitions, layouts, schemas, table occurrences, and secure encoded credentials.
    2. `<name>.f4data`: Active PostgreSQL table rows and database records.
- **Encrypted / Obfuscated Connection Credentials**:
  - Implemented reversible symmetric key-stream obfuscation/encryption (`enc:<base64>`) for database credentials (`user` and `password`).
  - Completely prevents plaintext credential leaks inside `.f4b` MessagePack bundles.
  - Implemented identically across Dart client (`DatabaseConnectionConfig`) and Go server (`solution_service.go`) with automated test coverage.
- **Desktop Application Distribution**:
  - Documented and unpacked native desktop applications for macOS (`File4Base.app`), Windows, and Linux in `dist/`.

## [0.3.0] - 2026-09-23

### Added
- **MessagePack Solution & Dual-File Architecture**:
  - Implemented binary MessagePack (`msgpack`) serialization for File4Base application files (`.f4b`), preserving layouts, schemas, table definitions, relationships, users, and database credentials.
  - Implemented secondary MessagePack database data file (`.f4data`) containing all physical table rows and records for database backup and restore.
- **Multi-Database PostgreSQL Architecture**:
  - Created `MultiDatabaseManager` in Go backend (`server/internal/dbal/multi_db.go`) supporting dynamic connection pools, runtime database creation, and seamless database switching.
  - Added REST API endpoints at `/api/v1/databases` (list, create, switch) and `/api/v1/solutions` (export/import solution, export/import data).
- **File Menu Save & Database Lifecycle**:
  - Wired full persistence workflows in `File4BaseMenuBar`: `New Database...` (`⌘N`), `Open...` (`⌘O`), `Save` (`⌘S`), `Save As...` (`⌘⇧S`), and `Save a Copy As...`.
  - Added `NewDatabaseDialog` wizard configuring solution file name, PostgreSQL database name, and credentials with automatic database creation.
  - Added `SaveCopyDialog` enabling dual-file export: `.f4b` (solution clone) vs `.f4data` (database records dump).
  - Added real-time active solution file and active PostgreSQL database chips to the status bar.
- **Layout Persistence Foreign Key Fix**:
  - Enhanced `CreateLayout` in `server/internal/schema/layout_service.go` to flexibly resolve table occurrence IDs when passed base table IDs or table names, eliminating foreign key violation errors on save.
- **Canonical Top Menu Bar (`File4BaseMenuBar`)**:
  - Implemented the complete 10-menu system (`File`, `Edit`, `View`, `Insert`, `Format`, `Records` / `Requests`, `Scripts`, `Tools`, `Window`, `Help`) pinned to the top-left margin across Desktop and WebDirect.
  - Dynamically swaps between `Records` and `Requests` menus based on operational mode (`Browse` vs `Find`).
  - Added unit test suite in `client/test/menu_bar_test.dart`.
- **Compiled Multiplatform Desktop Packages**:
  - Downloaded compiled release packages for macOS (`File4Base-macOS-universal.zip`), Windows (`File4Base-Windows-x64.zip`), and Linux (`File4Base-Linux-x64.tar.gz`) into the root `dist/` directory.

### Fixed
- **Menu Bar Alignment**:
  - Pinned top `MenuBar` to the left screen margin (`CrossAxisAlignment.stretch` and `Alignment.centerLeft`) instead of centering.
- **WebDirect Tab Icon & Branding**:
  - Replaced default Flutter browser tab favicon and application metadata in `client/web/` (`favicon.ico`, `favicon.png`, `manifest.json`, `index.html`) with official File4Base branding icons.
  - Added cache-busting queries (`?v=2`) and multi-resolution ICO/PNG declarations to bypass aggressive browser favicon caching.
- **API Connection & Browser CORS**:
  - Added `corsMiddleware` in Go server (`server/cmd/server/main.go`) handling `OPTIONS` preflight and `Access-Control-Allow-Origin: *`.
  - Added Nginx reverse proxy configuration (`client/nginx.conf`) proxying `/api/` and `/healthz` directly to the `server:8080` backend container.
  - Enabled intelligent browser origin fallback in `client/lib/main.dart` (`ServerUrlNotifier`) preventing cross-origin errors in WebDirect mode.

---

## [0.2.0] - 2026-09-23

### Added
- **Phase 4: Dynamic CRUD & Find Mode**:
  - Generic CRUD backend service (`server/internal/data/service.go`) executing dynamic SQL across DBAL engines.
  - File4Base Find Mode operator translator (`ParseFile4BaseFindCriteria` and `ExecuteFind`) supporting wildcards (`*`), ranges (`...`), exact equality (`=`), negations (`!`), and inequalities (`>`, `<`).
  - REST endpoints at `/api/v1/data/{table}` (GET, POST, PUT, DELETE, and POST `/find`).
  - Frontend record browser and stepper (`DataBrowserWidget`) with record creation, editing, deletion, and Find Mode request input.
- **Phase 5: Dynamic Layout Engine & 4 Execution Modes**:
  - Layout persistence service (`server/internal/schema/layout_service.go`) and REST API at `/api/v1/schemas/layouts`.
  - Deterministic JSON layout models adhering to `docs/specs/layout_schema.json`.
  - Interactive WYSIWYG Layout Designer (`LayoutDesignerWidget`) for **Layout Mode** (`⌘L`): 8px grid snapping, coordinate rulers, structural parts (Header, Body, Footer), draggable element placement, and real-time Object Inspector.
  - Page print preview simulator (`LayoutPreviewWidget`) for **Preview Mode** (`⌘U`): page margin boundaries, tabular data layout, totals, and pagination footer.
  - Active Table selector dropdown directly in the main top navigation bar.
- **Multiplatform Desktop & Branding Integration**:
  - Integrated official user-provided icons `file4base-dark.ico` and `file4base-light.ico`.
  - Extracted high-resolution PNG variants (`64px` through `1024px`) in `assets/branding/` and `client/assets/branding/`.
  - Configured native macOS desktop icons in `client/macos/Runner/Assets.xcassets/AppIcon.appiconset/`.
  - Configured native Windows desktop resources in `client/windows/runner/resources/app_icon.ico` and `Runner.rc`.
  - Configured WebDirect web favicon and PWA icons.
  - Added macOS network client sandbox entitlements (`com.apple.security.network.client`) to `Release.entitlements` and `DebugProfile.entitlements`.
  - Added configurable Server Host & Port Settings dialog (`ServerConnectionDialog`) allowing desktop clients to connect to custom host/ports with generic connectivity diagnostics.
  - Created desktop build and packaging script (`scripts/build_desktop.sh`) for macOS, Windows, and Linux.
  - Added GitHub Actions multiplatform desktop CI/CD workflow (`.github/workflows/desktop_release.yml`).
- **Governance & Version Management**:
  - Added `.agents/rules/versioning.md` establishing mandatory SemVer synchronization and living documentation rules for AI assistants and developers.
  - Added root `VERSION` file as the canonical single source of truth.
  - Added `docs/VERSIONING.md` describing release process, git workflow, and tagging.
  - Added `docs/api/API_REFERENCE.md` documenting all available REST API endpoints.

---

## [0.1.1] - 2026-09-23

### Added
- Automated Preflight Environment Checker (`client/lib/core/system/environment_checker.dart`) verifying Docker CLI, engine daemon status, and Apple Silicon / Intel architecture.
- Preflight warning dialog with one-click Docker Desktop launch or download links.
- WebDirect web container in `docker-compose.yml` serving client bundle on port `3000`.
- GNU General Public License v3.0 (`LICENSE`).

---

## [0.1.0] - 2026-09-22

### Added
- Clean Architecture repository structure (Decoupled Go backend + Flutter desktop client).
- Database Abstraction Layer (`internal/dbal`) supporting PostgreSQL 16 primary and MariaDB 11 profile.
- System catalog metadata tables (`sys_tables`, `sys_columns`, `sys_table_occurrences`, `sys_relationships`, `sys_layouts`).
- Dynamic DDL schema service (`server/internal/schema/service.go`) and REST API at `/api/v1/schemas/tables`.
- Manage Database visual dialog with Tables, Fields, and Relationship Graph canvas.
- Operational Mode state machine with 4 canonical File4Base modes (`Browse`, `Find`, `Layout`, `Preview`) and keyboard shortcuts (`⌘B`, `⌘F`, `⌘L`, `⌘U`).
