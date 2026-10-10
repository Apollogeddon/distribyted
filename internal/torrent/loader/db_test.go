package loader

import (
	"os"
	"testing"

	"github.com/dgraph-io/badger/v4"

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
	require.Equal("old/path1", links["new/path1"])
	require.Equal("old/path2", links["new/path2"])

	// Remove link
	err = s.RemoveLink("new/path1") // The targetPath is the NEW path (the key)
	require.NoError(err)

	links, err = s.ListLinks()
	require.NoError(err)
	require.Len(links, 1)
	require.NotContains(links, "new/path1")
	require.Contains(links, "new/path2")

	_ = s.Close()
}

func TestDB_InvalidRouteNames(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	s, err := NewDB("")
	require.NoError(err)
	defer s.Close()

	// "." made the key /route/<hash>, which crashed every later startup, and "../.."
	// reached into the links store
	for _, r := range []string{".", "..", "../../link/evil", "a/b", ""} {
		require.Error(s.AddMagnet(r, m1), r)
	}

	links, err := s.ListLinks()
	require.NoError(err)
	require.Empty(links)
}

func TestDB_ListMagnetsSkipsMalformedKeys(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	s, err := NewDB("")
	require.NoError(err)
	defer s.Close()

	require.NoError(s.AddMagnet("good", m1))
	// a key an older version could write, with no route segment
	require.NoError(s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(routeRootKey+"/c9e15763f722f23e98a29decdfae341b98d53056"), []byte(m1))
	}))

	magnets, err := s.ListMagnets()
	require.NoError(err)
	require.Equal(map[string][]string{"good": {m1}}, magnets)
}

func TestDB_Info(t *testing.T) {
	dir := t.TempDir()
	db, err := NewDB(dir)
	require.NoError(t, err)

	const h = "c9e15763f722f23e98a29decdfae341b98d53056"
	_, ok := db.LoadInfo(h)
	require.False(t, ok)
	require.NoError(t, db.SaveInfo(h, []byte("d4:name4:filme")))
	require.NoError(t, db.AddMagnet("route1", m1))
	require.NoError(t, db.Close())

	db, err = NewDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	info, ok := db.LoadInfo(h)
	require.True(t, ok, "kept across a restart")
	require.Equal(t, []byte("d4:name4:filme"), info)
	l, err := db.ListMagnets()
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"route1": {m1}}, l, "info isn't listed as a magnet")

	saved, err := db.SavedHashes()
	require.NoError(t, err)
	require.Equal(t, []string{h}, saved)

	require.NoError(t, db.ForgetInfo(h))
	_, ok = db.LoadInfo(h)
	require.False(t, ok)
	saved, err = db.SavedHashes()
	require.NoError(t, err)
	require.Empty(t, saved)
	require.NoError(t, db.ForgetInfo(h), "forgetting twice is fine")
}

func TestDB_Peers(t *testing.T) {
	dir := t.TempDir()
	db, err := NewDB(dir)
	require.NoError(t, err)

	const h = "c9e15763f722f23e98a29decdfae341b98d53056"
	require.Empty(t, db.LoadPeers(h))
	require.NoError(t, db.SavePeers(h, []string{"1.2.3.4:6881", "[2001:db8::1]:51413"}))
	require.NoError(t, db.Close())

	db, err = NewDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, []string{"1.2.3.4:6881", "[2001:db8::1]:51413"}, db.LoadPeers(h), "kept across a restart")
	l, err := db.ListMagnets()
	require.NoError(t, err)
	require.Empty(t, l, "peers aren't listed as magnets")

	saved, err := db.SavedHashes()
	require.NoError(t, err)
	require.Equal(t, []string{h}, saved, "peers alone count as saved")

	require.NoError(t, db.ForgetPeers(h))
	require.Empty(t, db.LoadPeers(h))
}
