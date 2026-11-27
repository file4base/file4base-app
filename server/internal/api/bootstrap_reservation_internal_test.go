package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBootstrapReservation_SetupCode(t *testing.T) {
	none := newBootstrapReservation("", []string{"malformed", ":root", "db:"}, "")
	assert.False(t, none.reserved("db"))
	assert.Empty(t, none.setupCode, "nothing reserved, no code")
	assert.False(t, none.codeMatches(""))

	b := newBootstrapReservation(" Admin ", []string{"Ops:root"}, "")
	assert.True(t, b.reserved("admin"))
	assert.True(t, b.reserved("OPS"))
	assert.False(t, b.reserved("root"))
	assert.Len(t, b.setupCode, 16, "80 random bits")
	assert.NotEqual(t, b.setupCode, newBootstrapReservation("admin", nil, "").setupCode)
	grouped := groupedSetupCode(b.setupCode)
	assert.Len(t, grouped, 19)
	assert.True(t, b.codeMatches(grouped))
	assert.False(t, b.codeMatches(""))
	assert.False(t, b.codeMatches(grouped[:18]))

	assert.False(t, b.fromEnv)

	fixed := newBootstrapReservation("admin", nil, "abcd-efgh-ijkl-mnop")
	assert.True(t, fixed.fromEnv)
	assert.True(t, fixed.codeMatches("ABCDEFGHIJKLMNOP"))

	// Too short to resist guessing, or nothing but separators: a random code,
	// which is then printed.
	for _, weak := range []string{"1234", "- -"} {
		r := newBootstrapReservation("admin", nil, weak)
		assert.False(t, r.fromEnv, weak)
		assert.Len(t, r.setupCode, 16, weak)
		assert.False(t, r.codeMatches(weak), weak)
	}

	// A seeded-only database keeps its seeded usernames; the admin database
	// takes any first owner.
	s := newBootstrapReservation("admin", []string{"ops:Root", "ops:deputy"}, "")
	assert.Equal(t, []string{"root", "deputy"}, s.databases["ops"])
	assert.Equal(t, "admin", s.adminDatabase)
}
