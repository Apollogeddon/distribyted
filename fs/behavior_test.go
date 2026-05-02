package fs

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"
)

type mockTorrent struct {
	Torrent
	hash metainfo.Hash
	info *metainfo.Info
}

func (m *mockTorrent) InfoHash() metainfo.Hash { return m.hash }
func (m *mockTorrent) Info() *metainfo.Info    { return m.info }
func (m *mockTorrent) GotInfo() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (m *mockTorrent) Files() []*torrent.File                { return nil }
func (m *mockTorrent) Name() string                        { return "mock" }
func (m *mockTorrent) PieceStateRuns() torrent.PieceStateRuns { return nil }
func (m *mockTorrent) Stats() torrent.TorrentStats           { return torrent.TorrentStats{} }
func (m *mockTorrent) Drop()                               {}

func TestBehavior_TransientMutability(t *testing.T) {
	require := require.New(t)
	tfs := NewTorrent(10)

	// Add a manual file (simulating a torrent file or manual creation)
	err := tfs.Create("/test.txt")
	require.NoError(err)

	// Verify it exists
	files, err := tfs.ReadDir("/")
	require.NoError(err)
	require.Contains(files, "test.txt")

	// Delete it
	err = tfs.Remove("/test.txt")
	require.NoError(err)

	// Verify it's gone
	files, err = tfs.ReadDir("/")
	require.NoError(err)
	require.NotContains(files, "test.txt")

	_, err = tfs.Open("/test.txt")
	require.Error(err)
}

func TestBehavior_HardLinks(t *testing.T) {
	require := require.New(t)
	tfs := NewTorrent(10)

	err := tfs.Create("/original.txt")
	require.NoError(err)
	
	f, err := tfs.Open("/original.txt")
	require.NoError(err)
	originalIno := f.Ino()

	// Create a link
	err = tfs.Link("/original.txt", "/linked.txt")
	require.NoError(err)

	// Verify link exists and has same Ino
	f2, err := tfs.Open("/linked.txt")
	require.NoError(err)
	require.Equal(originalIno, f2.Ino())
	
	// Verify nlink incremented
	require.Equal(uint32(2), f2.Nlink())
	
	// Remove original, link should still exist
	err = tfs.Remove("/original.txt")
	require.NoError(err)
	
	files, err := tfs.ReadDir("/")
	require.NoError(err)
	require.Contains(files, "linked.txt")
	require.NotContains(files, "original.txt")
}

func TestBehavior_RestartReset(t *testing.T) {
	require := require.New(t)
	
	// This "simulates" a restart by creating a new TorrentFS and adding the same torrent
	// hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	// Note: TorrentFS.AddTorrent usually handles file extraction from torrent.Files()
	// But since our mock returns nil files, we'll just verify the logic of transience
	// by showing that a new TorrentFS starts clean.

	tfs1 := NewTorrent(10)
	_ = tfs1.Create("/manual.txt")
	
	tfs2 := NewTorrent(10)
	files, _ := tfs2.ReadDir("/")
	require.NotContains(files, "manual.txt")
}

func TestBehavior_ReadSeek(t *testing.T) {
	require := require.New(t)
	tfs := NewTorrent(10)

	data := []byte("hello world")
	err := tfs.s.Add(NewMemoryFile(data), "/test.txt")
	require.NoError(err)

	f, err := tfs.Open("/test.txt")
	require.NoError(err)
	defer f.Close()

	// Read first 5 bytes
	buf := make([]byte, 5)
	n, err := f.Read(buf)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal("hello", string(buf))

	// Seek to end
	mf := f.(*MemoryFile)
	_, err = mf.Seek(6, 0)
	require.NoError(err)

	// Read rest
	buf = make([]byte, 5)
	n, err = f.Read(buf)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal("world", string(buf))
}

func TestBehavior_DirectoryRecursion(t *testing.T) {
	require := require.New(t)
	tfs := NewTorrent(10)

	err := tfs.s.Add(NewMemoryFile([]byte("data")), "/a/b/c/d.txt")
	require.NoError(err)

	// Check /a
	files, err := tfs.ReadDir("/a")
	require.NoError(err)
	require.Contains(files, "b")

	// Check /a/b
	files, err = tfs.ReadDir("/a/b")
	require.NoError(err)
	require.Contains(files, "c")

	// Check /a/b/c
	files, err = tfs.ReadDir("/a/b/c")
	require.NoError(err)
	require.Contains(files, "d.txt")
}

func TestBehavior_PathResolution(t *testing.T) {
	require := require.New(t)
	tfs := NewTorrent(10)

	err := tfs.s.Add(NewMemoryFile([]byte("data")), "/test.txt")
	require.NoError(err)

	paths := []string{
		"/test.txt",
		"test.txt",
		"./test.txt",
		"/sub/../test.txt",
		"//test.txt",
		"/test.txt/",
	}

	for _, p := range paths {
		_, err := tfs.Open(p)
		require.NoError(err, "Failed for path: %s", p)
	}
}

func TestBehavior_ConcurrentAccess(t *testing.T) {
	require := require.New(t)
	tfs := NewTorrent(10)

	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	err := tfs.s.Add(NewMemoryFile(data), "/large.bin")
	require.NoError(err)

	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			f, err := tfs.Open("/large.bin")
			require.NoError(err)
			defer f.Close()

			buf := make([]byte, 1024)
			for j := 0; j < 100; j++ {
				_, err := f.Read(buf)
				require.NoError(err)
			}
		}()
	}
	wg.Wait()
}

func TestBehavior_OOM_MassiveTorrent(t *testing.T) {
	// Create a storage that has 50,000 files
	tfs := NewTorrent(10)
	
	const numFiles = 50000
	for i := 0; i < numFiles; i++ {
		err := tfs.s.Add(NewMemoryFile([]byte("test")), fmt.Sprintf("/dir/file_%d.txt", i))
		require.NoError(t, err)
	}

	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Attempt to read the massive directory
	entries, err := tfs.ReadDir("/dir")
	require.NoError(t, err)
	require.Len(t, entries, numFiles)

	runtime.ReadMemStats(&m2)

	// Ensure allocations didn't spike beyond a reasonable threshold (e.g., 50MB)
	allocBytes := m2.TotalAlloc - m1.TotalAlloc
	require.Less(t, allocBytes, uint64(50*1024*1024), "ReadDir triggered massive memory allocation: %d bytes", allocBytes)
}
