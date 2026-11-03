package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

func searchStats() *dtorrent.Stats {
	ss := dtorrent.NewStats()
	ss.Add("radarr", &mockTorrent{hash: metainfo.NewHashFromHex("1111111111111111111111111111111111111111"), name: "Dune.Part.Two.2024.2160p"})
	ss.Add("radarr", &mockTorrent{hash: metainfo.NewHashFromHex("2222222222222222222222222222222222222222"), name: "Arrival.2016.1080p"})
	ss.Add("sonarr", &mockTorrent{hash: metainfo.NewHashFromHex("3333333333333333333333333333333333333333"), name: "Severance.S02.1080p"})
	return ss
}

// A search keeps the torrents whose name has every word typed, and the routes they're in.
func TestRoutesPage_Search(t *testing.T) {
	p := newRoutesPageFor(routesConf("radarr", "sonarr"), searchStats(), "  1080P arrival ", "")
	require.Equal(t, "1080P arrival", p.Query)
	require.Equal(t, "name", p.Sort)
	require.Equal(t, 1, p.Shown)
	require.Equal(t, 3, p.Total)
	require.Len(t, p.Routes, 1)
	require.Equal(t, "radarr", p.Routes[0].Name)
	require.Equal(t, 2, p.Routes[0].Total)
	require.Equal(t, "Arrival.2016.1080p", p.Routes[0].Torrents[0].Name)

	// with nothing typed, every route shows, empty or not
	p = newRoutesPageFor(routesConf("radarr", "sonarr", "empty"), searchStats(), "", "bogus")
	require.Equal(t, "name", p.Sort, "an unknown sort falls back to the default")
	require.Len(t, p.Routes, 3)
	require.Equal(t, 3, p.Shown)
}

func TestSortTorrents(t *testing.T) {
	ts := []torrentView{
		{Name: "b", activity: 10, age: 50, Health: health{Level: "ok"}},
		{Name: "A", activity: 500, age: 500, Health: health{Level: "warn"}},
		{Name: "c", activity: 0, age: 5, Health: health{Level: "wait"}},
	}
	names := func() (out []string) {
		for _, t := range ts {
			out = append(out, t.Name)
		}
		return out
	}
	sortTorrents(ts, "name")
	require.Equal(t, []string{"A", "b", "c"}, names())
	sortTorrents(ts, "active")
	require.Equal(t, []string{"A", "b", "c"}, names())
	sortTorrents(ts, "attention")
	require.Equal(t, []string{"A", "c", "b"}, names())
	sortTorrents(ts, "newest")
	require.Equal(t, []string{"c", "b", "A"}, names())
}

// The search form's request gets the page's address back for the address bar; the list's own
// refresh keeps the search and doesn't.
func TestRoutesTable_SearchKeepsItsAddress(t *testing.T) {
	r, err := NewHandler(nil, searchStats(), nil, nil, nil, nil, "", routesConf("radarr", "sonarr"), "", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/routes/table?q=dune&sort=newest&push=1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "/routes?q=dune&sort=newest", w.Header().Get("HX-Push-Url"))
	body := w.Body.String()
	require.Contains(t, body, "Dune.Part.Two")
	require.NotContains(t, body, "Severance")
	require.Contains(t, body, `hx-get="/routes/table?q=dune&amp;sort=newest"`)
	require.Contains(t, body, "1 of 3 torrents match")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/routes/table?q=dune&sort=newest", nil))
	require.Empty(t, w.Header().Get("HX-Push-Url"))

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/routes?q=nothing+here", nil))
	require.Contains(t, w.Body.String(), "No torrent's name has every word of “nothing here”")
	require.Contains(t, w.Body.String(), `value="nothing here"`)
}
