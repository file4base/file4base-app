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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// labelsOf is the label of each record, in order.
func labelsOf(rows []map[string]interface{}) []string {
	out := []string{}
	for _, row := range rows {
		out = append(out, fmt.Sprint(row["label"]))
	}
	return out
}

// keyedPair is a solution whose "parents" are related to "children" in the
// same solution and to "children" of another one (through the occurrence
// "ext_children") by key = key, and through other occurrences of the same
// two tables ("children_after", "ext_children_after") by key < key. Both
// keys are text, so either side's may hold several lines.
type keyedPair struct {
	a, b                    *solution
	parents                 *data.Service
	equal, less             map[string]string // relationship ids, by "local" and "external"
	parentsTable, childrenB string            // table ids
	childrenA               string
	occurrences             map[string]string // ids, by name
}

func newKeyedPair(t *testing.T, sortRelated string) (context.Context, *keyedPair) {
	t.Helper()
	ctx, a, b := twoSolutions(t, "xkey_parents", "xkey_children")
	p := &keyedPair{a: a, b: b, equal: map[string]string{}, less: map[string]string{}, occurrences: map[string]string{}}
	var childrenOcc string
	p.childrenB, _, _ = b.table(t, ctx, "children", [2]string{"key", "TEXT"}, [2]string{"label", "TEXT"}, [2]string{"rank", "NUMBER"})
	var parentsOcc string
	var parentFields map[string]string
	p.parentsTable, parentsOcc, parentFields = a.table(t, ctx, "parents", [2]string{"key", "TEXT"}, [2]string{"label", "TEXT"})
	var childFields map[string]string
	p.childrenA, childrenOcc, childFields = a.table(t, ctx, "children", [2]string{"key", "TEXT"}, [2]string{"label", "TEXT"},
		[2]string{"rank", "NUMBER"})
	ref, extChildren := a.external(t, ctx, "Children", "ext_children")
	_, extChildrenAfter := a.external(t, ctx, "Children", "ext_children_after")
	childrenAfter, err := a.schema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{BaseTableID: p.childrenA, Name: "children_after"})
	require.NoError(t, err)
	var sorted *string
	if sortRelated != "" {
		sorted = &sortRelated
	}
	for _, op := range []string{"=", "<"} {
		ids, local, external, suffix := p.equal, childrenOcc, extChildren, ""
		if op == "<" {
			ids, local, external, suffix = p.less, childrenAfter.ID, extChildrenAfter, "_after"
		}
		ids["local"] = a.relate(t, ctx, schema.CreateRelationshipInput{Name: "parents_children" + suffix,
			LeftOccurrenceID: parentsOcc, RightOccurrenceID: local, CascadeDelete: op == "=", SortRelated: sorted,
			Predicates: []schema.RelationshipPredicate{{LeftColumnID: parentFields["key"], Operator: op, RightColumnID: childFields["key"]}}})
		ids["external"] = a.relate(t, ctx, schema.CreateRelationshipInput{Name: "parents_ext_children" + suffix,
			LeftOccurrenceID: parentsOcc, RightOccurrenceID: external, SortRelated: sorted,
			Predicates: []schema.RelationshipPredicate{{LeftColumnID: parentFields["key"], Operator: op, RightColumnName: "key"}}})
	}
	a.bind(t, ctx, ref, b, map[string]string{"ext_children": "children", "ext_children_after": "children"})
	p.occurrences = map[string]string{"parents": parentsOcc, "children": childrenOcc, "ext_children": extChildren}
	p.parents = data.NewService(a.driver).WithPeers(staticPeers{b.name: data.NewService(b.driver)})
	return ctx, p
}

// children creates the same children in both solutions, in order.
func (p *keyedPair) children(t *testing.T, ctx context.Context, records ...map[string]interface{}) {
	t.Helper()
	for _, s := range []*solution{p.a, p.b} {
		svc := data.NewService(s.driver)
		for _, record := range records {
			_, err := svc.InsertRow(ctx, "children", record)
			require.NoError(t, err)
		}
	}
}

func (p *keyedPair) parent(t *testing.T, ctx context.Context, key, label string) string {
	t.Helper()
	row, err := p.parents.InsertRow(ctx, "parents", map[string]interface{}{"key": key, "label": label})
	require.NoError(t, err)
	return fmt.Sprint(row["id"])
}

// A text match field holding several lines is a key per line on either side
// of a relationship, for = and for other operators, separated by CR, LF or
// CRLF: in a portal, a batched portal, a find, a value list, a calculation
// and a cascading delete, for a table of the same solution and of another.
func TestMultiKeys_EitherSide(t *testing.T) {
	ctx, p := newKeyedPair(t, "")
	for _, side := range []string{"local", "external"} {
		occurrence := map[string]string{"local": "children", "external": "ext_children"}[side]
		p.a.unstored(t, ctx, p.parentsTable, side+"_count", fmt.Sprintf("Count ( %s::label )", occurrence), "Number")
		p.a.unstored(t, ctx, p.parentsTable, side+"_list", fmt.Sprintf("List ( %s::label )", occurrence), "Text")
	}
	p.children(t, ctx,
		map[string]interface{}{"key": "alpha", "label": "c1"},
		map[string]interface{}{"key": "gamma\r\ndelta", "label": "c2"}, // a multi-key of the related records
		map[string]interface{}{"key": "beta", "label": "c3"},
		map[string]interface{}{"key": "epsilon", "label": "c4"},
		map[string]interface{}{"key": "alpha extra", "label": "c5"}, // one line, which no line equals
	)
	one := p.parent(t, ctx, "alpha\rbeta", "p1") // a multi-key of the record in hand
	two := p.parent(t, ctx, "gamma", "p2")
	three := p.parent(t, ctx, "zeta\n\ndelta", "p3") // multi-keys on both sides
	none := p.parent(t, ctx, "", "p4")

	expected := map[string][]string{one: {"c1", "c3"}, two: {"c2"}, three: {"c2"}, none: {}}
	// key < key: a line of the parent's before a line of the child's.
	after := map[string][]string{one: {"c2", "c3", "c4", "c5"}, two: {}, three: {"c2", "c4"}, none: {}}
	ids := []string{one, two, three, none}

	for _, side := range []string{"local", "external"} {
		t.Run(side, func(t *testing.T) {
			for _, rel := range []struct {
				id   string
				want map[string][]string
			}{{p.equal[side], expected}, {p.less[side], after}} {
				for _, id := range ids {
					rows, err := p.parents.RelatedRows(ctx, "parents", id, rel.id, "", data.QueryOptions{})
					require.NoError(t, err)
					assert.Equal(t, rel.want[id], labelsOf(rows), "a portal")
				}
				batch, err := p.parents.RelatedRowsBatch(ctx, data.LocalOrigin("parents"), ids, rel.id, "", data.QueryOptions{})
				require.NoError(t, err)
				for _, id := range ids {
					assert.Equal(t, rel.want[id], labelsOf(batch[id]), "a batched portal")
				}
			}

			find := func(label string, rel string) []string {
				rows, err := p.parents.ExecuteFind(ctx, "parents", []data.FindRequest{
					{Criteria: []data.FindCriterion{{FieldName: "label", Value: label, RelationshipID: rel}}},
				}, data.QueryOptions{})
				require.NoError(t, err)
				return labelsOf(rows)
			}
			assert.Equal(t, []string{"p2", "p3"}, find("c2", p.equal[side]), "a find on a related multi-key")
			assert.Equal(t, []string{"p1"}, find("c3", p.equal[side]), "a find from a multi-key")
			assert.Empty(t, find("c5", p.equal[side]))
			assert.Equal(t, []string{"p1", "p3"}, find("c4", p.less[side]))

			row, err := p.parents.GetRow(ctx, "parents", one)
			require.NoError(t, err)
			assert.Equal(t, "2", fmt.Sprint(row[side+"_count"]), "a calculation")
			assert.Equal(t, "c1\rc3", row[side+"_list"])
			row, err = p.parents.GetRow(ctx, "parents", three)
			require.NoError(t, err)
			assert.Equal(t, "c2", row[side+"_list"])
		})
	}

	t.Run("value lists", func(t *testing.T) {
		for _, occurrence := range []string{"children", "ext_children"} {
			values := func(id string) []string {
				rows, err := p.parents.RelatedValues(ctx, p.occurrences["parents"], id, p.occurrences[occurrence], "label")
				require.NoError(t, err)
				labels := []string{}
				for _, v := range rows {
					labels = append(labels, v["label"])
				}
				return labels
			}
			assert.ElementsMatch(t, []string{"c1", "c3"}, values(one), occurrence)
			assert.Equal(t, []string{"c2"}, values(three), occurrence)
		}
	})

	t.Run("cascading delete", func(t *testing.T) {
		require.NoError(t, p.parents.DeleteRow(ctx, "parents", one))
		rows, err := data.NewService(p.a.driver).ListRows(ctx, "children", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"c2", "c4", "c5"}, labelsOf(rows), "both records the multi-key relates to are deleted")
		require.NoError(t, p.parents.DeleteRow(ctx, "parents", three))
		rows, err = data.NewService(p.a.driver).ListRows(ctx, "children", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"c4", "c5"}, labelsOf(rows), "a line of a related multi-key")
	})
}

// Records nothing sorts are in the order they were created, as FileMaker
// lists them: a found set, a portal on an unsorted relationship, and First,
// Last and GetNthRecord across one; a sort's ties keep that order.
func TestCreationOrder(t *testing.T) {
	ctx, p := newKeyedPair(t, "")
	for _, side := range []string{"local", "external"} {
		occurrence := map[string]string{"local": "children", "external": "ext_children"}[side]
		for name, formula := range map[string]string{
			"first": "%s::label", "last": "Last ( %s::label )", "second": "GetNthRecord ( %s::label ; 2 )",
		} {
			p.a.unstored(t, ctx, p.parentsTable, side+"_"+name, fmt.Sprintf(formula, occurrence), "Text")
		}
	}
	var records []map[string]interface{}
	var labels []string
	for i := range 24 {
		label := fmt.Sprintf("n%02d", i)
		records = append(records, map[string]interface{}{"key": "k", "label": label, "rank": i % 2})
		labels = append(labels, label)
	}
	p.children(t, ctx, records...)
	parent := p.parent(t, ctx, "k", "p")
	children := data.NewService(p.a.driver)

	rows, err := children.ListRows(ctx, "children", data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, labels, labelsOf(rows), "a found set")
	rows, err = children.ListRows(ctx, "children", data.QueryOptions{Limit: 5, Offset: 10})
	require.NoError(t, err)
	assert.Equal(t, labels[10:15], labelsOf(rows), "a page of it")
	rows, err = children.ExecuteFind(ctx, "children", []data.FindRequest{
		{Criteria: []data.FindCriterion{{FieldName: "key", Value: "k"}}}}, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, labels, labelsOf(rows), "a find")

	var even, odd []string
	for i, label := range labels {
		if i%2 == 0 {
			even = append(even, label)
		} else {
			odd = append(odd, label)
		}
	}
	rows, err = children.ListRows(ctx, "children", data.QueryOptions{SortBy: "rank"} /* descending */)
	require.NoError(t, err)
	assert.Equal(t, append(append([]string{}, odd...), even...), labelsOf(rows), "records that sort alike stay in creation order")

	for _, side := range []string{"local", "external"} {
		rows, err := p.parents.RelatedRows(ctx, "parents", parent, p.equal[side], "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, labels, labelsOf(rows), "%s: a portal", side)
		batch, err := p.parents.RelatedRowsBatch(ctx, data.LocalOrigin("parents"), []string{parent}, p.equal[side], "",
			data.QueryOptions{Limit: 4, Offset: 2})
		require.NoError(t, err)
		assert.Equal(t, labels[2:6], labelsOf(batch[parent]), "%s: a batched page", side)

		row, err := p.parents.GetRow(ctx, "parents", parent)
		require.NoError(t, err)
		assert.Equal(t, "n00", row[side+"_first"], side)
		assert.Equal(t, "n23", row[side+"_last"], side)
		assert.Equal(t, "n01", row[side+"_second"], side)
	}
}

// Last and GetNthRecord across a relationship that does not sort read the
// last records created, however many there are.
func TestCreationOrder_LastPastWhatIsReadAtOnce(t *testing.T) {
	ctx, p := newKeyedPair(t, "")
	for _, side := range []string{"local", "external"} {
		occurrence := map[string]string{"local": "children", "external": "ext_children"}[side]
		p.a.unstored(t, ctx, p.parentsTable, side+"_last", fmt.Sprintf("Last ( %s::label )", occurrence), "Text")
		p.a.unstored(t, ctx, p.parentsTable, side+"_nth", fmt.Sprintf("GetNthRecord ( %s::label ; 10000 )", occurrence), "Text")
		p.a.unstored(t, ctx, p.parentsTable, side+"_first", fmt.Sprintf("%s::label", occurrence), "Text")
	}
	for _, s := range []*solution{p.a, p.b} {
		s.bulkInsert(t, ctx, "children", []string{"key", "label"}, 10001, func(i int) []interface{} {
			return []interface{}{"BIG", fmt.Sprintf("big-%05d", i)}
		})
	}
	parent := p.parent(t, ctx, "BIG", "p")
	row, err := p.parents.GetRow(ctx, "parents", parent)
	require.NoError(t, err)
	for _, side := range []string{"local", "external"} {
		assert.Equal(t, "big-00000", row[side+"_first"], side)
		assert.Equal(t, "big-10000", row[side+"_last"], "%s: the last of 10001 records created", side)
		assert.Equal(t, "big-09999", row[side+"_nth"], side)
	}
}

// A portal sorted by an unstored calculation is sorted once its records are
// read, as FileMaker sorts it, up to the records one read sorts.
func TestRelatedRows_SortedByAnUnstoredCalculation(t *testing.T) {
	ctx, p := newKeyedPair(t, "score,-label")
	for _, s := range []*solution{p.a, p.b} {
		tableID := p.childrenA
		if s == p.b {
			tableID = p.childrenB
		}
		s.unstored(t, ctx, tableID, "score", "Mod ( rank ; 3 )", "Number")
	}
	var records []map[string]interface{}
	for i := range 9 {
		records = append(records, map[string]interface{}{"key": "k", "label": fmt.Sprintf("n%d", i), "rank": i})
	}
	// Two that sort alike, by score and label: they stay in creation order.
	records = append(records, map[string]interface{}{"key": "k", "label": "n1", "rank": 10})
	p.children(t, ctx, records...)
	parent := p.parent(t, ctx, "k", "p")
	other := p.parent(t, ctx, "k\rnone", "q")

	// score: n0 0, n1 1, n2 2, n3 0, n4 1, n5 2, n6 0, n7 1, n8 2, n1 (rank 10) 1.
	want := []string{"n6", "n3", "n0", "n7", "n4", "n1", "n1", "n8", "n5", "n2"}
	for _, side := range []string{"local", "external"} {
		rows, err := p.parents.RelatedRows(ctx, "parents", parent, p.equal[side], "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, want, labelsOf(rows), side)
		assert.Equal(t, "1", fmt.Sprint(rows[5]["rank"]), "%s: the first of two that sort alike", side)
		assert.Equal(t, "10", fmt.Sprint(rows[6]["rank"]), side)

		rows, err = p.parents.RelatedRows(ctx, "parents", parent, p.equal[side], "", data.QueryOptions{Limit: 3, Offset: 4})
		require.NoError(t, err)
		assert.Equal(t, want[4:7], labelsOf(rows), "%s: a page", side)
		rows, err = p.parents.RelatedRows(ctx, "parents", parent, p.equal[side], "",
			data.QueryOptions{Sort: []data.SortField{{Field: "score", Descending: true}}, Limit: 3})
		require.NoError(t, err)
		assert.Equal(t, []string{"n2", "n5", "n8"}, labelsOf(rows), "%s: the caller's sort", side)
		_, err = p.parents.RelatedRows(ctx, "parents", parent, p.equal[side], "",
			data.QueryOptions{Sort: []data.SortField{{Field: "score"}, {Field: "no_such_field"}}})
		// Another solution's field that is not there is a schema mismatch
		// naming it, as for any read there.
		require.ErrorContains(t, err, "unknown field: children.no_such_field", "%s: every field of the caller's sort is checked", side)

		batch, err := p.parents.RelatedRowsBatch(ctx, data.LocalOrigin("parents"), []string{parent, other}, p.equal[side], "",
			data.QueryOptions{Limit: 2, Offset: 1})
		require.NoError(t, err)
		assert.Equal(t, want[1:3], labelsOf(batch[parent]), "%s: a batched page", side)
		assert.Equal(t, want[1:3], labelsOf(batch[other]), side)
	}

	// Past what one read sorts, the read is refused rather than cut short.
	for _, s := range []*solution{p.a, p.b} {
		s.bulkInsert(t, ctx, "children", []string{"key", "label", "rank"}, 10001, func(i int) []interface{} {
			return []interface{}{"BIG", fmt.Sprintf("big-%05d", i), i}
		})
	}
	big := p.parent(t, ctx, "BIG", "big")
	for _, side := range []string{"local", "external"} {
		_, err := p.parents.RelatedRows(ctx, "parents", big, p.equal[side], "", data.QueryOptions{Limit: 10})
		require.ErrorIs(t, err, data.ErrTooManyRelated, side)
		_, err = p.parents.RelatedRowsBatch(ctx, data.LocalOrigin("parents"), []string{big}, p.equal[side], "", data.QueryOptions{Limit: 10})
		require.ErrorIs(t, err, data.ErrTooManyRelated, side)
	}

	// A found set is still never sorted by one.
	_, err := data.NewService(p.a.driver).ListRows(ctx, "children", data.QueryOptions{SortBy: "score"})
	require.ErrorIs(t, err, data.ErrUnstoredField)
}

// A table found without its creation order column is looked up again once
// that is no longer believed, so a column another server added is used.
func TestCreationOrder_ColumnAddedElsewhereIsNoticed(t *testing.T) {
	if testdb.Engine() == dbal.EngineSQLite {
		t.Skip("SQLite tables are created with their creation order, which cannot be added to a table later (docs/specs/sqlite_engine.md, \"Test exceptions\")")
	}
	ctx, a, _ := twoSolutions(t, "xelsewhere", "xelsewhere_unused")
	a.table(t, ctx, "later", [2]string{"label", "TEXT"})
	dialect := a.driver.Dialect()
	_, err := a.driver.DB().ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
		dialect.QuoteIdentifier("later"), dialect.QuoteIdentifier(dbal.CreationOrderColumn)))
	require.NoError(t, err)
	data.ForgetCreationOrder(a.driver.DB())
	has, err := data.HasCreationOrder(ctx, a.driver, "later")
	require.NoError(t, err)
	require.False(t, has)

	_, err = a.driver.DB().ExecContext(ctx, dbal.AddCreationOrderSQL(dialect, "later", 0))
	require.NoError(t, err)
	has, err = data.HasCreationOrder(ctx, a.driver, "later")
	require.NoError(t, err)
	assert.False(t, has, "a table found without the column is believed so for a while")

	defer data.SetMissingCreationOrderTTL(0)()
	has, err = data.HasCreationOrder(ctx, a.driver, "later")
	require.NoError(t, err)
	assert.True(t, has, "and then looked up again, without the catalog upgrade forgetting it")
}

// A table created before records were numbered in creation order is given
// the column when its catalog is brought up to date: the records it holds
// are numbered in the order the engine reads them, the next ones after them.
func TestCreationOrder_ExistingTableIsUpgraded(t *testing.T) {
	if testdb.Engine() == dbal.EngineSQLite {
		t.Skip("SQLite tables are created with their creation order, which cannot be added to a table later (docs/specs/sqlite_engine.md, \"Test exceptions\")")
	}
	ctx, a, _ := twoSolutions(t, "xupgrade", "xupgrade_unused")
	a.table(t, ctx, "older", [2]string{"label", "TEXT"})
	dialect := a.driver.Dialect()
	_, err := a.driver.DB().ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
		dialect.QuoteIdentifier("older"), dialect.QuoteIdentifier(dbal.CreationOrderColumn)))
	require.NoError(t, err)
	data.ForgetCreationOrder(a.driver.DB())
	svc := data.NewService(a.driver)
	var ids []string
	for i := range 12 {
		row, err := svc.InsertRow(ctx, "older", map[string]interface{}{"label": fmt.Sprintf("o%02d", i)})
		require.NoError(t, err)
		ids = append(ids, fmt.Sprint(row["id"]))
	}
	has, err := data.HasCreationOrder(ctx, a.driver, "older")
	require.NoError(t, err)
	require.False(t, has)
	rows, err := svc.ListRows(ctx, "older", data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 12, "a table without the column is read, by id")
	exportedIDs := func() []string {
		raw, err := a.schema.ExportDatabaseData(ctx, a.name)
		require.NoError(t, err)
		var bundle schema.DatabaseDataBundle
		require.NoError(t, msgpack.Unmarshal(raw, &bundle))
		var out []string
		for _, record := range bundle.TablesData["older"] {
			out = append(out, fmt.Sprint(record["id"]))
		}
		return out
	}
	var listed []string
	for _, row := range rows {
		listed = append(listed, fmt.Sprint(row["id"]))
	}
	assert.Equal(t, listed, exportedIDs(), "an export lists them as they are read, so a restore keeps their order")

	require.NoError(t, a.schema.EnsureSystemTables(ctx))
	has, err = data.HasCreationOrder(ctx, a.driver, "older")
	require.NoError(t, err)
	require.True(t, has)
	require.NoError(t, a.schema.EnsureSystemTables(ctx), "an upgraded catalog is only read")

	newest, err := svc.InsertRow(ctx, "older", map[string]interface{}{"label": "newest"})
	require.NoError(t, err)
	rows, err = svc.ListRows(ctx, "older", data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 13)
	got := make([]string, 0, 12)
	for _, row := range rows[:12] {
		got = append(got, fmt.Sprint(row["id"]))
	}
	if dialect.Engine() == dbal.EnginePostgres {
		assert.Equal(t, ids, got, "numbered in the order they were inserted in")
	} else {
		assert.ElementsMatch(t, ids, got, "numbered in id order")
	}
	assert.Equal(t, fmt.Sprint(newest["id"]), fmt.Sprint(rows[12]["id"]), "a new record comes after them")

	// An export lists the records in that order, which a restore inserts
	// them in.
	raw, err := a.schema.ExportDatabaseData(ctx, a.name)
	require.NoError(t, err)
	var bundle schema.DatabaseDataBundle
	require.NoError(t, msgpack.Unmarshal(raw, &bundle))
	assert.Equal(t, append(got, fmt.Sprint(newest["id"])), exportedIDs())
	assert.NotContains(t, bundle.TablesData["older"][0], dbal.CreationOrderColumn, "the order is not exported as a field")
}

// keyedTables are two tables of one solution, "holders" and "keyed", related
// by holders.<holderKey> = keyed.<keyedKey>, and each record's label.
type keyedTables struct {
	svc                 *data.Service
	rel                 string
	holders             string // table id
	holderOcc, keyedOcc string
}

func newKeyedTables(t *testing.T, prefix string, holderKey, keyedKey [2]string) (context.Context, *keyedTables) {
	t.Helper()
	ctx, a, _ := twoSolutions(t, prefix, prefix+"_unused")
	holders, holderOcc, holderFields := a.table(t, ctx, "holders", holderKey, [2]string{"label", "TEXT"})
	_, keyedOcc, keyedFields := a.table(t, ctx, "keyed", keyedKey, [2]string{"label", "TEXT"})
	rel := a.relate(t, ctx, schema.CreateRelationshipInput{Name: "holders_keyed",
		LeftOccurrenceID: holderOcc, RightOccurrenceID: keyedOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: holderFields[holderKey[0]], Operator: "=", RightColumnID: keyedFields[keyedKey[0]]}}})
	return ctx, &keyedTables{svc: data.NewService(a.driver), rel: rel, holders: holders, holderOcc: holderOcc, keyedOcc: keyedOcc}
}

func (k *keyedTables) insert(t *testing.T, ctx context.Context, table string, records ...map[string]interface{}) []string {
	t.Helper()
	var ids []string
	for _, record := range records {
		row, err := k.svc.InsertRow(ctx, table, record)
		require.NoError(t, err)
		ids = append(ids, fmt.Sprint(row["id"]))
	}
	return ids
}

// related is what a portal, a batched portal and a find on the related label
// each give for the holders: the labels related to each, and the holders
// found by label.
func (k *keyedTables) related(t *testing.T, ctx context.Context, ids []string) (portal, batch map[string][]string, find func(string) []string) {
	t.Helper()
	portal, batch = map[string][]string{}, map[string][]string{}
	rows, err := k.svc.RelatedRowsBatch(ctx, data.LocalOrigin("holders"), ids, k.rel, "", data.QueryOptions{})
	require.NoError(t, err)
	for _, id := range ids {
		one, err := k.svc.RelatedRows(ctx, "holders", id, k.rel, "", data.QueryOptions{})
		require.NoError(t, err)
		portal[id], batch[id] = labelsOf(one), labelsOf(rows[id])
	}
	find = func(label string) []string {
		found, err := k.svc.ExecuteFind(ctx, "holders", []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "label", Value: "==" + label, RelationshipID: k.rel}}},
		}, data.QueryOptions{})
		require.NoError(t, err)
		return labelsOf(found)
	}
	return portal, batch, find
}

// The lines a statement splits are named so that no field can be taken for
// one: a match field named like the splitter's own columns still matches.
func TestMultiKeys_FieldNamedLikeTheSplitter(t *testing.T) {
	for _, name := range []string{"f4b_n", "v", "f4b_line"} {
		t.Run(name, func(t *testing.T) {
			ctx, k := newKeyedTables(t, "xsplit_"+name, [2]string{"key", "TEXT"}, [2]string{name, "TEXT"})
			k.insert(t, ctx, "keyed",
				map[string]interface{}{name: "red\nblue", "label": "k1"},
				map[string]interface{}{name: "green", "label": "k2"})
			ids := k.insert(t, ctx, "holders",
				map[string]interface{}{"key": "blue", "label": "h1"},
				map[string]interface{}{"key": "green\rred", "label": "h2"})
			portal, batch, find := k.related(t, ctx, ids)
			want := map[string][]string{ids[0]: {"k1"}, ids[1]: {"k1", "k2"}}
			assert.Equal(t, want, portal)
			assert.Equal(t, want, batch)
			assert.Equal(t, []string{"h1", "h2"}, find("k1"))
		})
	}
}

// A line is a key when it is not empty, on either side and on either
// engine: a line of spaces is one, an empty line is none.
func TestMultiKeys_LineOfSpacesIsAKey(t *testing.T) {
	ctx, k := newKeyedTables(t, "xspaces", [2]string{"key", "TEXT"}, [2]string{"key", "TEXT"})
	k.insert(t, ctx, "keyed",
		map[string]interface{}{"key": "red\r\n  ", "label": "k1"}, // a line of spaces among others
		map[string]interface{}{"key": "  ", "label": "k2"},        // nothing but spaces
		map[string]interface{}{"key": "blue\n\n", "label": "k3"})  // empty lines, which are no keys
	ids := k.insert(t, ctx, "holders",
		map[string]interface{}{"key": "  ", "label": "h1"},
		map[string]interface{}{"key": "blue\n  ", "label": "h2"},
		map[string]interface{}{"key": "\n\n", "label": "h3"})
	portal, batch, find := k.related(t, ctx, ids)
	want := map[string][]string{ids[0]: {"k1", "k2"}, ids[1]: {"k1", "k2", "k3"}, ids[2]: {}}
	assert.Equal(t, want, portal, "a portal splits the record in hand's keys in Go")
	assert.Equal(t, want, batch)
	assert.Equal(t, []string{"h1", "h2"}, find("k2"), "a find splits both sides in SQL")
	assert.Equal(t, []string{"h2"}, find("k3"))
}

// A date or a timestamp meets the lines of a text field written as an ISO
// date (and time, in UTC), the same way whether the record in hand's key is
// read in Go or the two fields meet in SQL.
func TestMultiKeys_DatesAgainstLines(t *testing.T) {
	t.Run("date", func(t *testing.T) {
		ctx, k := newKeyedTables(t, "xdatekeys", [2]string{"day", "DATE"}, [2]string{"days", "TEXT"})
		k.insert(t, ctx, "keyed",
			map[string]interface{}{"days": "2026-03-01\n2026-03-05", "label": "k1"},
			map[string]interface{}{"days": "2026-03-05", "label": "k2"},
			map[string]interface{}{"days": "05/03/2026", "label": "k3"}) // no other format
		ids := k.insert(t, ctx, "holders",
			map[string]interface{}{"day": "2026-03-05", "label": "h1"},
			map[string]interface{}{"day": "2026-03-01", "label": "h2"})
		portal, batch, find := k.related(t, ctx, ids)
		want := map[string][]string{ids[0]: {"k1", "k2"}, ids[1]: {"k1"}}
		assert.Equal(t, want, portal)
		assert.Equal(t, want, batch)
		assert.Equal(t, []string{"h1", "h2"}, find("k1"))
		assert.Equal(t, []string{"h1"}, find("k2"))
	})
	t.Run("timestamp", func(t *testing.T) {
		ctx, k := newKeyedTables(t, "xstampkeys", [2]string{"at", "TIMESTAMP"}, [2]string{"ats", "TEXT"})
		k.insert(t, ctx, "keyed",
			map[string]interface{}{"ats": "2026-03-05 10:30:00\n2026-03-06", "label": "k1"},
			map[string]interface{}{"ats": "2026-03-06 00:00:00", "label": "k2"}) // midnight is a date alone
		ids := k.insert(t, ctx, "holders",
			map[string]interface{}{"at": time.Date(2026, 3, 5, 10, 30, 0, 0, time.UTC), "label": "h1"},
			map[string]interface{}{"at": time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC), "label": "h2"})
		portal, batch, find := k.related(t, ctx, ids)
		want := map[string][]string{ids[0]: {"k1"}, ids[1]: {"k1"}}
		assert.Equal(t, want, portal)
		assert.Equal(t, want, batch)
		assert.Equal(t, []string{"h1", "h2"}, find("k1"))
		assert.Empty(t, find("k2"))
	})
}

// Across several relationships, the records reached are listed all
// together, in the last relationship's sort order and then in the order they
// were created, not the related records of each record on the way one after
// the other: List, First and GetNthRecord read that order.
func TestCreationOrder_AcrossSeveralRelationships(t *testing.T) {
	for _, sorted := range []bool{false, true} {
		t.Run(fmt.Sprintf("sorted=%t", sorted), func(t *testing.T) {
			ctx, a, _ := twoSolutions(t, fmt.Sprintf("xpath%t", sorted), fmt.Sprintf("xpath%t_unused", sorted))
			groups, groupsOcc, groupFields := a.table(t, ctx, "groups", [2]string{"code", "TEXT"})
			_, membersOcc, memberFields := a.table(t, ctx, "members", [2]string{"code", "TEXT"}, [2]string{"key", "TEXT"})
			_, itemsOcc, itemFields := a.table(t, ctx, "items", [2]string{"key", "TEXT"}, [2]string{"label", "TEXT"},
				[2]string{"rank", "NUMBER"})
			a.relate(t, ctx, schema.CreateRelationshipInput{Name: "groups_members",
				LeftOccurrenceID: groupsOcc, RightOccurrenceID: membersOcc,
				Predicates: []schema.RelationshipPredicate{{LeftColumnID: groupFields["code"], Operator: "=", RightColumnID: memberFields["code"]}}})
			var sort *string
			if sorted {
				s := "rank"
				sort = &s
			}
			a.relate(t, ctx, schema.CreateRelationshipInput{Name: "members_items", SortRelated: sort,
				LeftOccurrenceID: membersOcc, RightOccurrenceID: itemsOcc,
				Predicates: []schema.RelationshipPredicate{{LeftColumnID: memberFields["key"], Operator: "=", RightColumnID: itemFields["key"]}}})
			for name, formula := range map[string]string{
				"listed": "List ( items::label )", "first": "items::label", "third": "GetNthRecord ( items::label ; 3 )",
			} {
				a.unstored(t, ctx, groups, name, formula, "Text")
			}
			svc := data.NewService(a.driver)
			insert := func(table string, record map[string]interface{}) {
				_, err := svc.InsertRow(ctx, table, record)
				require.NoError(t, err)
			}
			insert("members", map[string]interface{}{"code": "g", "key": "ma"})
			insert("members", map[string]interface{}{"code": "g", "key": "mb"})
			// Created alternately for the second member and the first; rank
			// sorts them the other way round, ties kept in creation order.
			for i, item := range []struct {
				key  string
				rank int
			}{{"mb", 4}, {"ma", 3}, {"mb", 2}, {"ma", 2}} {
				insert("items", map[string]interface{}{"key": item.key, "label": fmt.Sprintf("i%d", i+1), "rank": item.rank})
			}
			row, err := svc.InsertRow(ctx, "groups", map[string]interface{}{"code": "g"})
			require.NoError(t, err)
			row, err = svc.GetRow(ctx, "groups", fmt.Sprint(row["id"]))
			require.NoError(t, err)
			if sorted {
				assert.Equal(t, "i3\ri4\ri2\ri1", row["listed"])
				assert.Equal(t, "i3", row["first"])
				assert.Equal(t, "i2", row["third"])
			} else {
				assert.Equal(t, "i1\ri2\ri3\ri4", row["listed"])
				assert.Equal(t, "i1", row["first"])
				assert.Equal(t, "i3", row["third"])
			}
		})
	}
}
