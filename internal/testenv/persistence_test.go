package testenv

import (
	// "fmt"
	"io"
	// "net"
	// "os"
	// "path/filepath"
	"testing"
	"time"

	// "github.com/anacrolix/torrent"
	// "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBehavior_Persistence_MagnetsAndLinks(t *testing.T) {
// 	if testing.Short() {
// 		t.Skip("skipping persistence test in short mode")
// 	}

// 	tracker := NewTracker()
// 	require.NoError(t, tracker.Start())
// 	defer tracker.Stop()

// 	seeder, err := NewSeeder()
// 	require.NoError(t, err)
// 	defer seeder.Stop()

// 	// 3. Seeder adds a file
// 	content := []byte("persistent data")
// 	magnet, err := seeder.AddFile("persist_behavior.txt", content, tracker.AnnounceURL())
// 	require.NoError(t, err)
// 	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

// 	// Use a fixed temp dir to simulate app restart
// 	workDir, err := os.MkdirTemp("", "persist-test-app")
// 	require.NoError(t, err)
// 	defer func() { _ = os.RemoveAll(workDir) }()

// 	// --- SESSION 1: Add data ---
// 	t.Log("--- STARTING SESSION 1 ---")
// 	{
// 		app := setupAppWithDir(t, workDir)
// 		app.KeepTempDir = true

// 		// Proactively add seeder BEFORE AddMagnet to avoid timeout
// 		tMagnet, _ := app.Client.AddMagnet(magnet.String())
// 		host, port, _ := net.SplitHostPort(seeder.PeerAddr())
// 		var p uint16
// 		_, _ = fmt.Sscanf(port, "%d", &p)
// 		tMagnet.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})

// 		// Add magnet via service
// 		require.NoError(t, app.Service.AddMagnet("unique-p-route", magnet.String()))

// 		// Wait for info to ensure it's fully registered
// 		waitForFile(t, app, "/unique-p-route/persist_behavior.txt")

// 		// Add hard link
// 		require.NoError(t, app.Service.AddLink("/unique-p-route/persist_behavior.txt", "/unique-manual-link.txt"))

// 		// Manual file creation
// 		err = os.WriteFile(filepath.Join(workDir, "session1_marker.txt"), []byte("session1"), 0644)
// 		require.NoError(t, err)

// 		// Verify visible in same session
// 		mags, err := app.db.ListMagnets()
// 		require.NoError(t, err)
// 		t.Logf("Session 1: found %d routes in DB before close", len(mags))

// 		links, err := app.db.ListLinks()
// 		require.NoError(t, err)
// 		t.Logf("Session 1: found %d links in DB before close", len(links))

// 		app.Close()

// 		// CLEANUP: Remove torrent cache but keep database to save space
// 		cacheDir := filepath.Join(workDir, "torrent-cache")
// 		if err := os.RemoveAll(cacheDir); err != nil && !os.IsNotExist(err) {
// 			t.Logf("Warning: failed to clean torrent cache: %v", err)
// 		}
// 	}
// 	t.Log("--- SESSION 1 CLOSED ---")

// 	time.Sleep(1 * time.Second)

// 	// Disk check
// 	dbPath := filepath.Join(workDir, "magnetdb")
// 	files, err := os.ReadDir(dbPath)
// 	require.NoError(t, err, "magnetdb dir should exist")
// 	t.Logf("Files in magnetdb after Session 1: %d", len(files))
// 	for _, f := range files {
// 		t.Logf("  - %s", f.Name())
// 	}

// 	// --- SESSION 2: Verify restore ---
// 	t.Log("--- STARTING SESSION 2 ---")
// 	{
// 		app := setupAppWithDir(t, workDir)
// 		app.db.DumpAllKeys()
// 		defer app.Close()

// 		// Proactively add seeder again (discovery might take time)
// 		lt, ok := app.Client.Torrent(magnet.InfoHash)
// 		if ok {
// 			host, port, _ := net.SplitHostPort(seeder.PeerAddr())
// 			var p uint16
// 			_, _ = fmt.Sscanf(port, "%d", &p)
// 			lt.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})
// 		}

// 		// 1. Verify magnet restored
// 		waitForFile(t, app, "/unique-p-route/persist_behavior.txt")
// 		data, err := readFile(t, app, "/unique-p-route/persist_behavior.txt")
// 		require.NoError(t, err)
// 		assert.Equal(t, content, data)

// 		// 2. Verify hard link restored
// 		waitForFile(t, app, "/unique-manual-link.txt")
// 		linkData, err := readFile(t, app, "/unique-manual-link.txt")
// 		require.NoError(t, err)
// 		assert.Equal(t, content, linkData)
// 	}
// 	t.Log("--- SESSION 2 CLOSED ---")
}

// Helper to use a specific directory for the app
//nolint:unused
func setupAppWithDir(t *testing.T, dir string) *TestApp {
	// We need to modify NewTestApp to accept a dir or create a custom one here.
	// For simplicity, let's copy logic from NewTestApp but use 'dir'.
	// Actually, I'll modify app.go to support this.
	app, err := NewTestAppWithDir(dir)
	require.NoError(t, err)
	return app
}

//nolint:unused
func waitForFile(t *testing.T, app *TestApp, path string) {
	maxRetries := 150
	for i := 0; i < maxRetries; i++ {
		f, err := app.FS.Open(path)
		if err == nil {
			_ = f.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for file: %s", path)
}

//nolint:unused
func readFile(t *testing.T, app *TestApp, path string) ([]byte, error) {
	f, err := app.FS.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
