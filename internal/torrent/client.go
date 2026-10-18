package torrent

import (
	"fmt"
	"net"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	tlog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"

	"github.com/Apollogeddon/distribyted/internal/config"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

// peerKeepAlive is how long a peer connection's writer sleeps when it has nothing to send. anacrolix/torrent's
// writer can miss the signal to send piece requests and sleep until then, so this caps that stall (the library
// default is a minute); a keepalive only goes out while pieces are wanted, so idle connections cost nothing.
const peerKeepAlive = 5 * time.Second

// NewClient builds the anacrolix/torrent client from distribyted's own
// config. opts, if given, are applied to the underlying torrent.ClientConfig
// after distribyted's own settings and before the client is constructed —
// e.g. for tests that need to set ClientConfig.HTTPDialContext (see
// internal/testenv.ThrottledDialer.HTTPDialContext), which can only be
// configured at construction time, unlike the peer dialer which
// torrent.Client.AddDialer can add afterward. Variadic so every existing
// call site compiles unchanged.
func NewClient(st storage.ClientImpl, fis bep44.Store, cfg *config.TorrentGlobal, id [20]byte, opts ...func(*torrent.ClientConfig)) (*torrent.Client, error) {
	// No download or upload rate limit is configurable yet; the client runs unlimited.
	torrentCfg := torrent.NewDefaultClientConfig()
	torrentCfg.Seed = cfg.Seed
	torrentCfg.PeerID = string(id[:])
	torrentCfg.DefaultStorage = st
	torrentCfg.DisableIPv6 = cfg.DisableIPv6
	torrentCfg.DisableTCP = cfg.DisableTCP
	torrentCfg.DisableUTP = cfg.DisableUTP
	torrentCfg.NoDefaultPortForwarding = cfg.DisableUPnP
	torrentCfg.NoDHT = cfg.DisableDHT
	torrentCfg.KeepAliveTimeout = peerKeepAlive
	if cfg.MaxConnsPerTorrent > 0 {
		torrentCfg.EstablishedConnsPerTorrent = cfg.MaxConnsPerTorrent
		// a dial to a dead address holds its slot for the whole dial timeout, so with half as many
		// slots a torrent among many dead addresses took seconds longer to reach its seeders
		torrentCfg.HalfOpenConnsPerTorrent = cfg.MaxConnsPerTorrent
	}

	if cfg.ListenPort != 0 {
		if cfg.ListenPort == -1 {
			torrentCfg.ListenPort = 0
		} else {
			torrentCfg.ListenPort = cfg.ListenPort
		}
	}

	if cfg.IP != "" {
		ip := net.ParseIP(cfg.IP)
		if ip == nil {
			return nil, fmt.Errorf("invalid provided IP: %q", cfg.IP)
		}

		torrentCfg.PublicIp4 = ip
	}

	l := log.Logger.With().Str("component", "torrent-client").Logger()

	tl := tlog.NewLogger()
	tl.SetHandlers(&dlog.Torrent{L: l})
	torrentCfg.Logger = tl

	torrentCfg.ConfigureAnacrolixDhtServer = func(cfg *dht.ServerConfig) {
		cfg.Store = fis
		cfg.Exp = 2 * time.Hour
		cfg.NoSecurity = false
	}

	for _, opt := range opts {
		opt(torrentCfg)
	}

	return torrent.NewClient(torrentCfg)
}
