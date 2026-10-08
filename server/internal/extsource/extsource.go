// Package extsource reads records out of another SQL database, so a data set
// living in PostgreSQL, MySQL/MariaDB or SQL Server can be imported into
// File4Base without being exported to a file first (#47).
//
// It is not an ODBC bridge. Go has no ODBC in its standard library, and the
// cgo bridges available pull unixODBC and third-party drivers into the API
// image; File4Base talks to the engines that matter with Go drivers instead,
// which keeps the server a single static binary. The menu says what it is:
// an external SQL data source.
//
// Everything is read as **text**, exactly as the file parsers do, so turning
// text into a number or a date stays the import's job and a value that will
// not convert is reported with the row and column it came from.
package extsource

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dataio"
)

// ErrUnsupportedEngine is returned for an engine File4Base has no driver for.
var ErrUnsupportedEngine = errors.New("unsupported data source engine")

// ErrUnsafeStatement is returned for anything that is not a single SELECT.
var ErrUnsafeStatement = errors.New("only a single SELECT statement can be read")

// ErrConnection is returned when the source cannot be reached or refuses the
// credentials. Its message is what the driver said, which is what an owner
// setting a connection up needs to see.
var ErrConnection = errors.New("the data source could not be read")

// Engine is an external database File4Base can read.
type Engine string

const (
	EnginePostgres  Engine = "postgres"
	EngineMySQL     Engine = "mysql"
	EngineSQLServer Engine = "sqlserver"
)

// Engines are the engines File4Base reads, in the order the dialog offers
// them.
var Engines = []Engine{EnginePostgres, EngineMySQL, EngineSQLServer}

// Label is the engine's name as the interface writes it.
func (e Engine) Label() string {
	switch e {
	case EnginePostgres:
		return "PostgreSQL"
	case EngineMySQL:
		return "MySQL / MariaDB"
	case EngineSQLServer:
		return "Microsoft SQL Server"
	default:
		return string(e)
	}
}

// DefaultPort is the port an engine listens on when none is given.
func (e Engine) DefaultPort() int {
	switch e {
	case EnginePostgres:
		return 5432
	case EngineMySQL:
		return 3306
	case EngineSQLServer:
		return 1433
	default:
		return 0
	}
}

func (e Engine) known() bool {
	for _, known := range Engines {
		if known == e {
			return true
		}
	}
	return false
}

// Connection is how to reach an external database. The password is never part
// of it: it is supplied with the request that reads, or taken from an
// environment variable of the server, so that no secret is written to the
// catalog, to a solution file or to a log.
type Connection struct {
	Engine   Engine
	Host     string
	Port     int
	Database string
	Username string

	// Schema limits which tables are listed (PostgreSQL and SQL Server).
	Schema string

	// TLS asks the driver for an encrypted connection where it is optional.
	TLS bool
}

// ReadOptions bound a read, the way dataio.Limits bound a file.
type ReadOptions struct {
	// Table reads every column of one table; Query reads a SELECT written by
	// the caller. Exactly one of them is used, Table first.
	Table string
	Query string

	Limits  dataio.Limits
	Timeout time.Duration
}

func (o ReadOptions) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 30 * time.Second
	}
	if o.Timeout > 5*time.Minute {
		return 5 * time.Minute
	}
	return o.Timeout
}

// checkConnection rejects a connection that cannot be used before a driver
// is asked to parse it.
func checkConnection(conn Connection) error {
	if !conn.Engine.known() {
		return fmt.Errorf("%w: %s", ErrUnsupportedEngine, conn.Engine)
	}
	if strings.TrimSpace(conn.Host) == "" {
		return fmt.Errorf("%w: the host is missing", ErrConnection)
	}
	if strings.TrimSpace(conn.Database) == "" && conn.Engine != EngineMySQL {
		return fmt.Errorf("%w: the database is missing", ErrConnection)
	}
	return nil
}

// ListTables answers the tables the connection can read, so the import dialog
// can offer them instead of asking for a name.
func ListTables(ctx context.Context, conn Connection, password string) ([]string, error) {
	db, err := open(conn, password)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	query, args := tableListQuery(conn)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnection, err)
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConnection, err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnection, err)
	}
	return tables, nil
}

// Read runs the read and returns what the import pipeline already takes: a
// table of text, with its column names.
func Read(ctx context.Context, conn Connection, password string, opts ReadOptions) (*dataio.Table, error) {
	limits := opts.Limits
	if limits.MaxRows <= 0 || limits.MaxColumns <= 0 {
		limits = dataio.DefaultLimits()
	}

	statement, err := readStatement(conn, opts, limits)
	if err != nil {
		return nil, err
	}

	db, err := open(conn, password)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()

	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnection, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnection, err)
	}
	if len(columns) > limits.MaxColumns {
		return nil, fmt.Errorf("%w: %d columns, and the limit is %d",
			dataio.ErrSourceTooLarge, len(columns), limits.MaxColumns)
	}

	table := &dataio.Table{Columns: columns, Rows: [][]string{}}
	cells := make([]interface{}, len(columns))
	holders := make([]sql.RawBytes, len(columns))
	for i := range cells {
		cells[i] = &holders[i]
	}

	for rows.Next() {
		if len(table.Rows) >= limits.MaxRows {
			table.Truncated = true
			break
		}
		if err := rows.Scan(cells...); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConnection, err)
		}
		row := make([]string, len(columns))
		for i := range holders {
			row[i] = string(holders[i])
		}
		table.Rows = append(table.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnection, err)
	}
	return table, nil
}

// readStatement is the SELECT that will run: a whole table, or the caller's
// own statement once it has been checked.
func readStatement(conn Connection, opts ReadOptions, limits dataio.Limits) (string, error) {
	// One row over the limit, so a source that is exactly at it is not
	// reported as truncated.
	rowCap := limits.MaxRows + 1

	if table := strings.TrimSpace(opts.Table); table != "" {
		name, err := quoteQualified(conn.Engine, table)
		if err != nil {
			return "", err
		}
		return limitedSelect(conn.Engine, "SELECT * FROM "+name, rowCap), nil
	}

	query, err := checkQuery(opts.Query)
	if err != nil {
		return "", err
	}
	return limitedSelect(conn.Engine, query, rowCap), nil
}

// checkQuery accepts a single SELECT and nothing else. The connection is used
// read-only by construction — File4Base never writes to a source — and this
// keeps a typed statement from being the exception.
func checkQuery(query string) (string, error) {
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimSuffix(trimmed, ";")
	if trimmed == "" {
		return "", fmt.Errorf("%w: nothing to read", ErrUnsafeStatement)
	}
	if strings.Contains(trimmed, ";") {
		return "", fmt.Errorf("%w: it holds more than one statement", ErrUnsafeStatement)
	}
	lowered := strings.ToLower(trimmed)
	if !strings.HasPrefix(lowered, "select ") && !strings.HasPrefix(lowered, "with ") {
		return "", fmt.Errorf("%w: it begins with %q", ErrUnsafeStatement, firstWord(trimmed))
	}
	return trimmed, nil
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t\n\r("); i > 0 {
		return s[:i]
	}
	return s
}

// limitedSelect caps the rows in the engine's own spelling, so a large source
// is cut by the database rather than streamed in full and thrown away.
func limitedSelect(engine Engine, statement string, rowCap int) string {
	switch engine {
	case EngineSQLServer:
		// TOP must sit inside the statement, which is not something to do to
		// a statement someone else wrote; OFFSET/FETCH needs an ORDER BY.
		// The row cap in Read still applies.
		return statement
	default:
		return fmt.Sprintf("SELECT * FROM (%s) AS f4b_source LIMIT %d", statement, rowCap)
	}
}

// quoteQualified turns "schema.table" into the engine's quoted form, and
// refuses anything that is not a plain name, so a table picked from the list
// cannot carry SQL with it.
func quoteQualified(engine Engine, name string) (string, error) {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("%w: %q is not a table name", ErrUnsafeStatement, name)
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !plainName(part) {
			return "", fmt.Errorf("%w: %q is not a table name", ErrUnsafeStatement, name)
		}
		switch engine {
		case EngineMySQL:
			quoted = append(quoted, "`"+part+"`")
		case EngineSQLServer:
			quoted = append(quoted, "["+part+"]")
		default:
			quoted = append(quoted, `"`+part+`"`)
		}
	}
	return strings.Join(quoted, "."), nil
}

// plainName accepts the identifiers a table picker can produce: letters,
// digits, underscores and dollars, not starting with a digit.
func plainName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '$':
		default:
			return false
		}
	}
	return true
}

// tableListQuery reads the catalog of the source engine.
func tableListQuery(conn Connection) (string, []interface{}) {
	schema := strings.TrimSpace(conn.Schema)
	switch conn.Engine {
	case EngineMySQL:
		if schema == "" {
			schema = conn.Database
		}
		return `SELECT table_name FROM information_schema.tables
		        WHERE table_schema = ? AND table_type = 'BASE TABLE' ORDER BY table_name`, []interface{}{schema}
	case EngineSQLServer:
		if schema == "" {
			schema = "dbo"
		}
		return `SELECT table_name FROM information_schema.tables
		        WHERE table_schema = @p1 AND table_type = 'BASE TABLE' ORDER BY table_name`, []interface{}{schema}
	default:
		if schema == "" {
			schema = "public"
		}
		return `SELECT table_name FROM information_schema.tables
		        WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY table_name`, []interface{}{schema}
	}
}
