package extsource

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Only a single SELECT is read: File4Base never writes to a source, and a
// typed statement is not the exception (#47).
func TestCheckQuery(t *testing.T) {
	for _, accepted := range []string{
		"SELECT * FROM customers",
		"  select id, name from customers where city = 'Paris' ;",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
	} {
		got, err := checkQuery(accepted)
		require.NoError(t, err, accepted)
		assert.False(t, strings.HasSuffix(got, ";"))
	}

	for _, refused := range []string{
		"",
		"   ",
		"DELETE FROM customers",
		"UPDATE customers SET city = 'Paris'",
		"DROP TABLE customers",
		"SELECT 1; DROP TABLE customers",
		"INSERT INTO customers (id) VALUES ('x')",
		"EXEC sp_who",
	} {
		_, err := checkQuery(refused)
		assert.ErrorIs(t, err, ErrUnsafeStatement, refused)
	}
}

// A table name is quoted in the engine's own spelling, and anything that is
// not a plain name is refused rather than concatenated into SQL.
func TestQuoteQualified(t *testing.T) {
	cases := []struct {
		engine Engine
		name   string
		want   string
	}{
		{EnginePostgres, "customers", `"customers"`},
		{EnginePostgres, "public.customers", `"public"."customers"`},
		{EngineMySQL, "customers", "`customers`"},
		{EngineSQLServer, "dbo.customers", "[dbo].[customers]"},
	}
	for _, c := range cases {
		got, err := quoteQualified(c.engine, c.name)
		require.NoError(t, err, c.name)
		assert.Equal(t, c.want, got)
	}

	for _, refused := range []string{
		"customers; DROP TABLE x",
		`customers" OR 1=1 --`,
		"a.b.c",
		"",
		"  ",
		"2fast",
		"custo mers",
	} {
		_, err := quoteQualified(EnginePostgres, refused)
		assert.ErrorIs(t, err, ErrUnsafeStatement, refused)
	}
}

// The row cap is applied by the source database where the engine allows it.
func TestLimitedSelect(t *testing.T) {
	got := limitedSelect(EnginePostgres, "SELECT * FROM customers", 101)
	assert.Equal(t, "SELECT * FROM (SELECT * FROM customers) AS f4b_source LIMIT 101", got)

	// SQL Server has no LIMIT and TOP cannot be bolted on from outside, so
	// the statement is left alone and the reader's own cap applies.
	assert.Equal(t, "SELECT * FROM customers", limitedSelect(EngineSQLServer, "SELECT * FROM customers", 101))
}

// The connection string is built by the driver's own means, with the
// password supplied per read.
func TestDataSourceName(t *testing.T) {
	driver, dsn, err := dataSourceName(Connection{
		Engine: EnginePostgres, Host: "db.example", Database: "sales", Username: "reader",
	}, "s3cret")
	require.NoError(t, err)
	assert.Equal(t, "pgx", driver)
	assert.Contains(t, dsn, "db.example:5432", "the engine's default port is used")
	assert.Contains(t, dsn, "sslmode=disable")

	driver, dsn, err = dataSourceName(Connection{
		Engine: EngineMySQL, Host: "127.0.0.1", Port: 3307, Database: "sales", Username: "reader", TLS: true,
	}, "s3cret")
	require.NoError(t, err)
	assert.Equal(t, "mysql", driver)
	assert.Contains(t, dsn, "tcp(127.0.0.1:3307)")
	assert.Contains(t, dsn, "tls=preferred")

	driver, _, err = dataSourceName(Connection{
		Engine: EngineSQLServer, Host: "sql.example", Database: "sales", Username: "reader",
	}, "s3cret")
	require.NoError(t, err)
	assert.Equal(t, "sqlserver", driver)

	_, _, err = dataSourceName(Connection{Engine: "oracle", Host: "h", Database: "d"}, "")
	assert.ErrorIs(t, err, ErrUnsupportedEngine)
}

// A connection that cannot be used is refused before a driver sees it.
func TestCheckConnection(t *testing.T) {
	assert.ErrorIs(t, checkConnection(Connection{Engine: "odbc", Host: "h", Database: "d"}), ErrUnsupportedEngine)
	assert.ErrorIs(t, checkConnection(Connection{Engine: EnginePostgres, Database: "d"}), ErrConnection)
	assert.ErrorIs(t, checkConnection(Connection{Engine: EnginePostgres, Host: "h"}), ErrConnection)
	// MySQL can connect without naming a database, so that one is allowed.
	assert.NoError(t, checkConnection(Connection{Engine: EngineMySQL, Host: "h"}))
}

func TestEngineLabels(t *testing.T) {
	assert.Equal(t, "PostgreSQL", EnginePostgres.Label())
	assert.Equal(t, "MySQL / MariaDB", EngineMySQL.Label())
	assert.Equal(t, "Microsoft SQL Server", EngineSQLServer.Label())
	assert.Equal(t, 1433, EngineSQLServer.DefaultPort())
}
