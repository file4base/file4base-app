# SQLite engine for solution databases

SQLite is the third engine a File4Base server can keep its solution databases
in, next to PostgreSQL (the default) and MariaDB. It needs no database server:
each solution database is one file. Every feature the server offers on
PostgreSQL works on SQLite, with the exceptions listed under
[Blocked](#blocked) and [Test exceptions](#test-exceptions).

- Issue: [#199](https://github.com/file4base/file4base-app/issues/199)
- Engine: [`server/internal/dbal/sqlite_conn.go`](../../server/internal/dbal/sqlite_conn.go),
  [`sqlite_files.go`](../../server/internal/dbal/sqlite_files.go),
  [`sqlite_funcs.go`](../../server/internal/dbal/sqlite_funcs.go),
  [`sqlite/dialect.go`](../../server/internal/dbal/sqlite/dialect.go)
- Driver: `modernc.org/sqlite` (pure Go, no cgo), SQLite 3.53
- Benchmarks: [`server/cmd/f4b-bench`](../../server/cmd/f4b-bench)

---

## Choosing the engine

The engine is chosen when a server is installed, with `DB_ENGINE`. Solutions
do not move between engines by copying files: export a solution with its data
and import it into the other server.

| Variable | Default | Meaning |
| --- | --- | --- |
| `DB_ENGINE` | `postgres` | `sqlite` keeps the solution databases in files. |
| `DATABASE_URL` | `./data/solutions` | With SQLite, the data directory: one `<database>.sqlite` file per solution database. It is created `0700` if missing. |
| `SQLITE_BUSY_TIMEOUT` | `5s` | How long a write waits for the writer lock before it fails (a Go duration). |
| `SQLITE_ALLOW_UNSAFE_STORAGE` | `false` | Turns the refusal of an unsafe filesystem into a warning. Not supported. |

`docker-compose.sqlite.yml` is the stack on SQLite: the API alone, the
solutions on its named volume.

The default engine is one constant, `dbal.DefaultEngine`: changing it changes
the server's default, the test suite's default (`F4B_TEST_ENGINE`) and the
server tools' assumptions together.

## Files

- A database is `<data directory>/<name>.sqlite`, named as on the other
  engines (`dbal.IsValidDatabaseName`); a name that is not valid never becomes
  a path.
- It is created with `O_EXCL`, mode `0600`, and put in WAL mode at creation. A
  database is never created by opening it: the connections open with
  `mode=rw`, so a name that names no database fails as it does on a server.
- Dropping a database removes the file and its `-wal`, `-shm` and `-journal`
  companions.
- There is no administrative database. Listing the databases lists the
  directory.

## Connection contract

Persistent settings are set once, per-connection settings come from the DSN,
which the driver applies to **every** connection a pool opens before its first
statement.

| Setting | Value | Kind | Why |
| --- | --- | --- | --- |
| `journal_mode` | `WAL` | persistent | Readers never block the writer, nor the writer readers. Set when the database is created, read back when a pool is opened (a database that cannot be put in WAL is refused) and by the diagnostics. |
| `synchronous` | `FULL` | connection | A commit is on disk when it returns: a power loss loses no committed transaction. `NORMAL` would be ~500 times faster per commit but can lose the last commits. |
| `busy_timeout` | 5 s | connection | Bounds a write's wait; measured below. |
| transactions | `BEGIN IMMEDIATE` | connection | A transaction takes the writer lock before its first statement, so one that reads then writes never fails halfway with `SQLITE_BUSY`. |
| `journal_size_limit` | 64 MB | connection | A checkpoint shrinks the WAL back after a large import. |
| `foreign_keys` | on | connection | As on the other engines. |
| other pragmas | SQLite defaults | | `cache_size`, `mmap_size`, `temp_store`, `wal_autocheckpoint` (1000 pages): no measurement showed a gain worth the memory. |

The data directory must be on a **local filesystem**. A Docker **named volume**
is the supported setup.

### Writers take turns

SQLite allows one writer per database at a time. A write transaction (and a
write statement outside one) first takes the database's writer lock **of the
process**: a one-slot channel, which Go hands to waiters in the order they
came, bounded by `busy_timeout` (then `ErrSQLiteBusy`, whose text is SQLite's
"database is locked"). SQLite's own busy handler polls with growing sleeps, so
without the queue a writer under load could wait several seconds while later
ones went first (50 saturating editors: 2.7 s worst wait before, 0.4 s after).
SQLite's `busy_timeout` still covers writers in other processes (a restore, a
maintenance tool).

### A transaction SQLite ended

SQLite rolls a transaction back by itself on some errors (`SQLITE_FULL`,
`IOERR`, `NOMEM`, `INTERRUPT`, some `BUSY`), where PostgreSQL and MariaDB keep
it open; a statement run afterwards would then commit on its own. After any
error inside a transaction the connection asks SQLite whether the transaction
is still open; when it is not, every later statement and the commit fail with
`ErrSQLiteTransactionEnded`. The decision rests on SQLite's state, not a list
of error codes.

### Storage check

At start-up and before a database is created or restored, the data directory
is checked twice: its `statfs` type, and the type the mount table gives the
mount it is on. It is refused on NFS, SMB/CIFS, 9p, FUSE (`fuse.*`, gRPC FUSE,
osxfs), virtiofs, Docker Desktop's `fakeowner`, WSL's `drvfs`, VirtualBox,
Parallels and VMware shared folders, AFS, CephFS, GlusterFS and Lustre: their
locks or shared memory do not protect a WAL database from corruption.
`SQLITE_ALLOW_UNSAFE_STORAGE=true` only turns the refusal into a warning.

### Diagnostics

`GET /api/v1/server/maintenance` carries, on SQLite, a `sqlite` object read
from a live connection: SQLite version (and whether it has the fixes for the
WAL-reset and other corruption bugs of 3.44 to 3.51: 3.44.6+, 3.50.7+,
3.51.3+), `journal_mode`, `synchronous`, `busy_timeout_ms`, `foreign_keys`,
`journal_size_limit`, the filesystem, and `problems`: every setting that
differs from the contract. `POST /api/v1/server/maintenance/optimize` runs
`PRAGMA analysis_limit = 1000; PRAGMA optimize = 0x10002`.

## Backups

A physical backup is `VACUUM INTO`: a consistent, compacted copy taken while
the server keeps reading and writing (readers do not block, and the copy is a
read transaction). `dbal.BackupSQLite` writes it next to its destination,
checks it with `quick_check`, syncs it and publishes it with a hard link, so a
backup never replaces an existing file and a half-written one is never under
its name. `dbal.RestoreSQLiteDatabase` copies a backup next to the database,
checks it, puts it in WAL mode, checkpoints and publishes it under a name that
must not exist.

```bash
docker exec file4base-api /app/server sqlite-backup  <database> /app/data/backups/<file>
docker exec file4base-api /app/server sqlite-restore /app/data/backups/<file> <new database>
```

The server tools' snapshots (`.f4b.json`) work on SQLite as on the other
engines.

A long backup holds a read transaction, during which a checkpoint cannot
reset the WAL: it grows with the writes made meanwhile and shrinks back
(`journal_size_limit`) at the next checkpoint after.

## Text

| | PostgreSQL 16 (`postgres:16-alpine`) | SQLite | MariaDB 11 |
| --- | --- | --- | --- |
| Find `LIKE`, `=` | Unicode lower case on both sides: case-insensitive, accent-sensitive | the same: `lower()` is replaced by Go's Unicode mapping, `LIKE ... ESCAPE '\'` | `utf8mb4_general_ci`: case- **and accent-insensitive** |
| Sort | the database collation; on Alpine (musl) that is **code point order** (`Zoe` before `ab`, `É` after `z`) | `COLLATE F4B` = `calc.CompareText`: the order of a sort in a calculation and of unstored fields, ignoring case first, lower case first on a tie | `utf8mb4_general_ci` |
| Group by (reports) | exact text | exact text (`BINARY`) | case- and accent-insensitive groups (`Paris` and `PARIS` are one group) |
| Unique, equality, indexes | exact text | exact text (`BINARY`) | case-insensitive |

The F4B collation applies to `ORDER BY` of text fields only: equality, `GROUP
BY` and indexes stay `BINARY`, as on PostgreSQL. So a sort on SQLite gives
FileMaker's (and a calculation's) order, where the default PostgreSQL image
gives code point order; finds and reports give the same results on both.

Case mapping is character by character (Go `unicode.ToLower`/`ToUpper`, as
PostgreSQL's in a UTF-8 database): `İ` lowers to `i`, `ı` stays `ı`, so a find
for `YILMAZ` does not match `Yılmaz` on any engine. One difference: `upper('ß')`
is `ß` on SQLite and `ẞ` on PostgreSQL.

FileMaker finds by the field's index language, which for most languages also
ignores accents (`ecole` finds `École`). Neither PostgreSQL nor SQLite does
that here; this is a difference from FileMaker on both engines, not one
between them (unverified against FileMaker in this work).

## Numbers, dates and types

- SQLite stores a number as an integer or an IEEE double: 15 significant
  digits, where PostgreSQL's `NUMERIC` is exact. A value read back is given as
  its decimal text (`1500000`, `12.5`), as the PostgreSQL driver gives it.
  Sums use SQLite's compensated summation; the reports of the benchmark match
  PostgreSQL to the cent over 20,000 records.
- SQLite stores any value in any column. Each column gets a `CHECK` that keeps
  it to its declared type (a number, a date `julianday` reads, 0/1), so a
  value PostgreSQL refuses is refused here too.
- `VARCHAR(n)` lengths are not enforced (as on MariaDB in non-strict mode).
- Dates are `YYYY-MM-DD` text, timestamps `YYYY-MM-DD HH:MM:SS.fff+00:00` text
  in UTC: their text sorts in time order.
- An empty container is read as an empty value, not `NULL`.

## Inventory

Every engine branch and PostgreSQL-only construct outside `dbal` (141 engine
references in 37 files, plus the SQL text they guard) was read and classified.

### Portable (works on SQLite unchanged)

| Construct | Where | Note |
| --- | --- | --- |
| `$N` placeholders | ~110 `EngineMariaDB` branches across schema, data, api, extsource | SQLite takes the PostgreSQL form. |
| Double-quoted identifiers, `LIMIT/OFFSET`, `COALESCE`, `NULLIF`, `CASE`, CTEs | everywhere | |
| `TRUE`/`FALSE`, `RETURNING`, `ON CONFLICT ... DO NOTHING/UPDATE` | schema services | SQLite 3.35+. `ON CONFLICT (id) DO NOTHING` now runs on every engine but MariaDB. |
| `CREATE [UNIQUE] INDEX [IF NOT EXISTS]`, `DROP INDEX IF EXISTS`, `RENAME COLUMN`, `ADD COLUMN` | schema | |
| Savepoints, nested rollbacks | scripts, imports | |
| `UNION ALL` of plain branches, `IN (subquery)`, `EXISTS` | related reads, finds | |
| Data-source, script, privilege, MFA, menu, value-list, layout and saved-find services | schema | placeholder branches only. |

### Emulated

| PostgreSQL construct | SQLite form | Where |
| --- | --- | --- |
| `information_schema.columns/tables` | `sqlite_schema` × `pragma_table_info` (`dbal.CatalogColumnsSQL`, `CatalogTablesSQL`) | data/creation_order, schema/repetitions, creation_order, external_occurrences, transfer/scheduler |
| `SELECT ... FOR NO KEY UPDATE` | nothing (`dbal.RowLockClause`): the transaction already holds the database's writer lock | data/autoenter, schema/service, custom_function_design |
| Multi-key match fields: `string_to_array` / `unnest` | `json_each(f4b_lines(x))`, through a correlated subquery so `key`/`value` columns are not taken for `json_each`'s | data/multikey |
| `VALUES ($1::text), ...` key lists | `VALUES` without casts (also escapes the 500-term compound limit) | data/external_find |
| `(SELECT ... ORDER BY ... LIMIT) UNION ALL (...)` | each branch as `SELECT * FROM (...)` (`unionAll`) | data/related, related_reads |
| `ALTER COLUMN TYPE ... USING` | add a column, copy with `NULLIF(c, '')` checked against the new type, drop indexes, drop, rename, recreate indexes, in one savepoint | dbal (connection) |
| `ADD COLUMN IF NOT EXISTS`, `DROP COLUMN IF EXISTS`, drop of an indexed column | the same, in a savepoint, after reading `pragma_table_info` / dropping the column's indexes | dbal (connection) |
| `ILIKE`, `lower`/`upper` | `LOWER(...) LIKE LOWER(...) ESCAPE '\'`, Unicode `lower`/`upper` registered on the driver | dbal |
| `~` regular expressions (`NumericValue`) | `REGEXP` with Go RE2 | dbal |
| Text sort | `COLLATE F4B` | data/service |
| Empty values in a sort | first when ascending, as SQLite (and MariaDB) sort `NULL`; PostgreSQL puts them last. Sorts of unstored fields, done in memory, follow the engine | data/unstored_sort |
| Identity creation order | `INTEGER PRIMARY KEY AUTOINCREMENT` | dbal/creation_order |
| `now()` defaults | `strftime` UTC text | dialect |
| Unique violation (index name) | `UNIQUE constraint failed: table.column` mapped to the field | validation |
| Type safety | `CHECK` per column type | dialect |
| `bytea`/`numeric`/`boolean` values | normalized as the pgx driver returns them | dbal (rows) |
| `CREATE/DROP DATABASE`, `pg_database` | files in the data directory | dbal/multi_db |
| `VACUUM ANALYZE` | `PRAGMA optimize` | api/server_admin_handler |
| Container name, port | "embedded SQLite", port 0 | api |

### Blocked

| What | Why | Effect |
| --- | --- | --- |
| Adding a creation order to an existing table, and noticing a creation order column added elsewhere | SQLite cannot add an `AUTOINCREMENT` primary key to a table, nor drop one. | None for SQLite solutions: their tables are created with it. It is the upgrade path of tables created by File4Base before the creation order existed, which cannot be SQLite tables. |
| `ALTER COLUMN ... DROP NOT NULL` (external occurrence upgrade) | no such statement | Same: SQLite catalogs are created with the columns nullable. |
| An external SQL data source **on** SQLite | an external source is a server File4Base connects to | A SQLite server can still read PostgreSQL and MariaDB sources. |

## Cross-solution writes (#166)

A delete that cascades into other solutions deletes each solution's records
in one transaction of that solution, the other solutions first, this one
last, and reports a `PartialDeleteError` when a later solution fails after an
earlier one committed. That is the same on every engine: PostgreSQL has no
transaction across databases either. SQLite solutions are separate files and
are not `ATTACH`ed into one connection: in WAL mode, a transaction over
attached databases is atomic in each file but not across them on a crash, so
attaching would add no guarantee. A crash between two solutions leaves the
related records of the earlier solutions deleted and the record itself in
place; the delete can be repeated.

## External read access

A SQLite server has no database port: BI tools and scripts read a solution
through the Data API (`/api/v1/...`, with the solution's accounts and
privileges), not by opening the file. Opening a live database file from
another process is not supported (its writes would bypass the writer queue
and the privileges). File4Base has no OData endpoint yet.

## Test exceptions

The whole server suite runs on SQLite with no external service
(`F4B_TEST_ENGINE=sqlite go test ./...`, the `go-test-sqlite` CI job). Skipped
on SQLite:

| Test | Reason |
| --- | --- |
| `TestCreationOrder_ColumnAddedElsewhereIsNoticed`, `TestCreationOrder_ExistingTableIsUpgraded` | Blocked: upgrade of a table without creation order. |
| `TestExternalCascade_KeepsWhatMovedSinceThePlan` | The test holds a write transaction while another solution's connection writes to the same file; a single writer per database waits for it. |
| `TestReadFromAnotherDatabase`, `TestAPI_ImportFromAnExternalSource` | Use the test engine as an external SQL server. |
| `TestScriptService_*RollsBackOnFailingStep` (already skipped on MariaDB) | Rely on `VARCHAR` length enforcement. |
| `TestExternalCascade_RecordNamedByACaseVariantOfItsID` (already skipped off PostgreSQL) | Ids are compared with their case. |

## Benchmarks

`server/cmd/f4b-bench` runs the same synthetic workload through File4Base's
own services on each engine, on the same host. See the PR of #199 for the
full table; the measures that set the defaults:

<!-- BENCHMARK RESULTS -->
