package fs

import (
	"github.com/anacrolix/torrent"
)

// The piece cache (global_cache_size) evicts pieces without telling the torrent client, which
// goes on counting them complete. Its readahead then skips them, as there's nothing to fetch, so
// a read reaching one finds no data, makes the client notice, and waits while the piece is
// downloaded again: a stall per evicted piece, long enough to drop frames, and much longer once
// the client has let its peers go, thinking it had what was wanted. pieceSync checks the pieces
// a read is about to need against storage before the read gets there, and makes the client
// forget the evicted ones, so readahead fetches them in time.

type pieceSync struct {
	t        *torrent.Torrent
	offset   int64 // the file's, in the torrent
	length   int64 // the file's
	pieceLen int64
}

// newPieceSync returns nil when f has no torrent or its torrent no info, as in tests.
func newPieceSync(f *torrent.File) *pieceSync {
	if f == nil || f.Torrent() == nil {
		return nil
	}
	info := f.Torrent().Info()
	if info == nil || info.PieceLength <= 0 {
		return nil
	}
	return &pieceSync{t: f.Torrent(), offset: f.Offset(), length: f.Length(), pieceLen: info.PieceLength}
}

// pieceAt is the index of the piece holding the file's byte at off.
func (s *pieceSync) pieceAt(off int64) int {
	return int((s.offset + off) / s.pieceLen)
}

// sync makes the client forget every piece holding the file's bytes from off for length that it
// counts complete but storage no longer has, and reports whether there were any. Storage is
// asked without the client's lock; only a piece found evicted takes it.
func (s *pieceSync) sync(off, length int64) (evicted bool) {
	end := s.offset + min(off+length, s.length)
	for i := s.pieceAt(off); int64(i)*s.pieceLen < end; i++ {
		if !s.t.PieceState(i).Complete {
			continue
		}
		p := s.t.Piece(i)
		if c := p.Storage().Completion(); c.Ok && !c.Complete {
			p.UpdateCompletion()
			evicted = true
		}
	}
	return evicted
}
