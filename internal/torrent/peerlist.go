package torrent

import (
	"cmp"
	"slices"

	"github.com/anacrolix/torrent"

	"github.com/Apollogeddon/distribyted/internal/fs"
)

// PeerInfo is one connected peer of a torrent, for the web interface.
type PeerInfo struct {
	Addr   string // its address and port, in full
	Client string // the client it says it is, or ""
	Source string // how it was found, in words
	// Down and Up are bytes a second: Down averaged over the time data was expected from it,
	// Up over its last write
	Down, Up float64
	// Have is how many of the torrent's pieces it has, of Of
	Have, Of int
	Seed     bool
}

var peerSources = map[torrent.PeerSource]string{
	torrent.PeerSourceTracker:         "Tracker",
	torrent.PeerSourceIncoming:        "Connected to us",
	torrent.PeerSourceDhtGetPeers:     "DHT",
	torrent.PeerSourceDhtAnnouncePeer: "DHT",
	torrent.PeerSourcePex:             "Peer exchange",
	torrent.PeerSourceDirect:          "Saved peer",
	torrent.PeerSourceUtHolepunch:     "Hole punch",
}

// Peers lists t's connected peers, seeders first, then the fastest.
func Peers(t fs.Torrent) []PeerInfo {
	pc, ok := t.(peerConner)
	if !ok {
		return nil
	}
	of := 0
	if info := t.Info(); info != nil {
		of = info.NumPieces()
	}
	var out []PeerInfo
	for _, c := range pc.PeerConns() {
		st := c.Stats()
		p := PeerInfo{
			Source: peerSources[c.Discovery],
			Down:   st.DownloadRate,
			Up:     st.LastWriteUploadRate,
			Have:   st.RemotePieceCount,
			Of:     of,
		}
		if c.RemoteAddr != nil {
			p.Addr = c.RemoteAddr.String()
		}
		if name, ok := c.PeerClientName.Load().(string); ok {
			p.Client = name
		}
		if p.Source == "" {
			p.Source = string(c.Discovery)
		}
		p.Seed = of > 0 && p.Have >= of
		out = append(out, p)
	}
	SortPeers(out)
	return out
}

// SortPeers puts seeders first, then the fastest, then by address.
func SortPeers(ps []PeerInfo) {
	slices.SortStableFunc(ps, func(a, b PeerInfo) int {
		if a.Seed != b.Seed {
			if a.Seed {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.Down, a.Down), cmp.Compare(a.Addr, b.Addr))
	})
}
