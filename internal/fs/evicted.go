package fs

import (
	"errors"
	"io"

	"github.com/anacrolix/torrent"
	astorage "github.com/anacrolix/torrent/storage"
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

// localReader reads a handle's data straight from storage, if the client counts every piece it's
// in complete. It never waits on a peer, nor retries a failed read as the client's reader does,
// so a read it can't do is left to that reader and its deadline. Like that reader, it keeps the
// piece it last read open. Only one read uses it at a time (readAtWrapper's sem).
type localReader struct {
	s     *pieceSync
	piece int
	pr    astorage.PieceReader
}

func (s *pieceSync) newLocalReader() *localReader {
	return &localReader{s: s, piece: -1}
}

// read reports whether it read all of the file's bytes from off; at the end of the file it reads
// what's left and returns io.EOF. When it can't, any error is only for the record.
func (l *localReader) read(p []byte, off int64) (n int, ok bool, err error) {
	s := l.s
	if off >= s.length {
		return 0, true, io.EOF
	}
	want := int(min(int64(len(p)), s.length-off))
	for n < want {
		at := s.offset + off + int64(n)
		i := int(at / s.pieceLen)
		if !s.t.PieceState(i).Complete {
			return 0, false, nil
		}
		piece := s.t.Piece(i)
		if l.piece != i {
			l.close()
			pr, err := piece.Storage().NewReader()
			if err != nil {
				return 0, false, err
			}
			l.pr, l.piece = pr, i
		}
		pieceOff := at - piece.Info().Offset()
		m, err := l.pr.ReadAt(p[n:want][:min(int64(want-n), piece.Info().Length()-pieceOff)], pieceOff)
		n += m
		if m == 0 || err != nil && (!errors.Is(err, io.EOF) || n < want) {
			l.close()
			return 0, false, nil
		}
	}
	if want < len(p) {
		return n, true, io.EOF
	}
	return n, true, nil
}

func (l *localReader) close() {
	if l.pr != nil {
		_ = l.pr.Close()
		l.pr, l.piece = nil, -1
	}
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
