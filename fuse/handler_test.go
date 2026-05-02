package fuse

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/fs"
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
