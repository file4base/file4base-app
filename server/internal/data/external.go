package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrExternalUnavailable is returned when a request reaches a table
// occurrence of another solution's table (an external occurrence) and the
// records cannot be read from there. Every ExternalUnavailableError wraps it.
var ErrExternalUnavailable = errors.New("external table occurrence unavailable")

// Reasons an external occurrence cannot be read, as the API reports them.
const (
	// ExternalMissing: the file reference is not mapped to a solution, the
	// solution is not installed, or this server does not read records
	// across solutions yet.
	ExternalMissing = "missing"
)

// ExternalUnavailableError says which file reference an unreadable external
// occurrence goes through, the solution it is mapped to, and why it cannot be
// read. A request that reaches one fails with it rather than reading any
// table of this solution in its place.
type ExternalUnavailableError struct {
	Occurrence string
	Reference  string
	Database   string
	Reason     string
}

func (e *ExternalUnavailableError) Error() string {
	target := "a solution not mapped yet"
	if e.Database != "" {
		target = "the solution " + e.Database
	}
	return fmt.Sprintf("%s: %q is an occurrence of a table of %s, through the file reference %q; "+
		"records of another solution cannot be read from here yet",
		ErrExternalUnavailable, e.Occurrence, target, e.Reference)
}

func (e *ExternalUnavailableError) Unwrap() error { return ErrExternalUnavailable }

// externalSide returns the error for the first side of a relationship that is
// an external occurrence, or nil when both sides are local (or the
// relationship does not exist).
func (s *Service) externalSide(ctx context.Context, relationshipID string) error {
	d := s.driver.Dialect()
	q := `SELECT o.name, COALESCE(f.name, ''), COALESCE(f.database_name, '')
		FROM sys_relationships r
		JOIN sys_table_occurrences o ON o.id = r.left_occurrence_id OR o.id = r.right_occurrence_id
		LEFT JOIN sys_file_references f ON f.id = o.file_reference_id
		WHERE r.id = ` + d.Placeholder(1) + ` AND o.base_table_id IS NULL
		ORDER BY o.name`
	var e ExternalUnavailableError
	err := s.driver.DB().QueryRowContext(ctx, q, relationshipID).Scan(&e.Occurrence, &e.Reference, &e.Database)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		// A catalog without the external columns has no external occurrence.
		return nil
	}
	e.Reason = ExternalMissing
	return &e
}
