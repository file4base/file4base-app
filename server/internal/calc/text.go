package calc

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"
)

// Text rules observed in FileMaker Pro 26 (testdata/conformance.json).

// collate orders two texts as FileMaker's = and < do: ignoring case and
// accents, so "É" = "e" and "Ecole" is not before "École". Ligatures and
// "ß" are letters of their own: "Œ" is not "oe".
func collate(a, b string) int {
	return strings.Compare(primaryKey(a), primaryKey(b))
}

// primaryKey is the text without accents, in lower case.
func primaryKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if k, ok := letterKeys[r]; ok {
			b.WriteString(k)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// letterKeys places the Latin letters that Unicode does not decompose. A
// letter with a stroke is its base letter with an accent. "æ", "œ" and "ß"
// are letters of their own, sorted after the letter they start with: "æ" is
// before "b" but is not "ae".
var letterKeys = map[rune]string{
	'ø': "o", 'đ': "d", 'ł': "l", 'ħ': "h", 'ŧ': "t", 'ı': "i",
	'æ': "a\uffff", 'œ': "o\uffff", 'ß': "s\uffff",
}

// sortLess orders values for SortValues: as collate does, then unaccented
// before accented, then lower case before upper case, so that equal-looking
// values still come out in a stable, predictable order.
func sortLess(a, b string) bool {
	if c := collate(a, b); c != 0 {
		return c < 0
	}
	if x, y := norm.NFD.String(lowerText(a)), norm.NFD.String(lowerText(b)); x != y {
		return x < y
	}
	return a > b
}

// upperText and lowerText map each character on its own, as FileMaker does:
// "ß" stays "ß" and "İ" becomes "i".
func upperText(s string) string { return strings.Map(unicode.ToUpper, s) }
func lowerText(s string) string { return strings.Map(unicode.ToLower, s) }

// proper capitalises the first letter of every word and lowers the rest. A
// word starts after anything but a letter, a digit or a full stop, so
// "jean-pierre o'neil" is "Jean-Pierre O'Neil" and "a.b" is "A.b".
func proper(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			if start {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(unicode.ToLower(r))
			}
			start = false
		default:
			b.WriteRune(r)
			start = !unicode.IsDigit(r) && r != '.' && !unicode.Is(unicode.Mn, r)
		}
	}
	return b.String()
}

// words finds the words of a text as FileMaker's word functions do. Letters
// and digits make words; a full stop joins two letters ("a.b"), and the
// locale's decimal and thousands separators and ":" join two digits ("1,5",
// "12:30"). Everything else, apostrophes and hyphens included, separates.
func words(s string, loc *Locale) [][2]int {
	r := []rune(s)
	dec := loc.decimal()
	thousands := []rune(loc.orISO().ThousandsSeparator)
	isWord := func(ch rune) bool { return unicode.IsLetter(ch) || unicode.IsDigit(ch) || unicode.Is(unicode.Mn, ch) }
	joins := func(i int) bool {
		if i == 0 || i+1 >= len(r) {
			return false
		}
		prev, ch, next := r[i-1], r[i], r[i+1]
		if unicode.IsLetter(prev) && unicode.IsLetter(next) {
			return ch == '.'
		}
		if unicode.IsDigit(prev) && unicode.IsDigit(next) {
			return ch == dec || ch == ':' || ch == '-' || len(thousands) == 1 && ch == thousands[0] && ch != ' '
		}
		return false
	}
	var out [][2]int
	start := -1
	for i, ch := range r {
		in := isWord(ch) || start >= 0 && joins(i)
		switch {
		case in && start < 0:
			start = i
		case !in && start >= 0:
			out = append(out, [2]int{start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(r)})
	}
	return out
}

// findText finds needle in text from the 0-based rune offset from, ignoring
// case but not accents, as Position and PatternCount do. Matches do not
// overlap. It returns the 0-based offsets of every match.
func findText(text, needle string) []int {
	if needle == "" {
		return nil
	}
	t := []rune(lowerText(text))
	n := []rune(lowerText(needle))
	var out []int
	for i := 0; i+len(n) <= len(t); {
		if string(t[i:i+len(n)]) == string(n) {
			out = append(out, i)
			i += len(n)
			continue
		}
		i++
	}
	return out
}

// quote writes text as a FileMaker text literal, the way Quote does: a
// quotation mark is escaped, a return is written ¶ and a literal ¶ as \¶. A
// backslash is escaped only where it would otherwise read as an escape.
func quote(s string) string {
	r := []rune(s)
	var b strings.Builder
	b.WriteByte('"')
	for i, ch := range r {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\r':
			b.WriteRune('¶')
		case '¶':
			b.WriteString(`\¶`)
		case '\\':
			if i+1 == len(r) || strings.ContainsRune("\"\\¶\r", r[i+1]) {
				b.WriteString(`\\`)
			} else {
				b.WriteByte('\\')
			}
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// charText is Char: every five decimal digits, from the right, is one UTF-16
// code unit, the rightmost first. Char ( 6566 ) is "ᦦ" and Char ( 6600065 ) "AB".
func charText(n int64) string {
	var units []uint16
	for ; n > 0; n /= 100000 {
		if u := n % 100000; u != 0 {
			units = append(units, uint16(u))
		}
	}
	return string(utf16.Decode(units))
}

// codeUnits is the text as Code reads it: its UTF-16 code units.
func codeUnits(s string) []uint16 { return utf16.Encode([]rune(s)) }
