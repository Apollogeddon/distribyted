package torrent

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
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
	p := guardPiece(inner, 3, f)

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

func TestGuardedPieceKeepsWriteTo(t *testing.T) {
	f := &pieceReadFailures{failed: make(map[int]*readFailures), now: time.Now}
	p := guardPiece(&writerToPiece{}, 0, f)
	w, ok := p.(io.WriterTo)
	require.True(t, ok)
	var b bytes.Buffer
	_, err := w.WriteTo(&b)
	require.NoError(t, err)
	require.Equal(t, "whole", b.String())

	_, ok = guardPiece(&brokenPiece{}, 0, f).(io.WriterTo)
	require.False(t, ok, "a piece without WriteTo doesn't gain one")
}
