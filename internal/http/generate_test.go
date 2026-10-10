package http

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTemplVersionsMatch: the generated *_templ.go files call the templ runtime, so the
// generator pinned in tools/templ and the runtime in go.mod have to be the same version.
func TestTemplVersionsMatch(t *testing.T) {
	version := func(file string) string {
		b, err := os.ReadFile(file)
		require.NoError(t, err)
		m := regexp.MustCompile(`(?m)^\s*github\.com/a-h/templ (v\S+)`).FindSubmatch(b)
		require.NotNil(t, m, file)
		return string(m[1])
	}
	require.Equal(t, version("../../tools/templ/go.mod"), version("../../go.mod"))
}
