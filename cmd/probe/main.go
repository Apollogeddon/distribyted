// Command probe is a manual diagnostic tool, not part of the distribyted
// binary: it adds a real magnet over the real internet (real DHT, real
// trackers, no synthetic throttling) using the exact same
// internal/torrent.Service / internal/torrent.Timings / internal/fs.TorrentFS
// wiring production uses, and prints the resulting cold-start timing —
// metadata, first peer, first data, first read — as it happens.
//
// It exists because internal/testenv's benchmark harness is deliberately
// hermetic (loopback Tracker/Seeder, no real DHT) so CI stays fast and
// deterministic — but that means it structurally cannot answer "how long
// does real peer discovery actually take". This can, without needing a
// production deploy. See docs/benchmarking.md's "DHT/tracker peer-discovery
// latency" section for the caveat that matters most: this dev machine's
// network conditions (no VPN, different NAT/egress) are not representative
// of the production host, so treat the numbers as mechanism validation, not
// a production stand-in.
//
// Usage:
//
//	go run ./cmd/probe [-magnet "magnet:?..."] [-timeout 60s]
//	make probe MAGNET="magnet:?..."
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/anacrolix/dht/v2/bep44"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/Apollogeddon/distribyted/internal/torrent/loader"
)

// defaultMagnet is a well-known, non-infringing (Creative Commons) public
// test torrent already used elsewhere in this repo's own test suite (see
// internal/fs/torrent_test.go's testMagnet) — safe to hit real trackers/DHT
// for by default with no args.
const defaultMagnet = "magnet:?xt=urn:btih:a88fda5954e89178c372716a6a78b8180ed4dad3&dn=The+WIRED+CD+-+Rip.+Sample.+Mash.+Share&tr=udp%3A%2F%2Fexplodie.org%3A6969&tr=udp%3A%2F%2Ftracker.coppersurfer.tk%3A6969&tr=udp%3A%2F%2Ftracker.empire-js.us%3A1337&tr=udp%3A%2F%2Ftracker.leechers-paradise.org%3A6969&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337&tr=wss%3A%2F%2Ftracker.btorrent.xyz&tr=wss%3A%2F%2Ftracker.fastcast.nz&tr=wss%3A%2F%2Ftracker.openwebtorrent.com&ws=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2F&xs=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2Fwired-cd.torrent"

// noopBEP44Store avoids opening a persistent DHT item store for a
// short-lived probe run — this only affects BEP44 (arbitrary DHT key/value
// storage), which basic peer discovery (announce/get_peers) doesn't use, so
// it doesn't affect the numbers this tool is measuring.
type noopBEP44Store struct{}

func (noopBEP44Store) Put(*bep44.Item) error                 { return nil }
func (noopBEP44Store) Get(bep44.Target) (*bep44.Item, error) { return nil, bep44.ErrItemNotFound }
func (noopBEP44Store) Del(bep44.Target) error                { return nil }

func main() {
	magnet := flag.String("magnet", defaultMagnet, "magnet URI to probe")
	// Longer than internal/torrent.Timings' own internal watchdog deadline
	// (120s): if this timeout were shorter, the probe would always exit via
	// its own timeout first, and the "torrent cold start" summary line
	// (complete=false, missing=[...]) — the actual structured artifact this
	// tool exists to produce — would never get a chance to fire.
	timeout := flag.Duration("timeout", 150*time.Second, "how long to wait for a full cold start before giving up")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	if err := run(ctx, *magnet, *timeout); err != nil {
		log.Fatal().Err(err).Msg("probe failed")
	}
}

func run(ctx context.Context, magnet string, timeout time.Duration) error {
	tempDir, err := os.MkdirTemp("", "distribyted-probe")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	logDir := filepath.Join(tempDir, "logs")
	dlog.Load(&config.Log{Path: logDir})
	fmt.Printf("structured JSON log (same lines production emits) also written to %s\n\n", filepath.Join(logDir, dlog.FileName)) //nolint:forbidigo // CLI narration to the human running this, not a log statement

	id, err := dtorrent.GetOrCreatePeerID(filepath.Join(tempDir, "ID"))
	if err != nil {
		return fmt.Errorf("creating peer id: %w", err)
	}

	// DisableUPnP: a container/dev machine's router UPnP negotiation can add
	// several seconds of unrelated latency and isn't part of what this tool
	// measures (peer discovery / data transfer), unlike production which
	// wants it on for inbound reachability. Everything else matches
	// production defaults: real DHT, real trackers, no throttling.
	cfg := &config.TorrentGlobal{
		ListenPort:  -1,
		DisableUPnP: true,
	}

	st := storage.NewFile(filepath.Join(tempDir, "data"))
	c, err := dtorrent.NewClient(st, noopBEP44Store{}, cfg, id)
	if err != nil {
		return fmt.Errorf("creating torrent client: %w", err)
	}
	defer c.Close()

	db, err := loader.NewDB(filepath.Join(tempDir, "magnetdb"))
	if err != nil {
		return fmt.Errorf("creating magnet db: %w", err)
	}
	defer db.Close()

	tm := dtorrent.NewTimings()
	defer tm.Close()

	stats := dtorrent.NewStats()
	timeoutSecs := int(timeout.Seconds())
	svc := dtorrent.NewService(nil, db, stats, dtorrent.ClientWrapper{Client: c}, timeoutSecs, timeoutSecs, true, false, tm)
	defer svc.Close()

	var mu sync.Mutex
	var routeFS fs.Filesystem
	svc.OnRouteAdded(func(route string, fsys fs.Filesystem) {
		mu.Lock()
		defer mu.Unlock()
		routeFS = fsys
	})

	fmt.Println("adding magnet, waiting for metadata, a peer, and data — watch the log lines below:") //nolint:forbidigo // CLI narration, not a log statement
	fmt.Println()                                                                                     //nolint:forbidigo // CLI narration, not a log statement

	start := time.Now()
	addDone := make(chan error, 1)
	go func() { addDone <- svc.AddMagnet("probe", magnet) }()

	select {
	case err := <-addDone:
		if err != nil {
			return fmt.Errorf("adding magnet: %w", err)
		}
	case <-ctx.Done():
		return fmt.Errorf("timed out after %s waiting for the magnet to be added", timeout)
	}

	// Trigger a real read as soon as a file shows up, so Timings' "file
	// first read" line — the last stage of the chain — actually fires.
	// Metadata (and therefore the route's files) can still be arriving in
	// the background even after AddMagnet returned, per
	// continue_when_add_timeout semantics, hence the poll rather than a
	// single check.
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	read := false
	for !read {
		select {
		case <-ctx.Done():
			fmt.Println("\ntimed out before any file could be read — see the cold-start log line above for what's missing") //nolint:forbidigo // CLI narration, not a log statement
			return nil
		case <-ticker.C:
			mu.Lock()
			fsys := routeFS
			mu.Unlock()
			if fsys == nil {
				continue
			}
			entries, err := fsys.ReadDir("/")
			if err != nil || len(entries) == 0 {
				continue
			}
			for name := range entries {
				f, err := fsys.Open("/" + name)
				if err != nil {
					continue
				}
				buf := make([]byte, 4096)
				_, err = f.Read(buf)
				_ = f.Close()
				if err == nil {
					read = true
					break
				}
			}
		}
	}

	fmt.Printf("\ndone in %s — full stage-by-stage timing is in the log lines above (and %s)\n", time.Since(start), filepath.Join(logDir, dlog.FileName)) //nolint:forbidigo // CLI narration, not a log statement
	return nil
}
