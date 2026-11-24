package migration

import "testing"

func TestScriptStepResolvedByInstructionID(t *testing.T) {
	cases := []struct {
		name, id, want string
		supported      bool
	}{
		{"Ouvrir boîte dial. person.", "87", "Show Custom Dialog", true},
		{"Afficher enreg/requête/page", "16", "Go to Record/Request/Page", true},
		{"Afficher tous les enreg.", "23", "Show All Records", true},
		{"# (commentaire)", "89", "Comment", true},
		{"Trier enregistrements", "39", "Sort Records", true},
		{"Gestion erreurs", "86", "Set Error Capture", true},
		{"Show Custom Dialog", "87", "Show Custom Dialog", true},
	}
	for _, c := range cases {
		got := canonicalScriptStep(c.name, c.id)
		if got.Canonical != c.want || got.Supported != c.supported || got.Conflict {
			t.Errorf("%q id %s: got %+v, want %q supported=%v", c.name, c.id, got, c.want, c.supported)
		}
	}
}

func TestUnknownInstructionIDFallsBackToName(t *testing.T) {
	if got := canonicalScriptStep("Si", "99999"); got.Canonical != "If" || !got.Supported {
		t.Fatalf("unknown id: got %+v", got)
	}
	if got := canonicalScriptStep("Libellé inconnu", "99999"); got.Canonical != "" || got.English != "" {
		t.Fatalf("unknown id and name must stay unresolved: got %+v", got)
	}
}

func TestInstructionIDNeverOverridesEnglishName(t *testing.T) {
	got := canonicalScriptStep("Set Variable", "87")
	if got.Canonical != "Set Variable" || !got.Conflict {
		t.Fatalf("contradicting id: got %+v", got)
	}
}

// #133: every observed FileMaker instruction ID from real solutions must
// resolve to an executable canonical step with zero unsupported steps.
func TestAllObservedInstructionIDsAreExecutable(t *testing.T) {
	for id, english := range observedScriptStepIDs {
		got := canonicalScriptStep(english, id)
		if !got.Supported || got.Canonical == "" {
			t.Errorf("observed step id %s (%q) is not executable: %+v", id, english, got)
		}
	}
	for french, english := range observedFrenchScriptSteps {
		canonical, supported, localized := canonicalScriptStepName(french)
		if !supported || canonical == "" || !localized {
			t.Errorf("observed French step %q (%q) is not executable: canonical=%q supported=%v localized=%v",
				french, english, canonical, supported, localized)
		}
	}
}

// #133: options of supported steps (LayoutDestination, Repetition, Restore,
// Query, SortList, Show Custom Dialog Title/Message/Buttons/InputFields,
// Go to Record RowPageLocation/NoInteract/Exit, Commit Records NoInteract/Option/ESSForceCommit,
// DisplayCalculation, New/Select/Close Window) decode cleanly with zero
// unsupported steps and zero unmapped option manual-action warnings.
func TestScriptStepOptionsDecodeCleanly(t *testing.T) {
	const ddr = `<FMPReport><File name="FrenchSolution.fmp12">
<BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog>
  <Field id="1" name="Status" dataType="Text"/>
</FieldCatalog></BaseTable></BaseTableCatalog>
<ScriptCatalog><Script name="Workflow"><StepList>
  <Step id="86" name="Gestion erreurs" enable="True"><SetState state="True"/><StepText>Gestion erreurs [ Activée ]</StepText></Step>
  <Step id="6" name="Activer modèle" enable="True"><LayoutDestination value="OriginalLayout"/><DisableStepCollapsed state="False"/></Step>
  <Step id="6" name="Activer modèle" enable="True"><LayoutDestination value="LayoutNameByCalc"><Calculation><Text>$layout</Text></Calculation></LayoutDestination></Step>
  <Step id="141" name="Définir variable" enable="True"><Name>$arr</Name><Value><Calculation><Text>"v"</Text></Calculation></Value><Repetition><Calculation><Text>2</Text></Calculation></Repetition></Step>
  <Step id="68" name="Si" enable="True"><Calculation><Text>Get(LastError) = 0</Text></Calculation><DisplayCalculation><Chunk type="FunctionRef">Get</Chunk></DisplayCalculation></Step>
  <Step id="16" name="Afficher enreg/requête/page" enable="True"><RowPageLocation value="Next"/><NoInteract state="True"/><Exit state="True"/></Step>
  <Step id="28" name="Exécuter la recherche" enable="True"><Restore state="True"/><Query><Request action="Find"><CriteriaList><Criterion><Field name="Status" table="Visits"/><Value>open</Value></Criterion></CriteriaList></Request></Query></Step>
  <Step id="39" name="Trier enregistrements" enable="True"><Restore state="True"/><NoInteract state="True"/><SortList><Sort type="Descending"><Field name="Status" table="Visits"/></Sort></SortList></Step>
  <Step id="87" name="Ouvrir boîte dial. person." enable="True"><Title><Calculation><Text>"Confirm"</Text></Calculation></Title><Message><Calculation><Text>"Proceed?"</Text></Calculation></Message><Buttons><Button CommitState="True"><Calculation><Text>"OK"</Text></Calculation></Button><Button CommitState="False"><Calculation><Text>"Cancel"</Text></Calculation></Button></Buttons><InputFields><InputField><Field name="Status" table="Visits"/><Label><Calculation><Text>"New Status"</Text></Calculation></Label></InputField></InputFields></Step>
  <Step id="75" name="Valider enreg./requêtes" enable="True"><NoInteract state="True"/><Option value="SkipDataEntryValidation"/><ESSForceCommit state="True"/></Step>
  <Step id="122" name="Nouvelle fenêtre" enable="True"><Window name="Detail"><LayoutDestination value="SelectedLayout"><Layout name="Visit"/></LayoutDestination><NewWndStyles Style="Card"/><Width><Calculation><Text>600</Text></Calculation></Width></Window></Step>
  <Step id="147" name="Définir rubrique par nom" enable="True"><TargetField><Calculation><Text>"Visits::Status"</Text></Calculation></TargetField><Calculation><Text>"done"</Text></Calculation></Step>
  <Step id="70" name="Fin de si" enable="True"/>
</StepList></Script></ScriptCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: ddr})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Counts.UnsupportedCount != 0 {
		t.Fatalf("expected 0 unsupported steps, got %d: %+v", res.Report.Counts.UnsupportedCount, res.Report.Unsupported)
	}
	for _, m := range res.Report.ManualActionRequired {
		if m.Domain == "script" {
			t.Errorf("unexpected script manual_action_required: %+v", m)
		}
	}
	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 13 {
		t.Fatalf("expected 13 steps, got %d", len(steps))
	}
	if steps[0].StepType != "Set Error Capture" || steps[0].Params["state"] != true {
		t.Fatalf("step 0: %+v", steps[0])
	}
	if steps[1].Params["layout_destination"] != "original" {
		t.Fatalf("step 1: %+v", steps[1])
	}
	if steps[2].Params["layout_destination"] != "by_name" || steps[2].Params["layout_calculation"] != "$layout" {
		t.Fatalf("step 2: %+v", steps[2])
	}
	if steps[3].Params["repetition"] != "2" {
		t.Fatalf("step 3: %+v", steps[3])
	}
	if steps[5].Params["target"] != "next" || steps[5].Params["exit_after_last"] != true || steps[5].Params["dialog"] != false {
		t.Fatalf("step 5: %+v", steps[5])
	}
	if steps[6].Params["restore"] != true || steps[6].Params["find_requests"] == nil {
		t.Fatalf("step 6: %+v", steps[6])
	}
	if steps[7].Params["restore"] != true || steps[7].Params["sort_order"] == nil {
		t.Fatalf("step 7: %+v", steps[7])
	}
	if steps[8].Params["buttons"] == nil || steps[8].Params["input_fields"] == nil {
		t.Fatalf("step 8: %+v", steps[8])
	}
	if steps[9].Params["dialog"] != false || steps[9].Params["skip_validation"] != true || steps[9].Params["force_commit"] != true {
		t.Fatalf("step 9: %+v", steps[9])
	}
	if steps[10].StepType != "New Window" || steps[10].Params["window_name"] != "Detail" || steps[10].Params["layout_name"] != "Visit" {
		t.Fatalf("step 10: %+v", steps[10])
	}
	if steps[11].StepType != "Set Field By Name" || steps[11].Params["target_field"] != `"Visits::Status"` || steps[11].Params["value"] != `"done"` {
		t.Fatalf("step 11: %+v", steps[11])
	}
}
