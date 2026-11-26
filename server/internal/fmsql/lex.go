// Package fmsql reads the SQL FileMaker's ExecuteSQL function accepts and
// translates it into a parameterised query of the solution's database.
//
// A query is parsed into a syntax tree (Parse), its names are resolved
// against the solution's table occurrences and fields (Translate), and the
// statement run is built from that tree alone: table and field names are the
// catalog's, quoted by the dialect, and every value written in the query or
// passed as a ? argument is bound as a parameter. Nothing of the query's text
// reaches the database. What the subset does not cover is refused with a
// FileMaker SQL error (FQLnnnn), as ExecuteSQL answers "?" for it.
//
// The subset and its rules are described in docs/specs/calculation_formulas.md.
package fmsql

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error is a query that cannot be read or run, as FileMaker reports one:
// FQLnnnn/(line:offset): message, line from 1 and offset from 0, in runes.
type Error struct {
	Code    int
	Line    int
	Offset  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("FQL%04d/(%d:%d): %s", e.Code, e.Line, e.Offset, e.Message)
}

// The FileMaker SQL error codes the package answers with.
const (
	codeSyntax            = 1
	codeNoTable           = 2
	codeDuplicateTable    = 3
	codeTooComplex        = 4
	codeAggregate         = 5
	codeAmbiguousColumn   = 6
	codeNoColumn          = 7
	codeNoTableInScope    = 8
	codeNoColumnInTable   = 9
	codeBadTemporal       = 10
	codeNotPredicate      = 11
	codeBadOrdinal        = 12
	codeIncompatible      = 13
	codeNotComparable     = 17
	codeIncompatibleTypes = 18
	codeArgumentCount     = 20
	codeNotGrouped        = 25
	codeInvalidFunction   = 30
	codeAggregateInWhere  = 49
	codeBadOffset         = 50
	codeBadFetch          = 52
)

// errorAt is an error at a byte offset of the query.
func errorAt(src string, pos, code int, format string, args ...any) *Error {
	line, offset := 1, 0
	if pos > len(src) {
		pos = len(src)
	}
	prev := rune(0)
	for _, r := range src[:pos] {
		switch {
		case r == '\n' && prev == '\r':
			// CRLF ends one line.
		case r == '\n' || r == '\r':
			line++
			offset = 0
		default:
			offset++
		}
		prev = r
	}
	return &Error{Code: code, Line: line, Offset: offset, Message: fmt.Sprintf(format, args...)}
}

type tokenKind int

const (
	tokEOF    tokenKind = iota
	tokWord             // a name or a keyword written as a plain word
	tokQuoted           // a "quoted" name
	tokString           // a 'text' constant
	tokNumber
	tokParam // ?
	tokSymbol
)

type token struct {
	kind tokenKind
	text string // the name, the text constant without its quotes, the symbol
	pos  int    // byte offset in the query
}

// is reports whether the token is the keyword or symbol s, whatever its case.
func (t token) is(s string) bool {
	return (t.kind == tokWord || t.kind == tokSymbol) && strings.EqualFold(t.text, s)
}

// maxQueryLength bounds what is read, so a formula cannot make the server
// tokenize an arbitrarily long text.
const maxQueryLength = 64 << 10

func lex(src string) ([]token, error) {
	if len(src) > maxQueryLength {
		return nil, errorAt(src, 0, codeTooComplex, "The query is longer than %d bytes.", maxQueryLength)
	}
	if !utf8.ValidString(src) {
		return nil, errorAt(src, 0, codeSyntax, "There is an error in the syntax of the query.")
	}
	var out []token
	i := 0
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		switch {
		case unicode.IsSpace(r):
			i += size
		case strings.HasPrefix(src[i:], "--"):
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, errorAt(src, i, codeSyntax, "There is an error in the syntax of the query: a comment is not closed.")
			}
			i += end + 4
		case unicode.IsLetter(r):
			start := i
			for i < len(src) {
				r, size := utf8.DecodeRuneInString(src[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				i += size
			}
			out = append(out, token{kind: tokWord, text: src[start:i], pos: start})
		case r == '"' || r == '\'':
			start := i
			text, end, ok := quoted(src, i, byte(r))
			if !ok {
				return nil, errorAt(src, start, codeSyntax, "There is an error in the syntax of the query: a %c is not closed.", r)
			}
			kind := tokString
			if r == '"' {
				kind = tokQuoted
				if text == "" {
					return nil, errorAt(src, start, codeSyntax, "There is an error in the syntax of the query: a name is empty.")
				}
			}
			out = append(out, token{kind: kind, text: text, pos: start})
			i = end
		case r >= '0' && r <= '9' || r == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			start := i
			i = number(src, i)
			if i < len(src) {
				if next, _ := utf8.DecodeRuneInString(src[i:]); unicode.IsLetter(next) || next == '_' {
					return nil, errorAt(src, start, codeSyntax, "There is an error in the syntax of the query.")
				}
			}
			out = append(out, token{kind: tokNumber, text: src[start:i], pos: start})
		case r == '?':
			out = append(out, token{kind: tokParam, text: "?", pos: i})
			i++
		default:
			sym := ""
			for _, s := range []string{"<>", "<=", ">=", "!=", "||", "=", "<", ">", "+", "-", "*", "/", "(", ")", ",", "."} {
				if strings.HasPrefix(src[i:], s) {
					sym = s
					break
				}
			}
			if sym == "" {
				return nil, errorAt(src, i, codeSyntax, "There is an error in the syntax of the query.")
			}
			if sym == "!=" {
				sym = "<>"
			}
			out = append(out, token{kind: tokSymbol, text: sym, pos: i})
			i += len(sym)
		}
	}
	return append(out, token{kind: tokEOF, pos: len(src)}), nil
}

// quoted reads a text constant or a quoted name starting at the quote at i:
// a doubled quote stands for one.
func quoted(src string, i int, q byte) (string, int, bool) {
	var b strings.Builder
	i++
	for i < len(src) {
		if src[i] == q {
			if i+1 < len(src) && src[i+1] == q {
				b.WriteByte(q)
				i += 2
				continue
			}
			return b.String(), i + 1, true
		}
		b.WriteByte(src[i])
		i++
	}
	return "", i, false
}

// number reads digits, a decimal point and digits, and an exponent.
func number(src string, i int) int {
	digits := func() {
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	}
	digits()
	if i < len(src) && src[i] == '.' {
		i++
		digits()
	}
	if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
		j := i + 1
		if j < len(src) && (src[j] == '+' || src[j] == '-') {
			j++
		}
		if j < len(src) && src[j] >= '0' && src[j] <= '9' {
			i = j
			digits()
		}
	}
	return i
}
