package webdav

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebDAVServer(t *testing.T) {
	mfs := fs.NewMemory()

	// Calling with an invalid port to avoid blocking ListenAndServe
	err := NewWebDAVServer(mfs, -1, "admin", "admin")
	require.Error(t, err)

	handler := NewWebDAVHandler(mfs, "admin", "admin")
	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Without auth
	require.Equal(t, 401, w.Code)

	// With correct auth
	req.SetBasicAuth("admin", "admin")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	// it should hit the webdav handler, which will return 200 or 405 or 404
	require.NotEqual(t, 401, w2.Code)
}

func TestNewWebDAVServerWithListener(t *testing.T) {
	mfs := fs.NewMemory()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	
	go func() {
		_ = NewWebDAVServerWithListener(l, mfs, "user", "pass")
	}()
	
	addr := l.Addr().String()
	resp, err := http.Get("http://" + addr)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
	
	l.Close()
}
