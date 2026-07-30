package fs

import "sync"

type ContainerFs struct {
	mu sync.RWMutex
	s  *storage

	onLinkAdded      func(oldpath, newpath string)
	onLinkRemoved    func(path string)
	onLinkRenamed    func(oldpath, newpath string)
	onLastRefRemoved func(hash string)
}

func (fs *ContainerFs) OnLinkAdded(f func(oldpath, newpath string)) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.onLinkAdded = f
}

func (fs *ContainerFs) OnLinkRemoved(f func(path string)) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.onLinkRemoved = f
}

// OnLinkRenamed registers a callback fired when Rename moves a
// container-owned entry from oldpath to newpath. It exists separately from
// OnLinkAdded/OnLinkRemoved because Rename used to fire
// onLinkAdded(oldpath, newpath), persisting oldpath itself as newpath's
// source — but oldpath stops existing the moment the rename completes. On
// the next restart the loader tries to reconstruct the link by calling
// Link(oldpath, newpath), which fails forever since oldpath is gone,
// permanently orphaning the record. The handler wired to this callback
// must instead look up whatever oldpath's own source was (if it was itself
// a link) and re-persist *that* under newpath.
func (fs *ContainerFs) OnLinkRenamed(f func(oldpath, newpath string)) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.onLinkRenamed = f
}

// OnLastReferenceRemoved registers a callback fired when removing a path
// leaves no other entry in this filesystem's own storage (i.e. not counting
// files served by mounted routes, only directly-added entries such as
// links) matching that file's hash. This is how a hard-linked entry being
// deleted — the only delete path Radarr/Sonarr and a raw FUSE unlink can
// reach — can still trigger a full torrent teardown.
func (fs *ContainerFs) OnLastReferenceRemoved(f func(hash string)) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.onLastRefRemoved = f
}

func NewContainerFs(fss map[string]Filesystem) (*ContainerFs, error) {
	cfs := &ContainerFs{
		s: newStorage(GetSupportedFactories()),
	}
	for p, fs := range fss {
		if err := cfs.AddFS(p, fs); err != nil {
			return nil, err
		}
	}

	return cfs, nil
}

func (fs *ContainerFs) AddFS(p string, fss Filesystem) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.AddFS(fss, p)
}

func (fs *ContainerFs) Open(filename string) (File, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	f, err := fs.s.Get(filename)
	if err != nil {
		return nil, err
	}
	// Create a fresh handle so each Open gets its own reader and position,
	// preventing shared-state bugs when the same file is stored via a link.
	if h, ok := f.(*torrentFileHandle); ok {
		return h.NewHandle(), nil
	}
	return f, nil
}

func (fs *ContainerFs) ReadDir(path string) (map[string]File, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.s.Children(path)
}

// IsOwned reports whether path can safely be removed or renamed through
// this ContainerFs, as opposed to being route-mounted content that must be
// managed through its route instead. See storage.IsOwned.
func (fs *ContainerFs) IsOwned(path string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.s.IsOwned(path)
}

func (fs *ContainerFs) Link(oldpath, newpath string) error {
	fs.mu.Lock()
	f, err := fs.s.Get(oldpath)
	if err != nil {
		fs.mu.Unlock()
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		fs.mu.Unlock()
		return err
	}

	onLinkAdded := fs.onLinkAdded
	fs.mu.Unlock()

	// Callback runs with the lock released: like Remove, a caller-supplied
	// onLinkAdded may re-enter this ContainerFs (e.g. to persist the link),
	// which would deadlock if fs.mu were still held.
	if onLinkAdded != nil {
		onLinkAdded(oldpath, newpath)
	}

	return nil
}

func (fs *ContainerFs) Rename(oldpath, newpath string) error {
	fs.mu.Lock()
	f, err := fs.s.Get(oldpath)
	if err != nil {
		fs.mu.Unlock()
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		fs.mu.Unlock()
		return err
	}

	if err := fs.s.Remove(oldpath); err != nil {
		fs.mu.Unlock()
		return err
	}

	onLinkRenamed := fs.onLinkRenamed
	fs.mu.Unlock()

	// Callback runs with the lock released — see Remove/Link for why.
	if onLinkRenamed != nil {
		onLinkRenamed(oldpath, newpath)
	}

	return nil
}

func (fs *ContainerFs) Mkdir(path string) error {
	fs.mu.Lock()
	if err := fs.s.Add(&Dir{}, path); err != nil {
		fs.mu.Unlock()
		return err
	}

	onLinkAdded := fs.onLinkAdded
	fs.mu.Unlock()

	// Callback runs with the lock released — see Remove/Link for why.
	if onLinkAdded != nil {
		onLinkAdded("", path) // Empty oldpath signifies a directory creation
	}

	return nil
}

func (fs *ContainerFs) Rmdir(path string) error {
	fs.mu.Lock()
	if err := fs.s.Remove(path); err != nil {
		fs.mu.Unlock()
		return err
	}

	onLinkRemoved := fs.onLinkRemoved
	fs.mu.Unlock()

	// Callback runs with the lock released — see Remove/Link for why.
	if onLinkRemoved != nil {
		onLinkRemoved(path)
	}

	return nil
}

func (fs *ContainerFs) Create(path string) error {
	fs.mu.Lock()
	if err := fs.s.Add(NewMemoryFile(nil), path); err != nil {
		fs.mu.Unlock()
		return err
	}

	onLinkAdded := fs.onLinkAdded
	fs.mu.Unlock()

	// Callback runs with the lock released — see Remove/Link for why.
	if onLinkAdded != nil {
		onLinkAdded("", path)
	}

	return nil
}

func (fs *ContainerFs) Remove(path string) error {
	fs.mu.Lock()

	hash := ""
	if f, err := fs.s.Get(path); err == nil {
		hash = f.Hash()
	}

	removed, err := fs.s.RemovePaths(path)
	if err != nil {
		fs.mu.Unlock()
		return err
	}

	lastRef := hash != "" && !fs.s.HasHash(hash)
	onLinkRemoved := fs.onLinkRemoved
	onLastRefRemoved := fs.onLastRefRemoved
	fs.mu.Unlock()

	// Callbacks run with the lock released: onLastRefRemoved can trigger a
	// full torrent teardown (Service.RemoveFromHashOnly), which re-enters
	// this filesystem via RemoveByHash — holding fs.mu across that call
	// would deadlock.
	//
	// Fire onLinkRemoved for every path RemovePaths actually deleted, not
	// just the one the caller named: removing the last entry in a directory
	// prunes that now-empty parent too, and if the parent has its own
	// persisted link record (e.g. created via Mkdir), that record must be
	// cleaned up as well or it's orphaned exactly like the path itself
	// would be.
	if onLinkRemoved != nil {
		for _, p := range removed {
			onLinkRemoved(p)
		}
	}

	if lastRef && onLastRefRemoved != nil {
		onLastRefRemoved(hash)
	}

	return nil
}

// RemoveByHash removes every entry matching hash h — used when a torrent is
// torn down, to cascade-remove both its route-mounted entry and any virtual
// links pointing at it. It fires onLinkRemoved for every path removed, the
// same as Remove, so the persisted link DB stays in sync with the live
// tree; without this, links to a deleted torrent's files would survive in
// the DB forever with no way to delete them (their tree entry is already
// gone, so a later Remove(path) would just fail with os.ErrNotExist).
//
// It deliberately does NOT fire onLastRefRemoved: this method is itself
// downstream of a torrent teardown (Service.RemoveFromHash ->
// OnTorrentRemoved listener -> RemoveByHash), so firing it would call back
// into Service.RemoveFromHashOnly -> RemoveFromHash -> RemoveByHash again.
// That second pass happens to be a no-op (the hash is already gone from
// Stats, so GetRouteFromHash returns "" and it errors out) but only by
// accident, and it would log a spurious warning on every single torrent
// deletion.
func (fs *ContainerFs) RemoveByHash(h string) {
	fs.mu.Lock()
	removed := dedup(fs.s.RemoveByHash(h))
	onLinkRemoved := fs.onLinkRemoved
	fs.mu.Unlock()

	if onLinkRemoved != nil {
		for _, p := range removed {
			onLinkRemoved(p)
		}
	}
}

func dedup(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	seen := make(map[string]struct{}, len(paths))
	out := paths[:0]
	for _, p := range paths {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
