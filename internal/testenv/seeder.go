package testenv

import (
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
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
	cfg.DataDir = tmpDir
	cfg.NoUpload = false
	cfg.Seed = true
	cfg.ListenPort = 0 // random port
	cfg.NoDHT = true
	cfg.DisableIPv6 = true
	cfg.DisableTCP = false
	cfg.DisableUTP = true // often causes issues on Windows with many instances

	client, err := torrent.NewClient(cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
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
	os.RemoveAll(s.tmpDir)
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

	t.VerifyData()
	
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
