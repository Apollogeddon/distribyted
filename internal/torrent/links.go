package torrent

import (
	"context"
	"errors"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

// A link record maps the path a link is at to the path it shows. A folder or an empty file
// made through the mount has nothing to show, so its record holds one of these instead.
const (
	// DirRecord marks a folder. Before FileRecord, files were recorded this way too, so an
	// old record of a file comes back as a folder.
	DirRecord = "/"
	// FileRecord marks an empty file.
	FileRecord = "file:"
)

// IsLinkSource reports whether a link record's value is a path the link shows, rather than
// a mark for a folder or file made through the mount.
func IsLinkSource(v string) bool {
	return v != DirRecord && strings.HasPrefix(v, "/")
}

// maxLinkDepth bounds following a link to the link it was made from, so a cycle among
// records can't loop forever.
const maxLinkDepth = 16

// AddLink records the link at newpath to oldpath. When oldpath is itself a link, or inside
// one, the record holds what that link shows: oldpath may be deleted later, and the link
// has to be made again from its record after a restart.
func (s *Service) AddLink(oldpath, newpath string) error {
	oldpath = cleanRoute(oldpath)
	newpath = cleanRoute(newpath)

	if s.onLinkAdded != nil {
		s.onLinkAdded(oldpath, newpath)
	}
	source, err := s.resolveSource(oldpath)
	if err != nil {
		return err
	}
	return s.putLink(source, newpath)
}

// AddEntry records a folder (dir) or an empty file made at p through the mount.
func (s *Service) AddEntry(p string, dir bool) error {
	v := FileRecord
	if dir {
		v = DirRecord
	}
	return s.putLink(v, cleanRoute(p))
}

// RenameLink persists a ContainerFs rename of a container-owned entry from
// oldpath to newpath. If oldpath was itself a persisted link, newpath's
// record must point to whatever oldpath's own source was — not oldpath
// itself, which stops existing the moment the rename completes and so
// can't be used to reconstruct the link on the next restart.
func (s *Service) RenameLink(oldpath, newpath string) error {
	oldpath = cleanRoute(oldpath)
	newpath = cleanRoute(newpath)

	v, ok, err := s.db.GetLink(oldpath)
	if err != nil {
		return err
	}
	source := v // a folder or file made here stays one
	if !ok || IsLinkSource(v) {
		if source, err = s.resolveSource(oldpath); err != nil {
			return err
		}
	}

	if err := s.putLink(source, newpath); err != nil {
		return err
	}
	return s.db.RemoveLink(oldpath)
}

// putLink writes a link record, unless it already holds v: restoring links after a start
// records each again, and a write per link would sync the database once for every one.
func (s *Service) putLink(v, newpath string) error {
	if cur, ok, err := s.db.GetLink(newpath); err == nil && ok && cur == v {
		return nil
	}
	return s.db.AddLink(v, newpath)
}

// resolveSource follows p back through the links it's at or inside to the path they show.
func (s *Service) resolveSource(p string) (string, error) {
	for range maxLinkDepth {
		next, ok, err := s.sourceOf(p)
		if err != nil || !ok {
			return p, err
		}
		p = next
	}
	return p, nil
}

// sourceOf finds the link record nearest p, at p or one of its folders, and returns the
// path p shows through it. A folder made through the mount isn't a link, so a path inside
// one shows nothing else.
func (s *Service) sourceOf(p string) (string, bool, error) {
	for q := p; q != "/" && q != "."; q = path.Dir(q) {
		v, ok, err := s.db.GetLink(q)
		if err != nil {
			return "", false, err
		}
		if !ok {
			continue
		}
		if !IsLinkSource(v) {
			return "", false, nil
		}
		return path.Join(v, strings.TrimPrefix(p, q)), true, nil
	}
	return "", false, nil
}

// PersistLinks keeps what's made in cfs, its links and the folders and files made through
// the mount, in s's database, so they can be made again after a restart.
func PersistLinks(s *Service, cfs *fs.ContainerFs) {
	l := dlog.Logger("links")
	cfs.OnLinkAdded(func(oldpath, newpath string) {
		// a link restored after a start was recorded before; only a new one is prepared
		_, restored, _ := s.db.GetLink(cleanRoute(newpath))
		if err := s.AddLink(oldpath, newpath); err != nil {
			l.Warn().Err(err).Str("old", oldpath).Str("new", newpath).Msg("problem saving link to database")
		}
		if !restored {
			// off the import's link call, which waits on it, as the client's lock may be busy
			go prepareLinked(cfs, newpath, l)
		}
	})
	cfs.OnEntryMade(func(p string, dir bool) {
		if err := s.AddEntry(p, dir); err != nil {
			l.Warn().Err(err).Str(dlog.KeyPath, p).Msg("problem saving folder or file to database")
		}
	})
	cfs.OnLinkRemoved(func(p string) {
		if err := s.RemoveLink(p); err != nil {
			l.Warn().Err(err).Str(dlog.KeyPath, p).Msg("problem removing link from database")
		}
	})
	cfs.OnLinkRenamed(func(oldpath, newpath string) {
		if err := s.RenameLink(oldpath, newpath); err != nil {
			l.Warn().Err(err).Str("old", oldpath).Str("new", newpath).Msg("problem persisting renamed link to database")
		}
	})
}

// prepareLinked downloads the start and end of a file just linked into a library, as an *arr
// app's import does, so the first play starts at once: a player reads both before it shows a
// frame. A folder, or a file with no torrent, has nothing to prepare.
func prepareLinked(cfs *fs.ContainerFs, p string, l zerolog.Logger) {
	f, err := cfs.Open(p)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if pf, ok := f.(fs.Preparer); ok && pf.Prepare(fs.PrepareStart) {
		l.Debug().Str(dlog.KeyPath, p).Msg("preparing the linked file's start and end")
	}
}

const (
	// a link waiting for its torrent is tried again this often at first, then less often,
	// up to restoreRetryMax, besides each time a torrent is added
	restoreRetryMin = 2 * time.Second
	restoreRetryMax = time.Minute
	// torrents load one after another at a start; one retry covers several
	restoreDebounce = 500 * time.Millisecond
)

// pendingLink is a link waiting for the file it shows to appear.
type pendingLink struct {
	recorded string // the record's value, to notice it was deleted or changed meanwhile
	from     string // the path to link from, with older chained records followed
}

// LinkRestorer makes again, after a start, what PersistLinks recorded. Folders and files
// come back at once. A link needs the torrent it shows to have loaded, so it's tried again
// each time a torrent is added and every so often, until it's made, its record is deleted,
// or something else is put at its path.
type LinkRestorer struct {
	s     *Service
	cfs   *fs.ContainerFs
	log   zerolog.Logger
	grace time.Duration
	start time.Time
	nudge chan struct{}

	mu      sync.Mutex
	pending map[string]pendingLink
}

// NewLinkRestorer returns a restorer that reports ready (Ready) once every link is back,
// or once grace has passed, whichever is first.
func NewLinkRestorer(s *Service, cfs *fs.ContainerFs, grace time.Duration) *LinkRestorer {
	return &LinkRestorer{
		s:       s,
		cfs:     cfs,
		log:     dlog.Logger("links"),
		grace:   grace,
		start:   time.Now(),
		nudge:   make(chan struct{}, 1),
		pending: make(map[string]pendingLink),
	}
}

// Start makes the recorded folders and files, and the links whose files are already there.
// The rest wait for Run.
func (r *LinkRestorer) Start() error {
	r.start = time.Now()
	records, err := r.s.ListLinks()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	slices.Sort(keys) // a folder before what's in it

	var dirs, files int
	for _, k := range keys {
		v, p := records[k], cleanRoute(k)
		switch {
		case v == FileRecord:
			if err := r.cfs.Create(p); err != nil && !errors.Is(err, os.ErrExist) {
				r.log.Warn().Err(err).Str(dlog.KeyPath, p).Msg("can't make the file again")
				continue
			}
			files++
		case !IsLinkSource(v):
			if err := r.cfs.Mkdir(p); err != nil && !errors.Is(err, os.ErrExist) {
				r.log.Warn().Err(err).Str(dlog.KeyPath, p).Msg("can't make the folder again")
				continue
			}
			dirs++
		default:
			from, err := r.s.resolveSource(v)
			if err != nil {
				return err
			}
			r.pending[p] = pendingLink{recorded: v, from: from}
		}
	}
	r.log.Info().Int("folders", dirs).Int("files", files).Int("links", len(r.pending)).Msg("restoring what was made in the mount")

	r.s.OnTorrentAdded(func(string) { r.Nudge() })
	r.retry()
	return nil
}

// Nudge asks Run to try the waiting links again now.
func (r *LinkRestorer) Nudge() {
	select {
	case r.nudge <- struct{}{}:
	default:
	}
}

// Pending is how many links are still waiting.
func (r *LinkRestorer) Pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// Ready reports whether every link is back, or grace has passed since Start.
func (r *LinkRestorer) Ready() bool {
	return r.Pending() == 0 || time.Since(r.start) >= r.grace
}

// Run tries the waiting links again until none is left or ctx is done.
func (r *LinkRestorer) Run(ctx context.Context) {
	delay := restoreRetryMin
	warned := false
	for r.Pending() > 0 {
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-r.nudge:
			t.Stop()
			// a start adds torrents in a burst; let it settle before going through the list
			select {
			case <-ctx.Done():
				return
			case <-time.After(restoreDebounce):
			}
		case <-t.C:
			delay = min(delay*2, restoreRetryMax)
		}
		r.retry()

		if n := r.Pending(); !warned && n > 0 && time.Since(r.start) >= r.grace {
			warned = true
			r.log.Warn().Int("links", n).Strs("waiting", r.sample(5)).Msg("links still waiting for their torrents; they'll appear when the torrents load")
		}
	}
	if warned {
		r.log.Info().Msg("every link is restored")
	}
}

// retry tries each waiting link once, a folder before what's in it.
func (r *LinkRestorer) retry() {
	r.mu.Lock()
	paths := make([]string, 0, len(r.pending))
	for p := range r.pending {
		paths = append(paths, p)
	}
	snapshot := make(map[string]pendingLink, len(r.pending))
	for p, l := range r.pending {
		snapshot[p] = l
	}
	r.mu.Unlock()
	slices.Sort(paths)

	waiting := make(map[string]bool, len(paths))
	for _, p := range paths {
		// a link inside a linked archive waits for the archive, or its folder would be
		// made as a plain one and take the archive's place
		if parentWaiting(p, waiting) {
			waiting[p] = true
			continue
		}
		l := snapshot[p]
		if v, ok, err := r.s.db.GetLink(p); err != nil {
			waiting[p] = true
			continue
		} else if !ok || v != l.recorded {
			r.done(p) // deleted or replaced while it waited
			continue
		}

		err := r.cfs.Link(l.from, p)
		switch {
		case err == nil:
			r.done(p)
		case errors.Is(err, os.ErrExist):
			r.log.Warn().Str("old", l.from).Str("new", p).Msg("can't restore the link: something else is at its path")
			r.done(p)
		default:
			waiting[p] = true
		}
	}
}

func parentWaiting(p string, waiting map[string]bool) bool {
	for q := path.Dir(p); q != "/" && q != "."; q = path.Dir(q) {
		if waiting[q] {
			return true
		}
	}
	return false
}

func (r *LinkRestorer) done(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, p)
}

// sample is up to n of the waiting links' paths, for the log.
func (r *LinkRestorer) sample(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, n)
	for p := range r.pending {
		out = append(out, p)
	}
	slices.Sort(out)
	return out[:min(n, len(out))]
}
