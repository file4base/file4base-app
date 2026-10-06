// Package testdb points the integration tests at a database server. The same
// tests run against every supported engine: the engine and its connection
// strings come from the environment, so CI can run the suite twice.
//
//	F4B_TEST_ENGINE     postgres (default) or mariadb
//	F4B_TEST_ADMIN_DSN  server-level DSN (listing/creating databases)
//	F4B_TEST_DEV_DSN    DSN of the scratch database the package tests use
//
// The defaults match the development stack in docker-compose.yml.
package testdb

import (
	"os"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
}

// Engine is the engine under test.
func Engine() dbal.EngineType {
	if strings.EqualFold(os.Getenv("F4B_TEST_ENGINE"), "mariadb") {
		return dbal.EngineMariaDB
	}
	return dbal.EnginePostgres
}

// AdminDSN is the server-level DSN, whose database is the engine's own
// administrative database.
func AdminDSN() string {
	if dsn := os.Getenv("F4B_TEST_ADMIN_DSN"); dsn != "" {
		return dsn
	}
	if Engine() == dbal.EngineMariaDB {
		return "file4base:dev_password@tcp(127.0.0.1:3306)/mysql"
	}
	return "postgres://file4base:dev_password@localhost:5432/postgres?sslmode=disable"
}

// DevDSN is the DSN of the scratch database used by the package-level tests.
func DevDSN() string {
	if dsn := os.Getenv("F4B_TEST_DEV_DSN"); dsn != "" {
		return dsn
	}
	if Engine() == dbal.EngineMariaDB {
		return "file4base:dev_password@tcp(127.0.0.1:3306)/file4base_dev"
	}
	return "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
}
