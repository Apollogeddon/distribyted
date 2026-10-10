package testenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
)

// Peer speeds, as round-trip delay and upload rate.
var (
	fastPeer = PeerProfile{Latency: 20 * time.Millisecond, BytesPerSecond: 8 << 20}
	slowPeer = PeerProfile{Latency: 150 * time.Millisecond, BytesPerSecond: 512 << 10}
)

// swarmScenarios are the swarms the benchmarks run against, from a healthy one to the
// low-seed ones that struggle in practice. The file is 16MiB in 1MiB pieces.
var swarmScenarios = []struct {
	name string
	spec SwarmSpec
}{
	{"healthy", SwarmSpec{
		Seeders: []PeerProfile{fastPeer, fastPeer, fastPeer, fastPeer, fastPeer, fastPeer, fastPeer, fastPeer},
	}},
	{"few-seeds", SwarmSpec{
		Seeders: []PeerProfile{fastPeer, slowPeer},
		Dead:    150,
	}},
	{"few-seeds-crowded", SwarmSpec{
		Seeders:  []PeerProfile{fastPeer, slowPeer},
		Leechers: 60, LeecherProfile: fastPeer,
		Dead: 150,
	}},
	{"one-slow-seed-crowded", SwarmSpec{
		Seeders:  []PeerProfile{slowPeer},
		Leechers: 60, LeecherProfile: fastPeer,
		Dead: 150,
	}},
}

func newSwarm(tb testing.TB, spec SwarmSpec) *Swarm {
	tb.Helper()
	spec.Name = "film.mkv"
	spec.Size = 16 << 20
	spec.PieceLength = 1 << 20
	s, err := NewSwarm(spec)
	require.NoError(tb, err)
	tb.Cleanup(s.Close)
	return s
}

// swarmRead is what reading from a Swarm took.
type swarmRead struct {
	metadata  time.Duration // adding the magnet, until its metadata arrived
	firstByte time.Duration // opening the file and reading 64KiB at the offset
	failed    bool          // the add or the read timed out
}

// readFrom times reading 64KiB at offset, as a player starting or resuming a file does,
// checking the bytes are the torrent's.
func readFrom(tb testing.TB, app *TestApp, s *Swarm, offset int64) (time.Duration, bool) {
	tb.Helper()
	start := time.Now()
	f, err := app.FS.Open("/swarm/" + s.Magnet.DisplayName)
	if err != nil {
		return time.Since(start), false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 64<<10)
	n, err := f.ReadAt(buf, offset)
	took := time.Since(start)
	if err != nil && !errors.Is(err, io.EOF) {
		return took, false
	}
	require.True(tb, bytes.Equal(buf[:n], s.Content[offset:offset+int64(n)]), "read the torrent's bytes")
	return took, true
}

// coldRead adds the Swarm's magnet to a new app and reads the start of the file: a
// torrent distribyted has never seen.
func coldRead(tb testing.TB, app *TestApp, s *Swarm) swarmRead {
	tb.Helper()
	start := time.Now()
	if err := app.Service.AddMagnet("swarm", s.Magnet.String()); err != nil {
		return swarmRead{metadata: time.Since(start), failed: true}
	}
	r := swarmRead{metadata: time.Since(start)}
	var ok bool
	r.firstByte, ok = readFrom(tb, app, s, 0)
	r.failed = !ok
	return r
}

func newSwarmApp(tb testing.TB, s *Swarm, mod func(*config.TorrentGlobal)) *TestApp {
	tb.Helper()
	app, err := NewTestAppSwarm(mod)
	require.NoError(tb, err)
	tb.Cleanup(app.Close)
	app.Client.AddDialer(s.Dialer)
	return app
}

// TestSwarm checks the harness itself: distribyted finds the seeder through the tracker
// among peers that have nothing and addresses that never answer, reads the right bytes,
// and tries the other kinds of peer on the way.
func TestSwarm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping swarm test in short mode")
	}
	s := newSwarm(t, SwarmSpec{
		Seeders:  []PeerProfile{fastPeer},
		Leechers: 5, LeecherProfile: fastPeer,
		Dead: 10,
	})
	app := newSwarmApp(t, s, nil)

	r := coldRead(t, app, s)
	require.False(t, r.failed)
	_, ok := readFrom(t, app, s, 9<<20)
	require.True(t, ok, "a read further in")

	require.Eventually(t, func() bool {
		live, dead := s.Dialer.Dials()
		return live >= 6 && dead >= 1
	}, 30*time.Second, 100*time.Millisecond, "the leechers and the dead addresses were tried too")
}

// BenchmarkSwarm_ColdStart measures adding a magnet distribyted has never seen and reading
// the start of its file, against each scenario and connection limit: the time to the
// metadata, then to the first 64KiB. Each iteration is a new app. Run it with a fixed,
// small -benchtime (e.g. 3x): one iteration of a crowded swarm can take a minute.
//
// Reported metrics, per iteration: metadata-s and first-byte-s (seconds), failed (the add
// or the read hit its timeout), and the torrent's peers at the end: active (connected),
// seeders (connected seeders), dials-dead (attempts at addresses that never answer).
func BenchmarkSwarm_ColdStart(b *testing.B) {
	for _, sc := range swarmScenarios {
		for _, conns := range []int{25, 50} {
			b.Run(fmt.Sprintf("%s/conns=%d", sc.name, conns), func(b *testing.B) {
				var meta, first time.Duration
				var failed, active, seeders, deadDials int
				for range b.N {
					s := newSwarm(b, sc.spec)
					app := newSwarmApp(b, s, func(t *config.TorrentGlobal) { t.MaxConnsPerTorrent = conns })
					r := coldRead(b, app, s)
					meta += r.metadata
					first += r.firstByte
					if r.failed {
						failed++
					}
					if tt, ok := app.Client.Torrent(s.Magnet.InfoHash); ok {
						st := tt.Stats()
						active += st.ActivePeers
						seeders += st.ConnectedSeeders
					}
					_, dead := s.Dialer.Dials()
					deadDials += dead
				}
				n := float64(b.N)
				b.ReportMetric(meta.Seconds()/n, "metadata-s")
				b.ReportMetric(first.Seconds()/n, "first-byte-s")
				b.ReportMetric(float64(failed)/n, "failed")
				b.ReportMetric(float64(active)/n, "active")
				b.ReportMetric(float64(seeders)/n, "seeders")
				b.ReportMetric(float64(deadDials)/n, "dials-dead")
				b.ReportMetric(0, "ns/op")
			})
		}
	}
}

// BenchmarkSwarm_ReadAfterIdle measures the case a quick start is for: a torrent added
// earlier, read, then left alone, before something reads a part of it that isn't cached.
// It reports the peers still connected when that read starts (active-before, seeders-before)
// and how long the read took (resume-s), after each idle period. Idle periods past a
// minute are where the torrent library's keep-alive timeouts come in.
func BenchmarkSwarm_ReadAfterIdle(b *testing.B) {
	sc := swarmScenarios[2] // few-seeds-crowded
	for _, idle := range []time.Duration{0, 30 * time.Second, 3 * time.Minute} {
		b.Run(fmt.Sprintf("%s/idle=%s", sc.name, idle), func(b *testing.B) {
			var resume time.Duration
			var failed, active, seeders int
			for range b.N {
				s := newSwarm(b, sc.spec)
				app := newSwarmApp(b, s, nil)
				r := coldRead(b, app, s)
				require.False(b, r.failed, "the first read")

				time.Sleep(idle)
				if tt, ok := app.Client.Torrent(s.Magnet.InfoHash); ok {
					st := tt.Stats()
					active += st.ActivePeers
					seeders += st.ConnectedSeeders
				}
				took, ok := readFrom(b, app, s, 12<<20)
				resume += took
				if !ok {
					failed++
				}
			}
			n := float64(b.N)
			b.ReportMetric(resume.Seconds()/n, "resume-s")
			b.ReportMetric(float64(failed)/n, "failed")
			b.ReportMetric(float64(active)/n, "active-before")
			b.ReportMetric(float64(seeders)/n, "seeders-before")
			b.ReportMetric(0, "ns/op")
		})
	}
}
