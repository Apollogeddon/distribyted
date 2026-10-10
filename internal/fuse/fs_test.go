package fuse

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/stretchr/testify/require"
	"github.com/winfsp/cgofuse/fuse"
)

type mockFile struct {
	fs.BaseFile
	isDir bool
	size  int64
	data  []byte
}

func (m *mockFile) IsDir() bool  { return m.isDir }
func (m *mockFile) Size() int64  { return m.size }
func (m *mockFile) Close() error { return nil }
func (m *mockFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, nil
	}
	n := copy(p, m.data[off:])
	return n, nil
}
func (m *mockFile) Read(p []byte) (int, error) { return 0, nil }
func (m *mockFile) MatchHash(h string) bool    { return false }

type mockFilesystem struct {
	fs.Filesystem
	files map[string]*mockFile
}

func (m *mockFilesystem) Open(path string) (fs.File, error) {
	if f, ok := m.files[path]; ok {
		return f, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFilesystem) ReadDir(path string) (map[string]fs.File, error) {
	out := make(map[string]fs.File)
	for p, f := range m.files {
		out[p] = f
	}
	return out, nil
}

func (m *mockFilesystem) Create(path string) error {
	m.files[path] = &mockFile{isDir: false, size: 0}
	return nil
}
func (m *mockFilesystem) Remove(path string) error     { return nil }
func (m *mockFilesystem) Mkdir(path string) error      { return nil }
func (m *mockFilesystem) Rmdir(path string) error      { return nil }
func (m *mockFilesystem) Link(old, new string) error   { return nil }
func (m *mockFilesystem) Rename(old, new string) error { return nil }

func TestFS_Unit(t *testing.T) {
	require := require.New(t)
	mfs := &mockFilesystem{
		files: map[string]*mockFile{
			"/test.txt": {isDir: false, size: 4, data: []byte("test")},
		},
	}
	mfs.files["/test.txt"].SetIno(123)

	f := NewFS(mfs).(*FS)

	t.Run("Statfs", func(t *testing.T) {
		stat := &fuse.Statfs_t{}
		errc := f.Statfs("/", stat)
		require.Equal(0, errc)
		require.NotZero(stat.Bsize)
	})

	t.Run("Getattr Root", func(t *testing.T) {
		stat := &fuse.Stat_t{}
		errc := f.Getattr("/", stat, fhNone)
		require.Equal(0, errc)
		require.Equal(uint32(fuse.S_IFDIR|0o777), stat.Mode)
	})

	t.Run("Getattr File", func(t *testing.T) {
		stat := &fuse.Stat_t{}
		errc := f.Getattr("/test.txt", stat, fhNone)
		require.Equal(0, errc)
		require.Equal(uint32(fuse.S_IFREG|0o777), stat.Mode)
		require.Equal(int64(4), stat.Size)
		require.Equal(uint64(123), stat.Ino)
	})

	t.Run("Open and Read", func(t *testing.T) {
		errc, fh := f.Open("/test.txt", 0)
		require.Equal(0, errc)
		require.NotEqual(fhNone, fh)

		dest := make([]byte, 4)
		n := f.Read("/test.txt", dest, 0, fh)
		require.Equal(4, n)
		require.Equal([]byte("test"), dest)

		errc = f.Release("/test.txt", fh)
		require.Equal(0, errc)
	})

	t.Run("Readdir", func(t *testing.T) {
		var names []string
		fill := func(name string, stat *fuse.Stat_t, ofst int64) bool {
			names = append(names, name)
			return true
		}
		errc := f.Readdir("/", fill, 0, fhNone)
		require.Equal(0, errc)
		require.Contains(names, ".")
		require.Contains(names, "..")
		require.Contains(names, "/test.txt")
	})

	t.Run("Mutation Ops", func(t *testing.T) {
		require.Equal(0, f.Mkdir("/dir", 0o755))
		require.Equal(0, f.Rmdir("/dir"))
		require.Equal(0, f.Unlink("/test.txt"))
		require.Equal(0, f.Link("/test.txt", "/link.txt"))
		require.Equal(0, f.Rename("/test.txt", "/new.txt"))

		errc, fh := f.Create("/newfile", 0, 0o644)
		require.Equal(0, errc)
		f.Release("/newfile", fh)
	})

	t.Run("Non-existent file", func(t *testing.T) {
		errc, _ := f.Open("/notexists", 0)
		require.Equal(-fuse.ENOENT, errc)
	})
}

func TestErrno(t *testing.T) {
	for err, want := range map[error]int{
		nil:                                      0,
		os.ErrNotExist:                           -fuse.ENOENT,
		os.ErrExist:                              -fuse.EEXIST,
		os.ErrPermission:                         -fuse.EPERM,
		fs.ErrNotEmpty:                           -fuse.ENOTEMPTY,
		fmt.Errorf("x: %w", fs.ErrEntryTooLarge): -fuse.EFBIG,
		fmt.Errorf("wrapped: %w", fs.ErrNotEmpty): -fuse.ENOTEMPTY,
		errors.New("anything else"):               -fuse.EIO,
	} {
		require.Equal(t, want, errno(err), "%v", err)
	}
}

// slowDirFs blocks ReadDir until release is closed, as listing an archive does while its
// central directory downloads.
type slowDirFs struct {
	fs.Filesystem
	release chan struct{}
}

func (s *slowDirFs) ReadDir(p string) (map[string]fs.File, error) {
	<-s.release
	return s.Filesystem.ReadDir(p)
}

// TestFileHandler_SlowListingDoesNotBlockReads guards against a slow listing stalling every
// open file: ListDir held the handle lock across ReadDir, a concurrent open queued for the
// write lock behind it, and every read then queued behind that.
func TestFileHandler_SlowListingDoesNotBlockReads(t *testing.T) {
	mem := fs.NewMemory()
	require.NoError(t, mem.Storage.Add(fs.NewMemoryFile([]byte("open already")), "/a.txt"))
	require.NoError(t, mem.Storage.Add(fs.NewMemoryFile([]byte("opened later")), "/b.txt"))
	slow := &slowDirFs{Filesystem: mem, release: make(chan struct{})}
	fh := &fileHandler{fs: slow}

	open, err := fh.OpenHolder("/a.txt")
	require.NoError(t, err)

	go func() { _, _ = fh.ListDir("/") }()
	time.Sleep(50 * time.Millisecond)
	go func() { _, _ = fh.OpenHolder("/b.txt") }()
	time.Sleep(50 * time.Millisecond)

	read := make(chan error, 1)
	go func() {
		_, err := fh.GetFile("/a.txt", open)
		read <- err
	}()
	select {
	case err := <-read:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("reading an open file waited for an unrelated directory listing")
	}
	close(slow.release)
}
