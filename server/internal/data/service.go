package data

import (
	"context"
	"encoding/json"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
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
}

// FindRequest represents one disjunctive search request (OR unit)
type FindRequest struct {
	Criteria []FindCriterion `json:"criteria"`
	Omit     bool            `json:"omit"` // NOT condition
}

// QueryOptions controls sorting and pagination
type QueryOptions struct {
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	SortBy  string   `json:"sort_by,omitempty"`
	SortAsc bool     `json:"sort_asc"`
}

// Service handles dynamic generic table CRUD and query translation
type Service struct {
	driver dbal.DatabaseDriver
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

// tableFields returns the registered field names of a user table.
// Every user table owns at least its primary key column, so an empty result
// means the table is not part of the catalog.
func (s *Service) tableFields(ctx context.Context, tableName string) (map[string]struct{}, error) {
	// Internal tables are never user data, even if a catalog row names one.
	if dbal.IsReservedTableName(tableName) {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, tableName)
	}
	q := `SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = $1`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = ?`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed resolving table %s: %w", tableName, err)
	}
	defer rows.Close()

	fields := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		fields[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, tableName)
	}
	return fields, nil
}

// autoEnterConstants returns the constant auto-enter value of each field of
// the table whose options (sys_columns.default_value) enable one.
func (s *Service) autoEnterConstants(ctx context.Context, tableName string) (map[string]interface{}, error) {
	q := `SELECT c.name, c.default_value FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = $1 AND c.default_value IS NOT NULL`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT c.name, c.default_value FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = ? AND c.default_value IS NOT NULL`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading field options of %s: %w", tableName, err)
	}
	defer rows.Close()

	constants := make(map[string]interface{})
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		var opts struct {
			DataEnabled bool    `json:"data_enabled"`
			DataValue   *string `json:"data_value"`
		}
		// Options that are not a JSON object (legacy values) enable nothing.
		if json.Unmarshal([]byte(raw), &opts) == nil && opts.DataEnabled && opts.DataValue != nil && *opts.DataValue != "" {
			constants[name] = *opts.DataValue
		}
	}
	return constants, rows.Err()
}

func checkField(fields map[string]struct{}, tableName, fieldName string) error {
	if _, ok := fields[fieldName]; !ok {
		return fmt.Errorf("%w: %s.%s", ErrUnknownField, tableName, fieldName)
	}
	return nil
}

// InsertRow dynamically inserts a record into any table
func (s *Service) InsertRow(ctx context.Context, tableName string, record map[string]interface{}) (map[string]interface{}, error) {
	if record == nil {
		record = make(map[string]interface{})
	}

	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	for col := range record {
		if err := checkField(fields, tableName, col); err != nil {
			return nil, err
		}
	}

	// Auto-enter constant values (Fields dialog: "Data") for fields the
	// caller did not supply. An explicit value, even empty, wins.
	constants, err := s.autoEnterConstants(ctx, tableName)
	if err != nil {
		return nil, err
	}
	for col, val := range constants {
		if _, given := record[col]; !given {
			record[col] = val
		}
	}

	// Ensure id exists
	if _, ok := record["id"]; !ok || record["id"] == nil || record["id"] == "" {
		record["id"] = uuid.NewString()
	}

	dialect := s.driver.Dialect()
	columns := make([]string, 0, len(record))
	placeholders := make([]string, 0, len(record))
	values := make([]interface{}, 0, len(record))

	idx := 1
	for col, val := range record {
		columns = append(columns, dialect.QuoteIdentifier(col))
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
		return nil, fmt.Errorf("failed inserting into %s: %w", tableName, err)
	}

	return record, nil
}

// GetRow retrieves a single record by primary key id
func (s *Service) GetRow(ctx context.Context, tableName string, id string) (map[string]interface{}, error) {
	if _, err := s.tableFields(ctx, tableName); err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	sqlQuery := fmt.Sprintf(
		"SELECT * FROM %s WHERE %s = %s LIMIT 1",
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

	results, err := rowsToMaps(rows)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("record not found with id: %s", id)
	}
	return results[0], nil
}

// UpdateRow dynamically updates a row by id
func (s *Service) UpdateRow(ctx context.Context, tableName string, id string, updates map[string]interface{}) (map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	delete(updates, "id") // Protect primary key from alteration
	for col := range updates {
		if err := checkField(fields, tableName, col); err != nil {
			return nil, err
		}
	}
	if len(updates) == 0 {
		return s.GetRow(ctx, tableName, id)
	}

	dialect := s.driver.Dialect()
	setClauses := make([]string, 0, len(updates))
	values := make([]interface{}, 0, len(updates)+1)

	idx := 1
	for col, val := range updates {
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", dialect.QuoteIdentifier(col), dialect.Placeholder(idx)))
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
		return nil, fmt.Errorf("failed updating %s: %w", tableName, err)
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("record not found to update with id: %s", id)
	}

	return s.GetRow(ctx, tableName, id)
}

// DeleteRow removes a record by primary key id
func (s *Service) DeleteRow(ctx context.Context, tableName string, id string) error {
	if _, err := s.tableFields(ctx, tableName); err != nil {
		return err
	}
	dialect := s.driver.Dialect()
	sqlQuery := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = %s",
		dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"),
		dialect.Placeholder(1),
	)

	db := s.driver.DB()
	res, err := db.ExecContext(ctx, sqlQuery, id)
	if err != nil {
		return fmt.Errorf("failed deleting from %s: %w", tableName, err)
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("record not found with id: %s", id)
	}
	return nil
}

// ListRows retrieves rows with optional sorting and pagination
func (s *Service) ListRows(ctx context.Context, tableName string, opts QueryOptions) ([]map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if opts.SortBy != "" {
		if err := checkField(fields, tableName, opts.SortBy); err != nil {
			return nil, err
		}
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	dialect := s.driver.Dialect()
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > MaxPageSize {
		// Clamp, do not fall back to the default: a caller asking for more
		// than the maximum must still get a full page and keep paging.
		limit = MaxPageSize
	}

	orderClause := ""
	if opts.SortBy != "" {
		dir := "ASC"
		if !opts.SortAsc {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf("ORDER BY %s %s", dialect.QuoteIdentifier(opts.SortBy), dir)
	}

	sqlQuery := fmt.Sprintf(
		"SELECT * FROM %s %s LIMIT %d OFFSET %d",
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

	return rowsToMaps(rows)
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
	var orClauses []string
	var values []interface{}
	idx := 1

	for _, req := range requests {
		if len(req.Criteria) == 0 {
			continue
		}
		var andClauses []string
		for _, crit := range req.Criteria {
			if crit.FieldName == "" {
				continue
			}

			// If operator is empty or not parsed, parse it with ParseFile4BaseFindCriteria
			if (crit.Operator == "" || crit.Operator == "LIKE") && crit.Value != nil {
				strVal := strings.TrimSpace(fmt.Sprintf("%v", crit.Value))
				// If user entered explicit operator inside string
				if strings.HasPrefix(strVal, "=") || strings.HasPrefix(strVal, "!") ||
					strings.HasPrefix(strVal, ">") || strings.HasPrefix(strVal, "<") ||
					strings.Contains(strVal, "...") || strVal == "*" || strVal == "//" {
					crit = ParseFile4BaseFindCriteria(crit.FieldName, strVal)
				}
			}

			if err := checkField(fields, tableName, crit.FieldName); err != nil {
				return nil, err
			}
			colIdent := dialect.QuoteIdentifier(crit.FieldName)

			switch crit.Operator {
			case "IS_EMPTY":
				andClauses = append(andClauses, fmt.Sprintf("(%s IS NULL OR CAST(%s AS TEXT) = '')", colIdent, colIdent))

			case "IS_NOT_EMPTY":
				andClauses = append(andClauses, fmt.Sprintf("(%s IS NOT NULL AND CAST(%s AS TEXT) <> '')", colIdent, colIdent))

			case "RANGE":
				valStr := fmt.Sprintf("%v", crit.Value)
				valToStr := fmt.Sprintf("%v", crit.ValueTo)
				num1, err1 := strconv.ParseFloat(valStr, 64)
				num2, err2 := strconv.ParseFloat(valToStr, 64)
				if err1 == nil && err2 == nil {
					// Numeric range query with safe regex check so non-numeric column rows don't crash
					andClauses = append(andClauses, fmt.Sprintf(
						"(CASE WHEN CAST(%s AS TEXT) ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN CAST(CAST(%s AS TEXT) AS NUMERIC) ELSE NULL END) BETWEEN %s AND %s",
						colIdent, colIdent, dialect.Placeholder(idx), dialect.Placeholder(idx+1),
					))
					values = append(values, num1, num2)
				} else {
					// Text or date range query
					andClauses = append(andClauses, fmt.Sprintf("CAST(%s AS TEXT) BETWEEN %s AND %s", colIdent, dialect.Placeholder(idx), dialect.Placeholder(idx+1)))
					values = append(values, valStr, valToStr)
				}
				idx += 2

			case "LIKE":
				valStr := fmt.Sprintf("%v", crit.Value)
				// Use CAST to TEXT so it works on any column type (numeric, date, text, uuid, etc.)
				andClauses = append(andClauses, fmt.Sprintf("CAST(%s AS TEXT) ILIKE %s", colIdent, dialect.Placeholder(idx)))
				values = append(values, valStr)
				idx++

			case "=", "==":
				valStr := fmt.Sprintf("%v", crit.Value)
				// Case-insensitive exact match
				andClauses = append(andClauses, fmt.Sprintf("LOWER(CAST(%s AS TEXT)) = LOWER(%s)", colIdent, dialect.Placeholder(idx)))
				values = append(values, valStr)
				idx++

			case "!=":
				valStr := fmt.Sprintf("%v", crit.Value)
				andClauses = append(andClauses, fmt.Sprintf("(%s IS NULL OR LOWER(CAST(%s AS TEXT)) <> LOWER(%s))", colIdent, colIdent, dialect.Placeholder(idx)))
				values = append(values, valStr)
				idx++

			case ">", "<", ">=", "<=":
				valStr := fmt.Sprintf("%v", crit.Value)
				if num, err := strconv.ParseFloat(valStr, 64); err == nil {
					// Numeric comparison
					andClauses = append(andClauses, fmt.Sprintf(
						"(CASE WHEN CAST(%s AS TEXT) ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN CAST(CAST(%s AS TEXT) AS NUMERIC) ELSE NULL END) %s %s",
						colIdent, colIdent, crit.Operator, dialect.Placeholder(idx),
					))
					values = append(values, num)
				} else {
					// Text comparison
					andClauses = append(andClauses, fmt.Sprintf("CAST(%s AS TEXT) %s %s", colIdent, crit.Operator, dialect.Placeholder(idx)))
					values = append(values, valStr)
				}
				idx++

			default:
				valStr := fmt.Sprintf("%v", crit.Value)
				andClauses = append(andClauses, fmt.Sprintf("CAST(%s AS TEXT) ILIKE %s", colIdent, dialect.Placeholder(idx)))
				values = append(values, "%"+valStr+"%")
				idx++
			}
		}

		if len(andClauses) > 0 {
			joined := strings.Join(andClauses, " AND ")
			if req.Omit {
				orClauses = append(orClauses, fmt.Sprintf("NOT (%s)", joined))
			} else {
				orClauses = append(orClauses, fmt.Sprintf("(%s)", joined))
			}
		}
	}

	whereClause := ""
	if len(orClauses) > 0 {
		whereClause = "WHERE " + strings.Join(orClauses, " OR ")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	} else if limit > MaxPageSize {
		limit = MaxPageSize
	}

	sqlQuery := fmt.Sprintf(
		"SELECT * FROM %s %s LIMIT %d OFFSET %d",
		dialect.QuoteIdentifier(tableName),
		whereClause,
		limit,
		opts.Offset,
	)

	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, sqlQuery, values...)
	if err != nil {
		return nil, fmt.Errorf("find query failed: %w", err)
	}
	defer rows.Close()

	return rowsToMaps(rows)
}

func rowsToMaps(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

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
			if b, ok := val.([]byte); ok {
				rowMap[colName] = string(b)
			} else {
				rowMap[colName] = val
			}
		}
		results = append(results, rowMap)
	}
	return results, nil
}
