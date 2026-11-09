package fs

import (
	"os"
	"path"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"
)

func TestPieceSync_StartRanges(t *testing.T) {
	const mib = 1024 * 1024
	// a 1 GiB file starting part way into piece 2 of 16 MiB pieces
	s := &pieceSync{offset: 40 * mib, length: 1024 * mib, pieceLen: 16 * mib}
	r := s.startRanges()
	// bytes 40 MiB to 72 MiB of the torrent: pieces 2 to 4
	require.Equal(t, [2]int{2, 5}, r[0])
	// its last 8 MiB end at 1064 MiB, in piece 66
	require.Equal(t, [2]int{66, 67}, r[1])

	// a file smaller than both is all of its pieces, twice
	s = &pieceSync{offset: 0, length: 3 * mib, pieceLen: 2 * mib}
	r = s.startRanges()
	require.Equal(t, [2][2]int{{0, 2}, {0, 2}}, r)

	s = &pieceSync{offset: 5, length: 0, pieceLen: 2 * mib}
	r = s.startRanges()
	require.Equal(t, [2][2]int{{0, 0}, {0, 0}}, r)
}

// newSwarmClient is a client of its own, so what other tests downloaded isn't already there.
func newSwarmClient(t *testing.T) *torrent.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "fs-prepare")
	require.NoError(t, err)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dir
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.KeepAliveTimeout = keepAliveInterval
	cl, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		cl.Close()
		_ = os.RemoveAll(dir)
	})
	return cl
}

// Preparing a file downloads its start and end with nothing reading it, and Readiness says
// when they're there.
func TestTorrentFile_PrepareStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a torrent from a local swarm in short mode")
	}
	addFixture(t) // starts the fixture swarm
	cl := newSwarmClient(t)
	to, _ := cl.AddTorrentInfoHash(fixture.hash)
	to.AddClientPeer(fixture.seeder)
	waitForInfo(t, to)

	tfs := NewTorrent(5, false)
	var started []string
	tfs.OnReadStart(func(h string) { started = append(started, h) })
	tfs.AddTorrent(TorrentWrapper{to})
	f, err := tfs.Open(path.Join("/", fixtureName, fixtureFiles[0].name))
	require.NoError(t, err)
	p, ok := f.(Preparer)
	require.True(t, ok)

	r, ok := p.Readiness()
	require.True(t, ok)
	require.False(t, r.Ready())
	require.Equal(t, int64(fixtureFiles[0].size), r.Size)
	require.Equal(t, PrepareNone, r.Asked)

	require.True(t, p.Prepare(Request{What: PrepareStart, Why: ReasonLink}))
	require.Equal(t, []string{to.InfoHash().HexString()}, started, "the torrent is offered its saved peers")
	require.Eventually(t, func() bool {
		ready, ok := p.StartReady()
		return ok && ready
	}, swarmTimeout, 20*time.Millisecond)
	r, _ = p.Readiness()
	require.True(t, r.Ready())
	// the fixture's files are smaller than prepareStart, so all of it
	require.Equal(t, r.Size, r.Cached)
	tf := f.(*torrentFileHandle).torrentFile
	for i, seg := range tf.pieces.segments(8) {
		require.True(t, seg, "part %d is cached", i)
	}
	require.Equal(t, PrepareStart, r.Asked)

	require.True(t, p.Prepare(Request{What: PrepareNone}))
	r, _ = p.Readiness()
	require.Equal(t, PrepareNone, r.Asked)
}

// A file with no torrent, as in tests, has nothing to prepare.
func TestTorrentFile_PrepareWithoutTorrent(t *testing.T) {
	f := &torrentFile{}
	require.False(t, f.Prepare(Request{What: PrepareAll}))
	_, ok := f.Readiness()
	require.False(t, ok)
	_, ok = f.StartReady()
	require.False(t, ok)
}

func TestPieceSync_LeadRanges(t *testing.T) {
	const mib = 1024 * 1024
	// a 3 GiB episode of 16 MiB pieces, with a lead of 150 MiB: pieces 0 to 9
	s := &pieceSync{offset: 0, length: 3072 * mib, pieceLen: 16 * mib}
	r := s.leadRanges(150 * mib)
	require.Equal(t, [2]int{0, 10}, r[0])
	require.Equal(t, [2]int{191, 192}, r[1])

	// a tiny file, or no lead, still gets its first piece
	s = &pieceSync{offset: 0, length: 10, pieceLen: 16 * mib}
	require.Equal(t, [2][2]int{{0, 1}, {0, 1}}, s.leadRanges(64*mib))
	s = &pieceSync{offset: 0, length: 3072 * mib, pieceLen: 16 * mib}
	require.Equal(t, [2]int{0, 1}, s.leadRanges(0)[0])
}

// A file's start pieces are its first 32 MiB and last 8 MiB, wherever it sits in its torrent.
func TestStartPieces(t *testing.T) {
	const mib = 1024 * 1024
	require.Equal(t, [2][2]int{{0, 2}, {191, 192}}, StartPieces(0, 3072*mib, 16*mib))
	// the second file of a torrent, starting half way into piece 10
	require.Equal(t, [2][2]int{{10, 13}, {202, 203}}, StartPieces(168*mib, 3072*mib, 16*mib))
	require.Equal(t, [2][2]int{}, StartPieces(0, 0, 16*mib))
}
