package torrent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOrCreatePeerID(t *testing.T) {
	tempDir := t.TempDir()
	idPath := filepath.Join(tempDir, "peer_id")

	// 1. Create new ID
	id1, err := GetOrCreatePeerID(idPath)
	require.NoError(t, err)
	assert.Equal(t, 20, len(id1))

	// 2. Load existing ID
	id2, err := GetOrCreatePeerID(idPath)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
}

func TestGetOrCreatePeerID_InvalidFile(t *testing.T) {
	tempDir := t.TempDir()
	idPath := filepath.Join(tempDir, "peer_id")

	// Create a file with invalid length
	err := os.WriteFile(idPath, []byte("too short"), 0644)
	require.NoError(t, err)

	// Should create a new one and overwrite
	id, err := GetOrCreatePeerID(idPath)
	require.NoError(t, err)
	assert.Equal(t, 20, len(id))
	assert.NotEmpty(t, id)
}
