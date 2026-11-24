package migration

import (
	"strings"
	"testing"
)

func TestScriptCapabilityReportMatchesClientControlFlow(t *testing.T) {
	for _, name := range []string{"If", "Else If", "Else", "End If", "Set Variable", "Si", "Sinon si", "Sinon", "Fin de si", "Définir variable"} {
		t.Run(name, func(t *testing.T) {
			canonical, supported, _ := canonicalScriptStepName(name)
			if canonical == "" || !supported {
				t.Fatalf("client-executable operation %q is reported as catalog-only", name)
			}
		})
	}
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport><File name="Synthetic"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Status" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog><ScriptCatalog><Script name="Inspect"><StepList><Step name="Sort Records" enable="True"/><Step name="Omit Record" enable="True"/><Step name="Show Omitted Only" enable="False"/></StepList></Script></ScriptCatalog></File></FMPReport>`})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bundle.Scripts) != 1 || len(result.Bundle.Scripts[0].Steps) != 3 {
		t.Fatal("inspectable instructions were discarded")
	}
	steps := result.Bundle.Scripts[0].Steps
	if !steps[0].IsEnabled || !steps[1].IsEnabled || steps[2].IsEnabled {
		t.Fatal("source enabled state changed")
	}
	for _, name := range []string{"Sort Records", "Omit Record", "Show Omitted Only"} {
		found := false
		for _, item := range result.Report.ManualActionRequired {
			if item.Domain == "script" && strings.Contains(item.Detail, `Step "`+name+`"`) && strings.Contains(item.Detail, "cannot execute") {
				found = true
			}
		}
		if !found {
			t.Errorf("unexecutable instruction %q has no explicit execution blocker", name)
		}
	}
}
