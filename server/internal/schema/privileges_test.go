package schema_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// TestRolePrivileges covers the capability grants of #39 at the service level.
func TestRolePrivileges(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not accessible, skipping integration test:", err)
		return
	}
	t.Cleanup(func() { _ = driver.Close() }) // after the cleanup that empties sys_privileges

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

	// A database that stored nothing grants every bulk capability, so
	// nothing becomes restricted by upgrading to a version that has this
	// table; the Data API is held by owners only until it is granted.
	for _, role := range schema.PrivilegeRoles() {
		for _, capability := range schema.Capabilities() {
			allowed, err := svc.RoleAllows(ctx, role, capability)
			require.NoError(t, err)
			want := capability != schema.CapabilityFMRest || role == "owner"
			assert.Equal(t, want, allowed, "%s holding %s by default", role, capability)
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

	// A row stored without naming fmrest withholds it.
	allowed, err = svc.RoleAllows(ctx, "user", schema.CapabilityFMRest)
	require.NoError(t, err)
	assert.False(t, allowed)

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

	// GrantCapability adds one capability and leaves the others as stored.
	require.NoError(t, svc.GrantCapability(ctx, "admin", schema.CapabilityFMRest))
	caps, err = svc.CapabilitiesOf(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, []string{schema.CapabilityBulkExport, schema.CapabilityFMRest}, caps)
	require.NoError(t, svc.GrantCapability(ctx, "user", schema.CapabilityFMRest))
	caps, err = svc.CapabilitiesOf(ctx, "user")
	require.NoError(t, err)
	assert.Equal(t, []string{schema.CapabilityBulkExport, schema.CapabilityFMRest}, caps)
	assert.ErrorIs(t, svc.GrantCapability(ctx, "superuser", schema.CapabilityFMRest), schema.ErrUnknownRole)
	assert.ErrorIs(t, svc.GrantCapability(ctx, "user", "fmxdbc"), schema.ErrUnknownCapability)
}

// Privileges decoded from a request or a solution file that does not name
// fmrest leave the stored value as it is; naming it, even as false, sets it.
func TestRolePrivileges_OmittedFMRestKeepsTheStoredValue(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not accessible, skipping integration test:", err)
		return
	}
	t.Cleanup(func() { _ = driver.Close() }) // after the cleanup that empties sys_privileges
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

	fromJSON := func(body string) schema.RolePrivileges {
		var p schema.RolePrivileges
		require.NoError(t, json.Unmarshal([]byte(body), &p))
		return p
	}
	fromMsgpack := func(v map[string]interface{}) schema.RolePrivileges {
		data, err := msgpack.Marshal(v)
		require.NoError(t, err)
		var p schema.RolePrivileges
		require.NoError(t, msgpack.Unmarshal(data, &p))
		return p
	}
	holds := func(role string) bool {
		allowed, err := svc.RoleAllows(ctx, role, schema.CapabilityFMRest)
		require.NoError(t, err)
		return allowed
	}

	// Omitted for a role with no row: the row is written, not granted.
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{fromJSON(`{"role":"admin","bulk_export":false,"bulk_import":true}`)}))
	assert.False(t, holds("admin"))
	exportOK, err := svc.RoleAllows(ctx, "admin", schema.CapabilityBulkExport)
	require.NoError(t, err)
	assert.False(t, exportOK)

	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{fromJSON(`{"role":"user","bulk_export":true,"bulk_import":true,"fmrest":true}`)}))
	require.True(t, holds("user"))
	for _, p := range []schema.RolePrivileges{
		fromJSON(`{"role":"user","bulk_export":false,"bulk_import":false}`),
		fromMsgpack(map[string]interface{}{"role": "user", "bulk_export": true, "bulk_import": false}),
	} {
		require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{p}))
		assert.True(t, holds("user"), "omitted, fmrest is kept: %+v", p)
	}
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{
		fromMsgpack(map[string]interface{}{"role": "user", "bulk_export": true, "bulk_import": false, "fmrest": false}),
	}))
	assert.False(t, holds("user"), "named false, it is withdrawn")
	for _, p := range []schema.RolePrivileges{
		fromJSON(`{"role":"user","bulk_export":true,"bulk_import":true,"fmrest":null}`),
		fromMsgpack(map[string]interface{}{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": nil}),
	} {
		require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{{Role: "user", BulkExport: true, BulkImport: true, FMRest: true}}))
		require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{p}))
		assert.False(t, holds("user"), "named null, it is withdrawn: %+v", p)
	}

	// A value built in code always names it.
	require.NoError(t, svc.SetRolePrivileges(ctx, []schema.RolePrivileges{{Role: "user", BulkExport: true, BulkImport: true, FMRest: true}}))
	assert.True(t, holds("user"))
}
