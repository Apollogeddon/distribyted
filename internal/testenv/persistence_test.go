package testenv

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	dtorrent "github.com/Apollogeddon/distribyted/torrent"
	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBehavior_Persistence_MagnetsAndLinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persistence test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	content := []byte("persistent data")
	magnet, err := seeder.AddFile("persist_behavior.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	workDir, err := os.MkdirTemp("", "persist-test-app")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(workDir) }()

	// --- SESSION 1: Add magnet and hard link, then close ---
	t.Log("--- STARTING SESSION 1 ---")
	{
		app := setupAppWithDir(t, workDir)
		app.KeepTempDir = true

		tMagnet, _ := app.Client.AddMagnet(magnet.String())
		host, port, _ := net.SplitHostPort(seeder.PeerAddr())
		var p uint16
		_, _ = fmt.Sscanf(port, "%d", &p)
		tMagnet.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})

		require.NoError(t, app.Service.AddMagnet("unique-p-route", magnet.String()))
		waitForFile(t, app, "/unique-p-route/persist_behavior.txt")

		// Ensure piece data is fully written to the filecache before close so
		// session 2 can read without re-downloading.
		rawTor, _ := app.Client.Torrent(magnet.InfoHash)
		if rawTor != nil {
			// Without DownloadAll, anacrolix waits for a reader to set piece priorities.
			rawTor.DownloadAll()
			for i := 0; i < 150; i++ {
				if rawTor.Stats().PiecesComplete > 0 {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			require.Greater(t, rawTor.Stats().PiecesComplete, 0, "Session 1: piece must complete before close")
		}

		require.NoError(t, app.Service.AddLink("/unique-p-route/persist_behavior.txt", "/unique-manual-link.txt"))

		err = os.WriteFile(filepath.Join(workDir, "session1_marker.txt"), []byte("session1"), 0644)
		require.NoError(t, err)

		mags, err := app.db.ListMagnets()
		require.NoError(t, err)
		t.Logf("Session 1: found %d routes in DB before close", len(mags))

		links, err := app.db.ListLinks()
		require.NoError(t, err)
		t.Logf("Session 1: found %d links in DB before close", len(links))

		app.Close()
	}
	t.Log("--- SESSION 1 CLOSED ---")

	time.Sleep(1 * time.Second)

	dbPath := filepath.Join(workDir, "magnetdb")
	files, err := os.ReadDir(dbPath)
	require.NoError(t, err, "magnetdb dir should exist after session 1")
	t.Logf("Files in magnetdb after Session 1: %d", len(files))
	for _, f := range files {
		t.Logf("  - %s", f.Name())
	}

	// --- SESSION 2: Reopen and verify data was restored from DB ---
	t.Log("--- STARTING SESSION 2 ---")
	{
		app := setupAppWithDir(t, workDir)
		defer app.Close()

		// Wait for the background loader to restore the torrent from DB before
		// proactively adding the seeder peer (avoids tracker-only discovery).
		for i := 0; i < 100; i++ {
			rawTor, ok := app.Client.Torrent(magnet.InfoHash)
			if ok {
				host, port, _ := net.SplitHostPort(seeder.PeerAddr())
				var p uint16
				_, _ = fmt.Sscanf(port, "%d", &p)
				rawTor.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		waitForFile(t, app, "/unique-p-route/persist_behavior.txt")
		data, err := readFile(t, app, "/unique-p-route/persist_behavior.txt")
		require.NoError(t, err)
		assert.Equal(t, content, data)

		waitForFile(t, app, "/unique-manual-link.txt")
		linkData, err := readFile(t, app, "/unique-manual-link.txt")
		require.NoError(t, err)
		assert.Equal(t, content, linkData)
	}
	t.Log("--- SESSION 2 CLOSED ---")
}

func TestBehavior_Persistence_Pieces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping piece persistence test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 3 MB to ensure multiple pieces (default piece size 256 KB → ~12 pieces)
	content := make([]byte, 3*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	magnet, err := seeder.AddFile("persist_pieces.bin", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	workDir, err := os.MkdirTemp("", "persist-pieces-app")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(workDir) }()

	// --- SESSION 1: Download the first 1 MB and record piece count ---
	t.Log("--- STARTING SESSION 1 ---")
	var piecesCompleteBefore int
	{
		app := setupAppWithDir(t, workDir)
		app.KeepTempDir = true

		tMagnet, _ := app.Client.AddMagnet(magnet.String())
		host, port, _ := net.SplitHostPort(seeder.PeerAddr())
		var p uint16
		_, _ = fmt.Sscanf(port, "%d", &p)
		tMagnet.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})

		require.NoError(t, app.Service.AddMagnet("p-route", magnet.String()))
		waitForFile(t, app, "/p-route/persist_pieces.bin")

		lt, _ := app.Client.Torrent(magnet.InfoHash)
		require.NotNil(t, lt)

		// Wait for at least 4 pieces (1 MB worth at 256 KB/piece) to complete.
		// Using stats avoids a filecache race: MarkComplete is called before the
		// piece file is guaranteed to be fully flushed, which causes unexpected EOF
		// when we read immediately through the VFS under -race.
		lt.DownloadAll()
		for i := 0; i < 150; i++ {
			piecesCompleteBefore = lt.Stats().PiecesComplete
			if piecesCompleteBefore >= 4 {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Logf("Session 1: pieces complete: %d", piecesCompleteBefore)
		require.GreaterOrEqual(t, piecesCompleteBefore, 4, "Should have completed at least 4 pieces (1 MB)")

		// Allow the piece completion DB to flush before close
		time.Sleep(2 * time.Second)

		app.Close()
	}

	// Let the OS release any file locks before session 2 opens the same DB
	time.Sleep(5 * time.Second)

	// --- SESSION 2: Reopen and verify piece completion was preserved ---
	t.Log("--- STARTING SESSION 2 ---")
	{
		app := setupAppWithDir(t, workDir)
		defer app.Close()

		var tor *torrent.Torrent
		var ok bool
		for i := 0; i < 100; i++ {
			tor, ok = app.Client.Torrent(magnet.InfoHash)
			if ok {
				host, port, _ := net.SplitHostPort(seeder.PeerAddr())
				var p uint16
				_, _ = fmt.Sscanf(port, "%d", &p)
				tor.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		require.True(t, ok, "Torrent should have been restored from DB")

		lt := dtorrent.TorrentWrapper{Torrent: tor}
		select {
		case <-lt.GotInfo():
		case <-time.After(20 * time.Second):
			t.Fatal("Timeout waiting for torrent info in Session 2")
		}

		var piecesCompleteAfter int
		for i := 0; i < 50; i++ {
			piecesCompleteAfter = lt.Stats().PiecesComplete
			if piecesCompleteAfter >= piecesCompleteBefore {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		t.Logf("Session 2: pieces complete: %d", piecesCompleteAfter)
		assert.GreaterOrEqual(t, piecesCompleteAfter, piecesCompleteBefore, "Session 2 should have at least as many complete pieces as Session 1")
	}
}

func setupAppWithDir(t *testing.T, dir string) *TestApp {
	app, err := NewTestAppWithDir(dir)
	require.NoError(t, err)
	return app
}

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

func readFile(t *testing.T, app *TestApp, path string) ([]byte, error) {
	f, err := app.FS.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
