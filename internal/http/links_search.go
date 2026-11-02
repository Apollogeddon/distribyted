package http

import (
	"cmp"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
)

// The Add link form searches the mount for the file to link, so it needn't be typed out
// path and all: each word typed has to appear somewhere in a file's path.

const (
	// searchMinLen is the shortest query searched; one letter matches nearly everything.
	searchMinLen = 2
	// searchShown is how many matches the form lists.
	searchShown = 20
	// searchMaxEntries bounds how much of the mount one search reads, so a huge library
	// can't keep a request busy for long.
	searchMaxEntries = 50000
)

type searchMatch struct {
	Path, Name, Dir, Size string
	// nameHits is how many of the query's words are in the file's name, not just its folders
	nameHits int
}

type searchResults struct {
	Query   string
	Matches []searchMatch
	// More is set when there were more matches than shown, or the search stopped early.
	More bool
}

// searchFiles finds the files whose path holds every word of q, ignoring case. Folders
// aren't linked, so they aren't listed, and an archive is listed as the file it is rather
// than searched inside: opening one reads it, which may mean downloading it.
func searchFiles(cfs containerFS, q string) searchResults {
	res := searchResults{Query: q}
	words := strings.Fields(strings.ToLower(q))
	if len(strings.TrimSpace(q)) < searchMinLen || len(words) == 0 {
		return res
	}

	var matches []searchMatch
	seen := 0
	queue := []string{"/"}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		children, err := cfs.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(children))
		for name := range children {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if seen++; seen > searchMaxEntries {
				res.More = true
				queue = nil
				break
			}
			f := children[name]
			p := path.Join(dir, name)
			if f.IsDir() {
				queue = append(queue, p)
				continue
			}
			lowerPath, lowerName := strings.ToLower(p), strings.ToLower(name)
			hits, all := 0, true
			for _, w := range words {
				if !strings.Contains(lowerPath, w) {
					all = false
					break
				}
				if strings.Contains(lowerName, w) {
					hits++
				}
			}
			if !all {
				continue
			}
			matches = append(matches, searchMatch{
				Path:     p,
				Name:     name,
				Dir:      dir,
				Size:     humanize.IBytes(uint64(max(f.Size(), 0))),
				nameHits: hits,
			})
		}
	}

	// files named for what was typed first, then the shortest paths, as the likeliest
	slices.SortFunc(matches, func(a, b searchMatch) int {
		return cmp.Or(
			cmp.Compare(b.nameHits, a.nameHits),
			cmp.Compare(len(a.Path), len(b.Path)),
			strings.Compare(a.Path, b.Path),
		)
	})
	if len(matches) > searchShown {
		matches, res.More = matches[:searchShown], true
	}
	res.Matches = matches
	return res
}

var linksSearchHandler = func(cfs containerFS) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, linkSearchView(searchFiles(cfs, c.Query("source"))))
	}
}
