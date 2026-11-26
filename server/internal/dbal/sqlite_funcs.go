package dbal

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
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
//   - The F4B collation orders text as a sort in a calculation does
//     (calc.CompareText); sorts use it, comparisons do not.
const (
	// SQLiteSortCollation is the collation text sorts use on SQLite.
	SQLiteSortCollation = "F4B"
)

func init() {
	sqliteDriver.MustRegisterDeterministicScalarFunction("lower", 1, mapTextArg(unicode.ToLower))
	sqliteDriver.MustRegisterDeterministicScalarFunction("upper", 1, mapTextArg(unicode.ToUpper))
	sqliteDriver.MustRegisterDeterministicScalarFunction("regexp", 2, sqliteRegexp)
	sqliteDriver.MustRegisterDeterministicScalarFunction("f4b_lines", 1, sqliteLines)
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
func SQLiteTypeCheck(column, declType string) string {
	switch strings.ToUpper(declType) {
	case "NUMERIC":
		return fmt.Sprintf("CHECK (typeof(%s) IN ('integer', 'real', 'null'))", column)
	case "DATE", "TIMESTAMP":
		return fmt.Sprintf("CHECK (%[1]s IS NULL OR (typeof(%[1]s) = 'text' AND julianday(%[1]s) IS NOT NULL))", column)
	case "BOOLEAN":
		return fmt.Sprintf("CHECK (%[1]s IS NULL OR %[1]s IN (0, 1))", column)
	}
	return ""
}
