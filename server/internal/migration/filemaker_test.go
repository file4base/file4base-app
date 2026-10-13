package migration

import (
	"errors"
	"testing"
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

