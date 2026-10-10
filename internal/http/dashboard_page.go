package http

import (
	"fmt"
	"net/http"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The dashboard's figures are rendered on the server and refreshed every two seconds;
// the speed chart takes each refresh's rates from the figures' data attributes.

type dashboardPage struct {
	Title string
	Stats dashboardStats
}

type dashboardStats struct {
	// DownRate and UpRate are bytes per second, for the chart.
	DownRate, UpRate int64
	Down, Up         string
	CacheUsed        string
	CacheCapacity    string
	CachePercent     int
	CacheItems       string
	HasCache         bool
}

func newDashboardStats(fc *filecache.Cache, ss *torrent.Stats) dashboardStats {
	var st dashboardStats
	if ss != nil {
		g := ss.GlobalStats()
		if g.TimePassed > 0 {
			st.DownRate = int64(float64(g.DownloadedBytes) / g.TimePassed)
			st.UpRate = int64(float64(g.UploadedBytes) / g.TimePassed)
		}
	}
	st.Down = rate(st.DownRate, 1)
	st.Up = rate(st.UpRate, 1)

	if fc != nil {
		info := fc.Info()
		st.HasCache = info.Capacity > 0
		st.CacheUsed = humanize.IBytes(uint64(max(info.Filled, 0)))
		st.CacheCapacity = humanize.IBytes(uint64(max(info.Capacity, 0)))
		if info.Capacity > 0 {
			st.CachePercent = int(min(info.Filled*100/info.Capacity, 100))
		}
		st.CacheItems = humanize.Comma(int64(info.NumItems))
	}
	return st
}

func (s dashboardStats) CacheWidth() string { return fmt.Sprintf("%d%%", s.CachePercent) }

var dashboardHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", dashboardPage{Title: "Dashboard", Stats: newDashboardStats(fc, ss)})
	}
}

var dashboardStatsHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "dashboard-stats", newDashboardStats(fc, ss))
	}
}
