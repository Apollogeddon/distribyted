package testenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	dhttp "github.com/Apollogeddon/distribyted/internal/http"
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
	require.NoError(t, app.FS.Link(origPath, linkPath))
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

// TestRegression_LinksAPIEndToEnd exercises the /api/links HTTP endpoints
// (not the internal Service/ContainerFs methods directly) end-to-end: create
// a link, list it, delete it, and confirm the same last-reference cascade
// from TestRegression_HardlinkDeleteCascadesTorrentRemoval still fires when
// driven through the real HTTP handlers. This is also what would have caught
// testenv's link-callback wiring being backwards relative to production,
// since a wrong direction would leave AddLink never actually persisting.
func TestRegression_LinksAPIEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	content := []byte("links api end-to-end test data")
	magnet, err := seeder.AddFile("show.mkv", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	route := "downloads"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	origPath := "/" + route + "/show.mkv"
	require.Eventually(t, func() bool {
		f, err := app.FS.Open(origPath)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}, 15*time.Second, 200*time.Millisecond, "torrent file did not appear in route")

	client, err := app.HTTPClient()
	require.NoError(t, err)
	base := "http://" + app.HTTPAddr

	// Create the link via the real HTTP handler.
	newPath := "/library/Show S01E01.mkv"
	body, err := json.Marshal(dhttp.LinkAdd{OldPath: origPath, NewPath: newPath})
	require.NoError(t, err)
	addResp, err := client.Post(base+"/api/links", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, addResp.StatusCode)
	addResp.Body.Close()

	// This is the assertion that would catch backwards wiring: AddLink must
	// have actually persisted to the DB, not just updated the live tree.
	// loader.DB.ListLinks strips the leading "/" from its map keys (a
	// pre-existing quirk of how it parses its storage-key prefix; the HTTP
	// layer compensates for it, see internal/http/api.go's normalizeLinkPath),
	// so check against that raw form here rather than newPath directly.
	links, err := app.Service.ListLinks()
	require.NoError(t, err)
	require.Equal(t, origPath, links[strings.TrimPrefix(newPath, "/")], "link was not persisted via Service.AddLink")

	// The link must also be live in the tree, readable through the mount.
	f, err := app.FS.Open(newPath)
	require.NoError(t, err)
	_ = f.Close()

	// GET /api/links must show it.
	listResp, err := client.Get(base + "/api/links")
	require.NoError(t, err)
	var got []dhttp.Link
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&got))
	listResp.Body.Close()
	require.Len(t, got, 1)
	require.Equal(t, newPath, got[0].NewPath)
	require.Equal(t, origPath, got[0].OldPath)

	// Delete it via the real HTTP handler (path segments percent-encoded,
	// mirroring links.js's URL construction).
	segments := strings.Split(strings.TrimPrefix(newPath, "/"), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	delURL := base + "/api/links/" + strings.Join(segments, "/")
	delReq, err := http.NewRequest(http.MethodDelete, delURL, nil)
	require.NoError(t, err)
	delResp, err := client.Do(delReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, delResp.StatusCode)
	delResp.Body.Close()

	// Same cascade assertions as the direct-FS hardlink-delete regression:
	// this was the last reference, so the torrent must be fully torn down.
	require.Eventually(t, func() bool {
		_, ok := app.Client.Torrent(magnet.InfoHash)
		return !ok
	}, 5*time.Second, 100*time.Millisecond, "torrent was not dropped from the client after its last reference was deleted via the API")

	require.Empty(t, app.Stats.GetRouteFromHash(magnet.InfoHash.HexString()), "torrent stats should be removed after cascade")

	linksAfter, err := app.Service.ListLinks()
	require.NoError(t, err)
	require.NotContains(t, linksAfter, strings.TrimPrefix(newPath, "/"), "link record should be gone from the DB")
}

// TestRegression_TorrentDeleteRemovesLinkRecords is the end-to-end proof
// that the reported bug ("can't delete a link once its file is gone") can't
// recur: deleting a torrent must remove every link record pointing into it
// immediately, not just from the live tree — and that removal must survive
// a restart, proving it was actually written to the on-disk DB and not just
// cleared from the in-memory ContainerFs. Covers two links onto the same
// torrent (multiple links share a hash) and a link nested two directories
// deep (exercises the empty-parent-directory pruning path).
func TestRegression_TorrentDeleteRemovesLinkRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	content := []byte("torrent delete removes link records test data")
	magnet, err := seeder.AddFile("movie.mkv", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// A real on-disk DB (not testenv's default in-memory one) so the
	// restart check below actually proves persistence.
	tempDir, err := os.MkdirTemp("", "distribyted-test-orphan-links")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	app, err := NewTestAppWithDir(tempDir)
	require.NoError(t, err)
	app.KeepTempDir = true

	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	route := "downloads"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	origPath := "/" + route + "/movie.mkv"
	require.Eventually(t, func() bool {
		f, err := app.FS.Open(origPath)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}, 15*time.Second, 200*time.Millisecond, "torrent file did not appear in route")

	flatLink := "/library/movie.mkv"
	nestedLink := "/library2/a/b/movie.mkv"
	require.NoError(t, app.FS.Link(origPath, flatLink))
	require.NoError(t, app.FS.Link(origPath, nestedLink))

	links, err := app.Service.ListLinks()
	require.NoError(t, err)
	require.Len(t, links, 2, "both links should be persisted before the delete")

	require.NoError(t, app.Service.RemoveFromHash(route, magnet.InfoHash.HexString()))

	// The cascade must remove both link records immediately, not just the
	// live tree entries — this is what would have 500'd forever pre-fix.
	require.Eventually(t, func() bool {
		links, err := app.Service.ListLinks()
		return err == nil && len(links) == 0
	}, 5*time.Second, 100*time.Millisecond, "link records were not removed by the torrent-delete cascade")

	app.Close()

	// Reopen against the same on-disk DB to prove the records were actually
	// deleted from BoltDB, not just cleared from the in-memory tree.
	app2, err := NewTestAppWithDir(tempDir)
	require.NoError(t, err)
	defer app2.Close()

	linksAfterRestart, err := app2.Service.ListLinks()
	require.NoError(t, err)
	require.Empty(t, linksAfterRestart, "link records reappeared after restart — cascade did not persist to the DB")
}
