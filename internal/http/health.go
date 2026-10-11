package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HandlerOption configures NewHandler.
type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	ready func() bool
}

// WithReadiness sets what /readyz reports. Without it the handler is ready as soon as it serves,
// as it is built only after the torrent client has started and the routes have loaded.
func WithReadiness(ready func() bool) HandlerOption {
	return func(o *handlerOptions) {
		o.ready = ready
	}
}

// registerHealth adds /healthz and /readyz. They answer without authentication, so they say
// nothing but the status, and they aren't logged, as probes call them every few seconds.
func registerHealth(r *gin.Engine, ready func() bool) {
	healthz := func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	}
	readyz := func(c *gin.Context) {
		if ready != nil && !ready() {
			c.String(http.StatusServiceUnavailable, "not ready")
			return
		}
		c.String(http.StatusOK, "ok")
	}
	r.GET("/healthz", healthz)
	r.HEAD("/healthz", healthz)
	r.GET("/readyz", readyz)
	r.HEAD("/readyz", readyz)
}
