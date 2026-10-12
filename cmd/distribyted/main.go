package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/fuse"
	"github.com/Apollogeddon/distribyted/internal/http"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/Apollogeddon/distribyted/internal/torrent/loader"
	"github.com/Apollogeddon/distribyted/internal/webdav"
)

const (
	configFlag     = "config"
	fuseAllowOther = "fuse-allow-other"
	portFlag       = "http-port"
	webDAVPortFlag = "webdav-port"
	ipFlag         = "http-ip"
	webDAVIPFlag   = "webdav-ip"

	linkRetryMaxIter = 300 // 10 minutes at 2s per attempt
)

var (
	linkRetryInterval = 2 * time.Second
	itemStoreTTL      = 2 * time.Hour
	// how long a cached piece may keep failing reads before it's downloaded again
	readGiveUpAfter = time.Minute
)

var (
	Version = "dev"
	Build   = "none"
)

func main() {
	app := &cli.App{
		Name:    "distribyted",
		Usage:   "Torrent client with on-demand file downloading as a filesystem.",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    configFlag,
				Value:   "./distribyted-data/config/config.yaml",
				EnvVars: []string{"DISTRIBYTED_CONFIG"},
				Usage:   "YAML file containing distribyted configuration.",
			},
			&cli.IntFlag{
				Name:    portFlag,
				Value:   config.DefaultHTTPPort,
				EnvVars: []string{"DISTRIBYTED_HTTP_PORT"},
				Usage:   "HTTP port for web interface.",
			},
			&cli.IntFlag{
				Name:    webDAVPortFlag,
				Value:   config.DefaultWebDAVPort,
				EnvVars: []string{"DISTRIBYTED_WEBDAV_PORT"},
				Usage:   "Port used for WebDAV interface.",
			},
			&cli.StringFlag{
				Name:    ipFlag,
				EnvVars: []string{"DISTRIBYTED_HTTP_IP"},
				Usage:   "Address the web interface listens on, over the config's http.ip (default 127.0.0.1; 0.0.0.0 for every address).",
			},
			&cli.StringFlag{
				Name:    webDAVIPFlag,
				EnvVars: []string{"DISTRIBYTED_WEBDAV_IP"},
				Usage:   "Address WebDAV listens on, over the config's webdav.ip (default 127.0.0.1; 0.0.0.0 for every address).",
			},
			&cli.BoolFlag{
				Name:    fuseAllowOther,
				Value:   false,
				EnvVars: []string{"DISTRIBYTED_FUSE_ALLOW_OTHER"},
				Usage:   "Allow other users to access all fuse mountpoints. You need to add user_allow_other flag to /etc/fuse.conf file.",
			},
		},

		Action: func(c *cli.Context) error {
			// a port given on the command line or in the environment overrides the config
			// file; 0 means use the config's (the flags' defaults match the config defaults)
			o := overrides{httpIP: c.String(ipFlag), webDAVIP: c.String(webDAVIPFlag), fuseAllowOther: c.Bool(fuseAllowOther)}
			if c.IsSet(portFlag) {
				o.httpPort = c.Int(portFlag)
			}
			if c.IsSet(webDAVPortFlag) {
				o.webDAVPort = c.Int(webDAVPortFlag)
			}
			err := load(c.String(configFlag), o)

			// stop program execution on errors to avoid flashing consoles
			if err != nil && runtime.GOOS == "windows" {
				log.Error().Err(err).Msg("problem starting application")
				fmt.Print("Press 'Enter' to continue...") //nolint:forbidigo // Windows interactive pause, not a log statement
				_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
			}

			return err
		},

		Commands: []*cli.Command{
			{
				Name:  "healthcheck",
				Usage: "Exit 0 if a running distribyted's web server answers /healthz (or /readyz with --ready), for container health checks.",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "ready",
						Usage: "Check /readyz, which also waits for the FUSE mount, in place of /healthz.",
					},
				},
				Action: func(c *cli.Context) error {
					var port int
					if c.IsSet(portFlag) {
						port = c.Int(portFlag)
					}
					return healthcheck(c.String(configFlag), c.String(ipFlag), port, c.Bool("ready"))
				},
			},
		},

		HideHelpCommand: true,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("problem starting application")
	}
}

type storageLayer struct {
	fc  *filecache.Cache
	pc  storage.PieceCompletion
	st  storage.ClientImpl
	fis *torrent.FileItemStore
	id  [20]byte
}

func initStorageLayer(conf *config.Root) (storageLayer, error) {
	if err := os.MkdirAll(conf.Torrent.MetadataFolder, 0o750); err != nil {
		return storageLayer{}, fmt.Errorf("error creating metadata folder: %w", err)
	}

	cf := filepath.Join(conf.Torrent.MetadataFolder, "cache")
	fc, err := filecache.NewCache(cf)
	if err != nil {
		return storageLayer{}, fmt.Errorf("error creating cache: %w", err)
	}

	pcp := filepath.Join(conf.Torrent.MetadataFolder, "piece-completion")
	if err := os.MkdirAll(pcp, 0o750); err != nil {
		return storageLayer{}, fmt.Errorf("error creating piece completion folder: %w", err)
	}

	pc, err := storage.NewBoltPieceCompletion(pcp)
	if err != nil {
		return storageLayer{}, fmt.Errorf("error creating piece completion db: %w", err)
	}

	// Without a capacity func, the client doesn't know the cache evicts
	// pieces under it (SetCapacity is only called on fc, below, after
	// startup completes), which affects its piece-eviction/retry logic. A
	// negative filecache capacity means unlimited (see filecache.Cache).
	capFunc := func() (int64, bool) {
		if c := fc.Info().Capacity; c >= 0 {
			return c, true
		}
		return 0, false
	}
	// the capacity makes anacrolix/torrent retry a failed read without limit (GuardReads)
	st := torrent.GuardReads(storage.NewResourcePiecesOpts(fc.AsResourceProvider(), storage.ResourcePiecesOpts{Capacity: &capFunc}), readGiveUpAfter)
	// cache is not working with windows
	if runtime.GOOS == "windows" {
		st = storage.NewFileWithCompletion(cf, pc)
	}

	fis, err := torrent.NewFileItemStore(filepath.Join(conf.Torrent.MetadataFolder, "items"), itemStoreTTL)
	if err != nil {
		return storageLayer{}, fmt.Errorf("error starting item store: %w", err)
	}

	id, err := torrent.GetOrCreatePeerID(filepath.Join(conf.Torrent.MetadataFolder, "ID"))
	if err != nil {
		return storageLayer{}, fmt.Errorf("error creating node ID: %w", err)
	}

	return storageLayer{fc: fc, pc: pc, st: st, fis: fis, id: id}, nil
}

func startFUSEMount(mh *fuse.Handler, cfs *fs.ContainerFs, fusePath string) {
	go func() {
		if mh == nil {
			return
		}
		forceUnmount(fusePath)
		if err := mh.Mount(cfs); err != nil {
			log.Info().Err(err).Msg("error mounting filesystems")
		}
	}()
}

func startWebDAVMount(conf *config.Root, cfs *fs.ContainerFs) {
	go func() {
		if conf.WebDAV == nil {
			log.Warn().Msg("webDAV configuration not found!")
			return
		}
		if err := webdav.NewWebDAVServer(cfs, conf.WebDAV.IP, conf.WebDAV.Port, conf.WebDAV.User, conf.WebDAV.Pass); err != nil {
			log.Error().Err(err).Msg("error starting webDAV")
		}
	}()
}

// overrides are the settings the command line or the environment give over the config file;
// a zero value leaves the config's.
type overrides struct {
	httpPort, webDAVPort int
	httpIP, webDAVIP     string
	fuseAllowOther       bool
}

func (o overrides) apply(conf *config.Root) {
	if o.httpPort != 0 {
		conf.HTTPGlobal.Port = o.httpPort
	}
	if o.httpIP != "" {
		conf.HTTPGlobal.IP = o.httpIP
	}
	if conf.WebDAV != nil {
		if o.webDAVPort != 0 {
			conf.WebDAV.Port = o.webDAVPort
		}
		if o.webDAVIP != "" {
			conf.WebDAV.IP = o.webDAVIP
		}
	}
}

func load(configPath string, o overrides) error {
	ch := config.NewHandler(configPath)

	conf, err := ch.Get()
	if err != nil {
		return fmt.Errorf("error loading configuration: %w", err)
	}

	o.apply(conf)
	if err := config.Validate(conf); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	dlog.Load(conf.Log)

	sl, err := initStorageLayer(conf)
	if err != nil {
		return err
	}

	c, err := torrent.NewClient(sl.st, sl.fis, conf.Torrent, sl.id)
	if err != nil {
		return fmt.Errorf("error starting torrent client: %w", err)
	}

	if !conf.Torrent.Seed && len(conf.Servers) > 0 {
		log.Warn().Msg("seeding is disabled in configuration, but 'servers' are configured. Local folders will not be shared with the swarm.")
	}

	var servers []*torrent.Server
	for _, s := range conf.Servers {
		server := torrent.NewServer(c, sl.pc, s)
		servers = append(servers, server)
		if err := server.Start(); err != nil {
			return fmt.Errorf("error starting server: %w", err)
		}
	}

	cl := loader.NewConfig(conf.Routes)
	fl := loader.NewFolder(conf.Routes)
	ss := torrent.NewStats()

	dbl, err := loader.NewDB(filepath.Join(conf.Torrent.MetadataFolder, "magnetdb"))
	if err != nil {
		return fmt.Errorf("error starting magnet database: %w", err)
	}

	tm := torrent.NewTimings()
	ts := torrent.NewService([]loader.Loader{cl, fl}, dbl, ss, torrent.ClientWrapper{Client: c},
		conf.Torrent.AddTimeout,
		conf.Torrent.ReadTimeout,
		conf.Torrent.ContinueWhenAddTimeout,
		conf.Torrent.ResponsiveReads,
		tm,
	)

	var mh *fuse.Handler
	if conf.Fuse != nil {
		mh = fuse.NewHandler(o.fuseAllowOther || conf.Fuse.AllowOther, conf.Fuse.Path)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	var linkRetryWg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		<-sigChan
		// Signal link-retry goroutines to stop and wait for them to actually
		// exit before closing the databases below — a retry firing after
		// close would panic inside badger with a nil-pointer dereference.
		close(done)
		linkRetryWg.Wait()

		shutdownPlan{
			unmount: func() {
				if mh != nil {
					log.Info().Msg("unmounting fuse filesystem...")
					mh.Unmount()
				}
			},
			closeServers: func() {
				for _, s := range servers {
					if err := s.Close(); err != nil {
						log.Warn().Err(err).Msg("problem closing server")
					}
				}
			},
			recordPeers: ts.RecordPeers,
			closeClient: func() {
				c.Close()
				tm.Close()
			},
			stores: []namedCloser{
				{"items database", sl.fis.Close},
				{"magnet database", dbl.Close},
				{"piece completion database", sl.pc.Close},
			},
		}.run()
		log.Info().Msg("exiting")
		os.Exit(0)
	}()

	go runtimeStatsLogger(done)

	log.Info().Msg(fmt.Sprintf("setting cache size to %d MB", conf.Torrent.GlobalCacheSize))
	sl.fc.SetCapacity(conf.Torrent.GlobalCacheSize * 1024 * 1024)
	fs.SetExtractLimit(max(conf.Torrent.ArchiveExtractLimit, 0) * 1024 * 1024)

	fss, err := ts.Load()
	if err != nil {
		return fmt.Errorf("error when loading torrents: %w", err)
	}

	cfs, err := fs.NewContainerFs(fss)
	if err != nil {
		return fmt.Errorf("error creating container filesystem: %w", err)
	}

	links, err := ts.ListLinks()
	if err != nil {
		log.Warn().Err(err).Msg("problem loading links from database")
	}

	for np, op := range links {
		if op == "" {
			_ = cfs.Mkdir(np) // link dir may already exist
		} else {
			linkRetryWg.Add(1)
			go func(op, np string) {
				defer linkRetryWg.Done()
				ticker := time.NewTicker(linkRetryInterval)
				defer ticker.Stop()
				for i := 0; i < linkRetryMaxIter; i++ {
					select {
					case <-done:
						return
					case <-ticker.C:
						if err := cfs.Link(op, np); err == nil {
							return
						}
					}
				}
				log.Warn().Str("old", op).Str("new", np).Msg("giving up creating virtual link after max retries")
			}(op, np)
		}
	}

	cfs.OnLinkAdded(func(oldpath, newpath string) {
		if err := ts.AddLink(oldpath, newpath); err != nil {
			log.Warn().Err(err).Str("old", oldpath).Str("new", newpath).Msg("problem saving link to database")
		}
	})

	cfs.OnLinkRemoved(func(path string) {
		if err := ts.RemoveLink(path); err != nil {
			log.Warn().Err(err).Str(dlog.KeyPath, path).Msg("problem removing link from database")
		}
	})

	cfs.OnLinkRenamed(func(oldpath, newpath string) {
		if err := ts.RenameLink(oldpath, newpath); err != nil {
			log.Warn().Err(err).Str("old", oldpath).Str("new", newpath).Msg("problem persisting renamed link to database")
		}
	})

	cfs.OnLastReferenceRemoved(func(hash string) {
		if err := ts.RemoveFromHashOnly(hash); err != nil {
			log.Warn().Err(err).Str(dlog.KeyHash, hash).Msg("problem removing torrent after its last reference was deleted")
		}
	})

	ts.OnTorrentRemoved(func(h string) {
		log.Info().Str(dlog.KeyHash, h).Msg("cascading torrent removal to virtual links")
		cfs.RemoveByHash(h)
	})

	ts.OnRouteAdded(func(p string, fss fs.Filesystem) {
		log.Info().Str(dlog.KeyPath, p).Msg("dynamically adding new route to filesystem")
		if err := cfs.AddFS(p, fss); err != nil {
			log.Error().Err(err).Str(dlog.KeyPath, p).Msg("the new route can't be mounted: something else is already at its path")
		}
	})

	fusePath := "/distribyted-data/mount"
	if conf.Fuse != nil && conf.Fuse.Path != "" {
		fusePath = conf.Fuse.Path
	}

	startFUSEMount(mh, cfs, fusePath)
	startWebDAVMount(conf, cfs)

	httpfs := torrent.NewHTTPFS(cfs)
	logFilename := filepath.Join(conf.Log.Path, dlog.FileName)

	// the HTTP server starts once the torrent client is up and the routes have loaded, so what's
	// left to wait for is the FUSE mount, when there is one
	ready := func() bool { return mh == nil || mh.Mounted() }

	err = http.New(sl.fc, ss, ts, ch, servers, httpfs, logFilename, conf, fusePath, cfs, http.WithReadiness(ready))
	if err != nil {
		log.Error().Err(err).Msg("error initializing HTTP server")
	}
	return err
}

// shutdownPlan stops everything in an order where nothing is used once it's closed: peers are
// saved while the client is still up, and the client, which reads and writes the stores, is
// closed before them.
type shutdownPlan struct {
	unmount      func()
	closeServers func()
	recordPeers  func()
	closeClient  func()
	stores       []namedCloser
}

type namedCloser struct {
	name  string
	close func() error
}

func (p shutdownPlan) run() {
	p.unmount()
	log.Info().Msg("closing servers...")
	p.closeServers()
	log.Info().Msg("saving the torrents' good peers...")
	p.recordPeers()
	log.Info().Msg("closing torrent client...")
	p.closeClient()
	for _, s := range p.stores {
		log.Info().Msgf("closing %s...", s.name)
		if err := s.close(); err != nil {
			log.Warn().Err(err).Msgf("problem closing %s", s.name)
		}
	}
}

const runtimeStatsInterval = 5 * time.Minute

// runtimeStatsLogger periodically logs goroutine count and stack memory
// usage. A stuck torrent read that blows its deadline gets abandoned
// rather than left to hang the mount (see internal/fs/torrent.go's
// readAtWrapper), but the abandoned goroutine itself keeps running until
// its underlying read eventually returns — or, for a piece that can never
// complete, forever, slowly growing its stack. That's otherwise invisible
// short of attaching a profiler; this line is the cheap early-warning
// signal that goroutines/stack memory are climbing instead of plateauing.
func runtimeStatsLogger(done <-chan struct{}) {
	ticker := time.NewTicker(runtimeStatsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			log.Info().
				Int("goroutines", runtime.NumGoroutine()).
				Uint64("stack_sys_mb", ms.StackSys/1024/1024).
				Uint64("heap_alloc_mb", ms.HeapAlloc/1024/1024).
				Msg("runtime stats")
		}
	}
}

func forceUnmount(mnt string) {
	if runtime.GOOS == "windows" {
		return
	}
	// Try both fusermount and umount — errors are expected and irrelevant here
	_ = exec.Command("fusermount", "-uz", mnt).Run()
	_ = exec.Command("umount", "-l", mnt).Run()
}
