package webdav

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/stretchr/testify/require"
)

func TestNewWebDAVServer(t *testing.T) {
	mfs := fs.NewMemory()

	// Calling with an invalid port to avoid blocking ListenAndServe
	err := NewWebDAVServer(mfs, -1, "admin", "admin")
	require.Error(t, err)

	// Since NewWebDAVServer registers on the DefaultServeMux, we can test it
	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, req)

	// Without auth
	require.Equal(t, 401, w.Code)

	// With correct auth
	req.SetBasicAuth("admin", "admin")
	w2 := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w2, req)
	// it should hit the webdav handler, which will return 200 or 405 or 404
	require.NotEqual(t, 401, w2.Code)
}
