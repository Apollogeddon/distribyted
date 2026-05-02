package torrent

import (
	"io"
	"testing"

	"github.com/Apollogeddon/distribyted/fs"
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
