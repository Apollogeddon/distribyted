package testenv

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// TestDropWithParkedRead: a read of a torrent with no peers parks in anacrolix/torrent, and
// is abandoned when read_timeout passes. Dropping the torrent then woke it with an error that
// the library, on storage with a capacity, retries by recursing without limit: the stack
// overflowed within a second and took the process with it. The torrent is now dropped only
// once no read is in flight on it.
func TestDropWithParkedRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// a regression overflows a 64 MB stack in well under a second, rather than the default
	// 1 GB after many
	defer debug.SetMaxStack(debug.SetMaxStack(64 << 20))

	dir := t.TempDir()
	filePath := filepath.Join(dir, "parked.bin")
	require.NoError(t, os.WriteFile(filePath, []byte("data no peer will ever send"), 0o644))
	info := metainfo.Info{PieceLength: 256 * 1024}
	require.NoError(t, info.BuildFromFilePath(filePath))
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	ih := infohash.HashBytes(infoBytes)

	// storage with a capacity, as in production, holding none of the torrent's data
	st := &flakyStorage{ClientImpl: NewMapClientImpl()}
	id, err := dtorrent.GetOrCreatePeerID(filepath.Join(dir, "ID"))
	require.NoError(t, err)
	c, err := dtorrent.NewClient(dtorrent.GuardReads(st, time.Minute), noopBEP44Store{}, &config.TorrentGlobal{
		DisableDHT: true, DisableUPnP: true, DisableIPv6: true, ListenPort: -1,
	}, id)
	require.NoError(t, err)
	// the client stays open: closing it is what distribyted does only at exit

	to, _ := c.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: ih})
	require.NoError(t, to.SetInfoBytes(infoBytes))
	tfs := fs.NewTorrent(1, false)
	tfs.AddTorrent(fs.TorrentWrapper{Torrent: to})

	f, err := tfs.Open("/parked.bin")
	require.NoError(t, err)
	_, err = f.ReadAt(make([]byte, 8), 0)
	require.ErrorIs(t, err, fs.ErrReadTimeout, "no peer has the data, so the read is abandoned")
	hash := ih.HexString()
	require.Equal(t, 1, fs.LibraryReadsInFlight(hash), "the abandoned read is still parked in the library")

	// as Service.RemoveFromHash removes it
	tfs.RemoveTorrent(hash)
	fs.DropWhenIdle(hash, to.Drop)

	time.Sleep(2 * time.Second)
	_, stillThere := c.Torrent(ih)
	require.True(t, stillThere, "the drop waits for the parked read")
	require.Equal(t, 1, fs.LibraryReadsInFlight(hash))
}
