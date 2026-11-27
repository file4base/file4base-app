package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrProjectNotFound is returned when a project/database entry is not registered in the control store.
var ErrProjectNotFound = errors.New("project not found in control store")

// ProjectRecord links a logical File4Base project/solution name to its physical
// database, engine, design lock state, and project-level options.
type ProjectRecord struct {
	Database    string                 `json:"database"`
	Name        string                 `json:"name"`
	Engine      string                 `json:"engine"`
	DesignLocked bool                  `json:"design_locked"`
	FileOptions map[string]interface{} `json:"file_options,omitempty"`
	PageSetup   map[string]interface{} `json:"page_setup,omitempty"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// UserRecord mirrors an account registered for a project database in the API control store.
type UserRecord struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	IsActive bool   `json:"is_active"`
}

// TablePermissionRecord stores per-table access and modification permissions for a user in a project.
type TablePermissionRecord struct {
	UserID          string `json:"user_id"`
	TableName       string `json:"table_name"`
	AccessLevel     string `json:"access_level"` // "read_write", "read_only", "none"
	CanCreate       bool   `json:"can_create"`
	CanEdit         bool   `json:"can_edit"`
	CanDelete       bool   `json:"can_delete"`
	CanModifySchema bool   `json:"can_modify_schema"`
}

// LayoutPermissionRecord stores per-layout access permissions for a user in a project.
type LayoutPermissionRecord struct {
	UserID      string `json:"user_id"`
	LayoutID    string `json:"layout_id"`
	LayoutName  string `json:"layout_name,omitempty"`
	AccessLevel string `json:"access_level"` // "read_write", "read_only", "none"
}

// Store is the API's embedded SQLite control store that persists the
// relationship between projects, databases, project options, users, and their
// table and layout permissions.
type Store struct {
	db *sql.DB
}

// NewStore opens or initializes the SQLite control store at dbPath.
// Pass ":memory:" (or "") for an in-memory SQLite database.
func NewStore(dbPath string) (*Store, error) {
	cleanPath := strings.TrimSpace(dbPath)
	if cleanPath == "" {
		cleanPath = ":memory:"
	}
	if cleanPath != ":memory:" && !strings.HasPrefix(cleanPath, "file:") {
		dir := filepath.Dir(cleanPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("failed creating control store directory: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed opening sqlite control store: %w", err)
	}
	// SQLite handles concurrent writes best with a single writer connection and WAL/busy_timeout.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	stmts := []string{
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS projects (
			database_name TEXT PRIMARY KEY,
			project_name TEXT NOT NULL,
			engine TEXT NOT NULL DEFAULT 'postgres',
			design_locked INTEGER NOT NULL DEFAULT 0,
			file_options_json TEXT NOT NULL DEFAULT '{}',
			page_setup_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS project_users (
			database_name TEXT NOT NULL,
			user_id TEXT NOT NULL,
			username TEXT NOT NULL,
			role TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (database_name, user_id)
		);`,
		`CREATE TABLE IF NOT EXISTS project_table_permissions (
			database_name TEXT NOT NULL,
			user_id TEXT NOT NULL,
			table_name TEXT NOT NULL,
			access_level TEXT NOT NULL DEFAULT 'read_write',
			can_create INTEGER NOT NULL DEFAULT 1,
			can_edit INTEGER NOT NULL DEFAULT 1,
			can_delete INTEGER NOT NULL DEFAULT 1,
			can_modify_schema INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (database_name, user_id, table_name)
		);`,
		`CREATE TABLE IF NOT EXISTS project_layout_permissions (
			database_name TEXT NOT NULL,
			user_id TEXT NOT NULL,
			layout_id TEXT NOT NULL,
			layout_name TEXT NOT NULL DEFAULT '',
			access_level TEXT NOT NULL DEFAULT 'read_write',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (database_name, user_id, layout_id)
		);`,
		// SMTP profiles of the Send Mail script step (#133): one per
		// database, and the server-wide one under the empty name. The
		// profile names the environment variable of its password and never
		// holds the password itself.
		`CREATE TABLE IF NOT EXISTS mail_profiles (
			database_name TEXT PRIMARY KEY,
			profile_json TEXT NOT NULL,
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,
		// The solutions a server administrator allowed to send through the
		// server-wide SMTP profile. None is by default: anyone may create a
		// solution, and the server's mail account is not theirs.
		`CREATE TABLE IF NOT EXISTS mail_server_profile_grants (
			database_name TEXT PRIMARY KEY,
			granted_by TEXT NOT NULL DEFAULT '',
			granted_at TEXT NOT NULL
		);`,
		// Server accounts (#server-accounts): identities kept at server level,
		// unique across the server and compared without case. The name_key is the
		// lowercase name. The password is only a bcrypt hash.
		`CREATE TABLE IF NOT EXISTS server_accounts (
			name_key TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			super_owner INTEGER NOT NULL DEFAULT 0,
			all_databases INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);`,
		// The databases a server account reaches, when it does not reach all of them.
		`CREATE TABLE IF NOT EXISTS server_account_databases (
			name_key TEXT NOT NULL REFERENCES server_accounts(name_key) ON DELETE CASCADE,
			database_name TEXT NOT NULL,
			PRIMARY KEY (name_key, database_name)
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed initializing control store schema: %w", err)
		}
	}
	return s.initScheduleSchema(ctx)
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// UpsertProject creates or updates a project ↔ database record and its options.
func (s *Store) UpsertProject(ctx context.Context, p ProjectRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	dbName := strings.TrimSpace(p.Database)
	if dbName == "" {
		return errors.New("database name is required")
	}
	projName := strings.TrimSpace(p.Name)
	if projName == "" {
		projName = dbName
	}
	engine := strings.TrimSpace(p.Engine)
	if engine == "" {
		engine = "postgres"
	}

	// Preserve existing options if caller passed nil maps on a partial update
	existing, err := s.GetProject(ctx, dbName)
	if err == nil && existing != nil {
		if p.FileOptions == nil {
			p.FileOptions = existing.FileOptions
		}
		if p.PageSetup == nil {
			p.PageSetup = existing.PageSetup
		}
		if strings.TrimSpace(p.Name) == "" {
			projName = existing.Name
		}
	}

	fileOptsBytes, err := json.Marshal(nonNilMap(p.FileOptions))
	if err != nil {
		fileOptsBytes = []byte("{}")
	}
	pageSetupBytes, err := json.Marshal(nonNilMap(p.PageSetup))
	if err != nil {
		pageSetupBytes = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lockedInt := boolToInt(p.DesignLocked)

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO projects (database_name, project_name, engine, design_locked, file_options_json, page_setup_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(database_name) DO UPDATE SET
			project_name = excluded.project_name,
			engine = excluded.engine,
			design_locked = excluded.design_locked,
			file_options_json = excluded.file_options_json,
			page_setup_json = excluded.page_setup_json,
			updated_at = excluded.updated_at
	`, dbName, projName, engine, lockedInt, string(fileOptsBytes), string(pageSetupBytes), now)
	return err
}

// GetProject fetches a single project record by its physical database name.
func (s *Store) GetProject(ctx context.Context, databaseName string) (*ProjectRecord, error) {
	if s == nil || s.db == nil {
		return nil, ErrProjectNotFound
	}
	var rec ProjectRecord
	var lockedInt int
	var fileOptsRaw, pageSetupRaw, updatedRaw string
	err := s.db.QueryRowContext(ctx, `
		SELECT database_name, project_name, engine, design_locked, file_options_json, page_setup_json, updated_at
		FROM projects
		WHERE database_name = ?
	`, strings.TrimSpace(databaseName)).Scan(
		&rec.Database,
		&rec.Name,
		&rec.Engine,
		&lockedInt,
		&fileOptsRaw,
		&pageSetupRaw,
		&updatedRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.DesignLocked = lockedInt != 0
	rec.FileOptions = parseJSONMap(fileOptsRaw)
	rec.PageSetup = parseJSONMap(pageSetupRaw)
	if t, err := time.Parse(time.RFC3339Nano, updatedRaw); err == nil {
		rec.UpdatedAt = t
	}
	return &rec, nil
}

// ListProjects returns all registered projects ordered by database name.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT database_name, project_name, engine, design_locked, file_options_json, page_setup_json, updated_at
		FROM projects
		ORDER BY database_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ProjectRecord, 0)
	for rows.Next() {
		var rec ProjectRecord
		var lockedInt int
		var fileOptsRaw, pageSetupRaw, updatedRaw string
		if err := rows.Scan(
			&rec.Database,
			&rec.Name,
			&rec.Engine,
			&lockedInt,
			&fileOptsRaw,
			&pageSetupRaw,
			&updatedRaw,
		); err != nil {
			return nil, err
		}
		rec.DesignLocked = lockedInt != 0
		rec.FileOptions = parseJSONMap(fileOptsRaw)
		rec.PageSetup = parseJSONMap(pageSetupRaw)
		if t, err := time.Parse(time.RFC3339Nano, updatedRaw); err == nil {
			rec.UpdatedAt = t
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteProject removes a project and all its associated users and permissions from the control store.
func (s *Store) DeleteProject(ctx context.Context, databaseName string) error {
	if s == nil || s.db == nil {
		return nil
	}
	dbName := strings.TrimSpace(databaseName)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, q := range []string{
		`DELETE FROM project_table_permissions WHERE database_name = ?`,
		`DELETE FROM project_layout_permissions WHERE database_name = ?`,
		`DELETE FROM project_users WHERE database_name = ?`,
		// The server-wide SMTP profile is stored under the empty name.
		`DELETE FROM mail_profiles WHERE database_name = ? AND database_name <> ''`,
		`DELETE FROM mail_server_profile_grants WHERE database_name = ?`,
		`DELETE FROM projects WHERE database_name = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, dbName); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SyncUsers replaces the user directory snapshot for databaseName in the control store.
func (s *Store) SyncUsers(ctx context.Context, databaseName string, users []UserRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	dbName := strings.TrimSpace(databaseName)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM project_users WHERE database_name = ?`, dbName); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, u := range users {
		if strings.TrimSpace(u.ID) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_users (database_name, user_id, username, role, is_active, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, dbName, u.ID, u.Username, u.Role, boolToInt(u.IsActive), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListUsers returns all users synced for a database in the control store.
func (s *Store) ListUsers(ctx context.Context, databaseName string) ([]UserRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, username, role, is_active
		FROM project_users
		WHERE database_name = ?
		ORDER BY username ASC
	`, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]UserRecord, 0)
	for rows.Next() {
		var u UserRecord
		var activeInt int
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &activeInt); err != nil {
			return nil, err
		}
		u.IsActive = activeInt != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetTablePermissions replaces the table permissions of userID in databaseName.
func (s *Store) SetTablePermissions(ctx context.Context, databaseName, userID string, perms []TablePermissionRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	dbName := strings.TrimSpace(databaseName)
	uid := strings.TrimSpace(userID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM project_table_permissions WHERE database_name = ? AND user_id = ?
	`, dbName, uid); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seen := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		tName := strings.TrimSpace(p.TableName)
		if tName == "" {
			continue
		}
		if _, dup := seen[tName]; dup {
			continue
		}
		seen[tName] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_table_permissions (
				database_name, user_id, table_name, access_level,
				can_create, can_edit, can_delete, can_modify_schema, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, dbName, uid, tName, p.AccessLevel,
			boolToInt(p.CanCreate), boolToInt(p.CanEdit), boolToInt(p.CanDelete), boolToInt(p.CanModifySchema), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetTablePermissions returns the table permissions stored for userID in databaseName.
func (s *Store) GetTablePermissions(ctx context.Context, databaseName, userID string) ([]TablePermissionRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, table_name, access_level, can_create, can_edit, can_delete, can_modify_schema
		FROM project_table_permissions
		WHERE database_name = ? AND user_id = ?
		ORDER BY table_name ASC
	`, strings.TrimSpace(databaseName), strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TablePermissionRecord, 0)
	for rows.Next() {
		var p TablePermissionRecord
		var cCreate, cEdit, cDelete, cModify int
		if err := rows.Scan(&p.UserID, &p.TableName, &p.AccessLevel, &cCreate, &cEdit, &cDelete, &cModify); err != nil {
			return nil, err
		}
		p.CanCreate = cCreate != 0
		p.CanEdit = cEdit != 0
		p.CanDelete = cDelete != 0
		p.CanModifySchema = cModify != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetLayoutPermissions replaces the layout permissions of userID in databaseName.
func (s *Store) SetLayoutPermissions(ctx context.Context, databaseName, userID string, perms []LayoutPermissionRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	dbName := strings.TrimSpace(databaseName)
	uid := strings.TrimSpace(userID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM project_layout_permissions WHERE database_name = ? AND user_id = ?
	`, dbName, uid); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seen := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		lid := strings.TrimSpace(p.LayoutID)
		if lid == "" {
			continue
		}
		if _, dup := seen[lid]; dup {
			continue
		}
		seen[lid] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_layout_permissions (
				database_name, user_id, layout_id, layout_name, access_level, updated_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`, dbName, uid, lid, p.LayoutName, p.AccessLevel, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetLayoutPermissions returns the layout permissions stored for userID in databaseName.
func (s *Store) GetLayoutPermissions(ctx context.Context, databaseName, userID string) ([]LayoutPermissionRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, layout_id, layout_name, access_level
		FROM project_layout_permissions
		WHERE database_name = ? AND user_id = ?
		ORDER BY layout_id ASC
	`, strings.TrimSpace(databaseName), strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LayoutPermissionRecord, 0)
	for rows.Next() {
		var p LayoutPermissionRecord
		if err := rows.Scan(&p.UserID, &p.LayoutID, &p.LayoutName, &p.AccessLevel); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nonNilMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

func parseJSONMap(raw string) map[string]interface{} {
	out := make(map[string]interface{})
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return make(map[string]interface{})
	}
	return out
}

// ErrMailProfileNotFound is returned when no SMTP profile is stored for a
// database (or server-wide).
var ErrMailProfileNotFound = errors.New("no SMTP profile")

// MailProfileRecord is a stored SMTP profile. Database is empty for the
// server-wide profile. Profile is the profile's JSON, which holds no secret.
type MailProfileRecord struct {
	Database  string          `json:"database"`
	Profile   json.RawMessage `json:"profile"`
	UpdatedBy string          `json:"updated_by"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// PutMailProfile stores the SMTP profile of a database ("" for the
// server-wide one), replacing any.
func (s *Store) PutMailProfile(ctx context.Context, rec MailProfileRecord) error {
	if s == nil || s.db == nil {
		return errors.New("no control store")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mail_profiles (database_name, profile_json, updated_by, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(database_name) DO UPDATE SET
			profile_json = excluded.profile_json,
			updated_by = excluded.updated_by,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(rec.Database), string(rec.Profile), rec.UpdatedBy, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// GetMailProfile fetches the SMTP profile of a database ("" for the
// server-wide one).
func (s *Store) GetMailProfile(ctx context.Context, databaseName string) (*MailProfileRecord, error) {
	if s == nil || s.db == nil {
		return nil, ErrMailProfileNotFound
	}
	var rec MailProfileRecord
	var raw, updated string
	err := s.db.QueryRowContext(ctx, `
		SELECT database_name, profile_json, updated_by, updated_at FROM mail_profiles WHERE database_name = ?
	`, strings.TrimSpace(databaseName)).Scan(&rec.Database, &raw, &rec.UpdatedBy, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMailProfileNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.Profile = json.RawMessage(raw)
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		rec.UpdatedAt = t
	}
	return &rec, nil
}

// ListMailProfiles returns every stored SMTP profile, the server-wide one
// first.
func (s *Store) ListMailProfiles(ctx context.Context) ([]MailProfileRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT database_name, profile_json, updated_by, updated_at FROM mail_profiles ORDER BY database_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MailProfileRecord
	for rows.Next() {
		var rec MailProfileRecord
		var raw, updated string
		if err := rows.Scan(&rec.Database, &raw, &rec.UpdatedBy, &updated); err != nil {
			return nil, err
		}
		rec.Profile = json.RawMessage(raw)
		if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
			rec.UpdatedAt = t
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteMailProfile removes the SMTP profile of a database ("" for the
// server-wide one). Removing one that is not there is not an error.
func (s *Store) DeleteMailProfile(ctx context.Context, databaseName string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM mail_profiles WHERE database_name = ?`, strings.TrimSpace(databaseName))
	return err
}

// SetServerMailGrant allows (or stops) a database sending through the
// server-wide SMTP profile.
func (s *Store) SetServerMailGrant(ctx context.Context, databaseName, by string, allowed bool) error {
	if s == nil || s.db == nil {
		return errors.New("control store is not available")
	}
	name := strings.TrimSpace(databaseName)
	if name == "" {
		return errors.New("a database name is required")
	}
	if !allowed {
		_, err := s.db.ExecContext(ctx, `DELETE FROM mail_server_profile_grants WHERE database_name = ?`, name)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mail_server_profile_grants (database_name, granted_by, granted_at)
		VALUES (?, ?, ?)
		ON CONFLICT(database_name) DO UPDATE SET granted_by = excluded.granted_by, granted_at = excluded.granted_at
	`, name, by, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ServerMailGranted tells whether a database may send through the
// server-wide SMTP profile.
func (s *Store) ServerMailGranted(ctx context.Context, databaseName string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mail_server_profile_grants WHERE database_name = ?`, strings.TrimSpace(databaseName)).Scan(&n)
	return n > 0, err
}

// ListServerMailGrants returns the databases allowed to send through the
// server-wide SMTP profile, by name.
func (s *Store) ListServerMailGrants(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT database_name FROM mail_server_profile_grants ORDER BY database_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
