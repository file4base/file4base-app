package data_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiFixture is two tables joined on several criteria at once:
// accounts and the visits paid to them,
//
//	Accounts::code = Visits::code
//	AND Accounts::region ≠ Visits::region
//	AND Accounts::since < Visits::day
//
// so an account's visits are those with its code, from another region, after
// the day it opened.
type multiFixture struct {
	schemaSvc *schema.Service
	dataSvc   *data.Service

	accounts, visits             string // physical table names
	accountsOcc, visitsOcc       string // occurrence ids
	accountsOccName, visitsOccID string
	cols                         map[string]string // "table.field" -> column id
}

func newMultiFixture(t *testing.T, ctx context.Context, driver dbal.DatabaseDriver) *multiFixture {
	t.Helper()
	f := &multiFixture{schemaSvc: schema.NewService(driver), dataSvc: data.NewService(driver), cols: map[string]string{}}
	require.NoError(t, f.schemaSvc.EnsureSystemTables(ctx))
	stamp := uniqueTestStamp()
	f.accounts = fmt.Sprintf("mp_accounts_%d", stamp)
	f.visits = fmt.Sprintf("mp_visits_%d", stamp)

	tableIDs := map[string]string{}
	for table, fields := range map[string][]struct {
		name string
		kind dbal.AgnosticFieldType
	}{
		f.accounts: {{"code", dbal.FieldTypeText}, {"region", dbal.FieldTypeText}, {"since", dbal.FieldTypeNumber}},
		f.visits:   {{"code", dbal.FieldTypeText}, {"region", dbal.FieldTypeText}, {"day", dbal.FieldTypeNumber}, {"label", dbal.FieldTypeText}},
	} {
		tbl, err := f.schemaSvc.CreateTable(ctx, table, table)
		require.NoError(t, err)
		id := tbl.ID
		tableIDs[table] = id
		t.Cleanup(func() { _ = f.schemaSvc.DeleteTable(context.Background(), id) })
		for _, field := range fields {
			col, err := f.schemaSvc.AddColumn(ctx, id, schema.ColumnMetadata{
				Name: field.name, DisplayName: field.name, FieldType: field.kind, IsNullable: true,
			})
			require.NoError(t, err)
			f.cols[table+"."+field.name] = col.ID
		}
	}
	occurrences, err := f.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		switch o.BaseTableID {
		case tableIDs[f.accounts]:
			f.accountsOcc, f.accountsOccName = o.ID, o.Name
		case tableIDs[f.visits]:
			f.visitsOcc, f.visitsOccID = o.ID, o.Name
		}
	}
	require.NotEmpty(t, f.accountsOcc)
	require.NotEmpty(t, f.visitsOcc)
	return f
}

// criterion is accounts.left op visits.right.
func (f *multiFixture) criterion(left, op, right string) schema.RelationshipPredicate {
	return schema.RelationshipPredicate{
		LeftColumnID: f.cols[f.accounts+"."+left], Operator: op, RightColumnID: f.cols[f.visits+"."+right],
	}
}

func (f *multiFixture) relate(t *testing.T, ctx context.Context, in schema.CreateRelationshipInput) *schema.RelationshipMetadata {
	t.Helper()
	in.Name = fmt.Sprintf("mp_rel_%d", uniqueTestStamp())
	in.LeftOccurrenceID, in.RightOccurrenceID = f.accountsOcc, f.visitsOcc
	rel, err := f.schemaSvc.CreateRelationship(ctx, in)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.schemaSvc.DeleteRelationship(context.Background(), rel.ID) })
	return rel
}

func (f *multiFixture) insert(t *testing.T, ctx context.Context, table string, record map[string]interface{}) string {
	t.Helper()
	row, err := f.dataSvc.InsertRow(ctx, table, record)
	require.NoError(t, err)
	return fmt.Sprint(row["id"])
}

func labels(rows []map[string]interface{}, field string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprint(r[field]))
	}
	sort.Strings(out)
	return out
}

func TestMultiPredicateRelationships(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newMultiFixture(t, ctx, driver)
	rel := f.relate(t, ctx, schema.CreateRelationshipInput{
		Predicates: []schema.RelationshipPredicate{
			f.criterion("code", "=", "code"),
			f.criterion("region", "≠", "region"),
			f.criterion("since", "<", "day"),
		},
		CascadeDelete: true,
	})

	t.Run("the catalog keeps every criterion in order", func(t *testing.T) {
		require.Len(t, rel.Predicates, 3)
		assert.Equal(t, f.cols[f.accounts+".code"], rel.LeftColumnID, "the first criterion is still the relationship's pair")
		assert.Equal(t, "=", rel.Operator)

		all, err := f.schemaSvc.ListRelationships(ctx)
		require.NoError(t, err)
		var listed *schema.RelationshipMetadata
		for i := range all {
			if all[i].ID == rel.ID {
				listed = &all[i]
			}
		}
		require.NotNil(t, listed)
		require.Len(t, listed.Predicates, 3)
		got := []string{}
		for _, p := range listed.Predicates {
			got = append(got, p.LeftColumnName+" "+p.Operator+" "+p.RightColumnName)
		}
		assert.Equal(t, []string{"code = code", "region ≠ region", "since < day"}, got)
	})

	north := f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "A1", "region": "north", "since": 10})
	for _, v := range []map[string]interface{}{
		{"code": "A1", "region": "south", "day": 12, "label": "match"},
		{"code": "A1", "region": "east", "day": 30, "label": "match-later"},
		{"code": "A1", "region": "north", "day": 12, "label": "same-region"},
		{"code": "A1", "region": "south", "day": 5, "label": "too-early"},
		{"code": "A1", "region": "south", "day": 10, "label": "same-day"},
		{"code": "B2", "region": "south", "day": 12, "label": "other-code"},
	} {
		f.insert(t, ctx, f.visits, v)
	}

	t.Run("a portal shows the records meeting every criterion", func(t *testing.T) {
		rows, err := f.dataSvc.RelatedRows(ctx, f.accounts, north, rel.ID, "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"match", "match-later"}, labels(rows, "label"))
	})

	t.Run("read from the right side, each comparison is turned around", func(t *testing.T) {
		// The probe is in the north account's own region, so it is not one
		// of its visits and leaves the tests after this one alone.
		visit := f.insert(t, ctx, f.visits, map[string]interface{}{"code": "A1", "region": "north", "day": 11, "label": "probe"})
		f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "A1", "region": "west", "since": 1})   // related
		f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "A1", "region": "south", "since": 20}) // opened after
		f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "B2", "region": "east", "since": 3})   // another code
		rows, err := f.dataSvc.RelatedRows(ctx, f.visits, visit, rel.ID, "", data.QueryOptions{})
		require.NoError(t, err)
		var regions []string
		for _, r := range rows {
			regions = append(regions, fmt.Sprint(r["region"]))
		}
		sort.Strings(regions)
		assert.Equal(t, []string{"west"}, regions)
	})

	t.Run("a multi-key matches any of its lines on an equality", func(t *testing.T) {
		both := f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "A1\nB2", "region": "north", "since": 10})
		rows, err := f.dataSvc.RelatedRows(ctx, f.accounts, both, rel.ID, "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"match", "match-later", "other-code"}, labels(rows, "label"))
	})

	t.Run("an empty match field in any criterion relates nothing", func(t *testing.T) {
		blank := f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "A1", "since": 10})
		rows, err := f.dataSvc.RelatedRows(ctx, f.accounts, blank, rel.ID, "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("a related calculation reads through every criterion", func(t *testing.T) {
		tables, err := f.schemaSvc.ListTables(ctx)
		require.NoError(t, err)
		var accountsID string
		for _, tbl := range tables {
			if tbl.Name == f.accounts {
				accountsID = tbl.ID
			}
		}
		_, err = f.schemaSvc.AddColumn(ctx, accountsID, schema.ColumnMetadata{
			Name: "visit_days", DisplayName: "visit_days", FieldType: dbal.FieldTypeCalculation, IsNullable: true,
			CalculationFormula: unstoredCalcJSON(fmt.Sprintf("Sum ( %s::day )", f.visitsOccID), "Number"),
		})
		require.NoError(t, err)
		row, err := f.dataSvc.GetRow(ctx, f.accounts, north)
		require.NoError(t, err)
		assert.Equal(t, "42", fmt.Sprint(row["visit_days"]), "12 + 30: only the visits meeting every criterion")
	})

	t.Run("a find on a related field uses every criterion", func(t *testing.T) {
		found, err := f.dataSvc.ExecuteFind(ctx, f.accounts, []data.FindRequest{{Criteria: []data.FindCriterion{{
			FieldName: "label", Operator: "=", Value: "same-region", RelationshipID: rel.ID, Occurrence: f.visitsOcc,
		}}}}, data.QueryOptions{})
		require.NoError(t, err)
		ids := func(rows []map[string]interface{}) []string {
			var out []string
			for _, r := range rows {
				out = append(out, fmt.Sprint(r["id"]))
			}
			return out
		}
		assert.NotContains(t, ids(found), north, "the same-region visit is not one of the north account's")

		found, err = f.dataSvc.ExecuteFind(ctx, f.accounts, []data.FindRequest{{Criteria: []data.FindCriterion{{
			FieldName: "label", Operator: "=", Value: "match-later", RelationshipID: rel.ID, Occurrence: f.visitsOcc,
		}}}}, data.QueryOptions{})
		require.NoError(t, err)
		assert.Contains(t, ids(found), north)
	})

	t.Run("sorting related records", func(t *testing.T) {
		rows, err := f.dataSvc.RelatedRows(ctx, f.accounts, north, rel.ID, "",
			data.QueryOptions{Sort: []data.SortField{{Field: "day", Descending: true}}})
		require.NoError(t, err)
		var order []string
		for _, r := range rows {
			order = append(order, fmt.Sprint(r["label"]))
		}
		assert.Equal(t, []string{"match-later", "match"}, order)
	})

	t.Run("creation is refused unless every criterion is an equality", func(t *testing.T) {
		allowed := true
		_, err := f.schemaSvc.UpdateRelationship(ctx, rel.ID, schema.UpdateRelationshipInput{AllowCreation: &allowed})
		require.NoError(t, err)
		_, err = f.dataSvc.CreateRelatedRow(ctx, f.accounts, north, rel.ID, "", map[string]interface{}{"label": "new"})
		assert.ErrorIs(t, err, data.ErrCreationNotAllowed)
	})

	t.Run("a cascade deletes only the records meeting every criterion", func(t *testing.T) {
		require.NoError(t, f.dataSvc.DeleteRow(ctx, f.accounts, north))
		rows, err := f.dataSvc.ListRows(ctx, f.visits, data.QueryOptions{Limit: 100})
		require.NoError(t, err)
		left := labels(rows, "label")
		assert.NotContains(t, left, "match")
		assert.NotContains(t, left, "match-later")
		assert.Contains(t, left, "same-region")
		assert.Contains(t, left, "too-early")
		assert.Contains(t, left, "other-code")
	})
}

// A record created through a relationship of equalities gets every one of
// its match fields from the record in hand.
func TestMultiPredicateCreation(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newMultiFixture(t, ctx, driver)
	rel := f.relate(t, ctx, schema.CreateRelationshipInput{
		Predicates:    []schema.RelationshipPredicate{f.criterion("code", "=", "code"), f.criterion("region", "=", "region")},
		AllowCreation: true,
	})
	account := f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "C3", "region": "north"})

	created, err := f.dataSvc.CreateRelatedRow(ctx, f.accounts, account, rel.ID, "",
		map[string]interface{}{"label": "child", "code": "spoofed"})
	require.NoError(t, err)
	assert.Equal(t, "C3", created["code"], "the caller cannot aim the record at another parent")
	assert.Equal(t, "north", created["region"])

	rows, err := f.dataSvc.RelatedRows(ctx, f.accounts, account, rel.ID, "", data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"child"}, labels(rows, "label"))

	t.Run("an empty match field in any criterion is refused", func(t *testing.T) {
		partial := f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "C4"})
		_, err := f.dataSvc.CreateRelatedRow(ctx, f.accounts, partial, rel.ID, "", map[string]interface{}{"label": "orphan"})
		assert.ErrorIs(t, err, data.ErrNoMatchValue)
	})
}

// × relates every record for its criterion, empty fields included: with the
// other criteria it narrows nothing, and alone it relates every record.
func TestCartesianCriterion(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newMultiFixture(t, ctx, driver)
	withCode := f.relate(t, ctx, schema.CreateRelationshipInput{
		Predicates: []schema.RelationshipPredicate{f.criterion("code", "=", "code"), f.criterion("region", "×", "region")},
	})
	everything := f.relate(t, ctx, schema.CreateRelationshipInput{
		Predicates: []schema.RelationshipPredicate{f.criterion("region", "×", "region")},
	})
	account := f.insert(t, ctx, f.accounts, map[string]interface{}{"code": "D5"})
	f.insert(t, ctx, f.visits, map[string]interface{}{"code": "D5", "label": "d5"})
	f.insert(t, ctx, f.visits, map[string]interface{}{"code": "E6", "label": "e6"})

	rows, err := f.dataSvc.RelatedRows(ctx, f.accounts, account, withCode.ID, "", data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"d5"}, labels(rows, "label"))

	rows, err = f.dataSvc.RelatedRows(ctx, f.accounts, account, everything.ID, "", data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"d5", "e6"}, labels(rows, "label"))
}

// The criteria are checked when a relationship is saved, and a client that
// sends one pair of fields keeps working.
func TestMultiPredicateValidation(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newMultiFixture(t, ctx, driver)
	create := func(in schema.CreateRelationshipInput) error {
		in.LeftOccurrenceID, in.RightOccurrenceID = f.accountsOcc, f.visitsOcc
		rel, err := f.schemaSvc.CreateRelationship(ctx, in)
		if err == nil {
			_ = f.schemaSvc.DeleteRelationship(ctx, rel.ID)
		}
		return err
	}

	assert.ErrorIs(t, create(schema.CreateRelationshipInput{Predicates: []schema.RelationshipPredicate{}}),
		schema.ErrInvalidRelationship, "an empty list is refused")
	wrongSide := schema.RelationshipPredicate{
		LeftColumnID: f.cols[f.visits+".label"], Operator: "=", RightColumnID: f.cols[f.visits+".code"],
	}
	err := create(schema.CreateRelationshipInput{Predicates: []schema.RelationshipPredicate{f.criterion("code", "=", "code"), wrongSide}})
	require.ErrorIs(t, err, schema.ErrInvalidRelationship)
	assert.Contains(t, err.Error(), "criterion 2")
	assert.ErrorIs(t, create(schema.CreateRelationshipInput{Predicates: []schema.RelationshipPredicate{f.criterion("code", "~", "code")}}),
		schema.ErrInvalidRelationship, "an unknown operator is refused")

	legacy := f.relate(t, ctx, schema.CreateRelationshipInput{
		LeftColumnID: f.cols[f.accounts+".code"], RightColumnID: f.cols[f.visits+".code"], Operator: "<>",
	})
	require.Len(t, legacy.Predicates, 1)
	assert.Equal(t, "≠", legacy.Operator)

	t.Run("the criteria can be replaced, reordered and removed", func(t *testing.T) {
		updated, err := f.schemaSvc.UpdateRelationship(ctx, legacy.ID, schema.UpdateRelationshipInput{
			Predicates: []schema.RelationshipPredicate{f.criterion("since", "≤", "day"), f.criterion("code", "=", "code")},
		})
		require.NoError(t, err)
		require.Len(t, updated.Predicates, 2)
		assert.Equal(t, f.cols[f.accounts+".since"], updated.LeftColumnID, "the first criterion is the relationship's pair")
		assert.Equal(t, "≤", updated.Operator)

		_, err = f.schemaSvc.UpdateRelationship(ctx, legacy.ID, schema.UpdateRelationshipInput{Predicates: []schema.RelationshipPredicate{}})
		assert.ErrorIs(t, err, schema.ErrInvalidRelationship)

		updated, err = f.schemaSvc.UpdateRelationship(ctx, legacy.ID, schema.UpdateRelationshipInput{
			Predicates: []schema.RelationshipPredicate{f.criterion("code", "=", "code")},
		})
		require.NoError(t, err)
		require.Len(t, updated.Predicates, 1)
		resolved, err := f.dataSvc.Relationship(ctx, legacy.ID)
		require.NoError(t, err)
		assert.Len(t, resolved.Predicates, 1)
	})

	t.Run("deleting a field one criterion matches on deletes the relationship", func(t *testing.T) {
		rel := f.relate(t, ctx, schema.CreateRelationshipInput{
			Predicates: []schema.RelationshipPredicate{f.criterion("code", "=", "code"), f.criterion("since", "<", "day")},
		})
		tables, err := f.schemaSvc.ListTables(ctx)
		require.NoError(t, err)
		for _, tbl := range tables {
			if tbl.Name == f.visits {
				require.NoError(t, f.schemaSvc.DeleteColumn(ctx, tbl.ID, f.cols[f.visits+".day"]))
			}
		}
		_, err = f.dataSvc.Relationship(ctx, rel.ID)
		assert.ErrorIs(t, err, data.ErrRelationshipNotFound)
	})
}
