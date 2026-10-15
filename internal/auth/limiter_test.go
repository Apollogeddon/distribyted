package auth

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := NewLimiter()
	l.now = func() time.Time { return now }

	for range FreeAttempts - 1 {
		require.True(t, l.Allowed("1.2.3.4"))
		l.Failed("1.2.3.4")
	}
	require.True(t, l.Allowed("1.2.3.4"))
	l.Failed("1.2.3.4")

	// past the free attempts the address waits, and longer after each further failure
	require.False(t, l.Allowed("1.2.3.4"))
	require.Equal(t, time.Second, l.Wait("1.2.3.4"))
	require.Equal(t, "1", l.RetryAfter("1.2.3.4"))
	require.Equal(t, "1", l.RetryAfter("5.6.7.8"), "never 0, even once the wait has run out")
	require.True(t, l.Allowed("5.6.7.8"), "other addresses are unaffected")
	now = now.Add(time.Second)
	require.True(t, l.Allowed("1.2.3.4"))
	l.Failed("1.2.3.4")
	require.Equal(t, "2", l.RetryAfter("1.2.3.4"), "Retry-After says the real wait")
	now = now.Add(time.Second)
	require.False(t, l.Allowed("1.2.3.4"))
	now = now.Add(time.Second)
	require.True(t, l.Allowed("1.2.3.4"))

	// a successful login clears the record
	l.Succeeded("1.2.3.4")
	l.Failed("1.2.3.4")
	require.True(t, l.Allowed("1.2.3.4"))
}

func TestLimiterRemembersABoundedNumberOfAddresses(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := NewLimiter()
	l.now = func() time.Time { return now }
	l.maxTracked = 100

	for range FreeAttempts {
		l.Failed("guesser")
	}
	for i := range l.maxTracked + 100 {
		now = now.Add(time.Millisecond)
		l.Failed(strconv.Itoa(i))
	}
	require.LessOrEqual(t, len(l.attempts), l.maxTracked)
	require.True(t, l.Allowed("guesser"), "the address that failed longest ago is forgotten first")
}
