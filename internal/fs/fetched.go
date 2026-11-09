package fs

import (
	"sort"
	"sync"
	"time"
)

// The torrent client doesn't say why it downloaded a piece. Downloads with nothing playing are
// either something reading the mount or a preparation, so preparations tag the pieces they ask
// for with their reason, and every piece the storage completes is counted under its tag, or as
// read when it has none. A piece downloaded a second time was evicted from the cache before
// it was wanted again: that's counted too, as the cost of a cache too small or a policy wrong.

// Reason is why a piece was downloaded.
type Reason int8

const (
	// ReasonRead is a read through the mount, or the readahead in front of one.
	ReasonRead Reason = iota
	// ReasonLink is the start and end of a file just linked into a library.
	ReasonLink
	// ReasonNextEpisode is the lead of the episode after one nearly finished.
	ReasonNextEpisode
	// ReasonButton is a preparation asked for in the web interface.
	ReasonButton
	numReasons
)

func (r Reason) String() string {
	switch r {
	case ReasonLink:
		return "Linked into the library"
	case ReasonNextEpisode:
		return "Next episode"
	case ReasonButton:
		return "Prepared by hand"
	}
	return "Read"
}

// Reasons lists every Reason, in the order the web interface shows them.
func Reasons() []Reason {
	return []Reason{ReasonRead, ReasonLink, ReasonNextEpisode, ReasonButton}
}

const (
	// fetchHours is how many hours of downloads are kept by the hour.
	fetchHours = 24
	// maxSeenPieces bounds the record of pieces downloaded before; past it the record starts
	// again, and a piece downloaded again goes uncounted as such once.
	maxSeenPieces = 1 << 20
)

type pieceKey struct {
	hash  string
	index int
}

type fetchHour struct {
	hour         int64 // Unix hours
	bytes, again [numReasons]int64
}

var fetches = struct {
	sync.Mutex
	wanted map[pieceKey]Reason
	seen   map[pieceKey]struct{}
	total  [numReasons]int64
	again  [numReasons]int64
	hours  [fetchHours]fetchHour
	since  time.Time
	now    func() time.Time
}{
	wanted: map[pieceKey]Reason{},
	seen:   map[pieceKey]struct{}{},
	since:  time.Now(),
	now:    time.Now,
}

// wantPieces tags the torrent's pieces with why they were asked for: only pieces not in the
// cache, so a tag is gone once its piece downloads. A piece already tagged keeps its first
// reason.
func wantPieces(hash string, pieces []int, r Reason) {
	fetches.Lock()
	defer fetches.Unlock()
	for _, i := range pieces {
		k := pieceKey{hash, i}
		if _, ok := fetches.wanted[k]; !ok {
			fetches.wanted[k] = r
		}
	}
}

// unwantPieces drops the tags of the torrent's pieces [begin, end): they're no longer asked for.
func unwantPieces(hash string, begin, end int) {
	fetches.Lock()
	defer fetches.Unlock()
	for i := begin; i < end; i++ {
		delete(fetches.wanted, pieceKey{hash, i})
	}
}

// PieceDownloaded counts a piece of the torrent the storage completed, size bytes long, under
// the reason it was asked for.
func PieceDownloaded(hash string, index int, size int64) {
	fetches.Lock()
	defer fetches.Unlock()
	k := pieceKey{hash, index}
	r, ok := fetches.wanted[k]
	if ok {
		delete(fetches.wanted, k)
	} else {
		r = ReasonRead
	}
	_, again := fetches.seen[k]
	if !again {
		if len(fetches.seen) >= maxSeenPieces {
			fetches.seen = map[pieceKey]struct{}{}
		}
		fetches.seen[k] = struct{}{}
	}

	hour := fetches.now().Unix() / 3600
	h := &fetches.hours[hour%fetchHours]
	if h.hour != hour {
		*h = fetchHour{hour: hour}
	}
	fetches.total[r] += size
	h.bytes[r] += size
	if again {
		fetches.again[r] += size
		h.again[r] += size
	}
}

// FetchCount is the bytes downloaded for one reason.
type FetchCount struct {
	Reason Reason
	// Day and Total are what was downloaded in the last day, and since distribyted started.
	Day, Total int64
	// DayAgain and TotalAgain are the part of those downloaded before, then evicted.
	DayAgain, TotalAgain int64
}

// FetchStats is what was downloaded, by reason.
type FetchStats struct {
	Since   time.Time
	Reasons []FetchCount
}

// Fetched reports what was downloaded, by reason, in the order Reasons lists them.
func Fetched() FetchStats {
	fetches.Lock()
	defer fetches.Unlock()
	now := fetches.now().Unix() / 3600
	out := FetchStats{Since: fetches.since}
	for _, r := range Reasons() {
		c := FetchCount{Reason: r, Total: fetches.total[r], TotalAgain: fetches.again[r]}
		for _, h := range fetches.hours {
			if h.hour > now-fetchHours {
				c.Day += h.bytes[r]
				c.DayAgain += h.again[r]
			}
		}
		out.Reasons = append(out.Reasons, c)
	}
	return out
}

// resetFetches empties the record, for tests.
func resetFetches(now func() time.Time) {
	fetches.Lock()
	defer fetches.Unlock()
	fetches.wanted = map[pieceKey]Reason{}
	fetches.seen = map[pieceKey]struct{}{}
	fetches.total, fetches.again = [numReasons]int64{}, [numReasons]int64{}
	fetches.hours = [fetchHours]fetchHour{}
	fetches.now = now
	fetches.since = now()
}

// firstBytes keeps how long the latest player reads waited from opening a file to its first
// byte, so the web interface can show whether starting playback got slower.
var firstBytes = struct {
	sync.Mutex
	d    []time.Duration
	next int
}{}

const maxFirstBytes = 200

func recordFirstByte(d time.Duration) {
	firstBytes.Lock()
	defer firstBytes.Unlock()
	if len(firstBytes.d) < maxFirstBytes {
		firstBytes.d = append(firstBytes.d, d)
		return
	}
	firstBytes.d[firstBytes.next] = d
	firstBytes.next = (firstBytes.next + 1) % maxFirstBytes
}

// FirstByteStats is how long the latest opens waited for their first byte: how many there were,
// the median, and the slowest one in twenty.
type FirstByteStats struct {
	Count       int
	Median, P95 time.Duration
}

// FirstBytes reports how long the latest maxFirstBytes opens waited for their first byte.
func FirstBytes() FirstByteStats {
	firstBytes.Lock()
	d := append([]time.Duration(nil), firstBytes.d...)
	firstBytes.Unlock()
	if len(d) == 0 {
		return FirstByteStats{}
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return FirstByteStats{Count: len(d), Median: d[len(d)/2], P95: d[min(len(d)*95/100, len(d)-1)]}
}

func resetFirstBytes() {
	firstBytes.Lock()
	defer firstBytes.Unlock()
	firstBytes.d, firstBytes.next = nil, 0
}
