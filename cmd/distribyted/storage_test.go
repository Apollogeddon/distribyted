package main

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // BitTorrent v1 piece hashes are SHA-1
	"io/fs"
	"math/rand/v2"
	"path/filepath"
	"testing"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
)

// The piece cache keeps to global_cache_size on every platform, Windows included, evicting
// the pieces read longest ago, and pieces it keeps read back as written.
func TestStorageLayerKeepsToTheCacheSize(t *testing.T) {
	const (
		pieceLength = 16 * 1024
		pieces      = 12
		capacity    = 4 * pieceLength
	)
	conf := config.AddDefaults(&config.Root{Torrent: &config.TorrentGlobal{MetadataFolder: t.TempDir()}})
	sl, err := initStorageLayer(conf)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sl.fis.Close()
		_ = sl.pc.Close()
	})
	sl.fc.SetCapacity(capacity)

	data := make([]byte, pieceLength*pieces)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range data {
		data[i] = byte(r.Uint32())
	}
	info := &metainfo.Info{Name: "cache-test", PieceLength: pieceLength, Length: int64(len(data))}
	for i := 0; i < pieces; i++ {
		h := sha1.Sum(data[i*pieceLength : (i+1)*pieceLength]) //nolint:gosec // as above
		info.Pieces = append(info.Pieces, h[:]...)
	}
	ih := metainfo.HashBytes([]byte(info.Name))

	ti, err := sl.st.OpenTorrent(context.Background(), info, ih)
	require.NoError(t, err)
	piece := func(i int) storage.PieceImpl {
		if ti.PieceWithHash != nil {
			return ti.PieceWithHash(info.Piece(i), g.Some(info.Piece(i).V1Hash().Unwrap().Bytes()))
		}
		return ti.Piece(info.Piece(i))
	}

	for i := 0; i < pieces; i++ {
		want := data[i*pieceLength : (i+1)*pieceLength]
		p := piece(i)
		n, err := p.WriteAt(want, 0)
		require.NoError(t, err)
		require.Equal(t, pieceLength, n)
		require.NoError(t, p.MarkComplete())
		require.True(t, p.Completion().Complete, "piece %d is complete once written", i)

		got := make([]byte, pieceLength)
		n, err = p.ReadAt(got, 0)
		require.NoError(t, err)
		require.Equal(t, pieceLength, n)
		require.True(t, bytes.Equal(want, got), "piece %d reads back as written", i)

		require.LessOrEqual(t, cacheBytes(t, conf), int64(capacity), "after piece %d the cache is over its size", i)
	}

	require.False(t, piece(0).Completion().Complete, "the piece written first was evicted")
	require.True(t, piece(pieces-1).Completion().Complete, "the piece written last was kept")
	require.NoError(t, ti.Close())
}

// cacheBytes is the size of the files in the piece cache's folder.
func cacheBytes(t *testing.T, conf *config.Root) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(filepath.Join(conf.Torrent.MetadataFolder, "cache"), func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		total += fi.Size()
		return nil
	})
	require.NoError(t, err)
	return total
}
