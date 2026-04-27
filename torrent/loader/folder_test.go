package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolder_ListTorrentPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "distribyted-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	route1Dir := filepath.Join(tmpDir, "route1")
	err = os.MkdirAll(route1Dir, 0755)
	require.NoError(t, err)

	torrent1 := filepath.Join(route1Dir, "test1.torrent")
	err = os.WriteFile(torrent1, []byte("torrent content"), 0644)
	require.NoError(t, err)

	// Nested file
	subDir := filepath.Join(route1Dir, "sub")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)
	torrent2 := filepath.Join(subDir, "test2.torrent")
	err = os.WriteFile(torrent2, []byte("torrent content 2"), 0644)
	require.NoError(t, err)

	// Non-torrent file
	err = os.WriteFile(filepath.Join(route1Dir, "test.txt"), []byte("text content"), 0644)
	require.NoError(t, err)

	routes := []*config.Route{
		{
			Name:          "route1",
			TorrentFolder: route1Dir,
		},
		{
			Name:          "route2",
			TorrentFolder: "", // Should be ignored
		},
	}

	f := NewFolder(routes)
	paths, err := f.ListTorrentPaths()
	require.NoError(t, err)

	assert.Len(t, paths, 1)
	assert.ElementsMatch(t, []string{torrent1, torrent2}, paths["route1"])
}

func TestFolder_ListMagnets(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "distribyted-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	route1Dir := filepath.Join(tmpDir, "route1")
	err = os.MkdirAll(route1Dir, 0755)
	require.NoError(t, err)

	magnetContent1 := "magnet:?xt=urn:btih:1234567890abcdef"
	magnet1 := filepath.Join(route1Dir, "test1.magnet")
	err = os.WriteFile(magnet1, []byte(magnetContent1), 0644)
	require.NoError(t, err)

	// Magnet with whitespace/newlines
	magnetContent2 := "  magnet:?xt=urn:btih:abcdef1234567890  \n\n"
	magnet2 := filepath.Join(route1Dir, "test2.magnet")
	err = os.WriteFile(magnet2, []byte(magnetContent2), 0644)
	require.NoError(t, err)

	// Non-magnet file
	err = os.WriteFile(filepath.Join(route1Dir, "test.torrent"), []byte("torrent content"), 0644)
	require.NoError(t, err)

	routes := []*config.Route{
		{
			Name:          "route1",
			TorrentFolder: route1Dir,
		},
	}

	f := NewFolder(routes)
	magnets, err := f.ListMagnets()
	require.NoError(t, err)

	assert.Len(t, magnets, 1)
	assert.ElementsMatch(t, []string{magnetContent1, "magnet:?xt=urn:btih:abcdef1234567890"}, magnets["route1"])
}
