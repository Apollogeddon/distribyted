package fuse

import (
	"errors"
	"io"
	"math"
	"os"
	"sync"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	dlog "github.com/Apollogeddon/distribyted/log"
)

type FS struct {
	fuse.FileSystemBase
	fh *fileHandler

	log zerolog.Logger
	uid uint32
	gid uint32
}

func NewFS(fs fs.Filesystem) fuse.FileSystemInterface {
	l := dlog.Logger("fuse")
	return &FS{
		fh:  &fileHandler{fs: fs},
		log: l,
	}
}

func (fs *FS) Statfs(path string, stat *fuse.Statfs_t) int {
	stat.Bsize = 4096
	stat.Frsize = 4096
	stat.Blocks = 100 * 1024 * 1024 * 1024 * 1024 / 4096 // 100TB
	stat.Bfree = 100 * 1024 * 1024 * 1024 * 1024 / 4096  // 100TB
	stat.Bavail = 100 * 1024 * 1024 * 1024 * 1024 / 4096 // 100TB
	return 0
}

func (fs *FS) Open(path string, flags int) (errc int, fh uint64) {
	fh, err := fs.fh.OpenHolder(path)
	if os.IsNotExist(err) {
		fs.log.Debug().Str(dlog.KeyPath, path).Msg("file does not exists")
		return -fuse.ENOENT, fhNone

	}
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error opening file")
		return -fuse.EIO, fhNone
	}

	return 0, fh
}

func (fs *FS) Opendir(path string) (errc int, fh uint64) {
	return fs.Open(path, 0)
}

func (fs *FS) Getattr(path string, stat *fuse.Stat_t, fh uint64) (errc int) {
	stat.Mode = 0777
	if path == "/" {
		stat.Mode |= fuse.S_IFDIR
		stat.Ino = 1
		stat.Nlink = 2
		return 0
	}

	file, err := fs.fh.GetFile(path, fh)
	if os.IsNotExist(err) {
		fs.log.Debug().Str(dlog.KeyPath, path).Msg("file does not exists")
		return -fuse.ENOENT

	}
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error getting holder when reading file attributes")
		return -fuse.EIO
	}

	if file.IsDir() {
		stat.Mode |= fuse.S_IFDIR
	} else {
		stat.Mode |= fuse.S_IFREG
		stat.Size = file.Size()
		stat.Blocks = (stat.Size + 511) / 512
	}

	stat.Ino = file.Ino()
	stat.Nlink = file.Nlink()
	if file.IsDir() && stat.Nlink < 2 {
		stat.Nlink = 2
	}
	stat.Blksize = 4096

	now := fuse.Now()
	stat.Atim = now
	stat.Mtim = now
	stat.Ctim = now
	stat.Birthtim = now

	uid, gid, _ := fs.getContext()
	stat.Uid = uid
	stat.Gid = gid

	return 0
}

func (fs *FS) getContext() (uint32, uint32, int) {
	if fs.uid != 0 || fs.gid != 0 {
		return fs.uid, fs.gid, 0
	}

	// Only call fuse.Getcontext if we are likely in a real fuse mount
	// This is a bit of a hack but avoids panics in unit tests on Windows
	defer func() {
		if r := recover(); r != nil {
			// Recover from panic in fuse.Getcontext (likely DLL issue in tests)
			_ = r
		}
	}()

	return fuse.Getcontext()
}

func (fs *FS) Create(path string, flags int, mode uint32) (errc int, fh uint64) {
	fs.log.Info().Str(dlog.KeyPath, path).Msg("creating file")
	err := fs.fh.fs.Create(path)
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error creating file")
		return -fuse.EIO, fhNone
	}

	return fs.Open(path, flags)
}

func (fs *FS) Write(path string, buf []byte, off int64, fh uint64) int {
	// We don't really support writing data, but we return success to satisfy Sonarr/Radarr
	return len(buf)
}

func (fs *FS) Truncate(path string, size int64, fh uint64) int {
	return 0
}

func (fs *FS) Mknod(path string, mode uint32, dev uint64) int {
	err := fs.fh.fs.Create(path)
	if err != nil {
		return -fuse.EIO
	}
	return 0
}

func (fs *FS) Chmod(path string, mode uint32) int {
	return 0
}

func (fs *FS) Chown(path string, uid uint32, gid uint32) int {
	return 0
}

func (fs *FS) Utimens(path string, tmsp []fuse.Timespec) int {
	return 0
}

func (fs *FS) Unlink(path string) int {
	fs.log.Info().Str(dlog.KeyPath, path).Msg("unlinking file")
	err := fs.fh.fs.Remove(path)
	if os.IsNotExist(err) {
		return -fuse.ENOENT
	}
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error unlinking file")
		return -fuse.EIO
	}
	return 0
}

func (fs *FS) Access(path string, mask uint32) int {
	return 0
}

func (fs *FS) Read(path string, dest []byte, off int64, fh uint64) int {
	file, err := fs.fh.GetFile(path, fh)
	if os.IsNotExist(err) {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("file not found on READ operation")
		return -fuse.ENOENT

	}
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error getting holder reading data from file")
		return -fuse.EIO
	}

	end := int(math.Min(float64(len(dest)), float64(int64(file.Size())-off)))
	if end < 0 {
		end = 0
	}

	buf := dest[:end]

	n, err := file.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error reading data")
		return -fuse.EIO
	}

	return n
}

func (fs *FS) Release(path string, fh uint64) int {
	if err := fs.fh.Remove(fh); err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error getting holder when releasing file")
		return -fuse.EIO
	}

	return 0
}

func (fs *FS) Releasedir(path string, fh uint64) int {
	return fs.Release(path, fh)
}

func (fs *FS) Link(oldpath string, newpath string) int {
	fs.log.Info().Str("old", oldpath).Str("new", newpath).Msg("linking file")
	err := fs.fh.fs.Link(oldpath, newpath)
	if os.IsNotExist(err) {
		return -fuse.ENOENT
	}
	if err != nil {
		fs.log.Error().Err(err).Str("oldpath", oldpath).Str("newpath", newpath).Msg("error linking file")
		return -fuse.EIO
	}

	return 0
}

func (fs *FS) Rename(oldpath string, newpath string) int {
	fs.log.Info().Str("old", oldpath).Str("new", newpath).Msg("renaming file")
	err := fs.fh.fs.Rename(oldpath, newpath)
	if os.IsNotExist(err) {
		return -fuse.ENOENT
	}
	if err != nil {
		fs.log.Error().Err(err).Str("oldpath", oldpath).Str("newpath", newpath).Msg("error renaming file")
		return -fuse.EIO
	}

	return 0
}

func (fs *FS) Mkdir(path string, mode uint32) int {
	fs.log.Info().Str(dlog.KeyPath, path).Msg("mkdir operation")
	err := fs.fh.fs.Mkdir(path)
	if os.IsExist(err) {
		return -fuse.EEXIST
	}
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error creating directory")
		return -fuse.EIO
	}

	return 0
}

func (fs *FS) Rmdir(path string) int {
	err := fs.fh.fs.Rmdir(path)
	if os.IsNotExist(err) {
		return -fuse.ENOENT
	}
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error removing directory")
		return -fuse.EIO
	}

	return 0
}

func (fs *FS) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64) (errc int) {
	fill(".", nil, 0)
	fill("..", nil, 0)

	//TODO improve this function to make use of fh index if possible
	paths, err := fs.fh.ListDir(path)
	if err != nil {
		fs.log.Error().Err(err).Str(dlog.KeyPath, path).Msg("error reading directory")
		return -fuse.ENOSYS
	}

	for _, p := range paths {
		if !fill(p, nil, 0) {
			fs.log.Error().Str(dlog.KeyPath, path).Msg("error adding directory")
			break
		}
	}

	return 0
}

const fhNone = ^uint64(0)

var ErrHolderEmpty = errors.New("file holder is empty")
var ErrBadHolderIndex = errors.New("holder index too big")

type fileHandler struct {
	mu     sync.RWMutex
	opened []fs.File
	fs     fs.Filesystem
}

func (fh *fileHandler) GetFile(path string, fhi uint64) (fs.File, error) {
	fh.mu.RLock()
	defer fh.mu.RUnlock()

	if fhi == fhNone {
		return fh.lookupFile(path)
	}
	return fh.get(fhi)
}

func (fh *fileHandler) ListDir(path string) ([]string, error) {
	fh.mu.RLock()
	defer fh.mu.RUnlock()

	var out []string
	files, err := fh.fs.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for p := range files {
		out = append(out, p)
	}

	return out, nil
}

func (fh *fileHandler) OpenHolder(path string) (uint64, error) {
	file, err := fh.lookupFile(path)
	if err != nil {
		return fhNone, err
	}

	fh.mu.Lock()
	defer fh.mu.Unlock()

	for i, old := range fh.opened {
		if old == nil {
			fh.opened[i] = file
			return uint64(i), nil
		}
	}
	fh.opened = append(fh.opened, file)

	return uint64(len(fh.opened) - 1), nil
}

func (fh *fileHandler) get(fhi uint64) (fs.File, error) {
	if int(fhi) >= len(fh.opened) {
		return nil, ErrBadHolderIndex
	}
	h := fh.opened[int(fhi)]
	if h == nil {
		return nil, ErrHolderEmpty
	}

	return h, nil
}

func (fh *fileHandler) Remove(fhi uint64) error {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	if fhi == fhNone {
		return nil
	}

	if int(fhi) >= len(fh.opened) {
		return ErrBadHolderIndex
	}
	f := fh.opened[int(fhi)]
	if f == nil {
		return ErrHolderEmpty
	}

	if err := f.Close(); err != nil {
		return err
	}

	fh.opened[int(fhi)] = nil

	return nil
}

func (fh *fileHandler) lookupFile(path string) (fs.File, error) {
	file, err := fh.fs.Open(path)
	if err != nil {
		return nil, err
	}

	if file != nil {
		return file, nil
	}

	return nil, os.ErrNotExist
}
