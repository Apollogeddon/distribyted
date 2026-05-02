package http

import (
	"io"
	"math"
	"net/http"
	"os"
	"sort"

	"github.com/Apollogeddon/distribyted/torrent"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/gin-gonic/gin"
)

type torrentService interface {
	AddMagnet(r, m string) error
	RemoveFromHash(r, h string) error
	RemoveFromHashOnly(h string) error
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
			infos = append(infos, s.Info())
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

		var json RouteAdd
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := s.AddMagnet(route, json.Magnet); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, nil)
	}
}

var apiDelTorrentHandler = func(s torrentService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route := ctx.Param("route")
		hash := ctx.Param("torrent_hash")

		if err := s.RemoveFromHash(route, hash); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, nil)
	}
}

var apiLogHandler = func(path string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		f, err := os.Open(path)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fi, err := f.Stat()
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		max := math.Max(float64(-fi.Size()), -1024*8*8)
		_, err = f.Seek(int64(max), io.SeekEnd)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		_, err = io.Copy(ctx.Writer, f)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := f.Close(); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
}
