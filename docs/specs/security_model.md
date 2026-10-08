# Security model

File4Base restricts what an account can do in four ways, all of them enforced by
the server:

| Boundary | Granted by | Enforced in |
| --- | --- | --- |
| **Database** | the session: a sign-in binds the token to one database | `AuthMiddleware.Authenticate` |
| **Account role** | `sys_users.role`: `owner`, `admin`, `user` | `RequireRole` / `RequireAdmin` / `RequireOwner` |
| **Table and layout access** | `sys_user_permissions`: `read_write`, `read_only`, `none` per layout, from which the access level on a base table is derived | `schema.Service.TableAccess` / `LayoutAccess`, through `DataHandler.authorize` |
| **Extended privileges** | `sys_privileges`: one capability switch per account role | `RequireCapability` |

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

A session carries an *account stamp* — a hash of the password hash, role and
active flag (`schema.AccountStamp`). Every authenticated request checks the
stamp against the account as stored now, so changing a password, changing a
role or deactivating an account rejects and revokes the sessions opened before
the change, including one created by a sign-in that raced the revocation.

Other properties the server keeps:

- No default accounts. Creating a database provisions the first owner
  explicitly, with a password the caller chooses.
- The data API only reaches tables registered in the catalog; `sys_*` tables are
  unreachable, and a name with a reserved prefix cannot be created either.
- A database always keeps one active owner: the last one cannot be demoted,
  deactivated or deleted.
