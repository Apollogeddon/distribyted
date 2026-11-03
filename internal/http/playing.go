package http

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The dashboard's Now playing list is the files being read now: what a player is streaming,
// how far in it is, how much is cached ahead, and whether it's about to wait for data.

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
	ID string
	// Title is the name the library knows the file by, such as "Dune Part Two (2024)", or the
	// file's own name when nothing links to it; Source is its torrent's name.
	Title, Source string
	Status        health
	// Played and Buffered are how far into the file play and the cached data ahead of it
	// reach, in percent.
	Played, Buffered int
	Cached           []bool
	// Position, Buffer and Transfer are the facts under the rail: "1:02:10 of 2:46:00",
	// "56 s buffered", "9.0 MiB/s from 34 peers".
	Position, Buffer, Transfer string
	TorrentHref                string
}

func (v playingView) PlayedWidth() string   { return fmt.Sprintf("width: %d%%", v.Played) }
func (v playingView) BufferedWidth() string { return fmt.Sprintf("width: %d%%", v.Buffered) }
func (v playingView) HeadLeft() string      { return fmt.Sprintf("left: %d%%", v.Played) }

// activeReads is what's being read now; a var so tests can stand in for the filesystem.
var activeReads = dfs.ActiveReads

// libraryNames maps each linked file to the path the library knows it by.
func libraryNames(s torrentService) map[string]string {
	out := map[string]string{}
	if s == nil {
		return out
	}
	links, err := s.ListLinks()
	if err != nil {
		return out
	}
	for newpath, src := range links {
		if !torrent.IsLinkSource(src) {
			continue
		}
		np := normalizeLinkPath(newpath)
		// a film linked twice keeps one name, the same each refresh
		if cur, ok := out[src]; !ok || np < cur {
			out[src] = np
		}
	}
	return out
}

func newPlayingViews(ss *torrent.Stats, s torrentService, now time.Time) []playingView {
	reads := activeReads()
	if len(reads) == 0 {
		return nil
	}
	names := libraryNames(s)
	var out []playingView
	for i, r := range reads {
		v := playingView{
			ID:    fmt.Sprintf("playing-%s-%d", r.Hash, i),
			Title: path.Base(r.Path),
		}
		if ss != nil {
			v.Source = ss.Name(r.Hash)
			if route := ss.GetRouteFromHash(r.Hash); route != "" {
				v.TorrentHref = torrentHref(route, r.Hash)
				if np, ok := names[path.Join("/", route, r.Path)]; ok {
					v.Title = strings.TrimSuffix(path.Base(np), path.Ext(np))
				}
			}
		}
		if r.Size > 0 {
			v.Played = int(min(r.Pos*100/r.Size, 100))
			v.Buffered = int(min((r.Pos+r.Ahead)*100/r.Size, 100))
		}
		v.Cached = r.Cached
		v.Position, v.Buffer, v.Transfer = playingFacts(r)
		v.Status = playingStatus(r, now)
		out = append(out, v)
	}
	return out
}

// playingFacts says where play is, how much is buffered, and what's coming in: in time for a
// video or audio file whose duration is known, in bytes for anything else.
func playingFacts(r dfs.ReadActivity) (position, buffer, transfer string) {
	bytesOf := func(n int64) string { return humanize.IBytes(uint64(max(n, 0))) }
	if r.Duration > 0 && r.Size > 0 {
		at := time.Duration(float64(r.Duration) * float64(r.Pos) / float64(r.Size))
		position = clock(at) + " of " + clock(r.Duration)
	} else {
		position = bytesOf(r.Pos) + " of " + bytesOf(r.Size)
	}

	switch {
	case r.Ahead <= 0:
		buffer = "Nothing buffered"
	case r.Duration > 0 && r.Size > 0:
		buffer = span(time.Duration(float64(r.Duration)*float64(r.Ahead)/float64(r.Size))) + " buffered"
	case r.Rate > 0:
		buffer = bytesOf(r.Ahead) + " ahead, about " + span(time.Duration(r.Ahead/r.Rate)*time.Second)
	default:
		buffer = bytesOf(r.Ahead) + " cached ahead"
	}

	switch {
	case r.Rate > 0 && r.Peers > 0:
		transfer = rate(r.Rate, 1) + " from " + countOf(r.Peers, "peer")
	case r.Peers == 0:
		transfer = "No peers"
	default:
		transfer = countOf(r.Peers, "peer")
	}
	return position, buffer, transfer
}

// clock is d as a player shows it: 1:02:10, or 14:30 under an hour.
func clock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// span is a short length of time in words: 56 s, 3 min, 1 h 20 min.
func span(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s < 90:
		return fmt.Sprintf("%d s", s)
	case s < 3600:
		return fmt.Sprintf("%d min", (s+30)/60)
	}
	return fmt.Sprintf("%d h %d min", s/3600, s/60%60)
}

func playingStatus(r dfs.ReadActivity, now time.Time) health {
	end := r.Pos >= r.Size
	switch {
	case r.Waiting >= waitWarning:
		reason := fmt.Sprintf("A read has waited %s for data.", r.Waiting.Round(time.Second))
		if r.Peers == 0 {
			reason += " Its torrent has no peers left, so it can't download more."
		}
		return health{Level: "warn", Label: "Waiting for data", Reason: reason}
	case r.Peers == 0 && !end && r.Ahead < lowAhead:
		return health{Level: "warn", Label: "No peers", Reason: "Nothing more can download, so play will stop once what's cached runs out."}
	case now.Sub(r.LastRead) >= pausedAfter:
		return health{Level: "wait", Label: "Paused"}
	}
	return health{Level: "ok", Label: "Playing"}
}
