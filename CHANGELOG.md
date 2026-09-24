# Changelog

All notable changes to the **File4Base** project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [0.4.7] - 2026-09-24

### Added
- **Field Options Modal ("Options for Field <name>")**:
  - Implemented `FieldOptionsDialog` in `client/lib/features/schema_manager/field_options_dialog.dart` faithfully matching the native macOS FileMaker Pro field options dialog.
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
  - Implemented comprehensive FileMaker formula & wildcard search syntax in backend `ExecuteFind` and `ParseFile4BaseFindCriteria`:
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
