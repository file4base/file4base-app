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
