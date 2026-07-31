package http

import (
	"fmt"
	"testing"
)

// BenchmarkRouteForPath measures the longest-prefix-match cost that resolves
// which route a link belongs to, as the number of routes grows. This backs
// /api/links, polled every 2s by the dashboard (links.js) for as long as
// the tab stays open, so its cost scales with how many routes exist, not
// with anything the user is actively doing.
func BenchmarkRouteForPath(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("routes=%d", n), func(b *testing.B) {
			routeNames := make([]string, n)
			for i := 0; i < n; i++ {
				routeNames[i] = fmt.Sprintf("route%d", i)
			}
			// Worst case for a linear scan: match the last-registered route,
			// so every other route gets checked (and rejected) first.
			p := fmt.Sprintf("/route%d/movie.mkv", n-1)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := routeForPath(p, routeNames); got == "" {
					b.Fatal("expected a route match")
				}
			}
		})
	}
}
