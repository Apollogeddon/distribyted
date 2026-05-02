package torrent

import (
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"

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
	AddedMagnets map[string]string
}

func (m *MockLoaderAdder) AddMagnet(r, magnet string) error {
	if m.AddedMagnets == nil {
		m.AddedMagnets = make(map[string]string)
	}
	m.AddedMagnets[r] = magnet
	return nil
}
func (m *MockLoaderAdder) ListLinks() (map[string]string, error) {
	return nil, nil
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

func TestService_Load(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
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

	svc := NewService([]loader.Loader{l1}, db, stats, client, 1, 1, true)

	fss, err := svc.Load()
	require.NoError(t, err)

	require.Contains(t, fss, "/route1")
	require.Contains(t, fss, "/route2")

	// wait for torrents to be added properly and info to be ignored due to timeout
	time.Sleep(2 * time.Second)

	require.Len(t, svc.fss, 2)
}
