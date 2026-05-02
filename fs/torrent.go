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

var _ Filesystem = &TorrentFS{}

type TorrentFS struct {
	mu          sync.Mutex
	s           *storage
	ts          map[string]Torrent
	readTimeout int
}

func NewTorrent(readTimeout int) *TorrentFS {
	return &TorrentFS{
		s:           newStorage(SupportedFactories),
		ts:          make(map[string]Torrent),
		readTimeout: readTimeout,
	}
}

func (fs *TorrentFS) AddTorrent(t Torrent) {
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

func (fs *TorrentFS) addFiles(t Torrent) {
	ih := t.InfoHash().HexString()
	for _, file := range t.Files() {
		tf := &torrentFile{
			hash:       ih,
			readerFunc: file.NewReader,
			len:        file.Length(),
			timeout:    fs.readTimeout,
		}
		tf.SetIno(HashIno(ih + file.Path()))
		_ = fs.s.Add(tf, file.Path())
	}
}

func (fs *TorrentFS) RemoveTorrent(h string) {
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

func (fs *TorrentFS) Open(filename string) (File, error) {
	f, err := fs.s.Get(filename)
	if err != nil {
		return nil, err
	}

	if tf, ok := f.(*torrentFile); ok {
		return tf.NewHandle(), nil
	}

	return f, nil
}

func (fs *TorrentFS) ReadDir(path string) (map[string]File, error) {
	return fs.s.Children(path)
}

func (fs *TorrentFS) Link(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	return fs.s.Add(f, newpath)
}

func (fs *TorrentFS) Rename(oldpath, newpath string) error {
	f, err := fs.s.Get(oldpath)
	if err != nil {
		return err
	}

	if err := fs.s.Add(f, newpath); err != nil {
		return err
	}

	return fs.s.Remove(oldpath)
}

func (fs *TorrentFS) Mkdir(path string) error {
	return fs.s.Add(&Dir{}, path)
}

func (fs *TorrentFS) Rmdir(path string) error {
	f, err := fs.s.Get(path)
	if err != nil {
		return err
	}
	if !f.IsDir() {
		return os.ErrInvalid
	}

	return fs.s.Remove(path)
}

func (fs *TorrentFS) Create(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.s.Add(NewMemoryFile(nil), path)
}

func (fs *TorrentFS) Remove(path string) error {
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
	len        int64
	timeout    int
}

func (d *torrentFile) NewHandle() *torrentFileHandle {
	return &torrentFileHandle{
		torrentFile: d,
	}
}

func (d *torrentFile) Size() int64 {
	return d.len
}

func (d *torrentFile) IsDir() bool {
	return false
}

func (d *torrentFile) Close() error {
	return nil
}

func (d *torrentFile) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (d *torrentFile) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, io.EOF
}

func (d *torrentFile) MatchHash(hash string) bool {
	return d.hash == hash
}

var _ File = &torrentFileHandle{}

type torrentFileHandle struct {
	*torrentFile
	reader reader
	mu     sync.Mutex
}

func (h *torrentFileHandle) load() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.reader != nil {
		return
	}
	h.reader = newReadAtWrapper(h.readerFunc(), h.timeout)
}

func (h *torrentFileHandle) Read(p []byte) (n int, err error) {
	h.load()
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(
		time.Duration(h.timeout)*time.Second,
		func() {
			cancel()
		},
	)

	defer timer.Stop()

	return h.reader.ReadContext(ctx, p)
}

func (h *torrentFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	h.load()
	return h.reader.ReadAt(p, off)
}

func (h *torrentFileHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.reader != nil {
		return h.reader.Close()
	}
	return nil
}
