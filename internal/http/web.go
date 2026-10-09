package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var indexHandler = func(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", nil)
}
