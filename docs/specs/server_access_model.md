# Server access model: accounts, roles, grants

Status: **design, for discussion** (refs #185, #189, #190, #200, #208). Nothing
here is implemented yet unless the text says so. It replaces the
`Role + AllDatabases + Databases[]` shape of the server accounts in #189/#190
before that shape is released (no released version lets a server account sign
in: #187 landed after 0.22.0).

Terms: a **server account** lives in the control store and can reach several
databases. A **database account** (file account) is a `sys_users` row of one
database, managed in its Manage Security as today. *Database* and *solution*
are the same thing here; the API uses the database name.

## 1. Principles

1. **The control store decides.** Every request of a server-account session is
   checked against the control store. The `sys_users` row that stands for a
   server account in a database (the *mirror row*) is an identity only. No code
   reads it for an access decision, and it holds no role or super-owner value
   of its own (section 2.6).
2. **Server accounts and database accounts are separate**, as Admin Console
   accounts and file accounts are in FileMaker Server. The server-account
   sign-in path applies only where the account reaches the database; anywhere
   else the name is a database account name like any other. A name used by both
   in the same database is never resolved silently: a visible warning when the
   account or grant is created and at FileMaker import, and `409` at sign-in.
3. **Structure now, effect only when enforced.** The grant structure and its
   read paths ship in the API now. A write that sets something the server does
   not enforce yet answers `501`, and `API_REFERENCE.md` marks the field
   "reserved, no effect yet". The API never accepts a restriction it does not
   apply.
4. **Deny by default.** A new account has no roles and no grants. A missing
   grant, a missing field and an unknown value all mean no access. A failed
   control-store read fails closed (`503`), never falls back to another path.
5. **Least privilege.** Server roles give no data access; data grants give no
   server administration. Delegated administrators can only hand out what they
   hold (section 2.2).

## 2. Data model

### 2.1 Server account

| Field | Notes |
|---|---|
| `id` | Random 128-bit id, created with the account, immutable, never reused. All references (grants, mirror rows, sessions, audit) use it. |
| `name` | Unique ignoring case, no `@` or whitespace, at most 64 bytes (the `sys_users.username` limit). Renaming keeps the id. Directory accounts will be `name@domain`. |
| `active` | Disabled accounts fail sign-in exactly like a wrong password. |
| `auth_method` | `password` (bcrypt, as today). `directory` and `oauth` are reserved. |
| `mfa` | TOTP, enforced at every interactive sign-in path once enabled. Reserved in phase A (section 3.5). |
| `roles` | Set of server roles (2.2). |
| `revision` | Integer, incremented by every security change to the account (2.5). |

### 2.2 Server roles

Independent flags, close to FileMaker Server's administrator roles. None of them
gives access to records.

| Role | Covers | Today's equivalent |
|---|---|---|
| `administer_server` | Everything below, plus TLS, configuration, server roles, disconnecting sessions. | super-owner |
| `manage_accounts` | Server accounts and grants, bounded (below). | none |
| `manage_solutions` | Create, open/close, verify, rename, delete databases; maintenance. | owner of the database, for some actions |
| `manage_backups` | Schedules, retention, run, restore over a database. Downloading an archive also needs `full` on that database (an archive holds every table). | backups module |
| `configure_access_paths` | Connector switches (client, Data API, OData), IP allow-list. | none |
| `view_monitoring` | Dashboard, sessions list, telemetry, logs, audit trail (read only). | sessions/telemetry/audit modules |

Rules:

- **Bounded delegation.** A `manage_accounts` holder without `administer_server`
  may only create or change accounts that hold no server role, and cannot
  change their own grants or roles. Every write (single, replace, batch,
  delete) is checked in its transaction by comparing effective access before
  and after: it is refused (`403`, `delegation_exceeded`) if any account would
  end up, on any database, with more than the delegate holds there. Deleting a
  named grant counts (it falls back to `*`, which can be wider), and so does
  `*` itself, which also covers future databases: only a delegate whose own
  `*` is at least as wide may write one. Password, MFA and active resets are
  allowed only on accounts whose effective access is within the delegate's on
  every database and for `*`; otherwise a reset is a takeover of a wider
  account (the #209 pattern).
- Once one exists, the last active `administer_server` account cannot be
  disabled, demoted or deleted (today's last-super-owner guard).
- A database created by a `manage_solutions` holder gives its creator a `full`
  grant on it, nothing more.

### 2.3 Grant

A grant gives one principal access to one database, or to all of them (`*`).

| Field | Values | Default |
|---|---|---|
| `principal` | `{type: account|group, id}` | required |
| `database` | a database name, or `*` | required |
| `level` | `none`, `read_only`, `read_write`, `full` | `none` |
| `paths.client` | bool: File4Base desktop and web client (`/api/v1`) | `false` |
| `paths.data_api` | `off`, `ro`, `rw` (`/fmi/data`, #208) | `off` |
| `paths.odata` | `off`, `ro` | `off` |
| `export` | bool: bulk export (today's `bulk_export`) | `false` |
| `import` | bool: bulk import (today's `bulk_import`) | `false` |
| `run_scripts` | bool: server-side scripts | `false` |
| `design` | bool: schema, layouts, scripts, value lists, menus | `false` |
| `tables` | optional overrides, `{table_id: {access, create, edit, delete}}` | none |
| `layouts` | optional overrides, `{layout_id: access}` | none |

Levels, mapped to what the server already enforces:

| Level | Meaning | Session role it maps to |
|---|---|---|
| `none` | No access. Only useful to override `*` downwards. | no session |
| `read_only` | Read records. | `user`, every table read-only |
| `read_write` | Read, create, edit, delete records, within table overrides. | `user` (`admin` when `design`) |
| `full` | Everything in the database, like a file's full-access account: design, export, import, its database accounts and privilege sets, data sources, clone, lock design. Implies `design`, `export`, `import`; overrides are ignored. | `owner` |

`full` does not include dropping the database: `full` can create a database
owner, so once a server administrator exists (5) a database owner's
credentials no longer suffice, and a drop needs `manage_solutions` or
`administer_server`. Whole-database operations (solution data export and
import, solution export, backup download) need `full`, since they cover every
table and would bypass table overrides.

Validation (`422`):

- `design` requires `read_write` or `full`. Today's admin-guarded routes mix
  design with data writes (truncate, import), so design with `read_only` cannot
  be honoured.
- Overrides only on a grant for a named database (ids are per database), and
  never above the grant's `level`. A later change of `level` caps them at
  evaluation; the read API flags them `capped`.
- Paths do not raise the level: the Data API with `rw` on a `read_only` grant
  is read-only.

Overrides are keyed by the catalog id of the table (`sys_tables.id`) or layout
(`sys_layouts.id`), not by name: a table dropped and re-created under the same
name does not inherit the old rule. Overrides naming an id that no longer exists
are ignored and reported `stale`.

### 2.4 Resolution

For one principal and one database:

1. The grant for that database, if any, **replaces** the `*` grant as a whole.
   There is no field-by-field merge: what an administrator set explicitly for a
   database stays as set when the default changes. The UI pre-fills a new
   override from the default and marks rows that differ.
2. No grant for the database and no `*` grant: `none`.
3. Inside the grant, the most specific rule wins: layout override, then table
   override, then the grant `level`. This is today's
   `TableAccess`/`LayoutAccess` logic with one change: the fallback is the
   grant's level, not `read_write`.

For an account, its own grant and those of its groups are combined by
**union of what each grant permits, evaluated per request**: each grant is
resolved alone for the path, object and operation asked (level, then
overrides, then the path's ceiling, for example `data_api = ro`), and the
request is allowed if any grant allows it. Fields are never merged across
grants: `read_only + data_api rw` from one group and `read_write + data_api off`
from another give no Data API write. There is no explicit deny in this version.

Why union rather than FileMaker's first matching account in the account
order: it is order-free (no hidden ordering to keep right on 36 × 50 cells)
and monotonic (adding a grant never removes access, removing one never adds
any), so "who can reach X" is a plain query. Deny rules make every answer
depend on all other rules; they can be added later as a separate field without
changing existing grants. The cost: a group cannot be narrowed for one member;
give that person account grants and keep them out of the group.

### 2.5 Revision and session stamp

The stamp of a server-account session is
`H(account_id, account.revision, sorted(group_id:group.revision))`, taken from
the same read that verified the password (no sign-in race).

- `account.revision` increments on password, active, MFA, roles, rename, any
  grant of the account, and local group membership changes.
- `group.revision` increments on any grant of the group.
- Revisions never go back, so disabling then re-enabling an account, or
  removing then re-adding a database, never revives an old token (#190, 3).
- Deleting an account deletes its grants in the same transaction; a re-created
  account with the same name has a new id.
- Effective access may be cached in the session; it is valid only while the
  stamp matches. Each request therefore costs one indexed read of the
  account's `active` and `revision` (and its groups' revisions).
- A grant change signs the account out of every database. Grant changes are
  rare, and this keeps the rule easy to verify.

### 2.6 Database lifecycle and the mirror row

- **Instance id.** Each registered database gets a random, immutable instance
  id in the control store (`projects`). Named grants and sessions reference
  that id; the API still addresses databases by name and resolves it. A
  database created again under an old name gets a new id, so grants left
  behind by an interrupted drop authorise nothing (today the handler drops the
  database first and ignores the `DeleteProject` error).
- **Drop**: grants for the instance and their overrides are deleted, the
  revisions of the affected principals are incremented, and the database's
  sessions are revoked (#190, 3).
- **Rename**: `POST /solutions/rename` changes the logical name only; the
  database name and instance id, hence the grants, are unchanged.
- **Clone / restore under a new name**: a new instance, no grants copied.
- **`*`** covers databases created later, including by public creation. This is
  intended for administrators and must be visible on the account page.

The mirror row exists because per-database objects need an owner id (saved
finds, record ownership, and later PostgreSQL roles for RLS):

| Column | Mirror row value |
|---|---|
| `id` | `srv-` + the account id (random, same in every database, never derived from the name) |
| `server_account_id` | the account id (new column, `NULL` for database accounts) |
| `username` | the account name, refreshed at sign-in |
| `password_hash` | a non-bcrypt marker; never accepts a password |
| `role`, `super_owner` | neutral constants (`user`, `FALSE`), never read |

`sys_users` is shared with database accounts, so the columns stay; mirror rows
just never use them. Every per-database path refuses mirror rows:
`schema.Authenticate`, `/auth/link`, the credential check before deleting a
database, and `/security/users/{id}` password, role, active, MFA, super-owner,
permissions and delete (`409`, code `server_account_managed`). Server-account
overrides live in the control store, so a database owner cannot widen a server
account's access by editing that database's permission tables.

## 3. Enforcement

### 3.1 Sign-in, client path (`POST /api/v1/auth/login`)

1. Look the name up in the control store. A read error answers `503`.
2. The **server path** is taken only if the name is a server account **and** its
   effective level on the requested database is not `none`. Otherwise the
   database-account path runs as today.
3. Password (and MFA once enforced) is checked against the server account. A
   wrong password answers `401`. The throttle key is name + client IP, server
   wide, not per database.
4. Correct password but `paths.client` false: `403`, code `access_path_denied`
   (only revealed to someone who has the password).
5. A database account of the same name in that database: `409`,
   `name_collision`. The administrator renames one of them.
6. The mirror row is created or refreshed, and the session is bound to it with
   kind `client`, the database, the account id and the stamp.

**Console sign-in** (`POST /api/v1/server/auth/login`, #200): a server
account holding at least one server role signs in without a database and
without any grant. The session has kind `console`, no database and no mirror
row, and is accepted only on `/api/v1/server/*`. This is how an
administrator with no data grant, or the first one on an empty server, works;
the cutover in section 5 requires that this path exists and that the new
administrator has used it once.

### 3.2 Every request

Middleware: the session's stamp against the control store (2.5); then the
route's check against the effective grant for the session's database:

| Check | Source for a server-account session |
|---|---|
| Role guards (`RequireAdmin`, `RequireOwner`) | session role derived from the grant (2.3 table) |
| Record read/write, per table and layout | grant level and overrides (2.4). The `IsAdmin()` shortcuts that skip these checks today (`DataHandler.authorizeAction`, `UpdateLayout`) apply to `full` only: a `design` grant still needs table `delete` to truncate, and changing a layout needs `design`, whatever the layout override says. |
| `RequireCapability(bulk_export|bulk_import)` | grant `export` / `import`, not `sys_privileges` |
| Design routes | `design` or `full` |
| Database accounts, privilege sets, data sources | `full` only, even where the route says `RequireAdmin` |
| Server Tools | the server roles (2.2); `administer_server` stands in for `IsSuperOwner` |

A session's kind is fixed at sign-in, and a route group accepts only its own
kind: a `client` token is refused on `/fmi/data`, a Data API token on
`/api/v1`. This is the binding at sign-in that
[security_model.md](security_model.md#why-there-is-no-privilege-per-access-method)
asks for. It is not a per-client switch: desktop and web client remain one
path, since the server cannot tell them apart (#39).

### 3.3 Data API sessions (#208)

- `POST /fmi/data/{version}/databases/{database}/sessions` with Basic auth.
  Same name resolution as 3.1. The grant must have a level other than `none` and
  `paths.data_api` other than `off`; otherwise the FileMaker-style error for a
  missing privilege, after the password check.
- The token is bound to that one database and to kind `data_api`. It carries no
  `/api/v1` rights and cannot be linked to another database. The 15-minute idle
  expiry and per-session globals from #208 apply.
- Effective record access is the grant's level, then table and layout
  overrides, then the `data_api` ceiling last, so an override cannot reopen
  writes on an `ro` grant. A layout with access `none` is not reachable through
  the API (the layout is the Data API contract). The ceiling also bounds what a
  script run by the request may write.
- `script`, `script.prerequest`, `script.presort` and the execute-script route
  need `run_scripts`. Without it, or before the runner exists, the request fails
  with an error; it never runs without the script.
- Multi-file credentials (#208 P9) open one bound session per database, each
  checked against its own grant.

### 3.4 Other paths and actions

- **OData**: read-only, its own endpoint and kind, `paths.odata = ro`.
- **Export / import**: the existing bulk routes. `export` is not a data-loss
  control: anyone who can read records through the Data API can read them all.
- **Run scripts**: client-side scripts are ordinary API calls and are bounded
  by the grant already; `run_scripts` governs server-side execution only.
- **Design**: also bounded by the database's design lock, as today.
- **Linked sessions (#107)**: automatic and explicit linking
  (`session_links.go`) go through the same name resolution, collision, path
  and throttle checks as 3.1 in the target database. Until server accounts can
  link, a linked sign-in refuses a name that is a server account reaching the
  target database (`name_collision`), instead of authenticating the database
  row of that name.
- **Scheduled transfers and other unattended jobs** run without a request, so
  the middleware never sees them. Until a schedule stores the principal type
  and immutable id and resolves `active`, revision and grant from the control
  store at every run, schedules refuse server-account principals.

### 3.5 Enforced now vs reserved

Phase A is what #189/#190 should ship; the rest answers `501` on write.

| Item | Phase A | Later |
|---|---|---|
| Account `id`, `name`, `active`, `password`, `revision` | enforced | |
| Role `administer_server` | enforced | |
| Console sign-in (3.1), legacy cutover (5) | enforced | |
| Other server roles | reserved | with the console sections (#200) |
| Grants for accounts: `level`, `paths.client`, `export`, `import`, `design`, `*` | enforced | |
| Table and layout overrides | reserved | phase B |
| `paths.data_api` other than `off` | reserved | #208 P1 |
| `run_scripts: true` | reserved | #208 P10 |
| `paths.odata` other than `off` | reserved | #208 P13 |
| Group principals | reserved | with directory authentication |
| `auth_method` other than `password` | reserved | |
| MFA on server accounts | enrolment `501`; mirror enrolment `409` | phase B |

## 4. API

All under `/api/v1/server`, RFC 9457 problems as elsewhere. Paths use the
account id; `filter[name]=` finds an account by name. Writes need
`administer_server`, or `manage_accounts` within its bounds; so do reads of
grants. Every write is audited (`server_access`): actor, target, outcome, and
before/after values of an allow-list of fields (name, active, roles, grants).
Credential changes record only which credential changed (password, MFA);
passwords, hashes and MFA secrets are never logged or audited.

| Method and path | Purpose |
|---|---|
| `GET /accounts` | List. `page`, `page_size`, `q`, `filter[active]`, `filter[role]`, `filter[group]`, `sort`. |
| `POST /accounts` | Create: `{name, password, active, roles}`. `201` with `warnings`. |
| `GET`, `PATCH`, `DELETE /accounts/{id}` | One account; `PATCH` for `name`, `active`. |
| `PUT /accounts/{id}/password`, `PUT /accounts/{id}/roles` | As named. |
| `GET /accounts/{id}/grants` | The account's own grants (`*` first). `filter[database]`, `filter[differs_from_default]`, `filter[data_api]`, paginated. |
| `PUT /accounts/{id}/grants` | Replace the account's whole grant set atomically (the account page's Save). |
| `PUT`, `DELETE /accounts/{id}/grants/{database}` | One grant; `{database}` is a name or `*` (sent as `%2A`). Deleting a named grant falls back to `*`. |
| `GET`, `PUT /groups/{id}/grants…` | Same shape for groups. Reads answer an empty list; writes `501` until groups exist. |
| `GET /databases/{database}/grants` | Grants naming this database, plus the `*` grants that apply, marked `inherited`. |
| `GET /databases/{database}/access` | Who can reach X: effective access per account, with `filter[min_level]`, `filter[path]`. |
| `GET /accounts/{id}/access` | What can Y do: effective access per database. |
| `GET /access/effective?account={id}&database={db}` | One cell, with `sources`: the grant rows that produced it. |
| `GET /access/matrix` | The overview: databases as rows (paginated), accounts as columns (`accounts=` up to 100), same filters. |
| `POST /grants/batch` | Bulk edit: `{changes: [{principal, database, grant or null}]}`, all or nothing. |
| `GET /access-model` | Levels, paths, roles, and which fields are `enforced` or `reserved`, so clients do not hard-code phase A. |

Shapes:

- Lists: `{data, pagination: {page, page_size, total_count, total_pages}}`
  ([API_BEST_PRACTICES.md](API_BEST_PRACTICES.md) 5.3). Filtering and
  pagination run on the server.
- A grant is the table in 2.3, with `principal` and `database`. Responses add
  `inherited` (read endpoints only) and per-override `capped` / `stale`.
- Account and grant writes take `If-Match: "<revision>"`; a stale revision
  answers `412`, so two administrators editing the overview cannot overwrite
  each other.
- Warnings travel in successful responses:
  `warnings: [{code: "name_collision", database, detail}]`, at account creation,
  rename, grant creation, database account creation, and FileMaker import.
- `501` for reserved values:
  `{type: "https://file4base.org/errors/reserved", title: "Not Implemented", status: 501, code: "reserved", reserved_fields: ["paths.data_api"]}`.
  A reserved field sent at its default value is accepted, so a client can send
  back what it read. Nothing in the request is stored.
- `403` `delegation_exceeded` names the field and database a delegated
  administrator does not hold. `409` `last_administrator`, `name_collision`
  (sign-in only), `server_account_managed`. `422` uses `invalid_params`.
- `GET /api/v1/auth/session` adds the session's effective access, as a
  courtesy for the interface; the server checks every call.

## 5. Migration

**From the unreleased #187/#189/#190 store** (in place, at startup):

| Current | New |
|---|---|
| `name_key` primary key | new random `id`; `name_key` stays a unique index |
| `super_owner` | role `administer_server` |
| `all_databases` | one `*` grant |
| `databases[]` | one grant per database |
| role `owner` | `level: full` + `client` |
| role `admin` | `read_write` + `design` + `client` + `export` + `import` |
| role `user` | `read_write` + `client` + `export` + `import` |
| mirror rows `srv-<hash of name>` | re-keyed to `srv-<id>`, `role`/`super_owner` neutralised, owned objects moved |

Export and import are granted because database roles hold every capability by
default today; a database that withdrew them for a role does not restrict a
server account any more, and the migration log says so.

**Legacy server administration.** Three sources grant server administration today: the stored
`sys_users.super_owner` flag, the `FILE4BASE_SUPER_OWNER` seed, and owners of
`FILE4BASE_ADMIN_DATABASE`, at direct and at linked sign-in.

1. While the cutover has not happened, all three keep working as today, and
   the first-run flow (#200) offers to create an `administer_server` account.
2. **Cutover** is a durable flag in the control store, set once an active
   `administer_server` account exists and has signed in to the console (3.1).
   From then on none of the three sources grants anything, at any sign-in path,
   and every session holding legacy super-owner authority is revoked in the
   same step (the file-account stamp does not change, so the cached
   `SuperOwner` would otherwise survive). The cutover is audited; the server
   logs a warning at each start while either variable is still set;
   `PUT /security/users/{id}/super-owner` answers `409`; dropping a database
   needs a server role (2.3).
3. The variables and the per-database flag are removed in a later minor
   release.

**From FileMaker** (migration of a file):

| FileMaker | File4Base |
|---|---|
| File account with a privilege set | database account, unchanged |
| Full Access set | `full` |
| Data entry / read-only style sets | `read_write` / `read_only` with table and layout overrides |
| Record, layout access per table and layout | table and layout overrides |
| Value list access | follows layout access (not modelled separately) |
| Script access | `run_scripts` (server side) |
| Extended privileges `fmapp`, `fmwebdirect` | `paths.client` |
| `fmrest` | `paths.data_api` (`rw`, then reduced by the level) |
| `fmodata` | `paths.odata` |
| `fmxdbc` | not supported, reported |
| External (group) accounts | group grants, with a warning that File4Base unions groups where FileMaker took the first match |
| Admin Console administrator roles | server roles |

The table is the vocabulary an administrator uses when moving file accounts
to server grants. The import itself creates database accounts only, and those
have no access-path setting ([security_model.md](security_model.md)): a file
account whose privilege set has neither `fmapp` nor `fmwebdirect` (an
API-only account in FileMaker) is created inactive and reported, rather than
gaining client sign-in. The import also reports a name already used by a
server account that reaches the database (`name_collision`).

## 6. UI notes

Wording follows what a FileMaker administrator expects, in File4Base's own
words and layout: *access level*, *access paths*, *server roles*,
*All solutions (default)*, *database accounts* vs *server accounts*. No
FileMaker screens, labels or help text are copied.

- **Account page**: server roles as checkboxes, then the solution table:
  *All solutions (default)* first, then overrides, a *show all solutions*
  toggle and search. Columns follow 2.3 (*No access / Read only / Read & write
  / Full (owner)*, *Desktop & web client*, *Data API Off/RO/RW*, *OData
  (read)*, *Export*, *Import*, *Run scripts*, *Design*); a row expands to
  tables and optionally layouts. Inherited values in italics.
- **Overview** for about 36 accounts × 50 solutions: solutions as rows,
  accounts as columns; sticky header and first column; compact cells (a level
  code and a Data API dot); filters *differences from default*, *Data API
  only*, account group, search; solutions grouped and collapsible (by the
  solution folders of #200, once they exist).
- **Bulk edit**: select cells, apply a level or path, confirm the number of
  cells changed, one `POST /grants/batch`.
- Reserved fields are disabled with "not enforced yet" (from
  `GET /access-model`); a non-`none` default says it reaches solutions
  created later.

## 7. Open questions for Mario

1. Whole-row replacement of `*` by a named grant (2.4), or per-field
   inheritance?
2. One revision per account, so any grant change signs the account out
   everywhere: acceptable, or do you want per-database revisions?
3. Directory groups: membership read at sign-in only, or refreshed during the
   session? A change in the directory takes effect at the next sign-in
   otherwise.
4. Should database accounts reach the Data API too (a per-database `data_api`
   capability, as `fmrest` on a file account), or only server accounts, as
   #208 suggests?
5. Linked sessions (#107) for server accounts: link the solutions the account
   reaches without a second password, each under its own grant?
6. Step 4 (PostgreSQL roles and RLS): are the database roles a projection of
   the control-store grants, rebuilt on each revision?
7. Server accounts on MariaDB and SQLite (#199): anything engine-specific left
   once the mirror row carries no role?
