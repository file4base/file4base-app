package migration

import (
	"strings"
	"testing"
)

// The five steps added together: Insert Current Date, Open File, Select All,
// Refresh Portal and Set Error Logging resolve by instruction id whatever
// language the report names them in, and convert as runnable steps.
func TestAddedStepsResolveByInstructionID(t *testing.T) {
	cases := []struct{ id, english string }{
		{"13", "Insert Current Date"},
		{"33", "Open File"},
		{"50", "Select All"},
		{"180", "Refresh Portal"},
		{"200", "Set Error Logging"},
	}
	for _, c := range cases {
		for _, written := range []string{c.english, "Libellé localisé quelconque"} {
			got := canonicalScriptStep(written, c.id)
			if got.Canonical != c.english || !got.Supported || got.Conflict {
				t.Errorf("%q id %s: got %+v, want %q supported", written, c.id, got, c.english)
			}
		}
		if canonical, supported, _ := canonicalScriptStepName(c.english); canonical != c.english || !supported {
			t.Errorf("%q by name: canonical=%q supported=%v", c.english, canonical, supported)
		}
	}
}

func TestAddedStepsDecodeFromDatabaseDesignReport(t *testing.T) {
	const ddr = `<FMPReport><File name="Synthetic.fmp12">
<BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog>
  <Field id="1" name="Status" dataType="Text"/>
  <Field id="2" name="Visit Date" dataType="Date"/>
</FieldCatalog></BaseTable></BaseTableCatalog>
<ScriptCatalog><Script name="Workflow"><StepList>
  <Step id="200" name="Set Error Logging" enable="True"><Set state="True"/><Calculation><Text>"row " &amp; $i</Text></Calculation></Step>
  <Step id="13" name="Insert Current Date" enable="True"><SelectAll state="True"/><Field table="Visits" id="2" name="Visit Date"/></Step>
  <Step id="13" name="Insert Current Date" enable="True"><SelectAll state="False"/><Field name="$today"/></Step>
  <Step id="50" name="Select All" enable="True"/>
  <Step id="180" name="Refresh Portal" enable="True"><ObjectName value="Visit Lines"/></Step>
  <Step id="180" name="Refresh Portal" enable="True"><Object><Calculation><Text>$portal</Text></Calculation></Object></Step>
  <Step id="33" name="Open File" enable="True"><Option state="True"/><FileReference id="1" name="Remote Archive"/></Step>
  <Step id="200" name="Set Error Logging" enable="True"><Set state="False"/></Step>
</StepList></Script></ScriptCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: ddr})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Counts.UnsupportedCount != 0 {
		t.Fatalf("expected 0 unsupported steps, got %d: %+v", res.Report.Counts.UnsupportedCount, res.Report.Unsupported)
	}
	for _, m := range res.Report.ManualActionRequired {
		// The one expected item asks for the new file reference to be mapped.
		if m.Domain == "script" && m.SourceName != "Remote Archive" {
			t.Errorf("unexpected script manual_action_required: %+v", m)
		}
	}
	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 8 {
		t.Fatalf("expected 8 steps, got %d", len(steps))
	}
	want := []string{
		"Set Error Logging", "Insert Current Date", "Insert Current Date", "Select All",
		"Refresh Portal", "Refresh Portal", "Open File", "Set Error Logging",
	}
	for i, w := range want {
		if steps[i].StepType != w {
			t.Fatalf("step %d: type %q, want %q", i, steps[i].StepType, w)
		}
	}
	if p := steps[0].Params; p["state"] != true || p["custom_debug_info"] != `"row " & $i` {
		t.Fatalf("Set Error Logging [On]: %+v", p)
	}
	if p := steps[1].Params; p["field"] != "visit_date" || p["select"] != true {
		t.Fatalf("Insert Current Date into a field: %+v", p)
	}
	if p := steps[2].Params; p["name"] != "$today" || p["select"] != false {
		t.Fatalf("Insert Current Date into a variable: %+v", p)
	}
	if p := steps[4].Params; p["object_name"] != "Visit Lines" || p["object_name_is_calculation"] == true {
		t.Fatalf("Refresh Portal by name: %+v", p)
	}
	if p := steps[5].Params; p["object_name"] != "$portal" || p["object_name_is_calculation"] != true {
		t.Fatalf("Refresh Portal by calculation: %+v", p)
	}
	if p := steps[6].Params; p["file_reference"] != "Remote Archive" || p["hidden"] != true {
		t.Fatalf("Open File: %+v", p)
	}
	if _, ok := steps[6].Params["skip_validation"]; ok {
		t.Fatalf("Open File's Option was read as a commit option: %+v", steps[6].Params)
	}
	if p := steps[7].Params; p["state"] != false {
		t.Fatalf("Set Error Logging [Off]: %+v", p)
	}

	// Open File's file is a reference to map, exactly like Perform Script's.
	if len(res.Bundle.FileReferences) != 1 || res.Bundle.FileReferences[0].Name != "Remote Archive" {
		t.Fatalf("expected one unmapped file reference, got %+v", res.Bundle.FileReferences)
	}
	found := false
	for _, m := range res.Report.ManualActionRequired {
		if m.SourceName == "Remote Archive" && strings.Contains(m.Detail, "Workflow (step") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the Open File reference was not reported for mapping: %+v", res.Report.ManualActionRequired)
	}
}

func TestAddedStepsDecodeFromSaveAsXML(t *testing.T) {
	const design = `<FMSaveAsXML><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="2" name="Visit Date" datatype="Date" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <ScriptCatalog><Script id="81" name="Start"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Start"/><ObjectList>
 <Step id="200" name="Set Error Logging" enable="True"><ParameterValues><Parameter type="Boolean"><Boolean type="Logging" value="True"/></Parameter><Parameter type="Calculation"><Calculation><Text>$i</Text></Calculation></Parameter></ParameterValues></Step>
 <Step id="13" name="Insert Current Date" enable="True"><ParameterValues><Parameter type="Boolean"><Boolean type="Select entire contents" value="True"/></Parameter><Parameter type="Target"><FieldReference id="2" name="Visit Date"><TableOccurrenceReference id="1" name="Visits"/></FieldReference></Parameter></ParameterValues></Step>
 <Step id="180" name="Refresh Portal" enable="True"><ParameterValues><Parameter type="Object"><Object value="Visit Lines"/></Parameter></ParameterValues></Step>
 <Step id="33" name="Open File" enable="True"><ParameterValues><Parameter type="Boolean"><Boolean type="Open hidden" value="False"/></Parameter><Parameter type="DataSourceReference"><DataSourceReference id="1" name="Remote Archive"/></Parameter></ParameterValues></Step>
 <Step id="50" name="Select All" enable="True"/>
 </ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Counts.UnsupportedCount != 0 {
		t.Fatalf("expected 0 unsupported steps, got %+v", res.Report.Unsupported)
	}
	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(steps))
	}
	if p := steps[0].Params; steps[0].StepType != "Set Error Logging" || p["state"] != true || p["custom_debug_info"] != "$i" {
		t.Fatalf("Set Error Logging: %s %+v", steps[0].StepType, p)
	}
	if _, ok := steps[0].Params["value_is_calculation"]; ok {
		t.Fatalf("Set Error Logging's debug info was read as a value to write: %+v", steps[0].Params)
	}
	if p := steps[1].Params; steps[1].StepType != "Insert Current Date" || p["field"] != "visit_date" || p["select"] != true {
		t.Fatalf("Insert Current Date: %s %+v", steps[1].StepType, p)
	}
	if p := steps[2].Params; steps[2].StepType != "Refresh Portal" || p["object_name"] != "Visit Lines" {
		t.Fatalf("Refresh Portal: %s %+v", steps[2].StepType, p)
	}
	if p := steps[3].Params; steps[3].StepType != "Open File" || p["file_reference"] != "Remote Archive" || p["hidden"] != false {
		t.Fatalf("Open File: %s %+v", steps[3].StepType, p)
	}
	if _, ok := steps[3].Params["auto_open"]; ok {
		t.Fatalf("Open hidden was read as another option: %+v", steps[3].Params)
	}
	if steps[4].StepType != "Select All" {
		t.Fatalf("Select All: %s", steps[4].StepType)
	}
	for _, m := range res.Report.ManualActionRequired {
		if m.Domain == "script" && m.SourceName != "Remote Archive" {
			t.Errorf("unexpected script manual_action_required: %+v", m)
		}
	}
}

// Audit of the first version: the DDR writes Set Error Logging's state as
// <Option state="…"/> and a variable target as <Field>$name</Field>; a target
// that cannot be read must not leave the step free to write the active field.
func TestAddedStepsDDROptionAndTargetForms(t *testing.T) {
	const ddr = `<FMPReport><File name="Synthetic.fmp12">
<BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog><Field id="1" name="Status" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog>
<ScriptCatalog><Script name="Workflow"><StepList>
  <Step id="200" name="Set Error Logging" enable="True"><Option state="True"/><Calculation><Text>$i</Text></Calculation></Step>
  <Step id="200" name="Set Error Logging" enable="True"><Option state="False"/></Step>
  <Step id="13" name="Insert Current Date" enable="True"><SelectAll state="True"/><Field>$today</Field></Step>
  <Step id="13" name="Insert Current Date" enable="True"><SelectAll state="True"/><Field/></Step>
</StepList></Script></ScriptCatalog></File></FMPReport>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: ddr})
	if err != nil {
		t.Fatal(err)
	}
	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(steps))
	}
	if p := steps[0].Params; p["state"] != true || p["custom_debug_info"] != "$i" {
		t.Fatalf("Set Error Logging [On]: %+v", p)
	}
	if p := steps[1].Params; p["state"] != false {
		t.Fatalf("Set Error Logging [Off] must stay Off: %+v", p)
	}
	if _, ok := steps[1].Params["option"]; ok {
		t.Fatalf("the logging state was kept as a generic option: %+v", steps[1].Params)
	}
	if p := steps[2].Params; p["name"] != "$today" || p["target_unresolved"] == true {
		t.Fatalf("variable target in element text: %+v", p)
	}
	if p := steps[3].Params; p["target_unresolved"] != true {
		t.Fatalf("an unreadable target must be marked: %+v", p)
	}
	reported := false
	for _, m := range res.Report.ManualActionRequired {
		if m.Domain == "script" && strings.Contains(m.Detail, "Field") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("the unreadable target was not reported: %+v", res.Report.ManualActionRequired)
	}
}
