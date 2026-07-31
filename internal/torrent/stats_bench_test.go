package torrent

import (
	"fmt"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// setupStatsWithTorrents builds a Stats with n mock torrents registered
// under a single route, each with a small piece-state-run set so the
// piece-chunk conversion RoutesStats does has real (if trivial) work per
// torrent, not just an empty loop.
func setupStatsWithTorrents(n int) *Stats {
	s := NewStats()
	s.AddRoute("bench-route")

	psr := torrent.PieceStateRuns{
		{Length: 100, PieceState: torrent.PieceState{Completion: storage.Completion{Complete: true, Ok: true}}},
		{Length: 50, PieceState: torrent.PieceState{Partial: true, Completion: storage.Completion{Ok: true}}},
		{Length: 25, PieceState: torrent.PieceState{}},
	}

	for i := 0; i < n; i++ {
		hash := metainfo.NewHashFromHex(fmt.Sprintf("%040x", i+1))
		mockT := &mockTorrent{
			hash: hash,
			name: fmt.Sprintf("bench-torrent-%d", i),
			statsFunc: func() torrent.TorrentStats {
				return torrent.TorrentStats{
					TorrentGauges: torrent.TorrentGauges{TotalPeers: 10, ConnectedSeeders: 5},
				}
			},
			pieceStateRuns: psr,
			info:           &metainfo.Info{PieceLength: 16384, Name: fmt.Sprintf("bench-torrent-%d", i)},
		}
		s.Add("bench-route", mockT)
	}

	return s
}

// BenchmarkStats_RoutesStats measures the cost of recomputing piece-state
// runs for every torrent, as torrent count grows. This backs /api/routes,
// polled every 2s by the dashboard (routes.js) for as long as the tab stays
// open, so its cost scales with how many torrents exist, not with anything
// the user is actively doing. gTime is backdated before every iteration so
// each one takes the real computation path rather than Stats' own 2s
// previous-measurement cache (which would otherwise make every iteration
// after the first artificially free in a tight benchmark loop).
func BenchmarkStats_RoutesStats(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("torrents=%d", n), func(b *testing.B) {
			s := setupStatsWithTorrents(n)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.gTime = time.Now().Add(-5 * time.Second)
				_ = s.RoutesStats()
			}
		})
	}
}

// BenchmarkStats_GlobalStats is the same concern as RoutesStats above, for
// the /api/status endpoint (dashboard.js, also 2s-polled).
func BenchmarkStats_GlobalStats(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("torrents=%d", n), func(b *testing.B) {
			s := setupStatsWithTorrents(n)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.gTime = time.Now().Add(-5 * time.Second)
				_ = s.GlobalStats()
			}
		})
	}
}
