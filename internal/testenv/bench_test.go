package testenv

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

// splitHostPort parses a "host:port" address into anacrolix/torrent's
// PeerInfo shape, failing the benchmark on error instead of ignoring it.
func splitHostPort(b *testing.B, addr string) (string, uint16) {
	b.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(b, err)
	var port uint16
	_, err = fmt.Sscanf(portStr, "%d", &port)
	require.NoError(b, err)
	return host, port
}

// BenchmarkFileHandle_OpenToFirstRead measures the latency of opening a
// fresh read handle on an already-seeded torrent file and reading the
// first chunk — the exact path load() configures readahead on
// (internal/fs/torrent.go). The seeder here is on loopback, so this does
// NOT simulate real internet peer latency; treat absolute numbers as
// distribyted's own overhead (reader construction, locking, readahead
// setup), not real-world time-to-first-byte. Compare runs of this
// benchmark before/after a change with `benchstat` to catch regressions in
// that overhead specifically.
func BenchmarkFileHandle_OpenToFirstRead(b *testing.B) {
	tracker := NewTracker()
	require.NoError(b, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(b, err)
	defer seeder.Stop()

	// A few MB across several pieces, closer to real media than a
	// single-piece file, without making setup slow.
	content := make([]byte, 8*1024*1024)
	for i := range content {
		content[i] = byte(i)
	}
	magnet, err := seeder.AddFile("bench.bin", content, tracker.AnnounceURL())
	require.NoError(b, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(b, err)
	defer app.Close()

	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port := splitHostPort(b, seeder.PeerAddr())
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(port)},
	}})

	route := "bench"
	require.NoError(b, app.Service.AddMagnet(route, magnet.String()))

	path := "/" + route + "/bench.bin"
	require.Eventually(b, func() bool {
		f, err := app.FS.Open(path)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}, 15*time.Second, 50*time.Millisecond, "file did not appear in route")

	buf := make([]byte, 64*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := app.FS.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := f.Read(buf); err != nil {
			b.Fatal(err)
		}
		_ = f.Close()
	}
}

// runThrottledTTFB runs b.N iterations of: build a fresh throttled TestApp
// (no already-downloaded data to serve from cache), register the seeder as
// a peer, and time from that registration to the first successful read of
// the magnet's file — the actual network-bound path a badly-tuned readahead
// or read-timeout would show up on. Setup/teardown outside that path is
// excluded via StopTimer/StartTimer.
//
// Start the timer before AddPeers so the dial (and injected latency) is
// captured. AddPeers must come before Service.AddMagnet: the latter blocks
// fetching torrent metadata, which can only arrive over a connection to a
// peer that's already been registered — reversing the order deadlocks
// until the add-timeout.
func runThrottledTTFB(b *testing.B, magnet metainfo.Magnet, host string, seederPort uint16, latency time.Duration, bps int) {
	b.Helper()
	buf := make([]byte, 64*1024)

	b.StopTimer()
	for i := 0; i < b.N; i++ {
		app, err := NewTestAppNoDefaultDialer()
		require.NoError(b, err)
		app.Client.AddDialer(ThrottledDialer{Latency: latency, BytesPerSecond: bps})

		tMagnet, _ := app.Client.AddMagnet(magnet.String())
		route := "bench"
		path := "/" + route + "/" + magnet.DisplayName

		b.StartTimer()
		tMagnet.AddPeers([]torrent.PeerInfo{{
			Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(seederPort)},
		}})
		require.NoError(b, app.Service.AddMagnet(route, magnet.String()))
		require.Eventually(b, func() bool {
			f, err := app.FS.Open(path)
			if err != nil {
				return false
			}
			defer f.Close()
			_, err = f.Read(buf)
			return err == nil
		}, 30*time.Second, 5*time.Millisecond, "file did not become readable")
		b.StopTimer()

		app.Close()
	}
}

// BenchmarkFileHandle_OpenToFirstRead_Throttled measures time-to-first-byte
// against the real anacrolix/torrent client under WAN-like conditions
// (added RTT, capped transfer rate via ThrottledDialer) instead of loopback,
// across a spread of connection profiles at a fixed (small, 256KiB) piece
// length. See BenchmarkFileHandle_OpenToFirstRead_Throttled_PieceLength for
// the piece-length dimension, held fixed here so these three rows stay
// comparable across commits.
func BenchmarkFileHandle_OpenToFirstRead_Throttled(b *testing.B) {
	profiles := []struct {
		name    string
		latency time.Duration
		bps     int // bytes/sec
	}{
		{"cable/20ms_12MBps", 20 * time.Millisecond, 12 * 1024 * 1024},
		{"dsl/40ms_1MBps", 40 * time.Millisecond, 1024 * 1024},
		{"satellite/600ms_3MBps", 600 * time.Millisecond, 3 * 1024 * 1024},
	}

	for _, prof := range profiles {
		b.Run(prof.name, func(b *testing.B) {
			tracker := NewTracker()
			require.NoError(b, tracker.Start())
			defer tracker.Stop()

			seeder, err := NewSeeder()
			require.NoError(b, err)
			defer seeder.Stop()

			content := make([]byte, 8*1024*1024)
			for i := range content {
				content[i] = byte(i)
			}
			magnet, err := seeder.AddFile("bench.bin", content, tracker.AnnounceURL())
			require.NoError(b, err)
			tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

			host, seederPort := splitHostPort(b, seeder.PeerAddr())
			runThrottledTTFB(b, magnet, host, seederPort, prof.latency, prof.bps)
		})
	}
}

// BenchmarkFileHandle_OpenToFirstRead_Throttled_PieceLength isolates piece
// length as its own dimension, holding the connection profile fixed (DSL:
// latency-and-bandwidth-constrained, where the cost of a large incomplete
// piece is most visible). AddFile's default of 256KiB is far smaller than a
// typical media torrent's (often 2-8MiB); the SetResponsive()/readahead
// trade-off in internal/fs/torrent.go is piece-length-sensitive, and the
// existing suite couldn't previously detect that at all.
func BenchmarkFileHandle_OpenToFirstRead_Throttled_PieceLength(b *testing.B) {
	const (
		latency = 40 * time.Millisecond
		bps     = 1024 * 1024 // 1MB/s
	)
	pieceLengths := []int64{256 * 1024, 1024 * 1024, 4 * 1024 * 1024}

	for _, pl := range pieceLengths {
		b.Run(fmt.Sprintf("piece=%dKiB", pl/1024), func(b *testing.B) {
			tracker := NewTracker()
			require.NoError(b, tracker.Start())
			defer tracker.Stop()

			seeder, err := NewSeeder()
			require.NoError(b, err)
			defer seeder.Stop()

			// Large enough to span several pieces even at 4MiB.
			content := make([]byte, 16*1024*1024)
			for i := range content {
				content[i] = byte(i)
			}
			magnet, err := seeder.AddFileWithPieceLength("bench.bin", content, tracker.AnnounceURL(), pl)
			require.NoError(b, err)
			tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

			host, seederPort := splitHostPort(b, seeder.PeerAddr())
			runThrottledTTFB(b, magnet, host, seederPort, latency, bps)
		})
	}
}

// BenchmarkFileHandle_SequentialThroughput measures sustained sequential
// read throughput (b.SetBytes lets `benchstat` report MB/s directly) under
// the cable profile — bandwidth-bound rather than RTT-bound, the case where
// readahead size should matter most: a too-small readahead window leaves
// the connection idle between piece requests instead of keeping it full.
// Each iteration streams the whole file through a fresh app, so a change
// that helps time-to-first-byte at the expense of steady-state throughput
// (or vice versa) shows up as a regression here even if
// OpenToFirstRead_Throttled improves.
func BenchmarkFileHandle_SequentialThroughput(b *testing.B) {
	const (
		latency = 20 * time.Millisecond
		bps     = 12 * 1024 * 1024 // 12MB/s, cable profile
	)

	tracker := NewTracker()
	require.NoError(b, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(b, err)
	defer seeder.Stop()

	content := make([]byte, 8*1024*1024)
	for i := range content {
		content[i] = byte(i)
	}
	magnet, err := seeder.AddFile("bench.bin", content, tracker.AnnounceURL())
	require.NoError(b, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	host, seederPort := splitHostPort(b, seeder.PeerAddr())

	buf := make([]byte, 128*1024)
	b.SetBytes(int64(len(content)))

	b.StopTimer()
	for i := 0; i < b.N; i++ {
		app, err := NewTestAppNoDefaultDialer()
		require.NoError(b, err)
		app.Client.AddDialer(ThrottledDialer{Latency: latency, BytesPerSecond: bps})

		tMagnet, _ := app.Client.AddMagnet(magnet.String())
		route := "bench"
		path := "/" + route + "/bench.bin"

		b.StartTimer()
		tMagnet.AddPeers([]torrent.PeerInfo{{
			Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(seederPort)},
		}})
		require.NoError(b, app.Service.AddMagnet(route, magnet.String()))

		var f dfs.File
		require.Eventually(b, func() bool {
			opened, err := app.FS.Open(path)
			if err != nil {
				return false
			}
			f = opened
			return true
		}, 30*time.Second, 5*time.Millisecond, "file did not become readable")

		var read int
		for read < len(content) {
			n, err := f.Read(buf)
			read += n
			if err != nil {
				break
			}
		}
		_ = f.Close()
		b.StopTimer()
		require.Equal(b, len(content), read, "did not stream the full file")

		app.Close()
	}
}

// BenchmarkFileHandle_ConcurrentStreams measures wall time for N goroutines
// simultaneously doing a cold first read, all against one shared TestApp
// (one anacrolix Client) — the case where the per-read client-wide lock
// (internal/fs/torrent.go's readAtWrapper is backed by anacrolix's own
// Client-scoped reader lock; see reader.mu = t.cl.locker() in the library)
// could make concurrent streams contend with each other instead of
// scaling. Each stream is a distinct single-file torrent seeded by the same
// seeder process, so N independent swarms share one client and one seeder.
func BenchmarkFileHandle_ConcurrentStreams(b *testing.B) {
	const (
		latency = 20 * time.Millisecond
		bps     = 12 * 1024 * 1024 // 12MB/s, cable profile
	)

	for _, n := range []int{4, 16} {
		b.Run(fmt.Sprintf("streams=%d", n), func(b *testing.B) {
			tracker := NewTracker()
			require.NoError(b, tracker.Start())
			defer tracker.Stop()

			seeder, err := NewSeeder()
			require.NoError(b, err)
			defer seeder.Stop()

			magnets := make([]metainfo.Magnet, n)
			for i := 0; i < n; i++ {
				content := make([]byte, 2*1024*1024)
				for j := range content {
					content[j] = byte(i*31 + j)
				}
				name := fmt.Sprintf("stream%d.bin", i)
				m, err := seeder.AddFile(name, content, tracker.AnnounceURL())
				require.NoError(b, err)
				tracker.RegisterPeer(m.InfoHash, seeder.PeerAddr())
				magnets[i] = m
			}
			host, seederPort := splitHostPort(b, seeder.PeerAddr())
			buf := make([]byte, 64*1024)

			b.StopTimer()
			for iter := 0; iter < b.N; iter++ {
				app, err := NewTestAppNoDefaultDialer()
				require.NoError(b, err)
				app.Client.AddDialer(ThrottledDialer{Latency: latency, BytesPerSecond: bps})

				var wg sync.WaitGroup
				b.StartTimer()
				for i := 0; i < n; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						magnet := magnets[i]
						route := fmt.Sprintf("bench%d", i)
						path := "/" + route + "/" + magnet.DisplayName

						tMagnet, _ := app.Client.AddMagnet(magnet.String())
						tMagnet.AddPeers([]torrent.PeerInfo{{
							Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(seederPort)},
						}})
						if err := app.Service.AddMagnet(route, magnet.String()); err != nil {
							b.Error(err)
							return
						}

						deadline := time.Now().Add(30 * time.Second)
						for time.Now().Before(deadline) {
							f, err := app.FS.Open(path)
							if err == nil {
								_, err = f.Read(buf)
								_ = f.Close()
								if err == nil {
									return
								}
							}
							time.Sleep(5 * time.Millisecond)
						}
						b.Error("stream did not become readable in time")
					}(i)
				}
				wg.Wait()
				b.StopTimer()

				app.Close()
			}
		})
	}
}
