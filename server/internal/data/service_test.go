package data_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
}

func TestDataService_CRUDAndFindMode(t *testing.T) {
	dsn := "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: dbal.EnginePostgres,
		DSN:        dsn,
	})
	if err != nil {
		t.Skip("PostgreSQL not accessible:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed:", err)
		return
	}

	schemaSvc := schema.NewService(driver)
	dataSvc := data.NewService(driver)

	// Create contacts table
	tblName := fmt.Sprintf("contacts_test_%d", time.Now().UnixNano()%1000000)
	tbl, err := schemaSvc.CreateTable(ctx, "Contacts Test", tblName)
	require.NoError(t, err)

	_, err = schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name:        "first_name",
		DisplayName: "First Name",
		FieldType:   dbal.FieldTypeText,
		IsNullable:  false,
	})
	require.NoError(t, err)

	_, err = schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name:        "age",
		DisplayName: "Age",
		FieldType:   dbal.FieldTypeNumber,
		IsNullable:  true,
	})
	require.NoError(t, err)

	// 1. Insert rows
	r1, err := dataSvc.InsertRow(ctx, tblName, map[string]interface{}{
		"first_name": "Mario",
		"age":        35,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, r1["id"])

	r2, err := dataSvc.InsertRow(ctx, tblName, map[string]interface{}{
		"first_name": "Luigi",
		"age":        32,
	})
	require.NoError(t, err)

	// 2. Get row
	fetched, err := dataSvc.GetRow(ctx, tblName, r1["id"].(string))
	require.NoError(t, err)
	assert.Equal(t, "Mario", fetched["first_name"])

	// 3. Update row
	updated, err := dataSvc.UpdateRow(ctx, tblName, r1["id"].(string), map[string]interface{}{
		"age": 36,
	})
	require.NoError(t, err)
	assert.Equal(t, "36", fmtValue(updated["age"]))

	// 4. Find Mode: range search (30...40)
	critRange := data.ParseFileMakerFindCriteria("age", "30...40")
	findRes, err := dataSvc.ExecuteFind(ctx, tblName, []data.FindRequest{
		{Criteria: []data.FindCriterion{critRange}},
	}, data.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, findRes, 2)

	// 5. Find Mode: wildcard name search (Mar*)
	critWildcard := data.ParseFileMakerFindCriteria("first_name", "Mar*")
	findWildcard, err := dataSvc.ExecuteFind(ctx, tblName, []data.FindRequest{
		{Criteria: []data.FindCriterion{critWildcard}},
	}, data.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, findWildcard, 1)
	assert.Equal(t, "Mario", findWildcard[0]["first_name"])

	// 6. Delete row
	err = dataSvc.DeleteRow(ctx, tblName, r2["id"].(string))
	require.NoError(t, err)

	listAfterDelete, err := dataSvc.ListRows(ctx, tblName, data.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, listAfterDelete, 1)
}

func fmtValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}
