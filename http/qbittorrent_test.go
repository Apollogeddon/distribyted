package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestQBitTorrentsMockEndpoints(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Setup a router with just the mock endpoints
	router := gin.New()
	router.POST("/api/v2/torrents/createCategory", qBitTorrentsMockHandler)
	router.POST("/api/v2/torrents/setCategory", qBitTorrentsMockHandler)
	router.POST("/api/v2/torrents/addTags", qBitTorrentsMockHandler)
	router.POST("/api/v2/torrents/pause", qBitTorrentsMockHandler)
	router.POST("/api/v2/torrents/resume", qBitTorrentsMockHandler)

	endpoints := []string{
		"/api/v2/torrents/createCategory",
		"/api/v2/torrents/setCategory",
		"/api/v2/torrents/addTags",
		"/api/v2/torrents/pause",
		"/api/v2/torrents/resume",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", endpoint, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "", w.Body.String())
		})
	}
}