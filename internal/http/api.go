package http

import (
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/gin-gonic/gin"
)

type torrentService interface {
	AddMagnet(r, m string) error
	RemoveFromHash(r, h string) error
	RemoveFromHashOnly(h string) error
	ListLinks() (map[string]string, error)
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

var apiListLinksHandler = func(s torrentService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		links, err := s.ListLinks()
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		out := make([]Link, 0, len(links))
		for newPath, oldPath := range links {
			out = append(out, Link{
				OldPath: normalizeLinkPath(oldPath),
				NewPath: normalizeLinkPath(newPath),
				IsDir:   oldPath == "/" || oldPath == "",
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].NewPath < out[j].NewPath })

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

		err := lfs.Link(json.OldPath, json.NewPath)
		switch {
		case err == nil:
			ctx.JSON(http.StatusOK, nil)
		case errors.Is(err, os.ErrNotExist):
			ctx.JSON(http.StatusNotFound, gin.H{"error": "source path does not exist: " + json.OldPath})
		case errors.Is(err, os.ErrExist):
			ctx.JSON(http.StatusConflict, gin.H{"error": "destination path already exists: " + json.NewPath})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
	}
}

var apiDelLinkHandler = func(lfs linkFs, s torrentService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		p := path.Clean(ctx.Param("path"))
		if p == "" || p == "/" || p == "." {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
			return
		}

		links, err := s.ListLinks()
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		found := false
		for newPath := range links {
			if normalizeLinkPath(newPath) == p {
				found = true
				break
			}
		}
		if !found {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "no link at path: " + p})
			return
		}

		if err := lfs.Remove(p); err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, nil)
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
