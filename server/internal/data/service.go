package data

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/google/uuid"
)

// MaxPageSize is the largest number of rows a list or find request returns;
// larger limits are clamped to it.
const MaxPageSize = 1000

// FindCriterion represents a single field search condition
type FindCriterion struct {
	FieldName string      `json:"field_name"`
	Operator  string      `json:"operator"` // "=", "==", "!=", ">", "<", ">=", "<=", "LIKE", "RANGE", "IS_EMPTY", "IS_NOT_EMPTY"
	Value     interface{} `json:"value"`
	ValueTo   interface{} `json:"value_to,omitempty"` // For range queries

	// RelationshipID searches a field of a *related* record instead of the
	// record's own (#46): the criterion then finds the records that have a
	// related record matching it. Occurrence names the side to read when the
	// relationship joins a table to itself; Side ("left" or "right") names
	// the end when it joins an occurrence to itself, where the occurrence
	// alone names the right end (as `side` does for related reads).
	RelationshipID string `json:"relationship_id,omitempty"`
	Occurrence     string `json:"occurrence,omitempty"`
	Side           string `json:"side,omitempty"`
}

// isRelated reports whether the criterion searches a related record.
func (c FindCriterion) isRelated() bool {
	return strings.TrimSpace(c.RelationshipID) != ""
}

// FindRequest represents one disjunctive search request (OR unit)
type FindRequest struct {
	Criteria []FindCriterion `json:"criteria"`
	Omit     bool            `json:"omit"` // NOT condition
}

// SortField is one level of a sort order: records are ordered by the first
// field, ties are broken by the second, and so on.
type SortField struct {
	Field      string `json:"field"`
	Descending bool   `json:"descending"`
}

// QueryOptions controls sorting and pagination
type QueryOptions struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// SortBy and SortAsc are the single-field form, kept so existing callers
	// keep working. Sort supersedes them when it is given.
	SortBy  string `json:"sort_by,omitempty"`
	SortAsc bool   `json:"sort_asc"`

	Sort []SortField `json:"sort,omitempty"`

	// End is the end of the relationship a related read reads, needed when
	// the relationship joins an occurrence to itself; see SidesToward.
	End RelationEnd `json:"-"`
}

// SortOrder is the effective order: Sort when the caller gave one, otherwise
// the single SortBy/SortAsc pair, otherwise nothing.
func (o QueryOptions) SortOrder() []SortField {
	if len(o.Sort) > 0 {
		order := make([]SortField, 0, len(o.Sort))
		for _, level := range o.Sort {
			if strings.TrimSpace(level.Field) != "" {
				order = append(order, level)
			}
		}
		return order
	}
	if o.SortBy != "" {
		return []SortField{{Field: o.SortBy, Descending: !o.SortAsc}}
	}
	return nil
}

// orderByClause builds the ORDER BY for a sort order, checking every field
// against the table's registered fields so a caller cannot name a column that
// is not there (or inject one).
func orderByClause(dialect dbal.Dialect, fields fieldSet, tableName string, order []SortField) (string, error) {
	if len(order) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(order))
	seen := make(map[string]struct{}, len(order))
	for _, level := range order {
		if err := checkField(fields, tableName, level.Field); err != nil {
			return "", err
		}
		if _, repeated := seen[level.Field]; repeated {
			// A field twice in the order changes nothing after the first time.
			continue
		}
		seen[level.Field] = struct{}{}
		dir := "ASC"
		if level.Descending {
			dir = "DESC"
		}
		parts = append(parts, fmt.Sprintf("%s %s", dialect.QuoteIdentifier(level.Field), dir))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "ORDER BY " + strings.Join(parts, ", "), nil
}

// Service handles dynamic generic table CRUD and query translation
type Service struct {
	driver dbal.DatabaseDriver
	// canRead, when set, says whether the caller may read a table a
	// calculation reaches through relationships (see WithReadAccess).
	canRead func(ctx context.Context, tableName string) (bool, error)
	// peers, when set, opens the other solutions external occurrences read
	// from (see WithPeers).
	peers Peers
	// database is the name of the solution the driver is bound to (see
	// WithDatabase).
	database string
	// canWrite, when set, says whether the caller may create, edit or
	// delete the records of a table (see WithWriteAccess).
	canWrite func(ctx context.Context, tableName, action string) error
	// account is the account the service writes as (see WithAccount).
	account string
}

func NewService(driver dbal.DatabaseDriver) *Service {
	return &Service{driver: driver}
}

// ErrTableNotFound is returned when a request targets a table that is not a
// user table registered in the system catalog (sys_tables). System tables
// (sys_*) and any other physical table are never reachable through this service.
var ErrTableNotFound = errors.New("table not found")

// ErrUnknownField is returned when a request references a field that is not
// registered for the table in the system catalog (sys_columns).
var ErrUnknownField = errors.New("unknown field")

// ErrRecordNotFound is returned when a table has no record with that id.
var ErrRecordNotFound = errors.New("record not found")

// ErrInvalidValue is returned when a field value cannot be stored in its
// field, such as a CONTAINER value that is not base64.
var ErrInvalidValue = errors.New("invalid field value")

// fieldSet maps the registered field names of a table to their types.
type fieldSet map[string]dbal.AgnosticFieldType

// tableFields returns the registered fields of a user table and their types.
// Every user table owns at least its primary key column, so an empty result
// means the table is not part of the catalog.
func (s *Service) tableFields(ctx context.Context, tableName string) (fieldSet, error) {
	// Internal tables are never user data, even if a catalog row names one.
	if dbal.IsReservedTableName(tableName) {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, tableName)
	}
	q := `SELECT c.name, c.field_type, c.calculation_formula, c.max_repetitions FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = $1`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT c.name, c.field_type, c.calculation_formula, c.max_repetitions FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = ?`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed resolving table %s: %w", tableName, err)
	}
	defer rows.Close()

	fields := make(fieldSet)
	for rows.Next() {
		var name, fieldType string
		var formula *string
		var reps int
		if err := rows.Scan(&name, &fieldType, &formula, &reps); err != nil {
			return nil, err
		}
		if reps > 1 {
			fields[repetitionsKey(name)] = repeatsType(reps)
		}
		// A calculation is held in the column its formula's result needs, and
		// every read and write path cares about what is actually stored: a
		// numeric result has to be trimmed and compared like a number, which
		// is not what the declared type CALCULATION would say.
		fields[name] = StorageType(dbal.AgnosticFieldType(fieldType), formula)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, tableName)
	}
	return fields, nil
}

// checker returns the validation rules of a table's fields.
func (s *Service) checker(ctx context.Context, tableName string) (*validation.Checker, error) {
	fields, err := validation.LoadFields(ctx, s.driver.DB(), s.driver.Dialect(), tableName)
	if err != nil {
		return nil, err
	}
	return &validation.Checker{Dialect: s.driver.Dialect(), DB: s.driver.DB(), Table: tableName, Fields: fields}, nil
}

// decodeContainerValues turns the base64 text of CONTAINER fields into the
// bytes to store. JSON has no binary type, so the data API exchanges
// CONTAINER values as standard base64 (see rowsToMaps).
func decodeContainerValues(fields fieldSet, tableName string, record map[string]interface{}) error {
	for col, val := range record {
		if fields[col] != dbal.FieldTypeContainer {
			continue
		}
		text, ok := val.(string)
		if !ok {
			continue // nil, or bytes from an internal caller
		}
		b, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return fmt.Errorf("%w: %s.%s must be base64-encoded binary data", ErrInvalidValue, tableName, col)
		}
		record[col] = b
	}
	return nil
}

// selectList names the registered fields explicitly instead of SELECT *: only
// catalog fields are returned, and a statement prepared before a field was
// added is never reused with a different result shape (PostgreSQL rejects
// that with "cached plan must not change result type").
//
// A repeating field's other repetitions are read from its companion column,
// under the column's own name, which rowsToMaps maps back to repetitionsKey
// (see repetitions.go). An alias "field[]" would be cut at the engine's
// identifier limit for a long field name and clash with the field itself.
func selectList(dialect dbal.Dialect, fields fieldSet) string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, n := range names {
		names[i] = columnSQL(dialect, n)
	}
	return strings.Join(names, ", ")
}

// checkField checks that a field is registered for the table. It names a
// whole field, a column: a key "field[n]" of a record is checked by
// checkValueKey.
func checkField(fields fieldSet, tableName, fieldName string) error {
	if _, ok := fields[fieldName]; !ok || fields.isInternal(fieldName) {
		return fmt.Errorf("%w: %s.%s", ErrUnknownField, tableName, fieldName)
	}
	return nil
}

// InsertRow dynamically inserts a record into any table. The auto-enter
// options of its fields apply (see autoenter.go); a write made with a
// context marked by AsScript is a script's, not data entry.
func (s *Service) InsertRow(ctx context.Context, tableName string, record map[string]interface{}) (map[string]interface{}, error) {
	return s.insertRow(ctx, tableName, record, writeMode(ctx))
}

func (s *Service) insertRow(ctx context.Context, tableName string, record map[string]interface{}, mode autoMode) (map[string]interface{}, error) {
	if record == nil {
		record = make(map[string]interface{})
	}
	// A record handed back from a read carries its original texts; they are
	// not a field, and a client does not set them (#124).
	sourcetext.Strip(record)
	containermeta.Strip(record)

	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if err := normalizeRepetitionKeys(fields, tableName, record); err != nil {
		return nil, err
	}
	normalizeTemporalValues(fields, record)

	// The auto-enter options (Fields dialog) enter what the caller did not
	// give: a value given wins.
	if err := s.applyAutoEnter(ctx, tableName, fields, mode, record, ""); err != nil {
		return nil, err
	}
	if err := decodeContainerValues(fields, tableName, record); err != nil {
		return nil, err
	}

	// Ensure id exists
	if _, ok := record["id"]; !ok || record["id"] == nil || record["id"] == "" {
		record["id"] = uuid.NewString()
	}

	// Calculation fields are filled from their formulas (#30). They are
	// computed before the validation rules run, so a rule can check what the
	// formula produced.
	// A summary has no value in a record; anything sent for one is dropped
	// rather than stored where nothing would read it (#32).
	if err := s.dropSummaryValues(ctx, tableName, record); err != nil {
		return nil, err
	}
	if err := s.applyCalculations(ctx, tableName, record); err != nil {
		return nil, err
	}

	// Validation rules of the Fields dialog (#15)
	checker, err := s.checker(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if err := checker.Check(ctx, fmt.Sprint(record["id"]), record, nil, true); err != nil {
		return nil, err
	}

	dialect := s.driver.Dialect()
	writes, err := packRepetitions(ctx, s.driver.DB(), dialect, fields, tableName, "", record)
	if err != nil {
		return nil, err
	}
	columns := make([]string, 0, len(writes))
	placeholders := make([]string, 0, len(writes))
	values := make([]interface{}, 0, len(writes))

	idx := 1
	for col, val := range writes {
		columns = append(columns, columnSQL(dialect, col))
		placeholders = append(placeholders, dialect.Placeholder(idx))
		values = append(values, val)
		idx++
	}

	sqlQuery := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		dialect.QuoteIdentifier(tableName),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	db := s.driver.DB()
	if _, err := db.ExecContext(ctx, sqlQuery, values...); err != nil {
		if f, ok := validation.IsUniqueIndexViolation(err, tableName, checker.Fields); ok {
			return nil, validation.UniqueViolation(tableName, f)
		}
		return nil, fmt.Errorf("failed inserting into %s: %w", tableName, err)
	}

	return record, nil
}

// GetRow retrieves a single record by primary key id
func (s *Service) GetRow(ctx context.Context, tableName string, id string) (map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	sqlQuery := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = %s LIMIT 1",
		selectList(dialect, fields),
		dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"),
		dialect.Placeholder(1),
	)

	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, sqlQuery, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results, err := s.readRows(ctx, tableName, rows, fields)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrRecordNotFound, id)
	}
	return results[0], nil
}

// UpdateRow dynamically updates a row by id. The auto-enter options of its
// fields apply, as for InsertRow.
func (s *Service) UpdateRow(ctx context.Context, tableName string, id string, updates map[string]interface{}) (map[string]interface{}, error) {
	return s.updateRow(ctx, tableName, id, updates, writeMode(ctx))
}

func (s *Service) updateRow(ctx context.Context, tableName string, id string, updates map[string]interface{}, mode autoMode) (map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	sourcetext.Strip(updates)
	containermeta.Strip(updates)
	if err := normalizeRepetitionKeys(fields, tableName, updates); err != nil {
		return nil, err
	}
	normalizeTemporalValues(fields, updates)
	// Protect the primary key from alteration, also when it is sent as its
	// first repetition, "id[1]", which the line above makes "id".
	delete(updates, "id")
	// A summary has no value in a record, so anything sent for one is dropped
	// before the update is built. It happens here, not further down, so that
	// an update of summary fields alone is an update of nothing rather than a
	// statement with no columns to set (#32).
	if err := s.dropSummaryValues(ctx, tableName, updates); err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return s.GetRow(ctx, tableName, id)
	}
	// Modification stamps, and the calculated and looked-up values the
	// change reaches (see autoenter.go).
	if err := s.applyAutoEnter(ctx, tableName, fields, mode, updates, id); err != nil {
		return nil, err
	}
	if err := decodeContainerValues(fields, tableName, updates); err != nil {
		return nil, err
	}

	// Calculation fields are recomputed from the record as it will be after
	// this update, so changing a field the formula reads updates the result
	// (#30). The formula owns the value, so anything the caller sent for a
	// calculation field is replaced.
	if err := s.recalculateForUpdate(ctx, tableName, id, updates); err != nil {
		return nil, err
	}

	// The rules of the fields being changed apply to their new values (#15)
	checker, err := s.checker(ctx, tableName)
	if err != nil {
		return nil, err
	}
	changed := make(map[string]bool, len(updates))
	for col := range updates {
		changed[col] = true
	}
	if err := checker.Check(ctx, id, updates, changed, true); err != nil {
		return nil, err
	}

	dialect := s.driver.Dialect()
	writes, err := packRepetitions(ctx, s.driver.DB(), dialect, fields, tableName, id, updates)
	if err != nil {
		return nil, err
	}
	setClauses := make([]string, 0, len(writes))
	values := make([]interface{}, 0, len(writes)+1)

	idx := 1
	for col, val := range writes {
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", columnSQL(dialect, col), dialect.Placeholder(idx)))
		values = append(values, val)
		idx++
	}
	values = append(values, id)

	sqlQuery := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = %s",
		dialect.QuoteIdentifier(tableName),
		strings.Join(setClauses, ", "),
		dialect.QuoteIdentifier("id"),
		dialect.Placeholder(idx),
	)

	db := s.driver.DB()
	res, err := db.ExecContext(ctx, sqlQuery, values...)
	if err != nil {
		if f, ok := validation.IsUniqueIndexViolation(err, tableName, checker.Fields); ok {
			return nil, validation.UniqueViolation(tableName, f)
		}
		return nil, fmt.Errorf("failed updating %s: %w", tableName, err)
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("record not found to update with id: %s", id)
	}

	// A field given a value now holds exactly what it says: whatever text a
	// migration kept for it no longer describes it (#124).
	written := make([]string, 0, len(updates))
	for col := range updates {
		written = append(written, col)
	}
	if err := sourcetext.ClearFields(ctx, db, dialect, tableName, id, written); err != nil {
		return nil, err
	}
	if err := containermeta.ClearFields(ctx, db, dialect, tableName, id, written); err != nil {
		return nil, err
	}

	return s.GetRow(ctx, tableName, id)
}

// DeleteRow deletes a record, and with it the records that belong to it:
// every relationship of the table whose "Delete related records" option is on
// takes its matching records with it, down the whole chain (#36), into other
// solutions too (#107).
//
// Every record the delete reaches is found, and every refusal decided, before
// anything is deleted (see planDelete): a cascade that cannot be completed
// deletes nothing. The records of each other solution are then deleted in
// one transaction of that solution, and those of this one, the record itself
// last, in one transaction of this one.
func (s *Service) DeleteRow(ctx context.Context, tableName string, id string) error {
	plan, err := s.planDelete(ctx, tableName, id)
	if err != nil {
		return err
	}
	return plan.execute(ctx)
}

// AttachSourceText puts the original texts of the given records of a table
// under sourcetext.Key, for the records that have any (#124). The data API
// calls it on what it returns; reads inside the server, such as calculations,
// work on the typed values alone.
func (s *Service) AttachSourceText(ctx context.Context, tableName string, rows []map[string]interface{}) error {
	return sourcetext.AttachFromDB(ctx, s.driver.DB(), s.driver.Dialect(), tableName, rows)
}

// AttachContainerMeta attaches container file metadata (filename, MIME type,
// SHA-256 digest and byte length) under containermeta.Key (#125).
//
// A calculation whose result is a container reads as the container it
// refers to: its bytes in base64 and its metadata, like a container field.
func (s *Service) AttachContainerMeta(ctx context.Context, tableName string, rows []map[string]interface{}) error {
	if err := containermeta.AttachFromDB(ctx, s.driver.DB(), s.driver.Dialect(), tableName, rows); err != nil {
		return err
	}
	return s.resolveContainerResults(ctx, tableName, rows)
}

// ListRows retrieves rows with optional sorting and pagination
func (s *Service) ListRows(ctx context.Context, tableName string, opts QueryOptions) ([]map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	dialect := s.driver.Dialect()
	orderClause, err := s.orderBy(ctx, fields, tableName, opts.SortOrder())
	if err != nil {
		return nil, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > MaxPageSize {
		// Clamp, do not fall back to the default: a caller asking for more
		// than the maximum must still get a full page and keep paging.
		limit = MaxPageSize
	}

	sqlQuery := fmt.Sprintf(
		"SELECT %s FROM %s %s LIMIT %d OFFSET %d",
		selectList(dialect, fields),
		dialect.QuoteIdentifier(tableName),
		orderClause,
		limit,
		opts.Offset,
	)

	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("query failed for table %s: %w", tableName, err)
	}
	defer rows.Close()

	return s.readRows(ctx, tableName, rows, fields)
}

// ParseFile4BaseFindCriteria translates File4Base operators (*, ..., =, ==, !, !=, <, >, <=, >=, //, @) to SQL AST
func ParseFile4BaseFindCriteria(fieldName, rawCriteria string) FindCriterion {
	trimmed := strings.TrimSpace(rawCriteria)
	if trimmed == "" {
		return FindCriterion{FieldName: fieldName}
	}

	// Today's date formula: // -> current date YYYY-MM-DD
	if trimmed == "//" {
		trimmed = time.Now().UTC().Format("2006-01-02")
	} else if strings.Contains(trimmed, "//") {
		trimmed = strings.ReplaceAll(trimmed, "//", time.Now().UTC().Format("2006-01-02"))
	}

	// Range: val1...val2
	if strings.Contains(trimmed, "...") {
		parts := strings.SplitN(trimmed, "...", 2)
		return FindCriterion{
			FieldName: fieldName,
			Operator:  "RANGE",
			Value:     strings.TrimSpace(parts[0]),
			ValueTo:   strings.TrimSpace(parts[1]),
		}
	}

	// Empty field: = alone
	if trimmed == "=" {
		return FindCriterion{FieldName: fieldName, Operator: "IS_EMPTY", Value: ""}
	}

	// Strict exact match: ==val
	if strings.HasPrefix(trimmed, "==") {
		return FindCriterion{FieldName: fieldName, Operator: "==", Value: strings.TrimSpace(trimmed[2:])}
	}

	// Exact match: =val
	if strings.HasPrefix(trimmed, "=") {
		return FindCriterion{FieldName: fieldName, Operator: "=", Value: strings.TrimSpace(trimmed[1:])}
	}

	// Non-empty field: * alone
	if trimmed == "*" {
		return FindCriterion{FieldName: fieldName, Operator: "IS_NOT_EMPTY", Value: ""}
	}

	if strings.HasPrefix(trimmed, ">=") {
		return FindCriterion{FieldName: fieldName, Operator: ">=", Value: strings.TrimSpace(trimmed[2:])}
	}
	if strings.HasPrefix(trimmed, "<=") {
		return FindCriterion{FieldName: fieldName, Operator: "<=", Value: strings.TrimSpace(trimmed[2:])}
	}
	if strings.HasPrefix(trimmed, ">") {
		return FindCriterion{FieldName: fieldName, Operator: ">", Value: strings.TrimSpace(trimmed[1:])}
	}
	if strings.HasPrefix(trimmed, "<") {
		return FindCriterion{FieldName: fieldName, Operator: "<", Value: strings.TrimSpace(trimmed[1:])}
	}
	if strings.HasPrefix(trimmed, "!=") {
		return FindCriterion{FieldName: fieldName, Operator: "!=", Value: strings.TrimSpace(trimmed[2:])}
	}
	if strings.HasPrefix(trimmed, "!") {
		return FindCriterion{FieldName: fieldName, Operator: "!=", Value: strings.TrimSpace(trimmed[1:])}
	}

	// Wildcard matching: File4Base '*' -> SQL '%', '@' or '?' -> SQL '_'
	hasWildcard := strings.Contains(trimmed, "*") || strings.Contains(trimmed, "@") || strings.Contains(trimmed, "?")
	if hasWildcard {
		sqlWildcard := strings.ReplaceAll(trimmed, "*", "%")
		sqlWildcard = strings.ReplaceAll(sqlWildcard, "@", "_")
		sqlWildcard = strings.ReplaceAll(sqlWildcard, "?", "_")
		return FindCriterion{FieldName: fieldName, Operator: "LIKE", Value: sqlWildcard}
	}

	// Default: case-insensitive partial containment (%text%)
	return FindCriterion{FieldName: fieldName, Operator: "LIKE", Value: "%" + trimmed + "%"}
}

// buildFindWhere combines the find requests: the records that match any
// including request, minus the records that match any omitting request. With
// only omitting requests the starting point is every record, so a lone
// "omit city = New York" finds everyone who does not live there.
func buildFindWhere(include, omit []string) string {
	var parts []string
	if len(include) > 0 {
		parts = append(parts, "("+strings.Join(include, " OR ")+")")
	}
	if len(omit) > 0 {
		parts = append(parts, "NOT ("+strings.Join(omit, " OR ")+")")
	}
	if len(parts) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(parts, " AND ")
}

// criterionClause renders one criterion against an already-quoted column
// expression of fieldType, appending the values its placeholders take.
// colIdent is the record's own column, or a related table's column when the
// criterion reaches through a relationship (#46), so both read the same
// operators. A date or a timestamp is compared as one (see temporal_find.go).
func criterionClause(
	dialect dbal.Dialect, colIdent string, fieldType dbal.AgnosticFieldType,
	crit FindCriterion, idx *int, values *[]interface{},
) (string, error) {
	place := func() string {
		p := dialect.Placeholder(*idx)
		*idx++
		return p
	}
	if clause, ok, err := temporalCriterionClause(dialect, colIdent, fieldType, crit, place, values); err != nil || ok {
		return clause, err
	}
	return textCriterionClause(dialect, colIdent, crit, place, values), nil
}

// textCriterionClause renders a criterion that compares text, or numbers.
func textCriterionClause(
	dialect dbal.Dialect, colIdent string, crit FindCriterion, place func() string, values *[]interface{},
) string {
	switch crit.Operator {
	case "IS_EMPTY":
		return fmt.Sprintf("(%s IS NULL OR %s = '')", colIdent, dialect.CastToText(colIdent))

	case "IS_NOT_EMPTY":
		return fmt.Sprintf("(%s IS NOT NULL AND %s <> '')", colIdent, dialect.CastToText(colIdent))

	case "RANGE":
		valStr := fmt.Sprintf("%v", crit.Value)
		valToStr := fmt.Sprintf("%v", crit.ValueTo)
		num1, err1 := strconv.ParseFloat(valStr, 64)
		num2, err2 := strconv.ParseFloat(valToStr, 64)
		if err1 == nil && err2 == nil {
			// Numeric range query with safe regex check so non-numeric column rows don't crash
			clause := fmt.Sprintf("%s BETWEEN %s AND %s", dialect.NumericValue(colIdent), place(), place())
			*values = append(*values, num1, num2)
			return clause
		}
		// Text or date range query
		clause := fmt.Sprintf("%s BETWEEN %s AND %s", dialect.CastToText(colIdent), place(), place())
		*values = append(*values, valStr, valToStr)
		return clause

	case "LIKE":
		// Compared as text so it works on any column type
		clause := dialect.CaseInsensitiveLike(colIdent, place())
		*values = append(*values, fmt.Sprintf("%v", crit.Value))
		return clause

	case "=", "==":
		// Case-insensitive exact match
		clause := fmt.Sprintf("LOWER(%s) = LOWER(%s)", dialect.CastToText(colIdent), place())
		*values = append(*values, fmt.Sprintf("%v", crit.Value))
		return clause

	case "!=":
		clause := fmt.Sprintf("(%s IS NULL OR LOWER(%s) <> LOWER(%s))", colIdent, dialect.CastToText(colIdent), place())
		*values = append(*values, fmt.Sprintf("%v", crit.Value))
		return clause

	case ">", "<", ">=", "<=":
		valStr := fmt.Sprintf("%v", crit.Value)
		if num, err := strconv.ParseFloat(valStr, 64); err == nil {
			// Numeric comparison
			clause := fmt.Sprintf("%s %s %s", dialect.NumericValue(colIdent), crit.Operator, place())
			*values = append(*values, num)
			return clause
		}
		// Text comparison
		clause := fmt.Sprintf("%s %s %s", dialect.CastToText(colIdent), crit.Operator, place())
		*values = append(*values, valStr)
		return clause

	default:
		clause := dialect.CaseInsensitiveLike(colIdent, place())
		*values = append(*values, "%"+fmt.Sprintf("%v", crit.Value)+"%")
		return clause
	}
}

// findClauses is what a find adds to a statement on the table searched: the
// common table expressions it reads, to put before the statement, the WHERE
// clause that defines the found set, and the values their placeholders take,
// in the order they appear.
type findClauses struct {
	with, where string
	values      []interface{}
}

// buildFindClauses turns the find requests into the WHERE clause that defines
// the found set, and the values its placeholders take.
//
// It is shared by ExecuteFind and by Summarize, so a report totals exactly the
// records the find returned rather than a set built a second, slightly
// different way.
//
// related holds the relationships the criteria reach through, resolved before
// this is called (see Service.resolveFindRelations), so that building the
// clause stays a pure function of the catalog it was given.
func buildFindClauses(
	dialect dbal.Dialect, fields fieldSet, tableName string, requests []FindRequest,
	related map[string]relatedFindTarget,
) (findClauses, error) {
	// Requests that match are unioned; requests marked Omit subtract from what
	// they found. (OR-ing an omitting request in, as this used to do, made
	// "2011 except March" mean "2011 OR not March", which is almost every
	// record.)
	var includeClauses, omitClauses []string
	var out findClauses
	idx := 1

	// What other solutions found comes first, bound once however many
	// requests read it.
	out.with = externalKeySets(dialect, fields, requests, related, &idx, &out.values)

	// The included requests are written before the omitted ones, whatever
	// their order, and their values are taken in that order: a placeholder
	// that is not numbered (MariaDB) takes the next value.
	for _, omit := range []bool{false, true} {
		for _, req := range requests {
			if req.Omit != omit || len(req.Criteria) == 0 {
				continue
			}
			var andClauses []string
			for _, crit := range req.Criteria {
				if crit.FieldName == "" {
					continue
				}

				crit = normalizeCriterion(crit)
				if crit.isRelated() {
					clause, err := relatedCriterionClause(dialect, tableName, fields, crit, related, &idx, &out.values)
					if err != nil {
						return findClauses{}, err
					}
					andClauses = append(andClauses, clause)
					continue
				}

				if err := checkField(fields, tableName, crit.FieldName); err != nil {
					return findClauses{}, err
				}
				clause, err := criterionClause(dialect, dialect.QuoteIdentifier(crit.FieldName),
					fields[crit.FieldName], crit, &idx, &out.values)
				if err != nil {
					return findClauses{}, err
				}
				andClauses = append(andClauses, clause)
			}

			if len(andClauses) > 0 {
				joined := fmt.Sprintf("(%s)", strings.Join(andClauses, " AND "))
				if req.Omit {
					omitClauses = append(omitClauses, joined)
				} else {
					includeClauses = append(includeClauses, joined)
				}
			}
		}
	}

	out.where = buildFindWhere(includeClauses, omitClauses)
	return out, nil
}

// normalizeCriterion reads a criterion whose operator is not given, or that
// is a LIKE typed with an operator or wildcard in its value, as Find mode
// reads what is typed in a field (ParseFile4BaseFindCriteria).
func normalizeCriterion(crit FindCriterion) FindCriterion {
	if (crit.Operator != "" && crit.Operator != "LIKE") || crit.Value == nil {
		return crit
	}
	strVal := strings.TrimSpace(fmt.Sprintf("%v", crit.Value))
	// If user entered explicit operator or wildcard inside string
	if strings.HasPrefix(strVal, "=") || strings.HasPrefix(strVal, "!") ||
		strings.HasPrefix(strVal, ">") || strings.HasPrefix(strVal, "<") ||
		strings.Contains(strVal, "...") || strings.Contains(strVal, "*") ||
		strings.Contains(strVal, "@") || strings.Contains(strVal, "?") ||
		strings.Contains(strVal, "//") {
		parsed := ParseFile4BaseFindCriteria(crit.FieldName, strVal)
		parsed.RelationshipID, parsed.Occurrence, parsed.Side = crit.RelationshipID, crit.Occurrence, crit.Side
		return parsed
	}
	return crit
}

// ExecuteFind performs a File4Base Find Mode multi-request query
func (s *Service) ExecuteFind(ctx context.Context, tableName string, requests []FindRequest, opts QueryOptions) ([]map[string]interface{}, error) {
	if len(requests) == 0 {
		return s.ListRows(ctx, tableName, opts)
	}

	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	dialect := s.driver.Dialect()
	related, err := s.resolveFindRelations(ctx, tableName, requests)
	if err != nil {
		return nil, err
	}
	if err := s.refuseUnstoredCriteria(ctx, tableName, requests, related); err != nil {
		return nil, err
	}
	find, err := buildFindClauses(dialect, fields, tableName, requests, related)
	if err != nil {
		return nil, err
	}

	orderClause, err := s.orderBy(ctx, fields, tableName, opts.SortOrder())
	if err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	} else if limit > MaxPageSize {
		limit = MaxPageSize
	}

	sqlQuery := fmt.Sprintf(
		"%sSELECT %s FROM %s %s %s LIMIT %d OFFSET %d",
		find.with,
		selectList(dialect, fields),
		dialect.QuoteIdentifier(tableName),
		find.where,
		orderClause,
		limit,
		opts.Offset,
	)

	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, sqlQuery, find.values...)
	if err != nil {
		return nil, fmt.Errorf("find query failed: %w", err)
	}
	defer rows.Close()

	return s.readRows(ctx, tableName, rows, fields)
}

// readRows reads the records a query returned, their unstored calculations
// computed.
func (s *Service) readRows(ctx context.Context, tableName string, rows *sql.Rows, fields fieldSet) ([]map[string]interface{}, error) {
	records, err := rowsToMaps(rows, fields)
	if err != nil {
		return nil, err
	}
	// The connection is free again before the calculations read related
	// records with it.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := s.fillUnstored(ctx, tableName, records); err != nil {
		return nil, err
	}
	return records, nil
}

// rowsToMaps reads rows for the JSON API: CONTAINER values become base64
// text, other byte values (text the driver returns as bytes) become strings.
func rowsToMaps(rows *sql.Rows, fields fieldSet) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	companions := fields.companionColumns()
	results := make([]map[string]interface{}, 0)
	for rows.Next() {
		colValues := make([]interface{}, len(cols))
		colPointers := make([]interface{}, len(cols))
		for i := range colValues {
			colPointers[i] = &colValues[i]
		}

		if err := rows.Scan(colPointers...); err != nil {
			return nil, err
		}

		rowMap := make(map[string]interface{}, len(cols))
		for i, colName := range cols {
			val := colValues[i]
			if colName == dbal.CreationOrderColumn {
				continue // read to order by, never a field
			}
			if key, ok := companions[colName]; ok {
				if err := decodeRepetitions(fields, key, val, rowMap); err != nil {
					return nil, err
				}
				continue
			}
			if b, ok := val.([]byte); ok {
				switch fields[colName] {
				case dbal.FieldTypeContainer:
					rowMap[colName] = base64.StdEncoding.EncodeToString(b)
				case dbal.FieldTypeNumber:
					rowMap[colName] = dbal.TrimNumericText(string(b))
				default:
					rowMap[colName] = string(b)
				}
			} else if fields[colName] == dbal.FieldTypeBoolean {
				rowMap[colName] = dbal.NormalizeBool(val)
			} else {
				rowMap[colName] = val
			}
		}
		results = append(results, rowMap)
	}
	return results, nil
}

// FieldTypes returns the registered fields of a user table and the type each
// one actually stores, which for a calculation is the type its formula's
// result needs rather than the declared CALCULATION.
//
// It is exported so that a cross-solution transfer can check a mapping and
// convert a value against the destination's own field types before it writes
// anything (#96), without a second copy of the catalog query.
func (s *Service) FieldTypes(ctx context.Context, tableName string) (map[string]dbal.AgnosticFieldType, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	return map[string]dbal.AgnosticFieldType(fields), nil
}
