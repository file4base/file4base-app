package extsource

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"  // PostgreSQL, the engine File4Base itself speaks
	_ "github.com/microsoft/go-mssqldb" // Microsoft SQL Server, pure Go
)

// open dials the external database. The handle is short-lived: it is opened
// for one read and closed again, so a stored connection holds no socket and
// no credential between imports.
func open(conn Connection, password string) (*sql.DB, error) {
	if err := checkConnection(conn); err != nil {
		return nil, err
	}

	driver, dsn, err := dataSourceName(conn, password)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnection, err)
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

// dataSourceName builds the driver's connection string. It is never logged
// and never stored: the password is given per read.
func dataSourceName(conn Connection, password string) (driver, dsn string, err error) {
	port := conn.Port
	if port <= 0 {
		port = conn.Engine.DefaultPort()
	}
	address := net.JoinHostPort(conn.Host, strconv.Itoa(port))

	switch conn.Engine {
	case EnginePostgres:
		sslMode := "disable"
		if conn.TLS {
			sslMode = "require"
		}
		u := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(conn.Username, password),
			Host:   address,
			Path:   "/" + conn.Database,
		}
		q := u.Query()
		q.Set("sslmode", sslMode)
		u.RawQuery = q.Encode()
		return "pgx", u.String(), nil

	case EngineMySQL:
		cfg := mysql.NewConfig()
		cfg.Net = "tcp"
		cfg.Addr = address
		cfg.User = conn.Username
		cfg.Passwd = password
		cfg.DBName = conn.Database
		cfg.ParseTime = false // every value is read as text
		if conn.TLS {
			cfg.TLSConfig = "preferred"
		}
		return "mysql", cfg.FormatDSN(), nil

	case EngineSQLServer:
		u := url.URL{
			Scheme: "sqlserver",
			User:   url.UserPassword(conn.Username, password),
			Host:   address,
		}
		q := u.Query()
		if conn.Database != "" {
			q.Set("database", conn.Database)
		}
		q.Set("encrypt", map[bool]string{true: "true", false: "disable"}[conn.TLS])
		u.RawQuery = q.Encode()
		return "sqlserver", u.String(), nil

	default:
		return "", "", fmt.Errorf("%w: %s", ErrUnsupportedEngine, conn.Engine)
	}
}
