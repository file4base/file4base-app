package dbal_test

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/stretchr/testify/assert"
)

func TestUserTableNamePolicy(t *testing.T) {
	for _, name := range []string{"sys_users", "SYS_USERS", "  sys_tables ", "sys_user_permissions", "pg_user", "information_schema", "Mysql"} {
		assert.True(t, dbal.IsReservedTableName(name), name)
		assert.Error(t, dbal.CheckUserTableName(dbal.NormalizeIdentifier(name)), name)
	}
	for _, name := range []string{"customers", "system_log", "sysadmin", "page_views"} {
		assert.NoError(t, dbal.CheckUserTableName(name), name)
	}

	assert.NoError(t, dbal.CheckUserTableName(strings.Repeat("a", 63)))
	assert.ErrorIs(t, dbal.CheckUserTableName(strings.Repeat("a", 64)), dbal.ErrInvalidIdentifier)
	assert.ErrorIs(t, dbal.CheckIdentifier("column", strings.Repeat("b", 64)), dbal.ErrInvalidIdentifier)
	for _, bad := range []string{"", "1abc", "a-b", "a b", `a"b`, "ñame"} {
		assert.ErrorIs(t, dbal.CheckIdentifier("table", bad), dbal.ErrInvalidIdentifier, bad)
	}
}
