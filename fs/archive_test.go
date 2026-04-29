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
	n, err := f.Read(out)
	require.Equal(io.EOF, err)
	require.Equal(11, n)
	require.Equal(fileContent, out)

	// Test ReadAt
	outAt := make([]byte, 5)
	n, err = f2.ReadAt(outAt, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte("World"), outAt)

	// Test Close
	require.NoError(f.Close())
	require.NoError(f2.Close())

	// Test root Open
	root, err := zfs.Open("/")
	require.NoError(err)
	require.True(root.IsDir())

	// Test Permission errors
	require.Equal(os.ErrPermission, zfs.Link("", ""))
	require.Equal(os.ErrPermission, zfs.Rename("", ""))
	require.Equal(os.ErrPermission, zfs.Mkdir(""))
	require.Equal(os.ErrPermission, zfs.Rmdir(""))
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
