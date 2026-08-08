package testenv

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/stretchr/testify/require"
)

// TestWebseedDropPanic_Repro investigates a suspected panic hazard in
// anacrolix/torrent v1.61.0, found while planning webseed-prioritization
// work (not built, and not needed — see docs/benchmarking.md and the
// webseed-prioritization plan): Torrent.close does not remove that
// torrent's entries from Client.activeWebseedRequests (torrent.go's own
// disabled assertion says as much: "This doesn't work yet because requests
// remove themselves after they close, and we don't remove them
// synchronously"), and the 5s webseed-request-update timer
// (webseed-requesting.go) asserts panicif.True(elem.t.closed.IsSet()) on
// any stale entry it finds. Service.RemoveFromHash calls Torrent.Drop() on
// a user-triggered, production-reachable path (delete a torrent from the
// dashboard/API), so if reachable this would be a live crash risk
// independent of whether webseed prioritization is ever built.
//
// FINDING (verified, not just reasoned about): does not reproduce. Two
// attempts, both against a real anacrolix/torrent Client and a real HTTP
// webseed server, no mocks:
//  1. 941 rapid add/AddWebSeeds/DownloadAll/Drop cycles over 20s (~15ms
//     apart) — the version originally tried here, before realizing the
//     library only schedules webseed requests from its periodic 5s timer
//     (nothing triggers one immediately on DownloadAll), so most of those
//     cycles likely never had a request in flight to race at all.
//  2. 4 cycles with the webseed HTTP handler holding the request open
//     until the test observes it has actually started (a channel send from
//     inside the handler, not a sleep-and-hope), then calling Drop()
//     immediately and waiting for the held-open response to land afterward
//     — a deterministic in-flight overlap, not timing luck.
//
// Neither attempt triggered the assertion. Read literally, the code still
// looks capable of it (see the disabled-assertion comment above), but
// whatever actually cleans up webseed-peer state on Torrent.close appears
// to win the race reliably enough in v1.61.0 that this isn't a practical
// hazard — at least not via the direct Drop()-during-request-shape tried
// here. Not escalated further (no upstream issue filed): a ~1000-cycle,
// two-strategy negative result doesn't prove the assertion is dead code,
// but it doesn't justify chasing an unreproduced hazard either. Revisit if
// a real webseed-adjacent panic ever surfaces in production logs.
//
// Kept in the repo (skipped by default, run explicitly) as a ready-to-use
// repro harness for that case:
//
//	go test ./internal/testenv/ -run TestWebseedDropPanic_Repro -v -timeout 60s
func TestWebseedDropPanic_Repro(t *testing.T) {
	if testing.Short() {
		t.Skip("manual repro attempt, not part of the regular suite")
	}

	content := make([]byte, 8*1024*1024) // several pieces worth, room for concurrent requests
	for i := range content {
		content[i] = byte(i)
	}

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "repro.bin")
	require.NoError(t, os.WriteFile(srcFile, content, 0644))

	// The handler blocks until requestStarted fires, so the test can wait
	// for genuine proof a request is in flight (rather than guessing at a
	// delay) before calling Drop — removing timing luck from the repro.
	var hits atomic.Int64
	requestStarted := make(chan struct{}, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		time.Sleep(3 * time.Second) // hold the request open well past Drop()
		http.ServeContent(w, r, "repro.bin", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	info := metainfo.Info{PieceLength: 256 * 1024}
	require.NoError(t, info.BuildFromFilePath(srcFile))
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	ih := infohash.HashBytes(infoBytes)

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true

	c, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	defer c.Close()

	// anacrolix/torrent only schedules webseed requests from a periodic
	// timer (webseedRequestUpdateTimerInterval = 5s in client.go) — nothing
	// triggers one immediately on AddWebSeeds/DownloadAll (onNeedUpdateRequests
	// is a deliberate no-op, webseed-peer.go). So waiting for the first
	// server hit (not just sleeping) is what proves a request is live.
	const cycles = 4
	for i := 0; i < cycles; i++ {
		to, isNew := c.AddTorrentOpt(torrent.AddTorrentOpts{
			InfoHash: ih,
			Storage:  storage.NewFile(t.TempDir()),
		})
		require.True(t, isNew)
		require.NoError(t, to.SetInfoBytes(infoBytes))
		to.AddWebSeeds([]string{srv.URL + "/"})
		to.DownloadAll()

		select {
		case <-requestStarted:
			// Confirmed in flight (blocked in the handler's 3s sleep) —
			// drop immediately, guaranteeing overlap instead of hoping for it.
			to.Drop()
			t.Logf("cycle %d: dropped mid-request, total hits so far = %d", i, hits.Load())
		case <-time.After(10 * time.Second):
			t.Fatalf("cycle %d: no webseed request started within 10s", i)
		}

		time.Sleep(3500 * time.Millisecond) // let the blocked handler finish and the client process/reject the late response after Drop
	}
}
