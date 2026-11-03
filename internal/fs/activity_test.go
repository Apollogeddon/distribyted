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

// Reading past 95% of an episode-sized file says so once per handle; a small file never does.
func TestActivity_NearEndFiresOnce(t *testing.T) {
	f := activityFile(t, &stubTorrentReader{data: make([]byte, 1<<20)})
	f.path, f.len = "Show/S01E01.mkv", 200<<20
	fired := make(chan string, 4)
	f.onNearEnd = func(_, p string) { fired <- p }
	h := f.NewHandle()
	defer func() { _ = h.Close() }()
	buf := make([]byte, 1<<20)

	_, _ = h.ReadAt(buf, 100<<20) // halfway
	_, _ = h.ReadAt(buf, 190<<20) // 95.5% once read
	_, _ = h.ReadAt(buf, 195<<20)
	select {
	case p := <-fired:
		require.Equal(t, "Show/S01E01.mkv", p)
	case <-time.After(time.Second):
		t.Fatal("no near-end call")
	}
	select {
	case <-fired:
		t.Fatal("fired twice on one handle")
	case <-time.After(50 * time.Millisecond):
	}

	small := activityFile(t, &stubTorrentReader{data: make([]byte, 10)})
	small.path, small.len = "Show/S01E01.srt", 20
	small.onNearEnd = func(_, p string) { fired <- p }
	hs := small.NewHandle()
	defer func() { _ = hs.Close() }()
	_, _ = hs.ReadAt(make([]byte, 10), 10)
	select {
	case <-fired:
		t.Fatal("a small file isn't an episode")
	case <-time.After(50 * time.Millisecond):
	}
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
