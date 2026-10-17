package fs

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestFileinfo(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	fi := NewFileInfo("name", 42, false)

	require.Equal(fi.IsDir(), false)
	require.Equal(fi.Name(), "name")
	require.Equal(fi.Size(), int64(42))
	require.NotNil(fi.ModTime())
	require.Equal(fi.Mode(), os.FileMode(0o777))
	require.Equal(fi.Sys(), nil)

	fiDir := NewFileInfo("dir", 0, true)
	require.Equal(fiDir.IsDir(), true)
	require.Equal(fiDir.Mode(), os.FileMode(0o777)|os.ModeDir)
}

func TestDir(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	d := &Dir{}
	require.True(d.IsDir())
	require.Equal(int64(0), d.Size())
	require.NoError(d.Close())

	n, err := d.Read(nil)
	require.Equal(0, n)
	require.ErrorIs(err, io.EOF)

	n, err = d.ReadAt(nil, 0)
	require.Equal(0, n)
	require.ErrorIs(err, io.EOF)
}

func TestContainerFs_Mutation(t *testing.T) {
	require := require.New(t)
	cfs, _ := NewContainerFs(nil)

	// Create
	err := cfs.Create("/test-route/file.txt")
	require.NoError(err) // storage.Add usually handles parent creation

	// Remove
	err = cfs.Remove("/test-route/file.txt")
	require.NoError(err)

	// RemoveByHash
	cfs.RemoveByHash("any-hash")
}

func TestContainerFs_Links(t *testing.T) {
	require := require.New(t)
	cfs, _ := NewContainerFs(nil)

	linkAdded := false
	cfs.OnLinkAdded(func(o, n string) {
		linkAdded = true
	})

	linkRemoved := false
	cfs.OnLinkRemoved(func(p string) {
		linkRemoved = true
	})

	cfs.onLinkAdded("o", "n")
	require.True(linkAdded)

	cfs.onLinkRemoved("p")
	require.True(linkRemoved)
}

// blocksContent is n 4 KiB blocks, each starting with its index.
func blocksContent(n int) []byte {
	b := make([]byte, n*4096)
	for i := range n {
		b[i*4096], b[i*4096+1] = byte(i>>8), byte(i)
	}
	return b
}

// readAllConcurrently reads f to its end with 8 goroutines calling Read at once, and checks
// every block came back exactly once.
func readAllConcurrently(t *testing.T, f File, blocks int) {
	t.Helper()
	var mu sync.Mutex
	seen := make([]int, blocks)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := make([]byte, 4096)
			for {
				n, err := f.Read(p)
				if n == 4096 {
					mu.Lock()
					seen[int(p[0])<<8|int(p[1])]++
					mu.Unlock()
				} else if n != 0 {
					t.Errorf("short read of %d bytes", n)
				}
				if err != nil || n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	for i, c := range seen {
		if c != 1 {
			t.Errorf("block %d read %d times", i, c)
		}
	}
}

func TestTorrentFileHandle_ConcurrentReadsDontRepeat(t *testing.T) {
	content := blocksContent(256)
	tf := &torrentFile{
		readerFunc: func() torrent.Reader { return &seekingTorrentReader{Reader: bytes.NewReader(content)} },
		len:        int64(len(content)),
		timeout:    5,
		stats:      &readStats{},
		log:        zerolog.Nop(),
	}
	h := tf.NewHandle()
	defer func() { _ = h.Close() }()
	readAllConcurrently(t, h, 256)
}

func TestArchiveFileHandle_ConcurrentReadsDontRepeat(t *testing.T) {
	content := blocksContent(256)
	af := NewArchiveFile(func() (io.Reader, error) { return bytes.NewReader(content), nil }, int64(len(content)))
	h := af.NewHandle()
	defer func() { _ = h.Close() }()
	readAllConcurrently(t, h, 256)
}
