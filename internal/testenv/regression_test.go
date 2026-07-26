package testenv

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"
)

func TestRegression_VFS_Concurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 1. Create a large-ish file (5MB)
	contentSize := 5 * 1024 * 1024
	content := make([]byte, contentSize)
	for i := range content {
		content[i] = byte(i % 256)
	}

	magnet, err := seeder.AddFile("stress_test.bin", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// Reduce timeout for test
	app.Config.Torrent.AddTimeout = 10
	app.Service.SetAddTimeout(10)

	route := "stress-route"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	// Manually add peers
	var ttor *torrent.Torrent
	for i := 0; i < 50; i++ {
		ttor, _ = app.Client.Torrent(magnet.InfoHash)
		if ttor != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, ttor, "Torrent did not appear in client")

	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	ttor.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// Wait for info
	vfsPath := "/" + route + "/stress_test.bin"
	waitForFile(t, app, vfsPath)

	// 2. Concurrency stress
	const numGoroutines = 20
	const readsPerGoroutine = 50
	const maxReadSize = 128 * 1024 // 128KB

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*readsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine opens its own handle
			f, err := app.FS.Open(vfsPath)
			if err != nil {
				errCh <- fmt.Errorf("G%d: failed to open: %w", id, err)
				return
			}
			defer func() { _ = f.Close() }()

			r := rand.New(rand.NewSource(int64(id)))

			for j := 0; j < readsPerGoroutine; j++ {
				// Random offset and size
				offset := r.Int63n(int64(contentSize - maxReadSize))
				readSize := r.Intn(maxReadSize) + 1

				buf := make([]byte, readSize)
				n := 0
				for n < readSize {
					nn, err := f.ReadAt(buf[n:], offset+int64(n))
					if err != nil && err != io.EOF {
						errCh <- fmt.Errorf("G%d R%d: read failed at %d: %w", id, j, offset+int64(n), err)
						return
					}
					if nn == 0 {
						errCh <- fmt.Errorf("G%d R%d: read zero bytes at %d", id, j, offset+int64(n))
						return
					}
					n += nn
				}

				expected := content[offset : offset+int64(readSize)]
				if !bytes.Equal(buf, expected) {
					errCh <- fmt.Errorf("G%d R%d: data corruption at %d", id, j, offset)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestRegression_ThunderingHerd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Thundering Herd test on Windows due to known file locking/piece completion stalls")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 1. Create a file
	content := []byte("thundering herd test data")
	magnet, err := seeder.AddFile("thundering.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	app.Config.Torrent.AddTimeout = 10
	app.Service.SetAddTimeout(10)

	route := "thundering-route"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	var ttor *torrent.Torrent
	for i := 0; i < 50; i++ {
		ttor, _ = app.Client.Torrent(magnet.InfoHash)
		if ttor != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, ttor)

	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	ttor.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})

	vfsPath := "/" + route + "/thundering.txt"
	waitForFile(t, app, vfsPath)

	// 2. Thundering herd: 100 goroutines reading the SAME block
	const numGoroutines = 100
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			f, err := app.FS.Open(vfsPath)
			if err != nil {
				errCh <- fmt.Errorf("G%d: open failed: %w", id, err)
				return
			}
			defer func() { _ = f.Close() }()

			buf := make([]byte, len(content))
			n := 0
			for n < len(content) {
				nn, err := f.ReadAt(buf[n:], int64(n))
				if err != nil && err != io.EOF {
					errCh <- fmt.Errorf("G%d: read failed: %w", id, err)
					return
				}
				if nn == 0 {
					errCh <- fmt.Errorf("G%d: read zero bytes", id)
					return
				}
				n += nn
			}

			if !bytes.Equal(buf, content) {
				errCh <- fmt.Errorf("G%d: data corruption", id)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

// TestRegression_HardlinkDeleteCascadesTorrentRemoval reproduces a bug where
// deleting a hard-linked entry (the pattern Radarr/Sonarr use: hardlink the
// download into their library folder, then delete that hardlink on
// upgrade/removal) never tore down the underlying torrent. ContainerFs.Remove
// only removed the DB record for that one path; nothing ever asked whether
// it was the last reference and triggered Service.RemoveFromHashOnly. The
// torrent stayed registered, seeding, and visible in the dashboard forever.
func TestRegression_HardlinkDeleteCascadesTorrentRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	content := []byte("hardlink delete cascade test data")
	magnet, err := seeder.AddFile("movie.mkv", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// Proactively add the seeder as a peer before AddMagnet so metadata
	// arrives immediately instead of waiting on tracker-only discovery.
	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	route := "downloads"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	// Wait for the torrent to appear at its original route path.
	origPath := "/" + route + "/movie.mkv"
	require.Eventually(t, func() bool {
		f, err := app.FS.Open(origPath)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}, 15*time.Second, 200*time.Millisecond, "torrent file did not appear in route")

	// Simulate Radarr hardlinking the download into its library folder.
	linkPath := "/library/Movie (2024)/movie.mkv"
	require.NoError(t, app.Service.AddLink(origPath, linkPath))
	require.Eventually(t, func() bool {
		f, err := app.FS.Open(linkPath)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}, 5*time.Second, 100*time.Millisecond, "link did not appear")

	// Simulate Radarr deleting the hardlinked library entry — the only path
	// it manages, and the only one reachable for deletion via the mount.
	require.NoError(t, app.FS.Remove(linkPath))

	// The torrent must be fully torn down: dropped from the client and gone
	// from stats/routes, not just have its link record removed.
	require.Eventually(t, func() bool {
		_, ok := app.Client.Torrent(magnet.InfoHash)
		return !ok
	}, 5*time.Second, 100*time.Millisecond, "torrent was not dropped from the client after its last reference was deleted")

	require.Empty(t, app.Stats.GetRouteFromHash(magnet.InfoHash.HexString()), "torrent stats should be removed after cascade")
}
