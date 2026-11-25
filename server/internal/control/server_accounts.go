package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Server accounts are identities kept at server level. Their names carry no
// "@" suffix (directory accounts will be name@domain), are unique across the
// server, and compare without regard to case. An account either reaches every
// database or only the databases listed for it.

// ErrServerAccountNotFound is returned when no server account has the name.
var ErrServerAccountNotFound = errors.New("server account not found")

// ErrServerAccountExists is returned when a name is already taken, ignoring case.
var ErrServerAccountExists = errors.New("server account name already in use")

// ErrInvalidServerAccountName is returned for an empty name, one longer than
// the limit, or one containing "@" or whitespace.
var ErrInvalidServerAccountName = errors.New("invalid server account name")

// ErrLastSuperOwner is returned when an operation would leave the server
// without an active super-owner.
var ErrLastSuperOwner = errors.New("the last active super-owner cannot be disabled or deleted")

// ErrInvalidServerAccount is returned, wrapped with the reason, when a request
// is not valid: a missing or over-long password, or a scope that does not fit
// the all-databases flag.
var ErrInvalidServerAccount = errors.New("invalid server account")

func invalidServerAccount(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidServerAccount, reason)
}

// ErrInvalidCredentials is returned for an unknown account, a wrong password
// or a disabled account, so a caller cannot tell them apart.
var ErrInvalidCredentials = errors.New("invalid credentials")

const maxServerAccountNameLength = 128

// ServerAccount is a server-level identity and the databases it reaches.
type ServerAccount struct {
	Name         string    `json:"name"`
	Active       bool      `json:"active"`
	SuperOwner   bool      `json:"super_owner"`
	Role         string    `json:"role"`
	AllDatabases bool      `json:"all_databases"`
	Databases    []string  `json:"databases,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// maxServerAccountPasswordBytes is bcrypt's input limit. Longer passwords would
// be refused by the hash with an unclear error, so they are refused here.
const maxServerAccountPasswordBytes = 72

// timingHash is computed once, on first use, so that a failed lookup costs as
// much as a real password check without slowing every program that links this package.
var (
	timingHashOnce sync.Once
	timingHash     string
)

// burnServerAccountCheck spends the time of a real bcrypt comparison.
func burnServerAccountCheck(password string) {
	timingHashOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("file4base-server-account-timing"), bcrypt.DefaultCost)
		if err != nil {
			panic(fmt.Sprintf("control: bcrypt unavailable: %v", err))
		}
		timingHash = string(hash)
	})
	_ = bcrypt.CompareHashAndPassword([]byte(timingHash), []byte(password))
}

func validateServerAccountPassword(password string) error {
	if password == "" {
		return invalidServerAccount("password is required")
	}
	if len(password) > maxServerAccountPasswordBytes {
		return invalidServerAccount(fmt.Sprintf("password is longer than %d bytes", maxServerAccountPasswordBytes))
	}
	return nil
}

func normalizeServerAccountName(name string) (string, string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > maxServerAccountNameLength {
		return "", "", ErrInvalidServerAccountName
	}
	if strings.ContainsAny(trimmed, "@ \t\r\n") {
		return "", "", ErrInvalidServerAccountName
	}
	return trimmed, strings.ToLower(trimmed), nil
}

func normalizeDatabases(databases []string) []string {
	seen := make(map[string]struct{}, len(databases))
	out := make([]string, 0, len(databases))
	for _, db := range databases {
		name := strings.TrimSpace(db)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func validateScope(allDatabases bool, databases []string) ([]string, error) {
	clean := normalizeDatabases(databases)
	if allDatabases && len(clean) > 0 {
		return nil, invalidServerAccount("an account reaching all databases lists no databases")
	}
	if !allDatabases && len(clean) == 0 {
		return nil, invalidServerAccount("an account reaching some databases must list at least one")
	}
	return clean, nil
}

func serverAccountNowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// queryer is satisfied by *sql.DB and *sql.Tx, so reads can run inside a transaction.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// CreateServerAccount adds an account. The password is stored only as a bcrypt hash.
func (s *Store) CreateServerAccount(ctx context.Context, name, password, role string, superOwner, allDatabases bool, databases []string) error {
	display, key, err := normalizeServerAccountName(name)
	if err != nil {
		return err
	}
	role, err = normalizeServerAccountRole(role)
	if err != nil {
		return err
	}
	if err := validateServerAccountPassword(password); err != nil {
		return err
	}
	clean, err := validateScope(allDatabases, databases)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed hashing password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM server_accounts WHERE name_key = ?`, key).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return ErrServerAccountExists
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO server_accounts (name_key, name, password_hash, is_active, super_owner, all_databases, updated_at, role)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?)`,
		key, display, string(hash), boolToInt(superOwner), boolToInt(allDatabases), serverAccountNowText(), role,
	); err != nil {
		return err
	}
	for _, db := range clean {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO server_account_databases (name_key, database_name) VALUES (?, ?)`, key, db); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetServerAccount returns one account by name, ignoring case.
func (s *Store) GetServerAccount(ctx context.Context, name string) (*ServerAccount, error) {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return nil, ErrServerAccountNotFound
	}
	return s.loadServerAccount(ctx, s.db, key)
}

// ListServerAccounts returns every account, ordered by name.
func (s *Store) ListServerAccounts(ctx context.Context) ([]ServerAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name_key FROM server_accounts ORDER BY name_key`)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make([]ServerAccount, 0, len(keys))
	for _, key := range keys {
		acc, err := s.loadServerAccount(ctx, s.db, key)
		if err != nil {
			return nil, err
		}
		out = append(out, *acc)
	}
	return out, nil
}

// SetServerAccountActive enables or disables an account. Disabling the last
// active super-owner is refused.
func (s *Store) SetServerAccountActive(ctx context.Context, name string, active bool) error {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return ErrServerAccountNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	acc, err := s.loadServerAccount(ctx, tx, key)
	if err != nil {
		return err
	}
	if !active && acc.Active && acc.SuperOwner {
		if err := ensureAnotherActiveSuperOwner(ctx, tx, key); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE server_accounts SET is_active = ?, updated_at = ? WHERE name_key = ?`,
		boolToInt(active), serverAccountNowText(), key); err != nil {
		return err
	}
	return tx.Commit()
}

// SetServerAccountScope changes which databases an account reaches.
func (s *Store) SetServerAccountScope(ctx context.Context, name string, allDatabases bool, databases []string) error {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return ErrServerAccountNotFound
	}
	clean, err := validateScope(allDatabases, databases)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := s.loadServerAccount(ctx, tx, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE server_accounts SET all_databases = ?, updated_at = ? WHERE name_key = ?`,
		boolToInt(allDatabases), serverAccountNowText(), key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_account_databases WHERE name_key = ?`, key); err != nil {
		return err
	}
	for _, db := range clean {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO server_account_databases (name_key, database_name) VALUES (?, ?)`, key, db); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetServerAccountPassword replaces the password of an account.
func (s *Store) SetServerAccountPassword(ctx context.Context, name, password string) error {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return ErrServerAccountNotFound
	}
	if err := validateServerAccountPassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed hashing password: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE server_accounts SET password_hash = ?, updated_at = ? WHERE name_key = ?`,
		string(hash), serverAccountNowText(), key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrServerAccountNotFound
	}
	return nil
}

// DeleteServerAccount removes an account and its database scope. Deleting the
// last active super-owner is refused.
func (s *Store) DeleteServerAccount(ctx context.Context, name string) error {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return ErrServerAccountNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	acc, err := s.loadServerAccount(ctx, tx, key)
	if err != nil {
		return err
	}
	if acc.Active && acc.SuperOwner {
		if err := ensureAnotherActiveSuperOwner(ctx, tx, key); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_accounts WHERE name_key = ?`, key); err != nil {
		return err
	}
	return tx.Commit()
}

// AuthenticateServerAccount checks a name and password. An unknown name costs
// the same bcrypt comparison as a wrong password, and every failure, including a
// disabled account, returns ErrInvalidCredentials.
func (s *Store) AuthenticateServerAccount(ctx context.Context, name, password string) (*ServerAccount, error) {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		burnServerAccountCheck(password)
		return nil, ErrInvalidCredentials
	}
	var hash string
	var active int
	err = s.db.QueryRowContext(ctx,
		`SELECT password_hash, is_active FROM server_accounts WHERE name_key = ?`, key).Scan(&hash, &active)
	if errors.Is(err, sql.ErrNoRows) {
		burnServerAccountCheck(password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if active == 0 {
		return nil, ErrInvalidCredentials
	}
	return s.loadServerAccount(ctx, s.db, key)
}

func (s *Store) loadServerAccount(ctx context.Context, q queryer, key string) (*ServerAccount, error) {
	var (
		name                  string
		active, super, allDBs int
		updated, role         string
	)
	err := q.QueryRowContext(ctx, `
		SELECT name, is_active, super_owner, all_databases, updated_at, role
		FROM server_accounts WHERE name_key = ?`, key).
		Scan(&name, &active, &super, &allDBs, &updated, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrServerAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	acc := &ServerAccount{
		Name:         name,
		Active:       active != 0,
		SuperOwner:   super != 0,
		AllDatabases: allDBs != 0,
		Role:         role,
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		acc.UpdatedAt = t
	}
	rows, err := q.QueryContext(ctx,
		`SELECT database_name FROM server_account_databases WHERE name_key = ? ORDER BY database_name`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var db string
		if err := rows.Scan(&db); err != nil {
			return nil, err
		}
		acc.Databases = append(acc.Databases, db)
	}
	return acc, rows.Err()
}

// ensureAnotherActiveSuperOwner fails unless an active super-owner other than
// key exists, so the server can always be administered.
func ensureAnotherActiveSuperOwner(ctx context.Context, tx *sql.Tx, key string) error {
	var others int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM server_accounts
		WHERE super_owner = 1 AND is_active = 1 AND name_key <> ?`, key).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return ErrLastSuperOwner
	}
	return nil
}
