package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/torrent"
	"github.com/gin-gonic/gin"
)

// qBitTorrent represents a torrent in qBittorrent API format
type qBitTorrent struct {
	Hash           string  `json:"hash"`
	Name           string  `json:"name"`
	Size           int64   `json:"size"`
	Progress       float64 `json:"progress"`
	Dlspeed        int64   `json:"dlspeed"`
	Upspeed        int64   `json:"upspeed"`
	Priority       int     `json:"priority"`
	NumSeeds       int     `json:"num_seeds"`
	NumLeechs      int     `json:"num_leechs"`
	State          string  `json:"state"`
	SavePath       string  `json:"save_path"`
	ContentPath    string  `json:"content_path"`
	Category       string  `json:"category"`
	AddedOn        int64   `json:"added_on"`
	CompletionOn   int64   `json:"completion_on"`
	Tracker        string  `json:"tracker"`
	Tags           string  `json:"tags"`
	AmountLeft     int64   `json:"amount_left"`
	Completed      int64   `json:"completed"`
	TotalSize      int64   `json:"total_size"`
	Ratio          float64 `json:"ratio"`
	Eta            int64   `json:"eta"`
	Uploaded       int64   `json:"uploaded"`
	Downloaded     int64   `json:"downloaded"`
	Availability   float64 `json:"availability"`
	SequentialDl   bool    `json:"seq_dl"`
	FirstLastPiece bool    `json:"f_l_piece_prio"`
	LastActivity   int64   `json:"last_activity"`
}

func qBitLoginHandler(c *gin.Context) {
	// Dummy login, always success
	c.String(http.StatusOK, "Ok.")
}

func qBitWebapiVersionHandler(c *gin.Context) {
	// Mocked webapi version for compatibility
	c.String(http.StatusOK, "2.8.19")
}

func qBitAppVersionHandler(c *gin.Context) {
	// Mocked app version for compatibility
	c.String(http.StatusOK, "v4.3.5")
}

func qBitAppPreferencesHandler(c *gin.Context) {
	// Mocked preferences for compatibility
	c.JSON(http.StatusOK, gin.H{
		"save_path":                 "",
		"temp_path_enabled":         false,
		"listen_port":               8999,
		"upnp":                      false,
		"dl_limit":                  0,
		"up_limit":                  0,
		"max_connecs":               500,
		"max_connecs_per_torrent":    100,
		"max_uploads":               -1,
		"max_uploads_per_torrent":    -1,
		"web_ui_port":               4444,
		"scan_dirs":                  make(map[string]interface{}),
		"export_dir":                "",
		"mail_notification_enabled": false,
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
			totalDownloadData += st.BytesReadData.Int64()
			totalUploadData += st.BytesWrittenData.Int64()
		}

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
		resp := make([]qBitTorrent, 0)

		now := time.Now().Unix()

		for hash, t := range torrents {
			info := t.Info()
			name := t.Name()
			size := int64(0)
			progress := 0.0
			state := "stalledDL"
			category := ss.GetRouteFromHash(hash)

			if info != nil {
				size = info.TotalLength()
				progress = 1.0 // Report 100% to satisfy Radarr
				state = "uploading"
			}

			// Map distribyted torrent to qBit format
			qbt := qBitTorrent{
				Hash:           hash,
				Name:           name,
				Size:           size,
				Progress:       progress,
				State:          state,
				SavePath:       fusePath,
				ContentPath:    fusePath + "/" + name,
				Category:       category,
				Tracker:        "",
				Tags:           "",
				AddedOn:        now,
				CompletionOn:   now,
				AmountLeft:     0,
				Completed:      size,
				TotalSize:      size,
				Ratio:          1.0,
				Eta:            0,
				Uploaded:       0,
				Downloaded:     size,
				Availability:   1.0,
				SequentialDl:   false,
				FirstLastPiece: false,
				LastActivity:   now,
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
