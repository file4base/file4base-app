package dbal_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
}

func sampleTable() dbal.TableDefinition {
	return dbal.TableDefinition{
		Name: "customers",
		Columns: []dbal.ColumnDefinition{
			{Name: "id", Type: dbal.FieldTypeText, IsPrimaryKey: true},
			{Name: "first_name", Type: dbal.FieldTypeText, IsNullable: false},
			{Name: "balance", Type: dbal.FieldTypeNumber, IsNullable: true},
			{Name: "status", Type: dbal.FieldTypeText, IsNullable: false},
		},
	}
}

func TestPostgresDialect_BuildCreateTableSQL(t *testing.T) {
	dialect := postgres.New()
	sql, err := dialect.BuildCreateTableSQL(sampleTable())
	require.NoError(t, err)
	assert.Contains(t, sql, "CREATE TABLE \"customers\" (")
	assert.NotContains(t, sql, "IF NOT EXISTS")
	assert.Contains(t, sql, "\"id\" TEXT PRIMARY KEY")
	assert.Contains(t, sql, "\"first_name\" TEXT NOT NULL")
	assert.Contains(t, sql, "\"balance\" NUMERIC")
	assert.Contains(t, sql, "\"status\" TEXT NOT NULL")
	assert.NotContains(t, sql, "DEFAULT", "DDL never carries caller-supplied defaults (#1)")
}

func TestMariaDBDialect_BuildCreateTableSQL(t *testing.T) {
	dialect := mariadb.New()
	sql, err := dialect.BuildCreateTableSQL(sampleTable())
	require.NoError(t, err)
	assert.Contains(t, sql, "CREATE TABLE `customers` (")
	assert.NotContains(t, sql, "IF NOT EXISTS")
	// A key column cannot be LONGTEXT in MariaDB without a key length
	assert.Contains(t, sql, "`id` VARCHAR(252) PRIMARY KEY")
	assert.Contains(t, sql, "`first_name` LONGTEXT NOT NULL")
	assert.Contains(t, sql, "`balance` DECIMAL(65, 10)")
	assert.Contains(t, sql, "`status` LONGTEXT NOT NULL")
	assert.NotContains(t, sql, "NOT NULL DEFAULT", "DDL never carries caller-supplied defaults (#1)")
	assert.Contains(t, sql, "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;")
}

func TestPostgresDialect_BuildAddDropColumnSQL(t *testing.T) {
	dialect := postgres.New()
	addSQL, err := dialect.BuildAddColumnSQL("customers", dbal.ColumnDefinition{
		Name:       "email",
		Type:       dbal.FieldTypeText,
		IsNullable: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE \"customers\" ADD COLUMN \"email\" TEXT;", addSQL)

	dropSQL, err := dialect.BuildDropColumnSQL("customers", "email")
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE \"customers\" DROP COLUMN IF EXISTS \"email\";", dropSQL)
}

func TestMariaDBDialect_BuildAddDropColumnSQL(t *testing.T) {
	dialect := mariadb.New()
	addSQL, err := dialect.BuildAddColumnSQL("customers", dbal.ColumnDefinition{
		Name:       "email",
		Type:       dbal.FieldTypeText,
		IsNullable: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE `customers` ADD COLUMN `email` LONGTEXT;", addSQL)

	dropSQL, err := dialect.BuildDropColumnSQL("customers", "email")
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE `customers` DROP COLUMN `email`;", dropSQL)
}

// Every user table numbers its records in creation order, in a column the
// engine fills; an existing table is given it, with a bounded lock wait on
// MariaDB.
func TestDialects_CreationOrderColumn(t *testing.T) {
	def := sampleTable()
	def.CreationOrder = true
	pg, err := postgres.New().BuildCreateTableSQL(def)
	require.NoError(t, err)
	assert.Contains(t, pg, `"f4b$creation_order" BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE`)
	maria, err := mariadb.New().BuildCreateTableSQL(def)
	require.NoError(t, err)
	assert.Contains(t, maria, "`f4b$creation_order` BIGINT NOT NULL AUTO_INCREMENT UNIQUE")
	assert.Contains(t, pg, `"f4b$mod_count" BIGINT NOT NULL DEFAULT 0`, "and counts each record's modifications")
	assert.Contains(t, maria, "`f4b$mod_count` BIGINT NOT NULL DEFAULT 0")
	assert.Equal(t, `ALTER TABLE "customers" ADD COLUMN "f4b$mod_count" BIGINT NOT NULL DEFAULT 0`,
		dbal.AddModCountSQL(postgres.New(), "customers", 5))
	assert.Equal(t, "ALTER TABLE `customers` WAIT 5 ADD COLUMN `f4b$mod_count` BIGINT NOT NULL DEFAULT 0, ALGORITHM=INSTANT",
		dbal.AddModCountSQL(mariadb.New(), "customers", 5))
	assert.Equal(t, " OVERRIDING SYSTEM VALUE", dbal.OverridingSystemValue(dbal.EnginePostgres))
	assert.Empty(t, dbal.OverridingSystemValue(dbal.EngineMariaDB))

	assert.Equal(t, `ALTER TABLE "customers" ADD COLUMN "f4b$creation_order" BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE`,
		dbal.AddCreationOrderSQL(postgres.New(), "customers", 5), "PostgreSQL bounds the wait with lock_timeout")
	assert.Equal(t, "ALTER TABLE `customers` WAIT 5 ADD COLUMN `f4b$creation_order` BIGINT NOT NULL AUTO_INCREMENT UNIQUE",
		dbal.AddCreationOrderSQL(mariadb.New(), "customers", 5))
	assert.NotContains(t, dbal.AddCreationOrderSQL(mariadb.New(), "customers", 0), "WAIT")
}
