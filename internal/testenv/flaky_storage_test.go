package testenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/stretchr/testify/require"
)

// newFlakyTorrent builds a real anacrolix/torrent Client (no seeder, no
// network — everything below is offline) backed by flakyStorage wrapping an
// in-memory MapClientImpl, pre-seeds one small single-piece file's data
// directly into storage via storage.Client's public wrapper API, and adds
// it to the client with metadata already known (SetInfoBytes) so the
// client's initial piece-completion check finds it already complete —
// without ever touching the network. Returns the flaky storage handle (for
// Fail/Heal) and an *fs.TorrentFS wired to the torrent, the same
// abstraction the real FUSE/WebDAV mounts read through in production.
func newFlakyTorrent(t *testing.T, readTimeoutSeconds int, giveUpAfter time.Duration, closeClient bool) (*flakyStorage, *fs.TorrentFS, string, []byte) {
	t.Helper()

	dir := t.TempDir()
	name := "flaky.bin"
	filePath := filepath.Join(dir, name)
	content := []byte("the quick brown fox jumps over the lazy dog, repeated for a bit of size")
	require.NoError(t, os.WriteFile(filePath, content, 0o644))

	info := metainfo.Info{PieceLength: 256 * 1024}
	require.NoError(t, info.BuildFromFilePath(filePath))
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	ih := infohash.HashBytes(infoBytes)

	mapImpl := NewMapClientImpl()
	flaky := &flakyStorage{ClientImpl: mapImpl}

	// Pre-seed piece 0's data directly into storage via the library's own
	// public storage.Client wrapper — no real client, no peer, no network.
	sc := storage.NewClient(flaky)
	st, err := sc.OpenTorrent(t.Context(), &info, ih)
	require.NoError(t, err)
	piece := st.Piece(info.Piece(0))
	_, err = piece.WriteAt(content, 0)
	require.NoError(t, err)
	require.NoError(t, piece.MarkComplete())

	id, err := dtorrent.GetOrCreatePeerID(filepath.Join(dir, "ID"))
	require.NoError(t, err)

	// guarded as cmd/distribyted guards its storage
	c, err := dtorrent.NewClient(dtorrent.GuardReads(flaky, giveUpAfter), noopBEP44Store{}, &config.TorrentGlobal{
		DisableDHT:  true,
		DisableUPnP: true,
		DisableIPv6: true,
		ListenPort:  -1,
	}, id)
	require.NoError(t, err)
	if closeClient {
		t.Cleanup(func() { c.Close() })
	}

	to, _ := c.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: ih})
	require.NoError(t, to.SetInfoBytes(infoBytes))

	tfs := fs.NewTorrent(readTimeoutSeconds, false)
	tfs.AddTorrent(fs.TorrentWrapper{Torrent: to})

	return flaky, tfs, "/" + name, content
}

// TestFlakyStorage_HealableFailureRecoversWithoutHanging is the integration
// proof, against the real anacrolix/torrent library (not a mock), that the
// internal/fs/torrent.go redesign fixes the production OOM: reader.go's
// readAt (v1.61.0) retries a failed read on a capacity-declared storage by
// recursing into itself with no delay and no retry cap once all 3 attempts
// fail (see readAtWrapper's doc comment for the full mechanism). Before the
// fix, a read against a piece that starts failing here would either hang
// the calling goroutine forever or spin it at 100% CPU with an
// ever-growing stack. With the fix, the read returns within its configured
// deadline regardless, and — this test's actual point — a failure that
// later clears (flaky.Heal()) is recovered from cleanly rather than having
// already wedged the mount.
func TestFlakyStorage_HealableFailureRecoversWithoutHanging(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const readTimeoutSeconds = 2
	flaky, tfs, path, content := newFlakyTorrent(t, readTimeoutSeconds, time.Minute, true)

	// Sanity: prove the harness itself works before introducing failure.
	f, err := tfs.Open(path)
	require.NoError(t, err)
	buf := make([]byte, len(content))
	n, err := f.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, content, buf[:n])
	require.NoError(t, f.Close())

	flaky.Fail()
	go func() {
		time.Sleep(150 * time.Millisecond)
		flaky.Heal()
	}()

	f2, err := tfs.Open(path)
	require.NoError(t, err)
	defer func() { _ = f2.Close() }()

	// A hard, independent watchdog: if the fix regresses and this hangs,
	// fail loudly within a bounded time instead of hanging the whole suite.
	type result struct {
		n   int
		err error
	}
	resCh := make(chan result, 1)
	buf2 := make([]byte, len(content))
	go func() {
		n, err := f2.ReadAt(buf2, 0)
		resCh <- result{n, err}
	}()

	select {
	case r := <-resCh:
		require.NoError(t, r.err, "a healable failure must eventually succeed, not surface as a permanent error")
		require.Equal(t, content, buf2[:r.n])
	case <-time.After(5 * time.Second):
		t.Fatal("read did not return within 5s of a failure that healed after 150ms — the fix did not bound the read")
	}
}

// TestFlakyStorage_PermanentFailure: a piece whose storage keeps saying it's complete while
// every read of it fails. anacrolix/torrent retries such a read by recursing with no delay
// and no limit, so before dtorrent.GuardReads the abandoned read spun at 100% CPU with a
// growing stack until the process died. Now the failed reads slow down, and once they've
// failed for giveUpAfter the piece is incomplete, so the read waits for a download (there is
// no peer here) instead of retrying: the storage sees no more reads.
func TestFlakyStorage_PermanentFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const readTimeoutSeconds = 1
	const giveUpAfter = 300 * time.Millisecond
	// The client stays open: closing it wakes the parked read with an error that anacrolix/torrent
	// retries without limit until the stack overflows. distribyted closes the client only at exit,
	// and drops a torrent only once no read is parked in it (TestDropWithParkedRead).
	flaky, tfs, path, _ := newFlakyTorrent(t, readTimeoutSeconds, giveUpAfter, false)
	flaky.Fail()

	f, err := tfs.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	resCh := make(chan error, 1)
	go func() {
		_, err := f.ReadAt(make([]byte, 16), 0)
		resCh <- err
	}()
	select {
	case err := <-resCh:
		require.Error(t, err, "a piece that can never be read must not read successfully")
	case <-time.After(5 * time.Second):
		t.Fatal("read did not return within 5s")
	}

	// the abandoned read has stopped retrying: the storage sees no further reads
	require.Eventually(t, func() bool {
		before := flaky.Reads()
		time.Sleep(300 * time.Millisecond)
		return flaky.Reads() == before
	}, 5*time.Second, 10*time.Millisecond, "the abandoned read kept retrying a piece that can't be read")
	require.Less(t, flaky.Reads(), int64(1000), "failed reads were retried without slowing down")
}
