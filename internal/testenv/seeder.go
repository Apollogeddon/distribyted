package testenv

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type Seeder struct {
	client *torrent.Client
	tmpDir string
}

func NewSeeder() (*Seeder, error) {
	tmpDir, err := os.MkdirTemp("", "seeder")
	if err != nil {
		return nil, err
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.HeaderObfuscationPolicy.Preferred = false
	cfg.HeaderObfuscationPolicy.RequirePreferred = true
	cfg.DefaultStorage = storage.NewMMap(tmpDir)
	cfg.NoUpload = false
	cfg.Seed = true
	cfg.ListenPort = 0 // random port
	cfg.NoDHT = true
	cfg.DisableIPv6 = true
	cfg.DisableTCP = false
	cfg.DisableUTP = true // often causes issues on Windows with many instances
	cfg.EstablishedConnsPerTorrent = 500
	cfg.HalfOpenConnsPerTorrent = 250
	cfg.HeaderObfuscationPolicy.Preferred = false
	cfg.HeaderObfuscationPolicy.RequirePreferred = true

	client, err := torrent.NewClient(cfg)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}

	return &Seeder{
		client: client,
		tmpDir: tmpDir,
	}, nil
}

func (s *Seeder) Stop() {
	if s.client != nil {
		s.client.Close()
	}
	_ = os.RemoveAll(s.tmpDir)
}

func (s *Seeder) AddFile(name string, content []byte, announceURL string) (metainfo.Magnet, error) {
	path := filepath.Join(s.tmpDir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return metainfo.Magnet{}, err
	}

	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{{announceURL}},
	}

	info := metainfo.Info{
		PieceLength: 256 * 1024,
		Name:        name,
	}
	if err := info.BuildFromFilePath(path); err != nil {
		return metainfo.Magnet{}, err
	}

	mi.InfoBytes, _ = bencode.Marshal(info)

	t, err := s.client.AddTorrent(&mi)
	if err != nil {
		return metainfo.Magnet{}, err
	}

	if err := t.VerifyDataContext(context.Background()); err != nil {
		return metainfo.Magnet{}, err
	}
	// Wait for hash check to finish (simple way)
	for !t.Seeding() && t.Stats().PiecesComplete < t.NumPieces() {
		time.Sleep(100 * time.Millisecond)
	}

	return metainfo.Magnet{
		InfoHash:    t.InfoHash(),
		DisplayName: name,
	}, nil
}

func (s *Seeder) PeerAddr() string {
	addrs := s.client.ListenAddrs()
	if len(addrs) > 0 {
		return addrs[0].String()
	}
	return ""
}
