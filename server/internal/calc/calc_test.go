package calc_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
)

func evalText(t *testing.T, formula string, record calc.Record) string {
	t.Helper()
	expr, err := calc.Parse(formula)
	if err != nil {
		t.Fatalf("Parse(%q): %v", formula, err)
	}
	got, err := calc.Evaluate(expr, record, calc.ResultText)
	if err != nil {
		t.Fatalf("Evaluate(%q): %v", formula, err)
	}
	if got == nil {
		return ""
	}
	return got.(string)
}

func evalNumber(t *testing.T, formula string, record calc.Record) float64 {
	t.Helper()
	expr, err := calc.Parse(formula)
	if err != nil {
		t.Fatalf("Parse(%q): %v", formula, err)
	}
	got, err := calc.Evaluate(expr, record, calc.ResultNumber)
	if err != nil {
		t.Fatalf("Evaluate(%q): %v", formula, err)
	}
	if got == nil {
		return math.NaN()
	}
	return got.(float64)
}

// The calculation from the tutorial this project is modelled on: new customers
// pay 200, continuing ones pay 100.
func TestAnnualFeeCalculation(t *testing.T) {
	const formula = `IF(customer_type = "Continuing"; 100; 200)`

	if got := evalNumber(t, formula, calc.Record{"customer_type": "Continuing"}); got != 100 {
		t.Errorf("continuing customer = %v, want 100", got)
	}
	if got := evalNumber(t, formula, calc.Record{"customer_type": "New"}); got != 200 {
		t.Errorf("new customer = %v, want 200", got)
	}
	// Text comparison ignores case, as Find mode does.
	if got := evalNumber(t, formula, calc.Record{"customer_type": "continuing"}); got != 100 {
		t.Errorf("lowercase continuing = %v, want 100", got)
	}
	// A record with nothing in the field falls to the else branch.
	if got := evalNumber(t, formula, calc.Record{}); got != 200 {
		t.Errorf("empty customer type = %v, want 200", got)
	}
}

func TestArithmetic(t *testing.T) {
	record := calc.Record{"price": 10.0, "quantity": 3.0, "tax_rate": 21.0}

	cases := map[string]float64{
		"price * quantity":                        30,
		"price * quantity * (1 + tax_rate / 100)": 36.3,
		"price - quantity":                        7,
		"-price":                                  -10,
		"2 + 3 * 4":                               14,
		"(2 + 3) * 4":                             20,
		"ROUND(price * quantity * 1.21; 2)":       36.3,
		"ROUND(10 / 3; 3)":                        3.333,
		"ABS(quantity - price)":                   7,
		"SUM(price; quantity; 5)":                 18,
		"MIN(price; quantity)":                    3,
		"MAX(price; quantity)":                    10,
		"AVERAGE(10; 20; 30)":                     20,
	}
	for formula, want := range cases {
		if got := evalNumber(t, formula, record); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", formula, got, want)
		}
	}
}

func TestTextFunctions(t *testing.T) {
	record := calc.Record{"first_name": "Mary", "last_name": "Smith", "city": "  New York  "}

	cases := map[string]string{
		`first_name & " " & last_name`:       "Mary Smith",
		`CONCAT(first_name; " "; last_name)`: "Mary Smith",
		`UPPER(last_name)`:                   "SMITH",
		`LOWER(last_name)`:                   "smith",
		`TRIM(city)`:                         "New York",
		`LEFT(last_name; 2)`:                 "Sm",
		`RIGHT(last_name; 2)`:                "th",
		`MIDDLE(last_name; 2; 3)`:            "mit",
		`LEFT(last_name; 99)`:                "Smith",
		`COALESCE(middle_name; first_name)`:  "Mary",
	}
	for formula, want := range cases {
		if got := evalText(t, formula, record); got != want {
			t.Errorf("%s = %q, want %q", formula, got, want)
		}
	}

	if got := evalNumber(t, `LENGTH(last_name)`, record); got != 5 {
		t.Errorf("LENGTH = %v, want 5", got)
	}
}

func TestNumbersReadBackWithoutATrailingZero(t *testing.T) {
	// A whole number must not read as "100.0" when a formula builds text.
	got := evalText(t, `"Fee: " & fee_paid`, calc.Record{"fee_paid": 100.0})
	if got != "Fee: 100" {
		t.Fatalf("got %q, want %q", got, "Fee: 100")
	}
}

func TestLogicAndComparison(t *testing.T) {
	record := calc.Record{"fee_paid": 200.0, "city": "London", "notes": nil}

	cases := map[string]bool{
		`fee_paid >= 200`:                     true,
		`fee_paid > 200`:                      false,
		`fee_paid = 200`:                      true,
		`fee_paid <> 100`:                     true,
		`city = "london"`:                     true,
		`city <> "Paris"`:                     true,
		`fee_paid >= 100 AND city = "London"`: true,
		`fee_paid > 500 OR city = "London"`:   true,
		`NOT (fee_paid = 200)`:                false,
		`ISEMPTY(notes)`:                      true,
		`ISEMPTY(city)`:                       false,
	}
	for formula, want := range cases {
		expr, err := calc.Parse(formula)
		if err != nil {
			t.Fatalf("Parse(%q): %v", formula, err)
		}
		got, err := calc.Evaluate(expr, record, calc.ResultBoolean)
		if err != nil {
			t.Fatalf("Evaluate(%q): %v", formula, err)
		}
		if got.(bool) != want {
			t.Errorf("%s = %v, want %v", formula, got, want)
		}
	}
}

func TestAndOrStopEarly(t *testing.T) {
	// The right side would fail (text divided by a number); it must not run.
	expr, err := calc.Parse(`false AND (name / 2) > 1`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := calc.Evaluate(expr, calc.Record{"name": "Mary"}, calc.ResultBoolean); err != nil {
		t.Fatalf("AND should stop before the right side: %v", err)
	}
}

func TestDateFunctions(t *testing.T) {
	calc.Now = func() time.Time { return time.Date(2026, 10, 7, 11, 30, 0, 0, time.UTC) }
	defer func() { calc.Now = time.Now }()

	record := calc.Record{"date_paid": "2011-01-15T00:00:00Z"}

	if got := evalNumber(t, `YEAR(date_paid)`, record); got != 2011 {
		t.Errorf("YEAR = %v, want 2011", got)
	}
	if got := evalNumber(t, `MONTH(date_paid)`, record); got != 1 {
		t.Errorf("MONTH = %v, want 1", got)
	}
	if got := evalNumber(t, `DAY(date_paid)`, record); got != 15 {
		t.Errorf("DAY = %v, want 15", got)
	}
	if got := evalNumber(t, `YEAR(TODAY())`, nil); got != 2026 {
		t.Errorf("YEAR(TODAY()) = %v, want 2026", got)
	}

	expr, _ := calc.Parse(`TODAY()`)
	stored, err := calc.Evaluate(expr, nil, calc.ResultDate)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if stored != "2026-10-07" {
		t.Errorf("TODAY() stored as %v, want 2026-10-07", stored)
	}
}

func TestResultTypeShapesTheStoredValue(t *testing.T) {
	expr, _ := calc.Parse(`1 + 1`)

	if got, _ := calc.Evaluate(expr, nil, calc.ResultNumber); got != 2.0 {
		t.Errorf("as number = %v, want 2", got)
	}
	if got, _ := calc.Evaluate(expr, nil, calc.ResultText); got != "2" {
		t.Errorf("as text = %v, want \"2\"", got)
	}
	if got, _ := calc.Evaluate(expr, nil, calc.ResultBoolean); got != true {
		t.Errorf("as boolean = %v, want true", got)
	}
}

func TestEmptyResultIsStoredAsNothing(t *testing.T) {
	expr, _ := calc.Parse(`notes`)
	got, err := calc.Evaluate(expr, calc.Record{"notes": nil}, calc.ResultText)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestFieldsListsWhatTheFormulaReads(t *testing.T) {
	expr, err := calc.Parse(`IF(customer_type = "New"; fee_paid * 2; fee_paid) & " " & last_name`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := expr.Fields()
	want := []string{"customer_type", "fee_paid", "last_name"}
	if len(got) != len(want) {
		t.Fatalf("Fields() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Fields() = %v, want %v", got, want)
		}
	}
}

func TestParseErrorsPointAtTheProblem(t *testing.T) {
	cases := []struct {
		formula string
		wants   string
	}{
		{``, "the formula is empty"},
		{`   `, "the formula is empty"},
		{`1 +`, "ends too early"},
		{`(1 + 2`, `expected ")"`},
		{`NOSUCHFN(1)`, "there is no function"},
		{`ROUND()`, "needs at least 1"},
		{`UPPER(1; 2)`, "takes at most 1"},
		{`"unterminated`, "unterminated text literal"},
		{`1 # 2`, "unexpected character"},
		{`1 2`, "after the end of the formula"},
	}
	for _, c := range cases {
		_, err := calc.Parse(c.formula)
		if err == nil {
			t.Errorf("Parse(%q) should have failed", c.formula)
			continue
		}
		if !strings.Contains(err.Error(), c.wants) {
			t.Errorf("Parse(%q) = %v, want it to mention %q", c.formula, err, c.wants)
		}
		var ce *calc.Error
		if !asCalcError(err, &ce) {
			t.Errorf("Parse(%q) returned %T, want *calc.Error with a position", c.formula, err)
		}
	}
}

func TestEvaluationErrorsAreReported(t *testing.T) {
	cases := []struct {
		formula string
		record  calc.Record
		wants   string
	}{
		{`fee / 0`, calc.Record{"fee": 10.0}, "division by zero"},
		{`name * 2`, calc.Record{"name": "Mary"}, "is not a number"},
		{`YEAR(name)`, calc.Record{"name": "Mary"}, "is not a date"},
	}
	for _, c := range cases {
		expr, err := calc.Parse(c.formula)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.formula, err)
		}
		if _, err := calc.Evaluate(expr, c.record, calc.ResultNumber); err == nil ||
			!strings.Contains(err.Error(), c.wants) {
			t.Errorf("Evaluate(%q) = %v, want it to mention %q", c.formula, err, c.wants)
		}
	}
}

func TestResultTypeSpellings(t *testing.T) {
	// The field options dialog writes "Text", "Number", "Date", "Time",
	// "Timestamp"; anything else falls back to text.
	cases := map[string]calc.ResultType{
		"Text":      calc.ResultText,
		"number":    calc.ResultNumber,
		"Date":      calc.ResultDate,
		"Time":      calc.ResultTimestamp,
		"Timestamp": calc.ResultTimestamp,
		"Boolean":   calc.ResultBoolean,
		"":          calc.ResultText,
		"nonsense":  calc.ResultText,
	}
	for in, want := range cases {
		if got := calc.ParseResultType(in); got != want {
			t.Errorf("ParseResultType(%q) = %v, want %v", in, got, want)
		}
	}
}

func asCalcError(err error, target **calc.Error) bool {
	if ce, ok := err.(*calc.Error); ok {
		*target = ce
		return true
	}
	return false
}
