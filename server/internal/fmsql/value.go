package fmsql

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Kind is the type of a value: of an argument, a constant, an expression or
// a column of the result.
type Kind int

const (
	KindNull Kind = iota
	KindText
	KindNumber
	KindDate
	// KindTime is a time of day. A Time field is held as a timestamp on
	// 1 January 0001, so a time is compared and bound as that timestamp.
	KindTime
	KindTimestamp
	// kindCondition is a test: a comparison, AND, OR, NOT…
	kindCondition
)

func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindNumber:
		return "number"
	case KindDate:
		return "date"
	case KindTime:
		return "time"
	case KindTimestamp:
		return "timestamp"
	case kindCondition:
		return "condition"
	}
	return "null"
}

// Value is an argument or a constant, in the text SQL writes it in: a
// number as 12.5, a date as 2026-01-05, a time as 13:04:05, a timestamp as
// 2026-01-05 13:04:05, with a fraction of a second when it has one.
type Value struct {
	Kind Kind
	Text string
}

// The layouts of SQL dates, times and timestamps. Parsing accepts a
// fraction of a second after the seconds without the layout naming it.
const (
	dateLayout      = "2006-01-02"
	timeLayout      = "15:04:05"
	timestampLayout = "2006-01-02 15:04:05"
	// formats write the fraction of a second only when there is one.
	timeFormat      = "15:04:05.999999999"
	timestampFormat = "2006-01-02 15:04:05.999999999"
)

// temporalLiteral reads DATE '…', TIME '…' or TIMESTAMP '…'.
func temporalLiteral(keyword, text string) (Value, bool) {
	text = strings.TrimSpace(text)
	switch keyword {
	case "DATE":
		if at, err := time.Parse(dateLayout, text); err == nil {
			return Value{Kind: KindDate, Text: at.Format(dateLayout)}, true
		}
	case "TIME":
		if at, err := time.Parse(timeLayout, text); err == nil {
			return Value{Kind: KindTime, Text: at.Format(timeFormat)}, true
		}
	default:
		if at, err := time.Parse(timestampLayout, text); err == nil {
			return Value{Kind: KindTimestamp, Text: at.Format(timestampFormat)}, true
		}
	}
	return Value{}, false
}

// convert is v as a value of kind to, as a constant compared with a field of
// that type is read: '2026-01-05' compared with a date is that date. A time
// becomes its timestamp on 1 January 0001.
func convert(v Value, to Kind) (Value, bool) {
	if v.Kind == KindNull || v.Kind == KindText && v.Text == "" {
		return Value{Kind: KindNull}, true
	}
	if to == KindTime {
		to = KindTimestamp
	}
	switch to {
	case KindNull:
		return convert(v, base(v.Kind))
	case KindText:
		return Value{Kind: KindText, Text: v.Text}, true
	case KindNumber:
		switch v.Kind {
		case KindNumber:
			return v, true
		case KindText:
			if n, ok := parseNumber(v.Text); ok {
				return Value{Kind: KindNumber, Text: n.String()}, true
			}
		}
	case KindDate:
		switch v.Kind {
		case KindDate:
			return v, true
		case KindTimestamp:
			return Value{Kind: KindDate, Text: v.Text[:len(dateLayout)]}, true
		case KindText:
			if d, ok := temporalLiteral("DATE", v.Text); ok {
				return d, true
			}
		}
	case KindTimestamp:
		switch v.Kind {
		case KindTimestamp:
			return v, true
		case KindTime:
			return Value{Kind: KindTimestamp, Text: "0001-01-01 " + v.Text}, true
		case KindDate:
			return Value{Kind: KindTimestamp, Text: v.Text + " 00:00:00"}, true
		case KindText:
			for _, k := range []string{"TIMESTAMP", "TIME", "DATE"} {
				if t, ok := temporalLiteral(k, v.Text); ok {
					return convert(t, KindTimestamp)
				}
			}
		}
	}
	return Value{}, false
}

// maxDigits bounds a number: no more digits, before or after the point,
// than a database column holds, so that 1e999999999 is refused rather than
// written out.
const maxDigits = 65

// parseNumber reads a number, refusing one with more digits than maxDigits.
func parseNumber(text string) (decimal.Decimal, bool) {
	n, err := decimal.NewFromString(strings.TrimSpace(text))
	if err != nil {
		return decimal.Decimal{}, false
	}
	exp, digits := int64(n.Exponent()), int64(n.NumDigits())
	if exp < -maxDigits || exp+digits > maxDigits {
		return decimal.Decimal{}, false
	}
	return n, true
}

// base is the kind a value is compared and bound as.
func base(k Kind) Kind {
	if k == KindTime {
		return KindTimestamp
	}
	return k
}

// Format writes a value read from the database as ExecuteSQL answers it:
// numbers, dates, times and timestamps in SQL's own spelling, whatever the
// file's locale. A timestamp on 1 January 0001 is the time of day a Time
// field holds. Nothing is empty.
func Format(kind Kind, raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	if b, ok := raw.([]byte); ok {
		raw = string(b)
	}
	switch base(kind) {
	case KindNumber:
		switch v := raw.(type) {
		case int64:
			return strconv.FormatInt(v, 10), nil
		case float64:
			return decimal.NewFromFloat(v).String(), nil
		case bool:
			if v {
				return "1", nil
			}
			return "0", nil
		case string:
			n, err := decimal.NewFromString(strings.TrimSpace(v))
			if err != nil {
				return "", fmt.Errorf("%q is not a number", v)
			}
			return n.String(), nil
		}
	case KindDate:
		switch v := raw.(type) {
		case time.Time:
			return v.UTC().Format(dateLayout), nil
		case string:
			if len(v) >= len(dateLayout) {
				if at, err := time.Parse(dateLayout, v[:len(dateLayout)]); err == nil {
					return at.Format(dateLayout), nil
				}
			}
		}
	case KindTimestamp:
		var at time.Time
		switch v := raw.(type) {
		case time.Time:
			at = v.UTC()
		case string:
			parsed, err := time.Parse(timestampLayout, strings.Replace(strings.TrimSpace(v), "T", " ", 1))
			if err != nil {
				return "", fmt.Errorf("%q is not a timestamp", v)
			}
			at = parsed
		default:
			return "", fmt.Errorf("%v is not a timestamp", raw)
		}
		if at.Year() == 1 && at.Month() == time.January && at.Day() == 1 {
			return at.Format(timeFormat), nil
		}
		return at.Format(timestampFormat), nil
	default:
		switch v := raw.(type) {
		case string:
			return v, nil
		case time.Time:
			return v.UTC().Format(timestampFormat), nil
		}
		return fmt.Sprint(raw), nil
	}
	return "", fmt.Errorf("%v is not a %s", raw, kind)
}

// Argument is a ? argument of a kind, written as Value says, checked: a
// time past 24 hours, a number or a date SQL cannot read is refused.
func Argument(kind Kind, text string) (Value, bool) {
	switch kind {
	case KindNull:
		return Value{Kind: KindNull}, true
	case KindText:
		return Value{Kind: KindText, Text: text}, true
	case KindNumber:
		n, ok := parseNumber(text)
		return Value{Kind: KindNumber, Text: n.String()}, ok
	case KindDate:
		return temporalLiteral("DATE", text)
	case KindTime:
		return temporalLiteral("TIME", text)
	case KindTimestamp:
		return temporalLiteral("TIMESTAMP", text)
	}
	return Value{}, false
}
