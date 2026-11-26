package data_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
)

// A small number and a Boolean are found and matched by value on every
// engine. SQLite writes 0.000001 as "1.0e-06", which no longer read as a
// number once cast to text, and holds a Boolean as 0 or 1, which as text is
// never "true".
func TestNumbersAndBooleansAreFoundByValue(t *testing.T) {
	ctx, schemaSvc, svc, _ := newTemporalSolution(t)
	tables, err := schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	var visits string
	for _, tbl := range tables {
		if tbl.Name == "visits" {
			visits = tbl.ID
		}
	}
	require.NotEmpty(t, visits)
	for _, col := range []schema.ColumnMetadata{
		{Name: "amount", FieldType: dbal.FieldTypeNumber},
		{Name: "active", FieldType: dbal.FieldTypeBoolean},
	} {
		col.DisplayName, col.IsNullable = col.Name, true
		_, err := schemaSvc.AddColumn(ctx, visits, col)
		require.NoError(t, err)
	}
	for _, r := range []map[string]interface{}{
		{"name": "tiny", "amount": 0.000001, "active": true},
		{"name": "zero", "amount": 0, "active": false},
		{"name": "big", "amount": 12.5},
	} {
		_, err := svc.InsertRow(ctx, "visits", r)
		require.NoError(t, err)
	}
	find := func(field, op, value string) string {
		t.Helper()
		rows, err := svc.ExecuteFind(ctx, "visits", []data.FindRequest{{Criteria: []data.FindCriterion{
			{FieldName: field, Operator: op, Value: value},
		}}}, data.QueryOptions{})
		require.NoError(t, err)
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, fmt.Sprint(row["name"]))
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	require.Equal(t, "big,tiny", find("amount", ">", "0"))
	require.Equal(t, "tiny,zero", find("amount", "<=", "0.000001"))
	if testdb.Engine() != dbal.EngineMariaDB { // MariaDB's TINYINT reads "1" (docs/specs/sqlite_engine.md)
		require.Equal(t, "tiny", find("active", "==", "true"))
		require.Equal(t, "zero", find("active", "==", "false"))
	}

	report, err := svc.ImportRecords(ctx, "visits", &dataio.Table{
		Columns: []string{"active", "name"},
		Rows:    [][]string{{"true", "matched"}},
	}, data.ImportOptions{
		Action:       data.ImportUpdateMatching,
		Mappings:     []data.ImportMapping{{Column: 0, Field: "active"}, {Column: 1, Field: "name"}},
		MatchFields:  []string{"active"},
		AddUnmatched: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.Updated, "the true record is matched, not added again: %+v", report)
	require.Equal(t, 0, report.Added)
}
