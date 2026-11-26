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
  a path. Names are lower case, as `CreateDatabase` makes them: `Sales` is the
  database `sales` everywhere (open, drop, CLI), and a file with capitals is
  not listed.
- It is made under a temporary name (mode `0600`), put in WAL mode, synced,
  then published under its name with a link, which never replaces a file: a
  crash leaves no empty file under the name. A `-wal`, `-shm` or `-journal`
  file already next to the name is refused, since SQLite would replay it into
  the new database. A database is never created by opening it: the
  connections open with `mode=rw`, so a name that names no database fails as
  it does on a server.
- Dropping a database first renames its file out of its name (to a hidden
  name of its own, so a drop that stopped halfway is finished by the next),
  then removes the `-wal`, `-shm` and `-journal` companions, then the renamed
  file; it stops at the first failure, so a `-wal` that may hold commits is
  never removed while its database can still be opened. A pool refuses to
  open a connection on a file other than the one it was opened on (a
  database dropped and created again meanwhile). Creating, dropping and
  opening a database take turns in the server, per database (a slow `fsync`
  holds up that database only); a connection still in use when its pool is
  closed keeps the deleted files it has open until it is closed. There is no
  lock across processes: the CLI tools create and restore under names that
  must not exist, and must not drop a database a server has open.
- The data directory is the server's own: writable by its user only. Other
  users able to write in it could replace a temporary file between two
  steps of a creation or a backup.
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
maintenance tool). A single `VACUUM INTO` statement (a backup) does not take
the queue, and a query that writes (`INSERT`/`UPDATE`/`DELETE`/`REPLACE ...
RETURNING`, also after `WITH` and leading comments) does.

A queued writer holds the pool connection it waits on (10 per database). A
writer that holds the lock and reads on a second connection meanwhile (an
import evaluating a validation calculation) can find the pool full of
waiters; it then waits until they time out. Not seen in the benchmarks; a
larger pool or reading inside the transaction removes it.

### A transaction SQLite ended

SQLite rolls a transaction back by itself on some errors (`SQLITE_FULL`,
`IOERR`, `NOMEM`, `INTERRUPT`, some `BUSY`), where PostgreSQL and MariaDB keep
it open; a statement run afterwards would then commit on its own. After any
error inside a transaction the connection asks SQLite whether the transaction
is still open; when it is not, every later statement and the commit fail with
`ErrSQLiteTransactionEnded`. The decision rests on SQLite's state, not a list
of error codes. An emulated column change (below) runs in a savepoint that is
ended whatever happens to the request's context; when it cannot be, the
connection is discarded (closing it rolls back), never released after a
failed rollback.

### Storage check

At start-up and before a database is created or restored, the data directory
is checked twice: its `statfs` type, and the type the mount table gives the
mount it is on. It is refused on NFS, SMB/CIFS, 9p, FUSE (`fuse.*`, gRPC FUSE,
osxfs), virtiofs, Docker Desktop's `fakeowner`, WSL's `drvfs`, VirtualBox,
Parallels and VMware shared folders, AFS, CephFS, GlusterFS and Lustre: their
locks or shared memory do not protect a WAL database from corruption.
The database file itself is checked again when a pool is opened, since a file
can be mounted on its own. The check exists on Linux only; the server image
and the Linux hosts are where it runs. `SQLITE_ALLOW_UNSAFE_STORAGE=true` only
turns the refusal into a warning.

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
read transaction, which does not wait for writers nor hold them up).
`dbal.BackupSQLite` writes it into a `0600` temporary file next to its
destination, checks it, syncs it and publishes it with a hard link, so a
backup never replaces an existing file and a half-written one is never under
its name. `dbal.RestoreSQLiteDatabase` copies a backup next to the database,
checks it, puts it in WAL mode, checkpoints and publishes it under a name that
must not exist. The check is the header, the size the header gives, and
`quick_check` (SQLite reads an empty or truncated file as a database, which
`quick_check` passes). A file in WAL mode, or with a `-wal` next to it, is
refused: it is a copy of a live database, whose last commits may be in a WAL
that is not there (`VACUUM INTO` writes a rollback-journal file).

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
  PostgreSQL to the cent over 20,000 records. A total of integers beyond
  ±9.2 × 10^18 fails with `integer overflow` rather than being wrong.
  `STDDEV_SAMP` is registered on the driver.
- A number compared with a line of a multi-key text field is compared as the
  text File4Base gives it back (`0.000015`), not SQLite's cast.
- A number smaller than 10^-4 is cast to text in exponent form (`1.0e-06`,
  PostgreSQL `0.000001`): comparisons (`>`, ranges) compare the number, but a
  text find (`=`, `LIKE`) on its digits does not match it.
- SQLite stores any value in any column. Each column gets a `CHECK` that keeps
  it to its declared type (a number; a date that is a day of the calendar,
  `date(x) IS x`; a timestamp in the stored UTC form; 0/1), so a value
  PostgreSQL refuses is refused here too.
- A Boolean is 0/1; finds compare it as `true`/`false`, as PostgreSQL casts
  it. (MariaDB's `TINYINT` reads `1`: `==true` does not find it there, a
  pre-existing MariaDB difference.) Imports match a Boolean by value on every
  engine.
- A field type change is rewritten as a copy into a new column (SQLite has no
  `ALTER COLUMN ... TYPE`), in a savepoint. Text becomes a number when it is
  one, a date when it starts with a day of the calendar and is a date or
  timestamp as a whole (the day written is kept, not the UTC day of an
  offset), a timestamp in the stored UTC form, rounded to the millisecond.
  Other values become empty, as `NULLIF`/casts do on PostgreSQL; a `NOT NULL`
  column is refused rather than emptied.
- `STDDEV_SAMP` scales the numbers by the largest magnitude, so neither
  `1e200` nor `1e-200` overflows or vanishes.
- `VARCHAR(n)` lengths are not enforced (as on MariaDB in non-strict mode).
- Dates are `YYYY-MM-DD` text, timestamps `YYYY-MM-DD HH:MM:SS[.f…]+00:00`
  text in UTC (the check allows nothing else): their text sorts in time
  order. A date compared with a timestamp field in a relationship is
  midnight of its day, as PostgreSQL promotes it. A timestamp bound against
  a date field is its day, as PostgreSQL binds a parameter in the column's
  type (MariaDB compares it as a `DATETIME` instead).
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
| `information_schema.columns/tables` | `sqlite_schema` × `temp.pragma_table_info` (`dbal.CatalogColumnsSQL`, `CatalogTablesSQL`; `temp.` so a table of that name does not hide the function) | data/creation_order, schema/repetitions, creation_order, external_occurrences, transfer/scheduler |
| `SELECT ... FOR NO KEY UPDATE` | nothing (`dbal.RowLockClause`): the transaction already holds the database's writer lock | data/autoenter, schema/service, custom_function_design |
| Multi-key match fields: `string_to_array` / `unnest` | `temp.json_each(f4b_lines(x))`, through a correlated subquery so `key`/`value` columns are not taken for `json_each`'s | data/multikey |
| `VALUES ($1::text), ...` key lists | `VALUES` without casts (also escapes the 500-term compound limit) | data/external_find |
| `(SELECT ... ORDER BY ... LIMIT) UNION ALL (...)` | each branch as `SELECT * FROM (...)` (`unionAll`) | data/related, related_reads |
| `ALTER COLUMN TYPE ... USING` | add a column, copy with `NULLIF(c, '')` (a date or timestamp converted to its stored form) checked against the new type, drop indexes, drop, rename, recreate indexes, in one savepoint. A `NOT NULL` column is refused (SQLite cannot add the constraint to a populated table) | dbal (connection) |
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
