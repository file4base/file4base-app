package migration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// A migrated design whose calculations read related records applies — their
// fields are added before the relationships they read through — and reads
// back: a sum over the invoice's items, and a label two relationships away.
func TestRelatedCalculationsApply(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		testdb.Unavailable(t, "database not available:", err)
	}
	// A cleanup, not a defer: the tables are dropped by cleanups, which run
	// after the deferred calls.
	t.Cleanup(func() { _ = driver.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
	}

	stamp := time.Now().UnixNano()
	suffix := fmt.Sprintf(" %d", stamp)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: relatedCalculationsDesign(suffix)})
	require.NoError(t, err)
	// The graph positions a design brings are its own; spread them so they
	// cannot meet another test's occurrences in the shared database.
	for i := range res.Bundle.TableOccurrences {
		res.Bundle.TableOccurrences[i].XPos = float64(100_000 + stamp%1_000_000)
		res.Bundle.TableOccurrences[i].YPos = float64(100_000 + i*200)
	}
	file, err := msgpack.Marshal(res.Bundle)
	require.NoError(t, err)

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	_, err = schemaSvc.ImportSolution(ctx, file)
	require.NoError(t, err)
	tables, err := schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	name := map[string]string{}
	for _, tbl := range res.Bundle.Tables {
		name[tbl.DisplayName] = tbl.Name
		for _, created := range tables {
			if created.Name == tbl.Name {
				id := created.ID
				t.Cleanup(func() {
					if err := schemaSvc.DeleteTable(context.Background(), id); err != nil {
						t.Errorf("dropping %s: %v", tbl.Name, err)
					}
				})
			}
		}
	}
	invoices, items, products := name["Invoices"+suffix], name["Invoice Items"+suffix], name["Products"+suffix]
	require.NotEmpty(t, invoices)

	svc := data.NewService(driver)
	insert := func(table string, record map[string]interface{}) map[string]interface{} {
		row, err := svc.InsertRow(ctx, table, record)
		require.NoError(t, err)
		return row
	}
	invoice := insert(invoices, map[string]interface{}{"invoice_id": "INV-1", "discount": 5})
	require.Equal(t, "10", fmt.Sprint(invoice["discount_twice"]), "a calculation of its own record is still stored")
	insert(products, map[string]interface{}{"product_id": "P-1", "label": "Synthetic widget"})
	insert(items, map[string]interface{}{"invoice_id": "INV-1", "product_id": "P-1", "line_amount": 30})
	insert(items, map[string]interface{}{"invoice_id": "INV-1", "product_id": "P-2", "line_amount": 12})

	row, err := svc.GetRow(ctx, invoices, fmt.Sprint(invoice["id"]))
	require.NoError(t, err)
	require.Equal(t, "42", fmt.Sprint(row["items_total"]))
	require.Equal(t, "37", fmt.Sprint(row["net"]), "a calculation reading an unstored one is computed too")
	require.Equal(t, "Synthetic widget", row["first_label"], "a field two relationships away is read")
}
