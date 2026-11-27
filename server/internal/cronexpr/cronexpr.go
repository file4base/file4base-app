// Package cronexpr reads the five-field cron expressions a scheduled
// transfer runs on (#108) and works out when one is next due.
//
// It is deliberately the classic, small dialect — minute, hour, day of month,
// month, day of week — with `*`, lists (`1,15`), ranges (`1-5`) and steps
// (`*/15`, `0-30/10`), and nothing else: no seconds field, no names, no `L`,
// `W` or `#`. An expression it does not understand is refused when it is
// saved, never guessed at when it runs. Times are UTC, or with NextIn the
// wall clock of a time zone.
//
// When both the day of month and the day of week are restricted, a day
// matches if either does, which is what cron has always meant.
package cronexpr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalid is returned for an expression this package does not read.
var ErrInvalid = errors.New("invalid cron expression")

// Schedule is a parsed cron expression.
type Schedule struct {
	source  string
	minutes [60]bool
	hours   [24]bool
	days    [32]bool // 1..31
	months  [13]bool // 1..12
	weekday [7]bool  // 0 = Sunday

	// Whether the day fields were restricted, which decides how they combine.
	daysRestricted    bool
	weekdayRestricted bool
}

// String is the expression as it was written.
func (s *Schedule) String() string { return s.source }

type field struct {
	name     string
	min, max int
}

var fields = []field{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day of month", 1, 31},
	{"month", 1, 12},
	{"day of week", 0, 7}, // 7 is Sunday too
}

// Parse reads a five-field expression.
func Parse(expr string) (*Schedule, error) {
	source := strings.Join(strings.Fields(expr), " ")
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("%w: %q has %d fields; it needs five — minute, hour, day of month, month, day of week",
			ErrInvalid, source, len(parts))
	}

	s := &Schedule{source: source}
	for i, part := range parts {
		values, restricted, err := parseField(part, fields[i])
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %v", ErrInvalid, source, err)
		}
		for _, v := range values {
			switch i {
			case 0:
				s.minutes[v] = true
			case 1:
				s.hours[v] = true
			case 2:
				s.days[v] = true
			case 3:
				s.months[v] = true
			case 4:
				s.weekday[v%7] = true
			}
		}
		switch i {
		case 2:
			s.daysRestricted = restricted
		case 4:
			s.weekdayRestricted = restricted
		}
	}
	return s, nil
}

// parseField reads one field: a comma-separated list of `*`, `n`, `a-b`, each
// optionally followed by `/step`.
func parseField(text string, f field) ([]int, bool, error) {
	var values []int
	restricted := text != "*"
	for _, item := range strings.Split(text, ",") {
		if item == "" {
			return nil, false, fmt.Errorf("the %s field has an empty item", f.name)
		}
		rangePart, step := item, 1
		if slash := strings.Index(item, "/"); slash >= 0 {
			rangePart = item[:slash]
			n, err := strconv.Atoi(item[slash+1:])
			if err != nil || n <= 0 {
				return nil, false, fmt.Errorf("the %s field has a step %q that is not a positive number",
					f.name, item[slash+1:])
			}
			step = n
		}

		lo, hi := f.min, f.max
		switch {
		case rangePart == "*":
			if f.name == "day of week" {
				hi = 6 // `*` is each day once, not Sunday twice
			}
		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			a, errA := strconv.Atoi(bounds[0])
			b, errB := strconv.Atoi(bounds[1])
			if errA != nil || errB != nil {
				return nil, false, fmt.Errorf("the %s field has a range %q that is not two numbers", f.name, rangePart)
			}
			lo, hi = a, b
		default:
			n, err := strconv.Atoi(rangePart)
			if err != nil {
				return nil, false, fmt.Errorf("the %s field has %q, which is not a number", f.name, rangePart)
			}
			lo, hi = n, n
			if step != 1 {
				hi = f.max // `5/15` means from 5, every 15
			}
		}
		if lo < f.min || hi > f.max || lo > hi {
			return nil, false, fmt.Errorf("the %s field allows %d to %d, not %q", f.name, f.min, f.max, rangePart)
		}
		for v := lo; v <= hi; v += step {
			values = append(values, v)
		}
	}
	return values, restricted, nil
}

// maxSearch bounds the search for the next run. Every valid expression is due
// at least once in a leap-year cycle, so running past this means none is.
const maxSearch = 5 * 366 * 24 * time.Hour

// Next is the first time strictly after `after`, to the minute, at which the
// schedule is due, read in UTC. It returns the zero time for an expression
// that is never due, such as 0 0 30 2 *.
func (s *Schedule) Next(after time.Time) time.Time { return s.next(after) }

// NextIn is Next with the expression read as wall-clock time in loc, as
// the server schedules are (#188). A time a change to daylight saving time
// skips is not due that day, and one a change back repeats is due once, at
// its first occurrence. The time returned is in UTC.
func (s *Schedule) NextIn(after time.Time, loc *time.Location) time.Time {
	if loc == nil || loc == time.UTC {
		return s.next(after)
	}
	// The search runs on the wall clock, as if it were UTC, where every day
	// has all its minutes once; each time found is then placed in loc.
	a := after.In(loc)
	wall := time.Date(a.Year(), a.Month(), a.Day(), a.Hour(), a.Minute(), a.Second(), a.Nanosecond(), time.UTC)
	for i := 0; i < 4*24*60; i++ {
		n := s.next(wall)
		if n.IsZero() {
			return n
		}
		t := time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), n.Minute(), 0, 0, loc)
		// time.Date may give either occurrence of a repeated time: take the
		// first one, and only it, so the second is never due even once the
		// first has passed.
		for _, back := range []time.Duration{time.Hour, 30 * time.Minute} {
			if e := t.Add(-back); sameWallMinute(e.In(loc), n) {
				t = e
				break
			}
		}
		if sameWallMinute(t, n) && t.After(after) {
			return t.UTC()
		}
		wall = n // skipped (or already past, as the first of a repeated hour)
	}
	return time.Time{}
}

func sameWallMinute(t, wall time.Time) bool {
	return t.Day() == wall.Day() && t.Hour() == wall.Hour() && t.Minute() == wall.Minute()
}

func (s *Schedule) next(after time.Time) time.Time {
	t := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(maxSearch)
	for t.Before(limit) {
		if !s.months[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !s.hours[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
			continue
		}
		if !s.minutes[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

func (s *Schedule) dayMatches(t time.Time) bool {
	dom := s.days[t.Day()]
	dow := s.weekday[int(t.Weekday())]
	switch {
	case s.daysRestricted && s.weekdayRestricted:
		return dom || dow
	case s.daysRestricted:
		return dom
	case s.weekdayRestricted:
		return dow
	default:
		return true
	}
}
