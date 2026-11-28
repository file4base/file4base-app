package fmdapi

import (
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

// Object is a JSON object whose members keep the order they are listed in.
// FileMaker Server's replies have a fixed key order, which Go maps lose.
type Object []Field

// Field is one member of an Object.
type Field struct {
	Key   string
	Value any
}

// Marshal encodes v as FileMaker Server's front end does with
// JSON.stringify. v is built from nil, bool, string, int, int64, float64,
// Object, []Object and []any; anything else is a programming error.
func Marshal(v any) []byte {
	return appendJSON(nil, v)
}

func appendJSON(buf []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(buf, "null"...)
	case bool:
		return strconv.AppendBool(buf, x)
	case string:
		return appendJSString(buf, x)
	case int:
		return strconv.AppendInt(buf, int64(x), 10)
	case int64:
		return strconv.AppendInt(buf, x, 10)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return append(buf, "null"...)
		}
		return append(buf, formatJSNumber(x)...)
	case Object:
		buf = append(buf, '{')
		for i, f := range x {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSString(buf, f.Key)
			buf = append(buf, ':')
			buf = appendJSON(buf, f.Value)
		}
		return append(buf, '}')
	case []Object:
		buf = append(buf, '[')
		for i, o := range x {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSON(buf, o)
		}
		return append(buf, ']')
	case []any:
		buf = append(buf, '[')
		for i, e := range x {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSON(buf, e)
		}
		return append(buf, ']')
	}
	panic(fmt.Sprintf("fmdapi: cannot encode a %T", v))
}

// appendJSString quotes s as JSON.stringify does: only the quote, the
// backslash and control characters are escaped, so <, >, &, U+2028 and
// U+2029 stay as they are (Go's encoder escapes them). A lone surrogate,
// which s carries in WTF-8 form, is escaped as \uXXXX; any other invalid
// UTF-8 becomes U+FFFD.
func appendJSString(buf []byte, s string) []byte {
	const hex = "0123456789abcdef"
	buf = append(buf, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch c {
			case '"', '\\':
				buf = append(buf, '\\', c)
			case '\b':
				buf = append(buf, '\\', 'b')
			case '\f':
				buf = append(buf, '\\', 'f')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				if c < 0x20 {
					buf = append(buf, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
				} else {
					buf = append(buf, c)
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if u, ok := wtf8Surrogate(s[i:]); ok {
				buf = append(buf, '\\', 'u', hex[u>>12], hex[u>>8&0xF], hex[u>>4&0xF], hex[u&0xF])
				i += 3
				continue
			}
			buf = append(buf, "�"...)
			i++
			continue
		}
		buf = append(buf, s[i:i+size]...)
		i += size
	}
	return append(buf, '"')
}

// wtf8Surrogate decodes a surrogate code unit encoded as UTF-8 would
// encode it (ED A0..BF 80..BF), which UTF-8 itself forbids.
func wtf8Surrogate(s string) (uint16, bool) {
	if len(s) < 3 || s[0] != 0xED || s[1] < 0xA0 || s[1] > 0xBF || s[2] < 0x80 || s[2] > 0xBF {
		return 0, false
	}
	return 0xD000 | uint16(s[1]&0x3F)<<6 | uint16(s[2]&0x3F), true
}

// wtf8 converts UTF-16 to a Go string, keeping a lone surrogate in WTF-8
// form so that appendJSString writes it back as JavaScript would.
func wtf8(units []uint16) string {
	buf := make([]byte, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		switch {
		case u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF:
			buf = utf8.AppendRune(buf, 0x10000+(u-0xD800)<<10+rune(units[i+1])-0xDC00)
			i++
		case u >= 0xD800 && u <= 0xDFFF:
			buf = append(buf, 0xED, byte(0x80|(u>>6)&0x3F), byte(0x80|u&0x3F))
		default:
			buf = utf8.AppendRune(buf, u)
		}
	}
	return string(buf)
}

// formatJSNumber spells a finite number as JavaScript's Number toString
// does: the shortest digits that round-trip, in plain notation from 1e-7
// (exclusive) up to 1e21 (exclusive), in exponent notation outside it.
// Negative zero is "0".
func formatJSNumber(f float64) string {
	switch {
	case f == 0:
		return "0"
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if abs := math.Abs(f); abs < 1e-6 || abs >= 1e21 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		// Go writes e-07 where JavaScript writes e-7.
		n := len(s)
		if n >= 4 && s[n-4] == 'e' && s[n-3] == '-' && s[n-2] == '0' {
			s = s[:n-2] + s[n-1:]
		}
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
