package torrent

import (
	"context"
	"io"
	"sync"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"
)

const (
	// freeFailedReads is how many reads in a row may fail on a piece before each further
	// failure waits, from minFailedReadWait doubling up to maxFailedReadWait.
	freeFailedReads   = 8
	minFailedReadWait = 10 * time.Millisecond
	maxFailedReadWait = time.Second
)

// GuardReads wraps piece storage against a piece the storage calls complete but can't read.
// anacrolix/torrent retries such a read by recursing with no delay and no limit when the
// storage has a capacity, spinning until the process runs out of memory. Past a few failures,
// each failed read now waits a little longer, so a failure that clears (a busy disk) costs a
// short delay; one that lasts giveUpAfter makes the piece incomplete, and the read then waits
// for it to be downloaded again rather than retrying.
func GuardReads(st storage.ClientImpl, giveUpAfter time.Duration) storage.ClientImpl {
	return readGuardClient{ClientImpl: st, giveUpAfter: giveUpAfter}
}

type readGuardClient struct {
	storage.ClientImpl
	giveUpAfter time.Duration
}

func (c readGuardClient) OpenTorrent(ctx context.Context, info *metainfo.Info, ih metainfo.Hash) (storage.TorrentImpl, error) {
	t, err := c.ClientImpl.OpenTorrent(ctx, info, ih)
	if err != nil {
		return t, err
	}
	pieces := &pieceReadFailures{failed: make(map[int]*readFailures), giveUpAfter: c.giveUpAfter, now: time.Now}
	if piece := t.Piece; piece != nil {
		t.Piece = func(p metainfo.Piece) storage.PieceImpl {
			return guardPiece(piece(p), p.Index(), pieces)
		}
	}
	if piece := t.PieceWithHash; piece != nil {
		t.PieceWithHash = func(p metainfo.Piece, hash g.Option[[]byte]) storage.PieceImpl {
			return guardPiece(piece(p, hash), p.Index(), pieces)
		}
	}
	return t, nil
}

// pieceReadFailures tracks each piece's reads that failed in a row; it holds at most one
// entry per piece of the torrent.
type pieceReadFailures struct {
	mu          sync.Mutex
	failed      map[int]*readFailures
	giveUpAfter time.Duration
	now         func() time.Time
}

type readFailures struct {
	count      int
	since      time.Time
	distrusted bool
}

// fail records a failed read. It returns how long the reader should wait before going on,
// and whether the piece has now failed for so long that it no longer counts as complete.
func (f *pieceReadFailures) fail(i int) (wait time.Duration, giveUp bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	r, ok := f.failed[i]
	if !ok {
		r = &readFailures{since: now}
		f.failed[i] = r
	}
	r.count++
	switch {
	case r.distrusted || r.count <= freeFailedReads:
		return 0, false
	case now.Sub(r.since) >= f.giveUpAfter:
		r.distrusted = true
		return 0, true
	}
	return min(minFailedReadWait<<min(r.count-freeFailedReads-1, 16), maxFailedReadWait), false
}

func (f *pieceReadFailures) reset(i int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failed, i)
}

func (f *pieceReadFailures) distrusted(i int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.failed[i]
	return ok && r.distrusted
}

func guardPiece(p storage.PieceImpl, index int, f *pieceReadFailures) storage.PieceImpl {
	if p == nil {
		return nil
	}
	gp := &guardedPiece{PieceImpl: p, index: index, failures: f}
	// keep the faster whole-piece copy the storage may offer
	if _, ok := p.(io.WriterTo); ok {
		return guardedPieceWriterTo{gp}
	}
	return gp
}

type guardedPiece struct {
	storage.PieceImpl
	index    int
	failures *pieceReadFailures
}

func (p *guardedPiece) ReadAt(b []byte, off int64) (int, error) {
	n, err := p.PieceImpl.ReadAt(b, off)
	p.record(n, err)
	return n, err
}

func (p *guardedPiece) record(n int, err error) {
	if n > 0 || err == nil {
		p.failures.reset(p.index)
		return
	}
	wait, giveUp := p.failures.fail(p.index)
	if !giveUp {
		time.Sleep(wait)
		return
	}
	log.Warn().Int("piece", p.index).Err(err).
		Msg("piece storage calls this piece complete, but it can't be read: downloading it again")
	if err := p.MarkNotComplete(); err != nil {
		log.Warn().Int("piece", p.index).Err(err).Msg("problem marking the piece incomplete")
	}
}

func (p *guardedPiece) Completion() storage.Completion {
	c := p.PieceImpl.Completion()
	if c.Complete && p.failures.distrusted(p.index) {
		return storage.Completion{Ok: true}
	}
	return c
}

// MarkComplete follows a download that passed its hash check, so the piece is trusted again.
func (p *guardedPiece) MarkComplete() error {
	p.failures.reset(p.index)
	return p.PieceImpl.MarkComplete()
}

// NewReader keeps the storage's own piece reader, counting its reads as ReadAt does.
func (p *guardedPiece) NewReader() (storage.PieceReader, error) {
	pr, ok := p.PieceImpl.(storage.PieceReaderer)
	if !ok {
		return guardedReader{ReaderAt: p, Closer: io.NopCloser(nil)}, nil
	}
	r, err := pr.NewReader()
	if err != nil {
		return nil, err
	}
	return guardedReader{ReaderAt: readerAtFunc(func(b []byte, off int64) (int, error) {
		n, err := r.ReadAt(b, off)
		p.record(n, err)
		return n, err
	}), Closer: r}, nil
}

type guardedPieceWriterTo struct {
	*guardedPiece
}

func (p guardedPieceWriterTo) WriteTo(w io.Writer) (int64, error) {
	return p.PieceImpl.(io.WriterTo).WriteTo(w)
}

type guardedReader struct {
	io.ReaderAt
	io.Closer
}

type readerAtFunc func([]byte, int64) (int, error)

func (f readerAtFunc) ReadAt(b []byte, off int64) (int, error) { return f(b, off) }
