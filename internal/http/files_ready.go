package http

import (
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/config"
	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

// The readiness dialog shows how much of a torrent's file is in the piece cache, and has the
// client download its start and end, or all of it, before anyone presses play.

type readinessView struct {
	Path, Name, URL string
	// Missing is set when the file is gone, or isn't a torrent's.
	Missing    bool
	Ready      bool
	Start, End bool
	Size       string
	Cached     string
	Percent    int
	Peers      int
	Asked      string // what was asked for, in words, or ""
	Preparing  bool   // something was asked for that isn't all there yet
	// WholeTooBig says why the whole file can't be downloaded, when it can't.
	WholeTooBig string
}

// preparerAt is the torrent's file at p.
func preparerAt(cfs containerFS, p string) (dfs.Preparer, bool) {
	children, err := cfs.ReadDir(path.Dir(p))
	if err != nil {
		return nil, false
	}
	f, ok := children[path.Base(p)]
	if !ok || f.IsDir() {
		return nil, false
	}
	pf, ok := f.(dfs.Preparer)
	return pf, ok
}

// wholeFileLimit is the largest file downloaded whole: half the piece cache, so preparing one
// can't push every other film's start and end out of it.
func wholeFileLimit(conf *config.Root) int64 {
	if conf == nil || conf.Torrent == nil || conf.Torrent.GlobalCacheSize <= 0 {
		return -1
	}
	return conf.Torrent.GlobalCacheSize * 1024 * 1024 / 2
}

// wholeButtonClass leaves Whole file plain when it can't be pressed.
func wholeButtonClass(v readinessView) string {
	if v.WholeTooBig != "" {
		return "btn-secondary"
	}
	return "btn-primary"
}

func cachedWord(cached bool) string {
	if cached {
		return "Cached"
	}
	return "Not cached"
}

func newReadinessView(cfs containerFS, conf *config.Root, p string) readinessView {
	p = path.Clean("/" + p)
	v := readinessView{Path: p, Name: path.Base(p), URL: "/files/readiness?path=" + url.QueryEscape(p)}
	pf, ok := preparerAt(cfs, p)
	if !ok {
		v.Missing = true
		return v
	}
	r, ok := pf.Readiness()
	if !ok {
		v.Missing = true
		return v
	}
	v.Ready, v.Start, v.End, v.Peers = r.Ready(), r.Start, r.End, r.Peers
	v.Size = humanize.IBytes(uint64(max(r.Size, 0)))
	v.Cached = humanize.IBytes(uint64(max(r.Cached, 0)))
	if r.Size > 0 {
		v.Percent = int(r.Cached * 100 / r.Size)
	} else {
		v.Percent = 100
	}
	switch r.Asked {
	case dfs.PrepareStart:
		v.Asked, v.Preparing = "Start and end", !r.Ready()
	case dfs.PrepareAll:
		v.Asked, v.Preparing = "Whole file", r.Cached < r.Size
	case dfs.PrepareLead:
		// the start is pieces of 5%, so a little more is cached than asked for
		v.Asked, v.Preparing = "Next episode: first 5% and end", !r.End || r.Cached*100 < r.Size*5
	}
	if limit := wholeFileLimit(conf); limit >= 0 && r.Size > limit {
		v.WholeTooBig = fmt.Sprintf("At %s it's over half the %s piece cache, so it can't be kept whole.",
			v.Size, humanize.IBytes(uint64(conf.Torrent.GlobalCacheSize)*1024*1024))
	}
	return v
}

var filesReadinessHandler = func(cfs containerFS, conf *config.Root) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := newReadinessView(cfs, conf, c.Query("path"))
		triggerAfterSwap(c, map[string]any{"open-dialog": "file-dialog"})
		render(c, http.StatusOK, readinessDialog(v))
	}
}

var filesPrepareHandler = func(cfs containerFS, conf *config.Root) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := path.Clean("/" + c.PostForm("path"))
		pf, ok := preparerAt(cfs, p)
		if !ok {
			c.String(http.StatusNotFound, "Not a torrent's file: "+p)
			return
		}
		var (
			want dfs.Preparation
			msg  string
		)
		switch c.PostForm("what") {
		case "start":
			want, msg = dfs.PrepareStart, "Downloading the start and end of “"+path.Base(p)+"”."
		case "all":
			if v := newReadinessView(cfs, conf, p); v.WholeTooBig != "" {
				c.String(http.StatusUnprocessableEntity, v.WholeTooBig)
				return
			}
			want, msg = dfs.PrepareAll, "Downloading all of “"+path.Base(p)+"”."
		case "stop":
			want, msg = dfs.PrepareNone, "Stopped downloading “"+path.Base(p)+"” ahead."
		default:
			c.String(http.StatusBadRequest, "what must be start, all or stop")
			return
		}
		if !pf.Prepare(want) {
			c.String(http.StatusNotFound, "Not a torrent's file: "+p)
			return
		}
		triggerEvents(c, map[string]any{
			"files-changed": true,
			"toast":         toast{Level: "success", Message: msg},
		})
		render(c, http.StatusOK, readinessDialog(newReadinessView(cfs, conf, p)))
	}
}
