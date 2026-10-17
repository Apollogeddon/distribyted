package torrent

import (
	"bytes"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
)

// TestPieceStorage_EvictionKeepsPartialPieces: a piece part downloaded while the cache fills up
// and evicts must still be whole when it's finished. In one capped cache its first chunks were
// evicted, so it failed its hash check and the client banned the peer that sent it.
func TestPieceStorage_EvictionKeepsPartialPieces(t *testing.T) {
	const (
		chunk    = 16 << 10
		pieceLen = 4 * chunk
		pieces   = 24
	)
	dir := t.TempDir()
	content := make([]byte, pieces*pieceLen)
	r := rand.New(rand.NewPCG(5, 6))
	for i := range content {
		content[i] = byte(r.Uint32())
	}
	name := filepath.Join(dir, "file.bin")
	require.NoError(t, os.WriteFile(name, content, 0o644))
	info := metainfo.Info{PieceLength: pieceLen}
	require.NoError(t, info.BuildFromFilePath(name))

	cache, err := filecache.NewCache(filepath.Join(dir, "cache"))
	require.NoError(t, err)
	_ = cache.Info() // wait out the cache's first scan
	cache.SetCapacity(4 * pieceLen)
	partial, err := filecache.NewCache(filepath.Join(dir, "partial"))
	require.NoError(t, err)
	_ = partial.Info()

	tor, err := storage.NewClient(PieceStorage(cache, partial, time.Minute)).OpenTorrent(t.Context(), &info, metainfo.Hash{1})
	require.NoError(t, err)
	piece := func(i int) storage.Piece { return tor.Piece(info.Piece(i)) }
	write := func(i, from, to int) {
		p := piece(i)
		for c := from; c < to; c++ {
			off := i*pieceLen + c*chunk
			_, err := p.WriteAt(content[off:off+chunk], int64(c*chunk))
			require.NoError(t, err)
		}
	}

	// half of piece 0, then many more pieces than the cache holds, then the rest of piece 0
	write(0, 0, 2)
	for i := 1; i < pieces; i++ {
		write(i, 0, 4)
		require.NoError(t, piece(i).MarkComplete())
	}
	write(0, 2, 4)
	require.NoError(t, piece(0).MarkComplete())

	got := make([]byte, pieceLen)
	n, err := piece(0).ReadAt(got, 0)
	require.NoError(t, err)
	require.Equal(t, pieceLen, n)
	require.True(t, bytes.Equal(content[:pieceLen], got), "piece 0 came back other than written")
	require.LessOrEqual(t, cache.Info().Filled, int64(4*pieceLen), "complete pieces stay within the capacity")
}
