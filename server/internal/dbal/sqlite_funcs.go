package dbal

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"modernc.org/sqlite"

	"github.com/file4base/file4base-app/server/internal/calc"
)

// The functions and the collation every SQLite solution connection has
// (docs/specs/sqlite_engine.md, "Text"). They are registered on
// sqliteDriver, which opens solution databases only.
//
//   - lower and upper replace SQLite's, which change ASCII letters only, by
//     a character-by-character Unicode mapping: the one calculations use,
//     and PostgreSQL's in a UTF-8 database, so a find matches "ÉCOLE" with
//     "école" on every engine.
//   - regexp gives the REGEXP operator (dbal.NumericPattern) RE2 semantics.
//   - f4b_lines is the lines of a text, without the empty ones, as a JSON
//     array for json_each: a multi-key match field.
//   - f4b_number_text is a number's text as File4Base reads it back
//     (normalizeSQLiteValue): 0.000015, where CAST gives 1.5e-05.
//   - stddev_samp is PostgreSQL's aggregate, which SQLite lacks: the
//     standard deviation a summary field computes.
//   - The F4B collation orders text as a sort in a calculation does
//     (calc.CompareText); sorts use it, comparisons do not.
const (
	// SQLiteSortCollation is the collation text sorts use on SQLite.
	SQLiteSortCollation = "F4B"
	// SQLiteSortKey is the function giving a text's sort key
	// (calc.SortKey): text sorts order by it, and sorted fields are indexed
	// on it.
	SQLiteSortKey = "f4b_sortkey"
	// SQLiteNumberText is the function giving a number's text.
	SQLiteNumberText = "f4b_number_text"
)

// sqliteSortKey is a text's sort key; NULL stays NULL, and a value that is
// not text (a number in a text field) is keyed as its text.
func sqliteSortKey(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	switch v := args[0].(type) {
	case nil:
		return nil, nil
	case string:
		return calc.SortKey(v), nil
	case []byte:
		return calc.SortKey(string(v)), nil
	case int64:
		return calc.SortKey(strconv.FormatInt(v, 10)), nil
	case float64:
		return calc.SortKey(strconv.FormatFloat(v, 'f', -1, 64)), nil
	}
	return args[0], nil
}

func sqliteNumberText(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	switch v := args[0].(type) {
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case []byte:
		return string(v), nil
	}
	return args[0], nil
}

func init() {
	sqliteDriver.MustRegisterDeterministicScalarFunction("lower", 1, mapTextArg(unicode.ToLower))
	sqliteDriver.MustRegisterDeterministicScalarFunction("upper", 1, mapTextArg(unicode.ToUpper))
	sqliteDriver.MustRegisterDeterministicScalarFunction("regexp", 2, sqliteRegexp)
	sqliteDriver.MustRegisterDeterministicScalarFunction("f4b_lines", 1, sqliteLines)
	sqliteDriver.MustRegisterDeterministicScalarFunction(SQLiteNumberText, 1, sqliteNumberText)
	sqliteDriver.MustRegisterDeterministicScalarFunction(SQLiteSortKey, 1, sqliteSortKey)
	sqliteDriver.MustRegisterFunction("stddev_samp", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		MakeAggregate: func(sqlite.FunctionContext) (sqlite.AggregateFunction, error) { return &stddevSamp{}, nil },
	})
	if err := sqliteDriver.RegisterCollationUtf8(SQLiteSortCollation, sortCollation); err != nil {
		panic(err)
	}
}

// mapTextArg maps every character of a text argument; NULL stays NULL and
// a number, which has no case, is returned as it is.
func mapTextArg(mapping func(rune) rune) func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
	return func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		switch v := args[0].(type) {
		case string:
			return strings.Map(mapping, v), nil
		case []byte:
			return strings.Map(mapping, string(v)), nil
		default:
			return v, nil
		}
	}
}

// regexpCache keeps the compiled patterns. The patterns are File4Base's own
// (no user text becomes a pattern), so the cache stays small; it is bounded
// all the same.
var regexpCache sync.Map

const regexpCacheLimit = 256

var regexpCacheSize = struct {
	sync.Mutex
	n int
}{}

func compiledRegexp(pattern string) (*regexp.Regexp, error) {
	if re, ok := regexpCache.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexpCacheSize.Lock()
	if regexpCacheSize.n < regexpCacheLimit {
		if _, loaded := regexpCache.LoadOrStore(pattern, re); !loaded {
			regexpCacheSize.n++
		}
	}
	regexpCacheSize.Unlock()
	return re, nil
}

// sqliteRegexp is regexp(pattern, value), which X REGEXP Y calls as
// regexp(Y, X). It is NULL when either is NULL, as the operator is on the
// other engines.
func sqliteRegexp(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if args[0] == nil || args[1] == nil {
		return nil, nil
	}
	re, err := compiledRegexp(textOf(args[0]))
	if err != nil {
		return nil, err
	}
	if re.MatchString(textOf(args[1])) {
		return int64(1), nil
	}
	return int64(0), nil
}

// sqliteLines is f4b_lines(text): its lines, CR and LF both ending one,
// without the empty ones, as a JSON array. NULL has no lines.
func sqliteLines(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if args[0] == nil {
		return "[]", nil
	}
	lines := strings.FieldsFunc(textOf(args[0]), func(r rune) bool { return r == '\n' || r == '\r' })
	if lines == nil {
		lines = []string{}
	}
	out, err := json.Marshal(lines)
	if err != nil {
		return nil, err
	}
	return string(out), nil
}

// stddevSamp is the sample standard deviation of the numbers stepped
// through, by Welford's method; NULL, and a text, are left out, and it is
// NULL for fewer than two numbers, as on PostgreSQL.
type stddevSamp struct {
	n int64
	// The numbers are divided by scale, a power of two (so the division is
	// exact) at least the largest magnitude stepped through so far, so
	// their squares neither overflow nor vanish.
	scale     float64
	mean, sum float64 // sum of the squared differences from the mean
}

func (a *stddevSamp) Step(_ *sqlite.FunctionContext, args []driver.Value) error {
	var x float64
	switch v := args[0].(type) {
	case int64:
		x = float64(v)
	case float64:
		x = v
	default:
		return nil
	}
	if m := math.Abs(x); m >= a.scale && m > 0 {
		_, exp := math.Frexp(m) // m < 2^exp
		scale := math.Ldexp(1, exp)
		if a.scale > 0 {
			r := a.scale / scale
			a.mean *= r
			a.sum *= r * r
		}
		a.scale = scale
	}
	if a.scale > 0 {
		x /= a.scale
	}
	a.n++
	d := x - a.mean
	a.mean += d / float64(a.n)
	a.sum += d * (x - a.mean)
	return nil
}

func (a *stddevSamp) WindowInverse(*sqlite.FunctionContext, []driver.Value) error {
	return errors.New("stddev_samp is not a window function")
}

func (a *stddevSamp) WindowValue(*sqlite.FunctionContext) (driver.Value, error) {
	if a.n < 2 {
		return nil, nil
	}
	return a.scale * math.Sqrt(a.sum/float64(a.n-1)), nil
}

func (a *stddevSamp) Final(*sqlite.FunctionContext) {}

func textOf(v driver.Value) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

// sortCollation is the F4B collation. Two ASCII texts, the common case, are
// compared without allocating, by the rule calc.CompareText applies to them:
// ignoring case first, then lower case before upper case.
func sortCollation(a, b string) int {
	if c, ok := compareASCII(a, b); ok {
		return c
	}
	return calc.CompareText(a, b)
}

// compareASCII is calc.CompareText for two ASCII texts; ok is false when
// either is not ASCII.
func compareASCII(a, b string) (c int, ok bool) {
	tie := 0
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		x, y := a[i], b[i]
		if x >= 0x80 || y >= 0x80 {
			return 0, false
		}
		lx, ly := asciiLower(x), asciiLower(y)
		if lx != ly {
			if !isASCII(a[i:]) || !isASCII(b[i:]) {
				return 0, false
			}
			if lx < ly {
				return -1, true
			}
			return 1, true
		}
		if tie == 0 && x != y {
			// The texts are equal ignoring case so far; the first case
			// difference decides when nothing else does: lower case first.
			if x > y {
				tie = -1
			} else {
				tie = 1
			}
		}
	}
	if !isASCII(a[n:]) || !isASCII(b[n:]) {
		return 0, false
	}
	switch {
	case len(a) < len(b):
		return -1, true
	case len(a) > len(b):
		return 1, true
	}
	return tie, true
}

func asciiLower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// SQLiteTypeCheck is the CHECK constraint that keeps a column of a declared
// type to values of that type, as PostgreSQL's types do: SQLite stores any
// value in any column, converting only what its affinity can read ("12"
// into a NUMERIC column becomes 12, "abc" stays text). It is "" for a type
// that holds anything (TEXT, BLOB).
// sqliteSecondsGlob is a timestamp's text up to its seconds.
const sqliteSecondsGlob = "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]"

func SQLiteTypeCheck(column, declType string) string {
	switch strings.ToUpper(declType) {
	case "NUMERIC":
		return fmt.Sprintf("CHECK (typeof(%s) IN ('integer', 'real', 'null'))", column)
	case "DATE":
		// date() reads more than a date ("13:00") and moves a day that is
		// not in the calendar ("2026-02-30" is 2 March): the text must be
		// the day date() gives back.
		return fmt.Sprintf("CHECK (%[1]s IS NULL OR (typeof(%[1]s) = 'text' AND date(%[1]s) IS %[1]s))", column)
	case "TIMESTAMP":
		// The text bound timestamps are stored as: a day of the calendar,
		// a time, in UTC ("2026-01-02 03:04:05.25+00:00").
		// The fraction of a second, if any, is digits only.
		return fmt.Sprintf("CHECK (%[1]s IS NULL OR (typeof(%[1]s) = 'text' AND "+
			"(%[1]s GLOB '"+sqliteSecondsGlob+"+00:00' OR "+
			"(%[1]s GLOB '"+sqliteSecondsGlob+".[0-9]*+00:00' AND substr(%[1]s, 21, length(%[1]s) - 26) NOT GLOB '*[^0-9]*')) AND "+
			"date(%[1]s) IS substr(%[1]s, 1, 10)))", column)
	case "BOOLEAN":
		return fmt.Sprintf("CHECK (%[1]s IS NULL OR %[1]s IN (0, 1))", column)
	}
	return ""
}

// sqliteRetypeValue is the value a type change copies into a column of
// declType from expr: a date or a timestamp written as a client may write
// one ("2026-03-04T12:34:00Z") becomes the text that type is stored as, so
// a find for that day or instant still matches it. A text that names no day
// of the calendar is copied as it is, for the column's check to refuse.
func sqliteRetypeValue(expr, declType string) string {
	// The text must read whole as a date or time (julianday), on a day of
	// the calendar: "2026-03-04 garbage" is copied as it is and refused.
	valid := fmt.Sprintf("date(substr(%[1]s, 1, 10)) IS substr(%[1]s, 1, 10) AND julianday(%[1]s) IS NOT NULL", expr)
	switch strings.ToUpper(declType) {
	case "DATE":
		// The day written, as dbal.NormalizeTemporal keeps it from a text.
		return fmt.Sprintf("(CASE WHEN %[2]s THEN substr(%[1]s, 1, 10) ELSE %[1]s END)", expr, valid)
	case "TIMESTAMP":
		// In UTC, to the millisecond (SQLite's resolution).
		return fmt.Sprintf("(CASE WHEN %[2]s "+
			"THEN rtrim(rtrim(strftime('%%Y-%%m-%%d %%H:%%M:%%f', %[1]s), '0'), '.') || '+00:00' ELSE %[1]s END)", expr, valid)
	}
	return expr
}
