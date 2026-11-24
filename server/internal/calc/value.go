package calc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
)

// A value is what a formula works with. FileMaker has no separate truth type:
// a test is the number 1 or 0. Dates, times and timestamps are numbers too —
// a day number, seconds, and seconds since 1 January 0001 — so `date + 1` is
// the next day and `date2 - date1` the days between, as in FileMaker.
type value struct {
	kind valueKind
	num  decimal.Decimal // number, day number, or seconds
	text string
	// places is how many decimal places a number shows when it is written as
	// text: 0 means FileMaker's default of 16, SetPrecision asks for more.
	places int32
}

type valueKind int

const (
	kindEmpty valueKind = iota
	kindNumber
	kindText
	kindDate
	kindTime
	kindTimestamp
)

var empty = value{kind: kindEmpty}

func numberValue(n decimal.Decimal) value { return value{kind: kindNumber, num: n} }
func intValue(n int64) value              { return numberValue(decimal.NewFromInt(n)) }
func textValue(s string) value            { return value{kind: kindText, text: s} }
func dateValue(day int64) value           { return value{kind: kindDate, num: decimal.NewFromInt(day)} }
func timeOfDayValue(secs decimal.Decimal) value {
	return value{kind: kindTime, num: secs}
}
func timestampValue(secs decimal.Decimal) value { return value{kind: kindTimestamp, num: secs} }

func boolValue(b bool) value {
	if b {
		return intValue(1)
	}
	return intValue(0)
}

func (v value) isEmpty() bool { return v.kind == kindEmpty || (v.kind == kindText && v.text == "") }

// fromStored turns a value read from a record into something a formula works
// with. A stored string stays text: "007" is what somebody typed, and a NUMBER
// column already arrives as a number.
func fromStored(raw any) value {
	switch v := raw.(type) {
	case nil:
		return empty
	case value:
		return v
	case bool:
		return boolValue(v)
	case decimal.Decimal:
		return numberValue(v)
	case json.Number:
		if n, err := decimal.NewFromString(string(v)); err == nil {
			return numberValue(n)
		}
		return textValue(string(v))
	case float64:
		return numberValue(decimal.NewFromFloat(v))
	case float32:
		return numberValue(decimal.NewFromFloat32(v))
	case int:
		return intValue(int64(v))
	case int32:
		return intValue(int64(v))
	case int64:
		return intValue(v)
	case time.Time:
		return fromTime(v)
	case []byte:
		return textValue(string(v))
	case string:
		return textValue(v)
	default:
		return textValue(fmt.Sprintf("%v", v))
	}
}

// Typed is a value read from a column of the given type, made into what a
// formula should see. A database driver may return a NUMERIC as text ("10")
// or a TIMESTAMP at midnight that looks like a date; read through Typed they
// stay a number and a timestamp, so "amount > 9" holds after a round trip.
// A value that does not read as its type is kept as it came.
func Typed(t ResultType, raw any) any {
	switch r := raw.(type) {
	case nil, value:
		return raw
	case string:
		switch t {
		case ResultNumber:
			if n, err := decimal.NewFromString(strings.TrimSpace(r)); err == nil {
				return numberValue(n)
			}
		case ResultDate:
			if d, ok := parseISO(strings.TrimSpace(r)); ok {
				return dateValue(d)
			}
		case ResultTimestamp:
			if ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r)); err == nil {
				return timestampValue(timestampSeconds(ts))
			}
			if s, ok := ISOLocale.parseTimestamp(r); ok {
				return timestampValue(s)
			}
		}
	case []byte:
		return Typed(t, string(r))
	case json.Number:
		if n, err := decimal.NewFromString(string(r)); err == nil {
			return numberValue(n)
		}
	case time.Time:
		switch t {
		case ResultTimestamp:
			return timestampValue(timestampSeconds(r))
		case ResultDate:
			return dateValue(dayNumber(r.Year(), int(r.Month()), r.Day()))
		}
	}
	return raw
}

// fromTime reads a moment from the database: midnight in UTC is a date, as a
// DATE column comes back, and anything else a timestamp of its wall clock.
func fromTime(t time.Time) value {
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return dateValue(dayNumber(t.Year(), int(t.Month()), t.Day()))
	}
	return timestampValue(timestampSeconds(t))
}

// asNumber reads the value as FileMaker does in arithmetic: text counts by
// the number it holds (see textToNumber), text holding no digit is 0, and
// nothing is 0.
func (v value) asNumber(loc *Locale) decimal.Decimal {
	switch v.kind {
	case kindText:
		n, _ := textToNumber(v.text, loc.decimal())
		return n
	case kindEmpty:
		return decimal.Zero
	}
	return v.num
}

// numberOrEmpty is the value as GetAsNumber reads it: nothing, or text
// holding no digit at all, has no number.
func (v value) numberOrEmpty(loc *Locale) (decimal.Decimal, bool) {
	switch v.kind {
	case kindEmpty:
		return decimal.Zero, false
	case kindText:
		return textToNumber(v.text, loc.decimal())
	}
	return v.num, true
}

// textToNumber reads text the way FileMaker turns it into a number, which is
// permissive: the digits are kept wherever they are and everything else is
// dropped, the locale's first decimal separator is the decimal point, and a
// minus sign or an opening parenthesis before the first digit makes the
// number negative. "12 €" is 12, "a1b2" is 12, "(5)" is -5, and in a French
// file "1.5" is 15 while "1,5" is 1.5. An exponent counts only when nothing
// but the number comes before it, so "1e3" is 1000 and "x1e3" is 13.
func textToNumber(s string, dec rune) (decimal.Decimal, bool) {
	var b strings.Builder
	negative, seenDigit, seenPoint, junk := false, false, false, false
	exponent := ""
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			seenDigit = true
		case r == dec && !seenPoint:
			b.WriteRune('.')
			seenPoint = true
		case (r == '-' || r == '(') && !seenDigit:
			negative = true
		case (r == 'e' || r == 'E') && seenDigit && !junk && exponent == "":
			j := i + 1
			sign := ""
			if j < len(runes) && (runes[j] == '-' || runes[j] == '+') {
				sign = string(runes[j])
				j++
			}
			k := j
			for k < len(runes) && runes[k] >= '0' && runes[k] <= '9' {
				k++
			}
			if k == j {
				junk = true
				continue
			}
			exponent = sign + string(runes[j:k])
			i = k - 1
		case r == '+' && !seenDigit, unicode.IsSpace(r):
		default:
			junk = true
		}
	}
	if !seenDigit {
		return decimal.Zero, false
	}
	digits := strings.TrimSuffix(b.String(), ".")
	if strings.HasPrefix(digits, ".") {
		digits = "0" + digits
	}
	if exponent != "" {
		digits += "e" + exponent
	}
	n, err := decimal.NewFromString(digits)
	if err != nil {
		return decimal.Zero, false
	}
	if negative {
		n = n.Neg()
	}
	return n, true
}

// asBool is a test's answer: a number is true when it is not zero, and text
// by the number it holds, so "abc" is false and "a1" true, as in FileMaker.
func (v value) asBool(loc *Locale) bool {
	switch v.kind {
	case kindEmpty:
		return false
	case kindText:
		n, ok := textToNumber(v.text, loc.decimal())
		return ok && !n.IsZero()
	}
	return !v.num.IsZero()
}

// asText writes the value the way FileMaker does under the given locale.
func (v value) asText(loc *Locale) string {
	switch v.kind {
	case kindEmpty:
		return ""
	case kindText:
		return v.text
	case kindNumber:
		return loc.formatNumber(v.num, v.places)
	case kindDate:
		return loc.formatDate(v.num.IntPart())
	case kindTime:
		return loc.formatTime(v.num)
	case kindTimestamp:
		return loc.formatTimestamp(v.num)
	}
	return ""
}

// asDay reads the value as a day number: a date as itself, a timestamp by its
// day, a number as a day number, and text as a date written in the locale.
func (v value) asDay(loc *Locale) (int64, bool) {
	switch v.kind {
	case kindDate, kindNumber:
		return v.num.IntPart(), true
	case kindTimestamp:
		return v.num.Div(decimal.NewFromInt(secondsPerDay)).Floor().IntPart() + 1, true
	case kindText:
		return loc.parseDate(v.text)
	}
	return 0, false
}

// asSeconds reads the value as a time of day in seconds.
func (v value) asSeconds(loc *Locale) (decimal.Decimal, bool) {
	switch v.kind {
	case kindTime, kindNumber:
		return v.num, true
	case kindTimestamp:
		return v.num.Mod(decimal.NewFromInt(secondsPerDay)), true
	case kindText:
		return loc.parseTime(v.text)
	}
	return decimal.Zero, false
}

// asTimestampSeconds reads the value as seconds since 1 January 0001.
func (v value) asTimestampSeconds(loc *Locale) (decimal.Decimal, bool) {
	switch v.kind {
	case kindTimestamp, kindNumber:
		return v.num, true
	case kindDate:
		return decimal.NewFromInt((v.num.IntPart() - 1) * secondsPerDay), true
	case kindText:
		return loc.parseTimestamp(v.text)
	}
	return decimal.Zero, false
}

const secondsPerDay = 86400

// dayNumber is FileMaker's number for a date: 1 January 0001 is day 1. Month
// and day may overflow, so Date(13; 1; 2026) is January 2027 as in FileMaker.
func dayNumber(year, month, day int) int64 {
	// Normalise the month first; days past the month's end are added after.
	y := int64(year) + floorDiv(int64(month-1), 12)
	m := floorMod(int64(month-1), 12) + 1
	return daysFromCivil(y, m, 1) + int64(day) - 1 - daysFromCivil(1, 1, 1) + 1
}

// civil returns the year, month and day of a day number.
func civil(day int64) (int64, int64, int64) {
	return civilFromDays(day - 1 + daysFromCivil(1, 1, 1))
}

// daysFromCivil and civilFromDays are Howard Hinnant's proleptic Gregorian
// algorithms, exact for any year, unlike time.Duration arithmetic.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe
}

func civilFromDays(z int64) (int64, int64, int64) {
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

func timestampSeconds(t time.Time) decimal.Decimal {
	day := dayNumber(t.Year(), int(t.Month()), t.Day())
	secs := int64(t.Hour()*3600 + t.Minute()*60 + t.Second())
	whole := decimal.NewFromInt((day-1)*secondsPerDay + secs)
	if t.Nanosecond() == 0 {
		return whole
	}
	return whole.Add(decimal.New(int64(t.Nanosecond()), -9))
}

// toTime is a date or timestamp as a time.Time in UTC, for storing.
func toTime(secs decimal.Decimal) time.Time {
	whole := secs.Floor()
	frac := secs.Sub(whole)
	total := whole.IntPart()
	day := floorDiv(total, secondsPerDay) + 1
	rem := floorMod(total, secondsPerDay)
	y, m, d := civil(day)
	return time.Date(int(y), time.Month(m), int(d), int(rem/3600), int(rem%3600/60), int(rem%60),
		int(frac.Mul(decimal.New(1, 9)).IntPart()), time.UTC)
}
