package torrent

import (
	"os"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/config"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "client-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	st := storage.NewFile(tempDir)
	fis, err := NewFileItemStore(tempDir, 1*time.Hour)
	require.NoError(t, err)
	defer fis.Close()

	cfg := &config.TorrentGlobal{
		DisableIPv6: true,
		DisableUTP:  true,
		IP:          "127.0.0.1",
	}
	var id [20]byte
	copy(id[:], "test-peer-id-1234567")

	client, err := NewClient(st, fis, cfg, id)
	require.NoError(t, err)
	defer client.Close()

	require.NotNil(t, client)
}

func TestNewClient_InvalidIP(t *testing.T) {
	cfg := &config.TorrentGlobal{
		IP: "invalid-ip",
	}
	var id [20]byte
	_, err := NewClient(nil, nil, cfg, id)
	require.Error(t, err)
}
