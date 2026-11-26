package fmdapi

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// unknownParameters is the front end's refusal of the keys that are not
// parameters of the endpoint, listed in the order Object.keys gives them.
// It is nil when every key is known.
func unknownParameters(keys []string, known func(string) bool) *Error {
	var unknown []string
	for _, k := range keys {
		if !known(k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return parameterError("Unknown parameter(s): " + strings.Join(unknown, ","))
}

// positiveInteger reads a numeric parameter (_limit, offset, ...), given
// as a number or a string, as FileMaker Server's front end does with
// JavaScript's parseInt: leading spaces and a sign are allowed, the digits
// stop at the first other character ("1.5" is 1), and a value too large
// for an integer saturates. It must come out above 0.
func positiveInteger(name string, v Value) (int64, *Error) {
	n, ok := readInteger(v)
	if !ok || !(n > 0) {
		return 0, parameterError("The " + name + " must be an integer with value greater than 0.")
	}
	if n >= math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(n), nil
}

// dateFormats reads the dateformats parameter: 0 (US), 1 (file locale) or
// 2 (ISO 8601).
func dateFormats(v Value) (int, *Error) {
	n, ok := readInteger(v)
	if !ok || (n != 0 && n != 1 && n != 2) {
		return 0, parameterError("The dateformats must be an integer with value 0, 1 or 2.")
	}
	return int(n), nil
}

// readInteger is parseInt(String(v)) for a number or a string.
func readInteger(v Value) (float64, bool) {
	s, ok := v.jsString()
	if !ok {
		return 0, false
	}
	return parseIntJS(s)
}

// parseIntJS is JavaScript's parseInt(s) without a radix: the result, or
// false where parseInt gives NaN.
func parseIntJS(s string) (float64, bool) {
	s = strings.TrimLeftFunc(s, isJSWhitespace)
	sign := 1.0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	base := 10
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		base, s = 16, s[2:]
	}
	end := 0
	for end < len(s) && digitIn(s[end], base) {
		end++
	}
	if end == 0 {
		return 0, false
	}
	digits := s[:end]
	if base == 16 {
		// Correctly rounded, as V8 reads a power-of-two radix.
		f, _ := strconv.ParseFloat("0x"+digits+"p0", 64)
		return sign * f, true
	}
	f, _ := strconv.ParseFloat(digits, 64) // correctly rounded, +Inf when huge
	return sign * f, true
}

func digitIn(c byte, base int) bool {
	if base == 16 {
		return isHex(c)
	}
	return c >= '0' && c <= '9'
}

// isJSWhitespace is JavaScript's WhiteSpace and LineTerminator.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A || (r != 0x180E && unicode.Is(unicode.Zs, r))
}
