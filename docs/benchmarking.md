# Benchmarking

`make bench` runs distribyted's performance/latency benchmark suite
(`internal/fs`, `internal/http`, `internal/testenv`, `internal/torrent`,
`internal/webdav`). This document covers how to run it for a trustworthy
before/after comparison, what each group actually measures (and doesn't),
and the results of the optimization work this file was written alongside.

## Running a comparison

```
go install golang.org/x/perf/cmd/benchstat@latest

# fast suite (fs/http/torrent/webdav — sub-millisecond to low-millisecond)
go test -p 1 -run='^$' -bench=. -benchmem -count=8 -benchtime=15x \
  ./internal/fs/... ./internal/http/... ./internal/torrent/... ./internal/webdav/... \
  > before-fast.txt

# slow suite (internal/testenv — network-throttled, millisecond to second scale)
go test -p 1 -run='^$' -bench=. -benchmem -count=5 -benchtime=3x ./internal/testenv/... \
  > before-slow.txt

# ... make your change ...

# repeat both captures as after-fast.txt / after-slow.txt, then:
benchstat before-fast.txt after-fast.txt
benchstat before-slow.txt after-slow.txt
```

Notes on the flags:

- **`-p 1`** forces serial package execution. Without it, `go test` can run
  multiple packages' test binaries concurrently, interleaving their stdout
  line-by-line and corrupting benchstat's input (`parsing iteration count:
  invalid syntax`).
- **`-benchtime=Nx`** (a fixed iteration count) rather than a time budget.
  The `internal/testenv` benchmarks are wall-clock dominated by injected
  network latency (satellite profile ≈1.2s/iteration) — a time-based
  `-benchtime` produces a wildly different N run to run, which defeats a
  paired comparison.
- **`-count`** repeats the whole run N times so benchstat has a
  distribution to compare, not one sample per benchmark.

Every benchmark's own doc comment states what it measures and what it's
blind to — read it before trusting a delta.

## Calibrate noise before trusting a result (do this first)

Run the same capture twice with **no code change** between them and
benchstat the two runs against each other:

```
benchstat before-fast.txt before-fast-2.txt
```

Any benchmark that shows a "statistically significant" delta here is too
noisy at this `-count`/`-benchtime` to be evidence — a real change needs to
clear that noise floor, not just show `p < 0.05`.

In this project's sandboxed dev environment, the fast suite is noisy: at
`-count=8 -benchtime=15x`, several sub-microsecond benchmarks
(`StorageGet_ManyMounts`, `RouteForPath`) showed 30-170% stddev and produced
false-positive "significant" deltas of 50-100% in an A/A run. Don't make
accept/reject decisions on those without a much higher `-count`. The
`internal/testenv` suite (millisecond-to-second scale, dominated by real
injected network delay rather than measuring nanosecond-scale code paths)
was far more stable and is what the results below are based on.

## Pass/fail bar for a proposed optimization

- **Accept** if the targeted metric improves by a clear margin above the
  A/A noise floor, and no other benchmark regresses beyond its own noise
  floor.
- **Reject and revert** otherwise — including "it looks like it should
  help" or a change that's directionally positive but within noise. See C2
  below for a worked example.
- **Trade-off** (helps one metric, hurts another): don't keep it silently.
  Gate it behind a config flag defaulting to the safe/current behavior (see
  `responsive_reads`, C1 below), or revert.

## What's NOT covered here

- `docs/baseline.txt`-style raw numbers are deliberately **not** checked
  into the repo. They're machine/kernel/load-specific, go stale silently,
  and `benchstat` is only valid comparing runs from the same machine/session
  — a committed baseline can't be legitimately compared against a
  contributor's own run. Capture your own before/after locally.
- `BenchmarkWebDAVOpenAndRead` (`internal/webdav`) runs against
  `fs.NewMemory()`, not a torrent-backed filesystem — it cannot detect a
  change anywhere in the torrent read path (readahead, responsive reads,
  storage backend). Use `internal/testenv`'s benchmarks for that.
- `NewTestAppProductionStorage` (`internal/testenv/app.go`) — a constructor
  intended to benchmark against distribyted's actual production storage
  backend (`storage.NewResourcePieces`, not the in-memory/FileWithCompletion
  backends the rest of the suite uses) — is currently **unreliable**: a
  manual check reproducibly failed piece hash verification against a
  loopback seeder, with or without a storage capacity func, without
  `-race`. Root cause not identified (suspected read-during-write race in
  the library's `piecePerResource`, since it reproduces fastest on
  loopback). Don't rely on it until that's understood.

## Optimization results (2026-08)

Baseline: `internal/testenv` suite, `-count=5 -benchtime=3x`, this sandbox.

### Accepted: responsive reads (`responsive_reads` config flag, off by default)

`internal/fs/torrent.go`'s `torrentFileHandle.load()` now calls
`torrent.Reader.SetResponsive()` when `config.TorrentGlobal.ResponsiveReads`
is set. Normally a read waits for its covering piece to finish downloading
*and* pass hash verification; responsive mode returns as soon as the
covering chunks have arrived. Off by default because it means bytes can
reach a caller before their integrity has been confirmed — enable it
knowingly.

Time-to-first-byte, `OpenToFirstRead_Throttled_PieceLength` (DSL profile:
40ms RTT, 1MB/s), responsive vs. default:

| Piece length | Default | Responsive | Δ |
| :--- | ---: | ---: | ---: |
| 256KiB | 315.8ms | 107.9ms | **-65.8%** |
| 1024KiB | 1448.4ms | 109.5ms | **-92.4%** |
| 4096KiB | 3868.6ms | 111.1ms | **-97.1%** |

Also improved the three connection-profile benchmarks (cable -24.0%, DSL
-60.7%, satellite -4.6%, all p=0.008). Critically, the two guard metrics
this could plausibly have *regressed* did not: `SequentialThroughput` was
unchanged (708.2ms → 707.0ms, p=0.548) and neither `ConcurrentStreams` size
moved (p=0.690, p=0.421). Accepted.

### Reverted: adaptive readahead via `SetReadaheadFunc`

Tried replacing the flat 4MB `SetReadahead` with a `SetReadaheadFunc`
closure clamping `(current pos - contiguous read start)` between a 4MB
floor and a 32MB ceiling, hypothesizing it would help sustained throughput
on fast connections without hurting cold-start latency (the floor).

Targeted metric (`SequentialThroughput`, cable profile) showed **no
improvement**: 708.2ms → 707.4ms (p=0.548, not significant). Worse, the
untargeted `OpenToFirstRead_Throttled` benchmark **regressed**: cable
+66.6%, DSL +26.6% (both p≤0.016). Root cause: `SetReadaheadFunc` requires
the library to call back into our Go closure on every Read/Seek while the
client-wide lock is held (per the library's own doc comment), instead of
reading a plain static field — and, per `piecesUncached()`'s use of the
returned value for piece-priority scheduling, changed request ordering
under throttled conditions in a way that measurably hurt time-to-first-byte
even though the returned readahead *value* was identical at cold start.

Reverted in full; `internal/fs/torrent.go` still uses the static
`SetReadahead(4MB)`. See the code comment there for the same summary.

### Fixed, not benchmarked: storage capacity not declared to the client

`cmd/distribyted/main.go` builds torrent storage via
`storage.NewResourcePieces(fc.AsResourceProvider())` without ever telling
the torrent client the filecache's capacity — `fc.SetCapacity(...)` is
called separately, later, and the client has no way to know pieces can be
evicted out from under it. This affects the library's piece-eviction/retry
bookkeeping (`hasStorageCap()`, request-order maintenance, the reader's
retry-on-read-failure loop) during long streams on a capacity-limited
cache. Fixed by wiring a `storage.ResourcePiecesOpts.Capacity` func that
reads `fc.Info().Capacity` lazily.

**Not benchmarked**: this needs `NewTestAppProductionStorage` (see above —
currently unreliable) plus a cache-eviction-under-pressure scenario that
wasn't stable enough to land in one attempt. Shipped as a correctness fix
with no performance claim attached; revisit once the storage-backend
reliability issue above is understood.

### Scoped out (not attempted this cycle)

- **`max_conns_per_torrent` (25) vs. the torrent library's own default
  (50)**: plausible lever, but the current benchmark harness has exactly
  one seeder, so raising connection limits is unmeasurable by construction
  — there's nothing for a second connection to accomplish. Documented as a
  known deviation in `docs/configuration.md`; a real test needs a
  multi-seeder harness, which is its own scoped task, not a drive-by.
- **FUSE mount options** (`max_readahead` etc.): the library-level lock
  contention this could address is real (see `internal/fs/torrent.go`'s
  `readAtWrapper` — every read takes the client-wide lock), and fewer,
  larger reads from the FUSE layer would reduce lock round-trips. Not
  attempted: mounting a real FUSE filesystem isn't reliably available in
  this sandboxed dev environment, and shipping an unmeasured mount-option
  change wasn't acceptable.
- **`NoUpload` when not seeding**: anacrolix's own `torrentfs` tool sets
  this "to ensure downloads are responsive". Not attempted: the test
  harness's seeder unchokes unconditionally, so a benchmark here would
  measure nothing real, while the production risk (BitTorrent tit-for-tat
  chokes non-uploading peers in a real swarm) is real and unmeasurable
  locally.
- **DHT/tracker peer-discovery latency**: very plausibly the dominant
  contributor to real-world "slow to start streaming" reports — time to
  find *any* peer, not time-to-first-byte once connected — but there's no
  DHT swarm or public tracker in the loopback test harness, and building
  one is a research project, not a benchmarkable optimization.

  **Update**: this is no longer purely guesswork. `internal/torrent.Timings`
  now logs exactly this — magnet-added → metadata → first-peer → first-data
  → first-read — as a structured `torrent cold start` / `file first read`
  line, in production, on every torrent. And `make probe` (`cmd/probe`) runs
  that same instrumentation against a real magnet over the real internet
  (real DHT, real trackers, no synthetic throttling) from a dev machine —
  no production deploy needed to get *a* real number.

  The caveat that still matters: a dev machine's network conditions aren't
  production's. Production runs behind a VPN (`disable_utp`/`disable_ipv6`
  are set specifically for that — see the deploy config), which changes NAT
  and UDP behaviour in ways a bare dev sandbox doesn't replicate. A `make
  probe` run in a plain container (no VPN, restrictive egress) has reliably
  shown fast metadata (~750ms, via the magnet's `xs=` HTTP source) but zero
  peer connections ever completing out of ~30 discovered candidates within
  150s — a real result, but likely reflecting this sandbox's own outbound
  connectivity rather than anything true of the production host. Treat
  `make probe` output as mechanism validation (proof the instrumentation
  correctly distinguishes "found peers" from "connected to peers") and a
  rough sanity check, not a production stand-in — the actual production
  `torrent cold start` log lines are still the source of truth for whether
  this is worth acting on.
