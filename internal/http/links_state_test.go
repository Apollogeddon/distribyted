package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// linkStateTree has one link in the mount, one waiting for its torrent, and one missing.
func linkStateTree(t *testing.T) (http.Handler, []string) {
	t.Helper()
	route := dfs.NewMemory()
	require.NoError(t, route.Storage.Add(dfs.NewMemoryFile([]byte("film")), "/Dune/dune.mkv"))
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/radarr": route})
	require.NoError(t, err)
	require.NoError(t, cfs.Link("/radarr/Dune/dune.mkv", "/movies/Dune.mkv"))
	svc := &mockTorrentService{listLinksFunc: func() (map[string]string, error) {
		return map[string]string{
			"movies/Dune.mkv":    "/radarr/Dune/dune.mkv",
			"movies/Arrival.mkv": "/radarr/Arrival/arrival.mkv",
			"movies/Heat.mkv":    "/radarr/Heat/heat.mkv",
			"movies":             "/",
		}, nil
	}}
	waiting := []string{"/movies/Arrival.mkv"}
	r, err := NewHandler(nil, dtorrent.NewStats(), svc, nil, nil, nil, "", routesConf("radarr"), "", cfs,
		WithWaitingLinks(func() []string { return waiting }))
	require.NoError(t, err)
	return r, waiting
}

func TestLinksPage_States(t *testing.T) {
	r, _ := linkStateTree(t)
	body := get(r, "/links").Body.String()
	require.Contains(t, body, "1 link waiting for its torrent · 1 missing from the mount.")
	require.Contains(t, body, "Waiting for its torrent")
	require.Contains(t, body, ">Missing<")
	require.Contains(t, body, "/movies/Dune.mkv")

	// only the two that need attention
	body = get(r, "/links/list?show=attention").Body.String()
	require.Contains(t, body, "/movies/Arrival.mkv")
	require.Contains(t, body, "/movies/Heat.mkv")
	require.NotContains(t, body, "/movies/Dune.mkv")
	require.NotContains(t, body, "Show them")
}

func TestLinksPage_Search(t *testing.T) {
	r, _ := linkStateTree(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/links/list?q=DUNE&push=1", nil))
	require.Equal(t, "/links?q=DUNE&show=all", w.Header().Get("HX-Push-Url"))
	body := w.Body.String()
	require.Contains(t, body, "/movies/Dune.mkv")
	require.NotContains(t, body, "/movies/Heat.mkv")
	require.Contains(t, body, `hx-get="/links/list?q=DUNE&amp;show=all"`)

	body = get(r, "/links?q=nothing").Body.String()
	require.Contains(t, body, "No links match.")
}
