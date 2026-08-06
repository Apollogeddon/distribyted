package fs

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Apollogeddon/distribyted/internal/iio"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
)

var _ Filesystem = &TorrentFS{}

type TorrentFS struct {
	mu              sync.Mutex
	s               *storage
	ts              map[string]Torrent
	readTimeout     int
	responsiveReads bool
	log             zerolog.Logger
}

func NewTorrent(readTimeout int, responsiveReads bool) *TorrentFS {
	return &TorrentFS{
		s:               newStorage(GetSupportedFactories()),
		ts:              make(map[string]Torrent),
		readTimeout:     readTimeout,
		responsiveReads: responsiveReads,
		log:             dlog.Logger("torrent-fs"),
	}
}

func (fs *TorrentFS) AddTorrent(t Torrent) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	ih := t.InfoHash().HexString()
	fs.ts[ih] = t

	if t.Info() != nil {
		fs.addFiles(t)
		return
	}

	go func() {
		<-t.GotInfo()
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if _, ok := fs.ts[ih]; !ok {
			return // removed while waiting for metadata
		}
		fs.addFiles(t)
	}()
}

func (fs *TorrentFS) addFiles(t Torrent) {
	ih := t.InfoHash().HexString()
	for _, file := range t.Files() {
		tf := &torrentFile{
			hash:       ih,
			file:       file,
			readerFunc: file.NewReader,
			len:        file.Length(),
			timeout:    fs.readTimeout,
			responsive: fs.responsiveReads,
			stats:      &readStats{},
			log:        fs.log.With().Str(dlog.KeyPath, file.Path()).Logger(),
		}
		tf.SetIno(HashIno(ih + file.Path()))
		if err := fs.s.Add(tf, file.Path()); err != nil {
			fs.log.Error().Err(err).Str(dlog.KeyPath, file.Path()).Msg("failed to register file in storage")
		}
	}
}

func (fs *TorrentFS) RemoveTorrent(h string) {
	fs.log.Info().Str(dlog.KeyHash, h).Msg("removing torrent from filesystem")

	fs.mu.Lock()
	delete(fs.ts, h)
	fs.mu.Unlock()

	fs.s.RemoveByHash(h)
}

func (fs *TorrentFS) Open(filename string) (File, error) {
	f, err := fs.s.Get(filename)
	if err != nil {
		return nil, err
	}

	if tf, ok := f.(*torrentFile); ok {
		return tf.NewHandle(), nil
	}

	return f, nil
}

func (fs *TorrentFS) ReadDir(path string) (map[string]File, error) {
	return fs.s.Children(path)
}

func (fs *TorrentFS) Link(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	return fs.s.Add(f, newpath)
}

func (fs *TorrentFS) Rename(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		return err
	}

	return fs.s.Remove(oldpath)
}

func (fs *TorrentFS) Mkdir(path string) error {
	return fs.s.Add(&Dir{}, path)
}

func (fs *TorrentFS) Rmdir(path string) error {
	f, err := fs.s.Get(path)
	if err != nil {
		return err
	}
	if !f.IsDir() {
		return os.ErrInvalid
	}

	return fs.s.Remove(path)
}

func (fs *TorrentFS) Create(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Add(NewMemoryFile(nil), path)
}

func (fs *TorrentFS) Remove(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Remove(path)
}

// reader is what torrentFileHandle talks to instead of a raw torrent.Reader,
// so concurrent Read/ReadAt/Close calls on one handle are actually
// serialized (torrent.Reader itself is documented "not safe for concurrent
// use") and a read that never returns from the underlying library can't
// block every other read on the same handle forever. See readAtWrapper's
// doc comment for the full story.
type reader interface {
	iio.Reader
	// abandoned reports whether this reader gave up on a previous read that
	// blew its hard deadline. Once true it stays true forever: the
	// abandoned goroutine still owns the underlying torrent.Reader and its
	// scratch buffer, so nothing else may ever touch them again.
	abandoned() bool
}

// readStats counts abandoned/recovered/poisoned reads for one torrentFile,
// shared across every handle opened on it (see addFiles). abandoned minus
// recovered is the number of goroutines currently still stuck inside the
// underlying torrent library for this file — otherwise invisible.
type readStats struct {
	abandoned atomic.Int64
	recovered atomic.Int64
	poisoned  atomic.Int64
}

// ErrReadTimeout is returned when a read makes no progress within its
// configured timeout. errReaderAbandoned is returned by a wrapper that has
// already given up on an earlier stuck read and can never be used again —
// torrentFileHandle.Read/ReadAt handle it by transparently retrying once on
// a freshly created reader.
var (
	ErrReadTimeout     = errors.New("torrent read exceeded its deadline")
	errReaderAbandoned = errors.New("torrent reader abandoned after a stuck read")
)

const stallHeartbeat = 10 * time.Second

const (
	stateRunning int32 = iota
	stateDone
	stateAbandoned
)

// readAtWrapper serializes access to a torrent.Reader and enforces a hard,
// caller-controlled wall-clock deadline on every read.
//
// This exists because torrent.Reader (anacrolix/torrent v1.61.0) cannot be
// trusted to honour context cancellation once its internal storage-read
// retry loop has been entered: reader.go's readAt retries a failed read up
// to 3 times, and — when the torrent's storage reports a capacity (true for
// every torrent in this deployment; see cmd/distribyted/main.go's capFunc,
// added for exactly this reason per docs/benchmarking.md) — recurses back
// into itself with no delay and no retry cap if all 3 fail (its own
// comment: "this might cause us to get stuck if we retry for any error").
// Worse, once a caller-supplied context is cancelled, every later
// recursion sees an already-closed ctx.Done() and returns instantly,
// turning the retry into a zero-delay, permanently CPU-pinned recursion —
// Go doesn't optimize tail calls, so every recursion adds a stack frame,
// and the goroutine's stack grows without bound until the process OOMs.
// This was the confirmed root cause of a production OOMKilled crash.
//
// The fix has two parts. First, this wrapper never hands the underlying
// reader a context tied to our own deadline (see ReadContext) — a read
// that legitimately stalls (e.g. a piece the storage's cache evicted out
// from under it) then just parks correctly instead of spinning. Second,
// because that alone doesn't guarantee the underlying call returns (a
// torrent that has genuinely lost all its peers can still legitimately
// block forever, waiting for data that will never arrive), every read runs
// in its own goroutine and this wrapper enforces the deadline itself via
// select, abandoning (not killing — Go cannot kill a goroutine) a read
// that doesn't finish in time.
//
// Once abandoned, the wrapper is permanently dead: the abandoned goroutine
// still owns the underlying torrent.Reader and the scratch buffer it was
// reading into (reusing either while that goroutine might still be inside
// ReadContext, which does unsynchronized field writes, is a real data
// race, not a theoretical one — and writing into a caller-owned buffer
// after the call returns would corrupt a recycled FUSE kernel buffer), so
// every access after abandonment goes through sem/deadFlag rather than
// ever touching r or buf directly. torrentFileHandle.load() detects a dead
// wrapper and builds a fresh one on the next call.
type readAtWrapper struct {
	r       torrent.Reader
	timeout time.Duration
	log     zerolog.Logger
	stats   *readStats

	// sem guards r and buf: whoever holds it exclusively owns the read
	// head. A channel instead of a sync.Mutex because acquisition needs to
	// be abandonable on our own deadline, not just blocking — see doRead.
	// It is never released once the wrapper is marked dead: that is what
	// guarantees nobody else can ever touch r/buf again.
	sem chan struct{}
	buf []byte

	closedFlag atomic.Bool
	deadFlag   atomic.Bool
	deadCh     chan struct{}
	closeOnce  sync.Once
}

func newReadAtWrapper(r torrent.Reader, timeout time.Duration, stats *readStats, l zerolog.Logger) reader {
	if r == nil {
		return nil
	}
	return &readAtWrapper{
		r:       r,
		timeout: timeout,
		log:     l,
		stats:   stats,
		sem:     make(chan struct{}, 1),
		deadCh:  make(chan struct{}),
	}
}

func (rw *readAtWrapper) abandoned() bool { return rw.deadFlag.Load() }

type readResult struct {
	n   int
	err error
}

// doRead performs one read, seeking first if seek is true (ReadAt) or
// continuing from the underlying reader's own position otherwise (Read).
// It returns ErrReadTimeout if rw.timeout elapses without forward progress,
// abandoning the underlying read rather than waiting for it.
//
// min distinguishes the two callers' different contracts: io.ReaderAt (used
// by ReadAt) must return exactly len(p) bytes or an error — a short read is
// itself an error — so ReadAt passes min = len(p), retrying until it's
// filled the buffer. Plain io.Reader (used by Read) is explicitly allowed
// to return fewer bytes than requested with a nil error; forcing it through
// the same len(p) contract turned an ordinary end-of-file partial read into
// a spurious io.ErrUnexpectedEOF, so Read passes min = 1: return as soon as
// there's any forward progress, and let the caller's next Read observe EOF
// on a subsequent call the normal way.
func (rw *readAtWrapper) doRead(p []byte, off int64, seek bool, min int) (int, error) {
	if rw.closedFlag.Load() {
		return 0, io.EOF
	}
	if rw.deadFlag.Load() {
		rw.stats.poisoned.Add(1)
		return 0, errReaderAbandoned
	}

	acquireTimer := time.NewTimer(rw.timeout)
	select {
	case rw.sem <- struct{}{}:
		acquireTimer.Stop()
	case <-rw.deadCh:
		acquireTimer.Stop()
		return 0, errReaderAbandoned
	case <-acquireTimer.C:
		return 0, ErrReadTimeout
	}

	if rw.closedFlag.Load() {
		<-rw.sem
		return 0, io.EOF
	}

	if seek {
		if _, err := rw.r.Seek(off, io.SeekStart); err != nil {
			<-rw.sem
			rw.log.Error().Err(err).Int64("off", off).Msg("Seek failed")
			return 0, err
		}
	}

	if cap(rw.buf) < len(p) {
		rw.buf = make([]byte, len(p))
	}
	buf := rw.buf[:len(p)]

	start := time.Now()
	res := make(chan readResult, 1)
	prog := make(chan struct{}, 1)
	var state atomic.Int32

	go func() {
		n, err := readAtLeast(rw.r, buf, min, prog)
		if state.CompareAndSwap(stateRunning, stateDone) {
			res <- readResult{n, err}
			return
		}
		// Lost the race: doRead already abandoned this read on its
		// deadline. We now permanently own r and buf.
		rw.stats.recovered.Add(1)
		rw.log.Warn().Int64("off", off).
			Int64("recovered_total", rw.stats.recovered.Load()).
			Msg("abandoned torrent read finally returned; closing its reader")
		rw.closeOnce.Do(func() { _ = rw.r.Close() })
	}()

	deadline := time.NewTimer(rw.timeout)
	defer deadline.Stop()
	heartbeat := time.NewTimer(stallHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case r := <-res:
			<-rw.sem
			n := copy(p, buf[:r.n])
			if r.err != nil && r.err != io.EOF {
				rw.log.Error().Err(r.err).Int64("off", off).
					Float64("duration_sec", time.Since(start).Seconds()).
					Msg("ReadAt failed")
			}
			return n, r.err

		case <-prog:
			if !deadline.Stop() {
				<-deadline.C
			}
			deadline.Reset(rw.timeout)

		case <-heartbeat.C:
			rw.log.Warn().
				Int64("off", off).
				Float64("elapsed", time.Since(start).Seconds()).
				Msg("ReadAt still waiting for data (stall heartbeat)")
			heartbeat.Reset(stallHeartbeat)

		case <-deadline.C:
			if !state.CompareAndSwap(stateRunning, stateAbandoned) {
				// The worker finished in the tiny window between the timer
				// firing and us handling it — take its result rather than
				// abandoning a read that actually succeeded.
				r := <-res
				<-rw.sem
				return copy(p, buf[:r.n]), r.err
			}
			rw.markDead(off, len(p), time.Since(start))
			return 0, ErrReadTimeout
		}
	}
}

// markDead permanently poisons the wrapper. Called at most once: only the
// single goroutine holding sem can ever reach this, and sem is never
// released afterward, so no other doRead call can race it here.
func (rw *readAtWrapper) markDead(off int64, length int, elapsed time.Duration) {
	rw.deadFlag.Store(true)
	close(rw.deadCh)
	rw.stats.abandoned.Add(1)
	rw.log.Error().
		Int64("off", off).
		Int("len", length).
		Float64("elapsed", elapsed.Seconds()).
		Int64("abandoned_total", rw.stats.abandoned.Load()).
		Msg("read exceeded hard deadline; abandoning underlying torrent reader")
}

func (rw *readAtWrapper) ReadAt(p []byte, off int64) (int, error) {
	return rw.doRead(p, off, true, len(p))
}

func (rw *readAtWrapper) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return rw.doRead(p, 0, false, 1)
}

// ReadContext honours ctx only for an already-cancelled check up front —
// once a read is underway it is governed solely by rw.timeout. See the
// type doc comment for why handing a caller's context down to the
// underlying torrent.Reader is exactly the bug this wrapper exists to
// avoid.
func (rw *readAtWrapper) ReadContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return rw.Read(p)
}

// readAtLeast reads from r until buf has min bytes, EOF, or a permanent
// error. It never times out on its own — the caller (doRead) owns the
// deadline and abandons the calling goroutine if this doesn't return in
// time — but it does report forward progress on prog after every partial
// read, non-blocking, so the caller can extend its deadline accordingly.
func readAtLeast(r missinggo.ReadContexter, buf []byte, min int, prog chan<- struct{}) (n int, err error) {
	if len(buf) < min {
		return 0, io.ErrShortBuffer
	}
	for n < min && err == nil {
		var nn int
		nn, err = r.ReadContext(context.Background(), buf[n:])
		n += nn
		if nn > 0 && prog != nil {
			select {
			case prog <- struct{}{}:
			default:
			}
		}
	}
	if n >= min {
		err = nil
	} else if n > 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

// Close waits for any in-flight read to finish or hit its own deadline
// (never longer — every read self-terminates), then closes the underlying
// reader exactly once. If the in-flight read is abandoned while Close is
// waiting, Close returns immediately: the abandoned goroutine owns the
// underlying reader now, and closing it here would race that goroutine's
// own use of it.
func (rw *readAtWrapper) Close() error {
	if rw.closedFlag.Swap(true) {
		return nil
	}
	if rw.deadFlag.Load() {
		return nil
	}

	select {
	case rw.sem <- struct{}{}:
		defer func() { <-rw.sem }()
		if rw.deadFlag.Load() {
			return nil
		}
		var err error
		rw.closeOnce.Do(func() { err = rw.r.Close() })
		return err
	case <-rw.deadCh:
		return nil
	}
}

var _ File = &torrentFile{}

type torrentFile struct {
	BaseFile
	hash       string
	file       *torrent.File
	readerFunc func() torrent.Reader
	len        int64
	timeout    int
	responsive bool
	stats      *readStats
	log        zerolog.Logger
}

func (d *torrentFile) NewHandle() *torrentFileHandle {
	return &torrentFileHandle{
		torrentFile: d,
	}
}

func (d *torrentFile) Size() int64 {
	return d.len
}

func (d *torrentFile) IsDir() bool {
	return false
}

func (d *torrentFile) Close() error {
	return nil
}

func (d *torrentFile) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (d *torrentFile) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, io.EOF
}

func (d *torrentFile) MatchHash(hash string) bool {
	return d.hash == hash
}

func (d *torrentFile) Hash() string {
	return d.hash
}

var _ File = &torrentFileHandle{}

type torrentFileHandle struct {
	*torrentFile
	reader reader
	mu     sync.Mutex
	closed bool
}

// load returns the current reader, creating it on first call, or nil if the
// handle has been closed. The caller must use only the returned value for
// the rest of its operation — never re-read h.reader afterward — since a
// concurrent Close() nils h.reader at any time. Re-reading the field later
// (a previously-fixed bug here) let Close() race between a caller's
// nil-check and its use, causing a nil-interface panic on the following
// method call.
//
// The closed flag (rather than just checking h.reader == nil) exists so
// that once Close() has run, load() can never reconstruct a fresh reader
// behind it: without it, a Read/ReadAt racing after Close() would see
// h.reader == nil — indistinguishable from "not yet loaded" — and silently
// reopen a new underlying torrent reader for an already-closed handle.
//
// load() also discards and replaces a reader that has abandoned a stuck
// read (readAtWrapper.abandoned()): that reader is permanently dead — its
// underlying torrent.Reader and scratch buffer now belong to whatever
// goroutine it abandoned, forever — so it must never be returned again,
// but the handle itself should keep working via a freshly created one.
// This does not reintroduce the nil-race above: the check happens inside
// load() under h.mu, load() still returns a single value, and a stale
// abandoned reference a caller might still be holding can neither
// nil-deref (no field on it is ever nil'd) nor block (it returns
// errReaderAbandoned from an atomic load) — strictly safer than the
// original failure mode.
//
// readahead is a static prefetch window applied to every newly opened
// torrent.Reader (see load() below). Left unset, torrent.Reader's default
// readahead function computes readahead as (current position - contiguous
// read start position) — i.e. it starts at *zero* on every fresh Open/Seek
// and only grows as a read continues contiguously. That default is exactly
// why a freshly opened stream is slow to start: the first read gets almost
// no prefetch and has to wait piece-by-piece. A modest static window fixes
// the cold-start case without being so large that briefly-opened files
// (e.g. a media server probing container headers) waste bandwidth
// prefetching data nobody reads, or that multiple simultaneously-open files
// starve each other for swarm bandwidth.
//
// Tried and reverted: swapping this for a SetReadaheadFunc that grows the
// window with contiguous progress (up to a ceiling) to help sustained
// throughput. Measured no improvement on sequential throughput, and a
// statistically significant time-to-first-byte regression (+67% cable,
// +27% DSL) — the per-call function invocation this requires runs under the
// client-wide lock on every Read/Seek, unlike a plain static field read.
// See docs/benchmarking.md.
const readahead = 4 * 1024 * 1024 // 4MB

// responsive (torrentFile.responsive, set from config.TorrentGlobal.ResponsiveReads)
// controls torrent.Reader.SetResponsive(): a read normally waits for its
// covering piece to finish downloading AND pass hash verification before
// returning. Responsive mode returns as soon as the covering chunks have
// arrived, skipping that wait — faster, especially with large piece
// lengths, but the returned bytes have not yet been confirmed to match the
// torrent's hash. Off by default for that reason; see docs/benchmarking.md
// for the measured latency trade-off.

func (h *torrentFileHandle) load() reader {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if h.reader != nil && h.reader.abandoned() {
		h.log.Warn().Msg("discarding abandoned torrent reader; a fresh one will be created")
		h.reader = nil
	}
	if h.reader == nil {
		r := h.readerFunc()
		if r != nil {
			r.SetReadahead(readahead)
			if h.responsive {
				r.SetResponsive()
			}
		}
		h.reader = newReadAtWrapper(r, time.Duration(h.timeout)*time.Second, h.stats, h.log)
	}
	return h.reader
}

// Read and ReadAt retry exactly once, and only when the reader load()
// handed back turns out to have been poisoned by a stuck read (possibly
// from a concurrent caller sharing this same handle) — never on the
// caller's own ErrReadTimeout, which must be surfaced immediately rather
// than spend a second deadline.
func (h *torrentFileHandle) Read(p []byte) (n int, err error) {
	r := h.load()
	if r == nil {
		return 0, io.EOF
	}
	n, err = r.Read(p)
	if errors.Is(err, errReaderAbandoned) {
		if r2 := h.load(); r2 != nil && r2 != r {
			return r2.Read(p)
		}
	}
	return n, err
}

func (h *torrentFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	r := h.load()
	if r == nil {
		return 0, io.EOF
	}
	n, err = r.ReadAt(p, off)
	if errors.Is(err, errReaderAbandoned) {
		if r2 := h.load(); r2 != nil && r2 != r {
			return r2.ReadAt(p, off)
		}
	}
	return n, err
}

func (h *torrentFileHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.reader != nil {
		err := h.reader.Close()
		h.reader = nil
		return err
	}
	return nil
}
