package testenv

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/require"
)

func TestRegression_VFS_Concurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 1. Create a large-ish file (5MB)
	contentSize := 5 * 1024 * 1024
	content := make([]byte, contentSize)
	for i := range content {
		content[i] = byte(i % 256)
	}

	magnet, err := seeder.AddFile("stress_test.bin", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// Reduce timeout for test
	app.Config.Torrent.AddTimeout = 10
	app.Service.SetAddTimeout(10)

	route := "stress-route"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	// Manually add peers
	var ttor *torrent.Torrent
	for i := 0; i < 50; i++ {
		ttor, _ = app.Client.Torrent(magnet.InfoHash)
		if ttor != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, ttor, "Torrent did not appear in client")

	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	ttor.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// Wait for info
	vfsPath := "/" + route + "/stress_test.bin"
	waitForFile(t, app, vfsPath)

	// 2. Concurrency stress
	const numGoroutines = 20
	const readsPerGoroutine = 50
	const maxReadSize = 128 * 1024 // 128KB

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*readsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			// Each goroutine opens its own handle
			f, err := app.FS.Open(vfsPath)
			if err != nil {
				errCh <- fmt.Errorf("G%d: failed to open: %w", id, err)
				return
			}
			defer func() { _ = f.Close() }()

			r := rand.New(rand.NewSource(int64(id)))

			for j := 0; j < readsPerGoroutine; j++ {
				// Random offset and size
				offset := r.Int63n(int64(contentSize - maxReadSize))
				readSize := r.Intn(maxReadSize) + 1
				
				buf := make([]byte, readSize)
				n := 0
				for n < readSize {
					nn, err := f.ReadAt(buf[n:], offset+int64(n))
					if err != nil && err != io.EOF {
						errCh <- fmt.Errorf("G%d R%d: read failed at %d: %w", id, j, offset+int64(n), err)
						return
					}
					if nn == 0 {
						errCh <- fmt.Errorf("G%d R%d: read zero bytes at %d", id, j, offset+int64(n))
						return
					}
					n += nn
				}

				expected := content[offset : offset+int64(readSize)]
				if !bytes.Equal(buf, expected) {
					errCh <- fmt.Errorf("G%d R%d: data corruption at %d", id, j, offset)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestRegression_ThunderingHerd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 1. Create a file
	content := []byte("thundering herd test data")
	magnet, err := seeder.AddFile("thundering.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	app.Config.Torrent.AddTimeout = 10
	app.Service.SetAddTimeout(10)

	route := "thundering-route"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	var ttor *torrent.Torrent
	for i := 0; i < 50; i++ {
		ttor, _ = app.Client.Torrent(magnet.InfoHash)
		if ttor != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, ttor)

	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	ttor.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)}}})

	vfsPath := "/" + route + "/thundering.txt"
	waitForFile(t, app, vfsPath)

	// 2. Thundering herd: 100 goroutines reading the SAME block
	const numGoroutines = 100
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			f, err := app.FS.Open(vfsPath)
			if err != nil {
				errCh <- fmt.Errorf("G%d: open failed: %w", id, err)
				return
			}
			defer func() { _ = f.Close() }()

			buf := make([]byte, len(content))
			n := 0
			for n < len(content) {
				nn, err := f.ReadAt(buf[n:], int64(n))
				if err != nil && err != io.EOF {
					errCh <- fmt.Errorf("G%d: read failed: %w", id, err)
					return
				}
				if nn == 0 {
					errCh <- fmt.Errorf("G%d: read zero bytes", id)
					return
				}
				n += nn
			}

			if !bytes.Equal(buf, content) {
				errCh <- fmt.Errorf("G%d: data corruption", id)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}
