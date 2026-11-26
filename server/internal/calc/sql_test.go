package calc

import (
	"errors"
	"reflect"
	"testing"
)

// sqlContext answers every query with the same rows, and keeps what it
// was asked.
type sqlContext struct {
	RecordContext
	rows  [][]string
	err   error
	query string
	args  []SQLArgument
}

func (c *sqlContext) ExecuteSQL(query string, args []SQLArgument) ([][]string, error) {
	c.query, c.args = query, args
	return c.rows, c.err
}

func sqlAnswer(t *testing.T, ctx Context, formula string) any {
	t.Helper()
	expr, err := Parse(formula)
	if err != nil {
		t.Fatalf("%s: %v", formula, err)
	}
	v, err := EvaluateWith(expr, ctx, ResultText)
	if err != nil {
		t.Fatalf("%s: %v", formula, err)
	}
	return v
}

// The rows are joined with the separators given, a comma and a return
// when they are empty.
func TestExecuteSQLSeparators(t *testing.T) {
	ctx := &sqlContext{rows: [][]string{{"Ada", "12"}, {"Grace", ""}}}
	for formula, want := range map[string]any{
		`ExecuteSQL ( "SELECT a, b FROM t" ; "" ; "" )`:           "Ada,12\rGrace,",
		`ExecuteSQL ( "SELECT a, b FROM t" ; " | " ; "¶" )`:       "Ada | 12\rGrace | ",
		`ExecuteSQL ( "SELECT a, b FROM t" ; Char ( 9 ) ; "; " )`: "Ada\t12; Grace\t",
	} {
		if got := sqlAnswer(t, ctx, formula); got != want {
			t.Errorf("%s = %q, want %q", formula, got, want)
		}
	}
	ctx.rows = nil
	if got := sqlAnswer(t, ctx, `ExecuteSQL ( "SELECT a FROM t" ; "" ; "" )`); got != nil {
		t.Errorf("no rows = %q, want empty", got)
	}
}

// Arguments are given with their types.
func TestExecuteSQLArguments(t *testing.T) {
	ctx := &sqlContext{rows: [][]string{{"1"}}}
	sqlAnswer(t, ctx, `ExecuteSQL ( "SELECT a FROM t WHERE a = ?" ; "" ; "" ; "x" ; 12.5 ; Date ( 1 ; 5 ; 2026 ) ; `+
		`Time ( 13 ; 4 ; 5 ) ; Timestamp ( Date ( 1 ; 5 ; 2026 ) ; Time ( 13 ; 4 ; 5 ) ) ; "" )`)
	want := []SQLArgument{
		{Kind: "text", Text: "x"}, {Kind: "number", Text: "12.5"}, {Kind: "date", Text: "2026-01-05"},
		{Kind: "time", Text: "13:04:05"}, {Kind: "timestamp", Text: "2026-01-05 13:04:05"}, {Kind: "text", Text: ""},
	}
	if ctx.query != "SELECT a FROM t WHERE a = ?" || !reflect.DeepEqual(ctx.args, want) {
		t.Errorf("asked %q %#v", ctx.query, ctx.args)
	}
}

// A query that fails answers "?"; ExecuteSQLe says why.
func TestExecuteSQLErrors(t *testing.T) {
	ctx := &sqlContext{err: errors.New(`FQL0007/(1:7): The column named "x" does not exist`)}
	if got := sqlAnswer(t, ctx, `ExecuteSQL ( "SELECT x FROM t" ; "" ; "" )`); got != "?" {
		t.Errorf("got %q, want ?", got)
	}
	if got := sqlAnswer(t, ctx, `ExecuteSQLe ( "SELECT x FROM t" ; "" ; "" )`); got != `? ERROR: FQL0007/(1:7): The column named "x" does not exist` {
		t.Errorf("got %q", got)
	}
	// Where no query can run, as in a stored calculation, it answers "?".
	if got := sqlAnswer(t, &RecordContext{}, `ExecuteSQL ( "SELECT a FROM t" ; "" ; "" )`); got != "?" {
		t.Errorf("got %q, want ?", got)
	}
	if _, err := Parse(`ExecuteSQL ( "SELECT a FROM t" ; "" )`); err == nil {
		t.Error("ExecuteSQL with two parameters parsed")
	}
}

func TestRunsSQL(t *testing.T) {
	for formula, want := range map[string]bool{
		`ExecuteSQL ( "SELECT a FROM t" ; "" ; "" )`:                  true,
		`Let ( q = ExecuteSQLe ( "SELECT a FROM t" ; "" ; "" ) ; q )`: true,
		`Upper ( "ExecuteSQL" )`:                                      false,
	} {
		expr, err := Parse(formula)
		if err != nil {
			t.Fatal(err)
		}
		if expr.RunsSQL() != want {
			t.Errorf("%s: RunsSQL = %v", formula, !want)
		}
	}
}
