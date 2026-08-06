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
func newFlakyTorrent(t *testing.T, readTimeoutSeconds int) (*flakyStorage, *fs.TorrentFS, string, []byte) {
	t.Helper()

	dir := t.TempDir()
	name := "flaky.bin"
	filePath := filepath.Join(dir, name)
	content := []byte("the quick brown fox jumps over the lazy dog, repeated for a bit of size")
	require.NoError(t, os.WriteFile(filePath, content, 0644))

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

	c, err := dtorrent.NewClient(flaky, noopBEP44Store{}, &config.TorrentGlobal{
		DisableDHT:  true,
		DisableUPnP: true,
		DisableIPv6: true,
		ListenPort:  -1,
	}, id)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

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
	flaky, tfs, path, content := newFlakyTorrent(t, readTimeoutSeconds)

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

// TestFlakyStorage_PermanentFailure documents, and deliberately does not
// exercise, the one case this fix cannot make safe on its own: a piece
// whose storage.Completion() never stops lying that it's complete while
// ReadAt never stops failing. reader.go's retry-then-recurse (see
// readAtWrapper's doc comment) never finds a legitimate reason to block, so
// it spins at 100% CPU indefinitely in the real library, independent of
// this package's context-propagation fix. internal/fs's hard deadline
// (readAtWrapper.doRead) still bounds *our* read and keeps the mount
// responsive — but the abandoned goroutine itself keeps spinning forever in
// the background, its stack still growing without bound, which will
// eventually either exhaust the process's memory or hit Go's own
// unrecoverable stack-overflow fatal error. That is not survivable by
// definition, so this test must never actually invoke the scenario (doing
// so even once would risk crashing the entire test binary, not just this
// test) — it exists to document the gap and point at the fix that closes
// it.
//
// TODO(oom-fix-phase-2): once anacrolix/torrent carries a patch capping
// readAt's retry-then-recurse (see docs — the library's own reader.go
// comment already flags this exact risk), un-skip this test, wire it
// through a go.mod replace pointing at the patched module, and assert the
// read now returns ErrReadTimeout (or a library-level error) instead of
// spinning, without any goroutine-count growth after the deadline.
func TestFlakyStorage_PermanentFailure(t *testing.T) {
	t.Skip("intentionally not run — see doc comment: this scenario spins the real library forever and can only be bounded by a library patch, not by this package alone")
}
