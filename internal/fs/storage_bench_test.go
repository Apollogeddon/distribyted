package fs

import (
	"fmt"
	"testing"
)

// BenchmarkStorageGet_ManyMounts measures path lookup latency as the number
// of registered mounts (routes, plus any nested archive mounts) grows,
// guarding against a regression to worse-than-linear scaling in
// matchFilesystemLocked's longest-match scan (internal/fs/storage.go) — the
// fix for the route-prefix-collision bug (BACKLOG.md) replaced a
// first-match strings.HasPrefix loop with one that scans every mount to
// find the longest match, so this is worth watching as route counts grow.
func BenchmarkStorageGet_ManyMounts(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("mounts=%d", n), func(b *testing.B) {
			s := newStorage(nil)
			for i := 0; i < n; i++ {
				if err := s.AddFS(&DummyFs{}, fmt.Sprintf("/route%d", i)); err != nil {
					b.Fatal(err)
				}
			}
			// Worst case for a linear scan: the last-registered mount, so
			// every other mount gets checked (and rejected) first.
			path := fmt.Sprintf("/route%d/dir/here/file1.txt", n-1)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.Get(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
