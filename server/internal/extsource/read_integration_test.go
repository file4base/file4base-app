package extsource_test

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/extsource"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConnection turns the suite's own DSN into an external connection, so
// the reader is exercised against a real database of the engine under test.
func testConnection(t *testing.T) (extsource.Connection, string) {
	t.Helper()
	dsn := testdb.DevDSN()

	if testdb.Engine() == dbal.EngineMariaDB {
		cfg, err := mysql.ParseDSN(dsn)
		require.NoError(t, err)
		host, portText, _ := strings.Cut(cfg.Addr, ":")
		port, _ := strconv.Atoi(portText)
		return extsource.Connection{
			Engine: extsource.EngineMySQL, Host: host, Port: port,
			Database: cfg.DBName, Username: cfg.User,
		}, cfg.Passwd
	}

	u, err := url.Parse(dsn)
	require.NoError(t, err)
	port, _ := strconv.Atoi(u.Port())
	password, _ := u.User.Password()
	return extsource.Connection{
		Engine: extsource.EnginePostgres, Host: u.Hostname(), Port: port,
		Database: strings.TrimPrefix(u.Path, "/"), Username: u.User.Username(),
	}, password
}

// Reading records out of another SQL database (#47): what the import
// pipeline receives is the same table of text a file parser produces.
func TestReadFromAnotherDatabase(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	// A table that is not File4Base's own: this is somebody else's database.
	table := fmt.Sprintf("ext_people_%d", time.Now().UnixNano()%1000000)
	_, err = driver.DB().ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (person_id INT, full_name VARCHAR(64), fee NUMERIC(10,2))`, table))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = driver.DB().ExecContext(context.Background(), "DROP TABLE "+table)
	})
	for i, row := range []struct {
		id   int
		name string
		fee  string
	}{
		{1, "Sophie Tang", "200.00"},
		{2, "Gérard LeFranc", "100.50"},
		{3, "", "0"},
	} {
		placeholder := "($1, $2, $3)"
		if testdb.Engine() == dbal.EngineMariaDB {
			placeholder = "(?, ?, ?)"
		}
		_, err = driver.DB().ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s (person_id, full_name, fee) VALUES %s", table, placeholder),
			row.id, row.name, row.fee)
		require.NoError(t, err, "row %d", i)
	}

	conn, password := testConnection(t)

	// The tables of the source are listed, so the dialog can offer them.
	tables, err := extsource.ListTables(ctx, conn, password)
	require.NoError(t, err)
	assert.Contains(t, tables, table)

	// Reading a whole table gives its columns and every value as text.
	read, err := extsource.Read(ctx, conn, password, extsource.ReadOptions{Table: table})
	require.NoError(t, err)
	assert.Equal(t, []string{"person_id", "full_name", "fee"}, read.Columns)
	require.Len(t, read.Rows, 3)
	assert.Equal(t, "1", read.Rows[0][0])
	assert.Equal(t, "Sophie Tang", read.Rows[0][1])
	assert.Equal(t, "Gérard LeFranc", read.Rows[1][1], "text comes back as it was stored")
	assert.Equal(t, "", read.Rows[2][1], "an empty value stays empty")
	assert.False(t, read.Truncated)

	// A statement of the caller's own is read too, once it is checked.
	read, err = extsource.Read(ctx, conn, password, extsource.ReadOptions{
		Query: fmt.Sprintf("SELECT full_name FROM %s WHERE person_id = 1", table),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"full_name"}, read.Columns)
	require.Len(t, read.Rows, 1)
	assert.Equal(t, "Sophie Tang", read.Rows[0][0])

	// A source with more rows than the limit is cut, and says so.
	read, err = extsource.Read(ctx, conn, password, extsource.ReadOptions{
		Table:  table,
		Limits: dataio.Limits{MaxRows: 2, MaxColumns: 16, MaxBytes: 1 << 20},
	})
	require.NoError(t, err)
	assert.Len(t, read.Rows, 2)
	assert.True(t, read.Truncated)

	// A source with more columns than the limit is refused rather than cut:
	// a half-read row would map to the wrong fields.
	_, err = extsource.Read(ctx, conn, password, extsource.ReadOptions{
		Table:  table,
		Limits: dataio.Limits{MaxRows: 10, MaxColumns: 2, MaxBytes: 1 << 20},
	})
	assert.ErrorIs(t, err, dataio.ErrSourceTooLarge)

	// Anything that is not a single SELECT never reaches the source.
	_, err = extsource.Read(ctx, conn, password, extsource.ReadOptions{
		Query: "DROP TABLE " + table,
	})
	assert.ErrorIs(t, err, extsource.ErrUnsafeStatement)
	var stillThere int
	require.NoError(t, driver.DB().QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&stillThere))
	assert.Equal(t, 3, stillThere, "the refused statement did not run")

	// A wrong password is reported as what it is, not as a crash.
	_, err = extsource.Read(ctx, conn, "not-the-password", extsource.ReadOptions{Table: table})
	assert.ErrorIs(t, err, extsource.ErrConnection)
}
