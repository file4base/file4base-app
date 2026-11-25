package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Roles a server account can hold in the databases it reaches. The role is
// applied to the account's row in each database at sign-in, so it is the
// account's role everywhere it signs in.
const (
	ServerAccountRoleOwner = "owner"
	ServerAccountRoleAdmin = "admin"
	ServerAccountRoleUser  = "user"
)

// normalizeServerAccountRole returns the role to store: an empty role is a
// plain user, and anything but owner, admin or user is refused.
func normalizeServerAccountRole(role string) (string, error) {
	switch strings.TrimSpace(role) {
	case "":
		return ServerAccountRoleUser, nil
	case ServerAccountRoleOwner, ServerAccountRoleAdmin, ServerAccountRoleUser:
		return strings.TrimSpace(role), nil
	default:
		return "", invalidServerAccount("role must be owner, admin or user")
	}
}

// SetServerAccountRole changes the role an account holds in the databases it
// reaches. It takes effect at the account's next sign-in in each database;
// sessions already open are revoked by the stamp check.
func (s *Store) SetServerAccountRole(ctx context.Context, name, role string) error {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return ErrServerAccountNotFound
	}
	role, err = normalizeServerAccountRole(role)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE server_accounts SET role = ?, updated_at = ? WHERE name_key = ?`,
		role, serverAccountNowText(), key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrServerAccountNotFound
	}
	return nil
}

// serverAccountStamp fingerprints the state a session was opened with. Any
// change to the password hash, role, active flag or super-owner flag changes
// it, so the session is refused on its next request.
func serverAccountStamp(passwordHash, role string, active, superOwner, allDatabases bool) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("server\x00%s\x00%s\x00%t\x00%t\x00%t",
		passwordHash, role, active, superOwner, allDatabases)))
	return hex.EncodeToString(sum[:])
}

// ServerAccountSessionStamp returns the stamp of an account as stored now for
// a session in database. It fails with ErrServerAccountNotFound when the
// account is gone, disabled, or does not reach the database, so a session
// opened before any of those stops working on its next request.
func (s *Store) ServerAccountSessionStamp(ctx context.Context, name, database string) (string, error) {
	_, key, err := normalizeServerAccountName(name)
	if err != nil {
		return "", ErrServerAccountNotFound
	}
	var (
		hash, role            string
		active, super, allDBs int
	)
	err = s.db.QueryRowContext(ctx,
		`SELECT password_hash, role, is_active, super_owner, all_databases FROM server_accounts WHERE name_key = ?`,
		key).Scan(&hash, &role, &active, &super, &allDBs)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrServerAccountNotFound
		}
		return "", err
	}
	if active == 0 {
		return "", ErrServerAccountNotFound
	}
	if allDBs == 0 {
		var listed int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM server_account_databases WHERE name_key = ? AND database_name = ?`,
			key, database).Scan(&listed); err != nil {
			return "", err
		}
		if listed == 0 {
			return "", ErrServerAccountNotFound
		}
	}
	return serverAccountStamp(hash, role, true, super != 0, allDBs != 0), nil
}

// ensureServerAccountRoleColumn adds the role column to a server_accounts
// table created before accounts carried a role. Existing accounts are users.
func (s *Store) ensureServerAccountRoleColumn(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM pragma_table_info('server_accounts') WHERE name = 'role'`).Scan(&n); err != nil {
		return fmt.Errorf("failed reading server account columns: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`ALTER TABLE server_accounts ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`); err != nil {
		return fmt.Errorf("failed adding server account role: %w", err)
	}
	return nil
}
