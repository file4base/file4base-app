package fmdapi

import (
	"strconv"
	"unicode/utf16"
)

// parseJSON parses UTF-16 text as V8's JSON.parse does, the parser behind
// FileMaker Server's front end, and on a syntax error returns the message
// V8 gives: same wording, same position, line and column, same excerpt of
// the text. The message is UTF-16 because the excerpt may cut a surrogate
// pair in half.
//
// It mirrors V8's JsonParser (src/json/json-parser.cc) step by step,
// including its iterative descent, so that deep nesting cannot exhaust the
// stack.
func parseJSON(src []uint16) (Value, []uint16) {
	p := &jsonParser{src: src}
	v, ok := p.parse()
	if !ok {
		return Value{}, p.err
	}
	return v, nil
}

type jsonToken uint8

const (
	tokIllegal jsonToken = iota
	tokEOS
	tokWhitespace
	tokString
	tokNumber
	tokTrue
	tokFalse
	tokNull
	tokColon
	tokComma
	tokLBrack
	tokRBrack
	tokLBrace
	tokRBrace
)

// endOfText stands for the character past the end of the text.
const endOfText = -1

// tokenOf classifies a character as V8's one_char_json_tokens table does.
func tokenOf(c int) jsonToken {
	switch {
	case c == endOfText:
		return tokEOS
	case c == '"':
		return tokString
	case c == '-' || (c >= '0' && c <= '9'):
		return tokNumber
	case c == 't':
		return tokTrue
	case c == 'f':
		return tokFalse
	case c == 'n':
		return tokNull
	case c == ' ' || c == '\t' || c == '\r' || c == '\n':
		return tokWhitespace
	case c == ':':
		return tokColon
	case c == ',':
		return tokComma
	case c == '[':
		return tokLBrack
	case c == ']':
		return tokRBrack
	case c == '{':
		return tokLBrace
	case c == '}':
		return tokRBrace
	}
	return tokIllegal
}

func isDigit(c int) bool { return c >= '0' && c <= '9' }

// V8's message templates (src/common/message-template.h).
const (
	msgExpectedPropNameOrRBrace = "Expected property name or '}'"
	msgExpectedDoubleQuotedName = "Expected double-quoted property name"
	msgExpectedColon            = "Expected ':' after property name"
	msgExpectedCommaOrRBrack    = "Expected ',' or ']' after array element"
	msgExpectedCommaOrRBrace    = "Expected ',' or '}' after property value"
	msgUnterminatedString       = "Unterminated string"
	msgBadControlCharacter      = "Bad control character in string literal"
	msgBadUnicodeEscape         = "Bad Unicode escape"
	msgBadEscapedCharacter      = "Bad escaped character"
	msgNoNumberAfterMinusSign   = "No number after minus sign"
	msgExponentMissingNumber    = "Exponent part is missing a number"
	msgUnterminatedFraction     = "Unterminated fractional number"
	msgNonWhitespaceAfterJSON   = "Unexpected non-whitespace character after JSON"
	msgUnexpectedNumber         = "Unexpected number"
	msgUnexpectedString         = "Unexpected string"
	msgUnexpectedEndOfJSONInput = "Unexpected end of JSON input"
	jsonContextCharacters       = 10
	jsonMinContextSourceLength  = 2*jsonContextCharacters + 1
)

// maxJSONValues bounds the values and nesting levels of one body, so that
// a body within the size limit cannot make the server hold far more memory
// before the caller is authenticated. V8 has no such bound; File4Base
// answers a body over it as one over the size limit.
const maxJSONValues = 1 << 18

type jsonParser struct {
	src []uint16
	pos int
	err []uint16
	// values counts the values and frames made so far.
	values int
}

// count records one more value or frame, failing past maxJSONValues.
func (p *jsonParser) count() bool {
	if p.values++; p.values > maxJSONValues {
		p.err = utf16.Encode([]rune(errBodyTooLarge.Error()))
		return false
	}
	return true
}

func (p *jsonParser) char() int {
	if p.pos >= len(p.src) {
		return endOfText
	}
	return int(p.src[p.pos])
}

// peek is the token of the current character; characters beyond Latin-1
// are never tokens.
func (p *jsonParser) peek() jsonToken {
	c := p.char()
	if c > 0xFF {
		return tokIllegal
	}
	return tokenOf(c)
}

func (p *jsonParser) skipWhitespace() {
	for p.pos < len(p.src) && tokenOf(int(p.src[p.pos])) == tokWhitespace {
		p.pos++
	}
}

// check consumes the next token if it is tok.
func (p *jsonParser) check(tok jsonToken) bool {
	p.skipWhitespace()
	if p.peek() == tok {
		p.pos++
		return true
	}
	return false
}

// expectNext consumes the next token, which must be tok.
func (p *jsonParser) expectNext(tok jsonToken, msg string) bool {
	p.skipWhitespace()
	return p.expect(tok, msg)
}

func (p *jsonParser) expect(tok jsonToken, msg string) bool {
	if p.peek() != tok {
		return p.fail(p.peek(), msg)
	}
	p.pos++
	return true
}

// failChar reports the character c at the current position as unexpected.
func (p *jsonParser) failChar(c int) bool {
	if c > 0xFF {
		return p.fail(tokIllegal, "")
	}
	return p.fail(tokenOf(c), "")
}

// fail records the error V8's ReportUnexpectedToken raises for token at the
// current position: msg when given, else the message the token calls for.
// It always returns false.
func (p *jsonParser) fail(token jsonToken, msg string) bool {
	switch {
	case msg != "":
		p.err = p.positioned(msg)
	case token == tokEOS:
		p.err = utf16.Encode([]rune(msgUnexpectedEndOfJSONInput))
	case token == tokNumber:
		p.err = p.positioned(msgUnexpectedNumber)
	case token == tokString:
		p.err = p.positioned(msgUnexpectedString)
	case p.isSpecialSource():
		p.err = concat16(`"`, p.src, `" is not valid JSON`)
	default:
		ch := p.src[p.pos : p.pos+1]
		n := len(p.src)
		head := concat16("Unexpected token '", ch, "', ")
		switch {
		case n < jsonMinContextSourceLength:
			p.err = append(head, concat16(`"`, p.src, `" is not valid JSON`)...)
		case p.pos < jsonContextCharacters:
			p.err = append(head, concat16(`"`, p.src[:p.pos+jsonContextCharacters], `"... is not valid JSON`)...)
		case p.pos < n-jsonContextCharacters:
			p.err = append(head, concat16(`..."`, p.src[p.pos-jsonContextCharacters:p.pos+jsonContextCharacters], `"... is not valid JSON`)...)
		default:
			p.err = append(head, concat16(`..."`, p.src[p.pos-jsonContextCharacters:], `" is not valid JSON`)...)
		}
	}
	return false
}

// isSpecialSource reports the texts V8 names in full rather than quoting
// a token of: what JSON.parse receives for undefined, NaN, Infinity and {}.
func (p *jsonParser) isSpecialSource() bool {
	switch string(utf16.Decode(p.src)) {
	case "[object Object]", "undefined", "Infinity", "NaN":
		return true
	}
	return false
}

// positioned appends the position, line and column of the current
// character to msg. JSON line breaks are \n, \r and \r\n.
func (p *jsonParser) positioned(msg string) []uint16 {
	line, lastBreak := 1, 0
	for i := 0; i < p.pos; i++ {
		if p.src[i] == '\r' && i < p.pos-1 && p.src[i+1] == '\n' {
			i++
		}
		if p.src[i] == '\r' || p.src[i] == '\n' {
			line++
			lastBreak = i + 1
		}
	}
	where := " in JSON at position "
	if msg == msgNonWhitespaceAfterJSON {
		// V8's message already ends "after JSON".
		where = " at position "
	}
	return utf16.Encode([]rune(msg + where + strconv.Itoa(p.pos) +
		" (line " + strconv.Itoa(line) + " column " + strconv.Itoa(1+p.pos-lastBreak) + ")"))
}

func concat16(before string, mid []uint16, after string) []uint16 {
	out := utf16.Encode([]rune(before))
	out = append(out, mid...)
	return append(out, utf16.Encode([]rune(after))...)
}

// jsonFrame is an array or object whose members are being parsed.
type jsonFrame struct {
	object bool
	key    string
	obj    objectBuilder
	elems  []Value
}

func (p *jsonParser) parse() (Value, bool) {
	var stack []*jsonFrame
	var value Value
	for {
		// Produce a value.
		produced := false
		for !produced {
			if !p.count() {
				return Value{}, false
			}
			p.skipWhitespace()
			switch tok := p.peek(); tok {
			case tokString:
				p.pos++
				s, ok := p.scanString()
				if !ok {
					return Value{}, false
				}
				value, produced = Value{Kind: KindString, Text: s}, true
			case tokNumber:
				v, ok := p.scanNumber()
				if !ok {
					return Value{}, false
				}
				value, produced = v, true
			case tokLBrace:
				p.pos++
				if p.check(tokRBrace) {
					value, produced = Value{Kind: KindObject, Members: []Member{}}, true
					break
				}
				f := &jsonFrame{object: true}
				stack = append(stack, f)
				if !p.expectNext(tokString, msgExpectedPropNameOrRBrace) {
					return Value{}, false
				}
				key, ok := p.scanString()
				if !ok || !p.expectNext(tokColon, msgExpectedColon) {
					return Value{}, false
				}
				f.key = key
			case tokLBrack:
				p.pos++
				if p.check(tokRBrack) {
					value, produced = Value{Kind: KindArray, Elems: []Value{}}, true
					break
				}
				stack = append(stack, &jsonFrame{})
			case tokTrue, tokFalse, tokNull:
				lit := map[jsonToken]string{tokTrue: "true", tokFalse: "false", tokNull: "null"}[tok]
				if !p.scanLiteral(lit) {
					return Value{}, false
				}
				value, produced = Value{Kind: KindBool, Bool: tok == tokTrue}, true
				if tok == tokNull {
					value = Value{Kind: KindNull}
				}
			default:
				return Value{}, p.failChar(p.char())
			}
		}

		// Consume it into the array or object it belongs to.
		for next := false; !next; {
			if len(stack) == 0 {
				if !p.check(tokEOS) {
					return Value{}, p.fail(p.peek(), msgNonWhitespaceAfterJSON)
				}
				return value, true
			}
			f := stack[len(stack)-1]
			if f.object {
				f.obj.set(f.key, value)
				if p.check(tokComma) {
					if !p.expectNext(tokString, msgExpectedDoubleQuotedName) {
						return Value{}, false
					}
					key, ok := p.scanString()
					if !ok || !p.expectNext(tokColon, msgExpectedColon) {
						return Value{}, false
					}
					f.key, next = key, true
					continue
				}
				value = Value{Kind: KindObject, Members: f.obj.build()}
				if !p.expect(tokRBrace, msgExpectedCommaOrRBrace) {
					return Value{}, false
				}
			} else {
				f.elems = append(f.elems, value)
				if p.check(tokComma) {
					next = true
					continue
				}
				elems := f.elems
				if elems == nil {
					elems = []Value{}
				}
				value = Value{Kind: KindArray, Elems: elems}
				if !p.expect(tokRBrack, msgExpectedCommaOrRBrack) {
					return Value{}, false
				}
			}
			stack = stack[:len(stack)-1]
		}
	}
}

// scanString scans a string whose opening quote is consumed.
func (p *jsonParser) scanString() (string, bool) {
	var out []uint16
	for {
		start := p.pos
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			if c == '"' || c == '\\' || c < 0x20 {
				break
			}
			p.pos++
		}
		out = append(out, p.src[start:p.pos]...)
		if p.pos == len(p.src) {
			return "", p.fail(tokIllegal, msgUnterminatedString)
		}
		switch c := p.src[p.pos]; {
		case c == '"':
			p.pos++
			return wtf8(out), true
		case c == '\\':
			p.pos++
			e := p.char()
			if e < 0 || e > 0xFF {
				return "", p.failChar(e)
			}
			switch e {
			case '"', '\\', '/':
				out = append(out, uint16(e))
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				var r uint16
				for i := 0; i < 4; i++ {
					p.pos++
					d := hexValue(p.char())
					if d < 0 {
						return "", p.fail(tokIllegal, msgBadUnicodeEscape)
					}
					r = r<<4 | uint16(d)
				}
				out = append(out, r)
			default:
				return "", p.fail(tokIllegal, msgBadEscapedCharacter)
			}
			p.pos++
		default:
			return "", p.fail(tokIllegal, msgBadControlCharacter)
		}
	}
}

func hexValue(c int) int {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return -1
}

func (p *jsonParser) scanNumber() (Value, bool) {
	start := p.pos
	if p.char() == '-' {
		p.pos++
	}
	if p.char() == '0' {
		p.pos++
		if isDigit(p.char()) {
			// A leading zero is only allowed alone.
			return Value{}, p.fail(tokNumber, "")
		}
	} else {
		digits := p.pos
		p.skipDigits()
		if p.pos == digits {
			return Value{}, p.fail(tokIllegal, msgNoNumberAfterMinusSign)
		}
	}
	if p.char() == '.' {
		p.pos++
		if !isDigit(p.char()) {
			return Value{}, p.fail(tokIllegal, msgUnterminatedFraction)
		}
		p.skipDigits()
	}
	if c := p.char(); c == 'e' || c == 'E' {
		p.pos++
		if c := p.char(); c == '-' || c == '+' {
			p.pos++
		}
		if !isDigit(p.char()) {
			return Value{}, p.fail(tokIllegal, msgExponentMissingNumber)
		}
		p.skipDigits()
	}
	text := string(utf16.Decode(p.src[start:p.pos]))
	// The text is a valid JSON number; out of range it is ±Infinity, as in
	// JavaScript.
	f, _ := strconv.ParseFloat(text, 64)
	return Value{Kind: KindNumber, Num: f, Text: text}, true
}

func (p *jsonParser) skipDigits() {
	for isDigit(p.char()) {
		p.pos++
	}
}

// scanLiteral scans true, false or null, whose first character matched.
func (p *jsonParser) scanLiteral(lit string) bool {
	remaining := len(p.src) - p.pos
	if remaining >= len(lit) {
		match := true
		for i := 1; i < len(lit); i++ {
			if p.src[p.pos+i] != uint16(lit[i]) {
				match = false
				break
			}
		}
		if match {
			p.pos += len(lit)
			return true
		}
	}
	p.pos++
	for i := 1; i < len(lit) && i < remaining; i++ {
		if p.src[p.pos] != uint16(lit[i]) {
			return p.failChar(int(p.src[p.pos]))
		}
		p.pos++
	}
	return p.fail(tokEOS, "")
}
