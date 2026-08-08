package torrent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"

	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/Apollogeddon/distribyted/internal/torrent/loader"
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

func (tw TorrentWrapper) SetPriority(index int, prio torrent.PiecePriority) {
	tw.Torrent.Piece(index).SetPriority(prio)
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
	responsiveReads         bool

	ctx    context.Context
	cancel context.CancelFunc
	loadWg sync.WaitGroup

	lastHealth map[string]healthState
	timings    *Timings
}

type healthState struct {
	peers    int
	seeders  int
	progress string
}

func NewService(loaders []loader.Loader, db loader.LoaderAdder, stats *Stats, c TorrentClient, addTimeout, readTimeout int, continueWhenAddTimeout, responsiveReads bool, tm *Timings) *Service {
	l := dlog.Logger("torrent-service")
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		log:                    l,
		s:                      stats,
		c:                      c,
		fss:                    make(map[string]fs.Filesystem),
		loaders:                loaders,
		db:                     db,
		addTimeout:             addTimeout,
		readTimeout:            readTimeout,
		continueWhenAddTimeout: continueWhenAddTimeout,
		responsiveReads:        responsiveReads,
		ctx:                    ctx,
		cancel:                 cancel,
		lastHealth:             make(map[string]healthState),
		timings:                tm,
	}

	go s.runHealthLogger()

	return s
}

func (s *Service) runHealthLogger() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.logSwarmHealth()
		}
	}
}

func (s *Service) logSwarmHealth() {
	routes := s.s.RoutesStats()
	if len(routes) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range routes {
		for _, t := range r.TorrentStats {
			completedPieces := 0
			for _, chunk := range t.PieceChunks {
				if chunk.Status == Complete {
					completedPieces += chunk.NumPieces
				}
			}

			progressVal := 0.0
			if t.TotalPieces > 0 {
				progressVal = float64(completedPieces) / float64(t.TotalPieces) * 100
			}
			progress := fmt.Sprintf("%.1f%%", progressVal)

			last, ok := s.lastHealth[t.Hash]
			if ok && last.progress == progress {
				continue
			}

			rate := 0.0
			if t.TimePassed > 0 {
				rate = float64(t.DownloadedBytes) / t.TimePassed
			}

			// Concise summary: [Route] Name: Peers (Seeders), DL Speed, Progress
			s.log.Info().
				Str(dlog.KeyRoute, r.Name).
				Str(dlog.KeyName, t.Name).
				Int("peers", t.Peers).
				Int("seeders", t.Seeders).
				Str("dl", fmt.Sprintf("%.2f MB/s", rate/1024/1024)).
				Str("progress", progress).
				Msg("swarm health summary")

			s.lastHealth[t.Hash] = healthState{
				peers:    t.Peers,
				seeders:  t.Seeders,
				progress: progress,
			}
		}
	}
}

func (s *Service) SetReadTimeout(t int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readTimeout = t
}

func (s *Service) SetAddTimeout(t int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addTimeout = t
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
	for n, o := range links {
		s.log.Debug().Str("old", o).Str("new", n).Msg("restoring link")
		// Don't call AddLink as it writes back to DB. Call onLinkAdded directly.
		if s.onLinkAdded != nil {
			s.onLinkAdded(o, n)
		}
	}

	// Return a snapshot, not the live map: every route key above was already
	// inserted synchronously (addRoute runs in load()'s loop, before any of
	// its magnets' background goroutines are spawned), but those goroutines
	// go on to call addTorrent -> addRoute again for the same routes well
	// after Load() returns. That's harmless by itself (existing keys, no
	// further writes), but the caller (main.go) ranges the returned map with
	// no access to s.mu at all, so any future change here that adds a
	// genuinely-async write path would turn "was always safe by
	// coincidence" into a real concurrent-map crash with no warning. A copy
	// costs nothing at startup and removes that foot-gun entirely.
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := make(map[string]fs.Filesystem, len(s.fss))
	for k, v := range s.fss {
		snapshot[k] = v
	}
	return snapshot, nil
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
			s.loadWg.Add(1)
			go func(r, m string) {
				defer s.loadWg.Done()
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
			s.loadWg.Add(1)
			go func(r, p string) {
				defer s.loadWg.Done()
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
	path = cleanRoute(path)

	if s.onLinkRemoved != nil {
		s.onLinkRemoved(path)
	}
	return s.db.RemoveLink(path)
}

// RenameLink persists a ContainerFs rename of a container-owned entry from
// oldpath to newpath. If oldpath was itself a persisted link, newpath's
// record must point to whatever oldpath's own source was — not oldpath
// itself, which stops existing the moment the rename completes and so
// can't be used to reconstruct the link on the next restart.
func (s *Service) RenameLink(oldpath, newpath string) error {
	oldpath = cleanRoute(oldpath)
	newpath = cleanRoute(newpath)

	links, err := s.db.ListLinks()
	if err != nil {
		return err
	}

	source, ok := links[strings.TrimPrefix(oldpath, "/")]
	if !ok {
		source = oldpath
	}

	if err := s.db.AddLink(source, newpath); err != nil {
		return err
	}
	return s.db.RemoveLink(oldpath)
}

func (s *Service) OnLinkAdded(f func(string, string)) {
	s.onLinkAdded = f
}

func (s *Service) OnLinkRemoved(f func(string)) {
	s.onLinkRemoved = f
}

func cleanRoute(r string) string {
	return path.Clean("/" + r)
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
	_, exists := s.fss[folder]
	var tfs *fs.TorrentFS
	var listeners []func(string, fs.Filesystem)
	if !exists {
		tfs = fs.NewTorrent(s.readTimeout, s.responsiveReads)
		tfs.OnFirstRead(s.timings.FirstRead)
		s.fss[folder] = tfs
		listeners = append(listeners, s.routeAddedListeners...)
	}
	s.mu.Unlock()

	// Listeners run with the lock released: in production one calls
	// ContainerFs.AddFS, crossing into a different mutex — holding s.mu
	// across that call risks lock-ordering deadlocks with any future path
	// that acquires ContainerFs's lock before calling back into Service.
	for _, f := range listeners {
		f(folder, tfs)
	}
}

func (s *Service) addTorrent(r string, t fs.Torrent) error {
	hash := t.InfoHash().String()
	s.timings.Added(hash, r, t.Name(), webseedCount(t), t)

	// only get info if name is not available
	if t.Info() == nil {
		s.log.Info().Str(dlog.KeyHash, hash).Msg("getting torrent info")
		select {
		case <-time.After(time.Duration(s.addTimeout) * time.Second):
			s.log.Warn().Str(dlog.KeyHash, hash).Msg("timeout getting torrent info")
			if !s.continueWhenAddTimeout {
				return errors.New("timeout getting torrent info")
			}
			s.log.Info().Str(dlog.KeyHash, hash).Msg("ignoring timeout error and continuing in background")
		case <-t.GotInfo():
			s.timings.GotInfo(hash)
			s.log.Info().Str(dlog.KeyHash, hash).Msg("obtained torrent info")
		case <-s.ctx.Done():
			return nil
		}

	} else {
		s.timings.GotInfo(hash)
	}

	// Add to stats
	s.s.Add(r, t)

	// Add to filesystems
	s.addRoute(r)
	folder := path.Join("/", r)
	s.mu.Lock()
	defer s.mu.Unlock()

	fsEntry, exists := s.fss[folder]
	if !exists {
		return fmt.Errorf("error adding torrent to filesystem: route %s not found in map", folder)
	}

	tfs, ok := fsEntry.(*fs.TorrentFS)
	if !ok {
		return fmt.Errorf("error adding torrent to filesystem: route %s has unexpected type %T", folder, fsEntry)
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

	s.mu.Lock()
	fsEntry, exists := s.fss[folder]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("error removing torrent from filesystem: route %s not found", folder)
	}

	tfs, ok := fsEntry.(*fs.TorrentFS)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("error removing torrent from filesystem: route %s has unexpected type %T", folder, fsEntry)
	}

	tfs.RemoveTorrent(h)
	delete(s.lastHealth, h)
	s.timings.Forget(h)
	listeners := append([]func(string){}, s.torrentRemovedListeners...)
	s.mu.Unlock()

	// Remove from client
	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return err
	}

	t, ok := s.c.Torrent(metainfo.NewHashFromHex(h))
	if ok {
		t.Drop()
	}

	// Listeners run with the lock released, same as addRoute's
	// routeAddedListeners: a listener (e.g. cascading to ContainerFs) may
	// re-enter this Service, which would deadlock if s.mu were still held.
	for _, f := range listeners {
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
	s.cancel()
	s.loadWg.Wait()
	s.c.Close()
	s.timings.Close()
}
