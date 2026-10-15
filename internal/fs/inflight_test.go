package fs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropWhenIdle(t *testing.T) {
	drops := 0
	drop := func() { drops++ }

	// nothing in flight: dropped at once
	DropWhenIdle("idle", drop)
	require.Equal(t, 1, drops)

	// two reads in flight: dropped when the last returns
	libraryReadStarted("busy")
	libraryReadStarted("busy")
	DropWhenIdle("busy", drop)
	require.Equal(t, 1, drops)
	libraryReadEnded("busy")
	require.Equal(t, 1, drops)
	libraryReadEnded("busy")
	require.Equal(t, 2, drops)
	require.Zero(t, LibraryReadsInFlight("busy"))

	// added again before the read returned: kept
	libraryReadStarted("readded")
	DropWhenIdle("readded", drop)
	KeepTorrent("readded")
	libraryReadEnded("readded")
	require.Equal(t, 2, drops)
}
