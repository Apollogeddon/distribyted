package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
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
	Torrents       []cachedView
	Other          string // pieces of torrents no longer loaded, or ""
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

func newCachePage(fc *filecache.Cache, ss *torrent.Stats, now time.Time) cachePage {
	page := cachePage{Title: "Cache"}
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

var cachePageHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, cacheView(newCachePage(fc, ss, time.Now())))
	}
}

var cacheListHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, cacheList(newCachePage(fc, ss, time.Now())))
	}
}

var cacheFreeHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
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
