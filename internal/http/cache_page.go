package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The Cache page shows what the piece cache (global_cache_size) holds, torrent by torrent,
// and frees one torrent's share, so a film that played yesterday and buffers today can be
// explained: its pieces were read longest ago, and evicted.

type cachePage struct {
	Title          string
	Used, Capacity string
	Percent        int
	HasCache       bool
	// Protected says how much of the cache holds files' starts, when they're kept apart
	Protected string
	Torrents  []cachedView
	Other     string // pieces of torrents no longer loaded, or ""
	Downloads downloadsView
	// FirstByte says how long the latest opens waited for their first byte, or ""
	FirstByte string
}

// downloadsView is what was downloaded, by why.
type downloadsView struct {
	Since string
	Rows  []downloadRow
	// Day and Total are every reason's together
	Day, Total string
}

type downloadRow struct {
	Reason, Day, Total string
	// Again is how much of the last day's was downloaded before, and evicted, or ""
	Again string
}

type cachedView struct {
	ID, Name, Href string
	Size           string
	Pieces         string
	Percent        int // of the cache's capacity
	LastRead       string
	FreeURL        string
	FreeQuestion   string
}

func (v cachedView) Width() string { return fmt.Sprintf("width: %d%%", max(v.Percent, 1)) }

func (p cachePage) Width() string { return fmt.Sprintf("width: %d%%", p.Percent) }

func newCachePage(fc *torrent.PieceCache, ss *torrent.Stats, now time.Time) cachePage {
	page := cachePage{Title: "Cache", Downloads: newDownloadsView(dfs.Fetched(), now), FirstByte: firstByteNote(dfs.FirstBytes())}
	if fc == nil {
		return page
	}
	info := fc.Info()
	page.HasCache = info.Capacity > 0
	page.Used = humanize.IBytes(uint64(max(info.Filled, 0)))
	page.Capacity = humanize.IBytes(uint64(max(info.Capacity, 0)))
	if info.Capacity > 0 {
		page.Percent = int(min(info.Filled*100/info.Capacity, 100))
	}

	if pi, ok := fc.ProtectedInfo(); ok {
		page.Protected = humanize.IBytes(uint64(max(pi.Filled, 0))) + " of " + humanize.IBytes(uint64(max(pi.Capacity, 0))) +
			" is kept for files' first and last megabytes, so they start at once"
	}

	var u torrent.CacheUsage
	if ss != nil {
		u = torrent.GetCacheUsage(fc, ss.GetAllTorrents())
	} else {
		u = torrent.GetCacheUsage(fc, nil)
	}
	for _, c := range u.Torrents {
		v := cachedView{
			ID:           "cached-" + c.Hash,
			Name:         c.Name,
			Size:         humanize.IBytes(uint64(c.Bytes)),
			Pieces:       countOf(c.Pieces, "piece"),
			FreeURL:      "/cache/torrents/" + c.Hash,
			FreeQuestion: "Free the " + humanize.IBytes(uint64(c.Bytes)) + " “" + c.Name + "” has in the cache? Its pieces download again when it's next played.",
		}
		if info.Capacity > 0 {
			v.Percent = int(min(c.Bytes*100/info.Capacity, 100))
		}
		if !c.LastRead.IsZero() {
			v.LastRead = "read " + humanize.RelTime(c.LastRead, now, "ago", "from now")
		}
		if ss != nil {
			if r := ss.GetRouteFromHash(c.Hash); r != "" {
				v.Href = torrentHref(r, c.Hash)
			}
		}
		page.Torrents = append(page.Torrents, v)
	}
	if u.OtherPieces > 0 {
		page.Other = humanize.IBytes(uint64(u.OtherBytes)) + " in " + countOf(u.OtherPieces, "piece") +
			" of torrents no longer loaded, which go first as space is needed."
	}
	return page
}

func newDownloadsView(f dfs.FetchStats, now time.Time) downloadsView {
	v := downloadsView{Since: humanize.RelTime(f.Since, now, "ago", "from now")}
	var day, total int64
	for _, c := range f.Reasons {
		row := downloadRow{Reason: c.Reason.String(), Day: humanize.IBytes(uint64(c.Day)), Total: humanize.IBytes(uint64(c.Total))}
		if c.DayAgain > 0 {
			row.Again = humanize.IBytes(uint64(c.DayAgain)) + " of it again, after it was evicted"
		}
		day, total = day+c.Day, total+c.Total
		v.Rows = append(v.Rows, row)
	}
	v.Day, v.Total = humanize.IBytes(uint64(day)), humanize.IBytes(uint64(total))
	return v
}

func firstByteNote(s dfs.FirstByteStats) string {
	if s.Count == 0 {
		return ""
	}
	if s.Count == 1 {
		return fmt.Sprintf("The latest file opened had its first byte within %s.", roundDuration(s.Median))
	}
	return fmt.Sprintf("Of the latest %d files opened, half had their first byte within %s, and all but one in twenty within %s.",
		s.Count, roundDuration(s.Median), roundDuration(s.P95))
}

func roundDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

var cachePageHandler = func(fc *torrent.PieceCache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, cacheView(newCachePage(fc, ss, time.Now())))
	}
}

var cacheListHandler = func(fc *torrent.PieceCache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, cacheList(newCachePage(fc, ss, time.Now())))
	}
}

var cacheFreeHandler = func(fc *torrent.PieceCache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		hash := strings.ToLower(c.Param("hash"))
		var t dfs.Torrent
		if ss != nil {
			t = ss.GetAllTorrents()[hash]
		}
		if t == nil {
			c.String(http.StatusNotFound, "That torrent isn't loaded any more.")
			return
		}
		n := torrent.EvictFromCache(fc, t)
		triggerEvents(c, map[string]any{
			"cache-changed": true,
			"toast":         toast{Level: "success", Message: "Freed " + countOf(n, "piece") + " of “" + t.Name() + "”."},
		})
		c.Status(http.StatusOK)
	}
}
