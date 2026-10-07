package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A layout's portals and related fields name a relationship by id, so they
// have to survive Save and Open with that id still pointing at a relationship
// that is really there (#36).
func TestPortalsAndRelatedFieldsRoundTripThroughASolutionFile(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := time.Now().UnixNano() % 1000000
	companies, err := svc.CreateTable(ctx, "Portal Companies", fmt.Sprintf("portal_companies_%d", stamp))
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(context.Background(), companies.ID) }()

	customers, err := svc.CreateTable(ctx, "Portal Customers", fmt.Sprintf("portal_customers_%d", stamp))
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(context.Background(), customers.ID) }()

	left, err := svc.AddColumn(ctx, companies.ID, schema.ColumnMetadata{
		Name: "company", DisplayName: "Company", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)
	right, err := svc.AddColumn(ctx, customers.ID, schema.ColumnMetadata{
		Name: "company", DisplayName: "Company", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	occurrences, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	var companiesOcc, customersOcc string
	for _, o := range occurrences {
		switch o.BaseTableID {
		case companies.ID:
			companiesOcc = o.ID
		case customers.ID:
			customersOcc = o.ID
		}
	}
	require.NotEmpty(t, companiesOcc)
	require.NotEmpty(t, customersOcc)

	rel, err := svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:             fmt.Sprintf("portal_rel_%d", stamp),
		LeftOccurrenceID: companiesOcc, LeftColumnID: left.ID,
		RightOccurrenceID: customersOcc, RightColumnID: right.ID,
		Operator: "=", AllowCreation: true,
	})
	require.NoError(t, err)
	defer func() { _ = svc.DeleteRelationship(context.Background(), rel.ID) }()

	definition := map[string]interface{}{
		"id":               "layout-portal",
		"name":             fmt.Sprintf("Portal Layout %d", stamp),
		"table_occurrence": companies.Name,
		"objects": []interface{}{
			map[string]interface{}{
				"id": "portal_1", "type": "portal", "x": 40, "y": 160, "width": 400, "height": 150,
				"portal_config": map[string]interface{}{
					"relationship_id": rel.ID,
					"occurrence":      "Portal Customers",
					"row_count":       5,
					"allow_creation":  true,
				},
			},
			map[string]interface{}{
				"id": "fld_related", "type": "field", "x": 40, "y": 90, "width": 200, "height": 30,
				"field_binding": map[string]interface{}{
					"relationship_id":  rel.ID,
					"table_occurrence": "Portal Customers",
					"field_name":       "company",
				},
			},
		},
	}
	raw, err := json.Marshal(definition)
	require.NoError(t, err)

	layout, err := svc.CreateLayout(ctx, fmt.Sprintf("Portal Layout %d", stamp), companiesOcc, raw)
	require.NoError(t, err)
	defer func() { _ = svc.DeleteLayout(context.Background(), layout.ID) }()

	file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Portals"})
	require.NoError(t, err)

	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)

	var exported map[string]interface{}
	for _, l := range bundle.Layouts {
		if l.ID == layout.ID {
			exported = l.Definition
		}
	}
	require.NotNil(t, exported, "the layout is in the file")
	objects := exported["objects"].([]interface{})
	portalConfig := objects[0].(map[string]interface{})["portal_config"].(map[string]interface{})
	assert.Equal(t, rel.ID, portalConfig["relationship_id"], "the portal's relationship is in the file")
	assert.Equal(t, true, portalConfig["allow_creation"])

	// Importing the file back matches the relationship it already has, so the
	// portal still points at a relationship that is really there.
	_, err = svc.ImportSolution(ctx, file)
	require.NoError(t, err)

	reloaded, err := svc.GetLayout(ctx, layout.ID)
	require.NoError(t, err)

	var back map[string]interface{}
	require.NoError(t, json.Unmarshal(reloaded.Definition, &back))
	backObjects := back["objects"].([]interface{})
	backPortal := backObjects[0].(map[string]interface{})["portal_config"].(map[string]interface{})
	relID, _ := backPortal["relationship_id"].(string)
	require.NotEmpty(t, relID)

	relationships, err := svc.ListRelationships(ctx)
	require.NoError(t, err)
	var found bool
	for _, r := range relationships {
		if r.ID == relID {
			found = true
		}
	}
	assert.True(t, found, "the imported portal points at a relationship in this database")

	backBinding := backObjects[1].(map[string]interface{})["field_binding"].(map[string]interface{})
	assert.Equal(t, relID, backBinding["relationship_id"],
		"the related field followed the same relationship")
	assert.Equal(t, "Portal Customers", backBinding["table_occurrence"],
		"the occurrence name is not an id and is left as it is")
}
