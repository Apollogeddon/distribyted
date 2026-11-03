package http

import (
	"net/url"
	"path"
	"slices"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The dashboard's Recently added list is what an *arr app linked into the library lately,
// with whether each is ready to play, so a new film's first play can be checked before it's
// pressed.

const (
	recentWindow = 48 * time.Hour
	recentShown  = 10
)

// recentLinks is the Service's record of when links were made.
type recentLinks interface {
	LinksAddedSince(t time.Time) (map[string]time.Time, error)
}

type recentView struct {
	ID, Name, Folder string
	When, FullTime   string
	// Readiness and ReadinessURL are as on the Files page, empty for a file with no torrent.
	Readiness, ReadinessURL string
	Health                  health
	TorrentHref             string
}

func newRecentViews(s torrentService, ss *torrent.Stats, cfs containerFS, now time.Time) []recentView {
	rl, ok := s.(recentLinks)
	if !ok {
		return nil
	}
	added, err := rl.LinksAddedSince(now.Add(-recentWindow))
	if err != nil || len(added) == 0 {
		return nil
	}
	paths := make([]string, 0, len(added))
	for p := range added {
		paths = append(paths, p)
	}
	slices.SortFunc(paths, func(a, b string) int { return added[b].Compare(added[a]) })

	var out []recentView
	for _, p := range paths {
		if len(out) == recentShown {
			break
		}
		v := recentView{
			ID:       "recent-" + url.QueryEscape(p),
			Name:     path.Base(p),
			Folder:   path.Dir(p),
			When:     humanize.RelTime(added[p], now, "ago", "from now"),
			FullTime: added[p].Format(time.RFC1123),
		}
		if cfs == nil {
			out = append(out, v)
			continue
		}
		children, err := cfs.ReadDir(path.Dir(p))
		f, found := children[path.Base(p)]
		if err != nil || !found || f.IsDir() {
			continue // deleted or replaced since
		}
		if pf, ok := preparerAt(cfs, p); ok {
			if ready, ok := pf.StartReady(); ok {
				v.Readiness, v.ReadinessURL = "cold", "/files/readiness?path="+url.QueryEscape(p)
				if ready {
					v.Readiness = "ready"
				}
			}
		}
		if h := f.Hash(); h != "" && ss != nil {
			if ts, err := ss.Stats(h); err == nil {
				route := ss.GetRouteFromHash(h)
				v.Health = newTorrentView(route, ts).Health
				v.TorrentHref = torrentHref(route, h)
			}
		}
		out = append(out, v)
	}
	return out
}
