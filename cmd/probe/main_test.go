package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// TestFirstFile: a route's top level holds the torrent's folder, so the probe has to look
// inside it to find something to read.
func TestFirstFile(t *testing.T) {
	cfs, err := fs.NewContainerFs(nil)
	require.NoError(t, err)
	require.NoError(t, cfs.Mkdir("/The WIRED CD"))
	require.NoError(t, cfs.Mkdir("/The WIRED CD/art"))
	require.NoError(t, cfs.Create("/The WIRED CD/art/cover.jpg"))
	require.NoError(t, cfs.Create("/The WIRED CD/01 - Track.mp3"))

	p, ok := firstFile(cfs, "/")
	require.True(t, ok)
	require.Equal(t, "/The WIRED CD/01 - Track.mp3", p)

	empty, err := fs.NewContainerFs(nil)
	require.NoError(t, err)
	_, ok = firstFile(empty, "/")
	require.False(t, ok)
}
