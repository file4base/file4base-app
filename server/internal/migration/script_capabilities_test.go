package migration

import (
	"strings"
	"testing"
)

func TestScriptCapabilityReportMatchesClientControlFlow(t *testing.T) {
	for _, name := range []string{
		"If", "Else If", "Else", "End If", "Set Variable",
		"Si", "Sinon si", "Sinon", "Fin de si", "Définir variable",
		"Sort Records", "Omit Record", "Show Omitted Only", "Print", "Insert from URL",
		"Set Error Capture", "Set Field By Name", "Go to Related Record",
		"New Window", "Select Window", "Close Window", "Adjust Window",
	} {
		t.Run(name, func(t *testing.T) {
			canonical, supported, _ := canonicalScriptStepName(name)
			if canonical == "" || !supported {
				t.Fatalf("client-executable operation %q is reported as unsupported", name)
			}
		})
	}
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport><File name="Synthetic"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Status" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog><ScriptCatalog><Script name="Inspect"><StepList><Step name="Sort Records" enable="True"/><Step name="Omit Record" enable="True"/><Step name="Show Omitted Only" enable="False"/></StepList></Script></ScriptCatalog></File></FMPReport>`})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bundle.Scripts) != 1 || len(result.Bundle.Scripts[0].Steps) != 3 {
		t.Fatal("steps were discarded")
	}
	steps := result.Bundle.Scripts[0].Steps
	if !steps[0].IsEnabled || !steps[1].IsEnabled || steps[2].IsEnabled {
		t.Fatal("source enabled state changed")
	}
	if result.Report.Counts.UnsupportedCount != 0 {
		t.Fatalf("expected zero unsupported steps, got %d: %+v", result.Report.Counts.UnsupportedCount, result.Report.Unsupported)
	}
	for _, item := range result.Report.ManualActionRequired {
		if item.Domain == "script" && strings.Contains(item.Detail, "cannot execute") {
			t.Errorf("now-executable step still flagged as unexecutable: %+v", item)
		}
	}
}
