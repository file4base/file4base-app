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

func TestSecurityService(t *testing.T) {
	dsn := testdb.DevDSN()
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
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

	// EnsureSystemTables never provisions accounts on its own
	ownerName := fmt.Sprintf("sec_owner_%d", time.Now().UnixNano()%1000000)
	_, errNoDefault := svc.Authenticate(ctx, "admin", "admin")
	if errNoDefault == nil {
		t.Log("an 'admin/admin' account already exists in this development database (created by an older version)")
	}

	// 1. Provision an owner explicitly and authenticate with it
	require.NoError(t, svc.EnsureSystemTablesWithCredentials(ctx, ownerName, "owner-secret"))
	defer func() {
		_, _ = driver.DB().ExecContext(context.Background(), `DELETE FROM sys_users WHERE LOWER(username) = LOWER($1)`, ownerName)
	}()
	authUser, err := svc.Authenticate(ctx, ownerName, "owner-secret")
	require.NoError(t, err)
	assert.Equal(t, ownerName, authUser.Username)
	assert.Equal(t, "owner", authUser.Role)

	// 1b. The owner password alone is not enough: the username must match too
	_, errNoUser := svc.Authenticate(ctx, "", "owner-secret")
	assert.Error(t, errNoUser)
	_, errOtherUser := svc.Authenticate(ctx, "someone_else", "owner-secret")
	assert.ErrorIs(t, errOtherUser, schema.ErrInvalidCredentials)
	_, errWrongPass := svc.Authenticate(ctx, ownerName, "wrong")
	assert.ErrorIs(t, errWrongPass, schema.ErrInvalidCredentials)

	// 1c. Empty password must fail
	_, errEmptyPass := svc.Authenticate(ctx, ownerName, "")
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

	// 7b. An explicit "none" is stored (a missing row would default to read_write)
	require.NoError(t, svc.SetUserPermissions(ctx, user.ID, []schema.UserLayoutPermission{{LayoutID: layout.ID, AccessLevel: "none"}}))
	level, err := svc.LayoutAccess(ctx, user.ID, layout.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.AccessNone, level)
	assert.Error(t, svc.SetUserPermissions(ctx, user.ID, []schema.UserLayoutPermission{{LayoutID: layout.ID, AccessLevel: "superuser"}}))

	// 8. The last active owner can be neither deleted, demoted nor deactivated
	var owners int
	require.NoError(t, driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users WHERE role = 'owner' AND COALESCE(is_active, TRUE) = TRUE`).Scan(&owners))
	if owners == 1 {
		err = svc.DeleteUser(ctx, authUser.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only owner")
		assert.Error(t, svc.UpdateUser(ctx, authUser.ID, "", "user"))
		inactive := false
		assert.Error(t, svc.UpdateUser(ctx, authUser.ID, "", "owner", &inactive))
	}

	// 9. Deleting editor user succeeds
	err = svc.DeleteUser(ctx, user.ID)
	require.NoError(t, err)
}
