package data_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/require"
)

// An equality find on a Number field compares values, not the text the engine
// writes them as: MariaDB's DECIMAL(65, 10) reads 12 back as 12.0000000000,
// so "= 12" found nothing there, and a value stored as 12.0 was missed on
// PostgreSQL.
func TestNumericEqualityFindOnEveryEngine(t *testing.T) {
	ctx, schemaSvc, svc, _ := newTemporalSolution(t)
	tables, err := schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	var visitsID string
	for _, tbl := range tables {
		if tbl.Name == "visits" {
			visitsID = tbl.ID
		}
	}
	require.NotEmpty(t, visitsID)
	_, err = schemaSvc.AddColumn(ctx, visitsID, schema.ColumnMetadata{
		Name: "amount", DisplayName: "amount", FieldType: dbal.FieldTypeNumber, IsNullable: true,
	})
	require.NoError(t, err)
	for _, r := range []map[string]interface{}{
		{"name": "a", "amount": 12},
		{"name": "b", "amount": "12.0"},
		{"name": "c", "amount": 12.5},
		{"name": "d", "amount": -3},
		{"name": "e"},
	} {
		_, err := svc.InsertRow(ctx, "visits", r)
		require.NoError(t, err)
	}
	find := func(op string, value interface{}) string {
		rows, err := svc.ExecuteFind(ctx, "visits", []data.FindRequest{{
			Criteria: []data.FindCriterion{{FieldName: "amount", Operator: op, Value: value}},
		}}, data.QueryOptions{})
		require.NoError(t, err)
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, fmt.Sprint(row["name"]))
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	for _, c := range []struct {
		op    string
		value interface{}
		want  string
	}{
		{"=", 12, "a,b"},
		{"=", "12", "a,b"},
		{"==", "12.00", "a,b"},
		{"=", 12.5, "c"},
		{"=", "-3", "d"},
		{"!=", 12, "c,d,e"},
		{"=", 7, ""},
	} {
		require.Equal(t, c.want, find(c.op, c.value), "amount %s %v", c.op, c.value)
	}
}
