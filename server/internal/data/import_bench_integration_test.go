package data_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/require"
)

// TestImportTenThousandRowsTiming times an import of 10,000 rows, three times
// into a table without validation, then three times into one whose surname is
// validated (unique, not empty, always), which costs a lookup per row. It is a measurement, not a check, so it runs
// only when F4B_IMPORT_TIMING is set:
//
//	F4B_IMPORT_TIMING=1 go test -run TestImportTenThousandRowsTiming -v ./internal/data/
func TestImportTenThousandRowsTiming(t *testing.T) {
	if os.Getenv("F4B_IMPORT_TIMING") == "" {
		t.Skip("a timing, run with F4B_IMPORT_TIMING=1")
	}
	driver, _ := relatedTestDriver(t)
	if driver == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	const rows = 10000
	var csv strings.Builder
	csv.WriteString("Surname,Town,Paid\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&csv, "Name%05d,Town %d,%d.50\n", i, i%50, i%300)
	}
	source := parseCSV(t, csv.String())

	for run := 1; run <= 6; run++ {
		f := newImportFixture(t, ctx, driver)
		validated := run > 3
		if validated {
			tableID, columnID := importColumnID(t, ctx, f, "last_name")
			rules := `{"not_empty":true,"unique":true,"not_empty_timing":"always"}`
			_, err := f.schemaSvc.UpdateColumn(ctx, tableID, columnID, schema.UpdateColumnOptions{
				DisplayName: "Last Name", ValidationRules: &rules, UpdateValidation: true,
			})
			require.NoError(t, err)
		}

		started := time.Now()
		report, err := f.dataSvc.ImportRecords(ctx, f.table, source, data.ImportOptions{
			Action: data.ImportAdd,
			Mappings: []data.ImportMapping{
				{Column: 0, Field: "last_name"}, {Column: 1, Field: "city"}, {Column: 2, Field: "fee_paid"},
			},
		})
		elapsed := time.Since(started)
		require.NoError(t, err)
		require.Equal(t, rows, report.Added)
		t.Logf("run %d (validated %v): %d rows in %s (%.0f rows/s)", run, validated, rows,
			elapsed.Round(time.Millisecond), float64(rows)/elapsed.Seconds())
	}
}

// importColumnID answers the table and column ids of a field of the fixture.
func importColumnID(t *testing.T, ctx context.Context, f *importFixture, field string) (string, string) {
	t.Helper()
	tables, err := f.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	for _, table := range tables {
		if table.Name != f.table {
			continue
		}
		for _, c := range table.Columns {
			if c.Name == field {
				return table.ID, c.ID
			}
		}
	}
	t.Fatalf("%s has no field %s", f.table, field)
	return "", ""
}
