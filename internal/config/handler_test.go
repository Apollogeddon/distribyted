package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHandler(t *testing.T) {
	t.Parallel()
	h := NewHandler("my/path")
	require.Equal(t, "my/path", h.p)
}

func TestHandlerGetRaw_FileExists(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("foo: bar\n")
	err := os.WriteFile(confPath, content, 0o644)
	require.NoError(err)

	h := NewHandler(confPath)
	b, err := h.GetRaw()
	require.NoError(err)
	require.Equal(content, b)
}

func TestHandlerGetRaw_FileNotExists(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "newdir", "config.yaml")

	h := NewHandler(confPath)
	b, err := h.GetRaw()
	require.NoError(err)
	require.NotEmpty(b)

	// Check if file was created
	_, err = os.Stat(confPath)
	require.NoError(err)
}

func TestHandlerGet(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "config.yaml")

	// Create a valid yaml config
	content := []byte("log:\n  debug: true\n")
	err := os.WriteFile(confPath, content, 0o644)
	require.NoError(err)

	h := NewHandler(confPath)
	conf, err := h.Get()
	require.NoError(err)
	require.NotNil(conf)
	require.True(conf.Log.Debug)

	// Ensure defaults were added
	require.NotNil(conf.Torrent)
}

func TestHandlerGet_InvalidYAML(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "config.yaml")

	// Create an invalid yaml config
	content := []byte("log:\n  level: debug\n\tinvalid_indent: true\n")
	err := os.WriteFile(confPath, content, 0o644)
	require.NoError(err)

	h := NewHandler(confPath)
	conf, err := h.Get()
	require.Error(err)
	require.Nil(conf)
	require.Contains(err.Error(), "error parsing configuration file")
}

func TestHandlerGetRaw_DirError(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// Test case where GetRaw fails because directory cannot be read as a file
	tmpDir := t.TempDir()

	h := NewHandler(tmpDir)
	b, err := h.GetRaw()
	require.Error(err)
	require.Nil(b)
	require.Contains(err.Error(), "error reading configuration file")
}

func TestHandlerGet_RawError(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// Using a directory as file path will cause GetRaw to fail
	tmpDir := t.TempDir()

	h := NewHandler(tmpDir)
	conf, err := h.Get()
	require.Error(err)
	require.Nil(conf)
	require.Contains(err.Error(), "error reading configuration file")
}

// A new install's web interface and WebDAV serve this machine only, both from the defaults and
// from the configuration file it generates: their logins travel in plain HTTP.
func TestHandlerGet_GeneratedConfigListensOnLoopback(t *testing.T) {
	t.Parallel()

	conf, err := NewHandler(filepath.Join(t.TempDir(), "config.yaml")).Get()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", conf.HTTPGlobal.IP)
	require.Equal(t, "127.0.0.1", conf.WebDAV.IP)
	require.Equal(t, "127.0.0.1", DefaultIP)
}
