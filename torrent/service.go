package torrent

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"

	"github.com/Apollogeddon/distribyted/fs"
	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/Apollogeddon/distribyted/torrent/loader"
)

type TorrentClient interface {
	AddTorrentFromFile(string) (fs.Torrent, error)
	AddMagnet(string) (fs.Torrent, error)
	Torrent(metainfo.Hash) (fs.Torrent, bool)
	Close()
}

type TorrentWrapper struct {
	*torrent.Torrent
}

func (tw TorrentWrapper) GotInfo() <-chan struct{} {
	return tw.Torrent.GotInfo()
}

func (tw TorrentWrapper) InfoHash() metainfo.Hash {
	return tw.Torrent.InfoHash()
}

type ClientWrapper struct {
	*torrent.Client
}

func (tcw ClientWrapper) AddTorrentFromFile(p string) (fs.Torrent, error) {
	t, err := tcw.Client.AddTorrentFromFile(p)
	if err != nil {
		return nil, err
	}
	return TorrentWrapper{t}, nil
}

func (tcw ClientWrapper) AddMagnet(m string) (fs.Torrent, error) {
	t, err := tcw.Client.AddMagnet(m)
	if err != nil {
		return nil, err
	}
	return TorrentWrapper{t}, nil
}

func (tcw ClientWrapper) Torrent(h metainfo.Hash) (fs.Torrent, bool) {
	t, ok := tcw.Client.Torrent(h)
	if !ok {
		return nil, false
	}
	return TorrentWrapper{t}, true
}

func (tcw ClientWrapper) Close() {
	tcw.Client.Close()
}

type Service struct {
	c TorrentClient

	s *Stats

	mu  sync.Mutex
	fss map[string]fs.Filesystem

	routeAddedListeners     []func(string, fs.Filesystem)
	torrentRemovedListeners []func(string)
	onLinkAdded             func(string, string)
	onLinkRemoved           func(string)

	loaders []loader.Loader
	db      loader.LoaderAdder

	log                     zerolog.Logger
	addTimeout, readTimeout int
	continueWhenAddTimeout  bool
}

func NewService(loaders []loader.Loader, db loader.LoaderAdder, stats *Stats, c TorrentClient, addTimeout, readTimeout int, continueWhenAddTimeout bool) *Service {
	l := dlog.Logger("torrent-service")
	return &Service{
		log:                    l,
		s:                      stats,
		c:                      c,
		fss:                    make(map[string]fs.Filesystem),
		loaders:                loaders,
		db:                     db,
		addTimeout:             addTimeout,
		readTimeout:            readTimeout,
		continueWhenAddTimeout: continueWhenAddTimeout,
	}
}

func (s *Service) SetReadTimeout(t int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readTimeout = t
}

func (s *Service) Load() (map[string]fs.Filesystem, error) {
	// Load from config
	s.log.Info().Msg("adding torrents from configuration")
	for _, loader := range s.loaders {
		if err := s.load(loader); err != nil {
			return nil, err
		}
	}

	// Load from DB
	s.log.Info().Msg("adding torrents from database")
	if err := s.load(s.db); err != nil {
		s.log.Error().Err(err).Msg("error loading from database")
		return nil, err
	}

	links, err := s.db.ListLinks()
	if err != nil {
		s.log.Error().Err(err).Msg("error listing links from database")
		return nil, err
	}
	s.log.Debug().Int("count", len(links)).Msg("found links in database")
	for o, n := range links {
		s.log.Debug().Str("old", o).Str("new", n).Msg("restoring link")
		// Don't call AddLink as it writes back to DB. Call onLinkAdded directly.
		if s.onLinkAdded != nil {
			s.onLinkAdded(o, n)
		}
	}

	return s.fss, nil
}

func (s *Service) load(l loader.Loader) error {
	list, err := l.ListMagnets()
	if err != nil {
		return err
	}
	s.log.Debug().Int("routes", len(list)).Msg("found magnets in loader")
	for r, ms := range list {
		s.log.Debug().Str("route", r).Int("magnets", len(ms)).Msg("loading magnets for route")
		s.addRoute(r)
		for _, m := range ms {
			// Run in background to avoid blocking Load()
			go func(r, m string) {
				if err := s.addMagnet(r, m); err != nil {
					s.log.Error().Err(err).Str("route", r).Msg("error loading magnet in background")
				}
			}(r, m)
		}
	}

	list, err = l.ListTorrentPaths()
	if err != nil {
		return err
	}
	for r, ms := range list {
		s.addRoute(r)
		for _, p := range ms {
			go func(r, p string) {
				if err := s.addTorrentPath(r, p); err != nil {
					s.log.Error().Err(err).Str("route", r).Msg("error loading torrent path in background")
				}
			}(r, p)
		}
	}

	return nil
}

func (s *Service) AddMagnet(r, m string) error {
	if err := s.addMagnet(r, m); err != nil {
		return err
	}

	// Add to db
	return s.db.AddMagnet(r, m)
}

func (s *Service) ListLinks() (map[string]string, error) {
	return s.db.ListLinks()
}

func (s *Service) AddLink(oldpath, newpath string) error {
	oldpath = cleanRoute(oldpath)
	newpath = cleanRoute(newpath)

	if s.onLinkAdded != nil {
		s.onLinkAdded(oldpath, newpath)
	}
	return s.db.AddLink(oldpath, newpath)
}

func (s *Service) RemoveLink(path string) error {
	if s.onLinkRemoved != nil {
		s.onLinkRemoved(path)
	}
	return s.db.RemoveLink(path)
}

func (s *Service) OnLinkAdded(f func(string, string)) {
	s.onLinkAdded = f
}

func (s *Service) OnLinkRemoved(f func(string)) {
	s.onLinkRemoved = f
}

func cleanRoute(r string) string {
	return "/" + strings.Trim(r, "/")
}

func (s *Service) addTorrentPath(r, p string) error {
	// Add to client
	t, err := s.c.AddTorrentFromFile(p)
	if err != nil {
		return err
	}

	return s.addTorrent(r, t)
}

func (s *Service) addMagnet(r, m string) error {
	// Add to client
	t, err := s.c.AddMagnet(m)
	if err != nil {
		return err
	}

	return s.addTorrent(r, t)

}

func (s *Service) OnRouteAdded(f func(string, fs.Filesystem)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routeAddedListeners = append(s.routeAddedListeners, f)
}

func (s *Service) OnTorrentRemoved(f func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.torrentRemovedListeners = append(s.torrentRemovedListeners, f)
}

func (s *Service) addRoute(r string) {
	s.s.AddRoute(r)

	// Add to filesystems
	folder := path.Join("/", r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fss[folder]; !ok {
		tfs := fs.NewTorrent(s.readTimeout)
		s.fss[folder] = tfs
		for _, f := range s.routeAddedListeners {
			f(folder, tfs)
		}
	}
}

func (s *Service) addTorrent(r string, t fs.Torrent) error {
	// only get info if name is not available
	if t.Info() == nil {
		s.log.Info().Str(dlog.KeyHash, t.InfoHash().String()).Msg("getting torrent info")
		select {
		case <-time.After(time.Duration(s.addTimeout) * time.Second):
			s.log.Warn().Str(dlog.KeyHash, t.InfoHash().String()).Msg("timeout getting torrent info")
			if !s.continueWhenAddTimeout {
				return errors.New("timeout getting torrent info")
			}
			s.log.Info().Str(dlog.KeyHash, t.InfoHash().String()).Msg("ignoring timeout error and continuing in background")
		case <-t.GotInfo():
			s.log.Info().Str(dlog.KeyHash, t.InfoHash().String()).Msg("obtained torrent info")
		}

	}

	// Add to stats
	s.s.Add(r, t)

	// Add to filesystems
	s.addRoute(r)
	folder := path.Join("/", r)
	s.mu.Lock()
	defer s.mu.Unlock()

	fs_entry, exists := s.fss[folder]
	if !exists {
		return fmt.Errorf("error adding torrent to filesystem: route %s not found in map", folder)
	}

	tfs, ok := fs_entry.(*fs.TorrentFS)
	if !ok {
		return fmt.Errorf("error adding torrent to filesystem: route %s has unexpected type %T", folder, fs_entry)
	}

	tfs.AddTorrent(t)

	name := "unknown"
	if t.Info() != nil {
		name = t.Info().Name
	}
	s.log.Info().Str(dlog.KeyName, name).Str(dlog.KeyRoute, r).Msg("torrent added")

	return nil
}

func (s *Service) RemoveFromHash(r, h string) error {
	s.log.Info().Str(dlog.KeyRoute, r).Str(dlog.KeyHash, h).Msg("removing torrent")

	// Remove from db
	deleted, err := s.db.RemoveFromHash(r, h)
	if err != nil {
		return err
	}

	if !deleted {
		return fmt.Errorf("element with hash %v on route %v cannot be removed", h, r)
	}

	// Remove from stats
	s.s.Del(r, h)

	// Remove from fs
	folder := path.Join("/", r)

	tfs, ok := s.fss[folder].(*fs.TorrentFS)
	if !ok {
		return errors.New("error removing torrent from filesystem")
	}

	tfs.RemoveTorrent(h)

	// Remove from client
	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return err
	}

	t, ok := s.c.Torrent(metainfo.NewHashFromHex(h))
	if ok {
		t.Drop()
	}

	for _, f := range s.torrentRemovedListeners {
		f(h)
	}

	return nil
}

func (s *Service) RemoveFromHashOnly(h string) error {
	r := s.s.GetRouteFromHash(h)
	if r == "" {
		return fmt.Errorf("torrent with hash %v not found", h)
	}

	return s.RemoveFromHash(r, h)
}

func (s *Service) AddTorrentFromFile(r, p string) error {
	return s.addTorrentPath(r, p)
}

func (s *Service) Torrent(h string) (fs.Torrent, bool) {
	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return nil, false
	}
	return s.c.Torrent(mh)
}

func (s *Service) Close() {
	s.c.Close()
}
