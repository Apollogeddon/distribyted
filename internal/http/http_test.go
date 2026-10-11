package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A slow client can't hold a connection open by never finishing its headers, while a long
// download over /fs isn't cut off.
func TestNewServer_Timeouts(t *testing.T) {
	srv := newServer("127.0.0.1:4444", http.NotFoundHandler())
	assert.Equal(t, "127.0.0.1:4444", srv.Addr)
	assert.Equal(t, readHeaderTimeout, srv.ReadHeaderTimeout)
	assert.Zero(t, srv.ReadTimeout)
	assert.Zero(t, srv.WriteTimeout)
}
