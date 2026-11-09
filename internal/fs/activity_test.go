package fs

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func activityFile(t *testing.T, r torrent.Reader) *torrentFile {
	t.Helper()
	return &torrentFile{
		hash:       "aa",
		path:       "Film/film.mkv",
		len:        1000,
		timeout:    5,
		stats:      &readStats{},
		log:        zerolog.Nop(),
		readerFunc: func() torrent.Reader { return r },
	}
}

func activeFor(path string) (ReadActivity, bool) {
	for _, r := range ActiveReads() {
		if r.Path == path {
			return r, true
		}
	}
	return ReadActivity{}, false
}

// A file being read is listed with where its reads got to and how fast they went, and leaves
// the list when its handle closes.
func TestActiveReads_ListsAReadFileUntilClosed(t *testing.T) {
	f := activityFile(t, &stubTorrentReader{data: make([]byte, 100)})
	h := f.NewHandle()
	buf := make([]byte, 100)
	_, err := h.ReadAt(buf, 200)
	require.NoError(t, err)

	r, ok := activeFor("Film/film.mkv")
	require.True(t, ok)
	require.Equal(t, "aa", r.Hash)
	require.Equal(t, int64(300), r.Pos)
	require.Equal(t, int64(1000), r.Size)
	require.Equal(t, int64(100/activityWindow), r.Rate)
	require.Zero(t, r.Waiting)

	require.NoError(t, h.Close())
	_, ok = activeFor("Film/film.mkv")
	require.False(t, ok)
}

// A read waiting for data shows how long it has waited.
func TestActiveReads_ShowsAWaitingRead(t *testing.T) {
	block := make(chan struct{})
	f := activityFile(t, &stubTorrentReader{block: block, data: make([]byte, 10)})
	f.path = "Film/waiting.mkv"
	h := f.NewHandle()
	defer func() { _ = h.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.ReadAt(make([]byte, 10), 0)
	}()

	require.Eventually(t, func() bool {
		r, ok := activeFor("Film/waiting.mkv")
		return ok && r.Waiting > 50*time.Millisecond
	}, 5*time.Second, 10*time.Millisecond)
	close(block)
	<-done
	r, _ := activeFor("Film/waiting.mkv")
	require.Zero(t, r.Waiting)
}

// A handle closed before it reads isn't listed.
func TestActiveReads_ClosedHandleIsNotListed(t *testing.T) {
	f := activityFile(t, &stubTorrentReader{data: make([]byte, 10)})
	f.path = "Film/closed.mkv"
	h := f.NewHandle()
	require.NoError(t, h.Close())
	h.activity().done(0, 10)
	_, ok := activeFor("Film/closed.mkv")
	require.False(t, ok)
}

func TestAbandonedSince(t *testing.T) {
	start := time.Now()
	before := AbandonedSince(start)
	recordAbandoned(start.Add(time.Second))
	recordAbandoned(start.Add(2 * time.Second))
	require.Equal(t, before+2, AbandonedSince(start))
	require.Equal(t, 1, AbandonedSince(start.Add(1500*time.Millisecond)))
}

// episode is a 1000 MiB file reported to nearEnd, which plays for d when d is above 0.
func episode(t *testing.T, d time.Duration) (*torrentFile, chan int64) {
	t.Helper()
	f := activityFile(t, &stubTorrentReader{})
	f.path, f.len = "Show/S01E01.mkv", 1000<<20
	fired := make(chan int64, 4)
	f.onNearEnd = func(_, _ string, lead int64) { fired <- lead }
	durations.Lock()
	durations.m[f] = durationEntry{d: d}
	durations.Unlock()
	t.Cleanup(func() {
		durations.Lock()
		delete(durations.m, f)
		durations.Unlock()
	})
	return f, fired
}

// fakeClock replaces the activity clock for one test, starting now; each tick moves it on.
func fakeClock(t *testing.T) func(time.Duration) {
	t.Helper()
	at := time.Now()
	activityNow = func() time.Time { return at }
	t.Cleanup(func() { activityNow = time.Now })
	return func(d time.Duration) { at = at.Add(d) }
}

// read has a handle of f read [from, to) MiB in 1 MiB reads, each taking step.
func read(f *torrentFile, h *torrentFileHandle, from, to int64, tick func(time.Duration), step time.Duration) {
	a := h.activity()
	for off := from; off < to; off++ {
		tick(step)
		a.start()
		a.done(off<<20, 1<<20)
	}
}

func firedLead(t *testing.T, fired chan int64) (int64, bool) {
	t.Helper()
	select {
	case lead := <-fired:
		return lead, true
	case <-time.After(100 * time.Millisecond):
		return 0, false
	}
}

// What a player does to an episode makes the next one ready, once per handle; what a library
// scan or a credits search does doesn't.
func TestActivity_NearEnd(t *testing.T) {
	const playing = 3 * time.Second // per MiB: 1000 MiB in 50 minutes, real time
	cases := []struct {
		name     string
		duration time.Duration
		reads    [][2]int64 // MiB ranges, in order
		step     time.Duration
		fires    bool
	}{
		{"played through", 50 * time.Minute, [][2]int64{{0, 960}}, playing, true},
		{"played after skipping the intro", 50 * time.Minute, [][2]int64{{0, 10}, {30, 960}}, playing, true},
		{"the credits searched for", 50 * time.Minute, [][2]int64{{0, 5}, {900, 1000}}, time.Millisecond, false},
		{"started, then jumped to the end", 50 * time.Minute, [][2]int64{{0, 400}, {900, 1000}}, playing, false},
		{"read through by a scan", 50 * time.Minute, [][2]int64{{0, 1000}}, 300 * time.Millisecond, false},
		{"read through, duration unknown", 0, [][2]int64{{0, 1000}}, time.Millisecond, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tick := fakeClock(t)
			f, fired := episode(t, c.duration)
			h := f.NewHandle()
			defer func() { _ = h.Close() }()
			for _, r := range c.reads {
				read(f, h, r[0], r[1], tick, c.step)
			}
			_, ok := firedLead(t, fired)
			require.Equal(t, c.fires, ok)
			// once per handle
			read(f, h, 960, 1000, tick, c.step)
			_, again := firedLead(t, fired)
			require.False(t, again)
		})
	}
}

// The next episode's lead is five minutes of this one's playback.
func TestActivity_NearEndLead(t *testing.T) {
	tick := fakeClock(t)
	f, fired := episode(t, 50*time.Minute)
	h := f.NewHandle()
	defer func() { _ = h.Close() }()
	read(f, h, 0, 960, tick, 3*time.Second)
	lead, ok := firedLead(t, fired)
	require.True(t, ok)
	require.Equal(t, LeadFor(1000<<20, 50*time.Minute), lead)
	require.Equal(t, int64(100<<20), lead)
}

// A small file isn't an episode, and distribyted's own reads never count.
func TestActivity_NearEndNeverForSmallOrQuiet(t *testing.T) {
	tick := fakeClock(t)
	f, fired := episode(t, 0)
	f.len = 50 << 20
	h := f.NewHandle()
	read(f, h, 0, 50, tick, time.Second)
	_ = h.Close()
	_, ok := firedLead(t, fired)
	require.False(t, ok, "a small file isn't an episode")

	f.len = 1000 << 20
	q := f.NewHandle()
	q.quiet = true
	read(f, q, 0, 1000, tick, 3*time.Second)
	_ = q.Close()
	_, ok = firedLead(t, fired)
	require.False(t, ok, "distribyted's own reads aren't play")
}

func TestLeadFor(t *testing.T) {
	const mib, gib = int64(1 << 20), int64(1 << 30)
	cases := []struct {
		name string
		size int64
		d    time.Duration
		want int64
	}{
		{"an 8 GB episode of 50 minutes", 8 * gib, 50 * time.Minute, 8 * gib / 10},
		{"a 60 GB remux of 166 minutes, capped", 60 * gib, 166 * time.Minute, gib},
		{"a 200 MB episode of 22 minutes, at least the minimum", 200 * mib, 22 * time.Minute, 64 * mib},
		{"unknown duration: 5%", 2 * gib, 0, 2 * gib / 20},
		{"unknown duration, capped", 60 * gib, 0, 512 * mib},
		{"empty", 0, time.Minute, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, LeadFor(c.size, c.d))
		})
	}
}

// Each handle's first byte is timed from its open; distribyted's own reads aren't.
func TestActivity_RecordsFirstByte(t *testing.T) {
	resetFirstBytes()
	tick := fakeClock(t)
	f := activityFile(t, &stubTorrentReader{})
	h := f.NewHandle()
	h.openedAt = activityNow()
	defer func() { _ = h.Close() }()
	tick(250 * time.Millisecond)
	h.activity().done(0, 10)
	tick(time.Second)
	h.activity().done(10, 10)
	q := f.NewHandle()
	q.quiet = true
	q.activity().done(0, 10)

	s := FirstBytes()
	require.Equal(t, 1, s.Count)
	require.Equal(t, 250*time.Millisecond, s.Median)
}

// bytesReader is a torrent.Reader over a file's bytes.
type bytesReader struct{ *bytes.Reader }

func (bytesReader) SetContext(context.Context)             {}
func (bytesReader) SetReadahead(int64)                     {}
func (bytesReader) SetReadaheadFunc(torrent.ReadaheadFunc) {}
func (bytesReader) SetResponsive()                         {}
func (bytesReader) Close() error                           { return nil }
func (b bytesReader) ReadContext(_ context.Context, p []byte) (int, error) {
	return b.Read(p)
}

// mkvOf is a Matroska header saying the file plays for ms milliseconds.
func mkvOf(ms float64) []byte {
	el := func(id []byte, data ...[]byte) []byte {
		body := bytes.Join(data, nil)
		return append(append(append([]byte{}, id...), 0x80|byte(len(body))), body...)
	}
	d := make([]byte, 8)
	binary.BigEndian.PutUint64(d, math.Float64bits(ms))
	return append(el([]byte{0x1A, 0x45, 0xDF, 0xA3}),
		el([]byte{0x18, 0x53, 0x80, 0x67}, el([]byte{0x15, 0x49, 0xA9, 0x66}, el([]byte{0x44, 0x89}, d)))...)
}

// A video's duration is read once, in the background, by reads that don't count as playing;
// a book's never is.
func TestActiveReads_Duration(t *testing.T) {
	data := append(mkvOf(3_600_000), make([]byte, 1000)...)
	f := activityFile(t, nil)
	f.path, f.len = "Film/film.mkv", int64(len(data))
	f.readerFunc = func() torrent.Reader { return bytesReader{bytes.NewReader(data)} }
	h := f.NewHandle()
	defer func() { _ = h.Close() }()
	_, _ = h.ReadAt(make([]byte, 10), 500)

	require.Eventually(t, func() bool {
		r, ok := activeFor("Film/film.mkv")
		return ok && r.Duration == time.Hour
	}, 5*time.Second, 10*time.Millisecond)
	n := 0
	for _, r := range ActiveReads() {
		if r.Path == "Film/film.mkv" {
			n++
		}
	}
	require.Equal(t, 1, n, "the header read isn't a second stream")

	book := activityFile(t, &stubTorrentReader{data: make([]byte, 10)})
	book.path = "Books/book.epub"
	require.Zero(t, book.duration())
	durations.Lock()
	_, asked := durations.m[book]
	durations.Unlock()
	require.False(t, asked, "a book is never read for a duration")
}

func TestPieceSync_Segments(t *testing.T) {
	s := &pieceSync{length: 0, pieceLen: 16}
	require.Len(t, s.segments(4), 4)
}

// A player that opens the file again for each range it reads still plays it through, and gets
// the next episode ready once.
func TestActivity_NearEndAcrossReopenedHandles(t *testing.T) {
	tick := fakeClock(t)
	f, fired := episode(t, 50*time.Minute)
	for from := int64(0); from < 1000; from += 50 {
		h := f.NewHandle()
		read(f, h, from, from+50, tick, 3*time.Second)
		_ = h.Close()
	}
	_, ok := firedLead(t, fired)
	require.True(t, ok)
	h := f.NewHandle()
	read(f, h, 960, 1000, tick, 3*time.Second)
	_ = h.Close()
	_, again := firedLead(t, fired)
	require.False(t, again, "once per run")
}

// A library app reading the same file while it plays doesn't stop the player's own run.
func TestActivity_NearEndWithAScanAlongside(t *testing.T) {
	tick := fakeClock(t)
	f, fired := episode(t, 50*time.Minute)
	player, scan := f.NewHandle(), f.NewHandle()
	defer func() { _ = player.Close(); _ = scan.Close() }()
	pa, sa := player.activity(), scan.activity()
	for off := int64(0); off < 960; off++ {
		tick(3 * time.Second)
		pa.done(off<<20, 1<<20)
		if off%10 == 0 {
			sa.done(((off*7)%1000)<<20, 1<<20)
		}
	}
	_, ok := firedLead(t, fired)
	require.True(t, ok)
	require.GreaterOrEqual(t, pa.sequential(), int64(900<<20))
}
