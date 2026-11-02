package loader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const m2 = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

// Renaming a route moves its torrents, and every link, folder and file that's at or shows a
// path in it, and leaves the rest alone.
func TestDB_RenameRoute(t *testing.T) {
	db, err := NewDB(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AddMagnet("torrents-movies", m1))
	require.NoError(t, db.AddMagnet("torrents-moviesX", m2)) // only a prefix of the name
	require.NoError(t, db.AddMagnet("series", m2))
	for newpath, old := range map[string]string{
		"/movies/Film (2024)/film.mkv":      "/torrents-movies/Film.2024/film.mkv",
		"/movies/Film (2024)":               "/",
		"/movies/Other/other.mkv":           "/torrents-moviesX/Other/other.mkv",
		"/torrents-movies/Film.2024/a.nfo":  "file:",
		"/torrents-movies/Film.2024/extras": "/",
		"/torrents-movies":                  "/series/x.mkv", // the route's own path
		"/series/Show/s01e01.mkv":           "/series/Show/s01e01.mkv",
	} {
		require.NoError(t, db.AddLink(old, newpath))
	}

	torrents, links, err := db.RenameRoute("torrents-movies", "radarr")
	require.NoError(t, err)
	require.Equal(t, 1, torrents)
	require.Equal(t, 4, links)

	magnets, err := db.ListMagnets()
	require.NoError(t, err)
	require.Equal(t, []string{m1}, magnets["radarr"])
	require.NotContains(t, magnets, "torrents-movies")
	require.Equal(t, []string{m2}, magnets["torrents-moviesX"])
	require.Equal(t, []string{m2}, magnets["series"])

	got, err := db.ListLinks() // keyed without the leading /
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"movies/Film (2024)/film.mkv": "/radarr/Film.2024/film.mkv",
		"movies/Film (2024)":          "/",
		"movies/Other/other.mkv":      "/torrents-moviesX/Other/other.mkv",
		"radarr/Film.2024/a.nfo":      "file:",
		"radarr/Film.2024/extras":     "/",
		"radarr":                      "/series/x.mkv",
		"series/Show/s01e01.mkv":      "/series/Show/s01e01.mkv",
	}, got)

	// nothing's left to move
	torrents, links, err = db.RenameRoute("torrents-movies", "radarr")
	require.NoError(t, err)
	require.Zero(t, torrents)
	require.Zero(t, links)
}

// A record already at a path in the new route is kept over the one that would replace it.
func TestDB_RenameRouteKeepsWhatsThere(t *testing.T) {
	db, err := NewDB("")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AddLink("file:", "/old/a.nfo"))
	require.NoError(t, db.AddLink("/", "/new/a.nfo"))
	_, links, err := db.RenameRoute("old", "new")
	require.NoError(t, err)
	require.Equal(t, 1, links)
	got, err := db.ListLinks()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"new/a.nfo": "/"}, got) // ListLinks drops the leading /
}

func TestDB_RenameRouteInvalid(t *testing.T) {
	db, err := NewDB("")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, _, err = db.RenameRoute("..", "radarr")
	require.Error(t, err)
	_, _, err = db.RenameRoute("old", "a/b")
	require.Error(t, err)
	torrents, links, err := db.RenameRoute("same", "same")
	require.NoError(t, err)
	require.Zero(t, torrents+links)
}
