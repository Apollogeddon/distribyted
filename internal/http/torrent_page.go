package http

import (
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// A torrent's page lists its files, how ready each is to play, and the links that show
// them: what a library would lose with the torrent.

type torrentPage struct {
	Title   string
	Torrent torrentView
	// Missing is set when the route has no such torrent (any more).
	Missing bool
	BodyURL string
	Files   []torrentFileView
	// Linked counts the files something links to.
	Linked int
}

type torrentFileView struct {
	ID, Name, Path string
	Size           string
	FolderHref     string // the folder it's in, on the Files page
	// Readiness and ReadinessURL are as on the Files page; empty until the file is mounted.
	Readiness, ReadinessURL string
	// LinkedFrom are the paths of the links that show the file, or a file inside it.
	LinkedFrom []string
}

type torrentFileInfo struct {
	Path   string // in the torrent
	Length int64
}

// torrentFiles lists a torrent's files; a var so tests can stand in for a real torrent.
var torrentFiles = func(t dfs.Torrent) []torrentFileInfo {
	var out []torrentFileInfo
	for _, f := range t.Files() {
		out = append(out, torrentFileInfo{Path: f.Path(), Length: f.Length()})
	}
	return out
}

func torrentHref(route, hash string) string {
	return "/routes/" + url.PathEscape(route) + "/torrents/" + url.PathEscape(hash)
}

func newTorrentPage(ss *torrent.Stats, s torrentService, cfs containerFS, route, hash string) (torrentPage, int) {
	hash = strings.ToLower(hash)
	page := torrentPage{Title: "Torrent", BodyURL: torrentHref(route, hash) + "/body"}
	var t dfs.Torrent
	if ss != nil {
		t = ss.GetTorrentsInRoute(route)[hash]
	}
	if t == nil {
		page.Missing = true
		return page, http.StatusNotFound
	}
	if ts, err := ss.Stats(hash); err == nil {
		page.Torrent = newTorrentView(route, ts)
		page.Title = ts.Name
	} else {
		page.Torrent = torrentView{Name: t.Name(), Route: route, Hash: hash}
	}

	// every link's source, to find the links that show each file
	links := map[string][]string{}
	if s != nil {
		if records, err := s.ListLinks(); err == nil {
			for newpath, src := range records {
				if torrent.IsLinkSource(src) {
					links[src] = append(links[src], normalizeLinkPath(newpath))
				}
			}
		}
	}

	for i, f := range torrentFiles(t) {
		p := path.Join("/", route, f.Path)
		v := torrentFileView{
			ID:         "tfile-" + url.QueryEscape(p) + "-" + humanize.Comma(int64(i)),
			Name:       path.Base(f.Path),
			Path:       p,
			Size:       humanize.IBytes(uint64(max(f.Length, 0))),
			FolderHref: filesHref(path.Dir(p)),
		}
		if cfs != nil {
			if pf, ok := preparerAt(cfs, p); ok {
				if ready, ok := pf.StartReady(); ok {
					v.Readiness, v.ReadinessURL = "cold", "/files/readiness?path="+url.QueryEscape(p)
					if ready {
						v.Readiness = "ready"
					}
				}
			}
		}
		for src, from := range links {
			if src == p || strings.HasPrefix(src, p+"/") {
				v.LinkedFrom = append(v.LinkedFrom, from...)
			}
		}
		slices.Sort(v.LinkedFrom)
		if len(v.LinkedFrom) > 0 {
			page.Linked++
		}
		page.Files = append(page.Files, v)
	}
	slices.SortStableFunc(page.Files, func(a, b torrentFileView) int { return strings.Compare(a.Path, b.Path) })
	return page, http.StatusOK
}

var torrentPageHandler = func(ss *torrent.Stats, s torrentService, cfs containerFS) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, status := newTorrentPage(ss, s, cfs, c.Param("route"), c.Param("hash"))
		render(c, status, torrentDetailView(page))
	}
}

var torrentBodyHandler = func(ss *torrent.Stats, s torrentService, cfs containerFS) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := newTorrentPage(ss, s, cfs, c.Param("route"), c.Param("hash"))
		render(c, http.StatusOK, torrentDetailBody(page))
	}
}
