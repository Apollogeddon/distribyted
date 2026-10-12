package testenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// writerWakeInterval is the test clients' keepalive interval, which also bounds how long a peer
// connection's writer sleeps. anacrolix/torrent v1.61.0's writer can miss the wake-up that tells it
// to send new requests (it takes its wait channel after releasing the client lock) and then
// sleeps until the next keepalive, a minute by default: longer than the tests' read timeouts.
const writerWakeInterval = 100 * time.Millisecond

type Seeder struct {
	client  *torrent.Client
	tmpDir  string
	content *contentStorage
}

func NewSeeder() (*Seeder, error) {
	tmpDir, err := os.MkdirTemp("", "seeder")
	if err != nil {
		return nil, err
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.HeaderObfuscationPolicy.Preferred = false
	cfg.HeaderObfuscationPolicy.RequirePreferred = true
	// served from memory: anacrolix/torrent panics when a storage read for a peer request fails
	// after its torrent closed, as an mmap's does when Stop closes the client mid-request
	content := &contentStorage{}
	cfg.DefaultStorage = content
	cfg.NoUpload = false
	cfg.Seed = true
	cfg.ListenPort = 0 // random port
	cfg.KeepAliveTimeout = writerWakeInterval
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
		client:  client,
		tmpDir:  tmpDir,
		content: content,
	}, nil
}

func (s *Seeder) Stop() {
	if s.client != nil {
		s.client.Close()
	}
	_ = os.RemoveAll(s.tmpDir)
}

func (s *Seeder) AddFile(name string, content []byte, announceURL string) (metainfo.Magnet, error) {
	return s.AddFileWithPieceLength(name, content, announceURL, 256*1024)
}

// AddFileWithPieceLength is AddFile with a caller-chosen piece length.
// Piece length changes the cost profile of reads against an incomplete
// piece (bigger piece = more to hash/re-fetch before a read past it can
// return), so benchmarks that care about that cost need to control it
// directly instead of inheriting AddFile's fixed 256KiB, which is far
// smaller than typical media torrents (often 2-8MiB).
func (s *Seeder) AddFileWithPieceLength(name string, content []byte, announceURL string, pieceLength int64) (metainfo.Magnet, error) {
	path := filepath.Join(s.tmpDir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return metainfo.Magnet{}, err
	}

	// no announce URL: a seeder in a Swarm, which mustn't learn of (and dial) the other
	// peers through a tracker
	var mi metainfo.MetaInfo
	if announceURL != "" {
		mi.AnnounceList = [][]string{{announceURL}}
	}

	info := metainfo.Info{
		PieceLength: pieceLength,
		Name:        name,
	}
	if err := info.BuildFromFilePath(path); err != nil {
		return metainfo.Magnet{}, err
	}

	mi.InfoBytes, _ = bencode.Marshal(info)
	s.content.add(mi.HashInfoBytes(), content)

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

// MetaInfo is the metainfo of a torrent this seeder added, for a Leecher of the same torrent.
func (s *Seeder) MetaInfo(h metainfo.Hash) (metainfo.MetaInfo, bool) {
	t, ok := s.client.Torrent(h)
	if !ok {
		return metainfo.MetaInfo{}, false
	}
	return t.Metainfo(), true
}

// contentStorage is read-only storage holding each torrent's whole content in memory. Its reads
// never fail, even after the client has closed the torrent.
type contentStorage struct {
	torrents sync.Map // metainfo.Hash to []byte
}

func (cs *contentStorage) add(ih metainfo.Hash, content []byte) {
	cs.torrents.Store(ih, bytes.Clone(content))
}

func (cs *contentStorage) OpenTorrent(_ context.Context, _ *metainfo.Info, ih metainfo.Hash) (storage.TorrentImpl, error) {
	v, ok := cs.torrents.Load(ih)
	if !ok {
		return storage.TorrentImpl{}, fmt.Errorf("seeder has no content for %s", ih)
	}
	content := v.([]byte) //nolint:forcetypeassert // only add stores here
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return contentPiece(content[p.Offset() : p.Offset()+p.Length()])
		},
		Close: func() error { return nil },
	}, nil
}

type contentPiece []byte

func (cp contentPiece) ReadAt(b []byte, off int64) (int, error) {
	if off >= int64(len(cp)) {
		return 0, io.EOF
	}
	n := copy(b, cp[off:])
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}

func (contentPiece) WriteAt([]byte, int64) (int, error) {
	return 0, errors.New("seeder storage is read-only")
}

func (contentPiece) MarkComplete() error    { return nil }
func (contentPiece) MarkNotComplete() error { return nil }

func (contentPiece) Completion() storage.Completion {
	return storage.Completion{Ok: true, Complete: true}
}
