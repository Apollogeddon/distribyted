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

// NewClient builds the anacrolix/torrent client from distribyted's own
// config. opts, if given, are applied to the underlying torrent.ClientConfig
// after distribyted's own settings and before the client is constructed —
// e.g. for tests that need to set ClientConfig.HTTPDialContext (see
// internal/testenv.ThrottledDialer.HTTPDialContext), which can only be
// configured at construction time, unlike the peer dialer which
// torrent.Client.AddDialer can add afterward. Variadic so every existing
// call site compiles unchanged.
func NewClient(st storage.ClientImpl, fis bep44.Store, cfg *config.TorrentGlobal, id [20]byte, opts ...func(*torrent.ClientConfig)) (*torrent.Client, error) {
	// TODO download and upload limits
	torrentCfg := torrent.NewDefaultClientConfig()
	torrentCfg.Seed = cfg.Seed
	torrentCfg.PeerID = string(id[:])
	torrentCfg.DefaultStorage = st
	torrentCfg.DisableIPv6 = cfg.DisableIPv6
	torrentCfg.DisableTCP = cfg.DisableTCP
	torrentCfg.DisableUTP = cfg.DisableUTP
	torrentCfg.NoDefaultPortForwarding = cfg.DisableUPnP
	torrentCfg.NoDHT = cfg.DisableDHT
	if cfg.MaxConnsPerTorrent > 0 {
		torrentCfg.EstablishedConnsPerTorrent = cfg.MaxConnsPerTorrent
		torrentCfg.HalfOpenConnsPerTorrent = max(1, cfg.MaxConnsPerTorrent/2)
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
