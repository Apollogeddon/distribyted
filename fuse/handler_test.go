package fuse

import (
	"os"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHost struct {
	mountCalled   bool
	unmountCalled bool
	path          string
}

func (m *mockHost) Mount(path string, args []string) bool {
	m.mountCalled = true
	m.path = path
	return true
}

func (m *mockHost) Unmount() bool {
	m.unmountCalled = true
	return true
}

func TestHandler_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fuse-handler-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	h := NewHandler(true, tempDir)
	m := &mockHost{}
	h.host = m

	cfs, _ := fs.NewContainerFs(nil)
	err = h.Mount(cfs)
	require.NoError(t, err)

	// Wait a bit for the goroutine
	time.Sleep(100 * time.Millisecond)
	assert.True(t, m.mountCalled)
	assert.Equal(t, tempDir, m.path)

	h.Unmount()
	assert.True(t, m.unmountCalled)
}

func TestHandler_Unmount_Nil(t *testing.T) {
	h := &Handler{}
	h.Unmount() // Should not panic
}
