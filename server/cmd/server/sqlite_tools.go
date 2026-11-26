package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// runSQLiteTool runs the SQLite maintenance commands of the server binary,
// for a stack that runs DB_ENGINE=sqlite (docs/specs/sqlite_engine.md,
// "Backups"):
//
//	server sqlite-backup  <database> <backup file>
//	server sqlite-restore <backup file> <database>
//
// A backup is a VACUUM INTO copy, consistent while the server keeps
// writing, and never replaces an existing file. A restore makes a new
// database of the copy; it never replaces a database either. It reports
// whether args named one of the commands.
func runSQLiteTool(args []string) bool {
	if len(args) < 1 || (args[0] != "sqlite-backup" && args[0] != "sqlite-restore") {
		return false
	}
	if err := sqliteTool(context.Background(), args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return true
}

func sqliteTool(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: %s sqlite-backup <database> <backup file> | sqlite-restore <backup file> <database>", filepath.Base(os.Args[0]))
	}
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		raw = "./data/solutions"
	}
	dir, err := dbal.SQLiteDataDir(raw)
	if err != nil {
		return err
	}
	databaseFile := func(name string) (string, error) {
		// A database name is lower case, as the server creates, lists and
		// drops it.
		name = strings.ToLower(name)
		if !dbal.IsValidDatabaseName(name) || dbal.IsProtectedDatabase(name) {
			return "", fmt.Errorf("invalid database name %q", name)
		}
		return filepath.Join(dir, name+dbal.SQLiteFileExtension), nil
	}
	if args[0] == "sqlite-backup" {
		path, err := databaseFile(args[1])
		if err != nil {
			return err
		}
		drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: path})
		if err != nil {
			return err
		}
		defer drv.Close()
		if err := dbal.BackupSQLite(ctx, drv.DB(), args[2]); err != nil {
			return err
		}
		fmt.Println("backed up", args[1], "to", args[2])
		return nil
	}
	path, err := databaseFile(args[2])
	if err != nil {
		return err
	}
	if err := dbal.RestoreSQLiteDatabase(ctx, args[1], path); err != nil {
		return err
	}
	fmt.Println("restored", args[1], "as", args[2])
	return nil
}
