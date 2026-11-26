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
	// CapabilityFMRest is access through the FileMaker Data API at /fmi/data
	// (#208), FileMaker's fmrest extended privilege. Unlike the others it is
	// an access method a role holds only when an owner grants it: the Data
	// API is a second way in, which no account should gain by an upgrade.
	CapabilityFMRest = "fmrest"
)

// Capabilities lists every capability, in the order the interface shows them.
func Capabilities() []string {
	return []string{CapabilityBulkExport, CapabilityBulkImport, CapabilityFMRest}
}

// capabilityColumns is the sys_privileges column of each capability.
var capabilityColumns = map[string]string{
	CapabilityBulkExport: "bulk_export",
	CapabilityBulkImport: "bulk_import",
	CapabilityFMRest:     "fmrest",
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
	FMRest     bool   `json:"fmrest" msgpack:"fmrest"`
}

// Allows reports whether this set holds a capability.
func (p RolePrivileges) Allows(capability string) bool {
	switch capability {
	case CapabilityBulkExport:
		return p.BulkExport
	case CapabilityBulkImport:
		return p.BulkImport
	case CapabilityFMRest:
		return p.FMRest
	default:
		return false
	}
}

// With returns this set with one capability granted or withheld.
func (p RolePrivileges) With(capability string, allowed bool) RolePrivileges {
	switch capability {
	case CapabilityBulkExport:
		p.BulkExport = allowed
	case CapabilityBulkImport:
		p.BulkImport = allowed
	case CapabilityFMRest:
		p.FMRest = allowed
	}
	return p
}

// ErrUnknownCapability is returned for a capability File4Base does not have.
var ErrUnknownCapability = errors.New("unknown capability")

// ErrUnknownRole is returned when a role cannot hold privileges.
var ErrUnknownRole = errors.New("unknown role")

// defaultRolePrivileges is what a role holds when nothing is stored for it:
// every action, so that a database upgraded from a version without this table
// keeps working exactly as before and no restriction ever appears without an
// owner having asked for it, but not the Data API, which is opt-in.
func defaultRolePrivileges(role string) RolePrivileges {
	return RolePrivileges{Role: role, BulkExport: true, BulkImport: true}
}

// ownerPrivileges is what an owner holds, whatever is stored: everything, as
// FileMaker's [Full Access] holds every extended privilege.
func ownerPrivileges() RolePrivileges {
	p := RolePrivileges{Role: roleOwner}
	for _, capability := range Capabilities() {
		p = p.With(capability, true)
	}
	return p
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
	return s.roleHolds(ctx, role, capability)
}

func isCapability(capability string) bool {
	for _, c := range Capabilities() {
		if c == capability {
			return true
		}
	}
	return false
}

// roleHolds reads one stored grant of a role, falling back to the default
// when the role has no row yet. It reads that capability's column only, so a
// check of one capability does not depend on the columns added for others.
func (s *Service) roleHolds(ctx context.Context, role, capability string) (bool, error) {
	q := `SELECT ` + capabilityColumns[capability] + ` FROM sys_privileges WHERE role = $1`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT ` + capabilityColumns[capability] + ` FROM sys_privileges WHERE role = ?`
	}
	var allowed bool
	err := s.driver.DB().QueryRowContext(ctx, q, role).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultRolePrivileges(role).Allows(capability), nil
	}
	if err != nil {
		return false, fmt.Errorf("failed reading privileges of role %s: %w", role, err)
	}
	return allowed, nil
}

// GrantCapability grants one capability to a role, leaving what else it holds
// as it is. A migration uses it to carry over what the FileMaker file granted.
func (s *Service) GrantCapability(ctx context.Context, role, capability string) error {
	if !isCapability(capability) {
		return fmt.Errorf("%w: %s", ErrUnknownCapability, capability)
	}
	if !isPrivilegeRole(role) {
		return fmt.Errorf("%w: %s", ErrUnknownRole, role)
	}
	listed, err := s.ListRolePrivileges(ctx)
	if err != nil {
		return err
	}
	for _, p := range listed {
		if p.Role == role {
			return s.SetRolePrivileges(ctx, []RolePrivileges{p.With(capability, true)})
		}
	}
	return nil
}

// ListRolePrivileges returns one entry per role, in PrivilegeRoles order,
// with the owner's entry always fully granted.
func (s *Service) ListRolePrivileges(ctx context.Context) ([]RolePrivileges, error) {
	stored := map[string]RolePrivileges{}
	rows, err := s.driver.DB().QueryContext(ctx, `SELECT role, bulk_export, bulk_import, fmrest FROM sys_privileges`)
	if err != nil {
		return nil, fmt.Errorf("failed listing privileges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p RolePrivileges
		if err := rows.Scan(&p.Role, &p.BulkExport, &p.BulkImport, &p.FMRest); err != nil {
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
			p = ownerPrivileges()
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
			p = ownerPrivileges()
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
	qIns := `INSERT INTO sys_privileges (role, bulk_export, bulk_import, fmrest, updated_at) VALUES ($1, $2, $3, $4, $5)`
	if !postgres {
		qDel = `DELETE FROM sys_privileges WHERE role = ?`
		qIns = `INSERT INTO sys_privileges (role, bulk_export, bulk_import, fmrest, updated_at) VALUES (?, ?, ?, ?, ?)`
	}

	now := time.Now().UTC()
	for _, role := range roles {
		p := seen[role]
		if _, err := tx.ExecContext(ctx, qDel, role); err != nil {
			return fmt.Errorf("failed clearing privileges of role %s: %w", role, err)
		}
		if _, err := tx.ExecContext(ctx, qIns, role, p.BulkExport, p.BulkImport, p.FMRest, now); err != nil {
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
