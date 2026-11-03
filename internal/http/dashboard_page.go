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
	// Verdict sums up the streams in a sentence; Problem, when there is one, names the first
	// stream in trouble.
	Verdict, Problem string
	Down, Up         string
	// DownSpark and UpSpark are the last minute of speeds as sparkline points.
	DownSpark, UpSpark string
	Streams, Waiting   int
	// Torrents counts every torrent; ShortOfPeers those settled with fewer than two seeders.
	Torrents, ShortOfPeers int
	// Links counts the links to files; LinksWaiting and LinksMissing those that need a look.
	Links, LinksWaiting, LinksMissing int
	CacheUsed                         string
	CacheCapacity                     string
	CachePercent                      int
	HasCache                          bool
	Chart                             chartView
	Playing                           []playingView
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

func newDashboardStats(fc *filecache.Cache, ss *torrent.Stats, h *speedHistory, svc torrentService, cfs containerFS, waiting func() []string) dashboardStats {
	s := speedSample{at: time.Now()}
	if ss != nil {
		g := ss.GlobalStats()
		if g.TimePassed > 0 {
			s.down = int64(float64(g.DownloadedBytes) / g.TimePassed)
			s.up = int64(float64(g.UploadedBytes) / g.TimePassed)
		}
	}
	st := dashboardStats{Down: rate(s.down, 1), Up: rate(s.up, 1), Playing: newPlayingViews(ss, svc, s.at)}
	if h != nil {
		samples := h.add(s)
		st.Chart = newChartView(samples)
		st.DownSpark, st.UpSpark = sparkline(samples, func(x speedSample) int64 { return x.down }), sparkline(samples, func(x speedSample) int64 { return x.up })
	}

	st.Streams = len(st.Playing)
	for _, p := range st.Playing {
		if p.Status.Level == "warn" {
			st.Waiting++
			if st.Problem == "" {
				st.Problem = p.Title + map[string]string{"Waiting for data": " is waiting for data.", "No peers": " has no peers left."}[p.Status.Label]
			}
		}
	}
	st.Verdict = verdict(st.Streams)

	if ss != nil {
		for _, ts := range ss.Brief() {
			st.Torrents++
			settled := time.Duration(ts.AgeSeconds*float64(time.Second)) >= seederGrace
			if settled && ts.Seeders < 2 {
				st.ShortOfPeers++
			}
		}
	}
	if svc != nil {
		lp := newLinksPageFor(svc, ss, cfs, waiting, "", "")
		for _, l := range lp.Links {
			if !l.IsDir && !l.IsFile {
				st.Links++
			}
		}
		st.LinksWaiting, st.LinksMissing = lp.Waiting, lp.Missing
	}

	if fc != nil {
		info := fc.Info()
		st.HasCache = info.Capacity > 0
		st.CacheUsed = humanize.IBytes(uint64(max(info.Filled, 0)))
		st.CacheCapacity = humanize.IBytes(uint64(max(info.Capacity, 0)))
		if info.Capacity > 0 {
			st.CachePercent = int(min(info.Filled*100/info.Capacity, 100))
		}
	}
	return st
}

var numberWords = []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine"}

// verdict is the sentence the dashboard opens with.
func verdict(streams int) string {
	switch {
	case streams == 0:
		return "Nothing is playing."
	case streams < len(numberWords):
		return numberWords[streams] + " stream" + plural(streams) + " playing."
	}
	return fmt.Sprintf("%d streams playing.", streams)
}

// sparkline is the samples' values as polyline points on a 100 by 22 box, scaled to their
// own peak.
func sparkline(samples []speedSample, value func(speedSample) int64) string {
	if len(samples) < 2 {
		return ""
	}
	var peak int64 = 1
	for _, x := range samples {
		peak = max(peak, value(x))
	}
	pts := make([]string, len(samples))
	for i, x := range samples {
		pts[i] = fmt.Sprintf("%.1f,%.1f", float64(i)*100/float64(len(samples)-1), 20-float64(value(x))*18/float64(peak))
	}
	return strings.Join(pts, " ")
}

// LinksNote says which links need a look, or that none do.
func (s dashboardStats) LinksNote() string {
	switch {
	case s.LinksWaiting > 0 && s.LinksMissing > 0:
		return fmt.Sprintf("%d waiting, %d missing", s.LinksWaiting, s.LinksMissing)
	case s.LinksWaiting > 0:
		return fmt.Sprintf("%d waiting", s.LinksWaiting)
	case s.LinksMissing > 0:
		return fmt.Sprintf("%d missing", s.LinksMissing)
	}
	return "All in place"
}

func (s dashboardStats) CacheWidth() string { return fmt.Sprintf("width: %d%%", s.CachePercent) }

var dashboardHandler = func(fc *filecache.Cache, ss *torrent.Stats, h *speedHistory, s torrentService, cfs containerFS, waiting func() []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, dashboardView(newDashboardStats(fc, ss, h, s, cfs, waiting)))
	}
}

var dashboardStatsHandler = func(fc *filecache.Cache, ss *torrent.Stats, h *speedHistory, s torrentService, cfs containerFS, waiting func() []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, dashboardStatsView(newDashboardStats(fc, ss, h, s, cfs, waiting)))
	}
}
