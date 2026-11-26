package data_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// identityTable is a table of synthetic visits whose records' identities a
// test follows: a stored field, a global, and formulas reading the record's
// identity, stored, unstored and auto-entered.
type identityTable struct {
	svc     *data.Service
	name    string
	tableID string
}

func newIdentityTable(t *testing.T, ctx context.Context, s *solution, name string) *identityTable {
	t.Helper()
	tableID, _, _ := s.table(t, ctx, name, [2]string{"label", "TEXT"}, [2]string{"code", "TEXT"})
	notEmpty := `{"not_empty":true,"not_empty_timing":"always"}`
	for _, col := range []schema.ColumnMetadata{
		{Name: "shared", FieldType: dbal.FieldTypeText, DefaultValue: options(map[string]any{"storage_global": true})},
		{Name: "stored_id", FieldType: dbal.FieldTypeCalculation, CalculationFormula: calcJSON("Get ( RecordID ) & \"|\" & label", "TEXT")},
		{Name: "live_count", FieldType: dbal.FieldTypeCalculation, CalculationFormula: unstoredCalcJSON("Get ( RecordModificationCount )", "NUMBER")},
		{Name: "entered_id", FieldType: dbal.FieldTypeNumber, DefaultValue: options(map[string]any{
			"calculated_enabled": true, "calculated_formula": "Get ( RecordID )"})},
		{Name: "required", FieldType: dbal.FieldTypeText, ValidationRules: &notEmpty},
	} {
		col.DisplayName, col.IsNullable = col.Name, true
		_, err := s.schema.AddColumn(ctx, tableID, col)
		require.NoError(t, err)
	}
	return &identityTable{svc: data.NewService(s.driver), name: name, tableID: tableID}
}

func (tb *identityTable) insert(t *testing.T, ctx context.Context, label string) string {
	t.Helper()
	row, err := tb.svc.InsertRow(ctx, tb.name, map[string]interface{}{"label": label, "required": "yes"})
	require.NoError(t, err)
	return fmt.Sprint(row["id"])
}

func (tb *identityTable) identity(t *testing.T, ctx context.Context, id string) data.Identity {
	t.Helper()
	identity, err := tb.svc.RecordIdentity(ctx, tb.name, id)
	require.NoError(t, err)
	return identity
}

func (tb *identityTable) update(t *testing.T, ctx context.Context, id string, updates map[string]interface{}) {
	t.Helper()
	_, err := tb.svc.UpdateRow(ctx, tb.name, id, updates)
	require.NoError(t, err)
}

// A record ID is given in creation order, once: a create that fails spends
// one, a deleted record's is not given again, and a duplicate is a new
// record. Formulas read it, from the moment the record is created.
func TestRecordIdentity_RecordIDs(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "rid", "rid_unused")
	tb := newIdentityTable(t, ctx, a, "visits")

	first := tb.insert(t, ctx, "first")
	second := tb.insert(t, ctx, "second")
	assert.Equal(t, data.Identity{RecordID: 1, ModID: 0}, tb.identity(t, ctx, first))
	assert.Equal(t, data.Identity{RecordID: 2, ModID: 0}, tb.identity(t, ctx, second))

	row, err := tb.svc.GetRow(ctx, tb.name, second)
	require.NoError(t, err)
	assert.Equal(t, "2|second", fmt.Sprint(row["stored_id"]), "a stored calculation knows the new record's ID")
	assert.Equal(t, "2", fmt.Sprint(row["entered_id"]), "so does an auto-enter calculation")
	assert.Equal(t, "0", fmt.Sprint(row["live_count"]))
	assert.NotContains(t, row, dbal.CreationOrderColumn)
	assert.NotContains(t, row, dbal.ModCountColumn)

	// Failed creates spend their IDs, as FileMaker's do: a value a rule
	// refuses (509), a field the table does not have (102).
	_, err = tb.svc.InsertRow(ctx, tb.name, map[string]interface{}{"label": "refused"})
	require.Error(t, err)
	_, err = tb.svc.InsertRow(ctx, tb.name, map[string]interface{}{"label": "x", "required": "yes", "missing_field": "x"})
	require.ErrorIs(t, err, data.ErrUnknownField)
	third := tb.insert(t, ctx, "third")
	assert.Equal(t, int64(5), tb.identity(t, ctx, third).RecordID)

	// The last record deleted, its ID is not given again.
	require.NoError(t, tb.svc.DeleteRow(ctx, tb.name, third))
	fourth := tb.insert(t, ctx, "fourth")
	assert.Equal(t, int64(6), tb.identity(t, ctx, fourth).RecordID)

	tb.update(t, ctx, first, map[string]interface{}{"label": "first, edited"})
	copied, err := tb.svc.DuplicateRow(ctx, tb.name, first)
	require.NoError(t, err)
	assert.Equal(t, data.Identity{RecordID: 7, ModID: 0}, tb.identity(t, ctx, fmt.Sprint(copied["id"])),
		"a duplicate is a new record, never modified")
	assert.Equal(t, "7|first, edited", fmt.Sprint(copied["stored_id"]))

	id, err := tb.svc.FindByRecordID(ctx, tb.name, 2)
	require.NoError(t, err)
	assert.Equal(t, second, id)
	_, err = tb.svc.FindByRecordID(ctx, tb.name, 5)
	assert.ErrorIs(t, err, data.ErrRecordNotFound, "a deleted record's ID finds nothing")

	got, err := tb.svc.EvaluateExpression(ctx, tb.name, second, "Get ( RecordID ) & \"/\" & Get ( RecordModificationCount )", "TEXT")
	require.NoError(t, err)
	assert.Equal(t, "2/0", got.Text)
	got, err = tb.svc.EvaluateExpression(ctx, tb.name, "", "Get ( RecordID )", "TEXT")
	require.NoError(t, err)
	assert.Equal(t, "", got.Text, "no record, no record ID")

	// An import numbers the records it adds after the others; the records
	// a failed import would have added spend theirs.
	source := &dataio.Table{Columns: []string{"label", "required"}, Rows: [][]string{{"i1", "yes"}, {"i2", "yes"}}}
	opts := data.ImportOptions{Action: data.ImportAdd, Mappings: []data.ImportMapping{{Column: 0, Field: "label"}, {Column: 1, Field: "required"}}}
	_, err = tb.svc.ImportRecords(ctx, tb.name, source, opts)
	require.NoError(t, err)
	rows, err := tb.svc.ListRows(ctx, tb.name, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 6)
	assert.Equal(t, "8|i1", fmt.Sprint(rows[4]["stored_id"]))
	assert.Equal(t, "9|i2", fmt.Sprint(rows[5]["stored_id"]))
}

// The modification count goes up once with every write that changes a
// field stored with the record, and only then.
func TestRecordIdentity_ModificationCount(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "mod", "mod_unused")
	tb := newIdentityTable(t, ctx, a, "visits")
	id := tb.insert(t, ctx, "first")
	count := func() int64 { return tb.identity(t, ctx, id).ModID }

	tb.update(t, ctx, id, map[string]interface{}{"label": "second"})
	assert.Equal(t, int64(1), count())
	tb.update(t, ctx, id, map[string]interface{}{"label": "third", "code": "C-1"})
	assert.Equal(t, int64(2), count(), "one write, one modification")
	tb.update(t, ctx, id, map[string]interface{}{"label": "third"})
	assert.Equal(t, int64(2), count(), "a value sent back as it is changes nothing")
	tb.update(t, ctx, id, map[string]interface{}{})
	assert.Equal(t, int64(2), count(), "nothing to write")
	tb.update(t, ctx, id, map[string]interface{}{"shared": "everyone"})
	assert.Equal(t, int64(2), count(), "a global field is not the record's")
	tb.update(t, ctx, id, map[string]interface{}{"stored_id": "typed over"})
	assert.Equal(t, int64(2), count(), "a calculation is its formula's")
	_, err := tb.svc.RecalculateTable(ctx, tb.name)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count(), "a recalculation is no modification")

	row, err := tb.svc.GetRow(ctx, tb.name, id)
	require.NoError(t, err)
	assert.Equal(t, "2", fmt.Sprint(row["live_count"]))

	// An import that updates the record modifies it once; one that sends
	// what it holds does not.
	source := &dataio.Table{Columns: []string{"code", "label", "required"}, Rows: [][]string{{"C-1", "fourth", "yes"}}}
	opts := data.ImportOptions{Action: data.ImportUpdateMatching, MatchFields: []string{"code"},
		Mappings: []data.ImportMapping{{Column: 0, Field: "code"}, {Column: 1, Field: "label"}, {Column: 2, Field: "required"}}}
	_, err = tb.svc.ImportRecords(ctx, tb.name, source, opts)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count())
	_, err = tb.svc.ImportRecords(ctx, tb.name, source, opts)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count())
}

// A write conditioned on a modification count is refused when the record
// has another, as FileMaker's 306, and writes nothing.
func TestRecordIdentity_UpdateIfModID(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "modif", "modif_unused")
	tb := newIdentityTable(t, ctx, a, "visits")
	id := tb.insert(t, ctx, "first")

	updated, err := tb.svc.UpdateRowIfModID(ctx, tb.name, id, 0, map[string]interface{}{"label": "second"})
	require.NoError(t, err)
	assert.Equal(t, "second", fmt.Sprint(updated["label"]))
	assert.Equal(t, int64(1), tb.identity(t, ctx, id).ModID)

	for _, stale := range []int64{0, 2, 99} {
		_, err = tb.svc.UpdateRowIfModID(ctx, tb.name, id, stale, map[string]interface{}{"label": "lost"})
		assert.ErrorIs(t, err, data.ErrModIDMismatch, "modId %d", stale)
	}
	row, err := tb.svc.GetRow(ctx, tb.name, id)
	require.NoError(t, err)
	assert.Equal(t, "second", fmt.Sprint(row["label"]), "a refused write writes nothing")
	assert.Equal(t, int64(1), tb.identity(t, ctx, id).ModID)

	_, err = tb.svc.UpdateRowIfModID(ctx, tb.name, id, 1, map[string]interface{}{"label": "third"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), tb.identity(t, ctx, id).ModID)

	_, err = tb.svc.UpdateRowIfModID(ctx, tb.name, "00000000-0000-4000-8000-000000000000", 0, map[string]interface{}{"label": "x"})
	assert.ErrorIs(t, err, data.ErrRecordNotFound)
}

// A restore keeps the record IDs and modification counts its records carry,
// into a table that never gave them; restored into the table it came from,
// it gives its records new IDs, as an ID is never given twice.
func TestRecordIdentity_RestoreKeepsIdentities(t *testing.T) {
	ctx, a, b := twoSolutions(t, "ridsrc", "riddst")
	src := newIdentityTable(t, ctx, a, "visits")
	var ids []string
	for i := range 4 {
		ids = append(ids, src.insert(t, ctx, fmt.Sprintf("v%d", i)))
	}
	require.NoError(t, src.svc.DeleteRow(ctx, src.name, ids[1]))
	src.update(t, ctx, ids[2], map[string]interface{}{"label": "edited"})
	src.update(t, ctx, ids[2], map[string]interface{}{"label": "edited twice"})

	file, err := a.schema.ExportDatabaseData(ctx, a.name)
	require.NoError(t, err)
	var bundle schema.DatabaseDataBundle
	require.NoError(t, msgpack.Unmarshal(file, &bundle))
	require.Len(t, bundle.TablesData["visits"], 3)
	assert.Contains(t, bundle.TablesData["visits"][0], schema.RecordIdentityKey)
	assert.NotContains(t, bundle.TablesData["visits"][0], dbal.CreationOrderColumn, "the identity is not a field")

	dst := newIdentityTable(t, ctx, b, "visits")
	report, err := b.schema.ImportDatabaseData(ctx, file)
	require.NoError(t, err)
	assert.Equal(t, 3, report.RecordsInserted)
	assert.Equal(t, 0, report.RecordIDsRenumbered)
	want := map[string]data.Identity{ids[0]: {RecordID: 1}, ids[2]: {RecordID: 3, ModID: 2}, ids[3]: {RecordID: 4}}
	got, err := dst.svc.RecordIdentities(ctx, dst.name, []string{ids[0], ids[2], ids[3]})
	require.NoError(t, err)
	assert.Equal(t, want, got)
	next := dst.insert(t, ctx, "after the restore")
	assert.Equal(t, int64(5), dst.identity(t, ctx, next).RecordID, "a new record comes after the restored ones")

	// Into the table it came from: the records still there are skipped, the
	// deleted one would be restored under a new ID, as its own was spent.
	require.NoError(t, src.svc.DeleteRow(ctx, src.name, ids[3]))
	report, err = a.schema.ImportDatabaseData(ctx, file)
	require.NoError(t, err)
	assert.Equal(t, 1, report.RecordsInserted)
	assert.Equal(t, 2, report.RecordsSkipped)
	assert.Equal(t, 3, report.RecordIDsRenumbered)
	restored := src.identity(t, ctx, ids[3])
	assert.Greater(t, restored.RecordID, int64(4))
	assert.Equal(t, int64(0), restored.ModID)
	assert.Equal(t, data.Identity{RecordID: 3, ModID: 2}, src.identity(t, ctx, ids[2]), "a record skipped keeps its own")
}

// A table older than the modification count is given it at sign-in, every
// record counting none; until then it is written without one.
func TestRecordIdentity_ExistingTableIsUpgraded(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "modup", "modup_unused")
	tb := newIdentityTable(t, ctx, a, "visits")
	id := tb.insert(t, ctx, "first")
	tb.update(t, ctx, id, map[string]interface{}{"label": "second"})
	dialect := a.driver.Dialect()
	_, err := a.driver.DB().ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
		dialect.QuoteIdentifier("visits"), dialect.QuoteIdentifier(dbal.ModCountColumn)))
	require.NoError(t, err)
	data.ForgetCreationOrder(a.driver.DB())

	tb.update(t, ctx, id, map[string]interface{}{"label": "third"})
	_, err = tb.svc.UpdateRowIfModID(ctx, tb.name, id, 0, map[string]interface{}{"label": "fourth"})
	require.NoError(t, err, "a table that counts nothing has every record at 0")
	second := tb.insert(t, ctx, "another")
	assert.Equal(t, data.Identity{RecordID: 2}, tb.identity(t, ctx, second))

	require.NoError(t, a.schema.EnsureSystemTables(ctx))
	has, err := data.HasModCount(ctx, a.driver, "visits")
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, data.Identity{RecordID: 1}, tb.identity(t, ctx, id))
	tb.update(t, ctx, id, map[string]interface{}{"label": "fifth"})
	assert.Equal(t, int64(1), tb.identity(t, ctx, id).ModID)
	require.NoError(t, a.schema.EnsureSystemTables(ctx), "an upgraded catalog is only read")
}
