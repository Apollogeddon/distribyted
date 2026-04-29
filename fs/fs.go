package fs

import (
	"os"
	"time"

	"github.com/Apollogeddon/distribyted/iio"
)

type File interface {
	IsDir() bool
	Size() int64

	iio.Reader
	Close() error
}

type Filesystem interface {
	// Open opens the named file for reading. If successful, methods on the
	// returned file can be used for reading; the associated file descriptor has
	// mode O_RDONLY.
	Open(filename string) (File, error)

	// ReadDir reads the directory named by dirname and returns a list of
	// directory entries.
	ReadDir(path string) (map[string]File, error)

	// Link creates a new link (hard link) for the existing path.
	Link(oldpath, newpath string) error

	// Rename renames a file or directory.
	Rename(oldpath, newpath string) error

	// Mkdir creates a new directory.
	Mkdir(path string) error

	// Rmdir removes a directory.
	Rmdir(path string) error

	// Create creates a new file.
	Create(path string) error

	// Remove removes a file.
	Remove(path string) error
}

type fileInfo struct {
	name  string
	size  int64
	isDir bool
}

func NewFileInfo(name string, size int64, isDir bool) *fileInfo {
	return &fileInfo{
		name:  name,
		size:  size,
		isDir: isDir,
	}
}

func (fi *fileInfo) Name() string {
	return fi.name
}

func (fi *fileInfo) Size() int64 {
	return fi.size
}

func (fi *fileInfo) Mode() os.FileMode {
	if fi.isDir {
		return 0777 | os.ModeDir
	}

	return 0777
}

func (fi *fileInfo) ModTime() time.Time {
	// TODO fix it
	return time.Now()
}

func (fi *fileInfo) IsDir() bool {
	return fi.isDir
}

func (fi *fileInfo) Sys() interface{} {
	return nil
}
