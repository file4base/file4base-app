package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrExternalUnavailable is returned when a request reaches a table
// occurrence of another solution's table (an external occurrence) and the
// records cannot be read from there. Every ExternalUnavailableError wraps it.
var ErrExternalUnavailable = errors.New("external table occurrence unavailable")

// Reasons an external occurrence cannot be read, as the API reports them.
const (
	// ExternalMissing: the file reference is not mapped to a solution, the
	// table is not known yet, the solution is not installed, or what is
	// asked is not done across solutions yet (finds through a relationship
	// to another solution).
	ExternalMissing = "missing"
	// ExternalNotSignedIn: the session group holds no session of the other
	// solution, or the one it held expired or was revoked.
	ExternalNotSignedIn = "not_signed_in"
	// ExternalNoAccess: the account the group holds there may not read
	// the table.
	ExternalNoAccess = "no_access"
	// ExternalSchemaMismatch: the table, or a field read through the
	// occurrence, is not in the other solution (renamed or deleted).
	ExternalSchemaMismatch = "schema_mismatch"
)

// ExternalTarget is the table of another solution an external occurrence
// names: what a read through it opens.
type ExternalTarget struct {
	// Occurrence is the occurrence's name, Reference the file reference it
	// goes through.
	Occurrence string `json:"occurrence"`
	Reference  string `json:"reference"`
	// Database is the solution the reference is mapped to, Table the
	// table's name there; either is empty while it is not known.
	Database string `json:"database"`
	Table    string `json:"table"`
}

// unavailable is the error for a read of t that cannot be done, for reason.
func (t ExternalTarget) unavailable(reason string, cause error) *ExternalUnavailableError {
	return &ExternalUnavailableError{
		Occurrence: t.Occurrence, Reference: t.Reference, Database: t.Database, Table: t.Table,
		Reason: reason, cause: cause,
	}
}

// Unsupported is the error for something a request asks of t that is not
// done across solutions yet, which is reported as missing.
func (t ExternalTarget) Unsupported(what string) error {
	e := t.unavailable(ExternalMissing, nil)
	e.Detail = what + " is not done across solutions yet"
	return e
}

// Wrap classifies an error met reading or writing t in the other solution:
// a table or field that is not there is a schema mismatch, a table the
// account may not read is no access, and a write the account may not make
// says so with the reference and the solution. Any other error is returned
// as it is.
func (t ExternalTarget) Wrap(err error) error {
	var denied *PermissionError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrExternalUnavailable):
		return err
	case errors.As(err, &denied):
		if denied.External != nil {
			return err
		}
		named := *denied
		named.External = &t
		if named.Table == "" {
			named.Table = t.Table
		}
		return &named
	case errors.Is(err, ErrTableNotFound), errors.Is(err, ErrUnknownField):
		return t.unavailable(ExternalSchemaMismatch, err)
	case errors.Is(err, ErrNoAccess):
		return t.unavailable(ExternalNoAccess, err)
	}
	return err
}

// ExternalUnavailableError says which file reference an unreadable external
// occurrence goes through, the solution it is mapped to, and why it cannot be
// read. A request that reaches one fails with it rather than reading any
// table of this solution in its place.
type ExternalUnavailableError struct {
	Occurrence string
	Reference  string
	Database   string
	Table      string
	Reason     string
	// Detail says what was asked, when it is not done across solutions.
	Detail string
	cause  error
}

func (e *ExternalUnavailableError) Error() string {
	target := "a solution not mapped yet"
	if e.Database != "" {
		target = "the solution " + e.Database
	}
	if e.Table != "" {
		target = fmt.Sprintf("the table %q of %s", e.Table, target)
	}
	return fmt.Sprintf("%s: %q is an occurrence of %s, through the file reference %q; %s",
		ErrExternalUnavailable, e.Occurrence, target, e.Reference, e.remedy())
}

// remedy says what stops the read, and what fixes it.
func (e *ExternalUnavailableError) remedy() string {
	switch e.Reason {
	case ExternalNotSignedIn:
		return "sign in to " + e.Database + " to read its records"
	case ExternalNoAccess:
		return "the account signed in to " + e.Database + " may not read this table; " +
			"ask an administrator of " + e.Database + " for access, or sign in to it with another account"
	case ExternalSchemaMismatch:
		why := "the table or a field it uses is not there"
		if e.cause != nil {
			why = e.cause.Error()
		}
		return why + "; verify the file reference"
	}
	switch {
	case e.Detail != "":
		return e.Detail
	case e.Database == "":
		return "map the file reference to a solution"
	case e.Table == "":
		return "the table is not known yet; bind the file reference"
	}
	return "that solution is not available on this server"
}

func (e *ExternalUnavailableError) Unwrap() error { return ErrExternalUnavailable }

// Peers opens the other solutions external occurrences read from (#107),
// each as the account the caller's session group holds there.
type Peers interface {
	// Data is the data service of a solution, reading as that account with
	// that account's access, or an *ExternalUnavailableError when there is
	// none (not signed in, solution missing).
	Data(ctx context.Context, database string) (*Service, error)
}

// WithPeers lets the service read through external occurrences, from the
// solutions peers opens. Without it every such read is refused as missing,
// which is what the server's own work gets.
func (s *Service) WithPeers(peers Peers) *Service {
	s.peers = peers
	return s
}

// Open is the data service of the solution t is in, reading as the
// caller's account there, once it is known that the account may read the
// table and that the table is there. No record is read.
func (s *Service) Open(ctx context.Context, t ExternalTarget) (*Service, error) {
	if t.Database == "" || t.Table == "" || s.peers == nil {
		return nil, t.unavailable(ExternalMissing, nil)
	}
	peer, err := s.peers.Data(ctx, t.Database)
	if err != nil {
		reason := ExternalMissing
		var e *ExternalUnavailableError
		if errors.As(err, &e) && e.Reason != "" {
			reason = e.Reason
		}
		return nil, t.unavailable(reason, err)
	}
	// Access first, so a table the account may not read is not told apart
	// from one that is not there.
	if err := peer.newRelatedReader().checkRead(ctx, t.Table); err != nil {
		return nil, t.Wrap(err)
	}
	if _, err := peer.tableFields(ctx, t.Table); err != nil {
		return nil, t.Wrap(err)
	}
	return peer, nil
}

// Origin is the table a related read starts from: one of this solution, or
// one of another solution named by an external occurrence.
type Origin struct {
	Table    string
	External *ExternalTarget
}

// LocalOrigin is a table of this solution.
func LocalOrigin(table string) Origin { return Origin{Table: table} }

// key tells tables apart across solutions: a table of another solution
// never matches one of this solution of the same name.
func (o Origin) key() string {
	if o.External == nil {
		return o.Table
	}
	return o.External.Database + "\x00" + o.External.Table
}

// ErrOccurrenceNotFound is returned when a request names a table occurrence
// that is not in the catalog.
var ErrOccurrenceNotFound = errors.New("table occurrence not found")

// OccurrenceOrigin resolves a table occurrence, by id or else by name, to
// the table its records are in.
func (s *Service) OccurrenceOrigin(ctx context.Context, occurrence string) (Origin, error) {
	d := s.driver.Dialect()
	q := `SELECT o.name, o.base_table_id, COALESCE(t.name, ''), COALESCE(o.external_table, ''),
			COALESCE(f.name, ''), COALESCE(f.database_name, '')
		FROM sys_table_occurrences o
		LEFT JOIN sys_tables t ON t.id = o.base_table_id
		LEFT JOIN sys_file_references f ON f.id = o.file_reference_id
		WHERE o.id = ` + d.Placeholder(1) + ` OR LOWER(o.name) = LOWER(` + d.Placeholder(2) + `)
		ORDER BY CASE WHEN o.id = ` + d.Placeholder(3) + ` THEN 0 ELSE 1 END`
	var name, table, extTable, reference, database string
	var base sql.NullString
	err := s.driver.DB().QueryRowContext(ctx, q, occurrence, occurrence, occurrence).
		Scan(&name, &base, &table, &extTable, &reference, &database)
	if errors.Is(err, sql.ErrNoRows) {
		return Origin{}, fmt.Errorf("%w: %s", ErrOccurrenceNotFound, occurrence)
	}
	if err != nil {
		return Origin{}, fmt.Errorf("failed resolving table occurrence %s: %w", occurrence, err)
	}
	if base.Valid {
		if table == "" {
			return Origin{}, fmt.Errorf("%w: the table of %s no longer exists", ErrOccurrenceNotFound, name)
		}
		return LocalOrigin(table), nil
	}
	return Origin{Table: extTable, External: &ExternalTarget{
		Occurrence: name, Reference: reference, Database: database, Table: extTable,
	}}, nil
}

// origin is the table a side's records are in.
func (side RelationSide) origin() Origin {
	return Origin{Table: side.TableName, External: side.External}
}

// sideService is the data service a side's records are read and written
// with: this one for a table of this solution, the other solution's for an
// external side.
func (s *Service) sideService(ctx context.Context, side RelationSide) (*Service, error) {
	if side.External == nil {
		return s, nil
	}
	return s.Open(ctx, *side.External)
}

// fieldMarkerPrefix starts the marker of a field of another solution known
// only by its FileMaker id (schema.ExternalFieldMarker): "#" and that id.
const fieldMarkerPrefix = "#"

// ResolveFieldMarker returns the field of table a name stands for. A name
// is returned as it is; a marker is resolved by the source_field_id of the
// table's fields, as binding resolves it, to the one field migrated with
// that id. No field, or more than one, is ErrUnknownField.
func (s *Service) ResolveFieldMarker(ctx context.Context, table, name string) (string, error) {
	id, ok := strings.CutPrefix(name, fieldMarkerPrefix)
	if !ok {
		return name, nil
	}
	query := `SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = $1 AND c.source_field_id = $2`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		query = `SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = ? AND c.source_field_id = ?`
	}
	var names []string
	if id != "" {
		rows, err := s.driver.DB().QueryContext(ctx, query, table, id)
		if err != nil {
			return "", fmt.Errorf("failed resolving the field of %s with FileMaker id %s: %w", table, id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return "", err
			}
			names = append(names, n)
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
	}
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", fmt.Errorf("%w: %s has no field migrated with FileMaker id %q", ErrUnknownField, table, id)
	default:
		return "", fmt.Errorf("%w: the FileMaker id %q is ambiguous in %s: %s all have it",
			ErrUnknownField, id, table, strings.Join(names, ", "))
	}
}

// wrapSide classifies an error met reading a side (see ExternalTarget.Wrap).
func wrapSide(side RelationSide, err error) error {
	if side.External == nil || err == nil {
		return err
	}
	return side.External.Wrap(err)
}

// externalRelationshipSelect resolves a relationship with a side, or both,
// on an occurrence of another solution's table: such a side has no local
// table or column, but the table and field names the catalog keeps for it.
const externalRelationshipSelect = `SELECT r.id, r.name, r.operator, r.allow_creation, r.cascade_delete, r.sort_related,
		lo.id, lo.name, lo.base_table_id, COALESCE(lt.name, lo.external_table, ''), COALESCE(lc.name, r.left_column_name, ''),
		COALESCE(lf.name, ''), COALESCE(lf.database_name, ''),
		ro.id, ro.name, ro.base_table_id, COALESCE(rt.name, ro.external_table, ''), COALESCE(rc.name, r.right_column_name, ''),
		COALESCE(rf.name, ''), COALESCE(rf.database_name, '')
	FROM sys_relationships r
	JOIN sys_table_occurrences lo ON lo.id = r.left_occurrence_id
	LEFT JOIN sys_tables lt ON lt.id = lo.base_table_id
	LEFT JOIN sys_columns lc ON lc.id = r.left_column_id
	LEFT JOIN sys_file_references lf ON lf.id = lo.file_reference_id
	JOIN sys_table_occurrences ro ON ro.id = r.right_occurrence_id
	LEFT JOIN sys_tables rt ON rt.id = ro.base_table_id
	LEFT JOIN sys_columns rc ON rc.id = r.right_column_id
	LEFT JOIN sys_file_references rf ON rf.id = ro.file_reference_id
	WHERE r.id = `

// externalRelationship resolves a relationship the local statement left
// out because a side is an external occurrence. It is nil when no side is
// external, or when a local side no longer resolves, which is a relationship
// that is not found, as before.
func (s *Service) externalRelationship(ctx context.Context, relationshipID string) *Relationship {
	var rel Relationship
	var sortRelated, leftBase, rightBase sql.NullString
	var leftRef, leftDB, rightRef, rightDB string
	err := s.driver.DB().QueryRowContext(ctx, externalRelationshipSelect+s.driver.Dialect().Placeholder(1), relationshipID).Scan(
		&rel.ID, &rel.Name, &rel.Operator, &rel.AllowCreation, &rel.CascadeDelete, &sortRelated,
		&rel.Left.OccurrenceID, &rel.Left.OccurrenceName, &leftBase, &rel.Left.TableName, &rel.Left.Column, &leftRef, &leftDB,
		&rel.Right.OccurrenceID, &rel.Right.OccurrenceName, &rightBase, &rel.Right.TableName, &rel.Right.Column, &rightRef, &rightDB,
	)
	if err != nil {
		// Not found, or a catalog without the external columns, which has
		// no external occurrence.
		return nil
	}
	rel.SortRelated = sortRelated.String
	external := false
	for _, end := range []struct {
		side          *RelationSide
		base          sql.NullString
		reference, db string
	}{{&rel.Left, leftBase, leftRef, leftDB}, {&rel.Right, rightBase, rightRef, rightDB}} {
		if end.base.Valid {
			if end.side.TableName == "" || end.side.Column == "" {
				return nil
			}
			continue
		}
		external = true
		end.side.External = &ExternalTarget{
			Occurrence: end.side.OccurrenceName, Reference: end.reference, Database: end.db, Table: end.side.TableName,
		}
	}
	if !external {
		return nil
	}
	return &rel
}

// External is the first side of the relationship that is an external
// occurrence, or nil.
func (r *Relationship) External() *ExternalTarget {
	if r.Left.External != nil {
		return r.Left.External
	}
	return r.Right.External
}
