package loader

import (
	"fmt"
	"testing"
)

// BenchmarkDB_ListLinks measures the cost of listing all persisted links, as
// the number stored grows. This backs /api/links, polled every 2s by the
// dashboard (links.js) for as long as the tab stays open, so its cost scales
// with total links stored, not with anything the user is actively doing.
func BenchmarkDB_ListLinks(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("links=%d", n), func(b *testing.B) {
			db, err := NewDB("") // in-memory
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = db.Close() }()

			for i := 0; i < n; i++ {
				oldPath := fmt.Sprintf("/downloads/file%d.mkv", i)
				newPath := fmt.Sprintf("/library/movie%d.mkv", i)
				if err := db.AddLink(oldPath, newPath); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := db.ListLinks(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
