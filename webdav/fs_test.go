package webdav

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"
)

func TestWebDAVFilesystem(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mfs := fs.NewMemory()
	mf := fs.NewMemoryFile([]byte("test file content."))
	err := mfs.Storage.Add(mf, "/folder/file.txt")
	require.NoError(err)

	wfs := newFS(mfs)

	dir, err := wfs.OpenFile(context.Background(), "/", 0, 0)
	require.NoError(err)

	fi, err := dir.Readdir(0)
	require.NoError(err)
	require.Len(fi, 1)
	require.Equal("folder", fi[0].Name())
	
	// Test Readdir count > 0 and EOF
	fi2, err := dir.Readdir(1)
	require.ErrorIs(err, io.EOF)
	require.Nil(fi2)

	file, err := wfs.OpenFile(context.Background(), "/folder/file.txt", 0, 0)
	require.NoError(err)
	_, err = file.Readdir(0)
	require.ErrorIs(err, os.ErrInvalid)

	n, err := file.Seek(5, io.SeekStart)
	require.NoError(err)
	require.Equal(int64(5), n)

	// Test SeekCurrent
	n, err = file.Seek(2, io.SeekCurrent)
	require.NoError(err)
	require.Equal(int64(7), n)

	// Test SeekEnd
	n, err = file.Seek(-2, io.SeekEnd)
	require.NoError(err)
	require.Equal(int64(16), n)

	// Reset Seek
	n, err = file.Seek(0, io.SeekStart)
	require.NoError(err)
	require.Equal(int64(0), n)

	br := make([]byte, 4)
	nn, err := file.Read(br)
	require.NoError(err)
	require.Equal(4, nn)
	require.Equal([]byte("test"), br)

	n, err = file.Seek(0, io.SeekStart)
	require.NoError(err)
	require.Equal(int64(0), n)

	nn, err = file.Read(br)
	require.NoError(err)
	require.Equal(4, nn)
	require.Equal([]byte("test"), br)

	// Test file Stat
	fileStat, err := file.Stat()
	require.NoError(err)
	require.Equal(filepath.Base("//folder/file.txt"), fileStat.Name())
	require.Equal(int64(18), fileStat.Size())
	require.False(fileStat.IsDir())
	require.Equal(os.FileMode(0777), fileStat.Mode())
	require.NotNil(fileStat.ModTime())
	require.Nil(fileStat.Sys())

	// Test file Write
	wn, werr := file.Write([]byte("test"))
	require.Equal(0, wn)
	require.ErrorIs(werr, webdav.ErrNotImplemented)

	fInfo, err := wfs.Stat(context.Background(), "/folder/file.txt")
	require.NoError(err)
	require.Equal("/folder/file.txt", fInfo.Name())
	require.Equal(false, fInfo.IsDir())
	require.Equal(int64(18), fInfo.Size())
	require.Equal(os.FileMode(0777), fInfo.Mode())
	require.NotNil(fInfo.ModTime())
	require.Nil(fInfo.Sys())

	dirInfo, err := wfs.Stat(context.Background(), "/folder")
	require.NoError(err)
	require.True(dirInfo.IsDir())
	require.Equal(os.FileMode(0777)|os.ModeDir, dirInfo.Mode())
}

func TestMkdirRemoveRename(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mfs := fs.NewMemory()
	mf := fs.NewMemoryFile([]byte("test file content."))
	err := mfs.Storage.Add(mf, "/folder/file.txt")
	require.NoError(err)

	wfs := newFS(mfs)

	require.NoError(wfs.Mkdir(context.Background(), "test", 0))
	require.NoError(wfs.Rename(context.Background(), "test", "newTest"))
	
	// Test RemoveAll for Directory
	require.NoError(wfs.RemoveAll(context.Background(), "newTest"))

	// Test RemoveAll for non-existent file
	require.NoError(wfs.RemoveAll(context.Background(), "does-not-exist"))

	// Test RemoveAll for file
	require.NoError(wfs.RemoveAll(context.Background(), "folder/file.txt"))
}
