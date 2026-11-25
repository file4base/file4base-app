package control

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServerAccountStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "server_accounts_test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestServerAccount_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "Jdoe", "s3cret-pass", false, false, []string{"sales", " sales ", "hr"}))

	acc, err := store.GetServerAccount(ctx, "JDOE")
	require.NoError(t, err)
	assert.Equal(t, "Jdoe", acc.Name, "the name keeps its case; only comparison ignores it")
	assert.True(t, acc.Active)
	assert.False(t, acc.SuperOwner)
	assert.False(t, acc.AllDatabases)
	assert.Equal(t, []string{"hr", "sales"}, acc.Databases, "scope lists each database once, sorted")
}

func TestServerAccount_NamesAreUniqueIgnoringCase(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "admin", "pw-one", true, true, nil))
	err := store.CreateServerAccount(ctx, "ADMIN", "pw-two", false, true, nil)
	assert.ErrorIs(t, err, ErrServerAccountExists)
}

func TestServerAccount_NameRules(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	for _, bad := range []string{"", "   ", "jdoe@empresa.com", "two words", "tab\there"} {
		err := store.CreateServerAccount(ctx, bad, "pw", false, true, nil)
		assert.ErrorIs(t, err, ErrInvalidServerAccountName, "name %q", bad)
	}
	long := make([]byte, maxServerAccountNameLength+1)
	for i := range long {
		long[i] = 'a'
	}
	assert.ErrorIs(t, store.CreateServerAccount(ctx, string(long), "pw", false, true, nil), ErrInvalidServerAccountName)
}

func TestServerAccount_ScopeRules(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	assert.Error(t, store.CreateServerAccount(ctx, "all_and_some", "pw", false, true, []string{"sales"}),
		"an account reaching all databases lists none")
	assert.Error(t, store.CreateServerAccount(ctx, "nothing", "pw", false, false, nil),
		"an account reaching some databases lists at least one")
	assert.Error(t, store.CreateServerAccount(ctx, "no_password", "", false, true, nil))
}

func TestServerAccount_AuthenticateRefusesEveryFailureTheSameWay(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "clerk", "right-pass", false, false, []string{"sales"}))
	require.NoError(t, store.CreateServerAccount(ctx, "retired", "old-pass", false, true, nil))
	require.NoError(t, store.SetServerAccountActive(ctx, "retired", false))

	acc, err := store.AuthenticateServerAccount(ctx, "CLERK", "right-pass")
	require.NoError(t, err)
	assert.Equal(t, []string{"sales"}, acc.Databases)

	_, err = store.AuthenticateServerAccount(ctx, "clerk", "wrong-pass")
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	_, err = store.AuthenticateServerAccount(ctx, "nobody", "right-pass")
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	_, err = store.AuthenticateServerAccount(ctx, "retired", "old-pass")
	assert.ErrorIs(t, err, ErrInvalidCredentials, "a disabled account with the right password is refused")

	_, err = store.AuthenticateServerAccount(ctx, "bad@name", "anything")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestServerAccount_PasswordIsNeverStoredInClear(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "clerk", "plain-secret", false, true, nil))
	var hash string
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT password_hash FROM server_accounts WHERE name_key = 'clerk'`).Scan(&hash))
	assert.NotContains(t, hash, "plain-secret")
	assert.Contains(t, hash, "$2a$", "bcrypt hash")
}

func TestServerAccount_SetPasswordAndScope(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "clerk", "first-pass", false, false, []string{"sales"}))
	require.NoError(t, store.SetServerAccountPassword(ctx, "clerk", "second-pass"))
	_, err := store.AuthenticateServerAccount(ctx, "clerk", "first-pass")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	_, err = store.AuthenticateServerAccount(ctx, "clerk", "second-pass")
	assert.NoError(t, err)

	require.NoError(t, store.SetServerAccountScope(ctx, "clerk", false, []string{"hr"}))
	acc, err := store.GetServerAccount(ctx, "clerk")
	require.NoError(t, err)
	assert.Equal(t, []string{"hr"}, acc.Databases, "the old scope is replaced")

	require.NoError(t, store.SetServerAccountScope(ctx, "clerk", true, nil))
	acc, err = store.GetServerAccount(ctx, "clerk")
	require.NoError(t, err)
	assert.True(t, acc.AllDatabases)
	assert.Empty(t, acc.Databases)

	assert.ErrorIs(t, store.SetServerAccountPassword(ctx, "ghost", "pw"), ErrServerAccountNotFound)
}

func TestServerAccount_LastSuperOwnerIsProtected(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "root", "root-pass", true, true, nil))
	require.NoError(t, store.CreateServerAccount(ctx, "clerk", "clerk-pass", false, true, nil))

	assert.ErrorIs(t, store.SetServerAccountActive(ctx, "root", false), ErrLastSuperOwner)
	assert.ErrorIs(t, store.DeleteServerAccount(ctx, "root"), ErrLastSuperOwner)

	acc, err := store.GetServerAccount(ctx, "root")
	require.NoError(t, err)
	assert.True(t, acc.Active, "a refused change leaves the account as it was")

	require.NoError(t, store.CreateServerAccount(ctx, "backup", "backup-pass", true, true, nil))
	require.NoError(t, store.SetServerAccountActive(ctx, "root", false), "another active super-owner exists")
	assert.ErrorIs(t, store.DeleteServerAccount(ctx, "backup"), ErrLastSuperOwner,
		"root is disabled, so backup is the last active super-owner")
}

func TestServerAccount_DeleteRemovesScope(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "root", "root-pass", true, true, nil))
	require.NoError(t, store.CreateServerAccount(ctx, "clerk", "clerk-pass", false, false, []string{"sales", "hr"}))
	require.NoError(t, store.DeleteServerAccount(ctx, "CLERK"))

	_, err := store.GetServerAccount(ctx, "clerk")
	assert.ErrorIs(t, err, ErrServerAccountNotFound)

	var rows int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM server_account_databases WHERE name_key = 'clerk'`).Scan(&rows))
	assert.Zero(t, rows, "deleting an account removes the databases it reached")
}

func TestServerAccount_ListIsOrderedByName(t *testing.T) {
	ctx := context.Background()
	store := newServerAccountStore(t)

	require.NoError(t, store.CreateServerAccount(ctx, "zoe", "pw", false, true, nil))
	require.NoError(t, store.CreateServerAccount(ctx, "Anna", "pw", false, true, nil))

	list, err := store.ListServerAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "Anna", list[0].Name)
	assert.Equal(t, "zoe", list[1].Name)
}
