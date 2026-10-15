# Server Tools Administration Console, MFA (Google Authenticator) & Edit Menu Specification

## 1. Overview

This specification documents three core platform capabilities introduced in **File4Base**:
1. **Top-Level `Server Tools` Menu & Administration Console** (`ServerToolsDialog` & `/api/v1/server/*`): Unified operations console for managing Docker services, on-demand and scheduled backups, public domain & SSL/TLS certificates (ACME Let's Encrypt / ZeroSSL and custom PEM), live 60-minute API telemetry charts, connected client sessions, cron tasks, security audit logs, database maintenance (`VACUUM ANALYZE` / `REINDEX`), and outbound SMTP/Webhook notifications.
2. **Multi-Factor Authentication (MFA / 2FA) with Google Authenticator (RFC 6238 TOTP)** (`server/internal/schema/mfa.go`, `/api/v1/security/users/{id}/mfa/*`, `ManageSecurityDialog`, `DatabaseLoginDialog`): Per-account two-factor authentication provisioned via QR code (`otpauth://totp/`) and enforced at sign-in.
3. **Cross-Mode `Edit` Menu Operations (`Undo`, `Redo`, `Cut`, `Copy`, `Paste`, `Clear`, `Select All`)** (`EditMenuFocusTracker`, `LayoutDesignerWidgetState`): Seamless text editing and layout object manipulation from the top menu bar across Browse, Find, and Layout modes.

---

## 2. Top-Level `Server Tools` Menu & Console (`ServerToolsDialog`)

**Access.** Backups & Snapshots and Database Maintenance are *database tools*: the `owner`s and `admin`s of a database use them on their own database only. Every other module acts on the whole server and is reserved to **server administrators**: the owners of the database named by `FILE4BASE_ADMIN_DATABASE` (no other account, whatever its database role). Without that variable the server-wide modules are disabled. `GET /api/v1/server/access` tells the client which modules to show; the server enforces the same rules on every route.

The `Server Tools` menu is positioned immediately after `Tools` in `File4BaseMenuBar` (`File` · `Edit` · `View` · `Insert` · `Format` · `Records/Requests` · `Scripts` · `Tools` · **`Server Tools`** · `Window` · `Help`). Selecting any item opens `ServerToolsDialog` focused on the corresponding module:

| # | Menu Item | Section Key | REST Endpoint(s) | Capabilities |
|---|---|---|---|---|
| 1 | **Containers & Services...** | `containers` | `GET /api/v1/server/containers`<br>`POST /api/v1/server/containers/{name}/restart`<br>`GET /api/v1/server/containers/{name}/logs` | Real-time status, version, uptime, memory/goroutines, log tail viewer, and graceful service reload for `file4base-api`, `file4base-web`, and `file4base-postgres`. |
| 2 | **Backups & Snapshots...** | `backups` | `GET /api/v1/server/backups`<br>`POST /api/v1/server/backups`<br>`GET /api/v1/server/backups/{id}/download`<br>`POST /api/v1/server/backups/{id}/restore`<br>`DELETE /api/v1/server/backups/{id}` | Full server-side snapshots of schema, layouts, scripts, value lists, and table records, with 1-click download, point-in-time restore, and deletion. |
| 3 | **Domain & SSL/TLS Certificates (ACME)...** | `domain_tls` | `GET /api/v1/server/tls`<br>`PUT /api/v1/server/tls`<br>`POST /api/v1/server/tls/acme/issue`<br>`POST /api/v1/server/tls/custom` | Public domain configuration, automatic ACME (Let's Encrypt / ZeroSSL) certificate issuance (`http-01` / `tls-alpn-01`), custom PEM certificate chain + private key upload, and X.509 issuer/subject/expiry inspection. |
| 4 | **API Telemetry & Live Metrics...** | `telemetry` | `GET /api/v1/server/telemetry` | Rolling 60-minute ring buffer (`server/internal/telemetry/collector.go`) feeding live **Requests Per Minute (RPM)** bar charts, **Average vs P95 Latency (ms)** line charts, HTTP `2xx`/`4xx`/`5xx` counters, Go runtime memory/goroutines, DB ping latency, and top endpoints table. |
| 5 | **Connected Clients & Sessions...** | `sessions` | `GET /api/v1/server/sessions`<br>`DELETE /api/v1/server/sessions/{id}` | Live list of active authenticated bearer sessions (`username`, `role`, `database`, `created_at`, `expires_at`) with 1-click session revocation (`Force Disconnect`). |
| 6 | **Scheduled Tasks & Cron Scripts...** | `schedules` | `GET /api/v1/server/schedules`<br>`POST /api/v1/server/schedules`<br>`POST /api/v1/server/schedules/{id}/run`<br>`DELETE /api/v1/server/schedules/{id}` | Cron job scheduler (`0 2 * * *`, `*/15 * * * *`) for automated backups and server-side script execution with `Run Now` trigger. |
| 7 | **Audit & Security Logs...** | `audit_logs` | `GET /api/v1/server/audit-logs`<br>`POST /api/v1/server/audit-logs/clear` | Filterable security and administrative event log (`auth`, `mfa`, `backup`, `tls`, `container`, `maintenance`, `schedule`). |
| 8 | **Database Maintenance & Optimizer...** | `maintenance` | `GET /api/v1/server/maintenance`<br>`POST /api/v1/server/maintenance/run` | Live per-table column and row counts with 1-click `VACUUM ANALYZE`, `ANALYZE`, and `REINDEX DATABASE` execution. |
| 9 | **Server Notifications, SMTP & Webhooks...** | `notifications` | `GET /api/v1/server/notifications`<br>`PUT /api/v1/server/notifications`<br>`POST /api/v1/server/notifications/test` | Outbound SMTP email and Slack/Discord/Teams/Generic JSON webhook alerting with threshold triggers and test dispatch. |

---

## 3. Multi-Factor Authentication (MFA / Google Authenticator)

### 3.1 Cryptographic Standard
- **Algorithm**: RFC 6238 Time-Based One-Time Password (TOTP) using `HMAC-SHA1`, 6 digits, 30-second time step (`±1` step clock-skew tolerance), and 160-bit Base32-encoded secrets (`server/internal/schema/mfa.go`).
- **Catalog Schema**: `sys_users` includes `mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE` and `mfa_secret TEXT NOT NULL DEFAULT ''`, migrated automatically by `EnsureSystemTables`.

### 3.2 Provisioning & Sign-In Flow
1. **Setup (`ManageSecurityDialog`)**: In `File > Manage > Security...` (`Accounts` tab), clicking the **MFA (Google Authenticator)** icon (`Icons.phonelink_lock` / `Icons.verified_user`) in the **`OPTIONS`** column calls `POST /api/v1/security/users/{id}/mfa/setup` and renders the `otpauth://totp/` QR code offline via `package:qr` (`_QrCodePainter`) alongside the selectable Base32 secret.
2. **Verification & Activation**: Entering a valid 6-digit code from Google Authenticator calls `POST /api/v1/security/users/{id}/mfa/enable`, setting `mfa_enabled = true` and displaying the green `2FA` badge next to the account.
3. **Sign-In Enforcement (`DatabaseLoginDialog`)**: When `mfa_enabled` is `true`, `POST /api/v1/auth/login` without a `totp_code` responds with `401 Unauthorized` and `{"mfa_required": true}`, prompting `DatabaseLoginDialog` to reveal the 6-digit Google Authenticator input field.

---

## 4. Cross-Mode `Edit` Menu (`Undo`, `Redo`, `Cut`, `Copy`, `Paste`, `Clear`, `Select All`)

- **Browse & Find Modes (`EditMenuFocusTracker`)**: Clicking the top menu bar normally shifts primary focus away from the active `TextField`. `EditMenuFocusTracker` listens to `FocusManager.instance`, tracks the most recently focused `EditableTextState`, and maintains per-controller `undoStack`/`redoStack` histories via an `Expando`. All 7 commands (`Undo`, `Redo`, `Cut`, `Copy`, `Paste`, `Clear`, `Select All`) act directly on the active field; if no text edit can be undone in Browse mode, `Undo` falls back to `DataBrowserWidgetState.revertRecord()`.
- **Layout Mode (`LayoutDesignerWidgetState`)**: When no inline text editor is active, `Undo`, `Redo`, `Cut`, `Copy`, `Paste`, `Clear`, and `Select All` operate on the layout canvas objects (`undo()`, `redo()`, `cutSelected()`, `copySelected()`, `pasteObjects()`, `clearSelected()`, `selectAllObjects()`).
