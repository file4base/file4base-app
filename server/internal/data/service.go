package data

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// FindCriterion represents a single field search condition
type FindCriterion struct {
	FieldName string      `json:"field_name"`
	Operator  string      `json:"operator"` // "=", "!=", ">", "<", ">=", "<=", "LIKE", "RANGE"
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

// InsertRow dynamically inserts a record into any table
func (s *Service) InsertRow(ctx context.Context, tableName string, record map[string]interface{}) (map[string]interface{}, error) {
	if record == nil {
		record = make(map[string]interface{})
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
	delete(updates, "id") // Protect primary key from alteration
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
	dialect := s.driver.Dialect()
	limit := opts.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
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

// ParseFile4BaseFindCriteria translates File4Base operators (*, ..., =, !, <, >) to SQL AST
func ParseFile4BaseFindCriteria(fieldName, rawCriteria string) FindCriterion {
	trimmed := strings.TrimSpace(rawCriteria)

	if strings.Contains(trimmed, "...") {
		parts := strings.SplitN(trimmed, "...", 2)
		return FindCriterion{
			FieldName: fieldName,
			Operator:  "RANGE",
			Value:     strings.TrimSpace(parts[0]),
			ValueTo:   strings.TrimSpace(parts[1]),
		}
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
	if strings.HasPrefix(trimmed, "!") {
		return FindCriterion{FieldName: fieldName, Operator: "!=", Value: strings.TrimSpace(trimmed[1:])}
	}
	if strings.HasPrefix(trimmed, "=") {
		return FindCriterion{FieldName: fieldName, Operator: "=", Value: strings.TrimSpace(trimmed[1:])}
	}

	// Wildcard matching: File4Base '*' -> SQL '%'
	if strings.Contains(trimmed, "*") {
		sqlWildcard := strings.ReplaceAll(trimmed, "*", "%")
		return FindCriterion{FieldName: fieldName, Operator: "LIKE", Value: sqlWildcard}
	}

	// Default: case-insensitive partial containment
	return FindCriterion{FieldName: fieldName, Operator: "LIKE", Value: "%" + trimmed + "%"}
}

// ExecuteFind performs a File4Base Find Mode multi-request query
func (s *Service) ExecuteFind(ctx context.Context, tableName string, requests []FindRequest, opts QueryOptions) ([]map[string]interface{}, error) {
	if len(requests) == 0 {
		return s.ListRows(ctx, tableName, opts)
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
			colIdent := dialect.QuoteIdentifier(crit.FieldName)
			switch crit.Operator {
			case "RANGE":
				andClauses = append(andClauses, fmt.Sprintf("%s BETWEEN %s AND %s", colIdent, dialect.Placeholder(idx), dialect.Placeholder(idx+1)))
				values = append(values, crit.Value, crit.ValueTo)
				idx += 2
			case "LIKE":
				andClauses = append(andClauses, fmt.Sprintf("%s ILIKE %s", colIdent, dialect.Placeholder(idx)))
				values = append(values, crit.Value)
				idx++
			default:
				op := crit.Operator
				if op == "" {
					op = "="
				}
				andClauses = append(andClauses, fmt.Sprintf("%s %s %s", colIdent, op, dialect.Placeholder(idx)))
				values = append(values, crit.Value)
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
	if limit <= 0 || limit > 1000 {
		limit = 100
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
