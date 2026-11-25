package data_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/stretchr/testify/require"
)

// A find on a date, a timestamp or a time of day compares the instants and
// days themselves, on both engines, rather than the text each engine writes
// them as: a date finds the whole day of a timestamp, a range takes in its
// last day, and the date formats File4Base shows are read back.
func TestTemporalFindCriteriaOnEveryEngine(t *testing.T) {
	ctx, _, svc, _ := newTemporalSolution(t)
	for _, r := range []map[string]interface{}{
		{"name": "a", "day": "2026-01-02", "at": "2026-01-02T03:04:05Z", "clock": "0001-01-01T15:30:00Z"},
		{"name": "b", "day": "2026-01-31", "at": "2026-01-31T23:59:59Z", "clock": "0001-01-01T08:00:00Z"},
		{"name": "c", "day": "2026-02-01", "at": "2026-02-01T00:00:00Z", "clock": "0001-01-01T15:30:45Z"},
		{"name": "d", "day": "2025-12-31", "at": "2025-12-31T12:00:00Z"},
		{"name": "e"},
	} {
		_, err := svc.InsertRow(ctx, "visits", r)
		require.NoError(t, err)
	}

	find := func(requests ...data.FindRequest) (string, error) {
		rows, err := svc.ExecuteFind(ctx, "visits", requests, data.QueryOptions{})
		if err != nil {
			return "", err
		}
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, fmt.Sprint(row["name"]))
		}
		sort.Strings(names)
		return strings.Join(names, ","), nil
	}
	one := func(field, op, value, valueTo string) data.FindRequest {
		crit := data.FindCriterion{FieldName: field, Operator: op, Value: value}
		if op == "RANGE" {
			crit.ValueTo = valueTo
		}
		return data.FindRequest{Criteria: []data.FindCriterion{crit}}
	}
	// typed is what the client sends for text typed in Find mode
	// (DataBrowserWidget.criteriaFor): a bare value is a LIKE "%value%".
	typed := func(field, text string) data.FindRequest {
		return data.FindRequest{Criteria: []data.FindCriterion{{FieldName: field, Value: text}}}
	}

	cases := []struct {
		name string
		req  data.FindRequest
		want string
	}{
		// A date field.
		{"date, as typed", one("day", "LIKE", "%2026-01-02%", ""), "a"},
		{"date, exact", one("day", "=", "2026-01-02", ""), "a"},
		{"date, strict", one("day", "==", "2026-01-02", ""), "a"},
		{"date, day first", one("day", "=", "31/01/2026", ""), "b"},
		{"date, month first", one("day", "LIKE", "%01/31/2026%", ""), "b"},
		{"date, day and month alike", one("day", "=", "2/2/2026", ""), ""},
		{"date, dotted", one("day", "=", "31.01.2026", ""), "b"},
		{"date, month name", one("day", "=", "2 Jan 2026", ""), "a"},
		{"date, as an instant", one("day", "=", "2026-01-02T00:00:00Z", ""), "a"},
		{"date, a year", one("day", "LIKE", "%2026%", ""), "a,b,c"},
		{"date, a month", one("day", "=", "2026-01", ""), "a,b"},
		{"date, a month day first", one("day", "=", "01/2026", ""), "a,b"},
		{"date, range", one("day", "RANGE", "2026-01-01", "2026-01-31"), "a,b"},
		{"date, range day first", one("day", "RANGE", "1/1/2026", "31/1/2026"), "a,b"},
		{"date, range typed", typed("day", "2026-01-01...2026-01-31"), "a,b"},
		{"date, before", one("day", "<", "2026-01-02", ""), "d"},
		{"date, up to", one("day", "<=", "2026-01-02", ""), "a,d"},
		{"date, after", one("day", ">", "2026-01-31", ""), "c"},
		{"date, from", one("day", ">=", "2026-01-31", ""), "b,c"},
		{"date, typed after", typed("day", ">2026-01-31"), "c"},
		{"date, not", one("day", "!=", "2026-01-02", ""), "b,c,d,e"},
		{"date, empty", one("day", "IS_EMPTY", "", ""), "e"},
		{"date, wildcard", one("day", "LIKE", "2026-01-%", ""), "a,b"},

		// A timestamp field.
		{"timestamp, a day", one("at", "LIKE", "%2026-01-31%", ""), "b"},
		{"timestamp, a day day first", one("at", "=", "31/01/2026", ""), "b"},
		{"timestamp, RFC 3339", one("at", "=", "2026-01-02T03:04:05Z", ""), "a"},
		{"timestamp, with an offset", one("at", "=", "2026-01-02T04:04:05+01:00", ""), "a"},
		{"timestamp, as shown", one("at", "LIKE", "%2026-01-02 03:04:05%", ""), "a"},
		{"timestamp, to the minute", one("at", "=", "2026-01-02 03:04", ""), "a"},
		{"timestamp, range of days", one("at", "RANGE", "2026-01-01", "2026-01-31"), "a,b"},
		{"timestamp, range of instants", one("at", "RANGE", "2026-01-02 03:04:05", "2026-02-01 00:00:00"), "a,b,c"},
		{"timestamp, before a day", one("at", "<", "2026-01-02", ""), "d"},
		{"timestamp, up to a day", one("at", "<=", "2026-01-02", ""), "a,d"},
		{"timestamp, after a day", one("at", ">", "2026-01-31", ""), "c"},
		{"timestamp, from an instant", one("at", ">=", "2026-01-31 23:59:59", ""), "b,c"},
		{"timestamp, not a day", one("at", "!=", "2026-01-02", ""), "b,c,d,e"},
		{"timestamp, calculation", one("computed", "=", "2026-01-02", ""), "a"},

		// A Time field: a time of day on 1 January 0001.
		{"time, a minute", one("clock", "LIKE", "%15:30%", ""), "a,c"},
		{"time, a second", one("clock", "=", "15:30:00", ""), "a"},
		{"time, twelve-hour", one("clock", "=", "3:30 PM", ""), "a,c"},
		{"time, before", one("clock", "<", "12:00", ""), "b"},
		{"time, range", one("clock", "RANGE", "08:00", "15:30"), "a,b,c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := find(tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	// 02/01/2026 is 2 January or 1 February, and nothing says which: it is
	// refused rather than read one way.
	_, err := find(one("day", "=", "02/01/2026", ""))
	require.ErrorIs(t, err, data.ErrInvalidValue)
	require.Contains(t, err.Error(), "2026-01-02")
}

// Updating matching records finds a record by a date or a timestamp whatever
// text the file writes it in, on both engines.
func TestUpdateMatchingOnTemporalFields(t *testing.T) {
	ctx, _, svc, _ := newTemporalSolution(t)
	_, err := svc.InsertRow(ctx, "visits", map[string]interface{}{
		"name": "before", "day": "2026-01-02", "at": "2026-01-02T03:04:05Z",
	})
	require.NoError(t, err)

	update := func(field, text string) *data.ImportReport {
		t.Helper()
		report, err := svc.ImportRecords(ctx, "visits", &dataio.Table{
			Columns: []string{field, "name"},
			Rows:    [][]string{{text, "after " + field + " " + text}},
		}, data.ImportOptions{
			Action:      data.ImportUpdateMatching,
			Mappings:    []data.ImportMapping{{Column: 0, Field: field}, {Column: 1, Field: "name"}},
			MatchFields: []string{field},
		})
		require.NoError(t, err)
		return report
	}

	for _, tc := range []struct{ field, text string }{
		{"at", "2026-01-02T03:04:05Z"},
		{"at", "2026-01-02 03:04:05"},
		{"at", "2026-01-02T04:04:05+01:00"},
		{"day", "2026-01-02"},
	} {
		report := update(tc.field, tc.text)
		require.Equal(t, 1, report.Updated, "%s matched on %q: %+v", tc.field, tc.text, report)
	}
	for _, tc := range []struct{ field, text string }{
		{"at", "2026-01-02 03:04:06"},
		{"day", "2026-01-03"},
	} {
		report := update(tc.field, tc.text)
		require.Equal(t, 0, report.Updated, "%s matched on %q: %+v", tc.field, tc.text, report)
		require.Equal(t, 1, report.Skipped)
	}
}
