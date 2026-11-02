package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// searchTree is a route of torrents and a library linked from it.
func searchTree(t *testing.T) *dfs.ContainerFs {
	t.Helper()
	route := dfs.NewMemory()
	for p, size := range map[string]int{
		"/Dune.Part.Two.2024.2160p/Dune.Part.Two.2024.2160p.mkv": 40,
		"/Dune.Part.Two.2024.2160p/sample.mkv":                   2,
		"/Arrival.2016.1080p/Arrival.2016.1080p.mkv":             20,
		"/Show.S01/Season 1/Show.S01E01.mkv":                     5,
		"/Pack/extras.zip":                                       1,
	} {
		require.NoError(t, route.Storage.Add(dfs.NewMemoryFile(make([]byte, size)), p))
	}
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/torrents": route})
	require.NoError(t, err)
	require.NoError(t, cfs.Link("/torrents/Arrival.2016.1080p/Arrival.2016.1080p.mkv", "/movies/Arrival (2016)/Arrival (2016).mkv"))
	return cfs
}

func matchPaths(r searchResults) []string {
	out := make([]string, 0, len(r.Matches))
	for _, m := range r.Matches {
		out = append(out, m.Path)
	}
	return out
}

func TestSearchFiles(t *testing.T) {
	cfs := searchTree(t)

	// every word, in any case and order, anywhere in the path; the file named for them first
	r := searchFiles(cfs, "two DUNE")
	require.Equal(t, []string{
		"/torrents/Dune.Part.Two.2024.2160p/Dune.Part.Two.2024.2160p.mkv",
		"/torrents/Dune.Part.Two.2024.2160p/sample.mkv",
	}, matchPaths(r))
	require.False(t, r.More)

	// links are files too, found by their own path
	r = searchFiles(cfs, "arrival")
	require.ElementsMatch(t, []string{
		"/torrents/Arrival.2016.1080p/Arrival.2016.1080p.mkv",
		"/movies/Arrival (2016)/Arrival (2016).mkv",
	}, matchPaths(r))

	// a folder's name finds what's in it, but folders aren't listed
	r = searchFiles(cfs, "season 1")
	require.Equal(t, []string{"/torrents/Show.S01/Season 1/Show.S01E01.mkv"}, matchPaths(r))

	// an archive is listed as a file
	r = searchFiles(cfs, "extras")
	require.Equal(t, []string{"/torrents/Pack/extras.zip"}, matchPaths(r))

	require.Empty(t, searchFiles(cfs, "x").Matches, "one letter isn't searched")
	require.Empty(t, searchFiles(cfs, "  ").Matches)
	require.Empty(t, searchFiles(cfs, "nothing like this").Matches)
}

func TestSearchFiles_ShowsTheBestAndSaysThereAreMore(t *testing.T) {
	route := dfs.NewMemory()
	for i := range searchShown + 5 {
		require.NoError(t, route.Storage.Add(dfs.NewMemoryFile(nil), fmt.Sprintf("/Episodes/Show.E%02d.mkv", i)))
	}
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/torrents": route})
	require.NoError(t, err)

	r := searchFiles(cfs, "show")
	require.Len(t, r.Matches, searchShown)
	require.True(t, r.More)
}

func TestLinksSearchPage(t *testing.T) {
	r := filesRouter(t, searchTree(t), nil)

	w := get(r, "/links/search?source="+url.QueryEscape("dune 2160p"))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `data-pick-source="/torrents/Dune.Part.Two.2024.2160p/Dune.Part.Two.2024.2160p.mkv"`)
	require.Contains(t, body, "40 B")
	require.Contains(t, body, `role="option"`)

	w = get(r, "/links/search?source=zzzz")
	require.Contains(t, w.Body.String(), "No files match “zzzz”.")

	w = get(r, "/links/search?source=d")
	require.Empty(t, strings.TrimSpace(w.Body.String()), "too short to search: nothing listed")

	// the form's File field searches as it's typed in, into a dropdown
	w = get(r, "/links")
	require.Contains(t, w.Body.String(), `role="combobox"`)
	require.Contains(t, w.Body.String(), `hx-get="/links/search"`)
}

// A file made through the mount is listed as one, not as a link to the top of the mount.
func TestLinksPage_MadeEntries(t *testing.T) {
	links := map[string]string{"library/Music": dtorrent.DirRecord, "library/a.nfo": dtorrent.FileRecord}
	svc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) { return links, nil },
	}
	r := filesRouter(t, newTree(t), svc)

	body := get(r, "/links").Body.String()
	require.Contains(t, body, "A folder made here")
	require.Contains(t, body, "A file made here")
	require.NotContains(t, body, "shows <a")
}
