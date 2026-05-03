package fuse

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/stretchr/testify/require"
)

func checkWinFsp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("test for windows only")
	}

	// Check for WinFsp in registry
	cmd := exec.Command("reg", "query", "HKLM\\SOFTWARE\\WinFsp", "/v", "InstallDir")
	if err := cmd.Run(); err == nil {
		return
	}

	cmd = exec.Command("reg", "query", "HKLM\\SOFTWARE\\WOW6432Node\\WinFsp", "/v", "InstallDir")
	if err := cmd.Run(); err == nil {
		return
	}

	t.Skip("WinFsp not found, skipping FUSE tests")
}

func waitForFile(t *testing.T, path string) {
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for file: %s", path)
}

func TestHandler(t *testing.T) {
	checkWinFsp(t)

	require := require.New(t)

	p := "./testmnt"

	h := NewHandler(false, p)
	defer h.Unmount()

	mem := fs.NewMemory()

	err := mem.Storage.Add(fs.NewMemoryFile([]byte("test")), "/test.txt")
	require.NoError(err)

	cfs, err := fs.NewContainerFs(map[string]fs.Filesystem{"/mem": mem})
	require.NoError(err)

	err = h.Mount(cfs)
	require.NoError(err)

	target := filepath.Join(p, "mem", "test.txt")
	waitForFile(t, target)

	fi, err := os.Stat(target)
	require.NoError(err)

	require.False(fi.IsDir())
	require.Equal(int64(4), fi.Size())
}

func TestHandlerDriveLetter(t *testing.T) {
	checkWinFsp(t)

	require := require.New(t)

	p := "Z:"

	h := NewHandler(false, p)
	defer h.Unmount()

	mem := fs.NewMemory()

	err := mem.Storage.Add(fs.NewMemoryFile([]byte("test")), "/test.txt")
	require.NoError(err)

	cfs, err := fs.NewContainerFs(map[string]fs.Filesystem{"/mem": mem})
	require.NoError(err)

	err = h.Mount(cfs)
	require.NoError(err)

	target := filepath.Join(p, "mem", "test.txt")
	waitForFile(t, target)

	fi, err := os.Stat(target)
	require.NoError(err)

	require.False(fi.IsDir())
	require.Equal(int64(4), fi.Size())
}
