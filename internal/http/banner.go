package http

import (
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/gin-gonic/gin"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

// Every page carries a banner that says what's going wrong with streaming right now, so a
// problem is seen from whichever page is open, not only on the dashboard.

const (
	// stuckRead is how long a read may wait before the banner calls it stuck: longer than
	// the dashboard's flag, as a player rides out a few seconds.
	stuckRead = 10 * time.Second
	// abandonedWindow is how far back reads that gave up are counted.
	abandonedWindow = time.Hour
	// linksSettle is how long after a start links may still be waiting for their torrents
	// before the banner mentions them: they load one after another.
	linksSettle = 2 * time.Minute
)

type bannerItem struct {
	Level   string // warn or wait
	Message string
	Href    string
	Action  string
}

// abandonedSince is how many reads gave up since a time; a var so tests can stand in.
var abandonedSince = dfs.AbandonedSince

func newBanner(now, started time.Time, waitingLinks func() []string) []bannerItem {
	var items []bannerItem
	for _, r := range activeReads() {
		name := path.Base(r.Path)
		switch {
		case r.Waiting >= stuckRead:
			items = append(items, bannerItem{
				Level:   "warn",
				Message: fmt.Sprintf("“%s” has waited %s for data.", name, r.Waiting.Round(time.Second)),
				Href:    "/", Action: "See what's playing",
			})
		case r.Peers == 0 && r.Pos < r.Size && r.Ahead < lowAhead && now.Sub(r.LastRead) < pausedAfter:
			items = append(items, bannerItem{
				Level:   "warn",
				Message: fmt.Sprintf("“%s” is playing, but its torrent has no peers left.", name),
				Href:    "/", Action: "See what's playing",
			})
		}
	}
	if n := abandonedSince(now.Add(-abandonedWindow)); n > 0 {
		items = append(items, bannerItem{
			Level:   "warn",
			Message: fmt.Sprintf("%s gave up waiting for data in the last hour.", countOf(n, "read")),
			Href:    "/logs?level=error", Action: "See the errors",
		})
	}
	if waitingLinks != nil && now.Sub(started) >= linksSettle {
		if n := len(waitingLinks()); n > 0 {
			items = append(items, bannerItem{
				Level:   "wait",
				Message: fmt.Sprintf("%s still waiting for their torrents to load.", countOf(n, "link")),
				Href:    "/links?show=attention", Action: "Show them",
			})
		}
	}
	return items
}

var bannerHandler = func(started time.Time, waitingLinks func() []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, healthBanner(newBanner(time.Now(), started, waitingLinks)))
	}
}
