package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/torrent"
	"github.com/gin-gonic/gin"
)

// qBitTorrent represents a torrent in qBittorrent API format
type qBitTorrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Dlspeed      int64   `json:"dlspeed"`
	Upspeed      int64   `json:"upspeed"`
	Priority     int     `json:"priority"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	State        string  `json:"state"`
	SavePath     string  `json:"save_path"`
	ContentPath  string  `json:"content_path"`
	Category     string  `json:"category"`
	AddedOn      int64   `json:"added_on"`
	CompletionOn int64   `json:"completion_on"`
	Tracker      string  `json:"tracker"`
}

func qBitLoginHandler(c *gin.Context) {
	// Dummy login, always success
	c.String(http.StatusOK, "Ok.")
}

func qBitWebapiVersionHandler(c *gin.Context) {
	// Mocked webapi version for compatibility
	c.String(http.StatusOK, "2.8.19")
}

func qBitAppPreferencesHandler(c *gin.Context) {
	// Mocked preferences for compatibility
	c.JSON(http.StatusOK, gin.H{
		"save_path":              "",
		"temp_path_enabled":      false,
		"listen_port":            8999,
		"upnp":                   false,
		"dl_limit":               0,
		"up_limit":               0,
		"max_connecs":            500,
		"max_connecs_per_torrent": 100,
		"max_uploads":            -1,
		"max_uploads_per_torrent": -1,
		"web_ui_port":            4444,
	})
}

func qBitAppSetPreferencesHandler(c *gin.Context) {
	// Dummy set preferences, always success
	c.String(http.StatusOK, "Ok.")
}

func qBitTransferInfoHandler(ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		torrents := ss.GetAllTorrents()
		var totalDownloadData int64
		var totalUploadData int64

		for _, t := range torrents {
			st := t.Stats()
			// Stats() returns cumulative data and current rates
			// We can use the cumulative data for session totals
			// and calculate speeds from deltas if we wanted, but anacrolix/torrent
			// might not give us instant rates easily in a compatible way here
			// For now, we'll use a simplified version.
			totalDownloadData += st.BytesReadData.Int64()
			totalUploadData += st.BytesWrittenData.Int64()
		}

		// Since distribyted's torrent.Stats already tracks deltas for speeds,
		// we can leverage GlobalStats() but we need to be careful about interference.
		// For simplicity in this mock, we'll just return the session totals.

		c.JSON(http.StatusOK, gin.H{
			"connection_status":    "connected",
			"dht_nodes":            0,
			"dl_info_data":         totalDownloadData,
			"dl_info_speed":        0, // TODO: calculate real speed
			"dl_rate_limit":        0,
			"up_info_data":         totalUploadData,
			"up_info_speed":        0, // TODO: calculate real speed
			"up_rate_limit":        0,
			"refresh_interval":     2000,
			"queueing_enabled":     false,
			"use_alt_speed_limits": false,
		})
	}
}

var mockCreatedCategories = make(map[string]bool)

func qBitTorrentsCategoriesHandler(ch *config.Handler, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp := make(map[string]gin.H)

		// First, add all explicitly configured routes
		if ch != nil {
			if root, err := ch.Get(); err == nil && root != nil {
				for _, r := range root.Routes {
					resp[r.Name] = gin.H{
						"name":     r.Name,
						"savePath": "", 
					}
				}
			}
		}

		// Add dynamically mocked categories
		for cat := range mockCreatedCategories {
			resp[cat] = gin.H{
				"name":     cat,
				"savePath": "",
			}
		}

		// Also add any routes that have active torrents
		routes := ss.RoutesStats()
		for _, r := range routes {
			resp[r.Name] = gin.H{
				"name":     r.Name,
				"savePath": "",
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

func qBitTorrentsCreateCategoryHandler(c *gin.Context) {
	category := c.PostForm("category")
	if category != "" {
		mockCreatedCategories[category] = true
	}
	c.String(http.StatusOK, "")
}

func qBitTorrentsMockHandler(c *gin.Context) {
	// A generic mock handler for endpoints like pause, resume, setCategory, addTags.
	c.String(http.StatusOK, "")
}

func qBitTorrentsInfoHandler(ss *torrent.Stats, fusePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		torrents := ss.GetAllTorrents()
		var resp []qBitTorrent

		for hash, t := range torrents {
			info := t.Info()
			name := t.Name()
			size := int64(0)
			progress := 0.0
			state := "stalledDL"

			if info != nil {
				size = info.TotalLength()
				progress = 1.0 // Report 100% to satisfy Radarr
				state = "seeding"
			}

			// Map distribyted torrent to qBit format
			qbt := qBitTorrent{
				Hash:        hash,
				Name:        name,
				Size:        size,
				Progress:    progress,
				State:       state,
				SavePath:    fusePath,
				ContentPath: fusePath + "/" + name,
			}
			resp = append(resp, qbt)
		}

		c.JSON(http.StatusOK, resp)
	}
}

func qBitTorrentsAddHandler(s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		urls := c.PostForm("urls")
		category := c.PostForm("category")
		if category == "" {
			category = "torrents" // Default route
		}

		magnets := strings.Split(urls, "\n")
		for _, m := range magnets {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if err := s.AddMagnet(category, m); err != nil {
				// We log error but continue with others
				fmt.Printf("Error adding magnet via qBit API: %v\n", err)
			}
		}

		c.String(http.StatusOK, "Ok.")
	}
}

func qBitTorrentsDeleteHandler(s *torrent.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		hashes := c.PostForm("hashes")
		// qBit sends hashes separated by |
		hashList := strings.Split(hashes, "|")

		for _, h := range hashList {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			if err := s.RemoveFromHashOnly(h); err != nil {
				fmt.Printf("Error deleting torrent via qBit API: %v\n", err)
			}
		}

		c.String(http.StatusOK, "Ok.")
	}
}
