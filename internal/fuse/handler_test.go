package fuse

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHost struct {
	mu            sync.Mutex
	mountCalled   bool
	unmountCalled bool
	path          string
}

func (m *mockHost) Mount(path string, args []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mountCalled = true
	m.path = path
	return true
}

func (m *mockHost) Unmount() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unmountCalled = true
	return true
}

func (m *mockHost) wasMountCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mountCalled
}

func (m *mockHost) getPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.path
}

func (m *mockHost) wasUnmountCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unmountCalled
}

func TestHandler_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fuse-handler-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	h := NewHandler(true, tempDir)
	m := &mockHost{}
	h.host = m

	cfs, _ := fs.NewContainerFs(nil)
	err = h.Mount(cfs)
	require.NoError(t, err)

	// Wait a bit for the goroutine
	time.Sleep(100 * time.Millisecond)
	assert.True(t, m.wasMountCalled())
	assert.Equal(t, tempDir, m.getPath())

	h.Unmount()
	assert.True(t, m.wasUnmountCalled())
}

func TestHandler_Unmount_Nil(t *testing.T) {
	h := &Handler{}
	h.Unmount() // Should not panic
}

func TestHandler_MountedFollowsInitAndDestroy(t *testing.T) {
	h := NewHandler(false, t.TempDir())
	assert.False(t, h.Mounted())

	cfs, err := fs.NewContainerFs(nil)
	require.NoError(t, err)
	tr := &mountTracker{FileSystemInterface: NewFS(cfs), mounted: &h.mounted}

	tr.Init()
	assert.True(t, h.Mounted())
	tr.Destroy()
	assert.False(t, h.Mounted())
}

func TestHandler_NotMountedAfterMountReturns(t *testing.T) {
	h := NewHandler(false, t.TempDir())
	h.host = &mockHost{}
	h.mounted.Store(true)

	cfs, err := fs.NewContainerFs(nil)
	require.NoError(t, err)
	require.NoError(t, h.Mount(cfs))

	// the mock's Mount returns at once, as a failed or finished mount does
	assert.Eventually(t, func() bool { return !h.Mounted() }, time.Second, 10*time.Millisecond)
}
