package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// externalSourceSettings describes the suite's own database as an external
// data source, so the import is exercised against a real other database.
func externalSourceSettings(t *testing.T) (map[string]interface{}, string) {
	t.Helper()
	dsn := testdb.DevDSN()
	if testdb.Engine() == dbal.EngineMariaDB {
		cfg, err := mysql.ParseDSN(dsn)
		require.NoError(t, err)
		host, portText, _ := strings.Cut(cfg.Addr, ":")
		port, _ := strconv.Atoi(portText)
		return map[string]interface{}{
			"engine": "mysql", "host": host, "port": port,
			"database": cfg.DBName, "username": cfg.User,
		}, cfg.Passwd
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	port, _ := strconv.Atoi(u.Port())
	password, _ := u.User.Password()
	return map[string]interface{}{
		"engine": "postgres", "host": u.Hostname(), "port": port,
		"database": strings.TrimPrefix(u.Path, "/"), "username": u.User.Username(),
	}, password
}

// Importing records from another SQL database (#47), end to end: an owner
// registers the connection, the import names it, and the records arrive
// through the same mapping and validation a file goes through.
func TestAPI_ImportFromAnExternalSource(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_extsrc")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	// Somebody else's table, in the database the suite itself runs against.
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	external := fmt.Sprintf("ext_members_%d", time.Now().UnixNano()%1000000)
	_, err = driver.DB().ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (full_name VARCHAR(64), city VARCHAR(64))`, external))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = driver.DB().ExecContext(context.Background(), "DROP TABLE "+external) })
	for _, row := range [][2]string{{"Sophie Tang", "Hong Kong"}, {"Gerard LeFranc", "Paris"}} {
		placeholders := "($1, $2)"
		if testdb.Engine() == dbal.EngineMariaDB {
			placeholders = "(?, ?)"
		}
		_, err = driver.DB().ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s (full_name, city) VALUES %s", external, placeholders), row[0], row[1])
		require.NoError(t, err)
	}

	// The File4Base table the records are imported into.
	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
		map[string]string{"display_name": "Members", "custom_name": "members"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for _, field := range []string{"full_name", "city"} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner,
			map[string]interface{}{"display_name": field, "name": field, "field_type": "TEXT"}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	settings, password := externalSourceSettings(t)
	settings["name"] = "Legacy members"

	var source schema.DataSource
	rec = h.do(http.MethodPost, "/api/v1/data-sources", owner, settings, &source)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotEmpty(t, source.ID)
	assert.NotContains(t, rec.Body.String(), password, "a data source holds no password")

	// The source's tables are listed, which is also what "test connection" does.
	var listing struct {
		Tables []string `json:"tables"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data-sources/"+source.ID+"/tables", owner,
		map[string]string{"password": password}, &listing)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, listing.Tables, external)

	// Previewing reads the columns and the first rows, and writes nothing.
	body := map[string]interface{}{
		"data_source": map[string]interface{}{"id": source.ID, "table": external, "password": password},
	}
	var preview struct {
		Columns  []string   `json:"columns"`
		Rows     [][]string `json:"rows"`
		RowCount int        `json:"row_count"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/members/import/preview", owner, body, &preview)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"full_name", "city"}, preview.Columns)
	assert.Equal(t, 2, preview.RowCount)
	var before []map[string]interface{}
	h.do(http.MethodGet, "/api/v1/data/members", owner, nil, &before)
	assert.Empty(t, before, "a preview writes nothing")

	// The import itself goes through the same mapping as a file's.
	body["options"] = map[string]interface{}{
		"action": "add",
		"mappings": []map[string]interface{}{
			{"column": 0, "field": "full_name"},
			{"column": 1, "field": "city"},
		},
	}
	var report struct {
		Rows  int `json:"rows"`
		Added int `json:"added"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/members/import", owner, body, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, report.Added)

	var rows []map[string]interface{}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/members", owner, nil, &rows).Code)
	require.Len(t, rows, 2)

	// A statement that is not a SELECT never reaches the source.
	rec = h.do(http.MethodPost, "/api/v1/data/members/import/preview", owner, map[string]interface{}{
		"data_source": map[string]interface{}{
			"id": source.ID, "query": "DROP TABLE " + external, "password": password,
		},
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// A source that is not registered cannot be read, so an import cannot
	// name a machine of its own.
	rec = h.do(http.MethodPost, "/api/v1/data/members/import/preview", owner, map[string]interface{}{
		"data_source": map[string]interface{}{"id": "00000000-0000-0000-0000-000000000000", "table": external},
	}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// A wrong password is reported as the source being unreachable.
	rec = h.do(http.MethodPost, "/api/v1/data/members/import/preview", owner, map[string]interface{}{
		"data_source": map[string]interface{}{"id": source.ID, "table": external, "password": "nope"},
	}, nil)
	assert.Equal(t, http.StatusBadGateway, rec.Code)

	// Only an owner registers or changes a source; an admin may read the list.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "bob", "password": "bob-secret-pw", "role": "admin"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	admin := h.login(db, "bob", "bob-secret-pw")
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data-sources", admin, nil, nil).Code)
	rec = h.do(http.MethodPost, "/api/v1/data-sources", admin, map[string]interface{}{
		"name": "Mine", "engine": "postgres", "host": "evil.example", "database": "x", "username": "y",
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodDelete, "/api/v1/data-sources/"+source.ID, admin, nil, nil).Code)

	// What cannot be stored is refused.
	for name, bad := range map[string]map[string]interface{}{
		"no name":        {"engine": "postgres", "host": "h", "database": "d"},
		"unknown engine": {"name": "x", "engine": "oracle", "host": "h", "database": "d"},
		"no host":        {"name": "y", "engine": "postgres", "database": "d"},
		"bad env name":   {"name": "z", "engine": "postgres", "host": "h", "password_env": "not a name"},
	} {
		rec = h.do(http.MethodPost, "/api/v1/data-sources", owner, bad, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
	}

	// The connection travels in the solution file, without a password.
	file := exportSolution(t, h, owner, map[string]interface{}{"solution_name": "Members"})
	assert.NotContains(t, string(file), password, "a solution file carries no data source password")
	dstDB := uniqueDB("f4b_extsrc_dst")
	h.createDatabase(dstDB, "olivia", "olivia-secret")
	dst := h.login(dstDB, "olivia", "olivia-secret")
	var importReport map[string]interface{}
	rec = h.postBytes("/api/v1/solutions/import", dst, file, &importReport)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 1, importReport["data_sources_created"])
}
