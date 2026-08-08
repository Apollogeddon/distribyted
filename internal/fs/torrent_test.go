package fs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/rs/zerolog"

	"github.com/stretchr/testify/require"
)

const testMagnet = "magnet:?xt=urn:btih:a88fda5954e89178c372716a6a78b8180ed4dad3&dn=The+WIRED+CD+-+Rip.+Sample.+Mash.+Share&tr=udp%3A%2F%2Fexplodie.org%3A6969&tr=udp%3A%2F%2Ftracker.coppersurfer.tk%3A6969&tr=udp%3A%2F%2Ftracker.empire-js.us%3A1337&tr=udp%3A%2F%2Ftracker.leechers-paradise.org%3A6969&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337&tr=wss%3A%2F%2Ftracker.btorrent.xyz&tr=wss%3A%2F%2Ftracker.fastcast.nz&tr=wss%3A%2F%2Ftracker.openwebtorrent.com&ws=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2F&xs=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2Fwired-cd.torrent"

var Cli *torrent.Client

func TestMain(m *testing.M) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = os.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true

	client, err := torrent.NewClient(cfg)
	if err != nil {
		panic(err)
	}

	Cli = client

	exitVal := m.Run()

	client.Close()

	os.Exit(exitVal)
}

func TestTorrentFilesystem(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a real magnet in short mode")
	}
	require := require.New(t)

	to, err := Cli.AddMagnet(testMagnet)
	require.NoError(err)

	<-to.GotInfo()

	tfs := NewTorrent(600, false)
	tfs.AddTorrent(TorrentWrapper{to})

	files, err := tfs.ReadDir("/")
	require.NoError(err)
	require.Len(files, 1)
	require.Contains(files, "The WIRED CD - Rip. Sample. Mash. Share")

	files, err = tfs.ReadDir("/The WIRED CD - Rip. Sample. Mash. Share")
	require.NoError(err)
	require.Len(files, 18)

	f, err := tfs.Open("/The WIRED CD - Rip. Sample. Mash. Share/not_existing_file.txt")
	require.Equal(os.ErrNotExist, err)
	require.Nil(f)

	f, err = tfs.Open("/The WIRED CD - Rip. Sample. Mash. Share/01 - Beastie Boys - Now Get Busy.mp3")
	require.NoError(err)
	require.NotNil(f)
	require.Equal(f.Size(), int64(1964275))
	require.False(f.IsDir())

	b := make([]byte, 10)

	n, err := f.Read(b)
	require.NoError(err)
	require.Equal(10, n)
	require.Equal([]byte{0x49, 0x44, 0x33, 0x3, 0x0, 0x0, 0x0, 0x0, 0x1f, 0x76}, b)

	n, err = f.ReadAt(b, 10)
	require.NoError(err)
	require.Equal(10, n)

	n, err = f.ReadAt(b, 10000)
	require.NoError(err)
	require.Equal(10, n)

	// Test Torrent extra methods
	err = tfs.Mkdir("/newdir")
	require.NoError(err)

	err = tfs.Link("/The WIRED CD - Rip. Sample. Mash. Share/01 - Beastie Boys - Now Get Busy.mp3", "/linked.mp3")
	require.NoError(err)

	err = tfs.Rename("/linked.mp3", "/renamed.mp3")
	require.NoError(err)

	err = tfs.Rmdir("/newdir")
	require.NoError(err)

	// Error cases
	require.Error(tfs.Link("/notexists", "/target"))
	require.Error(tfs.Rename("/notexists", "/target"))
	require.Error(tfs.Rmdir("/notexists"))

	err = tfs.Rmdir("/renamed.mp3")
	require.Error(err)

	tfs.RemoveTorrent(to.InfoHash().HexString())
	files, err = tfs.ReadDir("/")
	require.NoError(err)
	require.Len(files, 0)

	require.NoError(f.Close())
}

func TestReadAtTorrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a real magnet in short mode")
	}
	require := require.New(t)

	to, err := Cli.AddMagnet(testMagnet)
	require.NoError(err)

	<-to.GotInfo()
	torrFile := to.Files()[0]

	tf := torrentFile{
		readerFunc: torrFile.NewReader,
		len:        torrFile.Length(),
		timeout:    500,
		stats:      &readStats{},
	}
	h := tf.NewHandle()
	defer func() { _ = h.Close() }()

	toRead := make([]byte, 5)
	n, err := h.ReadAt(toRead, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x0, 0x0, 0x1f, 0x76, 0x54}, toRead)

	n, err = h.ReadAt(toRead, 0)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x49, 0x44, 0x33, 0x3, 0x0}, toRead)
}

func TestReadAtWrapper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a real magnet in short mode")
	}
	t.Parallel()

	require := require.New(t)

	to, err := Cli.AddMagnet(testMagnet)
	require.NoError(err)

	<-to.GotInfo()
	torrFile := to.Files()[0]

	r := newReadAtWrapper(torrFile.NewReader(), 10*time.Second, &readStats{}, zerolog.Nop())
	defer func() { _ = r.Close() }()

	toRead := make([]byte, 5)
	n, err := r.ReadAt(toRead, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x0, 0x0, 0x1f, 0x76, 0x54}, toRead)

	n, err = r.ReadAt(toRead, 0)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x49, 0x44, 0x33, 0x3, 0x0}, toRead)

	_ = r.Close()
	n, err = r.ReadAt(toRead, 0)
	require.Equal(0, n)
	require.Equal(io.EOF, err)
}

// TestTorrentFS_ConcurrentAddRemove reproduces removing a torrent while its
// metadata is still arriving: RemoveTorrent used to range fs.s.files without
// storage's own lock while the GotInfo goroutine wrote it concurrently
// (fatal error: concurrent map iteration and map write). It also guards
// against the file resurrecting if metadata arrives after the removal.
func TestTorrentFS_ConcurrentAddRemove(t *testing.T) {
	for i := 0; i < 20; i++ {
		dir := t.TempDir()
		filePath := filepath.Join(dir, fmt.Sprintf("concurrent-%d.txt", i))
		content := []byte(fmt.Sprintf("concurrent add/remove test data %d", i))
		require.NoError(t, os.WriteFile(filePath, content, 0644))

		info := metainfo.Info{PieceLength: 256 * 1024}
		require.NoError(t, info.BuildFromFilePath(filePath))

		infoBytes, err := bencode.Marshal(info)
		require.NoError(t, err)
		ih := infohash.HashBytes(infoBytes)

		to, _ := Cli.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: ih})

		tfs := NewTorrent(5, false)
		tfs.AddTorrent(TorrentWrapper{to})

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			_ = to.SetInfoBytes(infoBytes)
		}()

		go func() {
			defer wg.Done()
			<-start
			tfs.RemoveTorrent(ih.HexString())
		}()

		close(start)
		wg.Wait()

		require.Eventually(t, func() bool {
			files, err := tfs.ReadDir("/")
			return err == nil && len(files) == 0
		}, 2*time.Second, 10*time.Millisecond, "file should not be present regardless of race outcome")

		to.Drop()
	}
}

// multiChunkReader is a minimal missinggo.ReadContexter returning one
// pre-set chunk per call, then io.EOF.
type multiChunkReader struct {
	chunks [][]byte
	i      int
}

func (m *multiChunkReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	if m.i >= len(m.chunks) {
		return 0, io.EOF
	}
	n := copy(p, m.chunks[m.i])
	m.i++
	return n, nil
}

func TestReadAtLeast(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// test short buffer error
	n, err := readAtLeast(nil, make([]byte, 1), 2, nil)
	require.Equal(0, n)
	require.ErrorIs(err, io.ErrShortBuffer)
}

// TestReadAtLeast_ReportsProgressAndAccumulates guards readAtLeast's basic
// contract: keep calling ReadContext until min bytes are gathered,
// signalling prog (non-blockingly) after every partial read.
//
// This replaces a previous test (TestReadAtLeast_PooledTimerNotStolen) that
// guarded a race in a since-removed pooled *time.Timer shared between
// readAtLeast's own per-iteration watchdog goroutine and its caller (see
// BACKLOG.md, "Pooled timer released before its watchdog stops", and commit
// a10f063). That whole mechanism — a per-iteration timeout inside
// readAtLeast, with a watchdog goroutine racing a caller over a pooled
// timer — no longer exists: timeout enforcement moved entirely to
// readAtWrapper.doRead, which owns its own timers and never shares one
// across goroutines, so the bug class that test guarded (a stale watchdog
// reading from a timer a new caller had already Reset()) is now
// structurally impossible rather than merely fixed.
func TestReadAtLeast_ReportsProgressAndAccumulates(t *testing.T) {
	multi := &multiChunkReader{chunks: [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}}
	buf := make([]byte, 6)
	prog := make(chan struct{}, 1)

	n, err := readAtLeast(multi, buf, 6, prog)
	require.NoError(t, err)
	require.Equal(t, 6, n)
	require.Equal(t, "abcdef", string(buf))

	select {
	case <-prog:
	default:
		t.Fatal("expected at least one non-blocking progress signal")
	}
}

// raceFakeReader is a minimal `reader` (internal/fs/torrent.go) fake used to
// exercise torrentFileHandle's locking, independent of a real torrent.Reader.
type raceFakeReader struct {
	mu     sync.Mutex
	closed bool
}

func (f *raceFakeReader) readLocked(p []byte) (int, error) {
	time.Sleep(time.Millisecond) // widen the window for a concurrent Close
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func (f *raceFakeReader) Read(p []byte) (int, error)              { return f.readLocked(p) }
func (f *raceFakeReader) ReadAt(p []byte, off int64) (int, error) { return f.readLocked(p) }
func (f *raceFakeReader) abandoned() bool                         { return false }

func (f *raceFakeReader) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// TestTorrentFileHandle_ReadCloseRace guards against a previously-fixed bug:
// Read/ReadAt used to re-read h.reader from the struct after load() released
// h.mu, rather than using the value load() returned. A concurrent Close()
// could nil h.reader in between, so the nil-check would pass but the field
// read moments later for the actual call would see nil, panicking on a
// nil-interface method call. Read/ReadAt now capture load()'s return value
// once and never touch h.reader again, so a race here should only ever
// surface as the harmless io.ErrClosedPipe from raceFakeReader, never a
// panic — this test's only real assertion is "runs clean under -race".
func TestTorrentFileHandle_ReadCloseRace(t *testing.T) {
	for i := 0; i < 200; i++ {
		h := &torrentFileHandle{
			torrentFile: &torrentFile{timeout: 5, stats: &readStats{}, log: zerolog.Nop()},
			reader:      &raceFakeReader{},
		}

		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4)
			_, _ = h.Read(buf)
		}()
		go func() {
			defer wg.Done()
			buf := make([]byte, 4)
			_, _ = h.ReadAt(buf, 0)
		}()
		go func() {
			defer wg.Done()
			_ = h.Close()
		}()
		wg.Wait()
	}
}

// fakeTorrentReader is a minimal torrent.Reader implementation for testing
// load()'s readahead configuration without a real torrent.
type fakeTorrentReader struct {
	readaheadSet  int64
	responsiveSet bool
}

func (f *fakeTorrentReader) SetContext(ctx context.Context)                         {}
func (f *fakeTorrentReader) Read(p []byte) (int, error)                             { return 0, io.EOF }
func (f *fakeTorrentReader) Seek(offset int64, whence int) (int64, error)           { return 0, nil }
func (f *fakeTorrentReader) Close() error                                           { return nil }
func (f *fakeTorrentReader) ReadContext(ctx context.Context, p []byte) (int, error) { return 0, io.EOF }
func (f *fakeTorrentReader) SetReadahead(a int64)                                   { f.readaheadSet = a }
func (f *fakeTorrentReader) SetReadaheadFunc(fn torrent.ReadaheadFunc)              {}
func (f *fakeTorrentReader) SetResponsive()                                         { f.responsiveSet = true }

// TestTorrentFileHandle_Load_SetsReadahead is the regression test for the
// slow-stream-start fix: a freshly opened torrent.Reader must get a static
// readahead window, since the library's default readahead function starts
// at zero on every fresh Open/Seek and only grows as a read continues
// contiguously.
func TestTorrentFileHandle_Load_SetsReadahead(t *testing.T) {
	fake := &fakeTorrentReader{}
	h := &torrentFileHandle{
		torrentFile: &torrentFile{
			timeout:    5,
			stats:      &readStats{},
			log:        zerolog.Nop(),
			readerFunc: func() torrent.Reader { return fake },
		},
	}

	r := h.load()
	require.NotNil(t, r)
	require.Equal(t, int64(readahead), fake.readaheadSet)
	require.False(t, fake.responsiveSet, "responsive must default off")

	// A second load() call must reuse the existing reader, not create (and
	// configure) a new one.
	fake.readaheadSet = 0
	r2 := h.load()
	require.Same(t, r, r2)
	require.Zero(t, fake.readaheadSet)
}

// stubTorrentReader is a configurable torrent.Reader fake for exercising
// readAtWrapper's stuck-read handling without a real torrent or network.
// Its ReadContext deliberately does not select on ctx.Done() while blocked:
// that's what reproduces "underlying reader that never honours
// cancellation and never returns" — the exact anacrolix/torrent v1.61.0
// behavior (see readAtWrapper's doc comment in torrent.go) that motivated
// this whole redesign.
type stubTorrentReader struct {
	block chan struct{} // if non-nil, ReadContext blocks here until closed
	data  []byte        // bytes copied into p once unblocked (or immediately if block == nil)

	reads  atomic.Int64
	closes atomic.Int64
}

func (s *stubTorrentReader) SetContext(context.Context)                {}
func (s *stubTorrentReader) SetReadahead(int64)                        {}
func (s *stubTorrentReader) SetReadaheadFunc(f torrent.ReadaheadFunc)  {}
func (s *stubTorrentReader) SetResponsive()                            {}
func (s *stubTorrentReader) Seek(off int64, whence int) (int64, error) { return off, nil }
func (s *stubTorrentReader) Close() error                              { s.closes.Add(1); return nil }
func (s *stubTorrentReader) Read(p []byte) (int, error) {
	return s.ReadContext(context.Background(), p)
}

func (s *stubTorrentReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	s.reads.Add(1)
	if s.block != nil {
		<-s.block
	}
	return copy(p, s.data), nil
}

// trickleTorrentReader returns 1 byte per ReadContext call after a fixed
// delay, to exercise readAtWrapper's per-progress deadline reset: a single
// read can legitimately take longer than one wrapper timeout, provided it
// keeps making forward progress within each window.
type trickleTorrentReader struct {
	delay  time.Duration
	remain int
}

func (t *trickleTorrentReader) SetContext(context.Context)                {}
func (t *trickleTorrentReader) SetReadahead(int64)                        {}
func (t *trickleTorrentReader) SetReadaheadFunc(torrent.ReadaheadFunc)    {}
func (t *trickleTorrentReader) SetResponsive()                            {}
func (t *trickleTorrentReader) Seek(off int64, whence int) (int64, error) { return off, nil }
func (t *trickleTorrentReader) Close() error                              { return nil }
func (t *trickleTorrentReader) Read(p []byte) (int, error) {
	return t.ReadContext(context.Background(), p)
}

func (t *trickleTorrentReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	if t.remain <= 0 {
		return 0, io.EOF
	}
	time.Sleep(t.delay)
	t.remain--
	p[0] = 'x'
	return 1, nil
}

// TestReadAtWrapper_StuckRead_ReturnsWithinDeadline is the core regression
// test for the OOM fix: a read whose underlying torrent.Reader never
// returns (and never honours context cancellation — see stubTorrentReader)
// must still surface an error within its configured deadline, instead of
// blocking forever while its goroutine's stack grows without bound.
func TestReadAtWrapper_StuckRead_ReturnsWithinDeadline(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}

	timeout := 100 * time.Millisecond
	r := newReadAtWrapper(stub, timeout, &readStats{}, zerolog.Nop())

	start := time.Now()
	n, err := r.ReadAt(make([]byte, 4), 0)
	elapsed := time.Since(start)

	require.Equal(t, 0, n)
	require.ErrorIs(t, err, ErrReadTimeout)
	require.GreaterOrEqual(t, elapsed, timeout)
	require.Less(t, elapsed, 3*timeout)
	require.True(t, r.(*readAtWrapper).abandoned())
}

// TestReadAtWrapper_DoesNotWriteCallerBufferAfterTimeout is the test that
// justifies doRead's scratch buffer: the abandoned worker keeps writing
// into whatever buffer it was given long after ReadAt has returned. If that
// were the caller's own p (e.g. a FUSE kernel read buffer that gets
// recycled for an unrelated file the instant this call returns), the result
// would be memory corruption and cross-file data disclosure, not just a
// stale value.
func TestReadAtWrapper_DoesNotWriteCallerBufferAfterTimeout(t *testing.T) {
	block := make(chan struct{})
	stub := &stubTorrentReader{block: block, data: []byte{0xFF, 0xFF, 0xFF, 0xFF}}

	timeout := 80 * time.Millisecond
	r := newReadAtWrapper(stub, timeout, &readStats{}, zerolog.Nop())

	p := make([]byte, 4)
	for i := range p {
		p[i] = 0xAA
	}

	n, err := r.ReadAt(p, 0)
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, ErrReadTimeout)
	require.Equal(t, []byte{0xAA, 0xAA, 0xAA, 0xAA}, p, "caller buffer must be untouched by a timed-out read")

	close(block) // let the abandoned worker finally proceed
	require.Eventually(t, func() bool { return stub.closes.Load() == 1 }, time.Second, 5*time.Millisecond)

	require.Equal(t, []byte{0xAA, 0xAA, 0xAA, 0xAA}, p, "caller buffer must stay untouched even after the abandoned worker finally completes")
}

// TestReadAtWrapper_SubsequentReadReturnsPoisonedAfterAbandon proves a dead
// wrapper fails fast rather than waiting out another full deadline — the
// second read must not wedge behind the first read's abandoned goroutine.
func TestReadAtWrapper_SubsequentReadReturnsPoisonedAfterAbandon(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}

	r := newReadAtWrapper(stub, 50*time.Millisecond, &readStats{}, zerolog.Nop())

	_, err := r.ReadAt(make([]byte, 4), 0)
	require.ErrorIs(t, err, ErrReadTimeout)

	start := time.Now()
	n, err := r.ReadAt(make([]byte, 4), 0)
	elapsed := time.Since(start)
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, errReaderAbandoned)
	require.Less(t, elapsed, 20*time.Millisecond, "a poisoned wrapper must fail fast, not wait out another deadline")
}

// TestReadAtWrapper_QueuedReadReleasedWhenHolderAbandons is the cascade
// regression test: a second read queued behind a stuck one must be released
// promptly when the holder abandons, not wedged for its own separate full
// deadline counted from when it queued.
func TestReadAtWrapper_QueuedReadReleasedWhenHolderAbandons(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}

	timeout := 400 * time.Millisecond
	r := newReadAtWrapper(stub, timeout, &readStats{}, zerolog.Nop())

	go func() { _, _ = r.ReadAt(make([]byte, 4), 0) }()
	time.Sleep(100 * time.Millisecond) // let read #1 grab sem well before read #2 starts

	start := time.Now()
	n, err := r.ReadAt(make([]byte, 4), 8)
	elapsed := time.Since(start)

	require.Equal(t, 0, n)
	require.ErrorIs(t, err, errReaderAbandoned)
	require.Less(t, elapsed, timeout, "queued read must be released by the holder's abandonment, not wait out its own separate deadline")
}

// TestReadAtWrapper_CloseDuringStuckRead confirms Close is bounded by the
// in-flight read's own deadline (never longer, since every read
// self-terminates) and never touches the underlying reader while a
// goroutine might still be inside it.
func TestReadAtWrapper_CloseDuringStuckRead(t *testing.T) {
	block := make(chan struct{})
	stub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}

	timeout := 150 * time.Millisecond
	r := newReadAtWrapper(stub, timeout, &readStats{}, zerolog.Nop())

	go func() { _, _ = r.ReadAt(make([]byte, 4), 0) }()
	time.Sleep(10 * time.Millisecond)

	start := time.Now()
	err := r.Close()
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Less(t, elapsed, 2*timeout, "Close must be bounded by the stuck read's own deadline")
	require.Equal(t, int64(0), stub.closes.Load(), "must not close a reader a goroutine is still inside")

	close(block)
	require.Eventually(t, func() bool { return stub.closes.Load() == 1 }, time.Second, 5*time.Millisecond)
}

// TestReadAtWrapper_CloseDuringNormalRead preserves the pre-redesign
// contract: Close waits out a genuinely in-flight (non-stuck) read, then
// closes the underlying reader exactly once.
func TestReadAtWrapper_CloseDuringNormalRead(t *testing.T) {
	release := make(chan struct{})
	stub := &stubTorrentReader{block: release, data: []byte("DATA")}
	r := newReadAtWrapper(stub, time.Second, &readStats{}, zerolog.Nop())

	type readOutcome struct {
		n   int
		err error
	}
	readCh := make(chan readOutcome, 1)
	go func() {
		n, err := r.ReadAt(make([]byte, 4), 0)
		readCh <- readOutcome{n, err}
	}()
	time.Sleep(10 * time.Millisecond)

	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- r.Close()
	}()
	time.Sleep(10 * time.Millisecond)

	close(release)

	ro := <-readCh
	require.NoError(t, ro.err)
	require.Equal(t, 4, ro.n)
	require.NoError(t, <-closeErrCh)
	require.Equal(t, int64(1), stub.closes.Load())
}

// TestReadAtWrapper_ProgressExtendsDeadline guards against regressing
// legitimately slow reads on a thin swarm: the deadline resets on every
// partial read, so a call can take longer in total than one timeout window
// provided it never goes a full window without making progress.
func TestReadAtWrapper_ProgressExtendsDeadline(t *testing.T) {
	trickle := &trickleTorrentReader{delay: 60 * time.Millisecond, remain: 5}
	timeout := 100 * time.Millisecond // > each 60ms gap, < the ~300ms total

	r := newReadAtWrapper(trickle, timeout, &readStats{}, zerolog.Nop())

	n, err := r.ReadAt(make([]byte, 5), 0)
	require.NoError(t, err)
	require.Equal(t, 5, n)
}

// TestTorrentFileHandle_RecoversAfterAbandonedRead is goal 3's regression
// test: after a read is abandoned, the handle must recover on its own —
// discarding the dead reader and building a fresh one — rather than staying
// wedged for the life of the handle.
func TestTorrentFileHandle_RecoversAfterAbandonedRead(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stuckStub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}
	workingStub := &stubTorrentReader{data: []byte("GOOD")}

	var calls atomic.Int64
	h := &torrentFileHandle{
		torrentFile: &torrentFile{
			timeout: 1, // seconds — torrentFile.timeout's smallest practical unit
			stats:   &readStats{},
			log:     zerolog.Nop(),
			readerFunc: func() torrent.Reader {
				if calls.Add(1) == 1 {
					return stuckStub
				}
				return workingStub
			},
		},
	}

	buf := make([]byte, 4)
	n, err := h.ReadAt(buf, 0)
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, ErrReadTimeout)
	require.Equal(t, int64(0), stuckStub.closes.Load(), "must not close a reader that still has a goroutine inside it")

	n, err = h.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Equal(t, "GOOD", string(buf))
	require.Equal(t, int64(2), calls.Load(), "readerFunc must be called again to build the fresh reader")
}

// TestTorrentFileHandle_ReadAtRetriesOnReaderPoisonedConcurrently covers the
// case Read/ReadAt's single-retry exists for: a reader poisoned by a
// *different* concurrent caller sharing the same handle, not just a caller
// retrying after its own previous call.
func TestTorrentFileHandle_ReadAtRetriesOnReaderPoisonedConcurrently(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stuckStub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}
	workingStub := &stubTorrentReader{data: []byte("GOOD")}

	var calls atomic.Int64
	h := &torrentFileHandle{
		torrentFile: &torrentFile{
			timeout: 1,
			stats:   &readStats{},
			log:     zerolog.Nop(),
			readerFunc: func() torrent.Reader {
				if calls.Add(1) == 1 {
					return stuckStub
				}
				return workingStub
			},
		},
	}

	// Both goroutines below share this same, soon-to-be-poisoned reader.
	_ = h.load()

	var wg sync.WaitGroup
	wg.Add(2)
	var err1, err2 error
	var n2 int
	go func() {
		defer wg.Done()
		_, err1 = h.ReadAt(make([]byte, 4), 0) // occupies sem, abandons at its deadline
	}()
	time.Sleep(20 * time.Millisecond)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4)
		n2, err2 = h.ReadAt(buf, 8) // queued behind the dying reader; must recover
	}()
	wg.Wait()

	require.ErrorIs(t, err1, ErrReadTimeout)
	require.NoError(t, err2)
	require.Equal(t, 4, n2)
}

// TestTorrentFile_StuckHandleDoesNotAffectOtherHandles: each Open()/NewHandle
// gets its own reader (see ContainerFs.Open's doc comment), so a stuck read
// on one handle must never affect reads on another handle of the same file.
func TestTorrentFile_StuckHandleDoesNotAffectOtherHandles(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	var calls atomic.Int64
	tf := &torrentFile{
		timeout: 1,
		stats:   &readStats{},
		log:     zerolog.Nop(),
		readerFunc: func() torrent.Reader {
			if calls.Add(1) == 1 {
				return &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}
			}
			return &stubTorrentReader{data: []byte("OK")}
		},
	}

	h1 := tf.NewHandle()
	h2 := tf.NewHandle()

	go func() { _, _ = h1.ReadAt(make([]byte, 4), 0) }()
	time.Sleep(20 * time.Millisecond)

	buf := make([]byte, 2)
	n, err := h2.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "OK", string(buf))
}

// TestReadAtWrapper_AbandonedGoroutinesAreBounded is the direct regression
// test for the OOM mechanism itself: N abandoned reads must leak roughly N
// parked goroutines (one worker each), not a runaway multiple, and they
// must all exit once their underlying read finally (or never — here,
// deliberately) returns.
func TestReadAtWrapper_AbandonedGoroutinesAreBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("goroutine-count test is slow")
	}
	const n = 25
	block := make(chan struct{})

	before := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stub := &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}
			r := newReadAtWrapper(stub, 60*time.Millisecond, &readStats{}, zerolog.Nop())
			_, _ = r.ReadAt(make([]byte, 4), 0)
		}()
	}
	wg.Wait() // every read above has abandoned by now

	// A loose lower bound rather than a tight band: this suite can run
	// alongside real-network tests (TestReadAtWrapper et al. against a real
	// magnet, unskipped outside -short) whose own background goroutines are
	// unrelated noise that only ever adds to the count, never subtracts.
	require.GreaterOrEqual(t, runtime.NumGoroutine(), before+n, "each abandoned read should leave its worker goroutine parked")
	peak := runtime.NumGoroutine()

	close(block) // release all n stuck goroutines

	// Measure the drop relative to peak, not a fresh absolute baseline: this
	// isolates the effect of closing block from any ambient goroutine churn
	// elsewhere in the suite, while still proving abandoned goroutines
	// actually exit once their read returns rather than leaking forever.
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= peak-n+5
	}, 2*time.Second, 10*time.Millisecond, "abandoned goroutines must exit once their read finally returns — closing block should free roughly n of them")
}

// TestTorrentFileHandle_FirstRead_FiresOnce guards the OnFirstRead hook
// added for cold-start timing instrumentation (internal/torrent.Timings):
// it must fire exactly once per file, regardless of how many handles are
// opened or how many reads happen, with the duration measured from the
// specific handle that produced the first byte.
func TestTorrentFileHandle_FirstRead_FiresOnce(t *testing.T) {
	type call struct {
		hash, path string
		sinceOpen  time.Duration
	}
	calls := make(chan call, 10)

	tf := &torrentFile{
		hash:    "deadbeef",
		path:    "/movie.mkv",
		timeout: 5,
		stats:   &readStats{},
		log:     zerolog.Nop(),
		readerFunc: func() torrent.Reader {
			return &stubTorrentReader{data: []byte{1, 2, 3, 4}}
		},
		onFirstRead: func(hash, path string, sinceOpen time.Duration) {
			calls <- call{hash, path, sinceOpen}
		},
	}

	h1 := tf.NewHandle()
	time.Sleep(5 * time.Millisecond) // give sinceOpen something non-zero to measure
	n, err := h1.ReadAt(make([]byte, 4), 0)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	h2 := tf.NewHandle()
	_, err = h2.ReadAt(make([]byte, 4), 0)
	require.NoError(t, err)

	select {
	case c := <-calls:
		require.Equal(t, "deadbeef", c.hash)
		require.Equal(t, "/movie.mkv", c.path)
		require.GreaterOrEqual(t, c.sinceOpen, 5*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("onFirstRead was never called")
	}

	select {
	case c := <-calls:
		t.Fatalf("onFirstRead fired a second time: %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestTorrentFileHandle_FirstRead_NotFiredOnTimeout confirms the hook only
// reports a genuine first byte, not a call that returned zero bytes because
// it hit its deadline.
func TestTorrentFileHandle_FirstRead_NotFiredOnTimeout(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	fired := false
	tf := &torrentFile{
		hash:    "h",
		path:    "/p",
		timeout: 1,
		stats:   &readStats{},
		log:     zerolog.Nop(),
		readerFunc: func() torrent.Reader {
			return &stubTorrentReader{block: block, data: []byte{0, 0, 0, 0}}
		},
		onFirstRead: func(hash, path string, sinceOpen time.Duration) { fired = true },
	}

	h := tf.NewHandle()
	n, err := h.ReadAt(make([]byte, 4), 0)
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, ErrReadTimeout)
	require.False(t, fired, "a timed-out read must not be reported as a first byte")
}

// TestTorrentFileHandle_FirstRead_NilSafeWhenUnset confirms a file with no
// OnFirstRead registered (the default — Service only sets one via
// TorrentFS.OnFirstRead) reads normally with no nil-func-call panic.
func TestTorrentFileHandle_FirstRead_NilSafeWhenUnset(t *testing.T) {
	tf := &torrentFile{
		hash:    "h",
		path:    "/p",
		timeout: 5,
		stats:   &readStats{},
		log:     zerolog.Nop(),
		readerFunc: func() torrent.Reader {
			return &stubTorrentReader{data: []byte{1, 2, 3, 4}}
		},
	}
	h := tf.NewHandle()
	n, err := h.ReadAt(make([]byte, 4), 0)
	require.NoError(t, err)
	require.Equal(t, 4, n)
}

// BenchmarkReadAtWrapper_ReadAt_Cached isolates readAtWrapper's own overhead
// (goroutine spawn, channel handoff, scratch-buffer copy) from real I/O, by
// serving every read from memory in a single ReadContext call. Compare
// before/after any change here with benchstat per docs/benchmarking.md.
func BenchmarkReadAtWrapper_ReadAt_Cached(b *testing.B) {
	data := make([]byte, 128*1024)
	stub := &stubTorrentReader{data: data}
	r := newReadAtWrapper(stub, 30*time.Second, &readStats{}, zerolog.Nop())
	defer func() { _ = r.Close() }()

	buf := make([]byte, len(data))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.ReadAt(buf, 0); err != nil {
			b.Fatal(err)
		}
	}
}
