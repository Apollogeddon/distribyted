package loader

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// See internal/torrent/main_test.go: zerolog.SetGlobalLevel is process-wide
// but each package compiles its own test binary, so the badger-backed DB's
// log noise (routed through zerolog, see db.go's dlog.Badger) needs this
// package's own copy to be quieted for benchmark output to be readable.
func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	os.Exit(m.Run())
}
