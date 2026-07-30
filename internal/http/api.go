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

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/gin-gonic/gin"
)

type torrentService interface {
	AddMagnet(r, m string) error
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
		p := path.Clean(ctx.Param("path"))

		children, err := cfs.ReadDir(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "no such directory: " + p})
				return
			}
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		out := make([]FSEntry, 0, len(children))
		for name, f := range children {
			childPath := path.Join(p, name)
			out = append(out, FSEntry{
				Name:  name,
				Path:  childPath,
				IsDir: f.IsDir(),
				Size:  f.Size(),
				Hash:  f.Hash(),
				Owned: cfs.IsOwned(childPath),
			})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].IsDir != out[j].IsDir {
				return out[i].IsDir
			}
			return out[i].Name < out[j].Name
		})

		ctx.JSON(http.StatusOK, out)
	}
}

var apiFsDeleteHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		p := path.Clean(ctx.Param("path"))
		if p == "" || p == "/" || p == "." {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
			return
		}

		err := cfs.Remove(p)
		switch {
		case err == nil:
			ctx.JSON(http.StatusOK, nil)
		case errors.Is(err, os.ErrNotExist):
			ctx.JSON(http.StatusNotFound, gin.H{"error": "no such path: " + p})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
	}
}

var apiFsMkdirHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var json MkdirRequest
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		p := path.Clean(json.Path)
		if p == "" || p == "/" || p == "." {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
			return
		}

		err := cfs.Mkdir(p)
		switch {
		case err == nil:
			ctx.JSON(http.StatusOK, nil)
		case errors.Is(err, os.ErrExist):
			ctx.JSON(http.StatusConflict, gin.H{"error": "path already exists: " + p})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
	}
}

var apiFsRenameHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var json RenameRequest
		if err := ctx.ShouldBindJSON(&json); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		oldPath := path.Clean(json.OldPath)
		newPath := path.Clean(json.NewPath)
		if oldPath == "" || oldPath == "/" || newPath == "" || newPath == "/" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "old_path and new_path are required"})
			return
		}

		if !cfs.IsOwned(oldPath) {
			ctx.JSON(http.StatusForbidden, gin.H{"error": "not renameable: part of a torrent's route content"})
			return
		}

		err := cfs.Rename(oldPath, newPath)
		switch {
		case err == nil:
			ctx.JSON(http.StatusOK, nil)
		case errors.Is(err, os.ErrNotExist):
			ctx.JSON(http.StatusNotFound, gin.H{"error": "source path does not exist: " + oldPath})
		case errors.Is(err, os.ErrExist):
			ctx.JSON(http.StatusConflict, gin.H{"error": "destination path already exists: " + newPath})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
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
		links, err := s.ListLinks()
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var routeNames []string
		if ss != nil { // nil only in tests that don't exercise route resolution
			routeNames = ss.RouteNames()
		}

		out := make([]Link, 0, len(links))
		for newPath, oldPath := range links {
			isDir := oldPath == "/" || oldPath == ""
			normOld := normalizeLinkPath(oldPath)
			route := ""
			if !isDir {
				route = routeForPath(normOld, routeNames)
			}
			out = append(out, Link{
				OldPath: normOld,
				NewPath: normalizeLinkPath(newPath),
				IsDir:   isDir,
				Route:   route,
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

		err = lfs.Remove(p)
		switch {
		case err == nil:
			ctx.JSON(http.StatusOK, nil)
		case errors.Is(err, os.ErrNotExist):
			// The DB record exists (found == true above) but the live tree
			// entry is already gone — an orphaned link, e.g. left behind by
			// a torrent deletion that cascaded before this fix shipped.
			// lfs.Remove can't clean up a record it can't find in the tree,
			// so reconcile the DB directly instead of leaving it stuck.
			if rmErr := s.RemoveLink(p); rmErr != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": rmErr.Error()})
				return
			}
			ctx.JSON(http.StatusOK, nil)
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
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
