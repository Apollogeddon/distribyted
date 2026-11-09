package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// fakeTorrentFile is a torrent's file as far as readiness goes.
type fakeTorrentFile struct {
	*dfs.MemoryFile
	r     dfs.Readiness
	asked []dfs.Preparation
	why   []dfs.Reason
}

func (f *fakeTorrentFile) Prepare(r dfs.Request) bool {
	f.asked = append(f.asked, r.What)
	f.why = append(f.why, r.Why)
	f.r.Asked, f.r.Lead = r.What, r.Lead
	return true
}
func (f *fakeTorrentFile) Readiness() (dfs.Readiness, bool) { return f.r, true }
func (f *fakeTorrentFile) StartReady() (bool, bool)         { return f.r.Ready(), true }

// readyTree has a torrent's file of size bytes at /torrents/Film/film.mkv, with a 2 MiB piece
// cache.
func readyTree(t *testing.T, size int64) (http.Handler, *fakeTorrentFile) {
	t.Helper()
	f := &fakeTorrentFile{MemoryFile: dfs.NewMemoryFile(make([]byte, 16)), r: dfs.Readiness{Size: size, Cached: size / 4, Peers: 3}}
	route := dfs.NewMemory()
	require.NoError(t, route.Storage.Add(f, "/Film/film.mkv"))
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/torrents": route})
	require.NoError(t, err)
	conf := routesConf()
	conf.Torrent = &config.TorrentGlobal{GlobalCacheSize: 2}
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", conf, "", cfs)
	require.NoError(t, err)
	return r, f
}

const filmPath = "/torrents/Film/film.mkv"

// A torrent's file in the Files page says whether it's ready to play, and opens its readiness.
func TestFilesPage_Readiness(t *testing.T) {
	r, f := readyTree(t, 1024)
	w := get(r, "/files?path=/torrents/Film")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Not cached")
	require.Contains(t, w.Body.String(), `hx-get="/files/readiness?path=%2Ftorrents%2FFilm%2Ffilm.mkv"`)

	f.r.Start, f.r.End = true, true
	w = get(r, "/files/list?path=/torrents/Film")
	require.Contains(t, w.Body.String(), "Ready to play")

	// the API lists it too
	w = get(r, "/api/fs/torrents/Film")
	require.Contains(t, w.Body.String(), `"ready":true`)
}

func TestFilesReadinessDialog(t *testing.T) {
	r, _ := readyTree(t, 1024)
	w := get(r, "/files/readiness?path="+url.QueryEscape(filmPath))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger-After-Swap"), "file-dialog")
	body := w.Body.String()
	require.Contains(t, body, "256 B of 1.0 KiB (25%)")
	require.Contains(t, body, `aria-valuenow="25"`)
	require.NotContains(t, body, `value="stop"`, "nothing to stop")

	w = get(r, "/files/readiness?path=/torrents/Film/gone.mkv")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "isn't a torrent's file")
}

func TestFilesPrepare(t *testing.T) {
	r, f := readyTree(t, 1024)
	prepare := func(p, what string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, postForm("/files/prepare", url.Values{"path": {p}, "what": {what}}))
		return w
	}

	w := prepare(filmPath, "start")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger"), "files-changed")
	require.Contains(t, w.Body.String(), "Start and end")
	require.Contains(t, w.Body.String(), `value="stop"`)

	require.Equal(t, http.StatusOK, prepare(filmPath, "all").Code)
	require.Equal(t, http.StatusOK, prepare(filmPath, "stop").Code)
	require.Equal(t, []dfs.Preparation{dfs.PrepareStart, dfs.PrepareAll, dfs.PrepareNone}, f.asked)
	require.Equal(t, []dfs.Reason{dfs.ReasonButton, dfs.ReasonButton, dfs.ReasonButton}, f.why)

	require.Equal(t, http.StatusBadRequest, prepare(filmPath, "most").Code)
	require.Equal(t, http.StatusNotFound, prepare("/torrents/Film", "start").Code)
	require.Len(t, f.asked, 3)
}

// A file over half the piece cache can't be downloaded whole: it would push every other
// film's start and end out.
func TestFilesPrepare_WholeFileTooBig(t *testing.T) {
	r, f := readyTree(t, 1536*1024)
	w := get(r, "/files/readiness?path="+url.QueryEscape(filmPath))
	require.Contains(t, w.Body.String(), "over half the 2.0 MiB piece cache")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/files/prepare", url.Values{"path": {filmPath}, "what": {"all"}}))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	require.Empty(t, f.asked)
}

// A file got ready as the next episode says so in its dialog.
func TestFilesReadinessDialog_NextEpisode(t *testing.T) {
	r, f := readyTree(t, 1000)
	f.r.Asked, f.r.Lead, f.r.Cached = dfs.PrepareLead, 100, 20
	body := get(r, "/files/readiness?path="+url.QueryEscape(filmPath)).Body.String()
	require.Contains(t, body, "Next episode: first 100 B and end")
	require.Contains(t, body, "Downloading ahead")
}
