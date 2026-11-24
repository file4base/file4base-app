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
		{"Trier enregistrements", "39", "Sort Records", false},
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

func TestUnimplementedStepIsNamedButNotExecuted(t *testing.T) {
	got := canonicalScriptStep("Gestion erreurs", "86")
	if got.Canonical != "" || got.English != "Set Error Capture" || !got.Localized {
		t.Fatalf("got %+v", got)
	}
}
