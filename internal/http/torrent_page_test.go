package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

const duneHash = "1111111111111111111111111111111111111111"

// A torrent's page lists its files, whether each is ready to play, and the links that show
// each one.
func TestTorrentPage(t *testing.T) {
	old := torrentFiles
	t.Cleanup(func() { torrentFiles = old })
	torrentFiles = func(dfs.Torrent) []torrentFileInfo {
		return []torrentFileInfo{
			{Path: "Dune/sample.mkv", Length: 50 << 20},
			{Path: "Dune/dune.mkv", Length: 4 << 30},
		}
	}

	ss := dtorrent.NewStats()
	ss.Add("radarr", &mockTorrent{hash: metainfo.NewHashFromHex(duneHash), name: "Dune"})
	film := &fakeTorrentFile{MemoryFile: dfs.NewMemoryFile(make([]byte, 16)), r: dfs.Readiness{Start: true, End: true}}
	route := dfs.NewMemory()
	require.NoError(t, route.Storage.Add(film, "/Dune/dune.mkv"))
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/radarr": route})
	require.NoError(t, err)
	svc := &mockTorrentService{listLinksFunc: func() (map[string]string, error) {
		return map[string]string{
			"movies/Dune (2021)/Dune (2021).mkv": "/radarr/Dune/dune.mkv",
			"movies/Dune (2021)":                 "/",
		}, nil
	}}
	r, err := NewHandler(nil, ss, svc, nil, nil, nil, "", routesConf("radarr"), "", cfs)
	require.NoError(t, err)

	w := get(r, "/routes/radarr/torrents/"+duneHash)
	require.Equal(t, 200, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "dune.mkv")
	require.Contains(t, body, "4.0 GiB")
	require.Contains(t, body, "/movies/Dune (2021)/Dune (2021).mkv")
	require.Contains(t, body, "Ready to play")
	require.Contains(t, body, "Not linked", "the sample isn't in the library")
	require.Contains(t, body, "1 of 2 files")
	require.Contains(t, body, `hx-get="/files/readiness?path=%2Fradarr%2FDune%2Fdune.mkv"`)

	// the Routes list links to it
	require.Contains(t, get(r, "/routes").Body.String(), `href="/routes/radarr/torrents/`+duneHash+`"`)

	w = get(r, "/routes/sonarr/torrents/"+duneHash)
	require.Equal(t, 404, w.Code)
	require.Contains(t, w.Body.String(), "isn't in this route any more")
	require.Contains(t, get(r, "/routes/sonarr/torrents/"+duneHash+"/body").Body.String(), "isn't in this route any more")
}

// A torrent's page lists its peers in full, and acts on the whole torrent.
func TestTorrentPage_PeersAndActions(t *testing.T) {
	oldFiles, oldPeers := torrentFiles, torrentPeers
	t.Cleanup(func() { torrentFiles, torrentPeers = oldFiles, oldPeers })
	torrentFiles = func(dfs.Torrent) []torrentFileInfo {
		return []torrentFileInfo{{Path: "Dune/dune.mkv", Length: 4 << 30}, {Path: "Dune/extra.mkv", Length: 1 << 30}}
	}
	torrentPeers = func(dfs.Torrent) []dtorrent.PeerInfo {
		return []dtorrent.PeerInfo{
			{Addr: "203.0.113.7:51413", Client: "qBittorrent 5.1.0", Source: "Tracker", Down: 3 << 20, Have: 100, Of: 100, Seed: true},
			{Addr: "[2001:db8::1]:6881", Client: "Transmission 4.0", Source: "DHT", Up: 64 << 10, Have: 42, Of: 100},
		}
	}

	ss := dtorrent.NewStats()
	ss.Add("radarr", &mockTorrent{hash: metainfo.NewHashFromHex(duneHash), name: "Dune"})
	film := &fakeTorrentFile{MemoryFile: dfs.NewMemoryFile(make([]byte, 16))}
	extra := &fakeTorrentFile{MemoryFile: dfs.NewMemoryFile(make([]byte, 16))}
	route := dfs.NewMemory()
	require.NoError(t, route.Storage.Add(film, "/Dune/dune.mkv"))
	require.NoError(t, route.Storage.Add(extra, "/Dune/extra.mkv"))
	cfs, err := dfs.NewContainerFs(map[string]dfs.Filesystem{"/radarr": route})
	require.NoError(t, err)
	var deleted string
	svc := &mockTorrentService{
		listLinksFunc:      func() (map[string]string, error) { return nil, nil },
		removeFromHashFunc: func(r, h string) error { deleted = r + "/" + h; return nil },
	}
	r, err := NewHandler(nil, ss, svc, nil, nil, nil, "", routesConf("radarr"), "", cfs)
	require.NoError(t, err)

	body := get(r, "/routes/radarr/torrents/"+duneHash).Body.String()
	require.Contains(t, body, "203.0.113.7:51413", "addresses are shown in full")
	require.Contains(t, body, "[2001:db8::1]:6881")
	require.Contains(t, body, "qBittorrent 5.1.0")
	require.Contains(t, body, "Seeder")
	require.Contains(t, body, "42%")
	require.Contains(t, body, "3.0 MiB/s")
	require.Contains(t, body, "2 connected peers")
	require.Contains(t, body, "magnet:?xt=urn:btih:"+duneHash)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/routes/radarr/torrents/"+duneHash+"/prepare", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger"), "2 files")
	require.Equal(t, []dfs.Preparation{dfs.PrepareStart}, film.asked)
	require.Equal(t, []dfs.Preparation{dfs.PrepareStart}, extra.asked)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/routes/sonarr/torrents/"+duneHash+"/prepare", nil))
	require.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/routes/radarr/torrents/"+duneHash+"?redirect=1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "/routes", w.Header().Get("HX-Redirect"))
	require.Equal(t, "radarr/"+duneHash, deleted)
}
