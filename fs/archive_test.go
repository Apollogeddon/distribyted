package fs

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/Apollogeddon/distribyted/iio"
	"github.com/stretchr/testify/require"
)

var fileContent []byte = []byte("Hello World")

func TestZipFilesystem(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	zReader, zLen := createTestZip(require)

	zfs := NewArchive(zReader, zLen, &Zip{})

	// Test ReadDir
	files, err := zfs.ReadDir("/path/to/test/file")
	require.NoError(err)

	require.Len(files, 1)
	f := files["1.txt"]
	require.NotNil(f)

	// Test Open
	f2, err := zfs.Open("/path/to/test/file/1.txt")
	require.NoError(err)
	require.NotNil(f2)
	require.False(f2.IsDir())
	require.Equal(int64(len(fileContent)), f2.Size())

	// Test Read
	out := make([]byte, 11)
	n, err := f2.Read(out)
	require.True(err == nil || err == io.EOF)
	require.Equal(11, n)
	require.Equal(fileContent, out)

	// Test ReadAt
	outAt := make([]byte, 5)
	n, err = f2.ReadAt(outAt, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte("World"), outAt)

	// Test Close
	require.NoError(f2.Close())

	// Test root Open
	root, err := zfs.Open("/")
	require.NoError(err)
	require.True(root.IsDir())

	// Test invalid Open
	_, err = zfs.Open("/notexists")
	require.Error(err)

	// Test ReadDir invalid path
	_, err = zfs.ReadDir("/invalid/path")
	require.Error(err)

	// Test mutation operations (some should return ErrPermission, some should work as memory-backed)
	require.Equal(os.ErrPermission, zfs.Link("", ""))
	require.Equal(os.ErrPermission, zfs.Rename("", ""))
	require.Equal(os.ErrPermission, zfs.Mkdir(""))
	require.Equal(os.ErrPermission, zfs.Rmdir(""))
	require.NoError(zfs.Create("/newfile.txt"))
	require.NoError(zfs.Remove("/newfile.txt"))
}

func TestZipFilesystem_Empty(t *testing.T) {
	require := require.New(t)
	buf := bytes.NewBuffer([]byte{})
	zWriter := zip.NewWriter(buf)
	require.NoError(zWriter.Close())

	zfs := NewArchive(newCBR(buf.Bytes()), int64(buf.Len()), &Zip{})
	files, err := zfs.ReadDir("/")
	require.NoError(err)
	require.Len(files, 0)
}

func TestRecursiveZipFilesystem(t *testing.T) {
	require := require.New(t)

	// 1. Create inner ZIP
	innerBuf := bytes.NewBuffer([]byte{})
	innerWriter := zip.NewWriter(innerBuf)
	f1, err := innerWriter.Create("inner.txt")
	require.NoError(err)
	_, err = f1.Write([]byte("inner content"))
	require.NoError(err)
	require.NoError(innerWriter.Close())

	// 2. Create outer ZIP containing inner ZIP
	outerBuf := bytes.NewBuffer([]byte{})
	outerWriter := zip.NewWriter(outerBuf)
	f2, err := outerWriter.Create("inner.zip")
	require.NoError(err)
	_, err = f2.Write(innerBuf.Bytes())
	require.NoError(err)
	require.NoError(outerWriter.Close())

	// 3. Mount outer ZIP
	zfs := NewArchive(newCBR(outerBuf.Bytes()), int64(outerBuf.Len()), &Zip{})

	// 4. Try to navigate into inner.zip
	// If recursion works, /inner.zip should be a directory (or mount point)
	// and we should be able to read /inner.zip/inner.txt
	f, err := zfs.Open("/inner.zip/inner.txt")
	require.NoError(err, "Recursive mounting should allow opening inner file")
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	require.NoError(err)
	require.Equal([]byte("inner content"), data)
}

func TestZipFilesystem_Corrupted(t *testing.T) {
	corrupted := []byte("this is not a valid zip file")
	zfs := NewArchive(newCBR(corrupted), int64(len(corrupted)), &Zip{})

	_, err := zfs.ReadDir("/")
	require.Error(t, err)

	// Open on a non-root path also propagates the load error
	_, err = zfs.Open("/some/file.txt")
	require.Error(t, err)
}

func TestRarFilesystem_Corrupted(t *testing.T) {
	corrupted := []byte("this is not a valid rar file")
	rfs := NewArchive(newCBR(corrupted), int64(len(corrupted)), &Rar{})

	_, err := rfs.ReadDir("/")
	require.Error(t, err)
}

func TestSevenZipFilesystem_Corrupted(t *testing.T) {
	corrupted := []byte("this is not a valid 7z file")
	sfs := NewArchive(newCBR(corrupted), int64(len(corrupted)), &SevenZip{})

	_, err := sfs.ReadDir("/")
	require.Error(t, err)
}

func TestZipFilesystem_CorruptedErrorPersists(t *testing.T) {
	// sync.Once only runs once; loadErr must be stored on the struct so that
	// subsequent calls after the first failure still return an error.
	corrupted := []byte("this is not a valid zip file")
	zfs := NewArchive(newCBR(corrupted), int64(len(corrupted)), &Zip{})

	_, err1 := zfs.ReadDir("/")
	require.Error(t, err1)

	_, err2 := zfs.ReadDir("/")
	require.Error(t, err2, "error must persist across repeated calls after first loadOnce failure")

	_, err3 := zfs.Open("/some/file.txt")
	require.Error(t, err3, "Open must also surface the stored load error")
}

func createTestZip(require *require.Assertions) (iio.Reader, int64) {
	buf := bytes.NewBuffer([]byte{})

	zWriter := zip.NewWriter(buf)

	f1, err := zWriter.Create("path/to/test/file/1.txt")
	require.NoError(err)
	_, err = f1.Write(fileContent)
	require.NoError(err)

	err = zWriter.Close()
	require.NoError(err)

	return newCBR(buf.Bytes()), int64(buf.Len())
}

type closeableByteReader struct {
	*bytes.Reader
}

func newCBR(b []byte) *closeableByteReader {
	return &closeableByteReader{
		Reader: bytes.NewReader(b),
	}
}

func (*closeableByteReader) Close() error {
	return nil
}
