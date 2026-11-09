package testenv

import (
	"net"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

// BenchmarkProbeReadahead compares probe_readahead off and on, on the cable profile, for a
// 64 MiB file of 4 MiB pieces (a film's piece length, scaled down):
//
//   - probe: what ffprobe or a library scan does, the header, the end, and four seeks through
//     the middle, 64 KiB each; reported as MiB downloaded per open (lower is better)
//   - first-read: open to first byte, as OpenToFirstRead_Throttled (must not get slower)
//   - sequential: the whole file read through, as SequentialThroughput (must not get slower)
func BenchmarkProbeReadahead(b *testing.B) {
	const (
		latency  = 20 * time.Millisecond
		bps      = 12 << 20
		pieceLen = 4 << 20
		size     = 64 << 20
	)
	tracker := NewTracker()
	require.NoError(b, tracker.Start())
	defer tracker.Stop()
	seeder, err := NewSeeder()
	require.NoError(b, err)
	defer seeder.Stop()
	magnet, err := seeder.AddFileWithPieceLength("probe.mkv", randomContent(41, size), tracker.AnnounceURL(), pieceLen)
	require.NoError(b, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())
	host, port := splitHostPort(b, seeder.PeerAddr())

	for _, mode := range []struct {
		name string
		on   bool
	}{{"off", false}, {"on", true}} {
		b.Run(mode.name, func(b *testing.B) {
			dfs.SetProbeReadahead(mode.on)
			defer dfs.SetProbeReadahead(false)

			b.Run("probe", func(b *testing.B) {
				var downloaded int64
				runProbeBench(b, magnet, host, port, latency, bps, func(b *testing.B, f dfs.File) {
					buf := make([]byte, 64<<10)
					for _, off := range []int64{0, size - int64(len(buf)), size / 5, size * 2 / 5, size * 3 / 5, size * 4 / 5} {
						_, err := f.ReadAt(buf, off)
						require.NoError(b, err)
					}
				}, func(t *torrent.Torrent) {
					// what was asked for ahead keeps arriving for a moment after the close
					time.Sleep(500 * time.Millisecond)
					s := t.Stats()
					downloaded += s.BytesReadData.Int64()
				})
				b.ReportMetric(float64(downloaded)/float64(b.N)/(1<<20), "MiB-downloaded/op")
			})

			b.Run("first-read", func(b *testing.B) {
				runProbeBench(b, magnet, host, port, latency, bps, func(b *testing.B, f dfs.File) {
					_, err := f.ReadAt(make([]byte, 64<<10), 0)
					require.NoError(b, err)
				}, nil)
			})

			b.Run("sequential", func(b *testing.B) {
				b.SetBytes(size)
				runProbeBench(b, magnet, host, port, latency, bps, func(b *testing.B, f dfs.File) {
					buf := make([]byte, 128<<10)
					var read int
					for read < size {
						n, err := f.Read(buf)
						read += n
						if err != nil {
							break
						}
					}
					require.Equal(b, size, read)
				}, nil)
			})
		})
	}
}

// runProbeBench times use on a fresh app's open of the magnet's file, from adding it; after,
// if not nil, sees the app's torrent once the file is closed, untimed.
func runProbeBench(b *testing.B, magnet metainfo.Magnet, host string, port uint16, latency time.Duration, bps int,
	use func(*testing.B, dfs.File), after func(*torrent.Torrent),
) {
	b.Helper()
	b.StopTimer()
	for range b.N {
		app, err := NewTestAppNoDefaultDialer()
		require.NoError(b, err)
		app.Client.AddDialer(ThrottledDialer{Latency: latency, BytesPerSecond: bps})
		t, _ := app.Client.AddMagnet(magnet.String())
		path := "/bench/" + magnet.DisplayName

		b.StartTimer()
		t.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(port)}}})
		require.NoError(b, app.Service.AddMagnet("bench", magnet.String()))
		var f dfs.File
		require.Eventually(b, func() bool {
			f, err = app.FS.Open(path)
			return err == nil
		}, 30*time.Second, 5*time.Millisecond, "file did not become readable")
		use(b, f)
		b.StopTimer()
		_ = f.Close()
		if after != nil {
			after(t)
		}
		app.Close()
	}
}
