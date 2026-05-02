package http

import (
	"fmt"
	"net/http"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/shurcooL/httpfs/html/vfstemplate"

	"github.com/Apollogeddon/distribyted"
	"github.com/Apollogeddon/distribyted/config"
	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/Apollogeddon/distribyted/torrent"
)

func New(fc *filecache.Cache, ss *torrent.Stats, s *torrent.Service, ch *config.Handler, tss []*torrent.Server, fs http.FileSystem, logPath string, conf *config.Root, fusePath string) error {
	r, err := NewHandler(fc, ss, s, ch, tss, fs, logPath, conf, fusePath)
	if err != nil {
		return err
	}

	log.Info().Str(dlog.KeyHost, fmt.Sprintf("%s:%d", conf.HTTPGlobal.IP, conf.HTTPGlobal.Port)).Msg("starting webserver")

	if err := r.Run(fmt.Sprintf("%s:%d", conf.HTTPGlobal.IP, conf.HTTPGlobal.Port)); err != nil {
		return fmt.Errorf("error initializing server: %w", err)
	}

	return nil
}

func NewHandler(fc *filecache.Cache, ss *torrent.Stats, s torrentService, ch *config.Handler, tss []*torrent.Server, fs http.FileSystem, logPath string, conf *config.Root, fusePath string) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.ErrorLogger())
	r.Use(Logger())

	r.GET("/assets/*filepath", func(c *gin.Context) {
		c.FileFromFS(c.Request.URL.Path, http.FS(distribyted.Assets))
	})

	if conf.HTTPGlobal.HTTPFS {
		log.Info().Str(dlog.KeyHost, fmt.Sprintf("%s:%d/fs", conf.HTTPGlobal.IP, conf.HTTPGlobal.Port)).Msg("starting HTTPFS")
		h := func(c *gin.Context) {
			path := c.Param("filepath")
			c.FileFromFS(path, fs)
		}
		r.GET("/fs/*filepath", h)
		r.HEAD("/fs/*filepath", h)

	}

	t, err := vfstemplate.ParseGlob(http.FS(distribyted.Templates), nil, "/templates/*")
	if err != nil {
		return nil, fmt.Errorf("error parsing html: %w", err)
	}

	r.SetHTMLTemplate(t)

	r.GET("/", indexHandler)
	r.GET("/routes", routesHandler(ss))
	r.GET("/logs", logsHandler)
	r.GET("/servers", serversFoldersHandler())
	r.GET("/version/api", qBitWebapiVersionHandler)

	api := r.Group("/api")
	{
		api.GET("/log", apiLogHandler(logPath))
		api.GET("/status", apiStatusHandler(fc, ss))
		api.GET("/servers", apiServersHandler(tss))

		api.GET("/routes", apiRoutesHandler(ss))
		api.POST("/routes/:route/torrent", apiAddTorrentHandler(s))
		api.DELETE("/routes/:route/torrent/:torrent_hash", apiDelTorrentHandler(s))

	}

	qbit := r.Group("/api/v2")
	{
		qbit.POST("/auth/login", qBitLoginHandler)
		qbit.GET("/app/webapiVersion", qBitWebapiVersionHandler)
		qbit.GET("/app/version", qBitAppVersionHandler)
		qbit.GET("/app/preferences", qBitAppPreferencesHandler(conf, fusePath))
		qbit.POST("/app/setPreferences", qBitAppSetPreferencesHandler)
		qbit.GET("/transfer/info", qBitTransferInfoHandler(ss))
		qbit.GET("/torrents/info", qBitTorrentsInfoHandler(ss, fusePath))
		qbit.GET("/torrents/categories", qBitTorrentsCategoriesHandler(ch, ss, fusePath))
		qbit.POST("/torrents/createCategory", qBitTorrentsCreateCategoryHandler)
		qbit.POST("/torrents/removeCategories", qBitTorrentsRemoveCategoriesHandler)
		qbit.POST("/torrents/setCategory", qBitTorrentsMockHandler)
		qbit.POST("/torrents/addTags", qBitTorrentsMockHandler)
		qbit.POST("/torrents/pause", qBitTorrentsMockHandler)
		qbit.POST("/torrents/resume", qBitTorrentsMockHandler)
		qbit.POST("/torrents/add", qBitTorrentsAddHandler(s))
		qbit.POST("/torrents/delete", qBitTorrentsDeleteHandler(s))
	}

	return r, nil
}

func Logger() gin.HandlerFunc {
	l := dlog.Logger("http")
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		c.Next()
		if raw != "" {
			path = path + "?" + raw
		}
		msg := c.Errors.String()
		if msg == "" {
			msg = "Request"
		}

		s := c.Writer.Status()
		switch {
		case s >= 400 && s < 500:
			l.Warn().Str(dlog.KeyPath, path).Int("status", s).Msg(msg)
		case s >= 500:
			l.Error().Str(dlog.KeyPath, path).Int("status", s).Msg(msg)
		default:
			l.Debug().Str(dlog.KeyPath, path).Int("status", s).Msg(msg)
		}
	}
}
