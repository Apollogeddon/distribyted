package testenv

import (
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"path"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// What distribyted downloads with nothing playing, against a real seeder: why each piece was
// downloaded, that only play gets the next episode ready, and that the cache keeps files'
// starts while a film plays through more than it holds.

const policyPiece = 1 << 20

// randomContent is n bytes no two pieces of which share a hash.
func randomContent(seed uint64, n int) []byte {
	b := make([]byte, n)
	r := rand.New(rand.NewPCG(seed, seed+1))
	for i := 0; i+8 <= n; i += 8 {
		v := r.Uint64()
		for j := range 8 {
			b[i+j] = byte(v >> (8 * j))
		}
	}
	return b
}

type policySwarm struct {
	tracker *Tracker
	seeder  *Seeder
}

func newPolicySwarm(t *testing.T) *policySwarm {
	t.Helper()
	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	t.Cleanup(tracker.Stop)
	seeder, err := NewSeeder()
	require.NoError(t, err)
	t.Cleanup(seeder.Stop)
	return &policySwarm{tracker: tracker, seeder: seeder}
}

// add seeds content as name, adds it to app's route, and returns its hash once its file opens.
func (s *policySwarm) add(t *testing.T, app *TestApp, route, name string, content []byte) string {
	t.Helper()
	magnet, err := s.seeder.AddFileWithPieceLength(name, content, s.tracker.AnnounceURL(), policyPiece)
	require.NoError(t, err)
	s.tracker.RegisterPeer(magnet.InfoHash, s.seeder.PeerAddr())
	tm, _ := app.Client.AddMagnet(magnet.String())
	host, port, err := net.SplitHostPort(s.seeder.PeerAddr())
	require.NoError(t, err)
	var p int
	_, err = fmt.Sscanf(port, "%d", &p)
	require.NoError(t, err)
	tm.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: p}}})
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))
	require.Eventually(t, func() bool {
		f, err := app.FS.Open(path.Join("/", route, name))
		if err == nil {
			_ = f.Close()
		}
		return err == nil
	}, 30*time.Second, 10*time.Millisecond, "%s did not become readable", name)
	return magnet.InfoHash.HexString()
}

// policyApp is an app with a piece cache of capacity bytes, percent of it protected (none at or
// below 0), reaching the seeder over loopback.
func policyApp(t *testing.T, capacity, percent int64) *TestApp {
	t.Helper()
	var app *TestApp
	var err error
	if percent > 0 {
		app, err = NewTestAppProtectedCache(capacity, percent, 30)
	} else {
		app, err = NewTestAppCappedCache(capacity, 30)
	}
	require.NoError(t, err)
	t.Cleanup(app.Close)
	app.Client.AddDialer(ThrottledDialer{})
	return app
}

func fetched(r dfs.Reason) int64 {
	for _, c := range dfs.Fetched().Reasons {
		if c.Reason == r {
			return c.Total
		}
	}
	return 0
}

func readiness(t *testing.T, app *TestApp, p string) dfs.Readiness {
	t.Helper()
	f, err := app.FS.Open(p)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	pf, ok := f.(dfs.Preparer)
	require.True(t, ok, "%s is a torrent's file", p)
	r, ok := pf.Readiness()
	require.True(t, ok)
	return r
}

// readRange reads [from, to) of the file at p on one handle, as FUSE does.
func readRange(t *testing.T, f dfs.File, from, to int64) {
	t.Helper()
	buf := make([]byte, 128<<10)
	for off := from; off < to; off += int64(len(buf)) {
		n := min(int64(len(buf)), to-off)
		_, err := f.ReadAt(buf[:n], off)
		if err != nil && err != io.EOF {
			require.NoError(t, err, "read at %d", off)
		}
	}
}

// Linking a file into a library downloads its start and end, counted as such, and nothing
// else: no read, no readahead.
func TestCachePolicy_LinkPrepareIsCountedAsLink(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	const size = 104 * policyPiece
	sw := newPolicySwarm(t)
	app := policyApp(t, 512<<20, -1)
	sw.add(t, app, "sonarr", "Show.S01E01.mkv", randomContent(11, size))

	link, read := fetched(dfs.ReasonLink), fetched(dfs.ReasonRead)
	require.NoError(t, app.FS.Link("/sonarr/Show.S01E01.mkv", "/series/Show/Season 1/Show - S01E01.mkv"))
	// its first 32 MiB and last 8 MiB, pieces 0 to 31 and 96 to 103
	const want = 40 * policyPiece
	require.Eventually(t, func() bool { return fetched(dfs.ReasonLink)-link >= want }, 60*time.Second, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, int64(want), fetched(dfs.ReasonLink)-link)
	require.Equal(t, read, fetched(dfs.ReasonRead), "nothing was read")
	require.True(t, readiness(t, app, "/series/Show/Season 1/Show - S01E01.mkv").Ready())
}

// Reading an episode's end, as a library app looking for its credits does, leaves the next
// one alone; reading it through, as a player does, gets the next one's lead ready.
func TestCachePolicy_NextEpisodeOnlyAfterPlay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	const size = 104 * policyPiece
	sw := newPolicySwarm(t)
	app := policyApp(t, 512<<20, -1)
	dtorrent.PrepareNextEpisodes(app.Service, app.FS)
	sw.add(t, app, "sonarr", "Show.S01E01.mkv", randomContent(21, size))
	sw.add(t, app, "sonarr", "Show.S01E02.mkv", randomContent(22, size))
	const (
		e01 = "/series/Show/Season 1/Show - S01E01.mkv"
		e02 = "/series/Show/Season 1/Show - S01E02.mkv"
	)
	require.NoError(t, app.FS.Link("/sonarr/Show.S01E01.mkv", e01))
	require.NoError(t, app.FS.Link("/sonarr/Show.S01E02.mkv", e02))
	require.Eventually(t, func() bool { return readiness(t, app, e02).Ready() }, 60*time.Second, 20*time.Millisecond)
	require.Equal(t, dfs.PrepareStart, readiness(t, app, e02).Asked)

	f, err := app.FS.Open(e01)
	require.NoError(t, err)
	readRange(t, f, 0, 1<<20)
	readRange(t, f, size*90/100, size)
	require.NoError(t, f.Close())
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, dfs.PrepareStart, readiness(t, app, e02).Asked, "a credits search isn't play")

	f, err = app.FS.Open(e01)
	require.NoError(t, err)
	readRange(t, f, 0, size)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool { return readiness(t, app, e02).Asked == dfs.PrepareLead }, 10*time.Second, 20*time.Millisecond)
	r := readiness(t, app, e02)
	// random bytes have no duration, so the lead is 5% of the file
	require.Equal(t, dfs.LeadFor(size, 0), r.Lead)
	require.True(t, r.End)
}

// A film read through, more than the cache holds, evicts the start of a file only prepared,
// unless starts are protected.
func TestCachePolicy_ProtectedStartsSurviveAFilm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	for _, c := range []struct {
		name    string
		percent int64
		kept    bool
	}{
		{"unprotected", -1, false},
		{"protected", 60, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			sw := newPolicySwarm(t)
			// with protection, 96 MiB for starts and 64 MiB for the rest
			app := policyApp(t, 160<<20, c.percent)
			require.Equal(t, c.percent > 0, app.Pieces.Protecting())

			sw.add(t, app, "radarr", "Next.mkv", randomContent(31, 70*policyPiece))
			sw.add(t, app, "radarr", "Film.mkv", randomContent(32, 200*policyPiece))
			const next = "/movies/Next (2026)/Next (2026).mkv"
			require.NoError(t, app.FS.Link("/radarr/Next.mkv", next))
			require.Eventually(t, func() bool { return readiness(t, app, next).Ready() }, 60*time.Second, 20*time.Millisecond)

			f, err := app.FS.Open("/radarr/Film.mkv")
			require.NoError(t, err)
			readRange(t, f, 0, 200*policyPiece)
			require.NoError(t, f.Close())

			r := readiness(t, app, next)
			require.Equal(t, c.kept, r.Start, "the prepared file's start is still cached")
			require.LessOrEqual(t, app.Pieces.Info().Filled, int64(160<<20))
		})
	}
}
