package torrent

import (
	"cmp"
	"encoding/hex"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// The piece cache (global_cache_size) keeps each complete piece as a file named for the
// piece's hash, completed/<hash>, with no mention of its torrent. CacheUsage maps them back,
// so the web interface can say what the cache holds and free one torrent's share.

// completedDir is where resource storage keeps a complete piece
// (storage.piecePerResourcePiece.completedInstancePath, with a provider that can't move).
const completedDir = "completed"

// CachedTorrent is one torrent's share of the piece cache.
type CachedTorrent struct {
	Hash, Name string
	Bytes      int64
	Pieces     int
	// LastRead is when one of its cached pieces was last read.
	LastRead time.Time
}

// CacheUsage is what the piece cache holds, the torrents with the most cached first.
type CacheUsage struct {
	Torrents []CachedTorrent
	// OtherBytes and OtherPieces are the pieces of no torrent loaded now.
	OtherBytes  int64
	OtherPieces int
}

// pieceHashes maps each of t's pieces' hashes, in hex, to its index.
func pieceHashes(t fs.Torrent) map[string]int {
	info := t.Info()
	if info == nil {
		return nil
	}
	out := make(map[string]int, info.NumPieces())
	for i := 0; i < info.NumPieces(); i++ {
		if h := info.Piece(i).V1Hash(); h.Ok {
			out[hex.EncodeToString(h.Value[:])] = i
		}
	}
	return out
}

// GetCacheUsage reads what fc holds, against the torrents in ts.
func GetCacheUsage(fc *PieceCache, ts map[string]fs.Torrent) CacheUsage {
	owner := map[string]string{}
	byHash := map[string]*CachedTorrent{}
	for h, t := range ts {
		for ph := range pieceHashes(t) {
			owner[ph] = h
		}
		byHash[h] = &CachedTorrent{Hash: h, Name: t.Name()}
	}

	var u CacheUsage
	if fc == nil {
		return u
	}
	fc.WalkItems(func(it filecache.ItemInfo) {
		p := string(it.Path)
		if path.Dir(p) != completedDir {
			return
		}
		h, ok := owner[path.Base(p)]
		if !ok {
			u.OtherBytes += it.Size
			u.OtherPieces++
			return
		}
		c := byHash[h]
		c.Bytes += it.Size
		c.Pieces++
		if it.Accessed.After(c.LastRead) {
			c.LastRead = it.Accessed
		}
	})
	for _, c := range byHash {
		if c.Pieces > 0 {
			u.Torrents = append(u.Torrents, *c)
		}
	}
	slices.SortFunc(u.Torrents, func(a, b CachedTorrent) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), strings.Compare(a.Name, b.Name))
	})
	return u
}

type pieceGetter interface {
	Piece(int) *torrent.Piece
}

// EvictFromCache removes t's pieces from fc, and has the client forget them, so they're
// downloaded again when next read. It returns how many it removed.
func EvictFromCache(fc *PieceCache, t fs.Torrent) int {
	if fc == nil {
		return 0
	}
	pg, _ := t.(pieceGetter)
	n := 0
	for ph, i := range pieceHashes(t) {
		p := path.Join(completedDir, ph)
		// Remove reports nothing for a piece that isn't there
		if _, err := fc.Stat(p); err != nil {
			continue
		}
		if err := fc.Remove(p); err != nil {
			continue
		}
		n++
		if pg != nil {
			pg.Piece(i).UpdateCompletion()
		}
	}
	return n
}
