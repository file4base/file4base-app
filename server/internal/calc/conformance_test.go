package calc

import (
	"encoding/json"
	"os"
	"testing"
)

// conformanceCase is one formula and the answer FileMaker Pro gave for it.
type conformanceCase struct {
	Formula string `json:"formula"`
	Locale  string `json:"locale"`
	// Text is the answer as FileMaker writes it as text in that locale.
	Text string `json:"text"`
	// Error is FileMaker's EvaluationError code; non-zero means the formula
	// fails to parse or to evaluate and File4Base must report an error too.
	Error int `json:"error"`
	// Note explains a case File4Base deliberately answers differently.
	Note string `json:"note,omitempty"`
}

// TestConformance checks the engine against answers observed in FileMaker
// Pro (testdata/conformance.json, see testdata/README.md).
func TestConformance(t *testing.T) {
	raw, err := os.ReadFile("testdata/conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Source string            `json:"source"`
		Cases  []conformanceCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	for _, c := range file.Cases {
		loc, ok := LocaleFor(c.Locale)
		if !ok {
			t.Fatalf("%q: unknown locale %q", c.Formula, c.Locale)
		}
		got, gotErr := conformanceAnswer(c.Formula, &loc)
		if c.Note != "" {
			continue // a deliberate difference, explained in the case
		}
		switch {
		case c.Error != 0 && gotErr == nil:
			t.Errorf("%s\n  FileMaker: error %d\n  File4Base: %q", c.Formula, c.Error, got)
		case c.Error == 0 && gotErr != nil:
			t.Errorf("%s\n  FileMaker: %q\n  File4Base: error %v", c.Formula, c.Text, gotErr)
		case c.Error == 0 && got != c.Text:
			t.Errorf("%s\n  FileMaker: %q\n  File4Base: %q", c.Formula, c.Text, got)
		}
	}
}

func conformanceAnswer(formula string, loc *Locale) (string, error) {
	expr, err := ParseWith(formula, Options{Locale: loc})
	if err != nil {
		return "", err
	}
	v, err := EvaluateValue(expr, &RecordContext{Loc: loc})
	if err != nil {
		return "", err
	}
	return v.Text(loc), nil
}
