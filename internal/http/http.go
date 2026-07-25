package http

import (
	"fmt"
	"net/http"
	"path"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/shurcooL/httpfs/html/vfstemplate"

	"github.com/Apollogeddon/distribyted/internal/config"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/Apollogeddon/distribyted/web"
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
	r.RedirectFixedPath = true
	r.RedirectTrailingSlash = true
	r.Use(gin.Recovery())
	r.Use(gin.ErrorLogger())
	r.Use(Logger())

	r.GET("/assets/*filepath", func(c *gin.Context) {
		c.FileFromFS(c.Request.URL.Path, http.FS(web.Assets))
	})

	t, err := vfstemplate.ParseGlob(http.FS(web.Templates), nil, "/templates/*")
	if err != nil {
		return nil, fmt.Errorf("error parsing html: %w", err)
	}

	r.SetHTMLTemplate(t)

	ac := newAuthConfig(conf.HTTPGlobal)
	st := newSessionStore(sessionTTL)
	browserAuth := browserAuthMiddleware(ac, st)
	qbitAuth := qbitAuthMiddleware(ac, st)

	r.GET("/login", loginPageHandler)
	r.POST("/login", loginSubmitHandler(ac, st))
	r.Any("/logout", logoutHandler(st))

	if conf.HTTPGlobal.HTTPFS {
		log.Info().Str(dlog.KeyHost, fmt.Sprintf("%s:%d/fs", conf.HTTPGlobal.IP, conf.HTTPGlobal.Port)).Msg("starting HTTPFS")
		h := func(c *gin.Context) {
			p := path.Clean(c.Param("filepath"))
			c.FileFromFS(p, fs)
		}
		fsGroup := r.Group("/fs", browserAuth)
		fsGroup.GET("/*filepath", h)
		fsGroup.HEAD("/*filepath", h)
	}

	pages := r.Group("", browserAuth)
	{
		pages.Any("/", indexHandler)
		pages.GET("/routes", routesHandler(ss))
		pages.GET("/logs", logsHandler)
		pages.GET("/servers", serversFoldersHandler())
		pages.GET("/version/api", qBitWebapiVersionHandler)
	}

	api := r.Group("/api", browserAuth)
	{
		api.GET("/log", apiLogHandler(logPath))
		api.GET("/status", apiStatusHandler(fc, ss))
		api.GET("/servers", apiServersHandler(tss))

		api.GET("/routes", apiRoutesHandler(ss))
		api.POST("/routes/:route/torrent", apiAddTorrentHandler(s))
		api.DELETE("/routes/:route/torrent/:torrent_hash", apiDelTorrentHandler(s))

	}

	cs := newCategoryStore()

	qbitPublic := r.Group("/api/v2")
	{
		qbitPublic.Any("/auth/login", qBitLoginHandler(ac, st))
		qbitPublic.Any("/auth/logout", qBitLogoutHandler(st))
	}

	qbit := r.Group("/api/v2", qbitAuth)
	{
		qbit.Any("/app/webapiVersion", qBitWebapiVersionHandler)
		qbit.Any("/app/version", qBitAppVersionHandler)
		qbit.Any("/app/preferences", qBitAppPreferencesHandler(conf, fusePath))
		qbit.Any("/app/setPreferences", qBitAppSetPreferencesHandler)
		qbit.Any("/transfer/info", qBitTransferInfoHandler(ss))
		qbit.Any("/transfer/speedLimitsMode", qBitTransferSpeedLimitsModeHandler)
		qbit.Any("/transfer/toggleSpeedLimitsMode", qBitTorrentsMockHandler)
		qbit.Any("/torrents/info", qBitTorrentsInfoHandler(ss, fusePath))
		qbit.Any("/torrents/categories", qBitTorrentsCategoriesHandler(cs, ch, ss, fusePath))
		qbit.Any("/torrents/createCategory", qBitTorrentsCreateCategoryHandler(cs))
		qbit.Any("/torrents/removeCategories", qBitTorrentsRemoveCategoriesHandler(cs))
		qbit.Any("/torrents/setCategory", qBitTorrentsMockHandler)
		qbit.Any("/torrents/addTags", qBitTorrentsMockHandler)
		qbit.Any("/torrents/pause", qBitTorrentsMockHandler)
		qbit.Any("/torrents/resume", qBitTorrentsMockHandler)
		qbit.Any("/sync/maindata", qBitSyncMaindataHandler(ss, cs, ch, fusePath))
		qbit.Any("/torrents/add", qBitTorrentsAddHandler(s))
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
