package migration

import "testing"

// Save as XML writes Go to Related Record as one Related parameter holding
// the occurrence, the layout, the options and the new window: each is read
// (#237). An option left out is off.
func TestGoToRelatedRecordDecodesFromSaveAsXML(t *testing.T) {
	const design = `<FMSaveAsXML><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="2" name="Visit Date" datatype="Date" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <ScriptCatalog><Script id="81" name="Open Line"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Open Line"/><ObjectList>
 <Step id="74" name="Go to Related Record" enable="True"><ParameterValues membercount="1"><Parameter type="Related"><TableOccurrenceReference id="3" name="Visit Lines" UUID="u1"/><LayoutReferenceContainer value="5"><LayoutReference id="7" name="Line Detail" UUID="u2"/></LayoutReferenceContainer><Animation name="None" value="0"/><Options ShowRelated="True"/></Parameter></ParameterValues></Step>
 <Step id="74" name="Go to Related Record" enable="True"><ParameterValues membercount="1"><Parameter type="Related"><TableOccurrenceReference id="3" name="Visit Lines" UUID="u1"/><LayoutReferenceContainer value="5"/><Options ShowRelated="True" matchFoundSet="True"/><WindowReference><Name/></WindowReference></Parameter></ParameterValues></Step>
 <Step id="74" name="Go to Related Record" enable="True"><ParameterValues membercount="1"><Parameter type="Related"><TableOccurrenceReference id="3" name="Visit Lines" UUID="u1"/><LayoutReferenceContainer value="5"><LayoutReference id="7" name="Line Detail" UUID="u2"/></LayoutReferenceContainer><Options/></Parameter></ParameterValues></Step>
 </ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	for i, s := range steps {
		if s.StepType != "Go to Related Record" || s.Params["related_table"] != "Visit Lines" {
			t.Fatalf("step %d: %s %+v", i, s.StepType, s.Params)
		}
	}
	if p := steps[0].Params; p["layout_name"] != "Line Detail" || p["show_only_related"] != true ||
		p["match_found_set"] != nil || p["new_window"] != nil {
		t.Fatalf("show only related, on a layout: %+v", p)
	}
	if p := steps[1].Params; p["layout_name"] != nil || p["show_only_related"] != true ||
		p["match_found_set"] != true || p["new_window"] != true {
		t.Fatalf("matching the found set, in a new window, on the current layout: %+v", p)
	}
	if p := steps[2].Params; p["show_only_related"] != false || p["layout_name"] != "Line Detail" {
		t.Fatalf("without Show only related records: %+v", p)
	}
	if res.Report.Counts.UnsupportedCount != 0 {
		t.Fatalf("unexpected unsupported steps: %+v", res.Report.Unsupported)
	}
}
