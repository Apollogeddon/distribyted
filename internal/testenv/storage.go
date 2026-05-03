package testenv

import (
	"context"
	"io"
	"sync"
	"syscall"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type MapClientImpl struct {
	mu     sync.Mutex
	pieces map[metainfo.Hash]map[metainfo.Piece][]byte
}

func NewMapClientImpl() *MapClientImpl {
	return &MapClientImpl{
		pieces: make(map[metainfo.Hash]map[metainfo.Piece][]byte),
	}
}

func (m *MapClientImpl) OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pieces[infoHash]; !ok {
		m.pieces[infoHash] = make(map[metainfo.Piece][]byte)
	}
	tp := m.pieces[infoHash]

	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return &mapPiece{p: p, tp: tp, mu: &m.mu, info: info}
		},
	}, nil
}

func (m *MapClientImpl) Close() error { return nil }

type mapPiece struct {
	p        metainfo.Piece
	tp       map[metainfo.Piece][]byte
	mu       *sync.Mutex
	info     *metainfo.Info
	complete bool
}

func (mp *mapPiece) WriteAt(b []byte, off int64) (int, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	data, ok := mp.tp[mp.p]
	if !ok {
		data = make([]byte, mp.info.PieceLength)
		mp.tp[mp.p] = data
	}
	copy(data[off:], b)
	return len(b), nil
}

func (mp *mapPiece) ReadAt(b []byte, off int64) (int, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	data, ok := mp.tp[mp.p]
	if !ok {
		return 0, io.EOF
	}
	n := copy(b, data[off:])
	return n, nil
}

func (mp *mapPiece) MarkComplete() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.complete = true
	return nil
}

func (mp *mapPiece) MarkNotComplete() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.complete = false
	return nil
}

func (mp *mapPiece) Completion() storage.Completion {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return storage.Completion{
		Ok:       true,
		Complete: mp.complete,
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
			return &limitPiece{PieceImpl: pImpl, ls: l}
		}
	}

	// We disable PieceWithHash to force using Piece, which we wrapped.
	tImpl.PieceWithHash = nil

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
		return 0, syscall.ENOSPC
	}

	n, err = lp.PieceImpl.WriteAt(p, off)
	if err == nil {
		lp.ls.written += int64(n)
	}
	return
}
