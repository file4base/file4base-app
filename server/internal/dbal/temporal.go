package dbal

import (
	"strings"
	"time"
)

// Dates and timestamps reach the database as text from several paths: an
// import converts to RFC 3339 ("2026-01-02T03:04:05Z"), a data file and a
// migration carry that text, a client sends it. PostgreSQL reads that text,
// but MariaDB's DATETIME and DATE refuse its "T", "Z" and offset; and a time
// of day written on 1 January of year 0, as earlier migrations did, is out of
// PostgreSQL's range. TemporalValue turns such text into what both engines
// store the same way.

// timestampLayouts are the texts a timestamp is accepted in. A text without
// an offset is in UTC, the convention of every converter that writes one.
var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02",
}

// dateLayouts are the texts a date is accepted in; a date written with a
// time keeps the day it was written on.
var dateLayouts = []string{
	"2006-01-02",
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
}

// TemporalValue is the value to bind for a column of fieldType: a timestamp
// given as text or time.Time becomes the time.Time in UTC that it names, and
// a date given as text or time.Time becomes its "2006-01-02" day, which every
// engine reads. A time of day on 1 January of year 0 is put on 1 January
// 0001, as the calculation engine and FileMaker place one. Anything else,
// including text that names no date or time, is returned unchanged, for the
// database to refuse as it would have.
func TemporalValue(fieldType AgnosticFieldType, value interface{}) interface{} {
	switch fieldType {
	case FieldTypeTimestamp:
		switch v := value.(type) {
		case string:
			if at, ok := parseTemporal(v, timestampLayouts); ok {
				return timeOfDayOnYearOne(at.UTC())
			}
		case time.Time:
			return timeOfDayOnYearOne(v.UTC())
		}
	case FieldTypeDate:
		switch v := value.(type) {
		case string:
			if at, ok := parseTemporal(v, dateLayouts); ok {
				return at.Format("2006-01-02")
			}
		case time.Time:
			return v.Format("2006-01-02")
		}
	}
	return value
}

func parseTemporal(text string, layouts []string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	for _, layout := range layouts {
		if at, err := time.Parse(layout, text); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

func timeOfDayOnYearOne(at time.Time) time.Time {
	if at.Year() == 0 && at.Month() == time.January && at.Day() == 1 {
		return time.Date(1, time.January, 1, at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), time.UTC)
	}
	return at
}
