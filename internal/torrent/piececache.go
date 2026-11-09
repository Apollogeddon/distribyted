package torrent

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/missinggo/v2/resource"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// A player reads a file's first megabytes and its last before it shows a frame, so those pieces
// decide how fast everything starts, and they're a small part of the cache. In one cache that
// evicts what was read longest ago, a single film played through, or a library app reading
// every file, pushed every other file's start out, and the next play waited on peers again.
// With protection on, the start pieces (fs.StartPieces) of every file large enough to play
// have a cache of their own, a share of the capacity that only other start pieces compete for.

// minProtectedFile is the smallest file whose start pieces are protected: something played,
// not a subtitle, a sample, or one of the thousand files of a game or book collection, which
// would fill the protected share and push the films out.
const minProtectedFile = 64 << 20

// PieceCache is where complete pieces are kept: a main cache, and, with protection, a second
// for the pieces every file's start needs.
type PieceCache struct {
	main, protected         *filecache.Cache
	mainRoot, protectedRoot string
	// percent is the protected cache's share of the capacity
	percent int64

	mu sync.RWMutex
	// starts are the hex hashes of the pieces kept in the protected cache
	starts map[string]struct{}
}

// NewPieceCache keeps every piece in main, unprotected.
func NewPieceCache(main *filecache.Cache) *PieceCache {
	return &PieceCache{main: main, starts: map[string]struct{}{}}
}

// OpenPieceCache opens the main cache at mainRoot and, when percent is above 0, the protected
// cache at protectedRoot, with that share of the capacity. With protection off, pieces left in
// protectedRoot by an earlier run are moved back into the main cache first.
func OpenPieceCache(mainRoot, protectedRoot string, percent int64) (*PieceCache, error) {
	if percent <= 0 {
		if err := moveAll(protectedRoot, mainRoot); err != nil {
			return nil, err
		}
	}
	main, err := filecache.NewCache(mainRoot)
	if err != nil {
		return nil, err
	}
	c := &PieceCache{main: main, mainRoot: mainRoot, protectedRoot: protectedRoot, starts: map[string]struct{}{}}
	if percent > 0 {
		c.protected, err = filecache.NewCache(protectedRoot)
		if err != nil {
			return nil, err
		}
		c.percent = min(percent, 90)
	}
	return c, nil
}

// moveAll moves every complete piece under from into to, leaving from empty.
func moveAll(from, to string) error {
	dir := filepath.Join(from, completedDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(to, completedDir), 0o750); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Rename(filepath.Join(dir, e.Name()), filepath.Join(to, completedDir, e.Name())); err != nil {
			return err
		}
	}
	return os.RemoveAll(from)
}

// Main is the cache every piece but the protected ones is kept in.
func (c *PieceCache) Main() *filecache.Cache { return c.main }

// Protecting reports whether start pieces have a cache of their own.
func (c *PieceCache) Protecting() bool { return c != nil && c.protected != nil }

// SetCapacity divides capacity bytes between the caches, or with capacity below 0 sets no limit.
func (c *PieceCache) SetCapacity(capacity int64) {
	if c.protected == nil || capacity < 0 {
		c.main.SetCapacity(capacity)
		if c.protected != nil {
			c.protected.SetCapacity(capacity)
		}
		return
	}
	share := capacity * c.percent / 100
	c.protected.SetCapacity(share)
	c.main.SetCapacity(capacity - share)
}

// Info is both caches' together.
func (c *PieceCache) Info() filecache.CacheInfo {
	i := c.main.Info()
	if c.protected != nil {
		p := c.protected.Info()
		i.Filled += p.Filled
		i.NumItems += p.NumItems
		if i.Capacity >= 0 && p.Capacity >= 0 {
			i.Capacity += p.Capacity
		} else {
			i.Capacity = -1
		}
	}
	return i
}

// ProtectedInfo is the protected cache's alone, if there's one.
func (c *PieceCache) ProtectedInfo() (filecache.CacheInfo, bool) {
	if c.protected == nil {
		return filecache.CacheInfo{}, false
	}
	return c.protected.Info(), true
}

// WalkItems calls f for every piece in either cache.
func (c *PieceCache) WalkItems(f func(filecache.ItemInfo)) {
	c.main.WalkItems(f)
	if c.protected != nil {
		c.protected.WalkItems(f)
	}
}

// Stat is the piece at p, in whichever cache holds it.
func (c *PieceCache) Stat(p string) (os.FileInfo, error) {
	fi, err := c.main.Stat(p)
	if err == nil || c.protected == nil {
		return fi, err
	}
	return c.protected.Stat(p)
}

// Remove removes the piece at p from both caches.
func (c *PieceCache) Remove(p string) error {
	err := c.main.Remove(p)
	if c.protected != nil {
		err = errors.Join(err, c.protected.Remove(p))
	}
	return err
}

// capacity is what the client is told the cache holds.
func (c *PieceCache) capacity() (int64, bool) {
	if i := c.Info(); i.Capacity >= 0 {
		return i.Capacity, true
	}
	return 0, false
}

func (c *PieceCache) isStart(hexHash string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.starts[hexHash]
	return ok
}

// protect marks the start pieces of info's files to be kept in the protected cache, moving
// any the main cache holds there.
func (c *PieceCache) protect(info *metainfo.Info) {
	if c.protected == nil || info == nil || info.PieceLength <= 0 {
		return
	}
	var hashes []string
	for _, f := range info.UpvertedFiles() {
		if f.Length < minProtectedFile {
			continue
		}
		for _, r := range fs.StartPieces(f.TorrentOffset, f.Length, info.PieceLength) {
			for i := r[0]; i < r[1] && i < info.NumPieces(); i++ {
				if h := info.Piece(i).V1Hash(); h.Ok {
					hashes = append(hashes, hex.EncodeToString(h.Value[:]))
				}
			}
		}
	}
	c.mu.Lock()
	for _, h := range hashes {
		c.starts[h] = struct{}{}
	}
	c.mu.Unlock()
	for _, h := range hashes {
		c.moveToProtected(h)
	}
}

// moveToProtected moves a start piece the main cache holds into the protected cache, so turning
// protection on, or a torrent loaded again, costs no download.
func (c *PieceCache) moveToProtected(hexHash string) {
	key := path.Join(completedDir, hexHash)
	if c.mainRoot == "" || c.protectedRoot == "" {
		return
	}
	if _, err := c.main.Stat(key); err != nil {
		return
	}
	if _, err := c.protected.Stat(key); err == nil {
		_ = c.main.Remove(key)
		return
	}
	to := filepath.Join(c.protectedRoot, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		return
	}
	if err := os.Rename(filepath.Join(c.mainRoot, filepath.FromSlash(key)), to); err != nil {
		log.Debug().Err(err).Str("piece", hexHash).Msg("couldn't move a start piece to the protected cache")
		return
	}
	// the main cache forgets the piece, the protected one learns of it
	_ = c.main.Remove(key)
	if f, err := c.protected.OpenFile(key, os.O_RDONLY); err == nil {
		_ = f.Close()
	}
}

// provider routes each complete piece to the cache it belongs in, and the parts of pieces still
// downloading to partial.
func (c *PieceCache) provider(partial resource.Provider) resource.Provider {
	return routedProvider{c: c, main: c.main.AsResourceProvider(), protected: c.protectedProvider(), partial: partial}
}

func (c *PieceCache) protectedProvider() resource.Provider {
	if c.protected == nil {
		return nil
	}
	return c.protected.AsResourceProvider()
}

type routedProvider struct {
	c                        *PieceCache
	main, protected, partial resource.Provider
}

func (p routedProvider) NewInstance(loc string) (resource.Instance, error) {
	if loc == partialDir || len(loc) > len(partialDir) && loc[:len(partialDir)+1] == partialDir+"/" {
		return p.partial.NewInstance(loc)
	}
	if p.protected != nil && path.Dir(loc) == completedDir && p.c.isStart(path.Base(loc)) {
		return p.protected.NewInstance(loc)
	}
	return p.main.NewInstance(loc)
}

// protectingClient tells the cache each torrent's start pieces as it opens, before any piece
// of it is read or written.
type protectingClient struct {
	storage.ClientImpl
	c *PieceCache
}

func (p protectingClient) OpenTorrent(ctx context.Context, info *metainfo.Info, ih metainfo.Hash) (storage.TorrentImpl, error) {
	p.c.protect(info)
	return p.ClientImpl.OpenTorrent(ctx, info, ih)
}
