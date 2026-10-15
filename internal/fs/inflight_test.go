package fs

import (
	"sync/atomic"
	"testing"
	"time"

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

func TestKeepTorrentWaitsForARunningDrop(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var dropped atomic.Bool
	libraryReadStarted("racing")
	DropWhenIdle("racing", func() {
		close(started)
		<-release
		dropped.Store(true)
	})
	// the last read returns, and the drop it runs is under way when the torrent is added again
	go libraryReadEnded("racing")
	<-started

	kept := make(chan struct{})
	go func() {
		KeepTorrent("racing")
		close(kept)
	}()
	select {
	case <-kept:
		t.Fatal("KeepTorrent returned while the drop was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-kept:
	case <-time.After(5 * time.Second):
		t.Fatal("KeepTorrent didn't return once the drop finished")
	}
	// so the caller sees the drop it raced and adds the torrent again
	require.True(t, dropped.Load())
}
