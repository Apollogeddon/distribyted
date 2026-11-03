package http

import (
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

func TestPlayingStatus(t *testing.T) {
	now := time.Now()
	playing := dfs.ReadActivity{Pos: 10, Size: 100, Peers: 4, Ahead: 64 << 20, LastRead: now}
	require.Equal(t, "Playing", playingStatus(playing, now).Label)

	waiting := playing
	waiting.Waiting, waiting.Peers = 12*time.Second, 0
	st := playingStatus(waiting, now)
	require.Equal(t, "Waiting for data", st.Label)
	require.Equal(t, "warn", st.Level)
	require.Contains(t, st.Reason, "12s")
	require.Contains(t, st.Reason, "no peers left")

	nopeers := playing
	nopeers.Peers, nopeers.Ahead = 0, 1<<20
	require.Equal(t, "No peers", playingStatus(nopeers, now).Label)

	// with the rest of the file cached, peers aren't needed
	nopeers.Pos = 100
	require.Equal(t, "Playing", playingStatus(nopeers, now).Label)

	paused := playing
	paused.LastRead = now.Add(-10 * time.Second)
	require.Equal(t, "Paused", playingStatus(paused, now).Label)
}

func TestDashboard_NowPlaying(t *testing.T) {
	old := activeReads
	t.Cleanup(func() { activeReads = old })
	activeReads = func() []dfs.ReadActivity { return nil }
	r := filesRouter(t, newTree(t), nil)
	require.Contains(t, get(r, "/").Body.String(), "Nothing is being read.")

	activeReads = func() []dfs.ReadActivity {
		return []dfs.ReadActivity{{
			Hash: "aa", Path: "Film.2024/film.mkv", Size: 4 << 30, Pos: 1 << 30,
			Rate: 2 << 20, Ahead: 64 << 20, Peers: 7, LastRead: time.Now(), Duration: 2 * time.Hour,
			Cached: []bool{true, false},
		}}
	}
	body := get(r, "/dashboard/stats").Body.String()
	require.Contains(t, body, "One stream playing.")
	require.Contains(t, body, "film.mkv")
	require.Contains(t, body, "30:00 of 2:00:00", "a quarter of a 2 h film")
	require.Contains(t, body, "2 min buffered")
	require.Contains(t, body, "2.0 MiB/s from 7 peers")
	require.Contains(t, body, "width: 25%")
	require.Contains(t, body, `<i class="on">`)
	require.Contains(t, body, "Playing")
}

// The dashboard names a stream as the library does, and gives a file with no duration, such
// as a book, its place in bytes.
func TestPlayingViews_TitlesAndBytes(t *testing.T) {
	old := activeReads
	t.Cleanup(func() { activeReads = old })
	ss := dtorrent.NewStats()
	ss.Add("radarr", &mockTorrent{hash: metainfo.NewHashFromHex(duneHash), name: "Dune.2021.2160p"})
	activeReads = func() []dfs.ReadActivity {
		return []dfs.ReadActivity{
			{Hash: duneHash, Path: "Dune/dune.mkv", Size: 100, Pos: 10, Peers: 3, LastRead: time.Now(), Duration: time.Hour, Ahead: 0},
			{Hash: duneHash, Path: "Dune/manual.pdf", Size: 40 << 20, Pos: 10 << 20, Rate: 1 << 20, Ahead: 5 << 20, Peers: 3, LastRead: time.Now()},
		}
	}
	svc := &mockTorrentService{listLinksFunc: func() (map[string]string, error) {
		return map[string]string{"movies/Dune (2021)/Dune (2021).mkv": "/radarr/Dune/dune.mkv"}, nil
	}}
	vs := newPlayingViews(ss, svc, time.Now())
	require.Len(t, vs, 2)
	require.Equal(t, "Dune (2021)", vs[0].Title)
	require.Equal(t, "Dune.2021.2160p", vs[0].Source)
	require.Equal(t, "6:00 of 1:00:00", vs[0].Position)
	require.Equal(t, "Nothing buffered", vs[0].Buffer)
	require.Equal(t, "3 peers", vs[0].Transfer)
	require.Equal(t, "/routes/radarr/torrents/"+duneHash, vs[0].TorrentHref)

	require.Equal(t, "manual.pdf", vs[1].Title)
	require.Equal(t, "10 MiB of 40 MiB", vs[1].Position)
	require.Equal(t, "5.0 MiB ahead, about 5 s", vs[1].Buffer)
}

func TestClockAndSpan(t *testing.T) {
	require.Equal(t, "1:02:10", clock(time.Hour+2*time.Minute+10*time.Second))
	require.Equal(t, "14:30", clock(14*time.Minute+30*time.Second))
	require.Equal(t, "56 s", span(56*time.Second))
	require.Equal(t, "3 min", span(3*time.Minute))
	require.Equal(t, "1 h 20 min", span(80*time.Minute))
}

func TestDashboardVerdict(t *testing.T) {
	require.Equal(t, "Nothing is playing.", verdict(0))
	require.Equal(t, "One stream playing.", verdict(1))
	require.Equal(t, "Two streams playing.", verdict(2))
	require.Equal(t, "12 streams playing.", verdict(12))
}

// The status line counts the torrents and links, and names the first stream in trouble.
func TestDashboard_StatusLine(t *testing.T) {
	old := activeReads
	t.Cleanup(func() { activeReads = old })
	activeReads = func() []dfs.ReadActivity {
		return []dfs.ReadActivity{{Path: "Heat/heat.mkv", Size: 100, Pos: 1, Waiting: 20 * time.Second, LastRead: time.Now()}}
	}
	ss := dtorrent.NewStats()
	ss.Add("radarr", &mockTorrent{hash: metainfo.NewHashFromHex(duneHash), name: "Dune"})
	st := newDashboardStats(nil, ss, nil, nil, nil, nil)
	require.Equal(t, "One stream playing.", st.Verdict)
	require.Equal(t, "heat.mkv is waiting for data.", st.Problem)
	require.Equal(t, 1, st.Torrents)
	require.Zero(t, st.ShortOfPeers, "a torrent still connecting isn't short of seeders yet")

	require.Equal(t, "All in place", dashboardStats{}.LinksNote())
	require.Equal(t, "1 waiting, 2 missing", dashboardStats{LinksWaiting: 1, LinksMissing: 2}.LinksNote())
	require.Equal(t, "2 missing", dashboardStats{LinksMissing: 2}.LinksNote())
}
