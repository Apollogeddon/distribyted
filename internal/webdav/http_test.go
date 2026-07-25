package webdav

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/fs"
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

	// With wrong password
	reqWrongPass, _ := http.NewRequest("GET", "/", nil)
	reqWrongPass.SetBasicAuth("admin", "wrong")
	wWrongPass := httptest.NewRecorder()
	handler.ServeHTTP(wWrongPass, reqWrongPass)
	require.Equal(t, 401, wWrongPass.Code)

	// With wrong username
	reqWrongUser, _ := http.NewRequest("GET", "/", nil)
	reqWrongUser.SetBasicAuth("eve", "admin")
	wWrongUser := httptest.NewRecorder()
	handler.ServeHTTP(wWrongUser, reqWrongUser)
	require.Equal(t, 401, wWrongUser.Code)
}

func TestNewWebDAVHandler_UnsetCredentialsFailClosed(t *testing.T) {
	mfs := fs.NewMemory()
	handler := NewWebDAVHandler(mfs, "", "")

	// No Authorization header at all
	req1, _ := http.NewRequest("GET", "/", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	require.Equal(t, 401, w1.Code)

	// Empty/empty credentials explicitly presented
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.SetBasicAuth("", "")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	require.Equal(t, 401, w2.Code)

	// Random credentials presented
	req3, _ := http.NewRequest("GET", "/", nil)
	req3.SetBasicAuth("admin", "admin")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	require.Equal(t, 401, w3.Code)
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
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, 401, resp.StatusCode)

	_ = l.Close()
}
