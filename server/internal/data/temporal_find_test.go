package data

import (
	"errors"
	"testing"
	"time"
)

func TestParseTemporalSpan(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		text     string
		from, to string // "" when the text names no span
	}{
		{"2026", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z"},
		{"2026-02", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z"},
		{"02/2026", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z"},
		{"2026-01-31", "2026-01-31T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"2026/01/31", "2026-01-31T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"31/01/2026", "2026-01-31T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"01/31/2026", "2026-01-31T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"31.1.2026", "2026-01-31T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"5/5/2026", "2026-05-05T00:00:00Z", "2026-05-06T00:00:00Z"},
		{"31 Jan 2026", "2026-01-31T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"31/01/2026 14:05", "2026-01-31T14:05:00Z", "2026-01-31T14:06:00Z"},
		{"31/01/2026 2:05 PM", "2026-01-31T14:05:00Z", "2026-01-31T14:06:00Z"},
		{"2026-01-31 14:05", "2026-01-31T14:05:00Z", "2026-01-31T14:06:00Z"},
		{"2026-01-31T14:05:06Z", "2026-01-31T14:05:06Z", "2026-01-31T14:05:07Z"},
		{"2026-01-31T15:05:06+01:00", "2026-01-31T14:05:06Z", "2026-01-31T14:05:07Z"},
		{"2026-01-31T14:05:06.25Z", "2026-01-31T14:05:06.25Z", "2026-01-31T14:05:06.250001Z"},
		{"14:05", "0001-01-01T14:05:00Z", "0001-01-01T14:06:00Z"},
		{"14:05:06", "0001-01-01T14:05:06Z", "0001-01-01T14:05:07Z"},
		{"12:00 AM", "0001-01-01T00:00:00Z", "0001-01-01T00:01:00Z"},
		{"0000-01-01T14:05:00Z", "0001-01-01T14:05:00Z", "0001-01-01T14:05:01Z"},
		{"31/02/2026", "", ""},
		{"2026-13", "", ""},
		{"25:00", "", ""},
		{"12", "", ""},
		{"next Tuesday", "", ""},
	} {
		span, ok, err := parseTemporalSpan(c.text)
		if err != nil {
			t.Errorf("%q: %v", c.text, err)
			continue
		}
		if c.from == "" {
			if ok {
				t.Errorf("%q names %v to %v, want no span", c.text, span.from, span.to)
			}
			continue
		}
		if !ok || !span.from.Equal(at(c.from)) || !span.to.Equal(at(c.to)) {
			t.Errorf("%q = %v to %v (%v), want %s to %s", c.text, span.from, span.to, ok, c.from, c.to)
		}
	}

	// Day first or month first cannot be told apart: refused, not guessed.
	if _, _, err := parseTemporalSpan("02/01/2026"); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("02/01/2026: %v, want ErrInvalidValue", err)
	}
}
