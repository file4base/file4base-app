// Package calc parses and evaluates the formulas of calculation fields.
//
// Formulas are evaluated here, in Go, rather than translated to SQL, so a
// calculation gives the same answer on PostgreSQL and on MariaDB. A field's
// value is computed when the record is written and stored in its column, so
// finding and sorting on a calculation field work like any other field.
//
// The grammar is in docs/specs/calculation_formulas.md.
package calc

import (
	"fmt"
	"strings"
	"unicode"
)

// Error is a formula that could not be parsed or evaluated. Pos is the 0-based
// rune offset in the formula, so the caller can point at what is wrong.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s (at position %d)", e.Msg, e.Pos) }

func errorAt(pos int, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokString
	tokIdent
	tokOperator
	tokLParen
	tokRParen
	tokComma
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

func (t token) isOperator(op string) bool {
	return t.kind == tokOperator && t.text == op
}

// isKeyword reports whether the token is the given word, ignoring case. The
// word operators (AND, OR, NOT) and the function names are matched this way.
func (t token) isKeyword(word string) bool {
	return t.kind == tokIdent && strings.EqualFold(t.text, word)
}

// lex turns a formula into tokens. Operators are matched longest-first so that
// ">=" does not come out as ">" followed by "=".
var multiCharOperators = []string{"<>", "!=", "<=", ">=", "=="}

func lex(src string) ([]token, error) {
	runes := []rune(src)
	var tokens []token
	i := 0

	for i < len(runes) {
		ch := runes[i]

		switch {
		case unicode.IsSpace(ch):
			i++

		case ch == '(':
			tokens = append(tokens, token{kind: tokLParen, text: "(", pos: i})
			i++

		case ch == ')':
			tokens = append(tokens, token{kind: tokRParen, text: ")", pos: i})
			i++

		// FileMaker separates arguments with ";"; "," is accepted too.
		case ch == ',' || ch == ';':
			tokens = append(tokens, token{kind: tokComma, text: string(ch), pos: i})
			i++

		case ch == '"' || ch == '\'':
			text, next, err := lexString(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: tokString, text: text, pos: i})
			i = next

		case unicode.IsDigit(ch) || (ch == '.' && i+1 < len(runes) && unicode.IsDigit(runes[i+1])):
			start := i
			seenDot := false
			for i < len(runes) && (unicode.IsDigit(runes[i]) || (runes[i] == '.' && !seenDot)) {
				if runes[i] == '.' {
					seenDot = true
				}
				i++
			}
			tokens = append(tokens, token{kind: tokNumber, text: string(runes[start:i]), pos: start})

		case ch == '_' || unicode.IsLetter(ch):
			start := i
			for i < len(runes) && (runes[i] == '_' || unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			tokens = append(tokens, token{kind: tokIdent, text: string(runes[start:i]), pos: start})

		default:
			if matched := matchOperator(runes, i); matched != "" {
				tokens = append(tokens, token{kind: tokOperator, text: matched, pos: i})
				i += len([]rune(matched))
				continue
			}
			return nil, errorAt(i, "unexpected character %q", string(ch))
		}
	}

	tokens = append(tokens, token{kind: tokEOF, pos: len(runes)})
	return tokens, nil
}

func matchOperator(runes []rune, i int) string {
	for _, op := range multiCharOperators {
		opRunes := []rune(op)
		if i+len(opRunes) <= len(runes) && string(runes[i:i+len(opRunes)]) == op {
			return op
		}
	}
	if strings.ContainsRune("+-*/&=<>", runes[i]) {
		return string(runes[i])
	}
	return ""
}

// lexString reads a quoted string. The quote that opened it closes it, and it
// may be doubled inside to mean itself ("say ""hi""").
func lexString(runes []rune, start int) (string, int, error) {
	quote := runes[start]
	var b strings.Builder
	i := start + 1
	for i < len(runes) {
		if runes[i] == quote {
			if i+1 < len(runes) && runes[i+1] == quote {
				b.WriteRune(quote)
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteRune(runes[i])
		i++
	}
	return "", 0, errorAt(start, "unterminated text literal")
}
