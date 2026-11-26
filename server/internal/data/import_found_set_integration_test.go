package data_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FileMaker's Import Records methods that work on the found set (#191):
// replacing it in order, updating only the matching records it holds, and
// performing auto-enter field by field.
func TestImportFoundSet(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	columns := []autoColumn{
		{name: "note", fieldType: dbal.FieldTypeText},
		{name: "status", fieldType: dbal.FieldTypeText, options: map[string]any{"data_enabled": true, "data_value": "New"}},
		{name: "modified_by", fieldType: dbal.FieldTypeText, options: map[string]any{"modification_enabled": true, "modification_type": "Account Name"}},
	}
	notes := []data.ImportMapping{{Column: 0, Field: "note"}}

	// seed adds records in the order given, and answers their ids in it.
	seed := func(t *testing.T, f *autoFixture, values ...string) []string {
		t.Helper()
		csv := "Note\n"
		for _, v := range values {
			csv += v + "\n"
		}
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, csv), data.ImportOptions{
			Action: data.ImportAdd, Mappings: notes, ReturnRecordIDs: true,
		})
		require.NoError(t, err)
		require.Len(t, report.RecordIDs, len(values))
		return report.RecordIDs
	}
	noteOf := func(t *testing.T, f *autoFixture, id string) string {
		t.Helper()
		row, err := f.dataSvc.GetRow(ctx, f.orders, id)
		require.NoError(t, err)
		return text(row["note"])
	}

	t.Run("replace pairs the n-th row with the n-th record of the found set", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		ids := seed(t, f, "a", "b", "c")
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nfirst\nsecond\n"), data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes,
			FoundSet: []string{ids[2], ids[0]}, ReturnRecordIDs: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 2, report.Updated)
		assert.Equal(t, []string{ids[2], ids[0]}, report.RecordIDs, "the records written, in found set order")
		assert.Equal(t, "first", noteOf(t, f, ids[2]))
		assert.Equal(t, "second", noteOf(t, f, ids[0]))
		assert.Equal(t, "b", noteOf(t, f, ids[1]), "a record outside the found set is left alone")
	})

	t.Run("replace with no found set is refused, never the whole table", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		ids := seed(t, f, "a", "b", "c")
		_, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nx\ny\n"), data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes,
		})
		require.ErrorIs(t, err, data.ErrImport)
		assert.Equal(t, "a", noteOf(t, f, ids[0]))

		_, err = f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nx\ny\n"), data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes, FoundSet: []string{ids[0], ids[1], ids[2]},
		})
		require.NoError(t, err)
		assert.Equal(t, "x", noteOf(t, f, ids[0]))
		assert.Equal(t, "y", noteOf(t, f, ids[1]))
		assert.Equal(t, "c", noteOf(t, f, ids[2]), "a record left over keeps what it holds")
	})

	t.Run("rows left once the found set runs out are added or skipped", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		ids := seed(t, f, "a", "b")
		source := parseCSV(t, "Note\nr1\nr2\nr3\n")
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, source, data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes, FoundSet: []string{ids[1]},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 2, report.Skipped)
		assert.Equal(t, "r1", noteOf(t, f, ids[1]))

		report, err = f.dataSvc.ImportRecords(ctx, f.orders, source, data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes, FoundSet: []string{ids[0]}, AddUnmatched: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 2, report.Added)
	})

	t.Run("a row passed over keeps its record's place", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		ids := seed(t, f, "a", "b")
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nbad\ngood\n"), data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes, FoundSet: ids, PassOver: map[int]bool{0: true},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, "a", noteOf(t, f, ids[0]))
		assert.Equal(t, "good", noteOf(t, f, ids[1]))
	})

	t.Run("replace matches on nothing", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		_, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nx\n"), data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes, MatchFields: []string{"note"}, FoundSet: []string{},
		})
		require.ErrorIs(t, err, data.ErrImport)
	})

	t.Run("update in a found set updates every match it holds, and only those", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		ids := seed(t, f, "dup", "dup", "dup")
		source := parseCSV(t, "Note,Status\ndup,Seen\n")
		mappings := []data.ImportMapping{{Column: 0, Field: "note"}, {Column: 1, Field: "status"}}
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, source, data.ImportOptions{
			Action: data.ImportUpdateMatching, Mappings: mappings, MatchFields: []string{"note"},
			FoundSet: []string{ids[2], ids[0]}, ReturnRecordIDs: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 2, report.Updated, "two records updated")
		assert.Equal(t, []string{ids[2], ids[0]}, report.RecordIDs, "in found set order")
		for i, want := range []string{"Seen", "", "Seen"} {
			row, err := f.dataSvc.GetRow(ctx, f.orders, ids[i])
			require.NoError(t, err)
			assert.Equal(t, want, text(row["status"]), "record %d", i+1)
		}
	})

	t.Run("a record of the found set deleted since keeps its place", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		ids := seed(t, f, "a", "b")
		require.NoError(t, f.dataSvc.DeleteRow(ctx, f.orders, ids[0]))
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nfor a\nfor b\n"), data.ImportOptions{
			Action: data.ImportReplaceFoundSet, Mappings: notes, FoundSet: []string{ids[0], ids[1]},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 1, report.Skipped)
		assert.Equal(t, "for b", noteOf(t, f, ids[1]), "b is not given a's row")
	})

	t.Run("a row whose match fields are all empty is neither matched nor added", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		seed(t, f, "a")
		mappings := []data.ImportMapping{{Column: 0, Field: "note"}, {Column: 1, Field: "status"}}
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note,Status\n,Seen\nz,Seen\n"), data.ImportOptions{
			Action: data.ImportUpdateMatching, Mappings: mappings, MatchFields: []string{"note"}, AddUnmatched: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Skipped)
		assert.Equal(t, 1, report.Added)
	})

	t.Run("a number key matches the number the column holds", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, autoColumn{name: "qty", fieldType: dbal.FieldTypeNumber},
			autoColumn{name: "note", fieldType: dbal.FieldTypeText})
		qty := []data.ImportMapping{{Column: 0, Field: "qty"}, {Column: 1, Field: "note"}}
		_, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Qty,Note\n7,first\n"), data.ImportOptions{
			Action: data.ImportAdd, Mappings: qty,
		})
		require.NoError(t, err)
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Qty,Note\n7.0,second\n"), data.ImportOptions{
			Action: data.ImportUpdateMatching, Mappings: qty, MatchFields: []string{"qty"}, AddUnmatched: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 0, report.Added, "7.0 is the 7 the record holds")
	})

	t.Run("auto-enter is performed only on the fields chosen", func(t *testing.T) {
		f := newAutoFixture(t, ctx, driver, columns...)
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, parseCSV(t, "Note\nx\n"), data.ImportOptions{
			Action: data.ImportAdd, Mappings: notes, PerformAutoEnter: true,
			AutoEnterFields: []string{"modified_by"}, ReturnRecordIDs: true,
		})
		require.NoError(t, err)
		row, err := f.dataSvc.GetRow(ctx, f.orders, report.RecordIDs[0])
		require.NoError(t, err)
		assert.Equal(t, "synthetic.clerk", text(row["modified_by"]))
		assert.Equal(t, "", text(row["status"]), "a field not chosen enters nothing")
	})
}
