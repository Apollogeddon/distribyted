package fs

import "time"

// A player opening a film reads its start, and, for most containers, its end, where MKV keeps
// its cues and an MP4 can keep its index, before it shows a frame. Preparing a file has the
// client download those ahead of time, or the whole file, so playing it starts at once.

const (
	// prepareStart is how much of a file's start is downloaded: the header and the first
	// seconds of play at a remux's bitrate.
	prepareStart = 32 * 1024 * 1024
	// prepareEnd is how much of its end is: the index a player looks for there.
	prepareEnd = 8 * 1024 * 1024
)

// Preparation is how much of a file was asked for ahead of a read.
type Preparation int32

const (
	PrepareNone Preparation = iota
	// PrepareStart is its start and end, enough for a player to open it and start playing.
	PrepareStart
	// PrepareAll is the whole file.
	PrepareAll
	// PrepareLead is its first Request.Lead bytes and its end: enough to start the next
	// episode at once, and play its first minutes while the rest downloads.
	PrepareLead
)

// Request is what to download of a file ahead of a read, and why.
type Request struct {
	What Preparation
	// Lead is how many bytes of the file's start PrepareLead downloads (LeadFor).
	Lead int64
	Why  Reason
}

const (
	// leadPlay is how much of the next episode's playback PrepareLead downloads: its first
	// minutes, while the rest downloads as it plays.
	leadPlay = 5 * time.Minute
	// minLead and maxLead bound the lead: at least a few pieces of the start, and at most
	// what a remux's first minutes need, whatever its size.
	minLead = 64 << 20
	maxLead = 1 << 30
	// leadPercent and maxGuessedLead are the lead of a file whose duration isn't known.
	leadPercent    = 5
	maxGuessedLead = 512 << 20
)

// LeadFor is how much of the start of a file like one size bytes long that plays for d to
// download ahead: leadPlay of it at its bitrate, or, with d unknown, leadPercent of it, up to
// maxGuessedLead.
func LeadFor(size int64, d time.Duration) int64 {
	if size <= 0 {
		return 0
	}
	if d <= 0 {
		return min(size*leadPercent/100, maxGuessedLead)
	}
	perSecond := float64(size) / d.Seconds()
	return min(max(int64(perSecond*leadPlay.Seconds()), minLead), maxLead)
}

// Readiness is how much of a torrent's file is in the piece cache, ready to read without
// waiting on a peer.
type Readiness struct {
	Size int64
	// Cached is how many of its bytes are in the cache.
	Cached int64
	// Start and End report whether the parts a player reads first, its first prepareStart
	// bytes and last prepareEnd, are all in the cache.
	Start, End bool
	// Asked is how much was last asked for (Prepare), and Lead, for PrepareLead, how many
	// bytes of the start.
	Asked Preparation
	Lead  int64
	// Peers is how many peers the torrent is connected to.
	Peers int
}

// Ready reports whether a player can open the file and start playing without waiting.
func (r Readiness) Ready() bool { return r.Start && r.End }

// Preparer is a file that can be downloaded ahead of a read: a torrent's file, not a folder,
// a file made through the mount, or a file inside an archive.
type Preparer interface {
	// Prepare has the client download r.What of the file, or, with PrepareNone, stop
	// downloading what isn't being read. It reports false when the file has no torrent.
	Prepare(r Request) bool
	// Readiness counts every piece of the file.
	Readiness() (Readiness, bool)
	// StartReady reports whether its start and end are cached (Readiness.Ready), reading a
	// few pieces' state: cheap enough for every file in a listing.
	StartReady() (ready, ok bool)
}

var _ Preparer = &torrentFile{}

// pieceRange is the pieces [begin, end) holding the file's bytes [off, off+n).
func (s *pieceSync) pieceRange(off, n int64) (begin, end int) {
	stop := min(off+n, s.length)
	off = max(off, 0)
	if stop <= off {
		return 0, 0
	}
	return s.pieceAt(off), s.pieceAt(stop-1) + 1
}

// startRanges are the pieces holding a file's first prepareStart bytes, and its last
// prepareEnd; they overlap for a small file.
func (s *pieceSync) startRanges() [2][2]int {
	b1, e1 := s.pieceRange(0, prepareStart)
	b2, e2 := s.pieceRange(s.length-prepareEnd, prepareEnd)
	return [2][2]int{{b1, e1}, {b2, e2}}
}

// leadRanges are the pieces holding a file's first lead bytes, at least one piece, and its
// last prepareEnd bytes.
func (s *pieceSync) leadRanges(lead int64) [2][2]int {
	b1, e1 := s.pieceRange(0, max(lead, 1))
	return [2][2]int{{b1, e1}, s.startRanges()[1]}
}

// StartPieces are the pieces [begin, end) holding the first prepareStart and last prepareEnd
// bytes of a file at offset in a torrent of pieceLen pieces: what a player reads before it
// shows a frame.
func StartPieces(offset, length, pieceLen int64) [2][2]int {
	if length <= 0 || pieceLen <= 0 {
		return [2][2]int{}
	}
	return (&pieceSync{offset: offset, length: length, pieceLen: pieceLen}).startRanges()
}

// cached reports whether piece i is in the piece cache. The client counts a piece the cache
// evicted complete (see pieceSync), so storage is asked, without the client's lock.
func (s *pieceSync) cached(i int) bool {
	if c := s.t.Piece(i).Storage().Completion(); c.Ok {
		return c.Complete
	}
	return s.t.PieceState(i).Complete
}

// cachedRange reports whether every piece in [begin, end) is cached.
func (s *pieceSync) cachedRange(begin, end int) bool {
	for i := begin; i < end; i++ {
		if !s.cached(i) {
			return false
		}
	}
	return true
}

// cachedBytes is how many of the file's bytes are in cached pieces.
func (s *pieceSync) cachedBytes() int64 {
	begin, end := s.pieceRange(0, s.length)
	var n int64
	for i := begin; i < end; i++ {
		if !s.cached(i) {
			continue
		}
		lo := max(int64(i)*s.pieceLen, s.offset)
		hi := min(int64(i+1)*s.pieceLen, s.offset+s.length)
		n += hi - lo
	}
	return n
}

func (d *torrentFile) Prepare(req Request) bool {
	s := d.pieces
	if s == nil {
		return false
	}
	d.asked.Store(int32(req.What))
	d.askedLead.Store(req.Lead)
	if req.What == PrepareNone {
		begin, end := s.pieceRange(0, s.length)
		s.t.CancelPieces(begin, end)
		unwantPieces(d.hash, begin, end)
		return true
	}

	ranges := [][2]int{}
	switch req.What {
	case PrepareAll:
		begin, end := s.pieceRange(0, s.length)
		ranges = append(ranges, [2]int{begin, end})
	case PrepareLead:
		r := s.leadRanges(req.Lead)
		ranges = append(ranges, r[0], r[1])
	default:
		r := s.startRanges()
		ranges = append(ranges, r[0], r[1])
	}
	for _, r := range ranges {
		// evicted pieces the client counts complete would be skipped
		s.sync(int64(r[0])*s.pieceLen-s.offset, int64(r[1]-r[0])*s.pieceLen)
		var missing []int
		for i := r[0]; i < r[1]; i++ {
			if !s.cached(i) {
				missing = append(missing, i)
			}
		}
		wantPieces(d.hash, missing, req.Why)
		s.t.DownloadPieces(r[0], r[1])
	}
	// the torrent may have let its peers go while it sat unread
	if d.onReadStart != nil {
		d.onReadStart(d.hash)
	}
	return true
}

func (d *torrentFile) Readiness() (Readiness, bool) {
	s := d.pieces
	if s == nil {
		return Readiness{}, false
	}
	r := s.startRanges()
	return Readiness{
		Size:   d.len,
		Cached: s.cachedBytes(),
		Start:  s.cachedRange(r[0][0], r[0][1]),
		End:    s.cachedRange(r[1][0], r[1][1]),
		Asked:  Preparation(d.asked.Load()),
		Lead:   d.askedLead.Load(),
		Peers:  s.t.Stats().ActivePeers,
	}, true
}

func (d *torrentFile) StartReady() (bool, bool) {
	s := d.pieces
	if s == nil {
		return false, false
	}
	r := s.startRanges()
	return s.cachedRange(r[0][0], r[0][1]) && s.cachedRange(r[1][0], r[1][1]), true
}
