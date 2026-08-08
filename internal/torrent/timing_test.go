package torrent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// newTestTimings builds a *Timings with a capturable logger and no
// background poller — tests drive polling explicitly via pollOnce(), per
// the doc comment on that method.
func newTestTimings(t *testing.T, buf *bytes.Buffer, deadline time.Duration) *Timings {
	// This package's TestMain (main_test.go) raises the *global* zerolog
	// level to Warn to keep benchmark output readable; zerolog checks both
	// the per-logger and the global level (log.go: "lvl < l.level ||
	// lvl < GlobalLevel()"), so a per-logger override alone isn't enough —
	// Info/Debug lines asserted on below would be silently dropped. Restore
	// the global level after the test so it doesn't leak into others.
	old := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(old) })

	return &Timings{
		log:      zerolog.New(buf).Level(zerolog.DebugLevel),
		tracked:  make(map[string]*coldStart),
		deadline: deadline,
		tick:     time.Millisecond, // unused directly by pollOnce, kept sane
	}
}

// logLines parses each JSON line in buf, filtered to the given message.
func logLines(t *testing.T, buf *bytes.Buffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		if m["message"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func TestTimings_Added_TracksEntryOnce(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	mt := &mockTorrent{gotInfo: make(chan struct{})}
	tm.Added("aa", "movies", "Movie", 2, mt)
	tm.Added("aa", "series", "Different", 5, mt) // must not reset the existing entry

	tm.mu.Lock()
	require.Len(t, tm.tracked, 1)
	c := tm.tracked["aa"]
	tm.mu.Unlock()

	require.Equal(t, "movies", c.route)
	require.Equal(t, 2, c.webseeds)
}

func TestTimings_CompleteRecord_WebseedSource(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	mt := &mockTorrent{
		info: &metainfo.Info{},
		stats: torrent.TorrentStats{
			TorrentGauges: torrent.TorrentGauges{ActivePeers: 0},
		},
	}
	mt.stats.WebSeeds.BytesReadUsefulData.Add(16384)

	tm.Added("bb", "movies", "Movie", 1, mt)
	tm.pollOnce()

	lines := logLines(t, buf, "torrent cold start")
	require.Len(t, lines, 1)
	require.Equal(t, true, lines[0]["complete"])
	require.Equal(t, "webseed", lines[0]["first_data_src"])
	require.Contains(t, lines[0], "metadata_ms")
	require.Contains(t, lines[0], "first_data_ms")
	require.NotContains(t, lines[0], "first_peer_ms", "no peer ever connected")

	// A second poll must not re-emit.
	tm.pollOnce()
	require.Len(t, logLines(t, buf, "torrent cold start"), 1)
}

func TestTimings_SourceAttribution_PeerAndBoth(t *testing.T) {
	cases := []struct {
		name     string
		peer     int64
		web      int64
		wantFrom string
	}{
		{"peer only", 100, 0, "peer"},
		{"both", 100, 100, "both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			tm := newTestTimings(t, buf, time.Minute)

			mt := &mockTorrent{info: &metainfo.Info{}}
			mt.stats.ActivePeers = 1
			mt.stats.PeerConns.BytesReadUsefulData.Add(c.peer)
			mt.stats.WebSeeds.BytesReadUsefulData.Add(c.web)

			tm.Added("cc", "r", "n", 0, mt)
			tm.pollOnce()

			lines := logLines(t, buf, "torrent cold start")
			require.Len(t, lines, 1)
			require.Equal(t, c.wantFrom, lines[0]["first_data_src"])
			require.Contains(t, lines[0], "first_peer_ms")
		})
	}
}

func TestTimings_Watchdog_EmitsIncompleteAndStopsPolling(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, 0) // already "overdue" on the very first poll

	mt := &mockTorrent{} // no info, no peers, no data — ever
	tm.Added("dd", "r", "n", 0, mt)

	tm.pollOnce()
	lines := logLines(t, buf, "torrent cold start")
	require.Len(t, lines, 1)
	require.Equal(t, false, lines[0]["complete"])
	require.Equal(t, "warn", lines[0]["level"])
	missing, ok := lines[0]["missing"].([]any)
	require.True(t, ok)
	require.Contains(t, missing, "metadata")
	require.Contains(t, missing, "first_peer")
	require.Contains(t, missing, "first_data")

	// The watchdog fires once, not on every subsequent poll.
	tm.pollOnce()
	tm.pollOnce()
	require.Len(t, logLines(t, buf, "torrent cold start"), 1)
}

func TestTimings_LateMetadata(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	mt := &mockTorrent{} // info nil at first
	tm.Added("ee", "r", "n", 0, mt)

	tm.pollOnce()
	tm.mu.Lock()
	require.True(t, tm.tracked["ee"].gotInfo.IsZero(), "metadata not yet known")
	tm.mu.Unlock()

	mt.info = &metainfo.Info{} // metadata arrives
	mt.stats.ActivePeers = 1
	mt.stats.PeerConns.BytesReadUsefulData.Add(1)

	tm.pollOnce()
	lines := logLines(t, buf, "torrent cold start")
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "metadata_ms")
}

func TestTimings_GotInfo_SetsImmediatelyWithoutWaitingForPoll(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	mt := &mockTorrent{}
	tm.Added("ff", "r", "n", 0, mt)
	tm.GotInfo("ff")

	tm.mu.Lock()
	got := !tm.tracked["ff"].gotInfo.IsZero()
	tm.mu.Unlock()
	require.True(t, got)

	// GotInfo for a hash that was never Added, or already forgotten, must
	// not create an entry.
	tm.GotInfo("unknown")
	tm.mu.Lock()
	_, exists := tm.tracked["unknown"]
	tm.mu.Unlock()
	require.False(t, exists)
}

func TestTimings_UntrackedHashesNeverAllocate(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	tm.GotInfo("nope")
	tm.FirstRead("nope", "/x", time.Second)
	tm.Forget("nope")

	tm.mu.Lock()
	defer tm.mu.Unlock()
	require.Empty(t, tm.tracked)
}

func TestTimings_Forget_Deletes(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	mt := &mockTorrent{}
	tm.Added("11", "r", "n", 0, mt)
	tm.Forget("11")

	tm.mu.Lock()
	defer tm.mu.Unlock()
	require.Empty(t, tm.tracked)
}

func TestTimings_FirstRead_LevelByThreshold(t *testing.T) {
	buf := &bytes.Buffer{}
	tm := newTestTimings(t, buf, time.Minute)

	tm.FirstRead("hash1", "/slow.mkv", 2*time.Second)
	tm.FirstRead("hash1", "/fast.mkv", 10*time.Millisecond)

	lines := logLines(t, buf, "file first read")
	require.Len(t, lines, 2)
	require.Equal(t, "info", lines[0]["level"])
	require.EqualValues(t, 2000, lines[0]["open_to_first_byte_ms"])
	require.Equal(t, "debug", lines[1]["level"])
}

func TestTimings_NilReceiverIsNoop(t *testing.T) {
	var tm *Timings
	require.NotPanics(t, func() {
		tm.Added("h", "r", "n", 0, &mockTorrent{})
		tm.GotInfo("h")
		tm.FirstRead("h", "/p", time.Second)
		tm.Forget("h")
		tm.Close()
	})
}

func TestTimings_CloseStopsPoller(t *testing.T) {
	tm := newTimings(time.Minute, time.Millisecond)
	done := make(chan struct{})
	go func() {
		tm.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return — poller goroutine did not stop")
	}
}

func TestWebseedCount(t *testing.T) {
	require.Equal(t, 0, webseedCount(&mockTorrent{}), "mock does not implement Metainfo")
}
