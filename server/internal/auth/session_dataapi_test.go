package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dataAPIClock(store *Store) *time.Time {
	current := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return current })
	return &current
}

func TestStore_DataAPISessionIdlesOutAfterFifteenMinutes(t *testing.T) {
	store := NewStore(time.Hour)
	now := dataAPIClock(store)

	token, sess, err := store.CreateDataAPI("u1", "alice", RoleUser, "sales", "SALES", "stamp")
	require.NoError(t, err)
	assert.Len(t, token, 64)
	assert.Equal(t, KindDataAPI, sess.Kind)
	assert.Equal(t, "SALES", sess.DataAPIName)
	assert.False(t, sess.SuperOwner)
	assert.Equal(t, now.Add(DefaultDataAPISessionTTL), sess.ExpiresAt)

	*now = now.Add(DefaultDataAPISessionTTL - time.Second)
	_, ok := store.DataAPI(token, true)
	require.True(t, ok, "a call inside the idle lifetime keeps the session")

	*now = now.Add(DefaultDataAPISessionTTL - time.Second)
	_, ok = store.DataAPI(token, false)
	require.True(t, ok, "the previous call renewed the session")

	*now = now.Add(time.Second)
	_, ok = store.DataAPI(token, true)
	assert.False(t, ok, "the session idles out after the lifetime")
	assert.Zero(t, store.Len(), "an expired session is dropped")
}

func TestStore_DataAPILookWithoutRenewDoesNotExtend(t *testing.T) {
	store := NewStore(time.Hour)
	now := dataAPIClock(store)

	token, _, err := store.CreateDataAPI("u1", "alice", RoleUser, "sales", "sales", "stamp")
	require.NoError(t, err)

	deadline := now.Add(DefaultDataAPISessionTTL)
	for _, at := range []time.Duration{5 * time.Minute, 10 * time.Minute, DefaultDataAPISessionTTL - time.Second} {
		*now = deadline.Add(at - DefaultDataAPISessionTTL)
		got, ok := store.DataAPI(token, false)
		require.True(t, ok)
		assert.Equal(t, deadline, got.ExpiresAt, "validateSession only looks")
	}
	*now = deadline
	_, ok := store.DataAPI(token, false)
	assert.False(t, ok, "fifteen minutes after sign-in, with only looks in between")
}

func TestStore_DataAPITTLIsConfigurable(t *testing.T) {
	store := NewStore(time.Hour)
	now := dataAPIClock(store)
	store.SetDataAPITTL(2 * time.Minute)

	token, _, err := store.CreateDataAPI("u1", "alice", RoleUser, "sales", "sales", "stamp")
	require.NoError(t, err)
	*now = now.Add(2 * time.Minute)
	_, ok := store.DataAPI(token, true)
	assert.False(t, ok)

	store.SetDataAPITTL(0)
	token, sess, err := store.CreateDataAPI("u1", "alice", RoleUser, "sales", "sales", "stamp")
	require.NoError(t, err)
	assert.Equal(t, now.Add(DefaultDataAPISessionTTL), sess.ExpiresAt, "non-positive selects the default")
	_, ok = store.DataAPI(token, true)
	assert.True(t, ok)
}

func TestStore_KindsDoNotCrossRouteGroups(t *testing.T) {
	store := NewStore(time.Hour)

	dapi, _, err := store.CreateDataAPI("u1", "alice", RoleOwner, "sales", "sales", "stamp")
	require.NoError(t, err)
	client, _, err := store.Create("u1", "alice", RoleOwner, "sales", "stamp", true)
	require.NoError(t, err)

	_, ok := store.Get(dapi)
	assert.False(t, ok, "a Data API token is no /api/v1 session")
	_, ok = store.DataAPI(client, true)
	assert.False(t, ok, "a client token is no Data API session")

	group := digest(dapi)
	_, ok = store.Root(group)
	assert.False(t, ok)
	_, ok = store.Member(group, "sales")
	assert.False(t, ok)
	_, _, err = store.Link(group, "u1", "alice", RoleUser, "other", "stamp", false)
	assert.ErrorIs(t, err, ErrGroupClosed)

	kinds := map[string]bool{}
	for _, info := range store.ListSessions("") {
		kinds[info.Kind] = true
	}
	assert.Equal(t, map[string]bool{"client": true, "data_api": true}, kinds)
}

func TestStore_DataAPIRevocation(t *testing.T) {
	store := NewStore(time.Hour)

	token, _, err := store.CreateDataAPI("u1", "alice", RoleUser, "sales", "sales", "stamp")
	require.NoError(t, err)
	store.Revoke(token)
	_, ok := store.DataAPI(token, true)
	assert.False(t, ok, "logout revokes immediately")

	token, _, err = store.CreateDataAPI("u1", "alice", RoleUser, "sales", "sales", "stamp")
	require.NoError(t, err)
	store.RevokeUser("sales", "u1", "")
	_, ok = store.DataAPI(token, true)
	assert.False(t, ok, "account changes end Data API sessions too")

	token, _, err = store.CreateDataAPI("u1", "alice", RoleUser, "sales", "sales", "stamp")
	require.NoError(t, err)
	store.RevokeDatabase("sales")
	_, ok = store.DataAPI(token, true)
	assert.False(t, ok)
}
