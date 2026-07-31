package fs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var dummyFactories = map[string]FsFactory{
	".test": func(f File) (Filesystem, error) {
		return &DummyFs{}, nil
	},
}

func TestStorage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	s := newStorage(dummyFactories)

	err := s.Add(&Dummy{}, "/path/to/dummy/file.txt")
	require.NoError(err)

	err = s.Add(&Dummy{}, "/path/to/dummy/file2.txt")
	require.NoError(err)

	contains := s.Has("/path")
	require.True(contains)

	contains = s.Has("/path/to/dummy/")
	require.True(contains)

	file, err := s.Get("/path/to/dummy/file.txt")
	require.NoError(err)
	require.NotNil(file)
	require.IsType(&Dummy{}, file)

	file, err = s.Get("/path/to/dummy/file3.txt")
	require.Error(err)
	require.Nil(file)

	files, err := s.Children("/path/to/dummy/")
	require.NoError(err)
	require.Len(files, 2)
	require.Contains(files, "file.txt")
	require.Contains(files, "file2.txt")

	err = s.Add(&Dummy{}, "/path/to/dummy/folder/file.txt")
	require.NoError(err)

	files, err = s.Children("/path/to/dummy/")
	require.NoError(err)
	require.Len(files, 3)
	require.Contains(files, "file.txt")
	require.Contains(files, "file2.txt")
	require.Contains(files, "folder")

	err = s.Add(&Dummy{}, "path/file4.txt")
	require.NoError(err)

	require.True(s.Has("/path/file4.txt"))

	files, err = s.Children("/")
	require.NoError(err)
	require.Len(files, 1)

	err = s.Add(&Dummy{}, "/path/special_file.test")
	require.NoError(err)

	file, err = s.Get("/path/special_file.test/dir/here/file1.txt")
	require.NoError(err)
	require.NotNil(file)
	require.IsType(&Dummy{}, file)

	files, err = s.Children("/path/special_file.test")
	require.NoError(err)
	require.NotNil(files)

	files, err = s.Children("/path/special_file.test/dir/here")
	require.NoError(err)
	require.Len(files, 2)

	err = s.Add(&Dummy{}, "/path/to/__special__path/file3.txt")
	require.NoError(err)

	file, err = s.Get("/path/to/__special__path/file3.txt")
	require.NoError(err)
	require.NotNil(file)
	require.IsType(&Dummy{}, file)

	s.Clear()
}

func TestStorageRemove(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	s := newStorage(dummyFactories)

	err := s.Add(&Dummy{}, "/path/to/file.txt")
	require.NoError(err)
	require.True(s.Has("/path/to/file.txt"))

	err = s.Remove("/path/to/file.txt")
	require.NoError(err)
	require.False(s.Has("/path/to/file.txt"))

	err = s.Remove("/non/existent")
	require.Error(err)
}

func TestStorageWindowsPath(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	s := newStorage(dummyFactories)

	err := s.Add(&Dummy{}, "\\path\\to\\dummy\\file.txt")
	require.NoError(err)

	file, err := s.Get("\\path\\to\\dummy\\file.txt")
	require.NoError(err)
	require.NotNil(file)
	require.IsType(&Dummy{}, file)

	file, err = s.Get("/path/to/dummy/file.txt")
	require.NoError(err)
	require.NotNil(file)
	require.IsType(&Dummy{}, file)
}

func TestStorageAddFs(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	s := newStorage(dummyFactories)

	err := s.AddFS(&DummyFs{}, "/test")
	require.NoError(err)

	f, err := s.Get("/test/dir/here/file1.txt")
	require.NoError(err)
	require.NotNil(f)

	err = s.AddFS(&DummyFs{}, "/test")
	require.Error(err)
}

// recordingFs records the path it was asked to Open/ReadDir, so a test can
// assert not just that lookup succeeded, but which mount it was routed to.
type recordingFs struct {
	DummyFs
	openedPath string
}

func (r *recordingFs) Open(filename string) (File, error) {
	r.openedPath = filename
	return &Dummy{}, nil
}

func (r *recordingFs) ReadDir(path string) (map[string]File, error) {
	r.openedPath = path
	return map[string]File{}, nil
}

// TestStorageGetFileFromFs_SiblingPrefixCollision is the regression test for
// BACKLOG.md's "Route lookup collides on prefix": getFileFromFsLocked used
// to match mounts with a bare strings.HasPrefix, so mount "/movies" would
// also match path "/movies-4k/file.mkv" (no separator boundary), and since
// map iteration order is randomized, which mount actually served the
// request varied run to run. Rebuild storage many times so the random
// iteration order is exercised repeatedly; the fix must win regardless.
func TestStorageGetFileFromFs_SiblingPrefixCollision(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 20; i++ {
		s := newStorage(nil)
		movies := &recordingFs{}
		movies4k := &recordingFs{}
		require.NoError(s.AddFS(movies, "/movies"))
		require.NoError(s.AddFS(movies4k, "/movies-4k"))

		_, err := s.Get("/movies-4k/file.mkv")
		require.NoError(err)
		// getFileFromFsLocked prepends its own separator on top of the
		// leading "/" already left by TrimPrefix — pre-existing, harmless,
		// unrelated to the matching fix under test here.
		require.Equal("//file.mkv", movies4k.openedPath, "iteration %d: wrong mount served the request", i)
		require.Empty(movies.openedPath, "iteration %d: /movies should never have been asked", i)
	}
}

// TestStorageGetFileFromFs_NestedMountLongestMatch guards the other half of
// the same fix: picking whichever mount an unordered map iteration reaches
// first (rather than the longest/most specific match) misroutes a nested
// mount — e.g. an archive file mounted inside a route — to its parent route
// instead.
func TestStorageGetFileFromFs_NestedMountLongestMatch(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 20; i++ {
		s := newStorage(nil)
		route := &recordingFs{}
		archive := &recordingFs{}
		// Set up directly rather than via AddFS: AddFS's own pre-existence
		// check (is something already there?) would itself consult this
		// same lookup path we're testing, and recordingFs.Open unconditionally
		// succeeding for any path would make it think "/movies/archive.zip"
		// already exists as a plain file under the "/movies" mount before
		// we ever get to register the nested mount. Real archive mounts are
		// created this same direct way, via addLocked's factory dispatch,
		// not through AddFS.
		s.filesystems["/movies"] = route
		s.filesystems["/movies/archive.zip"] = archive

		_, err := s.Get("/movies/archive.zip/inside/file.txt")
		require.NoError(err)
		require.Equal("//inside/file.txt", archive.openedPath, "iteration %d: wrong mount served the request", i)
		require.Empty(route.openedPath, "iteration %d: /movies should never have been asked", i)
	}
}

func TestStorageRemoveByHash(t *testing.T) {
	require := require.New(t)
	s := newStorage(nil)

	f1 := &mockHashFile{hash: "h1"}
	f2 := &mockHashFile{hash: "h2"}

	_ = s.Add(f1, "/f1.txt")
	_ = s.Add(f2, "/f2.txt")

	removed := s.RemoveByHash("h1")
	require.False(s.Has("/f1.txt"))
	require.True(s.Has("/f2.txt"))
	require.Equal([]string{"/f1.txt"}, removed)
}

func TestStorageRemoveByHash_MultipleEntries(t *testing.T) {
	require := require.New(t)
	s := newStorage(nil)

	// Simulate a torrent-backed file with a virtual link pointing at it:
	// both entries share the same hash, and both must be reported removed.
	f := &mockHashFile{hash: "h1"}
	_ = s.Add(f, "/original.txt")
	_ = s.Add(f, "/linked.txt")

	removed := s.RemoveByHash("h1")
	require.False(s.Has("/original.txt"))
	require.False(s.Has("/linked.txt"))
	require.ElementsMatch([]string{"/original.txt", "/linked.txt"}, removed)
}

func TestStorageRemovePaths_PrunesEmptyParent(t *testing.T) {
	require := require.New(t)
	s := newStorage(nil)

	_ = s.Add(&Dummy{}, "/library/a/b/movie.mkv")
	// A sibling under /library keeps it non-empty once /library/a is
	// pruned, so we can assert pruning stops there instead of continuing
	// all the way to root.
	_ = s.Add(&Dummy{}, "/library/other.txt")
	require.True(s.Has("/library/a/b"))
	require.True(s.Has("/library/a"))

	removed, err := s.RemovePaths("/library/a/b/movie.mkv")
	require.NoError(err)
	require.False(s.Has("/library/a/b/movie.mkv"))
	require.False(s.Has("/library/a/b"))
	require.False(s.Has("/library/a"))
	require.ElementsMatch([]string{"/library/a/b/movie.mkv", "/library/a/b", "/library/a"}, removed)

	// /library still has other.txt, so it's not pruned — only empty
	// parents are removed.
	require.True(s.Has("/library"))
	require.True(s.Has("/library/other.txt"))
}

func TestStorageHasHash(t *testing.T) {
	require := require.New(t)
	s := newStorage(nil)

	f1 := &mockHashFile{hash: "h1"}
	f2 := &mockHashFile{hash: "h1"}

	require.False(s.HasHash("h1"))

	_ = s.Add(f1, "/f1.txt")
	require.True(s.HasHash("h1"))
	require.False(s.HasHash("h2"))

	// A second entry with the same hash (e.g. a link) keeps it present
	// after the first is removed.
	_ = s.Add(f2, "/f2.txt")
	_ = s.Remove("/f1.txt")
	require.True(s.HasHash("h1"))

	_ = s.Remove("/f2.txt")
	require.False(s.HasHash("h1"))
}

type mockHashFile struct {
	Dummy
	hash string
}

func (m *mockHashFile) MatchHash(h string) bool {
	return m.hash == h
}

func (m *mockHashFile) Hash() string {
	return m.hash
}

func TestSupportedFactories(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	factories := GetSupportedFactories()
	require.Contains(factories, ".zip")
	require.Contains(factories, ".rar")
	require.Contains(factories, ".7z")

	fs, err := factories[".zip"](&Dummy{})
	require.NoError(err)
	require.NotNil(fs)

	fs, err = factories[".rar"](&Dummy{})
	require.NoError(err)
	require.NotNil(fs)

	fs, err = factories[".7z"](&Dummy{})
	require.NoError(err)
	require.NotNil(fs)
}

var _ Filesystem = &DummyFs{}

type DummyFs struct {
}

func (d *DummyFs) Open(filename string) (File, error) {
	return &Dummy{}, nil
}

func (d *DummyFs) ReadDir(path string) (map[string]File, error) {
	if path == "/dir/here" {
		return map[string]File{
			"file1.txt": &Dummy{},
			"file2.txt": &Dummy{},
		}, nil
	}

	return nil, os.ErrNotExist
}

func (d *DummyFs) Link(oldpath, newpath string) error {
	return error(nil)
}

func (d *DummyFs) Rename(oldpath, newpath string) error {
	return error(nil)
}

func (d *DummyFs) Mkdir(path string) error {
	return error(nil)
}

func (d *DummyFs) Rmdir(path string) error {
	return error(nil)
}

func (d *DummyFs) Create(path string) error {
	return error(nil)
}

func (d *DummyFs) Remove(path string) error {
	return error(nil)
}

var _ File = &Dummy{}

type Dummy struct {
	BaseFile
}

func (d *Dummy) Size() int64 {
	return 0
}

func (d *Dummy) IsDir() bool {
	return false
}

func (d *Dummy) Close() error {
	return nil
}

func (d *Dummy) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (d *Dummy) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, nil
}
