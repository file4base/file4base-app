package calc

import (
	"strings"
	"testing"
)

func renameLibraries(t *testing.T, defs []CustomFunction, from, to string) (*Library, *Library) {
	t.Helper()
	before, err := NewLibrary(defs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var renamed []CustomFunction
	for _, d := range defs {
		if strings.EqualFold(d.Name, from) {
			d.Name = to
		}
		renamed = append(renamed, d)
	}
	after, err := NewLibrary(renamed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return before, after
}

// A rename rewrites every call, with arguments or by the name alone, and
// nothing else: text, comments and a field of the same name stay as written.
func TestRenameFunction(t *testing.T) {
	defs := []CustomFunction{
		{Name: "TaxRate", Formula: "0.2"},
		{Name: "WithTax", Parameters: []string{"amount"}, Formula: "amount * ( 1 + TaxRate )"},
	}
	before, after := renameLibraries(t, defs, "WithTax", "GrossAmount")
	opts := Options{Library: before}
	src := `withtax ( price ) & " WithTax " // WithTax ( 1 )` + "\n+ WithTax(WithTax ( 2 ))"
	out, changed, err := RenameFunction(src, nil, opts, after, "WithTax", "GrossAmount")
	if err != nil || !changed {
		t.Fatalf("RenameFunction: %v %v", changed, err)
	}
	want := `GrossAmount ( price ) & " WithTax " // WithTax ( 1 )` + "\n+ GrossAmount(GrossAmount ( 2 ))"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}

	// A constant is called by its name alone, also inside another
	// function's formula, where the parameters are names of their own.
	before, after = renameLibraries(t, defs, "TaxRate", "VatRate")
	out, changed, err = RenameFunction(defs[1].Formula, defs[1].Parameters, Options{Library: before}, after, "TaxRate", "VatRate")
	if err != nil || !changed || out != "amount * ( 1 + VatRate )" {
		t.Errorf("got %q %v %v", out, changed, err)
	}

	// A formula that does not call it is left alone.
	out, changed, err = RenameFunction("price * 2", nil, Options{Library: before}, after, "TaxRate", "VatRate")
	if err != nil || changed || out != "price * 2" {
		t.Errorf("got %q %v %v", out, changed, err)
	}

	// A case-only rename rewrites the spelling.
	before, after = renameLibraries(t, defs, "TaxRate", "TAXRATE")
	out, changed, err = RenameFunction("TaxRate * 2", nil, Options{Library: before}, after, "TaxRate", "TAXRATE")
	if err != nil || !changed || out != "TAXRATE * 2" {
		t.Errorf("got %q %v %v", out, changed, err)
	}
}

// A field read as a plain word that a rename would turn into a call to the
// renamed function is refused, rather than silently changing the formula.
func TestRenameFunctionRefusesCapturingAField(t *testing.T) {
	defs := []CustomFunction{{Name: "TaxRate", Formula: "0.2"}}
	before, after := renameLibraries(t, defs, "TaxRate", "discount")
	_, _, err := RenameFunction("price - discount", nil, Options{Library: before}, after, "TaxRate", "discount")
	if err == nil || !strings.Contains(err.Error(), "${discount}") {
		t.Fatalf("want a refusal naming the field, got %v", err)
	}
	// Quoted, the field stays a field.
	out, changed, err := RenameFunction("price - ${discount} * TaxRate", nil, Options{Library: before}, after, "TaxRate", "discount")
	if err != nil || !changed || out != "price - ${discount} * discount" {
		t.Errorf("got %q %v %v", out, changed, err)
	}
}

func TestCallsFunction(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "Inner", Formula: "1"},
		{Name: "Outer", Formula: "Inner + 1"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	expr, err := ParseWith("Outer * 2", Options{Library: lib})
	if err != nil {
		t.Fatal(err)
	}
	if !expr.CallsFunction("outer") || expr.CallsFunction("Inner") {
		t.Errorf("CallsFunction must look at the formula itself only")
	}
}

// Recursion is allowed, as in FileMaker, as long as a condition can end it.
func TestRecursesEndlessly(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "Countdown", Parameters: []string{"n"}, Formula: "If ( n <= 0 ; 0 ; Countdown ( n - 1 ) )"},
		{Name: "Forever", Parameters: []string{"n"}, Formula: "Forever ( n + 1 )"},
		{Name: "Ping", Parameters: []string{"n"}, Formula: "Pong ( n ) + 1"},
		{Name: "Pong", Parameters: []string{"n"}, Formula: "Let ( x = n ; Ping ( x ) )"},
		{Name: "Guarded", Parameters: []string{"n"}, Formula: "n > 0 and Guarded ( n - 1 )"},
		{Name: "InCondition", Parameters: []string{"n"}, Formula: "If ( InCondition ( n ) ; 1 ; 0 )"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"Countdown": false, "Forever": true, "Ping": true, "pong": true, "Guarded": false, "InCondition": true, "missing": false,
	} {
		if got := lib.RecursesEndlessly(name); got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}
