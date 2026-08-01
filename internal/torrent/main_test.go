package torrent

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// zerolog.SetGlobalLevel is process-wide, but each Go package compiles its
// own test binary, so this only affects this package's tests — a sibling
// package (e.g. internal/torrent/loader) needs its own copy. Nothing here
// asserts on log output, and the noise otherwise drowns out benchmark
// result lines on stdout. Warnings and errors still print.
func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	os.Exit(m.Run())
}
