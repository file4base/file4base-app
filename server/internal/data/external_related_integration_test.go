package data_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Related records of another solution's table (#107): a relationship of
// Orders, matching on two criteria,
//
//	Orders::customer_code = Customers::code
//	AND Orders::region = Customers::region
//
// where Customers is a table of another solution, its own database. The
// match values are read in Orders, the related records in Customers, with
// no statement joining the two, and a list reads every record's related
// records in a fixed number of statements, however many records it shows.
func TestExternalRelated_ReadsTheOtherSolutionWithoutJoinOrN1(t *testing.T) {
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		t.Skip("database not available:", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
	}
	open := func(prefix string) (string, dbal.DatabaseDriver, *atomic.Int64) {
		name := fmt.Sprintf("f4b_%s_%d", prefix, uniqueTestStamp())
		require.NoError(t, mgr.CreateDatabase(ctx, name))
		t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
		plain, err := mgr.DriverFor(ctx, name)
		require.NoError(t, err)
		require.NoError(t, schema.NewService(plain).EnsureSystemTables(ctx))
		counting, queries := countingDriver(t, name)
		return name, counting, queries
	}
	_, orders, ordersQueries := open("xrel_orders")
	peerDB, peer, peerQueries := open("xrel_peer")
	ordersSchema, peerSchema := schema.NewService(orders), schema.NewService(peer)

	addTable := func(svc *schema.Service, name string, fields ...string) (string, map[string]string) {
		tbl, err := svc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		ids := map[string]string{}
		for _, f := range fields {
			col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{Name: f, DisplayName: f, FieldType: dbal.FieldTypeText, IsNullable: true})
			require.NoError(t, err)
			ids[f] = col.ID
		}
		return tbl.ID, ids
	}
	addTable(peerSchema, "customers", "code", "region", "full_name")
	ordersTable, orderFields := addTable(ordersSchema, "orders", "customer_code", "region")
	occurrences, err := ordersSchema.ListTableOccurrences(ctx)
	require.NoError(t, err)
	ordersOcc := ""
	for _, o := range occurrences {
		if o.BaseTableID == ordersTable {
			ordersOcc = o.ID
		}
	}
	require.NotEmpty(t, ordersOcc)

	ref, err := ordersSchema.CreateFileReference(ctx, schema.FileReferenceInput{Name: "Peer"})
	require.NoError(t, err)
	ext, err := ordersSchema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "order_customers", External: &schema.ExternalOccurrenceInput{FileReference: "Peer"},
	})
	require.NoError(t, err)
	rel, err := ordersSchema.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_customers", LeftOccurrenceID: ordersOcc, RightOccurrenceID: ext.ID,
		Predicates: []schema.RelationshipPredicate{
			{LeftColumnID: orderFields["customer_code"], Operator: "=", RightColumnName: "code"},
			{LeftColumnID: orderFields["region"], Operator: "=", RightColumnName: "region"},
		},
	})
	require.NoError(t, err)
	report, err := ordersSchema.BindFileReference(ctx, ref.ID, peerDB, peerSchema, map[string]string{"order_customers": "customers"})
	require.NoError(t, err)
	require.Equal(t, schema.ExternalBound, report.Occurrences[0].Status, report.Occurrences[0].Problems)

	peerData := data.NewService(peer)
	for _, c := range [][3]string{{"C-1", "north", "Ada"}, {"C-1", "south", "Ada South"}, {"C-2", "north", "Bob"}} {
		_, err := peerData.InsertRow(ctx, "customers", map[string]interface{}{"code": c[0], "region": c[1], "full_name": c[2]})
		require.NoError(t, err)
	}
	ordersData := data.NewService(orders).WithPeers(staticPeers{peerDB: peerData})
	want := map[string][]string{}
	var ids []string
	insert := func(code, region string, names ...string) {
		row, err := ordersData.InsertRow(ctx, "orders", map[string]interface{}{"customer_code": code, "region": region})
		require.NoError(t, err)
		id := fmt.Sprint(row["id"])
		ids = append(ids, id)
		want[id] = append([]string{}, names...)
	}
	insert("C-1", "north", "Ada")
	insert("C-1", "south", "Ada South")
	insert("C-2", "south") // the code alone does not relate it
	insert("", "north")    // an empty key relates to nothing
	for i := 0; i < 40; i++ {
		insert("C-2", "north", "Bob")
	}
	from := data.LocalOrigin("orders")
	names := func(rows []map[string]interface{}) []string {
		out := []string{}
		for _, r := range rows {
			out = append(out, fmt.Sprint(r["full_name"]))
		}
		return out
	}

	// One record at a time.
	for _, id := range ids[:4] {
		rows, err := ordersData.RelatedRowsFrom(ctx, from, id, rel.ID, "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, want[id], names(rows), "order %s", id)
	}

	// A list: what each record gets alone, in a fixed number of statements.
	batch := func(ids []string) int64 {
		ordersQueries.Store(0)
		peerQueries.Store(0)
		got, err := ordersData.RelatedRowsBatch(ctx, from, ids, rel.ID, "", data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, got, len(ids))
		for _, id := range ids {
			assert.Equal(t, want[id], names(got[id]), "order %s", id)
		}
		return ordersQueries.Load() + peerQueries.Load()
	}
	few, many := batch(ids[:3]), batch(ids)
	assert.Equal(t, few, many, "a batch of %d records takes as many statements as one of 3", len(ids))
	assert.Less(t, many, int64(len(ids)), "fewer statements than records")

	// A calculation reads the other solution through the same relationship,
	// both criteria included.
	_, err = ordersSchema.AddColumn(ctx, ordersTable, schema.ColumnMetadata{Name: "customer_names", DisplayName: "customer_names",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: unstoredCalcJSON("List ( order_customers::full_name )", "Text")})
	require.NoError(t, err)
	row, err := ordersData.GetRow(ctx, "orders", ids[0])
	require.NoError(t, err)
	assert.Equal(t, "Ada", row["customer_names"])

	// The other solution's records are read there: none of them is in Orders.
	_, err = data.NewService(orders).ListRows(ctx, "customers", data.QueryOptions{})
	assert.ErrorIs(t, err, data.ErrTableNotFound)
}

// countingDriver opens a database through a connection that counts the
// statements it runs.
func countingDriver(t *testing.T, database string) (dbal.DatabaseDriver, *atomic.Int64) {
	t.Helper()
	d, queries, _ := instrumentedDriver(t, database)
	return d, queries
}

// faultyDriver opens a database through a connection that fails every
// statement the returned check, once set, says to.
func faultyDriver(t *testing.T, database string) (dbal.DatabaseDriver, *atomic.Pointer[func(query string) bool]) {
	t.Helper()
	d, _, fail := instrumentedDriver(t, database)
	return d, fail
}

func instrumentedDriver(t *testing.T, database string) (dbal.DatabaseDriver, *atomic.Int64, *atomic.Pointer[func(query string) bool]) {
	t.Helper()
	var connector driver.Connector
	if testdb.Engine() == dbal.EngineSQLite {
		connector = dbal.SQLiteConnector(filepath.Join(testdb.AdminDSN(), database+dbal.SQLiteFileExtension))
	} else {
		dsn, err := dbal.NormalizeDSN(testdb.Engine(), dsnForDatabase(testdb.DevDSN(), database))
		require.NoError(t, err)
		name := "pgx"
		if testdb.Engine() == dbal.EngineMariaDB {
			name = "mysql"
		}
		base, err := sql.Open(name, dsn)
		require.NoError(t, err)
		connector, err = base.Driver().(driver.DriverContext).OpenConnector(dsn)
		require.NoError(t, err)
		_ = base.Close()
	}
	queries := &atomic.Int64{}
	fail := &atomic.Pointer[func(query string) bool]{}
	db := sql.OpenDB(countingConnector{Connector: connector, queries: queries, fail: fail})
	t.Cleanup(func() { _ = db.Close() })
	dialect, err := dbal.NewDialect(testdb.Engine())
	require.NoError(t, err)
	return dbal.NewGenericDriver(dialect, db), queries, fail
}

// dsnForDatabase is a DSN with another database: the last path segment, in
// both the URL form and MariaDB's user@tcp(host)/database form.
func dsnForDatabase(dsn, database string) string {
	rest, query, hasQuery := strings.Cut(dsn, "?")
	if i := strings.LastIndex(rest, "/"); i >= 0 {
		rest = rest[:i+1] + database
	}
	if hasQuery {
		return rest + "?" + query
	}
	return rest
}

type countingConnector struct {
	driver.Connector
	queries *atomic.Int64
	fail    *atomic.Pointer[func(query string) bool]
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return countingConn{Conn: conn, queries: c.queries, fail: c.fail}, nil
}

// countingConn counts the queries run on a connection, fails the statements
// fail says to, and passes everything else to the engine's own.
type countingConn struct {
	driver.Conn
	queries *atomic.Int64
	fail    *atomic.Pointer[func(query string) bool]
}

// errInjected is the failure a test injects into a statement.
var errInjected = errors.New("injected failure")

func (c countingConn) failing(query string) bool {
	check := c.fail.Load()
	return check != nil && (*check)(query)
}

func (c countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func (c countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.failing(query) {
		return nil, errInjected
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if c.failing(query) {
		return nil, errInjected
	}
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
}

func (c countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c countingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

func (c countingConn) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

// staticPeers opens the solutions it holds, as the services given.
type staticPeers map[string]*data.Service

func (p staticPeers) Data(_ context.Context, database string) (*data.Service, error) {
	if svc, ok := p[database]; ok {
		return svc, nil
	}
	return nil, &data.ExternalUnavailableError{Database: database, Reason: data.ExternalNotSignedIn}
}
