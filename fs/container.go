package fs

import "sync"

type ContainerFs struct {
	mu sync.RWMutex
	s  *storage
}

func NewContainerFs(fss map[string]Filesystem) (*ContainerFs, error) {
	cfs := &ContainerFs{
		s: newStorage(SupportedFactories),
	}
	_ = cfs.s.Add(&Dir{}, "/")
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

	return fs.s.Add(f, newpath)
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

	return fs.s.Remove(oldpath)
}

func (fs *ContainerFs) Mkdir(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Add(&Dir{}, path)
}

func (fs *ContainerFs) Rmdir(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Remove(path)
}
