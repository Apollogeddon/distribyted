package loader

import (
	"os"
	"testing"

	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
)

const m1 = "magnet:?xt=urn:btih:c9e15763f722f23e98a29decdfae341b98d53056"

func TestDB(t *testing.T) {
	require := require.New(t)

	tmpService, err := os.MkdirTemp("", "service")
	require.NoError(err)
	tmpStorage, err := os.MkdirTemp("", "storage")
	require.NoError(err)

	cs := storage.NewFile(tmpStorage)
	defer func() { _ = cs.Close() }()

	s, err := NewDB(tmpService)
	require.NoError(err)
	defer func() { _ = s.Close() }()

	err = s.AddMagnet("route1", "WRONG MAGNET")
	require.Error(err)

	err = s.AddMagnet("route1", m1)
	require.NoError(err)

	err = s.AddMagnet("route2", m1)
	require.NoError(err)

	l, err := s.ListMagnets()
	require.NoError(err)
	require.Len(l, 2)
	require.Len(l["route1"], 1)
	require.Equal(l["route1"][0], m1)
	require.Len(l["route2"], 1)
	require.Equal(l["route2"][0], m1)

	removed, err := s.RemoveFromHash("other", "c9e15763f722f23e98a29decdfae341b98d53056")
	require.NoError(err)
	require.False(removed)

	removed, err = s.RemoveFromHash("route1", "c9e15763f722f23e98a29decdfae341b98d53056")
	require.NoError(err)
	require.True(removed)

	l, err = s.ListMagnets()
	require.NoError(err)
	require.Len(l, 1)
	require.Len(l["route2"], 1)
	require.Equal(l["route2"][0], m1)

	lp, err := s.ListTorrentPaths()
	require.NoError(err)
	require.Nil(lp)

	require.NoError(s.Close())
	require.NoError(cs.Close())
}

func TestDB_Links(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "db-links")
	require.NoError(err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	s, err := NewDB(tmpDir)
	require.NoError(err)

	// Add links
	err = s.AddLink("old/path1", "new/path1")
	require.NoError(err)
	err = s.AddLink("old/path2", "new/path2")
	require.NoError(err)

	// List links
	links, err := s.ListLinks()
	require.NoError(err)
	require.Len(links, 2)
	require.Equal("new/path1", links["old/path1"])
	require.Equal("new/path2", links["old/path2"])

	// Remove link
	err = s.RemoveLink("new/path1") // The targetPath is the NEW path (the key)
	require.NoError(err)

	links, err = s.ListLinks()
	require.NoError(err)
	require.Len(links, 1)
	require.NotContains(links, "old/path1")
	require.Contains(links, "old/path2")

	_ = s.Close()
}
