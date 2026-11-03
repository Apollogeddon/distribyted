package torrent

import (
	"path"
	"slices"
	"strings"

	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

// When an episode is nearly over, the next one's first minutes and its end are downloaded,
// so it starts at once and plays while the rest arrives. The next episode is the next file of
// the same kind in the folder the playing one is linked into, as Sonarr names a season's
// episodes in order; failing that, the next in the torrent's own folder, as in a season pack.

// PrepareNextEpisodes has s get the next episode ready whenever one of cfs's torrent files is
// nearly read through.
func PrepareNextEpisodes(s *Service, cfs *fs.ContainerFs) {
	l := dlog.Logger("next-episode")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEpisode = func(route, p string) {
		src := path.Join("/", route, p)
		next, ok := nextEpisodeOf(s, cfs, src)
		if !ok {
			return
		}
		f, err := cfs.Open(next)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		if pf, ok := f.(fs.Preparer); ok && pf.Prepare(fs.PrepareLead) {
			l.Info().Str("playing", src).Str(dlog.KeyPath, next).Msg("getting the next episode ready")
		}
	}
}

func (s *Service) nearEnd(route, _, p string) {
	s.mu.Lock()
	next := s.nextEpisode
	s.mu.Unlock()
	if next != nil {
		next(route, p)
	}
}

// nextEpisodeOf finds the file to play after src, a torrent's file in cfs.
func nextEpisodeOf(s *Service, cfs *fs.ContainerFs, src string) (string, bool) {
	if links, err := s.db.ListLinks(); err == nil {
		var at []string
		for newpath, v := range links {
			if IsLinkSource(v) && cleanRoute(v) == src {
				at = append(at, cleanRoute(newpath))
			}
		}
		slices.Sort(at)
		for _, p := range at {
			if next, ok := nextInFolder(cfs, p); ok {
				return next, true
			}
		}
	}
	return nextInFolder(cfs, src)
}

// nextInFolder is the file after p in its folder, in name order, with the same extension.
func nextInFolder(cfs *fs.ContainerFs, p string) (string, bool) {
	dir, name := path.Dir(p), path.Base(p)
	children, err := cfs.ReadDir(dir)
	if err != nil {
		return "", false
	}
	ext := strings.ToLower(path.Ext(name))
	var names []string
	for n, f := range children {
		if !f.IsDir() && strings.ToLower(path.Ext(n)) == ext {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	i := slices.Index(names, name)
	if i < 0 || i+1 >= len(names) {
		return "", false
	}
	return path.Join(dir, names[i+1]), true
}
