package dbal_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	dbal.RegisterDialect(dbal.EngineSQLite, func() dbal.Dialect { return sqlite.New() })
}

func TestSQLiteDialect_BuildCreateTableSQL(t *testing.T) {
	def := sampleTable()
	got, err := sqlite.New().BuildCreateTableSQL(def)
	require.NoError(t, err)
	assert.NotContains(t, got, "IF NOT EXISTS", "a name already taken fails")
	assert.Contains(t, got, `"id" TEXT NOT NULL PRIMARY KEY`)
	assert.Contains(t, got, `"first_name" TEXT NOT NULL`)
	assert.Contains(t, got, `"balance" NUMERIC CHECK (typeof("balance") IN ('integer', 'real', 'null'))`)

	def.CreationOrder = true
	got, err = sqlite.New().BuildCreateTableSQL(def)
	require.NoError(t, err)
	assert.Contains(t, got, `"id" TEXT NOT NULL UNIQUE`)
	assert.Contains(t, got, `"f4b$creation_order" INTEGER PRIMARY KEY AUTOINCREMENT`)
}

func TestSQLiteDialect_ColumnChanges(t *testing.T) {
	d := sqlite.New()
	add, err := d.BuildAddColumnSQL("customers", dbal.ColumnDefinition{Name: "due", Type: dbal.FieldTypeDate, IsNullable: false})
	require.NoError(t, err)
	assert.Equal(t, `ALTER TABLE "customers" ADD COLUMN "due" DATE NOT NULL `+
		`CHECK ("due" IS NULL OR (typeof("due") = 'text' AND date("due") IS "due"));`, add)
	drop, err := d.BuildDropColumnSQL("customers", "due")
	require.NoError(t, err)
	assert.Equal(t, `ALTER TABLE "customers" DROP COLUMN IF EXISTS "due";`, drop)
	alter, err := d.BuildAlterColumnTypeSQL("customers", "due", dbal.FieldTypeTimestamp)
	require.NoError(t, err)
	assert.Equal(t, `ALTER TABLE "customers" ALTER COLUMN "due" TYPE TIMESTAMP;`, alter)
	assert.Equal(t, `LOWER(CAST(x AS TEXT)) LIKE LOWER($1) ESCAPE '\'`, d.CaseInsensitiveLike("x", "$1"))
	assert.Equal(t, "$3", d.Placeholder(3))
	assert.Equal(t, `"a""b"`, d.QuoteIdentifier(`a"b`))
}

func TestDefaultEngineIsPostgreSQL(t *testing.T) {
	assert.Equal(t, dbal.EnginePostgres, dbal.DefaultEngine, "making SQLite the default is a decision of its own (#199)")
}

// The manager's databases are the files of its data directory.
func TestSQLite_ManagerIsADirectory(t *testing.T) {
	dir := t.TempDir()
	m, err := dbal.NewMultiDatabaseManager(dbal.EngineSQLite, dir)
	require.NoError(t, err)
	defer m.Close()
	ctx := context.Background()
	require.NoError(t, m.Ping(ctx))
	assert.Nil(t, m.DB())

	require.NoError(t, m.CreateDatabase(ctx, "Sales"))
	assert.ErrorIs(t, m.CreateDatabase(ctx, "sales"), dbal.ErrDatabaseExists)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad-name.sqlite"), nil, 0o600))
	// A name is lower case: a mixed-case file is no database, and a mixed-case
	// name is the lower-case database, the one a drop of that name removes.
	require.NoError(t, dbal.CreateSQLiteDatabase(ctx, filepath.Join(dir, "Other.sqlite")))
	names, err := m.ListDatabases(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"sales"}, names)

	name, found, err := m.LookupDatabase(ctx, "SALES")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "sales", name)

	d, err := m.DriverFor(ctx, "sales")
	require.NoError(t, err)
	assert.Equal(t, dbal.EngineSQLite, d.Dialect().Engine())
	mixed, err := m.GetDriver(ctx, "Sales")
	require.NoError(t, err)
	assert.Same(t, d, mixed, "Sales is the sales database")
	_, err = m.GetDriver(ctx, "Other")
	assert.Error(t, err, "Other.sqlite is not the database other")
	_, err = m.DriverFor(ctx, "nothere")
	assert.Error(t, err)
	_, statErr := os.Stat(filepath.Join(dir, "nothere"+dbal.SQLiteFileExtension))
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "a lookup never creates a database")
	_, err = m.GetDriver(ctx, "../escape")
	assert.Error(t, err)

	require.NoError(t, m.DropDatabase(ctx, "sales"))
	names, err = m.ListDatabases(ctx)
	require.NoError(t, err)
	assert.Empty(t, names)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_, err := os.Stat(filepath.Join(dir, "sales"+dbal.SQLiteFileExtension+suffix))
		assert.True(t, errors.Is(err, os.ErrNotExist), suffix)
	}

	missing, err := dbal.NewMultiDatabaseManager(dbal.EngineSQLite, filepath.Join(dir, "absent"))
	require.NoError(t, err)
	assert.Error(t, missing.Ping(ctx))
}
