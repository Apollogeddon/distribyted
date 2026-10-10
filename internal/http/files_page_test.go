package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

func filesRouter(t *testing.T, cfs *dfs.ContainerFs, svc torrentService) http.Handler {
	t.Helper()
	conf := routesConf()
	conf.HTTPGlobal.HTTPFS = true
	r, err := NewHandler(nil, dtorrent.NewStats(), svc, nil, nil, nil, "", conf, "", cfs)
	require.NoError(t, err)
	return r
}

func newTree(t *testing.T) *dfs.ContainerFs {
	t.Helper()
	cfs, err := dfs.NewContainerFs(nil)
	require.NoError(t, err)
	require.NoError(t, cfs.Mkdir("/library"))
	require.NoError(t, cfs.Mkdir("/library/Films & Series"))
	require.NoError(t, cfs.Create("/library/Films & Series/film.mkv"))
	return cfs
}

func get(r http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestFilesPage(t *testing.T) {
	r := filesRouter(t, newTree(t), nil)

	w := get(r, "/files")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `hx-push-url="/files?path=%2Flibrary"`)

	w = get(r, "/files?path="+url.QueryEscape("/library/Films & Series"))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `<span aria-current="page">Films &amp; Series</span>`)
	require.Contains(t, body, `href="/fs/library/Films%20&amp;%20Series/film.mkv"`, "download link")
	require.Contains(t, body, `hx-get="/files/folder?path=%2Flibrary%2FFilms+%26+Series"`, "the folder path is escaped in the URL")

	w = get(r, "/files?path=/gone")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "doesn't exist any more")

	// the fragment for a folder removed while it was open says so, rather than failing
	w = get(r, "/files/list?path=/gone")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "doesn't exist any more")
}

func TestFilesFolderAndRename(t *testing.T) {
	cfs := newTree(t)
	r := filesRouter(t, cfs, nil)

	w := get(r, "/files/folder?path=/library")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger-After-Swap"), "open-dialog")

	for name, want := range map[string]string{"": "Enter a name.", "a/b": "can&#39;t contain", "Films & Series": "Path already exists"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, postForm("/files/folder", url.Values{"path": {"/library"}, "name": {name}}))
		require.Equal(t, http.StatusUnprocessableEntity, w.Code, name)
		require.Contains(t, w.Body.String(), want, name)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/files/folder", url.Values{"path": {"/library"}, "name": {"Music"}}))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger"), `"files-changed":true`)
	_, err := cfs.ReadDir("/library/Music")
	require.NoError(t, err)

	// renaming to the same name changes nothing and just closes the dialog
	w = httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/files/rename", url.Values{"path": {"/library/Music"}, "name": {"Music"}}))
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Header().Get("HX-Trigger"), "files-changed")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/files/rename", url.Values{"path": {"/library/Music"}, "name": {"Films & Series"}}))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	require.Contains(t, w.Body.String(), "Destination path already exists")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/files/rename", url.Values{"path": {"/library/Music"}, "name": {"Audio"}}))
	require.Equal(t, http.StatusOK, w.Code)
	_, err = cfs.ReadDir("/library/Audio")
	require.NoError(t, err)
}

func TestFilesDelete(t *testing.T) {
	r := filesRouter(t, newTree(t), nil)

	del := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/files/entry?path="+url.QueryEscape(p), nil))
		return w
	}

	w := del("/library")
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "isn't empty")

	w = del("/library/Films & Series/film.mkv")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger"), `"files-changed":true`)
}

func TestLinksPage(t *testing.T) {
	cfs := newTree(t)
	links := map[string]string{"library/Music": "", "films/film.mkv": "library/Films & Series/film.mkv"}
	svc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) { return links, nil },
	}
	r := filesRouter(t, cfs, svc)

	w := get(r, "/links")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "/films/film.mkv")
	require.Contains(t, body, `href="/files?path=%2Flibrary%2FFilms+%26+Series"`, "the source links to its folder")

	for name, form := range map[string]url.Values{
		"relative":     {"source": {"library/x"}, "target": {"/y"}},
		"root target":  {"source": {"/library/Films & Series/film.mkv"}, "target": {"/"}},
		"missing file": {"source": {"/nope.mkv"}, "target": {"/y.mkv"}},
	} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, postForm("/links", form))
		require.Equal(t, http.StatusUnprocessableEntity, w.Code, name)
		require.Contains(t, w.Body.String(), `class="error" id="link-error" role="alert"`, name)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/links", url.Values{"source": {"/library/Films & Series/film.mkv"}, "target": {"/films/new/film.mkv"}, "in_dialog": {"true"}}))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger"), `"close-dialog":"file-dialog"`)
	_, err := cfs.Open("/films/new/film.mkv")
	require.NoError(t, err, "folders on the way are made")
}
