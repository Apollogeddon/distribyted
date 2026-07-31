package fs

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/internal/iio"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
)

var _ Filesystem = &TorrentFS{}

type TorrentFS struct {
	mu          sync.Mutex
	s           *storage
	ts          map[string]Torrent
	readTimeout int
	log         zerolog.Logger
}

func NewTorrent(readTimeout int) *TorrentFS {
	return &TorrentFS{
		s:           newStorage(GetSupportedFactories()),
		ts:          make(map[string]Torrent),
		readTimeout: readTimeout,
		log:         dlog.Logger("torrent-fs"),
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

type reader interface {
	iio.Reader
	missinggo.ReadContexter
}

type readAtWrapper struct {
	timeout int
	mu      sync.Mutex
	closed  bool

	file    *torrent.File
	lastOff int64
	lastLen int

	torrent.Reader
	io.ReaderAt
	io.Closer

	log zerolog.Logger
}

func newReadAtWrapper(r torrent.Reader, file *torrent.File, timeout int, l zerolog.Logger) reader {
	if r == nil {
		return nil
	}
	return &readAtWrapper{Reader: r, file: file, timeout: timeout, log: l}
}

func (rw *readAtWrapper) ReadAt(p []byte, off int64) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.closed {
		return 0, io.EOF
	}

	rw.log.Debug().Int64("off", off).Int("len", len(p)).Msg("ReadAt request")

	rw.lastOff = off
	rw.lastLen = len(p)

	_, err := rw.Seek(off, io.SeekStart)
	if err != nil {
		rw.log.Error().Err(err).Msg("Seek failed")
		return 0, err
	}

	start := time.Now()
	rw.log.Debug().Int64("off", off).Int("len", len(p)).Msg("ReadAt started")

	// Start a stall-watcher for this specific request
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				rw.log.Warn().
					Int64("off", off).
					Float64("elapsed", time.Since(start).Seconds()).
					Msg("ReadAt still waiting for data (stall heartbeat)")
			}
		}
	}()

	n, err := readAtLeast(rw, rw.timeout, p, len(p), rw.log)
	close(done)

	elapsed := time.Since(start)
	if err != nil && err != io.EOF {
		rw.log.Error().
			Err(err).
			Int64("off", off).
			Float64("duration_sec", elapsed.Seconds()).
			Msg("ReadAt failed")
	} else {
		rw.log.Debug().
			Int64("off", off).
			Int("read", n).
			Float64("duration_sec", elapsed.Seconds()).
			Msg("ReadAt finished")
	}
	return n, err
}

var timerPool = sync.Pool{
	New: func() interface{} {
		t := time.NewTimer(time.Hour)
		t.Stop()
		return t
	},
}

// withReadTimeout runs fn with a context cancelled after timeout, calling warn
// if the deadline is what triggered the cancellation. The pooled timer is
// only returned to timerPool after the watchdog goroutine has fully exited
// (not merely been signalled to via cancel), so a subsequent borrower can
// never Reset() a timer a stale watchdog might still be reading from.
func withReadTimeout(timeout int, warn func(), fn func(ctx context.Context) (int, error)) (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timer := timerPool.Get().(*time.Timer)
	timer.Reset(time.Duration(timeout) * time.Second)

	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		select {
		case <-timer.C:
			warn()
			cancel()
		case <-ctx.Done():
		}
	}()

	n, err := fn(ctx)

	cancel()
	<-watchdogDone

	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timerPool.Put(timer)

	return n, err
}

func readAtLeast(r missinggo.ReadContexter, timeout int, buf []byte, min int, l zerolog.Logger) (n int, err error) {
	if len(buf) < min {
		return 0, io.ErrShortBuffer
	}
	for n < min && err == nil {
		gotSoFar := n
		var nn int
		nn, err = withReadTimeout(timeout, func() {
			l.Warn().Int("min", min).Int("got", gotSoFar).Msg("read operation timing out")
		}, func(ctx context.Context) (int, error) {
			return r.ReadContext(ctx, buf[n:])
		})
		n += nn
	}
	if n >= min {
		err = nil
	} else if n > 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (rw *readAtWrapper) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.closed {
		return nil
	}

	rw.closed = true
	return rw.Reader.Close()
}

var _ File = &torrentFile{}

type torrentFile struct {
	BaseFile
	hash       string
	file       *torrent.File
	readerFunc func() torrent.Reader
	len        int64
	timeout    int
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
const readahead = 4 * 1024 * 1024 // 4MB

func (h *torrentFileHandle) load() reader {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if h.reader == nil {
		r := h.readerFunc()
		if r != nil {
			r.SetReadahead(readahead)
		}
		h.reader = newReadAtWrapper(r, h.file, h.timeout, h.log)
	}
	return h.reader
}

func (h *torrentFileHandle) Read(p []byte) (n int, err error) {
	r := h.load()
	if r == nil {
		return 0, io.EOF
	}
	return withReadTimeout(h.timeout, func() {
		h.log.Warn().Msg("Read handle timeout")
	}, func(ctx context.Context) (int, error) {
		return r.ReadContext(ctx, p)
	})
}

func (h *torrentFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	r := h.load()
	if r == nil {
		return 0, io.EOF
	}
	return r.ReadAt(p, off)
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
