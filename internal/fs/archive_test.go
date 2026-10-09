package fs

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/iio"
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
	require.True(err == nil || errors.Is(err, io.EOF))
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

	// an archive can't be changed
	require.Equal(os.ErrPermission, zfs.Link("", ""))
	require.Equal(os.ErrPermission, zfs.Rename("", ""))
	require.Equal(os.ErrPermission, zfs.Mkdir(""))
	require.Equal(os.ErrPermission, zfs.Rmdir(""))
	require.Equal(os.ErrPermission, zfs.Create("/newfile.txt"))
	require.Equal(os.ErrPermission, zfs.Remove("/path/to/test/file/1.txt"))
	_, err = zfs.Open("/path/to/test/file/1.txt")
	require.NoError(err)
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

// storedRar builds a RAR 4 archive holding files without compression, which is enough to
// exercise reading entries back; no rar tool is needed.
func storedRar(files map[string][]byte, order []string) []byte {
	var b bytes.Buffer
	header := func(body []byte) {
		crc := crc32.ChecksumIEEE(body)
		_ = binary.Write(&b, binary.LittleEndian, uint16(crc))
		b.Write(body)
	}
	b.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1a, 0x07, 0x00}) // marker
	// archive header: type, flags, size, reserved
	main := []byte{0x73}
	main = binary.LittleEndian.AppendUint16(main, 0)
	main = binary.LittleEndian.AppendUint16(main, 13)
	main = append(main, make([]byte, 6)...)
	header(main)
	for _, name := range order {
		data := files[name]
		h := []byte{0x74}
		h = binary.LittleEndian.AppendUint16(h, 0x8000)
		h = binary.LittleEndian.AppendUint16(h, uint16(32+len(name)))
		h = binary.LittleEndian.AppendUint32(h, uint32(len(data))) // packed size
		h = binary.LittleEndian.AppendUint32(h, uint32(len(data))) // unpacked size
		h = append(h, 0)                                           // host OS
		h = binary.LittleEndian.AppendUint32(h, crc32.ChecksumIEEE(data))
		h = binary.LittleEndian.AppendUint32(h, 0) // time
		h = append(h, 20, 0x30)                    // version 2.0, stored
		h = binary.LittleEndian.AppendUint16(h, uint16(len(name)))
		h = binary.LittleEndian.AppendUint32(h, 0x20) // attributes
		h = append(h, name...)
		header(h)
		b.Write(data)
	}
	end := []byte{0x7b}
	end = binary.LittleEndian.AppendUint16(end, 0x4000)
	end = binary.LittleEndian.AppendUint16(end, 7)
	header(end)
	return b.Bytes()
}

// TestRarFilesystem reads every entry of a RAR archive. Each entry used to share the one
// stream that listing had already read to the end, so every read failed.
func TestRarFilesystem(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	files := map[string][]byte{"first.txt": []byte("first file"), "second.txt": []byte("the second file")}
	data := storedRar(files, []string{"first.txt", "second.txt"})
	rfs := NewArchive(newCBR(data), int64(len(data)), &Rar{})

	dir, err := rfs.ReadDir("/")
	require.NoError(err)
	require.Len(dir, 2)

	// read the later entry first, then the earlier one
	for _, name := range []string{"second.txt", "first.txt"} {
		f, err := rfs.Open("/" + name)
		require.NoError(err)
		buf := make([]byte, len(files[name]))
		n, err := f.ReadAt(buf, 0)
		require.NoError(err, name)
		require.Equal(files[name], buf[:n])
		require.NoError(f.Close())
	}
}

// flakyLoader times out on its first listing, as a read from a slow swarm does.
type flakyLoader struct {
	calls int
	Zip
}

func (l *flakyLoader) getFiles(r iio.Reader, size int64) (map[string]*ArchiveFile, error) {
	l.calls++
	if l.calls == 1 {
		return nil, ErrReadTimeout
	}
	return l.Zip.getFiles(r, size)
}

func TestArchive_RetriesAfterATimeout(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	data := zipOf(t, "inside.txt", "hello")
	l := &flakyLoader{}
	a := NewArchive(newCBR(data), int64(len(data)), l)

	_, err := a.ReadDir("/")
	require.ErrorIs(err, ErrReadTimeout)
	entries, err := a.ReadDir("/")
	require.NoError(err, "a timeout must not leave the archive broken")
	require.Contains(entries, "inside.txt")
}
