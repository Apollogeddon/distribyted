package fs

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/Apollogeddon/distribyted/iio"
	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

var _ loader = &Zip{}

type Zip struct {
}

func (fs *Zip) getFiles(reader iio.Reader, size int64) (map[string]*ArchiveFile, error) {
	zr, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, err
	}

	out := make(map[string]*ArchiveFile)
	for _, f := range zr.File {
		f := f
		if f.FileInfo().IsDir() {
			continue
		}

		rf := func() (iio.Reader, error) {
			zr, err := f.Open()
			if err != nil {
				return nil, err
			}

			return iio.NewDiskTeeReader(zr)
		}

		n := filepath.Join(string(os.PathSeparator), f.Name)
		af := NewArchiveFile(rf, f.FileInfo().Size())

		out[n] = af
	}

	return out, nil
}

var _ loader = &SevenZip{}

type SevenZip struct {
}

func (fs *SevenZip) getFiles(reader iio.Reader, size int64) (map[string]*ArchiveFile, error) {
	r, err := sevenzip.NewReader(reader, size)
	if err != nil {
		return nil, err
	}

	out := make(map[string]*ArchiveFile)
	for _, f := range r.File {
		f := f
		if f.FileInfo().IsDir() {
			continue
		}

		rf := func() (iio.Reader, error) {
			zr, err := f.Open()
			if err != nil {
				return nil, err
			}

			return iio.NewDiskTeeReader(zr)
		}

		af := NewArchiveFile(rf, f.FileInfo().Size())
		n := filepath.Join(string(os.PathSeparator), f.Name)

		out[n] = af
	}

	return out, nil
}

var _ loader = &Rar{}

type Rar struct {
}

func (fs *Rar) getFiles(reader iio.Reader, size int64) (map[string]*ArchiveFile, error) {
	r, err := rardecode.NewReader(iio.NewSeekerWrapper(reader, size))
	if err != nil {
		return nil, err
	}

	out := make(map[string]*ArchiveFile)
	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		rf := func() (iio.Reader, error) {
			return iio.NewDiskTeeReader(r)
		}

		n := filepath.Join(string(os.PathSeparator), header.Name)

		af := NewArchiveFile(rf, header.UnPackedSize)

		out[n] = af
	}

	return out, nil
}

type loader interface {
	getFiles(r iio.Reader, size int64) (map[string]*ArchiveFile, error)
}

var _ Filesystem = &archive{}

type archive struct {
	r iio.Reader
	s *storage

	size int64
	once sync.Once
	l    loader
}

func NewArchive(r iio.Reader, size int64, l loader) *archive {
	return &archive{
		r:    r,
		s:    newStorage(nil),
		size: size,
		l:    l,
	}
}

func (fs *archive) loadOnce() error {
	var errOut error
	fs.once.Do(func() {
		files, err := fs.l.getFiles(fs.r, fs.size)
		if err != nil {
			errOut = err
			return
		}

		for name, file := range files {
			if err := fs.s.Add(file, name); err != nil {
				errOut = err
				return
			}
		}
	})

	return errOut
}

func (fs *archive) Open(filename string) (File, error) {
	if filename == string(os.PathSeparator) {
		return &Dir{}, nil
	}

	if err := fs.loadOnce(); err != nil {
		return nil, err
	}

	f, err := fs.s.Get(filename)
	if err != nil {
		return nil, err
	}

	if af, ok := f.(*ArchiveFile); ok {
		return af.NewHandle(), nil
	}

	return f, nil
}

func (fs *archive) ReadDir(path string) (map[string]File, error) {
	if err := fs.loadOnce(); err != nil {
		return nil, err
	}

	return fs.s.Children(path)
}

func (fs *archive) Link(oldpath, newpath string) error {
	return os.ErrPermission
}

func (fs *archive) Rename(oldpath, newpath string) error {
	return os.ErrPermission
}

func (fs *archive) Mkdir(path string) error {
	return os.ErrPermission
}

func (fs *archive) Rmdir(path string) error {
	return os.ErrPermission
}

func (fs *archive) Create(path string) error {
	return fs.s.Add(NewMemoryFile(nil), path)
}

func (fs *archive) Remove(path string) error {
	return fs.s.Remove(path)
}

var _ File = &ArchiveFile{}

func NewArchiveFile(readerFunc func() (iio.Reader, error), len int64) *ArchiveFile {
	return &ArchiveFile{
		readerFunc: readerFunc,
		len:        len,
	}
}

type ArchiveFile struct {
	BaseFile
	readerFunc func() (iio.Reader, error)
	len        int64
}

func (d *ArchiveFile) NewHandle() *ArchiveFileHandle {
	return &ArchiveFileHandle{
		ArchiveFile: d,
	}
}

func (d *ArchiveFile) Size() int64 {
	return d.len
}

func (d *ArchiveFile) IsDir() bool {
	return false
}

func (d *ArchiveFile) Close() (err error) {
	return nil
}

func (d *ArchiveFile) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (d *ArchiveFile) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, io.EOF
}

var _ File = &ArchiveFileHandle{}

type ArchiveFileHandle struct {
	*ArchiveFile
	reader iio.Reader
	mu     sync.Mutex
}

func (h *ArchiveFileHandle) load() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.reader != nil {
		return nil
	}
	r, err := h.readerFunc()
	if err != nil {
		return err
	}

	h.reader = r

	return nil
}

func (h *ArchiveFileHandle) Read(p []byte) (n int, err error) {
	if err := h.load(); err != nil {
		return 0, err
	}

	return h.reader.Read(p)
}

func (h *ArchiveFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	if err := h.load(); err != nil {
		return 0, err
	}

	return h.reader.ReadAt(p, off)
}

func (h *ArchiveFileHandle) Close() (err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.reader != nil {
		err = h.reader.Close()
		h.reader = nil
	}

	return
}
