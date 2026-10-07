package calc

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// A value is what a formula works with: nothing (an empty field), a number,
// a text, a truth value or a moment in time.
type value struct {
	kind  valueKind
	num   float64
	text  string
	truth bool
	when  time.Time
}

type valueKind int

const (
	kindEmpty valueKind = iota
	kindNumber
	kindText
	kindBool
	kindTime
)

var empty = value{kind: kindEmpty}

func numberValue(n float64) value { return value{kind: kindNumber, num: n} }
func textValue(s string) value    { return value{kind: kindText, text: s} }
func boolValue(b bool) value      { return value{kind: kindBool, truth: b} }
func timeValue(t time.Time) value { return value{kind: kindTime, when: t} }

// The layouts a date or timestamp coming out of the database may be written in.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// fromStored turns a value read from a record into something a formula can
// work with. Anything it does not recognise becomes its text.
func fromStored(raw any) value {
	switch v := raw.(type) {
	case nil:
		return empty
	case bool:
		return boolValue(v)
	case float64:
		return numberValue(v)
	case float32:
		return numberValue(float64(v))
	case int:
		return numberValue(float64(v))
	case int32:
		return numberValue(float64(v))
	case int64:
		return numberValue(float64(v))
	case time.Time:
		return timeValue(v)
	case []byte:
		return textFromStored(string(v))
	case string:
		return textFromStored(v)
	default:
		return textFromStored(fmt.Sprintf("%v", v))
	}
}

// textFromStored keeps a stored string as text. It is not guessed into a
// number: "007" and "1e3" are text the user typed, and a NUMBER column
// already arrives as a number.
func textFromStored(s string) value {
	if s == "" {
		return empty
	}
	return textValue(s)
}

func (v value) isEmpty() bool { return v.kind == kindEmpty }

// asNumber coerces to a number. Empty counts as zero, as it does in a
// spreadsheet; text that is not a number is an error, so a typo is reported
// rather than silently treated as zero.
func (v value) asNumber(at int, what string) (float64, error) {
	switch v.kind {
	case kindEmpty:
		return 0, nil
	case kindNumber:
		return v.num, nil
	case kindBool:
		if v.truth {
			return 1, nil
		}
		return 0, nil
	case kindText:
		n, err := strconv.ParseFloat(strings.TrimSpace(v.text), 64)
		if err != nil {
			return 0, errorAt(at, "%s is not a number: %q", what, v.text)
		}
		return n, nil
	case kindTime:
		return 0, errorAt(at, "%s is a date, which cannot be used as a number", what)
	}
	return 0, errorAt(at, "%s cannot be used as a number", what)
}

// asText coerces to text. A whole number loses its ".0" so 100 reads as "100".
func (v value) asText() string {
	switch v.kind {
	case kindEmpty:
		return ""
	case kindNumber:
		return formatNumber(v.num)
	case kindText:
		return v.text
	case kindBool:
		if v.truth {
			return "true"
		}
		return "false"
	case kindTime:
		if v.when.Hour() == 0 && v.when.Minute() == 0 && v.when.Second() == 0 {
			return v.when.Format("2006-01-02")
		}
		return v.when.Format(time.RFC3339)
	}
	return ""
}

// asBool decides truth: a truth value as it is, a number by being non-zero,
// text by not being empty, and empty is false.
func (v value) asBool() bool {
	switch v.kind {
	case kindEmpty:
		return false
	case kindBool:
		return v.truth
	case kindNumber:
		return v.num != 0
	case kindText:
		return strings.TrimSpace(v.text) != ""
	case kindTime:
		return !v.when.IsZero()
	}
	return false
}

func (v value) asTime(at int, what string) (time.Time, error) {
	switch v.kind {
	case kindTime:
		return v.when, nil
	case kindText:
		if t, ok := parseTime(v.text); ok {
			return t, nil
		}
	case kindEmpty:
		return time.Time{}, errorAt(at, "%s is empty, so it has no date", what)
	}
	return time.Time{}, errorAt(at, "%s is not a date: %q", what, v.asText())
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// isNumeric reports whether the value can be compared as a number without a
// guess: comparisons fall back to text when either side cannot.
func (v value) isNumeric() bool {
	switch v.kind {
	case kindNumber, kindBool:
		return true
	case kindText:
		_, err := strconv.ParseFloat(strings.TrimSpace(v.text), 64)
		return err == nil
	}
	return false
}

func formatNumber(n float64) string {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return ""
	}
	if n == math.Trunc(n) && math.Abs(n) < 1e15 {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
