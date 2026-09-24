package schema_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityService(t *testing.T) {
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
	require.NoError(t, svc.EnsureSystemTables(ctx))

	// 1. Authenticate default owner (matches database name and password)
	authUser, err := svc.Authenticate(ctx, "file4base_dev", "file4base_dev")
	require.NoError(t, err)
	assert.Equal(t, "file4base_dev", authUser.Username)
	assert.Equal(t, "owner", authUser.Role)

	// 1b. Authenticate with empty username and owner password (database password access)
	authByPassOnly, err := svc.Authenticate(ctx, "", "file4base_dev")
	require.NoError(t, err)
	assert.Equal(t, "owner", authByPassOnly.Role)

	// 1c. Empty password must fail
	_, errEmptyPass := svc.Authenticate(ctx, "file4base_dev", "")
	assert.Error(t, errEmptyPass)
	assert.Contains(t, errEmptyPass.Error(), "password is required")

	// 2. Create standard user (or clean up previous if exists)
	_, _ = driver.DB().ExecContext(ctx, `DELETE FROM sys_users WHERE LOWER(username) = 'editor1'`)
	user, err := svc.CreateUser(ctx, "editor1", "pass123", "user")
	require.NoError(t, err)
	assert.Equal(t, "editor1", user.Username)
	assert.Equal(t, "user", user.Role)

	// 3. Authenticate standard user
	authEditor, err := svc.Authenticate(ctx, "editor1", "pass123")
	require.NoError(t, err)
	assert.Equal(t, "editor1", authEditor.Username)

	// 4. Create table occurrence and layout to test permissions
	_, err = svc.CreateTable(ctx, "Customers", "customers")
	if err != nil {
		// table may already exist
	}
	occs, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, occs)

	layout, err := svc.CreateLayout(ctx, "Customer List", occs[0].ID, json.RawMessage(`{"theme": "Default"}`))
	require.NoError(t, err)

	// 5. Assign read_only permission on layout to editor1
	perms := []schema.UserLayoutPermission{
		{
			LayoutID:    layout.ID,
			AccessLevel: "read_only",
		},
	}
	err = svc.SetUserPermissions(ctx, user.ID, perms)
	require.NoError(t, err)

	// 6. Verify permissions
	fetchedPerms, err := svc.GetUserPermissions(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, fetchedPerms, 1)
	assert.Equal(t, layout.ID, fetchedPerms[0].LayoutID)
	assert.Equal(t, "read_only", fetchedPerms[0].AccessLevel)

	// 7. Verify authentication returns updated permissions
	authEditorWithPerms, err := svc.Authenticate(ctx, "editor1", "pass123")
	require.NoError(t, err)
	require.Len(t, authEditorWithPerms.Permissions, 1)
	assert.Equal(t, "read_only", authEditorWithPerms.Permissions[0].AccessLevel)

	// 8. Prevent deleting the only owner
	_, _ = driver.DB().ExecContext(ctx, `DELETE FROM sys_users WHERE role = 'owner' AND id != $1`, authUser.ID)
	err = svc.DeleteUser(ctx, authUser.ID)
	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "only owner")
	}

	// 9. Deleting editor user succeeds
	err = svc.DeleteUser(ctx, user.ID)
	require.NoError(t, err)
}
