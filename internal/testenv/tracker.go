package testenv

import (
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

type announceResponse struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"` // compact format
}

type Tracker struct {
	addr string
	l    net.Listener
	s    *http.Server

	mu       sync.Mutex
	torrents map[metainfo.Hash][]string // hash -> list of peer addresses
}

func NewTracker() *Tracker {
	return &Tracker{
		torrents: make(map[metainfo.Hash][]string),
	}
}

func (t *Tracker) Start() error {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	t.l = l
	t.addr = l.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/announce", t.handleAnnounce)

	t.s = &http.Server{Handler: mux}
	go func() {
		_ = t.s.Serve(l)
	}()

	return nil
}

func (t *Tracker) Stop() {
	if t.s != nil {
		t.s.Close()
	}
}

func (t *Tracker) Addr() string {
	return t.addr
}

func (t *Tracker) AnnounceURL() string {
	return fmt.Sprintf("http://%s/announce", t.addr)
}

func (t *Tracker) RegisterPeer(hash metainfo.Hash, peerAddr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.torrents[hash] = append(t.torrents[hash], peerAddr)
}

func (t *Tracker) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	infoHashRaw := r.URL.Query().Get("info_hash")
	fmt.Printf("Tracker: Received announce for hash: %x\n", infoHashRaw)
	var hash metainfo.Hash
	copy(hash[:], infoHashRaw)

	t.mu.Lock()
	peers := t.torrents[hash]
	t.mu.Unlock()

	// Compact peer format: 4 bytes IP + 2 bytes Port
	var compactPeers []byte
	for _, p := range peers {
		host, portStr, err := net.SplitHostPort(p)
		if err != nil {
			continue
		}
		ip := net.ParseIP(host).To4()
		if ip == nil {
			continue
		}
		var port uint16
		_, _ = fmt.Sscanf(portStr, "%d", &port)

		compactPeers = append(compactPeers, ip...)
		compactPeers = append(compactPeers, byte(port>>8), byte(port&0xff))
	}

	resp := announceResponse{
		Interval: 60,
		Peers:    string(compactPeers),
	}

	w.Header().Set("Content-Type", "text/plain")
	_ = bencode.NewEncoder(w).Encode(resp)
}
