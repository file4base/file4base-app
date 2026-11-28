package schema_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A table permission that cannot be read must refuse access, not fall back to
// the layout-derived default, which is read-write.
func TestTableAccess_UnreadablePermissionRefusesAccess(t *testing.T) {
	s := newDesignSolution(t)
	user, err := s.svc.CreateUser(s.ctx, "clerk", "correct horse battery", "user")
	require.NoError(t, err)
	require.NoError(t, s.svc.SetUserTablePermissions(s.ctx, user.ID, []schema.UserTablePermission{
		{TableName: "invoices", AccessLevel: schema.AccessNone},
	}))

	level, err := s.svc.TableAccess(s.ctx, user.ID, "invoices")
	require.NoError(t, err)
	require.Equal(t, schema.AccessNone, level)

	_, err = s.driver.DB().ExecContext(s.ctx, `ALTER TABLE sys_table_permissions RENAME TO sys_table_permissions_hidden`)
	require.NoError(t, err)

	_, err = s.svc.TablePermissionFor(s.ctx, user.ID, "invoices")
	assert.Error(t, err)
	level, err = s.svc.TableAccess(s.ctx, user.ID, "invoices")
	assert.Error(t, err)
	assert.Equal(t, schema.AccessNone, level)
}
