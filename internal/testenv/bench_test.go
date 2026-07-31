package testenv

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"
)

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
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
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

// BenchmarkFileHandle_OpenToFirstRead_Throttled measures time-to-first-byte
// against the real anacrolix/torrent client under WAN-like conditions
// (added RTT, capped transfer rate via ThrottledDialer) instead of loopback.
//
// Unlike BenchmarkFileHandle_OpenToFirstRead, each b.N iteration builds a
// fresh TestApp (so there's no already-downloaded data to serve from cache)
// and times from the peer connection being registered to the first
// successful read — the actual network-bound path a badly-tuned readahead
// or read-timeout would show up on. Setup/teardown that isn't part of that
// path is excluded via StopTimer/StartTimer.
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

			host, port, _ := net.SplitHostPort(seeder.PeerAddr())
			var seederPort uint16
			_, _ = fmt.Sscanf(port, "%d", &seederPort)

			buf := make([]byte, 64*1024)

			b.StopTimer()
			for i := 0; i < b.N; i++ {
				app, err := NewTestAppNoDefaultDialer()
				require.NoError(b, err)
				app.Client.AddDialer(ThrottledDialer{Latency: prof.latency, BytesPerSecond: prof.bps})

				tMagnet, _ := app.Client.AddMagnet(magnet.String())
				route := "bench"
				path := "/" + route + "/bench.bin"

				// Start the timer before AddPeers so the dial (and our
				// injected latency) is captured. AddPeers must come before
				// Service.AddMagnet: the latter blocks fetching torrent
				// metadata, which can only arrive over a connection to a
				// peer that's already been registered — reversing the
				// order deadlocks until the add-timeout.
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
		})
	}
}
