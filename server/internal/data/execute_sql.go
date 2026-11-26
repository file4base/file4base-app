package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/fmsql"
)

// ExecuteSQL: FileMaker's ExecuteSQL function, run where the formula is
// computed, on the server, as the account the service reads as.
//
// The query is parsed and translated by fmsql into a statement of the
// solution's database, built from the solution's names alone, with every
// value bound as a parameter. It reads the table occurrences of this
// solution, each table only if the account may read it, as a related field
// is read (checkRead): a table it may not read answers "?", never its data.
// It ignores the relationships graph, as FileMaker's does.

// The bounds of one query.
const (
	// maxSQLRows is the most rows a query may answer: one answering more
	// is refused rather than read whole into memory.
	maxSQLRows = 10000
	// maxSQLBytes is the most text the values of an answer may hold.
	maxSQLBytes = 16 << 20
	// sqlTimeout stops a query, from reading its tables' fields, that runs
	// longer.
	sqlTimeout = 5 * time.Second
)

// ExecuteSQL implements calc.SQL.
func (c *unstoredContext) ExecuteSQL(query string, args []calc.SQLArgument) ([][]string, error) {
	return c.reader.executeSQL(c.ctx, query, args)
}

func (r *relatedReader) executeSQL(ctx context.Context, query string, args []calc.SQLArgument) ([][]string, error) {
	ctx, cancel := context.WithTimeout(ctx, sqlTimeout)
	defer cancel()
	stmt, err := fmsql.Parse(query)
	if err != nil {
		return nil, err
	}
	values := make([]fmsql.Value, len(args))
	for i, a := range args {
		v, ok := fmsql.Argument(sqlArgumentKind(a.Kind), a.Text)
		if !ok {
			return nil, fmt.Errorf("argument %d (%q) is not a %s SQL can read", i+1, a.Text, a.Kind)
		}
		values[i] = v
	}
	if ctx.Err() != nil {
		return nil, sqlRunError(ctx, ctx.Err())
	}
	dialect := r.s.driver.Dialect()
	q, err := fmsql.Translate(stmt, sqlCatalog{r: r, ctx: ctx}, dialect, values, fmsql.Options{MaxRows: maxSQLRows})
	if ctx.Err() != nil {
		return nil, sqlRunError(ctx, ctx.Err())
	}
	if err != nil {
		return nil, err
	}

	statement := q.SQL
	if dialect.Engine() == dbal.EngineMariaDB {
		// The server stops the statement itself: cancelling the request
		// would only close the connection.
		statement = fmt.Sprintf("SET STATEMENT max_statement_time = %d FOR %s", int(sqlTimeout/time.Second), statement)
	}
	rows, err := r.s.driver.DB().QueryContext(ctx, statement, q.Args...)
	if err != nil {
		return nil, sqlRunError(ctx, err)
	}
	defer rows.Close()
	var out [][]string
	size := 0
	raw := make([]any, len(q.Columns))
	ptrs := make([]any, len(raw))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if len(out) == maxSQLRows {
			return nil, fmt.Errorf("the query answers more than %d rows; ask for fewer with WHERE or FETCH FIRST", maxSQLRows)
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, sqlRunError(ctx, err)
		}
		row := make([]string, len(raw))
		for i, v := range raw {
			text, err := fmsql.Format(q.Columns[i], v)
			if err != nil {
				return nil, fmt.Errorf("column %d of the answer cannot be read: %w", i+1, err)
			}
			row[i] = text
			if size += len(text) + 1; size > maxSQLBytes {
				return nil, fmt.Errorf("the answer is longer than %d MB; ask for fewer rows or columns", maxSQLBytes>>20)
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, sqlRunError(ctx, err)
	}
	return out, nil
}

// sqlRunError says why the database did not answer.
func sqlRunError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the query ran longer than %s and was stopped", sqlTimeout)
	}
	return fmt.Errorf("the query could not be run: %v", err)
}

func sqlArgumentKind(kind string) fmsql.Kind {
	switch kind {
	case "text":
		return fmsql.KindText
	case "number":
		return fmsql.KindNumber
	case "date":
		return fmsql.KindDate
	case "time":
		return fmsql.KindTime
	case "timestamp":
		return fmsql.KindTimestamp
	}
	return fmsql.KindNull
}

// sqlCatalog is the table occurrences of the solution a reader reads, as a
// query names them.
type sqlCatalog struct {
	r   *relatedReader
	ctx context.Context
}

// errSQLExternal refuses an occurrence of another solution's table.
var errSQLExternal = errors.New("it is an occurrence of a table of another solution, which ExecuteSQL does not read")

func (c sqlCatalog) Occurrence(name string) (*fmsql.Table, error) {
	graph, err := c.r.occurrenceGraph(c.ctx)
	if err != nil {
		return nil, err
	}
	o, ok := graph.byName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, nil
	}
	if o.external != nil {
		return nil, errSQLExternal
	}
	if err := c.r.checkRead(c.ctx, o.table); err != nil {
		if errors.Is(err, ErrNoAccess) {
			return nil, fmt.Errorf("this account may not read it (%v)", ErrNoAccess)
		}
		return nil, err
	}
	return c.r.sqlTable(c.ctx, o.table)
}

// sqlTable is a table's fields as a query reads them, read once per request.
func (r *relatedReader) sqlTable(ctx context.Context, tableName string) (*fmsql.Table, error) {
	if t, ok := r.sqlTables[tableName]; ok {
		return t, nil
	}
	dialect := r.s.driver.Dialect()
	q := `SELECT c.name, c.display_name, c.field_type, c.calculation_formula, COALESCE(c.is_primary_key, FALSE)
		FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = ` + dialect.Placeholder(1) + ` ORDER BY c.created_at ASC`
	rows, err := r.s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the fields of %s: %w", tableName, err)
	}
	defer rows.Close()
	table := &fmsql.Table{Name: tableName}
	for rows.Next() {
		var name, display, fieldType string
		var formula *string
		var key bool
		if err := rows.Scan(&name, &display, &fieldType, &formula, &key); err != nil {
			return nil, err
		}
		f := fmsql.Field{Name: name, DisplayName: display, Key: key,
			Type: StorageType(dbal.AgnosticFieldType(fieldType), formula)}
		switch {
		case dbal.AgnosticFieldType(fieldType) == dbal.FieldTypeCalculation && formula != nil &&
			!ReadStoredCalculation(*formula).IsStored():
			f.Unreadable = "it is an unstored calculation, computed only when a record is shown"
		case f.Type == dbal.FieldTypeSummary:
			f.Unreadable = "it is a summary field"
		case f.Type == dbal.FieldTypeContainer, f.Type == FieldTypeContainerResult:
			f.Unreadable = "it is a container field"
		}
		table.Fields = append(table.Fields, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if r.sqlTables == nil {
		r.sqlTables = map[string]*fmsql.Table{}
	}
	r.sqlTables[tableName] = table
	return table, nil
}
