package torrent

import (
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
)

func TestStats(t *testing.T) {
	s := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	st := torrent.TorrentStats{
		TorrentGauges: torrent.TorrentGauges{
			TotalPeers:       10,
			ConnectedSeeders: 5,
		},
	}

	psr := torrent.PieceStateRuns{
		{Length: 10, PieceState: torrent.PieceState{Completion: storage.Completion{Complete: true, Ok: true}}},
		{Length: 5, PieceState: torrent.PieceState{Partial: true, Completion: storage.Completion{Ok: true}}},
	}

	mockT := &mockTorrent{
		hash: hash,
		name: "test-torrent",
		statsFunc: func() torrent.TorrentStats {
			return st
		},
		pieceStateRuns: psr,
		info: &metainfo.Info{
			PieceLength: 16384,
			Name:        "test-torrent",
		},
	}

	s.AddRoute("test-route")
	s.Add("test-route", mockT)

	t.Run("Get Stats", func(t *testing.T) {
		ts, err := s.Stats(hash.String())
		require.NoError(t, err)
		require.Equal(t, "test-torrent", ts.Name)
		require.Equal(t, hash.String(), ts.Hash)
		require.Equal(t, 10, ts.Peers)
		require.Equal(t, 5, ts.Seeders)
		require.Equal(t, 15, ts.TotalPieces)
		require.Equal(t, int64(16384), ts.PieceSize)
		require.Len(t, ts.PieceChunks, 2)
	})

	t.Run("Routes Stats", func(t *testing.T) {
		rs := s.RoutesStats()
		require.Len(t, rs, 1)
		require.Equal(t, "test-route", rs[0].Name)
		require.Len(t, rs[0].TorrentStats, 1)
	})

	t.Run("Global Stats", func(t *testing.T) {
		gs := s.GlobalStats()
		require.NotNil(t, gs)
	})

	t.Run("Get All Torrents", func(t *testing.T) {
		all := s.GetAllTorrents()
		require.Len(t, all, 1)
		require.Contains(t, all, hash.String())
	})

	t.Run("Get Route From Hash", func(t *testing.T) {
		route := s.GetRouteFromHash(hash.String())
		require.Equal(t, "test-route", route)
	})

	t.Run("Delete", func(t *testing.T) {
		s.Del("test-route", hash.String())
		_, err := s.Stats(hash.String())
		require.Error(t, err)
		require.Equal(t, ErrTorrentNotFound, err)
	})
}

// TestStats_AgeSeconds guards the fix for premature "seeders too low"
// warnings: Seeders only counts currently-connected, confirmed-seeding
// peers, which takes time to ramp up after a torrent is added — a healthy
// swarm can briefly show 0-1 seeders right at startup. AgeSeconds lets
// consumers withhold that warning until enough time has passed, so it must
// reflect real elapsed time since Add(), not reset on every poll.
func TestStats_AgeSeconds(t *testing.T) {
	s := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	mockT := &mockTorrent{
		hash: hash,
		name: "test-torrent",
		statsFunc: func() torrent.TorrentStats {
			return torrent.TorrentStats{}
		},
		info: &metainfo.Info{PieceLength: 16384, Name: "test-torrent"},
	}

	s.AddRoute("test-route")
	s.Add("test-route", mockT)

	ts, err := s.Stats(hash.String())
	require.NoError(t, err)
	require.InDelta(t, 0, ts.AgeSeconds, 1, "a freshly-added torrent should have ~0 age")

	// Backdate as if the torrent was added 30s ago, simulating polling well
	// after startup — AgeSeconds must reflect that, not reset to ~0 again.
	s.addedAt[hash.String()] = time.Now().Add(-30 * time.Second)

	ts, err = s.Stats(hash.String())
	require.NoError(t, err)
	require.InDelta(t, 30, ts.AgeSeconds, 1)
}

func TestStats_Sorting(t *testing.T) {
	require := require.New(t)

	// Test ByName sorting (for RouteStats)
	rs := ByName{
		{Name: "b"},
		{Name: "a"},
	}
	require.True(rs.Less(1, 0))
	rs.Swap(0, 1)
	require.Equal("a", rs[0].Name)
	require.Equal(2, rs.Len())

	// Test byName sorting (for TorrentStats)
	ts := byName{
		{Name: "b"},
		{Name: "a"},
	}
	require.True(ts.Less(1, 0))
	ts.Swap(0, 1)
	require.Equal("a", ts[0].Name)
	require.Equal(2, ts.Len())
}

func TestStats_PieceStatus(t *testing.T) {
	s := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	cases := []struct {
		psr    torrent.PieceStateRuns
		expect PieceStatus
	}{
		{torrent.PieceStateRuns{{Length: 1, PieceState: torrent.PieceState{Checking: true, Completion: storage.Completion{Ok: true}}}}, Checking},
		{torrent.PieceStateRuns{{Length: 1, PieceState: torrent.PieceState{Partial: true, Completion: storage.Completion{Ok: true}}}}, Partial},
		{torrent.PieceStateRuns{{Length: 1, PieceState: torrent.PieceState{Completion: storage.Completion{Complete: true, Ok: true}}}}, Complete},
		{torrent.PieceStateRuns{{Length: 1, PieceState: torrent.PieceState{Completion: storage.Completion{Ok: false}}}}, Error},
		{torrent.PieceStateRuns{{Length: 1, PieceState: torrent.PieceState{Completion: storage.Completion{Ok: true}}}}, Waiting},
	}

	for _, c := range cases {
		mockT := &mockTorrent{
			hash:           hash,
			pieceStateRuns: c.psr,
		}
		s.Add("route", mockT)
		backdateSamples(s, 5*time.Second) // Force update
		ts, _ := s.Stats(hash.String())
		require.Equal(t, c.expect, ts.PieceChunks[0].Status)
	}
}

func TestStats_Measurements(t *testing.T) {
	s := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")

	st := torrent.TorrentStats{}
	st.BytesReadData.Add(100)
	st.BytesWrittenData.Add(50)

	mockT := &mockTorrent{
		hash: hash,
	}
	mockT.statsFunc = func() torrent.TorrentStats {
		st := torrent.TorrentStats{}
		st.BytesReadData.Add(100)
		st.BytesWrittenData.Add(50)
		return st
	}

	s.Add("route", mockT)

	// First measurement
	ts1, _ := s.Stats(hash.String())
	require.Equal(t, int64(100), ts1.DownloadedBytes)
	require.Equal(t, int64(50), ts1.UploadedBytes)

	// Force gap to be passed
	backdateSamples(s, 5*time.Second)

	// Second measurement with more data
	mockT.statsFunc = func() torrent.TorrentStats {
		st := torrent.TorrentStats{}
		st.BytesReadData.Add(150)
		st.BytesWrittenData.Add(80)
		return st
	}

	ts2, _ := s.Stats(hash.String())
	require.Equal(t, int64(50), ts2.DownloadedBytes)
	require.Equal(t, int64(30), ts2.UploadedBytes)

	// read again within the gap: the same sample, not a new one
	ts3, _ := s.Stats(hash.String())
	require.Equal(t, int64(50), ts3.DownloadedBytes)
	require.Equal(t, int64(30), ts3.UploadedBytes)
}

// backdateSamples moves every torrent's latest sample into the past, so the next read
// takes a new one instead of returning the cached sample.
func backdateSamples(s *Stats, by time.Duration) {
	s.mut.Lock()
	defer s.mut.Unlock()
	for _, p := range s.previousStats {
		if !p.time.IsZero() {
			p.time = p.time.Add(-by)
		}
	}
}

func TestStats_RatesAreConsistent(t *testing.T) {
	s := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	read := int64(0)
	mockT := &mockTorrent{hash: hash, statsFunc: func() torrent.TorrentStats {
		st := torrent.TorrentStats{TorrentGauges: torrent.TorrentGauges{TotalPeers: 3, ConnectedSeeders: 1}}
		st.BytesReadData.Add(read)
		return st
	}}
	s.Add("route", mockT)

	// the first sample is measured from when the torrent was added, not from the zero
	// time, which made every speed round to nothing
	ts, err := s.Stats(hash.String())
	require.NoError(t, err)
	require.Less(t, ts.TimePassed, 5.0)

	read = 10_000
	backdateSamples(s, 2*time.Second)
	ts, err = s.Stats(hash.String())
	require.NoError(t, err)
	require.InDelta(t, 5_000, float64(ts.DownloadedBytes)/ts.TimePassed, 100)

	// reading again within the gap returns the same sample: same rate, and the peers and
	// seeders it measured rather than zeros
	again, err := s.Stats(hash.String())
	require.NoError(t, err)
	require.InDelta(t, 5_000, float64(again.DownloadedBytes)/again.TimePassed, 100)
	require.Equal(t, 3, again.Peers)
	require.Equal(t, 1, again.Seeders)

	// several clients polling the global figure don't inflate it
	for range 5 {
		gs := s.GlobalStats()
		require.InDelta(t, 5_000, float64(gs.DownloadedBytes)/gs.TimePassed, 100)
	}
}

func TestStats_DelKeepsOtherRoutes(t *testing.T) {
	s := NewStats()
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	mockT := &mockTorrent{hash: hash}
	s.Add("a", mockT)
	s.Add("b", mockT)

	s.Del("a", hash.String())
	require.Empty(t, s.GetTorrentsInRoute("a"))
	require.Contains(t, s.GetTorrentsInRoute("b"), hash.String())
	_, err := s.Stats(hash.String())
	require.NoError(t, err, "the torrent is still in route b")

	s.Del("b", hash.String())
	_, err = s.Stats(hash.String())
	require.ErrorIs(t, err, ErrTorrentNotFound)
}
