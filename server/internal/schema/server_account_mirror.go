package schema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ErrServerAccountNameTaken is returned when a database account of this
// database already has the name of a server account. The two are never merged.
var ErrServerAccountNameTaken = errors.New("a database account with this name already exists")

// serverAccountPasswordMarker is stored instead of a password hash. It is not
// a bcrypt hash, so the database never accepts a password for the mirror: a
// server account signs in through the server only.
const serverAccountPasswordMarker = "!server-account"

// ServerAccountUserID is the id of the sys_users row that stands for a server
// account in a database. It is derived from the name, so it is the same in
// every database and fits the id column.
func ServerAccountUserID(name string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(name))))
	return "srv-" + hex.EncodeToString(sum[:])[:32]
}

// EnsureServerAccountMirror creates or updates the row that stands for a
// server account in this database, with the account's role and super-owner
// flag, and returns its id. It works on PostgreSQL databases only, which is
// the only engine server accounts sign in to.
func (s *Service) EnsureServerAccountMirror(ctx context.Context, name, role string, superOwner bool) (string, error) {
	id := ServerAccountUserID(name)
	db := s.driver.DB()

	var existing string
	err := db.QueryRowContext(ctx,
		`SELECT id FROM sys_users WHERE lower(username) = lower($1)`, name).Scan(&existing)
	now := time.Now().UTC()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = db.ExecContext(ctx, `
			INSERT INTO sys_users (id, username, password_hash, role, is_active, mfa_enabled, mfa_secret, super_owner, created_at, updated_at)
			VALUES ($1, $2, $3, $4, TRUE, FALSE, '', $5, $6, $6)`,
			id, name, serverAccountPasswordMarker, role, superOwner, now)
		if err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	case existing != id:
		return "", ErrServerAccountNameTaken
	default:
		_, err = db.ExecContext(ctx, `
			UPDATE sys_users SET role = $1, is_active = TRUE, super_owner = $2, updated_at = $3 WHERE id = $4`,
			role, superOwner, now, id)
		if err != nil {
			return "", err
		}
	}
	return id, nil
}
