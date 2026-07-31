package testenv

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// Quiet the torrent client/store's debug and info logging (piece-order
// recalculation, badger compaction, etc.) for the whole package's test
// binary. Nothing here asserts on log output, and the noise otherwise
// drowns out benchmark result lines on stdout. Warnings and errors still
// print, so a genuine failure is still visible.
func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	os.Exit(m.Run())
}
