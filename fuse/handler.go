package fuse

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/Apollogeddon/distribyted/fs"
	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/rs/zerolog/log"

	dlog "github.com/Apollogeddon/distribyted/log"
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
		if err := os.MkdirAll(folder, 0744); err != nil && !os.IsExist(err) {
			return err
		}
	}

	if s.host == nil {
		s.host = fuse.NewFileSystemHost(NewFS(cfs))
	}

	// TODO improve error handling here
	go func() {
		var config []string

		if s.fuseAllowOther {
			config = append(config, "-o", "allow_other")
		}

		// Enable kernel-level caching for attributes and entries to improve performance
		config = append(config, "-o", "attr_timeout=60")
		config = append(config, "-o", "entry_timeout=60")

		ok := s.host.Mount(s.path, config)
		if !ok {
			log.Error().Str(dlog.KeyPath, s.path).Msg("error trying to mount filesystem")
		}
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
		//TODO try to force unmount if possible
		log.Error().Str(dlog.KeyPath, s.path).Msg("unmount failed")
	}
}
