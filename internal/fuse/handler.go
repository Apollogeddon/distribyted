package fuse

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/rs/zerolog/log"
	"github.com/winfsp/cgofuse/fuse"

	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

type FileSystemHost interface {
	Mount(path string, args []string) bool
	Unmount() bool
}

// ErrUnmounted is returned by Mount after Unmount: shutdown has begun.
var ErrUnmounted = errors.New("fuse: handler already unmounted")

type Handler struct {
	fuseAllowOther bool
	path           string
	newHost        func(fuse.FileSystemInterface) FileSystemHost

	// mu guards host and unmounted: Mount runs in its own goroutine and Unmount in the
	// signal handler's
	mu        sync.Mutex
	host      FileSystemHost
	unmounted bool
	mounted   atomic.Bool
}

func NewHandler(fuseAllowOther bool, path string) *Handler {
	return &Handler{
		fuseAllowOther: fuseAllowOther,
		path:           path,
		newHost: func(fs fuse.FileSystemInterface) FileSystemHost {
			return fuse.NewFileSystemHost(fs)
		},
	}
}

func (s *Handler) Mount(cfs *fs.ContainerFs) error {
	folder := s.path
	// On windows, the folder must don't exist
	if runtime.GOOS == "windows" {
		folder = filepath.Dir(s.path)
	}

	if filepath.VolumeName(folder) == "" {
		if err := os.MkdirAll(folder, 0o750); err != nil && !os.IsExist(err) {
			return err
		}
	}

	s.mu.Lock()
	if s.unmounted {
		s.mu.Unlock()
		return ErrUnmounted
	}
	if s.host == nil {
		s.host = s.newHost(&mountTracker{FileSystemInterface: NewFS(cfs), mounted: &s.mounted})
	}
	host := s.host
	s.mu.Unlock()

	// Mount blocks until the filesystem is unmounted, so it runs on its own and a failure can
	// only be logged: cgofuse reports none until then, and gives no reason.
	go func() {
		var config []string

		if s.fuseAllowOther {
			config = append(config, "-o", "allow_other")
		}

		// Enable kernel-level caching for attributes and entries to improve performance
		config = append(config, "-o", "attr_timeout=60")
		config = append(config, "-o", "entry_timeout=60")

		ok := host.Mount(s.path, config)
		s.mounted.Store(false)
		if !ok {
			log.Error().Str(dlog.KeyPath, s.path).Msg("error trying to mount filesystem")
		}
		log.Info().Str(dlog.KeyPath, s.path).Msg("FUSE mount goroutine exited")
	}()

	log.Info().Str(dlog.KeyPath, s.path).Msg("starting FUSE mount")

	return nil
}

// Mounted reports whether the filesystem is mounted: the kernel has initialised it and it hasn't
// been unmounted since.
func (s *Handler) Mounted() bool {
	return s.mounted.Load()
}

// mountTracker records when cgofuse initialises and destroys the filesystem, which it does once
// the mount is up and when it comes down.
type mountTracker struct {
	fuse.FileSystemInterface
	mounted *atomic.Bool
}

func (m *mountTracker) Init() {
	m.FileSystemInterface.Init()
	m.mounted.Store(true)
	log.Info().Msg("FUSE filesystem mounted")
}

func (m *mountTracker) Destroy() {
	m.mounted.Store(false)
	m.FileSystemInterface.Destroy()
}

// Unmount unmounts the filesystem, and stops a Mount that hasn't started yet from mounting it.
func (s *Handler) Unmount() {
	s.mu.Lock()
	s.unmounted = true
	host := s.host
	s.mu.Unlock()
	if host == nil {
		return
	}

	ok := host.Unmount()
	if !ok {
		// on Linux, the next start lazily unmounts what was left behind (forceUnmount)
		log.Error().Str(dlog.KeyPath, s.path).Msg("unmount failed")
	}
}
