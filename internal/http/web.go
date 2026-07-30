package http

import (
	"net/http"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/gin-gonic/gin"
)

var indexHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", nil)
}

var routesHandler = func(ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "routes.html", ss.RoutesStats())
	}
}

var logsHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "logs.html", nil)
}

var serversFoldersHandler = func() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "servers.html", nil)
	}
}

var linksPageHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "links.html", nil)
}

var filesPageHandler = func(conf *config.Root) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "files.html", gin.H{"HTTPFS": conf.HTTPGlobal.HTTPFS})
	}
}
