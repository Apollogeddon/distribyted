package fs

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Every handle that reads a torrent's file is tracked while it reads, so the dashboard can
// show what's playing: how fast each file is read, how far in, how much of what comes next is
// already cached, and whether a read is stuck waiting for a peer.

const (
	// activityWindow is how far back a file's read rate is averaged, in seconds.
	activityWindow = 10
	// activityIdle is how long after its last read a handle stops counting as playing. A
	// player that has buffered pauses for a while between reads.
	activityIdle = 30 * time.Second
	// maxAheadPieces bounds how far ahead of a read cached pieces are counted.
	maxAheadPieces = 64
)

type activityBucket struct {
	sec, n int64
}

// handleActivity is one handle's reads.
type handleActivity struct {
	file     *torrentFile
	openedAt time.Time
	// nearEnd is set once the handle has read past nearEndPercent of its file
	nearEnd atomic.Bool
	// waitingSince is when the read in progress started, in Unix nanoseconds, or 0
	waitingSince atomic.Int64

	mu       sync.Mutex
	pos      int64
	lastRead time.Time
	buckets  [activityWindow]activityBucket
}

var activities = struct {
	sync.Mutex
	m map[*torrentFileHandle]*handleActivity
}{m: make(map[*torrentFileHandle]*handleActivity)}

// activity is the handle's record, made on its first read. A closed handle's isn't kept.
func (h *torrentFileHandle) activity() *handleActivity {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.act != nil {
		return h.act
	}
	a := &handleActivity{file: h.torrentFile, openedAt: h.openedAt}
	if h.closed {
		return a
	}
	h.act = a
	activities.Lock()
	activities.m[h] = a
	activities.Unlock()
	return a
}

// forgetActivity runs under h.mu, once the handle is closed.
func (h *torrentFileHandle) forgetActivity() {
	if h.act == nil {
		return
	}
	activities.Lock()
	delete(activities.m, h)
	activities.Unlock()
}

func (a *handleActivity) start() { a.waitingSince.Store(time.Now().UnixNano()) }

func (a *handleActivity) done(off int64, n int) {
	a.waitingSince.Store(0)
	if n <= 0 {
		return
	}
	now := time.Now()
	sec := now.Unix()
	a.mu.Lock()
	b := &a.buckets[sec%activityWindow]
	if b.sec != sec {
		*b = activityBucket{sec: sec}
	}
	b.n += int64(n)
	a.pos = off + int64(n)
	a.lastRead = now
	a.mu.Unlock()

	f := a.file
	if f.onNearEnd != nil && f.len >= minNearEndFile && (off+int64(n))*100 >= f.len*nearEndPercent &&
		a.nearEnd.CompareAndSwap(false, true) {
		go f.onNearEnd(f.hash, f.path)
	}
}

const (
	// nearEndPercent is how far into a file a read has to be to count as nearly finished.
	nearEndPercent = 95
	// minNearEndFile is the smallest file whose end is watched for: an episode, not a
	// subtitle or a sample.
	minNearEndFile = 100 << 20
)

// ReadActivity is a file being read now, from every handle reading it.
type ReadActivity struct {
	Hash, Path string // the torrent's, and the file's path in it
	Size       int64
	// Pos is where the latest read ended.
	Pos int64
	// Rate is how many bytes a second were read, over the last activityWindow seconds.
	Rate int64
	// Ahead is how many bytes from Pos are cached, so play won't wait for them.
	Ahead int64
	// Waiting is how long the longest read in progress has waited for data, or 0.
	Waiting time.Duration
	// Abandoned is how many reads of the file gave up on their deadline, ever.
	Abandoned int64
	Peers     int
	Since     time.Time // when the first handle reading it was opened
	LastRead  time.Time
}

// ActiveReads lists the files read in the last activityIdle, or waiting on a read now, the
// most recently read first.
func ActiveReads() []ReadActivity {
	activities.Lock()
	list := make([]*handleActivity, 0, len(activities.m))
	for _, a := range activities.m {
		list = append(list, a)
	}
	activities.Unlock()

	now := time.Now()
	type key struct{ hash, path string }
	byFile := map[key]*ReadActivity{}
	var order []key
	for _, a := range list {
		var waiting time.Duration
		if ws := a.waitingSince.Load(); ws != 0 {
			waiting = now.Sub(time.Unix(0, ws))
		}
		a.mu.Lock()
		pos, last := a.pos, a.lastRead
		var bytes int64
		for _, b := range a.buckets {
			if b.sec > now.Unix()-activityWindow {
				bytes += b.n
			}
		}
		a.mu.Unlock()
		if waiting == 0 && (last.IsZero() || now.Sub(last) > activityIdle) {
			continue
		}

		k := key{a.file.hash, a.file.path}
		r, ok := byFile[k]
		if !ok {
			r = &ReadActivity{Hash: k.hash, Path: k.path, Size: a.file.len, Since: a.openedAt, Abandoned: a.file.stats.abandoned.Load()}
			byFile[k] = r
			order = append(order, k)
		}
		r.Rate += bytes / activityWindow
		r.Waiting = max(r.Waiting, waiting)
		if a.openedAt.Before(r.Since) {
			r.Since = a.openedAt
		}
		if last.After(r.LastRead) {
			r.LastRead, r.Pos = last, pos
		}
	}

	out := make([]ReadActivity, 0, len(order))
	for _, k := range order {
		r := byFile[k]
		var f *torrentFile
		for _, a := range list {
			if a.file.hash == k.hash && a.file.path == k.path {
				f = a.file
				break
			}
		}
		if f != nil && f.pieces != nil {
			r.Ahead = f.pieces.cachedAhead(r.Pos)
			r.Peers = f.pieces.t.Stats().ActivePeers
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastRead.After(out[j].LastRead) })
	return out
}

// cachedAhead is how many of the file's bytes from off are in cached pieces, without a gap,
// counting at most maxAheadPieces pieces.
func (s *pieceSync) cachedAhead(off int64) int64 {
	if off >= s.length {
		return 0
	}
	begin, end := s.pieceRange(off, s.length-off)
	end = min(end, begin+maxAheadPieces)
	stop := off
	for i := begin; i < end && s.cached(i); i++ {
		stop = min(int64(i+1)*s.pieceLen-s.offset, s.length)
	}
	return stop - off
}

// abandonedReads keeps when reads gave up on their deadline, the latest maxAbandonedKept, so
// the web interface can say a stream has been failing lately.
var abandonedReads = struct {
	sync.Mutex
	at []time.Time
}{}

const maxAbandonedKept = 256

func recordAbandoned(now time.Time) {
	abandonedReads.Lock()
	defer abandonedReads.Unlock()
	abandonedReads.at = append(abandonedReads.at, now)
	if n := len(abandonedReads.at); n > maxAbandonedKept {
		abandonedReads.at = abandonedReads.at[n-maxAbandonedKept:]
	}
}

// AbandonedSince is how many reads gave up on their deadline since t, counting at most the
// latest maxAbandonedKept.
func AbandonedSince(t time.Time) int {
	abandonedReads.Lock()
	defer abandonedReads.Unlock()
	n := 0
	for _, at := range abandonedReads.at {
		if at.After(t) {
			n++
		}
	}
	return n
}
