package http

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The Links page lists the links made through the file browser, the API or a *arr app: a
// path that shows another path's file, such as a film filed under a library's naming.

type linksPage struct {
	Title string
	Links []linkView
	Error string
	Form  linkForm
	// Query and Show narrow the list (show "attention" keeps the links that need it);
	// ListURL keeps them when it refreshes.
	Query, Show string
	ListURL     string
	// Total counts every link; Waiting and Missing the ones that need attention.
	Total, Waiting, Missing int
}

// Filtered reports whether the list is narrowed.
func (p linksPage) Filtered() bool { return p.Query != "" || p.Show == "attention" }

// The states a link's file can be in.
const (
	linkOK      = ""
	linkWaiting = "waiting"
	linkMissing = "missing"
)

type linkView struct {
	Link
	ID             string
	SourceHref     string
	DeleteURL      string
	DeleteQuestion string
	// State is linkWaiting while the link waits for its torrent after a start, or linkMissing
	// when it isn't in the mount and isn't waiting for anything.
	State string
}

type linkForm struct {
	Source, Target string
	Error          string
	// InDialog is set when the form opens from the Files page, in its dialog.
	InDialog bool
}

// newLinksPageFor lists the links whose paths hold every word of query, ignoring case, and
// with show "attention", only those waiting for their torrent or missing from cfs.
func newLinksPageFor(s torrentService, ss *torrent.Stats, cfs containerFS, waiting func() []string, query, show string) linksPage {
	query = strings.TrimSpace(query)
	if show != "attention" {
		show = "all"
	}
	page := linksPage{Title: "Links", Query: query, Show: show}
	page.ListURL = "/links/list?" + url.Values{"q": {query}, "show": {show}}.Encode()
	if s == nil {
		return page
	}
	links, err := listLinks(s, ss)
	if err != nil {
		page.Error = sentence(err)
		return page
	}
	isWaiting := map[string]bool{}
	if waiting != nil {
		for _, p := range waiting() {
			isWaiting[path.Clean("/"+p)] = true
		}
	}
	exists := existsIn(cfs)
	words := strings.Fields(strings.ToLower(query))
	for _, l := range links {
		page.Total++
		v := linkView{
			Link:           l,
			ID:             "link-" + url.QueryEscape(l.NewPath),
			DeleteURL:      "/links/entry?path=" + url.QueryEscape(l.NewPath),
			DeleteQuestion: "Delete the link “" + l.NewPath + "”? If nothing else points at its torrent, the torrent is removed too.",
		}
		if !l.IsDir && !l.IsFile {
			v.SourceHref = filesHref(path.Dir(l.OldPath))
			switch {
			case isWaiting[l.NewPath]:
				v.State = linkWaiting
				page.Waiting++
			case cfs != nil && !exists(l.NewPath):
				v.State = linkMissing
				page.Missing++
			}
		}
		if show == "attention" && v.State == linkOK || !matchesWords(l.NewPath+" "+l.OldPath, words) {
			continue
		}
		page.Links = append(page.Links, v)
	}
	return page
}

// existsIn reports whether a path is in cfs, reading each folder once.
func existsIn(cfs containerFS) func(string) bool {
	dirs := map[string]map[string]dfs.File{}
	return func(p string) bool {
		if cfs == nil {
			return true
		}
		dir := path.Dir(p)
		children, ok := dirs[dir]
		if !ok {
			children, _ = cfs.ReadDir(dir)
			dirs[dir] = children
		}
		_, ok = children[path.Base(p)]
		return ok
	}
}

func linksPageFrom(c *gin.Context, s torrentService, ss *torrent.Stats, cfs containerFS, waiting func() []string) linksPage {
	return newLinksPageFor(s, ss, cfs, waiting, c.Query("q"), c.Query("show"))
}

var linksPageHandler = func(s torrentService, ss *torrent.Stats, cfs containerFS, waiting func() []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, linksView(linksPageFrom(c, s, ss, cfs, waiting)))
	}
}

// linksListHandler renders the list the page refreshes. The search form asks with push set,
// and gets the page's own address back for the address bar.
var linksListHandler = func(s torrentService, ss *torrent.Stats, cfs containerFS, waiting func() []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := linksPageFrom(c, s, ss, cfs, waiting)
		if c.Query("push") != "" {
			c.Header("HX-Push-Url", "/links?"+url.Values{"q": {page.Query}, "show": {page.Show}}.Encode())
		}
		render(c, http.StatusOK, linksList(page))
	}
}

// linksFormHandler fills the Files page's dialog with a form to link the given file.
var linksFormHandler = func(c *gin.Context) {
	src := path.Clean("/" + c.Query("source"))
	triggerAfterSwap(c, map[string]any{"open-dialog": "file-dialog"})
	render(c, http.StatusOK, linkFormView(linkForm{Source: src, Target: "/" + path.Base(src), InDialog: true}))
}

var linksAddHandler = func(lfs linkFs) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := linkForm{
			Source:   strings.TrimSpace(c.PostForm("source")),
			Target:   strings.TrimSpace(c.PostForm("target")),
			InDialog: c.PostForm("in_dialog") == "true",
		}
		var err error
		switch {
		case !strings.HasPrefix(f.Source, "/") || !strings.HasPrefix(f.Target, "/"):
			err = refuse(http.StatusBadRequest, "Both paths start with /, from the top of the mount.")
		case path.Clean(f.Target) == "/":
			err = refuse(http.StatusBadRequest, "Choose where the link goes.")
		default:
			err = addLink(lfs, path.Clean(f.Source), path.Clean(f.Target))
		}
		if err != nil {
			f.Error = sentence(err)
			render(c, http.StatusUnprocessableEntity, linkFormView(f))
			return
		}

		dialog := "add-link"
		if f.InDialog {
			dialog = "file-dialog"
		}
		triggerEvents(c, map[string]any{
			"links-changed": true,
			"files-changed": true,
			"close-dialog":  dialog,
			"toast":         toast{Level: "success", Message: "Linked " + path.Clean(f.Target) + "."},
		})
		render(c, http.StatusOK, linkFormView(linkForm{InDialog: f.InDialog}))
	}
}

var linksDeleteHandler = func(lfs linkFs, s torrentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := removeLink(lfs, s, c.Query("path")); err != nil {
			c.String(errStatus(err), sentence(err))
			return
		}
		triggerEvents(c, map[string]any{
			"links-changed": true,
			"toast":         toast{Level: "success", Message: "Link deleted."},
		})
		c.Status(http.StatusOK)
	}
}

// linkAttention says how many links need attention, as "2 links waiting for their torrents ·
// 1 missing".
func linkAttention(p linksPage) string {
	var parts []string
	if p.Waiting > 0 {
		parts = append(parts, countOf(p.Waiting, "link")+" waiting for their torrents")
	}
	if p.Missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing from the mount", p.Missing))
	}
	return strings.Join(parts, " · ") + "."
}
