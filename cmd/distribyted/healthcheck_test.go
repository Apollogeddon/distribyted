package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestHealthcheckURL_WithoutConfigUsesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.yaml")

	url, err := healthcheckURL(p, "", 0, false)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:4444/healthz", url)
	assert.NoFileExists(t, p, "a health check must not write a configuration")
}

func TestHealthcheckURL_FromConfig(t *testing.T) {
	p := writeConfig(t, "http:\n  ip: 10.1.2.3\n  port: 5555\n")

	url, err := healthcheckURL(p, "", 0, true)
	require.NoError(t, err)
	assert.Equal(t, "http://10.1.2.3:5555/readyz", url)
}

func TestHealthcheckURL_ConfigWithoutPort(t *testing.T) {
	p := writeConfig(t, "http:\n  ip: 10.1.2.3\n  user: admin\n  pass: secret\n")

	url, err := healthcheckURL(p, "", 0, false)
	require.NoError(t, err)
	assert.Equal(t, "http://10.1.2.3:4444/healthz", url)
}

func TestHealthcheckURL_PortFlagAndUnspecifiedIP(t *testing.T) {
	p := writeConfig(t, "http:\n  ip: \"::\"\n  port: 5555\n")

	url, err := healthcheckURL(p, "", 6666, false)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:6666/healthz", url)
}

func TestHealthcheck(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" && !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	p := writeConfig(t, "http:\n  ip: "+host+"\n  port: "+port+"\n")

	require.NoError(t, healthcheck(p, "", 0, false))
	require.Error(t, healthcheck(p, "", 0, true))
	ready.Store(true)
	require.NoError(t, healthcheck(p, "", 0, true))

	n, err := strconv.Atoi(port)
	require.NoError(t, err)
	srv.Close()
	assert.Error(t, healthcheck(p, "", n, false), "nothing listening")
}

func TestHealthcheckURL_IPFlag(t *testing.T) {
	p := writeConfig(t, "http:\n  ip: 10.1.2.3\n  port: 5555\n")

	url, err := healthcheckURL(p, "10.9.9.9", 0, false)
	require.NoError(t, err)
	assert.Equal(t, "http://10.9.9.9:5555/healthz", url)

	// as the container image sets it: every address, reached on loopback
	url, err = healthcheckURL(p, "0.0.0.0", 0, false)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:5555/healthz", url)
}
