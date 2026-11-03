package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// recentService is a torrent service that knows when its links were made.
type recentService struct {
	mockTorrentService
	added map[string]time.Time
}

func (s *recentService) LinksAddedSince(t time.Time) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for p, at := range s.added {
		if !at.Before(t) {
			out[p] = at
		}
	}
	return out, nil
}

func TestDashboard_RecentlyAdded(t *testing.T) {
	route := dfs.NewMemory()
	film := &fakeTorrentFile{MemoryFile: dfs.NewMemoryFile(make([]byte, 16)), r: dfs.Readiness{Start: true, End: true}}
	require.NoError(t, route.Storage.Add(film, "/Dune/dune.mkv"))
	require.NoError(t, route.Storage.Add(&fakeTorrentFile{MemoryFile: dfs.NewMemoryFile(make([]byte, 16))}, "/Heat/heat.mkv"))
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/radarr": route})
	require.NoError(t, err)
	require.NoError(t, cfs.Link("/radarr/Dune/dune.mkv", "/movies/Dune (2021)/Dune (2021).mkv"))
	require.NoError(t, cfs.Link("/radarr/Heat/heat.mkv", "/movies/Heat (1995)/Heat (1995).mkv"))

	now := time.Now()
	svc := &recentService{added: map[string]time.Time{
		"/movies/Dune (2021)/Dune (2021).mkv": now.Add(-2 * time.Hour),
		"/movies/Heat (1995)/Heat (1995).mkv": now.Add(-10 * time.Minute),
		"/movies/Old (1990)/Old (1990).mkv":   now.Add(-72 * time.Hour),
		"/movies/Gone (2000)/Gone (2000).mkv": now.Add(-time.Hour), // deleted since
	}}

	views := newRecentViews(svc, dtorrent.NewStats(), cfs, now)
	require.Len(t, views, 2)
	require.Equal(t, "Heat (1995).mkv", views[0].Name, "newest first")
	require.Equal(t, "10 minutes ago", views[0].When)
	require.Equal(t, "cold", views[0].Readiness)
	require.Equal(t, "Dune (2021).mkv", views[1].Name)
	require.Equal(t, "/movies/Dune (2021)", views[1].Folder)
	require.Equal(t, "ready", views[1].Readiness)

	r, err := NewHandler(nil, dtorrent.NewStats(), svc, nil, nil, nil, "", routesConf("radarr"), "", cfs)
	require.NoError(t, err)
	body := get(r, "/").Body.String()
	require.Contains(t, body, "Recently added")
	require.Contains(t, body, "Heat (1995).mkv")
	require.Contains(t, body, `id="file-dialog"`)

	// a service that doesn't keep times shows the card empty
	require.Nil(t, newRecentViews(&mockTorrentService{}, nil, cfs, now))
}
