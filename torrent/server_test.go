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
