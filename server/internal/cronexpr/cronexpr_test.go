package cronexpr_test

import (
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/cronexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(text string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", text)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNext(t *testing.T) {
	cases := []struct {
		expr, after, want string
	}{
		{"* * * * *", "2026-11-10 21:13", "2026-11-10 21:14"},
		{"0 2 * * *", "2026-11-10 21:13", "2026-11-11 02:00"},
		{"0 2 * * *", "2026-11-11 01:59", "2026-11-11 02:00"},
		{"0 2 * * *", "2026-11-11 02:00", "2026-11-12 02:00"}, // strictly after
		{"*/15 * * * *", "2026-11-10 21:13", "2026-11-10 21:15"},
		{"*/15 * * * *", "2026-11-10 21:59", "2026-11-10 22:00"},
		{"5/20 * * * *", "2026-11-10 21:26", "2026-11-10 21:45"},
		{"0 9-17/4 * * *", "2026-11-10 14:00", "2026-11-10 17:00"},
		{"30 6 1,15 * *", "2026-11-02 00:00", "2026-11-15 06:30"},
		{"0 0 1 1 *", "2026-11-10 21:13", "2027-01-01 00:00"},
		{"0 8 * * 1-5", "2026-11-13 09:00", "2026-11-16 08:00"}, // Friday → Monday
		{"0 8 * * 0", "2026-11-10 00:00", "2026-11-15 08:00"},   // Sunday as 0
		{"0 8 * * 7", "2026-11-10 00:00", "2026-11-15 08:00"},   // Sunday as 7
		// Both day fields restricted: either matches.
		{"0 0 13 * 5", "2026-11-01 00:00", "2026-11-06 00:00"},
		{"0 0 29 2 *", "2026-03-01 00:00", "2028-02-29 00:00"}, // the next leap day
	}
	for _, tc := range cases {
		s, err := cronexpr.Parse(tc.expr)
		require.NoError(t, err, tc.expr)
		assert.Equal(t, at(tc.want), s.Next(at(tc.after)), "%s after %s", tc.expr, tc.after)
	}
}

func TestNext_NeverDue(t *testing.T) {
	s, err := cronexpr.Parse("0 0 30 2 *")
	require.NoError(t, err)
	assert.True(t, s.Next(at("2026-01-01 00:00")).IsZero(), "February has no 30th")
}

func TestParse_RefusesWhatItDoesNotRead(t *testing.T) {
	for _, expr := range []string{
		"",
		"* * * *",
		"* * * * * *",  // no seconds field
		"60 * * * *",   // minute out of range
		"* 24 * * *",   // hour out of range
		"* * 0 * *",    // day of month starts at 1
		"* * * 13 *",   // month out of range
		"* * * * 8",    // day of week out of range
		"*/0 * * * *",  // zero step
		"10-5 * * * *", // backwards range
		"a * * * *",
		"* * * JAN *", // names are not read
		"1,,2 * * * *",
	} {
		_, err := cronexpr.Parse(expr)
		assert.ErrorIs(t, err, cronexpr.ErrInvalid, "%q", expr)
	}
}

func TestString_IsTheExpressionNormalized(t *testing.T) {
	s, err := cronexpr.Parse("  0   2 * *  * ")
	require.NoError(t, err)
	assert.Equal(t, "0 2 * * *", s.String())
}

// A server schedule reads its expression in the server's time zone, and a
// change of daylight saving time neither runs it twice nor shifts it.
func TestNextIn_ReadsTheWallClockOfItsZone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)
	local := func(text string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", text, paris)
		require.NoError(t, err)
		return v
	}
	daily, err := cronexpr.Parse("0 2 * * *")
	require.NoError(t, err)
	assert.Equal(t, local("2026-11-11 02:00").UTC(), daily.NextIn(local("2026-11-10 21:13"), paris))
	assert.Equal(t, at("2026-11-11 01:00"), daily.NextIn(local("2026-11-10 21:13"), paris), "02:00 in Paris is 01:00 UTC in winter")
	assert.Equal(t, local("2026-07-01 02:00").UTC(), daily.NextIn(local("2026-06-30 12:00"), paris))

	// 29 March 2026: 02:00 to 03:00 does not exist in Paris, so a 02:30 job
	// does not run that day.
	gap, err := cronexpr.Parse("30 2 * * *")
	require.NoError(t, err)
	assert.Equal(t, local("2026-03-30 02:30").UTC(), gap.NextIn(local("2026-03-29 00:00"), paris))

	// 25 October 2026: 02:00 to 03:00 happens twice; a 02:30 job runs once.
	repeat, err := cronexpr.Parse("30 2 * * *")
	require.NoError(t, err)
	first := repeat.NextIn(local("2026-10-25 00:00"), paris)
	assert.Equal(t, at("2026-10-25 00:30"), first, "the first 02:30, still summer time")
	assert.Equal(t, local("2026-10-26 02:30").UTC(), repeat.NextIn(first, paris), "not again an hour later")
	assert.Equal(t, local("2026-10-26 02:30").UTC(), repeat.NextIn(at("2026-10-25 01:10"), paris),
		"nor when worked out again at 02:10 winter time, between the two")
	hourly, err := cronexpr.Parse("0 * * * *")
	require.NoError(t, err)
	assert.Equal(t, at("2026-10-25 02:00"), hourly.NextIn(at("2026-10-25 00:00"), paris), "03:00 local, after the repeated hour")

	assert.Equal(t, daily.Next(at("2026-11-10 21:13")), daily.NextIn(at("2026-11-10 21:13"), time.UTC))
}
