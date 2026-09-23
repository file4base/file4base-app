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
	defVal := "'active'"
	return dbal.TableDefinition{
		Name: "customers",
		Columns: []dbal.ColumnDefinition{
			{Name: "id", Type: dbal.FieldTypeText, IsPrimaryKey: true},
			{Name: "first_name", Type: dbal.FieldTypeText, IsNullable: false},
			{Name: "balance", Type: dbal.FieldTypeNumber, IsNullable: true},
			{Name: "status", Type: dbal.FieldTypeText, IsNullable: false, DefaultValue: &defVal},
		},
	}
}

func TestPostgresDialect_BuildCreateTableSQL(t *testing.T) {
	dialect := postgres.New()
	sql, err := dialect.BuildCreateTableSQL(sampleTable())
	require.NoError(t, err)
	assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS \"customers\" (")
	assert.Contains(t, sql, "\"id\" TEXT PRIMARY KEY")
	assert.Contains(t, sql, "\"first_name\" TEXT NOT NULL")
	assert.Contains(t, sql, "\"balance\" NUMERIC")
	assert.Contains(t, sql, "\"status\" TEXT NOT NULL DEFAULT 'active'")
}

func TestMariaDBDialect_BuildCreateTableSQL(t *testing.T) {
	dialect := mariadb.New()
	sql, err := dialect.BuildCreateTableSQL(sampleTable())
	require.NoError(t, err)
	assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS `customers` (")
	assert.Contains(t, sql, "`id` LONGTEXT PRIMARY KEY")
	assert.Contains(t, sql, "`first_name` LONGTEXT NOT NULL")
	assert.Contains(t, sql, "`balance` DECIMAL(65, 10)")
	assert.Contains(t, sql, "`status` LONGTEXT NOT NULL DEFAULT 'active'")
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
