package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_CreateGetRevoke(t *testing.T) {
	store := NewStore(time.Hour)

	token, sess, err := store.Create("u1", "alice", RoleAdmin, "sales", "stamp")
	require.NoError(t, err)
	assert.Len(t, token, 64)
	assert.Equal(t, "sales", sess.Database)

	got, ok := store.Get(token)
	require.True(t, ok)
	assert.Equal(t, "alice", got.Username)
	assert.True(t, got.IsAdmin())
	assert.False(t, got.IsOwner())

	_, ok = store.Get("not-a-token")
	assert.False(t, ok)
	_, ok = store.Get("")
	assert.False(t, ok)

	store.Revoke(token)
	_, ok = store.Get(token)
	assert.False(t, ok)
}

func TestStore_TokensAreUniqueAndNotStoredInClear(t *testing.T) {
	store := NewStore(time.Hour)
	t1, _, err := store.Create("u1", "alice", RoleUser, "db", "stamp")
	require.NoError(t, err)
	t2, _, err := store.Create("u1", "alice", RoleUser, "db", "stamp")
	require.NoError(t, err)
	assert.NotEqual(t, t1, t2)

	for key := range store.sessions {
		assert.NotEqual(t, t1, key)
		assert.NotEqual(t, t2, key)
	}
}

func TestStore_ExpiryIsSliding(t *testing.T) {
	store := NewStore(10 * time.Minute)
	current := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }

	token, _, err := store.Create("u1", "alice", RoleUser, "db", "stamp")
	require.NoError(t, err)

	current = current.Add(9 * time.Minute)
	_, ok := store.Get(token)
	require.True(t, ok, "activity inside the TTL keeps the session alive")

	current = current.Add(9 * time.Minute)
	_, ok = store.Get(token)
	require.True(t, ok, "the previous access extended the lifetime")

	current = current.Add(11 * time.Minute)
	_, ok = store.Get(token)
	assert.False(t, ok, "an idle session expires")
	assert.Equal(t, 0, store.Len())
}

func TestStore_RevokeUserAndDatabase(t *testing.T) {
	store := NewStore(time.Hour)
	a1, _, _ := store.Create("u1", "alice", RoleUser, "db1", "stamp")
	a2, _, _ := store.Create("u1", "alice", RoleUser, "db1", "stamp")
	b1, _, _ := store.Create("u2", "bob", RoleUser, "db1", "stamp")
	c1, _, _ := store.Create("u1", "alice", RoleUser, "db2", "stamp")

	store.RevokeUser("db1", "u1", a2)
	_, ok := store.Get(a1)
	assert.False(t, ok)
	_, ok = store.Get(a2)
	assert.True(t, ok, "the excepted token survives")
	_, ok = store.Get(b1)
	assert.True(t, ok)
	_, ok = store.Get(c1)
	assert.True(t, ok, "same user id in another database is untouched")

	store.RevokeDatabase("db1")
	_, ok = store.Get(a2)
	assert.False(t, ok)
	_, ok = store.Get(b1)
	assert.False(t, ok)
	_, ok = store.Get(c1)
	assert.True(t, ok)
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()
	_, ok := FromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, "", TokenFromContext(ctx))

	ctx = WithToken(WithSession(ctx, Session{Username: "alice", Role: RoleOwner}), "tok")
	sess, ok := FromContext(ctx)
	require.True(t, ok)
	assert.True(t, sess.IsOwner())
	assert.Equal(t, "tok", TokenFromContext(ctx))
}

func TestStore_KeepsAndRestampsTheAccountStamp(t *testing.T) {
	store := NewStore(time.Hour)
	token, sess, err := store.Create("u1", "alice", RoleUser, "db", "stamp-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Stamp != "stamp-1" {
		t.Fatalf("stamp = %q", sess.Stamp)
	}
	store.Restamp(token, "stamp-2")
	got, ok := store.Get(token)
	if !ok || got.Stamp != "stamp-2" {
		t.Fatalf("after Restamp: ok=%v stamp=%q", ok, got.Stamp)
	}
	store.Restamp("unknown-token", "x") // no effect, no panic
}
