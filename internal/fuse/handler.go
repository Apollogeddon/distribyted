package fuse

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/rs/zerolog/log"
	"github.com/winfsp/cgofuse/fuse"

	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

type FileSystemHost interface {
	Mount(path string, args []string) bool
	Unmount() bool
}

type Handler struct {
	fuseAllowOther bool
	path           string

	host FileSystemHost
}

func NewHandler(fuseAllowOther bool, path string) *Handler {
	return &Handler{
		fuseAllowOther: fuseAllowOther,
		path:           path,
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

	if s.host == nil {
		s.host = fuse.NewFileSystemHost(NewFS(cfs))
	}

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

		if ok := s.host.Mount(s.path, config); !ok {
			log.Error().Str(dlog.KeyPath, s.path).Msg("error trying to mount filesystem")
		}
		log.Info().Str(dlog.KeyPath, s.path).Msg("FUSE mount goroutine exited")
	}()

	log.Info().Str(dlog.KeyPath, s.path).Msg("starting FUSE mount")

	return nil
}

func (s *Handler) Unmount() {
	if s.host == nil {
		return
	}

	ok := s.host.Unmount()
	if !ok {
		// on Linux, the next start lazily unmounts what was left behind (forceUnmount)
		log.Error().Str(dlog.KeyPath, s.path).Msg("unmount failed")
	}
}
