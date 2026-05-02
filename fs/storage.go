package fs

import (
	"os"
	"path"
	"strings"
	"sync"
)

const separator = "/"

type FsFactory func(f File) (Filesystem, error)

var SupportedFactories = map[string]FsFactory{
	".zip": func(f File) (Filesystem, error) {
		return NewArchive(f, f.Size(), &Zip{}), nil
	},
	".rar": func(f File) (Filesystem, error) {
		return NewArchive(f, f.Size(), &Rar{}), nil
	},
	".7z": func(f File) (Filesystem, error) {
		return NewArchive(f, f.Size(), &SevenZip{}), nil
	},
}

type storage struct {
	mu        sync.RWMutex
	factories map[string]FsFactory

	files       map[string]File
	filesystems map[string]Filesystem
	children    map[string]map[string]File
}

func newStorage(factories map[string]FsFactory) *storage {
	s := &storage{
		files:       make(map[string]File),
		children:    make(map[string]map[string]File),
		filesystems: make(map[string]Filesystem),
		factories:   factories,
	}

	_ = s.Add(&Dir{}, separator)
	return s
}

func (s *storage) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.files = make(map[string]File)
	s.children = make(map[string]map[string]File)
	s.filesystems = make(map[string]Filesystem)

	_ = s.addLocked(&Dir{}, "/")
}

func (s *storage) Has(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasLocked(path)
}

func (s *storage) hasLocked(p string) bool {
	p = clean(p)

	f := s.files[p]
	if f != nil {
		return true
	}

	if f, _ := s.getFileFromFsLocked(p); f != nil {
		return true
	}

	return false
}

func (s *storage) AddFS(fs Filesystem, p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p = clean(p)
	if s.hasLocked(p) {
		if dir, err := s.getLocked(p); err == nil {
			if !dir.IsDir() {
				return os.ErrExist
			}
		}

		return nil
	}

	s.filesystems[p] = fs
	return s.createParentLocked(p, &Dir{})
}

func (s *storage) Add(f File, p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(f, p)
}

func (s *storage) addLocked(f File, p string) error {
	p = clean(p)
	if s.hasLocked(p) {
		if dir, err := s.getLocked(p); err == nil {
			if !dir.IsDir() {
				return os.ErrExist
			}
		}

		return nil
	}

	if p == separator {
		s.files[p] = f
		f.SetIno(GenerateIno())
		f.IncNlink()
		return nil
	}

	ext := path.Ext(p)
	if ffs := s.factories[ext]; ffs != nil {
		fs, err := ffs(f)
		if err != nil {
			return err
		}

		s.filesystems[p] = fs
		f.SetIno(GenerateIno())
		f.IncNlink()
	} else {
		s.files[p] = f
		f.SetIno(GenerateIno())
		f.IncNlink()
	}

	return s.createParentLocked(p, f)
}

func (s *storage) Remove(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p = clean(p)
	f, ok := s.files[p]
	if !ok {
		// Check filesystems
		if _, ok := s.filesystems[p]; !ok {
			return os.ErrNotExist
		}
	} else {
		f.DecNlink()
	}

	delete(s.files, p)
	delete(s.filesystems, p)

	base, filename := path.Split(p)
	base = clean(base)

	if children, ok := s.children[base]; ok {
		delete(children, filename)
		// Prune empty parent directory recursively
		if len(s.children[base]) == 0 && base != separator {
			_ = s.removeLocked(base)
		}
	}

	return nil
}

func (s *storage) removeLocked(p string) error {
	f, ok := s.files[p]
	if !ok {
		// Check filesystems
		if _, ok := s.filesystems[p]; !ok {
			return os.ErrNotExist
		}
	} else {
		f.DecNlink()
	}

	delete(s.files, p)
	delete(s.filesystems, p)

	base, filename := path.Split(p)
	base = clean(base)

	if children, ok := s.children[base]; ok {
		delete(children, filename)
		// Prune empty parent directory recursively
		if len(s.children[base]) == 0 && base != separator {
			_ = s.removeLocked(base)
		}
	}

	return nil
}

func (s *storage) RemoveByHash(h string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for p, f := range s.files {
		if f.MatchHash(h) {
			_ = s.removeLocked(p)
		}
	}
}

func (s *storage) createParentLocked(p string, f File) error {
	base, filename := path.Split(p)
	base = clean(base)

	if err := s.addLocked(&Dir{}, base); err != nil {
		return err
	}

	if _, ok := s.children[base]; !ok {
		s.children[base] = make(map[string]File)
	}

	if filename != "" {
		s.children[base][filename] = f
	}

	return nil
}

func (s *storage) Children(path string) (map[string]File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path = clean(path)

	l := make(map[string]File)

	// Get children from sub-filesystems
	files, err := s.getDirFromFsLocked(path)
	if err == nil {
		for n, f := range files {
			l[n] = f
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Merge with children from the container itself
	for n, f := range s.children[path] {
		l[n] = f
	}

	if len(l) == 0 && !s.hasLocked(path) {
		return nil, os.ErrNotExist
	}

	return l, nil
}

func (s *storage) Get(path string) (File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getLocked(path)
}

func (s *storage) getLocked(path string) (File, error) {
	path = clean(path)
	if !s.hasLocked(path) {
		return nil, os.ErrNotExist
	}

	file, ok := s.files[path]
	if ok {
		return file, nil
	}

	return s.getFileFromFsLocked(path)
}

func (s *storage) getFileFromFsLocked(p string) (File, error) {
	for fsp, fs := range s.filesystems {
		if strings.HasPrefix(p, fsp) {
			return fs.Open(separator + strings.TrimPrefix(p, fsp))
		}
	}

	return nil, os.ErrNotExist
}

func (s *storage) getDirFromFsLocked(p string) (map[string]File, error) {
	if p == "/" {
		return nil, os.ErrNotExist
	}

	for fsp, fs := range s.filesystems {
		if strings.HasPrefix(p, fsp) {
			path := strings.TrimPrefix(p, fsp)
			return fs.ReadDir(path)
		}
	}

	return nil, os.ErrNotExist
}

func clean(p string) string {
	return path.Clean(separator + strings.ReplaceAll(p, "\\", "/"))
}
