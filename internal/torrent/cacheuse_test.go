package torrent

import (
	"bytes"
	"encoding/hex"
	"os"
	"path"
	"testing"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// cacheTorrent is a torrent with n pieces, each hash made of b.
func cacheTorrent(name string, b byte, n int) *mockTorrent {
	var pieces []byte
	for i := range n {
		pieces = append(pieces, bytes.Repeat([]byte{b + byte(i)}, 20)...)
	}
	info := &metainfo.Info{Name: name, PieceLength: 16, Length: int64(16 * n), Pieces: pieces}
	return &mockTorrent{name: name, info: info, hash: metainfo.HashBytes([]byte(name))}
}

func cachePiece(t *testing.T, fc *filecache.Cache, b byte, size int) {
	t.Helper()
	f, err := fc.OpenFile(path.Join(completedDir, hex.EncodeToString(bytes.Repeat([]byte{b}, 20))), os.O_CREATE|os.O_RDWR)
	require.NoError(t, err)
	_, err = f.Write(make([]byte, size))
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestCacheUsage(t *testing.T) {
	fc, err := filecache.NewCache(t.TempDir())
	require.NoError(t, err)
	dune, heat := cacheTorrent("Dune", 0x10, 3), cacheTorrent("Heat", 0x40, 2)
	ts := map[string]fs.Torrent{"dune": dune, "heat": heat}

	cachePiece(t, fc, 0x10, 100)
	cachePiece(t, fc, 0x11, 100)
	cachePiece(t, fc, 0x40, 50)
	cachePiece(t, fc, 0x99, 7) // a torrent that isn't loaded

	u := GetCacheUsage(NewPieceCache(fc), ts)
	require.Len(t, u.Torrents, 2)
	require.Equal(t, "Dune", u.Torrents[0].Name)
	require.Equal(t, int64(200), u.Torrents[0].Bytes)
	require.Equal(t, 2, u.Torrents[0].Pieces)
	require.False(t, u.Torrents[0].LastRead.IsZero())
	require.Equal(t, "Heat", u.Torrents[1].Name)
	require.Equal(t, int64(7), u.OtherBytes)
	require.Equal(t, 1, u.OtherPieces)

	require.Equal(t, 2, EvictFromCache(NewPieceCache(fc), dune))
	u = GetCacheUsage(NewPieceCache(fc), ts)
	require.Len(t, u.Torrents, 1)
	require.Equal(t, "Heat", u.Torrents[0].Name)
	require.Zero(t, EvictFromCache(nil, dune))
}
