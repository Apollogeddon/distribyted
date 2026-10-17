package testenv

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"net"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

// TestReadAfterCacheEviction reads a file twice through a piece cache smaller than it. The first
// read fills the cache, which evicts the file's start as it goes, without the client knowing:
// it still counts those pieces complete, and with nothing more wanted it lets the seeder go.
// Reading them again then found no data and no peer to fetch it from, and waited out the read
// timeout, read after read. The second read must now download them again, from the seeder the
// torrent saved as a good peer.
func TestReadAfterCacheEviction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		pieceLen = 1 << 20
		size     = 24 * pieceLen
		capacity = 8 * pieceLen
		// well under the deadline a stuck read waited out before
		readTimeout = 30
	)
	content := make([]byte, size)
	// random, so no two pieces share a hash: the cache keeps a piece under its hash, and
	// repeating content would make every piece one file
	r := rand.New(rand.NewPCG(1, 2))
	for i := range content {
		content[i] = byte(r.Uint32())
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()
	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()
	magnet, err := seeder.AddFileWithPieceLength("evicted.bin", content, tracker.AnnounceURL(), pieceLen)
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestAppCappedCache(capacity, readTimeout)
	require.NoError(t, err)
	defer app.Close()
	app.Client.AddDialer(ThrottledDialer{Latency: 10 * time.Millisecond, BytesPerSecond: 16 << 20})

	tm, _ := app.Client.AddMagnet(magnet.String())
	host, port, err := net.SplitHostPort(seeder.PeerAddr())
	require.NoError(t, err)
	var p uint16
	_, err = fmt.Sscanf(port, "%d", &p)
	require.NoError(t, err)
	tm.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})
	require.NoError(t, app.Service.AddMagnet("evicted", magnet.String()))

	var f dfs.File
	require.Eventually(t, func() bool {
		f, err = app.FS.Open("/evicted/evicted.bin")
		return err == nil
	}, 30*time.Second, 10*time.Millisecond, "file did not become readable")
	defer func() { _ = f.Close() }()

	// FUSE's read size, from the start, as a player reads
	readAll := func(pass int) {
		buf := make([]byte, 128<<10)
		for off := int64(0); off < size; off += int64(len(buf)) {
			n, err := f.ReadAt(buf, off)
			require.NoError(t, err, "pass %d, read at %d", pass, off)
			require.Equal(t, len(buf), n)
			require.True(t, bytes.Equal(content[off:off+int64(n)], buf[:n]), "pass %d, read at %d: wrong data", pass, off)
			if pass == 1 && off == size/2 {
				// what the service's peer recorder does every 30 s of a stream
				app.Service.RecordPeers()
			}
		}
	}
	readAll(1)
	start := time.Now()
	readAll(2)
	t.Logf("second read of %d MiB through an %d MiB cache took %v", size>>20, capacity>>20, time.Since(start))
}
