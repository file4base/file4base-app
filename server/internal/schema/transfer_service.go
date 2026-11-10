package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// ErrTransferNotFound is returned when no transfer definition has the given
// id or name.
var ErrTransferNotFound = errors.New("transfer definition not found")

// ErrTransferExists is returned when the solution already has a transfer
// definition with that name.
var ErrTransferExists = errors.New("a transfer definition with that name already exists")

// ErrInvalidTransfer is returned for a definition that cannot be stored as
// given.
var ErrInvalidTransfer = errors.New("invalid transfer definition")

// Bounds on a transfer definition, so a stored definition stays something a
// developer wrote rather than a payload.
const (
	maxTransferName     = 128
	maxTransferMappings = 200
	maxTransferMatch    = 10
)

// Transfer modes: what the destination does with the rows the source sends.
const (
	// TransferModeAdd makes every source record a new destination record.
	TransferModeAdd = "add"

	// TransferModeUpdateMatching finds the destination record whose match
	// fields hold the same values and updates it. This is the mode that makes
	// a transfer replayable: running it twice updates the same records
	// instead of duplicating them.
	TransferModeUpdateMatching = "update_matching"
)

// What happens to a source record that matches no destination record. Only
// TransferModeUpdateMatching consults it.
const (
	// TransferUnmatchedAdd adds the record.
	TransferUnmatchedAdd = "add"

	// TransferUnmatchedSkip passes over the record and counts it as skipped.
	TransferUnmatchedSkip = "skip"
)

// What happens to a source value the destination field cannot hold.
const (
	// TransferRejectedAbort refuses the whole transfer: nothing is written,
	// and the run reports every rejected row. This is the default, and it is
	// the all-or-nothing contract a manual import already follows (#38).
	TransferRejectedAbort = "abort"

	// TransferRejectedSkip leaves the rejected rows out and commits the rest
	// in one transaction. The rejected rows are quarantined in the run
	// report, with the row, the field and why.
	TransferRejectedSkip = "skip"
)

// TransferEnd is one side of a transfer: a table of a solution.
//
// Database is the solution's database name. It is empty for the solution that
// holds the definition, so a definition carried in a solution bundle keeps
// working in the copy it is restored into.
type TransferEnd struct {
	Database string `json:"database,omitempty" msgpack:"database,omitempty"`
	Table    string `json:"table" msgpack:"table"`
}

// IsLocal reports whether this end is the solution holding the definition.
func (e TransferEnd) IsLocal() bool { return strings.TrimSpace(e.Database) == "" }

// DatabaseOr returns the end's database, or fallback when the end is local.
func (e TransferEnd) DatabaseOr(fallback string) string {
	if e.IsLocal() {
		return fallback
	}
	return strings.TrimSpace(e.Database)
}

// TransferMapping sends one field of the source table to one field of the
// destination table. Every field a transfer writes is mapped explicitly:
// there is no "match the names and hope", because two solutions that were
// developed apart never agree on every name.
type TransferMapping struct {
	SourceField      string `json:"source_field" msgpack:"source_field"`
	DestinationField string `json:"destination_field" msgpack:"destination_field"`
}

// TransferDefinition is a persisted, named record transfer between two
// solutions (#96).
//
// It holds no credentials and no connection details. Which accounts may run
// it is decided when it runs, by the session tokens the caller presents for
// each end; the definition only says what moves where.
type TransferDefinition struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Source      TransferEnd `json:"source"`
	Destination TransferEnd `json:"destination"`

	// FoundSet narrows what the source sends, in the same form a find takes.
	// Empty sends the whole table.
	FoundSet []data.FindRequest `json:"found_set,omitempty"`

	// Sort is the order the source reads its records in. A transfer of
	// parents before children does not depend on it, but a run report reads
	// better when the rows come in a stable order.
	Sort []data.SortField `json:"sort,omitempty"`

	Mappings []TransferMapping `json:"mappings"`

	// MatchFields are destination fields, and they are the stable business
	// keys a replay relies on. Every one of them has to be mapped, so the
	// source always supplies the key it is matched on.
	MatchFields []string `json:"match_fields,omitempty"`

	Mode        string `json:"mode"`
	OnUnmatched string `json:"on_unmatched,omitempty"`
	OnRejected  string `json:"on_rejected"`

	// DateOrder says how a date the source writes as 03/04/2011 is to be
	// read. There is no way to tell from the value, so the definition says.
	DateOrder string `json:"date_order"`

	// CreatedBy is the account that saved it.
	CreatedBy string `json:"created_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SourceFields are the source fields of the mappings, in mapping order.
func (t *TransferDefinition) SourceFields() []string {
	fields := make([]string, 0, len(t.Mappings))
	for _, m := range t.Mappings {
		fields = append(fields, m.SourceField)
	}
	return fields
}

// DestinationFields are the destination fields of the mappings, in mapping
// order, which is the column order a transfer hands the destination import.
func (t *TransferDefinition) DestinationFields() []string {
	fields := make([]string, 0, len(t.Mappings))
	for _, m := range t.Mappings {
		fields = append(fields, m.DestinationField)
	}
	return fields
}

// TransferInput is what a caller supplies to save or replace a definition.
type TransferInput struct {
	Name        string             `json:"name"`
	Source      TransferEnd        `json:"source"`
	Destination TransferEnd        `json:"destination"`
	FoundSet    []data.FindRequest `json:"found_set,omitempty"`
	Sort        []data.SortField   `json:"sort,omitempty"`
	Mappings    []TransferMapping  `json:"mappings"`
	MatchFields []string           `json:"match_fields,omitempty"`
	Mode        string             `json:"mode"`
	OnUnmatched string             `json:"on_unmatched,omitempty"`
	OnRejected  string             `json:"on_rejected,omitempty"`
	DateOrder   string             `json:"date_order,omitempty"`
}

// transferPayload is the part of a definition stored as JSON. The ends are
// stored in their own columns so a definition can be found by the table it
// reads or writes without decoding every row.
type transferPayload struct {
	FoundSet    []data.FindRequest `json:"found_set,omitempty"`
	Sort        []data.SortField   `json:"sort,omitempty"`
	Mappings    []TransferMapping  `json:"mappings"`
	MatchFields []string           `json:"match_fields,omitempty"`
	Mode        string             `json:"mode"`
	OnUnmatched string             `json:"on_unmatched,omitempty"`
	OnRejected  string             `json:"on_rejected"`
	DateOrder   string             `json:"date_order"`
}

func (s *Service) transferPlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// checkTransfer validates a definition's structure.
//
// It deliberately does not check that the mapped fields exist: one end of a
// transfer is usually another solution's database, which this service cannot
// reach, and a definition that could only be saved while the far solution was
// online would be useless. The fields are resolved when the transfer runs,
// against both databases, and a mapping that names a field neither table has
// stops the run before anything is written.
func checkTransfer(in TransferInput) (TransferInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("%w: a transfer needs a name", ErrInvalidTransfer)
	}
	if len([]rune(in.Name)) > maxTransferName {
		return in, fmt.Errorf("%w: the name of a transfer is at most %d characters",
			ErrInvalidTransfer, maxTransferName)
	}

	var err error
	if in.Source, err = checkTransferEnd(in.Source, "source"); err != nil {
		return in, err
	}
	if in.Destination, err = checkTransferEnd(in.Destination, "destination"); err != nil {
		return in, err
	}
	if in.Source.DatabaseOr("") == in.Destination.DatabaseOr("") &&
		in.Source.Table == in.Destination.Table {
		return in, fmt.Errorf("%w: the source and the destination are the same table", ErrInvalidTransfer)
	}

	if len(in.Mappings) == 0 {
		return in, fmt.Errorf("%w: a transfer needs at least one field mapping", ErrInvalidTransfer)
	}
	if len(in.Mappings) > maxTransferMappings {
		return in, fmt.Errorf("%w: a transfer maps at most %d fields",
			ErrInvalidTransfer, maxTransferMappings)
	}
	mapped := map[string]struct{}{}
	clean := make([]TransferMapping, 0, len(in.Mappings))
	for i, m := range in.Mappings {
		source := dbal.NormalizeIdentifier(m.SourceField)
		destination := dbal.NormalizeIdentifier(m.DestinationField)
		if source == "" || destination == "" {
			return in, fmt.Errorf("%w: mapping %d needs a source field and a destination field",
				ErrInvalidTransfer, i+1)
		}
		if _, clash := mapped[destination]; clash {
			return in, fmt.Errorf("%w: two source fields are sent to %q", ErrInvalidTransfer, destination)
		}
		mapped[destination] = struct{}{}
		clean = append(clean, TransferMapping{SourceField: source, DestinationField: destination})
	}
	in.Mappings = clean

	switch in.Mode {
	case "":
		in.Mode = TransferModeUpdateMatching
	case TransferModeAdd, TransferModeUpdateMatching:
	default:
		return in, fmt.Errorf("%w: %q is not a transfer mode; it is %q or %q",
			ErrInvalidTransfer, in.Mode, TransferModeAdd, TransferModeUpdateMatching)
	}

	keys := make([]string, 0, len(in.MatchFields))
	for _, field := range in.MatchFields {
		field = dbal.NormalizeIdentifier(field)
		if field == "" {
			continue
		}
		if _, ok := mapped[field]; !ok {
			return in, fmt.Errorf(
				"%w: records are matched on %q, so a source field has to be sent to it",
				ErrInvalidTransfer, field)
		}
		keys = append(keys, field)
	}
	if len(keys) > maxTransferMatch {
		return in, fmt.Errorf("%w: a transfer matches on at most %d fields",
			ErrInvalidTransfer, maxTransferMatch)
	}
	in.MatchFields = keys

	if in.Mode == TransferModeUpdateMatching && len(keys) == 0 {
		return in, fmt.Errorf(
			"%w: updating matching records needs at least one match field, and it is the "+
				"stable key a replay of this transfer depends on", ErrInvalidTransfer)
	}
	if in.Mode == TransferModeAdd && len(keys) > 0 {
		return in, fmt.Errorf("%w: %q adds every record, so it matches on nothing",
			ErrInvalidTransfer, TransferModeAdd)
	}

	switch in.OnUnmatched {
	case "":
		in.OnUnmatched = TransferUnmatchedAdd
	case TransferUnmatchedAdd, TransferUnmatchedSkip:
	default:
		return in, fmt.Errorf("%w: %q is not an unmatched-record policy; it is %q or %q",
			ErrInvalidTransfer, in.OnUnmatched, TransferUnmatchedAdd, TransferUnmatchedSkip)
	}

	switch in.OnRejected {
	case "":
		in.OnRejected = TransferRejectedAbort
	case TransferRejectedAbort, TransferRejectedSkip:
	default:
		return in, fmt.Errorf("%w: %q is not a rejected-row policy; it is %q or %q",
			ErrInvalidTransfer, in.OnRejected, TransferRejectedAbort, TransferRejectedSkip)
	}

	switch data.DateOrder(strings.ToLower(strings.TrimSpace(in.DateOrder))) {
	case "":
		in.DateOrder = string(data.DateISO)
	case data.DateISO, data.DateDMY, data.DateMDY:
		in.DateOrder = strings.ToLower(strings.TrimSpace(in.DateOrder))
	default:
		return in, fmt.Errorf("%w: %q is not a date order; it is %q, %q or %q",
			ErrInvalidTransfer, in.DateOrder, data.DateISO, data.DateDMY, data.DateMDY)
	}

	return in, nil
}

func checkTransferEnd(end TransferEnd, side string) (TransferEnd, error) {
	end.Database = strings.TrimSpace(end.Database)
	if end.Database != "" {
		if !dbal.IsValidDatabaseName(end.Database) {
			return end, fmt.Errorf("%w: %q is not a database name the %s can name",
				ErrInvalidTransfer, end.Database, side)
		}
		if dbal.IsProtectedDatabase(end.Database) {
			return end, fmt.Errorf("%w: %q is an engine-internal database, never a solution",
				ErrInvalidTransfer, end.Database)
		}
	}
	end.Table = dbal.NormalizeIdentifier(end.Table)
	if end.Table == "" {
		return end, fmt.Errorf("%w: a transfer needs the %s table", ErrInvalidTransfer, side)
	}
	if dbal.IsReservedTableName(end.Table) {
		return end, fmt.Errorf("%w: %q is an internal table, never a transfer %s",
			ErrInvalidTransfer, end.Table, side)
	}
	return end, nil
}

// CreateTransfer stores a new transfer definition. createdBy is the account
// saving it.
func (s *Service) CreateTransfer(ctx context.Context, in TransferInput, createdBy string) (*TransferDefinition, error) {
	in, err := checkTransfer(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.transferNameTaken(ctx, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrTransferExists, in.Name)
	}

	now := time.Now().UTC()
	def := &TransferDefinition{
		ID:          uuid.NewString(),
		Name:        in.Name,
		Source:      in.Source,
		Destination: in.Destination,
		FoundSet:    in.FoundSet,
		Sort:        in.Sort,
		Mappings:    in.Mappings,
		MatchFields: in.MatchFields,
		Mode:        in.Mode,
		OnUnmatched: in.OnUnmatched,
		OnRejected:  in.OnRejected,
		DateOrder:   in.DateOrder,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	encoded, err := encodeTransferPayload(def)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`INSERT INTO sys_transfers
	    (id, name, source_database, source_table, destination_database, destination_table,
	     definition, created_by, created_at, updated_at)
	    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		s.transferPlaceholder(1), s.transferPlaceholder(2), s.transferPlaceholder(3),
		s.transferPlaceholder(4), s.transferPlaceholder(5), s.transferPlaceholder(6),
		s.transferPlaceholder(7), s.transferPlaceholder(8), s.transferPlaceholder(9),
		s.transferPlaceholder(10))
	var owner interface{}
	if strings.TrimSpace(createdBy) != "" {
		owner = createdBy
	}
	if _, err := s.driver.DB().ExecContext(ctx, q,
		def.ID, def.Name, def.Source.Database, def.Source.Table,
		def.Destination.Database, def.Destination.Table,
		encoded, owner, def.CreatedAt, def.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed saving the transfer %q: %w", def.Name, err)
	}
	return def, nil
}

// UpdateTransfer replaces a transfer definition.
func (s *Service) UpdateTransfer(ctx context.Context, id string, in TransferInput) (*TransferDefinition, error) {
	current, err := s.GetTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	in, err = checkTransfer(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.transferNameTaken(ctx, in.Name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrTransferExists, in.Name)
	}

	def := &TransferDefinition{
		ID:          id,
		Name:        in.Name,
		Source:      in.Source,
		Destination: in.Destination,
		FoundSet:    in.FoundSet,
		Sort:        in.Sort,
		Mappings:    in.Mappings,
		MatchFields: in.MatchFields,
		Mode:        in.Mode,
		OnUnmatched: in.OnUnmatched,
		OnRejected:  in.OnRejected,
		DateOrder:   in.DateOrder,
		CreatedBy:   current.CreatedBy,
		CreatedAt:   current.CreatedAt,
		UpdatedAt:   time.Now().UTC(),
	}
	encoded, err := encodeTransferPayload(def)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`UPDATE sys_transfers SET name = %s, source_database = %s, source_table = %s,
	    destination_database = %s, destination_table = %s, definition = %s, updated_at = %s
	    WHERE id = %s`,
		s.transferPlaceholder(1), s.transferPlaceholder(2), s.transferPlaceholder(3),
		s.transferPlaceholder(4), s.transferPlaceholder(5), s.transferPlaceholder(6),
		s.transferPlaceholder(7), s.transferPlaceholder(8))
	if _, err := s.driver.DB().ExecContext(ctx, q,
		def.Name, def.Source.Database, def.Source.Table,
		def.Destination.Database, def.Destination.Table,
		encoded, def.UpdatedAt, id); err != nil {
		return nil, fmt.Errorf("failed updating the transfer %s: %w", id, err)
	}
	return def, nil
}

// DeleteTransfer removes a transfer definition.
func (s *Service) DeleteTransfer(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_transfers WHERE id = %s`, s.transferPlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting the transfer %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrTransferNotFound, id)
	}
	return nil
}

const transferColumns = `id, name, source_database, source_table, destination_database,
	destination_table, definition, created_by, created_at, updated_at`

// GetTransfer reads one transfer definition by id.
func (s *Service) GetTransfer(ctx context.Context, id string) (*TransferDefinition, error) {
	q := fmt.Sprintf(`SELECT %s FROM sys_transfers WHERE id = %s`,
		transferColumns, s.transferPlaceholder(1))
	def, err := scanTransfer(s.driver.DB().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrTransferNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return def, nil
}

// GetTransferByName reads one transfer definition by name, case-insensitively.
//
// A script step names the transfer it runs, because a name is what a
// developer writes and what survives a solution bundle being restored into
// another database with fresh ids.
func (s *Service) GetTransferByName(ctx context.Context, name string) (*TransferDefinition, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: (no name)", ErrTransferNotFound)
	}
	q := fmt.Sprintf(`SELECT %s FROM sys_transfers WHERE LOWER(name) = LOWER(%s)`,
		transferColumns, s.transferPlaceholder(1))
	def, err := scanTransfer(s.driver.DB().QueryRowContext(ctx, q, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrTransferNotFound, name)
	}
	if err != nil {
		return nil, err
	}
	return def, nil
}

// ListTransfers reads the transfer definitions, by name.
func (s *Service) ListTransfers(ctx context.Context) ([]TransferDefinition, error) {
	q := fmt.Sprintf(`SELECT %s FROM sys_transfers ORDER BY name ASC`, transferColumns)
	rows, err := s.driver.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed reading the transfer definitions: %w", err)
	}
	defer rows.Close()

	transfers := []TransferDefinition{}
	for rows.Next() {
		def, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		transfers = append(transfers, *def)
	}
	return transfers, rows.Err()
}

func (s *Service) transferNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_transfers WHERE LOWER(name) = LOWER(%s) AND id <> %s`,
		s.transferPlaceholder(1), s.transferPlaceholder(2))
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, q, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed checking the name %q: %w", name, err)
	}
	return count > 0, nil
}

func encodeTransferPayload(def *TransferDefinition) (string, error) {
	encoded, err := json.Marshal(transferPayload{
		FoundSet:    def.FoundSet,
		Sort:        def.Sort,
		Mappings:    def.Mappings,
		MatchFields: def.MatchFields,
		Mode:        def.Mode,
		OnUnmatched: def.OnUnmatched,
		OnRejected:  def.OnRejected,
		DateOrder:   def.DateOrder,
	})
	if err != nil {
		return "", fmt.Errorf("the transfer %q cannot be stored: %w", def.Name, err)
	}
	return string(encoded), nil
}

func scanTransfer(row rowScanner) (*TransferDefinition, error) {
	var def TransferDefinition
	var encoded string
	var createdBy sql.NullString
	var sourceDB, destinationDB sql.NullString
	if err := row.Scan(&def.ID, &def.Name, &sourceDB, &def.Source.Table,
		&destinationDB, &def.Destination.Table, &encoded, &createdBy,
		&def.CreatedAt, &def.UpdatedAt); err != nil {
		return nil, err
	}
	def.Source.Database = sourceDB.String
	def.Destination.Database = destinationDB.String
	def.CreatedBy = createdBy.String

	var payload transferPayload
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return nil, fmt.Errorf("the transfer %q cannot be read: %w", def.Name, err)
	}
	def.FoundSet = payload.FoundSet
	def.Sort = payload.Sort
	def.Mappings = payload.Mappings
	def.MatchFields = payload.MatchFields
	def.Mode = payload.Mode
	def.OnUnmatched = payload.OnUnmatched
	def.OnRejected = payload.OnRejected
	def.DateOrder = payload.DateOrder
	return &def, nil
}
