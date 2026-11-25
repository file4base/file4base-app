package migration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
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
			// The occurrence is of the other file's table 7, never of this
			// file's table 7: it is migrated unbound, and its join names
			// the other file's fields instead of pointing at local ones.
			var occ *schema.BundleOccurrence
			for i := range got.Bundle.TableOccurrences {
				if got.Bundle.TableOccurrences[i].Name == "Alias" {
					occ = &got.Bundle.TableOccurrences[i]
				}
			}
			if occ == nil || occ.External == nil || occ.BaseTableID != "" {
				t.Fatalf("external table ID substituted with a same-ID local table: %+v", occ)
			}
			if occ.External.FileReference != "Peer.fmp12" || occ.External.Table != "outdatedname" {
				t.Fatalf("external occurrence: %+v", occ.External)
			}
			if len(got.Bundle.Relationships) != 1 {
				t.Fatalf("the join through the external occurrence was not migrated: %+v", got.Report.Rejected)
			}
			rel := got.Bundle.Relationships[0]
			if rel.LeftColumnID != "" || rel.RightColumnID != "" || rel.LeftColumnName != "code" || rel.RightColumnName != "code" {
				t.Fatalf("external join must name the other file's fields: %+v", rel)
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

// Save a Copy as XML writes a custom list as one <Text>, one value per line
// with carriage returns, and a field list as <PrimaryField><FieldReference>
// on a table occurrence; options File4Base cannot reproduce are reported.
func TestSaveAsXMLValueLists(t *testing.T) {
	doc := `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Colours"/><BaseTable id="130" name="Shades"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Colours"/><ObjectList><Field id="1" name="Code" datatype="Text" fieldtype="Normal"/><Field id="2" name="Label" datatype="Text" fieldtype="Normal"/></ObjectList></FieldCatalog>
 <FieldCatalog><BaseTableReference id="130" name="Shades"/><ObjectList><Field id="1" name="Tone" datatype="Text" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog><TableOccurrence id="7" name="Palette"><BaseTableSourceReference><BaseTableReference id="129" name="Colours"/></BaseTableSourceReference></TableOccurrence>
 <TableOccurrence id="8" name="Shade"><BaseTableSourceReference><BaseTableReference id="130" name="Shades"/></BaseTableSourceReference></TableOccurrence>
 <TableOccurrence id="9" name="Swatch"><BaseTableSourceReference><BaseTableReference id="129" name="Colours"/></BaseTableSourceReference></TableOccurrence></TableOccurrenceCatalog>
 <ValueListCatalog>
  <ValueList id="1" name="Sizes"><Source value="Custom"></Source><CustomValues><Text><![CDATA[Small` + "\r" + `Medium` + "\r\r" + `Large]]></Text></CustomValues></ValueList>
  <ValueList id="2" name="Codes"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField><ShowRelated value="False"></ShowRelated></Field></ValueList>
  <ValueList id="3" name="Labels"><Source value="FromField"></Source><Field><PrimaryField show="False" sort="False"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField><SecondaryField show="True" sort="True"><FieldReference id="2" name="Label"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></SecondaryField><ShowRelated value="True"><TableOccurrenceReference id="8" name="Shade"/></ShowRelated></Field></ValueList>
  <ValueList id="4" name="Shared"><Source value="External"></Source></ValueList>
  <ValueList id="5" name="Gone"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="9" name="Deleted"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField></Field></ValueList>
  <ValueList id="6" name="Mixed"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField><SecondaryField show="False" sort="False"><FieldReference id="1" name="Tone"><TableOccurrenceReference id="8" name="Shade"/></FieldReference></SecondaryField><ShowRelated value="True"><TableOccurrenceReference id="99" name="Elsewhere"/></ShowRelated></Field></ValueList>
  <ValueList id="7" name="Self"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField><SecondaryField show="False" sort="False"><FieldReference id="2" name="Label"><TableOccurrenceReference id="9" name="Swatch"/></FieldReference></SecondaryField></Field></ValueList>
 </ValueListCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	lists := map[string]schema.BundleValueList{}
	for _, vl := range got.Bundle.ValueLists {
		lists[vl.Name] = vl
	}
	if len(lists) != 5 {
		t.Fatalf("value lists = %v, want Sizes, Codes, Labels, Mixed and Self", lists)
	}
	if lists["Sizes"].CustomValues != "Small\nMedium\nLarge" {
		t.Fatalf("custom values = %q", lists["Sizes"].CustomValues)
	}
	columns := map[string]string{}
	for _, c := range got.Bundle.Tables[0].Columns {
		columns[c.Name] = c.ID
	}
	occurrences := map[string]string{}
	for _, o := range got.Bundle.TableOccurrences {
		occurrences[o.Name] = o.ID
	}
	for _, n := range []string{"Codes", "Labels", "Mixed", "Self"} {
		if vl := lists[n]; vl.Kind != "field" || vl.SourceColumnID == nil || *vl.SourceColumnID != columns["code"] {
			t.Fatalf("%s not bound to Colours::Code: %+v", n, vl)
		}
	}
	if vl := lists["Codes"]; vl.SecondColumnID != nil || vl.ShowSecondOnly || vl.SortBy != "" || vl.RelatedOccurrenceID != nil {
		t.Errorf("a list of one field gained options: %+v", vl)
	}

	labels := lists["Labels"]
	if labels.SecondColumnID == nil || *labels.SecondColumnID != columns["label"] {
		t.Errorf("Labels second field = %v, want Colours::Label", labels.SecondColumnID)
	}
	if !labels.ShowSecondOnly || labels.SortBy != schema.ValueListSortSecond {
		t.Errorf("Labels must show and sort by its second field only: %+v", labels)
	}
	if labels.RelatedOccurrenceID == nil || *labels.RelatedOccurrenceID != occurrences["Shade"] ||
		labels.SourceOccurrenceID == nil || *labels.SourceOccurrenceID != occurrences["Palette"] {
		t.Errorf("Labels must include only the values related from Shade, read through Palette: %+v", labels)
	}
	if mixed := lists["Mixed"]; mixed.SecondColumnID != nil || mixed.RelatedOccurrenceID != nil {
		t.Errorf("Mixed kept options it cannot honour: %+v", mixed)
	}
	// Swatch is another occurrence of Colours: its Label is a related
	// record's, not the label of the record whose code is stored.
	if self := lists["Self"]; self.SecondColumnID != nil {
		t.Errorf("Self took its second field from another occurrence of the same table: %+v", self)
	}

	reported := map[string]string{}
	for _, it := range append(got.Report.Unsupported, got.Report.ManualActionRequired...) {
		if it.Domain == "value_list" {
			reported[it.SourceName] = it.Detail
		}
	}
	for _, want := range []struct{ name, text string }{
		{"Mixed", "second field Shade::Tone"}, {"Mixed", `related from "Elsewhere"`},
		{"Shared", "another FileMaker file"}, {"Gone", "Palette::Deleted"}, {"Self", "second field Swatch::Label"},
	} {
		if !strings.Contains(reported[want.name], want.text) {
			t.Errorf("%s report %q does not mention %q", want.name, reported[want.name], want.text)
		}
	}
	for _, n := range []string{"Codes", "Labels"} {
		if detail, ok := reported[n]; ok {
			t.Errorf("%s reported as lost: %s", n, detail)
		}
	}
}

// FileMaker cannot index an unstored calculation and refuses one for a value
// list ("This field cannot be indexed"). A list that names one anyway is
// reported rather than migrated as a list that would always be empty; one
// that names it as its second field keeps its first field and says so.
func TestValueListOfAnUnindexableFieldIsReported(t *testing.T) {
	doc := `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Colours"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Colours"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/><Field id="2" name="Label" datatype="Text" fieldtype="Normal"/>
  <Field id="3" name="Shout" datatype="Text" fieldtype="Calculated"><Calculation><Calculation><Text>Upper ( Label )</Text></Calculation></Calculation><Storage storeCalculationResults="False"/></Field>
  <Field id="4" name="Whisper" datatype="Text" fieldtype="Calculated"><Calculation><Calculation><Text>Lower ( Label )</Text></Calculation></Calculation></Field>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog><TableOccurrence id="7" name="Palette"><BaseTableSourceReference><BaseTableReference id="129" name="Colours"/></BaseTableSourceReference></TableOccurrence></TableOccurrenceCatalog>
 <ValueListCatalog>
  <ValueList id="1" name="Shouts"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="3" name="Shout"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField></Field></ValueList>
  <ValueList id="2" name="Shouted codes"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="1" name="Code"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField><SecondaryField show="True" sort="False"><FieldReference id="3" name="Shout"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></SecondaryField></Field></ValueList>
  <ValueList id="3" name="Whispers"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="4" name="Whisper"><TableOccurrenceReference id="7" name="Palette"/></FieldReference></PrimaryField></Field></ValueList>
 </ValueListCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	lists := map[string]schema.BundleValueList{}
	for _, vl := range got.Bundle.ValueLists {
		lists[vl.Name] = vl
	}
	if _, ok := lists["Shouts"]; ok {
		t.Errorf("a list of an unstored calculation was migrated: %+v", lists["Shouts"])
	}
	if vl, ok := lists["Shouted codes"]; !ok || vl.SourceColumnID == nil || vl.SecondColumnID != nil {
		t.Errorf("Shouted codes must keep its first field and lose its unstored second one: %+v", vl)
	}
	if _, ok := lists["Whispers"]; !ok {
		t.Error("a list of a stored calculation must migrate")
	}

	reported := map[string]ReportItem{}
	for _, it := range append(got.Report.Unsupported, got.Report.ManualActionRequired...) {
		if it.Domain == "value_list" {
			reported[it.SourceName] = it
		}
	}
	if it := reported["Shouts"]; it.Category != CategoryManualActionRequired ||
		!strings.Contains(it.Detail, "Palette::Shout") || !strings.Contains(it.Detail, "cannot be indexed") ||
		!strings.Contains(it.Detail, "unstored calculation") {
		t.Errorf("Shouts report = %+v", it)
	}
	if it := reported["Shouted codes"]; !strings.Contains(it.Detail, "second field Palette::Shout, which cannot be indexed") {
		t.Errorf("Shouted codes report = %+v", it)
	}
	if it, ok := reported["Whispers"]; ok {
		t.Errorf("Whispers reported: %+v", it)
	}
}

// A DDR writes the options of a list taken from a field in the value list
// itself, naming each field with <Field table name>, where table is the
// field's table occurrence, and the occurrence related values start from
// with <ShowRelated><Table name>.
func TestDDRFieldValueListOptions(t *testing.T) {
	doc := `<FMPReport type="Report" version="21.0v1"><File name="Synthetic.fmp12">
 <BaseTableCatalog>
  <BaseTable id="1" name="Projects"><FieldCatalog><Field id="1" dataType="Text" fieldType="Normal" name="Ref"/></FieldCatalog></BaseTable>
  <BaseTable id="2" name="Tasks"><FieldCatalog><Field id="1" dataType="Text" fieldType="Normal" name="TaskID"/><Field id="2" dataType="Text" fieldType="Normal" name="Title"/><Field id="3" dataType="Text" fieldType="Normal" name="ProjectRef"/></FieldCatalog></BaseTable>
 </BaseTableCatalog>
 <RelationshipGraph><TableList><Table id="10" name="Projects" baseTable="Projects" baseTableId="1"/><Table id="11" name="Project Tasks" baseTable="Tasks" baseTableId="2"/><Table id="12" name="Parent Task" baseTable="Tasks" baseTableId="2"/></TableList>
 <RelationshipList><Relationship id="1"><LeftTable name="Projects"/><RightTable name="Project Tasks"/><JoinPredicateList><JoinPredicate type="Equal"><LeftField><Field table="Projects" id="1" name="Ref"/></LeftField><RightField><Field table="Project Tasks" id="3" name="ProjectRef"/></RightField></JoinPredicate></JoinPredicateList></Relationship></RelationshipList></RelationshipGraph>
 <ValueListCatalog>
  <ValueList id="1" name="Task titles"><Source value="Field"/><PrimaryField show="True" sort="False"><Field table="Project Tasks" id="1" name="TaskID"/></PrimaryField><SecondaryField show="False" sort="True"><Field table="Project Tasks" id="2" name="Title"/></SecondaryField><ShowRelated value="True"><Table id="10" name="Projects"/></ShowRelated></ValueList>
  <ValueList id="2" name="Task ids"><Source value="Field"/><PrimaryField show="True" sort="True"><Field table="Project Tasks" id="1" name="TaskID"/></PrimaryField><ShowRelated value="False"/></ValueList>
  <ValueList id="3" name="Task parents"><Source value="Field"/><PrimaryField show="True" sort="True"><Field table="Project Tasks" id="1" name="TaskID"/></PrimaryField><SecondaryField show="False" sort="False"><Field table="Parent Task" id="2" name="Title"/></SecondaryField><ShowRelated value="False"/></ValueList>
 </ValueListCatalog>
</File></FMPReport>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]string{}
	for _, table := range got.Bundle.Tables {
		for _, c := range table.Columns {
			columns[table.Name+"."+c.Name] = c.ID
		}
	}
	occurrences := map[string]string{}
	for _, o := range got.Bundle.TableOccurrences {
		occurrences[o.Name] = o.ID
	}
	lists := map[string]schema.BundleValueList{}
	for _, vl := range got.Bundle.ValueLists {
		lists[vl.Name] = vl
	}
	if len(lists) != 3 {
		t.Fatalf("value lists = %+v, want Task titles, Task ids and Task parents; report %+v", lists, got.Report)
	}

	titles := lists["Task titles"]
	if titles.SourceColumnID == nil || *titles.SourceColumnID != columns["tasks.taskid"] {
		t.Fatalf("Task titles not bound to Tasks::TaskID: %+v (columns %v)", titles, columns)
	}
	if titles.SecondColumnID == nil || *titles.SecondColumnID != columns["tasks.title"] {
		t.Errorf("Task titles second field = %v, want Tasks::Title", titles.SecondColumnID)
	}
	if titles.ShowSecondOnly || titles.SortBy != schema.ValueListSortSecond {
		t.Errorf("Task titles must show both fields, sorted by the second: %+v", titles)
	}
	if titles.RelatedOccurrenceID == nil || *titles.RelatedOccurrenceID != occurrences["Projects"] ||
		titles.SourceOccurrenceID == nil || *titles.SourceOccurrenceID != occurrences["Project Tasks"] {
		t.Errorf("Task titles must include only the tasks related from Projects: %+v", titles)
	}
	if ids := lists["Task ids"]; ids.SourceColumnID == nil || ids.SecondColumnID != nil || ids.RelatedOccurrenceID != nil {
		t.Errorf("Task ids = %+v, want Tasks::TaskID alone", ids)
	}
	// Parent Task is another occurrence of Tasks (a self-join): its Title is
	// the parent's, so it is not kept as the task's own second field.
	if parents := lists["Task parents"]; parents.SourceColumnID == nil || parents.SecondColumnID != nil {
		t.Errorf("Task parents = %+v, want Tasks::TaskID without a second field", parents)
	}
	for _, it := range got.Report.Unsupported {
		if it.Domain != "value_list" {
			continue
		}
		if it.SourceName != "Task parents" || !strings.Contains(it.Detail, "second field Parent Task::Title") {
			t.Errorf("unexpected value list report: %s: %s", it.SourceName, it.Detail)
		}
	}
}
