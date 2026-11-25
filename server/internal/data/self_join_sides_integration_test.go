package data_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staffFixture is a hierarchy drawn on one occurrence: staff::code on the
// left, staff::boss_code on the right, so the left end is a boss and the
// right end the boss's reports. Both ends are the same occurrence, so only
// the end tells them apart.
type staffFixture struct {
	*relatedFixture
	staff, occurrence string
}

func newStaffFixture(t *testing.T, in schema.CreateRelationshipInput) *staffFixture {
	t.Helper()
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return nil
	}
	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	f := &staffFixture{relatedFixture: &relatedFixture{schemaSvc: schemaSvc, dataSvc: data.NewService(driver)}}
	f.staff = fmt.Sprintf("rel_staff_%d", uniqueTestStamp())
	tbl, err := schemaSvc.CreateTable(ctx, "Rel Staff", f.staff)
	require.NoError(t, err)
	t.Cleanup(func() { _ = schemaSvc.DeleteTable(t.Context(), tbl.ID) })
	ids := map[string]string{}
	for _, name := range []string{"code", "boss_code"} {
		col, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: name, DisplayName: name, FieldType: dbal.FieldTypeText, IsNullable: true,
		})
		require.NoError(t, err)
		ids[name] = col.ID
	}
	occurrences, err := schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		if o.BaseTableID == tbl.ID {
			f.occurrence = o.ID
		}
	}
	require.NotEmpty(t, f.occurrence)

	in.Name = fmt.Sprintf("staff_bosses_%d", uniqueTestStamp())
	in.LeftOccurrenceID, in.LeftColumnID = f.occurrence, ids["code"]
	in.RightOccurrenceID, in.RightColumnID = f.occurrence, ids["boss_code"]
	in.Operator = "="
	rel, err := schemaSvc.CreateRelationship(ctx, in)
	require.NoError(t, err)
	t.Cleanup(func() { _ = schemaSvc.DeleteRelationship(t.Context(), rel.ID) })
	f.relationship = rel.ID

	// ceo ← vp ← (lead ← dev), ceo ← cfo
	for _, p := range [][2]string{{"ceo", ""}, {"vp", "ceo"}, {"cfo", "ceo"}, {"lead", "vp"}, {"dev", "lead"}} {
		_, err := f.dataSvc.InsertRow(ctx, f.staff, map[string]interface{}{"code": p[0], "boss_code": p[1]})
		require.NoError(t, err)
	}
	return f
}

func (f *staffFixture) id(t *testing.T, code string) string {
	t.Helper()
	rows, err := f.dataSvc.ListRows(t.Context(), f.staff, data.QueryOptions{})
	require.NoError(t, err)
	for _, r := range rows {
		if r["code"] == code {
			return fmt.Sprint(r["id"])
		}
	}
	t.Fatalf("no %s", code)
	return ""
}

func (f *staffFixture) codes(t *testing.T) []string {
	t.Helper()
	rows, err := f.dataSvc.ListRows(t.Context(), f.staff, data.QueryOptions{})
	require.NoError(t, err)
	out := []string{}
	for _, r := range rows {
		out = append(out, fmt.Sprint(r["code"]))
	}
	sort.Strings(out)
	return out
}

// The right end's option deletes a boss's reports, down the hierarchy.
func TestSelfJoinCascadeDeletesDownTheHierarchy(t *testing.T) {
	f := newStaffFixture(t, schema.CreateRelationshipInput{CascadeDelete: true})
	if f == nil {
		return
	}
	require.NoError(t, f.dataSvc.DeleteRow(t.Context(), f.staff, f.id(t, "vp")))
	assert.Equal(t, []string{"ceo", "cfo"}, f.codes(t), "the vp's reports went, and their reports; the vp's boss stays")
}

// The left end's option deletes a report's boss, up the hierarchy, and never
// the reports.
func TestSelfJoinLeftCascadeDeletesUpTheHierarchy(t *testing.T) {
	f := newStaffFixture(t, schema.CreateRelationshipInput{LeftCascadeDelete: true})
	if f == nil {
		return
	}
	require.NoError(t, f.dataSvc.DeleteRow(t.Context(), f.staff, f.id(t, "lead")))
	assert.Equal(t, []string{"cfo", "dev"}, f.codes(t), "the lead's boss went, and theirs; the lead's report stays")
}

// Creating names the end created in: a boss from a report through the left
// end, a report from a boss through the right end, each only when that end
// allows it.
func TestSelfJoinCreatesOnTheEndNamed(t *testing.T) {
	f := newStaffFixture(t, schema.CreateRelationshipInput{LeftAllowCreation: true})
	if f == nil {
		return
	}
	ctx := t.Context()
	from := data.LocalOrigin(f.staff)
	_, err := f.dataSvc.CreateRelatedRowToward(ctx, from, f.id(t, "cfo"), f.relationship, f.occurrence, data.EndRight, nil)
	require.ErrorIs(t, err, data.ErrCreationNotAllowed, "the right end does not allow creation")
	_, err = f.dataSvc.CreateRelatedRow(ctx, f.staff, f.id(t, "cfo"), f.relationship, f.occurrence, nil)
	require.ErrorIs(t, err, data.ErrCreationNotAllowed, "the occurrence alone names the right end")

	f2 := newStaffFixture(t, schema.CreateRelationshipInput{AllowCreation: true})
	report, err := f2.dataSvc.CreateRelatedRowToward(ctx, data.LocalOrigin(f2.staff), f2.id(t, "cfo"), f2.relationship, "", data.EndRight,
		map[string]interface{}{"code": "analyst"})
	require.NoError(t, err)
	assert.Equal(t, "cfo", report["boss_code"], "a report of the cfo")

	// A dev whose boss is not in the table yet gets one.
	row, err := f.dataSvc.InsertRow(ctx, f.staff, map[string]interface{}{"code": "intern", "boss_code": "mentor"})
	require.NoError(t, err)
	boss, err := f.dataSvc.CreateRelatedRowToward(ctx, from, fmt.Sprint(row["id"]), f.relationship, f.occurrence, data.EndLeft, nil)
	require.NoError(t, err)
	assert.Equal(t, "mentor", boss["code"], "the intern's boss, created from the intern")

	bosses, err := f.dataSvc.RelatedRowsFrom(ctx, from, fmt.Sprint(row["id"]), f.relationship, "", data.QueryOptions{End: data.EndLeft})
	require.NoError(t, err)
	require.Len(t, bosses, 1)
	assert.Equal(t, "mentor", bosses[0]["code"], "read through the left end")
}

// Deleting and creating apply no sort: a relationship sort on an unstored
// calculation, which a read refuses, does not stop either.
func TestCascadeAndCreationIgnoreTheRelationshipSort(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, true, true, "")
	customers, err := f.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	var customersID string
	for _, tbl := range customers {
		if tbl.Name == f.customers {
			customersID = tbl.ID
		}
	}
	_, err = f.schemaSvc.AddColumn(ctx, customersID, schema.ColumnMetadata{
		Name: "shout", DisplayName: "shout", FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: unstoredCalcJSON(`last_name & "!"`, "Text"),
	})
	require.NoError(t, err)
	setLeftOptions(t, f, schema.UpdateRelationshipInput{SortRelated: strPtr("shout")})

	company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Sorted By A Calc"})
	require.NoError(t, err)
	_, err = f.dataSvc.CreateRelatedRow(ctx, f.companies, company["id"].(string), f.relationship, f.customersOccurrence,
		map[string]interface{}{"last_name": "Soto"})
	require.NoError(t, err, "creating reads no page, so it sorts nothing")

	require.NoError(t, f.dataSvc.DeleteRow(ctx, f.companies, company["id"].(string)), "a cascade sorts nothing")
	remaining, err := f.dataSvc.ListRows(ctx, f.customers, data.QueryOptions{})
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// A find on a related field through a self-join searches the end it names:
// the left end finds the records whose boss matches, the right end (or the
// occurrence alone) those with a report that matches.
func TestSelfJoinFindSearchesTheEndNamed(t *testing.T) {
	f := newStaffFixture(t, schema.CreateRelationshipInput{})
	if f == nil {
		return
	}
	find := func(code, side string) []string {
		t.Helper()
		rows, err := f.dataSvc.ExecuteFind(t.Context(), f.staff, []data.FindRequest{{Criteria: []data.FindCriterion{
			{FieldName: "code", Operator: "==", Value: code, RelationshipID: f.relationship, Occurrence: f.occurrence, Side: side},
		}}}, data.QueryOptions{})
		require.NoError(t, err)
		out := []string{}
		for _, r := range rows {
			out = append(out, fmt.Sprint(r["code"]))
		}
		sort.Strings(out)
		return out
	}
	assert.Equal(t, []string{"cfo", "vp"}, find("ceo", "left"), "whose boss is the ceo")
	assert.Equal(t, []string{"vp"}, find("lead", "right"), "who has the lead as a report")
	assert.Equal(t, []string{"vp"}, find("lead", ""), "the occurrence alone names the right end")
	assert.Equal(t, []string{"vp"}, find("lead", "RIGHT"))

	_, err := f.dataSvc.ExecuteFind(t.Context(), f.staff, []data.FindRequest{{Criteria: []data.FindCriterion{
		{FieldName: "code", Value: "ceo", RelationshipID: f.relationship, Occurrence: f.occurrence, Side: "middle"},
	}}}, data.QueryOptions{})
	require.ErrorIs(t, err, data.ErrInvalidValue, "an end that is neither")
}
