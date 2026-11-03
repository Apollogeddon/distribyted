package torrent

import (
	"archive/zip"
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/fs"
	"github.com/Apollogeddon/distribyted/internal/torrent/loader"
)

// linkEnv is a database and a route that outlive restarts: each boot builds the mount
// again from them, as a start of distribyted does.
type linkEnv struct {
	t     *testing.T
	db    *countingDB
	svc   *Service
	route *fs.Memory
	// mount, when set, is mounted at /torrents in place of route
	mount fs.Filesystem
}

// countingDB counts link record writes.
type countingDB struct {
	*loader.DB
	writes int
}

func (c *countingDB) AddLink(oldpath, newpath string) error {
	c.writes++
	return c.DB.AddLink(oldpath, newpath)
}

func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()
	db, err := loader.NewDB(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	cdb := &countingDB{DB: db}
	route := fs.NewMemory()
	require.NoError(t, route.Storage.Add(fs.NewMemoryFile([]byte("film")), "/Film.2024/film.mkv"))
	return &linkEnv{
		t:     t,
		db:    cdb,
		svc:   NewService(nil, cdb, nil, nil, 1, 1, true, false, nil),
		route: route,
	}
}

// boot builds the mount, records what's made in it, and restores what was recorded.
func (e *linkEnv) boot() (*fs.ContainerFs, *LinkRestorer) {
	e.t.Helper()
	var mount fs.Filesystem = e.route
	if e.mount != nil {
		mount = e.mount
	}
	cfs, err := fs.NewContainerFs(map[string]fs.Filesystem{"/torrents": mount})
	require.NoError(e.t, err)
	PersistLinks(e.svc, cfs)
	r := NewLinkRestorer(e.svc, cfs, time.Hour)
	require.NoError(e.t, r.Start())
	return cfs, r
}

func (e *linkEnv) record(newpath string) (string, bool) {
	e.t.Helper()
	v, ok, err := e.db.GetLink(newpath)
	require.NoError(e.t, err)
	return v, ok
}

func zipOf(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(name)
	require.NoError(t, err)
	_, err = f.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// A file made through the mount, as an app writing a .nfo or a .partial~ does, came back
// as a folder after a restart: folders and files were recorded alike.
func TestLinks_MadeFileAndFolderComeBackAsThemselves(t *testing.T) {
	e := newLinkEnv(t)
	cfs, _ := e.boot()
	require.NoError(t, cfs.Mkdir("/library/Film (2024)"))
	require.NoError(t, cfs.Create("/library/Film (2024)/film.nfo"))

	v, _ := e.record("/library/Film (2024)/film.nfo")
	require.Equal(t, FileRecord, v)
	v, _ = e.record("/library/Film (2024)")
	require.Equal(t, DirRecord, v)

	cfs, r := e.boot()
	require.Zero(t, r.Pending())
	f, err := cfs.Open("/library/Film (2024)/film.nfo")
	require.NoError(t, err)
	require.False(t, f.IsDir())
	f, err = cfs.Open("/library/Film (2024)")
	require.NoError(t, err)
	require.True(t, f.IsDir())
}

// Records from before FileRecord marked folders and files alike with "/"; they come back
// as folders, as they did.
func TestLinks_OldFolderRecordComesBackAsFolder(t *testing.T) {
	e := newLinkEnv(t)
	require.NoError(t, e.db.AddLink("/", "/library/old"))

	cfs, r := e.boot()
	require.Zero(t, r.Pending())
	f, err := cfs.Open("/library/old")
	require.NoError(t, err)
	require.True(t, f.IsDir())
}

// A link made from another link was recorded as showing that link, so once that link was
// deleted it couldn't be made again after a restart.
func TestLinks_LinkFromALinkSurvivesItsDeletionAndARestart(t *testing.T) {
	e := newLinkEnv(t)
	cfs, _ := e.boot()
	require.NoError(t, cfs.Link("/torrents/Film.2024/film.mkv", "/movies/A/a.mkv"))
	require.NoError(t, cfs.Link("/movies/A/a.mkv", "/movies/B/b.mkv"))

	v, _ := e.record("/movies/B/b.mkv")
	require.Equal(t, "/torrents/Film.2024/film.mkv", v)

	require.NoError(t, cfs.Remove("/movies/A/a.mkv"))
	cfs, r := e.boot()
	require.Zero(t, r.Pending())
	_, err := cfs.Open("/movies/B/b.mkv")
	require.NoError(t, err)
}

// A link to a file inside a linked archive is recorded as the file inside the archive it
// was linked from.
func TestLinks_LinkInsideALinkedArchiveRecordsTheTorrentPath(t *testing.T) {
	e := newLinkEnv(t)
	// a route opens the archives in its torrents as folders, as a torrent's route does; a
	// ContainerFs over the plain files does that here
	raw := fs.NewMemory()
	require.NoError(t, raw.Storage.Add(fs.NewMemoryFile(zipOf(t, "inside.txt", "hello")), "/a.zip"))
	route, err := fs.NewContainerFs(map[string]fs.Filesystem{"/raw": raw})
	require.NoError(t, err)
	require.NoError(t, route.Link("/raw/a.zip", "/Pack/a.zip"))
	e.mount = route
	cfs, _ := e.boot()
	require.NoError(t, cfs.Link("/torrents/Pack/a.zip", "/lib/a.zip"))
	require.NoError(t, cfs.Link("/lib/a.zip/inside.txt", "/lib/inside.txt"))

	v, _ := e.record("/lib/inside.txt")
	require.Equal(t, "/torrents/Pack/a.zip/inside.txt", v)

	cfs, r := e.boot()
	require.Zero(t, r.Pending())
	_, err = cfs.Open("/lib/inside.txt")
	require.NoError(t, err)
	entries, err := cfs.ReadDir("/lib/a.zip")
	require.NoError(t, err)
	require.Contains(t, entries, "inside.txt")
}

// A chained record written before AddLink followed links is followed when restored, and
// rewritten to the path it shows.
func TestLinks_OldChainedRecordIsRestoredAndRewritten(t *testing.T) {
	e := newLinkEnv(t)
	require.NoError(t, e.db.AddLink("/torrents/Film.2024/film.mkv", "/movies/A/a.mkv"))
	require.NoError(t, e.db.AddLink("/movies/A/a.mkv", "/movies/B/b.mkv"))

	cfs, r := e.boot()
	require.Zero(t, r.Pending())
	_, err := cfs.Open("/movies/B/b.mkv")
	require.NoError(t, err)
	v, _ := e.record("/movies/B/b.mkv")
	require.Equal(t, "/torrents/Film.2024/film.mkv", v)
}

// Renaming a file made through the mount keeps it a file.
func TestLinks_RenamedMadeFileStaysAFile(t *testing.T) {
	e := newLinkEnv(t)
	cfs, _ := e.boot()
	require.NoError(t, cfs.Create("/library/a.partial~"))
	require.NoError(t, cfs.Rename("/library/a.partial~", "/library/a.nfo"))

	v, _ := e.record("/library/a.nfo")
	require.Equal(t, FileRecord, v)
	_, ok := e.record("/library/a.partial~")
	require.False(t, ok)
}

// Restoring writes nothing when every record already holds what it would write: a write
// syncs the database, once per link.
func TestLinks_RestoreDoesNotRewriteUnchangedRecords(t *testing.T) {
	e := newLinkEnv(t)
	cfs, _ := e.boot()
	require.NoError(t, cfs.Mkdir("/library"))
	require.NoError(t, cfs.Link("/torrents/Film.2024/film.mkv", "/library/film.mkv"))

	e.db.writes = 0
	_, r := e.boot()
	require.Zero(t, r.Pending())
	require.Zero(t, e.db.writes)
}

// A link whose torrent hasn't loaded waits for it, keeps the restorer not ready, and comes
// back once the torrent is there, however long that takes.
func TestLinks_LinkWaitsForItsTorrent(t *testing.T) {
	e := newLinkEnv(t)
	require.NoError(t, e.db.AddLink("/torrents/Late.2024/late.mkv", "/movies/late.mkv"))

	cfs, r := e.boot()
	require.Equal(t, 1, r.Pending())
	require.Equal(t, []string{"/movies/late.mkv"}, r.Waiting())
	require.False(t, r.Ready())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()

	require.NoError(t, e.route.Storage.Add(fs.NewMemoryFile([]byte("late")), "/Late.2024/late.mkv"))
	r.Nudge()
	require.Eventually(t, func() bool { return r.Pending() == 0 }, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, r.Waiting())
	require.True(t, r.Ready())
	_, err := cfs.Open("/movies/late.mkv")
	require.NoError(t, err)
	<-done // Run returns with nothing left
}

// The restorer reports ready once its grace has passed, even with links still waiting, so
// one torrent with no peers can't hold everything up.
func TestLinks_ReadyAfterGrace(t *testing.T) {
	e := newLinkEnv(t)
	require.NoError(t, e.db.AddLink("/torrents/Gone/gone.mkv", "/movies/gone.mkv"))
	cfs, err := fs.NewContainerFs(map[string]fs.Filesystem{"/torrents": e.route})
	require.NoError(t, err)
	r := NewLinkRestorer(e.svc, cfs, 50*time.Millisecond)
	require.NoError(t, r.Start())
	require.False(t, r.Ready())
	require.Eventually(t, r.Ready, time.Second, 10*time.Millisecond)
	require.Equal(t, 1, r.Pending())
}

// A waiting link deleted from the Links page isn't brought back when its torrent loads.
func TestLinks_DeletedWaitingLinkIsNotRestored(t *testing.T) {
	e := newLinkEnv(t)
	require.NoError(t, e.db.AddLink("/torrents/Late.2024/late.mkv", "/movies/late.mkv"))
	cfs, r := e.boot()
	require.Equal(t, 1, r.Pending())

	require.NoError(t, e.svc.RemoveLink("/movies/late.mkv"))
	require.NoError(t, e.route.Storage.Add(fs.NewMemoryFile([]byte("late")), "/Late.2024/late.mkv"))
	r.retry()
	require.Zero(t, r.Pending())
	_, err := cfs.Open("/movies/late.mkv")
	require.Error(t, err)
	_, ok := e.record("/movies/late.mkv")
	require.False(t, ok)
}

// A link whose path is taken by something else is given up, not tried forever.
func TestLinks_LinkWhosePathIsTakenIsGivenUp(t *testing.T) {
	e := newLinkEnv(t)
	require.NoError(t, e.db.AddLink("/torrents/Late.2024/late.mkv", "/movies/late.mkv"))
	cfs, r := e.boot()
	require.Equal(t, 1, r.Pending())

	require.NoError(t, cfs.Mkdir("/movies/late.mkv"))
	require.NoError(t, e.route.Storage.Add(fs.NewMemoryFile([]byte("late")), "/Late.2024/late.mkv"))
	r.retry()
	require.Zero(t, r.Pending())
	f, err := cfs.Open("/movies/late.mkv")
	require.NoError(t, err)
	require.True(t, f.IsDir())
}

// preparingFile is a file that records what it was asked to prepare.
type preparingFile struct {
	*fs.MemoryFile
	mu    sync.Mutex
	asked []fs.Preparation
}

func (p *preparingFile) Prepare(a fs.Preparation) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, a)
	return true
}

func (p *preparingFile) prepared() []fs.Preparation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.asked)
}
func (p *preparingFile) Readiness() (fs.Readiness, bool) { return fs.Readiness{}, true }
func (p *preparingFile) StartReady() (bool, bool)        { return false, true }

// A file linked into a library has its start and end downloaded, so its first play starts at
// once; the same link restored after a start isn't prepared again.
func TestLinks_NewLinkIsPreparedRestoredIsNot(t *testing.T) {
	e := newLinkEnv(t)
	f := &preparingFile{MemoryFile: fs.NewMemoryFile([]byte("film"))}
	require.NoError(t, e.route.Storage.Add(f, "/Film.2025/film.mkv"))
	cfs, _ := e.boot()
	require.NoError(t, cfs.Link("/torrents/Film.2025/film.mkv", "/movies/film.mkv"))
	require.Eventually(t, func() bool { return len(f.prepared()) == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, []fs.Preparation{fs.PrepareStart}, f.prepared())

	_, r := e.boot()
	require.Zero(t, r.Pending())
	time.Sleep(50 * time.Millisecond) // a prepare would have run by now
	require.Len(t, f.prepared(), 1)
}

// A link made through the mount records when it was made; restored, it keeps that time;
// deleted, its time goes with it.
func TestLinks_AddedTimes(t *testing.T) {
	e := newLinkEnv(t)
	cfs, _ := e.boot()
	before := time.Now().Add(-time.Second)
	require.NoError(t, cfs.Link("/torrents/Film.2024/film.mkv", "/movies/film.mkv"))
	added, err := e.svc.LinksAddedSince(before)
	require.NoError(t, err)
	at, ok := added["/movies/film.mkv"]
	require.True(t, ok)

	cfs, _ = e.boot()
	added, err = e.svc.LinksAddedSince(before)
	require.NoError(t, err)
	require.Equal(t, at, added["/movies/film.mkv"], "a restore keeps the first time")

	require.NoError(t, cfs.Rename("/movies/film.mkv", "/movies/renamed.mkv"))
	added, _ = e.svc.LinksAddedSince(before)
	require.Equal(t, at, added["/movies/renamed.mkv"])
	require.NotContains(t, added, "/movies/film.mkv")

	require.NoError(t, cfs.Remove("/movies/renamed.mkv"))
	added, _ = e.svc.LinksAddedSince(before)
	require.Empty(t, added)

	require.NoError(t, cfs.Link("/torrents/Film.2024/film.mkv", "/movies/again.mkv"))
	e.svc.ForgetLinksAddedBefore(time.Now().Add(time.Hour))
	added, _ = e.svc.LinksAddedSince(before)
	require.Empty(t, added)
}
