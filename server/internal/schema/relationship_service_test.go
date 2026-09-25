package schema_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelationshipService_CRUD(t *testing.T) {
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

	// 1. Create two tables with columns
	ts := time.Now().UnixNano() % 1000000
	t1Name := fmt.Sprintf("rel_t1_%d", ts)
	t2Name := fmt.Sprintf("rel_t2_%d", ts)

	tbl1, err := svc.CreateTable(ctx, "Customers", t1Name)
	require.NoError(t, err)
	tbl2, err := svc.CreateTable(ctx, "Invoices", t2Name)
	require.NoError(t, err)

	col1, err := svc.AddColumn(ctx, tbl1.ID, schema.ColumnMetadata{
		Name:        "customer_id",
		DisplayName: "Customer ID",
		FieldType:   dbal.FieldTypeText,
		IsNullable:  false,
	})
	require.NoError(t, err)

	col2, err := svc.AddColumn(ctx, tbl2.ID, schema.ColumnMetadata{
		Name:        "kf_customer_id",
		DisplayName: "FK Customer ID",
		FieldType:   dbal.FieldTypeText,
		IsNullable:  false,
	})
	require.NoError(t, err)

	// 2. Test Table Occurrences
	// List default occurrences
	occs, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	var to1, to2 *schema.TableOccurrence
	for i := range occs {
		if occs[i].BaseTableID == tbl1.ID {
			to1 = &occs[i]
		}
		if occs[i].BaseTableID == tbl2.ID {
			to2 = &occs[i]
		}
	}
	require.NotNil(t, to1)
	require.NotNil(t, to2)

	// Create custom occurrence (e.g. Customers 2)
	toCustomName := fmt.Sprintf("Customers_2_%d", ts)
	toCustom, err := svc.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		BaseTableID: tbl1.ID,
		Name:        toCustomName,
		XPos:        250.0,
		YPos:        180.0,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, toCustom.ID)
	assert.Equal(t, toCustomName, toCustom.Name)
	assert.Equal(t, 250.0, toCustom.XPos)
	assert.Equal(t, 180.0, toCustom.YPos)

	// Update occurrence position
	newX := 320.0
	newY := 210.0
	renamed := fmt.Sprintf("Customers_Renamed_%d", ts)
	updatedTO, err := svc.UpdateTableOccurrence(ctx, toCustom.ID, schema.UpdateOccurrenceInput{
		Name: &renamed,
		XPos: &newX,
		YPos: &newY,
	})
	require.NoError(t, err)
	assert.Equal(t, renamed, updatedTO.Name)
	assert.Equal(t, 320.0, updatedTO.XPos)

	// 3. Test Relationships CRUD
	relName := fmt.Sprintf("Customers_to_Invoices_%d", ts)
	rel, err := svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:              relName,
		LeftOccurrenceID:  to1.ID,
		LeftColumnID:      col1.ID,
		RightOccurrenceID: to2.ID,
		RightColumnID:     col2.ID,
		Operator:          "=",
		AllowCreation:     true,
		CascadeDelete:     false,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, rel.ID)
	assert.Equal(t, relName, rel.Name)
	assert.Equal(t, "=", rel.Operator)
	assert.True(t, rel.AllowCreation)
	assert.False(t, rel.CascadeDelete)

	// List Relationships
	rels, err := svc.ListRelationships(ctx)
	require.NoError(t, err)
	var foundRel *schema.RelationshipMetadata
	for i := range rels {
		if rels[i].ID == rel.ID {
			foundRel = &rels[i]
			break
		}
	}
	require.NotNil(t, foundRel)
	assert.Equal(t, relName, foundRel.Name)

	// Update Relationship
	newOp := "≠"
	newCascade := true
	updatedRel, err := svc.UpdateRelationship(ctx, rel.ID, schema.UpdateRelationshipInput{
		Operator:      &newOp,
		CascadeDelete: &newCascade,
	})
	require.NoError(t, err)
	assert.Equal(t, "≠", updatedRel.Operator)
	assert.True(t, updatedRel.CascadeDelete)

	// Delete Relationship
	err = svc.DeleteRelationship(ctx, rel.ID)
	require.NoError(t, err)

	// Delete custom occurrence
	err = svc.DeleteTableOccurrence(ctx, toCustom.ID)
	require.NoError(t, err)
}
