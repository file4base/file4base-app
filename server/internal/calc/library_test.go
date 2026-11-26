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
		{Name: "Caller", Parameters: []string{"x"}, Formula: "Between ( x ) + 1"},
		{Name: "Between", Parameters: []string{"x"}, Formula: "Broken ( x ) * 2"},
		{Name: "Broken", Parameters: []string{"x"}, Formula: "x + + )"},
		{Name: "Fine", Parameters: []string{"x"}, Formula: "x * 3"},
	}, Options{})
	var broken *LibraryError
	if !errors.As(err, &broken) {
		t.Fatalf("err = %v, want a *LibraryError", err)
	}
	if len(broken.Problems) != 3 {
		t.Fatalf("problems = %v, want Broken, Between and Caller", broken.Problems)
	}
	if got := broken.Problems["Between"].Error(); got != "calls the custom function Broken, which cannot be read" {
		t.Errorf("Between: %s", got)
	}
	if got := broken.Problems["Caller"].Error(); got != "calls the custom function Between, which cannot be read" {
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
	_, err := Parse(`NoSuchFunction ( "Logo" ; "content" )`)
	var problem *Error
	if !errors.As(err, &problem) || problem.Function != "NoSuchFunction" {
		t.Fatalf("err = %#v, want the function named", err)
	}
}

// A custom function cannot take the name of a built-in one, whatever its
// case, as FileMaker does not let it: it is left out, and so is every
// function that calls it, which meant it and not the built-in one. A bare
// Pi then stays the built-in, and never reaches the refused Pi(x).
func TestCustomFunctionCannotTakeABuiltinName(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "pi", Parameters: []string{"x"}, Formula: "x * 2"},
		{Name: "Trim", Parameters: []string{"t"}, Formula: "t"},
		{Name: "Tidy", Parameters: []string{"t"}, Formula: "Trim ( t )"},
		{Name: "Twice", Parameters: []string{"x"}, Formula: "x * 2"},
	}, Options{})
	var refused *LibraryError
	if !errors.As(err, &refused) || len(refused.Problems) != 3 {
		t.Fatalf("err = %v, want pi, Trim and Tidy refused", err)
	}
	if got := refused.Problems["pi"].Error(); got != "is named like the built-in function PI, which a custom function cannot replace" {
		t.Errorf("pi: %s", got)
	}
	if got := refused.Problems["Tidy"].Error(); got != "calls the custom function Trim, which cannot be read" {
		t.Errorf("Tidy: %s", got)
	}
	if names := lib.Names(); strings.Join(names, ",") != "Twice" {
		t.Errorf("Names() = %v, want only Twice", names)
	}
	expr, err := ParseWith("Round ( Pi ; 2 ) + Twice ( 1 )", Options{Library: lib})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Evaluate(expr, Record{}, ResultNumber)
	if err != nil || fmt.Sprint(got) != "5.14" {
		t.Errorf("Round ( Pi ; 2 ) + Twice ( 1 ) = %v, %v; want 5.14", got, err)
	}
}

// A custom function called with another number of arguments than it takes
// is an error naming it and what it takes, when the formula is read and, for
// a formula read with other functions than it is computed with, when it is
// computed; never a crash.
func TestCustomFunctionArity(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{
		{Name: "Twice", Parameters: []string{"x"}, Formula: "x * 2"},
		{Name: "kOne", Formula: "1"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for formula, want := range map[string]string{
		"Twice ( 1 ; 2 )": "Twice takes 1 argument(s), got 2",
		"Twice ( )":       "Twice takes 1 argument(s), got 0",
		"kOne ( 1 )":      "kOne takes 0 argument(s), got 1",
	} {
		if _, err := ParseWith(formula, Options{Library: lib}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", formula, err, want)
		}
	}
	// Twice named bare, as a formula read without it would hold it.
	expr := &Expr{root: callNode{name: "TWICE"}, src: "Twice", lib: lib}
	if _, err := Evaluate(expr, Record{}, ResultNumber); err == nil || !strings.Contains(err.Error(), "Twice takes 1 argument(s), got 0") {
		t.Errorf("bare Twice: %v", err)
	}
}

// A field named like a custom function, or like Pi, is quoted when a
// formula is written with it, so it is read back as the field.
func TestQuoteFieldName(t *testing.T) {
	lib, err := NewLibrary([]CustomFunction{{Name: "Rate", Formula: "0.2"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"rate": "${rate}", "pi": "${pi}", "price": "price", "Self": "${Self}"} {
		if got := QuoteFieldName(name, lib); got != want {
			t.Errorf("QuoteFieldName(%q) = %q, want %q", name, got, want)
		}
	}
	expr, err := ParseWith("${rate} * 10 + Rate", Options{Library: lib})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Evaluate(expr, Record{"rate": 3}, ResultNumber)
	if err != nil || fmt.Sprint(got) != "30.2" {
		t.Errorf("${rate} * 10 + Rate = %v, %v; want 30.2", got, err)
	}
}
