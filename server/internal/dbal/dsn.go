package dbal

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// dsnTemplate is a parsed base DSN that can be re-pointed at another database
// on the same server. Each engine has its own connection-string grammar:
// PostgreSQL uses a URL, MariaDB uses the MySQL driver's own format
// (`user:password@tcp(host:port)/database?params`), so neither can be parsed
// with the other's parser.
type dsnTemplate interface {
	// database is the database named in the base DSN ("" when it names none).
	database() string
	// forDatabase returns the DSN of another database on the same server.
	forDatabase(name string) string
}

// parseDSN reads a base DSN for an engine.
func parseDSN(engine EngineType, dsn string) (dsnTemplate, error) {
	switch engine {
	case EngineMariaDB:
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid MariaDB DSN: %w; expected user:password@tcp(host:3306)/database", err)
		}
		return &mysqlDSN{cfg: cfg}, nil
	default:
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid base DSN: %w", err)
		}
		if u.Scheme == "" {
			return nil, fmt.Errorf("invalid base DSN: missing scheme (expected postgres://user:password@host:5432/database)")
		}
		return &urlDSN{u: u}, nil
	}
}

var utcLocation = time.UTC

type urlDSN struct{ u *url.URL }

func (d *urlDSN) database() string { return strings.TrimPrefix(d.u.Path, "/") }

func (d *urlDSN) forDatabase(name string) string {
	c := *d.u
	c.Path = "/" + name
	return c.String()
}

type mysqlDSN struct{ cfg *mysql.Config }

func (d *mysqlDSN) database() string { return d.cfg.DBName }

func (d *mysqlDSN) forDatabase(name string) string {
	c := d.cfg.Clone()
	c.DBName = name
	applyMySQLDefaults(c)
	return c.FormatDSN()
}

// applyMySQLDefaults sets the connection options File4Base relies on, so a
// MariaDB connection behaves like the PostgreSQL one.
func applyMySQLDefaults(c *mysql.Config) {
	// Times come back as time.Time, in UTC, so stored timestamps do not shift.
	c.ParseTime = true
	if c.Loc == nil || c.Loc.String() == "Local" {
		c.Loc = utcLocation
	}
	// Report matched rows, not changed rows, so an UPDATE that stores the same
	// value still counts as having found its record (as PostgreSQL does).
	c.ClientFoundRows = true
}

// NormalizeDSN applies the options File4Base needs to a connection string.
// Connections opened directly (not through MultiDatabaseManager) go through
// it too, so every connection to an engine behaves the same.
func NormalizeDSN(engine EngineType, dsn string) (string, error) {
	if engine != EngineMariaDB {
		return dsn, nil
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("invalid MariaDB DSN: %w; expected user:password@tcp(host:3306)/database", err)
	}
	applyMySQLDefaults(cfg)
	return cfg.FormatDSN(), nil
}
