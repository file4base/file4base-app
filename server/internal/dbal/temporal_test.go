package dbal

import (
	"testing"
	"time"
)

func TestTemporalValue(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v.UTC()
	}
	for _, c := range []struct {
		fieldType AgnosticFieldType
		in, want  interface{}
	}{
		{FieldTypeTimestamp, "2026-01-02T03:04:05Z", at("2026-01-02T03:04:05Z")},
		{FieldTypeTimestamp, "2026-01-02T03:04:05.25+02:00", at("2026-01-02T01:04:05.25Z")},
		{FieldTypeTimestamp, "2026-01-02 03:04:05", at("2026-01-02T03:04:05Z")},
		{FieldTypeTimestamp, "2026-01-02", at("2026-01-02T00:00:00Z")},
		{FieldTypeTimestamp, "0000-01-01T15:30:00Z", at("0001-01-01T15:30:00Z")},
		{FieldTypeTimestamp, "0001-01-01T15:30:00Z", at("0001-01-01T15:30:00Z")},
		{FieldTypeTimestamp, at("2026-01-02T03:04:05Z").In(time.FixedZone("x", 3600)), at("2026-01-02T03:04:05Z")},
		{FieldTypeTimestamp, "next Tuesday", "next Tuesday"},
		{FieldTypeTimestamp, nil, nil},
		{FieldTypeDate, "2026-01-02T00:00:00Z", "2026-01-02"},
		{FieldTypeDate, "2026-01-02T23:30:00-05:00", "2026-01-02"},
		{FieldTypeDate, "2026-01-02", "2026-01-02"},
		{FieldTypeDate, at("2026-01-02T00:00:00Z"), "2026-01-02"},
		{FieldTypeDate, "31/12/2026", "31/12/2026"},
		{FieldTypeText, "2026-01-02T03:04:05Z", "2026-01-02T03:04:05Z"},
	} {
		got := TemporalValue(c.fieldType, c.in)
		if gt, ok := got.(time.Time); ok {
			if wt, ok := c.want.(time.Time); !ok || !gt.Equal(wt) || gt.Location() != time.UTC {
				t.Errorf("%s %v = %v, want %v", c.fieldType, c.in, got, c.want)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s %v = %#v, want %#v", c.fieldType, c.in, got, c.want)
		}
	}
}
