package dbal

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	fs, unsafe := filesystemOf(dir)
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
func SQLiteFilesystem(dir string) (string, bool) { return filesystemOf(dir) }

// filesystemOf is sqliteFilesystem; a test can stand in for the mounts.
var filesystemOf = sqliteFilesystem

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
		// A name is lower case, as CreateDatabase makes it: a file named otherwise
		// was not made by File4Base and is no database of it.
		if IsProtectedDatabase(name) || !validDatabaseName.MatchString(name) || name != strings.ToLower(name) {
			continue
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

// CreateSQLiteDatabase creates an empty database file in WAL mode. It
// returns ErrDatabaseExists when the file is already there. The database is
// made and synced under a temporary name, then published under its own
// (publishSQLiteFile), so a crash never leaves an empty file under the
// name, which would list as a database and refuse a new attempt.
func CreateSQLiteDatabase(ctx context.Context, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return ErrDatabaseExists
	}
	if err := CheckSQLiteStorage(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := exclusiveTempFile(path, "create")
	if err != nil {
		return err
	}
	defer removeSQLiteFiles(tmp)
	db, err := openSQLite(ctx, tmp)
	if err != nil {
		return err
	}
	// An empty file has no header until the first write; the journal mode is
	// persistent only once it is written.
	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 0"); err != nil {
		_ = db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	return publishSQLiteFile(tmp, path)
}

// exclusiveTempFile creates an empty file, readable by its owner only, next
// to path, whose name no database or backup has. Its prefix is short, so a
// long database name still fits the file system's limit.
func exclusiveTempFile(path, purpose string) (string, error) {
	f, err := createTempFile(path, purpose)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func createTempFile(path, purpose string) (*os.File, error) {
	return os.CreateTemp(filepath.Dir(path), ".f4b-"+purpose+"-*")
}

// publishSQLiteFile syncs a closed database file and gives it the name
// dest, which must not exist; tmp keeps its own name until it is removed.
// A link refuses to replace a file, where a rename would. A -wal, -shm or
// -journal file already next to dest is refused: SQLite would read it as
// the new database's own, and replay what a dropped or crashed database had
// written into it.
func publishSQLiteFile(tmp, dest string) error {
	if err := syncFile(tmp); err != nil {
		return err
	}
	if side := sqliteSidecar(dest); side != "" {
		return fmt.Errorf("%s is left from an earlier database of that name: move it away first", side)
	}
	if err := os.Link(tmp, dest); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrDatabaseExists
		}
		return err
	}
	if err := syncDir(filepath.Dir(dest)); err != nil {
		// Reported as failed, so it is not left published: a retry would
		// find the name taken.
		_ = os.Remove(dest)
		return err
	}
	return nil
}

// sqliteSidecar is a file SQLite keeps next to the database path, when one
// is there.
func sqliteSidecar(path string) string {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return path + suffix
		}
	}
	return ""
}

// removeSQLiteFiles deletes a database file and the files SQLite keeps next
// to it. It stops at the first file it cannot remove.
//
// The database file is first renamed out of its name: a -wal may hold
// commits the file does not, so the -wal goes only once the database can
// no longer be opened under the name (a database whose rename fails is
// left whole). A database created under the name meanwhile is refused
// while the old -wal is there (publishSQLiteFile).
func removeSQLiteFiles(path string) error {
	gone := ""
	if _, err := os.Lstat(path); err == nil {
		f, err := createTempFile(path, "dropped")
		if err != nil {
			return err
		}
		gone = f.Name()
		_ = f.Close()
		if err := os.Rename(path, gone); err != nil {
			_ = os.Remove(gone)
			return err
		}
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	forgetWriterQueue(path)
	if gone != "" {
		return os.Remove(gone)
	}
	return nil
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
	// VACUUM INTO writes into an empty file that exists; this one is the
	// owner's only, as the database is, whatever the umask.
	tmp, err := exclusiveTempFile(dest, "backup")
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer removeSQLiteFiles(tmp)
	if _, err := db.ExecContext(ctx, "VACUUM INTO $1", tmp); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := checkSQLiteBackup(ctx, tmp); err != nil {
		return fmt.Errorf("backup check: %w", err)
	}
	if err := publishSQLiteFile(tmp, dest); err != nil {
		if errors.Is(err, ErrDatabaseExists) {
			err = os.ErrExist
		}
		return fmt.Errorf("backup publish: %w", err)
	}
	return nil
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
	if side := sqliteSidecar(backup); side != "" {
		return fmt.Errorf("restore: %s is next to the backup: a backup is the single file sqlite-backup writes, and a copy of a live database without its WAL may miss what was committed last", side)
	}
	// The copy is written through the descriptor that created the file.
	out, err := createTempFile(dest, "restore")
	if err != nil {
		return err
	}
	tmp := out.Name()
	defer removeSQLiteFiles(tmp)
	if err := copyInto(out, backup); err != nil {
		return err
	}
	if err := checkSQLiteBackup(ctx, tmp); err != nil {
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
	return publishSQLiteFile(tmp, dest)
}

// checkSQLiteBackup checks that a file is a whole SQLite database: its
// header, the size the header gives, and quick_check. SQLite reads an empty
// or truncated file as a database of its own, which quick_check passes.
func checkSQLiteBackup(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	var h [100]byte
	if _, err := io.ReadFull(f, h[:]); err != nil || string(h[:16]) != "SQLite format 3\x00" {
		return fmt.Errorf("%s is not a SQLite database", path)
	}
	// A backup (VACUUM INTO) is in rollback-journal mode, versions 1: a
	// database file in WAL mode (2) is a copy of a live database, whose
	// last commits may be in a -wal that is not there.
	if h[18] != 1 || h[19] != 1 {
		return fmt.Errorf("%s is a database file in WAL mode, not a backup: take one with sqlite-backup", path)
	}
	pageSize := int64(binary.BigEndian.Uint16(h[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	pages := int64(binary.BigEndian.Uint32(h[28:32]))
	// The header's page count holds when it was written with the change it
	// counts (the version-valid-for number equals the change counter).
	if pages == 0 || string(h[24:28]) != string(h[92:96]) || info.Size() < pages*pageSize {
		return fmt.Errorf("%s is incomplete: %d bytes for a database of %d pages of %d bytes", path, info.Size(), pages, pageSize)
	}
	return quickCheckFile(ctx, path)
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

// copyInto copies src into out, syncs and closes it.
func copyInto(out *os.File, src string) (err error) {
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
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
