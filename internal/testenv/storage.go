package testenv

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type MapClientImpl struct {
	mu          sync.Mutex
	pieces      map[metainfo.Hash]map[int][]byte
	completions map[metainfo.Hash]map[int]bool
}

func NewMapClientImpl() *MapClientImpl {
	return &MapClientImpl{
		pieces:      make(map[metainfo.Hash]map[int][]byte),
		completions: make(map[metainfo.Hash]map[int]bool),
	}
}

func (m *MapClientImpl) OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pieces[infoHash]; !ok {
		m.pieces[infoHash] = make(map[int][]byte)
	}
	if _, ok := m.completions[infoHash]; !ok {
		m.completions[infoHash] = make(map[int]bool)
	}
	tp := m.pieces[infoHash]
	cp := m.completions[infoHash]

	return storage.TorrentImpl{
		// Keyed by piece index rather than the metainfo.Piece value itself:
		// metainfo.Piece embeds a *metainfo.Info pointer, so two Piece
		// values for what is logically the same piece (same torrent, same
		// index) compare unequal as map keys whenever they were built from
		// two different *Info instances — e.g. one parsed by the real
		// torrent.Client from bencoded bytes, another constructed directly
		// by a test pre-seeding data before the client ever sees it. Both
		// byte data and completion state are shared per (infoHash, index)
		// regardless of which *Info pointer produced the metainfo.Piece.
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return &mapPiece{index: p.Index(), tp: tp, cp: cp, mu: &m.mu, info: info}
		},
	}, nil
}

func (m *MapClientImpl) Close() error { return nil }

type mapPiece struct {
	index int
	tp    map[int][]byte
	cp    map[int]bool
	mu    *sync.Mutex
	info  *metainfo.Info
}

func (mp *mapPiece) WriteAt(b []byte, off int64) (int, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	data, ok := mp.tp[mp.index]
	if !ok {
		data = make([]byte, mp.info.PieceLength)
		mp.tp[mp.index] = data
	}
	copy(data[off:], b)
	return len(b), nil
}

func (mp *mapPiece) ReadAt(b []byte, off int64) (int, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	data, ok := mp.tp[mp.index]
	if !ok {
		return 0, io.EOF
	}
	n := copy(b, data[off:])
	return n, nil
}

func (mp *mapPiece) MarkComplete() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.cp[mp.index] = true
	return nil
}

func (mp *mapPiece) MarkNotComplete() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.cp[mp.index] = false
	return nil
}

func (mp *mapPiece) Completion() storage.Completion {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return storage.Completion{
		Ok:       true,
		Complete: mp.cp[mp.index],
	}
}

type limitStorage struct {
	storage.ClientImpl
	limitBytes int64
	written    int64
	mu         sync.Mutex
}

func (l *limitStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	tImpl, err := l.ClientImpl.OpenTorrent(ctx, info, infoHash)
	if err != nil {
		return tImpl, err
	}

	origPiece := tImpl.Piece
	if origPiece != nil {
		tImpl.Piece = func(p metainfo.Piece) storage.PieceImpl {
			pImpl := origPiece(p)
			if pImpl == nil {
				return nil
			}
			return &limitPiece{PieceImpl: pImpl, ls: l}
		}
	}

	origPieceWithHash := tImpl.PieceWithHash
	if origPieceWithHash != nil {
		tImpl.PieceWithHash = func(p metainfo.Piece, hash generics.Option[[]byte]) storage.PieceImpl {
			pImpl := origPieceWithHash(p, hash)
			if pImpl == nil {
				return nil
			}
			return &limitPiece{PieceImpl: pImpl, ls: l}
		}
	} else {
		// We disable PieceWithHash to force using Piece, which we wrapped.
		tImpl.PieceWithHash = nil
	}

	return tImpl, nil
}

type limitPiece struct {
	storage.PieceImpl
	ls *limitStorage
}

func (lp *limitPiece) WriteAt(p []byte, off int64) (n int, err error) {
	lp.ls.mu.Lock()
	defer lp.ls.mu.Unlock()

	if lp.ls.written+int64(len(p)) > lp.ls.limitBytes {
		fmt.Printf("limitStorage hit limit! written=%d, len=%d, limit=%d\n", lp.ls.written, len(p), lp.ls.limitBytes) //nolint:forbidigo
		return 0, syscall.ENOSPC
	}

	n, err = lp.PieceImpl.WriteAt(p, off)
	if err == nil {
		lp.ls.written += int64(n)
	}
	return
}

func (l *limitStorage) Written() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.written
}

// flakyStorage reproduces, against the real anacrolix/torrent library, the
// exact condition responsible for a production OOMKilled crash: a piece
// storage that (a) reports a capacity — so Torrent.hasStorageCap() is true,
// matching cmd/distribyted/main.go's production wiring, and reader.go's
// retry-on-read-failure loop takes its recursing branch instead of just
// returning an error — and (b) can start failing ReadAt on an
// already-complete piece, modeling a disk-backed cache (missinggo/v2/
// filecache) evicting a piece's backing file out from under a torrent that
// still believes it's complete.
//
// Fail switches every piece to fail ReadAt and lie that it's still complete
// (Completion() keeps returning Complete: true even though reads now
// error) — the TOCTOU state reader.go's own doc comment on hasStorageCap
// describes ("whether we can expect data to vanish while trying to read").
// Heal reverses both: reads succeed again and Completion() tells the truth.
//
// See internal/fs/torrent.go's readAtWrapper doc comment for the full
// mechanism this is exercising, and flaky_storage_test.go for how the
// healable vs. permanently-flaky cases are actually used.
type flakyStorage struct {
	storage.ClientImpl
	failing atomic.Bool
}

func (fs *flakyStorage) Fail() { fs.failing.Store(true) }
func (fs *flakyStorage) Heal() { fs.failing.Store(false) }

func (fs *flakyStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	tImpl, err := fs.ClientImpl.OpenTorrent(ctx, info, infoHash)
	if err != nil {
		return tImpl, err
	}

	origPiece := tImpl.Piece
	if origPiece != nil {
		tImpl.Piece = func(p metainfo.Piece) storage.PieceImpl {
			pImpl := origPiece(p)
			if pImpl == nil {
				return nil
			}
			return &flakyPiece{PieceImpl: pImpl, fs: fs}
		}
	}

	origPieceWithHash := tImpl.PieceWithHash
	if origPieceWithHash != nil {
		tImpl.PieceWithHash = func(p metainfo.Piece, hash generics.Option[[]byte]) storage.PieceImpl {
			pImpl := origPieceWithHash(p, hash)
			if pImpl == nil {
				return nil
			}
			return &flakyPiece{PieceImpl: pImpl, fs: fs}
		}
	} else {
		// Force using Piece, which we wrapped, same as limitStorage above.
		tImpl.PieceWithHash = nil
	}

	// Without this, Torrent.hasStorageCap() is false and reader.go's failed
	// read just returns an error instead of recursing — this test would
	// prove nothing about the actual production bug, which only exists on
	// the recursing branch (see cmd/distribyted/main.go's capFunc, wired
	// there for exactly this reason per docs/benchmarking.md).
	capFunc := func() (int64, bool) { return 1 << 30, true }
	tImpl.Capacity = &capFunc

	return tImpl, nil
}

type flakyPiece struct {
	storage.PieceImpl
	fs *flakyStorage
}

func (fp *flakyPiece) ReadAt(p []byte, off int64) (int, error) {
	if fp.fs.failing.Load() {
		return 0, syscall.EIO
	}
	return fp.PieceImpl.ReadAt(p, off)
}

func (fp *flakyPiece) Completion() storage.Completion {
	if fp.fs.failing.Load() {
		return storage.Completion{Ok: true, Complete: true}
	}
	return fp.PieceImpl.Completion()
}
