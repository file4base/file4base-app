package data_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cascadePair is Orders, whose orders cascade to their lines in Orders and
// to their customers in Peer, another solution, on two criteria (#107):
//
//	Orders::customer_code = Lines::order_code
//	Orders::customer_code = Customers::code AND Orders::region = Customers::region
type cascadePair struct {
	ctx                context.Context
	ordersDB, peerDB   string
	orders, peer       *data.Service
	failOrders, failPB func(func(query string) bool)
}

func newCascadePair(t *testing.T) *cascadePair {
	t.Helper()
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
	p := &cascadePair{ctx: ctx}
	open := func(prefix string) (string, dbal.DatabaseDriver, func(func(string) bool)) {
		name := fmt.Sprintf("f4b_%s_%d", prefix, uniqueTestStamp())
		require.NoError(t, mgr.CreateDatabase(ctx, name))
		t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
		plain, err := mgr.DriverFor(ctx, name)
		require.NoError(t, err)
		require.NoError(t, schema.NewService(plain).EnsureSystemTables(ctx))
		d, fail := faultyDriver(t, name)
		return name, d, func(check func(string) bool) {
			if check == nil {
				fail.Store(nil)
				return
			}
			fail.Store(&check)
		}
	}
	var orders, peer dbal.DatabaseDriver
	p.ordersDB, orders, p.failOrders = open("xcas_orders")
	p.peerDB, peer, p.failPB = open("xcas_peer")
	ordersSchema, peerSchema := schema.NewService(orders), schema.NewService(peer)

	addTable := func(svc *schema.Service, name string, fields ...string) map[string]string {
		tbl, err := svc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		ids := map[string]string{"": tbl.ID}
		for _, f := range fields {
			col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{Name: f, DisplayName: f, FieldType: dbal.FieldTypeText, IsNullable: true})
			require.NoError(t, err)
			ids[f] = col.ID
		}
		return ids
	}
	addTable(peerSchema, "customers", "code", "region", "full_name")
	orderFields := addTable(ordersSchema, "orders", "customer_code", "region")
	lineFields := addTable(ordersSchema, "lines", "order_code", "label")
	occurrences, err := ordersSchema.ListTableOccurrences(ctx)
	require.NoError(t, err)
	occ := map[string]string{}
	for _, o := range occurrences {
		occ[o.BaseTableID] = o.ID
	}

	ref, err := ordersSchema.CreateFileReference(ctx, schema.FileReferenceInput{Name: "Peer"})
	require.NoError(t, err)
	ext, err := ordersSchema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "order_customers", External: &schema.ExternalOccurrenceInput{FileReference: "Peer"},
	})
	require.NoError(t, err)
	_, err = ordersSchema.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_customers", LeftOccurrenceID: occ[orderFields[""]], RightOccurrenceID: ext.ID,
		Predicates: []schema.RelationshipPredicate{
			{LeftColumnID: orderFields["customer_code"], Operator: "=", RightColumnName: "code"},
			{LeftColumnID: orderFields["region"], Operator: "=", RightColumnName: "region"},
		},
		CascadeDelete: true,
	})
	require.NoError(t, err)
	_, err = ordersSchema.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_lines", LeftOccurrenceID: occ[orderFields[""]], LeftColumnID: orderFields["customer_code"],
		RightOccurrenceID: occ[lineFields[""]], RightColumnID: lineFields["order_code"], Operator: "=",
		CascadeDelete: true,
	})
	require.NoError(t, err)
	_, err = ordersSchema.BindFileReference(ctx, ref.ID, p.peerDB, peerSchema, map[string]string{"order_customers": "customers"})
	require.NoError(t, err)

	p.peer = data.NewService(peer).WithDatabase(p.peerDB)
	p.orders = data.NewService(orders).WithDatabase(p.ordersDB).WithPeers(staticPeers{p.peerDB: p.peer})
	return p
}

func (p *cascadePair) insert(t *testing.T, svc *data.Service, table string, record map[string]interface{}) string {
	t.Helper()
	row, err := svc.InsertRow(p.ctx, table, record)
	require.NoError(t, err)
	return fmt.Sprint(row["id"])
}

func (p *cascadePair) values(t *testing.T, svc *data.Service, table, field string) []string {
	t.Helper()
	rows, err := svc.ListRows(p.ctx, table, data.QueryOptions{})
	require.NoError(t, err)
	out := []string{}
	for _, r := range rows {
		out = append(out, fmt.Sprint(r[field]))
	}
	sort.Strings(out)
	return out
}

// deletesFrom matches the statements that delete records of a table, in
// either engine's quoting.
func deletesFrom(table string) func(string) bool {
	return func(query string) bool {
		q := strings.ToLower(query)
		return strings.HasPrefix(strings.TrimSpace(q), "delete from") &&
			(strings.Contains(q, `"`+table+`"`) || strings.Contains(q, "`"+table+"`"))
	}
}

// A cascading delete that crosses solutions plans everything first, deletes
// the other solution's records in one transaction there, then this one's in
// one transaction here; a failure tells what was done.
func TestExternalCascade_PlansThenDeletesEachSolutionInOneTransaction(t *testing.T) {
	p := newCascadePair(t)
	for _, c := range [][3]string{{"C-1", "north", "Ada"}, {"C-1", "south", "Ada South"}, {"C-2", "north", "Bob"}} {
		p.insert(t, p.peer, "customers", map[string]interface{}{"code": c[0], "region": c[1], "full_name": c[2]})
	}
	order := p.insert(t, p.orders, "orders", map[string]interface{}{"customer_code": "C-1", "region": "north"})
	p.insert(t, p.orders, "orders", map[string]interface{}{"customer_code": "C-2", "region": "north"})
	p.insert(t, p.orders, "lines", map[string]interface{}{"order_code": "C-1", "label": "first"})
	p.insert(t, p.orders, "lines", map[string]interface{}{"order_code": "C-1", "label": "second"})
	p.insert(t, p.orders, "lines", map[string]interface{}{"order_code": "C-2", "label": "other"})
	unchanged := func(t *testing.T) {
		t.Helper()
		assert.Equal(t, []string{"C-1", "C-2"}, p.values(t, p.orders, "orders", "customer_code"))
		assert.Equal(t, []string{"first", "other", "second"}, p.values(t, p.orders, "lines", "label"))
		assert.Equal(t, []string{"Ada", "Ada South", "Bob"}, p.values(t, p.peer, "customers", "full_name"))
	}

	t.Run("refused in the other solution, nothing is deleted", func(t *testing.T) {
		p.peer.WithWriteAccess(func(_ context.Context, table, action string) error {
			return &data.PermissionError{Account: "alice", Table: table, Action: action}
		})
		defer p.peer.WithWriteAccess(nil)
		err := p.orders.DeleteRow(p.ctx, "orders", order)
		require.ErrorIs(t, err, data.ErrNoPermission)
		var perm *data.PermissionError
		require.ErrorAs(t, err, &perm)
		require.NotNil(t, perm.External)
		assert.Equal(t, p.peerDB, perm.External.Database)
		assert.Equal(t, "customers", perm.Table)
		assert.Equal(t, data.ActionDelete, perm.Action)
		assert.Contains(t, err.Error(), `"Peer"`)
		unchanged(t)
	})

	t.Run("the other solution fails, nothing is deleted", func(t *testing.T) {
		p.failPB(deletesFrom("customers"))
		defer p.failPB(nil)
		err := p.orders.DeleteRow(p.ctx, "orders", order)
		require.ErrorIs(t, err, errInjected)
		assert.NotErrorIs(t, err, data.ErrPartialDelete)
		assert.ErrorContains(t, err, p.peerDB)
		assert.ErrorContains(t, err, `"Peer"`)
		unchanged(t)
	})

	t.Run("this solution fails after the other committed", func(t *testing.T) {
		p.failOrders(deletesFrom("lines"))
		defer p.failOrders(nil)
		err := p.orders.DeleteRow(p.ctx, "orders", order)
		require.ErrorIs(t, err, data.ErrPartialDelete)
		require.ErrorIs(t, err, errInjected)
		var partial *data.PartialDeleteError
		require.ErrorAs(t, err, &partial)
		assert.Equal(t, []string{p.peerDB}, partial.Committed)
		assert.Equal(t, p.ordersDB, partial.Database)
		assert.Contains(t, err.Error(), "delete them again")
		// The order and its lines are kept together: the local part is one
		// transaction.
		assert.Equal(t, []string{"C-1", "C-2"}, p.values(t, p.orders, "orders", "customer_code"))
		assert.Equal(t, []string{"first", "other", "second"}, p.values(t, p.orders, "lines", "label"))
		assert.Equal(t, []string{"Ada South", "Bob"}, p.values(t, p.peer, "customers", "full_name"))
	})

	t.Run("deleted again once the cause is fixed", func(t *testing.T) {
		require.NoError(t, p.orders.DeleteRow(p.ctx, "orders", order))
		assert.Equal(t, []string{"C-2"}, p.values(t, p.orders, "orders", "customer_code"))
		assert.Equal(t, []string{"other"}, p.values(t, p.orders, "lines", "label"))
		assert.Equal(t, []string{"Ada South", "Bob"}, p.values(t, p.peer, "customers", "full_name"))
		err := p.orders.DeleteRow(p.ctx, "orders", order)
		assert.ErrorIs(t, err, data.ErrRecordNotFound)
	})

	t.Run("the other solution unreachable refuses before any change", func(t *testing.T) {
		orders := p.orders.WithPeers(staticPeers{})
		defer orders.WithPeers(staticPeers{p.peerDB: p.peer})
		other := p.insert(t, p.orders, "orders", map[string]interface{}{"customer_code": "C-2", "region": "north"})
		err := orders.DeleteRow(p.ctx, "orders", other)
		var unavailable *data.ExternalUnavailableError
		require.ErrorAs(t, err, &unavailable)
		assert.Equal(t, data.ExternalNotSignedIn, unavailable.Reason)
		assert.Equal(t, []string{"C-2", "C-2"}, p.values(t, p.orders, "orders", "customer_code"))
		assert.Equal(t, []string{"other"}, p.values(t, p.orders, "lines", "label"))
		assert.Equal(t, []string{"Ada South", "Bob"}, p.values(t, p.peer, "customers", "full_name"))
	})
}
