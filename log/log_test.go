package log

import (
	"os"
	"testing"

	"github.com/Apollogeddon/distribyted/config"
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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)
	Load(&config.Log{Path: tmpDir, Debug: false})
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
