package torrent

import (
	"strings"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/missinggo/v2/resource"
	"github.com/anacrolix/torrent/storage"
)

// PieceStorage is the storage distribyted downloads into: complete pieces in cache, which evicts
// the ones read longest ago past its capacity (global_cache_size), and pieces still downloading in
// partial, which evicts nothing. Kept in one cache, a slow or abandoned piece's chunks were evicted
// before it finished, so it failed its hash check, and the client banned the peer that sent it,
// with every other address it holds for that IP: an honest swarm, lost until the next restart.
//
// The cache's capacity is told to the client, which then retries a failed read without limit; the
// storage is guarded against that (GuardReads).
func PieceStorage(cache, partial *filecache.Cache, giveUpAfter time.Duration) storage.ClientImpl {
	capacity := func() (int64, bool) {
		if c := cache.Info().Capacity; c >= 0 {
			return c, true
		}
		return 0, false
	}
	provider := splitProvider{complete: cache.AsResourceProvider(), partial: partial.AsResourceProvider()}
	return GuardReads(storage.NewResourcePiecesOpts(provider, storage.ResourcePiecesOpts{Capacity: &capacity}), giveUpAfter)
}

// partialDir is where anacrolix/torrent's resource storage keeps a piece's chunks until it's
// complete (storage.piecePerResourcePiece.incompleteDirPath).
const partialDir = "incompleted"

type splitProvider struct {
	complete, partial resource.Provider
}

func (p splitProvider) NewInstance(loc string) (resource.Instance, error) {
	if loc == partialDir || strings.HasPrefix(loc, partialDir+"/") {
		return p.partial.NewInstance(loc)
	}
	return p.complete.NewInstance(loc)
}
