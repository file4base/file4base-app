package migration

import "testing"

// Original synthetic scripts; the option elements follow what FileMaker Pro
// writes for Set Error Capture and Go to Record/Request/Page in a DDR and in
// Save a Copy as XML.
func TestErrorCaptureAndGoToRecordOptionsDecode(t *testing.T) {
	const ddr = `<FMPReport><File name="Synthetic"><ScriptCatalog><Script name="Walk"><StepList>
<Step id="86" name="Gestion erreurs" enable="True"><Set state="True"/></Step>
<Step id="16" name="Atteindre enreg./requête/page" enable="True"><NoInteract state="False"/><Exit state="True"/><RowPageLocation value="Next"/></Step>
<Step id="16" name="Atteindre enreg./requête/page" enable="True"><NoInteract state="False"/><RowPageLocation value="First"/></Step>
<Step id="16" name="Atteindre enreg./requête/page" enable="True"><NoInteract state="True"/><RowPageLocation value="ByCalculation"/><Calculation><Text>$row</Text></Calculation></Step>
<Step id="86" name="Gestion erreurs" enable="True"><Set state="False"/></Step>
</StepList></Script></ScriptCatalog></File></FMPReport>`
	const native = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ScriptCatalog><Script id="81" name="Walk"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Walk"/><ObjectList>
 <Step id="86" name="Set Error Capture" enable="True"><Options>196608</Options><ParameterValues><Parameter type="Boolean"><Boolean id="131072" value="True"/></Parameter></ParameterValues></Step>
 <Step id="16" name="Go to Record/Request/Page" enable="True"><Options>8194</Options><ParameterValues><Parameter type="Records"><List name="Next" value="4"><Boolean type="Exit after last" position="187" value="True"/></List></Parameter></ParameterValues></Step>
 <Step id="16" name="Go to Record/Request/Page" enable="True"><Options>2</Options><ParameterValues><Parameter type="Records"><List name="First" value="1"/></Parameter></ParameterValues></Step>
 <Step id="16" name="Go to Record/Request/Page" enable="True"><ParameterValues><Parameter type="Records"><List name="By Calculation" value="5"><Calculation><Calculation><Text>$row</Text></Calculation></Calculation></List></Parameter></ParameterValues></Step>
 <Step id="86" name="Set Error Capture" enable="True"><ParameterValues><Parameter type="Boolean"><Boolean id="131072" value="False"/></Parameter></ParameterValues></Step>
 </ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`
	for name, design := range map[string]string{"ddr": ddr, "native": native} {
		result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(result.Bundle.Scripts) != 1 || len(result.Bundle.Scripts[0].Steps) != 5 {
			t.Fatalf("%s: scripts %+v", name, result.Bundle.Scripts)
		}
		steps := result.Bundle.Scripts[0].Steps
		for i, step := range steps {
			if step.StepType == "comment" {
				t.Fatalf("%s: step %d was quarantined: %+v", name, i, step)
			}
		}
		if steps[0].Params["state"] != true || steps[4].Params["state"] != false {
			t.Fatalf("%s: capture state %v / %v", name, steps[0].Params, steps[4].Params)
		}
		if steps[1].Params["target"] != "next" || steps[1].Params["exit_after_last"] != true {
			t.Fatalf("%s: next with exit %v", name, steps[1].Params)
		}
		if steps[2].Params["target"] != "first" || steps[2].Params["exit_after_last"] == true {
			t.Fatalf("%s: first %v", name, steps[2].Params)
		}
		if steps[3].Params["target"] != "$row" || steps[3].Params["target_is_calculation"] != true {
			t.Fatalf("%s: by calculation %v", name, steps[3].Params)
		}
		for _, item := range result.Report.ManualActionRequired {
			if item.Domain == "script" {
				t.Fatalf("%s: decoded options reported as manual: %+v", name, item)
			}
		}
	}
}

func TestErrorCaptureStepType(t *testing.T) {
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport><File name="S"><ScriptCatalog><Script name="A"><StepList><Step id="86" name="Set Error Capture" enable="True"><Set state="True"/></Step></StepList></Script></ScriptCatalog></File></FMPReport>`})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Bundle.Scripts[0].Steps[0].StepType; got != "Set Error Capture" {
		t.Fatalf("step type %q", got)
	}
}

// An Options bit nobody has matched to a parameter is still reported.
func TestUnknownStepOptionBitIsReported(t *testing.T) {
	if !optionsRepeatParameters("Set Error Capture", "65536") || optionsRepeatParameters("Set Error Capture", "65537") ||
		optionsRepeatParameters("Go to Record/Request/Page", "8195") || optionsRepeatParameters("Beep", "0") {
		t.Fatal("only the observed redundant bits may be accepted")
	}
}
