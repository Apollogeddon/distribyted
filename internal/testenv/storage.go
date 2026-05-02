package testenv

import (
	"context"
	"sync"
	"syscall"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

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
