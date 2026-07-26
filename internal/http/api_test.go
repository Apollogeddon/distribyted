package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/config"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTorrentService struct {
	addMagnetFunc          func(r, m string) error
	removeFromHashFunc     func(r, h string) error
	removeFromHashOnlyFunc func(h string) error
	listLinksFunc          func() (map[string]string, error)
}

func (m *mockTorrentService) AddMagnet(r, magnet string) error {
	return m.addMagnetFunc(r, magnet)
}

func (m *mockTorrentService) RemoveFromHash(r, h string) error {
	return m.removeFromHashFunc(r, h)
}

func (m *mockTorrentService) RemoveFromHashOnly(h string) error {
	return m.removeFromHashOnlyFunc(h)
}

func (m *mockTorrentService) ListLinks() (map[string]string, error) {
	return m.listLinksFunc()
}

type mockLinkFs struct {
	linkFunc   func(oldpath, newpath string) error
	removeFunc func(path string) error
}

func (m *mockLinkFs) Link(oldpath, newpath string) error {
	return m.linkFunc(oldpath, newpath)
}

func (m *mockLinkFs) Remove(path string) error {
	return m.removeFunc(path)
}

func TestApiStatusHandler(t *testing.T) {
	ss := dtorrent.NewStats()
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{
			Port:        4444,
			IP:          "0.0.0.0",
			HTTPFS:      false,
			DisableAuth: true,
		},
	}

	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/status", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Contains(t, response, "cacheItems")
	assert.Contains(t, response, "torrentStats")
}

func TestApiServersHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/servers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApiRoutesHandler(t *testing.T) {
	ss := dtorrent.NewStats()
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/routes", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApiAddTorrentHandler(t *testing.T) {
	mockSvc := &mockTorrentService{
		addMagnetFunc: func(r, m string) error {
			assert.Equal(t, "test-route", r)
			assert.Equal(t, "test-magnet", m)
			return nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	body, _ := json.Marshal(RouteAdd{Magnet: "test-magnet"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/routes/test-route/torrent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

type CloseNotifyingRecorder struct {
	*httptest.ResponseRecorder
}

func (c *CloseNotifyingRecorder) CloseNotify() <-chan bool {
	return make(<-chan bool)
}

func TestApiLogHandler(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "testlog")
	assert.NoError(t, err)
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	content := "test log content"
	_, err = tmpfile.WriteString(content)
	assert.NoError(t, err)
	_ = tmpfile.Close()

	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, tmpfile.Name(), conf, "", nil)
	assert.NoError(t, err)

	w := &CloseNotifyingRecorder{httptest.NewRecorder()}
	req, _ := http.NewRequest("GET", "/api/log", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, content, w.Body.String())
}

func TestApiAddTorrentHandlerInvalidJson(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/routes/test-route/torrent", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApiDelTorrentHandler(t *testing.T) {
	mockSvc := &mockTorrentService{
		removeFromHashFunc: func(r, h string) error {
			assert.Equal(t, "test-route", r)
			assert.Equal(t, "test-hash", h)
			return nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/routes/test-route/torrent/test-hash", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApiListLinksHandler(t *testing.T) {
	// loader.DB.ListLinks strips the leading "/" from its map keys (a
	// pre-existing quirk of how it parses its storage-key prefix); the
	// handler must normalize this back so the API's output paths are usable
	// (e.g. as-is against ContainerFs, or round-tripped into DELETE).
	mockSvc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) {
			return map[string]string{
				"library/z-movie.mkv": "/downloads/z-movie.mkv",
				"library/a-movie.mkv": "/downloads/a-movie.mkv",
				"some/dir":            "/",
			}, nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/links", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var links []Link
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &links))
	require.Len(t, links, 3)

	// sorted by NewPath
	assert.Equal(t, "/library/a-movie.mkv", links[0].NewPath)
	assert.Equal(t, "/downloads/a-movie.mkv", links[0].OldPath)
	assert.False(t, links[0].IsDir)

	assert.Equal(t, "/library/z-movie.mkv", links[1].NewPath)
	assert.False(t, links[1].IsDir)

	assert.Equal(t, "/some/dir", links[2].NewPath)
	assert.True(t, links[2].IsDir)
}

func TestApiListLinksHandler_Empty(t *testing.T) {
	mockSvc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/links", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", w.Body.String())
}

func TestApiListLinksHandler_Error(t *testing.T) {
	mockSvc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) {
			return nil, errors.New("db error")
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/links", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestApiAddLinkHandler(t *testing.T) {
	mockLfs := &mockLinkFs{
		linkFunc: func(oldpath, newpath string) error {
			assert.Equal(t, "/downloads/movie.mkv", oldpath)
			assert.Equal(t, "/library/movie.mkv", newpath)
			return nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", mockLfs)
	assert.NoError(t, err)

	body, _ := json.Marshal(LinkAdd{OldPath: "/downloads/movie.mkv", NewPath: "/library/movie.mkv"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApiAddLinkHandler_MissingField(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	body, _ := json.Marshal(LinkAdd{OldPath: "/downloads/movie.mkv"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApiAddLinkHandler_SourceNotFound(t *testing.T) {
	mockLfs := &mockLinkFs{
		linkFunc: func(oldpath, newpath string) error {
			return os.ErrNotExist
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", mockLfs)
	assert.NoError(t, err)

	body, _ := json.Marshal(LinkAdd{OldPath: "/nope", NewPath: "/also-nope"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestApiAddLinkHandler_DestExists(t *testing.T) {
	mockLfs := &mockLinkFs{
		linkFunc: func(oldpath, newpath string) error {
			return os.ErrExist
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", mockLfs)
	assert.NoError(t, err)

	body, _ := json.Marshal(LinkAdd{OldPath: "/a", NewPath: "/b"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestApiDelLinkHandler(t *testing.T) {
	// Mock key matches real loader.DB.ListLinks behavior: no leading "/".
	mockSvc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) {
			return map[string]string{"library/movie.mkv": "/downloads/movie.mkv"}, nil
		},
	}
	mockLfs := &mockLinkFs{
		removeFunc: func(path string) error {
			assert.Equal(t, "/library/movie.mkv", path)
			return nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", mockLfs)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/links/library/movie.mkv", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApiDelLinkHandler_RootRejected(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/links/", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApiDelLinkHandler_UnknownPathNotFound(t *testing.T) {
	mockSvc := &mockTorrentService{
		listLinksFunc: func() (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/links/not-a-link.txt", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestQBitTorrentsAddHandler(t *testing.T) {
	mockSvc := &mockTorrentService{
		addMagnetFunc: func(r, m string) error {
			assert.Equal(t, "torrents", r)
			assert.Equal(t, "test-magnet", m)
			return nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/torrents/add", strings.NewReader("urls=test-magnet"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Ok.", w.Body.String())
}

func TestQBitTorrentsDeleteHandler(t *testing.T) {
	mockSvc := &mockTorrentService{
		removeFromHashOnlyFunc: func(h string) error {
			assert.Equal(t, "test-hash", h)
			return nil
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/torrents/delete", strings.NewReader("hashes=test-hash"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Ok.", w.Body.String())
}

func TestApiAddTorrentHandlerError(t *testing.T) {
	mockSvc := &mockTorrentService{
		addMagnetFunc: func(r, m string) error {
			return errors.New("add error")
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	body, _ := json.Marshal(RouteAdd{Magnet: "test-magnet"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/routes/test-route/torrent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestQBitCategoryIsolation(t *testing.T) {
	// Each NewHandler call must use its own categoryStore; categories added to
	// one handler must not be visible in another (the old package-level global
	// caused test pollution and inter-instance leakage in production).
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r1, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", conf, "/fuse", nil)
	require.NoError(t, err)
	r2, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", conf, "/fuse", nil)
	require.NoError(t, err)

	// Add a category to r1 only
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/torrents/createCategory", strings.NewReader("category=isolated-cat"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r1.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// r2 must not see that category
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v2/torrents/categories", nil)
	r2.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	resp := make(map[string]interface{})
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotContains(t, resp, "isolated-cat", "category stores must not be shared between handler instances")
}

func TestQBitTorrentsCategoriesFlow(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", conf, "/fuse", nil)
	assert.NoError(t, err)

	// Create category
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/torrents/createCategory", strings.NewReader("category=new-cat"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// List categories
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v2/torrents/categories", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	resp := make(map[string]interface{})
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "new-cat")

	// Remove category
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/v2/torrents/removeCategories", strings.NewReader("categories=new-cat"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify removed
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v2/torrents/categories", nil)
	r.ServeHTTP(w, req)
	resp2 := make(map[string]interface{})
	err = json.Unmarshal(w.Body.Bytes(), &resp2)
	assert.NoError(t, err)
	assert.NotContains(t, resp2, "new-cat")
}

type mockTorrent struct {
	hash  metainfo.Hash
	name  string
	stats torrent.TorrentStats
}

func (m *mockTorrent) InfoHash() metainfo.Hash { return m.hash }
func (m *mockTorrent) Info() *metainfo.Info    { return nil }
func (m *mockTorrent) GotInfo() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (m *mockTorrent) Files() []*torrent.File                 { return nil }
func (m *mockTorrent) Name() string                           { return m.name }
func (m *mockTorrent) PieceStateRuns() torrent.PieceStateRuns { return nil }
func (m *mockTorrent) Stats() torrent.TorrentStats            { return m.stats }
func (m *mockTorrent) Drop()                                  {}
func (m *mockTorrent) DownloadPieces(int, int)                {}
func (m *mockTorrent) SetPriority(int, torrent.PiecePriority) {}

func TestQBitTorrentsInfoWithData(t *testing.T) {
	ss := dtorrent.NewStats()
	hash := metainfo.NewHashFromHex("0123456789abcdef0123456789abcdef01234567")
	mt := &mockTorrent{
		hash: hash,
		name: "test-torrent",
	}
	ss.Add("test-category", mt)

	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "/fuse", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/torrents/info", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []qBitTorrent
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(resp))
	assert.Equal(t, hash.String(), resp[0].Hash)
	assert.Equal(t, "test-torrent", resp[0].Name)
	assert.Equal(t, "test-category", resp[0].Category)
	assert.Equal(t, "stalledDL", resp[0].State)
}

func TestQBitTransferInfoWithData(t *testing.T) {
	ss := dtorrent.NewStats()
	hash := metainfo.NewHashFromHex("0123456789abcdef0123456789abcdef01234567")

	// mock anacrolix stats
	astats := torrent.TorrentStats{}
	// Use Add method for anacrolix/torrent.Count
	astats.BytesReadData.Add(1024)
	astats.BytesWrittenData.Add(512)

	mt := &mockTorrent{
		hash:  hash,
		name:  "test-torrent",
		stats: astats,
	}
	ss.Add("test-category", mt)

	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/transfer/info", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, float64(1024), resp["dl_info_data"])
	assert.Equal(t, float64(512), resp["up_info_data"])
}

func TestApiDelTorrentHandlerError(t *testing.T) {
	mockSvc := &mockTorrentService{
		removeFromHashFunc: func(r, h string) error {
			return errors.New("del error")
		},
	}
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	r, err := NewHandler(nil, nil, mockSvc, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/routes/test-route/torrent/test-hash", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestQBitWebapiVersionHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{
			Port:        4444,
			IP:          "0.0.0.0",
			HTTPFS:      false,
			DisableAuth: true,
		},
	}

	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/app/webapiVersion", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "2.8.19", w.Body.String())
}

func TestQBitLoginHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/auth/login", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Ok.", w.Body.String())
}

func TestQBitAppVersionHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/app/version", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "v4.3.5", w.Body.String())
}

func TestQBitAppPreferencesHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
		Torrent:    &config.TorrentGlobal{DisableIPv6: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "/test/path", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/app/preferences", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "/test/path", resp["save_path"])
	assert.Equal(t, float64(4444), resp["web_ui_port"])
	assert.Equal(t, false, resp["ipv6_enabled"])
}

func TestQBitAppSetPreferencesHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/app/setPreferences", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Ok.", w.Body.String())
}

func TestQBitTransferInfoHandler(t *testing.T) {
	ss := dtorrent.NewStats()
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/transfer/info", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "connected", resp["connection_status"])
}

func TestQBitTorrentsInfoHandler(t *testing.T) {
	ss := dtorrent.NewStats()
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/torrents/info", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(resp))
}

func TestQBitTorrentsCategoriesHandler(t *testing.T) {
	ss := dtorrent.NewStats()
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "/fuse", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/torrents/categories", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestQBitTorrentsCreateCategoryHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/torrents/createCategory", strings.NewReader("category=test-cat"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestQBitTorrentsMockHandler(t *testing.T) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v2/torrents/pause", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWebHandlers(t *testing.T) {
	ss := dtorrent.NewStats()
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}
	r, err := NewHandler(nil, ss, nil, nil, nil, nil, "", conf, "", nil)
	assert.NoError(t, err)

	paths := []string{"/", "/routes", "/logs", "/servers"}
	for _, path := range paths {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", path, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}
