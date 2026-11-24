package calc

import (
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
