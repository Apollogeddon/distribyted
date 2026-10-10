package fs

import (
	"os"
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

// TestContainer_RemoveByHash_FiresLinkRemoved is the regression test for the
// orphaned-link bug: RemoveByHash (the cascade fired when a torrent is torn
// down) must fire onLinkRemoved for every container-owned path it evicts —
// both the original entry and any virtual links sharing its hash — so the
// persisted link DB record is cleaned up in step with the live tree instead
// of surviving forever as an undeletable orphan.
func TestContainer_RemoveByHash_FiresLinkRemoved(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	var removedPaths []string
	c.OnLinkRemoved(func(path string) {
		removedPaths = append(removedPaths, path)
	})

	f := &mockHashFile{hash: "cascade-hash"}
	require.NoError(c.s.Add(f, "/original.txt"))
	require.NoError(c.Link("/original.txt", "/library/linked.txt"))

	c.RemoveByHash("cascade-hash")

	require.False(c.s.Has("/original.txt"))
	require.False(c.s.Has("/library/linked.txt"))
	// /library is pruned too since the link was its only content — and
	// onLinkRemoved must fire for it as well, or a directory link record
	// created via Mkdir would be orphaned the same way the file link would.
	require.False(c.s.Has("/library"))
	require.ElementsMatch([]string{"/original.txt", "/library/linked.txt", "/library"}, removedPaths)
}

// TestContainer_RemoveByHash_DoesNotFireLastRef guards a deliberate design
// choice: RemoveByHash must NOT fire onLastRefRemoved. It is itself the
// downstream of a torrent teardown (Service.RemoveFromHash -> a
// ts.OnTorrentRemoved listener -> RemoveByHash); firing onLastRefRemoved
// would call back into Service.RemoveFromHashOnly -> RemoveFromHash ->
// RemoveByHash a second time.
func TestContainer_RemoveByHash_DoesNotFireLastRef(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	lastRefFired := false
	c.OnLastReferenceRemoved(func(hash string) {
		lastRefFired = true
	})

	f := &mockHashFile{hash: "no-lastref"}
	require.NoError(c.s.Add(f, "/only-ref.txt"))

	c.RemoveByHash("no-lastref")

	require.False(c.s.Has("/only-ref.txt"))
	require.False(lastRefFired)
}

// TestContainer_RemoveByHash_CallbackReentry guards against the same
// deadlock class as TestContainer_LastReferenceRemoved_CallbackReentry:
// onLinkRemoved fired from RemoveByHash may re-enter this ContainerFs (in
// production, Service.RemoveLink -> DB write, but a caller could also read
// the tree), which would deadlock if fs.mu were still held.
func TestContainer_RemoveByHash_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	c.OnLinkRemoved(func(path string) {
		_, _ = c.ReadDir("/")
	})

	f := &mockHashFile{hash: "reentrant-hash"}
	require.NoError(c.s.Add(f, "/source.txt"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.RemoveByHash("reentrant-hash")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RemoveByHash deadlocked when its callback re-entered ContainerFs")
	}
}

// TestContainer_Link_CallbackReentry guards against the same deadlock class
// as TestContainer_LastReferenceRemoved_CallbackReentry, but for Link: a
// caller-supplied onLinkAdded callback may re-enter this ContainerFs (e.g.
// production wiring persists the link via Service.AddLink, and some wiring
// styles route back through cfs.Link), which would deadlock if fs.mu were
// still held across the callback.
func TestContainer_Link_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	c.OnLinkAdded(func(oldpath, newpath string) {
		_ = c.Link(oldpath, newpath) // re-enters ContainerFs, would deadlock if fs.mu were still held
	})

	f := &mockHashFile{hash: "reentrant-link"}
	require.NoError(c.s.Add(f, "/source.txt"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Link("/source.txt", "/dest.txt")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Link deadlocked when its callback re-entered ContainerFs")
	}
}

// TestContainer_Rename_CallbackReentry, TestContainer_Mkdir_CallbackReentry,
// TestContainer_Rmdir_CallbackReentry, and TestContainer_Create_CallbackReentry
// guard against the same deadlock class as the Link/Remove reentry tests
// above, for the remaining ContainerFs methods that fire callbacks. Any
// caller-supplied callback re-entering this ContainerFs — even just to read,
// via ReadDir's RLock — would deadlock if fs.mu were still held from the
// triggering call, since sync.RWMutex is not reentrant even for read locks
// held by the same goroutine that holds the write lock.
func TestContainer_Rename_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	c.OnLinkRenamed(func(oldpath, newpath string) {
		_, _ = c.ReadDir("/")
	})

	f := &mockHashFile{hash: "reentrant-rename"}
	require.NoError(c.s.Add(f, "/source.txt"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Rename("/source.txt", "/dest.txt")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Rename deadlocked when its callback re-entered ContainerFs")
	}
}

func TestContainer_Mkdir_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	c.OnLinkAdded(func(oldpath, newpath string) {
		_, _ = c.ReadDir("/")
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Mkdir("/newdir")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Mkdir deadlocked when its callback re-entered ContainerFs")
	}
}

func TestContainer_Rmdir_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)
	require.NoError(c.Mkdir("/rmdir-target"))

	c.OnLinkRemoved(func(path string) {
		_, _ = c.ReadDir("/")
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Rmdir("/rmdir-target")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Rmdir deadlocked when its callback re-entered ContainerFs")
	}
}

func TestContainer_Create_CallbackReentry(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	c, err := NewContainerFs(nil)
	require.NoError(err)

	c.OnLinkAdded(func(oldpath, newpath string) {
		_, _ = c.ReadDir("/")
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Create("/newfile.txt")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Create deadlocked when its callback re-entered ContainerFs")
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

// TestContainer_RoutesAndFoldersAreProtected covers what FUSE and WebDAV could do to a
// route: rmdir or mv on its mount point unmounted it until restart, removing a folder with
// links in it orphaned them, and renaming a file onto a folder deleted the file.
func TestContainer_RoutesAndFoldersAreProtected(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c, err := NewContainerFs(map[string]Filesystem{"/movies": &DummyFs{}})
	require.NoError(err)

	// the route and the torrents in it
	require.ErrorIs(c.Rmdir("/movies"), os.ErrPermission)
	require.ErrorIs(c.Rename("/movies", "/films"), os.ErrPermission)
	require.ErrorIs(c.Remove("/movies/dir/here/file1.txt"), os.ErrPermission)
	_, err = c.Open("/movies/dir/here/file1.txt")
	require.NoError(err, "the route is still mounted")

	// a folder of links
	require.NoError(c.Mkdir("/library"))
	require.ErrorIs(c.Mkdir("/library"), os.ErrExist)
	require.NoError(c.Link("/movies/dir/here/file1.txt", "/library/movie.mkv"))
	require.ErrorIs(c.Rmdir("/library"), ErrNotEmpty)
	require.ErrorIs(c.Rename("/library", "/lib"), ErrNotEmpty)
	_, err = c.Open("/library/movie.mkv")
	require.NoError(err, "the link is still there")

	// renaming onto an existing folder keeps both
	require.NoError(c.Mkdir("/other"))
	require.ErrorIs(c.Rename("/library/movie.mkv", "/other"), os.ErrExist)
	_, err = c.Open("/library/movie.mkv")
	require.NoError(err)

	// removing its last entry prunes the empty folder, and a later mkdir doesn't bring its
	// old link back
	require.NoError(c.Remove("/library/movie.mkv"))
	require.NoError(c.Mkdir("/library"))
	entries, err := c.ReadDir("/library")
	require.NoError(err)
	require.Empty(entries)
}

// TestContainer_LinkToArchive covers hard-linking an archive, as Sonarr and Radarr do on
// import: the link was made to the archive's folder view, so it opened as an empty file and
// listing it failed with "not a valid zip file".
func TestContainer_LinkToArchive(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	route := NewMemory()
	require.NoError(route.Storage.Add(NewMemoryFile(zipOf(t, "inside.txt", "hello")), "/T/a.zip"))
	c, err := NewContainerFs(map[string]Filesystem{"/route": route})
	require.NoError(err)

	require.NoError(c.Link("/route/T/a.zip", "/lib/a.zip"))
	entries, err := c.ReadDir("/lib/a.zip")
	require.NoError(err)
	require.Contains(entries, "inside.txt")

	// each open of an entry gets its own handle, so closing one can't pull the reader from
	// under another
	require.NoError(c.Link("/lib/a.zip/inside.txt", "/lib/inside.txt"))
	h1, err := c.Open("/lib/inside.txt")
	require.NoError(err)
	h2, err := c.Open("/lib/inside.txt")
	require.NoError(err)
	require.NotSame(h1, h2)
	require.NoError(h1.Close())
	buf := make([]byte, 5)
	n, err := h2.ReadAt(buf, 0)
	require.NoError(err)
	require.Equal("hello", string(buf[:n]))
}
