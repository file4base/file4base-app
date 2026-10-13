package control

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_ProjectsUsersAndPermissions(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "control_test.db")

	store, err := NewStore(dbFile)
	require.NoError(t, err)
	defer store.Close()

	// 1. Upsert & Get Project
	err = store.UpsertProject(ctx, ProjectRecord{
		Database:     "invoices_db",
		Name:         "Invoices Solution",
		Engine:       "postgres",
		DesignLocked: false,
		FileOptions:  map[string]interface{}{"default_layout": "Invoice Form"},
		PageSetup:    map[string]interface{}{"paper_size": "A4"},
	})
	require.NoError(t, err)

	proj, err := store.GetProject(ctx, "invoices_db")
	require.NoError(t, err)
	assert.Equal(t, "Invoices Solution", proj.Name)
	assert.Equal(t, "postgres", proj.Engine)
	assert.False(t, proj.DesignLocked)
	assert.Equal(t, "Invoice Form", proj.FileOptions["default_layout"])
	assert.Equal(t, "A4", proj.PageSetup["paper_size"])

	// Partial update preserves existing options
	err = store.UpsertProject(ctx, ProjectRecord{
		Database:     "invoices_db",
		Name:         "Invoices Pro",
		Engine:       "postgres",
		DesignLocked: true,
	})
	require.NoError(t, err)

	proj, err = store.GetProject(ctx, "invoices_db")
	require.NoError(t, err)
	assert.Equal(t, "Invoices Pro", proj.Name)
	assert.True(t, proj.DesignLocked)
	assert.Equal(t, "Invoice Form", proj.FileOptions["default_layout"])

	projects, err := store.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 1)

	// 2. Sync Users
	err = store.SyncUsers(ctx, "invoices_db", []UserRecord{
		{ID: "u-1", Username: "admin", Role: "owner", IsActive: true},
		{ID: "u-2", Username: "clerk", Role: "user", IsActive: true},
	})
	require.NoError(t, err)

	users, err := store.ListUsers(ctx, "invoices_db")
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, "admin", users[0].Username)
	assert.Equal(t, "clerk", users[1].Username)

	// 3. Table Permissions
	err = store.SetTablePermissions(ctx, "invoices_db", "u-2", []TablePermissionRecord{
		{
			UserID:          "u-2",
			TableName:       "invoices",
			AccessLevel:     "read_write",
			CanCreate:       true,
			CanEdit:         true,
			CanDelete:       false,
			CanModifySchema: false,
		},
		{
			UserID:          "u-2",
			TableName:       "audit_log",
			AccessLevel:     "read_only",
			CanCreate:       false,
			CanEdit:         false,
			CanDelete:       false,
			CanModifySchema: false,
		},
	})
	require.NoError(t, err)

	tPerms, err := store.GetTablePermissions(ctx, "invoices_db", "u-2")
	require.NoError(t, err)
	require.Len(t, tPerms, 2)
	assert.Equal(t, "audit_log", tPerms[0].TableName)
	assert.Equal(t, "read_only", tPerms[0].AccessLevel)
	assert.False(t, tPerms[0].CanCreate)
	assert.Equal(t, "invoices", tPerms[1].TableName)
	assert.True(t, tPerms[1].CanCreate)
	assert.True(t, tPerms[1].CanEdit)
	assert.False(t, tPerms[1].CanDelete)

	// 4. Layout Permissions
	err = store.SetLayoutPermissions(ctx, "invoices_db", "u-2", []LayoutPermissionRecord{
		{
			UserID:      "u-2",
			LayoutID:    "lay-1",
			LayoutName:  "Invoice Entry",
			AccessLevel: "read_write",
		},
	})
	require.NoError(t, err)

	lPerms, err := store.GetLayoutPermissions(ctx, "invoices_db", "u-2")
	require.NoError(t, err)
	require.Len(t, lPerms, 1)
	assert.Equal(t, "lay-1", lPerms[0].LayoutID)
	assert.Equal(t, "read_write", lPerms[0].AccessLevel)

	// 5. Delete Project cascades cleanup
	require.NoError(t, store.DeleteProject(ctx, "invoices_db"))
	_, err = store.GetProject(ctx, "invoices_db")
	assert.ErrorIs(t, err, ErrProjectNotFound)
}
