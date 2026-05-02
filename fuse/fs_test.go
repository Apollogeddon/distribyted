package fuse

import (
	"os"
	"testing"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/stretchr/testify/require"
)

type mockFile struct {
	fs.BaseFile
	isDir bool
	size  int64
	data  []byte
}

func (m *mockFile) IsDir() bool { return m.isDir }
func (m *mockFile) Size() int64 { return m.size }
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
func (m *mockFilesystem) Remove(path string) error               { return nil }
func (m *mockFilesystem) Mkdir(path string) error                { return nil }
func (m *mockFilesystem) Rmdir(path string) error                { return nil }
func (m *mockFilesystem) Link(old, new string) error             { return nil }
func (m *mockFilesystem) Rename(old, new string) error           { return nil }

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
		require.Equal(uint32(fuse.S_IFDIR|0777), stat.Mode)
	})

	t.Run("Getattr File", func(t *testing.T) {
		stat := &fuse.Stat_t{}
		errc := f.Getattr("/test.txt", stat, fhNone)
		require.Equal(0, errc)
		require.Equal(uint32(fuse.S_IFREG|0777), stat.Mode)
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
		require.Equal(0, f.Mkdir("/dir", 0755))
		require.Equal(0, f.Rmdir("/dir"))
		require.Equal(0, f.Unlink("/test.txt"))
		require.Equal(0, f.Link("/test.txt", "/link.txt"))
		require.Equal(0, f.Rename("/test.txt", "/new.txt"))
		
		errc, fh := f.Create("/newfile", 0, 0644)
		require.Equal(0, errc)
		f.Release("/newfile", fh)
	})

	t.Run("Non-existent file", func(t *testing.T) {
		errc, _ := f.Open("/notexists", 0)
		require.Equal(-fuse.ENOENT, errc)
	})
}
