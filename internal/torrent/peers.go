package torrent

import (
	"net"
	"net/netip"
	"slices"
	"sort"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

// A torrent's good peers, those that sent it data, are saved so that the next time it
// needs peers, after sitting unread or after a restart, it tries them first rather than
// finding them again among every address trackers and the DHT return, most of which in a
// swarm with few seeders have nothing or don't answer. Saving addresses costs nothing
// while the torrent is unread: no connection is kept open for it.
const (
	// maxSavedPeers is how many addresses are kept per torrent, the latest good ones first.
	maxSavedPeers = 20
	// peerRecordInterval is how often the connected peers that sent data are recorded.
	peerRecordInterval = 30 * time.Second
	// peerOfferInterval is the least time between offering a torrent its saved peers,
	// which every reader opened on it asks for.
	peerOfferInterval = 15 * time.Second
)

type peerConner interface {
	PeerConns() []*torrent.PeerConn
}

type peerAdder interface {
	AddPeers([]torrent.PeerInfo) int
}

// usefulPeers returns the addresses of t's connected peers that have sent it data, the
// ones that sent most first. A connection a peer opened to us is listed at its own,
// passing, port, which won't take a connection; it simply fails when tried.
func usefulPeers(t fs.Torrent) []string {
	pc, ok := t.(peerConner)
	if !ok {
		return nil
	}
	type peer struct {
		addr string
		read int64
	}
	var ps []peer
	for _, c := range pc.PeerConns() {
		st := c.Stats()
		read := st.BytesReadUsefulData.Int64()
		if read == 0 || c.RemoteAddr == nil {
			continue
		}
		ap, err := netip.ParseAddrPort(c.RemoteAddr.String())
		if err != nil {
			continue
		}
		ps = append(ps, peer{ap.String(), read})
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].read > ps[j].read })
	addrs := make([]string, 0, len(ps))
	for _, p := range ps {
		addrs = append(addrs, p.addr)
	}
	return addrs
}

// mergePeers puts fresh before saved, without repeats, keeping at most max.
func mergePeers(fresh, saved []string, maxPeers int) []string {
	out := make([]string, 0, maxPeers)
	for _, a := range slices.Concat(fresh, saved) {
		if len(out) == maxPeers {
			break
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

func (s *Service) runPeerRecorder() {
	ticker := time.NewTicker(peerRecordInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.RecordPeers()
		}
	}
}

// RecordPeers saves every torrent's connected peers that have sent it data. It runs every
// peerRecordInterval, and on shutdown, before the torrent database closes, so peers found
// since the last run aren't lost.
func (s *Service) RecordPeers() {
	for _, t := range s.s.GetAllTorrents() {
		s.recordPeers(t)
	}
}

// recordPeers saves t's connected peers that have sent it data, ahead of those saved
// before, if that changes the list.
func (s *Service) recordPeers(t fs.Torrent) {
	fresh := usefulPeers(t)
	if len(fresh) == 0 {
		return
	}
	hash := t.InfoHash().HexString()
	saved := s.db.LoadPeers(hash)
	merged := mergePeers(fresh, saved, maxSavedPeers)
	if slices.Equal(merged, saved) {
		return
	}
	if err := s.db.SavePeers(hash, merged); err != nil {
		s.log.Warn().Err(err).Str(dlog.KeyHash, hash).Msg("saving the torrent's good peers")
	}
}

// offerPeers gives t its saved peers, which it tries before the addresses trackers and
// the DHT return. Peers it already has are ignored, and an unread torrent only connects
// to them once it next needs data. At most once per peerOfferInterval per torrent.
func (s *Service) offerPeers(t fs.Torrent) {
	pa, ok := t.(peerAdder)
	if !ok {
		return
	}
	hash := t.InfoHash().HexString()
	s.mu.Lock()
	if time.Since(s.peersOffered[hash]) < peerOfferInterval {
		s.mu.Unlock()
		return
	}
	s.peersOffered[hash] = time.Now()
	s.mu.Unlock()

	var pis []torrent.PeerInfo
	for _, a := range s.db.LoadPeers(hash) {
		ap, err := netip.ParseAddrPort(a)
		if err != nil {
			continue
		}
		pis = append(pis, torrent.PeerInfo{
			Addr:    net.TCPAddrFromAddrPort(ap),
			Source:  torrent.PeerSourceDirect,
			Trusted: true,
		})
	}
	if len(pis) > 0 {
		pa.AddPeers(pis)
	}
}

// offerPeersOnRead is the TorrentFS read-start callback: the torrent is about to need
// data, so it gets its saved peers. It runs under the file handle's lock, so the work is
// done elsewhere.
func (s *Service) offerPeersOnRead(hash string) {
	t, ok := s.s.GetAllTorrents()[hash]
	if !ok {
		return
	}
	go s.offerPeers(t)
}
