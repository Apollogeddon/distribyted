package fs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/rs/zerolog"

	"github.com/stretchr/testify/require"
)

const testMagnet = "magnet:?xt=urn:btih:a88fda5954e89178c372716a6a78b8180ed4dad3&dn=The+WIRED+CD+-+Rip.+Sample.+Mash.+Share&tr=udp%3A%2F%2Fexplodie.org%3A6969&tr=udp%3A%2F%2Ftracker.coppersurfer.tk%3A6969&tr=udp%3A%2F%2Ftracker.empire-js.us%3A1337&tr=udp%3A%2F%2Ftracker.leechers-paradise.org%3A6969&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337&tr=wss%3A%2F%2Ftracker.btorrent.xyz&tr=wss%3A%2F%2Ftracker.fastcast.nz&tr=wss%3A%2F%2Ftracker.openwebtorrent.com&ws=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2F&xs=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2Fwired-cd.torrent"

var Cli *torrent.Client

func TestMain(m *testing.M) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = os.TempDir()
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true

	// disable webseeds to avoid a panic when closing client on tests
	cfg.DisableWebseeds = true

	client, err := torrent.NewClient(cfg)
	if err != nil {
		panic(err)
	}

	Cli = client

	exitVal := m.Run()

	client.Close()

	os.Exit(exitVal)
}

func TestTorrentFilesystem(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a real magnet in short mode")
	}
	require := require.New(t)

	to, err := Cli.AddMagnet(testMagnet)
	require.NoError(err)

	<-to.GotInfo()

	tfs := NewTorrent(600)
	tfs.AddTorrent(TorrentWrapper{to})

	files, err := tfs.ReadDir("/")
	require.NoError(err)
	require.Len(files, 1)
	require.Contains(files, "The WIRED CD - Rip. Sample. Mash. Share")

	files, err = tfs.ReadDir("/The WIRED CD - Rip. Sample. Mash. Share")
	require.NoError(err)
	require.Len(files, 18)

	f, err := tfs.Open("/The WIRED CD - Rip. Sample. Mash. Share/not_existing_file.txt")
	require.Equal(os.ErrNotExist, err)
	require.Nil(f)

	f, err = tfs.Open("/The WIRED CD - Rip. Sample. Mash. Share/01 - Beastie Boys - Now Get Busy.mp3")
	require.NoError(err)
	require.NotNil(f)
	require.Equal(f.Size(), int64(1964275))
	require.False(f.IsDir())

	b := make([]byte, 10)

	n, err := f.Read(b)
	require.NoError(err)
	require.Equal(10, n)
	require.Equal([]byte{0x49, 0x44, 0x33, 0x3, 0x0, 0x0, 0x0, 0x0, 0x1f, 0x76}, b)

	n, err = f.ReadAt(b, 10)
	require.NoError(err)
	require.Equal(10, n)

	n, err = f.ReadAt(b, 10000)
	require.NoError(err)
	require.Equal(10, n)

	// Test Torrent extra methods
	err = tfs.Mkdir("/newdir")
	require.NoError(err)

	err = tfs.Link("/The WIRED CD - Rip. Sample. Mash. Share/01 - Beastie Boys - Now Get Busy.mp3", "/linked.mp3")
	require.NoError(err)

	err = tfs.Rename("/linked.mp3", "/renamed.mp3")
	require.NoError(err)

	err = tfs.Rmdir("/newdir")
	require.NoError(err)

	// Error cases
	require.Error(tfs.Link("/notexists", "/target"))
	require.Error(tfs.Rename("/notexists", "/target"))
	require.Error(tfs.Rmdir("/notexists"))

	err = tfs.Rmdir("/renamed.mp3")
	require.Error(err)

	tfs.RemoveTorrent(to.InfoHash().HexString())
	files, err = tfs.ReadDir("/")
	require.NoError(err)
	require.Len(files, 0)

	require.NoError(f.Close())
}

func TestReadAtTorrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a real magnet in short mode")
	}
	require := require.New(t)

	to, err := Cli.AddMagnet(testMagnet)
	require.NoError(err)

	<-to.GotInfo()
	torrFile := to.Files()[0]

	tf := torrentFile{
		readerFunc: torrFile.NewReader,
		len:        torrFile.Length(),
		timeout:    500,
	}
	h := tf.NewHandle()
	defer func() { _ = h.Close() }()

	toRead := make([]byte, 5)
	n, err := h.ReadAt(toRead, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x0, 0x0, 0x1f, 0x76, 0x54}, toRead)

	n, err = h.ReadAt(toRead, 0)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x49, 0x44, 0x33, 0x3, 0x0}, toRead)
}

func TestReadAtWrapper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that fetches a real magnet in short mode")
	}
	t.Parallel()

	require := require.New(t)

	to, err := Cli.AddMagnet(testMagnet)
	require.NoError(err)

	<-to.GotInfo()
	torrFile := to.Files()[0]

	r := newReadAtWrapper(torrFile.NewReader(), torrFile, 10, zerolog.Nop())
	defer func() { _ = r.Close() }()

	toRead := make([]byte, 5)
	n, err := r.ReadAt(toRead, 6)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x0, 0x0, 0x1f, 0x76, 0x54}, toRead)

	n, err = r.ReadAt(toRead, 0)
	require.NoError(err)
	require.Equal(5, n)
	require.Equal([]byte{0x49, 0x44, 0x33, 0x3, 0x0}, toRead)

	_ = r.Close()
	n, err = r.ReadAt(toRead, 0)
	require.Equal(0, n)
	require.Equal(io.EOF, err)
}

// TestTorrentFS_ConcurrentAddRemove reproduces removing a torrent while its
// metadata is still arriving: RemoveTorrent used to range fs.s.files without
// storage's own lock while the GotInfo goroutine wrote it concurrently
// (fatal error: concurrent map iteration and map write). It also guards
// against the file resurrecting if metadata arrives after the removal.
func TestTorrentFS_ConcurrentAddRemove(t *testing.T) {
	for i := 0; i < 20; i++ {
		dir := t.TempDir()
		filePath := filepath.Join(dir, fmt.Sprintf("concurrent-%d.txt", i))
		content := []byte(fmt.Sprintf("concurrent add/remove test data %d", i))
		require.NoError(t, os.WriteFile(filePath, content, 0644))

		info := metainfo.Info{PieceLength: 256 * 1024}
		require.NoError(t, info.BuildFromFilePath(filePath))

		infoBytes, err := bencode.Marshal(info)
		require.NoError(t, err)
		ih := infohash.HashBytes(infoBytes)

		to, _ := Cli.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: ih})

		tfs := NewTorrent(5)
		tfs.AddTorrent(TorrentWrapper{to})

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			_ = to.SetInfoBytes(infoBytes)
		}()

		go func() {
			defer wg.Done()
			<-start
			tfs.RemoveTorrent(ih.HexString())
		}()

		close(start)
		wg.Wait()

		require.Eventually(t, func() bool {
			files, err := tfs.ReadDir("/")
			return err == nil && len(files) == 0
		}, 2*time.Second, 10*time.Millisecond, "file should not be present regardless of race outcome")

		to.Drop()
	}
}

func TestReadAtLeast(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// test short buffer error
	n, err := readAtLeast(nil, 1, make([]byte, 1), 2, zerolog.Nop())
	require.Equal(0, n)
	require.ErrorIs(err, io.ErrShortBuffer)
}

// fakeContextReader is a minimal missinggo.ReadContexter for exercising
// readAtLeast's timer handling without a real torrent.
type fakeContextReader struct {
	delay time.Duration
	data  []byte
}

func (f *fakeContextReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	select {
	case <-time.After(f.delay):
		return copy(p, f.data), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// TestReadAtLeast_PooledTimerNotStolen guards against a previously-fixed bug
// in readAtLeast (internal/fs/torrent.go): timerPool.Put(timer) used to run
// before the watchdog goroutine was guaranteed to have exited, so a
// concurrent caller could Get() and Reset() the same *time.Timer while the
// previous call's watchdog was still selecting on it, silently starving that
// read's timeout and hanging it indefinitely. This was the suspected root
// cause of the 10s->90s integration-test timeout bump in commit a10f063 (see
// BACKLOG.md, Medium: "Pooled timer released before its watchdog stops").
// withReadTimeout now waits on a watchdogDone channel before returning the
// timer to the pool, closing the race.
func TestReadAtLeast_PooledTimerNotStolen(t *testing.T) {
	l := zerolog.Nop()
	buf := make([]byte, 4)

	// Churn the timer pool: many short-timeout reads that return immediately,
	// each pooling a timer whose watchdog goroutine may still be alive.
	fast := &fakeContextReader{delay: 0, data: []byte("fast")}
	for i := 0; i < 50; i++ {
		_, _ = readAtLeast(fast, 1, buf, 4, l)
	}

	// A read that legitimately needs longer than the churn's 1s timeout
	// window to arrive, but well within its own 30s timeout.
	slow := &fakeContextReader{delay: 2 * time.Second, data: []byte("data")}
	done := make(chan struct{})
	var n int
	var err error
	go func() {
		n, err = readAtLeast(slow, 30, buf, 4, l)
		close(done)
	}()

	select {
	case <-done:
		require.NoError(t, err)
		require.Equal(t, 4, n)
	case <-time.After(10 * time.Second):
		t.Fatal("readAtLeast hung, likely because a stolen pooled timer starved its watchdog")
	}
}
