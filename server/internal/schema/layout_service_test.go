package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayoutService_CRUD(t *testing.T) {
	dsn := "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: dbal.EnginePostgres,
		DSN:        dsn,
	})
	if err != nil {
		t.Skip("PostgreSQL not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	// Create a table to ensure we have an occurrence
	tblName := fmt.Sprintf("layouts_test_tbl_%d", time.Now().UnixNano()%1000000)
	tbl, err := svc.CreateTable(ctx, "Layouts Test Table", tblName)
	require.NoError(t, err)

	occurrences, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, occurrences)

	var targetTO string
	for _, o := range occurrences {
		if o.BaseTableID == tbl.ID {
			targetTO = o.ID
			break
		}
	}
	require.NotEmpty(t, targetTO)

	// 1. Create Layout
	layoutDef := json.RawMessage(`{"theme":"Enlightened","width":1024,"parts":[],"objects":[]}`)
	layout, err := svc.CreateLayout(ctx, "Invoice Main Layout", targetTO, layoutDef)
	require.NoError(t, err)
	assert.NotEmpty(t, layout.ID)
	assert.Equal(t, "Invoice Main Layout", layout.Name)

	// 2. Get Layout
	fetched, err := svc.GetLayout(ctx, layout.ID)
	require.NoError(t, err)
	assert.Equal(t, layout.Name, fetched.Name)

	// 3. Update Layout
	updatedDef := json.RawMessage(`{"theme":"Enlightened Touch","width":1200,"parts":[],"objects":[]}`)
	updated, err := svc.UpdateLayout(ctx, layout.ID, "Invoice Main Layout Renamed", updatedDef)
	require.NoError(t, err)
	assert.Equal(t, "Invoice Main Layout Renamed", updated.Name)

	// 4. List Layouts
	allLayouts, err := svc.ListLayouts(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, allLayouts)

	// 5. Delete Layout
	err = svc.DeleteLayout(ctx, layout.ID)
	require.NoError(t, err)

	_, err = svc.GetLayout(ctx, layout.ID)
	require.Error(t, err)
}
