package torrent

import (
	"github.com/Apollogeddon/distribyted/fs"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type mockTorrent struct {
	fs.Torrent
	hash           metainfo.Hash
	gotInfo        chan struct{}
	name           string
	pieceStateRuns torrent.PieceStateRuns
	stats          torrent.TorrentStats
	statsFunc      func() torrent.TorrentStats
	info           *metainfo.Info
}

func (m *mockTorrent) InfoHash() metainfo.Hash { return m.hash }
func (m *mockTorrent) Info() *metainfo.Info    { return m.info }
func (m *mockTorrent) GotInfo() <-chan struct{} { return m.gotInfo }
func (m *mockTorrent) Name() string            { return m.name }
func (m *mockTorrent) Drop()                   {}
func (m *mockTorrent) PieceStateRuns() torrent.PieceStateRuns { return m.pieceStateRuns }
func (m *mockTorrent) Stats() torrent.TorrentStats {
	if m.statsFunc != nil {
		return m.statsFunc()
	}
	return m.stats
}
func (m *mockTorrent) Files() []*torrent.File { return nil }

type mockTorrentClient struct {
	TorrentClient
	addMagnetFunc          func(string) (fs.Torrent, error)
	addTorrentFromFileFunc func(string) (fs.Torrent, error)
	torrentFunc            func(metainfo.Hash) (fs.Torrent, bool)
	closeFunc              func()
}

func (m *mockTorrentClient) AddMagnet(s string) (fs.Torrent, error) {
	if m.addMagnetFunc != nil {
		return m.addMagnetFunc(s)
	}
	return nil, nil
}

func (m *mockTorrentClient) AddTorrentFromFile(s string) (fs.Torrent, error) {
	if m.addTorrentFromFileFunc != nil {
		return m.addTorrentFromFileFunc(s)
	}
	return nil, nil
}

func (m *mockTorrentClient) Torrent(h metainfo.Hash) (fs.Torrent, bool) {
	if m.torrentFunc != nil {
		return m.torrentFunc(h)
	}
	return nil, false
}

func (m *mockTorrentClient) Close() {
	if m.closeFunc != nil {
		m.closeFunc()
	}
}
