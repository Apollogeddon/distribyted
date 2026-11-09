package http

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
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
	f, err := fc.OpenFile("completed/"+hex.EncodeToString(bytes.Repeat([]byte{b}, 20)), os.O_CREATE|os.O_RDWR)
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

	r, err := NewHandler(dtorrent.NewPieceCache(fc), ss, nil, nil, nil, nil, "", routesConf("radarr"), "", nil)
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

// The Cache page says what was downloaded and why, how quickly opens got their first byte,
// and how much is kept for files' starts.
func TestCachePage_DownloadsAndProtected(t *testing.T) {
	dir := t.TempDir()
	pc, err := dtorrent.OpenPieceCache(filepath.Join(dir, "cache"), filepath.Join(dir, "protected"), 10)
	require.NoError(t, err)
	pc.SetCapacity(100 << 20)
	dfs.PieceDownloaded("cafe", 1, 3<<20)

	r, err := NewHandler(pc, dtorrent.NewStats(), nil, nil, nil, nil, "", routesConf("radarr"), "", nil)
	require.NoError(t, err)
	body := get(r, "/cache/list").Body.String()
	require.Contains(t, body, "Downloaded")
	for _, why := range []string{"Read", "Linked into the library", "Next episode", "Prepared by hand"} {
		require.Contains(t, body, why)
	}
	require.Contains(t, body, "0 B of 10 MiB is kept for files&#39; first and last megabytes")

	page := newCachePage(pc, nil, time.Now())
	var read downloadRow
	for _, row := range page.Downloads.Rows {
		if row.Reason == "Read" {
			read = row
		}
	}
	require.NotEqual(t, "0 B", read.Day)
}

func TestFirstByteNote(t *testing.T) {
	require.Empty(t, firstByteNote(dfs.FirstByteStats{}))
	require.Equal(t, "Of the latest 40 files opened, half had their first byte within 120ms, and all but one in twenty within 2.5s.",
		firstByteNote(dfs.FirstByteStats{Count: 40, Median: 120 * time.Millisecond, P95: 2480 * time.Millisecond}))
	require.Equal(t, "The latest file opened had its first byte within 1s.",
		firstByteNote(dfs.FirstByteStats{Count: 1, Median: time.Second, P95: time.Second}))
}
