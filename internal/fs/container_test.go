package fs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContainer(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	fss := map[string]Filesystem{
		"/test": &DummyFs{},
	}

	c, err := NewContainerFs(fss)
	require.NoError(err)

	f, err := c.Open("/test/dir/here")
	require.NoError(err)
	require.NotNil(f)

	files, err := c.ReadDir("/")
	require.NoError(err)
	require.Len(files, 1)

	// Test Mkdir
	err = c.Mkdir("/newdir")
	require.NoError(err)
	require.True(c.s.Has("/newdir"))

	// Test Link
	err = c.Link("/test/dir/here/file1.txt", "/linked_file.txt")
	require.NoError(err)
	require.True(c.s.Has("/linked_file.txt"))

	// Test Rename
	err = c.Rename("/linked_file.txt", "/renamed_file.txt")
	require.NoError(err)
	require.True(c.s.Has("/renamed_file.txt"))
	require.False(c.s.Has("/linked_file.txt"))

	// Test Rmdir
	err = c.Rmdir("/newdir")
	require.NoError(err)
	require.False(c.s.Has("/newdir"))
}

func TestContainer_LastReferenceRemoved(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	var removedHashes []string
	c.OnLastReferenceRemoved(func(hash string) {
		removedHashes = append(removedHashes, hash)
	})

	f := &mockHashFile{hash: "abc123"}
	require.NoError(c.s.Add(f, "/original.txt"))

	// Simulate a hardlink: a second path referencing the same underlying file.
	require.NoError(c.Link("/original.txt", "/linked.txt"))

	// One reference remains after removing the first, so no cascade yet.
	require.NoError(c.Remove("/original.txt"))
	require.Empty(removedHashes)

	// Removing the last reference triggers the cascade with the right hash.
	require.NoError(c.Remove("/linked.txt"))
	require.Equal([]string{"abc123"}, removedHashes)
}

// TestContainer_LastReferenceRemoved_CallbackReentry guards against a
// deadlock: OnLastReferenceRemoved fires while fs.mu must NOT be held,
// because in production the callback (Service.RemoveFromHashOnly) re-enters
// this same ContainerFs via RemoveByHash. Simulate that re-entry directly.
func TestContainer_LastReferenceRemoved_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	c.OnLastReferenceRemoved(func(hash string) {
		c.RemoveByHash(hash) // re-enters ContainerFs, would deadlock if fs.mu were still held
	})

	f := &mockHashFile{hash: "reentrant"}
	require.NoError(c.s.Add(f, "/only-ref.txt"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Remove("/only-ref.txt")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Remove deadlocked when its callback re-entered ContainerFs")
	}
}

func TestContainer_RemoveHashlessFileDoesNotCascade(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	called := false
	c.OnLastReferenceRemoved(func(hash string) {
		called = true
	})

	require.NoError(c.Create("/plain.txt"))
	require.NoError(c.Remove("/plain.txt"))
	require.False(called)
}
