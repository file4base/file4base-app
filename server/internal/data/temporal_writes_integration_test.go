package data_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// Dates and timestamps are written as text by every path that writes
// records: RFC 3339 with its "Z" (the import's notation, an auto-enter
// stamp), a time of day on 1 January of year 0 or 1 (what earlier migrations
// and the calculation engine write for a Time field). MariaDB's DATETIME and
// DATE take none of these, and PostgreSQL has no year 0: each value is
// stored, on both engines, as the instant or the day it names.
func TestTemporalTextIsStoredOnEveryEngine(t *testing.T) {
	ctx, schemaSvc, svc, driver := newTemporalSolution(t)
	instant := func(v interface{}) string {
		t.Helper()
		switch at := v.(type) {
		case time.Time:
			return at.UTC().Format("2006-01-02 15:04:05")
		case string:
			parsed, err := time.Parse(time.RFC3339Nano, at)
			if err != nil {
				parsed, err = time.Parse("2006-01-02 15:04:05", at)
			}
			require.NoError(t, err, at)
			return parsed.UTC().Format("2006-01-02 15:04:05")
		}
		t.Fatalf("not a timestamp: %#v", v)
		return ""
	}
	day := func(v interface{}) string {
		t.Helper()
		switch d := v.(type) {
		case time.Time:
			return d.Format("2006-01-02")
		case string:
			return d[:10]
		}
		t.Fatalf("not a date: %#v", v)
		return ""
	}
	read := func(id string) map[string]interface{} {
		t.Helper()
		row, err := svc.GetRow(ctx, "visits", id)
		require.NoError(t, err)
		return row
	}

	// The data API: a record created and changed with RFC 3339 text.
	created, err := svc.InsertRow(ctx, "visits", map[string]interface{}{
		"day": "2026-01-02T00:00:00Z", "at": "2026-01-02T03:04:05Z", "clock": "0000-01-01T15:30:00Z",
	})
	require.NoError(t, err, "a record created with RFC 3339 text")
	id := fmt.Sprint(created["id"])
	row := read(id)
	require.Equal(t, "2026-01-02", day(row["day"]))
	require.Equal(t, "2026-01-02 03:04:05", instant(row["at"]))
	require.Equal(t, "0001-01-01 15:30:00", instant(row["clock"]), "a time of day is on 1 January 0001")
	require.Equal(t, "2026-01-02 03:04:05", instant(row["computed"]))

	_, err = svc.UpdateRow(ctx, "visits", id, map[string]interface{}{"at": "2026-02-03T04:05:06.5+02:00", "clock": "0001-01-01T08:00:00Z"})
	require.NoError(t, err, "a record changed with RFC 3339 text")
	row = read(id)
	require.Equal(t, "2026-02-03 02:05:06", instant(row["at"]), "the instant, in UTC")
	require.Equal(t, "0001-01-01 08:00:00", instant(row["clock"]))
	// A formula reads the instant that is stored, not the text sent.
	require.Equal(t, "2", fmt.Sprint(row["hour"]), "Hour ( at ) of the instant in UTC")

	// Every repetition of a timestamp is kept as the field's own column is.
	_, err = svc.UpdateRow(ctx, "visits", id, map[string]interface{}{
		"stamps[2]": "0000-01-01T15:30:00Z", "stamps[3]": "2026-01-02T04:04:05.25+01:00",
	})
	require.NoError(t, err, "repetitions given as RFC 3339 text")
	row = read(id)
	require.Equal(t, "0001-01-01 15:30:00", instant(row["stamps[2]"]), "a time of day in a repetition")
	require.Equal(t, "2026-01-02 03:04:05", instant(row["stamps[3]"]), "an instant in a repetition, in UTC")

	// Midnight on 1 January 0001, a Time field's midnight, is Go's zero
	// time, which the MariaDB driver would send as 0000-00-00: it is stored
	// as the midnight it is, and found as one.
	midnight, err := svc.InsertRow(ctx, "visits", map[string]interface{}{"name": "midnight", "clock": "0000-01-01T00:00:00Z"})
	require.NoError(t, err, "a Time field's midnight")
	var stored string
	require.NoError(t, driver.DB().QueryRowContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		driver.Dialect().CastToText(driver.Dialect().QuoteIdentifier("clock")), driver.Dialect().QuoteIdentifier("visits"),
		driver.Dialect().QuoteIdentifier("id"), driver.Dialect().Placeholder(1)), midnight["id"]).Scan(&stored))
	require.True(t, strings.HasPrefix(stored, "0001-01-01 00:00:00"), "stored as %q", stored)
	found, err := svc.ExecuteFind(ctx, "visits", []data.FindRequest{{Criteria: []data.FindCriterion{
		{FieldName: "clock", Operator: "=", Value: "00:00"}}}}, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, found, 1, "midnight is found as midnight")
	require.Equal(t, "midnight", fmt.Sprint(found[0]["name"]))

	// An import, whose conversion writes RFC 3339.
	report, err := svc.ImportRecords(ctx, "visits", &dataio.Table{
		Columns: []string{"day", "at"},
		Rows:    [][]string{{"2026-03-04", "2026-03-04 05:06:07"}},
	}, data.ImportOptions{Action: data.ImportAdd, Mappings: []data.ImportMapping{{Column: 0, Field: "day"}, {Column: 1, Field: "at"}}})
	require.NoError(t, err, "an imported timestamp")
	require.Equal(t, 1, report.Added, "%+v", report)

	// A data file, as a migration writes its records: RFC 3339, and a Time
	// field's time of day on 1 January of year 0.
	file, err := msgpack.Marshal(schema.DatabaseDataBundle{Format: schema.DataFormat, Version: schema.DataVersion,
		TablesData: map[string][]map[string]interface{}{"visits": {{
			"id": "restored", "day": "2026-05-06", "at": "2026-05-06T07:08:09Z", "clock": "0000-01-01T23:59:59Z",
		}, {
			// A date the file holds as a time, which msgpack decodes in the
			// server's zone: west of UTC, the evening before.
			"id": "restored-day", "day": time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
		}}}})
	require.NoError(t, err)
	local := time.Local
	time.Local = time.FixedZone("UTC-5", -5*3600)
	_, err = schemaSvc.ImportDatabaseData(ctx, file)
	time.Local = local
	require.NoError(t, err, "a data file's timestamps")
	require.Equal(t, "2026-05-07", day(read("restored-day")["day"]), "a date decoded in a zone west of UTC")
	row = read("restored")
	require.Equal(t, "2026-05-06", day(row["day"]))
	require.Equal(t, "2026-05-06 07:08:09", instant(row["at"]))
	require.Equal(t, "0001-01-01 23:59:59", instant(row["clock"]))

	// A value that names no date or instant is still refused, not stored
	// as something else.
	_, err = svc.InsertRow(ctx, "visits", map[string]interface{}{"at": "next Tuesday"})
	require.Error(t, err)
}

// newTemporalSolution makes a solution in a database of its own, dropped
// when the test ends, with a table of every temporal kind: a date, a
// timestamp, a Time field (a timestamp on 1 January 0001), a repeating
// timestamp, a calculation whose result is a timestamp and one reading a
// timestamp, beside a text field naming each record.
func newTemporalSolution(t *testing.T) (context.Context, *schema.Service, *data.Service, dbal.DatabaseDriver) {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		testdb.Unavailable(t, "database not available:", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
	}
	name := fmt.Sprintf("f4b_temporal_%d", uniqueTestStamp())
	require.NoError(t, mgr.CreateDatabase(ctx, name))
	t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
	driver, err := mgr.DriverFor(ctx, name)
	require.NoError(t, err)
	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))

	tbl, err := schemaSvc.CreateTable(ctx, "visits", "Visits")
	require.NoError(t, err)
	add := func(col schema.ColumnMetadata) {
		col.DisplayName, col.IsNullable = col.Name, true
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, col)
		require.NoError(t, err)
	}
	formula := `{"formula":"If ( IsEmpty ( day ) ; \"\" ; Timestamp ( day ; Time ( 3 ; 4 ; 5 ) ) )","result_type":"Timestamp"}`
	add(schema.ColumnMetadata{Name: "day", FieldType: dbal.FieldTypeDate})
	add(schema.ColumnMetadata{Name: "at", FieldType: dbal.FieldTypeTimestamp})
	add(schema.ColumnMetadata{Name: "clock", FieldType: dbal.FieldTypeTimestamp})
	add(schema.ColumnMetadata{Name: "computed", FieldType: dbal.FieldTypeCalculation, CalculationFormula: &formula})
	hour := `{"formula":"If ( IsEmpty ( at ) ; \"\" ; Hour ( at ) )","result_type":"Number"}`
	add(schema.ColumnMetadata{Name: "hour", FieldType: dbal.FieldTypeCalculation, CalculationFormula: &hour})
	add(schema.ColumnMetadata{Name: "stamps", FieldType: dbal.FieldTypeTimestamp, MaxRepetitions: 3})
	add(schema.ColumnMetadata{Name: "name", FieldType: dbal.FieldTypeText})
	return ctx, schemaSvc, data.NewService(driver), driver
}
