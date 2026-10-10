package http

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The dashboard's figures and speed chart are rendered on the server and refreshed every two
// seconds. The chart is an SVG drawn from the last minute of speeds, which distribyted keeps
// while someone is looking at it.

const (
	historyLen      = 30
	historyInterval = 2 * time.Second
	// chart coordinates: the SVG stretches to its box, and lines keep their width
	chartW, chartH = 300.0, 100.0
)

type dashboardStats struct {
	Down, Up      string
	CacheUsed     string
	CacheCapacity string
	CachePercent  int
	CacheItems    string
	HasCache      bool
	Chart         chartView
}

type speedSample struct {
	at       time.Time
	down, up int64
}

// speedHistory is the last minute of overall speeds, a sample per refresh. Several open
// dashboards share it: a refresh sooner than the interval adds nothing.
type speedHistory struct {
	mu      sync.Mutex
	samples []speedSample
}

func (h *speedHistory) add(s speedSample) []speedSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := len(h.samples); n > 0 && s.at.Sub(h.samples[n-1].at) < historyInterval*3/4 {
		h.samples[n-1].down, h.samples[n-1].up = s.down, s.up
	} else {
		h.samples = append(h.samples, s)
	}
	// a gap means nobody was watching; the old line would join across it
	if n := len(h.samples); n > 1 && s.at.Sub(h.samples[n-2].at) > historyInterval*historyLen {
		h.samples = h.samples[n-1:]
	}
	if len(h.samples) > historyLen {
		h.samples = h.samples[len(h.samples)-historyLen:]
	}
	return append([]speedSample(nil), h.samples...)
}

type chartView struct {
	DownLine, UpLine, DownArea string
	// Ticks label the gridlines, from the top.
	Ticks []chartTick
	Label string
}

type chartTick struct {
	Y     float64 // 0–100, from the top
	Label string
}

func (t chartTick) Top() string { return fmt.Sprintf("%.2f%%", t.Y) }

func newChartView(samples []speedSample) chartView {
	var peak int64
	for _, s := range samples {
		peak = max(peak, s.down, s.up)
	}
	top := niceRate(peak)
	cv := chartView{Ticks: []chartTick{
		{Y: 0, Label: rate(top, 1)},
		{Y: 50, Label: rate(top/2, 1)},
		{Y: 100, Label: "0 B/s"},
	}}
	if len(samples) < 2 {
		return cv
	}

	step := chartW / float64(historyLen-1)
	x0 := chartW - float64(len(samples)-1)*step
	point := func(i int, v int64) string {
		y := chartH - float64(v)/float64(top)*chartH
		return fmt.Sprintf("%.1f,%.1f", x0+float64(i)*step, y)
	}
	var down, up []string
	for i, s := range samples {
		down = append(down, point(i, s.down))
		up = append(up, point(i, s.up))
	}
	cv.DownLine = "M" + strings.Join(down, " L")
	cv.UpLine = "M" + strings.Join(up, " L")
	cv.DownArea = fmt.Sprintf("%s L%.1f,%.1f L%.1f,%.1f Z", cv.DownLine, chartW, chartH, x0, chartH)
	last := samples[len(samples)-1]
	cv.Label = fmt.Sprintf("Speed over the last minute. Now %s down and %s up.", rate(last.down, 1), rate(last.up, 1))
	return cv
}

// niceRate is the round figure at the top of the chart: 1, 2 or 5 times a power of ten of
// a binary unit, at least 1 KiB/s.
func niceRate(peak int64) int64 {
	unit := int64(1)
	for peak >= unit*1024 {
		unit *= 1024
	}
	for _, m := range []int64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1024} {
		if peak <= m*unit {
			return max(m*unit, 1024)
		}
	}
	return max(peak, 1024)
}

func newDashboardStats(fc *filecache.Cache, ss *torrent.Stats, h *speedHistory) dashboardStats {
	s := speedSample{at: time.Now()}
	if ss != nil {
		g := ss.GlobalStats()
		if g.TimePassed > 0 {
			s.down = int64(float64(g.DownloadedBytes) / g.TimePassed)
			s.up = int64(float64(g.UploadedBytes) / g.TimePassed)
		}
	}
	st := dashboardStats{Down: rate(s.down, 1), Up: rate(s.up, 1)}
	if h != nil {
		st.Chart = newChartView(h.add(s))
	}

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

func (s dashboardStats) CacheWidth() string { return fmt.Sprintf("width: %d%%", s.CachePercent) }

var dashboardHandler = func(fc *filecache.Cache, ss *torrent.Stats, h *speedHistory) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, dashboardView(newDashboardStats(fc, ss, h)))
	}
}

var dashboardStatsHandler = func(fc *filecache.Cache, ss *torrent.Stats, h *speedHistory) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, dashboardStatsView(newDashboardStats(fc, ss, h)))
	}
}
