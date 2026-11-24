package migration

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// Synthetic designs only: two tables, Accounts and Visits, joined on three
// criteria.

const multiPredicateDDR = `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
	`<BaseTable id="129" name="Accounts"><FieldCatalog>` +
	`<Field id="1" name="Code" dataType="Text" fieldType="Normal"/>` +
	`<Field id="2" name="Region" dataType="Text" fieldType="Normal"/>` +
	`<Field id="3" name="Since" dataType="Number" fieldType="Normal"/>` +
	`</FieldCatalog></BaseTable>` +
	`<BaseTable id="130" name="Visits"><FieldCatalog>` +
	`<Field id="1" name="Account Code" dataType="Text" fieldType="Normal"/>` +
	`<Field id="2" name="Region" dataType="Text" fieldType="Normal"/>` +
	`<Field id="3" name="Day" dataType="Number" fieldType="Normal"/>` +
	`</FieldCatalog></BaseTable>` +
	`</BaseTableCatalog><RelationshipGraph>` +
	`<TableList><Table id="1" name="Accounts" baseTable="Accounts" baseTableId="129"/>` +
	`<Table id="2" name="Account Visits" baseTable="Visits" baseTableId="130"/></TableList>` +
	`<RelationshipList><Relationship id="1">` +
	`<LeftTable name="Accounts"/><RightTable name="Account Visits" cascadeDelete="True"/>` +
	`<JoinPredicateList>` +
	`<JoinPredicate type="Equal"><LeftField><Field id="1" name="Code" table="Accounts"/></LeftField>` +
	`<RightField><Field id="1" name="Account Code" table="Account Visits"/></RightField></JoinPredicate>` +
	`<JoinPredicate type="NotEqual"><LeftField><Field id="2" name="Region" table="Accounts"/></LeftField>` +
	`<RightField><Field id="2" name="Region" table="Account Visits"/></RightField></JoinPredicate>` +
	`<JoinPredicate type="LessThan"><LeftField><Field id="3" name="Since" table="Accounts"/></LeftField>` +
	`<RightField><Field id="3" name="Day" table="Account Visits"/></RightField></JoinPredicate>` +
	`</JoinPredicateList></Relationship></RelationshipList></RelationshipGraph></File></FMPReport>`

func columnNames(bundle *schema.SolutionBundle) map[string]string {
	names := map[string]string{}
	for _, table := range bundle.Tables {
		for _, col := range table.Columns {
			names[col.ID] = table.Name + "." + col.Name
		}
	}
	return names
}

func describeCriteria(bundle *schema.SolutionBundle, rel schema.BundleRelationship) []string {
	names := columnNames(bundle)
	var out []string
	for _, p := range rel.PredicateList() {
		out = append(out, names[p.LeftColumnID]+" "+p.Operator+" "+names[p.RightColumnID])
	}
	return out
}

func assertNoFirstPredicateItem(t *testing.T, report *ConversionReport) {
	t.Helper()
	for _, list := range [][]ReportItem{report.Unsupported, report.ManualActionRequired, report.Rejected, report.Preserved} {
		for _, item := range list {
			if strings.Contains(item.Detail, "Only the first join predicate") || strings.Contains(item.Detail, "remaining join predicates") {
				t.Fatalf("a migrated multi-predicate relationship is still reported as cut short: %q", item.Detail)
			}
		}
	}
}

func TestMultiPredicateRelationshipFromDDR(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: multiPredicateDDR})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("expected the relationship, got %d (rejected: %+v)", len(res.Bundle.Relationships), res.Report.Rejected)
	}
	rel := res.Bundle.Relationships[0]
	want := []string{"accounts.code = visits.account_code", "accounts.region ≠ visits.region", "accounts.since < visits.day"}
	got := describeCriteria(res.Bundle, rel)
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("criteria: got %v, want %v", got, want)
	}
	if rel.LeftColumnID != rel.Predicates[0].LeftColumnID || rel.Operator != "=" {
		t.Fatal("the first criterion must also be the relationship's own pair")
	}
	if !rel.CascadeDelete {
		t.Fatal("cascade delete was lost")
	}
	assertNoFirstPredicateItem(t, res.Report)
}

// A criterion written from the right-hand side is turned around.
func TestMultiPredicateRelationshipMirrorsAReversedCriterion(t *testing.T) {
	doc := strings.Replace(multiPredicateDDR,
		`<JoinPredicate type="LessThan"><LeftField><Field id="3" name="Since" table="Accounts"/></LeftField>`+
			`<RightField><Field id="3" name="Day" table="Account Visits"/></RightField></JoinPredicate>`,
		`<JoinPredicate type="GreaterThan"><LeftField><Field id="3" name="Day" table="Account Visits"/></LeftField>`+
			`<RightField><Field id="3" name="Since" table="Accounts"/></RightField></JoinPredicate>`, 1)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	got := describeCriteria(res.Bundle, res.Bundle.Relationships[0])
	if len(got) != 3 || got[2] != "accounts.since < visits.day" {
		t.Fatalf("expected Visits::Day > Accounts::Since read as accounts.since < visits.day, got %v", got)
	}
}

// A criterion whose field cannot be found refuses the relationship rather
// than migrating it wider than the source defined it.
func TestMultiPredicateRelationshipWithAnUnresolvableCriterion(t *testing.T) {
	doc := strings.Replace(multiPredicateDDR,
		`<RightField><Field id="2" name="Region" table="Account Visits"/></RightField>`,
		`<RightField><Field id="77" name="Gone" table="Account Visits"/></RightField>`, 1)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if len(res.Bundle.Relationships) != 0 {
		t.Fatal("a relationship missing one of its criteria must not be migrated")
	}
	found := false
	for _, item := range res.Report.Rejected {
		if item.Domain == "relationship" && strings.Contains(item.Detail, "Gone") && strings.Contains(item.Detail, "1 of its 3 criteria") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the unresolvable criterion to be named, got %+v", res.Report.Rejected)
	}
	assertNoFirstPredicateItem(t, res.Report)
}

func TestMultiPredicateRelationshipFromSaveAsXML(t *testing.T) {
	const design = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Accounts"/><BaseTable id="130" name="Visits"/></BaseTableCatalog>
 <FieldsForTables>
 <FieldCatalog><BaseTableReference id="129" name="Accounts"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/><Field id="2" name="Opened" datatype="Date" fieldtype="Normal"/>
 </ObjectList></FieldCatalog>
 <FieldCatalog><BaseTableReference id="130" name="Visits"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/><Field id="2" name="Visited" datatype="Date" fieldtype="Normal"/>
 </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Accounts"><BaseTableSourceReference><BaseTableReference id="129" name="Accounts"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Later Visits"><BaseTableSourceReference><BaseTableReference id="130" name="Visits"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Accounts"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Later Visits"/></RightTable>
  <JoinPredicateList>
   <JoinPredicate type="Equal">
    <LeftField><FieldReference id="1" name="Code"><TableOccurrenceReference id="1" name="Accounts"/></FieldReference></LeftField>
    <RightField><FieldReference id="1" name="Code"><TableOccurrenceReference id="2" name="Later Visits"/></FieldReference></RightField>
   </JoinPredicate>
   <JoinPredicate type="LessThanOrEqual">
    <LeftField><FieldReference id="2" name="Opened"><TableOccurrenceReference id="1" name="Accounts"/></FieldReference></LeftField>
    <RightField><FieldReference id="2" name="Visited"><TableOccurrenceReference id="2" name="Later Visits"/></FieldReference></RightField>
   </JoinPredicate>
   <JoinPredicate type="CartesianProduct">
    <LeftField><FieldReference id="1" name="Code"><TableOccurrenceReference id="1" name="Accounts"/></FieldReference></LeftField>
    <RightField><FieldReference id="2" name="Visited"><TableOccurrenceReference id="2" name="Later Visits"/></FieldReference></RightField>
   </JoinPredicate>
  </JoinPredicateList>
 </Relationship></RelationshipCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("expected the relationship, got %d (rejected: %+v)", len(res.Bundle.Relationships), res.Report.Rejected)
	}
	got := describeCriteria(res.Bundle, res.Bundle.Relationships[0])
	want := []string{"accounts.code = visits.code", "accounts.opened ≤ visits.visited", "accounts.code × visits.visited"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("criteria: got %v, want %v", got, want)
	}
	assertNoFirstPredicateItem(t, res.Report)
}
