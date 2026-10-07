package data_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
)

// Several find requests against a real engine (#34): including requests are
// unioned, omitting requests subtract, and the result comes back in the order
// the caller asked for (#35).
func TestExecuteFind_MultipleRequests(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
		DSN:        testdb.DevDSN(),
	})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	dataSvc := data.NewService(driver)

	name := fmt.Sprintf("find_requests_%d", time.Now().UnixNano()%1000000)
	tbl, err := schemaSvc.CreateTable(ctx, "Find Requests", name)
	require.NoError(t, err)
	defer func() { _ = schemaSvc.DeleteTable(ctx, tbl.ID) }()

	for _, col := range []struct{ field, label string }{
		{"last_name", "Last Name"},
		{"city", "City"},
		{"customer_type", "Customer Type"},
	} {
		_, err = schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name:        col.field,
			DisplayName: col.label,
			FieldType:   dbal.FieldTypeText,
			IsNullable:  true,
		})
		require.NoError(t, err)
	}

	rows := []map[string]interface{}{
		{"last_name": "Smith", "city": "New York", "customer_type": "Continuing"},
		{"last_name": "Lee", "city": "London", "customer_type": "Continuing"},
		{"last_name": "Johnson", "city": "London", "customer_type": "New"},
		{"last_name": "Cannon", "city": "Paris", "customer_type": "New"},
		{"last_name": "Alvarez", "city": "New York", "customer_type": "Continuing"},
	}
	for _, row := range rows {
		_, err = dataSvc.InsertRow(ctx, name, row)
		require.NoError(t, err)
	}

	eq := func(field, value string) data.FindCriterion {
		return data.FindCriterion{FieldName: field, Operator: "=", Value: value}
	}
	lastNames := func(found []map[string]interface{}) []string {
		out := make([]string, 0, len(found))
		for _, r := range found {
			out = append(out, fmt.Sprintf("%v", r["last_name"]))
		}
		return out
	}
	byLastName := data.QueryOptions{Sort: []data.SortField{{Field: "last_name"}}}

	t.Run("one request matches on all its criteria", func(t *testing.T) {
		found, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{eq("city", "New York"), eq("customer_type", "Continuing")}},
		}, byLastName)
		require.NoError(t, err)
		require.Equal(t, []string{"Alvarez", "Smith"}, lastNames(found))
	})

	t.Run("two requests are unioned", func(t *testing.T) {
		found, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{eq("city", "New York")}},
			{Criteria: []data.FindCriterion{eq("city", "London")}},
		}, byLastName)
		require.NoError(t, err)
		require.Equal(t, []string{"Alvarez", "Johnson", "Lee", "Smith"}, lastNames(found))
	})

	t.Run("an omitting request subtracts from what the others found", func(t *testing.T) {
		// "In New York or London, except the new customers". OR-ing the
		// omitting request in used to return every record instead.
		found, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{eq("city", "New York")}},
			{Criteria: []data.FindCriterion{eq("city", "London")}},
			{Criteria: []data.FindCriterion{eq("customer_type", "New")}, Omit: true},
		}, byLastName)
		require.NoError(t, err)
		require.Equal(t, []string{"Alvarez", "Lee", "Smith"}, lastNames(found))
	})

	t.Run("only omitting requests start from every record", func(t *testing.T) {
		found, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{eq("city", "New York")}, Omit: true},
		}, byLastName)
		require.NoError(t, err)
		require.Equal(t, []string{"Cannon", "Johnson", "Lee"}, lastNames(found))
	})

	t.Run("the found set honours a multi-field sort order", func(t *testing.T) {
		found, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{eq("customer_type", "Continuing")}},
		}, data.QueryOptions{Sort: []data.SortField{
			{Field: "city"},
			{Field: "last_name", Descending: true},
		}})
		require.NoError(t, err)
		// London before New York; inside New York, Smith before Alvarez.
		require.Equal(t, []string{"Lee", "Smith", "Alvarez"}, lastNames(found))
	})

	t.Run("a sort field the table does not have is rejected", func(t *testing.T) {
		_, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{eq("city", "London")}},
		}, data.QueryOptions{Sort: []data.SortField{{Field: "salary"}}})
		require.ErrorIs(t, err, data.ErrUnknownField)
	})
}
