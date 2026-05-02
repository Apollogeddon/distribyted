package testenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/fs"
	dhttp "github.com/Apollogeddon/distribyted/http"
	dtorrent "github.com/Apollogeddon/distribyted/torrent"
	"github.com/Apollogeddon/distribyted/torrent/loader"
	"github.com/Apollogeddon/distribyted/webdav"
	"github.com/anacrolix/missinggo/v2/filecache"
	atorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
)

type TestApp struct {
	Config      *config.Root
	Client      *atorrent.Client
	Service     *dtorrent.Service
	Stats       *dtorrent.Stats
	FS          *fs.ContainerFs
	TempDir     string
	Cache       *filecache.Cache
	HttpAddr    string
	WebDavAddr  string
	httpServer  *http.Server
	db          *loader.DB
	itemStore   *dtorrent.FileItemStore
	KeepTempDir bool
}

func NewTestApp() (*TestApp, error) {
	tempDir, err := os.MkdirTemp("", "distribyted-test")
	if err != nil {
		return nil, err
	}
	return newTestApp(tempDir, nil)
}

func NewTestAppLimited(limit int64) (*TestApp, error) {
	tempDir, err := os.MkdirTemp("", "distribyted-test-limited")
	if err != nil {
		return nil, err
	}
	return newTestApp(tempDir, &limit)
}

func NewTestAppWithDir(tempDir string) (*TestApp, error) {
	return newTestApp(tempDir, nil)
}

func newTestApp(tempDir string, limit *int64) (*TestApp, error) {
	conf := &config.Root{
		Torrent: &config.TorrentGlobal{
			MetadataFolder:         tempDir,
			AddTimeout:             60,
			ReadTimeout:            60,
			ContinueWhenAddTimeout: true,
			GlobalCacheSize:        100,
			DisableIPv6:            true,
			DisableUTP:             true,
			DisableUPnP:            true,
			DisableDHT:             true,
			ListenPort:             -1,
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

	cf := filepath.Join(tempDir, "cache")
	fc, err := filecache.NewCache(cf)
	if err != nil {
		return nil, err
	}
	var st storage.ClientImpl = storage.NewFile(cf)
	if limit != nil {
		st = &limitStorage{ClientImpl: st, limitBytes: *limit}
	}

	fis, err := dtorrent.NewFileItemStore(filepath.Join(tempDir, "items"), 2*time.Hour)
	if err != nil {
		return nil, err
	}

	id, _ := dtorrent.GetOrCreatePeerID(filepath.Join(tempDir, "ID"))

	c, err := dtorrent.NewClient(st, fis, conf.Torrent, id)
	if err != nil {
		return nil, err
	}

	ss := dtorrent.NewStats()
	dbl, err := loader.NewDB(filepath.Join(tempDir, "magnetdb"))
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

	links, _ := ts.ListLinks()
	for o, n := range links {
		go func(oldpath, newpath string) {
			for i := 0; i < 30; i++ { // 30 seconds max for tests
				if err := cfs.Link(oldpath, newpath); err == nil {
					return
				}
				time.Sleep(1 * time.Second)
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
			fmt.Printf("WebDAV error: %v\n", err)
		}
	}()

	return &TestApp{
		Config:     conf,
		Client:     c,
		Service:    ts,
		Stats:      ss,
		FS:         cfs,
		TempDir:    tempDir,
		Cache:      fc,
		HttpAddr:   httpAddr,
		WebDavAddr: webDavAddr,
		httpServer: httpServer,
		db:         dbl,
		itemStore:  fis,
	}, nil
}

func (a *TestApp) Close() {
	if a.httpServer != nil {
		_ = a.httpServer.Shutdown(context.Background())
	}
	a.Client.Close()
	if a.db != nil {
		_ = a.db.Close()
	}
	if a.itemStore != nil {
		_ = a.itemStore.Close()
	}
	if !a.KeepTempDir {
		os.RemoveAll(a.TempDir)
	}
}
