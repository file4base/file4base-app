package data

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Record identity (#208).
//
// Every record has a record ID, the number it was created with, and a
// modification count, as FileMaker's do (see dbal.ModCountColumn): the
// Data API's recordId and modId, which Get ( RecordID ) and
// Get ( RecordModificationCount ) answer.
//
// A record ID is taken from the table's allocator before the record is
// written (ReserveRecordIDs), never inside the write's transaction: a create
// that fails has spent its ID, as in FileMaker, and a long import does not
// hold the allocator. A formula computed for the new record knows it.
//
// The modification count goes up by one with every write that modifies the
// record, by the rule a modification stamp follows: a write modifies a
// record when it changes the value of a field stored with it, not a
// calculation, a summary or a global field. A value sent back as it is
// changes nothing, and so does a recalculation.

// Identity is a record's record ID and modification count.
type Identity struct {
	RecordID int64
	ModID    int64
}

// ErrModIDMismatch is returned by a write conditioned on a modification
// count the record no longer has: FileMaker's error 306, "Record
// modification ID does not match".
var ErrModIDMismatch = errors.New("record modification ID does not match")

// ErrModCountUnavailable is returned by a write conditioned on a modification
// count to a table that does not count modifications yet (its upgrade waits
// for a later sign-in): the condition could not be checked.
var ErrModCountUnavailable = errors.New("the table does not count record modifications yet")

// identityBatch bounds the ids one statement looks up.
const identityBatch = 500

// ReserveRecordIDs takes n record IDs from a table's allocator, in
// ascending order. Each is given once: an ID reserved and not used is lost,
// as FileMaker loses the ID of a record whose creation fails. A table that
// has no creation order column yet (see schema.EnsureSystemTables) has no
// allocator: it gets no IDs, and its records are numbered by the engine.
func ReserveRecordIDs(ctx context.Context, driver dbal.DatabaseDriver, tableName string, n int) ([]int64, error) {
	if n <= 0 {
		return nil, nil
	}
	has, err := HasCreationOrder(ctx, driver, tableName)
	if err != nil || !has {
		return nil, err
	}
	dialect := driver.Dialect()
	db := driver.DB()
	if dialect.Engine() == dbal.EnginePostgres {
		rows, err := db.QueryContext(ctx, `SELECT nextval(`+dbal.RecordIDSequenceSQL("$1")+`) FROM generate_series(1, $2)`,
			dialect.QuoteIdentifier(tableName), n)
		if err != nil {
			return nil, fmt.Errorf("failed reserving record IDs of %s: %w", tableName, err)
		}
		defer rows.Close()
		ids := make([]int64, 0, n)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		slices.Sort(ids)
		return ids, nil
	}

	// MariaDB: the table's row of sys_record_ids is advanced by n and the
	// statement answers its new value (LAST_INSERT_ID(expr)), in one atomic
	// step of its own.
	for attempt := 0; attempt < 2; attempt++ {
		res, err := db.ExecContext(ctx, `UPDATE `+dbal.RecordIDsTable+` SET last_id = LAST_INSERT_ID(last_id + ?) WHERE table_name = ?`,
			n, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed reserving record IDs of %s: %w", tableName, err)
		}
		if matched, _ := res.RowsAffected(); matched == 0 {
			if err := seedRecordIDs(ctx, db, dialect, tableName); err != nil {
				return nil, err
			}
			continue
		}
		last, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("failed reserving record IDs of %s: %w", tableName, err)
		}
		ids := make([]int64, n)
		for i := range ids {
			ids[i] = last - int64(n) + 1 + int64(i)
		}
		return ids, nil
	}
	return nil, fmt.Errorf("failed reserving record IDs of %s: its allocator could not be created", tableName)
}

// seedRecordIDs gives a MariaDB table its row of sys_record_ids, past every
// record ID the table holds and every one its AUTO_INCREMENT counter gave,
// a record since deleted included. Another server seeding it at the same
// time leaves the first row written.
func seedRecordIDs(ctx context.Context, db *sql.DB, dialect dbal.Dialect, tableName string) error {
	q := `INSERT IGNORE INTO ` + dbal.RecordIDsTable + ` (table_name, last_id) SELECT ?, GREATEST(COALESCE(MAX(` +
		dialect.QuoteIdentifier(dbal.CreationOrderColumn) + `), 0), COALESCE((SELECT AUTO_INCREMENT - 1 FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?), 0)) FROM ` + dialect.QuoteIdentifier(tableName)
	if _, err := db.ExecContext(ctx, q, tableName, tableName); err != nil {
		return fmt.Errorf("failed creating the record ID allocator of %s: %w", tableName, err)
	}
	return nil
}

// ClaimRecordIDs moves a table's allocator past last, so that records
// written with IDs up to last, as a restore writes the IDs it carries, are
// never given another's, and returns the highest ID it had given before, 0
// when none: an ID above it was never a record's. An allocator already past
// last stays where it is.
//
// On MariaDB the allocator is read and moved in one statement, so a record
// created meanwhile takes an ID either before the claim or past last. A
// PostgreSQL sequence cannot be read and moved at once: a record created
// between the two can take an ID a restore also writes, and one of the two
// inserts is then refused by the column's unique constraint.
func ClaimRecordIDs(ctx context.Context, driver dbal.DatabaseDriver, tableName string, last int64) (int64, error) {
	has, err := HasCreationOrder(ctx, driver, tableName)
	if err != nil || !has {
		return 0, err
	}
	dialect := driver.Dialect()
	db := driver.DB()
	if dialect.Engine() == dbal.EnginePostgres {
		var given, set sql.NullInt64
		err := db.QueryRowContext(ctx, `SELECT g, CASE WHEN $2::bigint > g THEN setval(s::regclass, $2::bigint) END
			FROM (SELECT COALESCE(pg_sequence_last_value(s::regclass), 0) AS g, s
			FROM (SELECT `+dbal.RecordIDSequenceSQL("$1")+` AS s) q) r`,
			dialect.QuoteIdentifier(tableName), last).Scan(&given, &set)
		if err != nil {
			return 0, fmt.Errorf("failed claiming the record IDs of %s: %w", tableName, err)
		}
		return given.Int64, nil
	}
	if err := seedRecordIDs(ctx, db, dialect, tableName); err != nil {
		return 0, err
	}
	// LAST_INSERT_ID(expr) keeps the value the row had, which the statement
	// answers.
	res, err := db.ExecContext(ctx, `UPDATE `+dbal.RecordIDsTable+` SET last_id = GREATEST(LAST_INSERT_ID(last_id), ?) WHERE table_name = ?`,
		last, tableName)
	if err != nil {
		return 0, fmt.Errorf("failed claiming the record IDs of %s: %w", tableName, err)
	}
	given, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed claiming the record IDs of %s: %w", tableName, err)
	}
	return given, nil
}

// recoverRecordIDs moves a table's allocator past every record ID the table
// holds, after an insert was refused because its ID was taken: by a record
// written without the allocator, as a server of an earlier version writes
// one on MariaDB, or by hand. The next ID reserved is free.
func recoverRecordIDs(ctx context.Context, driver dbal.DatabaseDriver, tableName string) {
	dialect := driver.Dialect()
	var highest sql.NullInt64
	if err := driver.DB().QueryRowContext(ctx, fmt.Sprintf("SELECT MAX(%s) FROM %s",
		dialect.QuoteIdentifier(dbal.CreationOrderColumn), dialect.QuoteIdentifier(tableName))).Scan(&highest); err != nil {
		return
	}
	_, _ = ClaimRecordIDs(ctx, driver, tableName, highest.Int64)
}

// ForgetRecordIDs drops a deleted table's allocator. PostgreSQL drops the
// sequence with the table.
func ForgetRecordIDs(ctx context.Context, q execer, dialect dbal.Dialect, tableName string) error {
	if dialect.Engine() != dbal.EngineMariaDB {
		return nil
	}
	_, err := q.ExecContext(ctx, `DELETE FROM `+dbal.RecordIDsTable+` WHERE table_name = ?`, tableName)
	return err
}

// RecordIdentity reads the identity of a record.
func (s *Service) RecordIdentity(ctx context.Context, tableName, id string) (Identity, error) {
	if _, err := s.tableFields(ctx, tableName); err != nil {
		return Identity{}, err
	}
	found, err := s.recordIdentities(ctx, s.driver.DB(), tableName, []string{id})
	if err != nil {
		return Identity{}, err
	}
	// The one record the engine matched, however its collation spells id.
	for _, identity := range found {
		return identity, nil
	}
	return Identity{}, fmt.Errorf("%w: %s", ErrRecordNotFound, id)
}

// RecordIdentities reads the identities of records, by id. A record that is
// not there is not in the answer.
func (s *Service) RecordIdentities(ctx context.Context, tableName string, ids []string) (map[string]Identity, error) {
	if _, err := s.tableFields(ctx, tableName); err != nil {
		return nil, err
	}
	return s.recordIdentities(ctx, s.driver.DB(), tableName, ids)
}

func (s *Service) recordIdentities(ctx context.Context, q execer, tableName string, ids []string) (map[string]Identity, error) {
	found := make(map[string]Identity, len(ids))
	columns, err := s.identityColumns(ctx, tableName)
	if err != nil || columns == "" {
		return found, err
	}
	dialect := s.driver.Dialect()
	for start := 0; start < len(ids); start += identityBatch {
		chunk := ids[start:min(start+identityBatch, len(ids))]
		marks := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for i, id := range chunk {
			marks[i] = dialect.Placeholder(i + 1)
			args[i] = id
		}
		rows, err := q.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
			dialect.QuoteIdentifier("id"), columns, dialect.QuoteIdentifier(tableName),
			dialect.QuoteIdentifier("id"), strings.Join(marks, ", ")), args...)
		if err != nil {
			return nil, fmt.Errorf("failed reading the record IDs of %s: %w", tableName, err)
		}
		for rows.Next() {
			var id string
			var identity Identity
			if err := rows.Scan(&id, &identity.RecordID, &identity.ModID); err != nil {
				rows.Close()
				return nil, err
			}
			found[id] = identity
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

// identityColumns is the select list of a table's record ID and
// modification count, "" for a table that has no record IDs yet. A table
// that has no modification count yet counts no modification.
func (s *Service) identityColumns(ctx context.Context, tableName string) (string, error) {
	hasID, err := HasCreationOrder(ctx, s.driver, tableName)
	if err != nil || !hasID {
		return "", err
	}
	hasMod, err := HasModCount(ctx, s.driver, tableName)
	if err != nil {
		return "", err
	}
	dialect := s.driver.Dialect()
	columns := dialect.QuoteIdentifier(dbal.CreationOrderColumn)
	if hasMod {
		return columns + ", " + dialect.QuoteIdentifier(dbal.ModCountColumn), nil
	}
	return columns + ", 0", nil
}

// FindByRecordID returns the id of a table's record with that record ID.
func (s *Service) FindByRecordID(ctx context.Context, tableName string, recordID int64) (string, error) {
	if _, err := s.tableFields(ctx, tableName); err != nil {
		return "", err
	}
	has, err := HasCreationOrder(ctx, s.driver, tableName)
	if err != nil {
		return "", err
	}
	if !has {
		return "", fmt.Errorf("%w: record ID %d", ErrRecordNotFound, recordID)
	}
	dialect := s.driver.Dialect()
	var id string
	err = s.driver.DB().QueryRowContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier(dbal.CreationOrderColumn), dialect.Placeholder(1)), recordID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: record ID %d", ErrRecordNotFound, recordID)
	}
	if err != nil {
		return "", fmt.Errorf("failed finding record ID %d of %s: %w", recordID, tableName, err)
	}
	return id, nil
}

// UpdateRowIfModID updates a record as UpdateRow does, provided it still has
// the modification count modID, as a Data API edit given a modId does: a
// record modified since is not written, and the answer is
// ErrModIDMismatch. The count is checked first, so a mismatched write enters
// nothing, and again by the UPDATE itself, so a write made in between is
// not overwritten.
func (s *Service) UpdateRowIfModID(
	ctx context.Context, tableName, id string, modID int64, updates map[string]interface{},
) (map[string]interface{}, error) {
	return s.updateRowIf(ctx, tableName, id, updates, writeMode(ctx), &modID)
}

// checkModID refuses a write conditioned on a modification count the record
// does not have.
func (s *Service) checkModID(ctx context.Context, tableName, id string, modID int64) error {
	if has, err := HasModCount(ctx, s.driver, tableName); err != nil {
		return err
	} else if !has {
		return ErrModCountUnavailable
	}
	identity, err := s.RecordIdentity(ctx, tableName, id)
	if err != nil {
		return err
	}
	if identity.ModID != modID {
		return ErrModIDMismatch
	}
	return nil
}

// modification is what a write does to a record's modification count.
type modification struct {
	// counted is true when the table counts modifications at all.
	counted bool
	// modifies is true when the write modifies the record.
	modifies bool
	// read is the record's identity when the write compared its values, and
	// found tells whether the record was there; current is what it held.
	read    Identity
	found   bool
	current map[string]interface{}
	// modifying are the fields a write modifies the record by changing.
	modifying map[string]bool
}

// countsModification reports whether writing updates modifies the record
// id, as it is stored.
func (s *Service) countsModification(
	ctx context.Context, tableName, id string, fields fieldSet, updates map[string]interface{},
) (modification, error) {
	has, err := HasModCount(ctx, s.driver, tableName)
	if err != nil || !has {
		return modification{}, err
	}
	modifying, err := s.modifyingFields(ctx, tableName)
	if err != nil {
		return modification{}, err
	}
	current, read, found, err := storedRowIdentity(ctx, s.driver.DB(), s.driver.Dialect(), tableName, id, fields)
	if err != nil {
		return modification{}, err
	}
	return modification{counted: true, modifies: modifiesRecord(fields, modifying, updates, current),
		read: read, found: found, current: current, modifying: modifying}, nil
}

// alsoModifiedBy notes what the auto-enter options entered as a consequence
// of the write: a value they change modifies the record too.
func (m *modification) alsoModifiedBy(fields fieldSet, updates map[string]interface{}) {
	if m.counted && !m.modifies {
		m.modifies = modifiesRecord(fields, m.modifying, updates, m.current)
	}
}

// countSQL is the assignment an UPDATE counts the modification with, its
// value bound at placeholder index next, or "" when it counts none. Whether
// the write modifies the record was decided on the values read with the
// count m.read: when the record has been written since, that decision no
// longer holds, and the write is counted, which can only make a write
// conditioned on the count it read fail, never let one through.
func (m modification) countSQL(dialect dbal.Dialect, next int) (string, []interface{}) {
	if !m.counted {
		return "", nil
	}
	modCount := dialect.QuoteIdentifier(dbal.ModCountColumn)
	if m.modifies || !m.found {
		return modCount + " = " + modCount + " + 1", nil
	}
	return fmt.Sprintf("%s = %s + CASE WHEN %s = %s THEN 0 ELSE 1 END", modCount, modCount, modCount,
		dialect.Placeholder(next)), []interface{}{m.read.ModID}
}

// storedRowIdentity reads a record as storedRow does, with its identity
// read by the same statement. found is false when it is not there.
func storedRowIdentity(
	ctx context.Context, q execer, dialect dbal.Dialect, tableName, id string, fields fieldSet,
) (map[string]interface{}, Identity, bool, error) {
	query := fmt.Sprintf("SELECT %s, %s AS f4b_record_id, %s AS f4b_mod_id FROM %s WHERE %s = %s LIMIT 1",
		selectList(dialect, fields), dialect.QuoteIdentifier(dbal.CreationOrderColumn),
		dialect.QuoteIdentifier(dbal.ModCountColumn), dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"), dialect.Placeholder(1))
	rows, err := q.QueryContext(ctx, query, id)
	if err != nil {
		return nil, Identity{}, false, err
	}
	defer rows.Close()
	stored, err := rowsToMaps(rows, fields)
	if err != nil || len(stored) == 0 {
		return map[string]interface{}{}, Identity{}, false, err
	}
	row := stored[0]
	var identity Identity
	for key, target := range map[string]*int64{"f4b_record_id": &identity.RecordID, "f4b_mod_id": &identity.ModID} {
		n, ok := wholeNumberOf(row[key])
		if !ok {
			return nil, Identity{}, false, fmt.Errorf("failed reading the identity of %s %s", tableName, id)
		}
		*target = n
		delete(row, key)
	}
	return row, identity, true, nil
}

// wholeNumberOf reads an integer column as the driver hands it.
func wholeNumberOf(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int:
		return int64(n), true
	case uint64:
		return int64(n), n <= 1<<63-1
	case []byte:
		parsed, err := strconv.ParseInt(string(n), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		return parsed, err == nil
	}
	return 0, false
}

// modifyingFields are the fields of a table a write modifies a record by
// changing: those stored with it, not a calculation, a summary or a global.
func (s *Service) modifyingFields(ctx context.Context, tableName string) (map[string]bool, error) {
	dialect := s.driver.Dialect()
	rows, err := s.driver.DB().QueryContext(ctx, `SELECT c.name, c.field_type, c.default_value FROM sys_columns c
		JOIN sys_tables t ON t.id = c.table_id WHERE t.name = `+dialect.Placeholder(1), tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the fields of %s: %w", tableName, err)
	}
	defer rows.Close()
	modifying := map[string]bool{}
	for rows.Next() {
		var name, fieldType string
		var options sql.NullString
		if err := rows.Scan(&name, &fieldType, &options); err != nil {
			return nil, err
		}
		switch dbal.AgnosticFieldType(fieldType) {
		case dbal.FieldTypeCalculation, dbal.FieldTypeSummary:
			continue
		}
		if options.Valid {
			if opts, err := autoenter.Parse(&options.String); err == nil && opts.StorageGlobal {
				continue
			}
		}
		modifying[name] = true
	}
	return modifying, rows.Err()
}

// modifiesRecord reports whether writing updates over the stored record
// current modifies it: whether it changes the value of a field in modifying.
func modifiesRecord(fields fieldSet, modifying map[string]bool, updates, current map[string]interface{}) bool {
	compare := &autoEnter{types: fields}
	for key, value := range updates {
		base := compare.baseOf(key)
		if !modifying[base] || base == "id" {
			continue
		}
		if fields.typeOf(base) == dbal.FieldTypeContainer {
			// What is written may be the bytes; what is read is their base64.
			if b, ok := value.([]byte); ok {
				value = base64.StdEncoding.EncodeToString(b)
			}
		}
		if !compare.same(base, value, current[key]) {
			return true
		}
	}
	return false
}

// identityScope keeps the identities of records known to one write or read,
// by table and id: those of records being created, which are not stored
// yet, and those read ahead for many records at once.
type identityScope struct {
	mu    sync.Mutex
	known map[identityKey]Identity
}

// identityKey names a record of a table of one database: an external
// relationship can reach a table of the same name, with the same ids, in
// another.
type identityKey struct {
	db        *sql.DB
	table, id string
}

type identityScopeKey struct{}

// withIdentityScope gives ctx a scope, unless it has one.
func withIdentityScope(ctx context.Context) (context.Context, *identityScope) {
	if scope, ok := ctx.Value(identityScopeKey{}).(*identityScope); ok {
		return ctx, scope
	}
	scope := &identityScope{known: map[identityKey]Identity{}}
	return context.WithValue(ctx, identityScopeKey{}, scope), scope
}

func identityScopeOf(ctx context.Context) *identityScope {
	scope, _ := ctx.Value(identityScopeKey{}).(*identityScope)
	return scope
}

func (sc *identityScope) put(db *sql.DB, tableName, id string, identity Identity) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.known[identityKey{db, tableName, id}] = identity
}

func (sc *identityScope) get(db *sql.DB, tableName, id string) (Identity, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	identity, ok := sc.known[identityKey{db, tableName, id}]
	return identity, ok
}

// forget drops what the scope knows, as a scan moves to its next page.
func (sc *identityScope) forget() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	clear(sc.known)
}

// prefetchIdentities reads ahead the identities of rows of a table into the
// scope of ctx, for formulas that read them.
func (s *Service) prefetchIdentities(ctx context.Context, tableName string, rows []map[string]interface{}) error {
	scope := identityScopeOf(ctx)
	if scope == nil {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id, _ := row["id"].(string); id != "" {
			if _, known := scope.get(s.driver.DB(), tableName, id); !known {
				ids = append(ids, id)
			}
		}
	}
	found, err := s.recordIdentities(ctx, s.driver.DB(), tableName, ids)
	if err != nil {
		return err
	}
	for id, identity := range found {
		scope.put(s.driver.DB(), tableName, id, identity)
	}
	return nil
}

// identityOf is the identity of a record of a table, as a formula reads it:
// the one a record being created was given, else what is stored. A record
// that is neither, one not created yet, has none.
func (s *Service) identityOf(ctx context.Context, tableName string, row map[string]interface{}) (*Identity, error) {
	id, _ := row["id"].(string)
	scope := identityScopeOf(ctx)
	if scope != nil {
		// A record being created that has no id yet is known as "".
		if identity, ok := scope.get(s.driver.DB(), tableName, id); ok {
			return &identity, nil
		}
	}
	if id == "" {
		return nil, nil
	}
	found, err := s.recordIdentities(ctx, s.driver.DB(), tableName, []string{id})
	if err != nil {
		return nil, err
	}
	identity, ok := found[id]
	if !ok {
		return nil, nil
	}
	if scope != nil {
		scope.put(s.driver.DB(), tableName, id, identity)
	}
	return &identity, nil
}

// readsIdentity reports whether a formula reads a record's identity.
func readsIdentity(expr *calc.Expr) bool {
	for _, selector := range expr.GetSelectors() {
		if selector == "RECORDID" || selector == "RECORDMODIFICATIONCOUNT" {
			return true
		}
	}
	return false
}

// identitySelector answers Get ( RecordID ) and
// Get ( RecordModificationCount ) for a record; ok is false for any other
// selector.
func identitySelector(selector string, identity func() (*Identity, error)) (any, bool, error) {
	if selector != "RECORDID" && selector != "RECORDMODIFICATIONCOUNT" {
		return nil, false, nil
	}
	known, err := identity()
	if err != nil || known == nil {
		return nil, true, err
	}
	if selector == "RECORDID" {
		return known.RecordID, true, nil
	}
	return known.ModID, true, nil
}

// storedContext is what a stored calculation reads: its record, and the
// record's identity.
type storedContext struct {
	*calc.RecordContext
	identity func() (*Identity, error)
}

func (c *storedContext) Get(selector string) (any, error) {
	if v, ok, err := identitySelector(selector, c.identity); ok {
		return v, err
	}
	return c.RecordContext.Get(selector)
}
