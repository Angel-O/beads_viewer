# Hypothesis Ledger — `bv --robot-triage` slowness

Triangulated via three orthogonal angles per claim (strace / time / source / perf / bench).

```
graph analysis is the bottleneck      : REJECTS — warm FullTriage = 1.26 ms; cold plan = 904 ms with only 2 git calls and a cache hit. Analysis is ~0.04% of triage wall.

correlation git fan-out dominates     : SUPPORTS — strace shows 336 `git show` (168 commits × 2). Removing correlation (plan/insights) drops git execs 340→2 and wall 2.87s→0.9s. /usr/bin/time sys=1.31s matches fork/exec cost.

two git calls per commit are needed   : REJECTS (opportunity) — name-status + numstat over the same SHA can be one `git show --name-status --numstat`, or all commits in ONE `git log --name-status --numstat <range>` (336→1). franken_networkx "batch, don't fan-out" pattern.

disk cache makes repeat calls fast    : REJECTS — the cache READ+REWRITE of a 6.6 MB file (stdlib encoding/json) is itself ~31% CPU / ~0.9s on every call, even on a hit (rewrites whole file to bump LRU AccessedAt under a flock). Self-inflicted floor.

stdlib encoding/json vs goccy         : SUPPORTS — perf shows encoding/json.Unmarshal 13.9% + Encode 13.8% in the cache path; the project already vendors goccy/go-json (used in loader) but the cache uses stdlib.

cache is big because of string maps    : SUPPORTS (contributing) — GraphStats serializes ~12 per-node map[string]float64; 757 nodes → 6.6 MB JSON. int-indexed arrays / compact encoding would shrink this an order of magnitude and speed (de)serialize.

network update check blocks triage    : REJECTS — only 1 bv exec + 340 git; no network execs in strace; update check is detached with timeout.

insights 8.4s = same floor            : REJECTS — insights has only 2 git calls; its 7.5s beyond the floor is exact betweenness O(V·E) + cycles + HITS + eigenvector on 757 nodes (rank 3), a separate target.

it's I/O-wait bound                    : REJECTS — plan user CPU 1046 ms > wall 904 ms ⇒ CPU-bound across cores; nanosleep/futex in `strace -w` are summed-across-threads artifacts, not real waits.
```

## Optimization ordering for extreme-software-optimization (Impact×Confidence/Effort)
1. **Rank 2 first** (disk cache): highest confidence, affects ALL robot commands, low blast radius. Options: don't rewrite on read-hit (separate small LRU sidecar / atomic touch), switch to goccy, compact int-indexed encoding, gzip. Biggest single lever for the whole tool.
2. **Rank 1** (correlation git fan-out): batch 336 git calls → 1 `git log --name-status --numstat`. Triage-specific, large win, medium effort (parse a combined stream — `extractor_snapshot.go` already parses name-status streams).
3. **Rank 3** (insights exact Phase-2): size-gate to approximate betweenness sooner / parallelize Brandes / iterative-convergence HITS+eigenvector (mine franken_networkx techniques).
4. **Rank 4** (string-keyed maps → int-indexed): compounds with #2 (smaller cache) and #3 (faster metric write-back).
5. **Rank 5** (loader allocs + ComputeDataHash): trim 22.8k allocs, incremental/partial hashing.

Guardrails for every round: keep a golden `--robot-triage`/`--robot-plan`/`--robot-insights` JSON, run `go test ./...`, `go vet`, `gofmt -l`, `ubs`, and re-benchmark with hyperfine before claiming a win. One lever per round.

## 2026-08-23 current-campaign negative evidence ledger

Starting revision: `4fc261a1a7f3c885a6a06c272a954d8954978a52`.
The live-profile source snapshot was transferred by RCH as
`beads_viewer/2f64b06266de270c`; SHA-256 receipts for `cmd/bv/main.go`,
`cmd/bv/robot_registry.go`, `pkg/analysis/whatif.go`, and
`.beads/issues.jsonl` matched the local checkout.

| ID | Rejected or deferred claim | Evidence | Retry predicate |
|---|---|---|---|
| NE-20260823-01 | Local wall-clock results are usable. | The Mac reported load averages from 108 to 160 during intake. No local latency number from that window receives credit. | Retry only after fresh `uptime` reports 1-minute load below 10. |
| NE-20260823-02 | The existing real-data `FullAnalysis` and `FastAnalysis` benchmarks measure fresh analysis. | On a pinned RCH worker, `FullAnalysis` reported 0.565-0.599 ms while `FastAnalysis` reported 0.714-0.742 ms. Source inspection shows both enter the five-minute process-wide incremental graph cache after the first iteration. | Add an explicit fresh-analysis benchmark seam that resets or bypasses the incremental cache, then re-run on one worker. |
| NE-20260823-03 | `BenchmarkRobotDiskCache_ReadHit` isolates cache decode. | The benchmark reported 26.5-32.3 ms, 16.3 MB/op, and about 92.5k allocs/op for a 601 KB cache entry, but constructs and hashes a 4,000-issue analyzer inside every timed iteration. | Move analyzer construction and the cache key outside the timed loop or expose an internal read-hit benchmark seam. |
| NE-20260823-04 | Graph metrics remain the dominant warm insights cost. | On the low-load `hz1` snapshot, `--profile-startup --profile-json` measured load 23.76 ms, graph build 0.90 ms, phase 1 0.98 ms, and all phase-2 metrics 8.02 ms. Warm `--robot-insights` was 0.21-0.31 s. | Reconsider only if a larger/dense graph or `--force-full-analysis` makes measured metric time dominate the full command. |
| NE-20260823-05 | The historical 8.4-second insights result is a current baseline. | The prior campaign already identified it as a cold exact-betweenness/cache-contamination artifact; the current 540-issue graph uses approximate betweenness and completes that metric in about 1.3-1.8 ms. | Retry only with the exact historical cache/config/dataset regime and label it cold exact analysis. |
| NE-20260823-06 | The low-load RCH mirror proves triage-history/correlation latency. | RCH intentionally excludes `.git`; `ValidateRepository` therefore skipped history enrichment. Six triage runs at 0.05-0.08 s cover load, analysis, triage, and encoding, not Git correlation. | Profile in a low-load checkout with complete Git history and the same source/data fingerprint. |
| NE-20260823-07 | `--cpu-profile` currently provides a trustworthy robot-command profile. | Profiling starts under a defer in `main`, while registry-backed robot dispatch terminates through `os.Exit`, which skips deferred `pprof.StopCPUProfile`. | Repair and negatively test profile finalization before using this seam for CPU attribution. |
| NE-20260823-08 | Linux `perf` can provide the missing CPU attribution on `hz1`. | `perf_event_paranoid=4` rejected both cycles and recording; the resulting data file was zero bytes. | Retry only with an authorized observability capability or after the Go CPU-profile lifecycle is repaired. |
| NE-20260823-09 | The current analysis disk cache is always a win. | Commit `f9487866` records a 3.1k-issue result of 0.41 s cached versus 0.30 s uncached even after the v3 per-entry redesign. This is commit evidence, not a live reproduction on the present 540-issue dataset. | Reproduce cache-on/cache-off on the same low-load host and representative large graph before changing cache policy. |
| NE-20260823-10 | The historical verification scripts are acceptable gauntlet evidence as-is. | `tests/artifacts/perf/verify.sh` suppresses stderr; `scripts/verify_isomorphic.sh` uses checkout/stash and recursive deletion; `scripts/capture_baseline.sh` targets the obsolete `BEADS_FILE` variable. | Repair a harness under independent review, or use direct non-destructive commands with preserved stderr. |
| NE-20260823-11 | Session-history search found no other failed attempts. | `cass health` reported a stale lexical index last updated 2026-08-18; broad matches were noisy and exact probes were incomplete. | Re-run after a healthy current index; until then, absence claims are forbidden. |

Current positive profile anchors, all on low-load `hz1` unless stated otherwise:

- `--profile-startup`: 540 issues, 685 blocking edges, 32.76 ms including load.
- `--robot-insights`: 0.21-0.31 s over six cold-process/warm-cache runs;
  all reported graph metrics computed, no timeouts.
- RCH `hz2` microbenchmarks: JSONL load 7.67-10.31 ms and 13.12 MB/op;
  graph construction 0.333-0.373 ms and 358 KB/op; TUI layered-1k rebuild
  1.23-1.33 ms; export layered-1k layout 1.26-1.51 ms.
- These anchors rank investigation targets. They are not before/after speedup claims.
