package torrent

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"sync"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/iio"
)

var _ http.FileSystem = &HTTPFS{}

type HTTPFS struct {
	fs dfs.Filesystem
}

func NewHTTPFS(fs dfs.Filesystem) *HTTPFS {
	return &HTTPFS{fs: fs}
}

func (fs *HTTPFS) Open(name string) (http.File, error) {
	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	fi := dfs.NewFileInfo(name, f.Size(), f.IsDir())

	// a folder is listed on its first Readdir, so serving a file never lists anything
	return newHTTPFile(f, func() ([]os.FileInfo, error) {
		return fs.filesToFileInfo(name)
	}, fi), nil
}

func (fs *HTTPFS) filesToFileInfo(path string) ([]fs.FileInfo, error) {
	files, err := fs.fs.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var out []os.FileInfo
	for n, f := range files {
		out = append(out, dfs.NewFileInfo(n, f.Size(), f.IsDir()))
	}

	return out, nil
}

var _ http.File = &httpFile{}

type httpFile struct {
	iio.ReaderSeeker

	mu sync.Mutex
	// dirPos, dirContent and listed are protected by mu.
	dirPos     int
	dirContent []os.FileInfo
	listed     bool
	list       func() ([]fs.FileInfo, error)

	fi fs.FileInfo
}

func newHTTPFile(f dfs.File, list func() ([]fs.FileInfo, error), fi fs.FileInfo) *httpFile {
	return &httpFile{
		list: list,
		fi:   fi,

		ReaderSeeker: iio.NewSeekerWrapper(f, f.Size()),
	}
}

func (f *httpFile) Readdir(count int) ([]fs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.fi.IsDir() {
		return nil, os.ErrInvalid
	}

	if !f.listed {
		fis, err := f.list()
		if err != nil {
			return nil, err
		}
		f.dirContent = fis
		f.listed = true
	}

	old := f.dirPos
	if old >= len(f.dirContent) {
		// The os.File Readdir docs say that at the end of a directory,
		// the error is io.EOF if count > 0 and nil if count <= 0.
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	if count > 0 {
		f.dirPos += count
		if f.dirPos > len(f.dirContent) {
			f.dirPos = len(f.dirContent)
		}
	} else {
		f.dirPos = len(f.dirContent)
		old = 0
	}

	return f.dirContent[old:f.dirPos], nil
}

func (f *httpFile) Stat() (fs.FileInfo, error) {
	return f.fi, nil
}
