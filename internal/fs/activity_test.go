package fs

import (
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
