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

// The auto-enter options travel in a solution file as they are, the live
// next serial value with them, and come back on import.
func TestAutoEnterOptionsRoundTripThroughASolutionFile(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	name := fmt.Sprintf("ae_bundle_%d", uniqueTestStamp())
	tbl, err := svc.CreateTable(ctx, "AE Bundle", name)
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(context.Background(), tbl.ID) }()

	options := map[string]any{
		"serial_enabled": true, "serial_next": "T-0007", "serial_increment": 3, "serial_generate": "commit",
		"calculated_enabled": true, "calculated_formula": `"x" & ref`, "do_not_replace": true, "calculated_skip_if_empty": false,
		"looked_up_enabled": true, "looked_up_occurrence": "Elsewhere", "looked_up_field": "code",
		"looked_up_no_match": "use_constant", "looked_up_no_match_value": "none", "looked_up_dont_copy_if_empty": true,
		"prohibit_modification": true, "indexing": "minimal",
	}
	raw, err := json.Marshal(options)
	require.NoError(t, err)
	stored := string(raw)
	col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "ref", DisplayName: "Ref", FieldType: dbal.FieldTypeText, IsNullable: true, DefaultValue: &stored,
	})
	require.NoError(t, err)

	file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "AE Bundle"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	var exported *string
	for _, table := range bundle.Tables {
		if table.Name != name {
			continue
		}
		for _, c := range table.Columns {
			if c.Name == "ref" {
				exported = c.DefaultValue
			}
		}
	}
	require.NotNil(t, exported, "the options are in the file")
	assert.JSONEq(t, stored, *exported)

	// Turned off here, then the file brings them back.
	_, err = svc.UpdateColumn(ctx, tbl.ID, col.ID, schema.UpdateColumnOptions{DisplayName: "Ref", DefaultValue: nil, UpdateDefaultValue: true})
	require.NoError(t, err)
	_, err = svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	var back *string
	for _, table := range tables {
		for _, c := range table.Columns {
			if table.Name == name && c.Name == "ref" {
				back = c.DefaultValue
			}
		}
	}
	require.NotNil(t, back)
	assert.JSONEq(t, stored, *back)
}
