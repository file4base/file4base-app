package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Capabilities a database owner can grant to or withhold from an account
// role. They are actions, not access methods: every client speaks to the same
// REST API, so a restriction by client type would only be a hint the caller
// sets itself, while an action is something the server performs and can
// therefore refuse.
const (
	// CapabilityBulkExport covers writing many records out at once: the
	// record export of a table or found set, and the .f4data data file.
	CapabilityBulkExport = "bulk_export"
	// CapabilityBulkImport covers bringing many records in at once: record
	// import from a file, its preview, and the .f4data data file.
	CapabilityBulkImport = "bulk_import"
)

// Capabilities lists every capability, in the order the interface shows them.
func Capabilities() []string {
	return []string{CapabilityBulkExport, CapabilityBulkImport}
}

// PrivilegeRoles lists the roles a capability can be granted to. The owner
// role is included so the interface can show it, but it always holds every
// capability: an owner must not be able to lock themselves out of their own
// database.
func PrivilegeRoles() []string {
	return []string{roleOwner, roleAdmin, roleUser}
}

// The account roles. They are the same strings sys_users stores and
// auth.Session carries; isValidRole checks them as well.
const (
	roleOwner = "owner"
	roleAdmin = "admin"
	roleUser  = "user"
)

// RolePrivileges is the set of capabilities one role holds.
type RolePrivileges struct {
	Role       string `json:"role" msgpack:"role"`
	BulkExport bool   `json:"bulk_export" msgpack:"bulk_export"`
	BulkImport bool   `json:"bulk_import" msgpack:"bulk_import"`
}

// Allows reports whether this set holds a capability.
func (p RolePrivileges) Allows(capability string) bool {
	switch capability {
	case CapabilityBulkExport:
		return p.BulkExport
	case CapabilityBulkImport:
		return p.BulkImport
	default:
		return false
	}
}

// ErrUnknownCapability is returned for a capability File4Base does not have.
var ErrUnknownCapability = errors.New("unknown capability")

// ErrUnknownRole is returned when a role cannot hold privileges.
var ErrUnknownRole = errors.New("unknown role")

// defaultRolePrivileges is what a role holds when nothing is stored for it:
// everything. A database upgraded from a version without this table keeps
// working exactly as before, and no restriction ever appears without an owner
// having asked for it.
func defaultRolePrivileges(role string) RolePrivileges {
	return RolePrivileges{Role: role, BulkExport: true, BulkImport: true}
}

// RoleAllows reports whether an account role may use a capability. Owners
// always may; an unknown capability never is allowed.
func (s *Service) RoleAllows(ctx context.Context, role, capability string) (bool, error) {
	if !isCapability(capability) {
		return false, fmt.Errorf("%w: %s", ErrUnknownCapability, capability)
	}
	if role == roleOwner {
		return true, nil
	}
	p, err := s.rolePrivileges(ctx, role)
	if err != nil {
		return false, err
	}
	return p.Allows(capability), nil
}

func isCapability(capability string) bool {
	for _, c := range Capabilities() {
		if c == capability {
			return true
		}
	}
	return false
}

// rolePrivileges reads one role's stored grants, falling back to the default
// when the role has no row yet.
func (s *Service) rolePrivileges(ctx context.Context, role string) (RolePrivileges, error) {
	q := `SELECT bulk_export, bulk_import FROM sys_privileges WHERE role = $1`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT bulk_export, bulk_import FROM sys_privileges WHERE role = ?`
	}
	p := defaultRolePrivileges(role)
	err := s.driver.DB().QueryRowContext(ctx, q, role).Scan(&p.BulkExport, &p.BulkImport)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultRolePrivileges(role), nil
	}
	if err != nil {
		return defaultRolePrivileges(role), fmt.Errorf("failed reading privileges of role %s: %w", role, err)
	}
	return p, nil
}

// ListRolePrivileges returns one entry per role, in PrivilegeRoles order,
// with the owner's entry always fully granted.
func (s *Service) ListRolePrivileges(ctx context.Context) ([]RolePrivileges, error) {
	stored := map[string]RolePrivileges{}
	rows, err := s.driver.DB().QueryContext(ctx, `SELECT role, bulk_export, bulk_import FROM sys_privileges`)
	if err != nil {
		return nil, fmt.Errorf("failed listing privileges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p RolePrivileges
		if err := rows.Scan(&p.Role, &p.BulkExport, &p.BulkImport); err != nil {
			return nil, err
		}
		stored[p.Role] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]RolePrivileges, 0, len(PrivilegeRoles()))
	for _, role := range PrivilegeRoles() {
		p, ok := stored[role]
		if !ok {
			p = defaultRolePrivileges(role)
		}
		p.Role = role
		if role == roleOwner {
			p = defaultRolePrivileges(role)
		}
		out = append(out, p)
	}
	return out, nil
}

// SetRolePrivileges replaces the stored grants of the roles given. Roles left
// out keep what they hold; the owner's entry is stored fully granted whatever
// it says, because an owner always holds every capability.
func (s *Service) SetRolePrivileges(ctx context.Context, privileges []RolePrivileges) error {
	seen := map[string]RolePrivileges{}
	for _, p := range privileges {
		if !isPrivilegeRole(p.Role) {
			return fmt.Errorf("%w: %s", ErrUnknownRole, p.Role)
		}
		if p.Role == roleOwner {
			p = defaultRolePrivileges(roleOwner)
		}
		seen[p.Role] = p
	}
	if len(seen) == 0 {
		return nil
	}

	roles := make([]string, 0, len(seen))
	for role := range seen {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	postgres := s.driver.Dialect().Engine() == dbal.EnginePostgres
	qDel := `DELETE FROM sys_privileges WHERE role = $1`
	qIns := `INSERT INTO sys_privileges (role, bulk_export, bulk_import, updated_at) VALUES ($1, $2, $3, $4)`
	if !postgres {
		qDel = `DELETE FROM sys_privileges WHERE role = ?`
		qIns = `INSERT INTO sys_privileges (role, bulk_export, bulk_import, updated_at) VALUES (?, ?, ?, ?)`
	}

	now := time.Now().UTC()
	for _, role := range roles {
		p := seen[role]
		if _, err := tx.ExecContext(ctx, qDel, role); err != nil {
			return fmt.Errorf("failed clearing privileges of role %s: %w", role, err)
		}
		if _, err := tx.ExecContext(ctx, qIns, role, p.BulkExport, p.BulkImport, now); err != nil {
			return fmt.Errorf("failed storing privileges of role %s: %w", role, err)
		}
	}
	return tx.Commit()
}

func isPrivilegeRole(role string) bool {
	for _, r := range PrivilegeRoles() {
		if r == role {
			return true
		}
	}
	return false
}

// CapabilitiesOf returns the capabilities one role holds, as the client needs
// them to tell a user what they can do before they try.
func (s *Service) CapabilitiesOf(ctx context.Context, role string) ([]string, error) {
	out := make([]string, 0, len(Capabilities()))
	for _, capability := range Capabilities() {
		allowed, err := s.RoleAllows(ctx, role, capability)
		if err != nil {
			return nil, err
		}
		if allowed {
			out = append(out, capability)
		}
	}
	return out, nil
}
