package schema

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type UserMetadata struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"` // "owner", "admin", "user"
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
	Permissions []UserLayoutPermission `json:"permissions"`
}

// Authenticate verifies user credentials and returns the user with layout permissions
func (s *Service) Authenticate(ctx context.Context, username, password string) (*AuthUser, error) {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	q := `SELECT id, username, password_hash, role FROM sys_users WHERE LOWER(username) = LOWER($1) LIMIT 1`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `SELECT id, username, password_hash, role FROM sys_users WHERE LOWER(username) = LOWER(?) LIMIT 1`
	}

	var id, uName, hash, role string
	err := db.QueryRowContext(ctx, q, username).Scan(&id, &uName, &hash, &role)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
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
		Permissions: perms,
	}, nil
}

// ListUsers returns all registered users
func (s *Service) ListUsers(ctx context.Context) ([]UserMetadata, error) {
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, username, role, created_at, updated_at FROM sys_users ORDER BY username ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed listing users: %w", err)
	}
	defer rows.Close()

	users := make([]UserMetadata, 0)
	for rows.Next() {
		var u UserMetadata
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

// CreateUser creates a new user account with hashed password
func (s *Service) CreateUser(ctx context.Context, username, password, role string) (*UserMetadata, error) {
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}
	if role != "owner" && role != "admin" && role != "user" {
		role = "user"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed hashing password: %w", err)
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	id := uuid.NewString()
	now := time.Now().UTC()

	q := `INSERT INTO sys_users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `INSERT INTO sys_users (id, username, password_hash, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	}

	_, err = db.ExecContext(ctx, q, id, username, string(hash), role, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed creating user: %w", err)
	}

	return &UserMetadata{
		ID:        id,
		Username:  username,
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// UpdateUser updates password and/or role
func (s *Service) UpdateUser(ctx context.Context, id, password, role string) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()
	now := time.Now().UTC()

	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed hashing password: %w", err)
		}
		q := `UPDATE sys_users SET password_hash = $1, role = $2, updated_at = $3 WHERE id = $4`
		if dialect.Engine() != dbal.EnginePostgres {
			q = `UPDATE sys_users SET password_hash = ?, role = ?, updated_at = ? WHERE id = ?`
		}
		_, err = db.ExecContext(ctx, q, string(hash), role, now, id)
		return err
	}

	q := `UPDATE sys_users SET role = $1, updated_at = $2 WHERE id = $3`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `UPDATE sys_users SET role = ?, updated_at = ? WHERE id = ?`
	}
	_, err := db.ExecContext(ctx, q, role, now, id)
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

// SetUserPermissions replaces all layout permissions for a user
func (s *Service) SetUserPermissions(ctx context.Context, userID string, perms []UserLayoutPermission) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// 1. Delete existing permissions
	qDel := `DELETE FROM sys_user_permissions WHERE user_id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		qDel = `DELETE FROM sys_user_permissions WHERE user_id = ?`
	}
	if _, err := db.ExecContext(ctx, qDel, userID); err != nil {
		return fmt.Errorf("failed deleting old permissions: %w", err)
	}

	// 2. Insert new permissions (only if not 'none')
	qIns := `INSERT INTO sys_user_permissions (id, user_id, layout_id, access_level, created_at) VALUES ($1, $2, $3, $4, $5)`
	if dialect.Engine() != dbal.EnginePostgres {
		qIns = `INSERT INTO sys_user_permissions (id, user_id, layout_id, access_level, created_at) VALUES (?, ?, ?, ?, ?)`
	}

	now := time.Now().UTC()
	for _, p := range perms {
		if p.AccessLevel == "none" || p.LayoutID == "" {
			continue
		}
		permID := uuid.NewString()
		_, err := db.ExecContext(ctx, qIns, permID, userID, p.LayoutID, p.AccessLevel, now)
		if err != nil {
			return fmt.Errorf("failed inserting permission for layout %s: %w", p.LayoutID, err)
		}
	}

	return nil
}
