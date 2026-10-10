package testenv

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// PeerProfile is how a peer in a Swarm looks from distribyted: the delay before its TCP
// handshake, standing in for round-trip time, and its upload speed (0 for unlimited).
type PeerProfile struct {
	Latency        time.Duration
	BytesPerSecond int
}

// SwarmSpec describes a swarm for one torrent, shaped like the ones that struggle in
// practice: a few seeders, among peers that have none of the torrent and addresses that
// never answer, all handed out by the tracker in no particular order.
type SwarmSpec struct {
	// Seeders each have the whole torrent.
	Seeders []PeerProfile
	// Leechers accept connections but have none of the torrent and never get any, so each
	// takes up one of distribyted's connections for nothing.
	Leechers       int
	LeecherProfile PeerProfile
	// Dead addresses never answer: dialling one waits for the dial timeout, as for a peer
	// that has gone offline or sits behind a firewall.
	Dead int

	Name        string
	Size        int
	PieceLength int64
}

// Swarm runs SwarmSpec's peers and a tracker for them on loopback. Its Magnet names the
// tracker, so distribyted finds the peers the way it does for real: by announcing.
type Swarm struct {
	Magnet  metainfo.Magnet
	Content []byte
	Dialer  *SwarmDialer

	tracker  *Tracker
	seeders  []*Seeder
	leechers []*Leecher
}

// deadNet holds the Swarm's dead addresses: TEST-NET-1, which is reserved for
// documentation and never routed, though SwarmDialer never dials it anyway.
var deadNet = net.IPv4(192, 0, 2, 0)

func NewSwarm(spec SwarmSpec) (_ *Swarm, err error) {
	if len(spec.Seeders) == 0 {
		return nil, errors.New("a swarm needs a seeder")
	}
	s := &Swarm{
		tracker: NewTracker(),
		Dialer:  &SwarmDialer{profiles: map[string]PeerProfile{}},
	}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	if err := s.tracker.Start(); err != nil {
		return nil, err
	}

	s.Content = make([]byte, spec.Size)
	for i := range s.Content {
		s.Content[i] = byte(i * 7)
	}

	var addrs []string
	var mi metainfo.MetaInfo
	for _, p := range spec.Seeders {
		sd, err := NewSeeder()
		if err != nil {
			return nil, err
		}
		s.seeders = append(s.seeders, sd)
		m, err := sd.AddFileWithPieceLength(spec.Name, s.Content, "", spec.PieceLength)
		if err != nil {
			return nil, err
		}
		s.Magnet = m
		mi, _ = sd.MetaInfo(m.InfoHash)
		a := loopback(sd.PeerAddr())
		s.Dialer.profiles[a] = p
		addrs = append(addrs, a)
	}
	for range spec.Leechers {
		l, err := NewLeecher(mi)
		if err != nil {
			return nil, err
		}
		s.leechers = append(s.leechers, l)
		a := loopback(l.PeerAddr())
		s.Dialer.profiles[a] = spec.LeecherProfile
		addrs = append(addrs, a)
	}
	for i := range spec.Dead {
		ip := make(net.IP, 4)
		copy(ip, deadNet.To4())
		ip[3] = byte(1 + i%250)
		addrs = append(addrs, net.JoinHostPort(ip.String(), fmt.Sprint(10000+i)))
	}

	// a fixed shuffle: the same order every run, with the seeders somewhere in the middle
	r := rand.New(rand.NewPCG(1, uint64(len(addrs))))
	r.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })
	for _, a := range addrs {
		s.tracker.RegisterPeer(s.Magnet.InfoHash, a)
	}

	s.Magnet.Trackers = []string{s.tracker.AnnounceURL()}
	return s, nil
}

// loopback is a listen address as peers dial it: 127.0.0.1 rather than the unspecified
// address a client listens on.
func loopback(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return net.JoinHostPort("127.0.0.1", port)
}

func (s *Swarm) Close() {
	for _, l := range s.leechers {
		l.Stop()
	}
	for _, sd := range s.seeders {
		sd.Stop()
	}
	s.tracker.Stop()
}

// SwarmDialer connects distribyted to a Swarm's peers, each with its own PeerProfile, and
// never connects to a dead address. Add it with app.Client.AddDialer to an app whose own
// dialer is off (NewTestAppSwarm), or the unthrottled default wins every race.
type SwarmDialer struct {
	profiles map[string]PeerProfile

	mu    sync.Mutex
	dials map[string]int
}

func (d *SwarmDialer) DialerNetwork() string { return "tcp" }

func (d *SwarmDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	d.mu.Lock()
	if d.dials == nil {
		d.dials = map[string]int{}
	}
	d.dials[addr]++
	d.mu.Unlock()

	p, ok := d.profiles[addr]
	if !ok {
		host, _, _ := net.SplitHostPort(addr)
		if ip := net.ParseIP(host).To4(); ip != nil && ip[0] == 192 && ip[1] == 0 && ip[2] == 2 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("swarm: no peer at %s", addr)
	}
	return ThrottledDialer(p).Dial(ctx, addr)
}

// Dials is how many times distribyted tried each kind of address.
func (d *SwarmDialer) Dials() (live, dead int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for a, n := range d.dials {
		if _, ok := d.profiles[a]; ok {
			live += n
		} else {
			dead += n
		}
	}
	return live, dead
}

// Leecher is a peer with a torrent's metadata and none of its data, which it never
// downloads.
type Leecher struct {
	client *torrent.Client
	tmpDir string
}

func NewLeecher(mi metainfo.MetaInfo) (*Leecher, error) {
	tmpDir, err := os.MkdirTemp("", "leecher")
	if err != nil {
		return nil, err
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DefaultStorage = storage.NewMMap(tmpDir)
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.DisablePEX = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.Seed = false
	cfg.HeaderObfuscationPolicy.Preferred = false
	cfg.HeaderObfuscationPolicy.RequirePreferred = true

	c, err := torrent.NewClient(cfg)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}
	l := &Leecher{client: c, tmpDir: tmpDir}
	// only the info: the generated metainfo's empty piece layers fail to add
	t, err := c.AddTorrent(&metainfo.MetaInfo{InfoBytes: mi.InfoBytes})
	if err != nil {
		l.Stop()
		return nil, err
	}
	t.DisallowDataDownload()
	return l, nil
}

func (l *Leecher) PeerAddr() string {
	if addrs := l.client.ListenAddrs(); len(addrs) > 0 {
		return addrs[0].String()
	}
	return ""
}

func (l *Leecher) Stop() {
	l.client.Close()
	_ = os.RemoveAll(l.tmpDir)
}
