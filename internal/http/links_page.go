package http

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The Links page lists the links made through the file browser, the API or a *arr app: a
// path that shows another path's file, such as a film filed under a library's naming.

type linksPage struct {
	Title string
	Links []linkView
	Error string
	Form  linkForm
}

type linkView struct {
	Link
	ID             string
	SourceHref     string
	DeleteURL      string
	DeleteQuestion string
}

type linkForm struct {
	Source, Target string
	Error          string
	// InDialog is set when the form opens from the Files page, in its dialog.
	InDialog bool
}

func newLinksPage(s torrentService, ss *torrent.Stats) linksPage {
	page := linksPage{Title: "Links"}
	if s == nil {
		return page
	}
	links, err := listLinks(s, ss)
	if err != nil {
		page.Error = sentence(err)
		return page
	}
	for _, l := range links {
		v := linkView{
			Link:           l,
			ID:             "link-" + url.QueryEscape(l.NewPath),
			DeleteURL:      "/links/entry?path=" + url.QueryEscape(l.NewPath),
			DeleteQuestion: "Delete the link “" + l.NewPath + "”? If nothing else points at its torrent, the torrent is removed too.",
		}
		if !l.IsDir {
			v.SourceHref = filesHref(path.Dir(l.OldPath))
		}
		page.Links = append(page.Links, v)
	}
	return page
}

var linksPageHandler = func(s torrentService, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, linksView(newLinksPage(s, ss)))
	}
}

var linksListHandler = func(s torrentService, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, linksList(newLinksPage(s, ss)))
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
