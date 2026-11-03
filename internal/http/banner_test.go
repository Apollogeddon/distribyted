package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

func stubBanner(t *testing.T, reads []dfs.ReadActivity, abandoned int) {
	t.Helper()
	oldReads, oldAbandoned := activeReads, abandonedSince
	t.Cleanup(func() { activeReads, abandonedSince = oldReads, oldAbandoned })
	activeReads = func() []dfs.ReadActivity { return reads }
	abandonedSince = func(time.Time) int { return abandoned }
}

func TestBanner(t *testing.T) {
	now := time.Now()
	stubBanner(t, nil, 0)
	require.Empty(t, newBanner(now, now, nil), "nothing wrong, nothing shown")

	stubBanner(t, []dfs.ReadActivity{
		{Path: "Dune/dune.mkv", Waiting: 25 * time.Second, Peers: 3, Size: 10, LastRead: now},
		{Path: "Heat/heat.mkv", Peers: 0, Pos: 1, Size: 100, Ahead: 1 << 20, LastRead: now},
		{Path: "Fine/fine.mkv", Peers: 9, Pos: 1, Size: 100, Ahead: 1 << 30, LastRead: now},
	}, 3)
	waiting := func() []string { return []string{"/movies/a.mkv", "/movies/b.mkv"} }

	items := newBanner(now, now, waiting)
	require.Len(t, items, 3, "links aren't mentioned while a start is still settling")
	require.Equal(t, "“dune.mkv” has waited 25s for data.", items[0].Message)
	require.Equal(t, "“heat.mkv” is playing, but its torrent has no peers left.", items[1].Message)
	require.Equal(t, "3 reads gave up waiting for data in the last hour.", items[2].Message)

	items = newBanner(now, now.Add(-time.Hour), waiting)
	require.Len(t, items, 4)
	require.Equal(t, "2 links still waiting for their torrents to load.", items[3].Message)
	require.Equal(t, "/links?show=attention", items[3].Href)
}

// Every page loads the banner, which comes from its own endpoint.
func TestBanner_OnEveryPage(t *testing.T) {
	stubBanner(t, nil, 2)
	r := filesRouter(t, newTree(t), nil)
	for _, p := range []string{"/", "/routes", "/files", "/links", "/cache", "/logs", "/servers"} {
		require.Contains(t, get(r, p).Body.String(), `id="health-banner"`, p)
	}
	body := get(r, "/banner").Body.String()
	require.Contains(t, body, "2 reads gave up waiting for data in the last hour.")
	require.Contains(t, body, `href="/logs?level=error"`)
}
