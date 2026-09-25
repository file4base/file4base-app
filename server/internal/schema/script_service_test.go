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

func TestScriptService_CRUD(t *testing.T) {
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

	// 1. Create Script with Steps
	scriptName := fmt.Sprintf("test_script_%d", time.Now().UnixNano()%1000000)
	steps := []schema.ScriptStepMetadata{
		{
			SequenceIdx: 1,
			StepType:    "go_to_layout",
			Params:      json.RawMessage(`{"layout_name":"Invoices_Detail"}`),
			IsEnabled:   true,
		},
		{
			SequenceIdx: 2,
			StepType:    "set_variable",
			Params:      json.RawMessage(`{"variable":"$subtotal","calc":"Sum(Items.price)"}`),
			IsEnabled:   true,
		},
		{
			SequenceIdx: 3,
			StepType:    "if",
			Params:      json.RawMessage(`{"condition":"Invoices::Total > 5000"}`),
			IsEnabled:   true,
		},
	}

	created, err := svc.CreateScript(ctx, scriptName, "invoices", nil, true, steps)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, scriptName, created.Name)
	assert.Equal(t, "invoices", created.ContextTable)
	assert.True(t, created.IsActive)
	assert.Len(t, created.Steps, 3)

	// 2. Get Script
	fetched, err := svc.GetScript(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, scriptName, fetched.Name)
	assert.Len(t, fetched.Steps, 3)
	assert.Equal(t, "go_to_layout", fetched.Steps[0].StepType)
	assert.Equal(t, "set_variable", fetched.Steps[1].StepType)
	assert.Equal(t, "if", fetched.Steps[2].StepType)

	// 3. List Scripts
	list, err := svc.ListScripts(ctx)
	require.NoError(t, err)
	found := false
	for _, s := range list {
		if s.ID == created.ID {
			found = true
			assert.Len(t, s.Steps, 3)
			break
		}
	}
	assert.True(t, found, "created script should be in listed scripts")

	// 4. Update Script
	updatedSteps := append(steps, schema.ScriptStepMetadata{
		SequenceIdx: 4,
		StepType:    "show_dialog",
		Params:      json.RawMessage(`{"title":"Notice","message":"Completed"}`),
		IsEnabled:   true,
	})
	updatedName := scriptName + "_updated"
	updated, err := svc.UpdateScript(ctx, created.ID, updatedName, "invoices", nil, false, updatedSteps)
	require.NoError(t, err)
	assert.Equal(t, updatedName, updated.Name)
	assert.False(t, updated.IsActive)
	assert.Len(t, updated.Steps, 4)

	// 5. Duplicate Script
	duplicated, err := svc.DuplicateScript(ctx, created.ID)
	require.NoError(t, err)
	assert.NotEqual(t, created.ID, duplicated.ID)
	assert.Equal(t, updatedName+"_copia", duplicated.Name)
	assert.Len(t, duplicated.Steps, 4)

	// 6. Delete Scripts
	require.NoError(t, svc.DeleteScript(ctx, created.ID))
	require.NoError(t, svc.DeleteScript(ctx, duplicated.ID))

	_, errNotFound := svc.GetScript(ctx, created.ID)
	assert.Error(t, errNotFound)
}
