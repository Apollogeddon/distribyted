package http

import (
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The Servers page shows each folder distribyted shares as a torrent: its state, its
// magnet link to copy, and who is connected. The folder itself is set in the configuration
// file, so it is shown, not edited.

type serversPage struct {
	Title   string
	Servers []serverView
}

type serverView struct {
	torrent.ServerInfo
	Level                string // ok, wait, warn or error
	Updated, UpdatedFull string
}

var serverLevels = map[string]string{"Seeding": "ok", "Reading": "wait", "Updating": "wait", "Error": "error"}

func newServersPage(tss []*torrent.Server) serversPage {
	page := serversPage{Title: "Servers"}
	for _, s := range tss {
		info := s.Info()
		v := serverView{ServerInfo: info, Level: serverLevels[info.State]}
		if v.Level == "" {
			v.Level = "warn"
		}
		if info.UpdatedAt > 0 {
			t := time.Unix(info.UpdatedAt, 0)
			v.Updated = humanize.Time(t)
			v.UpdatedFull = t.Format(time.RFC1123)
		}
		page.Servers = append(page.Servers, v)
	}
	return page
}

var serversHandler = func(tss []*torrent.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "servers.html", newServersPage(tss))
	}
}

var serversListHandler = func(tss []*torrent.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "servers-list", newServersPage(tss))
	}
}
