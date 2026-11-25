package data_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// solution is one solution of a two-solution test: its own database, opened
// through a connection that counts its statements.
type solution struct {
	name    string
	driver  dbal.DatabaseDriver
	queries *atomic.Int64
	schema  *schema.Service
}

// twoSolutions creates two solutions, each its own database, or skips the
// test when no database server is reachable.
func twoSolutions(t *testing.T, prefixA, prefixB string) (context.Context, *solution, *solution) {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		t.Skip("database not available:", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
	}
	open := func(prefix string) *solution {
		name := fmt.Sprintf("f4b_%s_%d", prefix, uniqueTestStamp())
		require.NoError(t, mgr.CreateDatabase(ctx, name))
		t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
		plain, err := mgr.DriverFor(ctx, name)
		require.NoError(t, err)
		require.NoError(t, schema.NewService(plain).EnsureSystemTables(ctx))
		driver, queries := countingDriver(t, name)
		return &solution{name: name, driver: driver, queries: queries, schema: schema.NewService(driver)}
	}
	return ctx, open(prefixA), open(prefixB)
}

// table creates a table with fields of the given types, and returns its id,
// its occurrence's id and its fields' ids.
func (s *solution) table(t *testing.T, ctx context.Context, name string, fields ...[2]string) (string, string, map[string]string) {
	t.Helper()
	tbl, err := s.schema.CreateTable(ctx, name, name)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, f := range fields {
		col, err := s.schema.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{Name: f[0], DisplayName: f[0],
			FieldType: dbal.AgnosticFieldType(f[1]), IsNullable: true})
		require.NoError(t, err)
		ids[f[0]] = col.ID
	}
	occurrences, err := s.schema.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		if o.BaseTableID == tbl.ID {
			return tbl.ID, o.ID, ids
		}
	}
	t.Fatalf("table %s has no occurrence", name)
	return "", "", nil
}

// unstored adds an unstored calculation to a table.
func (s *solution) unstored(t *testing.T, ctx context.Context, tableID, name, formula, result string) {
	t.Helper()
	_, err := s.schema.AddColumn(ctx, tableID, schema.ColumnMetadata{Name: name, DisplayName: name,
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: unstoredCalcJSON(formula, result)})
	require.NoError(t, err)
}

// external adds an occurrence of a table of another solution, through the
// file reference named reference, creating that reference when it is new.
func (s *solution) external(t *testing.T, ctx context.Context, reference, occurrence string) (string, string) {
	t.Helper()
	refs, err := s.schema.ListFileReferences(ctx)
	require.NoError(t, err)
	refID := ""
	for _, r := range refs {
		if r.Name == reference {
			refID = r.ID
		}
	}
	if refID == "" {
		ref, err := s.schema.CreateFileReference(ctx, schema.FileReferenceInput{Name: reference})
		require.NoError(t, err)
		refID = ref.ID
	}
	occ, err := s.schema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: occurrence, External: &schema.ExternalOccurrenceInput{FileReference: reference},
	})
	require.NoError(t, err)
	return refID, occ.ID
}

// bind maps a file reference to the other solution, each occurrence to its
// table there.
func (s *solution) bind(t *testing.T, ctx context.Context, refID string, peer *solution, tables map[string]string) {
	t.Helper()
	report, err := s.schema.BindFileReference(ctx, refID, peer.name, peer.schema, tables)
	require.NoError(t, err)
	for _, o := range report.Occurrences {
		require.Equal(t, schema.ExternalBound, o.Status, "%s: %v", o.Name, o.Problems)
	}
}

func (s *solution) relate(t *testing.T, ctx context.Context, in schema.CreateRelationshipInput) string {
	t.Helper()
	rel, err := s.schema.CreateRelationship(ctx, in)
	require.NoError(t, err)
	return rel.ID
}

func (s *solution) statements() int64 { return s.queries.Load() }

// Unstored calculations read another solution's records through external
// occurrences (#107): a related field, aggregates, the other solution's own
// unstored calculations (which read this one back), and a ring of formulas
// across the two, which stops.
//
//	Orders (solution A)                  Customers (solution B)
//	orders::customer_code  ──────────▶  customers::code
//	order_customers (external of B)      customer_orders (external of A)
func TestExternalCalculations_ReadTheOtherSolution(t *testing.T) {
	ctx, a, b := twoSolutions(t, "xcalc_orders", "xcalc_customers")

	customersTable, customersOcc, _ := b.table(t, ctx, "customers",
		[2]string{"code", "TEXT"}, [2]string{"region", "TEXT"}, [2]string{"full_name", "TEXT"}, [2]string{"credit", "NUMBER"})
	ordersTable, ordersOcc, orderFields := a.table(t, ctx, "orders",
		[2]string{"customer_code", "TEXT"}, [2]string{"amount", "NUMBER"})

	// A reads B through order_customers, B reads A through customer_orders.
	customersRef, orderCustomers := a.external(t, ctx, "Customers", "order_customers")
	byName := "full_name"
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "orders_customers", LeftOccurrenceID: ordersOcc,
		RightOccurrenceID: orderCustomers, SortRelated: &byName, Predicates: []schema.RelationshipPredicate{
			{LeftColumnID: orderFields["customer_code"], Operator: "=", RightColumnName: "code"}}})
	ordersRef, customerOrders := b.external(t, ctx, "Orders", "customer_orders")
	customerFields := map[string]string{}
	tables, err := b.schema.ListTables(ctx)
	require.NoError(t, err)
	for _, tbl := range tables {
		if tbl.ID == customersTable {
			for _, c := range tbl.Columns {
				customerFields[c.Name] = c.ID
			}
		}
	}
	b.relate(t, ctx, schema.CreateRelationshipInput{Name: "customers_orders", LeftOccurrenceID: customersOcc,
		RightOccurrenceID: customerOrders, Predicates: []schema.RelationshipPredicate{
			{LeftColumnID: customerFields["code"], Operator: "=", RightColumnName: "customer_code"}}})
	// A reference nothing is bound to yet: reading it is missing.
	_, archived := a.external(t, ctx, "Archive", "archived_customers")
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "orders_archive", LeftOccurrenceID: ordersOcc,
		RightOccurrenceID: archived, Predicates: []schema.RelationshipPredicate{
			{LeftColumnID: orderFields["customer_code"], Operator: "=", RightColumnName: "code"}}})

	b.unstored(t, ctx, customersTable, "label", `full_name & " (" & region & ")"`, "Text")
	b.unstored(t, ctx, customersTable, "order_total", "Sum ( customer_orders::amount )", "Number")
	b.unstored(t, ctx, customersTable, "ring_b", "Sum ( customer_orders::ring_a ) + 1", "Number")
	for _, c := range [][3]string{
		{"customer_name", "order_customers::full_name", "Text"},
		{"customer_label", "order_customers::label", "Text"},
		{"customer_count", "Count ( order_customers::code )", "Number"},
		{"credit_total", "Sum ( order_customers::credit )", "Number"},
		{"names", "List ( order_customers::full_name )", "Text"},
		{"round_trip", "Sum ( order_customers::order_total )", "Number"},
		{"ring_a", "Sum ( order_customers::ring_b )", "Number"},
		{"archived_name", "archived_customers::full_name", "Text"},
	} {
		a.unstored(t, ctx, ordersTable, c[0], c[1], c[2])
	}
	a.bind(t, ctx, customersRef, b, map[string]string{"order_customers": "customers"})
	b.bind(t, ctx, ordersRef, a, map[string]string{"customer_orders": "orders"})
	// A field the other solution no longer has, as after a rename there.
	a.unstored(t, ctx, ordersTable, "renamed", "order_customers::no_such_field", "Text")

	ordersData, customersData := data.NewService(a.driver), data.NewService(b.driver)
	ordersData.WithPeers(staticPeers{b.name: customersData})
	customersData.WithPeers(staticPeers{a.name: ordersData})

	customers := map[string]string{}
	for _, c := range []struct {
		code, region, name string
		credit             int
	}{{"C-1", "north", "Ada", 10}, {"C-1", "south", "Ada South", 5}, {"C-2", "north", "Bob", 7}} {
		row, err := customersData.InsertRow(ctx, "customers", map[string]interface{}{
			"code": c.code, "region": c.region, "full_name": c.name, "credit": c.credit})
		require.NoError(t, err)
		customers[c.name] = fmt.Sprint(row["id"])
	}
	order := func(code string, amount int) string {
		row, err := ordersData.InsertRow(ctx, "orders", map[string]interface{}{"customer_code": code, "amount": amount})
		require.NoError(t, err)
		return fmt.Sprint(row["id"])
	}
	first, second := order("C-1", 100), order("C-2", 20)
	order("C-2", 30)
	empty, unmatched := order("", 1), order("C-9", 2)

	text := func(v interface{}) string {
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	row, err := ordersData.GetRow(ctx, "orders", first)
	require.NoError(t, err)
	assert.Equal(t, "Ada", text(row["customer_name"]), "the first related record, in the relationship's sort order")
	assert.Equal(t, "Ada (north)", text(row["customer_label"]), "the other solution's own calculation, computed there")
	assert.Equal(t, "2", text(row["customer_count"]))
	assert.Equal(t, "15", text(row["credit_total"]))
	assert.Equal(t, "Ada\rAda South", text(row["names"]), "List separates values with returns, as FileMaker does")
	assert.Equal(t, "200", text(row["round_trip"]), "each of the two customers totals the C-1 orders of A")
	assert.Contains(t, row, "ring_a", "a ring across the two solutions stops")
	assert.Empty(t, text(row["renamed"]), "a field the other solution does not have is empty")
	assert.Empty(t, text(row["archived_name"]), "a reference bound to no solution is empty")

	row, err = ordersData.GetRow(ctx, "orders", second)
	require.NoError(t, err)
	assert.Equal(t, "Bob", text(row["customer_name"]))
	assert.Equal(t, "7", text(row["credit_total"]))
	assert.Equal(t, "50", text(row["round_trip"]))

	for _, id := range []string{empty, unmatched} {
		row, err = ordersData.GetRow(ctx, "orders", id)
		require.NoError(t, err)
		assert.Empty(t, text(row["customer_name"]), "nothing is related")
		assert.Equal(t, "0", text(row["customer_count"]))
	}

	// B's own records read A in turn, as the account the group holds in A.
	row, err = customersData.GetRow(ctx, "customers", customers["Bob"])
	require.NoError(t, err)
	assert.Equal(t, "50", text(row["order_total"]))

	t.Run("a page reads each relationship once, however many records it shows", func(t *testing.T) {
		page := func(limit int) int64 {
			a.queries.Store(0)
			b.queries.Store(0)
			rows, err := ordersData.ListRows(ctx, "orders", data.QueryOptions{Limit: limit, SortBy: "amount", SortAsc: false})
			require.NoError(t, err)
			require.Len(t, rows, limit)
			for _, row := range rows {
				if code := text(row["customer_code"]); code != "" && code != "C-9" {
					assert.NotEmpty(t, text(row["customer_name"]), "order %v", row["id"])
					assert.NotEmpty(t, text(row["round_trip"]), "order %v", row["id"])
				}
			}
			return a.statements() + b.statements()
		}
		few := page(3)
		// Each new order has a customer of its own: as many keys as orders.
		for i := 0; i < 40; i++ {
			code := fmt.Sprintf("C-%d", 100+i)
			_, err := customersData.InsertRow(ctx, "customers", map[string]interface{}{"code": code, "full_name": "Customer " + code})
			require.NoError(t, err)
			order(code, 3)
		}
		many := page(45)
		assert.Equal(t, few, many, "a page of 45 records takes as many statements as one of 3")
		assert.Less(t, many, int64(45), "fewer statements than records")
	})

	t.Run("an account of the other solution that may not read the table gets <No Access>", func(t *testing.T) {
		restricted := data.NewService(a.driver).WithPeers(staticPeers{b.name: data.NewService(b.driver).
			WithReadAccess(func(_ context.Context, table string) (bool, error) { return table != "customers", nil })})
		row, err := restricted.GetRow(ctx, "orders", first)
		require.NoError(t, err, "the record is read all the same")
		assert.Equal(t, "100", text(row["amount"]))
		assert.Empty(t, text(row["customer_name"]))
		assert.Empty(t, text(row["credit_total"]))
		_, err = restricted.EvaluateExpression(ctx, "orders", first, "Sum ( order_customers::credit )", "Number")
		require.ErrorContains(t, err, "<No Access>")
	})

	t.Run("each solution is read with its own account's access", func(t *testing.T) {
		// B may read its customers, but the account the group holds in A
		// may not read A's orders: B's total of A's orders is <No Access>.
		peerOfB := data.NewService(a.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
			return table != "orders", nil
		})
		viaB := data.NewService(b.driver).WithPeers(staticPeers{a.name: peerOfB})
		row, err := viaB.GetRow(ctx, "customers", customers["Bob"])
		require.NoError(t, err)
		assert.Equal(t, "Bob (north)", text(row["label"]))
		assert.Empty(t, text(row["order_total"]))
		_, err = viaB.EvaluateExpression(ctx, "customers", customers["Bob"], "Sum ( customer_orders::amount )", "Number")
		require.ErrorContains(t, err, "<No Access>")
	})

	t.Run("without a session in the other solution the field is empty", func(t *testing.T) {
		for name, svc := range map[string]*data.Service{
			"not signed in": data.NewService(a.driver).WithPeers(staticPeers{}),
			"no peers":      data.NewService(a.driver),
		} {
			row, err := svc.GetRow(ctx, "orders", first)
			require.NoError(t, err, name)
			assert.Equal(t, "100", text(row["amount"]), name)
			assert.Empty(t, text(row["customer_name"]), name)
			assert.Empty(t, text(row["customer_count"]), name)
		}
		_, err := data.NewService(a.driver).WithPeers(staticPeers{}).
			EvaluateExpression(ctx, "orders", first, "order_customers::full_name", "Text")
		require.ErrorContains(t, err, "sign in to "+b.name)
	})
}

// A find on a related field of another solution's table (#107) finds what
// the same find on a related field of a table of this solution holding the
// same records finds: for every find operator, every relationship operator,
// several criteria, and an omitting request.
func TestExternalFind_FindsWhatALocalRelatedFindFinds(t *testing.T) {
	ctx, a, b := twoSolutions(t, "xfind_orders", "xfind_customers")

	customerFields := [][2]string{{"code", "TEXT"}, {"region", "TEXT"}, {"full_name", "TEXT"}, {"credit", "NUMBER"}}
	b.table(t, ctx, "customers", customerFields...)
	_, localOcc, localFields := a.table(t, ctx, "local_customers", customerFields...)
	ordersTable, ordersOcc, orderFields := a.table(t, ctx, "orders",
		[2]string{"customer_code", "TEXT"}, [2]string{"region", "TEXT"}, [2]string{"amount", "NUMBER"})
	ref, externalOcc := a.external(t, ctx, "Customers", "order_customers")

	type predicate struct{ order, op, customer string }
	relationships := map[string][]predicate{
		"equal":     {{"customer_code", "=", "code"}},
		"less":      {{"amount", "<", "credit"}},
		"at most":   {{"amount", "≤", "credit"}},
		"greater":   {{"amount", ">", "credit"}},
		"at least":  {{"amount", "≥", "credit"}},
		"not equal": {{"customer_code", "≠", "code"}},
		"cartesian": {{"customer_code", "×", "code"}},
		"two keys":  {{"customer_code", "=", "code"}, {"region", "=", "region"}},
		"mixed":     {{"customer_code", "=", "code"}, {"amount", "<", "credit"}},
	}
	type pair struct{ local, external string }
	rels := map[string]pair{}
	for name, preds := range relationships {
		var local, external []schema.RelationshipPredicate
		for _, p := range preds {
			local = append(local, schema.RelationshipPredicate{LeftColumnID: orderFields[p.order], Operator: p.op, RightColumnID: localFields[p.customer]})
			external = append(external, schema.RelationshipPredicate{LeftColumnID: orderFields[p.order], Operator: p.op, RightColumnName: p.customer})
		}
		slug := strings.ReplaceAll(name, " ", "_")
		rels[name] = pair{
			local: a.relate(t, ctx, schema.CreateRelationshipInput{Name: "local_" + slug, LeftOccurrenceID: ordersOcc,
				RightOccurrenceID: localOcc, Predicates: local}),
			external: a.relate(t, ctx, schema.CreateRelationshipInput{Name: "external_" + slug, LeftOccurrenceID: ordersOcc,
				RightOccurrenceID: externalOcc, Predicates: external}),
		}
	}
	a.bind(t, ctx, ref, b, map[string]string{"order_customers": "customers"})

	customersData := data.NewService(b.driver)
	ordersData := data.NewService(a.driver).WithPeers(staticPeers{b.name: customersData})
	for _, c := range []map[string]interface{}{
		{"code": "C-1", "region": "north", "full_name": "Ada", "credit": 10},
		{"code": "C-1", "region": "south", "full_name": "Ada South", "credit": 5},
		{"code": "C-2", "region": "north", "full_name": "Bob", "credit": 7},
		{"code": "C-3", "region": "east", "full_name": "", "credit": 3},
		{"code": "", "region": "west", "full_name": "Nobody", "credit": 50},
		{"code": "C-4", "region": "north", "full_name": "Cleo", "credit": nil},
	} {
		_, err := customersData.InsertRow(ctx, "customers", c)
		require.NoError(t, err)
		_, err = ordersData.InsertRow(ctx, "local_customers", c)
		require.NoError(t, err)
	}
	for _, o := range []map[string]interface{}{
		{"customer_code": "C-1", "region": "north", "amount": 4},
		{"customer_code": "C-1", "region": "south", "amount": 8},
		{"customer_code": "C-2", "region": "north", "amount": 7},
		{"customer_code": "C-2", "region": "south", "amount": 12},
		{"customer_code": "C-3", "region": "east", "amount": 1},
		{"customer_code": "C-4", "region": "north", "amount": 2},
		{"customer_code": "", "region": "north", "amount": 5},
		{"customer_code": "C-9", "region": "north", "amount": nil},
	} {
		_, err := ordersData.InsertRow(ctx, "orders", o)
		require.NoError(t, err)
	}

	criteria := []data.FindCriterion{
		{FieldName: "full_name", Value: "Ada"},   // contains
		{FieldName: "full_name", Value: "=Ada"},  // exact
		{FieldName: "full_name", Value: "==Bob"}, // strict
		{FieldName: "full_name", Value: "!Ada"},  // not equal
		{FieldName: "full_name", Value: "A*"},    // wildcard
		{FieldName: "full_name", Value: "*"},     // not empty
		{FieldName: "full_name", Value: "="},     // empty
		{FieldName: "full_name", Value: "B...C"}, // text range
		{FieldName: "full_name", Operator: "LIKE", Value: "%o%"},
		{FieldName: "credit", Value: ">6"},
		{FieldName: "credit", Value: "<6"},
		{FieldName: "credit", Value: ">=7"},
		{FieldName: "credit", Value: "<=7"},
		{FieldName: "credit", Value: "4...9"},
		{FieldName: "credit", Operator: "IS_EMPTY"},
		{FieldName: "credit", Operator: "IS_NOT_EMPTY"},
		{FieldName: "region", Operator: "=", Value: "NORTH"},
		{FieldName: "region", Operator: "!=", Value: "north"},
	}
	find := func(requests ...data.FindRequest) []string {
		rows, err := ordersData.ExecuteFind(ctx, "orders", requests, data.QueryOptions{})
		require.NoError(t, err)
		out := []string{}
		for _, r := range rows {
			out = append(out, fmt.Sprint(r["id"]))
		}
		sort.Strings(out)
		return out
	}
	through := func(rel string, crit data.FindCriterion) data.FindCriterion {
		crit.RelationshipID = rel
		return crit
	}
	found := 0
	for name, rel := range rels {
		for _, crit := range criteria {
			local := find(data.FindRequest{Criteria: []data.FindCriterion{through(rel.local, crit)}})
			external := find(data.FindRequest{Criteria: []data.FindCriterion{through(rel.external, crit)}})
			assert.Equal(t, local, external, "%s relationship, %s %s %v", name, crit.FieldName, crit.Operator, crit.Value)
			found += len(local)
		}
		// Two criteria on the related table and one of the orders' own,
		// less what an omitting request finds.
		requests := func(rel string) []data.FindRequest {
			return []data.FindRequest{
				{Criteria: []data.FindCriterion{
					through(rel, data.FindCriterion{FieldName: "credit", Value: ">4"}),
					through(rel, data.FindCriterion{FieldName: "region", Value: "north"}),
					{FieldName: "amount", Value: ">3"},
				}},
				{Criteria: []data.FindCriterion{through(rel, data.FindCriterion{FieldName: "full_name", Value: "Ada South"})}, Omit: true},
			}
		}
		assert.Equal(t, find(requests(rel.local)...), find(requests(rel.external)...), "%s relationship, several requests", name)
	}
	assert.Positive(t, found, "the finds find something")

	t.Run("a summary totals the same found set", func(t *testing.T) {
		encoded, err := data.SummarySpec{Type: data.SummaryTotal, Field: "amount"}.Encode()
		require.NoError(t, err)
		_, err = a.schema.AddColumn(ctx, ordersTable, schema.ColumnMetadata{Name: "amount_total", DisplayName: "amount_total",
			FieldType: dbal.FieldTypeSummary, IsNullable: true, CalculationFormula: &encoded})
		require.NoError(t, err)
		total := func(rel string) string {
			res, err := ordersData.Summarize(ctx, "orders", data.SummaryRequest{Requests: []data.FindRequest{
				{Criteria: []data.FindCriterion{{FieldName: "full_name", Value: "Ada", RelationshipID: rel}}}}})
			require.NoError(t, err)
			return fmt.Sprint(res.Grand["amount_total"])
		}
		assert.Equal(t, "12", total(rels["equal"].external), "the two C-1 orders")
		assert.Equal(t, total(rels["equal"].local), total(rels["equal"].external))
	})

	t.Run("without a session in the other solution the find is refused", func(t *testing.T) {
		_, err := data.NewService(a.driver).WithPeers(staticPeers{}).ExecuteFind(ctx, "orders", []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "full_name", Value: "Ada", RelationshipID: rels["equal"].external}}},
		}, data.QueryOptions{})
		var unavailable *data.ExternalUnavailableError
		require.ErrorAs(t, err, &unavailable)
		assert.Equal(t, data.ExternalNotSignedIn, unavailable.Reason)
	})

	t.Run("nor when the account there may not read the table", func(t *testing.T) {
		restricted := data.NewService(a.driver).WithPeers(staticPeers{b.name: data.NewService(b.driver).
			WithReadAccess(func(_ context.Context, table string) (bool, error) { return table != "customers", nil })})
		_, err := restricted.ExecuteFind(ctx, "orders", []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "full_name", Value: "Ada", RelationshipID: rels["equal"].external}}},
		}, data.QueryOptions{})
		var unavailable *data.ExternalUnavailableError
		require.ErrorAs(t, err, &unavailable)
		assert.Equal(t, data.ExternalNoAccess, unavailable.Reason)
	})

	t.Run("a field the other solution does not have is a schema mismatch", func(t *testing.T) {
		_, err := ordersData.ExecuteFind(ctx, "orders", []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "no_such_field", Value: "x", RelationshipID: rels["equal"].external}}},
		}, data.QueryOptions{})
		var unavailable *data.ExternalUnavailableError
		require.ErrorAs(t, err, &unavailable)
		assert.Equal(t, data.ExternalSchemaMismatch, unavailable.Reason)
	})

	t.Run("a find matching too many match values is refused", func(t *testing.T) {
		dialect := b.driver.Dialect()
		const batch = 1000
		for start := 0; start <= data.MaxExternalFindKeys; start += batch {
			var marks []string
			var args []interface{}
			for i := start; i < start+batch && i <= data.MaxExternalFindKeys; i++ {
				n := len(args)
				marks = append(marks, fmt.Sprintf("(%s, %s, %s)", dialect.Placeholder(n+1), dialect.Placeholder(n+2), dialect.Placeholder(n+3)))
				args = append(args, uuid.NewString(), fmt.Sprintf("BULK-%05d", i), "Bulk customer")
			}
			_, err := b.driver.DB().ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES %s",
				dialect.QuoteIdentifier("customers"), dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier("code"),
				dialect.QuoteIdentifier("full_name"), strings.Join(marks, ", ")), args...)
			require.NoError(t, err)
		}
		_, err := ordersData.ExecuteFind(ctx, "orders", []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "full_name", Value: "Bulk", RelationshipID: rels["equal"].external}}},
		}, data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrFindTooBroad)
		// A bound needs no list of keys, however many records match.
		_, err = ordersData.ExecuteFind(ctx, "orders", []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "full_name", Value: "Bulk", RelationshipID: rels["less"].external}}},
		}, data.QueryOptions{})
		require.NoError(t, err)
	})
}
