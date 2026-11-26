package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// A backup taken with the server binary restores as a new database, and
// neither command replaces a file or reaches outside the data directory.
func TestSQLiteTool_BackupThenRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("DATABASE_URL", dir)
	require.NoError(t, dbal.CreateSQLiteDatabase(ctx, filepath.Join(dir, "sales.sqlite")))
	drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: filepath.Join(dir, "sales.sqlite")})
	require.NoError(t, err)
	_, err = drv.DB().ExecContext(ctx, `CREATE TABLE t (n INTEGER); INSERT INTO t VALUES (1), (2), (3)`)
	require.NoError(t, err)
	drv.Close()

	backup := filepath.Join(t.TempDir(), "sales.backup")
	require.NoError(t, sqliteTool(ctx, []string{"sqlite-backup", "sales", backup}))
	assert.Error(t, sqliteTool(ctx, []string{"sqlite-backup", "sales", backup}), "an existing backup is not replaced")
	require.NoError(t, sqliteTool(ctx, []string{"sqlite-restore", backup, "Sales_Copy"}), "a name is the lower-case database")
	assert.ErrorIs(t, sqliteTool(ctx, []string{"sqlite-restore", backup, "SALES"}), dbal.ErrDatabaseExists)
	assert.Error(t, sqliteTool(ctx, []string{"sqlite-backup", "../sales", backup + "2"}))
	assert.Error(t, sqliteTool(ctx, []string{"sqlite-restore", backup, "../escape"}))

	copyDrv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: filepath.Join(dir, "sales_copy.sqlite")})
	require.NoError(t, err)
	defer copyDrv.Close()
	var n int
	require.NoError(t, copyDrv.DB().QueryRowContext(ctx, `SELECT SUM(n) FROM t`).Scan(&n))
	assert.Equal(t, 6, n)
	var mode string
	require.NoError(t, copyDrv.DB().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode))
	assert.Equal(t, "wal", mode)
}
