package torrent

import (
	"context"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"

	"github.com/Apollogeddon/distribyted/internal/fs"
	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

// webseedCount returns how many webseed URLs t declares, or 0 if t doesn't
// expose that (e.g. a test mock). *torrent.Torrent (via TorrentWrapper)
// satisfies this through its Metainfo method; the webseed list is
// populated at AddTorrentOpt time regardless of whether metadata has
// arrived yet, since it comes from the magnet's own ws= params.
func webseedCount(t fs.Torrent) int {
	mt, ok := t.(interface{ Metainfo() metainfo.MetaInfo })
	if !ok {
		return 0
	}
	return len(mt.Metainfo().UrlList)
}

const (
	timingDefaultDeadline = 120 * time.Second
	timingDefaultTick     = time.Second
	slowOpenThreshold     = time.Second
)

// coldStart tracks one torrent's magnet-added -> metadata -> first-peer ->
// first-data timeline. Every field is only ever mutated under Timings.mu.
type coldStart struct {
	hash     string
	route    string
	name     string
	webseeds int
	t        fs.Torrent

	added     time.Time
	gotInfo   time.Time
	firstPeer time.Time
	firstData time.Time
	dataFrom  string

	emitted bool
}

// complete deliberately does not require firstPeer: a torrent served
// entirely by a webseed may never need a peer connection at all, and that
// is success, not an incomplete record stuck waiting on the watchdog.
// first_peer_ms is still reported when known — see emit — it's just not a
// precondition for the "everything relevant happened quickly" verdict.
func (c *coldStart) complete() bool {
	return !c.gotInfo.IsZero() && !c.firstData.IsZero()
}

// coldStartSnapshot is a plain-value copy of a coldStart taken under
// Timings.mu, so the actual log call can run lock-free.
type coldStartSnapshot struct {
	hash, route, name, dataFrom                string
	webseeds                                   int
	metadataMs, firstPeerMs, firstDataMs       int64
	haveMetadata, haveFirstPeer, haveFirstData bool
	complete                                   bool
	missingMetadata, missingPeer, missingData  bool
}

// Timings collects and logs how long each torrent takes to get metadata,
// connect a peer, and receive its first useful byte, plus how long each
// opened file takes to serve its first byte — the production visibility
// that was missing when diagnosing "slow to start streaming" reports (see
// docs/benchmarking.md's "Scoped out: DHT/tracker peer-discovery latency").
//
// It polls Torrent.Stats()/Info() on a fixed interval rather than hooking
// per-chunk/per-connection library callbacks, deliberately: those callbacks
// fire under the anacrolix client's own internal lock, and this package
// just spent a cycle fixing an OOM caused by exactly that kind of coupling
// in the read path (see internal/fs/torrent.go's readAtWrapper doc
// comment). Second-scale polling resolution is more than enough to
// diagnose multi-second cold starts.
type Timings struct {
	log zerolog.Logger

	mu      sync.Mutex
	tracked map[string]*coldStart

	deadline time.Duration
	tick     time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTimings starts the background poller. Every exported method is a
// no-op on a nil *Timings, so no call site needs to nil-check it.
func NewTimings() *Timings {
	return newTimings(timingDefaultDeadline, timingDefaultTick)
}

func newTimings(deadline, tick time.Duration) *Timings {
	ctx, cancel := context.WithCancel(context.Background())
	tm := &Timings{
		log:      dlog.Logger("torrent-timing"),
		tracked:  make(map[string]*coldStart),
		deadline: deadline,
		tick:     tick,
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go tm.run()
	return tm
}

// Added records a torrent being added. hash is the lowercase hex info-hash,
// matching this package's dlog.KeyHash convention elsewhere. A torrent
// already tracked (e.g. re-added from the DB after a restart-time retry) is
// left alone rather than restarting its clock.
func (tm *Timings) Added(hash, route, name string, webseeds int, t fs.Torrent) {
	if tm == nil {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, ok := tm.tracked[hash]; ok {
		return
	}
	tm.tracked[hash] = &coldStart{
		hash: hash, route: route, name: name,
		webseeds: webseeds, t: t, added: time.Now(),
	}
}

// GotInfo records when a torrent's metadata became available. Called
// directly from Service.addTorrent's own GotInfo select, so a fast
// metadata fetch doesn't have to wait for the next poll tick to be noticed.
func (tm *Timings) GotInfo(hash string) {
	if tm == nil {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if c, ok := tm.tracked[hash]; ok && c.gotInfo.IsZero() {
		c.gotInfo = time.Now()
	}
}

// FirstRead logs a one-time, per-file "how long from open to first byte"
// line. Independent of the cold-start record above — a torrent added at
// startup may not be opened for days — so it is not gated on Forget.
func (tm *Timings) FirstRead(hash, path string, sinceOpen time.Duration) {
	if tm == nil {
		return
	}
	ev := tm.log.Info()
	if sinceOpen < slowOpenThreshold {
		ev = tm.log.Debug()
	}
	ev.Str(dlog.KeyHash, hash).
		Str(dlog.KeyPath, path).
		Int64("open_to_first_byte_ms", sinceOpen.Milliseconds()).
		Msg("file first read")
}

// Forget drops a torrent's cold-start record. Must be called whenever a
// torrent is removed (Service.RemoveFromHash) — tracked entries are only
// ever created from Added, so this is what keeps the map bounded to live
// torrents, the same discipline Service.lastHealth already follows.
func (tm *Timings) Forget(hash string) {
	if tm == nil {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.tracked, hash)
}

// Close stops the poller and waits for it to exit. Safe to call once; a
// nil *Timings is a no-op.
func (tm *Timings) Close() {
	if tm == nil {
		return
	}
	tm.cancel()
	<-tm.done
}

func (tm *Timings) run() {
	defer close(tm.done)
	ticker := time.NewTicker(tm.tick)
	defer ticker.Stop()
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.pollOnce()
		}
	}
}

// pollOnce checks every un-emitted entry. It is unexported but deliberately
// callable directly (not just via the ticker) so tests can drive it
// synchronously instead of racing the poller goroutine.
func (tm *Timings) pollOnce() {
	tm.mu.Lock()
	pending := make([]*coldStart, 0, len(tm.tracked))
	for _, c := range tm.tracked {
		if !c.emitted {
			pending = append(pending, c)
		}
	}
	tm.mu.Unlock()

	now := time.Now()
	for _, c := range pending {
		tm.pollEntry(c, now)
	}
}

func (tm *Timings) pollEntry(c *coldStart, now time.Time) {
	// Info()/Stats() take the anacrolix client's own read lock; called here,
	// outside tm.mu, so this goroutine never holds two locks at once.
	info := c.t.Info()
	st := c.t.Stats()

	tm.mu.Lock()
	if c.gotInfo.IsZero() && info != nil {
		c.gotInfo = now
	}
	if c.firstPeer.IsZero() && st.ActivePeers > 0 {
		c.firstPeer = now
	}
	if c.firstData.IsZero() {
		web := st.WebSeeds.BytesReadUsefulData.Int64()
		peer := st.PeerConns.BytesReadUsefulData.Int64()
		switch {
		case web > 0 && peer > 0:
			c.firstData, c.dataFrom = now, "both"
		case web > 0:
			c.firstData, c.dataFrom = now, "webseed"
		case peer > 0:
			c.firstData, c.dataFrom = now, "peer"
		}
	}

	complete := c.complete()
	// >= rather than >: on a clock coarse enough for two back-to-back
	// time.Now() calls to return the same instant (observed on Windows CI
	// runners), now.Sub(c.added) can be exactly 0 — a strict > would then
	// never consider a zero-duration deadline (used by tests to simulate
	// "already overdue on the first poll") actually overdue. >= is also the
	// more correct semantic regardless: the deadline instant itself should
	// count as overdue, not just strictly after it.
	overdue := now.Sub(c.added) >= tm.deadline
	shouldEmit := (complete || overdue) && !c.emitted

	var snap coldStartSnapshot
	if shouldEmit {
		c.emitted = true
		snap = coldStartSnapshot{
			hash: c.hash, route: c.route, name: c.name, webseeds: c.webseeds,
			complete: complete, dataFrom: c.dataFrom,
		}
		if !c.gotInfo.IsZero() {
			snap.haveMetadata = true
			snap.metadataMs = c.gotInfo.Sub(c.added).Milliseconds()
		} else {
			snap.missingMetadata = true
		}
		if !c.firstPeer.IsZero() {
			snap.haveFirstPeer = true
			snap.firstPeerMs = c.firstPeer.Sub(c.added).Milliseconds()
		} else {
			snap.missingPeer = true
		}
		if !c.firstData.IsZero() {
			snap.haveFirstData = true
			snap.firstDataMs = c.firstData.Sub(c.added).Milliseconds()
		} else {
			snap.missingData = true
		}
	}
	tm.mu.Unlock()

	if shouldEmit {
		tm.emit(snap)
	}
}

func (tm *Timings) emit(snap coldStartSnapshot) {
	ev := tm.log.Info()
	if !snap.complete {
		ev = tm.log.Warn()
	}
	ev = ev.Str(dlog.KeyHash, snap.hash).
		Str(dlog.KeyRoute, snap.route).
		Str(dlog.KeyName, snap.name).
		Int("webseeds", snap.webseeds).
		Bool("complete", snap.complete)

	if snap.haveMetadata {
		ev = ev.Int64("metadata_ms", snap.metadataMs)
	}
	if snap.haveFirstPeer {
		ev = ev.Int64("first_peer_ms", snap.firstPeerMs)
	}
	if snap.haveFirstData {
		ev = ev.Int64("first_data_ms", snap.firstDataMs).Str("first_data_src", snap.dataFrom)
	}
	if !snap.complete {
		var missing []string
		if snap.missingMetadata {
			missing = append(missing, "metadata")
		}
		if snap.missingPeer {
			missing = append(missing, "first_peer")
		}
		if snap.missingData {
			missing = append(missing, "first_data")
		}
		ev = ev.Strs("missing", missing)
	}

	ev.Msg("torrent cold start")
}
