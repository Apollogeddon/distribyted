package torrent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

func TestPieceReadFailures(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := &pieceReadFailures{failed: make(map[int]*readFailures), giveUpAfter: time.Minute, now: func() time.Time { return now }}

	for range freeFailedReads {
		wait, giveUp := f.fail(0)
		require.Zero(t, wait)
		require.False(t, giveUp)
	}
	// past the free failures each one waits, twice as long as the last, up to the cap
	wait, _ := f.fail(0)
	require.Equal(t, minFailedReadWait, wait)
	wait, _ = f.fail(0)
	require.Equal(t, 2*minFailedReadWait, wait)
	for range 20 {
		wait, _ = f.fail(0)
	}
	require.Equal(t, maxFailedReadWait, wait)
	require.False(t, f.distrusted(0))

	// failing for giveUpAfter makes the piece incomplete, once
	now = now.Add(time.Minute)
	_, giveUp := f.fail(0)
	require.True(t, giveUp)
	require.True(t, f.distrusted(0))
	wait, giveUp = f.fail(0)
	require.Zero(t, wait)
	require.False(t, giveUp)

	// other pieces are unaffected, and a good read or a new download trusts the piece again
	require.False(t, f.distrusted(1))
	f.reset(0)
	require.False(t, f.distrusted(0))
	require.Empty(t, f.failed)
}

// brokenPiece claims to be complete and can't be read.
type brokenPiece struct {
	storage.PieceImpl
	notComplete int
}

func (*brokenPiece) ReadAt([]byte, int64) (int, error) { return 0, errors.New("EIO") }
func (*brokenPiece) Completion() storage.Completion {
	return storage.Completion{Ok: true, Complete: true}
}
func (p *brokenPiece) MarkNotComplete() error { p.notComplete++; return nil }
func (*brokenPiece) MarkComplete() error      { return nil }

type writerToPiece struct{ brokenPiece }

func (*writerToPiece) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write([]byte("whole"))
	return int64(n), err
}

func TestGuardedPieceGivesUpOnAPieceItCantRead(t *testing.T) {
	f := &pieceReadFailures{failed: make(map[int]*readFailures), giveUpAfter: 0, now: time.Now}
	inner := &brokenPiece{}
	p := guardPiece(inner, 3, size16, f)

	require.True(t, p.Completion().Complete)
	r, err := p.(storage.PieceReaderer).NewReader()
	require.NoError(t, err)
	for range freeFailedReads + 1 {
		_, err := r.ReadAt(make([]byte, 4), 0)
		require.Error(t, err)
	}
	require.Equal(t, 1, inner.notComplete, "the storage is told the piece is no longer complete")
	require.Equal(t, storage.Completion{Ok: true}, p.Completion(), "the piece no longer counts as complete")

	// a download that passed its hash check trusts it again
	require.NoError(t, p.MarkComplete())
	require.True(t, p.Completion().Complete)
}

func size16() int64 { return 16 }

func readFetched() int64 {
	for _, c := range fs.Fetched().Reasons {
		if c.Reason == fs.ReasonRead {
			return c.Total
		}
	}
	return 0
}

// A piece the storage completes is counted as downloaded, at its length; one that fails to
// complete isn't.
func TestGuardedPieceCountsItsDownload(t *testing.T) {
	f := &pieceReadFailures{failed: make(map[int]*readFailures), now: time.Now, hash: "cc"}
	before := readFetched()
	require.NoError(t, guardPiece(&brokenPiece{}, 2, size16, f).MarkComplete())
	require.Equal(t, before+16, readFetched())

	require.Error(t, guardPiece(&failingCompletePiece{}, 3, size16, f).MarkComplete())
	require.Equal(t, before+16, readFetched())
}

type failingCompletePiece struct{ brokenPiece }

func (*failingCompletePiece) MarkComplete() error { return errors.New("disk full") }

func TestGuardedPieceKeepsWriteTo(t *testing.T) {
	f := &pieceReadFailures{failed: make(map[int]*readFailures), now: time.Now}
	p := guardPiece(&writerToPiece{}, 0, size16, f)
	w, ok := p.(io.WriterTo)
	require.True(t, ok)
	var b bytes.Buffer
	_, err := w.WriteTo(&b)
	require.NoError(t, err)
	require.Equal(t, "whole", b.String())

	_, ok = guardPiece(&brokenPiece{}, 0, size16, f).(io.WriterTo)
	require.False(t, ok, "a piece without WriteTo doesn't gain one")
}

// fakeStorage hands out one broken piece and a whole-torrent reader that can't read either.
type fakeStorage struct{ piece *brokenPiece }

func (f fakeStorage) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	broken := func() (int, error) { return 0, errors.New("EIO") }
	return storage.TorrentImpl{
		Piece: func(metainfo.Piece) storage.PieceImpl { return f.piece },
		NewReader: func() storage.TorrentReader {
			return guardedReader{ReaderAt: readerAtFunc(func([]byte, int64) (int, error) { return broken() }), Closer: io.NopCloser(nil)}
		},
	}, nil
}

func TestGuardReadsCountsWholeTorrentReaders(t *testing.T) {
	inner := &brokenPiece{}
	info := &metainfo.Info{PieceLength: 16, Length: 64}
	ti, err := GuardReads(fakeStorage{inner}, 0).OpenTorrent(context.Background(), info, metainfo.Hash{})
	require.NoError(t, err)

	r := ti.NewReader()
	for range freeFailedReads + 1 {
		_, err := r.ReadAt(make([]byte, 4), 20)
		require.Error(t, err)
	}
	require.Equal(t, 1, inner.notComplete, "failed whole-torrent reads count against their piece too")
}

// endPiece is a piece read at its end: there's nothing more to read.
type endPiece struct{ brokenPiece }

func (*endPiece) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }

func TestGuardedPieceDoesNotCountReadsAtTheEnd(t *testing.T) {
	f := &pieceReadFailures{failed: make(map[int]*readFailures), giveUpAfter: 0, now: time.Now}
	inner := &endPiece{}
	p := guardPiece(inner, 0, size16, f)
	r, err := p.(storage.PieceReaderer).NewReader()
	require.NoError(t, err)
	for range freeFailedReads + 2 {
		n, err := r.ReadAt(make([]byte, 4), 16)
		require.Zero(t, n)
		require.ErrorIs(t, err, io.EOF)
	}
	require.Zero(t, inner.notComplete, "reading at the end of a piece isn't a failed read")
	require.True(t, p.Completion().Complete)
	require.Empty(t, f.failed)
}

func TestGuardReadsIgnoresWholeTorrentReadsPastTheEnd(t *testing.T) {
	inner := &brokenPiece{}
	info := &metainfo.Info{PieceLength: 16, Length: 64}
	ti, err := GuardReads(fakeStorage{inner}, 0).OpenTorrent(context.Background(), info, metainfo.Hash{})
	require.NoError(t, err)

	r := ti.NewReader()
	for range freeFailedReads + 1 {
		_, err := r.ReadAt(make([]byte, 4), 64)
		require.Error(t, err)
	}
	require.Zero(t, inner.notComplete, "a read past the end has no piece to count against")
}
