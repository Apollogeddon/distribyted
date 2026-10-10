package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var indexHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", nil)
}

var logsHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "logs.html", nil)
}

var serversFoldersHandler = func() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "servers.html", nil)
	}
}
