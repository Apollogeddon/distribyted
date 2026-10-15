package fs

import "sync"

// anacrolix/torrent v1.61.0 retries a failed read by recursing with no delay whenever the
// storage has a capacity, as distribyted's does. A read parked waiting for data wakes with
// an error when its torrent is dropped, and then recurses until the stack overflows and the
// process dies. A read readAtWrapper abandoned stays parked in the library, so deleting a
// torrent that wouldn't play reached this. A torrent is therefore dropped only once no
// library read is in flight on it: until then it stays in the client, out of every route,
// and the parked read returns when its data arrives.

var inflight = struct {
	mu    sync.Mutex
	reads map[string]int
	drops map[string]func()
}{reads: make(map[string]int), drops: make(map[string]func())}

func libraryReadStarted(hash string) {
	inflight.mu.Lock()
	defer inflight.mu.Unlock()
	inflight.reads[hash]++
}

func libraryReadEnded(hash string) {
	inflight.mu.Lock()
	inflight.reads[hash]--
	if inflight.reads[hash] > 0 {
		inflight.mu.Unlock()
		return
	}
	delete(inflight.reads, hash)
	drop := inflight.drops[hash]
	delete(inflight.drops, hash)
	inflight.mu.Unlock()
	if drop != nil {
		drop()
	}
}

// DropWhenIdle calls drop, which drops the torrent with this hash from the client, now if no
// library read is in flight on it, or else once the last one returns.
func DropWhenIdle(hash string, drop func()) {
	inflight.mu.Lock()
	if inflight.reads[hash] > 0 {
		inflight.drops[hash] = drop
		inflight.mu.Unlock()
		return
	}
	inflight.mu.Unlock()
	drop()
}

// KeepTorrent cancels a pending DropWhenIdle: the torrent was added again.
func KeepTorrent(hash string) {
	inflight.mu.Lock()
	defer inflight.mu.Unlock()
	delete(inflight.drops, hash)
}

// LibraryReadsInFlight reports how many library reads are in flight on the torrent.
func LibraryReadsInFlight(hash string) int {
	inflight.mu.Lock()
	defer inflight.mu.Unlock()
	return inflight.reads[hash]
}
