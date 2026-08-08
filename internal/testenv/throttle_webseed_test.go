package testenv

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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

// TestThrottledDialer_HTTPDialContext_ThrottlesWebseedFetches proves
// ThrottledDialer.HTTPDialContext (wired through internal/torrent.NewClient's
// new variadic opts) actually delays webseed HTTP requests. This closes the
// gap found while planning webseed-prioritization work: Client.AddDialer
// only throttles peer connections — a benchmark that throttled only that
// would run webseed HTTP traffic over unthrottled loopback, making any
// webseed-vs-peer comparison meaningless.
//
// The measurement window starts the first time the dial func is actually
// invoked (via a sync.Once), not from when the torrent was added — the
// library only schedules webseed requests from its own periodic timer (see
// webseed_drop_repro_test.go's doc comment), so including that wait would
// let the assertion pass on jitter alone even if HTTPDialContext were never
// wired at all. Measuring strictly from "dial invoked" to "data received"
// isolates exactly the artificial delay this test exists to confirm.
func TestThrottledDialer_HTTPDialContext_ThrottlesWebseedFetches(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test, not part of short mode")
	}

	content := bytes.Repeat([]byte("throttled webseed fetch content. "), 64) // a few KB

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "throttled.bin")
	require.NoError(t, os.WriteFile(srcFile, content, 0644))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "throttled.bin", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	info := metainfo.Info{PieceLength: 256 * 1024}
	require.NoError(t, info.BuildFromFilePath(srcFile))
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	ih := infohash.HashBytes(infoBytes)

	const latency = 2 * time.Second
	throttle := ThrottledDialer{Latency: latency}

	var dialInvokedAt time.Time
	var dialOnce sync.Once
	dialCtx := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialOnce.Do(func() { dialInvokedAt = time.Now() })
		return throttle.HTTPDialContext(ctx, network, addr)
	}

	id, err := dtorrent.GetOrCreatePeerID(filepath.Join(t.TempDir(), "ID"))
	require.NoError(t, err)

	c, err := dtorrent.NewClient(
		storage.NewFile(t.TempDir()),
		noopBEP44Store{},
		&config.TorrentGlobal{DisableDHT: true, DisableUPnP: true, DisableIPv6: true, ListenPort: -1},
		id,
		func(cc *torrent.ClientConfig) { cc.HTTPDialContext = dialCtx },
	)
	require.NoError(t, err)
	defer c.Close()

	to, isNew := c.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash: ih,
		Storage:  storage.NewFile(t.TempDir()),
	})
	require.True(t, isNew)
	require.NoError(t, to.SetInfoBytes(infoBytes))
	to.AddWebSeeds([]string{srv.URL + "/"})
	to.DownloadAll()

	tfs := fs.NewTorrent(30, false)
	tfs.AddTorrent(fs.TorrentWrapper{Torrent: to})

	f, err := tfs.Open("/" + info.Name)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	buf := make([]byte, len(content))
	n, err := f.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, content, buf[:n])

	require.False(t, dialInvokedAt.IsZero(), "webseed HTTP dial was never invoked — nothing to measure")
	require.GreaterOrEqual(t, time.Since(dialInvokedAt), latency,
		"webseed fetch completed faster than the configured dial latency — HTTPDialContext is not actually throttling it")
}

// TestNewTestAppThrottledWebseed_Constructs is a narrow smoke test for the
// actual constructor a benchmark would call (as opposed to the lower-level
// dtorrent.NewClient opt exercised directly above): confirms the
// httpDialContext plumbing through newTestApp doesn't break app
// construction or shutdown, independent of whether anything ever uses it.
func TestNewTestAppThrottledWebseed_Constructs(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test, not part of short mode")
	}

	app, err := NewTestAppThrottledWebseed(10*time.Millisecond, 0)
	require.NoError(t, err)
	defer app.Close()

	require.NotNil(t, app.Client)
	require.NotNil(t, app.Service)
}
