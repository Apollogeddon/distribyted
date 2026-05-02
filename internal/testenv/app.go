package testenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/storage"
	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/fs"
	dhttp "github.com/Apollogeddon/distribyted/http"
	dtorrent "github.com/Apollogeddon/distribyted/torrent"
	"github.com/Apollogeddon/distribyted/torrent/loader"
	"github.com/Apollogeddon/distribyted/webdav"
	atorrent "github.com/anacrolix/torrent"
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
	return NewTestAppWithDir(tempDir)
}

func NewTestAppWithDir(tempDir string) (*TestApp, error) {
	conf := &config.Root{
		Torrent: &config.TorrentGlobal{
			MetadataFolder:         tempDir,
			AddTimeout:             10,
			ReadTimeout:            10,
			ContinueWhenAddTimeout: true,
			GlobalCacheSize:        100,
			DisableIPv6:            true,
			DisableUTP:             true,
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
	st := storage.NewFile(cf)

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

	cfs, _ := fs.NewContainerFs(nil)

	ts.OnRouteAdded(func(p string, fss fs.Filesystem) {
		_ = cfs.AddFS(p, fss)
	})
	ts.OnLinkAdded(func(oldpath, newpath string) {
		_ = cfs.Link(oldpath, newpath)
	})
	ts.OnLinkRemoved(func(path string) {
		_ = cfs.Remove(path)
	})

	fss, _ := ts.Load()
	for p, fs := range fss {
		_ = cfs.AddFS(p, fs)
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
	fmt.Sscanf(webDavPortStr, "%d", &webDavPort)

	httpfs := dtorrent.NewHTTPFS(cfs)
	
	ch := config.NewHandler("") 
	
	h, err := dhttp.NewHandler(fc, ss, ts, ch, nil, httpfs, "", conf, "/fuse")
	if err != nil {
		return nil, err
	}
	
	httpServer := &http.Server{Handler: h}
	go httpServer.Serve(httpListener)

	go func() {
		if err := webdav.NewWebDAVServerWithListener(webDavListener, cfs, conf.WebDAV.User, conf.WebDAV.Pass); err != nil {
		}
	}()

	return &TestApp{
		Config:      conf,
		Client:      c,
		Service:     ts,
		Stats:       ss,
		FS:          cfs,
		TempDir:     tempDir,
		Cache:       fc,
		HttpAddr:    httpAddr,
		WebDavAddr:  webDavAddr,
		httpServer:  httpServer,
		db:          dbl,
		itemStore:   fis,
	}, nil
}

func (a *TestApp) Close() {
	if a.httpServer != nil {
		a.httpServer.Shutdown(context.Background())
	}
	a.Client.Close()
	if a.db != nil {
		a.db.Close()
	}
	if a.itemStore != nil {
		a.itemStore.Close()
	}
	if !a.KeepTempDir {
		os.RemoveAll(a.TempDir)
	}
}
