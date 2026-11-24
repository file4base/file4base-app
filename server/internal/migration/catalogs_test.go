package migration

import (
	"fmt"
	"testing"
)

// Original synthetic fixtures: references may carry the same names and IDs as
// definitions, but must never add objects or change what graph edges point at.
func TestCatalogDefinitionsIgnoreReferences(t *testing.T) {
	definitions := `<BaseTableCatalog><BaseTable id="1" name="Visits"><FieldCatalog><Field id="1" name="Code" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog>
	<RelationshipGraph><TableList><Table name="VisitAlias" baseTable="Visits" baseTableId="1"/></TableList>
	<RelationshipList><Relationship id="1"><LeftTable name="VisitAlias"/><RightTable name="VisitAlias"/><JoinPredicateList><JoinPredicate type="Equal"><LeftField><Field id="1" name="Code"/></LeftField><RightField><Field id="1" name="Code"/></RightField></JoinPredicate></JoinPredicateList></Relationship></RelationshipList></RelationshipGraph>
	<ScriptCatalog><Group><Group><Script name="Create"><StepList><Step name="New Record/Request"/></StepList></Script></Group></Group></ScriptCatalog>
	<LayoutCatalog><Group><Layout name="Visit Detail" table="VisitAlias"/></Group></LayoutCatalog>
	<ValueListCatalog><ValueList name="State"><Value>Open</Value></ValueList></ValueListCatalog>`
	references := `<Options><OnOpen><Layout name="Visit Detail"/></OnOpen><WindowTriggers><OnFirstWindowOpen><Script name="Create"/></OnFirstWindowOpen></WindowTriggers></Options>
	<PrivilegesCatalog><PrivilegeSet name="Staff"><Records><TableList><BaseTable id="1" name="Visits"/></TableList></Records><Layouts><LayoutList><Layout name="Visit Detail"/></LayoutList></Layouts><Scripts><ScriptList><Script name="Create"/></ScriptList></Scripts></PrivilegeSet></PrivilegesCatalog>
	<LayoutCatalog><Group><Layout name="References" table="VisitAlias"><Object><ValueList name="State"/><ScriptTriggers><Trigger><Script name="Create"/></Trigger></ScriptTriggers></Object></Layout></Group></LayoutCatalog>`
	for _, order := range []string{definitions + references, references + definitions} {
		got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport><File name="Synthetic.fmp12">` + order + `</File></FMPReport>`})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Bundle.Tables) != 1 || len(got.Bundle.Scripts) != 1 || len(got.Bundle.Layouts) != 2 || len(got.Bundle.Relationships) != 1 || len(got.Bundle.ValueLists) != 1 {
			t.Fatalf("references changed definition identity: %+v", got.Report.Counts)
		}
		if len(got.Bundle.Scripts[0].Steps) != 1 || got.Bundle.Scripts[0].Steps[0].StepType != "New Record/Request" {
			t.Fatal("grouped script contents lost")
		}
	}
}

func TestNativeGraphUsesSourceIDs(t *testing.T) {
	base := `<BaseTableCatalog><BaseTable id="7" name="Visits"><FieldCatalog><Field id="1" name="Code" dataType="Text"/><Field id="2" name="code" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog>`
	for _, external := range []bool{false, true} {
		ref := ""
		if external {
			ref = `<FileReference name="Peer.fmp12" id="1"/>`
		}
		doc := `<FMPReport><File>` + base + `<RelationshipGraph><TableList><Table name="Alias" baseTable="OutdatedName" baseTableId="7">` + ref + `</Table></TableList><RelationshipList><Relationship id="1"><LeftTable name="Alias"/><RightTable name="Alias"/><JoinPredicateList><JoinPredicate type="Equal"><LeftField><Field id="1" name="code"/></LeftField><RightField><Field id="2" name="Code"/></RightField></JoinPredicate></JoinPredicateList></Relationship></RelationshipList></RelationshipGraph></File></FMPReport>`
		got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
		if err != nil {
			t.Fatal(err)
		}
		if external {
			if len(got.Bundle.Relationships) != 0 {
				t.Fatal("external table ID substituted with a same-ID local table")
			}
			continue
		}
		if len(got.Bundle.Relationships) != 1 {
			t.Fatalf("source identity was not resolved: %+v", got.Report.Rejected)
		}
		rel := got.Bundle.Relationships[0]
		if rel.LeftColumnID != got.Bundle.Tables[0].Columns[1].ID || rel.RightColumnID != got.Bundle.Tables[0].Columns[2].ID {
			t.Fatal("join was rebound by spelling rather than source field ID")
		}
	}
}

func TestRecordMigrationUsesCompleteFieldMap(t *testing.T) {
	data := `<FMPXMLRESULT><METADATA><FIELD NAME="Code" TYPE="TEXT"/><FIELD NAME="code" TYPE="TEXT"/><FIELD NAME="id" TYPE="TEXT"/></METADATA><RESULTSET FOUND="1"><ROW RECORDID="1"><COL><DATA>first</DATA></COL><COL><DATA>second</DATA></COL><COL><DATA>business-key</DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`
	design := `<FMPReport><File><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text"/><Field name="code" dataType="Text"/><Field name="id" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Visits": data}})
	if err != nil {
		t.Fatal(err)
	}
	row := got.TableRows["visits"][0]
	if row["code"] != "first" || row["code_2"] != "second" || row["id_2"] != "business-key" || row["id"] != nil {
		t.Fatalf("source values changed columns: %+v", row)
	}
}

func TestNamedJoinKeepsCollidingFieldsDistinct(t *testing.T) {
	doc := `<FMPReport><File><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text"/><Field name="code" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog>
	<RelationshipCatalog><Relationship name="Both" left="Visits" left_field="Code" right="Visits" right_field="code"/></RelationshipCatalog></File></FMPReport>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bundle.Relationships) != 1 {
		t.Fatal("named join not imported")
	}
	rel := got.Bundle.Relationships[0]
	if rel.LeftColumnID != got.Bundle.Tables[0].Columns[1].ID || rel.RightColumnID != got.Bundle.Tables[0].Columns[2].ID {
		t.Fatal("case-folded aliases overwrote destination column identities")
	}
}

func TestRecordMigrationPreservesWhitespaceOnlyText(t *testing.T) {
	design := `<FMPReport><File><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Notes" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	data := `<FMPXMLRESULT><METADATA><FIELD NAME="Notes" TYPE="TEXT"/></METADATA><RESULTSET FOUND="1"><ROW><COL><DATA>   </DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Visits": data}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TableRows["visits"]) != 1 || got.TableRows["visits"][0]["notes"] != "   " {
		t.Fatal("whitespace-only text was replaced with an empty value")
	}
}

func TestDynamicValueListUsesSourceFieldMap(t *testing.T) {
	doc := `<FMPReport><File><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="id" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog><ValueListCatalog><ValueList name="Business keys" kind="field" table="Visits" field="id"/></ValueListCatalog></File></FMPReport>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bundle.ValueLists) != 1 || *got.Bundle.ValueLists[0].SourceColumnID != got.Bundle.Tables[0].Columns[1].ID {
		t.Fatal("value list bound to generated primary key instead of source field")
	}
}

func TestMissingExplicitLayoutOccurrenceIsNotSubstituted(t *testing.T) {
	for _, target := range []string{`<Layout name="Peer records" table="MissingPeer"/>`, `<Layout name="Peer records"><Table name=""/></Layout>`} {
		doc := `<FMPReport><File><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog><LayoutCatalog>` + target + `</LayoutCatalog></File></FMPReport>`
		got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Bundle.Layouts) != 0 || got.Report.Counts.RejectedCount != 1 {
			t.Fatal("layout silently bound to unrelated local records")
		}
	}
}

func TestQuarantinedInstructionKeepsSourceIdentity(t *testing.T) {
	doc := `<FMPReport><File><ScriptCatalog><Script name="Unsupported"><StepList><Step id="98765" name="Unknown operation" enable="True"><Calculation><Text>42</Text></Calculation></Step></StepList></Script></ScriptCatalog></File></FMPReport>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	step := got.Bundle.Scripts[0].Steps[0]
	if step.IsEnabled || step.Params["filemaker_step_id"] != "98765" || step.Params["filemaker_step_enabled"] != true || step.Params["filemaker_parameters"] == nil {
		t.Fatal("quarantine lost original instruction identity or operands")
	}
}

func TestFrenchHTMLParameterizedInstructions(t *testing.T) {
	for _, tc := range []struct{ line, key, value string }{
		{`Définir variable [ $$Batch ; Valeur: "ready" ]`, "name", "$$Batch"},
		{`Définir variable [ $$Batch ; Valeur: "ready" ]`, "value", `"ready"`},
		{`Si [ $count = 2 ]`, "condition", "$count = 2"},
		{`Sinon si [ $count = 3 ]`, "condition", "$count = 3"},
		{`Atteindre la mise en page [ "Visit Detail" ]`, "layout_name", "Visit Detail"},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.line, tc.key), func(t *testing.T) {
			_, params := parseHTMLScriptStepLine(tc.line)
			if params[tc.key] != tc.value {
				t.Fatalf("lost localized operand: %+v", params)
			}
		})
	}
}

func TestNativeFrenchControlFlowNames(t *testing.T) {
	// Names are verified against Claris FileMaker Pro Help's French pages for
	// go-to-layout, exit-script, halt-script, exit-loop-if and comment.
	doc := `<FMPReport><File><ScriptCatalog><Script name="Control"><StepList>
	<Step id="6" name="Activer modèle"><Layout name="Visit Detail"/></Step>
	<Step name="Fin de boucle si"><Calculation><Text>$count = 2</Text></Calculation></Step>
	<Step name="Fin de script"><Calculation><Text>42</Text></Calculation></Step>
	<Step name="Interrompre script"/>
	<Step name="# (commentaire)"><Text>Original synthetic note</Text></Step>
	</StepList></Script></ScriptCatalog></File></FMPReport>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Go to Layout", "Exit Loop If", "Exit Script", "Halt Script", "Comment"}
	for i, step := range got.Bundle.Scripts[0].Steps {
		if step.StepType != want[i] || !step.IsEnabled {
			t.Fatalf("French control-flow step %d quarantined or misidentified", i)
		}
	}
	steps := got.Bundle.Scripts[0].Steps
	if steps[0].Params["layout_name"] != "Visit Detail" || steps[1].Params["condition"] != "$count = 2" || steps[2].Params["result"] != "42" {
		t.Fatal("control-flow operands lost")
	}
}
