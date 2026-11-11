package migration

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/validation"
)

func TestIsFMP12Binary(t *testing.T) {
	if !IsFMP12Binary("Contacts.fmp12", []byte("not-xml")) {
		t.Fatal("expected .fmp12 filename to be detected")
	}
	hbamHeader := append([]byte{0x00, 0x01, 0x00, 0x00, 0x00}, []byte("HBAM2 FileMaker Pro 12.0")...)
	if !IsFMP12Binary("", hbamHeader) {
		t.Fatal("expected HBAM2 binary header to be detected")
	}
	if IsFMP12Binary("Contacts.xml", []byte("<FMPReport></FMPReport>")) {
		t.Fatal("did not expect XML to be detected as .fmp12 binary")
	}
}

func TestAnalyzeAndConvert_RejectsBinaryFMP12(t *testing.T) {
	hbamHeader := append([]byte{0x00, 0x01, 0x00, 0x00, 0x00}, []byte("HBAM2 FileMaker Pro 12.0")...)
	_, err := AnalyzeAndConvert(MigrationRequest{
		DesignXML: string(hbamHeader),
	})
	if !errors.Is(err, ErrBinaryFMP12) {
		t.Fatalf("expected ErrBinaryFMP12, got %v", err)
	}
}

func TestAnalyzeAndConvert_DDRAndFMPXMLResult(t *testing.T) {
	ddrXML := `<?xml version="1.0" encoding="UTF-8"?>
<FMPReport type="Report" version="19.0">
  <File name="BorealInventory.fmp12">
    <BaseTableCatalog>
      <BaseTable name="Specimens">
        <FieldCatalog>
          <Field name="Accession Code" dataType="Text" fieldType="Normal"/>
          <Field name="Germination Rate" dataType="Number" fieldType="Normal"/>
          <Field name="CheckInTime" dataType="Time" fieldType="Normal"/>
          <Field name="Tags" dataType="Text" fieldType="Normal" maxRepetition="5"/>
          <Field name="CurrentSessionFilter" dataType="Text" fieldType="Normal" global="True"/>
          <Field name="RecordedToday" dataType="Date" fieldType="Calculation">
            <Calculation><Text>Get ( CurrentDate )</Text></Calculation>
          </Field>
          <Field name="LegacySQLCount" dataType="Number" fieldType="Calculation">
            <Calculation><Text>ExecuteSQL("SELECT COUNT(*) FROM Specimens"; ""; "")</Text></Calculation>
          </Field>
        </FieldCatalog>
      </BaseTable>
      <BaseTable name="Collectors">
        <FieldCatalog>
          <Field name="Collector Name" dataType="Text" fieldType="Normal"/>
        </FieldCatalog>
      </BaseTable>
    </BaseTableCatalog>
    <TableOccurrenceCatalog>
      <TableOccurrence name="Specimens" baseTable="Specimens"/>
      <TableOccurrence name="Collectors" baseTable="Collectors"/>
    </TableOccurrenceCatalog>
    <RelationshipCatalog>
      <Relationship name="Specimens_Collectors" left="Specimens" left_field="Accession Code" right="Collectors" right_field="Collector Name" operator="=" allow_creation="true"/>
      <Relationship name="Cartesian_Check" left="Specimens" left_field="Accession Code" right="Collectors" right_field="Collector Name" operator="x"/>
    </RelationshipCatalog>
    <ValueListCatalog>
      <ValueList name="StorageZones" kind="custom">
        <Value>Cold Vault A</Value>
        <Value>Dry Cabinet B</Value>
      </ValueList>
    </ValueListCatalog>
    <LayoutCatalog>
      <Layout name="Specimen Details" table="Specimens">
        <Part type="header" height="50"/>
        <Part type="body" height="320"/>
        <Field name="Accession Code"/>
        <Field name="Germination Rate"/>
        <ScriptTrigger event="OnLayoutEnter" script="InitScript"/>
      </Layout>
    </LayoutCatalog>
    <ScriptCatalog>
      <Script name="InitScript">
        <StepList>
          <Step name="Go to Layout" layout="Specimen Details" enable="True"/>
          <Step name="Set Field" field="Accession Code" value="ARC-100" enable="True"/>
          <Step name="If" calculation="1 = 1" enable="True"/>
          <Step name="Perform AppleScript" script="beep" enable="True"/>
          <Step name="End If" enable="True"/>
        </StepList>
      </Script>
    </ScriptCatalog>
    <AccountCatalog>
      <Account name="[Guest]" privilegeSet="[Read-Only Access]"/>
      <Account name="curator" privilegeSet="[Full Access]"/>
      <Account name="lab_tech" privilegeSet="[Data Entry Only]"/>
    </AccountCatalog>
  </File>
</FMPReport>`

	fmpDataXML := `<?xml version="1.0" encoding="UTF-8"?>
<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult">
  <METADATA>
    <FIELD NAME="Accession Code" TYPE="TEXT"/>
    <FIELD NAME="Germination Rate" TYPE="NUMBER"/>
  </METADATA>
  <RESULTSET FOUND="2">
    <ROW RECORDID="1" MODID="1">
      <COL><DATA>ARC-001</DATA></COL>
      <COL><DATA>94.5</DATA></COL>
    </ROW>
    <ROW RECORDID="2" MODID="1">
      <COL><DATA>ARC-002</DATA></COL>
      <COL><DATA>88.0</DATA></COL>
    </ROW>
  </RESULTSET>
</FMPXMLRESULT>`

	res, err := AnalyzeAndConvert(MigrationRequest{
		DesignXML: ddrXML,
		DataFiles: map[string]string{
			"Specimens": fmpDataXML,
		},
	})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	if res.Report.SolutionName != "BorealInventory" {
		t.Fatalf("expected solution name BorealInventory, got %q", res.Report.SolutionName)
	}
	if !res.Report.TriggersQuarantined {
		t.Fatal("expected TriggersQuarantined to be true")
	}
	if !res.Report.AccountsDisabled {
		t.Fatal("expected AccountsDisabled to be true")
	}
	if res.Report.Counts.Tables != 2 {
		t.Fatalf("expected 2 tables, got %d", res.Report.Counts.Tables)
	}
	if res.Report.Counts.Relationships != 1 {
		t.Fatalf("expected 1 supported relationship (Cartesian rejected), got %d", res.Report.Counts.Relationships)
	}
	if res.Report.Counts.ValueLists != 1 {
		t.Fatalf("expected 1 value list, got %d", res.Report.Counts.ValueLists)
	}
	if res.Report.Counts.Layouts != 1 {
		t.Fatalf("expected 1 layout, got %d", res.Report.Counts.Layouts)
	}
	if res.Report.Counts.Scripts != 1 {
		t.Fatalf("expected 1 script, got %d", res.Report.Counts.Scripts)
	}
	if res.Report.Counts.Accounts != 2 {
		t.Fatalf("expected 2 accounts ([Guest] rejected), got %d", res.Report.Counts.Accounts)
	}
	if res.Report.Counts.RecordsReady != 2 {
		t.Fatalf("expected 2 records parsed from FMPXMLRESULT, got %d", res.Report.Counts.RecordsReady)
	}
	if len(res.Report.Unsupported) == 0 {
		t.Fatal("expected unsupported items (repetitions, ExecuteSQL, Cartesian, Perform AppleScript) to be recorded")
	}
	if len(res.Report.ManualActionRequired) == 0 {
		t.Fatal("expected manual_action_required items (triggers, passwords, unsupported steps/calcs) to be recorded")
	}
	for _, u := range res.Bundle.Users {
		if u.IsActive {
			t.Fatalf("migrated account %q must be disabled until password assignment", u.Username)
		}
	}
}

func TestAnalyzeAndConvert_HTMLDDRAndFractions(t *testing.T) {
	htmlDDR := `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN">
<html>
<head><title>recipes.fmp12</title></head>
<body>
<span class="partheading"><a name="valFileOptionOverviewSectionAnchor_"/>recipes.fmp12</span>
<span class="section"><a name="valTablesSectionAnchor_"/>Tables</span>
<span class="subheader"><a name="129BaseTableAnchor_"/>recipes</span>
<table>
<tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>
<tr><td><a name="1292FieldAnchor_"/>id</td><td>Normal, Text</td><td>Indexed</td></tr>
<tr><td><a name="1291FieldAnchor_"/>Name</td><td>Normal, Text</td><td></td></tr>
<tr><td><a name="1295FieldAnchor_"/>Photo</td><td>Normal, Binary</td><td></td></tr>
<tr><td><a name="12913FieldAnchor_"/>Yield</td><td>Normal, Text</td><td></td></tr>
<tr><td><a name="12914FieldAnchor_"/>Yield_calc</td><td>Calculated, Text</td><td>Unindexed, = If ( recipes::Yield &ne; &quot;&quot; ; &quot;Yield: &quot; &amp; recipes::Yield ; &quot;&quot; ) (Storage Options: Do not store)</td></tr>
</table>
<span class="subheader"><a name="133BaseTableAnchor_"/>recipe_ingredients</span>
<table>
<tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>
<tr><td><a name="1332FieldAnchor_"/>id</td><td>Normal, Text</td><td></td></tr>
<tr><td><a name="1333FieldAnchor_"/>recipe_id</td><td>Normal, Text</td><td></td></tr>
<tr><td><a name="1331FieldAnchor_"/>Ingredient</td><td>Normal, Text</td><td></td></tr>
<tr><td><a name="1335FieldAnchor_"/>qty</td><td>Normal, Number</td><td></td></tr>
<tr><td><a name="1339FieldAnchor_"/>unit</td><td>Normal, Text</td><td></td></tr>
<tr><td><a name="13311FieldAnchor_"/>Ingredient_formatted</td><td>Calculated, Text</td><td>Unindexed, = Case(not IsEmpty ( recipe_ingredients::qty );recipe_ingredients::qty &amp; &quot; &quot;) &amp; Case(not IsEmpty ( recipe_ingredients::unit );recipe_ingredients::unit &amp; &quot; &quot;) &amp; recipe_ingredients::Ingredient</td></tr>
<tr><td><a name="13312FieldAnchor_"/>qty metric</td><td>Normal, Number</td><td></td></tr>
</table>
<span class="section"><a name="valRelationshipsSectionAnchor_"/>Relationships</span>
<span class="subheader">Table Occurrences</span>
<table>
<tr><th>Name</th><th>Source</th></tr>
<tr><td>recipes</td><td>recipes</td></tr>
<tr><td>recipe_ingredients</td><td>recipe_ingredients</td></tr>
</table>
<span class="subheader"><a name="valDetailsSectionAnchor_"/>Details</span>
<span class="subheader"><a name="1RelationshipAnchor_"/>recipe_ingredients=recipes</span>
<table>
<tr><td>Predicate:</td><td></td><td>recipe_ingredients::recipe_id = recipes::id</td></tr>
<tr><td></td><td>Create</td><td>Yes</td></tr>
</table>
<span class="section"><a name="valLayoutsSectionAnchor_"/>Layouts</span>
<span class="subheader"><a name="1LayoutAnchor_"/>Edit Recipe</span>
<table>
<tr><th>Table</th><th>Show Records From</th><th>Bounds</th></tr>
<tr><td>recipes</td><td>recipes</td><td>Width: 768 pt Height: 952 pt</td></tr>
</table>
<span>Regular Fields (Layout Object)</span>
<table>
<tr><td>recipes::Name</td><td>54 pt</td><td>18 pt</td><td>76 pt</td><td>739 pt</td><td>Edit Box</td></tr>
<tr><td>recipe_ingredients::Ingredient</td><td>2 pt</td><td>0 pt</td><td>23 pt</td><td>313 pt</td><td>Edit Box</td></tr>
</table>
<span>Portals (Layout Object)</span>
<table>
<tr><td>376 pt</td><td>18 pt</td><td>605 pt</td><td>739 pt</td><td>recipe_ingredients</td><td>7</td><td>Yes</td></tr>
<tr><td>recipe_ingredients::Ingredient</td><td>2 pt</td><td>0 pt</td><td>23 pt</td><td>313 pt</td></tr>
</table>
<span class="section"><a name="valScriptsSectionAnchor_"/>Scripts</span>
</body></html>`

	res, err := AnalyzeAndConvert(MigrationRequest{
		DesignXML: htmlDDR,
	})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert HTML DDR failed: %v", err)
	}
	if res.Report.SourceFormat != "filemaker_ddr_html" {
		t.Fatalf("expected filemaker_ddr_html, got %q", res.Report.SourceFormat)
	}
	if res.Report.Counts.Tables != 2 {
		t.Fatalf("expected 2 tables, got %d", res.Report.Counts.Tables)
	}
	if res.Report.Counts.Relationships != 1 {
		t.Fatalf("expected 1 relationship, got %d", res.Report.Counts.Relationships)
	}
	if res.Report.Counts.Layouts != 1 {
		t.Fatalf("expected 1 layout, got %d", res.Report.Counts.Layouts)
	}
	if got := coerceDataValue("1/3", "NUMBER"); got != 0.3333 {
		t.Fatalf("expected 1/3 -> 0.3333, got %v", got)
	}
	if got := coerceDataValue("1 1/4", "NUMBER"); got != 1.25 {
		t.Fatalf("expected 1 1/4 -> 1.25, got %v", got)
	}
	if got := coerceDataValue("DE31E6D2-A0B8-4DC3-AD6D-AFD2B10DB7FA", "TEXT"); got != "de31e6d2-a0b8-4dc3-ad6d-afd2b10db7fa" {
		t.Fatalf("expected lowercase UUID, got %v", got)
	}
}

// fmpXMLResultProductName is one record export per FileMaker generation, each
// spelling its <PRODUCT> element the way that version writes it. Up to version
// 12 the name is "FileMaker Pro", which the binary signature scan used to match
// (see IsFMP12Binary), rejecting a valid export as a binary database.
var fmpXMLResultProductName = map[string]string{
	"FileMaker 7 (NAME carries the whole product name)":  `NAME="FileMaker Pro" VERSION="7.0"`,
	"FileMaker 12 (NAME carries the whole product name)": `NAME="FileMaker Pro" VERSION="12.0v1"`,
	"FileMaker 13 (VERSION carries the Pro designation)": `NAME="FileMaker" VERSION="Pro 13.0v1"`,
}

func fmpXMLResult(productAttrs string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult">
  <ERRORCODE>0</ERRORCODE>
  <PRODUCT BUILD="01/01/2024" ` + productAttrs + `/>
  <DATABASE DATEFORMAT="MM/dd/yyyy" LAYOUT="Contact List" NAME="TestDB.fmp12" RECORDS="1" TIMEFORMAT="hh:mm:ss"/>
  <METADATA><FIELD EMPTYOK="YES" MAXREPEAT="1" NAME="FirstName" TYPE="TEXT"/></METADATA>
  <RESULTSET FOUND="1">
    <ROW MODID="1" RECORDID="1"><COL><DATA>Ada</DATA></COL></ROW>
  </RESULTSET>
</FMPXMLRESULT>`
}

func TestIsFMP12Binary_AcceptsRecordExportNamingFileMakerPro(t *testing.T) {
	for label, attrs := range fmpXMLResultProductName {
		if IsFMP12Binary("Contact", []byte(fmpXMLResult(attrs))) {
			t.Errorf("%s: FMPXMLRESULT export was detected as a .fmp12 binary", label)
		}
	}
	// A byte order mark and leading blank lines still open an XML document.
	withBOM := append([]byte{0xEF, 0xBB, 0xBF, '\n', ' '}, []byte(fmpXMLResult(`NAME="FileMaker Pro" VERSION="7.0"`))...)
	if IsFMP12Binary("Contact", withBOM) {
		t.Error("FMPXMLRESULT export behind a byte order mark was detected as a .fmp12 binary")
	}
	// The filename still decides when it names a database file outright.
	if !IsFMP12Binary("Contacts.fp7", []byte(fmpXMLResult(`NAME="FileMaker" VERSION="Pro 13.0v1"`))) {
		t.Error("expected a .fp7 filename to be detected whatever the payload looks like")
	}
}

func TestAnalyzeAndConvert_ParsesRecordExportNamingFileMakerPro(t *testing.T) {
	for label, attrs := range fmpXMLResultProductName {
		converted, err := AnalyzeAndConvert(MigrationRequest{
			DesignXML: ddrWithCustomValueList,
			DataFiles: map[string]string{"Contact": fmpXMLResult(attrs)},
		})
		if err != nil {
			t.Fatalf("%s: AnalyzeAndConvert: %v", label, err)
		}
		if got := converted.Report.Counts.RecordsReady; got != 1 {
			t.Errorf("%s: records_ready = %d, want 1", label, got)
		}
	}
}

// ddrWithCustomValueList is the shape a FileMaker DDR gives a custom value
// list: one <Text> child per entry inside <CustomValues>, with the element's
// own character data holding nothing but layout whitespace.
const ddrWithCustomValueList = `<?xml version="1.0" encoding="UTF-8"?>
<FMPReport type="Report" version="21.0v1">
  <File name="TestDB">
    <BaseTableCatalog>
      <BaseTable id="1" name="Contact" records="0">
        <FieldCatalog>
          <Field id="1" dataType="Text" fieldType="Normal" name="FirstName"/>
        </FieldCatalog>
      </BaseTable>
    </BaseTableCatalog>
    <ValueListCatalog>
      <ValueList id="1" name="Status Values">
        <Source value="Custom"/>
        <CustomValues>
          <Text>Active</Text>
          <Text>Inactive</Text>
        </CustomValues>
      </ValueList>
    </ValueListCatalog>
  </File>
</FMPReport>`

func TestAnalyzeAndConvert_ReadsDDRCustomValueListEntries(t *testing.T) {
	converted, err := AnalyzeAndConvert(MigrationRequest{DesignXML: ddrWithCustomValueList})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	if got := len(converted.Bundle.ValueLists); got != 1 {
		t.Fatalf("converted %d value lists, want 1", got)
	}
	// Empty entries are what made the import fail validation and roll the
	// whole solution back.
	if got, want := converted.Bundle.ValueLists[0].CustomValues, "Active\nInactive"; got != want {
		t.Errorf("custom values = %q, want %q", got, want)
	}
	if got := converted.Report.Counts.ValueLists; got != 1 {
		t.Errorf("report counted %d value lists, want 1", got)
	}
}

func TestAnalyzeAndConvert_ReadsCustomValueListFromCharacterData(t *testing.T) {
	design := `<design format="file4base_design"><ValueListCatalog>` +
		"<ValueList name=\"Status Values\"><CustomValues>Active\nInactive\n</CustomValues></ValueList>" +
		`</ValueListCatalog></design>`
	converted, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	if got := len(converted.Bundle.ValueLists); got != 1 {
		t.Fatalf("converted %d value lists, want 1", got)
	}
	if got, want := converted.Bundle.ValueLists[0].CustomValues, "Active\nInactive"; got != want {
		t.Errorf("custom values = %q, want %q", got, want)
	}
}

func TestAnalyzeAndConvert_SkipsValueListWithNoEntries(t *testing.T) {
	design := `<FMPReport type="Report" version="21.0v1"><File name="TestDB"><ValueListCatalog>` +
		`<ValueList id="1" name="Empty List"><Source value="Custom"/><CustomValues/></ValueList>` +
		`</ValueListCatalog></File></FMPReport>`
	converted, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	// Skipped rather than carried into the import as an invalid value list.
	if got := len(converted.Bundle.ValueLists); got != 0 {
		t.Errorf("converted %d value lists, want 0", got)
	}
	if got := converted.Report.Counts.ManualActionCount; got != 1 {
		t.Fatalf("manual_action_count = %d, want 1", got)
	}
	if got := converted.Report.ManualActionRequired[0].SourceName; got != "Empty List" {
		t.Errorf("manual action names %q, want %q", got, "Empty List")
	}
}

// A step's operands live in child elements, not attributes, and a <Script> or
// <Layout> nested in a step is a reference rather than a definition (#88). The
// step order is also pinned here: sequence indices are 1-based and unique
// (#98).
func TestAnalyzeAndConvert_DecodesNestedScriptStepParameters(t *testing.T) {
	xmlDoc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog><Field name="Notes" dataType="Text"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog><ScriptCatalog>` +
		`<Script name="Dispatch"><StepList>` +
		`<Step name="Set Field" enable="True"><Field table="Visits" name="Notes"/><Calculation><Text>"Completed"</Text></Calculation></Step>` +
		`<Step name="Go to Layout" enable="True"><Layout name="Visit Detail" id="1"/></Step>` +
		`<Step name="Perform Script" enable="True"><Script name="Receive" id="2"/><FileReference name="RemoteArchive" id="1"/><Calculation><Text>"SV-001"</Text></Calculation></Step>` +
		`</StepList></Script></ScriptCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: xmlDoc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	if res.Report.Counts.Scripts != 1 {
		t.Fatalf("expected the one script the document defines, got %d", res.Report.Counts.Scripts)
	}
	if res.Report.Counts.Layouts != 0 {
		t.Fatalf("a layout a step points at is not a layout definition, got %d layouts", res.Report.Counts.Layouts)
	}

	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	for i, step := range steps {
		if step.SequenceIdx != i+1 {
			t.Fatalf("step %d has sequence_idx %d; indices must be 1-based and unique", i, step.SequenceIdx)
		}
	}

	if got := steps[0].Params["field"]; got != "notes" {
		t.Fatalf("Set Field target: expected %q, got %v", "notes", got)
	}
	if got := steps[0].Params["value"]; got != `"Completed"` {
		t.Fatalf("Set Field value: expected the nested calculation, got %v", got)
	}
	if got := steps[1].Params["layout_name"]; got != "Visit Detail" {
		t.Fatalf("Go to Layout destination: expected %q, got %v", "Visit Detail", got)
	}
	if got := steps[2].Params["script_name"]; got != "Receive" {
		t.Fatalf("Perform Script target: expected %q, got %v", "Receive", got)
	}
	if got := steps[2].Params["script_parameter"]; got != `"SV-001"` {
		t.Fatalf("Perform Script parameter: expected the nested calculation, got %v", got)
	}
	if got := steps[2].Params["file_reference"]; got != "RemoteArchive" {
		t.Fatalf("Perform Script external file: expected %q, got %v", "RemoteArchive", got)
	}

	// The external file becomes a file reference of its own, unmapped,
	// because nothing in the document says which solution it is (#107).
	if len(res.Bundle.FileReferences) != 1 {
		t.Fatalf("expected one file reference, got %d", len(res.Bundle.FileReferences))
	}
	if ref := res.Bundle.FileReferences[0]; ref.Name != "RemoteArchive" || ref.Database != "" {
		t.Fatalf("expected an unmapped reference named RemoteArchive, got %+v", ref)
	}

	// ...and it is reported for someone to map, naming the step that uses it.
	flagged := false
	for _, item := range res.Report.ManualActionRequired {
		if item.Domain == "script" && item.SourceName == "RemoteArchive" &&
			strings.Contains(item.Detail, "Dispatch (step 3)") &&
			strings.Contains(item.Detail, "never run a script of this solution") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("expected the external file to be reported for mapping, got %+v",
			res.Report.ManualActionRequired)
	}
}

// Several steps calling into the same external file share one reference, and
// two files make two (#107).
func TestAnalyzeAndConvert_OneFileReferencePerExternalFile(t *testing.T) {
	xmlDoc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog><Field name="Notes" dataType="Text"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog><ScriptCatalog>` +
		`<Script name="Send"><StepList>` +
		`<Step name="Perform Script" enable="True"><Script name="Receive" id="2"/><FileReference name="Results" id="1"/></Step>` +
		`<Step name="Perform Script" enable="True"><Script name="Report" id="3"/><FileReference name="results" id="1"/></Step>` +
		`<Step name="Perform Script" enable="True"><Script name="Archive" id="4"/><FileReference name="Archive" id="2"/></Step>` +
		`<Step name="Perform Script" enable="True"><Script name="Local" id="5"/></Step>` +
		`</StepList></Script></ScriptCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: xmlDoc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	names := []string{}
	for _, ref := range res.Bundle.FileReferences {
		names = append(names, ref.Name)
	}
	if strings.Join(names, ",") != "Archive,Results" {
		t.Fatalf("expected one reference per external file, case-insensitively, got %v", names)
	}
	if got, ok := res.Bundle.Scripts[0].Steps[3].Params["file_reference"]; ok {
		t.Fatalf("a local Perform Script must not gain a file reference, got %v", got)
	}
}

// Two source names that normalize alike both survive, under reported names,
// and a source field called id does not disappear behind the generated primary
// key (#89).
func TestAnalyzeAndConvert_ReportsNormalizedNameCollisions(t *testing.T) {
	xmlDoc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog>` +
		`<Field name="Visit Code" dataType="Text"/>` +
		`<Field name="Visit-Code" dataType="Text"/>` +
		`<Field name="id" dataType="Number"/>` +
		`</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: xmlDoc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if res.Report.Counts.Fields != 3 {
		t.Fatalf("expected all 3 source fields to be migrated, got %d", res.Report.Counts.Fields)
	}

	byName := map[string]string{}
	for _, col := range res.Bundle.Tables[0].Columns {
		byName[col.Name] = col.FieldType
	}
	for _, want := range []string{"id", "visit_code", "visit_code_2", "id_2"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("expected column %q, got %v", want, byName)
		}
	}
	if byName["id_2"] != "NUMBER" {
		t.Fatalf("the renamed source id field keeps its type: got %q", byName["id_2"])
	}
	if res.Report.Counts.TranslatedCount == 0 {
		t.Fatal("every collision must be reported")
	}
}

// A record export is converted exactly: a number keeps its digits, text keeps
// its spaces, and a record whose every exported value is blank is still a
// record (#90).
func TestAnalyzeAndConvert_ConvertsRecordExportExactly(t *testing.T) {
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog>` +
		`<Field name="Code" dataType="Text"/><Field name="Notes" dataType="Text"/><Field name="Amount" dataType="Number"/>` +
		`</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	records := `<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult"><METADATA>` +
		`<FIELD NAME="Code" TYPE="TEXT"/><FIELD NAME="Notes" TYPE="TEXT"/><FIELD NAME="Amount" TYPE="NUMBER"/>` +
		`</METADATA><RESULTSET FOUND="2">` +
		`<ROW RECORDID="1"><COL><DATA>SV-001</DATA></COL><COL><DATA>  Preserve these spaces  </DATA></COL><COL><DATA>9007199254740993</DATA></COL></ROW>` +
		`<ROW RECORDID="2"><COL><DATA/></COL><COL><DATA/></COL><COL><DATA/></COL></ROW>` +
		`</RESULTSET></FMPXMLRESULT>`

	res, err := AnalyzeAndConvert(MigrationRequest{
		DesignXML: design,
		DataFiles: map[string]string{"Visits": records},
	})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	rows := res.TableRows["visits"]
	if len(rows) != 2 {
		t.Fatalf("expected both exported records, got %d", len(rows))
	}
	if res.Report.Counts.RecordsReady != 2 {
		t.Fatalf("records_ready must reconcile with the source rows: got %d", res.Report.Counts.RecordsReady)
	}
	if got := rows[0]["notes"]; got != "  Preserve these spaces  " {
		t.Fatalf("text whitespace was not preserved: %q", got)
	}
	if got := fmt.Sprint(rows[0]["amount"]); got != "9007199254740993" {
		t.Fatalf("the number lost precision: %q", got)
	}
	if len(rows[1]) != 0 {
		t.Fatalf("the blank record carries no values of its own: %v", rows[1])
	}
}

// An attachment is never ignored: invalid base64 fails the conversion, and one
// that cannot be addressed is reported rather than passed over (#91).
func TestAnalyzeAndConvert_ValidatesFMP12Attachment(t *testing.T) {
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`

	if _, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, FMP12Base64: "%%%"}); err == nil {
		t.Fatal("invalid base64 in fmp12_base64 must fail the conversion")
	}

	res, err := AnalyzeAndConvert(MigrationRequest{
		DesignXML:   design,
		FMP12Base64: base64.StdEncoding.EncodeToString([]byte("HBAM2 but not addressable")),
	})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if res.Report.Counts.RejectedCount == 0 {
		t.Fatal("an attachment no record could be read from must be reported, not ignored")
	}
}

// The authentic Save-a-Copy-as-XML document of FileMaker Pro 20 / Claris 26 is
// refused with the formats that do work, rather than accepted into tables with
// no fields (#87).
func TestAnalyzeAndConvert_RejectsNativeSaveAsXML(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?><FMSaveAsXML><FMSaveAsXMLFile name="Recipes.fmp12">` +
		`<BaseTableCatalog><BaseTable name="Recipes" id="129"/></BaseTableCatalog>` +
		`<FieldsForTables><FieldCatalog><Field name="Title" datatype="text" fieldtype="normal"/></FieldCatalog></FieldsForTables>` +
		`</FMSaveAsXMLFile></FMSaveAsXML>`

	_, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if !errors.Is(err, ErrInvalidMigrationXML) {
		t.Fatalf("expected an invalid-XML error, got %v", err)
	}
	if !strings.Contains(err.Error(), "FMSaveAsXML") || !strings.Contains(err.Error(), "Design Report") {
		t.Fatalf("the refusal must name the format and the supported ones: %v", err)
	}
}

// A design whose tables arrive without a single field was not understood, and
// is refused rather than reported as a successful conversion (#87).
func TestAnalyzeAndConvert_RefusesTablesWithNoFields(t *testing.T) {
	doc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`

	if _, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc}); !errors.Is(err, ErrInvalidMigrationXML) {
		t.Fatalf("expected an invalid-XML error, got %v", err)
	}
}

// A native DDR writes its occurrences and relationships in the
// RelationshipGraph, with the two sides as child elements and the join in a
// JoinPredicateList (#101).
func TestAnalyzeAndConvert_ReadsNativeRelationshipGraph(t *testing.T) {
	doc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable id="1" name="Parents"><FieldCatalog><Field id="1" name="Key" dataType="Text" fieldType="Normal"/></FieldCatalog></BaseTable>` +
		`<BaseTable id="2" name="Children"><FieldCatalog><Field id="1" name="ParentKey" dataType="Text" fieldType="Normal"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog><RelationshipGraph>` +
		`<TableList><Table id="1" name="ParentAlias" baseTable="Parents" baseTableId="1"/>` +
		`<Table id="2" name="ChildAlias" baseTable="Children" baseTableId="2"/></TableList>` +
		`<RelationshipList><Relationship id="1">` +
		`<LeftTable name="ParentAlias" cascadeCreate="False" cascadeDelete="False"/>` +
		`<RightTable name="ChildAlias" cascadeCreate="True" cascadeDelete="False"/>` +
		`<JoinPredicateList><JoinPredicate type="Equal">` +
		`<LeftField><Field name="Key" table="ParentAlias"/></LeftField>` +
		`<RightField><Field name="ParentKey" table="ChildAlias"/></RightField>` +
		`</JoinPredicate></JoinPredicateList>` +
		`</Relationship></RelationshipList></RelationshipGraph></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	names := make([]string, 0, len(res.Bundle.TableOccurrences))
	for _, occ := range res.Bundle.TableOccurrences {
		names = append(names, occ.Name)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "ChildAlias" || names[1] != "ParentAlias" {
		t.Fatalf("expected the source aliases and no invented occurrences, got %v", names)
	}

	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("expected the one relationship the graph declares, got %d (rejected: %+v)",
			len(res.Bundle.Relationships), res.Report.Rejected)
	}
	rel := res.Bundle.Relationships[0]
	if rel.Operator != "=" {
		t.Fatalf("expected an equality join, got %q", rel.Operator)
	}
	if !rel.AllowCreation {
		t.Fatal("cascadeCreate on the right-hand table means records may be created through the relationship")
	}
	occByID := map[string]string{}
	for _, occ := range res.Bundle.TableOccurrences {
		occByID[occ.ID] = occ.Name
	}
	if occByID[rel.LeftOccurrenceID] != "ParentAlias" || occByID[rel.RightOccurrenceID] != "ChildAlias" {
		t.Fatalf("the join is between the source aliases: got %q -> %q",
			occByID[rel.LeftOccurrenceID], occByID[rel.RightOccurrenceID])
	}
}

// A field's nested Storage, Validation and AutoEnter options are read, kept
// where File4Base can enforce them, and reported where it cannot (#102).
func TestAnalyzeAndConvert_ReadsNestedFieldOptions(t *testing.T) {
	doc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog>` +
		`<Field name="Code" dataType="Text" fieldType="Normal">` +
		`<AutoEnter><ConstantData>SV-000</ConstantData></AutoEnter>` +
		`<Validation message="False" valuelist="False" validateCalculation="False">` +
		`<NotEmpty value="True"/><Unique value="True"/><MaxLength value="12"/>` +
		`</Validation>` +
		`<Storage global="False" maxRepetition="1"/>` +
		`</Field>` +
		`<Field name="SessionFilter" dataType="Text" fieldType="Normal"><Storage global="True" maxRepetition="1"/></Field>` +
		`<Field name="Answers" dataType="Text" fieldType="Normal"><Storage global="False" maxRepetition="5"/></Field>` +
		`</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	var code schema.BundleColumn
	for _, col := range res.Bundle.Tables[0].Columns {
		if col.Name == "code" {
			code = col
		}
	}
	if code.ValidationRules == nil {
		t.Fatal("the validation rules of the source field were not migrated")
	}
	rules, err := validation.Parse(code.ValidationRules)
	if err != nil {
		t.Fatalf("the migrated rules are not valid validation rules: %v", err)
	}
	if !rules.NotEmpty || !rules.Unique {
		t.Fatalf("expected not-empty and unique to hold after migration, got %+v", rules)
	}
	if !rules.MaxLengthEnabled || rules.MaxLength == nil || *rules.MaxLength != 12 {
		t.Fatalf("expected the 12-character limit, got %+v", rules)
	}
	if code.IsNullable {
		t.Fatal("a not-empty field is not nullable")
	}
	if code.DefaultValue == nil || *code.DefaultValue != "SV-000" {
		t.Fatalf("expected the auto-enter constant as the default value, got %v", code.DefaultValue)
	}

	// Storage-level global and repeating fields are reported, as the
	// attribute-level ones always were.
	var globalReported, repeatingReported bool
	for _, item := range res.Report.Translated {
		if item.SourceName == "visits.SessionFilter" && strings.Contains(item.Detail, "global") {
			globalReported = true
		}
	}
	for _, item := range res.Report.Unsupported {
		if item.SourceName == "visits.Answers" && strings.Contains(item.Detail, "Repeating") {
			repeatingReported = true
		}
	}
	if !globalReported {
		t.Fatalf("a Storage global=True field must be reported as translated: %+v", res.Report.Translated)
	}
	if !repeatingReported {
		t.Fatalf("a Storage maxRepetition=5 field must be reported as unsupported: %+v", res.Report.Unsupported)
	}
}

// Every step of an HTML DDR script is accounted for: an unsupported
// operation in the middle of a script is kept as a disabled comment with its
// original line and reported, rather than deleted while the script is called
// preserved (#104).
func TestAnalyzeAndConvert_HTMLDDRKeepsUnsupportedScriptSteps(t *testing.T) {
	htmlDDR := `<!DOCTYPE html><html><body>` +
		`<span class="partheading"><a name="valFileOptionOverviewSectionAnchor_"/>Synthetic.fmp12</span>` +
		`<span class="section"><a name="valTablesSectionAnchor_"/>Tables</span>` +
		`<span class="subheader"><a name="129BaseTableAnchor_"/>Visits</span>` +
		`<table><tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>` +
		`<tr><td><a name="1291FieldAnchor_"/>Code</td><td>Normal, Text</td><td></td></tr></table>` +
		`<span class="section"><a name="valScriptsSectionAnchor_"/>Scripts</span>` +
		`<table><tr><td>Script Name</td><td>Guard</td></tr>` +
		`<tr><td>Script Steps</td><td><ul>` +
		`<li>New Record/Request</li>` +
		`<li>Send Mail [ With dialog: Off ]</li>` +
		`<li>Commit Records/Requests</li>` +
		`</ul></td></tr></table>` +
		`<span class="section"><a name="valAccountsSectionAnchor_"/>Accounts</span>` +
		`</body></html>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: htmlDDR})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if len(res.Bundle.Scripts) != 1 {
		t.Fatalf("expected 1 script, got %d", len(res.Bundle.Scripts))
	}

	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected all 3 source steps to be accounted for, got %d: %+v", len(steps), steps)
	}
	for i, step := range steps {
		if step.SequenceIdx != i+1 {
			t.Fatalf("step %d has sequence_idx %d; indices must be 1-based and unique", i, step.SequenceIdx)
		}
	}
	if steps[1].StepType != "Comment" || steps[1].IsEnabled {
		t.Fatalf("the unsupported step is kept as a disabled comment, got %+v", steps[1])
	}
	if got := fmt.Sprint(steps[1].Params["original_filemaker_step"]); got != "Send Mail" {
		t.Fatalf("the original step name is kept: got %q", got)
	}
	if got := fmt.Sprint(steps[1].Params["text"]); !strings.Contains(got, "Send Mail [ With dialog: Off ]") {
		t.Fatalf("the original line is kept for review: got %q", got)
	}

	reported := false
	for _, item := range res.Report.Unsupported {
		if item.Domain == "script" && strings.Contains(item.Detail, "Send Mail") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("the dropped operation must be reported: %+v", res.Report.Unsupported)
	}
	if res.Report.Counts.ManualActionCount == 0 {
		t.Fatal("an unsupported step needs a manual action entry saying what to do about it")
	}
}

// A French HTML Database Design Report of the same solution converts to the
// same tables, fields and scripts as the English one: the anchors and the
// shape of the report are the same in every language, and the words that are
// not are translated (#100).
func TestConvertHTMLDDR_FrenchReportMatchesEnglish(t *testing.T) {
	report := func(language string) string {
		tableLabel, nameLabel, stepsLabel := "Table Name:", "Script Name", "Script Steps"
		fieldType, firstStep, lastStep := "Normal, Text", "New Record/Request", "Commit Records/Requests"
		if language == "French" {
			tableLabel, nameLabel, stepsLabel = "Nom de la table :", "Nom du script", "Instructions de script"
			fieldType, firstStep, lastStep = "Normale, Texte", "Nouvel enreg./requête", "Valider les enregistrements/requêtes"
		}
		return `<!DOCTYPE html><html><body>` +
			`<span class="partheading"><a name="valFileOptionOverviewSectionAnchor_"/>Synthetic.fmp12</span>` +
			`<span class="section"><a name="valTablesSectionAnchor_"/>Tables</span>` +
			`<table><tr><td>` + tableLabel + `<a name="129BaseTableAnchor_"/>Visits</td></tr>` +
			`<tr><td><a name="1291FieldAnchor_"/>Code</td><td>` + fieldType + `</td><td></td></tr></table>` +
			`<span class="section"><a name="valScriptsSectionAnchor_"/>Scripts</span>` +
			`<table><tr><td>` + nameLabel + `</td><td>Guard</td></tr>` +
			`<tr><td>` + stepsLabel + `</td><td><ul><li>` + firstStep + `</li><li>` + lastStep + `</li></ul></td></tr></table>` +
			`<span class="section"><a name="valAccountsSectionAnchor_"/>Accounts</span>` +
			`</body></html>`
	}

	english, err := AnalyzeAndConvert(MigrationRequest{DesignXML: report("English")})
	if err != nil {
		t.Fatalf("the English report failed: %v", err)
	}
	french, err := AnalyzeAndConvert(MigrationRequest{DesignXML: report("French")})
	if err != nil {
		t.Fatalf("the French report failed: %v", err)
	}

	if french.Report.Counts.Tables != english.Report.Counts.Tables ||
		french.Report.Counts.Fields != english.Report.Counts.Fields ||
		french.Report.Counts.Scripts != english.Report.Counts.Scripts {
		t.Fatalf("the French report converted to %d tables / %d fields / %d scripts, the English one to %d / %d / %d",
			french.Report.Counts.Tables, french.Report.Counts.Fields, french.Report.Counts.Scripts,
			english.Report.Counts.Tables, english.Report.Counts.Fields, english.Report.Counts.Scripts)
	}
	if french.Report.Counts.Tables != 1 || french.Report.Counts.Fields != 1 {
		t.Fatalf("expected the one table and its field, got %+v", french.Report.Counts)
	}
	if french.Bundle.Tables[0].Columns[1].FieldType != "TEXT" {
		t.Fatalf("Texte is a text field: got %q", french.Bundle.Tables[0].Columns[1].FieldType)
	}

	frenchSteps := french.Bundle.Scripts[0].Steps
	englishSteps := english.Bundle.Scripts[0].Steps
	if len(frenchSteps) != len(englishSteps) {
		t.Fatalf("expected the same steps, got %d against %d", len(frenchSteps), len(englishSteps))
	}
	for i := range frenchSteps {
		if frenchSteps[i].StepType != englishSteps[i].StepType {
			t.Fatalf("step %d converted to %q from French and %q from English",
				i+1, frenchSteps[i].StepType, englishSteps[i].StepType)
		}
		if !frenchSteps[i].IsEnabled {
			t.Fatalf("step %d of the French script arrived disabled", i+1)
		}
		if frenchSteps[i].Params["filemaker_step_name"] == nil {
			t.Fatalf("step %d must keep the name the report wrote for it", i+1)
		}
	}
}

// A report in a language the reader does not know is refused, rather than
// reported as a successful preflight with no tables at all (#100).
func TestConvertHTMLDDR_RefusesAnUnreadableReport(t *testing.T) {
	doc := `<!DOCTYPE html><html><body>` +
		`<span class="partheading"><a name="valFileOptionOverviewSectionAnchor_"/>Synthetic.fmp12</span>` +
		`<span class="section"><a name="valTablesSectionAnchor_"/>Tables</span>` +
		`<table><tr><td>テーブル名：Visits</td></tr></table>` +
		`<span class="section"><a name="valLayoutsSectionAnchor_"/>Layouts</span>` +
		`</body></html>`

	_, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if !errors.Is(err, ErrInvalidMigrationXML) {
		t.Fatalf("expected the report to be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "Supported report languages") {
		t.Fatalf("the refusal must say which languages are read: %v", err)
	}
}

// An XML DDR exported in French names its steps in French; the instruction id
// and the original name are kept as diagnostic metadata (#103).
func TestAnalyzeAndConvert_ReadsLocalizedXMLScriptSteps(t *testing.T) {
	design := func(stepName string) string {
		return `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
			`<BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text"/></FieldCatalog></BaseTable>` +
			`</BaseTableCatalog><ScriptCatalog><Script name="Guard"><StepList>` +
			`<Step id="9" name="` + stepName + `" enable="True"/>` +
			`</StepList></Script></ScriptCatalog></File></FMPReport>`
	}

	english, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design("New Record/Request")})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	french, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design("Nouvel enreg./requête")})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}

	if len(french.Bundle.Scripts[0].Steps) != 1 {
		t.Fatalf("expected one step, got %+v", french.Bundle.Scripts[0].Steps)
	}
	step := french.Bundle.Scripts[0].Steps[0]
	if step.StepType != english.Bundle.Scripts[0].Steps[0].StepType {
		t.Fatalf("the French step converted to %q, the English one to %q",
			step.StepType, english.Bundle.Scripts[0].Steps[0].StepType)
	}
	if !step.IsEnabled {
		t.Fatal("a recognized step is imported enabled, whatever language named it")
	}
	if got := fmt.Sprint(step.Params["filemaker_step_id"]); got != "9" {
		t.Fatalf("the instruction id is kept as metadata: got %q", got)
	}
	if got := fmt.Sprint(step.Params["filemaker_step_name"]); got != "Nouvel enreg./requête" {
		t.Fatalf("the name the report wrote is kept as metadata: got %q", got)
	}

	// A step no supported locale names stays quarantined, and says so.
	unknown, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design("Envoyer un signal propriétaire")})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	quarantined := unknown.Bundle.Scripts[0].Steps[0]
	if quarantined.StepType != "Comment" || quarantined.IsEnabled {
		t.Fatalf("an unknown step is a disabled comment, got %+v", quarantined)
	}
	if unknown.Report.Counts.UnsupportedCount == 0 {
		t.Fatal("an unknown step must be reported as unsupported")
	}
}
