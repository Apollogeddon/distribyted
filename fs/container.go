package fs

type ContainerFs struct {
	s *storage
}

func NewContainerFs(fss map[string]Filesystem) (*ContainerFs, error) {
	s := newStorage(SupportedFactories)
	_ = s.Add(&Dir{}, "/")
	for p, fs := range fss {
		if err := s.AddFS(fs, p); err != nil {
			return nil, err
		}
	}

	return &ContainerFs{s: s}, nil
}

func (fs *ContainerFs) Open(filename string) (File, error) {
	return fs.s.Get(filename)
}

func (fs *ContainerFs) ReadDir(path string) (map[string]File, error) {
	return fs.s.Children(path)
}

func (fs *ContainerFs) Link(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	return fs.s.Add(f, newpath)
}

func (fs *ContainerFs) Rename(oldpath, newpath string) error {
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
	return fs.s.Add(&Dir{}, path)
}

func (fs *ContainerFs) Rmdir(path string) error {
	return fs.s.Remove(path)
}
