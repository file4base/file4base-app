// Package calc parses and evaluates FileMaker calculation formulas.
//
// Formulas are evaluated here, in Go, rather than translated to SQL, so a
// calculation gives the same answer on PostgreSQL and on MariaDB. The grammar
// and the value rules are FileMaker's, so a formula migrated from a FileMaker
// solution means what it meant there. They are described in
// docs/specs/calculation_formulas.md.
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
	tokIdent    // a function name, a keyword, or a field written as a plain word
	tokField    // a field the lexer matched against the declared names
	tokVariable // $local or $$global; text keeps the dollar signs
	tokOperator
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokComma
)

type token struct {
	kind  tokenKind
	text  string
	table string // the table occurrence written before "::", if any
	pos   int
	end   int // where the token's source text ends
	// missing marks FileMaker's placeholder for a deleted table or field.
	missing bool
}

func (t token) isOperator(op string) bool {
	return t.kind == tokOperator && t.text == op
}

// isKeyword reports whether the token is the given word, ignoring case. The
// word operators (AND, OR, NOT, XOR) and the function names are matched this way.
func (t token) isKeyword(word string) bool {
	return t.kind == tokIdent && t.table == "" && strings.EqualFold(t.text, word)
}

// Operators are matched longest first so that ">=" does not come out as ">"
// followed by "=". FileMaker's own symbols read as their ASCII spelling.
var multiCharOperators = []string{"<>", "!=", "<=", ">=", "=="}

var symbolOperators = map[rune]string{'≠': "<>", '≤': "<=", '≥': ">="}

// pilcrow is FileMaker's ¶: a carriage return, which is what FileMaker stores
// between the values of a list.
const pilcrow = "\r"

// names is what the lexer knows about the fields a formula may read, so that
// it recognises a field whose name holds spaces or symbols, as FileMaker does.
// Every list is sorted longest first.
type names struct {
	local  []string            // fields of the formula's own table
	tables []string            // table occurrences
	fields map[string][]string // fields of each occurrence, keyed by its lowercase name
	funcs  []string            // custom functions, whose names may start with "#"
	// decimal is the file's decimal separator: in a French file numbers are
	// written 1,5 and "," separates nothing else.
	decimal rune
	// strict refuses "." as a decimal point where the locale writes ",", as
	// FileMaker's Evaluate does. A stored formula accepts both, because
	// FileMaker's own exports write either.
	strict bool
}

// point is the other character read as a decimal point besides the locale's.
func (n *names) point() rune {
	if n != nil && n.strict {
		return n.decimalSeparator()
	}
	return '.'
}

func (n *names) decimalSeparator() rune {
	if n == nil || n.decimal == 0 {
		return '.'
	}
	return n.decimal
}

func lex(src string, known *names) ([]token, error) {
	runes := []rune(src)
	var tokens []token
	i := 0

	for i < len(runes) {
		ch := runes[i]
		if n := len(tokens); n > 0 && tokens[n-1].end == 0 {
			tokens[n-1].end = i
		}

		switch {
		case unicode.IsSpace(ch):
			i++

		case ch == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i < len(runes) && runes[i] != '\n' && runes[i] != '\r' {
				i++
			}

		case ch == '/' && i+1 < len(runes) && runes[i+1] == '*':
			end := indexRunes(runes, i+2, []rune("*/"))
			if end < 0 {
				return nil, errorAt(i, "unterminated comment")
			}
			i = end + 2

		case ch == '(':
			tokens = append(tokens, token{kind: tokLParen, text: "(", pos: i})
			i++

		case ch == ')':
			tokens = append(tokens, token{kind: tokRParen, text: ")", pos: i})
			i++

		case ch == '[':
			tokens = append(tokens, token{kind: tokLBracket, text: "[", pos: i})
			i++

		case ch == ']':
			tokens = append(tokens, token{kind: tokRBracket, text: "]", pos: i})
			i++

		case isNumberStart(runes, i, known.decimalSeparator(), known.point()):
			start := i
			i = scanNumber(runes, i, known.decimalSeparator(), known.point())
			text := string(runes[start:i])
			if d := known.decimalSeparator(); d != '.' && !strings.Contains(text, ".") {
				text = strings.Replace(text, string(d), ".", 1)
			}
			if strings.HasSuffix(text, ".") {
				return nil, errorAt(start, "a number cannot end with its decimal separator")
			}
			tokens = append(tokens, token{kind: tokNumber, text: text, pos: start})

		// FileMaker separates arguments with ";"; "," is accepted too where it
		// is not the decimal separator.
		case ch == ',' || ch == ';':
			tokens = append(tokens, token{kind: tokComma, text: string(ch), pos: i})
			i++

		case ch == '¶':
			tokens = append(tokens, token{kind: tokString, text: pilcrow, pos: i})
			i++

		case ch == '"' || ch == '\'':
			text, next, err := lexString(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: tokString, text: text, pos: i})
			i = next

		case ch == '$' && i+1 < len(runes) && runes[i+1] == '{':
			// ${Name + With Symbols}: FileMaker's quoting for a field or table
			// name that would otherwise read as an expression.
			t, next, err := lexQuotedName(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, t)
			i = next

		case ch == '$':
			start := i
			i++
			if i < len(runes) && runes[i] == '$' {
				i++
			}
			nameStart := i
			for i < len(runes) && (isNameRune(runes[i]) || runes[i] == '#' || runes[i] == '.') {
				i++
			}
			if i == nameStart {
				return nil, errorAt(start, "a variable needs a name after $")
			}
			tokens = append(tokens, token{kind: tokVariable, text: string(runes[start:i]), pos: start})

		default:
			if t, next, ok := lexCustomFunctionName(runes, i, known); ok {
				tokens = append(tokens, t)
				i = next
				continue
			}
			if t, next, ok := lexKnownField(runes, i, known); ok {
				tokens = append(tokens, t)
				i = next
				continue
			}
			if isNameStart(ch) {
				t, next := lexWord(runes, i)
				tokens = append(tokens, t)
				i = next
				continue
			}
			if t, next, ok := lexMissingReference(runes, i); ok {
				tokens = append(tokens, t)
				i = next
				continue
			}
			if op, width := matchOperator(runes, i); op != "" {
				tokens = append(tokens, token{kind: tokOperator, text: op, pos: i})
				i += width
				continue
			}
			return nil, errorAt(i, "unexpected character %q", string(ch))
		}
	}

	if n := len(tokens); n > 0 && tokens[n-1].end == 0 {
		tokens[n-1].end = len(runes)
	}
	tokens = append(tokens, token{kind: tokEOF, pos: len(runes), end: len(runes)})
	return tokens, nil
}

func isNameStart(r rune) bool { return r == '_' || r == '~' || unicode.IsLetter(r) }

func isNameRune(r rune) bool {
	return r == '_' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// lexQuotedName reads ${Name} or ${Table}::${Field} (either part may be
// quoted). The result is a field reference whatever the declared names are.
func lexQuotedName(runes []rune, i int) (token, int, error) {
	start := i
	first, next, err := quotedNamePart(runes, i)
	if err != nil {
		return token{}, 0, err
	}
	if next+1 < len(runes) && runes[next] == ':' && runes[next+1] == ':' {
		j := next + 2
		var field string
		if j+1 < len(runes) && runes[j] == '$' && runes[j+1] == '{' {
			field, j, err = quotedNamePart(runes, j)
			if err != nil {
				return token{}, 0, err
			}
		} else {
			k := j
			for k < len(runes) && isNameRune(runes[k]) {
				k++
			}
			if k == j {
				return token{}, 0, errorAt(j, "a field name must follow \"::\"")
			}
			field, j = string(runes[j:k]), k
		}
		return token{kind: tokField, table: first, text: field, pos: start}, j, nil
	}
	return token{kind: tokField, text: first, pos: start}, next, nil
}

func quotedNamePart(runes []rune, i int) (string, int, error) {
	end := indexRunes(runes, i+2, []rune("}"))
	if end < 0 {
		return "", 0, errorAt(i, "${ is not closed by }")
	}
	name := string(runes[i+2 : end])
	if strings.TrimSpace(name) == "" {
		return "", 0, errorAt(i, "${} names nothing")
	}
	return name, end + 1, nil
}

// lexMissingReference reads what FileMaker writes for a reference to a table
// or field that was deleted: "<Table Missing>::<Field Missing>" (in French
// "<Table manquante>::<Rubrique manquante>"), or "Articles::<Field Missing>".
func lexMissingReference(runes []rune, i int) (token, int, bool) {
	part := func(j int) (string, int, bool) {
		if j >= len(runes) || runes[j] != '<' {
			return "", 0, false
		}
		end := indexRunes(runes, j+1, []rune(">"))
		if end < 0 {
			return "", 0, false
		}
		inner := string(runes[j+1 : end])
		if inner == "" || strings.ContainsAny(inner, "<=\r\n\"") {
			return "", 0, false
		}
		return string(runes[j : end+1]), end + 1, true
	}
	table, next, ok := part(i)
	if !ok || next+1 >= len(runes) || runes[next] != ':' || runes[next+1] != ':' {
		return token{}, 0, false
	}
	field, end, ok := part(next + 2)
	if !ok {
		return token{}, 0, false
	}
	return token{kind: tokField, table: table, text: field, missing: true, pos: i}, end, true
}

// lexWord reads a plain name, and "Table::Field" when "::" follows. A plain
// name may hold dots, as FileMaker field names often do ("Prix.HT").
func lexWord(runes []rune, i int) (token, int) {
	start := i
	i++ // the first character was checked by isNameStart; it may be "~"
	for i < len(runes) && isNameRune(runes[i]) {
		i++
	}
	word := string(runes[start:i])
	if i+2 < len(runes) && runes[i] == ':' && runes[i+1] == ':' && runes[i+2] == '<' {
		if end := indexRunes(runes, i+3, []rune(">")); end > 0 {
			return token{kind: tokField, table: word, text: string(runes[i+2 : end+1]), missing: true, pos: start}, end + 1
		}
	}
	if i+2 < len(runes) && runes[i] == ':' && runes[i+1] == ':' && isNameStart(runes[i+2]) {
		j := i + 2
		for j < len(runes) && isNameRune(runes[j]) {
			j++
		}
		return token{kind: tokIdent, table: word, text: string(runes[i+2 : j]), pos: start}, j
	}
	return token{kind: tokIdent, text: word, pos: start}, i
}

// lexKnownField recognises, longest name first, "Occurrence::Field" or a field
// of the formula's own table among the names the caller declared. FileMaker
// names may hold spaces and symbols ("Date de début", "Prix HT €"), which a
// plain word cannot.
func lexKnownField(runes []rune, i int, known *names) (token, int, bool) {
	if known == nil {
		return token{}, 0, false
	}
	for _, table := range known.tables {
		end, ok := matchName(runes, i, table)
		if !ok || end+1 >= len(runes) || runes[end] != ':' || runes[end+1] != ':' {
			continue
		}
		for _, field := range known.fields[strings.ToLower(table)] {
			if fieldEnd, ok := matchName(runes, end+2, field); ok && endsName(runes, fieldEnd) {
				return token{kind: tokField, table: table, text: field, pos: i}, fieldEnd, true
			}
		}
	}
	for _, field := range known.local {
		end, ok := matchName(runes, i, field)
		if !ok || !endsName(runes, end) {
			continue
		}
		if end+1 < len(runes) && runes[end] == ':' && runes[end+1] == ':' {
			continue // an occurrence that happens to be named like a field
		}
		if _, builtin := functions[strings.ToUpper(field)]; builtin && callFollows(runes, end) {
			continue // Date ( 1 ; 1 ; 1901 ) calls Date, even beside a field "Date"
		}
		return token{kind: tokField, text: field, pos: i}, end, true
	}
	return token{}, 0, false
}

// lexCustomFunctionName recognises a declared custom function whose name a
// plain word cannot hold, such as the widespread "#" and "#Get" functions.
// It is only taken when a "(" follows, as a call.
func lexCustomFunctionName(runes []rune, i int, known *names) (token, int, bool) {
	if known == nil || isNameStart(runes[i]) {
		return token{}, 0, false
	}
	for _, fn := range known.funcs {
		end, ok := matchName(runes, i, fn)
		if !ok || !endsName(runes, end) {
			continue
		}
		if callFollows(runes, end) {
			return token{kind: tokIdent, text: fn, pos: i}, end, true
		}
	}
	return token{}, 0, false
}

// callFollows reports whether "(" is the next thing after i, past spaces.
func callFollows(runes []rune, i int) bool {
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	return i < len(runes) && runes[i] == '('
}

// matchName reports whether name is written at i, ignoring case as FileMaker
// does, and where it ends.
func matchName(runes []rune, i int, name string) (int, bool) {
	nr := []rune(name)
	if len(nr) == 0 || i+len(nr) > len(runes) {
		return 0, false
	}
	for k, r := range nr {
		if unicode.ToLower(runes[i+k]) != unicode.ToLower(r) {
			return 0, false
		}
	}
	return i + len(nr), true
}

// endsName reports whether a name may end at i, so that "Prix" is not read
// out of "PrixHT".
func endsName(runes []rune, i int) bool {
	return i >= len(runes) || !isNameRune(runes[i])
}

// isNumberStart reports whether a number begins at i: a digit, or the
// decimal separator followed by a digit (",5" in a French file).
// A "." is read as a decimal point whatever the locale: it means nothing else
// outside a name, and a formula written on an English system and opened in a
// French one keeps it (".2").
func isNumberStart(runes []rune, i int, dec, point rune) bool {
	ch := runes[i]
	if ch >= '0' && ch <= '9' {
		return true
	}
	return (ch == dec || ch == point) && i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9'
}

func scanNumber(runes []rune, i int, dec, point rune) int {
	seenDot := false
	for i < len(runes) && (runes[i] >= '0' && runes[i] <= '9' || ((runes[i] == dec || runes[i] == point) && !seenDot)) {
		if runes[i] == dec || runes[i] == point {
			seenDot = true
		}
		i++
	}
	// Scientific notation: 1E3, 2.5e-4.
	if i < len(runes) && (runes[i] == 'e' || runes[i] == 'E') {
		j := i + 1
		if j < len(runes) && (runes[j] == '+' || runes[j] == '-') {
			j++
		}
		if j < len(runes) && unicode.IsDigit(runes[j]) {
			for j < len(runes) && unicode.IsDigit(runes[j]) {
				j++
			}
			return j
		}
	}
	return i
}

func matchOperator(runes []rune, i int) (string, int) {
	for _, op := range multiCharOperators {
		opRunes := []rune(op)
		if i+len(opRunes) <= len(runes) && string(runes[i:i+len(opRunes)]) == op {
			return op, len(opRunes)
		}
	}
	if op, ok := symbolOperators[runes[i]]; ok {
		return op, 1
	}
	if strings.ContainsRune("+-*/&=<>^", runes[i]) {
		return string(runes[i]), 1
	}
	return "", 0
}

func indexRunes(runes []rune, from int, sub []rune) int {
	for i := from; i+len(sub) <= len(runes); i++ {
		if string(runes[i:i+len(sub)]) == string(sub) {
			return i
		}
	}
	return -1
}

// lexString reads a quoted text. Inside double quotes FileMaker's escapes
// apply: \" is a quote, \\ a backslash, \¶ a pilcrow, and ¶ a return. A doubled
// quote also means itself, and single quotes are accepted for formulas written
// in File4Base.
func lexString(runes []rune, start int) (string, int, error) {
	quote := runes[start]
	var b strings.Builder
	i := start + 1
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == quote:
			if i+1 < len(runes) && runes[i+1] == quote {
				b.WriteRune(quote)
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		case r == '\\' && quote == '"' && i+1 < len(runes) && strings.ContainsRune("\"\\¶", runes[i+1]):
			b.WriteRune(runes[i+1])
			i += 2
		case r == '¶' && quote == '"':
			b.WriteString(pilcrow)
			i++
		default:
			b.WriteRune(r)
			i++
		}
	}
	return "", 0, errorAt(start, "unterminated text literal")
}
