# Security model

File4Base restricts what an account can do in four ways, all of them enforced by
the server:

| Boundary | Granted by | Enforced in |
| --- | --- | --- |
| **Database** | the session: a sign-in binds the token to one database | `AuthMiddleware.Authenticate` |
| **Account role** | `sys_users.role`: `owner`, `admin`, `user` | `RequireRole` / `RequireAdmin` / `RequireOwner` |
| **Table and layout access** | `sys_user_permissions`: `read_write`, `read_only`, `none` per layout, from which the access level on a base table is derived | `schema.Service.TableAccess` / `LayoutAccess`, through `DataHandler.authorize` |
| **Extended privileges** | `sys_privileges`: one capability switch per account role | `RequireCapability` |

On top of these four, a **super-owner** (`sys_users.super_owner`) is a
per-account, server-wide privilege: it grants full control of every Server Tool
of every database, regardless of the database the session signed in to. It is
distinct from the four boundaries above — which all scope to the session's own
database — and is covered in [Super-owners](#super-owners) below.

It does **not** restrict access by the client used. That is a deliberate
decision, explained below.

## Extended privileges

An extended privilege is an *action* the server performs on request, and can
therefore refuse. There are two:

| Capability | Covers |
| --- | --- |
| `bulk_export` | `POST /api/v1/data/{table}/export`, `GET /api/v1/solutions/export-data` |
| `bulk_import` | `POST /api/v1/data/{table}/import`, `POST /api/v1/data/{table}/import/preview`, `POST /api/v1/solutions/import-data` |

They are held per account role in `sys_privileges`:

```sql
CREATE TABLE sys_privileges (
    role        VARCHAR(32) PRIMARY KEY,   -- owner | admin | user
    bulk_export BOOLEAN NOT NULL DEFAULT TRUE,
    bulk_import BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ
);
```

Rules:

- **A role with no row holds every privilege.** A database created before this
  table existed is therefore not restricted by upgrading to a version that has
  it: a restriction only ever appears because an owner asked for one.
- **An owner always holds every privilege**, whatever is stored. An owner must
  not be able to lock themselves out of their own database, so the owner row is
  written fully granted and read as fully granted.
- **Only an owner may change them.** An admin can read them. Letting an admin
  grant a privilege to their own role would be no restriction at all.
- **A change applies to sessions that are already open**, on the next request
  they make: the check reads the stored grants rather than anything carried in
  the token.
- Reading or writing **one record at a time** is not an extended privilege. That
  is governed by the account role and the table and layout access levels.
- They travel in the solution bundle (`privileges`) and are restored on import.
  Being keyed by role, they need no id remapping.

A client is told what its own role may do — `capabilities` in the responses of
`POST /api/v1/auth/login` and `GET /api/v1/auth/session` — so that the interface
can say a privilege is withheld instead of letting the request come back
refused. That is a courtesy, not the boundary: the server checks every call.

## Why there is no privilege per access method

A natural-looking fifth boundary would be the *access method*: allow this role
the desktop client, deny it the browser, deny it direct REST calls. File4Base
does not offer it, and the Extended privileges tab says so.

Every File4Base client is a REST client. The desktop application, the File4Base
Web Client served by Nginx and a script running `curl` all call the same
endpoints with the same `Authorization: Bearer` token. What the server receives
is a request; what would identify the client is something the request itself
claims — a header, a user agent, a path through a reverse proxy. Anyone holding
valid credentials can send any of those. A switch built on it would read as a
restriction and behave as a suggestion, which is worse than not having it: an
owner would believe REST access was closed while it was open.

Making it real would require the method to be *bound at sign-in*, so the session
carries it and a token issued for one method could not be replayed on another,
plus a secret shared between the Web Client's proxy and the API so that the
server can recognise its own browser client rather than take its word for it.
That is a different feature, with its own key management; it is not implemented,
so nothing claims it. See
[#39](https://github.com/file4base/file4base-app/issues/39) for the decision and
what building it would need.

## Account state and sessions

A session carries an *account stamp* — a hash of the password hash, role,
active flag and super-owner flag (`schema.AccountStamp`). Every authenticated
request checks the stamp against the account as stored now, so changing a
password, changing a role, deactivating an account or changing its super-owner
privilege rejects and revokes the sessions opened before the change, including
one created by a sign-in that raced the revocation.

Other properties the server keeps:

- No default accounts. Creating a database provisions the first owner
  explicitly, with a password the caller chooses.
- The data API only reaches tables registered in the catalog; `sys_*` tables are
  unreachable, and a name with a reserved prefix cannot be created either.
- A database always keeps one active owner: the last one cannot be demoted,
  deactivated or deleted.

## Super-owners

Server-wide administration is an *account* privilege, not a database role. An
account with `sys_users.super_owner` set reaches every Server Tool of every
database (see [server_tools_and_mfa.md](server_tools_and_mfa.md)); a database
role alone never does, since by default anyone may create a database and become
its owner.

- **Bootstrap.** `FILE4BASE_SUPER_OWNER=database:username` (comma-separated for
  several) marks accounts as super-owners at sign-in even before any flag is
  stored, so the first one can exist without another to grant it. The effective
  value at sign-in is `super_owner (stored) OR seeded`, and it is carried in the
  session (`auth.Session.SuperOwner`).
- **Delegation.** A super-owner grants or revokes the privilege on other
  **owner** accounts with `PUT /api/v1/security/users/{id}/super-owner`
  (`RequireSuperOwner`). The target must be an owner, and a super-owner cannot
  strip their own privilege (so the population cannot lock itself out by
  accident).
- **Compatibility.** An owner of `FILE4BASE_ADMIN_DATABASE` remains a server
  administrator even without the flag, so existing admin-database setups keep
  working. That legacy path grants the server-wide *tools* but not the right to
  manage the super-owner flag.
- The super-owner flag is part of the account stamp, so granting or revoking it
  ends the target's existing sessions at once (see above).

## Transport and payload protection

Two layers protect traffic between a client and `file4base-api`:

- **TLS.** Enable it with `TLS_ENABLED=true` (plus `TLS_CERT_FILE`/`TLS_KEY_FILE`,
  or an ephemeral self-signed ECDSA P-256 certificate). TLS is what actually
  keeps traffic confidential on a network.
- **AES-256-GCM payload envelope** (`internal/crypto`). The Web Client and the
  API can wrap request/response bodies in an authenticated `F4BE\x01` envelope
  (12-byte random nonce, GCM tag over the magic header as additional data),
  keyed from `FILE4BASE_CHANNEL_SECRET`. This is defence-in-depth, not a
  substitute for TLS: **when `FILE4BASE_CHANNEL_SECRET` is unset the key is
  derived from a built-in default that is public (it is in the source), so the
  envelope then offers no confidentiality on its own.** The server logs a
  warning at startup in that case. Set `FILE4BASE_CHANNEL_SECRET` and serve over
  TLS on any non-local deployment. The middleware caps the amount of an
  encrypted request body it reads into memory (512 MiB, well above the 256 MiB
  import ceiling) so a hostile oversized payload cannot exhaust memory.

A client's address, as recorded in the audit log, the session list and the
login-throttle key, is taken from the direct TCP peer and never from a
client-supplied `X-Forwarded-For` / `X-Real-IP` header, unless the request
arrived through a reverse proxy listed in `FILE4BASE_TRUSTED_PROXIES`. A direct
client therefore cannot forge the address attributed to it.

## Hardening review (0.15.1)

A focused review of the file importers, the encrypted channel and the control
store was carried out for 0.15.1. Summary of findings and their disposition:

- **File importers** (`internal/migration`: binary `.fmp12` records and HTML DDR
  parsing). Imported table and column names are reduced to a strict
  `[a-z0-9_]` whitelist (`sanitizeIdentifier`) and every DDL/DML statement that
  uses them also goes through `dialect.QuoteIdentifier`, so a crafted DDR cannot
  inject SQL. The binary `.fmp12` page-tree and record decoders bound every read
  against the input length, and uploads are capped at 256 MiB. Regular
  expressions are Go's RE2 (linear time, no catastrophic backtracking) and the
  one pattern built from parsed field names escapes them with
  `regexp.QuoteMeta`. No code change was required; the only note is cosmetic
  (some regexps are compiled per call rather than once).
- **Encrypted channel** (`internal/crypto`). The AEAD construction is sound
  (AES-256-GCM, random nonce, authenticated header). Two hardening changes were
  made: a startup warning when `FILE4BASE_CHANNEL_SECRET` is unset (the default
  key provides no confidentiality), and a 512 MiB cap on the in-memory read of
  an encrypted request body (see above).
- **Control store** (`internal/control`). All SQL is parameterised (`?`
  placeholders) and multi-statement writes run in transactions; no identifier or
  value is interpolated into a statement. No injection surface was found; no
  change required.
