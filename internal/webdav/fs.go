package webdav

import (
	"context"
	"hash/fnv"
	"io"
	"mime"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/iio"
	"github.com/rs/zerolog"
	"golang.org/x/net/webdav"
)

var _ webdav.FileSystem = &WebDAV{}

type WebDAV struct {
	fs  fs.Filesystem
	log zerolog.Logger
}

func newFS(fs fs.Filesystem, l zerolog.Logger) *WebDAV {
	return &WebDAV{fs: fs, log: l}
}

func (wd *WebDAV) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	p := "/" + name
	// TODO handle flag and permissions
	f, err := wd.lookupFile(p)
	if err != nil {
		return nil, err
	}

	wd.log.Info().Str("path", p).Msg("file opened")
	// a slash path: filepath.Base on Windows reads "//folder/file" as a volume name
	wdf := newFile(p, f, func() ([]os.FileInfo, error) {
		return wd.listDir(p)
	}, wd.log.With().Str("path", p).Logger())
	return wdf, nil
}

func (wd *WebDAV) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	p := "/" + name
	f, err := wd.lookupFile(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return newFileInfo(p, f), nil
}

func (wd *WebDAV) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	p := "/" + name
	return wd.fs.Mkdir(p)
}

func (wd *WebDAV) RemoveAll(ctx context.Context, name string) error {
	p := "/" + name
	f, err := wd.fs.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	isDir := f.IsDir()
	_ = f.Close()

	if !isDir {
		return wd.fs.Remove(p)
	}

	// a WebDAV DELETE of a collection deletes everything in it; removing only the folder
	// would fail on its contents, or orphan them
	children, err := wd.fs.ReadDir(p)
	if err != nil {
		return err
	}
	for child := range children {
		if err := wd.RemoveAll(ctx, path.Join(name, child)); err != nil {
			return err
		}
	}
	// removing a folder's last entry prunes the folder itself, which is the goal here
	if err := wd.fs.Rmdir(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (wd *WebDAV) Rename(ctx context.Context, oldName, newName string) error {
	return wd.fs.Rename("/"+oldName, "/"+newName)
}

func (wd *WebDAV) lookupFile(path string) (fs.File, error) {
	return wd.fs.Open(path)
}

func (wd *WebDAV) listDir(dir string) ([]os.FileInfo, error) {
	files, err := wd.fs.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []os.FileInfo
	for n, f := range files {
		out = append(out, newFileInfo(path.Join(dir, n), f))
	}

	return out, nil
}

var _ webdav.File = &webDAVFile{}

type webDAVFile struct {
	iio.Reader

	fi os.FileInfo

	mudp   sync.Mutex
	dirPos int

	mup        sync.Mutex
	pos        int64
	dirFunc    func() ([]os.FileInfo, error)
	dirContent []os.FileInfo

	log zerolog.Logger
}

func newFile(name string, f fs.File, df func() ([]os.FileInfo, error), l zerolog.Logger) *webDAVFile {
	return &webDAVFile{
		fi:      newFileInfo(name, f),
		dirFunc: df,
		Reader:  f,
		log:     l,
	}
}

func (wdf *webDAVFile) Close() error {
	wdf.log.Info().Msg("file closed")
	return wdf.Reader.Close()
}

func (wdf *webDAVFile) Readdir(count int) ([]os.FileInfo, error) {
	wdf.mudp.Lock()
	defer wdf.mudp.Unlock()

	if !wdf.fi.IsDir() {
		return nil, os.ErrInvalid
	}

	if wdf.dirContent == nil {
		dc, err := wdf.dirFunc()
		if err != nil {
			return nil, err
		}
		wdf.dirContent = dc
	}

	old := wdf.dirPos
	if old >= len(wdf.dirContent) {
		// The os.File Readdir docs say that at the end of a directory,
		// the error is io.EOF if count > 0 and nil if count <= 0.
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	if count > 0 {
		wdf.dirPos += count
		if wdf.dirPos > len(wdf.dirContent) {
			wdf.dirPos = len(wdf.dirContent)
		}
	} else {
		wdf.dirPos = len(wdf.dirContent)
		old = 0
	}

	return wdf.dirContent[old:wdf.dirPos], nil
}

func (wdf *webDAVFile) Stat() (os.FileInfo, error) {
	return wdf.fi, nil
}

func (wdf *webDAVFile) Read(p []byte) (int, error) {
	wdf.mup.Lock()
	defer wdf.mup.Unlock()

	n, err := wdf.ReadAt(p, wdf.pos)
	wdf.pos += int64(n)

	return n, err
}

func (wdf *webDAVFile) Seek(offset int64, whence int) (int64, error) {
	wdf.mup.Lock()
	defer wdf.mup.Unlock()

	switch whence {
	case io.SeekStart:
		wdf.pos = offset
	case io.SeekCurrent:
		wdf.pos += offset
	case io.SeekEnd:
		wdf.pos = wdf.fi.Size() + offset
	}

	return wdf.pos, nil
}

func (wdf *webDAVFile) Write(p []byte) (n int, err error) {
	return 0, webdav.ErrNotImplemented
}

type webDAVFileInfo struct {
	name  string
	size  int64
	isDir bool
	// path and hash make the ETag, worked out only when a client asks for it
	path string
	hash string
}

// startedAt is every file's modification time. The files have none of their own, and a
// time that changed on every request, as time.Now did, told clients each file had changed:
// resumed downloads restarted and nothing could be cached.
var startedAt = time.Now().Truncate(time.Second)

var (
	_ webdav.ETager       = &webDAVFileInfo{}
	_ webdav.ContentTyper = &webDAVFileInfo{}
)

// newFileInfo describes the file at path p. Name() is the last element only, as
// os.FileInfo requires: WebDAV clients show it as the entry's display name.
func newFileInfo(p string, f fs.File) *webDAVFileInfo {
	return &webDAVFileInfo{
		name:  path.Base(p),
		size:  f.Size(),
		isDir: f.IsDir(),
		path:  p,
		hash:  f.Hash(),
	}
}

// ETag is the same for the same file however often it's asked for, and across restarts:
// its path, its torrent and its size.
func (wdfi *webDAVFileInfo) ETag(context.Context) (string, error) {
	h := fnv.New64a()
	_, _ = io.WriteString(h, path.Clean("/"+wdfi.path))
	_, _ = io.WriteString(h, "\x00")
	_, _ = io.WriteString(h, wdfi.hash)
	b := make([]byte, 0, 36)
	b = append(b, '"')
	b = strconv.AppendUint(b, h.Sum64(), 16)
	b = append(b, '-')
	b = strconv.AppendInt(b, wdfi.size, 16)
	b = append(b, '"')
	return string(b), nil
}

// ContentType comes from the name. Without it, the WebDAV handler opens and reads the
// start of every file in a listing to guess, which for a torrent means downloading it.
func (wdfi *webDAVFileInfo) ContentType(context.Context) (string, error) {
	if wdfi.isDir {
		return "", webdav.ErrNotImplemented
	}
	if t := mime.TypeByExtension(path.Ext(wdfi.name)); t != "" {
		return t, nil
	}
	return "application/octet-stream", nil
}

func (wdfi *webDAVFileInfo) Name() string {
	return wdfi.name
}

func (wdfi *webDAVFileInfo) Size() int64 {
	return wdfi.size
}

func (wdfi *webDAVFileInfo) Mode() os.FileMode {
	if wdfi.isDir {
		return 0o777 | os.ModeDir
	}

	return 0o777
}

func (wdfi *webDAVFileInfo) ModTime() time.Time {
	return startedAt
}

func (wdfi *webDAVFileInfo) IsDir() bool {
	return wdfi.isDir
}

func (wdfi *webDAVFileInfo) Sys() interface{} {
	return nil
}
