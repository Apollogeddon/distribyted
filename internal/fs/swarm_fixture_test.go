package fs

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	astorage "github.com/anacrolix/torrent/storage"
)

// keepAliveInterval is the test clients' keepalive interval, which also bounds how long a
// peer connection's writer sleeps: anacrolix/torrent v1.61.0's writer can miss the wake-up that
// tells it to send new requests, and then sleeps until the next keepalive (see testenv).
const keepAliveInterval = 100 * time.Millisecond

// fixtureName is the folder the fixture torrent holds, and fixtureFiles its files in order.
const fixtureName = "Fixture album"

var fixtureFiles = []struct {
	name string
	size int
}{
	{"01 - First track.bin", 70_001},
	{"02 - Second track.bin", 16_384},
	{"03 - Third track.bin", 9_999},
}

// fixtureContent is the content of fixtureFiles[i], the same on every run.
func fixtureContent(i int) []byte {
	b := make([]byte, fixtureFiles[i].size)
	r := rand.New(rand.NewPCG(uint64(i), 42))
	for j := range b {
		b[j] = byte(r.Uint32())
	}
	return b
}

// fixtureSwarm is a seeder in this process with the fixture torrent, so tests fetch a
// torrent's metadata and pieces over the wire without the internet.
type fixtureSwarm struct {
	seeder *torrent.Client
	dir    string
	hash   metainfo.Hash
	err    error
}

var (
	fixtureOnce sync.Once
	fixture     fixtureSwarm
)

func startFixtureSwarm() fixtureSwarm {
	dir, err := os.MkdirTemp("", "fs-fixture")
	if err != nil {
		return fixtureSwarm{err: err}
	}
	folder := filepath.Join(dir, fixtureName)
	if err := os.Mkdir(folder, 0o750); err != nil {
		return fixtureSwarm{dir: dir, err: err}
	}
	for i, f := range fixtureFiles {
		if err := os.WriteFile(filepath.Join(folder, f.name), fixtureContent(i), 0o600); err != nil {
			return fixtureSwarm{dir: dir, err: err}
		}
	}
	// small pieces, so a file spans several and the files share some
	info := metainfo.Info{PieceLength: 16 * 1024}
	if err := info.BuildFromFilePath(folder); err != nil {
		return fixtureSwarm{dir: dir, err: err}
	}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info)}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dir
	cfg.DefaultStorage = astorage.NewFileWithCompletion(dir, astorage.NewMapPieceCompletion())
	cfg.Seed = true
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.KeepAliveTimeout = keepAliveInterval
	seeder, err := torrent.NewClient(cfg)
	if err != nil {
		return fixtureSwarm{dir: dir, err: err}
	}
	s := fixtureSwarm{seeder: seeder, dir: dir, hash: mi.HashInfoBytes()}
	t, err := seeder.AddTorrent(&mi)
	if err != nil {
		s.err = err
		return s
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := t.VerifyDataContext(ctx); err != nil {
		s.err = err
		return s
	}
	if t.BytesMissing() != 0 {
		s.err = fmt.Errorf("the fixture seeder has %d of %d pieces", t.Stats().PiecesComplete, t.NumPieces())
	}
	return s
}

func (s fixtureSwarm) close() {
	if s.seeder != nil {
		s.seeder.Close()
	}
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

// addFixture adds the fixture torrent to Cli by its hash alone, with the seeder as its only
// peer, and waits for its metadata.
func addFixture(t *testing.T) *torrent.Torrent {
	t.Helper()
	fixtureOnce.Do(func() { fixture = startFixtureSwarm() })
	if fixture.err != nil {
		t.Fatalf("starting the fixture swarm: %v", fixture.err)
	}
	to, _ := Cli.AddTorrentInfoHash(fixture.hash)
	to.AddClientPeer(fixture.seeder)
	waitForInfo(t, to)
	return to
}
