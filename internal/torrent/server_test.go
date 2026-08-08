package torrent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
)

func TestServer_StartAndWatch(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true

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
		Name:            "test-server",
		Path:            serverFolder,
		WatcherInterval: 1,
	}

	srv := NewServer(client, pc, serverCfg)

	err = srv.Start()
	require.NoError(t, err)
	defer func() { _ = srv.Close() }()

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

	// wait for watcher to trigger magnet recreation
	time.Sleep(2 * time.Second)

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

func TestServer_WalkFuncSkipsErrors(t *testing.T) {
	srv := NewServer(nil, nil, &config.Server{Name: "test-server"})

	w, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer func() { _ = w.Close() }()

	walkFn := srv.watchFolderWalkFunc(w)

	// info is nil whenever err is set; this must not panic and must keep
	// scanning (return nil), not abort the whole walk.
	require.NotPanics(t, func() {
		err := walkFn("/nope", nil, errors.New("permission denied"))
		require.NoError(t, err)
	})
}

func TestServer_Start_UnreadableSubdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}

	serverFolder := t.TempDir()

	readableFile := filepath.Join(serverFolder, "readable.txt")
	require.NoError(t, os.WriteFile(readableFile, []byte("hello"), 0644))

	noAccessDir := filepath.Join(serverFolder, "noaccess")
	require.NoError(t, os.Mkdir(noAccessDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(noAccessDir, "hidden.txt"), []byte("secret"), 0644))
	require.NoError(t, os.Chmod(noAccessDir, 0000))
	t.Cleanup(func() { _ = os.Chmod(noAccessDir, 0755) })

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true

	client, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	pc := storage.NewMapPieceCompletion()

	srv := NewServer(client, pc, &config.Server{Name: "test-server", Path: serverFolder})

	// This is the exact bug: filepath.Walk passes a nil info alongside a
	// non-nil err for the unreadable subdir, and the old code called
	// info.Mode() before checking err, panicking here and never returning
	// from Start(). Magnet generation itself still fails separately (the
	// third-party BuildFromFilePath can't read the subdir either), which is
	// expected and not what this test is about.
	require.NoError(t, srv.Start())
	defer func() { _ = srv.Close() }()

	require.Eventually(t, func() bool {
		return srv.Info().State != ""
	}, 5*time.Second, 100*time.Millisecond, "server should reach a terminal state without crashing")
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
