package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/config"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

func routesConf(names ...string) *config.Root {
	conf := &config.Root{HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true}}
	for _, n := range names {
		conf.Routes = append(conf.Routes, &config.Route{Name: n})
	}
	return conf
}

func postForm(target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return req
}

// TestRoutesPage lists every configured route, including one without torrents, alongside
// routes that only exist because something was added to them, and offers them all in the
// add form.
func TestRoutesPage(t *testing.T) {
	ss := dtorrent.NewStats()
	ss.AddRoute("added-at-runtime")
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", routesConf("movies", "empty"), "", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/routes", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	for _, name := range []string{"movies", "empty", "added-at-runtime"} {
		require.Contains(t, body, `<option value="`+name+`"`)
	}
	require.Contains(t, body, "No torrents yet")
	require.Contains(t, body, `hx-get="/routes/table"`)
	require.NotContains(t, body, "cdn.", "the page must not need the internet")

	// the fragment the page polls is the routes alone
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/routes/table", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, strings.HasPrefix(strings.TrimSpace(w.Body.String()), `<section id="routes"`))
}

func TestRoutesAdd(t *testing.T) {
	var added []string
	svc := &mockTorrentService{addMagnetFunc: func(r, m string) error {
		if strings.Contains(m, "fails") {
			return errors.New("timed out")
		}
		added = append(added, r+" "+m)
		return nil
	}}
	r, err := NewHandler(nil, dtorrent.NewStats(), svc, nil, nil, nil, "", routesConf("movies"), "", nil)
	require.NoError(t, err)

	cases := []struct {
		name, route, magnet, wantError string
	}{
		{"not a magnet", "movies", "https://example.com/x.torrent", "Paste a magnet link"},
		{"no route", "", "magnet:?xt=urn:btih:abc", "Choose a route."},
		{"bad route", "../etc", "magnet:?xt=urn:btih:abc", "Choose a route."},
		{"service error", "movies", "magnet:?xt=urn:btih:fails", "Couldn&#39;t add the magnet: timed out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, postForm("/routes/torrents", url.Values{"route": {tc.route}, "magnet": {tc.magnet}}))
			require.Equal(t, http.StatusUnprocessableEntity, w.Code)
			require.Contains(t, w.Body.String(), tc.wantError)
			require.Contains(t, w.Body.String(), `aria-invalid="true"`)
			require.Empty(t, w.Header().Get("HX-Trigger"))
		})
	}
	require.Empty(t, added)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, postForm("/routes/torrents", url.Values{"route": {"movies"}, "magnet": {"  magnet:?xt=urn:btih:abc  "}}))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"movies magnet:?xt=urn:btih:abc"}, added)
	trigger := w.Header().Get("HX-Trigger")
	require.Contains(t, trigger, `"close-dialog":"add-magnet"`)
	require.Contains(t, trigger, `"routes-changed":true`)
	require.NotContains(t, w.Body.String(), "magnet:?xt=urn:btih:abc", "the form comes back empty")
}

func TestRoutesDelete(t *testing.T) {
	var removed string
	svc := &mockTorrentService{removeFromHashFunc: func(r, h string) error {
		if r == "broken" {
			return errors.New("no such torrent")
		}
		removed = r + " " + h
		return nil
	}}
	r, err := NewHandler(nil, dtorrent.NewStats(), svc, nil, nil, nil, "", routesConf("my films"), "", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/routes/my%20films/torrents/ABCDEF", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "my films abcdef", removed)
	require.Contains(t, w.Header().Get("HX-Trigger"), `"routes-changed":true`)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/routes/broken/torrents/abc", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "no such torrent")
}

// TestHTMXRequestWithoutSession: htmx would follow a redirect to the login page and swap
// it into the routes list, so it gets a 401 telling it to load the login page instead.
func TestHTMXRequestWithoutSession(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "", nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/routes/table", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", "http://localhost:4444/routes")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "/login?next=%2Froutes", w.Header().Get("HX-Redirect"))

	// a page URL from elsewhere doesn't become the place login returns to
	req.Header.Set("HX-Current-URL", "https://evil.example//evil.example/x")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, "/login?next=%2F", w.Header().Get("HX-Redirect"))
}

func TestTorrentView(t *testing.T) {
	pending := newTorrentView("movies", &dtorrent.TorrentStats{Name: "a", Hash: "h", AgeSeconds: 60})
	require.Equal(t, "wait", pending.Health.Level)
	require.Equal(t, "Fetching metadata", pending.Health.Label)
	require.Empty(t, pending.Pieces)

	ts := &dtorrent.TorrentStats{
		Name: "b", Hash: "h", TotalPieces: 4, PieceSize: 1 << 20, Peers: 6, Seeders: 5, AgeSeconds: 60,
		DownloadedBytes: 3 << 20, TimePassed: 2,
		PieceChunks: []*dtorrent.PieceChunk{
			{Status: dtorrent.Complete, NumPieces: 1},
			{Status: dtorrent.Waiting, NumPieces: 3},
		},
	}
	tv := newTorrentView("my films", ts)
	require.Equal(t, "/routes/my%20films/torrents/h", tv.DeleteURL)
	require.Equal(t, "1.5 MiB/s", tv.Down)
	require.Equal(t, "0 B/s", tv.Up)
	require.Equal(t, "25% · 1 of 4 pieces", tv.Progress)
	require.Equal(t, []pieceSegment{
		{Class: "piece-complete", Width: "25.0000%", Label: "Pieces 0–0 downloaded"},
		{Class: "piece-waiting", Width: "75.0000%", Label: "Pieces 1–3 not downloaded"},
	}, tv.Pieces)
	require.Equal(t, "ok", tv.Health.Level)
}

func TestTorrentHealth(t *testing.T) {
	cases := []struct {
		name          string
		ts            dtorrent.TorrentStats
		level, reason string
	}{
		{"healthy", dtorrent.TorrentStats{Peers: 8, Seeders: 5, PieceSize: 1 << 20, AgeSeconds: 60}, "ok", ""},
		{"still connecting", dtorrent.TorrentStats{AgeSeconds: 5, PieceSize: 1 << 20}, "wait", "Finding peers"},
		{"no peers", dtorrent.TorrentStats{AgeSeconds: 60, PieceSize: 1 << 20}, "warn", "No peers are connected"},
		{"one seeder", dtorrent.TorrentStats{Peers: 3, Seeders: 1, AgeSeconds: 60, PieceSize: 1 << 20}, "warn", "Only 1 seeder connected"},
		{"large pieces", dtorrent.TorrentStats{Peers: 8, Seeders: 5, AgeSeconds: 60, PieceSize: 16 << 20}, "warn", "Pieces are 16 MiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := torrentHealth(&tc.ts)
			require.Equal(t, tc.level, h.Level)
			require.Contains(t, h.Reason, tc.reason)
		})
	}
}

// TestJSONStringIsASCII: header values reach the page as Latin-1, so the JSON in them must
// be plain ASCII and still decode to the original text.
func TestJSONStringIsASCII(t *testing.T) {
	in := map[string]any{"toast": toast{Level: "success", Message: "Folder “Séries 🎬” created."}}
	s, err := jsonString(in)
	require.NoError(t, err)
	for _, r := range s {
		require.Less(t, r, rune(0x80), s)
	}
	var out map[string]toast
	require.NoError(t, json.Unmarshal([]byte(s), &out))
	require.Equal(t, "Folder “Séries 🎬” created.", out["toast"].Message)
}
