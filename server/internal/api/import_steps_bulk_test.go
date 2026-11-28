package api

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/transfer"
)

// A bulk confirmation creates only what the review offers in bulk: a step
// inferred with high or medium confidence over every table, from the table
// inferred, its target saved in FileMaker (#191). Synthetic steps only.
func TestBulkRefusal(t *testing.T) {
	step := transfer.ImportStep{
		TargetTable: "orders", Method: "add",
		Maps: []transfer.ImportStepMap{{Position: 1, Target: transfer.ImportStepField{Name: "code"}}},
	}
	high := transfer.Inference{Table: "entries", Confidence: transfer.ConfidenceHigh}
	if why := bulkRefusal(step, high, "entries"); why != "" {
		t.Fatalf("a high-confidence step was refused: %s", why)
	}
	medium := high
	medium.Confidence = transfer.ConfidenceMedium
	if why := bulkRefusal(step, medium, "entries"); why != "" {
		t.Fatalf("a medium-confidence step was refused: %s", why)
	}

	fromLayout := step
	fromLayout.TargetFromLayout = true
	low := high
	low.Confidence = transfer.ConfidenceLow
	hidden := high
	hidden.TablesHidden = 1
	problem := step
	problem.SplitRepetitions = true
	for name, c := range map[string]struct {
		step  transfer.ImportStep
		inf   transfer.Inference
		table string
		want  string
	}{
		"low confidence":           {step, low, "entries", "low confidence"},
		"ambiguous":                {step, transfer.Inference{Reason: "2 tables fit"}, "entries", "no source table is inferred"},
		"another table":            {step, high, "others", "inferred source table is entries, not others"},
		"tables left out":          {step, hidden, "entries", "could not be weighed"},
		"a problem":                {problem, high, "entries", "cannot get a transfer as saved"},
		"target from Go to Layout": {fromLayout, high, "entries", "taken from the Go to Layout"},
	} {
		if why := bulkRefusal(c.step, c.inf, c.table); !strings.Contains(why, c.want) {
			t.Errorf("%s: refusal = %q", name, why)
		}
	}
}
