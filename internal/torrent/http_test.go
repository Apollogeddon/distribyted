package torrent

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPFS_Open(t *testing.T) {
	mfs := fs.NewMemory()
	data := []byte("test data")
	err := mfs.Storage.Add(fs.NewMemoryFile(data), "/test.txt")
	require.NoError(t, err)

	hfs := NewHTTPFS(mfs)

	// Test open file
	f, err := hfs.Open("/test.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), stat.Size())
	assert.False(t, stat.IsDir())

	// Test open dir
	d, err := hfs.Open("/")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	stat, err = d.Stat()
	require.NoError(t, err)
	assert.True(t, stat.IsDir())

	files, err := d.Readdir(0)
	require.NoError(t, err)
	assert.Equal(t, 1, len(files))
	assert.Equal(t, "test.txt", files[0].Name())

	// Test Readdir with count
	d2, _ := hfs.Open("/")
	files2, err := d2.Readdir(1)
	require.NoError(t, err)
	assert.Equal(t, 1, len(files2))

	files3, err := d2.Readdir(1)
	require.Equal(t, io.EOF, err)
	assert.Equal(t, 0, len(files3))
}

func TestHTTPFS_OpenNotFound(t *testing.T) {
	mfs := fs.NewMemory()
	hfs := NewHTTPFS(mfs)

	_, err := hfs.Open("/notfound.txt")
	assert.Error(t, err)
}

// countingFS counts ReadDir calls and can fail them.
type countingFS struct {
	*fs.Memory
	readDirs atomic.Int32
	err      error
}

func (c *countingFS) ReadDir(path string) (map[string]fs.File, error) {
	c.readDirs.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return c.Memory.ReadDir(path)
}

func TestHTTPFS_ListsFolderOnFirstReaddir(t *testing.T) {
	mfs := &countingFS{Memory: fs.NewMemory()}
	require.NoError(t, mfs.Storage.Add(fs.NewMemoryFile([]byte("data")), "/dir/a.txt"))
	hfs := NewHTTPFS(mfs)

	f, err := hfs.Open("/dir/a.txt")
	require.NoError(t, err)
	_ = f.Close()
	assert.Zero(t, mfs.readDirs.Load(), "serving a file lists nothing")

	d, err := hfs.Open("/dir")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.Zero(t, mfs.readDirs.Load(), "opening a folder lists nothing yet")

	files, err := d.Readdir(1)
	require.NoError(t, err)
	require.Len(t, files, 1)
	_, err = d.Readdir(1)
	require.Equal(t, io.EOF, err)
	assert.Equal(t, int32(1), mfs.readDirs.Load(), "a folder is listed once")
}

func TestHTTPFS_ReaddirError(t *testing.T) {
	boom := errors.New("boom")
	mfs := &countingFS{Memory: fs.NewMemory(), err: boom}
	hfs := NewHTTPFS(mfs)

	d, err := hfs.Open("/")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	_, err = d.Readdir(0)
	require.ErrorIs(t, err, boom)
}
