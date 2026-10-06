package schema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type UserMetadata struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"` // "owner", "admin", "user"
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserLayoutPermission struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	LayoutID    string `json:"layout_id"`
	LayoutName  string `json:"layout_name,omitempty"`
	AccessLevel string `json:"access_level"` // "read_write", "read_only", "none"
}

type AuthUser struct {
	ID          string                 `json:"id"`
	Username    string                 `json:"username"`
	Role        string                 `json:"role"`
	IsActive    bool                   `json:"is_active"`
	Permissions []UserLayoutPermission `json:"permissions"`
	// Stamp identifies the account state the user signed in with (see
	// AccountStamp); it is stored in the session, never sent to clients.
	Stamp string `json:"-"`
}

// ErrAccountNotFound is returned when an account does not exist (any more).
var ErrAccountNotFound = errors.New("account not found")

// AccountStamp summarizes the security state of an account: its password
// hash, role and active flag. Any change to them (password rotation, role
// change, deactivation) changes the stamp, which invalidates the sessions
// opened before the change.
func AccountStamp(passwordHash, role string, isActive bool) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%t", passwordHash, role, isActive)))
	return hex.EncodeToString(sum[:])
}

// CurrentAccountStamp returns the AccountStamp of an account as stored now,
// or ErrAccountNotFound when it was deleted.
func (s *Service) CurrentAccountStamp(ctx context.Context, userID string) (string, error) {
	q := `SELECT password_hash, role, COALESCE(is_active, TRUE) FROM sys_users WHERE id = $1`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT password_hash, role, COALESCE(is_active, TRUE) FROM sys_users WHERE id = ?`
	}
	var hash, role string
	var active bool
	if err := s.driver.DB().QueryRowContext(ctx, q, userID).Scan(&hash, &role, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrAccountNotFound
		}
		return "", err
	}
	return AccountStamp(hash, role, active), nil
}

// Access levels a user can hold on a layout.
const (
	AccessReadWrite = "read_write"
	AccessReadOnly  = "read_only"
	AccessNone      = "none"
)

// ErrInvalidCredentials is returned by Authenticate for any username/password mismatch.
// The message is deliberately identical for unknown users and wrong passwords.
var ErrInvalidCredentials = errors.New("invalid username or password")

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// burnPasswordCheck spends the same time as a real bcrypt comparison so that
// response timing does not reveal whether a username exists.
func burnPasswordCheck(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("file4base-timing-equalizer"), bcrypt.DefaultCost)
	})
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

func isValidRole(role string) bool {
	return role == "owner" || role == "admin" || role == "user"
}

func isValidAccessLevel(level string) bool {
	return level == AccessReadWrite || level == AccessReadOnly || level == AccessNone
}

// Authenticate verifies user credentials and returns the user with layout permissions.
// Both the username and the password must match the same account.
func (s *Service) Authenticate(ctx context.Context, username, password string) (*AuthUser, error) {
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("password is required")
	}
	cleanUser := strings.ToLower(strings.TrimSpace(username))
	if cleanUser == "" {
		return nil, errors.New("username is required")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	var id, uName, hash, role string
	var isActive bool

	q := `SELECT id, username, password_hash, role, COALESCE(is_active, TRUE) FROM sys_users WHERE LOWER(username) = LOWER($1) LIMIT 1`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `SELECT id, username, password_hash, role, COALESCE(is_active, TRUE) FROM sys_users WHERE LOWER(username) = LOWER(?) LIMIT 1`
	}
	if err := db.QueryRowContext(ctx, q, cleanUser).Scan(&id, &uName, &hash, &role, &isActive); err != nil {
		burnPasswordCheck(password)
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	if !isActive {
		return nil, errors.New("user account is deactivated")
	}

	// Fetch permissions
	perms, err := s.GetUserPermissions(ctx, id)
	if err != nil {
		perms = make([]UserLayoutPermission, 0)
	}

	return &AuthUser{
		ID:          id,
		Username:    uName,
		Role:        role,
		IsActive:    isActive,
		Permissions: perms,
		Stamp:       AccountStamp(hash, role, isActive),
	}, nil
}

// CountUsers returns the number of accounts registered in sys_users.
func (s *Service) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// HasSystemCatalog reports whether the database already contains the File4Base user catalog.
func (s *Service) HasSystemCatalog(ctx context.Context) bool {
	var count int
	return s.driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users`).Scan(&count) == nil
}

// GetUser returns a single user account by id.
func (s *Service) GetUser(ctx context.Context, id string) (*UserMetadata, error) {
	q := `SELECT id, username, role, COALESCE(is_active, TRUE), created_at, updated_at FROM sys_users WHERE id = $1`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT id, username, role, COALESCE(is_active, TRUE), created_at, updated_at FROM sys_users WHERE id = ?`
	}
	var u UserMetadata
	if err := s.driver.DB().QueryRowContext(ctx, q, id).Scan(&u.ID, &u.Username, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, errors.New("user not found")
	}
	return &u, nil
}

// countActiveOwners returns how many active owner accounts exist.
func (s *Service) countActiveOwners(ctx context.Context) int {
	var count int
	_ = s.driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users WHERE role = 'owner' AND COALESCE(is_active, TRUE) = TRUE`).Scan(&count)
	return count
}

// LayoutAccess returns the access level a user holds on a layout.
// A layout without an explicit permission row defaults to read_write.
func (s *Service) LayoutAccess(ctx context.Context, userID, layoutID string) (string, error) {
	q := `SELECT access_level FROM sys_user_permissions WHERE user_id = $1 AND layout_id = $2`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT access_level FROM sys_user_permissions WHERE user_id = ? AND layout_id = ?`
	}
	var level string
	err := s.driver.DB().QueryRowContext(ctx, q, userID, layoutID).Scan(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return AccessReadWrite, nil
	}
	if err != nil {
		return AccessNone, fmt.Errorf("failed resolving layout access: %w", err)
	}
	if !isValidAccessLevel(level) {
		return AccessNone, nil
	}
	return level, nil
}

// TableAccess derives the access level a user holds on a base table from the
// layout permissions: the data API works on tables, while permissions are
// granted per layout, so the most permissive level across every layout built
// on the table applies. A table without layouts, or a layout without an
// explicit permission row, defaults to read_write.
func (s *Service) TableAccess(ctx context.Context, userID, tableName string) (string, error) {
	q := `SELECT p.access_level
	      FROM sys_layouts l
	      JOIN sys_table_occurrences o ON o.id = l.table_occurrence_id
	      JOIN sys_tables t ON t.id = o.base_table_id
	      LEFT JOIN sys_user_permissions p ON p.layout_id = l.id AND p.user_id = $1
	      WHERE t.name = $2`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT p.access_level
		      FROM sys_layouts l
		      JOIN sys_table_occurrences o ON o.id = l.table_occurrence_id
		      JOIN sys_tables t ON t.id = o.base_table_id
		      LEFT JOIN sys_user_permissions p ON p.layout_id = l.id AND p.user_id = ?
		      WHERE t.name = ?`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, userID, tableName)
	if err != nil {
		return AccessNone, fmt.Errorf("failed resolving table access: %w", err)
	}
	defer rows.Close()

	rank := map[string]int{AccessNone: 0, AccessReadOnly: 1, AccessReadWrite: 2}
	best := -1
	for rows.Next() {
		var level sql.NullString
		if err := rows.Scan(&level); err != nil {
			return AccessNone, err
		}
		current := rank[AccessReadWrite]
		if level.Valid {
			current = rank[level.String] // unknown values rank as "none"
		}
		if current > best {
			best = current
		}
	}
	if err := rows.Err(); err != nil {
		return AccessNone, err
	}

	switch best {
	case -1, 2:
		return AccessReadWrite, nil
	case 1:
		return AccessReadOnly, nil
	default:
		return AccessNone, nil
	}
}

// ListUsers returns all registered users
func (s *Service) ListUsers(ctx context.Context) ([]UserMetadata, error) {
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, username, role, COALESCE(is_active, TRUE), created_at, updated_at FROM sys_users ORDER BY username ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed listing users: %w", err)
	}
	defer rows.Close()

	users := make([]UserMetadata, 0)
	for rows.Next() {
		var u UserMetadata
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

// CreateUser creates a new user account with hashed password
func (s *Service) CreateUser(ctx context.Context, username, password, role string, isActive ...bool) (*UserMetadata, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}
	if !isValidRole(role) {
		role = "user"
	}
	active := true
	if len(isActive) > 0 {
		active = isActive[0]
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed hashing password: %w", err)
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	id := uuid.NewString()
	now := time.Now().UTC()

	q := `INSERT INTO sys_users (id, username, password_hash, role, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `INSERT INTO sys_users (id, username, password_hash, role, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	}

	_, err = db.ExecContext(ctx, q, id, username, string(hash), role, active, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed creating user: %w", err)
	}

	return &UserMetadata{
		ID:        id,
		Username:  username,
		Role:      role,
		IsActive:  active,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// UpdateUser updates password and/or role and is_active
func (s *Service) UpdateUser(ctx context.Context, id, password, role string, isActive ...*bool) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()
	now := time.Now().UTC()

	var activeVal *bool
	if len(isActive) > 0 {
		activeVal = isActive[0]
	}

	current, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if role == "" {
		role = current.Role
	}
	if !isValidRole(role) {
		return fmt.Errorf("invalid role '%s'", role)
	}
	// Never leave a database without an active owner
	if current.Role == "owner" && current.IsActive {
		demoted := role != "owner"
		deactivated := activeVal != nil && !*activeVal
		if (demoted || deactivated) && s.countActiveOwners(ctx) <= 1 {
			return errors.New("cannot demote or deactivate the only active owner account")
		}
	}

	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed hashing password: %w", err)
		}
		if activeVal != nil {
			q := `UPDATE sys_users SET password_hash = $1, role = $2, is_active = $3, updated_at = $4 WHERE id = $5`
			if dialect.Engine() != dbal.EnginePostgres {
				q = `UPDATE sys_users SET password_hash = ?, role = ?, is_active = ?, updated_at = ? WHERE id = ?`
			}
			_, err = db.ExecContext(ctx, q, string(hash), role, *activeVal, now, id)
			return err
		}
		q := `UPDATE sys_users SET password_hash = $1, role = $2, updated_at = $3 WHERE id = $4`
		if dialect.Engine() != dbal.EnginePostgres {
			q = `UPDATE sys_users SET password_hash = ?, role = ?, updated_at = ? WHERE id = ?`
		}
		_, err = db.ExecContext(ctx, q, string(hash), role, now, id)
		return err
	}

	if activeVal != nil {
		q := `UPDATE sys_users SET role = $1, is_active = $2, updated_at = $3 WHERE id = $4`
		if dialect.Engine() != dbal.EnginePostgres {
			q = `UPDATE sys_users SET role = ?, is_active = ?, updated_at = ? WHERE id = ?`
		}
		_, err = db.ExecContext(ctx, q, role, *activeVal, now, id)
		return err
	}

	q := `UPDATE sys_users SET role = $1, updated_at = $2 WHERE id = $3`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `UPDATE sys_users SET role = ?, updated_at = ? WHERE id = ?`
	}
	_, err = db.ExecContext(ctx, q, role, now, id)
	return err
}

// DeleteUser deletes a user account (prevents deleting the last owner)
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// Check if this user is an owner
	var role string
	qRole := `SELECT role FROM sys_users WHERE id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		qRole = `SELECT role FROM sys_users WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, qRole, id).Scan(&role); err != nil {
		return errors.New("user not found")
	}

	if role == "owner" {
		var ownerCount int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users WHERE role = 'owner'`).Scan(&ownerCount)
		if ownerCount <= 1 {
			return errors.New("cannot delete the only owner account")
		}
	}

	qDel := `DELETE FROM sys_users WHERE id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		qDel = `DELETE FROM sys_users WHERE id = ?`
	}
	_, err := db.ExecContext(ctx, qDel, id)
	return err
}

// GetUserPermissions returns all layout permissions for a given user
func (s *Service) GetUserPermissions(ctx context.Context, userID string) ([]UserLayoutPermission, error) {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	q := `SELECT p.id, p.user_id, p.layout_id, COALESCE(l.name, ''), p.access_level
	      FROM sys_user_permissions p
	      LEFT JOIN sys_layouts l ON l.id = p.layout_id
	      WHERE p.user_id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `SELECT p.id, p.user_id, p.layout_id, COALESCE(l.name, ''), p.access_level
		      FROM sys_user_permissions p
		      LEFT JOIN sys_layouts l ON l.id = p.layout_id
		      WHERE p.user_id = ?`
	}

	rows, err := db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("failed fetching user permissions: %w", err)
	}
	defer rows.Close()

	perms := make([]UserLayoutPermission, 0)
	for rows.Next() {
		var p UserLayoutPermission
		if err := rows.Scan(&p.ID, &p.UserID, &p.LayoutID, &p.LayoutName, &p.AccessLevel); err != nil {
			continue
		}
		perms = append(perms, p)
	}
	return perms, nil
}

// SetUserPermissions replaces all layout permissions for a user.
// Every level is stored explicitly, including "none": a layout without a row
// defaults to read_write, so dropping "none" rows would silently grant access.
func (s *Service) SetUserPermissions(ctx context.Context, userID string, perms []UserLayoutPermission) error {
	for _, p := range perms {
		if p.LayoutID != "" && !isValidAccessLevel(p.AccessLevel) {
			return fmt.Errorf("invalid access level '%s' for layout %s", p.AccessLevel, p.LayoutID)
		}
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete existing permissions
	qDel := `DELETE FROM sys_user_permissions WHERE user_id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		qDel = `DELETE FROM sys_user_permissions WHERE user_id = ?`
	}
	if _, err := tx.ExecContext(ctx, qDel, userID); err != nil {
		return fmt.Errorf("failed deleting old permissions: %w", err)
	}

	// 2. Insert the new permission set
	qIns := `INSERT INTO sys_user_permissions (id, user_id, layout_id, access_level, created_at) VALUES ($1, $2, $3, $4, $5)`
	if dialect.Engine() != dbal.EnginePostgres {
		qIns = `INSERT INTO sys_user_permissions (id, user_id, layout_id, access_level, created_at) VALUES (?, ?, ?, ?, ?)`
	}

	now := time.Now().UTC()
	seen := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		if p.LayoutID == "" {
			continue
		}
		if _, dup := seen[p.LayoutID]; dup {
			continue
		}
		seen[p.LayoutID] = struct{}{}
		if _, err := tx.ExecContext(ctx, qIns, uuid.NewString(), userID, p.LayoutID, p.AccessLevel, now); err != nil {
			return fmt.Errorf("failed inserting permission for layout %s: %w", p.LayoutID, err)
		}
	}

	return tx.Commit()
}
