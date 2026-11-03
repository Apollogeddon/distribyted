package http

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

// infoTorrent is a torrent with metadata: two pieces, whose hashes are made of b.
type infoTorrent struct {
	mockTorrent
	info *metainfo.Info
}

func (t *infoTorrent) Info() *metainfo.Info { return t.info }

func newInfoTorrent(hash, name string, b byte) *infoTorrent {
	pieces := append(bytes.Repeat([]byte{b}, 20), bytes.Repeat([]byte{b + 1}, 20)...)
	return &infoTorrent{
		mockTorrent: mockTorrent{hash: metainfo.NewHashFromHex(hash), name: name},
		info:        &metainfo.Info{Name: name, PieceLength: 1 << 20, Length: 2 << 20, Pieces: pieces},
	}
}

func cacheFile(t *testing.T, fc *filecache.Cache, b byte, size int) {
	t.Helper()
	f, err := fc.OpenFile("completed/"+hex.EncodeToString(bytes.Repeat([]byte{b}, 20)), 0o102)
	require.NoError(t, err)
	_, err = f.Write(make([]byte, size))
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestCachePage(t *testing.T) {
	fc, err := filecache.NewCache(t.TempDir())
	require.NoError(t, err)
	fc.SetCapacity(4 << 20)
	ss := dtorrent.NewStats()
	ss.Add("radarr", newInfoTorrent(duneHash, "Dune", 0x10))
	cacheFile(t, fc, 0x10, 1<<20)
	cacheFile(t, fc, 0x11, 1<<20)
	cacheFile(t, fc, 0x77, 1000)

	r, err := NewHandler(fc, ss, nil, nil, nil, nil, "", routesConf("radarr"), "", nil)
	require.NoError(t, err)
	body := get(r, "/cache").Body.String()
	require.Contains(t, body, "Dune")
	require.Contains(t, body, "2.0 MiB · 50% of the cache")
	require.Contains(t, body, "2 pieces")
	require.Contains(t, body, `href="/routes/radarr/torrents/`+duneHash+`"`)
	require.Contains(t, body, "1000 B in 1 piece of torrents no longer loaded")
	require.Contains(t, get(r, "/").Body.String(), `href="/cache"`, "the dashboard links to it")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/cache/torrents/"+duneHash, nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("HX-Trigger"), "Freed 2 pieces")
	require.NotContains(t, get(r, "/cache/list").Body.String(), "Dune")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/cache/torrents/ffff", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}
