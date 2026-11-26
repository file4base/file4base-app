package dbal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// On SQLite the "server" is a directory and each solution database is the
// file <name>.sqlite in it (docs/specs/sqlite_engine.md, "Files"). The base
// DSN (DATABASE_URL) is the directory. There is no administrative database:
// listing, creating and dropping databases are file operations.

// SQLiteFileExtension ends the name of every solution database file.
const SQLiteFileExtension = ".sqlite"

// sqliteDir is the dsnTemplate of a SQLite data directory.
type sqliteDir struct{ dir string }

func (d *sqliteDir) database() string { return "" }

func (d *sqliteDir) forDatabase(name string) string {
	return filepath.Join(d.dir, name+SQLiteFileExtension)
}

// SQLiteDataDir reads the data directory from a SQLite base DSN: a path,
// file:path or sqlite://path.
func SQLiteDataDir(dsn string) (string, error) {
	dir, err := SQLitePath(dsn)
	if err != nil {
		return "", fmt.Errorf("invalid SQLite data directory: %w", err)
	}
	return dir, nil
}

// checkSQLiteDir checks that the data directory exists, is a directory, is
// writable, and is on a filesystem SQLite can lock safely.
func checkSQLiteDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("sqlite data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sqlite data directory %s is not a directory", dir)
	}
	probe, err := os.CreateTemp(dir, ".f4b-probe-*")
	if err != nil {
		return fmt.Errorf("sqlite data directory %s is not writable: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return CheckSQLiteStorage(dir)
}

// ErrUnsafeSQLiteStorage is returned for a data directory on a filesystem
// where SQLite's locks or WAL shared memory are not reliable.
var ErrUnsafeSQLiteStorage = errors.New("unsafe storage for SQLite")

// AllowUnsafeSQLiteStorage turns the refusal of an unsafe filesystem into a
// warning (SQLITE_ALLOW_UNSAFE_STORAGE). It is set once, at start-up.
var AllowUnsafeSQLiteStorage = false

// CheckSQLiteStorage refuses a directory on a network or FUSE filesystem
// (NFS, SMB/CIFS, 9p, and the virtiofs/gRPC-FUSE bind mounts of Docker
// Desktop): their locks and shared memory do not protect a WAL database
// from corruption. A local filesystem, such as a Docker named volume, is the
// supported storage.
func CheckSQLiteStorage(dir string) error {
	fs, unsafe := sqliteFilesystem(dir)
	if !unsafe {
		return nil
	}
	err := fmt.Errorf("%w: %s is on %s; keep SQLite databases on a local filesystem (a Docker named volume, not a bind mount from Docker Desktop or a network share)", ErrUnsafeSQLiteStorage, dir, fs)
	if AllowUnsafeSQLiteStorage {
		sqliteWarn(err)
		return nil
	}
	return err
}

// sqliteWarn reports a refused condition that was allowed. It is a variable
// so tests can watch it.
var sqliteWarn = func(err error) { fmt.Fprintln(os.Stderr, "WARNING:", err) }

// SQLiteFilesystem names the filesystem a directory is on ("" when it is
// unknown) and whether SQLite databases are refused there.
func SQLiteFilesystem(dir string) (string, bool) { return sqliteFilesystem(dir) }

// listSQLiteDatabases lists the solution databases of a data directory.
func listSQLiteDatabases(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed listing databases: %w", err)
	}
	result := []string{}
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), SQLiteFileExtension)
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		if IsProtectedDatabase(name) || !validDatabaseName.MatchString(name) {
			continue
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

// CreateSQLiteDatabase creates an empty database file in WAL mode. It
// returns ErrDatabaseExists when the file is already there, and leaves no
// file behind when it fails.
func CreateSQLiteDatabase(ctx context.Context, path string) (err error) {
	if err := CheckSQLiteStorage(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrDatabaseExists
		}
		return err
	}
	_ = f.Close()
	defer func() {
		if err != nil {
			removeSQLiteFiles(path)
		}
	}()
	db, err := openSQLite(ctx, path)
	if err != nil {
		return err
	}
	defer db.Close()
	// An empty file has no header until the first write; the journal mode is
	// persistent only once it is written.
	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 0"); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// removeSQLiteFiles deletes a database file and the files SQLite keeps next
// to it.
func removeSQLiteFiles(path string) error {
	var firstErr error
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// BackupSQLite writes a consistent copy of a live database to dest, which
// must not exist, with VACUUM INTO: it reads one snapshot, so writers carry
// on meanwhile. The copy is written to a temporary file in dest's directory,
// checked, synced and only then published under its name, so dest is either
// absent or a complete, checked database. The copy is compact and in
// rollback-journal mode; openSQLite turns it to WAL when it is restored.
func BackupSQLite(ctx context.Context, db *sql.DB, dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("backup %s: %w", dest, os.ErrExist)
	}
	tmp := fmt.Sprintf("%s.tmp-%d", dest, time.Now().UnixNano())
	defer removeSQLiteFiles(tmp)
	if _, err := db.ExecContext(ctx, "VACUUM INTO $1", tmp); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := quickCheckFile(ctx, tmp); err != nil {
		return fmt.Errorf("backup check: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		return err
	}
	// link refuses to replace a file, where rename would.
	if err := os.Link(tmp, dest); err != nil {
		return fmt.Errorf("backup publish: %w", err)
	}
	return syncDir(filepath.Dir(dest))
}

// RestoreSQLiteDatabase makes a backup the database file dest, which must
// not exist: the backup is copied next to dest, checked, turned to WAL and
// published under dest's name.
func RestoreSQLiteDatabase(ctx context.Context, backup, dest string) (err error) {
	if _, err := os.Lstat(dest); err == nil {
		return ErrDatabaseExists
	}
	if err := CheckSQLiteStorage(filepath.Dir(dest)); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.restore-%d", dest, time.Now().UnixNano())
	defer removeSQLiteFiles(tmp)
	if err := copyFile(backup, tmp); err != nil {
		return err
	}
	if err := quickCheckFile(ctx, tmp); err != nil {
		return fmt.Errorf("restore check: %w", err)
	}
	db, err := openSQLite(ctx, tmp) // sets WAL
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	if err := syncFile(tmp); err != nil {
		return err
	}
	if err := os.Link(tmp, dest); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrDatabaseExists
		}
		return err
	}
	return syncDir(filepath.Dir(dest))
}

func quickCheckFile(ctx context.Context, path string) error {
	db := sql.OpenDB(&sqliteConnector{dsn: sqliteReadOnlyDSN(path)})
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("quick_check: %s", result)
	}
	return nil
}

func sqliteReadOnlyDSN(path string) string {
	return strings.Replace(sqliteDSN(path), "mode=rw", "mode=ro", 1)
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	if _, err = out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}

// SQLiteDiagnostics is what the connection contract says a solution
// database's connections run with, read back from one of them.
type SQLiteDiagnostics struct {
	Version           string   `json:"version"`
	VersionQualified  bool     `json:"version_qualified"`
	JournalMode       string   `json:"journal_mode"`
	Synchronous       string   `json:"synchronous"`
	BusyTimeoutMS     int64    `json:"busy_timeout_ms"`
	ForeignKeys       bool     `json:"foreign_keys"`
	JournalSizeLimit  int64    `json:"journal_size_limit"`
	Filesystem        string   `json:"filesystem"`
	FilesystemRefused bool     `json:"filesystem_refused"`
	Problems          []string `json:"problems"`
}

// OK reports whether every setting is the one the contract expects.
func (d SQLiteDiagnostics) OK() bool { return len(d.Problems) == 0 }

// DiagnoseSQLite reads the settings of a connection of db, whose file is
// path, and lists every difference from the connection contract.
func DiagnoseSQLite(ctx context.Context, db *sql.DB, path string) (SQLiteDiagnostics, error) {
	var d SQLiteDiagnostics
	conn, err := db.Conn(ctx)
	if err != nil {
		return d, err
	}
	defer conn.Close()
	var sync, fk int64
	for _, q := range []struct {
		sql  string
		dest any
	}{
		{"SELECT sqlite_version()", &d.Version},
		{"PRAGMA journal_mode", &d.JournalMode},
		{"PRAGMA synchronous", &sync},
		{"PRAGMA busy_timeout", &d.BusyTimeoutMS},
		{"PRAGMA foreign_keys", &fk},
		{"PRAGMA journal_size_limit", &d.JournalSizeLimit},
	} {
		if err := conn.QueryRowContext(ctx, q.sql).Scan(q.dest); err != nil {
			return d, fmt.Errorf("%s: %w", q.sql, err)
		}
	}
	d.Synchronous = map[int64]string{0: "OFF", 1: "NORMAL", 2: "FULL", 3: "EXTRA"}[sync]
	d.ForeignKeys = fk == 1
	d.VersionQualified = SQLiteVersionQualified(d.Version)
	d.Filesystem, d.FilesystemRefused = sqliteFilesystem(filepath.Dir(path))

	problem := func(format string, args ...any) { d.Problems = append(d.Problems, fmt.Sprintf(format, args...)) }
	if !d.VersionQualified {
		problem("SQLite %s lacks the WAL-reset fix (3.44.6, 3.50.7 or 3.51.3 and later)", d.Version)
	}
	if !strings.EqualFold(d.JournalMode, "wal") {
		problem("journal_mode is %s, not wal", d.JournalMode)
	}
	if d.Synchronous != "FULL" {
		problem("synchronous is %s, not FULL", d.Synchronous)
	}
	if want := SQLiteBusyTimeout().Milliseconds(); d.BusyTimeoutMS != want {
		problem("busy_timeout is %d ms, not %d", d.BusyTimeoutMS, want)
	}
	if !d.ForeignKeys {
		problem("foreign_keys is off")
	}
	if d.JournalSizeLimit != sqliteJournalSizeLimit {
		problem("journal_size_limit is %d, not %d", d.JournalSizeLimit, sqliteJournalSizeLimit)
	}
	if d.FilesystemRefused {
		problem("the database is on %s, which SQLite cannot lock safely", d.Filesystem)
	}
	return d, nil
}

// SQLiteVersionQualified reports whether a SQLite version has the fix for
// the WAL-reset corruption bug: 3.44.6+, 3.50.7+ or 3.51.3+.
func SQLiteVersionQualified(version string) bool {
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return false
	}
	switch {
	case major != 3:
		return major > 3
	case minor > 51:
		return true
	case minor == 51:
		return patch >= 3
	case minor == 50:
		return patch >= 7
	case minor >= 45:
		return false
	case minor == 44:
		return patch >= 6
	}
	return false
}
