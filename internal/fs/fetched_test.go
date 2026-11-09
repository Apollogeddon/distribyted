package fs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fetchedFor(r Reason) FetchCount {
	for _, c := range Fetched().Reasons {
		if c.Reason == r {
			return c
		}
	}
	return FetchCount{}
}

// A piece is counted under the reason it was asked for, and a piece nobody tagged as a read.
func TestFetched_CountsByReason(t *testing.T) {
	resetFetches(time.Now)
	wantPieces("aa", []int{0, 1}, ReasonLink)
	wantPieces("aa", []int{1, 2}, ReasonNextEpisode) // piece 1 keeps its first reason

	PieceDownloaded("aa", 0, 100)
	PieceDownloaded("aa", 1, 100)
	PieceDownloaded("aa", 2, 50)
	PieceDownloaded("aa", 3, 10)
	PieceDownloaded("bb", 0, 7) // same index, another torrent

	require.Equal(t, int64(200), fetchedFor(ReasonLink).Total)
	require.Equal(t, int64(50), fetchedFor(ReasonNextEpisode).Total)
	require.Equal(t, int64(17), fetchedFor(ReasonRead).Total)
	require.Equal(t, int64(200), fetchedFor(ReasonLink).Day)
	require.Zero(t, fetchedFor(ReasonButton).Total)
}

// A tag is spent once its piece downloads: the same piece evicted and read again is a read,
// counted as downloaded again.
func TestFetched_TagIsSpentAndAgainCounted(t *testing.T) {
	resetFetches(time.Now)
	wantPieces("aa", []int{0}, ReasonLink)
	PieceDownloaded("aa", 0, 100)
	PieceDownloaded("aa", 0, 100)

	link, read := fetchedFor(ReasonLink), fetchedFor(ReasonRead)
	require.Equal(t, int64(100), link.Total)
	require.Zero(t, link.TotalAgain)
	require.Equal(t, int64(100), read.Total)
	require.Equal(t, int64(100), read.TotalAgain)
	require.Equal(t, int64(100), read.DayAgain)
}

// Cancelling a preparation drops its tags.
func TestFetched_Unwant(t *testing.T) {
	resetFetches(time.Now)
	wantPieces("aa", []int{0, 1, 2}, ReasonButton)
	unwantPieces("aa", 0, 2)
	PieceDownloaded("aa", 1, 10)
	PieceDownloaded("aa", 2, 10)
	require.Equal(t, int64(10), fetchedFor(ReasonRead).Total)
	require.Equal(t, int64(10), fetchedFor(ReasonButton).Total)
}

// The last day's counts leave out older hours; the totals keep them.
func TestFetched_LastDay(t *testing.T) {
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	resetFetches(func() time.Time { return at })
	PieceDownloaded("aa", 0, 100)
	at = at.Add(23 * time.Hour)
	PieceDownloaded("aa", 1, 10)
	require.Equal(t, int64(110), fetchedFor(ReasonRead).Day)

	at = at.Add(2 * time.Hour)
	PieceDownloaded("aa", 2, 1)
	c := fetchedFor(ReasonRead)
	require.Equal(t, int64(11), c.Day)
	require.Equal(t, int64(111), c.Total)
}

func TestFirstBytes(t *testing.T) {
	resetFirstBytes()
	require.Zero(t, FirstBytes().Count)
	for i := 1; i <= maxFirstBytes+20; i++ {
		recordFirstByte(time.Duration(i) * time.Millisecond)
	}
	s := FirstBytes()
	require.Equal(t, maxFirstBytes, s.Count, "only the latest are kept")
	require.Equal(t, 121*time.Millisecond, s.Median)
	require.Equal(t, 211*time.Millisecond, s.P95)
}
