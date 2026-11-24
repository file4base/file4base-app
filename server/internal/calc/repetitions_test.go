package calc

import (
	"testing"
)

func repetitionContext() *RecordContext {
	return &RecordContext{
		Record: Record{
			"score": intValue(3), "score[2]": intValue(5), "score[4]": intValue(1),
			"tag": textValue("a"), "tag[3]": textValue("c"),
			"unit": textValue("kg"),
		},
		Repeats: map[string]int{"score": 4, "tag": 3},
	}
}

func repetitionAnswer(t *testing.T, formula string, rep int) string {
	t.Helper()
	expr, err := Parse(formula)
	if err != nil {
		t.Fatalf("%s: %v", formula, err)
	}
	v, err := EvaluateRepetition(expr, repetitionContext(), ResultText, rep)
	if err != nil {
		t.Fatalf("%s: %v", formula, err)
	}
	if v == nil {
		return ""
	}
	return v.(string)
}

func TestRepetitionReferences(t *testing.T) {
	cases := []struct {
		formula string
		rep     int
		want    string
	}{
		{"score", 1, "3"},
		{"score[2]", 1, "5"},
		{"score[3]", 1, ""},
		{"GetRepetition ( score ; 4 )", 1, "1"},
		{"GetRepetition ( score ; 0 )", 1, ""},
		{"GetRepetition ( unit ; 1 )", 1, "kg"},
		{"GetRepetition ( unit ; 2 )", 1, ""},
		{"unit[2]", 1, ""},
		{"GetRepetition ( \"x\" ; 1 ) & GetRepetition ( \"x\" ; 2 )", 1, "x"},
		{"Sum ( score )", 1, "9"},
		{"Count ( score )", 1, "3"},
		{"Average ( score )", 1, "3"},
		{"Min ( score )", 1, "1"},
		{"Max ( score )", 1, "5"},
		{"List ( tag )", 1, "a\rc"},
		{"Last ( score )", 1, "1"},
		{"Last ( tag )", 1, "c"},
		{"Last ( unit )", 1, "kg"},
		{"Sum ( score[2] )", 1, "5"},
		{"Get ( CalculationRepetitionNumber )", 1, "1"},
		// A repeating calculation: the repetition being computed.
		{"Get ( CalculationRepetitionNumber )", 3, "3"},
		{"score", 2, "5"},
		{"score & unit", 2, "5"},
		{"score & unit", 1, "3kg"},
		{"score & Extend ( unit )", 2, "5kg"},
		{"score[1]", 2, "3"},
		{"\"k\"", 3, "k"},
		{"Sum ( score )", 2, "9"},
		{"tag", 2, ""},
		{"tag", 3, "c"},
	}
	for _, c := range cases {
		if got := repetitionAnswer(t, c.formula, c.rep); got != c.want {
			t.Errorf("%s (repetition %d) = %q, want %q", c.formula, c.rep, got, c.want)
		}
	}
}

// relatedRepetitions answers Table::Field[n] as the related records' key
// "Field[n]".
type relatedRepetitions struct{ RecordContext }

func (relatedRepetitions) Related(table, field string) ([]any, error) {
	switch field {
	case "icon":
		return []any{"first"}, nil
	case "icon[3]":
		return []any{"third"}, nil
	}
	return nil, nil
}

func TestRelatedRepetitions(t *testing.T) {
	for formula, want := range map[string]string{
		"Shared::icon":                       "first",
		"Shared::icon[3]":                    "third",
		"GetRepetition ( Shared::icon ; 3 )": "third",
		"GetRepetition ( Shared::icon ; 2 )": "",
	} {
		expr, err := Parse(formula)
		if err != nil {
			t.Fatal(err)
		}
		got, err := EvaluateWith(expr, &relatedRepetitions{}, ResultText)
		if err != nil {
			t.Fatalf("%s: %v", formula, err)
		}
		if got == nil {
			got = ""
		}
		if got != want {
			t.Errorf("%s = %q, want %q", formula, got, want)
		}
	}
}
