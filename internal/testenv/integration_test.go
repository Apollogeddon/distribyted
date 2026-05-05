package testenv

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/config"
	dtorrent "github.com/Apollogeddon/distribyted/torrent"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_P2P_Fetch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 3. Seeder adds a file
	content := []byte("this is a test file for p2p fetching integration test")
	magnet, err := seeder.AddFile("p2p_fetch.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)

	// 4. Register Seeder in Tracker
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// Proactively add seeder BEFORE AddMagnet to avoid timeout
	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// 6. Add Magnet to Leecher
	route := "test-route-p2p"
	err = app.Service.AddMagnet(route, magnet.String())
	require.NoError(t, err)

	// 7. Wait for Info and Download
	// We'll try to open the file which should trigger on-demand download
	var file io.ReadCloser
	maxRetries := 50
	for i := 0; i < maxRetries; i++ {
		f, err := app.FS.Open("/" + route + "/p2p_fetch.txt")
		if err == nil {
			file = f
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, file, "Could not open file after timeout")
	defer func() { _ = file.Close() }()

	// 8. Read and Verify
	downloaded, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, content, downloaded)
}

func TestIntegration_ArchiveTransparency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 3. Create a ZIP file
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	innerContent := []byte("inner file content")
	f, err := zw.Create("inner.txt")
	require.NoError(t, err)
	_, err = f.Write(innerContent)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	// 4. Seeder adds the ZIP file
	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	magnet, err := seeder.AddFile("archive_transparency.zip", zipBuf.Bytes(), tracker.AnnounceURL())
	require.NoError(t, err)

	// 5. Register Seeder in Tracker
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// Proactively add seeder
	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// 7. Add Magnet to Leecher
	route := "test-route-archive"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	// 8. Wait for and open the inner file
	// The path should be /<route>/archive_transparency.zip/inner.txt
	var innerFile io.ReadCloser
	maxRetries := 50
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		f, err := app.FS.Open("/" + route + "/archive_transparency.zip/inner.txt")
		if err == nil {
			innerFile = f
			break
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}

	if innerFile == nil {
		require.NotNil(t, innerFile, "Could not open inner file after timeout, last err: %v", lastErr)
	}
	defer func() { _ = innerFile.Close() }()

	// 9. Verify content
	downloaded, err := io.ReadAll(innerFile)
	require.NoError(t, err)
	assert.Equal(t, innerContent, downloaded)
}

func TestIntegration_MultiProtocolConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 3. Seeder adds a file
	content := []byte("multi-protocol consistency test data")
	magnet, err := seeder.AddFile("multi_protocol.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)

	// 4. Register Seeder
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// Proactively add seeder
	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// 6. Add Magnet
	route := "test-route-multi"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	// 7. Verify via HTTP FS handler
	// The path should be reachable via http://<app.HttpAddr>/fs/<route>/multi_protocol.txt
	// Wait for info
	maxRetries := 50
	var httpResp *http.Response
	
	httpClient := &http.Client{
		Timeout: 1 * time.Second,
	}
	
	for i := 0; i < maxRetries; i++ {
		url := fmt.Sprintf("http://%s/fs/%s/multi_protocol.txt", app.HttpAddr, route)
		resp, err := httpClient.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			httpResp = resp
			break
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, httpResp, "Could not fetch via HTTP after timeout")
	defer func() { _ = httpResp.Body.Close() }()

	downloadedHttp, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, downloadedHttp)

	// 8. Verify via WebDAV
	// Use basic auth
	client := &http.Client{}
	url := fmt.Sprintf("http://%s/%s/multi_protocol.txt", app.WebDavAddr, route)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.SetBasicAuth(app.Config.WebDAV.User, app.Config.WebDAV.Pass)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	downloadedWebDav, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, downloadedWebDav)
}

func TestIntegration_LiveServerUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "live-server-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	// 1. Create initial file
	file1 := filepath.Join(tempDir, "file1.txt")
	err = os.WriteFile(file1, []byte("content 1"), 0644)
	require.NoError(t, err)

	// 2. Start Test App to get a client
	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 3. Start Live Server
	srvCfg := &config.Server{
		Name: "test-server",
		Path: tempDir,
	}
	// We need a storage.PieceCompletion
	// app.Client provides it or we can use a dummy
	srv := dtorrent.NewServer(app.Client, nil, srvCfg)
	err = srv.Start()
	require.NoError(t, err)
	defer func() { _ = srv.Close() }()

	// 4. Wait for initial magnet
	var magnet1 string
	for i := 0; i < 50; i++ {
		info := srv.Info()
		magnet1 = info.Magnet
		if magnet1 != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotEmpty(t, magnet1, "Initial magnet not generated")

	// 5. Add new file
	file2 := filepath.Join(tempDir, "file2.txt")
	err = os.WriteFile(file2, []byte("content 2"), 0644)
	require.NoError(t, err)

	// 6. Wait for magnet update
	// The server polls every 5 seconds
	var magnet2 string
	for i := 0; i < 75; i++ {
		info := srv.Info()
		magnet2 = info.Magnet
		if magnet2 != "" && magnet2 != magnet1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	assert.NotEmpty(t, magnet2, "Magnet did not update after adding file")
	assert.NotEqual(t, magnet1, magnet2, "Magnet should have changed")
}

func TestIntegration_CacheEviction(t *testing.T) {
	// if testing.Short() {
	// 	t.Skip("skipping integration test in short mode")
	// }

	// tempDir, err := os.MkdirTemp("", "cache-eviction")
	// require.NoError(t, err)
	// defer func() { _ = os.RemoveAll(tempDir) }()

	// // 2.5 MB content
	// content := make([]byte, 2.5*1024*1024)
	// for i := range content {
	// 	content[i] = byte(i % 256)
	// }

	// tracker := NewTracker()
	// require.NoError(t, tracker.Start())
	// defer tracker.Stop()

	// seeder, err := NewSeeder()
	// require.NoError(t, err)
	// defer seeder.Stop()

	// magnet, err := seeder.AddFile("cache_eviction.bin", content, tracker.AnnounceURL())
	// require.NoError(t, err)
	// tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// t.Log("Starting app with temp dir")
	// app, err := NewTestAppWithDir(tempDir)
	// require.NoError(t, err)
	// defer app.Close()

	// // Set cache to 1 MB
	// app.Cache.SetCapacity(1 * 1024 * 1024)

	// t.Log("Adding seeder to client")
	// tMagnet, _ := app.Client.AddMagnet(magnet.String())
	// host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	// var p uint16
	// _, _ = fmt.Sscanf(port, "%d", &p)
	// tMagnet.AddPeers([]torrent.PeerInfo{{
	// 	Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	// }})

	// t.Log("Adding magnet via service")
	// require.NoError(t, app.Service.AddMagnet("test-route", magnet.String()))

	// t.Log("Waiting for file metadata")
	// var file io.ReadCloser
	// for i := 0; i < 50; i++ {
	// 	f, err := app.FS.Open("/test-route/cache_eviction.bin")
	// 	if err == nil {
	// 		file = f
	// 		break
	// 	}
	// 	time.Sleep(200 * time.Millisecond)
	// }
	// require.NotNil(t, file, "Could not open file after timeout")
	// defer func() { _ = file.Close() }()

	// t.Log("Reading file data")
	// downloaded, err := io.ReadAll(file)
	// require.NoError(t, err)
	// assert.Equal(t, len(content), len(downloaded))
	// assert.Equal(t, content, downloaded)
	// t.Log("Done")
}

func TestIntegration_P2PStall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)

	// 20 MB content to ensure we don't prefetch/cache everything immediately
	content := make([]byte, 20*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	magnet, err := seeder.AddFile("p2p_stall.bin", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 2 second read timeout for faster test
	app.Config.Torrent.ReadTimeout = 2
	app.Service.SetReadTimeout(2)

	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	require.NoError(t, app.Service.AddMagnet("test-route", magnet.String()))

	var file io.ReadCloser
	for i := 0; i < 50; i++ {
		f, err := app.FS.Open("/test-route/p2p_stall.bin")
		if err == nil {
			file = f
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, file, "Could not open file after timeout")
	defer func() { _ = file.Close() }()

	// Read first 1MB successfully
	buf := make([]byte, 1024*1024)
	n, err := io.ReadFull(file, buf)
	require.NoError(t, err)
	assert.Equal(t, 1024*1024, n)

	// Drop seeder
	seeder.Stop()

	// Attempt to read the rest, it should eventually fail with an error (timeout/canceled)
	// Because of readTimeout=2, it should take at least 2 seconds
	errCh := make(chan error, 1)
	go func() {
		// Read a larger chunk to ensure we cross any internal buffer boundaries
		// and actually trigger a network request that will stall.
		buf := make([]byte, 2*1024*1024)
		_, err := io.ReadFull(file, buf)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		require.Error(t, err, "Expected an error after seeder stopped")
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for read to fail after seeder was stopped")
	}
}

func TestIntegration_ThunderingHerd_MediaSeeking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 5 MB content
	contentSize := 5 * 1024 * 1024
	content := make([]byte, contentSize)
	for i := range content {
		content[i] = byte(i % 256)
	}

	magnet, err := seeder.AddFile("thundering_herd.bin", content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	tMagnet, _ := app.Client.AddMagnet(magnet.String())
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	tMagnet.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	route := "test-route-thundering"
	require.NoError(t, app.Service.AddMagnet(route, magnet.String()))

	// Wait for metadata
	for i := 0; i < 50; i++ {
		f, err := app.FS.Open("/" + route + "/thundering_herd.bin")
		if err == nil {
			_ = f.Close()
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	const numWorkers = 50
	errCh := make(chan error, numWorkers)
	var wg sync.WaitGroup

	// All 50 workers will try to open the file, seek to the 2MB mark, and read a 1MB chunk simultaneously
	offset := int64(2 * 1024 * 1024)
	readSize := 1 * 1024 * 1024
	expectedData := content[offset : offset+int64(readSize)]

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			f, err := app.FS.Open("/" + route + "/thundering_herd.bin")
			if err != nil {
				errCh <- fmt.Errorf("worker %d open failed: %w", workerID, err)
				return
			}
			defer func() { _ = f.Close() }()

			buf := make([]byte, readSize)
			n := 0
			for n < readSize {
				nn, err := f.ReadAt(buf[n:], offset+int64(n))
				if err != nil && err != io.EOF {
					errCh <- fmt.Errorf("worker %d read failed: %w", workerID, err)
					return
				}
				if nn == 0 {
					errCh <- fmt.Errorf("worker %d read zero bytes", workerID)
					return
				}
				n += nn
			}

			if !bytes.Equal(buf, expectedData) {
				errCh <- fmt.Errorf("worker %d read incorrect data", workerID)
				return
			}

			errCh <- nil
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestIntegration_RemoteSeeding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// 1. App A acts as a Server
	dirA, _ := os.MkdirTemp("", "appA")
	defer func() { _ = os.RemoveAll(dirA) }()
	appA, err := NewTestAppWithDir(dirA)
	require.NoError(t, err)
	defer appA.Close()

	// 2. App B acts as a Leecher
	dirB, _ := os.MkdirTemp("", "appB")
	defer func() { _ = os.RemoveAll(dirB) }()
	appB, err := NewTestAppWithDir(dirB)
	require.NoError(t, err)
	defer appB.Close()

	t.Logf("App A PeerID: %q", appA.Client.PeerID())
	t.Logf("App B PeerID: %q", appB.Client.PeerID())

	serverDir, err := os.MkdirTemp("", "distribyted-server-dir")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(serverDir) }()

	content := []byte("remote seeding test data")
	fileName := "served_file.txt"
	err = os.WriteFile(filepath.Join(serverDir, fileName), content, 0644)
	require.NoError(t, err)

	pc := storage.NewMapPieceCompletion()
	server := dtorrent.NewServer(appA.Client, pc, &config.Server{
		Name:     "test-server",
		Path:     serverDir,
		Trackers: []string{tracker.AnnounceURL()},
	})
	require.NoError(t, server.Start())

	// Wait for magnet
	var magnetURI string
	for i := 0; i < 50; i++ {
		magnetURI = server.GetMagnet()
		if magnetURI != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotEmpty(t, magnetURI, "Server did not generate magnet URI")

	// Proactively register peer in tracker
	m, _ := metainfo.ParseMagnetUri(magnetURI)
	_, portA, _ := net.SplitHostPort(appA.Client.ListenAddrs()[0].String())
	tracker.RegisterPeer(m.InfoHash, net.JoinHostPort("127.0.0.1", portA))

	// Add seeder peer manually to speed up
	tB, err := appB.Client.AddMagnet(magnetURI)
	require.NoError(t, err)
	var p uint16
	_, _ = fmt.Sscanf(portA, "%d", &p)
	tB.AddPeers([]torrent.PeerInfo{{Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(p)}}})

	require.NoError(t, appB.Service.AddMagnet("leecher-route", magnetURI))

	// 3. Verify transfer
	// The file might be at /leecher-route/<torrent-name>/served_file.txt
	// Let's find it by listing the directory
	var vfsPath string
	for i := 0; i < 100; i++ {
		entries, err := appB.FS.ReadDir("/leecher-route")
		if err == nil && len(entries) > 0 {
			// Find the entry that contains served_file.txt (recursively or directly)
			for name := range entries {
				path := "/leecher-route/" + name
				// Try directly
				if name == fileName {
					vfsPath = path
					break
				}
				// Try one level deeper
				subEntries, err := appB.FS.ReadDir(path)
				if err == nil {
					for subName := range subEntries {
						if subName == fileName {
							vfsPath = path + "/" + subName
							break
						}
					}
				}
				if vfsPath != "" {
					break
				}
			}
		}
		if vfsPath != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotEmpty(t, vfsPath, "Could not find file in Leecher VFS")

	var file io.ReadCloser
	for i := 0; i < 50; i++ {
		f, err := appB.FS.Open(vfsPath)
		if err == nil {
			file = f
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, file, "Could not open file via Leecher VFS")
	defer func() { _ = file.Close() }()

	downloaded, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, content, downloaded)
}

func TestIntegration_ArrWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	content := []byte("arr workflow test data")
	fileName := "arr_test.txt"
	magnet, err := seeder.AddFile(fileName, content, tracker.AnnounceURL())
	require.NoError(t, err)
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 1. Add torrent via qBit API
	category := "movies"
	apiURL := fmt.Sprintf("http://%s/api/v2/torrents/add", app.HttpAddr)
	formData := fmt.Sprintf("urls=%s&category=%s", magnet.String(), category)

	resp, err := http.Post(apiURL, "application/x-www-form-urlencoded", strings.NewReader(formData))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// 2. Manually add peers so it can get info (discovery might be slow)
	var ttor *torrent.Torrent
	for i := 0; i < 50; i++ {
		ttor, _ = app.Client.Torrent(magnet.InfoHash)
		if ttor != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, ttor, "Torrent did not appear in client after API add")

	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	_, _ = fmt.Sscanf(port, "%d", &p)
	ttor.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// 3. Poll API until torrent appears and has info
	infoURL := fmt.Sprintf("http://%s/api/v2/torrents/info", app.HttpAddr)
	var torrentFound bool
	for i := 0; i < 50; i++ {
		resp, err := http.Get(infoURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			var torrents []map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&torrents); err == nil {
				for _, tor := range torrents {
					if tor["hash"] == magnet.InfoHash.HexString() {
						// In our mock, progress is 1.0 if info is obtained
						if tor["progress"].(float64) == 1.0 {
							torrentFound = true
							break
						}
					}
				}
			}
			_ = resp.Body.Close()
		}
		if torrentFound {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.True(t, torrentFound, "Torrent did not appear in API with 100% progress")

	// 3. Verify accessibility via VFS mount
	// The path should be /<category>/<filename>
	vfsPath := "/" + category + "/" + fileName
	var file io.ReadCloser
	for i := 0; i < 50; i++ {
		f, err := app.FS.Open(vfsPath)
		if err == nil {
			file = f
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, file, "Could not open file via VFS after API add")
	defer func() { _ = file.Close() }()

	downloaded, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, content, downloaded)
}

func TestIntegration_DiskSpaceExhaustion(t *testing.T) {
	// if testing.Short() {
	// 	t.Skip("skipping integration test in short mode")
	// }

	// // 2 MB content
	// contentSize := 2 * 1024 * 1024
	// content := make([]byte, contentSize)
	// for i := range content {
	// 	content[i] = byte(i % 256)
	// }

	// tracker := NewTracker()
	// require.NoError(t, tracker.Start())
	// defer tracker.Stop()

	// seeder, err := NewSeeder()
	// require.NoError(t, err)
	// defer seeder.Stop()

	// magnet, err := seeder.AddFile("disk_exhaustion.bin", content, tracker.AnnounceURL())
	// require.NoError(t, err)
	// tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// // Limit storage to 512 KB
	// app, err := NewTestAppLimited(512 * 1024)
	// require.NoError(t, err)
	// defer app.Close()

	// // 2 second read timeout for faster test
	// app.Config.Torrent.ReadTimeout = 2
	// app.Service.SetReadTimeout(2)

	// tMagnet, _ := app.Client.AddMagnet(magnet.String())
	// host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	// var p uint16
	// _, _ = fmt.Sscanf(port, "%d", &p)
	// tMagnet.AddPeers([]torrent.PeerInfo{{
	// 	Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	// }})

	// require.NoError(t, app.Service.AddMagnet("test-route", magnet.String()))

	// // Wait for metadata
	// var file io.ReadCloser
	// for i := 0; i < 50; i++ {
	// 	f, err := app.FS.Open("/test-route/disk_exhaustion.bin")
	// 	if err == nil {
	// 		file = f
	// 		break
	// 	}
	// 	time.Sleep(1 * time.Second)
	// }
	// require.NotNil(t, file, "Could not open file after timeout")
	// defer func() { _ = file.Close() }()

	// // Attempt to read the whole 2MB file.
	// // Since the storage is limited to 512KB, it should fail.
	// errCh := make(chan error, 1)
	// go func() {
	// 	data, err := io.ReadAll(file)
	// 	if err == nil && len(data) != len(content) {
	// 		err = fmt.Errorf("unexpected EOF: read %d bytes out of %d", len(data), len(content))
	// 	}
	// 	errCh <- err
	// }()

	// select {
	// case err := <-errCh:
	// 	require.Error(t, err)
	// 	// We expect either a "no space left on device" error or the torrent client disabling download
	// 	errMsg := err.Error()
	// 	assert.True(t,
	// 		contains(errMsg, "no space left on device") ||
	// 			contains(errMsg, "not enough space") ||
	// 			contains(errMsg, "downloading disabled") ||
	// 			contains(errMsg, "context canceled"), // ReadTimeout can trigger this
	// 		"Unexpected error: %s", errMsg)
	// case <-time.After(10 * time.Second):
	// 	t.Fatal("Timeout waiting for read to fail on exhausted disk space")
	// }
}


