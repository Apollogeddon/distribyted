package log

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger(t *testing.T) {
	l := Logger("test")
	assert.NotNil(t, l)
}

func TestLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "testlogdir")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	conf := &config.Log{
		Path:  tmpDir,
		Debug: true,
	}

	Load(conf)

	l := Logger("test")
	l.Debug().Msg("test message")
}

func TestLoad_NoDebug(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "testlogdir2")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	Load(&config.Log{Path: tmpDir, Debug: false})
}

func TestLoad_WithUncreatableLogPath(t *testing.T) {
	// Place a regular file where Load will try to create a directory, so
	// newRollingFile returns nil. Without the nil-guard in Load this panics.
	tmpDir := t.TempDir()
	blockingFile := filepath.Join(tmpDir, "notadir")
	require.NoError(t, os.WriteFile(blockingFile, []byte(""), 0644))

	conf := &config.Log{
		Path:  filepath.Join(blockingFile, "logs"),
		Debug: false,
	}

	require.NotPanics(t, func() { Load(conf) })
}

func TestNewRollingFile(t *testing.T) {
	tmpDir := t.TempDir()
	conf := &config.Log{
		Path:       tmpDir,
		MaxBackups: 1,
		MaxSize:    1,
		MaxAge:     1,
	}
	w := newRollingFile(conf)
	assert.NotNil(t, w)

	w2 := newRollingFile(nil)
	assert.Nil(t, w2)
}
