package migration

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// triggerFixture is a handwritten <FMSaveAsXML> document with the trigger
// shapes a Save a Copy as XML export writes: an <ScriptTriggers> block on
// the layout and on a layout object, each <ScriptTrigger action=…> naming
// its script with a <ScriptReference> whose <Calculation> is the parameter,
// one whose script was deleted (no reference at all), and the file's own
// triggers in the metadata.
const triggerFixture = `<?xml version="1.0" encoding="UTF-8"?>
<FMSaveAsXML Has_DDR_INFO="True">
  <File name="Greenhouse.fmp12">
    <BaseTableCatalog>
      <BaseTable id="1" name="Plants"/>
    </BaseTableCatalog>
    <FieldsForTables>
      <FieldCatalog>
        <BaseTableSourceReference id="1" name="Plants"/>
        <Field id="1" name="_Label" datatype="text" fieldtype="normal"/>
        <Field id="2" name="Height" datatype="number" fieldtype="normal"/>
      </FieldCatalog>
    </FieldsForTables>
    <RelationshipGraph>
      <TableList>
        <Table id="1" name="Plants" baseTable="Plants" baseTableId="1"/>
      </TableList>
    </RelationshipGraph>
    <LayoutCatalog>
      <Layout id="1" name="Plant Card">
        <TableOccurrenceReference id="1" name="Plants"/>
        <LayoutObject id="7" type="Edit Box" name="">
          <Field name="Height" table="Plants"/>
          <ScriptTriggers membercount="2">
            <ScriptTrigger id="4" action="OnObjectSave" browseMode="True">
              <ScriptReference id="6" name="Water">
                <Calculation><DDRREF kind="ChunkList">_7_ScriptTrigger_4</DDRREF></Calculation>
              </ScriptReference>
            </ScriptTrigger>
            <ScriptTrigger id="5" action="OnObjectExit" browseMode="True"></ScriptTrigger>
          </ScriptTriggers>
        </LayoutObject>
        <ScriptTriggers membercount="3">
          <ScriptTrigger id="1" action="OnLayoutEnter" browseMode="True">
            <ScriptReference id="1" name="Greet">
              <Calculation><Text><![CDATA[_Label & "!"]]></Text></Calculation>
            </ScriptReference>
          </ScriptTrigger>
          <ScriptTrigger id="2" action="OnLayoutExit" browseMode="True">
            <ScriptReference id="1" name="Greet">
              <Calculation><Text><![CDATA[Elsewhere::Nothing]]></Text></Calculation>
            </ScriptReference>
          </ScriptTrigger>
          <ScriptTrigger id="3" action="OnRecordLoad" browseMode="True">
            <ScriptReference id="99" name="Gone"/>
          </ScriptTrigger>
        </ScriptTriggers>
      </Layout>
    </LayoutCatalog>
    <ScriptCatalog>
      <Script id="1" name="Greet" contextTable="Plants"/>
      <Script id="5" name="Water" contextTable="Plants"/>
      <Script id="6" name="Water" contextTable="Plants"/>
    </ScriptCatalog>
    <Metadata>
      <AddAction>
        <ScriptTriggers membercount="1">
          <ScriptTrigger id="201" action="OnFirstWindowOpen" browseMode="True">
            <ScriptReference id="1" name="Greet"/>
          </ScriptTrigger>
        </ScriptTriggers>
      </AddAction>
    </Metadata>
  </File>
</FMSaveAsXML>`

// Every trigger is kept, off, bound to the imported script and fields, and
// the report says so once instead of once per trigger (#26).
func TestScriptTriggers_KeptDisabledAndBound(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: triggerFixture})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Bundle.Layouts) != 1 {
		t.Fatalf("expected one layout, got %d", len(res.Bundle.Layouts))
	}
	def := res.Bundle.Layouts[0].Definition
	for _, slot := range schema.LayoutTriggerSlots {
		if _, ok := def[slot.Key]; ok {
			t.Fatalf("a migrated trigger must never be in the slot a client runs (%s)", slot.Key)
		}
	}
	scriptIDs := map[string][]string{}
	for _, sc := range res.Bundle.Scripts {
		scriptIDs[sc.Name] = append(scriptIDs[sc.Name], sc.ID)
	}
	list, _ := def[schema.LayoutScriptTriggersKey].([]interface{})
	if len(list) != 5 {
		t.Fatalf("expected 5 stored triggers, got %d: %v", len(list), list)
	}
	byEvent := map[string]map[string]interface{}{}
	for _, raw := range list {
		e := raw.(map[string]interface{})
		byEvent[e["event"].(string)] = e
		if e["enabled"] != false || e["quarantined_by_migration"] != true {
			t.Fatalf("every migrated trigger arrives off and marked: %v", e)
		}
		if id, _ := e["id"].(string); id == "" {
			t.Fatalf("every trigger has an id: %v", e)
		}
		if _, ok := e[sourceScriptIDKey]; ok {
			t.Fatalf("the FileMaker script id is not stored: %v", e)
		}
	}

	enter := byEvent["OnLayoutEnter"]
	if enter["script_id"] != scriptIDs["Greet"][0] {
		t.Fatalf("OnLayoutEnter is bound to the imported script: %v", enter)
	}
	if p, _ := enter["parameter"].(string); strings.Contains(p, "_Label") || !strings.Contains(p, "label") {
		t.Fatalf("the parameter names the imported field: %q", p)
	}
	if _, ok := enter["parameter_problem"]; ok {
		t.Fatalf("a parameter that reads is not a problem: %v", enter)
	}
	exit := byEvent["OnLayoutExit"]
	if why, _ := exit["parameter_problem"].(string); why == "" || exit["parameter"] != "Elsewhere::Nothing" {
		t.Fatalf("a parameter that does not read is kept as written, with why: %v", exit)
	}
	save := byEvent["OnObjectSave"]
	if save["script_id"] != scriptIDs["Water"][1] {
		t.Fatalf("a script is found by its FileMaker id before its name, as two can share a name: %v", save)
	}
	object, _ := save["object"].(map[string]interface{})
	if object["type"] != "Edit Box" || object["field"] != "Plants::Height" {
		t.Fatalf("the object a trigger is set on is described: %v", save)
	}
	if _, ok := byEvent["OnObjectExit"]["script_name"]; ok {
		t.Fatalf("a trigger whose script was deleted names none: %v", byEvent["OnObjectExit"])
	}
	if _, ok := byEvent["OnRecordLoad"]["script_id"]; ok || byEvent["OnRecordLoad"]["script_name"] != "Gone" {
		t.Fatalf("an unresolved script keeps its name and no id: %v", byEvent["OnRecordLoad"])
	}

	var triggers []ReportItem
	for _, list := range [][]ReportItem{res.Report.Preserved, res.Report.Translated, res.Report.Unsupported, res.Report.Rejected, res.Report.ManualActionRequired} {
		for _, it := range list {
			if it.Domain == "trigger" {
				triggers = append(triggers, it)
			}
		}
	}
	expect := []string{
		"5 script trigger(s) were imported disabled (3 on layouts, 2 on layout objects)",
		`The script "Gone" this trigger runs was not found`,
		`The script parameter "Elsewhere::Nothing"`,
		"1 script trigger(s) name no script",
		"1 OnObjectExit trigger(s) kept, disabled",
		"1 OnObjectSave trigger(s) kept, disabled",
		"1 OnRecordLoad trigger(s) kept, disabled",
		`1 file script trigger(s) were not imported (OnFirstWindowOpen -> "Greet")`,
	}
	if len(triggers) != len(expect) {
		t.Fatalf("expected %d trigger items, got %d: %+v", len(expect), len(triggers), triggers)
	}
	for _, want := range expect {
		found := false
		for _, it := range triggers {
			found = found || strings.Contains(it.Detail, want)
		}
		if !found {
			t.Errorf("missing report item %q in %+v", want, triggers)
		}
	}
	if !res.Report.TriggersQuarantined {
		t.Fatal("triggers are still reported quarantined")
	}
}

// A File4Base layout definition carried in the document keeps its triggers,
// switched off and moved out of the slots a client runs.
func TestScriptTriggers_ExplicitDefinitionIsQuarantined(t *testing.T) {
	def := map[string]interface{}{
		"on_layout_enter": map[string]interface{}{"type": "perform_script", "script_name": "Greet"},
		"script_triggers": []interface{}{map[string]interface{}{"event": "OnRecordLoad", "enabled": true, "script_name": "Greet"}},
	}
	quarantineDefinitionTriggers(def)
	if _, ok := def["on_layout_enter"]; ok {
		t.Fatal("the slot is emptied")
	}
	list := def["script_triggers"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("both triggers are kept: %v", list)
	}
	for _, raw := range list {
		e := raw.(map[string]interface{})
		if e["enabled"] != false || e["quarantined_by_migration"] != true || e["id"] == "" {
			t.Fatalf("kept off, marked, with an id: %v", e)
		}
	}
	if list[1].(map[string]interface{})["event"] != "OnLayoutEnter" {
		t.Fatalf("the slot's event is named: %v", list[1])
	}
}

// A trigger running a script of another FileMaker file is never bound to the
// script of this file that has the same id or name, and one naming a script
// several imported scripts share is not bound to one of them (#26).
func TestScriptTriggers_OtherFileAndAmbiguousName(t *testing.T) {
	doc := strings.Replace(triggerFixture, `<ScriptTriggers membercount="3">
          <ScriptTrigger id="1" action="OnLayoutEnter" browseMode="True">
            <ScriptReference id="1" name="Greet">`, `<ScriptTriggers membercount="3">
          <ScriptTrigger id="1" action="OnLayoutEnter" browseMode="True">
            <ScriptReference id="1" name="Greet">
              <DataSourceReference id="2" name="Seed Bank"/>`, 1)
	doc = strings.Replace(doc, `<ScriptTrigger id="2" action="OnLayoutExit" browseMode="True">
            <ScriptReference id="1" name="Greet">`, `<ScriptTrigger id="2" action="OnLayoutExit" browseMode="True">
            <ScriptReference name="Water">`, 1)
	if doc == triggerFixture {
		t.Fatal("fixture not rewritten")
	}
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	byEvent := map[string]map[string]interface{}{}
	for _, raw := range res.Bundle.Layouts[0].Definition[schema.LayoutScriptTriggersKey].([]interface{}) {
		e := raw.(map[string]interface{})
		byEvent[e["event"].(string)] = e
	}
	enter := byEvent["OnLayoutEnter"]
	if _, bound := enter["script_id"]; bound || enter["file_reference"] != "Seed Bank" || enter["script_name"] != "Greet" {
		t.Fatalf("a script of another file keeps its file and binds to nothing here: %v", enter)
	}
	if _, kept := enter[sourceScriptIDKey]; kept {
		t.Fatalf("the other file's script id is not kept: %v", enter)
	}
	exit := byEvent["OnLayoutExit"]
	if _, bound := exit["script_id"]; bound || exit["script_name"] != "Water" {
		t.Fatalf("a name two scripts share binds to neither: %v", exit)
	}
	var details []string
	for _, it := range res.Report.ManualActionRequired {
		if it.Domain == "trigger" {
			details = append(details, it.Detail)
		}
	}
	joined := strings.Join(details, "\n")
	for _, want := range []string{`"Greet" this trigger runs is in another FileMaker file (Seed Bank)`, `2 imported scripts are named "Water"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
}
