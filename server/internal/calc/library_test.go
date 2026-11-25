package calc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A function may call itself, one defined after it, and one that calls it
// back, whatever order the catalog lists them in.
func TestLibraryResolvesWhateverTheOrder(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "Gross", Parameters: []string{"amount"}, Formula: "Round ( amount * ( 1 + kRate ) ; 2 )"},
		{Name: "IsEven", Parameters: []string{"n"}, Formula: "If ( n = 0 ; 1 ; IsOdd ( n - 1 ) )"},
		{Name: "Factorial", Parameters: []string{"n"}, Formula: "If ( n ≤ 1 ; 1 ; n * Factorial ( n - 1 ) )"},
		{Name: "IsOdd", Parameters: []string{"n"}, Formula: "If ( n = 0 ; 0 ; IsEven ( n - 1 ) )"},
		{Name: "kRate", Formula: "0.2"},
	}, Options{})
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}
	for formula, want := range map[string]string{
		"Gross ( 100 )":       "120",
		"Factorial ( 5 )":     "120",
		"IsEven ( 7 )":        "0",
		"IsOdd ( 7 )":         "1",
		"Gross ( price ) * 2": "24",
	} {
		expr, err := ParseWith(formula, Options{Library: lib})
		if err != nil {
			t.Fatalf("%s: %v", formula, err)
		}
		got, err := Evaluate(expr, Record{"price": 10}, ResultNumber)
		if err != nil {
			t.Fatalf("%s: %v", formula, err)
		}
		if fmt.Sprint(got) != want {
			t.Errorf("%s = %v, want %s", formula, got, want)
		}
	}
}

// A function that cannot be read is left out, and so is every function that
// calls it, each said why; the others still work.
func TestLibraryLeavesOutWhatCallsABrokenFunction(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "Caller", Parameters: []string{"x"}, Formula: "Middle ( x ) + 1"},
		{Name: "Middle", Parameters: []string{"x"}, Formula: "Broken ( x ) * 2"},
		{Name: "Broken", Parameters: []string{"x"}, Formula: "x + + )"},
		{Name: "Fine", Parameters: []string{"x"}, Formula: "x * 3"},
	}, Options{})
	var broken *LibraryError
	if !errors.As(err, &broken) {
		t.Fatalf("err = %v, want a *LibraryError", err)
	}
	if len(broken.Problems) != 3 {
		t.Fatalf("problems = %v, want Broken, Middle and Caller", broken.Problems)
	}
	if got := broken.Problems["Middle"].Error(); got != "calls the custom function Broken, which cannot be read" {
		t.Errorf("Middle: %s", got)
	}
	if got := broken.Problems["Caller"].Error(); got != "calls the custom function Middle, which cannot be read" {
		t.Errorf("Caller: %s", got)
	}
	if names := lib.Names(); strings.Join(names, ",") != "Fine" {
		t.Errorf("Names() = %v, want only Fine", names)
	}
}

// A formula reads what the custom functions it calls read.
func TestExprReadsThroughCustomFunctions(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "Outer", Parameters: []string{"x"}, Formula: "Inner ( x ) & Lines::Label"},
		{Name: "Inner", Parameters: []string{"x"}, Formula: "x & Get ( AccountName ) & Sum ( Lines::Amount ) & Inner ( x )"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	expr, err := ParseWith("Outer ( code )", Options{Library: lib})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(expr.Fields()); got != "[code]" {
		t.Errorf("Fields() = %s, want [code]: a parameter is not a field", got)
	}
	if got := fmt.Sprint(expr.RelatedFields()); got != "[{Lines Amount false} {Lines Label false}]" {
		t.Errorf("RelatedFields() = %s", got)
	}
	if got := fmt.Sprint(expr.GetSelectors()); got != "[ACCOUNTNAME]" {
		t.Errorf("GetSelectors() = %s", got)
	}
	if got := fmt.Sprint(expr.Functions()); got != "[INNER OUTER SUM]" {
		t.Errorf("Functions() = %s", got)
	}
}

// The formula of a custom function is rewritten like a calculation's: its
// field references renamed and its numbers written with ".", its parameters
// left alone.
func TestRewriteFunction(t *testing.T) {
	fr, _ := LocaleFor("fr-FR")
	opts := Options{Tables: map[string][]string{"Catalogue Articles": {"Prix HT"}}, Locale: &fr}
	fn := CustomFunction{Name: "PrixRemisé", Parameters: []string{"taux"}, Formula: "Catalogue Articles::Prix HT * ( 1 - taux ) + 0,5 // taux"}
	lib, err := NewLibrary([]CustomFunction{fn}, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Library = lib
	out, unresolved, err := RewriteFunction(fn, opts, func(table, field string) (string, bool) {
		return "${" + table + "}::prix_ht", table != ""
	})
	if err != nil || len(unresolved) > 0 {
		t.Fatalf("RewriteFunction: %v %v", err, unresolved)
	}
	if want := "${Catalogue Articles}::prix_ht * ( 1 - taux ) + 0.5 // taux"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// A function neither File4Base nor the solution defines is named in the
// error, so a caller can say which one it is.
func TestUnknownFunctionIsNamed(t *testing.T) {
	_, err := Parse(`ExecuteSQL ( "SELECT 1" ; "" ; "" )`)
	var problem *Error
	if !errors.As(err, &problem) || problem.Function != "ExecuteSQL" {
		t.Fatalf("err = %#v, want the function named", err)
	}
}
