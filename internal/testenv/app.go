package testenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/fs"
	dhttp "github.com/Apollogeddon/distribyted/http"
	dtorrent "github.com/Apollogeddon/distribyted/torrent"
	"github.com/Apollogeddon/distribyted/internal/torrent/loader"
	"github.com/Apollogeddon/distribyted/webdav"
	"github.com/anacrolix/missinggo/v2/filecache"
	atorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
)

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
	itemStore    *dtorrent.FileItemStore
	pc           storage.PieceCompletion
	KeepTempDir  bool
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewTestApp() (*TestApp, error) {
	return newTestApp("", nil, true)
}

func NewTestAppLimited(limit int64) (*TestApp, error) {
	tempDir, err := os.MkdirTemp("", "distribyted-test-limited")
	if err != nil {
		return nil, err
	}
	return newTestApp(tempDir, &limit, false)
}

func NewTestAppWithDir(tempDir string) (*TestApp, error) {
	return newTestApp(tempDir, nil, false)
}

func newTestApp(tempDir string, limit *int64, inMemory bool) (*TestApp, error) {
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
			DisableUPnP:            true,
			DisableDHT:             true,
			ListenPort:             -1,
			Seed:                   true,
		},
		HTTPGlobal: &config.HTTPGlobal{
			Port:   0, // random
			IP:     "127.0.0.1",
			HTTPFS: true,
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
	if inMemory {
		// Pure in-memory storage for torrent data
		st = NewMapClientImpl()
	} else {
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
		// so there is no piece-level rename race.
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

	itemPath := ""
	if !inMemory {
		itemPath = filepath.Join(actualTempDir, "items")
	}
	fis, err := dtorrent.NewFileItemStore(itemPath, 2*time.Hour)
	if err != nil {
		return nil, err
	}

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
	)

	fss, _ := ts.Load()
	cfs, _ := fs.NewContainerFs(fss)

	ts.OnRouteAdded(func(p string, fss fs.Filesystem) {
		_ = cfs.AddFS(p, fss)
	})
	ts.OnLinkAdded(func(oldpath, newpath string) {
		_ = cfs.Link(oldpath, newpath)
	})
	ts.OnLinkRemoved(func(path string) {
		_ = cfs.Remove(path)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	links, _ := ts.ListLinks()
	for n, o := range links {
		go func(oldpath, newpath string) {
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

	h, err := dhttp.NewHandler(fc, ss, ts, ch, nil, httpfs, "", conf, "/fuse")
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

	return &TestApp{
		Config:       conf,
		Client:       c,
		Service:      ts,
		Stats:        ss,
		FS:           cfs,
		TempDir:      actualTempDir,
		Cache:        fc,
		LimitStorage: ls,
		HTTPAddr:     httpAddr,
		WebDavAddr:   webDavAddr,
		httpServer:   httpServer,
		db:           dbl,
		itemStore:    fis,
		pc:           pc,
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}

func (a *TestApp) Close() {
	if a.cancel != nil {
		a.cancel()
	}
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
	if a.itemStore != nil {
		_ = a.itemStore.Close()
	}
	if a.TempDir != "" && !a.KeepTempDir {
		_ = os.RemoveAll(a.TempDir)
	}
}
