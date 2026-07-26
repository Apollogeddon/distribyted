package fs

import "sync"

type ContainerFs struct {
	mu sync.RWMutex
	s  *storage

	onLinkAdded      func(oldpath, newpath string)
	onLinkRemoved    func(path string)
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

	onLinkAdded := fs.onLinkAdded
	onLinkRemoved := fs.onLinkRemoved
	fs.mu.Unlock()

	// Callbacks run with the lock released — see Remove/Link for why.
	if onLinkAdded != nil {
		onLinkAdded(oldpath, newpath)
	}

	if onLinkRemoved != nil {
		onLinkRemoved(oldpath)
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

	if err := fs.s.Remove(path); err != nil {
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
	if onLinkRemoved != nil {
		onLinkRemoved(path)
	}

	if lastRef && onLastRefRemoved != nil {
		onLastRefRemoved(hash)
	}

	return nil
}

func (fs *ContainerFs) RemoveByHash(h string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.s.RemoveByHash(h)
}
