package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Apollogeddon/distribyted/internal/config"
	dfs "github.com/Apollogeddon/distribyted/internal/fs"
)

// BenchmarkApiFsListHandler measures /api/fs/*path listing latency as
// directory size grows, guarding against a regression in the per-entry work
// apiFsListHandler does (an IsOwned lookup and a sort per request) — see
// internal/http/api.go.
func BenchmarkApiFsListHandler(b *testing.B) {
	conf := &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true},
	}

	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			entries := make(map[string]dfs.File, n)
			for i := 0; i < n; i++ {
				entries[fmt.Sprintf("file%d.txt", i)] = dfs.NewMemoryFile(nil)
			}
			mockLfs := &mockLinkFs{
				readDirFunc: func(path string) (map[string]dfs.File, error) {
					return entries, nil
				},
				isOwnedFunc: func(path string) bool { return false },
			}

			r, err := NewHandler(nil, nil, nil, nil, nil, nil, "", conf, "", mockLfs)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/api/fs/library", nil)
				r.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatalf("unexpected status: %d", w.Code)
				}
			}
		})
	}
}
