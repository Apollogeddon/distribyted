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
	err := os.WriteFile(confPath, content, 0644)
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
	err := os.WriteFile(confPath, content, 0644)
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
	err := os.WriteFile(confPath, content, 0644)
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
