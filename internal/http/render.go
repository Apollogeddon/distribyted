package http

import (
	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

// render writes a page or fragment with the given status. Headers set before it, such as
// HX-Trigger, go out with it.
func render(c *gin.Context, status int, comp templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := comp.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
	}
}
