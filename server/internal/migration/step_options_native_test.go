package migration

import (
	"strings"
	"testing"
)

// Synthetic design in the Save a Copy as XML wrapper grammar, with the step
// options File4Base used to leave unread. No source export is reproduced.
const stepOptionsDesign = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Total" datatype="Number" fieldtype="Normal"/>
  <Field id="3" name="Note" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog><TableOccurrence id="7" name="Open Orders"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence></TableOccurrenceCatalog>
 <ScriptCatalog><Script id="81" name="Steps"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Steps"/><ObjectList>
 <Step id="35" name="Import Records" enable="True"><ParameterValues>
  <Parameter type="Boolean"><Boolean type="Verify SSL Certificates" value="False"/></Parameter>
  <Parameter type="Boolean"><Boolean type="With dialog" value="False"/></Parameter>
  <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Ledger.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
  <Parameter type="ImportField"><ImportField addasnew="True" delimiter="0">
   <Options keepOriginalData="False" copyContainersAsIs="False" matchFieldNames="False" doAutoEntry="True" splitRepetitions="False"/>
   <Target><TableOccurrenceReference id="7" name="Open Orders"/></Target>
   <Field membercount="3">
    <Map New="False" name="Source field 1 match with " index="1" id="1" kind="2" Options="0"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Open Orders"/></FieldReference></Map>
    <Map New="False" name="Source field 2 import to " index="2" id="2" kind="0" Options="0"><FieldReference id="2" name="Total"><TableOccurrenceReference id="7" name="Open Orders"/></FieldReference></Map>
    <Map New="False" name="Source field 3 not import to " index="3" id="3" kind="1" Options="0"><FieldReference id="3" name="Note"><TableOccurrenceReference id="7" name="Open Orders"/></FieldReference></Map>
   </Field>
   <CharacterSet name="UTF-8" value="5"/>
   <action name="Update" value="2"/>
  </ImportField></Parameter>
 </ParameterValues></Step>
 <Step id="35" name="Import Records" enable="True"><ParameterValues>
  <Parameter type="DataSourceReference"><DataSourceReference fileType="TABS" value="1"><UniversalPathList fileType="TABS"><ObjectList><Location>$path</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
  <Parameter type="ImportField"><ImportField addasnew="False"><Options doAutoEntry="False"/><Target><TableOccurrenceReference id="7" name="Open Orders"/></Target>
   <Field><Map name="Source field 1 import to " index="1" kind="0"><FieldReference id="3" name="Note"/></Map></Field>
   <action name="Add" value="0"/></ImportField></Parameter>
 </ParameterValues></Step>
 <Step id="1" name="Perform Script" enable="True"><ParameterValues><Parameter type="List"><List name="From list" value="1"></List></Parameter><Parameter type="Parameter"><Parameter/></Parameter></ParameterValues></Step>
 <Step id="1" name="Perform Script" enable="True"><ParameterValues><Parameter type="List"><List name="From list" value="1"><DataSourceReference id="2" name="Ledger"/></List></Parameter><Parameter type="Parameter"><Parameter/></Parameter></ParameterValues></Step>
 <Step id="63" name="Send Mail" enable="True"><ParameterValues><Parameter type="Email">
  <Boolean type="With dialog" value="True"/>
  <Send SMTP="False" OAuthAuthentication="False">
   <To><CollectAddresses value="False"/><Calculation><Calculation><Text>"team@example.invalid"</Text></Calculation></Calculation></To>
   <CC><CollectAddresses value="False"/></CC><BCC><CollectAddresses value="False"/></BCC>
   <Subject><Calculation><Calculation><Text>"Weekly"</Text></Calculation></Calculation></Subject>
   <Message><Calculation><Calculation><Text>$body</Text></Calculation></Calculation></Message>
   <Multiple value="False"/>
  </Send></Parameter></ParameterValues></Step>
 <Step id="63" name="Send Mail" enable="True"><ParameterValues><Parameter type="Email">
  <Send SMTP="True"><To><CollectAddresses value="False"/><Calculation><Text>"a@example.invalid"</Text></Calculation></To><Multiple value="False"/></Send>
  <UniversalPathList membercount="1"><ObjectList><Location>$report</Location></ObjectList></UniversalPathList>
 </Parameter></ParameterValues></Step>
 <Step id="144" name="Save Records as PDF" enable="True"><ParameterValues>
  <Parameter type="Restore"><Restore value="True"/></Parameter>
  <Parameter type="Options"><Options type="Current record" value="2"/></Parameter>
  <Parameter type="Boolean"><Boolean type="With dialog" value="False"/></Parameter>
  <Parameter type="Boolean"><Boolean type="Append to existing PDF" value="False"/></Parameter>
  <Parameter type="UniversalPathList"><UniversalPathList CreateMail="True" membercount="1"><ObjectList><Location>$out</Location></ObjectList></UniversalPathList></Parameter>
  <Parameter type="SaveResult"><List name="File" value="0"/></Parameter>
 </ParameterValues></Step>
 <Step id="99" name="Go to Portal Row" enable="True"><ParameterValues>
  <Parameter type="Boolean"><Boolean type="Select" value="True"/></Parameter>
  <Parameter type="Portal"><List name="Last" value="2"/></Parameter>
 </ParameterValues></Step>
 <Step id="99" name="Go to Portal Row" enable="True"><ParameterValues>
  <Parameter type="Portal"><List name="By Calculation…" value="5"><Boolean type="With dialog" value="False"/><Calculation><Calculation><Text>$row</Text></Calculation></Calculation></List></Parameter>
 </ParameterValues></Step>
 <Step id="16" name="Go to Record/Request/Page" enable="True"><ParameterValues>
  <Parameter type="Records"><List name="Next" value="4"><Boolean type="Exit after last" value="True"/></List></Parameter>
 </ParameterValues></Step>
 <Step id="91" name="Replace Field Contents" enable="True"><ParameterValues>
  <Parameter type="Boolean"><Boolean type="With dialog" value="False"/></Parameter>
  <Parameter type="FieldReference"><FieldReference id="2" name="Total"><TableOccurrenceReference id="7" name="Open Orders"/></FieldReference></Parameter>
  <Parameter type="replace"><List name="Replace with calculation: " value="3"><Boolean type="Skip auto-enter options" value="False"/><Calculation><Calculation><Text>Total * 2</Text></Calculation></Calculation></List></Parameter>
 </ParameterValues></Step>
 <Step id="87" name="Show Custom Dialog" enable="True"><ParameterValues>
  <Parameter type="Title"><Calculation><Text>"Code"</Text></Calculation></Parameter>
  <Parameter type="Button1" value="OK"><Boolean type="Commit" value="True"/></Parameter>
  <Parameter type="Button2" value="Cancel"><Boolean type="Commit" value="False"/></Parameter>
  <Parameter type="Button3"><Boolean type="Commit" value="False"/></Parameter>
  <Parameter type="Field1"><Boolean type="Password" value="False"/>
   <Parameter type="Target"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Open Orders"/></FieldReference></Parameter>
   <Parameter type="Label"><Calculation><Calculation><Text>"Order code"</Text></Calculation></Calculation></Parameter></Parameter>
  <Parameter type="Field2"><Boolean type="Password" value="True"/>
   <Parameter type="Target"><Variable value="$secret"/></Parameter></Parameter>
 </ParameterValues></Step>
 </ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`

func TestStepOptionsAreRead(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: stepOptionsDesign})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Report.ManualActionRequired {
		if strings.Contains(item.Detail, "does not read") {
			t.Errorf("an option is still unread: %s", item.Detail)
		}
	}
	steps := result.Bundle.Scripts[0].Steps
	if len(steps) != 12 {
		t.Fatalf("steps = %d", len(steps))
	}

	t.Run("Import Records from a FileMaker file", func(t *testing.T) {
		p := steps[0].Params
		src, _ := p["import_source"].(map[string]interface{})
		if src["kind"] != "filemaker" || src["file"] != "Ledger" {
			t.Fatalf("source = %#v", src)
		}
		if p["import_method"] != "update" || p["add_remaining"] != true || p["auto_enter"] != true ||
			p["target_table"] != "orders" || p["character_set"] != "UTF-8" {
			t.Fatalf("options = %#v", p)
		}
		maps, _ := p["field_mappings"].([]map[string]interface{})
		if len(maps) != 2 || maps[0]["action"] != "match" || maps[0]["target_field"] != "code" ||
			maps[0]["source_position"] != 1 || maps[1]["action"] != "import" || maps[1]["target_field"] != "total" {
			t.Fatalf("mappings = %#v", maps)
		}
		if _, named := maps[0]["source_field"]; named {
			t.Fatal("a position was taken for a field name")
		}
		note, _ := p["migration_note"].(string)
		for _, want := range []string{`"Ledger"`, "into orders", "match on code", "adding the others",
			"source field 1 -> code", "source field 2 -> total", "without guessing"} {
			if !strings.Contains(note, want) {
				t.Errorf("note lacks %q: %s", want, note)
			}
		}
		if !reported(result.Report, "Steps > Import Records (step 1)", "without guessing") {
			t.Error("the step's report item does not say why it stays manual")
		}
		if reported(result.Report, "Steps > Import Records (step 1)", "runs a File4Base transfer definition") {
			t.Error("the generic transfer item was kept beside the specific one")
		}
	})

	t.Run("Import Records from a file on disk", func(t *testing.T) {
		p := steps[1].Params
		src, _ := p["import_source"].(map[string]interface{})
		if src["kind"] != "file" || src["path_is_calculation"] != true || p["import_method"] != "add" {
			t.Fatalf("params = %#v", p)
		}
		if note, _ := p["migration_note"].(string); !strings.Contains(note, "never read files from a disk") {
			t.Fatalf("note = %q", note)
		}
	})

	t.Run("Perform Script of a deleted or unnamed script", func(t *testing.T) {
		if steps[2].Params["script_missing"] != true || !strings.Contains(steps[2].Params["migration_note"].(string), "error 104") {
			t.Fatalf("missing script = %#v", steps[2].Params)
		}
		p := steps[3].Params
		if p["script_unresolved"] != true || p["file_reference"] != "Ledger" {
			t.Fatalf("unresolved script = %#v", p)
		}
		found := false
		for _, ref := range result.Bundle.FileReferences {
			found = found || ref.Name == "Ledger"
		}
		if !found {
			t.Fatal("the file the script belongs to has no file reference")
		}
	})

	t.Run("Send Mail", func(t *testing.T) {
		p := steps[4].Params
		if p["to"] != `"team@example.invalid"` || p["to_is_calculation"] != true || p["subject"] != `"Weekly"` ||
			p["message"] != "$body" || p["send_via"] != "email_client" || p["dialog"] != true || p["one_email_per_record"] != false {
			t.Fatalf("params = %#v", p)
		}
		if note, _ := p["migration_note"].(string); !strings.Contains(note, "opens a draft") ||
			!strings.Contains(note, "does not send unattended") || strings.Contains(note, "cannot run") {
			t.Fatalf("note = %q", note)
		}
		note, _ := steps[5].Params["migration_note"].(string)
		if !strings.Contains(note, "SMTP") || !strings.Contains(note, "attaches files") {
			t.Fatalf("note = %q", note)
		}
	})

	t.Run("Save Records as PDF", func(t *testing.T) {
		p := steps[6].Params
		if p["save_to"] != "file" || p["records"] != "current" || p["path"] != "$out" || p["create_email"] != true {
			t.Fatalf("params = %#v", p)
		}
		if note, _ := p["migration_note"].(string); !strings.Contains(note, "error 3") {
			t.Fatalf("note = %q", note)
		}
	})

	t.Run("Go to Portal Row and Go to Record targets", func(t *testing.T) {
		if steps[7].Params["target"] != "last" || steps[7].Params["select"] != true {
			t.Fatalf("portal row = %#v", steps[7].Params)
		}
		if steps[8].Params["target"] != "by_calculation" || steps[8].Params["record_calculation"] != "$row" {
			t.Fatalf("portal row by calculation = %#v", steps[8].Params)
		}
		// File4Base moves to a portal row in Browse mode: the step runs as
		// written, with no note.
		for _, step := range steps[7:9] {
			if note, ok := step.Params["migration_note"]; ok {
				t.Fatalf("note = %q", note)
			}
		}
		// The code in value="4" is not record 4.
		if steps[9].Params["target"] != "next" || steps[9].Params["exit_after_last"] != true {
			t.Fatalf("go to record = %#v", steps[9].Params)
		}
	})

	t.Run("Replace Field Contents", func(t *testing.T) {
		p := steps[10].Params
		if p["replace_type"] != "calculation" || p["value"] != "Total * 2" || p["value_is_calculation"] != true ||
			p["field"] != "total" || p["skip_auto_enter"] != false {
			t.Fatalf("params = %#v", p)
		}
	})

	t.Run("Show Custom Dialog buttons", func(t *testing.T) {
		// A Cancel button does not commit the input fields, and the empty
		// third button is not shown.
		buttons, _ := steps[11].Params["buttons"].([]map[string]interface{})
		if len(buttons) != 2 || buttons[0]["label"] != "OK" || buttons[0]["commit"] != true ||
			buttons[1]["label"] != "Cancel" || buttons[1]["commit"] != false {
			t.Fatalf("buttons = %#v", buttons)
		}
	})

	t.Run("Show Custom Dialog input fields", func(t *testing.T) {
		inputs, _ := steps[11].Params["input_fields"].([]map[string]interface{})
		if len(inputs) != 2 || inputs[0]["field"] != "code" || inputs[0]["label"] != `"Order code"` ||
			inputs[0]["label_is_calculation"] != true || inputs[0]["password"] != false ||
			inputs[1]["field"] != "$secret" || inputs[1]["password"] != true {
			t.Fatalf("inputs = %#v", inputs)
		}
	})
}

func reported(report *ConversionReport, source, detail string) bool {
	for _, item := range report.ManualActionRequired {
		if item.SourceName == source && strings.Contains(item.Detail, detail) {
			return true
		}
	}
	return false
}

func TestFileMakerFileName(t *testing.T) {
	for path, want := range map[string]string{
		"file:Ledger":                      "Ledger",
		"file:Ledger.fmp12":                "Ledger",
		"fmnet:/host.example/Ledger.fmp12": "Ledger",
		"filemac:/Disk/dir/Ledger.fmp12":   "Ledger",
		"file:$path":                       "",
		"":                                 "",
	} {
		if got := fileMakerFileName(path); got != want {
			t.Errorf("fileMakerFileName(%q) = %q, want %q", path, got, want)
		}
	}
}
