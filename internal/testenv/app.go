package testenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/fs"
	dhttp "github.com/Apollogeddon/distribyted/internal/http"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/Apollogeddon/distribyted/internal/torrent/loader"
	"github.com/Apollogeddon/distribyted/internal/webdav"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/anacrolix/missinggo/v2/filecache"
	atorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
)

// noopBEP44Store is a bep44.Store that does nothing.
// DHT is disabled in all testenv apps, so the store is never called.
// Using a no-op avoids opening a badger DB whose internal goroutines
// (updateSize) can deadlock SignalAndWait under -race.
type noopBEP44Store struct{}

func (noopBEP44Store) Put(*bep44.Item) error                 { return nil }
func (noopBEP44Store) Get(bep44.Target) (*bep44.Item, error) { return nil, bep44.ErrItemNotFound }
func (noopBEP44Store) Del(bep44.Target) error                { return nil }

type TestApp struct {
	Config       *config.Root
	Client       *atorrent.Client
	Service      *dtorrent.Service
	Stats        *dtorrent.Stats
	FS           *fs.ContainerFs
	TempDir      string
	Cache        *filecache.Cache
	LimitStorage *limitStorage
	HTTPAddr     string
	WebDavAddr   string
	httpServer   *http.Server
	db           *loader.DB
	pc           storage.PieceCompletion
	KeepTempDir  bool
	ctx          context.Context
	cancel       context.CancelFunc
	linkRetryWg  sync.WaitGroup
	httpClient   *http.Client
}

func NewTestApp() (*TestApp, error) {
	return newTestApp("", nil, true, false, false, false)
}

func NewTestAppLimited(limit int64) (*TestApp, error) {
	tempDir, err := os.MkdirTemp("", "distribyted-test-limited")
	if err != nil {
		return nil, err
	}
	return newTestApp(tempDir, &limit, false, false, false, false)
}

func NewTestAppWithDir(tempDir string) (*TestApp, error) {
	return newTestApp(tempDir, nil, false, false, false, false)
}

// NewTestAppNoDefaultDialer is like NewTestApp, but disables TCP so the
// client never registers its listen socket as a default dialer (see
// anacrolix/torrent's Client.dialers / DialForPeerConns). Without this, a
// custom Dialer added via Client.AddDialer (e.g. ThrottledDialer) still
// loses every connection race to the fast, unthrottled default dialer,
// making the addition a no-op. A manually-added dialer still connects fine
// over TCP regardless of this flag — it isn't gated the same way.
func NewTestAppNoDefaultDialer() (*TestApp, error) {
	return newTestApp("", nil, true, true, false, false)
}

// NewTestAppNoDefaultDialerResponsive is NewTestAppNoDefaultDialer with
// config.TorrentGlobal.ResponsiveReads on, for benchmarks comparing
// responsive vs. default read latency under the same throttled-dialer
// conditions (see internal/fs/torrent.go's responsive field).
func NewTestAppNoDefaultDialerResponsive() (*TestApp, error) {
	return newTestApp("", nil, true, true, false, true)
}

// NewTestAppProductionStorage is like NewTestApp, but backs torrent data
// with storage.NewResourcePiecesOpts(filecache, ...) instead of the
// in-memory or FileWithCompletion backends the other constructors use.
// This is what cmd/distribyted/main.go actually uses in production
// (FileWithCompletion is only a Windows fallback), and its incomplete-piece
// read path (a directory scan plus one file open per 16KiB chunk) has a
// real cost that MapClientImpl/FileWithCompletion don't — needed to measure
// changes like responsive reads whose production impact can otherwise look
// free in a benchmark.
//
// KNOWN INTERMITTENTLY UNRELIABLE as of this writing: a manual end-to-end
// check against a loopback seeder failed piece hash verification ("piece
// failed hash. banning peer") on this storage backend once, with a
// Capacity func set, under heavy concurrent system load (many other
// go test processes running at once) and without -race. Follow-up: ~40
// repro attempts across single-chunk pieces, multi-chunk pieces, the exact
// original scenario, with and without -race, and under deliberately
// induced CPU contention (20x `yes` competing for the CPU) all passed
// cleanly — so this is not a simple, reliably-reproducible bug in this
// constructor's own wiring (bare storage.NewResourcePieces() with no
// Capacity func failed identically when first isolated, ruling out the
// Capacity func as the cause). Left as an open, load-dependent heisenbug;
// suspect a narrow read-during-write timing window in the underlying
// library's piecePerResource (upstream, not this repo) that only heavy
// scheduler contention widens enough to hit. Do not treat a failure here as
// a regression in whatever you're actually testing, but also don't assume
// it can't recur — it did, once, under load resembling a busy CI machine.
func NewTestAppProductionStorage() (*TestApp, error) {
	return newTestApp("", nil, false, false, true, false)
}

func newTestApp(tempDir string, limit *int64, inMemory bool, disableDefaultDialer bool, resourcePieces bool, responsiveReads bool) (*TestApp, error) {
	actualTempDir := tempDir
	if actualTempDir == "" {
		var err error
		actualTempDir, err = os.MkdirTemp("", "distribyted-test-auto")
		if err != nil {
			return nil, err
		}
	}

	conf := &config.Root{
		Torrent: &config.TorrentGlobal{
			MetadataFolder:         actualTempDir,
			AddTimeout:             120,
			ReadTimeout:            120,
			ContinueWhenAddTimeout: true,
			GlobalCacheSize:        100,
			DisableIPv6:            true,
			DisableUTP:             true,
			DisableTCP:             disableDefaultDialer,
			DisableUPnP:            true,
			DisableDHT:             true,
			ListenPort:             -1,
			Seed:                   true,
			ResponsiveReads:        responsiveReads,
		},
		HTTPGlobal: &config.HTTPGlobal{
			Port:   0, // random
			IP:     "127.0.0.1",
			HTTPFS: true,
			User:   "test",
			Pass:   "test",
		},
		WebDAV: &config.WebDAVGlobal{
			Port: 0, // random
			User: "test",
			Pass: "test",
		},
	}

	var st storage.ClientImpl
	var fc *filecache.Cache
	var pc storage.PieceCompletion
	switch {
	case inMemory:
		// Pure in-memory storage for torrent data
		st = NewMapClientImpl()
	case resourcePieces:
		cf := filepath.Join(actualTempDir, "cache")
		var err error
		fc, err = filecache.NewCache(cf)
		if err != nil {
			return nil, err
		}
		// Block until filecache's background rescan goroutine releases the
		// mutex, or concurrent piece writes stall behind it early on.
		_ = fc.Info()

		// Mirrors cmd/distribyted/main.go's production storage, capacity
		// wiring included. See NewTestAppProductionStorage's doc comment
		// for why this exists as a separate path from the default below,
		// and for a known reliability caveat with this storage backend.
		capFunc := func() (int64, bool) { return -1, false }
		st = storage.NewResourcePiecesOpts(fc.AsResourceProvider(), storage.ResourcePiecesOpts{Capacity: &capFunc})
	default:
		cf := filepath.Join(actualTempDir, "cache")
		var err error
		fc, err = filecache.NewCache(cf)
		if err != nil {
			return nil, err
		}
		// Block until filecache's background rescan goroutine releases the mutex.
		// Without this, concurrent writes (piece chunks) block on the mutex while
		// rescan holds it, delaying piece completion under -race and many goroutines.
		_ = fc.Info()

		pcp := filepath.Join(actualTempDir, "piece-completion")
		if err := os.MkdirAll(pcp, 0744); err != nil {
			return nil, err
		}
		pc, err = storage.NewBoltPieceCompletion(pcp)
		if err != nil {
			return nil, err
		}

		// Use FileWithCompletion (file-based + BoltDB) instead of ResourcePieces
		// (filecache). ResourcePieces has a race under -race: MarkComplete renames
		// the piece file before the data is fully readable, causing unexpected EOF.
		// FileWithCompletion only renames at the per-file level (all pieces done),
		// so there is no piece-level rename race. NewTestAppProductionStorage uses
		// ResourcePieces directly for benchmarks that need the real behavior and
		// deliberately don't run under -race.
		pieceDir := filepath.Join(actualTempDir, "pieces")
		if err := os.MkdirAll(pieceDir, 0744); err != nil {
			return nil, err
		}
		st = storage.NewFileWithCompletion(pieceDir, pc)
	}

	var ls *limitStorage
	if limit != nil {
		ls = &limitStorage{ClientImpl: st, limitBytes: *limit}
		st = ls
	}

	// DHT is disabled in tests so the store is never called; use a no-op to
	// avoid opening a badger DB whose internal goroutines can hang on close.
	fis := noopBEP44Store{}

	idPath := ""
	if !inMemory {
		idPath = filepath.Join(actualTempDir, "ID")
	}
	id, _ := dtorrent.GetOrCreatePeerID(idPath)

	c, err := dtorrent.NewClient(st, fis, conf.Torrent, id)
	if err != nil {
		return nil, err
	}

	ss := dtorrent.NewStats()
	dbPath := ""
	if !inMemory {
		dbPath = filepath.Join(actualTempDir, "magnetdb")
	}
	dbl, err := loader.NewDB(dbPath)
	if err != nil {
		return nil, err
	}

	ts := dtorrent.NewService(nil, dbl, ss, dtorrent.ClientWrapper{Client: c},
		conf.Torrent.AddTimeout,
		conf.Torrent.ReadTimeout,
		conf.Torrent.ContinueWhenAddTimeout,
		conf.Torrent.ResponsiveReads,
	)

	fss, _ := ts.Load()
	cfs, _ := fs.NewContainerFs(fss)

	ts.OnRouteAdded(func(p string, fss fs.Filesystem) {
		_ = cfs.AddFS(p, fss)
	})
	cfs.OnLinkAdded(func(oldpath, newpath string) {
		_ = ts.AddLink(oldpath, newpath)
	})
	cfs.OnLinkRemoved(func(path string) {
		_ = ts.RemoveLink(path)
	})
	cfs.OnLinkRenamed(func(oldpath, newpath string) {
		_ = ts.RenameLink(oldpath, newpath)
	})
	cfs.OnLastReferenceRemoved(func(hash string) {
		_ = ts.RemoveFromHashOnly(hash)
	})
	ts.OnTorrentRemoved(func(h string) {
		cfs.RemoveByHash(h)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	app := &TestApp{
		Config:       conf,
		Client:       c,
		Service:      ts,
		Stats:        ss,
		FS:           cfs,
		TempDir:      actualTempDir,
		Cache:        fc,
		LimitStorage: ls,
		db:           dbl,
		pc:           pc,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Tracked by app.linkRetryWg so Close() can wait for these to actually
	// observe ctx.Done() and return before closing db out from under them.
	// Without that wait, cancel() racing the 1s ticker (select doesn't
	// prefer an already-ready ctx.Done() over an also-ready ticker.C) could
	// let a goroutine proceed into cfs.Link -> ts.AddLink -> db.AddLink ->
	// db.Sync() after Close() had already closed db, panicking inside
	// badger with a nil-pointer dereference.
	links, _ := ts.ListLinks()
	for n, o := range links {
		app.linkRetryWg.Add(1)
		go func(oldpath, newpath string) {
			defer app.linkRetryWg.Done()
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for i := 0; i < 30; i++ { // 30 seconds max for tests
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := cfs.Link(oldpath, newpath); err == nil {
						return
					}
				}
			}
		}(o, n)
	}

	// Start servers
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	httpAddr := httpListener.Addr().String()

	webDavListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	webDavAddr := webDavListener.Addr().String()
	_, webDavPortStr, _ := net.SplitHostPort(webDavAddr)
	var webDavPort int
	_, _ = fmt.Sscanf(webDavPortStr, "%d", &webDavPort)

	httpfs := dtorrent.NewHTTPFS(cfs)

	ch := config.NewHandler("")

	h, err := dhttp.NewHandler(fc, ss, ts, ch, nil, httpfs, "", conf, "/fuse", cfs)
	if err != nil {
		return nil, err
	}

	httpServer := &http.Server{Handler: h, Addr: httpAddr}
	go func() {
		_ = httpServer.Serve(httpListener)
	}()

	go func() {
		if err := webdav.NewWebDAVServerWithListener(webDavListener, cfs, conf.WebDAV.User, conf.WebDAV.Pass); err != nil {
			fmt.Printf("WebDAV error: %v\n", err) //nolint:forbidigo
		}
	}()

	app.HTTPAddr = httpAddr
	app.WebDavAddr = webDavAddr
	app.httpServer = httpServer

	return app, nil
}

// HTTPClient returns an *http.Client already logged in against this app's
// HTTP server (qBittorrent-compatible session cookie), memoized across calls.
func (a *TestApp) HTTPClient() (*http.Client, error) {
	if a.httpClient != nil {
		return a.httpClient, nil
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar}

	form := url.Values{"username": {a.Config.HTTPGlobal.User}, "password": {a.Config.HTTPGlobal.Pass}}
	resp, err := client.PostForm("http://"+a.HTTPAddr+"/api/v2/auth/login", form)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()

	a.httpClient = client
	return client, nil
}

func (a *TestApp) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	// Must finish before db is closed below: a link-retry goroutine that
	// hasn't yet noticed ctx.Done() can still be mid-flight into
	// db.AddLink/db.Sync().
	a.linkRetryWg.Wait()
	if a.httpServer != nil {
		_ = a.httpServer.Shutdown(context.Background())
	}
	a.Client.Close()
	if a.pc != nil {
		_ = a.pc.Close()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
	if a.TempDir != "" && !a.KeepTempDir {
		_ = os.RemoveAll(a.TempDir)
	}
}
