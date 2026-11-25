package data

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// A find on a date or a timestamp compares the days and instants the column
// holds, not their text: each engine writes a timestamp as text its own way
// (PostgreSQL in the session's zone), and neither writes it as File4Base
// shows it or as it is typed. What is typed names a span of time, as in
// FileMaker: "2026" is the year, "2026-01" or "01/2026" the month,
// "2026-01-02" the day, "2026-01-02 03:04" the minute, and so on; "15:30" is
// a time of day, which a Time field holds on 1 January 0001. A criterion
// finds the values in that span, and a comparison or a range is taken
// against its edges, so "...2026-01-31" takes in the whole of the 31st. A
// text without a zone is in UTC, the zone File4Base shows instants in.

// temporalSpan is the stretch of time a criterion names: from inclusive, to
// exclusive. dayOnly is set when it is a whole number of days.
type temporalSpan struct {
	from, to time.Time
	dayOnly  bool
}

var (
	// 02/01/2026, 2.1.2026 or 02-01-2026, with a time of day or not.
	numericDate = regexp.MustCompile(`^(\d{1,2})[/.-](\d{1,2})[/.-](\d{4})(?:[ T]+(.+))?$`)
	// 01/2026, a month.
	numericMonth = regexp.MustCompile(`^(\d{1,2})[/.-](\d{4})$`)
	isoMonth     = regexp.MustCompile(`^(\d{4})-(\d{1,2})$`)
	year         = regexp.MustCompile(`^\d{4}$`)
	// 15:30, 15:30:45, 15:30:45.25, 3:30 PM.
	clockTime = regexp.MustCompile(`^(\d{1,2}):(\d{2})(?::(\d{2})(?:[.,](\d{1,9}))?)?\s*([AaPp][Mm])?$`)
)

// isoInstants are the instants written ISO-fashion, with the span each
// names: to the second (or finer, with a fraction) or to the minute.
var isoInstants = []struct {
	layout string
	minute bool
}{
	{time.RFC3339Nano, false},
	{"2006-01-02T15:04:05.999999999", false},
	{"2006-01-02 15:04:05.999999999Z07:00", false},
	{"2006-01-02 15:04:05.999999999", false},
	{"2006-01-02T15:04Z07:00", true},
	{"2006-01-02T15:04", true},
	{"2006-01-02 15:04", true},
}

// namedDays are the days written with the month's name, as File4Base shows
// one ("2 Jan 2026").
var namedDays = []string{"2 Jan 2006", "2 January 2006", "Jan 2 2006", "January 2 2006", "Jan 2, 2006", "January 2, 2006"}

// parseTemporalSpan reads what is typed in Find mode as a span of time. ok
// is false when the text names none, and the criterion is then compared as
// text. A day written with its day and month both 12 or less, and no way to
// tell them apart, is an error rather than a guess.
func parseTemporalSpan(text string) (span temporalSpan, ok bool, err error) {
	text = strings.TrimSpace(text)
	day := func(y, m, d int) (time.Time, bool) {
		at := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
		return at, at.Year() == y && int(at.Month()) == m && at.Day() == d
	}
	whole := func(from time.Time, years, months, days int) temporalSpan {
		return temporalSpan{from: from, to: from.AddDate(years, months, days), dayOnly: true}
	}

	switch {
	case text == "":
		return span, false, nil

	case year.MatchString(text):
		y, _ := strconv.Atoi(text)
		from, _ := day(y, 1, 1)
		return whole(from, 1, 0, 0), true, nil

	case isoMonth.MatchString(text):
		m := isoMonth.FindStringSubmatch(text)
		return monthSpan(m[1], m[2])

	case numericMonth.MatchString(text):
		m := numericMonth.FindStringSubmatch(text)
		return monthSpan(m[2], m[1])

	case clockTime.MatchString(text):
		from, to, ok := clockSpan(text, time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC))
		return temporalSpan{from: from, to: to}, ok, nil

	case numericDate.MatchString(text):
		m := numericDate.FindStringSubmatch(text)
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		dmy, dmyOK := day(y, b, a)
		mdy, mdyOK := day(y, a, b)
		var on time.Time
		switch {
		case dmyOK && mdyOK && !dmy.Equal(mdy):
			return span, false, fmt.Errorf(
				"%w: %q could be %s or %s; write the date year first, as %s",
				ErrInvalidValue, text, dmy.Format("2 January 2006"), mdy.Format("2 January 2006"),
				dmy.Format("2006-01-02"))
		case dmyOK:
			on = dmy
		case mdyOK:
			on = mdy
		default:
			return span, false, nil
		}
		if m[4] == "" {
			return whole(on, 0, 0, 1), true, nil
		}
		from, to, ok := clockSpan(strings.TrimSpace(m[4]), on)
		return temporalSpan{from: from, to: to}, ok, nil
	}

	for _, layout := range []string{"2006-01-02", "2006/01/02"} {
		if at, err := time.Parse(layout, text); err == nil {
			return whole(at, 0, 0, 1), true, nil
		}
	}
	for _, layout := range namedDays {
		if at, err := time.Parse(layout, text); err == nil {
			return whole(at, 0, 0, 1), true, nil
		}
	}
	for _, instant := range isoInstants {
		at, err := time.Parse(instant.layout, text)
		if err != nil {
			continue
		}
		at = dbal.NormalizeTemporal(dbal.FieldTypeTimestamp, at).(time.Time)
		switch {
		case instant.minute:
			return temporalSpan{from: at, to: at.Add(time.Minute)}, true, nil
		case at.Nanosecond() != 0:
			return temporalSpan{from: at, to: at.Add(time.Microsecond)}, true, nil
		default:
			return temporalSpan{from: at, to: at.Add(time.Second)}, true, nil
		}
	}
	return span, false, nil
}

func monthSpan(yearText, monthText string) (temporalSpan, bool, error) {
	y, _ := strconv.Atoi(yearText)
	m, _ := strconv.Atoi(monthText)
	if m < 1 || m > 12 {
		return temporalSpan{}, false, nil
	}
	from := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	return temporalSpan{from: from, to: from.AddDate(0, 1, 0), dayOnly: true}, true, nil
}

// clockSpan is the minute, second or instant a time of day names on day.
func clockSpan(text string, day time.Time) (from, to time.Time, ok bool) {
	m := clockTime.FindStringSubmatch(text)
	if m == nil {
		return from, to, false
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	second, _ := strconv.Atoi(m[3])
	nanos := 0
	if m[4] != "" {
		nanos, _ = strconv.Atoi((m[4] + "00000000")[:9])
	}
	switch strings.ToUpper(m[5]) {
	case "AM", "PM":
		if hour < 1 || hour > 12 {
			return from, to, false
		}
		hour %= 12
		if strings.EqualFold(m[5], "PM") {
			hour += 12
		}
	}
	if hour > 23 || minute > 59 || second > 59 {
		return from, to, false
	}
	from = time.Date(day.Year(), day.Month(), day.Day(), hour, minute, second, nanos, time.UTC)
	switch {
	case m[3] == "":
		return from, from.Add(time.Minute), true
	case m[4] != "":
		return from, from.Add(time.Microsecond), true
	default:
		return from, from.Add(time.Second), true
	}
}

// temporalCriterionClause renders a criterion on a date or timestamp column
// as a comparison of the column with the edges of the span its text names.
// handled is false when the criterion is not one (a wildcard, a text that
// names no date or time), and it is then compared as text.
func temporalCriterionClause(
	dialect dbal.Dialect, colIdent string, fieldType dbal.AgnosticFieldType,
	crit FindCriterion, place func() string, values *[]interface{},
) (clause string, handled bool, err error) {
	if fieldType != dbal.FieldTypeDate && fieldType != dbal.FieldTypeTimestamp {
		return "", false, nil
	}
	spanOf := func(v interface{}) (temporalSpan, bool, error) {
		if v == nil {
			return temporalSpan{}, false, nil
		}
		span, ok, err := parseTemporalSpan(fmt.Sprint(v))
		// A date column holds days: a time of day alone names none, and an
		// instant is found on the day it falls on.
		if ok && fieldType == dbal.FieldTypeDate && !span.dayOnly {
			if span.from.Year() == 1 && span.from.YearDay() == 1 {
				return temporalSpan{}, false, nil
			}
			from := time.Date(span.from.Year(), span.from.Month(), span.from.Day(), 0, 0, 0, 0, time.UTC)
			span = temporalSpan{from: from, to: from.AddDate(0, 0, 1), dayOnly: true}
		}
		return span, ok, err
	}
	bind := func(at time.Time) string {
		p := place()
		if fieldType == dbal.FieldTypeDate {
			*values = append(*values, at.Format("2006-01-02"))
		} else {
			*values = append(*values, dbal.TemporalValue(dialect.Engine(), fieldType, at))
		}
		return p
	}

	switch crit.Operator {
	case "RANGE":
		// Either end may be left open ("...2026-01-31"). Both are read
		// before either is bound, so a range compared as text binds
		// nothing here.
		var spans [2]*temporalSpan
		for i, v := range []interface{}{crit.Value, crit.ValueTo} {
			if v == nil || strings.TrimSpace(fmt.Sprint(v)) == "" {
				continue
			}
			span, ok, err := spanOf(v)
			if err != nil || !ok {
				return "", false, err
			}
			spans[i] = &span
		}
		var parts []string
		if spans[0] != nil {
			parts = append(parts, fmt.Sprintf("%s >= %s", colIdent, bind(spans[0].from)))
		}
		if spans[1] != nil {
			parts = append(parts, fmt.Sprintf("%s < %s", colIdent, bind(spans[1].to)))
		}
		if len(parts) == 0 {
			return "", false, nil
		}
		return "(" + strings.Join(parts, " AND ") + ")", true, nil

	case "LIKE", "", "=", "==", "!=", "<", "<=", ">", ">=":
	default:
		return "", false, nil
	}

	text := fmt.Sprint(crit.Value)
	if crit.Operator == "LIKE" {
		// What is typed is sent as "%text%"; a wildcard of its own makes
		// it a pattern on the text.
		text = strings.TrimSuffix(strings.TrimPrefix(text, "%"), "%")
		if strings.ContainsAny(text, "%_") {
			return "", false, nil
		}
	}
	span, ok, err := spanOf(text)
	if err != nil || !ok {
		return "", false, err
	}
	switch crit.Operator {
	case "<":
		return fmt.Sprintf("%s < %s", colIdent, bind(span.from)), true, nil
	case "<=":
		return fmt.Sprintf("%s < %s", colIdent, bind(span.to)), true, nil
	case ">":
		return fmt.Sprintf("%s >= %s", colIdent, bind(span.to)), true, nil
	case ">=":
		return fmt.Sprintf("%s >= %s", colIdent, bind(span.from)), true, nil
	case "!=":
		return fmt.Sprintf("(%s IS NULL OR %s < %s OR %s >= %s)",
			colIdent, colIdent, bind(span.from), colIdent, bind(span.to)), true, nil
	default:
		return fmt.Sprintf("(%s >= %s AND %s < %s)",
			colIdent, bind(span.from), colIdent, bind(span.to)), true, nil
	}
}
