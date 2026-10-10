package torrent

import (
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

func TestMergePeers(t *testing.T) {
	require.Equal(t, []string{"a", "b", "c"}, mergePeers([]string{"a", "b"}, []string{"b", "c"}, 5), "the latest first, no repeats")
	require.Equal(t, []string{"a", "b"}, mergePeers([]string{"a"}, []string{"b", "c"}, 2), "at most max")
	require.Empty(t, mergePeers(nil, nil, 5))
}

// peerTorrent is a mockTorrent that records the peers it's given.
type peerTorrent struct {
	*mockTorrent
	mu    *sync.Mutex
	added *[]string
}

func (t peerTorrent) AddPeers(pis []torrent.PeerInfo) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, pi := range pis {
		*t.added = append(*t.added, pi.Addr.String())
	}
	return len(pis)
}

func (t peerTorrent) addedPeers() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), *t.added...)
}

// TestService_OfferPeers: a torrent is given its saved peers when it's added and when a
// read starts, at most once per peerOfferInterval, and they're forgotten with it.
func TestService_OfferPeers(t *testing.T) {
	hash := metainfo.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4")
	gotInfo := make(chan struct{})
	close(gotInfo)
	var added []string
	tor := peerTorrent{
		mockTorrent: &mockTorrent{hash: hash, name: "film", gotInfo: gotInfo},
		mu:          &sync.Mutex{},
		added:       &added,
	}
	c := &mockTorrentClient{
		addMagnetFunc: func(string) (fs.Torrent, error) { return tor, nil },
		torrentFunc:   func(metainfo.Hash) (fs.Torrent, bool) { return tor, true },
	}
	db := &MockLoaderAdder{}
	require.NoError(t, db.SavePeers(hash.HexString(), []string{"1.2.3.4:6881", "not an address", "5.6.7.8:51413"}))

	svc := NewService(nil, db, NewStats(), c, 1, 1, false, false, nil)
	require.NoError(t, svc.AddMagnet("films", "magnet:?xt=urn:btih:"+hash.HexString()))
	require.Equal(t, []string{"1.2.3.4:6881", "5.6.7.8:51413"}, tor.addedPeers(), "given on add, bad addresses skipped")

	svc.offerPeersOnRead(hash.String())
	time.Sleep(50 * time.Millisecond)
	require.Len(t, tor.addedPeers(), 2, "not again within peerOfferInterval")

	svc.mu.Lock()
	svc.peersOffered[hash.HexString()] = time.Now().Add(-peerOfferInterval)
	svc.mu.Unlock()
	svc.offerPeersOnRead(hash.String())
	require.Eventually(t, func() bool {
		return len(tor.addedPeers()) == 4
	}, time.Second, 10*time.Millisecond, "given again when a read starts later")

	require.NoError(t, svc.RemoveFromHash("films", hash.HexString()))
	require.Empty(t, db.LoadPeers(hash.HexString()), "forgotten with the torrent")
}
