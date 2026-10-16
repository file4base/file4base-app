package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newScriptTestService connects to the scratch database and returns a ready
// Service, skipping the test when the engine is not reachable.
func newScriptTestService(t *testing.T) (*schema.Service, func()) {
	t.Helper()
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
		DSN:        testdb.DevDSN(),
	})
	if err != nil {
		t.Skip("PostgreSQL not available:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		_ = driver.Close()
		t.Skip("PostgreSQL ping failed:", err)
	}
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	return svc, func() { _ = driver.Close() }
}

// TestScriptService_UpdateRollsBackOnFailingStep verifies that a step which
// fails to persist rolls back the whole UpdateScript, leaving the previously
// stored script (name, active flag and steps) untouched rather than wiped out
// and half-rewritten. It relies on PostgreSQL rejecting an over-length
// step_type (VARCHAR(64)), so it is skipped on other engines.
func TestScriptService_UpdateRollsBackOnFailingStep(t *testing.T) {
	if testdb.Engine() != dbal.EnginePostgres {
		t.Skip("rollback test relies on PostgreSQL VARCHAR length enforcement")
	}
	svc, closeFn := newScriptTestService(t)
	defer closeFn()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	name := fmt.Sprintf("rollback_script_%d", time.Now().UnixNano()%1000000)
	original := []schema.ScriptStepMetadata{
		{SequenceIdx: 1, StepType: "go_to_layout", Params: json.RawMessage(`{"layout_name":"A"}`), IsEnabled: true},
		{SequenceIdx: 2, StepType: "set_variable", Params: json.RawMessage(`{"variable":"$x"}`), IsEnabled: true},
	}
	created, err := svc.CreateScript(ctx, name, "invoices", nil, true, original)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteScript(context.Background(), created.ID) })
	require.Len(t, created.Steps, 2)

	// An over-length step_type (> VARCHAR(64)) makes the second step's INSERT
	// fail after the UPDATE, the DELETE and the first new step have been staged.
	badUpdate := []schema.ScriptStepMetadata{
		{SequenceIdx: 1, StepType: "valid_step", Params: json.RawMessage(`{}`), IsEnabled: true},
		{SequenceIdx: 2, StepType: strings.Repeat("x", 100), Params: json.RawMessage(`{}`), IsEnabled: true},
	}
	_, err = svc.UpdateScript(ctx, created.ID, name+"_broken", "contacts", nil, false, badUpdate)
	require.Error(t, err, "UpdateScript should fail on the over-length step")

	// Nothing of the failed update may have landed: the script keeps its
	// original name, active flag and both original steps.
	after, err := svc.GetScript(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, name, after.Name, "name must be unchanged after rollback")
	assert.Equal(t, "invoices", after.ContextTable, "context_table must be unchanged after rollback")
	assert.True(t, after.IsActive, "is_active must be unchanged after rollback")
	require.Len(t, after.Steps, 2, "original steps must survive the rolled-back update")
	assert.Equal(t, "go_to_layout", after.Steps[0].StepType)
	assert.Equal(t, "set_variable", after.Steps[1].StepType)
}

// TestScriptService_CreateRollsBackOnFailingStep verifies that a failing step
// during CreateScript rolls back the script row too, so no half-created script
// is left behind.
func TestScriptService_CreateRollsBackOnFailingStep(t *testing.T) {
	if testdb.Engine() != dbal.EnginePostgres {
		t.Skip("rollback test relies on PostgreSQL VARCHAR length enforcement")
	}
	svc, closeFn := newScriptTestService(t)
	defer closeFn()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	name := fmt.Sprintf("create_rollback_%d", time.Now().UnixNano()%1000000)
	steps := []schema.ScriptStepMetadata{
		{SequenceIdx: 1, StepType: "valid_step", Params: json.RawMessage(`{}`), IsEnabled: true},
		{SequenceIdx: 2, StepType: strings.Repeat("y", 100), Params: json.RawMessage(`{}`), IsEnabled: true},
	}
	_, err := svc.CreateScript(ctx, name, "invoices", nil, true, steps)
	require.Error(t, err, "CreateScript should fail on the over-length step")

	// The script row must not survive the rolled-back create.
	list, err := svc.ListScripts(ctx)
	require.NoError(t, err)
	for _, s := range list {
		assert.NotEqual(t, name, s.Name, "a rolled-back create must leave no script row")
	}
}

func TestScriptService_CRUD(t *testing.T) {
	dsn := testdb.DevDSN()
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
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
