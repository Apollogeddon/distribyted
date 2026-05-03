package torrent

import (
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/Apollogeddon/distribyted/torrent/loader"
)

type MockLoader struct {
	Magnets      map[string][]string
	TorrentPaths map[string][]string
}

func (m *MockLoader) ListMagnets() (map[string][]string, error) {
	return m.Magnets, nil
}
func (m *MockLoader) ListTorrentPaths() (map[string][]string, error) {
	return m.TorrentPaths, nil
}

type MockLoaderAdder struct {
	MockLoader
	mu           sync.Mutex
	Links        map[string]string
	AddedMagnets map[string]string
}

func (m *MockLoaderAdder) AddMagnet(r, magnet string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AddedMagnets == nil {
		m.AddedMagnets = make(map[string]string)
	}
	m.AddedMagnets[r] = magnet
	return nil
}
func (m *MockLoaderAdder) ListLinks() (map[string]string, error) {
	return m.Links, nil
}
func (m *MockLoaderAdder) AddLink(oldpath, newpath string) error {
	return nil
}
func (m *MockLoaderAdder) RemoveLink(path string) error {
	return nil
}
func (m *MockLoaderAdder) RemoveFromHash(r, h string) (bool, error) {
	return true, nil
}

func TestService_Load_Full(t *testing.T) {
	stats := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	magnet := "magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4"

	mockT := &mockTorrent{
		hash:    hash,
		gotInfo: make(chan struct{}),
	}
	close(mockT.gotInfo)

	mockC := &mockTorrentClient{
		addMagnetFunc: func(s string) (fs.Torrent, error) {
			return mockT, nil
		},
	}

	ml := &MockLoaderAdder{
		MockLoader: MockLoader{
			Magnets: map[string][]string{"r1": {magnet}},
		},
		Links: map[string]string{"o1": "n1"},
	}

	svc := NewService(nil, ml, stats, mockC, 1, 1, true)

	fss, err := svc.Load()
	require.NoError(t, err)
	require.Contains(t, fss, "/r1")
}

func TestService_Load(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.DisableWebseeds = true

	client, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	stats := NewStats()

	l1 := &MockLoader{
		Magnets: map[string][]string{
			"route1": {"magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4"},
		},
	}

	db := &MockLoaderAdder{
		MockLoader: MockLoader{
			Magnets: map[string][]string{
				"route2": {"magnet:?xt=urn:btih:3b5b68df56502da2f1fb89eb5b3cba14d3345c2f"},
			},
		},
	}

	svc := NewService([]loader.Loader{l1}, db, stats, &ClientWrapper{client}, 1, 1, true)

	fss, err := svc.Load()
	require.NoError(t, err)

	require.Contains(t, fss, "/route1")
	require.Contains(t, fss, "/route2")

	// wait for torrents to be added properly and info to be ignored due to timeout
	time.Sleep(2 * time.Second)

	require.Len(t, svc.fss, 2)
}

func TestService_addTorrent_Timeout(t *testing.T) {
	stats := NewStats()
	gotInfo := make(chan struct{})
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	mockT := &mockTorrent{
		hash:    hash,
		gotInfo: gotInfo,
	}

	mockC := &mockTorrentClient{
		addMagnetFunc: func(s string) (fs.Torrent, error) {
			return mockT, nil
		},
	}

	svc := NewService(nil, &MockLoaderAdder{}, stats, mockC, 1, 1, true)

	// This should not return error even if it timeouts
	err := svc.addMagnet("test", "magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	require.NoError(t, err)
}

func TestService_RemoveFromHash(t *testing.T) {
	stats := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	mockT := &mockTorrent{
		hash:    hash,
		name:    "test",
		gotInfo: make(chan struct{}),
	}
	close(mockT.gotInfo)

	mockC := &mockTorrentClient{
		torrentFunc: func(h metainfo.Hash) (fs.Torrent, bool) {
			if h == hash {
				return mockT, true
			}
			return nil, false
		},
		addMagnetFunc: func(s string) (fs.Torrent, error) {
			return mockT, nil
		},
	}

	db := &MockLoaderAdder{}
	svc := NewService(nil, db, stats, mockC, 1, 1, true)

	err := svc.AddMagnet("route1", "magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	require.NoError(t, err)

	err = svc.RemoveFromHash("route1", hash.HexString())
	require.NoError(t, err)
}

func TestService_Listeners(t *testing.T) {
	stats := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	mockT := &mockTorrent{
		hash:    hash,
		gotInfo: make(chan struct{}),
	}
	close(mockT.gotInfo)

	mockC := &mockTorrentClient{
		addMagnetFunc: func(s string) (fs.Torrent, error) {
			return mockT, nil
		},
		torrentFunc: func(h metainfo.Hash) (fs.Torrent, bool) {
			return mockT, true
		},
	}

	svc := NewService(nil, &MockLoaderAdder{}, stats, mockC, 1, 1, true)

	routeAddedCalled := false
	svc.OnRouteAdded(func(r string, f fs.Filesystem) {
		routeAddedCalled = true
	})

	torrentRemovedCalled := false
	svc.OnTorrentRemoved(func(h string) {
		torrentRemovedCalled = true
	})

	err := svc.AddMagnet("route1", "magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	require.NoError(t, err)
	require.True(t, routeAddedCalled)

	err = svc.RemoveFromHash("route1", hash.HexString())
	require.NoError(t, err)
	require.True(t, torrentRemovedCalled)
}

func TestService_AddLink(t *testing.T) {
	db := &MockLoaderAdder{}
	svc := NewService(nil, db, nil, nil, 1, 1, true)

	err := svc.AddLink("old", "new")
	require.NoError(t, err)

	err = svc.RemoveLink("new")
	require.NoError(t, err)

	_, err = svc.ListLinks()
	require.NoError(t, err)
}

func TestService_PublicMethods(t *testing.T) {
	stats := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	mockT := &mockTorrent{hash: hash}
	mockC := &mockTorrentClient{
		torrentFunc: func(h metainfo.Hash) (fs.Torrent, bool) {
			return mockT, true
		},
		addTorrentFromFileFunc: func(path string) (fs.Torrent, error) {
			return mockT, nil
		},
	}

	svc := NewService(nil, &MockLoaderAdder{}, stats, mockC, 1, 1, true)

	// Test AddTorrentFromFile
	err := svc.AddTorrentFromFile("r1", "p1")
	require.NoError(t, err)

	// Test Torrent
	tor, ok := svc.Torrent(hash.HexString())
	require.True(t, ok)
	require.Equal(t, mockT, tor)

	_, ok = svc.Torrent("invalid")
	require.False(t, ok)

	// Test Close
	svc.Close()
}

func TestService_LinkCallbacks(t *testing.T) {
	svc := NewService(nil, &MockLoaderAdder{}, nil, nil, 1, 1, true)

	addedOld, addedNew := "", ""
	svc.OnLinkAdded(func(o, n string) {
		addedOld, addedNew = o, n
	})

	removedPath := ""
	svc.OnLinkRemoved(func(p string) {
		removedPath = p
	})

	_ = svc.AddLink("o", "n")
	require.Equal(t, "/o", addedOld)
	require.Equal(t, "/n", addedNew)

	_ = svc.RemoveLink("n")
	require.Equal(t, "n", removedPath)
}

func TestService_RemoveFromHashOnly(t *testing.T) {
	stats := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	mockT := &mockTorrent{
		hash:    hash,
		gotInfo: make(chan struct{}),
	}
	close(mockT.gotInfo)

	mockC := &mockTorrentClient{
		addMagnetFunc: func(s string) (fs.Torrent, error) {
			return mockT, nil
		},
		torrentFunc: func(h metainfo.Hash) (fs.Torrent, bool) {
			return mockT, true
		},
	}

	svc := NewService(nil, &MockLoaderAdder{}, stats, mockC, 1, 1, true)

	// Should fail if not added yet
	err := svc.RemoveFromHashOnly(hash.HexString())
	require.Error(t, err)

	_ = svc.AddMagnet("route1", "magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	err = svc.RemoveFromHashOnly(hash.HexString())
	require.NoError(t, err)
}

func TestService_ConcurrentMagnetAdds(t *testing.T) {
	stats := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	magnet := "magnet:?xt=urn:btih:e3b0c44298fc1c149afbf4c8996fb92427ae41e4"

	mockT := &mockTorrent{
		hash:    hash,
		gotInfo: make(chan struct{}),
	}
	close(mockT.gotInfo)

	mockC := &mockTorrentClient{
		addMagnetFunc: func(s string) (fs.Torrent, error) {
			return mockT, nil
		},
		torrentFunc: func(h metainfo.Hash) (fs.Torrent, bool) {
			return mockT, true
		},
	}

	db := &MockLoaderAdder{}
	svc := NewService(nil, db, stats, mockC, 1, 1, true)

	errCh := make(chan error, 100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			route := "route" // Add to same route
			if idx%2 == 0 {
				route = "route2" // Or multiple routes
			}
			errCh <- svc.AddMagnet(route, magnet)
		}(i)
	}

	for i := 0; i < 100; i++ {
		err := <-errCh
		require.NoError(t, err)
	}

	// Should have 2 routes in the DB
	require.Len(t, db.AddedMagnets, 2)
}
