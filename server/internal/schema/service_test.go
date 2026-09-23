package schema_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
}

func TestSchemaService_LivePostgres(t *testing.T) {
	dsn := "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: dbal.EnginePostgres,
		DSN:        dsn,
	})
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping integration test:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed, skipping integration test:", err)
		return
	}

	svc := schema.NewService(driver)

	// 1. Ensure system tables
	err = svc.EnsureSystemTables(ctx)
	require.NoError(t, err)

	// 2. Create dynamic table
	tableName := fmt.Sprintf("invoices_test_%d", time.Now().UnixNano()%1000000)
	tbl, err := svc.CreateTable(ctx, "Customer Invoices", tableName)
	require.NoError(t, err)
	assert.NotEmpty(t, tbl.ID)
	assert.Equal(t, tableName, tbl.Name)
	assert.Equal(t, "Customer Invoices", tbl.DisplayName)
	assert.Len(t, tbl.Columns, 1) // id column

	// 3. Add column to table
	col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name:        "total_amount",
		DisplayName: "Total Amount",
		FieldType:   dbal.FieldTypeNumber,
		IsNullable:  true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, col.ID)
	assert.Equal(t, "total_amount", col.Name)

	// 4. List tables and verify
	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	var found bool
	for _, item := range tables {
		if item.ID == tbl.ID {
			found = true
			assert.Equal(t, "Customer Invoices", item.DisplayName)
			assert.Len(t, item.Columns, 2) // id + total_amount
		}
	}
	assert.True(t, found, "Newly created table should be present in ListTables")
}
