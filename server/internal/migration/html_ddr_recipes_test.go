package migration

import (
	"os"
	"strings"
	"testing"
)

// TestConvertHTMLDDR_RecipeDatabase runs the HTML DDR reader over a design
// report of a real FileMaker solution, so the reader is held to a document
// FileMaker itself produced rather than only to the shapes our hand-written
// fixtures cover. See testdata/README.md for the fixture's provenance.
func TestConvertHTMLDDR_RecipeDatabase(t *testing.T) {
	raw, err := os.ReadFile("testdata/recipes_html_ddr.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	converted, err := AnalyzeAndConvert(MigrationRequest{
		SolutionName: "Recipes",
		DesignXML:    string(raw),
	})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}

	if got, want := converted.Report.SourceFormat, "filemaker_ddr_html"; got != want {
		t.Errorf("source format = %q, want %q", got, want)
	}

	// Base tables, named as File4Base identifiers.
	wantTables := []string{"recipes", "recipe_ingredients", "recipe_tags"}
	gotTables := make(map[string]bool, len(converted.Bundle.Tables))
	for _, tbl := range converted.Bundle.Tables {
		gotTables[tbl.Name] = true
	}
	for _, name := range wantTables {
		if !gotTables[name] {
			t.Errorf("table %q is missing; got %v", name, keysOf(gotTables))
		}
	}
	if got, want := len(converted.Bundle.Tables), len(wantTables); got != want {
		t.Errorf("converted %d tables, want %d", got, want)
	}

	// Four occurrences over three tables: the solution reaches
	// recipe_ingredients twice, the second time through a sorted self-join.
	// Collapsing the alias onto its base table would silently merge the two.
	wantOccurrences := []string{
		"recipes", "recipe_ingredients", "recipe_tags", "recipe_ingredients ordered",
	}
	gotOccurrences := make(map[string]bool, len(converted.Bundle.TableOccurrences))
	for _, occ := range converted.Bundle.TableOccurrences {
		gotOccurrences[occ.Name] = true
	}
	for _, name := range wantOccurrences {
		if !gotOccurrences[name] {
			t.Errorf("table occurrence %q is missing; got %v", name, keysOf(gotOccurrences))
		}
	}

	// Every relationship resolved: none was rejected for dangling endpoints.
	if got, want := len(converted.Bundle.Relationships), 3; got != want {
		t.Errorf("converted %d relationships, want %d", got, want)
	}
	var selfJoin bool
	for _, rel := range converted.Bundle.Relationships {
		if strings.Contains(rel.Name, "recipe_ingredients ordered") {
			selfJoin = true
		}
	}
	if !selfJoin {
		t.Error("the self-join onto \"recipe_ingredients ordered\" was not converted")
	}

	if got, want := converted.Report.Counts.Fields, 24; got != want {
		t.Errorf("converted %d fields, want %d", got, want)
	}
	if got, want := converted.Report.Counts.Layouts, 7; got != want {
		t.Errorf("converted %d layouts, want %d", got, want)
	}

	// The solution ships a Guest account, which File4Base refuses to carry, and
	// an Admin account that arrives disabled until a password is assigned.
	var guestRejected bool
	for _, item := range converted.Report.Rejected {
		// FileMaker writes the built-in account bracketed, as it does its
		// built-in privilege sets.
		if item.SourceName == "[Guest]" {
			guestRejected = true
		}
	}
	if !guestRejected {
		t.Error("the Guest account was not rejected by the zero-default-account policy")
	}
	var adminPending bool
	for _, item := range converted.Report.ManualActionRequired {
		if item.SourceName == "Admin" {
			adminPending = true
		}
	}
	if !adminPending {
		t.Error("the Admin account was not reported as needing a password")
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
