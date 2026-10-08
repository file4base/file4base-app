package schema_test

import (
	"context"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRolePrivileges covers the capability grants of #39 at the service level.
func TestRolePrivileges(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not accessible, skipping integration test:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed, skipping integration test:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	t.Cleanup(func() {
		_, _ = driver.DB().ExecContext(context.Background(), `DELETE FROM sys_privileges`)
	})
	_, err = driver.DB().ExecContext(ctx, `DELETE FROM sys_privileges`)
	require.NoError(t, err)

	// A database that stored nothing grants everything, so nothing becomes
	// restricted by upgrading to a version that has this table.
	for _, role := range schema.PrivilegeRoles() {
		for _, capability := range schema.Capabilities() {
			allowed, err := svc.RoleAllows(ctx, role, capability)
			require.NoError(t, err)
			assert.True(t, allowed, "%s should hold %s by default", role, capability)
		}
	}

	// Withholding one capability leaves the other alone.
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{
		{Role: "user", BulkExport: false, BulkImport: true},
	}))
	allowed, err := svc.RoleAllows(ctx, "user", schema.CapabilityBulkExport)
	require.NoError(t, err)
	assert.False(t, allowed)
	allowed, err = svc.RoleAllows(ctx, "user", schema.CapabilityBulkImport)
	require.NoError(t, err)
	assert.True(t, allowed)

	// Roles left out of the call keep what they hold.
	allowed, err = svc.RoleAllows(ctx, "admin", schema.CapabilityBulkExport)
	require.NoError(t, err)
	assert.True(t, allowed)

	// Writing the same role again replaces its row rather than adding one.
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{
		{Role: "user", BulkExport: true, BulkImport: false},
	}))
	listed, err := svc.ListRolePrivileges(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 3)
	byRole := map[string]schema.RolePrivileges{}
	for _, p := range listed {
		byRole[p.Role] = p
	}
	assert.True(t, byRole["user"].BulkExport)
	assert.False(t, byRole["user"].BulkImport)

	// An owner always holds every capability, whatever is asked for or
	// stored: an owner must not be able to lock themselves out.
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{
		{Role: "owner", BulkExport: false, BulkImport: false},
	}))
	listed, err = svc.ListRolePrivileges(ctx)
	require.NoError(t, err)
	for _, p := range listed {
		if p.Role == "owner" {
			assert.True(t, p.BulkExport)
			assert.True(t, p.BulkImport)
		}
	}
	for _, capability := range schema.Capabilities() {
		allowed, err := svc.RoleAllows(ctx, "owner", capability)
		require.NoError(t, err)
		assert.True(t, allowed)
	}

	// Roles and capabilities File4Base does not have are refused.
	assert.ErrorIs(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{{Role: "superuser"}}), schema.ErrUnknownRole)
	_, err = svc.RoleAllows(ctx, "admin", "f4bwebdirect")
	assert.ErrorIs(t, err, schema.ErrUnknownCapability)

	// CapabilitiesOf answers what the clients are told at sign-in.
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{
		{Role: "admin", BulkExport: true, BulkImport: false},
	}))
	caps, err := svc.CapabilitiesOf(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, []string{schema.CapabilityBulkExport}, caps)
}
