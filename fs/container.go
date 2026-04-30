package fs

import "sync"

type ContainerFs struct {
	mu sync.RWMutex
	s  *storage

	onLinkAdded   func(oldpath, newpath string)
	onLinkRemoved func(path string)
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

func NewContainerFs(fss map[string]Filesystem) (*ContainerFs, error) {
	cfs := &ContainerFs{
		s: newStorage(SupportedFactories),
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
	return fs.s.Get(filename)
}

func (fs *ContainerFs) ReadDir(path string) (map[string]File, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.s.Children(path)
}

func (fs *ContainerFs) Link(oldpath, newpath string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		return err
	}

	if fs.onLinkAdded != nil {
		fs.onLinkAdded(oldpath, newpath)
	}

	return nil
}

func (fs *ContainerFs) Rename(oldpath, newpath string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		return err
	}

	if err := fs.s.Remove(oldpath); err != nil {
		return err
	}

	if fs.onLinkAdded != nil {
		fs.onLinkAdded(oldpath, newpath)
	}

	if fs.onLinkRemoved != nil {
		fs.onLinkRemoved(oldpath)
	}

	return nil
}

func (fs *ContainerFs) Mkdir(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := fs.s.Add(&Dir{}, path); err != nil {
		return err
	}

	if fs.onLinkAdded != nil {
		fs.onLinkAdded("", path) // Empty oldpath signifies a directory creation
	}

	return nil
}

func (fs *ContainerFs) Rmdir(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := fs.s.Remove(path); err != nil {
		return err
	}

	if fs.onLinkRemoved != nil {
		fs.onLinkRemoved(path)
	}

	return nil
}

func (fs *ContainerFs) Create(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := fs.s.Add(NewMemoryFile(nil), path); err != nil {
		return err
	}

	if fs.onLinkAdded != nil {
		fs.onLinkAdded("", path)
	}

	return nil
}

func (fs *ContainerFs) Remove(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := fs.s.Remove(path); err != nil {
		return err
	}

	if fs.onLinkRemoved != nil {
		fs.onLinkRemoved(path)
	}

	return nil
}

func (fs *ContainerFs) RemoveByHash(h string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.s.RemoveByHash(h)
}
