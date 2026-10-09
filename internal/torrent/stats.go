package torrent

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

var ErrTorrentNotFound = errors.New("torrent not found")

type PieceStatus string

const (
	Checking PieceStatus = "H"
	Partial  PieceStatus = "P"
	Complete PieceStatus = "C"
	Waiting  PieceStatus = "W"
	Error    PieceStatus = "?"
)

type PieceChunk struct {
	Status    PieceStatus `json:"status"`
	NumPieces int         `json:"numPieces"`
}

type TorrentStats struct {
	Name            string        `json:"name"`
	Hash            string        `json:"hash"`
	DownloadedBytes int64         `json:"downloadedBytes"`
	UploadedBytes   int64         `json:"uploadedBytes"`
	Peers           int           `json:"peers"`
	Seeders         int           `json:"seeders"`
	TimePassed      float64       `json:"timePassed"`
	PieceChunks     []*PieceChunk `json:"pieceChunks"`
	TotalPieces     int           `json:"totalPieces"`
	PieceSize       int64         `json:"pieceSize"`
	// AgeSeconds is how long ago this torrent was added. Seeders is only
	// currently-connected, confirmed-seeding peers (see Stats.stats) — right
	// after adding a torrent, connections and bitfield exchange haven't had
	// time to ramp up yet, so a low count here doesn't yet mean the swarm is
	// unhealthy. Consumers should use this to withhold low-seeder warnings
	// for a grace period rather than alarming on a number that hasn't
	// stabilized yet.
	AgeSeconds float64 `json:"ageSeconds"`
}

type byName []*TorrentStats

func (a byName) Len() int           { return len(a) }
func (a byName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool { return a[i].Name < a[j].Name }

type GlobalTorrentStats struct {
	DownloadedBytes int64   `json:"downloadedBytes"`
	UploadedBytes   int64   `json:"uploadedBytes"`
	TimePassed      float64 `json:"timePassed"`
}

type RouteStats struct {
	Name         string          `json:"name"`
	TorrentStats []*TorrentStats `json:"torrentStats"`
}

type ByName []*RouteStats

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

// stat is a torrent's latest sample: the bytes moved since the sample before it, over
// interval. A cached sample keeps its own interval, so bytes / interval stays the rate it
// measured however often it is read.
type stat struct {
	totalDownloadBytes int64
	downloadBytes      int64
	totalUploadBytes   int64
	uploadBytes        int64
	peers              int
	seeders            int
	time               time.Time
	interval           time.Duration
}

type Stats struct {
	mut             sync.Mutex
	torrents        map[string]fs.Torrent
	torrentsByRoute map[string]map[string]fs.Torrent
	previousStats   map[string]*stat
	addedAt         map[string]time.Time
}

func NewStats() *Stats {
	return &Stats{
		torrents:        make(map[string]fs.Torrent),
		torrentsByRoute: make(map[string]map[string]fs.Torrent),
		previousStats:   make(map[string]*stat),
		addedAt:         make(map[string]time.Time),
	}
}

func (s *Stats) AddRoute(route string) {
	s.mut.Lock()
	defer s.mut.Unlock()
	_, ok := s.torrentsByRoute[route]
	if !ok {
		s.torrentsByRoute[route] = make(map[string]fs.Torrent)
	}
}

func (s *Stats) Add(route string, t fs.Torrent) {
	s.mut.Lock()
	defer s.mut.Unlock()

	h := t.InfoHash().String()

	s.torrents[h] = t
	// a torrent in a second route keeps the samples it already has
	if _, ok := s.previousStats[h]; !ok {
		s.previousStats[h] = &stat{}
		s.addedAt[h] = time.Now()
	}

	_, ok := s.torrentsByRoute[route]
	if !ok {
		s.torrentsByRoute[route] = make(map[string]fs.Torrent)
	}

	s.torrentsByRoute[route][h] = t
}

// Del removes a torrent from one route, and forgets it entirely once no route has it.
func (s *Stats) Del(route, hash string) {
	s.mut.Lock()
	defer s.mut.Unlock()

	if ts, ok := s.torrentsByRoute[route]; ok {
		delete(ts, hash)
	}
	for _, ts := range s.torrentsByRoute {
		if _, ok := ts[hash]; ok {
			return
		}
	}

	delete(s.torrents, hash)
	delete(s.previousStats, hash)
	delete(s.addedAt, hash)
}

func (s *Stats) GetAllTorrents() map[string]fs.Torrent {
	s.mut.Lock()
	defer s.mut.Unlock()

	out := make(map[string]fs.Torrent)
	for h, t := range s.torrents {
		out[h] = t
	}
	return out
}

// RouteNames returns the names of every route that currently has torrents
// registered. It's a plain key listing (no piece-state computation, unlike
// RoutesStats), cheap enough to call on every poll of a page that needs to
// know which route a path falls under.
func (s *Stats) RouteNames() []string {
	s.mut.Lock()
	defer s.mut.Unlock()

	names := make([]string, 0, len(s.torrentsByRoute))
	for route := range s.torrentsByRoute {
		names = append(names, route)
	}
	sort.Strings(names)
	return names
}

func (s *Stats) GetRouteFromHash(hash string) string {
	s.mut.Lock()
	defer s.mut.Unlock()

	var routes []string
	for route, torrents := range s.torrentsByRoute {
		if _, ok := torrents[hash]; ok {
			routes = append(routes, route)
		}
	}

	if len(routes) == 0 {
		return ""
	}

	sort.Strings(routes)
	return routes[0]
}

func (s *Stats) GetRoutesFromHash(hash string) []string {
	s.mut.Lock()
	defer s.mut.Unlock()

	var routes []string
	for route, torrents := range s.torrentsByRoute {
		if _, ok := torrents[hash]; ok {
			routes = append(routes, route)
		}
	}

	sort.Strings(routes)
	return routes
}

func (s *Stats) GetTorrentsInRoute(route string) map[string]fs.Torrent {
	s.mut.Lock()
	defer s.mut.Unlock()

	torrents, ok := s.torrentsByRoute[route]
	if !ok {
		return nil
	}

	out := make(map[string]fs.Torrent)
	for h, t := range torrents {
		out[h] = t
	}
	return out
}

func (s *Stats) Stats(hash string) (*TorrentStats, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	t, ok := s.torrents[hash]
	if !ok {
		return nil, ErrTorrentNotFound
	}

	now := time.Now()

	return s.stats(now, t, true), nil
}

func (s *Stats) RoutesStats() []*RouteStats {
	s.mut.Lock()
	defer s.mut.Unlock()

	now := time.Now()

	var out []*RouteStats
	for r, tl := range s.torrentsByRoute {
		var tStats []*TorrentStats
		for _, t := range tl {
			ts := s.stats(now, t, true)
			tStats = append(tStats, ts)
		}

		sort.Sort(byName(tStats))

		rs := &RouteStats{
			Name:         r,
			TorrentStats: tStats,
		}
		out = append(out, rs)
	}

	return out
}

// GlobalStats reports the summed rate of every torrent's latest sample as bytes over a
// one-second TimePassed, so callers' bytes / TimePassed is bytes per second. Calling it
// changes no state, so several clients polling at once don't skew each other's figures.
func (s *Stats) GlobalStats() *GlobalTorrentStats {
	s.mut.Lock()
	defer s.mut.Unlock()

	now := time.Now()

	var downloadRate, uploadRate float64
	for _, torrent := range s.torrents {
		tStats := s.stats(now, torrent, false)
		if tStats.TimePassed > 0 {
			downloadRate += float64(tStats.DownloadedBytes) / tStats.TimePassed
			uploadRate += float64(tStats.UploadedBytes) / tStats.TimePassed
		}
	}

	return &GlobalTorrentStats{
		DownloadedBytes: int64(downloadRate),
		UploadedBytes:   int64(uploadRate),
		TimePassed:      1,
	}
}

func (s *Stats) stats(now time.Time, t fs.Torrent, chunks bool) *TorrentStats {
	ts := &TorrentStats{}
	hash := t.InfoHash().String()
	prev, ok := s.previousStats[hash]
	if !ok {
		return &TorrentStats{}
	}
	ts.AgeSeconds = now.Sub(s.addedAt[hash]).Seconds()
	cur := prev
	// sample at most every gap; a sooner read gets the latest sample unchanged
	if prev.time.IsZero() || now.Sub(prev.time) >= gap {
		since := prev.time
		if since.IsZero() {
			since = s.addedAt[hash]
		}
		st := t.Stats()
		rd := st.BytesReadData.Int64()
		wd := st.BytesWrittenData.Int64()
		cur = &stat{
			downloadBytes:      rd - prev.totalDownloadBytes,
			uploadBytes:        wd - prev.totalUploadBytes,
			totalDownloadBytes: rd,
			totalUploadBytes:   wd,
			time:               now,
			interval:           now.Sub(since),
			peers:              st.TotalPeers,
			seeders:            st.ConnectedSeeders,
		}
		s.previousStats[hash] = cur
	}

	ts.DownloadedBytes = cur.downloadBytes
	ts.UploadedBytes = cur.uploadBytes
	ts.Peers = cur.peers
	ts.Seeders = cur.seeders
	ts.TimePassed = cur.interval.Seconds()
	var totalPieces int
	if chunks {
		var pch []*PieceChunk
		for _, psr := range t.PieceStateRuns() {
			var s PieceStatus
			switch {
			case psr.Checking:
				s = Checking
			case psr.Partial:
				s = Partial
			case psr.Complete:
				s = Complete
			case !psr.Ok:
				s = Error
			default:
				s = Waiting
			}

			pch = append(pch, &PieceChunk{
				Status:    s,
				NumPieces: psr.Length,
			})
			totalPieces += psr.Length
		}
		ts.PieceChunks = pch
	}

	ts.Hash = t.InfoHash().String()
	ts.Name = t.Name()
	ts.TotalPieces = totalPieces

	if t.Info() != nil {
		ts.PieceSize = t.Info().PieceLength
	}

	return ts
}

const gap time.Duration = 2 * time.Second
