package fs

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/iio"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/torrent"
)

var _ Filesystem = &Torrent{}

type Torrent struct {
	mu          sync.RWMutex
	ts          map[string]*torrent.Torrent
	s           *storage
	readTimeout int
}

func NewTorrent(readTimeout int) *Torrent {
	return &Torrent{
		s:           newStorage(SupportedFactories),
		ts:          make(map[string]*torrent.Torrent),
		readTimeout: readTimeout,
	}
}

func (fs *Torrent) AddTorrent(t *torrent.Torrent) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.ts[t.InfoHash().HexString()] = t

	if t.Info() != nil {
		fs.addFiles(t)
		return
	}

	go func() {
		<-t.GotInfo()
		fs.mu.Lock()
		defer fs.mu.Unlock()
		fs.addFiles(t)
	}()
}

func (fs *Torrent) addFiles(t *torrent.Torrent) {
	for _, file := range t.Files() {
		_ = fs.s.Add(&torrentFile{
			hash:       t.InfoHash().HexString(),
			readerFunc: file.NewReader,
			len:        file.Length(),
			timeout:    fs.readTimeout,
		}, file.Path())
	}
}

func (fs *Torrent) RemoveTorrent(h string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	delete(fs.ts, h)

	// Surgical removal: only remove files that belong to this hash
	for p, f := range fs.s.files {
		if f.MatchHash(h) {
			_ = fs.s.Remove(p)
		}
	}

	// Also cleanup directories that might have become empty or belonged to the torrent
	// Since storage.Remove handles parent cleanup if needed, we just need to make sure 
	// we didn't leave any top-level folders that were part of the torrent.
}

func (fs *Torrent) Open(filename string) (File, error) {
	return fs.s.Get(filename)
}

func (fs *Torrent) ReadDir(path string) (map[string]File, error) {
	return fs.s.Children(path)
}

func (fs *Torrent) Link(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	return fs.s.Add(f, newpath)
}

func (fs *Torrent) Rename(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		return err
	}

	return fs.s.Remove(oldpath)
}

func (fs *Torrent) Mkdir(path string) error {
	return fs.s.Add(&Dir{}, path)
}

func (fs *Torrent) Rmdir(path string) error {
	f, err := fs.s.Get(path)
	if err != nil {
		return err
	}
	if !f.IsDir() {
		return os.ErrInvalid
	}

	return fs.s.Remove(path)
}

func (fs *Torrent) Create(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Add(NewMemoryFile(nil), path)
}

func (fs *Torrent) Remove(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Remove(path)
}

type reader interface {
	iio.Reader
	missinggo.ReadContexter
}

type readAtWrapper struct {
	timeout int
	mu      sync.Mutex
	closed  bool

	torrent.Reader
	io.ReaderAt
	io.Closer
}

func newReadAtWrapper(r torrent.Reader, timeout int) reader {
	return &readAtWrapper{Reader: r, timeout: timeout}
}

func (rw *readAtWrapper) ReadAt(p []byte, off int64) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.closed {
		return 0, io.EOF
	}

	_, err := rw.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}

	return readAtLeast(rw, rw.timeout, p, len(p))
}

func readAtLeast(r missinggo.ReadContexter, timeout int, buf []byte, min int) (n int, err error) {
	if len(buf) < min {
		return 0, io.ErrShortBuffer
	}
	for n < min && err == nil {
		var nn int

		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(
			time.Duration(timeout)*time.Second,
			func() {
				cancel()
			},
		)

		nn, err = r.ReadContext(ctx, buf[n:])
		n += nn

		timer.Stop()
	}
	if n >= min {
		err = nil
	} else if n > 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (rw *readAtWrapper) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.closed {
		return nil
	}

	rw.closed = true
	return rw.Reader.Close()
}

var _ File = &torrentFile{}

type torrentFile struct {
	BaseFile
	hash       string
	readerFunc func() torrent.Reader
	reader     reader
	len        int64
	timeout    int
}

func (d *torrentFile) load() {
	if d.reader != nil {
		return
	}
	d.reader = newReadAtWrapper(d.readerFunc(), d.timeout)
}

func (d *torrentFile) Size() int64 {
	return d.len
}

func (d *torrentFile) IsDir() bool {
	return false
}

func (d *torrentFile) Close() error {
	var err error
	if d.reader != nil {
		err = d.reader.Close()
	}

	d.reader = nil

	return err
}

func (d *torrentFile) Read(p []byte) (n int, err error) {
	d.load()
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(
		time.Duration(d.timeout)*time.Second,
		func() {
			cancel()
		},
	)

	defer timer.Stop()

	return d.reader.ReadContext(ctx, p)
}

func (d *torrentFile) ReadAt(p []byte, off int64) (n int, err error) {
	d.load()
	return d.reader.ReadAt(p, off)
}

func (d *torrentFile) MatchHash(hash string) bool {
	return d.hash == hash
}
