package migration

import (
	"encoding/json"
	"strings"
	"testing"
)

// secretSentinel stands for a password written in a design document. It must
// not reach the bundle or the report in any form.
const secretSentinel = "s3ntinel-pw-0b5e"

// stepSecretsDesign is a synthetic Save a Copy as XML design whose Send Mail
// and Save Records as PDF steps carry passwords in the shapes FileMaker may
// write them: an element, an attribute, an option type, and a PDF Security
// block.
const stepSecretsDesign = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <ScriptCatalog><Script id="81" name="Steps"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Steps"/><ObjectList>
 <Step id="63" name="Send Mail" enable="True"><ParameterValues><Parameter type="Email">
  <Boolean type="With dialog" value="False"/>
  <Send SMTP="True" OAuthAuthentication="False">
   <To><CollectAddresses value="False"/><Calculation><Text>"a@example.invalid"</Text></Calculation></To>
   <SMTP><FromAddress value="sales@example.invalid"/><Server value="smtp.example.invalid"/>
    <UserName value="sales"/><Password value="` + secretSentinel + `"/>
    <Account name="sales" password="` + secretSentinel + `"/>
    <Option type="SMTPPassword"><Text>` + secretSentinel + `</Text></Option>
    <SMTPPassword>` + secretSentinel + `</SMTPPassword></SMTP>
  </Send>
 </Parameter></ParameterValues></Step>
 <Step id="144" name="Save Records as PDF" enable="True"><ParameterValues>
  <Parameter type="Options"><Options type="Records being browsed" value="1">
   <Security><Open value="True" password="` + secretSentinel + `"/><Control value="False"/></Security>
  </Options></Parameter>
  <Parameter type="UniversalPathList"><UniversalPathList membercount="1"><ObjectList><Location>$out</Location></ObjectList></UniversalPathList></Parameter>
 </ParameterValues></Step>
 <Step id="144" name="Save Records as PDF" enable="True"><ParameterValues>
  <Parameter type="Options"><Options type="Current record" value="1">
   <Security><Open value="False"/><Control value="False"/><EnableCopying value="True"/></Security>
  </Options></Parameter>
  <Parameter type="UniversalPathList"><UniversalPathList membercount="1"><ObjectList><Location>$out</Location></ObjectList></UniversalPathList></Parameter>
 </ParameterValues></Step>
 </ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`

func TestStepPasswordsAreNeverMigrated(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: stepSecretsDesign})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secretSentinel) {
		t.Fatal("a password written in a step reached the bundle or the report")
	}
	steps := result.Bundle.Scripts[0].Steps
	if len(steps) != 3 {
		t.Fatalf("steps = %d", len(steps))
	}
	if !strings.Contains(string(raw), redactedSecret) {
		t.Fatal("the redacted option should still say a password was set")
	}
	if steps[0].Params["smtp_server_in_step"] != true {
		t.Fatalf("params = %#v", steps[0].Params)
	}
}

func TestAProtectedPDFIsRefusedWithANote(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: stepSecretsDesign})
	if err != nil {
		t.Fatal(err)
	}
	steps := result.Bundle.Scripts[0].Steps
	protected, plain := steps[1].Params, steps[2].Params
	if protected["pdf_security"] != true {
		t.Fatalf("protected params = %#v", protected)
	}
	note, _ := protected["migration_note"].(string)
	if !strings.Contains(note, "password") || !strings.Contains(note, "error 3") {
		t.Fatalf("note = %q", note)
	}
	found := false
	for _, item := range result.Report.ManualActionRequired {
		if strings.Contains(item.Detail, "protects the PDF with a password") {
			found = true
		}
	}
	if !found {
		t.Fatal("no manual action for the protected PDF")
	}
	if _, ok := plain["pdf_security"]; ok {
		t.Fatalf("an unprotected PDF is flagged: %#v", plain)
	}
	if n, ok := plain["migration_note"]; ok {
		t.Fatalf("an unprotected PDF has a note: %v", n)
	}
}
