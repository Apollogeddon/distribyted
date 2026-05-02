package testenv

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	dtorrent "github.com/Apollogeddon/distribyted/torrent"
	"github.com/anacrolix/torrent"
	"github.com/Apollogeddon/distribyted/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_P2P_Fetch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 1. Start Tracker
	tracker := NewTracker()
	err := tracker.Start()
	require.NoError(t, err)
	defer tracker.Stop()

	// 2. Start Seeder
	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 3. Seeder adds a file
	content := []byte("this is a test file for p2p fetching integration test")
	magnet, err := seeder.AddFile("test.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)

	// 4. Register Seeder in Tracker
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// 5. Start Test App (the Leecher)
	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 6. Add Magnet to Leecher
	err = app.Service.AddMagnet("test-route", magnet.String())
	require.NoError(t, err)

	// Proactively add seeder as peer to speed up discovery
	lt, ok := app.Client.Torrent(magnet.InfoHash)
	if ok {
		// Parse seeder addr
		host, port, _ := net.SplitHostPort(seeder.PeerAddr())
		var p uint16
		fmt.Sscanf(port, "%d", &p)
		lt.AddPeers([]torrent.PeerInfo{{
			Addr: &net.TCPAddr{
				IP:   net.ParseIP(host),
				Port: int(p),
			},
		}})
	}

	// 7. Wait for Info and Download
	// We'll try to open the file which should trigger on-demand download
	var file io.ReadCloser
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		f, err := app.FS.Open("/test-route/test.txt")
		if err == nil {
			file = f
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.NotNil(t, file, "Could not open file after timeout")
	defer file.Close()

	// 8. Read and Verify
	downloaded, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, content, downloaded)
}

func TestIntegration_ArchiveTransparency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 1. Start Tracker
	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// 2. Start Seeder
	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

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
	magnet, err := seeder.AddFile("test.zip", zipBuf.Bytes(), tracker.AnnounceURL())
	require.NoError(t, err)

	// 5. Register Seeder in Tracker
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// 6. Start Test App
	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 7. Add Magnet to Leecher
	require.NoError(t, app.Service.AddMagnet("test-route", magnet.String()))

	// Proactively add seeder
	lt, _ := app.Client.Torrent(magnet.InfoHash)
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	fmt.Sscanf(port, "%d", &p)
	lt.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// 8. Wait for and open the inner file
	// The path should be /test-route/test.zip/inner.txt
	var innerFile io.ReadCloser
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		f, err := app.FS.Open("/test-route/test.zip/inner.txt")
		if err == nil {
			innerFile = f
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.NotNil(t, innerFile, "Could not open inner file after timeout")
	defer innerFile.Close()

	// 9. Verify content
	downloaded, err := io.ReadAll(innerFile)
	require.NoError(t, err)
	assert.Equal(t, innerContent, downloaded)
}

func TestIntegration_MultiProtocolConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 1. Start Tracker
	tracker := NewTracker()
	require.NoError(t, tracker.Start())
	defer tracker.Stop()

	// 2. Start Seeder
	seeder, err := NewSeeder()
	require.NoError(t, err)
	defer seeder.Stop()

	// 3. Seeder adds a file
	content := []byte("multi-protocol consistency test data")
	magnet, err := seeder.AddFile("multi.txt", content, tracker.AnnounceURL())
	require.NoError(t, err)

	// 4. Register Seeder
	tracker.RegisterPeer(magnet.InfoHash, seeder.PeerAddr())

	// 5. Start Test App
	app, err := NewTestApp()
	require.NoError(t, err)
	defer app.Close()

	// 6. Add Magnet
	require.NoError(t, app.Service.AddMagnet("test-route", magnet.String()))

	// Proactively add seeder
	lt, _ := app.Client.Torrent(magnet.InfoHash)
	host, port, _ := net.SplitHostPort(seeder.PeerAddr())
	var p uint16
	fmt.Sscanf(port, "%d", &p)
	lt.AddPeers([]torrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: int(p)},
	}})

	// 7. Verify via HTTP FS handler
	// The path should be reachable via http://<app.HttpAddr>/fs/test-route/multi.txt
	// Wait for info
	maxRetries := 30
	var httpResp *http.Response
	for i := 0; i < maxRetries; i++ {
		url := fmt.Sprintf("http://%s/fs/test-route/multi.txt", app.HttpAddr)
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			httpResp = resp
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	require.NotNil(t, httpResp, "Could not fetch via HTTP after timeout")
	defer httpResp.Body.Close()

	downloadedHttp, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, downloadedHttp)

	// 8. Verify via WebDAV
	// Use basic auth
	client := &http.Client{}
	url := fmt.Sprintf("http://%s/test-route/multi.txt", app.WebDavAddr)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.SetBasicAuth(app.Config.WebDAV.User, app.Config.WebDAV.Pass)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
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
	defer os.RemoveAll(tempDir)

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
	defer srv.Close()

	// 4. Wait for initial magnet
	var magnet1 string
	for i := 0; i < 10; i++ {
		info := srv.Info()
		if info.Magnet != "" {
			magnet1 = info.Magnet
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.NotEmpty(t, magnet1, "Initial magnet not generated")

	// 5. Add new file
	file2 := filepath.Join(tempDir, "file2.txt")
	err = os.WriteFile(file2, []byte("content 2"), 0644)
	require.NoError(t, err)

	// 6. Wait for magnet update
	// The server polls every 5 seconds
	var magnet2 string
	for i := 0; i < 15; i++ {
		info := srv.Info()
		if info.Magnet != "" && info.Magnet != magnet1 {
			magnet2 = info.Magnet
			break
		}
		time.Sleep(1 * time.Second)
	}
	assert.NotEmpty(t, magnet2, "Magnet did not update after adding file")
	assert.NotEqual(t, magnet1, magnet2, "Magnet should have changed")
	}
