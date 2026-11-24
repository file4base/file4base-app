package migration

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

func TestNativeStructureCatalogsAndOperands(t *testing.T) {
	// Original synthetic design in the native wrapper grammar. No source export
	// or private names, formulas, records or script bodies are reproduced.
	const design = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="1" name="Status" datatype="Text" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog><TableOccurrence id="1" name="Active Visits"><BaseTableSourceReference><BaseTableReference id="129" name="Visits"/></BaseTableSourceReference></TableOccurrence></TableOccurrenceCatalog>
 <LayoutCatalog><Layout id="90" name="Group" isFolder="True"/><Layout id="91" name="Visit"><TableOccurrenceReference id="1" name="Active Visits"/></Layout><Layout id="90" name="Group" isFolder="Marker"/></LayoutCatalog>
 <ScriptCatalog><Script id="80" name="Group" isFolder="True"/><Script id="81" name="Start"/><Script id="82" name="Finish"/><Script id="80" name="Group" isFolder="Marker"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Start"/><ObjectList>
 <Step id="141" name="Set Variable" enable="True"><ParameterValues><Parameter type="Variable"><value><Calculation><Calculation><Text>3</Text></Calculation></Calculation></value><Name value="$batch"/><repetition><Calculation><Text>1</Text></Calculation></repetition></Parameter></ParameterValues></Step>
 <Step id="76" name="Set Field" enable="False"><ParameterValues><Parameter type="FieldReference"><FieldReference id="1" name="Status"><TableOccurrenceReference id="1" name="Active Visits"/></FieldReference></Parameter><Parameter type="Calculation"><Calculation><Calculation><Text>"ready"</Text></Calculation></Calculation></Parameter></ParameterValues></Step>
 <Step id="1" name="Perform Script" enable="True"><ParameterValues><Parameter type="List"><List><ScriptReference id="82" name="Finish"/></List></Parameter><Parameter type="Parameter"><Parameter><Calculation><Text>$batch</Text></Calculation></Parameter></Parameter></ParameterValues></Step>
 <Step id="6" name="Go to Layout" enable="True"><ParameterValues><Parameter type="LayoutReferenceContainer"><LayoutReferenceContainer><LayoutReference id="91" name="Visit"/></LayoutReferenceContainer></Parameter><Parameter type="Animation"><Animation value="0"/></Parameter><Parameter type="CustomPluginOption"><CustomPluginOption value="1"/></Parameter></ParameterValues></Step>
 </ObjectList></Script><Script><ScriptReference id="82" name="Finish"/><ObjectList><Step id="103" name="Exit Script" enable="True"><ParameterValues><Parameter type="Calculation"><Calculation><Text>"finished"</Text></Calculation></Parameter></ParameterValues></Step></ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bundle.Tables) != 1 || result.Report.Counts.Fields != 1 || len(result.Bundle.Scripts) != 2 || len(result.Bundle.Layouts) != 1 {
		t.Fatal("native definitions or folder markers were misread")
	}
	steps := result.Bundle.Scripts[0].Steps
	if len(steps) != 4 || steps[0].Params["name"] != "$batch" || steps[0].Params["value"] != "3" {
		t.Fatal("native variable operands were lost")
	}
	if steps[1].IsEnabled || steps[1].Params["field"] != "status" || steps[1].Params["value"] != `"ready"` || steps[1].Params["value_is_calculation"] != true {
		t.Fatal("native field operands or source enabled state changed")
	}
	if steps[2].Params["script_name"] != "Finish" || steps[2].Params["script_parameter"] != "$batch" {
		t.Fatal("native script reference or argument was lost")
	}
	if steps[3].Params["layout_name"] != "Visit" {
		t.Fatal("native layout reference was lost")
	}
	if result.Bundle.Scripts[1].Steps[0].Params["result"] != `"finished"` {
		t.Fatal("native return expression was lost")
	}
	found := false
	for _, item := range result.Report.ManualActionRequired {
		if item.Domain == "script" {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown native parameter option was hidden")
	}
}

func TestContainerCalculationIsMigrated(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog><Field id="1" name="Attachment" dataType="Binary" fieldType="Calculation"><Calculation><Text>If ( Kind = "a" ; Front ; Back )</Text></Calculation></Field><Field id="2" name="Kind" dataType="Text" fieldType="Normal"/><Field id="3" name="Front" dataType="Binary" fieldType="Normal"/><Field id="4" name="Back" dataType="Binary" fieldType="Normal"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`})
	if err != nil {
		t.Fatal(err)
	}
	var column *schema.BundleColumn
	for i, c := range result.Bundle.Tables[0].Columns {
		if c.DisplayName == "Attachment" {
			column = &result.Bundle.Tables[0].Columns[i]
		}
	}
	if column == nil || column.FieldType != "CALCULATION" || column.CalculationFormula == nil {
		t.Fatalf("a container calculation is not migrated as a calculation: %#v", column)
	}
	if !strings.Contains(*column.CalculationFormula, `"result_type":"Container"`) {
		t.Fatalf("result type lost: %s", *column.CalculationFormula)
	}
	for _, item := range append(result.Report.Unsupported, result.Report.ManualActionRequired...) {
		if strings.Contains(item.SourceName, "Attachment") {
			t.Fatalf("a container calculation is still reported: %+v", item)
		}
	}
}

func TestNativeNestedCalculatedFieldRemainsCalculation(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMSaveAsXML><Structure><AddAction><BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog><FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="1" name="Quantity" datatype="Number" fieldtype="Calculated"><Calculation><Calculation><Text>3</Text></Calculation></Calculation></Field></ObjectList></FieldCatalog></FieldsForTables></AddAction></Structure></FMSaveAsXML>`})
	if err != nil {
		t.Fatal(err)
	}
	column := result.Bundle.Tables[0].Columns[1]
	if column.FieldType != "CALCULATION" || column.CalculationFormula == nil {
		t.Fatal("native calculated field was silently converted to a stored snapshot")
	}
}

// FileMaker writes layout and script menu separators as items named "-" with
// isSeparatorItem="True", on the deleted-occurrence placeholder. They are
// skipped; a real layout whose occurrence was deleted is reported, not refused.
func TestNativeSeparatorsAndDeletedOccurrenceLayouts(t *testing.T) {
	const design = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="1" name="Status" datatype="Text" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog><TableOccurrence id="1" name="Visits"><BaseTableSourceReference><BaseTableReference id="129" name="Visits"/></BaseTableSourceReference></TableOccurrence></TableOccurrenceCatalog>
 <LayoutCatalog>
  <Layout id="1" name="Visit"><TableOccurrenceReference id="1" name="Visits"/></Layout>
  <Layout id="2" name="-" isSeparatorItem="True"><TableOccurrenceReference id="-1" name="&lt;Table Missing&gt;"/></Layout>
  <Layout id="3" name="Old report"><TableOccurrenceReference id="-1" name="&lt;Table Missing&gt;"/></Layout>
 </LayoutCatalog>
 <ScriptCatalog><Script id="80" name="Start"/><Script id="81" name="-" isSeparatorItem="True"/></ScriptCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bundle.Layouts) != 1 || result.Bundle.Layouts[0].Name != "Visit" {
		t.Fatalf("layouts = %+v, want only Visit", result.Bundle.Layouts)
	}
	if len(result.Bundle.Scripts) != 1 || result.Bundle.Scripts[0].Name != "Start" {
		t.Fatalf("scripts = %+v, want only Start", result.Bundle.Scripts)
	}
	if n := result.Report.Counts.RejectedCount; n != 0 {
		t.Fatalf("rejected = %d (%+v), want 0", n, result.Report.Rejected)
	}
	reported := 0
	for _, item := range result.Report.Unsupported {
		if item.Domain == "layout" {
			if item.SourceName != "Old report" {
				t.Fatalf("separator reported: %+v", item)
			}
			reported++
		}
	}
	if reported != 1 {
		t.Fatalf("deleted-occurrence layout reported %d times, want 1", reported)
	}
}

// An occurrence whose base table was deleted keeps an empty source reference;
// it and what uses it are reported as not migrated rather than refused.
func TestNativeOccurrenceOnDeletedTable(t *testing.T) {
	const design = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="1" name="Code" datatype="Text" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Visits" type="Local"><BaseTableSourceReference type="BaseTableReference"><BaseTableReference id="129" name="Visits"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Archive" type="Local"><BaseTableSourceReference type="BaseTableReference"></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1"><LeftTable><TableOccurrenceReference id="1" name="Visits"/></LeftTable><RightTable><TableOccurrenceReference id="2" name="Archive"/></RightTable>
  <JoinPredicateList><JoinPredicate type="Equal"><LeftField><FieldReference id="1" name="Code"><TableOccurrenceReference id="1" name="Visits"/></FieldReference></LeftField><RightField><FieldReference id="9" name="Code"><TableOccurrenceReference id="2" name="Archive"/></FieldReference></RightField></JoinPredicate></JoinPredicateList></Relationship></RelationshipCatalog>
 <LayoutCatalog><Layout id="1" name="Visit"><TableOccurrenceReference id="1" name="Visits"/></Layout><Layout id="2" name="Old archive"><TableOccurrenceReference id="2" name="Archive"/></Layout></LayoutCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	if n := result.Report.Counts.RejectedCount; n != 0 {
		t.Fatalf("rejected = %d: %+v", n, result.Report.Rejected)
	}
	domains := map[string]int{}
	for _, item := range result.Report.Unsupported {
		domains[item.Domain]++
	}
	if domains["occurrence"] != 1 || domains["relationship"] != 1 || domains["layout"] != 1 {
		t.Fatalf("unsupported by domain = %v, want one occurrence, relationship and layout", domains)
	}
	if len(result.Bundle.Layouts) != 1 || len(result.Bundle.Relationships) != 0 {
		t.Fatalf("layouts %d, relationships %d", len(result.Bundle.Layouts), len(result.Bundle.Relationships))
	}
}

// An unstored calculation is computed whenever it is shown, so a cell a
// native file still holds for it is stale: it is not read, whatever its type,
// even when it refers to a container object the table does not have.
func TestNativeUnstoredCalculationCellsAreIgnored(t *testing.T) {
	const design = `<FMPReport><File name="Assets.fmp12"><BaseTableCatalog>` +
		`<BaseTable id="129" name="Documents"><FieldCatalog>` +
		`<Field id="1" name="Title" dataType="Text"/>` +
		`<Field id="2" name="Attachment" dataType="Container"/>` +
		`<Field id="3" name="Shown" dataType="Binary" fieldType="Calculated"><Calculation><Text>Attachment</Text></Calculation><Storage storeCalculationResults="False"/></Field>` +
		`<Field id="4" name="Unknown" dataType="Binary" fieldType="Calculated"><Calculation><Text>NoSuchFunction ( Attachment )</Text></Calculation><Storage storeCalculationResults="False"/></Field>` +
		`</FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`
	payload := appendHBAM7TableCatalog(nil, 129, "Documents", []testHBAM7FieldSpec{
		{id: 1, name: "Title", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Attachment", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeContainer},
		{id: 3, name: "Shown", fieldClass: hbamFieldClassCalculation, dataType: hbamDataTypeContainer},
		{id: 4, name: "Unknown", fieldClass: hbamFieldClassCalculation, dataType: hbamDataTypeContainer},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Plan"})
	// Both calculations point at object 2, which this table does not hold.
	payload = appendSection5ContainerRef(payload, 129, 1, 3, "02")
	payload = appendSection5ContainerRef(payload, 129, 1, 4, "02")

	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: design}, buildSyntheticHBAM7(payload))
	if err != nil {
		t.Fatal(err)
	}
	if converted.Report.Counts.RejectedCount != 0 || converted.Report.Counts.RecordsReady != 1 || converted.Report.Counts.ContainerFilesMissing != 0 {
		t.Fatalf("a stale cell of an unstored calculation refused the record: %+v", converted.Report.Counts)
	}
	row := converted.TableRows["documents"][0]
	if row["title"] != "Plan" || row["shown"] != nil || row["unknown"] != nil {
		t.Fatalf("unexpected row: %#v", row)
	}
}
