package http

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/config"
)

// The Files page browses the combined tree every mount shows. Each folder has its own URL
// (/files?path=...), so back, forward and reload stay where they were. Forms open in one
// dialog, #file-dialog, which the server fills and then asks the page to open.

type filesPage struct {
	Title      string
	Path       string
	ListURL    string
	FolderForm string
	Crumbs     []crumb
	Entries    []fileView
	// Missing is set when the folder doesn't exist (any more).
	Missing bool
}

type crumb struct {
	Name, Href, ListURL string
	Current             bool
}

type fileView struct {
	ID               string
	Name, Path       string
	IsDir, Owned     bool
	Size             string
	Href             string // a folder's page
	ListURL, PushURL string // what htmx loads and shows in the address bar for a folder
	Download         string // a file's HTTPFS address, when HTTPFS is on
	RenameForm       string
	LinkForm         string
	DeleteURL        string
	DeleteQuestion   string
}

func filesHref(p string) string {
	if p == "/" {
		return "/files"
	}
	return "/files?path=" + url.QueryEscape(p)
}

func filesListURL(p string) string { return "/files/list?path=" + url.QueryEscape(p) }

func newFilesPage(cfs containerFS, conf *config.Root, p string) (filesPage, int) {
	p = path.Clean("/" + p)
	page := filesPage{Title: "Files", Path: p, ListURL: filesListURL(p), FolderForm: "/files/folder?path=" + url.QueryEscape(p)}
	page.Crumbs = []crumb{{Name: "All files", Href: "/files", ListURL: filesListURL("/")}}
	if p != "/" {
		acc := ""
		for _, seg := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
			acc += "/" + seg
			page.Crumbs = append(page.Crumbs, crumb{Name: seg, Href: filesHref(acc), ListURL: filesListURL(acc)})
		}
	}
	page.Crumbs[len(page.Crumbs)-1].Current = true

	if cfs == nil {
		page.Missing = true
		return page, http.StatusNotFound
	}
	entries, err := listDir(cfs, p)
	if err != nil {
		page.Missing = true
		return page, errStatus(err)
	}

	httpfs := conf != nil && conf.HTTPGlobal != nil && conf.HTTPGlobal.HTTPFS
	for _, e := range entries {
		q := url.QueryEscape(e.Path)
		v := fileView{ID: "file-" + q, Name: e.Name, Path: e.Path, IsDir: e.IsDir, Owned: e.Owned}
		if e.IsDir {
			v.Href = filesHref(e.Path)
			v.ListURL = filesListURL(e.Path)
		} else {
			v.Size = humanize.IBytes(uint64(max(e.Size, 0)))
			if httpfs {
				v.Download = (&url.URL{Path: "/fs" + e.Path}).EscapedPath()
			}
			v.LinkForm = "/links/form?source=" + q
		}
		if e.Owned {
			v.RenameForm = "/files/rename?path=" + q
			v.DeleteURL = "/files/entry?path=" + q
			if e.IsDir {
				v.DeleteQuestion = "Delete the folder “" + e.Name + "”? Only an empty folder can be deleted."
			} else {
				v.DeleteQuestion = "Delete “" + e.Name + "”? If nothing else points at its torrent, the torrent is removed too."
			}
		}
		page.Entries = append(page.Entries, v)
	}
	return page, http.StatusOK
}

var filesPageHandler = func(cfs containerFS, conf *config.Root) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, status := newFilesPage(cfs, conf, c.Query("path"))
		c.HTML(status, "files.html", page)
	}
}

var filesListHandler = func(cfs containerFS, conf *config.Root) gin.HandlerFunc {
	return func(c *gin.Context) {
		// a folder that's gone still renders, as a message, so the page can say so
		page, _ := newFilesPage(cfs, conf, c.Query("path"))
		c.HTML(http.StatusOK, "files-view", page)
	}
}

// nameForm is the dialog form for a new folder or a new name.
type nameForm struct {
	Title, Action, Submit string
	Path                  string // the folder to create in, or the entry to rename
	Name                  string
	Error                 string
}

func showNameForm(c *gin.Context, status int, f nameForm) {
	if status == http.StatusOK {
		triggerAfterSwap(c, map[string]any{"open-dialog": "file-dialog"})
	}
	c.HTML(status, "name-form", f)
}

var filesFolderFormHandler = func(c *gin.Context) {
	showNameForm(c, http.StatusOK, nameForm{
		Title: "New folder", Action: "/files/folder", Submit: "Create",
		Path: path.Clean("/" + c.Query("path")),
	})
}

var filesRenameFormHandler = func(c *gin.Context) {
	p := path.Clean("/" + c.Query("path"))
	showNameForm(c, http.StatusOK, nameForm{
		Title: "Rename", Action: "/files/rename", Submit: "Rename",
		Path: p, Name: path.Base(p),
	})
}

var filesFolderHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := nameForm{
			Title: "New folder", Action: "/files/folder", Submit: "Create",
			Path: path.Clean("/" + c.PostForm("path")), Name: strings.TrimSpace(c.PostForm("name")),
		}
		err := validName(f.Name)
		if err == nil {
			err = makeDir(cfs, path.Join(f.Path, f.Name))
		}
		if err != nil {
			f.Error = sentence(err)
			showNameForm(c, http.StatusUnprocessableEntity, f)
			return
		}
		fileChanged(c, "Folder “"+f.Name+"” created.")
	}
}

var filesRenameHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := nameForm{
			Title: "Rename", Action: "/files/rename", Submit: "Rename",
			Path: path.Clean("/" + c.PostForm("path")), Name: strings.TrimSpace(c.PostForm("name")),
		}
		err := validName(f.Name)
		if err == nil && f.Name == path.Base(f.Path) {
			c.Header("HX-Trigger", `{"close-dialog":"file-dialog"}`)
			c.Status(http.StatusOK)
			return
		}
		if err == nil {
			err = renameEntry(cfs, f.Path, path.Join(path.Dir(f.Path), f.Name))
		}
		if err != nil {
			f.Error = sentence(err)
			showNameForm(c, http.StatusUnprocessableEntity, f)
			return
		}
		fileChanged(c, "Renamed to “"+f.Name+"”.")
	}
}

var filesDeleteHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := removeEntry(cfs, c.Query("path")); err != nil {
			c.String(errStatus(err), sentence(err))
			return
		}
		triggerEvents(c, map[string]any{
			"files-changed": true,
			"toast":         toast{Level: "success", Message: "Deleted."},
		})
		c.Status(http.StatusOK)
	}
}

// fileChanged answers a dialog form that worked: close it, refresh the list, say so.
func fileChanged(c *gin.Context, msg string) {
	triggerEvents(c, map[string]any{
		"files-changed": true,
		"links-changed": true,
		"close-dialog":  "file-dialog",
		"toast":         toast{Level: "success", Message: msg},
	})
	c.Status(http.StatusOK)
}

// sentence turns an operation's error into something to show under a field.
func sentence(err error) string {
	msg := err.Error()
	if msg == "" {
		return "Something went wrong."
	}
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

// triggerAfterSwap has htmx fire these events once it has put the response in the page.
func triggerAfterSwap(c *gin.Context, events map[string]any) {
	if b, err := jsonString(events); err == nil {
		c.Header("HX-Trigger-After-Swap", b)
	}
}
