package data_test

import (
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// On SQLite, a list sorted by a text field that has its sort index reads
// the index, and gives the same records in the same order as the collation
// does without it, on every page, ascending or descending, whatever the
// other sort fields; once a change of the field drops the index, the list
// is sorted without it again.
func TestSortIndex_ListsTheSameRecordsInTheSameOrder(t *testing.T) {
	if testdb.Engine() != dbal.EngineSQLite {
		t.Skip("sort indexes are SQLite's (docs/specs/sqlite_engine.md)")
	}
	ctx, a, _ := twoSolutions(t, "xsortindex", "xsortindex_unused")
	a.table(t, ctx, "words", [2]string{"label", "TEXT"}, [2]string{"other", "TEXT"})
	svc := data.NewService(a.driver)
	texts := []string{"", "a", "A", "á", "Á", "b", "B", "é", "e", "E", "z", "œ", "oe", "ß", "ss", "straße", "Straße", "日本"}
	for i := range 120 {
		row := map[string]interface{}{"label": texts[i%len(texts)], "other": texts[(i*7)%len(texts)]}
		if i%13 == 0 {
			row["label"] = nil
		}
		_, err := svc.InsertRow(ctx, "words", row)
		require.NoError(t, err)
	}
	orders := [][]data.SortField{
		{{Field: "label"}},
		{{Field: "label", Descending: true}},
		{{Field: "label"}, {Field: "other", Descending: true}},
		{{Field: "label", Descending: true}, {Field: "other"}},
	}
	list := func(order []data.SortField) []string {
		var ids []string
		for offset := 0; ; offset += 25 {
			rows, err := svc.ListRows(ctx, "words", data.QueryOptions{Sort: order, Limit: 25, Offset: offset})
			require.NoError(t, err)
			for _, r := range rows {
				ids = append(ids, fmt.Sprint(r["id"]))
			}
			if len(rows) < 25 {
				return ids
			}
		}
	}
	var before [][]string
	for _, order := range orders {
		before = append(before, list(order))
	}
	_, err := a.driver.DB().ExecContext(ctx, `CREATE INDEX words_label_key ON "words" (`+dbal.SQLiteSortKeySQL(`"label"`)+`)`)
	require.NoError(t, err)
	require.NotEmpty(t, dbal.SQLiteSortIndex(ctx, a.driver.DB(), "words", "label"))
	for i, order := range orders {
		assert.Equal(t, before[i], list(order), "order %v through the index", order)
	}

	_, err = a.driver.DB().ExecContext(ctx, `DROP INDEX words_label_key`)
	require.NoError(t, err)
	assert.Equal(t, before[0], list(orders[0]), "without the index again")
}
