package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
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
	require.Contains(t, st.Reason, "No peers")

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
			Rate: 2 << 20, Ahead: 64 << 20, Peers: 7, LastRead: time.Now(),
		}}
	}
	body := get(r, "/dashboard/stats").Body.String()
	require.Contains(t, body, "film.mkv")
	require.Contains(t, body, "1.0 GiB of 4.0 GiB")
	require.Contains(t, body, "2.0 MiB/s")
	require.Contains(t, body, "64 MiB cached ahead · about 32s")
	require.Contains(t, body, `aria-valuenow="25"`)
	require.Contains(t, body, "Playing")
}
