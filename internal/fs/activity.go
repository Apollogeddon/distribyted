package fs

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Apollogeddon/distribyted/internal/media"
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

// activityNow is the clock reads are timed by, swapped in tests.
var activityNow = time.Now

type activityBucket struct {
	sec, n int64
}

// handleActivity is one handle's reads.
type handleActivity struct {
	file     *torrentFile
	openedAt time.Time
	// nearEnd is set once the handle has played past nearEndPercent of its file
	nearEnd atomic.Bool
	// quiet is set for the handle distribyted reads a file's duration through: it isn't a
	// player, so its reads don't count as playing
	quiet bool
	// waitingSince is when the read in progress started, in Unix nanoseconds, or 0
	waitingSince atomic.Int64
	// gotFirstByte is set once the handle's first read returned data
	gotFirstByte atomic.Bool
	// probed is set once the file's duration was asked for, to judge the pace of its play
	probed atomic.Bool

	mu       sync.Mutex
	pos      int64
	lastRead time.Time
	buckets  [activityWindow]activityBucket
	// seq is the handle's run of reads: a player reads on from where it was, a scan jumps to
	// what it wants to look at
	seq readRun
	// inRun is set when the handle's latest read carried on the file's run, sharedBytes long
	inRun       bool
	sharedBytes int64
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
	a := &handleActivity{file: h.torrentFile, openedAt: h.openedAt, quiet: h.quiet}
	// a closed handle's reads aren't kept, nor those distribyted makes itself
	if h.closed || h.quiet {
		h.act = a
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
	if n <= 0 || a.quiet {
		return
	}
	now := activityNow()
	if a.gotFirstByte.CompareAndSwap(false, true) {
		recordFirstByte(now.Sub(a.openedAt))
	}
	sec := now.Unix()
	a.mu.Lock()
	b := &a.buckets[sec%activityWindow]
	if b.sec != sec {
		*b = activityBucket{sec: sec}
	}
	b.n += int64(n)
	own := a.seq.advance(off, n, now)
	a.pos = off + int64(n)
	a.lastRead = now
	a.mu.Unlock()
	// a player that opens the file again for each range it reads carries on the file's run
	f := a.file
	shared := f.run.advance(off, n, now)
	a.mu.Lock()
	a.inRun = shared.bytes > 0 && shared.bytes >= own.bytes
	a.sharedBytes = shared.bytes
	a.mu.Unlock()

	if f.onNearEnd == nil || f.len < minNearEndFile || max(own.bytes, shared.bytes)*100 < f.len*minPlayedPercent {
		return
	}
	// what was played is judged by its pace, which needs the file's duration: ask for it once
	// play gets this far, so it's known by the end
	if a.probed.CompareAndSwap(false, true) {
		f.duration()
	}
	if (off+int64(n))*100 < f.len*nearEndPercent {
		return
	}
	d := f.knownDuration()
	if !own.played(f.len, d, now) && !shared.played(f.len, d, now) {
		return
	}
	if a.nearEnd.CompareAndSwap(false, true) && f.run.fire() {
		go f.onNearEnd(f.hash, f.path, LeadFor(f.len, d))
	}
}

// readRun is a run of reads each starting about where the last ended, as a player's do: how
// much it read, and since when.
type readRun struct {
	pos, bytes int64
	since      time.Time
}

// advance adds a read of n at off to the run, or starts a new one when the read jumped, and
// returns it.
func (r *readRun) advance(off int64, n int, now time.Time) readRun {
	// the kernel reads a few requests ahead of a player at once, so a little out of order
	if r.since.IsZero() || off < r.pos-seqSlack || off > r.pos+seqSlack {
		*r = readRun{since: now}
	}
	r.bytes += int64(n)
	r.pos = max(r.pos, off+int64(n))
	return *r
}

// played reports whether the run is play of a file size bytes long that plays for d, now:
// minPlayedPercent of it, read no faster than maxPlayPace times real time.
func (r readRun) played(size int64, d time.Duration, now time.Time) bool {
	return r.bytes*100 >= size*minPlayedPercent && playedAtPace(r.bytes, size, d, now.Sub(r.since))
}

// fileRun is a file's run of reads across every handle reading it, so a player that opens
// the file again for each range it reads still plays it through.
type fileRun struct {
	mu    sync.Mutex
	run   readRun
	fired bool // nearEnd fired for this run
}

func (f *fileRun) advance(off int64, n int, now time.Time) readRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := f.run.since
	r := f.run.advance(off, n, now)
	if r.since != start {
		f.fired = false
	}
	return r
}

// fire reports whether nearEnd hasn't fired for this run yet, marking it fired.
func (f *fileRun) fire() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fired {
		return false
	}
	f.fired = true
	return true
}

// sequential is how much the handle has read since it last jumped, or the file's run when the
// handle's reads are part of it.
func (a *handleActivity) sequential() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inRun {
		return max(a.seq.bytes, a.sharedBytes)
	}
	return a.seq.bytes
}

// playedAtPace reports whether reading n of a file size bytes long that plays for d took long
// enough, at took, to be someone watching: no faster than maxPlayPace times real time. A scan
// reads as fast as the swarm sends. With d unknown, any pace passes.
func playedAtPace(n, size int64, d, took time.Duration) bool {
	if d <= 0 || size <= 0 {
		return true
	}
	played := time.Duration(float64(d) * float64(n) / float64(size))
	return took*maxPlayPace >= played
}

const (
	// nearEndPercent is how far into a file a read has to be to count as nearly finished.
	nearEndPercent = 95
	// minNearEndFile is the smallest file whose end is watched for: an episode, not a
	// subtitle or a sample.
	minNearEndFile = 100 << 20
	// minPlayedPercent is how much of a file a handle has to have read without jumping for
	// its end to mean an episode is finishing: a player skipping an intro still does, a
	// library app reading the last minutes for its credits doesn't.
	minPlayedPercent = 30
	// maxPlayPace is how many times faster than real time a read can go and still be play: a
	// player buffers ahead, but on average reads at the rate it shows.
	maxPlayPace = 2
	// seqSlack is how far from where the last read ended the next can start and still read on.
	seqSlack = 8 << 20
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
	// Duration is how long the file plays for, read from its header, or 0 when it isn't a
	// video or audio file, or the header hasn't been read (yet).
	Duration time.Duration
	// Cached divides the file into cachedSegments parts, each set when all of its pieces are
	// in the cache.
	Cached   []bool
	Peers    int
	Since    time.Time // when the first handle reading it was opened
	LastRead time.Time
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
			r.Cached = f.cachedSegments()
		}
		if f != nil {
			r.Duration = f.duration()
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

// cachedSegments is how many parts a file's cached map has.
const cachedSegments = 80

// segmentsTTL is how long a file's cached map is kept before it's read again: reading it
// asks storage about every piece.
const segmentsTTL = 10 * time.Second

var segmentCache = struct {
	sync.Mutex
	m map[*torrentFile]segmentEntry
}{m: map[*torrentFile]segmentEntry{}}

type segmentEntry struct {
	at   time.Time
	segs []bool
}

func (f *torrentFile) cachedSegments() []bool {
	now := time.Now()
	segmentCache.Lock()
	e, ok := segmentCache.m[f]
	segmentCache.Unlock()
	if ok && now.Sub(e.at) < segmentsTTL {
		return e.segs
	}
	segs := f.pieces.segments(cachedSegments)
	segmentCache.Lock()
	for k, v := range segmentCache.m {
		if now.Sub(v.at) > time.Minute {
			delete(segmentCache.m, k)
		}
	}
	segmentCache.m[f] = segmentEntry{at: now, segs: segs}
	segmentCache.Unlock()
	return segs
}

// segments divides the file into n parts and reports, for each, whether all of its pieces
// are cached.
func (s *pieceSync) segments(n int) []bool {
	out := make([]bool, n)
	if s.length <= 0 {
		return out
	}
	begin, end := s.pieceRange(0, s.length)
	cached := make([]bool, end-begin)
	for i := range cached {
		cached[i] = s.cached(begin + i)
	}
	for k := range out {
		lo, hi := s.length*int64(k)/int64(n), s.length*int64(k+1)/int64(n)
		b, e := s.pieceRange(lo, max(hi-lo, 1))
		all := e > b
		for i := b; i < e && all; i++ {
			all = cached[i-begin]
		}
		out[k] = all
	}
	return out
}

// durations remembers each file's duration, read once from its header in the background.
var durations = struct {
	sync.Mutex
	m map[*torrentFile]durationEntry
}{m: map[*torrentFile]durationEntry{}}

type durationEntry struct {
	d time.Duration
}

// knownDuration is how long f plays for, if its header has been read, or 0.
func (f *torrentFile) knownDuration() time.Duration {
	durations.Lock()
	defer durations.Unlock()
	return durations.m[f].d
}

// duration is how long f plays for, or 0 while its header is being read, or when it isn't
// a video or audio file. The first ask starts the read.
func (f *torrentFile) duration() time.Duration {
	if !media.IsTimed(f.path) {
		return 0
	}
	durations.Lock()
	e, ok := durations.m[f]
	if !ok {
		durations.m[f] = durationEntry{}
	}
	durations.Unlock()
	if ok {
		return e.d
	}
	go func() {
		h := f.NewHandle()
		h.quiet = true
		d, _ := media.Duration(h, f.len, f.path)
		_ = h.Close()
		durations.Lock()
		durations.m[f] = durationEntry{d: d}
		durations.Unlock()
	}()
	return 0
}
