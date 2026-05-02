package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/fs"
	"github.com/Apollogeddon/distribyted/fuse"
	"github.com/Apollogeddon/distribyted/http"
	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/Apollogeddon/distribyted/torrent"
	"github.com/Apollogeddon/distribyted/torrent/loader"
	"github.com/Apollogeddon/distribyted/webdav"
)

const (
	configFlag     = "config"
	fuseAllowOther = "fuse-allow-other"
	portFlag       = "http-port"
	webDAVPortFlag = "webdav-port"
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
				Value:   4444,
				EnvVars: []string{"DISTRIBYTED_HTTP_PORT"},
				Usage:   "HTTP port for web interface.",
			},
			&cli.IntFlag{
				Name:    webDAVPortFlag,
				Value:   36911,
				EnvVars: []string{"DISTRIBYTED_WEBDAV_PORT"},
				Usage:   "Port used for WebDAV interface.",
			},
			&cli.BoolFlag{
				Name:    fuseAllowOther,
				Value:   false,
				EnvVars: []string{"DISTRIBYTED_FUSE_ALLOW_OTHER"},
				Usage:   "Allow other users to access all fuse mountpoints. You need to add user_allow_other flag to /etc/fuse.conf file.",
			},
		},

		Action: func(c *cli.Context) error {
			err := load(c.String(configFlag), c.Int(portFlag), c.Int(webDAVPortFlag), c.Bool(fuseAllowOther))

			// stop program execution on errors to avoid flashing consoles
			if err != nil && runtime.GOOS == "windows" {
				log.Error().Err(err).Msg("problem starting application")
				fmt.Print("Press 'Enter' to continue...")
				_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
			}

			return err
		},

		HideHelpCommand: true,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("problem starting application")
	}
}

func load(configPath string, port, webDAVPort int, fuseAllowOther bool) error {
	ch := config.NewHandler(configPath)

	conf, err := ch.Get()
	if err != nil {
		return fmt.Errorf("error loading configuration: %w", err)
	}

	dlog.Load(conf.Log)

	if err := os.MkdirAll(conf.Torrent.MetadataFolder, 0744); err != nil {
		return fmt.Errorf("error creating metadata folder: %w", err)
	}

	cf := filepath.Join(conf.Torrent.MetadataFolder, "cache")
	fc, err := filecache.NewCache(cf)
	if err != nil {
		return fmt.Errorf("error creating cache: %w", err)
	}

	st := storage.NewResourcePieces(fc.AsResourceProvider())

	// cache is not working with windows
	if runtime.GOOS == "windows" {
		st = storage.NewFile(cf)
	}

	fis, err := torrent.NewFileItemStore(filepath.Join(conf.Torrent.MetadataFolder, "items"), 2*time.Hour)
	if err != nil {
		return fmt.Errorf("error starting item store: %w", err)
	}

	id, err := torrent.GetOrCreatePeerID(filepath.Join(conf.Torrent.MetadataFolder, "ID"))
	if err != nil {
		return fmt.Errorf("error creating node ID: %w", err)
	}

	c, err := torrent.NewClient(st, fis, conf.Torrent, id)
	if err != nil {
		return fmt.Errorf("error starting torrent client: %w", err)
	}

	pcp := filepath.Join(conf.Torrent.MetadataFolder, "piece-completion")
	if err := os.MkdirAll(pcp, 0744); err != nil {
		return fmt.Errorf("error creating piece completion folder: %w", err)
	}

	pc, err := storage.NewBoltPieceCompletion(pcp)
	if err != nil {
		return fmt.Errorf("error creating servers piece completion: %w", err)
	}

	var servers []*torrent.Server
	for _, s := range conf.Servers {
		server := torrent.NewServer(c, pc, s)
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

	ts := torrent.NewService([]loader.Loader{cl, fl}, dbl, ss, torrent.ClientWrapper{c},
		conf.Torrent.AddTimeout,
		conf.Torrent.ReadTimeout,
		conf.Torrent.ContinueWhenAddTimeout,
	)

	var mh *fuse.Handler
	if conf.Fuse != nil {
		mh = fuse.NewHandler(fuseAllowOther || conf.Fuse.AllowOther, conf.Fuse.Path)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {

		<-sigChan
		if mh != nil {
			log.Info().Msg("unmounting fuse filesystem...")
			mh.Unmount()
		}
		log.Info().Msg("closing servers...")
		for _, s := range servers {
			if err := s.Close(); err != nil {
				log.Warn().Err(err).Msg("problem closing server")
			}
		}
		log.Info().Msg("closing items database...")
		fis.Close()
		log.Info().Msg("closing magnet database...")
		dbl.Close()
		log.Info().Msg("closing torrent client...")
		c.Close()

		log.Info().Msg("exiting")
		os.Exit(0)
	}()

	log.Info().Msg(fmt.Sprintf("setting cache size to %d MB", conf.Torrent.GlobalCacheSize))
	fc.SetCapacity(conf.Torrent.GlobalCacheSize * 1024 * 1024)

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

	for newpath, oldpath := range links {
		if oldpath == "" {
			_ = cfs.Mkdir(newpath)
		} else {
			go func(op, np string) {
				for i := 0; i < 300; i++ { // 10 minutes max for app
					if err := cfs.Link(op, np); err == nil {
						return
					}
					time.Sleep(2 * time.Second)
				}
			}(oldpath, newpath)
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

	ts.OnTorrentRemoved(func(h string) {
		log.Info().Str(dlog.KeyHash, h).Msg("cascading torrent removal to virtual links")
		cfs.RemoveByHash(h)
	})

	ts.OnRouteAdded(func(p string, fss fs.Filesystem) {
		log.Info().Str(dlog.KeyPath, p).Msg("dynamically adding new route to filesystem")
		_ = cfs.AddFS(p, fss)
	})

	go func() {
		if mh == nil {
			return
		}

		if err := mh.Mount(cfs); err != nil {
			log.Info().Err(err).Msg("error mounting filesystems")
		}
	}()

	go func() {
		if conf.WebDAV != nil {
			port = webDAVPort
			if port == 0 {
				port = conf.WebDAV.Port
			}

			if err := webdav.NewWebDAVServer(cfs, port, conf.WebDAV.User, conf.WebDAV.Pass); err != nil {
				log.Error().Err(err).Msg("error starting webDAV")
			}
		}

		log.Warn().Msg("webDAV configuration not found!")
	}()

	httpfs := torrent.NewHTTPFS(cfs)
	logFilename := filepath.Join(conf.Log.Path, dlog.FileName)

	fusePath := "/distribyted-data/mount"
	if conf.Fuse != nil && conf.Fuse.Path != "" {
		fusePath = conf.Fuse.Path
	}

	err = http.New(fc, ss, ts, ch, servers, httpfs, logFilename, conf, fusePath)
	log.Error().Err(err).Msg("error initializing HTTP server")
	return err
}

func forceUnmount(mnt string) {
	if runtime.GOOS == "windows" {
		return
	}
	// Try both fusermount and umount
	_ = exec.Command("fusermount", "-uz", mnt).Run()
	_ = exec.Command("umount", "-l", mnt).Run()
}
