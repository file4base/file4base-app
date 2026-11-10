package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// ErrFileReferenceNotFound is returned when no file reference has the given
// id or name.
var ErrFileReferenceNotFound = errors.New("file reference not found")

// ErrFileReferenceExists is returned when the solution already has a file
// reference with that name.
var ErrFileReferenceExists = errors.New("a file reference with that name already exists")

// ErrInvalidFileReference is returned for a file reference that cannot be
// stored as given.
var ErrInvalidFileReference = errors.New("invalid file reference")

// ErrFileReferenceUnmapped is returned when a file reference names no
// solution yet, so a step that uses it has nowhere to go.
var ErrFileReferenceUnmapped = errors.New("file reference is not mapped to a solution")

const maxFileReferenceName = 128

// FileReference names another solution that this solution's scripts reach
// (#107). It is what FileMaker calls an external data source of type
// FileMaker: a `Perform Script` step names the reference, and the reference
// says which solution it means.
//
// The mapping is explicit on purpose. A FileMaker file called "Results" and a
// File4Base solution database called "results" are not the same thing until
// someone says so, and guessing would run a script in a solution nobody
// chose. A reference created by the assisted migration therefore starts
// unmapped, with an empty Database, and a step that uses it stops until it is
// mapped.
//
// A reference holds no credentials: a script performed through it runs on a
// session the user opened in that solution.
type FileReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Database is the solution's database name, or empty while the reference
	// is not mapped.
	Database string `json:"database"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsMapped reports whether the reference names a solution.
func (f *FileReference) IsMapped() bool { return strings.TrimSpace(f.Database) != "" }

// FileReferenceInput is what a caller supplies to save or replace a reference.
type FileReferenceInput struct {
	Name     string `json:"name"`
	Database string `json:"database"`
}

func (s *Service) fileReferencePlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

func checkFileReference(in FileReferenceInput) (FileReferenceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("%w: a file reference needs a name", ErrInvalidFileReference)
	}
	if len([]rune(in.Name)) > maxFileReferenceName {
		return in, fmt.Errorf("%w: the name of a file reference is at most %d characters",
			ErrInvalidFileReference, maxFileReferenceName)
	}
	in.Database = strings.TrimSpace(in.Database)
	if in.Database != "" {
		if !dbal.IsValidDatabaseName(in.Database) {
			return in, fmt.Errorf("%w: %q is not a solution database name",
				ErrInvalidFileReference, in.Database)
		}
		if dbal.IsProtectedDatabase(in.Database) {
			return in, fmt.Errorf("%w: %q is an engine-internal database, never a solution",
				ErrInvalidFileReference, in.Database)
		}
	}
	return in, nil
}

// CreateFileReference stores a new file reference.
func (s *Service) CreateFileReference(ctx context.Context, in FileReferenceInput) (*FileReference, error) {
	in, err := checkFileReference(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.fileReferenceNameTaken(ctx, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrFileReferenceExists, in.Name)
	}

	now := time.Now().UTC()
	ref := &FileReference{
		ID:        uuid.NewString(),
		Name:      in.Name,
		Database:  in.Database,
		CreatedAt: now,
		UpdatedAt: now,
	}
	q := fmt.Sprintf(`INSERT INTO sys_file_references (id, name, database_name, created_at, updated_at)
	                  VALUES (%s, %s, %s, %s, %s)`,
		s.fileReferencePlaceholder(1), s.fileReferencePlaceholder(2), s.fileReferencePlaceholder(3),
		s.fileReferencePlaceholder(4), s.fileReferencePlaceholder(5))
	if _, err := s.driver.DB().ExecContext(ctx, q,
		ref.ID, ref.Name, ref.Database, ref.CreatedAt, ref.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed saving the file reference %q: %w", ref.Name, err)
	}
	return ref, nil
}

// UpdateFileReference renames a reference or maps it to another solution.
func (s *Service) UpdateFileReference(ctx context.Context, id string, in FileReferenceInput) (*FileReference, error) {
	current, err := s.GetFileReference(ctx, id)
	if err != nil {
		return nil, err
	}
	in, err = checkFileReference(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.fileReferenceNameTaken(ctx, in.Name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrFileReferenceExists, in.Name)
	}

	now := time.Now().UTC()
	q := fmt.Sprintf(`UPDATE sys_file_references SET name = %s, database_name = %s, updated_at = %s WHERE id = %s`,
		s.fileReferencePlaceholder(1), s.fileReferencePlaceholder(2),
		s.fileReferencePlaceholder(3), s.fileReferencePlaceholder(4))
	if _, err := s.driver.DB().ExecContext(ctx, q, in.Name, in.Database, now, id); err != nil {
		return nil, fmt.Errorf("failed updating the file reference %s: %w", id, err)
	}
	return &FileReference{
		ID: id, Name: in.Name, Database: in.Database,
		CreatedAt: current.CreatedAt, UpdatedAt: now,
	}, nil
}

// DeleteFileReference removes a file reference.
func (s *Service) DeleteFileReference(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_file_references WHERE id = %s`, s.fileReferencePlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting the file reference %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrFileReferenceNotFound, id)
	}
	return nil
}

const fileReferenceColumns = `id, name, database_name, created_at, updated_at`

// GetFileReference reads one file reference by id.
func (s *Service) GetFileReference(ctx context.Context, id string) (*FileReference, error) {
	q := fmt.Sprintf(`SELECT %s FROM sys_file_references WHERE id = %s`,
		fileReferenceColumns, s.fileReferencePlaceholder(1))
	ref, err := scanFileReference(s.driver.DB().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrFileReferenceNotFound, id)
	}
	return ref, err
}

// GetFileReferenceByName reads one file reference by name, case-insensitively,
// which is how a script step names it.
func (s *Service) GetFileReferenceByName(ctx context.Context, name string) (*FileReference, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: (no name)", ErrFileReferenceNotFound)
	}
	q := fmt.Sprintf(`SELECT %s FROM sys_file_references WHERE LOWER(name) = LOWER(%s)`,
		fileReferenceColumns, s.fileReferencePlaceholder(1))
	ref, err := scanFileReference(s.driver.DB().QueryRowContext(ctx, q, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrFileReferenceNotFound, name)
	}
	return ref, err
}

// ListFileReferences reads the file references, by name.
func (s *Service) ListFileReferences(ctx context.Context) ([]FileReference, error) {
	q := fmt.Sprintf(`SELECT %s FROM sys_file_references ORDER BY name ASC`, fileReferenceColumns)
	rows, err := s.driver.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed reading the file references: %w", err)
	}
	defer rows.Close()

	refs := []FileReference{}
	for rows.Next() {
		ref, err := scanFileReference(rows)
		if err != nil {
			return nil, err
		}
		refs = append(refs, *ref)
	}
	return refs, rows.Err()
}

func (s *Service) fileReferenceNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_file_references WHERE LOWER(name) = LOWER(%s) AND id <> %s`,
		s.fileReferencePlaceholder(1), s.fileReferencePlaceholder(2))
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, q, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed checking the name %q: %w", name, err)
	}
	return count > 0, nil
}

func scanFileReference(row rowScanner) (*FileReference, error) {
	var ref FileReference
	var database sql.NullString
	if err := row.Scan(&ref.ID, &ref.Name, &database, &ref.CreatedAt, &ref.UpdatedAt); err != nil {
		return nil, err
	}
	ref.Database = database.String
	return &ref, nil
}
