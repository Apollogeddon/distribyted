package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/torrent"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQBitCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ss := torrent.NewStats()
	
	router := gin.New()
	router.GET("/api/v2/app/version", qBitAppVersionHandler)
	router.GET("/api/v2/torrents/info", qBitTorrentsInfoHandler(ss, "/tmp/mount"))
	
	// Create a dummy config for testing
	rootConf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{Port: 4444},
		Torrent:    &config.TorrentGlobal{DisableIPv6: true},
	}
	router.GET("/api/v2/app/preferences", qBitAppPreferencesHandler(rootConf, "/tmp/mount"))

	t.Run("AppVersion", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v2/app/version", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "v")
	})

	t.Run("PreferencesFields", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v2/app/preferences", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var prefs map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &prefs)
		require.NoError(t, err)

		// Check for some critical fields Sonarr might check
		assert.Contains(t, prefs, "web_ui_port")
		assert.Contains(t, prefs, "scan_dirs")
	})

	t.Run("TorrentsInfoSchema", func(t *testing.T) {
		// We don't need actual torrents to check the schema if we had at least one,
		// but since we want to check the fields, empty list is fine for status code.
		// To check fields, let's mock one if possible or just check the struct.
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v2/torrents/info", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "[]", w.Body.String())
	})
}

func TestQBitTorrentStructSerialization(t *testing.T) {
	// Directly test the serialization to ensure all fields are present in JSON
	qbt := qBitTorrent{
		Hash: "hash",
		Name: "name",
		Category: "cat",
		AddedOn: 123,
		LastActivity: 456,
	}

	b, err := json.Marshal(qbt)
	require.NoError(t, err)
	
	jsonStr := string(b)
	assert.Contains(t, jsonStr, "\"hash\":\"hash\"")
	assert.Contains(t, jsonStr, "\"category\":\"cat\"")
	assert.Contains(t, jsonStr, "\"added_on\":123")
	assert.Contains(t, jsonStr, "\"last_activity\":456")
	assert.Contains(t, jsonStr, "\"amount_left\":0")
	assert.Contains(t, jsonStr, "\"ratio\":0") // Default for float64 if not set in literal
}
