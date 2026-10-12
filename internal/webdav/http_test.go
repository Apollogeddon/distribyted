package webdav

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebDAVServer(t *testing.T) {
	mfs := fs.NewMemory()

	// Calling with an invalid port to avoid blocking ListenAndServe
	err := NewWebDAVServer(mfs, "127.0.0.1", -1, "admin", "admin")
	require.Error(t, err)

	handler := NewWebDAVHandler(mfs, "admin", "admin")
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
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
	reqWrongPass, _ := http.NewRequest(http.MethodGet, "/", nil)
	reqWrongPass.SetBasicAuth("admin", "wrong")
	wWrongPass := httptest.NewRecorder()
	handler.ServeHTTP(wWrongPass, reqWrongPass)
	require.Equal(t, 401, wWrongPass.Code)

	// With wrong username
	reqWrongUser, _ := http.NewRequest(http.MethodGet, "/", nil)
	reqWrongUser.SetBasicAuth("eve", "admin")
	wWrongUser := httptest.NewRecorder()
	handler.ServeHTTP(wWrongUser, reqWrongUser)
	require.Equal(t, 401, wWrongUser.Code)
}

func TestNewWebDAVHandler_UnsetCredentialsFailClosed(t *testing.T) {
	mfs := fs.NewMemory()
	handler := NewWebDAVHandler(mfs, "", "")

	// No Authorization header at all
	req1, _ := http.NewRequest(http.MethodGet, "/", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	require.Equal(t, 401, w1.Code)

	// Empty/empty credentials explicitly presented
	req2, _ := http.NewRequest(http.MethodGet, "/", nil)
	req2.SetBasicAuth("", "")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	require.Equal(t, 401, w2.Code)

	// Random credentials presented
	req3, _ := http.NewRequest(http.MethodGet, "/", nil)
	req3.SetBasicAuth("admin", "admin")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	require.Equal(t, 401, w3.Code)
}

func TestNewWebDAVServer_BindsConfiguredAddress(t *testing.T) {
	// take a free port, then hand it to the server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	go func() { _ = NewWebDAVServer(fs.NewMemory(), "127.0.0.1", port, "user", "pass") }()

	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		resp, err := http.Get(url) //nolint:noctx // a test probe
		if !assert.NoError(c, err) {
			return
		}
		_ = resp.Body.Close()
		assert.Equal(c, http.StatusUnauthorized, resp.StatusCode)
	}, 5*time.Second, 20*time.Millisecond)

	// an address the host doesn't have is refused rather than widened to every interface
	require.Error(t, NewWebDAVServer(fs.NewMemory(), "192.0.2.1", port, "user", "pass"))
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

// A slow client can't hold a connection open by never finishing its headers, while a long
// download isn't cut off.
func TestNewServer_Timeouts(t *testing.T) {
	srv := newServer(fs.NewMemory(), "user", "pass")
	assert.Equal(t, readHeaderTimeout, srv.ReadHeaderTimeout)
	assert.Zero(t, srv.ReadTimeout)
	assert.Zero(t, srv.WriteTimeout)
}
