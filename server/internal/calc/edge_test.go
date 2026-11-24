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
		`IsValid ( GetAsDate ( 18446744073709551617 ) )`:            "0",
		`IsValidExpression ( "GetLayoutObjectAttribute ( 1 ; 2 ; 3 ; 4 ; 5 )" )`: "0",
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

func TestAstraPass3Regressions(t *testing.T) {
	// A custom function named Random replaces the built-in.
	lib, err := NewLibrary([]CustomFunction{{Name: "Random", Formula: "4"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	expr, err := ParseWith("Random + 1", Options{Library: lib})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := EvaluateValue(expr, &RecordContext{}); err != nil || v.Text(nil) != "5" {
		t.Errorf("custom Random + 1 = %v, %v; want 5", v, err)
	}
	// An aggregate over a deleted field is the deleted-reference error.
	expr, err = Parse("Sum ( Orders::<Field Missing> )")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateValue(expr, &RecordContext{}); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Errorf("Sum of a deleted field: %v", err)
	}
	// IsValidExpression is as strict as Evaluate about a French decimal.
	fr, _ := LocaleFor("fr")
	expr, err = ParseWith(`IsValidExpression ( "1.5" )`, Options{Locale: &fr})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := EvaluateValue(expr, &RecordContext{}); err != nil || v.Text(&fr) != "0" {
		t.Errorf(`IsValidExpression ( "1.5" ) in French = %v, %v; want 0`, v, err)
	}
}
