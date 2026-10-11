package http

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
)

// authConf turns authentication on, which the health endpoints must not need.
func authConf() *config.Root {
	return &config.Root{HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, User: "test", Pass: "test"}}
}

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestHealthz_ServesWithoutAuth(t *testing.T) {
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", authConf(), "", nil, WithReadiness(func() bool { return false }))
	require.NoError(t, err)

	// liveness doesn't depend on readiness
	w := serve(t, r, http.MethodGet, "/healthz")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())

	w = serve(t, r, http.MethodHead, "/healthz")
	assert.Equal(t, http.StatusOK, w.Code)

	// the rest of the server still asks for a login
	w = serve(t, r, http.MethodGet, "/api/status")
	assert.Equal(t, http.StatusFound, w.Code)
}

func TestReadyz_ReadyWithoutReadinessCheck(t *testing.T) {
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", authConf(), "", nil)
	require.NoError(t, err)

	w := serve(t, r, http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestReadyz_FollowsReadiness(t *testing.T) {
	var ready atomic.Bool
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", authConf(), "", nil, WithReadiness(ready.Load))
	require.NoError(t, err)

	w := serve(t, r, http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "not ready", w.Body.String())

	w = serve(t, r, http.MethodHead, "/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	ready.Store(true)
	w = serve(t, r, http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}
