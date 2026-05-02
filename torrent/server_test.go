package torrent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
)

func TestServer_StartAndWatch(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableWebseeds = true

	client, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	pc := storage.NewMapPieceCompletion()

	serverFolder := t.TempDir()

	// create a dummy file
	dummyFile := filepath.Join(serverFolder, "dummy.txt")
	err = os.WriteFile(dummyFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	serverCfg := &config.Server{
		Name: "test-server",
		Path: serverFolder,
	}

	srv := NewServer(client, pc, serverCfg)

	err = srv.Start()
	require.NoError(t, err)
	defer srv.Close()

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	info := srv.Info()
	firstMagnet := info.Magnet
	require.Equal(t, "test-server", info.Name)
	require.Equal(t, serverFolder, info.Folder)
	require.NotEmpty(t, firstMagnet)
	require.Equal(t, SEEDING.String(), info.State)

	// test fsnotify by adding a new file
	dummyFile2 := filepath.Join(serverFolder, "dummy2.txt")
	err = os.WriteFile(dummyFile2, []byte("hello world 2"), 0644)
	require.NoError(t, err)

	// wait for watcher to trigger magnet recreation (runs every 5 seconds)
	time.Sleep(6 * time.Second)

	info2 := srv.Info()
	require.NotEqual(t, firstMagnet, info2.Magnet, "magnet should be updated after new file")
}

func TestServer_Trackers(t *testing.T) {
	srv := &Server{cfg: &config.Server{Trackers: []string{"udp://tracker.com:80"}}}
	require.Equal(t, []string{"udp://tracker.com:80"}, srv.trackers())
}

func TestServer_CloseNil(t *testing.T) {
	srv := &Server{}
	require.NoError(t, srv.Close())
}

func TestServer_Start_InvalidPath(t *testing.T) {
	// Create a file and try to use it as a base directory to guarantee MkdirAll fails
	dummyFile := filepath.Join(t.TempDir(), "dummy")
	require.NoError(t, os.WriteFile(dummyFile, []byte("test"), 0644))
	invalidPath := filepath.Join(dummyFile, "nested")

	srv := NewServer(nil, nil, &config.Server{Path: invalidPath})
	err := srv.Start()
	require.Error(t, err)
}
