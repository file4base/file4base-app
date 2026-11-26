package data_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A relationship whose match fields are dates relates the records of the
// same day, and cascades to them only. The key is read back from the source
// record: on SQLite the driver reads a DATE column as a time.Time, which was
// bound back as a timestamp text no DATE column equals, so a portal showed
// nothing and a ≠ cascade deleted the records of the same day.
//
// A timestamp key read from the record and compared with a date field is
// its day, as PostgreSQL binds it (the parameter takes the column's type).
func TestRelationshipOnADateMatchField(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	for _, tc := range []struct {
		name, op   string
		parentType dbal.AgnosticFieldType
		parent     interface{}
		related    []string
	}{
		{"date =", "=", dbal.FieldTypeDate, "2026-01-02", []string{"Same day"}},
		{"date ≠", "≠", dbal.FieldTypeDate, "2026-01-02", []string{"Next day"}},
		{"timestamp past midnight =", "=", dbal.FieldTypeTimestamp, "2026-01-02T12:00:00Z", []string{"Same day"}},
		{"timestamp past midnight <", "<", dbal.FieldTypeTimestamp, "2026-01-02T12:00:00Z", []string{"Next day"}},
	} {
		op := tc.op
		t.Run(tc.name, func(t *testing.T) {
			if tc.parentType == dbal.FieldTypeTimestamp && op == "=" && testdb.Engine() == dbal.EngineMariaDB {
				t.Skip("MariaDB compares the date as a DATETIME at midnight, which a timestamp past midnight does not equal")
			}
			schemaSvc := schema.NewService(driver)
			require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
			dataSvc := data.NewService(driver)
			stamp := uniqueTestStamp()
			days := fmt.Sprintf("date_days_%d", stamp)
			events := fmt.Sprintf("date_events_%d", stamp)

			daysTable, err := schemaSvc.CreateTable(ctx, "Date Days", days)
			require.NoError(t, err)
			t.Cleanup(func() { _ = schemaSvc.DeleteTable(context.Background(), daysTable.ID) })
			eventsTable, err := schemaSvc.CreateTable(ctx, "Date Events", events)
			require.NoError(t, err)
			t.Cleanup(func() { _ = schemaSvc.DeleteTable(context.Background(), eventsTable.ID) })

			dayOnDays, err := schemaSvc.AddColumn(ctx, daysTable.ID, schema.ColumnMetadata{
				Name: "day", DisplayName: "Day", FieldType: tc.parentType, IsNullable: true,
			})
			require.NoError(t, err)
			dayOnEvents, err := schemaSvc.AddColumn(ctx, eventsTable.ID, schema.ColumnMetadata{
				Name: "day", DisplayName: "Day", FieldType: dbal.FieldTypeDate, IsNullable: true,
			})
			require.NoError(t, err)
			_, err = schemaSvc.AddColumn(ctx, eventsTable.ID, schema.ColumnMetadata{
				Name: "last_name", DisplayName: "Title", FieldType: dbal.FieldTypeText, IsNullable: true,
			})
			require.NoError(t, err)

			occurrences, err := schemaSvc.ListTableOccurrences(ctx)
			require.NoError(t, err)
			var daysOcc, eventsOcc string
			for _, o := range occurrences {
				switch o.BaseTableID {
				case daysTable.ID:
					daysOcc = o.ID
				case eventsTable.ID:
					eventsOcc = o.ID
				}
			}
			rel, err := schemaSvc.CreateRelationship(ctx, schema.CreateRelationshipInput{
				Name:              fmt.Sprintf("days_events_%d", stamp),
				LeftOccurrenceID:  daysOcc,
				LeftColumnID:      dayOnDays.ID,
				RightOccurrenceID: eventsOcc,
				RightColumnID:     dayOnEvents.ID,
				Operator:          op,
				CascadeDelete:     true,
			})
			require.NoError(t, err)
			t.Cleanup(func() { _ = schemaSvc.DeleteRelationship(context.Background(), rel.ID) })

			day, err := dataSvc.InsertRow(ctx, days, map[string]interface{}{"day": tc.parent})
			require.NoError(t, err)
			for _, e := range []struct{ day, title string }{
				{"2026-01-02", "Same day"},
				{"2026-01-03", "Next day"},
			} {
				_, err := dataSvc.InsertRow(ctx, events, map[string]interface{}{"day": e.day, "last_name": e.title})
				require.NoError(t, err)
			}
			stays := []string{}
			for _, title := range []string{"Same day", "Next day"} {
				if !slices.Contains(tc.related, title) {
					stays = append(stays, title)
				}
			}

			rows, err := dataSvc.RelatedRows(ctx, days, day["id"].(string), rel.ID, eventsOcc, data.QueryOptions{})
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.related, lastNames(rows), "the portal shows the records the dates relate")

			require.NoError(t, dataSvc.DeleteRow(ctx, days, day["id"].(string)))
			remaining, err := dataSvc.ListRows(ctx, events, data.QueryOptions{})
			require.NoError(t, err)
			assert.ElementsMatch(t, stays, lastNames(remaining), "the cascade deleted the related records only")
		})
	}
}
