package migration

import "testing"

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
 <Step id="6" name="Go to Layout" enable="True"><ParameterValues><Parameter type="LayoutReferenceContainer"><LayoutReferenceContainer><LayoutReference id="91" name="Visit"/></LayoutReferenceContainer></Parameter><Parameter type="Animation"><Animation value="0"/></Parameter></ParameterValues></Step>
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
		t.Fatal("unimplemented native animation option was hidden")
	}
}

func TestBinaryCalculationCannotBecomeText(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog><Field id="1" name="Attachment" dataType="Binary" fieldType="Calculation"><Calculation><Text>"unavailable"</Text></Calculation></Field></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`})
	if err != nil {
		t.Fatal(err)
	}
	column := result.Bundle.Tables[0].Columns[1]
	if column.FieldType != "CONTAINER" || column.CalculationFormula != nil || result.Report.Counts.UnsupportedCount == 0 {
		t.Fatal("binary calculation was misreported as a supported text calculation")
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
