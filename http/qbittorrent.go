package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/Apollogeddon/distribyted/fs"
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

func qBitAppPreferencesHandler(conf *config.Root, fusePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Mocked preferences for compatibility
		pref := gin.H{
			"save_path":                 fusePath,
			"temp_path_enabled":         false,
			"listen_port":               8999,
			"upnp":                      false,
			"dl_limit":                  0,
			"up_limit":                  0,
			"max_connecs":               500,
			"max_connecs_per_torrent":   100,
			"max_uploads":               -1,
			"max_uploads_per_torrent":   -1,
			"web_ui_port":               4444,
			"scan_dirs":                 make(map[string]interface{}),
			"export_dir":                "",
			"mail_notification_enabled": false,
		}

		if conf.HTTPGlobal != nil {
			pref["web_ui_port"] = conf.HTTPGlobal.Port
		}

		if conf.Torrent != nil {
			// qbit uses enabled, distribyted uses disabled
			pref["ipv6_enabled"] = !conf.Torrent.DisableIPv6
		}

		c.JSON(http.StatusOK, pref)
	}
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

		gs := ss.GlobalStats()
		var dlSpeed int64
		var upSpeed int64
		if gs.TimePassed > 0 {
			dlSpeed = int64(float64(gs.DownloadedBytes) / gs.TimePassed)
			upSpeed = int64(float64(gs.UploadedBytes) / gs.TimePassed)
		}

		c.JSON(http.StatusOK, gin.H{
			"connection_status":    "connected",
			"dht_nodes":            0,
			"dl_info_data":         totalDownloadData,
			"dl_info_speed":        dlSpeed,
			"dl_rate_limit":        0,
			"up_info_data":         totalUploadData,
			"up_info_speed":        upSpeed,
			"up_rate_limit":        0,
			"refresh_interval":     2000,
			"queueing_enabled":     false,
			"use_alt_speed_limits": false,
		})
	}
}

var mockCreatedCategories = make(map[string]bool)

func qBitTorrentsCategoriesHandler(ch *config.Handler, ss *torrent.Stats, fusePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp := make(map[string]gin.H)

		// First, add all explicitly configured routes
		if ch != nil {
			if root, err := ch.Get(); err == nil && root != nil {
				for _, r := range root.Routes {
					savePath := fusePath
					if r.Name != "" {
						savePath = fusePath + "/" + r.Name
					}
					resp[r.Name] = gin.H{
						"name":     r.Name,
						"savePath": savePath,
					}
				}
			}
		}

		// Add dynamically mocked categories
		for cat := range mockCreatedCategories {
			savePath := fusePath
			if cat != "" {
				savePath = fusePath + "/" + cat
			}
			resp[cat] = gin.H{
				"name":     cat,
				"savePath": savePath,
			}
		}

		// Also add any routes that have active torrents
		routes := ss.RoutesStats()
		for _, r := range routes {
			if _, exists := resp[r.Name]; !exists {
				savePath := fusePath
				if r.Name != "" {
					savePath = fusePath + "/" + r.Name
				}
				resp[r.Name] = gin.H{
					"name":     r.Name,
					"savePath": savePath,
				}
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

func qBitTorrentsRemoveCategoriesHandler(c *gin.Context) {
	categories := c.PostForm("categories")
	catList := strings.FieldsFunc(categories, func(r rune) bool {
		return r == '\n' || r == '|'
	})
	if len(catList) == 0 && categories != "" {
		catList = []string{categories}
	}

	for _, cat := range catList {
		cat = strings.TrimSpace(cat)
		delete(mockCreatedCategories, cat)
	}
	c.String(http.StatusOK, "")
}

func qBitTorrentsMockHandler(c *gin.Context) {
	// A generic mock handler for endpoints like pause, resume, setCategory, addTags.
	c.String(http.StatusOK, "")
}

func qBitTorrentsInfoHandler(ss *torrent.Stats, fusePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		categoryFilter := c.Query("category")

		var torrents map[string]fs.Torrent
		if categoryFilter != "" && categoryFilter != "all" {
			torrents = ss.GetTorrentsInRoute(categoryFilter)
		} else {
			torrents = ss.GetAllTorrents()
		}

		resp := make([]qBitTorrent, 0)
		now := time.Now().Unix()

		for hash, t := range torrents {
			ts, _ := ss.Stats(hash)
			info := t.Info()
			name := t.Name()
			size := int64(0)
			progress := 0.0
			state := "stalledDL"

			// Determine category: if filtering by category, use that.
			// Otherwise, use the first route found (GetRouteFromHash).
			category := categoryFilter
			if category == "" || category == "all" {
				category = ss.GetRouteFromHash(hash)
			}

			var dlSpeed int64
			var upSpeed int64
			if ts.TimePassed > 0 {
				dlSpeed = int64(float64(ts.DownloadedBytes) / ts.TimePassed)
				upSpeed = int64(float64(ts.UploadedBytes) / ts.TimePassed)
			}

			completedPieces := 0
			for _, chunk := range ts.PieceChunks {
				if chunk.Status == torrent.Complete {
					completedPieces += chunk.NumPieces
				}
			}

			if ts.TotalPieces > 0 {
				progress = float64(completedPieces) / float64(ts.TotalPieces)
			}

			if info != nil {
				size = info.TotalLength()
				state = "uploading"
				if progress < 1.0 {
					state = "downloading"
				}
			}

			// Map distribyted torrent to qBit format
			savePath := fusePath
			if category != "" {
				savePath = fusePath + "/" + category
			}

			qbt := qBitTorrent{
				Hash:           hash,
				Name:           name,
				Size:           size,
				Progress:       progress,
				State:          state,
				SavePath:       savePath,
				ContentPath:    savePath + "/" + name,
				Category:       category,
				Tracker:        "",
				Tags:           "",
				AddedOn:        now,
				CompletionOn:   now,
				AmountLeft:     size - int64(progress*float64(size)),
				Completed:      int64(progress * float64(size)),
				TotalSize:      size,
				Ratio:          1.0,
				Eta:            0,
				Uploaded:       0,
				Downloaded:     int64(progress * float64(size)),
				Availability:   1.0,
				SequentialDl:   false,
				FirstLastPiece: false,
				LastActivity:   now,
				Dlspeed:        dlSpeed,
				Upspeed:        upSpeed,
				NumSeeds:       ts.Seeders,
				NumLeechs:      ts.Peers - ts.Seeders,
			}
			resp = append(resp, qbt)
		}

		c.JSON(http.StatusOK, resp)
	}
}

func qBitTorrentsAddHandler(s torrentService) gin.HandlerFunc {
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

func qBitTorrentsDeleteHandler(s torrentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		hashes := c.PostForm("hashes")
		// qBit sends hashes separated by |
		hashList := strings.Split(hashes, "|")

		for _, h := range hashList {
			h = strings.ToLower(strings.TrimSpace(h))
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
