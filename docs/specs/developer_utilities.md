# Developer Utilities (`#53`)

**Tools > Developer Utilities...** (`#53`) provides an assistant for preparing, hardening, encrypting, and packaging a File4Base solution before delivery or production deployment.

## 1. Capabilities

### 1.1 Rename Solution & Rewrite Internal References
- Endpoint: `POST /api/v1/solutions/rename` (requires `owner` role and an unlocked design).
- Updates the logical `solution_name` in `sys_solution_meta` and the API's SQLite control store (`sys_projects`), and rewrites layout definitions and script step parameters that reference the previous solution name.

### 1.2 Kiosk Mode & AES-256-GCM Solution Encryption
- Endpoint: `PUT /api/v1/solutions/options` (persisted in `file_options` inside `sys_solution_meta` and the API SQLite control store, and carried inside `.f4p` bundles).
- **Enable Kiosk Mode (`kiosk_mode: true`)**: Opens the solution in full-screen presentation mode for non-admin users, hiding the top application menu bar and status toolbars so end users interact exclusively through the solution's layouts and custom buttons.
- **Require AES-256-GCM Encryption (`encryption_enabled: true`, `cipher: "aes-256-gcm"`)**: Enforces authenticated `F4BE\x01` AES-256-GCM envelope encryption for solution bundles and client-to-API payload transfers (`X-File4Base-Encrypted: aes-256-gcm`), optionally recording an encryption key hint (`encryption_key_hint`).

### 1.3 Deploy Clean Production Clone (Zero Records)
- Endpoint: `POST /api/v1/solutions/clone` (requires `owner` role).
- Creates a new physical database on the server (`target_database`), provisions the owner account, and imports the complete solution structure — tables, fields, table occurrences, relationships, layouts, scripts, value lists, custom menu sets, accounts, role privileges, saved finds, and external data source definitions — with **zero user records**.
- Supports locking the new clone's design immediately (`lock_design: true`) in a single atomic operation while leaving the development database unlocked.

### 1.4 Remove Admin Design Access Permanently (`design_locked`)
- Endpoint: `POST /api/v1/solutions/lock-design` with `{"confirm": true}` (requires `owner` role).
- Permanently and irreversibly sets `design_locked = true` in `sys_solution_meta`.
- Enforced by the `RequireDesignUnlocked` middleware across all schema, layout, script, value list, custom menu set, external data source, and solution import/rename routes (`403 Forbidden`), protecting intellectual property while keeping record CRUD, searches, exports, and user authentication active.
