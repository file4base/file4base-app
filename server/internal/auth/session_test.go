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

	token, sess, err := store.Create("u1", "alice", RoleAdmin, "sales", "stamp", false)
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
	t1, _, err := store.Create("u1", "alice", RoleUser, "db", "stamp", false)
	require.NoError(t, err)
	t2, _, err := store.Create("u1", "alice", RoleUser, "db", "stamp", false)
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

	token, _, err := store.Create("u1", "alice", RoleUser, "db", "stamp", false)
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
	a1, _, _ := store.Create("u1", "alice", RoleUser, "db1", "stamp", false)
	a2, _, _ := store.Create("u1", "alice", RoleUser, "db1", "stamp", false)
	b1, _, _ := store.Create("u2", "bob", RoleUser, "db1", "stamp", false)
	c1, _, _ := store.Create("u1", "alice", RoleUser, "db2", "stamp", false)

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
	token, sess, err := store.Create("u1", "alice", RoleUser, "db", "stamp-1", false)
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

func TestStore_SessionGroups(t *testing.T) {
	store := NewStore(10 * time.Minute)
	current := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }

	root, survey, err := store.Create("u1", "alice", RoleUser, "survey", "s1", false)
	require.NoError(t, err)
	require.NotEmpty(t, survey.Group)
	assert.False(t, survey.Linked)

	hrToken, hr, err := store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h-stamp", false)
	require.NoError(t, err)
	assert.Equal(t, survey.Group, hr.Group)
	assert.True(t, hr.Linked)
	assert.Equal(t, "hr", hr.Database)

	// A linked session is an ordinary session of its own solution.
	got, ok := store.Get(hrToken)
	require.True(t, ok)
	assert.Equal(t, "h1", got.UserID)

	member, ok := store.Member(survey.Group, "hr")
	require.True(t, ok)
	assert.Equal(t, "h1", member.UserID)
	// Database names are matched exactly: on PostgreSQL "HR" is another one.
	_, ok = store.Member(survey.Group, "HR")
	assert.False(t, ok)
	assert.False(t, store.Unlink(survey.Group, "HR"))

	// A cycle back to the group's own solution resolves to its sign-in.
	self, ok := store.Member(survey.Group, "survey")
	require.True(t, ok)
	assert.Equal(t, "u1", self.UserID)
	_, ok = store.Member(survey.Group, "codes")
	assert.False(t, ok)
	_, ok = store.Member("", "hr")
	assert.False(t, ok)

	_, _, err = store.Link(survey.Group, "u9", "alice", RoleUser, "survey", "x", false)
	assert.ErrorIs(t, err, ErrLinkToRoot)

	// Linking again replaces the group's session there.
	again, _, err := store.Link(survey.Group, "h2", "bob", RoleUser, "hr", "h-stamp", false)
	require.NoError(t, err)
	_, ok = store.Get(hrToken)
	assert.False(t, ok)
	members := store.Members(survey.Group)
	require.Len(t, members, 1)
	assert.Equal(t, "h2", members[0].UserID)

	assert.True(t, store.Unlink(survey.Group, "hr"))
	assert.False(t, store.Unlink(survey.Group, "hr"))
	_, ok = store.Get(again)
	assert.False(t, ok)
	_, ok = store.Get(root)
	assert.True(t, ok, "unlinking leaves the sign-in")

	// Signing out of the solution that started the group revokes the group.
	hrToken, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h-stamp", false)
	require.NoError(t, err)
	store.Revoke(root)
	_, ok = store.Get(hrToken)
	assert.False(t, ok)
	_, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h-stamp", false)
	assert.ErrorIs(t, err, ErrGroupClosed)
	assert.Zero(t, store.Len())
}

func TestStore_LinkedSessionsExpireAndAreRevokedLikeOthers(t *testing.T) {
	store := NewStore(10 * time.Minute)
	current := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }

	root, survey, err := store.Create("u1", "alice", RoleUser, "survey", "s1", false)
	require.NoError(t, err)
	_, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)

	// Using the member through the group keeps it alive...
	current = current.Add(9 * time.Minute)
	_, ok := store.Get(root)
	require.True(t, ok)
	_, ok = store.Member(survey.Group, "hr")
	require.True(t, ok)
	// ...and an idle member expires on its own, and is never renewed.
	current = current.Add(9 * time.Minute)
	_, ok = store.Get(root)
	require.True(t, ok)
	current = current.Add(2 * time.Minute)
	_, ok = store.Get(root)
	require.True(t, ok)
	_, ok = store.Member(survey.Group, "hr")
	assert.False(t, ok, "an expired member reads as not signed in")
	_, ok = store.Member(survey.Group, "hr")
	assert.False(t, ok)

	// Revoking an account in the linked solution revokes that member only.
	_, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)
	store.RevokeUser("hr", "h1", "")
	_, ok = store.Member(survey.Group, "hr")
	assert.False(t, ok)
	_, ok = store.Get(root)
	assert.True(t, ok)

	// Revoking the sign-in's account, or letting it expire, takes the group.
	_, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)
	store.RevokeUser("survey", "u1", "")
	assert.Zero(t, store.Len())

	root, survey, err = store.Create("u1", "alice", RoleUser, "survey", "s1", false)
	require.NoError(t, err)
	_, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)
	current = current.Add(9 * time.Minute)
	_, ok = store.Member(survey.Group, "hr")
	require.True(t, ok)
	current = current.Add(2 * time.Minute)
	assert.Zero(t, store.Len(), "a member goes once the sign-in it is linked to expired")
	_, ok = store.Get(root)
	assert.False(t, ok)

	_, survey, err = store.Create("u1", "alice", RoleUser, "survey", "s1", false)
	require.NoError(t, err)
	_, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)
	store.RevokeDatabase("survey")
	assert.Zero(t, store.Len())
}

// A linked token used on its own never outlives the sign-in of its group.
func TestStore_LinkedTokenEndsWithItsRoot(t *testing.T) {
	store := NewStore(10 * time.Minute)
	current := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }

	root, survey, err := store.Create("u1", "alice", RoleUser, "survey", "s1", false)
	require.NoError(t, err)
	linked, _, err := store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)

	// Using the linked token keeps it alive, not the sign-in.
	current = current.Add(6 * time.Minute)
	_, ok := store.Get(linked)
	require.True(t, ok)
	current = current.Add(6 * time.Minute)
	_, ok = store.Get(linked)
	assert.False(t, ok, "the sign-in expired: the linked token goes with it")
	_, ok = store.Get(root)
	assert.False(t, ok)
	assert.Zero(t, store.Len())

	// A revoked sign-in takes its linked tokens at once.
	root, survey, err = store.Create("u1", "alice", RoleUser, "survey", "s1", false)
	require.NoError(t, err)
	linked, _, err = store.Link(survey.Group, "h1", "alice", RoleUser, "hr", "h", false)
	require.NoError(t, err)
	store.Revoke(root)
	_, ok = store.Get(linked)
	assert.False(t, ok)
}
