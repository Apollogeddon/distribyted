package http

import (
	"errors"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The file and link operations behind both the JSON API and the Files and Links pages, so
// the two apply the same rules and report the same problems.

// opError is an operation that was refused or failed: the status the JSON API answers with,
// and a message for the person who asked.
type opError struct {
	status int
	msg    string
}

func (e *opError) Error() string { return e.msg }

func refuse(status int, msg string) error { return &opError{status: status, msg: msg} }

// errStatus is the HTTP status for an error from one of the operations below.
func errStatus(err error) int {
	var oe *opError
	if errors.As(err, &oe) {
		return oe.status
	}
	return http.StatusInternalServerError
}

// respondJSON answers an API call with null, or with {"error": ...} and the error's status.
func respondJSON(ctx *gin.Context, err error) {
	if err != nil {
		ctx.JSON(errStatus(err), gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, nil)
}

func isRoot(p string) bool { return p == "" || p == "/" || p == "." }

// validName checks a single file or folder name typed into the page.
func validName(name string) error {
	switch {
	case name == "" || name == "." || name == "..":
		return refuse(http.StatusBadRequest, "Enter a name.")
	case strings.ContainsAny(name, "/\\"):
		return refuse(http.StatusBadRequest, "A name can't contain / or \\.")
	}
	return nil
}

func listDir(cfs containerFS, p string) ([]FSEntry, error) {
	p = path.Clean("/" + p)
	children, err := cfs.ReadDir(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, refuse(http.StatusNotFound, "no such directory: "+p)
		}
		return nil, err
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
	return out, nil
}

func removeEntry(cfs containerFS, p string) error {
	p = path.Clean(p)
	if isRoot(p) {
		return refuse(http.StatusBadRequest, "path is required")
	}
	if !cfs.IsOwned(p) {
		return refuse(http.StatusForbidden, "not deletable: part of a torrent's route content")
	}
	err := cfs.Remove(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return refuse(http.StatusNotFound, "no such path: "+p)
	case errors.Is(err, os.ErrPermission):
		return refuse(http.StatusForbidden, "not deletable: part of a torrent's route content")
	case errors.Is(err, dfs.ErrNotEmpty):
		return refuse(http.StatusConflict, "the folder isn't empty: "+p+". Delete what's in it first")
	}
	return err
}

func makeDir(cfs containerFS, p string) error {
	p = path.Clean(p)
	if isRoot(p) {
		return refuse(http.StatusBadRequest, "path is required")
	}
	err := cfs.Mkdir(p)
	if errors.Is(err, os.ErrExist) {
		return refuse(http.StatusConflict, "path already exists: "+p)
	}
	return err
}

func renameEntry(cfs containerFS, oldPath, newPath string) error {
	oldPath, newPath = path.Clean(oldPath), path.Clean(newPath)
	if isRoot(oldPath) || isRoot(newPath) {
		return refuse(http.StatusBadRequest, "old_path and new_path are required")
	}
	if !cfs.IsOwned(oldPath) {
		return refuse(http.StatusForbidden, "not renameable: part of a torrent's route content")
	}
	err := cfs.Rename(oldPath, newPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return refuse(http.StatusNotFound, "source path does not exist: "+oldPath)
	case errors.Is(err, os.ErrExist):
		return refuse(http.StatusConflict, "destination path already exists: "+newPath)
	case errors.Is(err, os.ErrPermission):
		return refuse(http.StatusForbidden, "not renameable: part of a torrent's route content")
	case errors.Is(err, dfs.ErrNotEmpty):
		return refuse(http.StatusConflict, "a folder with something in it can't be renamed: "+oldPath)
	}
	return err
}

func addLink(lfs linkFs, oldPath, newPath string) error {
	err := lfs.Link(oldPath, newPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return refuse(http.StatusNotFound, "source path does not exist: "+oldPath)
	case errors.Is(err, os.ErrExist):
		return refuse(http.StatusConflict, "destination path already exists: "+newPath)
	}
	return err
}

func listLinks(s torrentService, ss *torrent.Stats) ([]Link, error) {
	links, err := s.ListLinks()
	if err != nil {
		return nil, err
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
	return out, nil
}

func removeLink(lfs linkFs, s torrentService, p string) error {
	p = path.Clean(p)
	if isRoot(p) {
		return refuse(http.StatusBadRequest, "path is required")
	}

	links, err := s.ListLinks()
	if err != nil {
		return err
	}
	found := false
	for newPath := range links {
		if normalizeLinkPath(newPath) == p {
			found = true
			break
		}
	}
	if !found {
		return refuse(http.StatusNotFound, "no link at path: "+p)
	}

	err = lfs.Remove(p)
	if errors.Is(err, dfs.ErrNotEmpty) {
		return refuse(http.StatusConflict, "the folder isn't empty: "+p+". Delete what's in it first")
	}
	if errors.Is(err, os.ErrNotExist) {
		// The DB record exists (found above) but the live tree entry is already gone — an
		// orphaned link, e.g. left behind by a torrent deletion that cascaded before that
		// was fixed. lfs.Remove can't clean up a record it can't find in the tree, so
		// reconcile the DB directly instead of leaving it stuck.
		return s.RemoveLink(p)
	}
	return err
}
