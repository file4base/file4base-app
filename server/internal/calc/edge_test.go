package calc

import (
	"strings"
	"sync"
	"testing"
)

// Formulas that once crashed the engine or did unbounded work must answer,
// or fail cleanly.
func TestEdgeInputsAnswerOrFailCleanly(t *testing.T) {
	cases := map[string]string{
		`Round ( 1 ; 100000 )`:                                      "1",
		`Round ( 1 ; 4294967295 )`:                                  "1",
		`Choose ( 9223372036854775807 ; "a" )`:                      "",
		`If ( IsValid ( SerialIncrement ( "INV٢" ; 1 ) ) ; 1 ; 1 )`: "1",
		`Sqrt ( 1e310 ) = 1e155`:                                    "1",
		`Last ( 7 )`:                                                "7",
	}
	for formula, want := range cases {
		expr, err := Parse(formula)
		if err != nil {
			t.Fatalf("%s: %v", formula, err)
		}
		v, err := EvaluateValue(expr, &RecordContext{})
		if err != nil {
			t.Errorf("%s: %v", formula, err)
			continue
		}
		if got := v.Text(nil); got != want {
			t.Errorf("%s = %q, want %q", formula, got, want)
		}
	}
}

func TestEvaluateThatCallsItselfStops(t *testing.T) {
	expr, err := Parse(`Evaluate ( rule )`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = EvaluateValue(expr, &RecordContext{Record: Record{"rule": "Evaluate ( rule )"}})
	if err == nil {
		t.Fatal("a formula that evaluates itself forever must fail")
	}
}

func TestExpIsSafeConcurrently(t *testing.T) {
	expr, err := Parse(`Exp ( 2 ) & Ln ( 3 ) & 2 ^ 0.5`)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := EvaluateValue(expr, &RecordContext{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestFormulasFromRealSolutionsParse(t *testing.T) {
	frLocale, _ := LocaleFor("fr")
	iso := Options{}
	fr := Options{Locale: &frLocale}
	for _, c := range []struct {
		formula string
		opts    Options
		want    string
	}{
		{"// only a comment", iso, ""},
		{"/* a block */", iso, ""},
		{"Let ( $#list.step = 2 ; $#list.step * 3 )", iso, "6"},
		{"Random < 1", iso, "1"},
		{"amount * .2", fr, "2"},
		{"amount * 0.5", fr, "5"},
	} {
		expr, err := ParseWith(c.formula, c.opts)
		if err != nil {
			t.Fatalf("%q: %v", c.formula, err)
		}
		v, err := EvaluateValue(expr, &RecordContext{Record: Record{"amount": 10}})
		if err != nil {
			t.Fatalf("%q: %v", c.formula, err)
		}
		if got := v.Text(nil); got != c.want {
			t.Errorf("%q = %q, want %q", c.formula, got, c.want)
		}
	}
}

func TestDeletedReferencesParseButCannotBeRead(t *testing.T) {
	for _, formula := range []string{
		"<Table Missing>::<Field Missing> & \"x\"",
		"Articles::<Field Missing> + 1",
		"<Table manquante>::<Rubrique manquante>",
	} {
		expr, err := Parse(formula)
		if err != nil {
			t.Fatalf("%q: %v", formula, err)
		}
		refs := expr.RelatedFields()
		if len(refs) != 1 || !refs[0].Missing {
			t.Errorf("%q: references %+v, want one missing", formula, refs)
		}
		if _, err := EvaluateValue(expr, &RecordContext{}); err == nil || !strings.Contains(err.Error(), "deleted") {
			t.Errorf("%q: error %v, want a deleted reference", formula, err)
		}
	}
	// A field named like a function does not hide the function.
	if expr, err := ParseWith("Date ( 1 ; 2 ; 2026 ) > Date", Options{Fields: []string{"Date"}}); err != nil {
		t.Errorf("a field named Date: %v", err)
	} else if fields := expr.Fields(); len(fields) != 1 || fields[0] != "Date" {
		t.Errorf("a field named Date: fields %v", fields)
	}
	// Comparisons are not references.
	expr, err := Parse("a <b and b> c")
	if err != nil || len(expr.RelatedFields()) != 0 {
		t.Errorf("a <b and b> c: %v %v", err, expr)
	}
}

func TestSelfAndLayoutObjectsNeedALayout(t *testing.T) {
	for _, formula := range []string{"Self + 1", "GetLayoutObjectAttribute ( \"total\" ; \"content\" )"} {
		expr, err := Parse(formula)
		if err != nil {
			t.Fatalf("%q: %v", formula, err)
		}
		if _, err := EvaluateValue(expr, &RecordContext{}); err == nil {
			t.Errorf("%q: want an error", formula)
		}
	}
}
