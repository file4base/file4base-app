package schema_test

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

// A solution file carries its custom functions, msgpack-encoded beside the
// rest of the design.
func TestCustomFunctionsEncodeInASolutionFile(t *testing.T) {
	b := schema.SolutionBundle{Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Shapes",
		CustomFunctions: []schema.BundleCustomFunction{
			{Name: "Area", Parameters: []string{"w", "h"}, Formula: "w * h", Access: schema.CustomFunctionAccessFullAccess},
			{Name: "kOne", Formula: "1"},
		}}
	file, err := msgpack.Marshal(b)
	require.NoError(t, err)
	decoded, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	require.Equal(t, b.CustomFunctions, decoded.CustomFunctions)

	// A file without any has none, and writes none.
	b.CustomFunctions = nil
	file, err = msgpack.Marshal(b)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, msgpack.Unmarshal(file, &raw))
	require.NotContains(t, raw, "custom_functions")
}

// Importing a solution file adds its custom functions before its fields, so
// a calculation calling one is accepted and computed, even when the file
// lists a function before the one it calls; exporting writes them back. A
// function the solution already has keeps its definition, and a file with a
// function that does not read is refused, with nothing of it left behind.
func TestCustomFunctionsRoundTripThroughASolutionFile(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		testdb.Unavailable(t, "database not available:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
		return
	}
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	square, area := fmt.Sprintf("Square_%d", stamp), fmt.Sprintf("Area_%d", stamp)
	defer func() {
		_, _ = driver.DB().ExecContext(context.Background(),
			`DELETE FROM `+data.CustomFunctionsTable+` WHERE name LIKE `+driver.Dialect().Placeholder(1), fmt.Sprintf("%%_%d", stamp))
	}()
	tableName := fmt.Sprintf("cf_tiles_%d", stamp)
	formula := fmt.Sprintf(`{"formula":"%s ( side )","result_type":"Number"}`, square)
	bundle := schema.SolutionBundle{
		Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Tiles",
		Tables: []schema.BundleTable{{ID: "t", Name: tableName, DisplayName: "Tiles", Columns: []schema.BundleColumn{
			{ID: "c-id", Name: "id", DisplayName: "ID", FieldType: "TEXT", IsPrimaryKey: true},
			{ID: "c-side", Name: "side", DisplayName: "Side", FieldType: "NUMBER", IsNullable: true},
			{ID: "c-surface", Name: "surface", DisplayName: "Surface", FieldType: "CALCULATION", IsNullable: true, CalculationFormula: &formula},
		}}},
		CustomFunctions: []schema.BundleCustomFunction{
			{Name: square, Parameters: []string{"s"}, Formula: area + " ( s ; s )"},
			{Name: area, Parameters: []string{"w", "h"}, Formula: "w * h", Access: schema.CustomFunctionAccessFullAccess},
		},
	}
	file, err := msgpack.Marshal(bundle)
	require.NoError(t, err)
	report, err := svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	require.Equal(t, 2, report.CustomFunctionsCreated)
	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	for _, tbl := range tables {
		if tbl.Name == tableName {
			defer func(id string) { _ = svc.DeleteTable(context.Background(), id) }(tbl.ID)
		}
	}

	row, err := data.NewService(driver).InsertRow(ctx, tableName, map[string]interface{}{"side": 3})
	require.NoError(t, err)
	require.Equal(t, "9", fmt.Sprint(row["surface"]))

	exported, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Tiles"})
	require.NoError(t, err)
	back, err := schema.DecodeSolutionBundle(exported)
	require.NoError(t, err)
	carried := map[string]schema.BundleCustomFunction{}
	for _, fn := range back.CustomFunctions {
		carried[fn.Name] = fn
	}
	require.Equal(t, schema.BundleCustomFunction{Name: square, Parameters: []string{"s"}, Formula: area + " ( s ; s )", Access: schema.CustomFunctionAccessAll}, carried[square])
	require.Equal(t, bundle.CustomFunctions[1], carried[area])

	// Imported again, the functions are already there and keep their
	// definition.
	again := schema.SolutionBundle{Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Tiles",
		CustomFunctions: []schema.BundleCustomFunction{{Name: area, Parameters: []string{"a", "b"}, Formula: "a + b"}}}
	file, err = msgpack.Marshal(again)
	require.NoError(t, err)
	report, err = svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	require.Zero(t, report.CustomFunctionsCreated)
	functions, err := svc.ListCustomFunctions(ctx)
	require.NoError(t, err)
	for _, fn := range functions {
		if fn.Name == area {
			require.Equal(t, "w * h", fn.Formula)
		}
	}

	// A function that does not read refuses the file, which leaves nothing.
	broken := fmt.Sprintf("Broken_%d", stamp)
	refused := schema.SolutionBundle{Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Broken",
		Tables: []schema.BundleTable{{ID: "t", Name: tableName + "_b", DisplayName: "Broken", Columns: []schema.BundleColumn{
			{ID: "c-id", Name: "id", DisplayName: "ID", FieldType: "TEXT", IsPrimaryKey: true},
		}}},
		CustomFunctions: []schema.BundleCustomFunction{{Name: broken, Parameters: []string{"x"}, Formula: "x + + )"}}}
	file, err = msgpack.Marshal(refused)
	require.NoError(t, err)
	_, err = svc.ImportSolution(ctx, file)
	require.ErrorIs(t, err, schema.ErrInvalidCustomFunction)
	functions, err = svc.ListCustomFunctions(ctx)
	require.NoError(t, err)
	for _, fn := range functions {
		require.NotEqual(t, broken, fn.Name)
	}
	tables, err = svc.ListTables(ctx)
	require.NoError(t, err)
	for _, tbl := range tables {
		require.NotEqual(t, tableName+"_b", tbl.Name)
	}
}

// A custom function cannot take the name of a built-in function, as
// FileMaker does not let it: a file defining one is refused, saying so.
func TestCustomFunctionNamedLikeABuiltinIsRefused(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	sol := newExternalSolution(t, ctx, mgr, "cf_builtin")
	file, err := msgpack.Marshal(schema.SolutionBundle{Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Builtin",
		CustomFunctions: []schema.BundleCustomFunction{{Name: "upper", Parameters: []string{"t"}, Formula: "t"}}})
	require.NoError(t, err)
	_, err = sol.svc.ImportSolution(ctx, file)
	require.ErrorIs(t, err, schema.ErrInvalidCustomFunction)
	require.Contains(t, err.Error(), "upper is the name of a built-in function")
	functions, err := sol.svc.ListCustomFunctions(ctx)
	require.NoError(t, err)
	require.Empty(t, functions)
}

// A solution holds one function of a name whatever its case, even written
// directly, so two imports at once cannot add two spellings of it.
func TestCustomFunctionNameIsUniqueWhateverItsCase(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	sol := newExternalSolution(t, ctx, mgr, "cf_unique")
	d := sol.driver.Dialect()
	insert := `INSERT INTO ` + data.CustomFunctionsTable + ` (id, name, parameters, formula, access) VALUES (` +
		d.Placeholder(1) + `, ` + d.Placeholder(2) + `, '[]', '1', 'all')`
	_, err := sol.driver.DB().ExecContext(ctx, insert, "cf-1", "Twice")
	require.NoError(t, err)
	_, err = sol.driver.DB().ExecContext(ctx, insert, "cf-2", "TWICE")
	require.True(t, dbal.IsDuplicateKey(err), "a second spelling: %v", err)
}

// A migration merged into an existing solution that fails is undone from a
// snapshot of the design, which removes the custom functions it added and
// keeps those the solution had.
func TestRestoreDesignRemovesTheCustomFunctionsAdded(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	sol := newExternalSolution(t, ctx, mgr, "cf_restore")
	importFunctions := func(functions ...schema.BundleCustomFunction) {
		t.Helper()
		file, err := msgpack.Marshal(schema.SolutionBundle{Format: schema.SolutionFormat, Version: schema.SolutionVersion,
			SolutionName: "Restore", CustomFunctions: functions})
		require.NoError(t, err)
		_, err = sol.svc.ImportSolution(ctx, file)
		require.NoError(t, err)
	}
	importFunctions(schema.BundleCustomFunction{Name: "Kept", Formula: "1"})
	snap, err := sol.svc.SnapshotDesign(ctx, "Restore")
	require.NoError(t, err)
	importFunctions(schema.BundleCustomFunction{Name: "Added", Parameters: []string{"x"}, Formula: "x + Kept"})

	require.NoError(t, sol.svc.RestoreDesign(ctx, snap))
	functions, err := sol.svc.ListCustomFunctions(ctx)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	require.Equal(t, "Kept", functions[0].Name)
}
