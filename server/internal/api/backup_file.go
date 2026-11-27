package api

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// A backup file (.f4b.json) is one JSON object. Its header fields come first,
// so that listing backups and checking who may use one read only the start
// of the file, and its records are written as they are read, so that taking
// a backup does not hold the database in memory (#203).

// backupHeader is what a backup says about itself.
type backupHeader struct {
	ID        string `json:"id"`
	Database  string `json:"database"`
	Engine    string `json:"engine,omitempty"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	Notes     string `json:"notes"`
	Type      string `json:"type"`
	// ScheduleID is the schedule that made a "scheduled" backup (#188).
	ScheduleID  string `json:"schedule_id"`
	TablesCount int    `json:"tables_count"`
}

// backupHeaderOrder is the order the header fields are written in.
var backupHeaderOrder = []string{"id", "database", "engine", "created_at", "created_by", "notes", "type", "schedule_id", "tables_count"}

func (b backupHeader) field(key string) interface{} {
	switch key {
	case "id":
		return b.ID
	case "database":
		return b.Database
	case "engine":
		return b.Engine
	case "created_at":
		return b.CreatedAt
	case "created_by":
		return b.CreatedBy
	case "notes":
		return b.Notes
	case "type":
		return b.Type
	case "schedule_id":
		return b.ScheduleID
	default:
		return b.TablesCount
	}
}

// readBackupHeader reads the header of a backup file. A file written before
// #203 has its fields in alphabetical order, the records among them, and is
// read whole, as it was then.
func readBackupHeader(path string) (backupHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return backupHeader{}, err
	}
	defer f.Close()
	var hdr backupHeader
	dec := json.NewDecoder(bufio.NewReader(f))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return backupHeader{}, errors.New("not a backup file")
	}
	seen := 0
	for dec.More() && seen < len(backupHeaderOrder) {
		tok, err := dec.Token()
		if err != nil {
			return backupHeader{}, err
		}
		key, _ := tok.(string)
		var dst interface{}
		switch key {
		case "id":
			dst = &hdr.ID
		case "database":
			dst = &hdr.Database
		case "engine":
			dst = &hdr.Engine
		case "created_at":
			dst = &hdr.CreatedAt
		case "created_by":
			dst = &hdr.CreatedBy
		case "notes":
			dst = &hdr.Notes
		case "type":
			dst = &hdr.Type
		case "schedule_id":
			dst = &hdr.ScheduleID
		case "tables_count":
			dst = &hdr.TablesCount
		}
		if dst == nil {
			if seen > 0 && hdr.ID != "" {
				// The header of a file that starts with its id, written
				// before a header field was added, ends here.
				break
			}
			// Not a header field: an older file.
			return readWholeBackupHeader(path)
		}
		if err := dec.Decode(dst); err != nil {
			return backupHeader{}, err
		}
		seen++
	}
	hdr.Database = strings.TrimSpace(hdr.Database)
	return hdr, nil
}

func readWholeBackupHeader(path string) (backupHeader, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return backupHeader{}, err
	}
	var hdr backupHeader
	if err := json.Unmarshal(raw, &hdr); err != nil {
		return backupHeader{}, err
	}
	hdr.Database = strings.TrimSpace(hdr.Database)
	return hdr, nil
}

// backupWriter writes the fields after the header.
type backupWriter struct {
	w     *bufio.Writer
	count int
}

func (bw *backupWriter) key(key string) error {
	if bw.count > 0 {
		if _, err := bw.w.WriteString(",\n"); err != nil {
			return err
		}
	}
	bw.count++
	k, _ := json.Marshal(key)
	_, err := fmt.Fprintf(bw.w, "  %s: ", k)
	return err
}

// Field writes a field whose value is marshalled to JSON.
func (bw *backupWriter) Field(key string, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return bw.RawField(key, raw)
}

// RawField writes a field whose value is JSON already.
func (bw *backupWriter) RawField(key string, raw json.RawMessage) error {
	if err := bw.key(key); err != nil {
		return err
	}
	_, err := bw.w.Write(raw)
	return err
}

// Base64Field writes a field whose value is the base64 of what write writes.
func (bw *backupWriter) Base64Field(key string, write func(io.Writer) error) error {
	if err := bw.key(key); err != nil {
		return err
	}
	if err := bw.w.WriteByte('"'); err != nil {
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, bw.w)
	if err := write(enc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return bw.w.WriteByte('"')
}

// writeBackupFile writes a backup file, its header first, then what body
// writes. It is written under a temporary name and renamed once complete, so
// a failed or interrupted backup leaves no file that looks like a backup.
// It returns the size of the file.
func writeBackupFile(path string, hdr backupHeader, body func(*backupWriter) error) (int64, error) {
	partial := path + ".partial"
	// newBackupID reserved the name by creating this file: it is this
	// writer's alone.
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	fail := func(err error) (int64, error) {
		_ = f.Close()
		_ = os.Remove(partial)
		return 0, err
	}
	bw := &backupWriter{w: bufio.NewWriterSize(f, 1<<20)}
	if _, err := bw.w.WriteString("{\n"); err != nil {
		return fail(err)
	}
	for _, key := range backupHeaderOrder {
		if err := bw.Field(key, hdr.field(key)); err != nil {
			return fail(err)
		}
	}
	if err := body(bw); err != nil {
		return fail(err)
	}
	if _, err := bw.w.WriteString("\n}\n"); err != nil {
		return fail(err)
	}
	if err := bw.w.Flush(); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(partial)
		return 0, err
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return 0, err
	}
	// The new name is durable too: a restore relies on its pre-restore
	// backup surviving a crash of the host.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		syncErr := dir.Sync()
		_ = dir.Close()
		if syncErr != nil {
			return 0, syncErr
		}
	}
	return info.Size(), nil
}

// writeRemainingFields writes the fields of an uploaded backup other than its
// header, in a stable order.
func writeRemainingFields(bw *backupWriter, fields map[string]json.RawMessage) error {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		// The header is the one just written. A key spelled like one of its
		// fields in another case would be read as that field by a JSON
		// decoder, and is dropped.
		isHeader := false
		for _, h := range backupHeaderOrder {
			isHeader = isHeader || strings.EqualFold(k, h)
		}
		if !isHeader {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := bw.RawField(k, fields[k]); err != nil {
			return err
		}
	}
	return nil
}

// writeSnapshot backs dbName up to a new file of the backups folder. The
// catalog is brought up to date first (see schema.EnsureCatalogCurrent);
// then everything is read in one read-only snapshot of the database, so the
// design, the accounts and every table's records are as they were at one
// moment even while users keep working (#203). Any part that cannot be read
// fails the backup, rather than leaving one that restores to nothing.
func (h *ServerAdminHandler) writeSnapshot(ctx context.Context, dbName, suffix, createdBy, notes, bkpType string, now time.Time) (BackupRecord, error) {
	return h.writeSnapshotFor(ctx, dbName, suffix, createdBy, notes, bkpType, "", now)
}

// writeSnapshotFor is writeSnapshot for the schedule scheduleID, if any.
func (h *ServerAdminHandler) writeSnapshotFor(ctx context.Context, dbName, suffix, createdBy, notes, bkpType, scheduleID string, now time.Time) (BackupRecord, error) {
	driver, err := h.dbMgr.DriverFor(ctx, dbName)
	if err != nil {
		return BackupRecord{}, err
	}
	live := schema.NewService(driver)
	if !live.HasSystemCatalog(ctx) {
		return BackupRecord{}, fmt.Errorf("%s has no File4Base catalog", dbName)
	}
	if err := live.EnsureCatalogCurrent(ctx); err != nil {
		return BackupRecord{}, fmt.Errorf("the catalog could not be brought up to date: %w", err)
	}

	snap, err := h.dbMgr.OpenSnapshot(ctx, dbName)
	if err != nil {
		return BackupRecord{}, fmt.Errorf("the database could not be read: %w", err)
	}
	defer func() {
		_ = snap.Rollback()
		schema.ForgetPool(snap.DB())
	}()
	if h.dbMgr.Engine() == dbal.EnginePostgres {
		// PostgreSQL shows a snapshot a table rewritten after it began (a
		// field whose type changed) as empty. Locking the tables before the
		// first read, which is when the snapshot begins, makes such a change
		// wait for the backup instead.
		if err := lockTablesForSnapshot(ctx, live, snap); err != nil {
			return BackupRecord{}, err
		}
	}
	svc := schema.NewService(snap)

	tables, err := svc.ListTables(ctx)
	if err != nil {
		return BackupRecord{}, fmt.Errorf("the tables could not be read: %w", err)
	}
	parts := []struct {
		key, what string
		read      func() (interface{}, error)
	}{
		{"table_occurrences", "the table occurrences", func() (interface{}, error) { return svc.ListTableOccurrences(ctx) }},
		{"relationships", "the relationships", func() (interface{}, error) { return svc.ListRelationships(ctx) }},
		{"layouts", "the layouts", func() (interface{}, error) { return svc.ListLayouts(ctx) }},
		{"scripts", "the scripts", func() (interface{}, error) { return svc.ListScripts(ctx) }},
		{"value_lists", "the value lists", func() (interface{}, error) { return svc.ListValueLists(ctx) }},
		{"users", "the accounts", func() (interface{}, error) { return svc.ListUsers(ctx) }},
	}
	values := make([]interface{}, len(parts))
	for i, p := range parts {
		if values[i], err = p.read(); err != nil {
			return BackupRecord{}, fmt.Errorf("%s could not be read: %w", p.what, err)
		}
	}
	solutionBytes, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: dbName})
	if err != nil {
		return BackupRecord{}, fmt.Errorf("the solution could not be exported: %w", err)
	}

	if backupHookForTest != nil {
		backupHookForTest()
	}
	tableData := make(map[string]interface{}, len(tables))
	for _, t := range tables {
		tableData[t.Name] = map[string]interface{}{"metadata": t, "columns": t.Columns}
	}
	backupID, err := h.newBackupID(dbName, now, suffix)
	if err != nil {
		return BackupRecord{}, err
	}
	hdr := backupHeader{
		ID: backupID, Database: dbName, Engine: string(h.dbMgr.Engine()), CreatedAt: now.Format(time.RFC3339),
		CreatedBy: createdBy, Notes: notes, Type: bkpType, ScheduleID: scheduleID, TablesCount: len(tables),
	}
	filename := backupID + ".f4b.json"
	size, err := writeBackupFile(filepath.Join(h.backupsDir, filename), hdr, func(bw *backupWriter) error {
		if err := bw.Field("tables", tableData); err != nil {
			return err
		}
		for i, p := range parts {
			if err := bw.Field(p.key, values[i]); err != nil {
				return err
			}
		}
		if err := bw.Base64Field("solution_bundle_b64", func(w io.Writer) error {
			_, err := w.Write(solutionBytes)
			return err
		}); err != nil {
			return err
		}
		if err := bw.Base64Field("data_bundle_b64", func(w io.Writer) error {
			if err := svc.WriteDatabaseData(ctx, dbName, w); err != nil {
				return fmt.Errorf("the records could not be exported: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		// Everything was read in the one transaction.
		return snap.Check(ctx)
	})
	if err != nil {
		return BackupRecord{}, err
	}
	return BackupRecord{
		ID: backupID, Filename: filename, Database: dbName, CreatedAt: hdr.CreatedAt, CreatedBy: createdBy,
		SizeBytes: size, TablesCount: len(tables), Notes: notes, Type: bkpType, ScheduleID: scheduleID,
	}, nil
}

func lockTablesForSnapshot(ctx context.Context, live *schema.Service, snap *dbal.TxDriver) error {
	tables, err := live.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("the tables could not be read: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}
	names := make([]string, 0, len(tables))
	for _, t := range tables {
		names = append(names, snap.Dialect().QuoteIdentifier(t.Name))
	}
	if _, err := snap.DB().ExecContext(ctx, "LOCK TABLE "+strings.Join(names, ", ")+" IN ACCESS SHARE MODE"); err != nil {
		return fmt.Errorf("the tables could not be locked for the backup: %w", err)
	}
	return nil
}

// backupHookForTest runs, in tests, between reading the design of a backup
// and its records.
var backupHookForTest func()

// newBackupID names a backup of dbName taken at now, and reserves the name
// by creating its temporary file, which writeBackupFile then writes: two
// backups started in the same second never share a file. A suffix tells
// the kinds of backup apart.
func (h *ServerAdminHandler) newBackupID(dbName string, now time.Time, suffix string) (string, error) {
	base := fmt.Sprintf("bkp_%s_%s", dbName, now.Format("20060102_150405"))
	if suffix != "" {
		base += "_" + suffix
	}
	id := base
	for n := 2; n <= 1000; n++ {
		final := filepath.Join(h.backupsDir, id+".f4b.json")
		f, err := os.OpenFile(final+".partial", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case err == nil:
			_ = f.Close()
			_, statErr := os.Stat(final)
			if errors.Is(statErr, os.ErrNotExist) {
				return id, nil
			}
			_ = os.Remove(final + ".partial")
			if statErr != nil {
				return "", fmt.Errorf("the backups folder cannot be used: %w", statErr)
			}
		case !errors.Is(err, os.ErrExist):
			return "", fmt.Errorf("the backups folder cannot be written: %w", err)
		}
		id = fmt.Sprintf("%s_%d", base, n)
	}
	return "", errors.New("no free name for the backup in the backups folder")
}

// removePartialBackups deletes what backups interrupted by a stop of the
// server left behind.
func removePartialBackups(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.f4b.json.partial"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
