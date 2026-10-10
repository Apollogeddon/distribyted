package fs

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/Apollogeddon/distribyted/internal/iio"
	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

var _ loader = &Zip{}

type Zip struct{}

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

		rf := func() (io.Reader, error) {
			return f.Open()
		}

		n := filepath.Join(string(os.PathSeparator), f.Name) //nolint:gosec // G305: intentional archive path join
		af := NewArchiveFile(rf, f.FileInfo().Size())

		out[n] = af
	}

	return out, nil
}

var _ loader = &SevenZip{}

type SevenZip struct{}

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

		rf := func() (io.Reader, error) {
			return f.Open()
		}

		af := NewArchiveFile(rf, f.FileInfo().Size())
		n := filepath.Join(string(os.PathSeparator), f.Name) //nolint:gosec // G305: intentional archive path join

		out[n] = af
	}

	return out, nil
}

var _ loader = &Rar{}

type Rar struct{}

func (fs *Rar) getFiles(reader iio.Reader, size int64) (map[string]*ArchiveFile, error) {
	r, err := rardecode.NewReader(iio.NewSeekerWrapper(reader, size))
	if err != nil {
		return nil, err
	}

	out := make(map[string]*ArchiveFile)
	for {
		header, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.IsDir {
			continue
		}

		// a RAR archive can only be read in order, and listing it has already read r to the
		// end, so each entry is opened with a fresh reader skipped forward to it
		name := header.Name
		rf := func() (io.Reader, error) {
			return openRarEntry(reader, size, name)
		}

		n := filepath.Join(string(os.PathSeparator), header.Name) //nolint:gosec // G305: intentional archive path join

		af := NewArchiveFile(rf, header.UnPackedSize)

		out[n] = af
	}

	return out, nil
}

func openRarEntry(reader iio.Reader, size int64, name string) (io.Reader, error) {
	r, err := rardecode.NewReader(iio.NewSeekerWrapper(reader, size))
	if err != nil {
		return nil, err
	}
	for {
		header, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, os.ErrNotExist
			}
			return nil, err
		}
		if header.Name == name {
			return r, nil
		}
	}
}

type loader interface {
	getFiles(r iio.Reader, size int64) (map[string]*ArchiveFile, error)
}

var _ Filesystem = &archive{}

type archive struct {
	r iio.Reader
	s *storage

	size    int64
	mu      sync.Mutex
	loaded  bool
	loadErr error
	l       loader
}

func NewArchive(r iio.Reader, size int64, l loader) *archive {
	return &archive{
		r:    r,
		s:    newStorage(GetSupportedFactories()),
		size: size,
		l:    l,
	}
}

// loadOnce reads the archive's listing the first time it's needed. A corrupt archive stays
// failed, but a read that timed out, on a slow or empty swarm, is tried again next time
// rather than leaving the archive broken until restart.
func (fs *archive) loadOnce() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.loaded {
		return fs.loadErr
	}

	files, err := fs.l.getFiles(fs.r, fs.size)
	if err != nil {
		if errors.Is(err, ErrReadTimeout) || errors.Is(err, errReaderAbandoned) {
			return err
		}
		fs.loaded, fs.loadErr = true, err
		return err
	}

	for name, file := range files {
		if err := fs.s.Add(file, name); err != nil {
			fs.loaded, fs.loadErr = true, err
			return err
		}
	}
	fs.loaded = true
	return nil
}

// Close releases the reader the archive was opened with.
func (fs *archive) Close() error {
	if c, ok := fs.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (fs *archive) Open(filename string) (File, error) {
	if filename == separator || filename == string(os.PathSeparator) {
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

// Create and Remove refuse, like the other changes: an archive is read only, and removing
// an entry would only hide it until the archive is listed again.
func (fs *archive) Create(path string) error {
	return os.ErrPermission
}

func (fs *archive) Remove(path string) error {
	return os.ErrPermission
}

var _ File = &ArchiveFile{}

// ErrEntryTooLarge is reading an archive entry bigger than the extraction limit.
var ErrEntryTooLarge = errors.New("archive entry is larger than the extraction limit")

// extractLimit is the largest archive entry that is extracted to disk to be read; 0 means
// no limit. Archives are read by extracting each entry as it is read into a temporary file,
// so without a limit a crafted torrent could fill the disk.
var extractLimit atomic.Int64

// SetExtractLimit sets the largest archive entry, in bytes, that may be extracted. 0 means
// no limit.
func SetExtractLimit(n int64) { extractLimit.Store(max(n, 0)) }

func init() { SetExtractLimit(DefaultExtractLimit) }

// DefaultExtractLimit is the extraction limit when the configuration sets none.
const DefaultExtractLimit = 4 << 30

func NewArchiveFile(open func() (io.Reader, error), len int64) *ArchiveFile {
	return &ArchiveFile{
		open: open,
		len:  len,
	}
}

// ArchiveFile is an entry in an archive. Its handles share one extracted copy, made when
// the first of them reads and removed when the last of them closes.
type ArchiveFile struct {
	BaseFile
	open func() (io.Reader, error)
	len  int64

	// limit overrides the extraction limit for this entry; 0 uses SetExtractLimit's.
	limit int64

	mu     sync.Mutex
	shared iio.Reader
	users  int
}

// acquire returns the entry's extracted copy, starting it if no handle has it open.
func (d *ArchiveFile) acquire() (iio.Reader, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shared == nil {
		limit := d.limit
		if limit == 0 {
			limit = extractLimit.Load()
		}
		if limit > 0 && d.len > limit {
			return nil, fmt.Errorf("%w (%d bytes, limit %d)", ErrEntryTooLarge, d.len, limit)
		}
		raw, err := d.open()
		if err != nil {
			return nil, err
		}
		// a header can claim one size and the data run on; nothing past the claimed size is
		// written to disk
		src := limitedReadCloser{Reader: io.LimitReader(raw, d.len)}
		if c, ok := raw.(io.Closer); ok {
			src.Closer = c
		}
		r, err := iio.NewDiskTeeReader(src)
		if err != nil {
			_ = src.Close()
			return nil, err
		}
		d.shared = r
	}
	d.users++
	return d.shared, nil
}

func (d *ArchiveFile) release() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.users--
	if d.users > 0 || d.shared == nil {
		return nil
	}
	err := d.shared.Close()
	d.shared = nil
	return err
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func (l limitedReadCloser) Close() error {
	if l.Closer == nil {
		return nil
	}
	return l.Closer.Close()
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

// ArchiveFileHandle is one reader of an archive entry, with its own position in the
// entry's shared extracted copy.
type ArchiveFileHandle struct {
	*ArchiveFile
	mu     sync.Mutex
	reader iio.Reader
	closed bool
	pos    int64
}

// load returns the shared copy, joining it on first use. Callers use the reader it returns
// rather than reading h.reader again, which Close may set to nil meanwhile.
func (h *ArchiveFileHandle) load() (iio.Reader, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, os.ErrClosed
	}
	if h.reader != nil {
		return h.reader, nil
	}
	r, err := h.acquire()
	if err != nil {
		return nil, err
	}
	h.reader = r
	return r, nil
}

func (h *ArchiveFileHandle) Read(p []byte) (int, error) {
	r, err := h.load()
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	off := h.pos
	h.mu.Unlock()

	n, err := r.ReadAt(p, off)
	h.mu.Lock()
	h.pos = off + int64(n)
	h.mu.Unlock()
	if n > 0 && errors.Is(err, io.EOF) {
		err = nil
	}
	return n, err
}

func (h *ArchiveFileHandle) ReadAt(p []byte, off int64) (int, error) {
	r, err := h.load()
	if err != nil {
		return 0, err
	}
	return r.ReadAt(p, off)
}

func (h *ArchiveFileHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.reader == nil {
		return nil
	}
	h.reader = nil
	return h.release()
}
