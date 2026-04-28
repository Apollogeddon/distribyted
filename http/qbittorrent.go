package http

import (
	"fmt"
	"net/http"
	"strings"

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
