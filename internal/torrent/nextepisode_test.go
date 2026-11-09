package torrent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// The episode after a nearly finished one is the next file of its kind in the folder it's
// linked into, and gets the lead it was given and its end ready; the last one has nothing after it.
func TestNextEpisode_FromTheLibraryFolder(t *testing.T) {
	e := newLinkEnv(t)
	eps := map[string]*preparingFile{}
	// separate torrents, one episode each, as single-episode grabs are
	for _, n := range []string{"E01", "E02", "E03"} {
		f := &preparingFile{MemoryFile: fs.NewMemoryFile([]byte(n))}
		eps[n] = f
		require.NoError(t, e.route.Storage.Add(f, "/Show."+n+"/show."+n+".mkv"))
	}
	cfs, _ := e.boot()
	PrepareNextEpisodes(e.svc, cfs)
	for _, n := range []string{"E01", "E02", "E03"} {
		require.NoError(t, cfs.Link("/torrents/Show."+n+"/show."+n+".mkv", "/series/Show/Season 1/Show - S01"+n+".mkv"))
	}
	require.NoError(t, cfs.Create("/series/Show/Season 1/Show - S01E01.srt"))
	// the links' own start-and-end preparing isn't what this checks
	time.Sleep(50 * time.Millisecond)
	for _, f := range eps {
		f.mu.Lock()
		f.asked = nil
		f.mu.Unlock()
	}

	e.svc.nearEnd("torrents", "", "Show.E01/show.E01.mkv", 100<<20)
	require.Equal(t, []fs.Preparation{fs.PrepareLead}, eps["E02"].prepared())
	require.Equal(t, fs.Request{What: fs.PrepareLead, Lead: 100 << 20, Why: fs.ReasonNextEpisode}, eps["E02"].requests()[0])
	require.Empty(t, eps["E03"].prepared())

	e.svc.nearEnd("torrents", "", "Show.E03/show.E03.mkv", 100<<20)
	require.Empty(t, eps["E01"].prepared(), "nothing comes after the last episode")
}

// An episode nothing links to finds the next one in its own torrent's folder, as in a season
// pack.
func TestNextEpisode_FromASeasonPack(t *testing.T) {
	e := newLinkEnv(t)
	e1 := &preparingFile{MemoryFile: fs.NewMemoryFile([]byte("1"))}
	e2 := &preparingFile{MemoryFile: fs.NewMemoryFile([]byte("2"))}
	require.NoError(t, e.route.Storage.Add(e1, "/Show.S01/Show.S01E01.mkv"))
	require.NoError(t, e.route.Storage.Add(e2, "/Show.S01/Show.S01E02.mkv"))
	require.NoError(t, e.route.Storage.Add(fs.NewMemoryFile([]byte("subs")), "/Show.S01/Show.S01E01.en.srt"))
	cfs, _ := e.boot()

	e.svc.nearEnd("torrents", "", "Show.S01/Show.S01E01.mkv", 100<<20)
	require.Empty(t, e2.prepared(), "nothing happens until PrepareNextEpisodes is set up")

	PrepareNextEpisodes(e.svc, cfs)
	e.svc.nearEnd("torrents", "", "Show.S01/Show.S01E01.mkv", 100<<20)
	require.Equal(t, []fs.Preparation{fs.PrepareLead}, e2.prepared())
}
