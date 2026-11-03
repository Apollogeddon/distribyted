package http

import (
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
