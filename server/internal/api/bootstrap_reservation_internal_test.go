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

	fixed := newBootstrapReservation("admin", nil, "abcd-efgh")
	assert.True(t, fixed.codeMatches("ABCDEFGH"))
}
