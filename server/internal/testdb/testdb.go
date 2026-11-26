// Package testdb points the integration tests at a database server. The same
// tests run against every supported engine: the engine and its connection
// strings come from the environment, so CI can run the suite once per engine.
//
//	F4B_TEST_ENGINE     postgres (default), mariadb or sqlite
//	F4B_TEST_ADMIN_DSN  server-level DSN (listing/creating databases); on
//	                    SQLite, the data directory
//	F4B_TEST_DEV_DSN    DSN of the scratch database the package tests use;
//	                    on SQLite, its file
//
// The defaults match the development stack in docker-compose.yml. SQLite
// needs no service: by default each test process gets a data directory of
// its own under the system's temporary directory, with the scratch database
// in it, so packages tested in parallel never share a file.
package testdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/dbal/sqlite"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
	dbal.RegisterDialect(dbal.EngineSQLite, func() dbal.Dialect { return sqlite.New() })
}

// Engine is the engine under test.
func Engine() dbal.EngineType {
	raw := strings.TrimSpace(os.Getenv("F4B_TEST_ENGINE"))
	if raw == "" {
		return dbal.DefaultEngine
	}
	engine, err := dbal.ParseEngineType(raw)
	if err != nil {
		panic("F4B_TEST_ENGINE: " + err.Error())
	}
	return engine
}

// AdminDSN is the server-level DSN, whose database is the engine's own
// administrative database.
func AdminDSN() string {
	if dsn := os.Getenv("F4B_TEST_ADMIN_DSN"); dsn != "" {
		return dsn
	}
	switch Engine() {
	case dbal.EngineMariaDB:
		return "file4base:dev_password@tcp(127.0.0.1:3306)/mysql"
	case dbal.EngineSQLite:
		return sqliteDir()
	}
	return "postgres://file4base:dev_password@localhost:5432/postgres?sslmode=disable"
}

// DevDSN is the DSN of the scratch database used by the package-level tests.
func DevDSN() string {
	if dsn := os.Getenv("F4B_TEST_DEV_DSN"); dsn != "" {
		return dsn
	}
	switch Engine() {
	case dbal.EngineMariaDB:
		return "file4base:dev_password@tcp(127.0.0.1:3306)/file4base_dev"
	case dbal.EngineSQLite:
		return sqliteDevFile()
	}
	return "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
}

var (
	sqliteDirOnce sync.Once
	sqliteDirPath string
	sqliteDevOnce sync.Once
	sqliteDevPath string
)

// sqliteDir is this process's SQLite data directory. When it cannot be
// made, the path of one that does not exist is returned, so the tests skip
// as they do when a server is unreachable.
func sqliteDir() string {
	sqliteDirOnce.Do(func() {
		dir, err := os.MkdirTemp("", "f4b-sqlite-test-")
		if err != nil {
			sqliteDirPath = filepath.Join(os.TempDir(), "f4b-sqlite-test-unavailable")
			return
		}
		sqliteDirPath = dir
	})
	return sqliteDirPath
}

// sqliteDevFile is the scratch database of this process, created the first
// time it is asked for.
func sqliteDevFile() string {
	sqliteDevOnce.Do(func() {
		dir := AdminDSN()
		sqliteDevPath = filepath.Join(dir, "file4base_dev"+dbal.SQLiteFileExtension)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := dbal.CreateSQLiteDatabase(ctx, sqliteDevPath); err != nil && !errors.Is(err, dbal.ErrDatabaseExists) {
			panic("testdb: creating the SQLite scratch database: " + err.Error())
		}
	})
	return sqliteDevPath
}
