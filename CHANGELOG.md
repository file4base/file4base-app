# Changelog

All notable changes to the **File4Base** project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [0.4.1] - 2026-09-23

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
