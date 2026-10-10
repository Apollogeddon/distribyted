package http

import (
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
)

type torrentService interface {
	AddMagnet(r, m string) error
	AddTorrentMetaInfo(r string, mi *metainfo.MetaInfo) error
	RemoveFromHash(r, h string) error
	RemoveFromHashOnly(h string) error
	ListLinks() (map[string]string, error)
	// RemoveLink deletes a link's persisted DB record directly, bypassing
	// linkFs. Used only to reconcile a link whose ContainerFs entry is
	// already gone (see apiDelLinkHandler) — normal deletion still goes
	// through linkFs so the DB stays in sync with the live tree.
	RemoveLink(path string) error
}

// linkFs is the minimal surface apiAddLinkHandler/apiDelLinkHandler need.
// *fs.ContainerFs satisfies this directly: routing link mutations through it
// (not Service.AddLink/RemoveLink) keeps the live filesystem tree, the
// BoltDB record, and the last-reference torrent-teardown cascade all in
// sync, since that's where those callbacks are wired (see cmd/distribyted/main.go).
type linkFs interface {
	Link(oldpath, newpath string) error
	Remove(path string) error
}

// containerFS is the surface the /api/fs/* file-browser handlers need.
// *fs.ContainerFs satisfies it directly. It embeds linkFs rather than
// duplicating Remove so apiDelLinkHandler/apiAddLinkHandler keep their
// narrower dependency.
type containerFS interface {
	linkFs
	ReadDir(path string) (map[string]dfs.File, error)
	Rename(oldpath, newpath string) error
	Mkdir(path string) error
	IsOwned(path string) bool
}

var apiFsListHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		out, err := listDir(cfs, ctx.Param("path"))
		if err != nil {
			respondJSON(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, out)
	}
}

var apiFsDeleteHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		respondJSON(ctx, removeEntry(cfs, ctx.Param("path")))
	}
}

var apiFsMkdirHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var json MkdirRequest
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		respondJSON(ctx, makeDir(cfs, json.Path))
	}
}

var apiFsRenameHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var json RenameRequest
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		respondJSON(ctx, renameEntry(cfs, json.OldPath, json.NewPath))
	}
}

var apiStatusHandler = func(fc *filecache.Cache, ss *torrent.Stats) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		numItems := int64(0)
		filled := int64(0)
		capacity := int64(0)

		if fc != nil {
			info := fc.Info()
			numItems = int64(info.NumItems)
			filled = info.Filled / 1024 / 1024
			capacity = info.Capacity / 1024 / 1024
		}

		ctx.JSON(http.StatusOK, gin.H{
			"cacheItems":    numItems,
			"cacheFilled":   filled,
			"cacheCapacity": capacity,
			"torrentStats":  ss.GlobalStats(),
		})
	}
}

var apiServersHandler = func(ss []*torrent.Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		infos := make([]*torrent.ServerInfo, 0)
		for _, s := range ss {
			info := s.Info()
			infos = append(infos, &info)
		}
		ctx.JSON(http.StatusOK, infos)
	}
}

var apiRoutesHandler = func(ss *torrent.Stats) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		s := ss.RoutesStats()
		sort.Sort(torrent.ByName(s))
		ctx.JSON(http.StatusOK, s)
	}
}

var apiAddTorrentHandler = func(s torrentService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		if route == "" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "route is required"})
			return
		}

		var json RouteAdd
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := s.AddMagnet(route, json.Magnet); err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, nil)
	}
}

var apiDelTorrentHandler = func(s torrentService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		hash := strings.ToLower(ctx.Param("torrent_hash"))
		if route == "" || hash == "" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "route and hash are required"})
			return
		}

		if err := s.RemoveFromHash(route, hash); err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, nil)
	}
}

// normalizeLinkPath re-adds the leading "/" that loader.DB.ListLinks strips
// from its map keys when parsing its stored key prefix. This mirrors
// Service.cleanRoute's normalization so paths returned by this API always
// match what ContainerFs itself expects (and what was originally passed to
// AddLink), rather than the DB's internal storage-key encoding.
func normalizeLinkPath(p string) string {
	return path.Clean("/" + p)
}

// routeForPath resolves which route a link's oldPath falls under, by
// longest-prefix match against routeNames — routes mount at
// path.Join("/", name), so any path under that mount belongs to it. Calling
// RoutesStats() to answer this would be too expensive here: it recomputes
// piece-state runs for every torrent, and the Links page polls every 2s.
// Returns "" if no route matches (e.g. a link pointing at another link).
func routeForPath(p string, routeNames []string) string {
	best := ""
	bestPrefixLen := -1
	for _, name := range routeNames {
		prefix := path.Join("/", name)
		if p != prefix && !strings.HasPrefix(p, prefix+"/") {
			continue
		}
		if len(prefix) > bestPrefixLen {
			bestPrefixLen = len(prefix)
			best = name
		}
	}
	return best
}

var apiListLinksHandler = func(s torrentService, ss *torrent.Stats) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		out, err := listLinks(s, ss)
		if err != nil {
			respondJSON(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, out)
	}
}

var apiAddLinkHandler = func(lfs linkFs) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var json LinkAdd
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		respondJSON(ctx, addLink(lfs, json.OldPath, json.NewPath))
	}
}

var apiDelLinkHandler = func(lfs linkFs, s torrentService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		respondJSON(ctx, removeLink(lfs, s, ctx.Param("path")))
	}
}

var apiLogHandler = func(path string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		f, err := os.Open(path)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer f.Close()

		fi, err := f.Stat()
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		max := math.Max(float64(-fi.Size()), -1024*8*8)
		_, err = f.Seek(int64(max), io.SeekEnd)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		_, err = io.Copy(ctx.Writer, f)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
}
