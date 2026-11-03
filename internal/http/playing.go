package http

import (
	"fmt"
	"path"
	"time"

	"github.com/dustin/go-humanize"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The dashboard's Now playing list is the files being read now: what a player is streaming,
// how far in it is, and whether it's about to wait for data.

const (
	// waitWarning is how long a read may wait for data before it's shown as stuck.
	waitWarning = 2 * time.Second
	// pausedAfter is how long without a read before a file shows as paused, its player
	// having buffered enough.
	pausedAfter = 5 * time.Second
	// lowAhead is cached data ahead of play that's too little to ride out a lost swarm.
	lowAhead = 16 << 20
)

type playingView struct {
	ID            string
	Name, Torrent string
	Rate          string
	Percent       int
	Position      string // "1.2 GiB of 4.0 GiB"
	Ahead         string // "64 MiB cached ahead · about 40 s"
	Peers         int
	Status        health
}

func (v playingView) Width() string { return fmt.Sprintf("width: %d%%", v.Percent) }

// activeReads is what's being read now; a var so tests can stand in for the filesystem.
var activeReads = dfs.ActiveReads

func newPlayingViews(ss *torrent.Stats, now time.Time) []playingView {
	var out []playingView
	for _, r := range activeReads() {
		v := playingView{
			ID:    "playing-" + r.Hash + "-" + fmt.Sprint(len(out)),
			Name:  path.Base(r.Path),
			Rate:  rate(r.Rate, 1),
			Peers: r.Peers,
		}
		if ss != nil {
			v.Torrent = ss.Name(r.Hash)
		}
		if r.Size > 0 {
			v.Percent = int(min(r.Pos*100/r.Size, 100))
		}
		v.Position = humanize.IBytes(uint64(max(r.Pos, 0))) + " of " + humanize.IBytes(uint64(max(r.Size, 0)))
		v.Ahead = humanize.IBytes(uint64(max(r.Ahead, 0))) + " cached ahead"
		if r.Rate > 0 && r.Ahead > 0 {
			v.Ahead += " · about " + (time.Duration(r.Ahead/r.Rate) * time.Second).String()
		}
		v.Status = playingStatus(r, now)
		out = append(out, v)
	}
	return out
}

func playingStatus(r dfs.ReadActivity, now time.Time) health {
	end := r.Pos >= r.Size
	switch {
	case r.Waiting >= waitWarning:
		reason := fmt.Sprintf("A read has waited %s for data.", r.Waiting.Round(time.Second))
		if r.Peers == 0 {
			reason += " No peers are connected."
		}
		return health{Level: "warn", Label: "Waiting for data", Reason: reason}
	case r.Peers == 0 && !end && r.Ahead < lowAhead:
		return health{Level: "warn", Label: "No peers", Reason: "Nothing more can download, so play will stop once what's cached runs out."}
	case now.Sub(r.LastRead) >= pausedAfter:
		return health{Level: "wait", Label: "Paused", Reason: "No reads for a few seconds: the player has buffered, or is paused."}
	}
	return health{Level: "ok", Label: "Playing"}
}
