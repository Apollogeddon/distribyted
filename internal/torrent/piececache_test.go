package torrent

import (
	"bytes"
	"encoding/hex"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
)

// cacheFixture is a 128 MiB file of 4 MiB pieces: its start pieces are 0 to 7 (its first
// 32 MiB) and 30 and 31 (its last 8 MiB), the other 22 are its middle.
type cacheFixture struct {
	dir     string
	content []byte
	info    metainfo.Info
}

const (
	fixturePiece  = 4 << 20
	fixturePieces = 32
)

func newCacheFixture(t *testing.T) *cacheFixture {
	t.Helper()
	dir := t.TempDir()
	content := make([]byte, fixturePieces*fixturePiece)
	r := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < len(content); i += 8 {
		v := r.Uint64()
		for j := 0; j < 8; j++ {
			content[i+j] = byte(v >> (8 * j))
		}
	}
	name := filepath.Join(dir, "film.mkv")
	require.NoError(t, os.WriteFile(name, content, 0o644))
	info := metainfo.Info{PieceLength: fixturePiece}
	require.NoError(t, info.BuildFromFilePath(name))
	require.NoError(t, os.Remove(name))
	return &cacheFixture{dir: dir, content: content, info: info}
}

func isStartPiece(i int) bool { return i < 8 || i >= 30 }

func (f *cacheFixture) open(t *testing.T, c *PieceCache) *storage.Torrent {
	t.Helper()
	partial, err := filecache.NewCache(filepath.Join(f.dir, "partial"))
	require.NoError(t, err)
	_ = partial.Info()
	tor, err := storage.NewClient(PieceStorage(c, partial, time.Minute)).OpenTorrent(t.Context(), &f.info, metainfo.Hash{1})
	require.NoError(t, err)
	return tor
}

func (f *cacheFixture) write(t *testing.T, tor *storage.Torrent, i int) {
	t.Helper()
	p := tor.Piece(f.info.Piece(i))
	_, err := p.WriteAt(f.content[i*fixturePiece:(i+1)*fixturePiece], 0)
	require.NoError(t, err)
	require.NoError(t, p.MarkComplete())
}

func (f *cacheFixture) cached(t *testing.T, tor *storage.Torrent, i int) bool {
	t.Helper()
	p := tor.Piece(f.info.Piece(i))
	if !p.Completion().Complete {
		return false
	}
	got := make([]byte, fixturePiece)
	n, err := p.ReadAt(got, 0)
	return err == nil && n == fixturePiece && bytes.Equal(got, f.content[i*fixturePiece:(i+1)*fixturePiece])
}

func (f *cacheFixture) key(i int) string {
	h := f.info.Piece(i).V1Hash()
	return path.Join(completedDir, hex.EncodeToString(h.Value[:]))
}

func openCache(t *testing.T, f *cacheFixture, percent int64) *PieceCache {
	t.Helper()
	c, err := OpenPieceCache(filepath.Join(f.dir, "cache"), filepath.Join(f.dir, "cache-protected"), percent)
	require.NoError(t, err)
	_ = c.Info()
	return c
}

// A file's start pieces go to the protected cache, the rest to the main one, and reading the
// whole file through, more than the cache holds, evicts only from the main cache.
func TestPieceCache_ProtectsStartsFromEviction(t *testing.T) {
	f := newCacheFixture(t)
	c := openCache(t, f, 70)
	require.True(t, c.Protecting())
	c.SetCapacity(16 * fixturePiece) // 11 pieces protected, 4.8 main
	tor := f.open(t, c)

	for i := range fixturePieces {
		f.write(t, tor, i)
	}
	for i := range fixturePieces {
		if isStartPiece(i) {
			require.True(t, f.cached(t, tor, i), "start piece %d survives", i)
			_, err := c.protected.Stat(f.key(i))
			require.NoError(t, err, "start piece %d is in the protected cache", i)
		} else {
			_, err := c.protected.Stat(f.key(i))
			require.Error(t, err, "middle piece %d isn't protected", i)
		}
	}
	require.True(t, f.cached(t, tor, 29), "the latest middle piece is kept")
	require.False(t, f.cached(t, tor, 8), "the earliest middle piece was evicted")
	require.LessOrEqual(t, c.main.Info().Filled, c.main.Info().Capacity)
	require.LessOrEqual(t, c.protected.Info().Filled, c.protected.Info().Capacity)

	info := c.Info()
	require.Equal(t, int64(16*fixturePiece), info.Capacity)
	require.Equal(t, c.main.Info().Filled+c.protected.Info().Filled, info.Filled)
	walked := 0
	c.WalkItems(func(filecache.ItemInfo) { walked++ })
	require.Equal(t, info.NumItems, walked)
}

// Within the protected cache, the start read longest ago goes first.
func TestPieceCache_ProtectedEvictsItsOwnOldest(t *testing.T) {
	f := newCacheFixture(t)
	c := openCache(t, f, 50)
	c.SetCapacity(12 * fixturePiece) // 6 protected
	tor := f.open(t, c)
	for i := range 8 {
		f.write(t, tor, i)
	}
	require.False(t, f.cached(t, tor, 0))
	require.False(t, f.cached(t, tor, 1))
	require.True(t, f.cached(t, tor, 7))
}

// Turning protection on moves the start pieces the main cache holds, and off moves them back:
// neither downloads anything again.
func TestPieceCache_MovesPiecesWhenTurnedOnAndOff(t *testing.T) {
	f := newCacheFixture(t)
	c := openCache(t, f, -1)
	require.False(t, c.Protecting())
	tor := f.open(t, c)
	for _, i := range []int{0, 1, 10, 31} {
		f.write(t, tor, i)
	}

	c = openCache(t, f, 10)
	tor = f.open(t, c)
	for _, i := range []int{0, 1, 31} {
		_, err := c.main.Stat(f.key(i))
		require.Error(t, err, "piece %d left the main cache", i)
		_, err = c.protected.Stat(f.key(i))
		require.NoError(t, err, "piece %d is in the protected cache", i)
		require.True(t, f.cached(t, tor, i))
	}
	_, err := c.main.Stat(f.key(10))
	require.NoError(t, err, "a middle piece stays")
	require.Equal(t, int64(4*fixturePiece), c.Info().Filled)

	c = openCache(t, f, -1)
	tor = f.open(t, c)
	for _, i := range []int{0, 1, 10, 31} {
		require.True(t, f.cached(t, tor, i), "piece %d is back in the main cache", i)
	}
	require.Equal(t, 4, c.Info().NumItems)
	_, err = os.Stat(filepath.Join(f.dir, "cache-protected"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Freeing a torrent's cache removes its pieces from both caches.
func TestPieceCache_RemoveFromBoth(t *testing.T) {
	f := newCacheFixture(t)
	c := openCache(t, f, 20)
	tor := f.open(t, c)
	f.write(t, tor, 0)
	f.write(t, tor, 10)
	for _, i := range []int{0, 10} {
		_, err := c.Stat(f.key(i))
		require.NoError(t, err)
		require.NoError(t, c.Remove(f.key(i)))
		_, err = c.Stat(f.key(i))
		require.Error(t, err)
	}
	require.Zero(t, c.Info().NumItems)
}

// Without a limit, neither cache has one; small files' pieces aren't protected.
func TestPieceCache_CapacityAndSmallFiles(t *testing.T) {
	f := newCacheFixture(t)
	c := openCache(t, f, 10)
	c.SetCapacity(-1)
	require.Equal(t, int64(-1), c.Info().Capacity)
	c.SetCapacity(1000)
	p, ok := c.ProtectedInfo()
	require.True(t, ok)
	require.Equal(t, int64(100), p.Capacity)
	require.Equal(t, int64(1000), c.Info().Capacity)

	small := metainfo.Info{PieceLength: 1 << 20, Name: "subs.srt", Length: 2 << 20, Pieces: make([]byte, 40)}
	c.protect(&small)
	require.Empty(t, c.starts)
}
