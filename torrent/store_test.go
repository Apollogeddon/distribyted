package torrent

import (
	"os"
	"testing"
	"time"

	"github.com/anacrolix/dht/v2/bep44"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileItemStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "item-store-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	fis, err := NewFileItemStore(tempDir, 1*time.Hour)
	require.NoError(t, err)
	defer func() { _ = fis.Close() }()

	// Create a BEP44 item
	// For simplicity, we'll use a dummy item
	item := &bep44.Item{
		V: []byte("test value"),
	}
	target := item.Target()

	// Put
	err = fis.Put(item)
	require.NoError(t, err)

	// Get
	got, err := fis.Get(target)
	require.NoError(t, err)
	assert.Equal(t, item.V, got.V)

	// Get not found
	var dummyTarget bep44.Target
	copy(dummyTarget[:], "nonexistent")
	_, err = fis.Get(dummyTarget)
	assert.Equal(t, bep44.ErrItemNotFound, err)
}

func TestFileItemStore_Del(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "item-store-del")
	defer func() { _ = os.RemoveAll(tempDir) }()
	fis, _ := NewFileItemStore(tempDir, 1*time.Hour)
	defer func() { _ = fis.Close() }()

	var target bep44.Target
	err := fis.Del(target)
	assert.NoError(t, err) // Del is currently a no-op
}
