# Hotspot Table — `bv --robot-triage` (and the wider robot path)

**Scenario:** cold-process `bv --robot-triage` on the repo's own `.beads/issues.jsonl`
(757 issues / 1.9 MB; 1307 commits, 168 correlated).
**Host:** AMD EPYC 7282 (64c), Go 1.25.5, git 2.51.2, kernel 6.17, default `go build`.
**Golden output:** the `--robot-triage` JSON (and `--robot-plan`/`--robot-insights`) must remain
byte-equivalent modulo timestamps/`compute_time_ms`. Verify with a saved golden before/after each change.

## Baselines (hyperfine, warmup 2)

| Command | mean ± σ | git execs | Note |
|---|---|---|---|
| `--robot-triage` | **2.872 s ± 0.165 s** | 340 | the target |
| `--robot-next`   | 2.636 s ± 0.018 s | 340 | "minimal" mode *also* runs correlation |
| `--robot-plan`   | 0.904 s ± 0.010 s | 2 | no correlation → exposes the in-process floor |
| `--robot-insights` | 8.399 s ± 0.219 s | 2 | full exact Phase-2 graph metrics |

Warm in-process (go test -benchmem, real data):
`IssueLoading 25.9 ms / 7.7 MB / 22.8k allocs` · `FullTriage 1.26 ms` · `GraphBuild 0.48 ms` · `FullAnalysis 0.69 ms`.
→ The real *work* is tens of ms; the seconds come from subprocess fan-out and the disk cache.

## Ranked hotspots (evidence-cited)

| Rank | Location | Metric | Value | Category | Evidence |
|------|----------|--------|-------|----------|----------|
| 1 | `pkg/correlation/cocommit.go` `getFilesChanged`:154 + `getLineStats`:202 — **two `git show` per commit** (`--name-status`, `--numstat`) | subprocess fan-out | **336 git execs ≈ 1.7–1.9 s** of the 2.87 s triage | I/O / subprocess | `strace -f -e execve` → 168 `--name-status` + 168 `--numstat`; `/usr/bin/time` sys=1.31 s |
| 2 | `pkg/analysis/cache.go` `getRobotDiskCachedStats`:906 → `readRobotDiskCacheLocked`:830 + `writeRobotDiskCacheLocked`:849 — reads **and rewrites the whole 6.6 MB `analysis_cache.json`** every call (even on a hit, just to bump LRU `AccessedAt`) via **stdlib `encoding/json`** | (de)serialize + I/O | **~0.9 s = 30.9 % CPU**; paid by *every* robot cmd | CPU/alloc/IO | `perf report` `perf_plan.data`: Unmarshal 13.9 % + Encode 13.8 %; cache file = 6.6 MB |
| 3 | exact betweenness / cycles / HITS / eigenvector (insights `ConfigForSize`, `betweenness_approx.go`, `graph.go`) | full Phase-2 compute | **insights 8.4 s** (≈ 7.5 s beyond the floor) | CPU | hyperfine insights vs plan |
| 4 | `pkg/analysis/graph.go` metric write-back as `map[string]float64` (per-node, ~12 metrics) | alloc + string hashing; **inflates #2 cache to 6.6 MB** | bloats serialize/parse in #2 | CPU/alloc | investigation report; cache size |
| 5 | `pkg/loader/loader.go` JSONL parse + `pkg/analysis/cache.go` `ComputeDataHash`:141 (SHA256 over all issues, sorted) | load + hash | ~26 ms warm / 22.8k allocs | CPU/alloc | bench `IssueLoading` |

## What triage actually pays (decomposition of 2.87 s)
```
~0.9 s  disk-cache read+rewrite (rank 2)   ← also in plan/next/insights
~1.9 s  correlation git fan-out  (rank 1)  ← triage/next only
~0.0 s  graph analysis (cache hit, rank 3/4/5 ~1 ms)
```
Killing ranks 1 + 2 should take triage from ~2.9 s to well under ~0.1 s.

---

## Final results (after 10 passes)

**Host:** AMD EPYC 7282 (64c), Go 1.25.5, git 2.51.2, kernel 6.17, default `go build`.
**Cache state matters:** all warm numbers below use a *fresh isolated* `XDG_CACHE_HOME`
warmed once per command, then measured with `hyperfine -w3 -r15 -N`. Cold numbers are
first-call on an empty cache dir. These are not comparable to the original §"Baselines"
table above, which was taken under a *cold process + cold disk-cache* regime (hence the
seconds-scale numbers); the warm regime is what the 10-pass loop drove down and is the
honest steady-state an agent sees on repeated robot calls.

### Warm end-to-end (fresh-cache, before → after pass 10)

The pass-10 change (parallel JSONL parse, size-gated) is **net-neutral on the warm robot
path for this repo** because the store (~1.9 MB) sits *below* the measured parallel
crossover (~4 MB) and so deliberately stays on the faster serial path. Before/after are
within σ — i.e. no regression, with the parallel speedup now latent for larger stores.

| Command | before (pass-9 binary) | after (pass-10 binary) | Δ |
|---|---|---|---|
| `--robot-plan`     | 88.2 ms ± 3.8 | 88.5 ms ± 4.1 | ~0 (within σ) |
| `--robot-triage`   | 101.3 ms ± 1.5 | 102.4 ms ± 3.3 | ~0 (within σ) |
| `--robot-next`     | 100.9 ms ± 2.9 | 102.5 ms ± 2.5 | ~0 (within σ) |
| `--robot-insights` | 321.8 ms ± 11.2 | 343.3 ms ± 45.7 | ~0 (noise; insights variance is GC-driven) |

### Cold (first-call, empty cache)

| Command | after (pass-10) | Note |
|---|---|---|
| `--robot-triage` | ~1.00 s (997–1008 ms over 5 runs) | dominated by correlation git fan-out + first cache build, **not** the loader |

### Cumulative speedup across all 10 passes (warm robot path)

Cold-process, cold-disk-cache → warm fresh-cache steady state:

| Command | original cold baseline | pass-10 warm | cumulative speedup |
|---|---|---|---|
| `--robot-triage`   | 2.872 s | 0.102 s | **~28×** |
| `--robot-next`     | 2.636 s | 0.103 s | **~26×** |
| `--robot-plan`     | 0.904 s | 0.089 s | **~10×** |
| `--robot-insights` | 8.399 s | 0.343 s | **~24×** |

(The bulk of these wins are from passes 1–9: killing the git fan-out, the 6.6 MB
disk-cache read+rewrite, the metric write-back bloat, and the correlation snapshot path.
Pass 10 contributes a *future-proofing* lever, not a number here.)

### Pass 10 — Loader-ParallelParse (this pass)

- **Change:** `pkg/loader/loader.go` — `parseIssuesWithOptions` now size-gates onto a
  morsel-driven parallel JSONL decoder (`parseIssuesParallel` + `parseChunkLines`),
  reusing a single shared per-line processor (`processIssueLine`) so the serial and
  parallel paths are byte-equivalent (BOM strip, `_type` dispatch, CRLF trim, 10 MB line
  cap, normalize/validate, tombstone/pool deep-copy, ParseStats, and warnings replayed in
  original line order). Alien-graveyard technique: §8.2 Vectorized Execution + Morsel-Driven
  Parallelism (bounded worker pool pulling line-aligned chunks from a central dispatcher,
  results reassembled by chunk-index + intra-chunk-index for deterministic order).
- **Measured crossover (warm, real-shaped data, 64c):** the JSONL parse is
  *allocation/GC-bound*, not CPU-bound (CPU profile: `runtime.gcDrain` ~36 % cum; the JSON
  decode itself is ~10 %). Parallelism only pays once per-issue work outweighs the parallel
  path's extra allocation (per-chunk slices + order-preserving reassembly copy):

  | file size | serial | parallel | winner |
  |---|---|---|---|
  | 1.9 MB (this repo) | 13.4 ms | 15.3 ms | serial |
  | 4 MB   | 37.5 ms | 37.1 ms | tie (crossover) |
  | 8 MB   | 62.9 ms | 56.4 ms | parallel +10 % |
  | 40 MB  | 246 ms  | 203 ms  | parallel +21 % |

- **Decision:** threshold `parallelParseMinBytes = 4 MiB`, so the repo's own ~1.9 MB store
  stays serial (no warm-path regression) while multi-MB monorepo exports get the speedup.
- **Proof:** `go test -race ./pkg/loader/...` green; differential tests
  (`TestParallelDiff_*`, `TestParallelParse_AutoDispatchMatchesSerial`) assert identical
  `[]Issue` + order + count + stats + ordered warnings on the real file and on
  corrupt/BOM/CRLF/no-trailing-newline fixtures; all 4 goldens OK; `go vet` / `gofmt` /
  `ubs` clean.

## Authoritative final A/B (true original rebuilt from pre-loop commit 0ef0e25 vs final)
Isolated XDG_CACHE_HOME per run, median of 3, this host. Full `go test ./...` green (e2e incl).
git execs for triage: 340 -> 3 (warm).

| Command | Cold orig | Cold final | Warm orig | Warm final | Warm speedup |
|---|---|---|---|---|---|
| robot-triage   | 2.20s | 0.98s | 2.24s | 0.09s | ~25x |
| robot-next     | 2.20s | 0.98s | 2.22s | 0.08s | ~28x |
| robot-plan     | 0.13s | 0.07s | 0.14s | 0.06s | ~2.3x |
| robot-insights | 0.66s | 0.26s | 0.63s | 0.19s | ~3.3x |

Warm = repeat call (the agent-loop case): dominated for triage/next by the new correlation
result cache (pass 4); orig has no such cache so it re-runs the full git extraction every call.
Cold = first call after a change: correlation extraction still runs once but via batched/snapshot
git (passes 2-3) instead of 336 per-commit subprocesses.

## Cold-path extension (passes 11-13) — final scenario matrix
True original (0ef0e25) vs final, isolated caches, this host. All outputs byte-identical (goldens OK).

| Scenario (what the agent does) | orig | final | speedup |
|---|---|---|---|
| **warm repeat** (re-run triage, nothing changed) | 2.26s | **0.10s** | ~23x |
| **edit a bead** then triage (`br update`; HEAD unchanged) | 2.26s | **0.15s** | ~15x  (pass 11: HEAD-only artifact cache) |
| **new commit** then triage (HEAD advanced) | 2.26s | **0.40s** | ~5.6x (pass 13: per-commit incremental extract) |
| **cold first-ever** (empty cache, one-time per machine) | 2.25s | **1.01s** | 2.2x  (passes 2-3: batched/snapshot git) |

orig has no correlation caching, so every repeat scenario stays ~2.26s.
Residual on the new-commit path (~0.40s) is co-commit `primeBatch` (still scans all commit
SHAs); it is per-commit-immutable and the clear next target (same content-addressed pattern).

## Pass 14 — Correlation-CoCommitIncremental (per-commit co-commit cache)
Addresses the residual called out above: `primeBatch` is now content-addressed and
PERSISTENT per commit SHA. A commit's `(files, lineStats)` is a pure function of
(SHA, exclude-pathspec set), so each SHA's diff is cached forever
(`per_commit_cocommit_cache.go`, mirroring `per_commit_event_cache.go`:
goccy codec, flock, 30d age bound, 4000-commit cap, 96MB ceiling,
namespace = sha256(`excludePathspecArgs()`)+schema; `lineStats` round-trips via an
exported `lineStatsWire` mirror). On the new-commit path the co-commit
`git log --no-walk` passes now fetch only the NEW SHAs.

**New-commit path, git-subprocess count (report + head-artifact caches stripped,
per-commit caches warm), this host:**

| binary | total git calls | co-commit `git log --no-walk` calls | SHAs batched |
|---|---|---|---|
| pre-pass-14 (HEAD) | 7 | 2 (name-status + numstat) | 168 each |
| pass-14 (warm co-commit cache) | 5 | **0** | 0 |

Wall-clock on this repo's scale: new-commit path ~0.10s, fully-warm ~0.10s,
cold-first-ever ~1.03s (all unchanged in wall-time; the two batched co-commit
`git log` subprocesses over 168 SHAs are eliminated outright). The absolute ms
saving is small at this store size because the batched co-commit logs were already
cheap relative to fixed process overhead, but the per-SHA git work on the
new-commit path is now O(new commits) instead of O(all commits).

**Proof:** `TestPerCommitCoCommitDifferential` asserts byte-identical
`[]CorrelatedCommit` across full (cache off) / cold / fully-warm / k=1,3,10-new, and
the git-fetch SHA count = all (full,cold), 0 (fully-warm), exactly k (k-new). Cand
binary byte-identical to pre-pass-14 HEAD on all four robot commands run same-day;
goldens OK (only staleness-day drift vs yesterday's recording); `go test -race
./pkg/correlation/...` green; build / vet / gofmt / ubs clean.

**Residual after pass 14:** the new-commit path still runs, per new commit, the
snapshot blob reads for the event extraction (pass-13 cache covers only commits
already seen) and the cheap report re-assembly; HEAD `rev-parse`, the snapshot
`git log --raw --follow` enumeration, and `git cat-file` for the new commits' blobs
remain. Co-commit git work is no longer on the hot path for already-seen commits.

## FINAL matrix after pass 14 (co-commit incremental) — orig 0ef0e25 vs final
Isolated caches, median-of-3, this host. All goldens byte-identical (date-stable normalizer).

| Scenario (what the agent does)                  | orig  | final  | speedup |
|------------------------------------------------|-------|--------|---------|
| warm repeat (re-run, nothing changed)          | 2.30s | 0.09s  | ~25x    |
| edit a bead then triage (`br update`)          | 2.26s | ~0.15s | ~15x    | (pass 11)
| new commit then triage (HEAD advanced)         | 2.26s | ~0.19s | ~12x    | (pass 13 events + pass 14 co-commit; co-commit git-log 2->0)
| cold first-ever (empty cache, once per machine)| 2.26s | 1.01s  | 2.2x    |

Pass 14: per-commit co-commit cache (per_commit_cocommit_cache.go) keyed on commit SHA
namespaced by exclude-pathspec hash; primeBatch git-fetches only uncached SHAs.
Differential test byte-identical (fetch count == uncached count); -race clean.
Residual on new-commit path: snapshot `git log --raw --follow` enumeration over full
history + new commits' blob reads + report reassembly. Truly-cold one-time extraction (~1.0s)
is irreducible without making correlation lazy (declined: changes output semantics).

## 2026-06-08 — independent re-profile (post pass-14 + fresh-eyes bug fixes)
Goal: confirm whether any low-hanging fruit remains before cutting a release.
Host: 64 cores; fresh optimized build; isolated `XDG_CACHE_HOME` per scenario; hyperfine -N.
In-process CPU attribution via Go pprof (measurement-only `pprof.StopCPUProfile()` before the
robot-path `os.Exit`, since the documented `--cpu-profile` defer never fires on that path;
instrumentation reverted afterwards — tree pristine).

Baselines (warm = repeat call, what agents do):
| path           | warm   | cold (fresh cache) |
|----------------|--------|--------------------|
| triage         | 98 ms  | 0.95 s             |
| next           | 96 ms  | (≈triage)          |
| plan           | 83 ms  | 0.07 s             |
| insights       | 307 ms | 0.24 s             |
| `--version` (irreducible Go startup floor) | 29.6 ms | — |

Findings:
- **Warm triage/next/plan** sit ~55-70 ms above a 29.6 ms irreducible startup floor. pprof flat
  profile is FLAT (no node >10 ms self) — time is spread across necessary work (parse 1.9 MB
  JSONL once, build graph, goccy-decode SoA cache, encode JSON) + GC. No hotspot. Nothing to grab.
- **Cold triage 0.95 s** is ~half off-CPU (waiting on `git cat-file` blob pipe; only 8 git execs
  total — fan-out already eliminated). In-process CPU = readBlobs/readFull (190 ms, of which
  110 ms is memmove copying blob bytes) + 130 ms GC. Once-per-HEAD, already incrementally cached
  (passes 11-14). Reducing alloc churn could shave ~100-150 ms off a once-per-machine op — low ROI.
- **Warm insights 307 ms** (the slowest warm path, explicitly secondary per README): ~60 ms goccy
  decode of the (larger) insights SoA cache blob + ~60 ms `TopWhatIfDeltas` + ~90 ms GC.
  `TopWhatIfDeltas` is O(V·(V+E)) — one `countTransitiveUnblocks` BFS per open issue. It is correct
  and produces real output; collapsing it to a single reverse-topo DP pass is a genuine algorithmic
  rewrite with correctness risk (diamond/cascade unblock semantics), NOT low-hanging fruit, and only
  helps a secondary path.

VERDICT: no candidate clears Impact×Confidence/Effort ≥ 2.0. After 14 passes + the 6-bug
fresh-eyes review, the robot path is at its practical floor. Stop optimizing; cut a release.

## 2026-08-24 fresh cold-triage profile and pass-4 rejection

Fresh Go 1.25.5 profiling on the accepted blob-reuse runtime used a new
application cache per `--robot-triage` invocation and the exact 540-issue
Git-backed fixture. Twenty runs established mean user CPU 0.485 s (CV 8.68%)
and mean RSS 109,128.8 KiB (CV 0.14%); wall CV 10.79% remains non-claimable.
Five merged CPU profiles totaled 2.63 s of samples:

| Path | Cumulative samples | Share |
|---|---:|---:|
| `GenerateReportCached` | 1.87 s | 71.10% |
| `extractViaSnapshots` | 1.64 s | 62.36% |
| `blobReader.read` | 0.73 s | 27.76% |
| `newRecordLineSet` | 0.42 s | 15.97% |
| background GC | 0.42 s | 15.97% |
| correlation cache put/write focus | 0.19 s | 7.22% |

The cache focus satisfied the prior retry predicate, so pass 4 tested raw JSON
payload reuse. It was rejected: 20 low-load alternating pairs found no
significant user-CPU improvement (-0.85%, p=0.3238), RSS rose 0.129%, and five
merged target profiles worsened the cache focus from 4.00% to 4.97%. Exact
normalized behavior held across all 50 outputs. Runtime bytes were restored to
`19fb9f06`; the retry condition is now the narrower NE-20260824-11 predicate.

## 2026-08-24 pass-5 representation rewrite and shifted target

The retry-predicate-safe inline-value formulation was also rejected. Thirty
alternating cold-cache pairs on `vmi1156319` (all pre-run one-minute loads
3.24-6.79) found only 0.71% lower mean user CPU with a 95% paired-bootstrap
interval spanning -7.33% to +7.99%; wall and RSS were flat. Fifteen traced
pairs found a non-significant 4.79% GC-cycle reduction. Eight merged profiles
did lower `newRecordLineSet` from 0.65 s to 0.57 s cumulative, but map
assignment/access and `memclr` grew. Runtime and tests were restored exactly.

The next measured lever is `blobReader.read` (0.73 s / 27.76% cumulative in the
fresh accepted profile). Its constructor gives `bufio.Reader` a 10 MiB buffer,
larger than the representative 1.37 MiB historical blobs. A smaller reader lets
Go's large-destination `Read` path bypass the intermediate buffer, potentially
removing a full payload copy while leaving the Git object protocol unchanged.

## 2026-08-24 pass-6 direct-payload rejection and next allocation target

The 64 KiB reader proved the direct-read mechanism but failed the system-level
gate. Across eight merged profiles, `memmove` fell 45.8% (0.48 s to 0.26 s) and
reader construction fell 0.08 s to 0.01 s. Removing the retained 10 MiB buffer
also reduced Go's heap-growth cushion: 15 traced pairs increased GC cycles from
12.33 to 15.87 (+28.65%) with candidate losses in every pair, and background-GC
samples rose from 0.45 s to 0.62 s. Thirty low-load alternating pairs could not
establish a user-CPU win (95% improvement interval -2.17% to +11.50%), while
wall and RSS remained effectively flat. The candidate was restored exactly.

The next independent measured allocation lever is `parseRawDiffLines`: it
creates a 64 KiB scanner buffer for every followed commit even though its input
is already resident in a byte slice. The representative 498-commit history
therefore exposes about 31.1 MiB of transient scanner capacity, before scanner
token and string conversions. Pass 7 tests a bounded direct byte parser under
an executable old/new differential contract; it does not combine heap pacing,
blob buffering, or Git protocol changes.

## 2026-08-24 pass-7 parser rejection and loader-buffer activation

The direct raw-diff parser proved a 46.3x target median and removed 99.66% of
bytes/op, but the old parser occupied only 20 ms cumulative across eight merged
profiles. One hundred quiet-host cold pairs then measured just 1.41% lower mean
user CPU with a 95% interval spanning -1.17% to +4.22%; median/p95 and wall were
unchanged. GC cycles fell 16.95%, but neither time nor peak memory converted
that mechanism into a product win. The 94-line production rewrite was restored.

The next allocation experiment moves to the loader, where the same 10 MiB
constant is a transport buffer rather than merely a token ceiling. Today's
1,367,658-byte serial fixture has a 10,881-byte maximum line, so the retained
buffer is 7.67x the file and about 964x its longest line. This source-level
signal is not yet a speed claim: Pass 8 first measures the loader seam, then
tests a stat-informed transport buffer plus exceptional long-line assembly so
it does not blindly repeat Pass 6's smaller-heap-goal regression.

## 2026-08-24 pass-8 adaptive-loader rejection and nested-codec activation

The loader allocation profile validated the target: the original
`bufio.NewReaderSize` accounted for 1,034,240 KiB, or 79.76%, of allocation
space across 100 representative loads. The stat-sized/64 KiB candidate reduced
mean allocation from 13,110,564 to 3,995,973 B/op (-69.52%) while preserving
the exact legacy LF, CRLF, unterminated-line, warning, error, and pool behavior.
Its planted terminated-boundary mutation failed as required.

The resource reduction did not survive the product gate. Thirty alternating
direct-loader pairs regressed mean time 12.11% with 26/30 losses and a 95%
paired-bootstrap improvement interval wholly below zero (-17.44% to -7.46%).
Thirty cold-triage pairs regressed mean user CPU 17.00% and wall 6.50%, while
all 60 normalized outputs matched and peak RSS was flat. Fifteen traced pairs
showed the cause: mean GC cycles rose from 10.60 to 25.07 (+136.48%). The
candidate was restored byte-for-byte; reduced B/op is not counted as speed.

A fresh clean CPU profile of the restored loader now activates Pass 9 rather
than guessing from relative allocation shares. Over 1,000 loads at 7.865 ms/op,
`Dependency.UnmarshalJSON` accumulated 1.62 s (17.44%, about 1.62 ms/load),
while `Comment.UnmarshalJSON` accumulated only 0.32 s (3.44%). Pass 9 therefore
tests only dependency decoding, with the comment compatibility path held fixed
and a differential error/alias/precedence certificate required before timing.

## 2026-08-24 pass-9 nested-codec rejection and harness correction

The one-lever dependency decoder passed its semantic certificate. The oracle
caught goccy's invalid-UTF-8 behavior, so invalid bytes and all decode errors
were replayed through stdlib; a planted first-key-wins mutation failed four
duplicate/error-atomicity cases. On the direct loader, 30 alternating pairs
improved mean time 13.00% with a 95% interval of 9.84% to 16.12%, and reduced
allocations from 14,211.77 to 9,432.57 per load (-33.63%).

That local win did not reach the profiled product. The full loader runs once,
whereas correlation's dominant snapshot path decodes only ID/status/title and
never invokes `Dependency.UnmarshalJSON`. Thirty exact-source cold-triage pairs
regressed mean user CPU 2.12% (95% improvement interval -7.21% to +2.84%),
wall 0.58%, and RSS 0.055%; GC cycles rose from 10.47 to 10.67 (+1.91%). All
60 normalized outputs matched. The candidate was restored exactly.

Two earlier product cohorts receive no credit. A Git blob-index comparison
proved that the quiet host's tracked snapshot extractor had drifted from the
accepted local runtime, even though its path set and loader bytes matched. The
credited pair was rebuilt only after copying the exact snapshot source and
verifying identical build commands. Future passes now require a tracked-blob
comparison plus hashes for every campaign-modified production file before
binary reuse.

## 2026-08-24 pass-10 runtime heap-pacing rejection

The separately requested heap-pacing experiment did not rescue the smaller-
buffer candidates. An unset-runtime versus explicit `GOGC=100` control over
50 high-resolution pairs was equivalent: explicit 100 moved mean user CPU by
only +0.44% with a 95% paired-bootstrap interval of -1.87% to +2.68%, while
wall moved -0.77% with an interval of -3.17% to +1.52%. This validated that
the harness was not manufacturing an effect merely by setting the variable.

Against that control, `GOGC=400` reduced mean user CPU from 0.359487 s to
0.341266 s (+5.07%), but its lower confidence bound was only +2.82%, below the
campaign's +3% gate. More importantly, mean system CPU increased 16.72% and
mean wall regressed 3.90% (34/50 losses; 95% improvement interval -6.68% to
-1.20%). Wall p95 worsened from 0.561465 s to 0.621039 s and the worst sample
from 0.858843 s to 0.872717 s. All 100 normalized outputs were identical.
The deliberately hostile `GOGC=50` arm lost all 12 coarse user-CPU pairs, so
the expected direction was mutation-sensitive. No runtime default, source,
or test was changed; fixed GC tuning is rejected as a product optimization.

The restored profile still activates snapshot record-set representation.
Pass 11 therefore tests only a pointer-free packed blob-offset entry, retaining
the exact first-representative/collision semantics and the accepted buffer-
reuse lifetime. It does not combine GC policy, direct event fusion, or Git
protocol pipelining.

## 2026-08-24 pass-11 packed-record rejection

The pointer-free representation worked at its target seam. Packed blob offsets
plus a lazy duplicate-count map reduced `newRecordLineSet` from 557 to 17
allocations per representative load (-96.95%), from 54,800 to 37,520 B/op
(-31.53%), and from 207.660 to 198.518 us/op (+4.40%). The executable reference
oracle covered LF, CRLF, unterminated records, multiplicity, forced hash
collisions, first-representative behavior, and recycled-buffer lifetime. A
planted missing-repeat-marker mutation failed and the live legacy/snapshot
differential remained exact.

The seam win again failed to become a product win. The first 50-pair cohort was
invalidated by variability above 10% and a short host disturbance that struck
two adjacent candidate slots; its artifacts remain negative harness evidence.
A fresh, seed-11011 randomized balanced 50-pair cohort stayed below the 10% CV
gate for user, total CPU, and wall. Mean user CPU improved only 1.22% (95%
paired-bootstrap interval -0.86% to +3.27%), total CPU 0.58% (-1.92% to
+2.98%), and wall 1.07% (-0.95% to +3.06%). Wall and RSS p95 both regressed
slightly. All 100 normalized outputs shared one exact hash. The candidate was
rejected and the production/test bytes were restored exactly.

Pass 12 moves to the larger named avoidable path rather than varying this
container again: direct record-delta-to-event fusion removes synthesized diff
buffers and Scanner framing while retaining the same semantic finalizer and a
deterministic conservative fallback.

## 2026-08-24 pass-12 direct-event-fusion rejection

The candidate accumulated positive old/new record multiplicities directly
into the shared event finalizer, bypassing synthesized diff text and Scanner
framing on certified inputs. It retained the complete legacy pipeline for
records near the 10 MiB token ceiling and for conflicting same-ID snapshots.
The synthetic matrix covered CRLF, malformed records, filters, duplicates,
dense 726-record snapshots, and buffer lifetime. The real Git oracle matched
all 1,776 events in order and across all seven compared fields. A planted
`>` to `>=` multiplicity mutation made identical sets emit one event and was
caught before restoration.

The 12-pair screen was positive, so the candidate advanced to the declared
seed-12012 balanced randomized 50-pair cohort on the quiet exact-source host.
All 100 normalized outputs shared SHA-256
`f8f098b09e0a34b5257ae2f5cfc15da85a99ccc311cb348f6ec1dd9165a75853`.
Mean user CPU fell from 0.386278 s to 0.362202 s (-6.23%, paired-bootstrap 95%
interval +4.21% to +8.18%), but total CPU fell only from 0.588110 s to
0.570598 s (-2.98%, interval +0.21% to +5.64%). System CPU instead rose from
0.201832 s to 0.208397 s (+3.25%), and its p95 rose 4.55%. Mean wall improved
3.83%, but baseline wall CV was 11.11%; baseline total-CPU CV was 10.97%.
Mean RSS differed by only 0.24 KiB, while candidate worst RSS was 228 KiB
higher.

The decision card required lower-95% gains of at least 3% for both user and
total CPU, sub-10% CV, and non-regressing system CPU/tails/RSS. User CPU alone
passed; total CPU, variability, and system CPU did not. The source/test
candidate is rejected and restored. Preserved remote evidence root:
`/data/tmp/bv-p12-20260824.fusion`; confirm TSV SHA-256
`468be0e186489e690e0f3de924d857d31d10c8c0ee2f88fffd7ab70d47dc934d`.

Pass 13 therefore attacks the producer/consumer serialization boundary rather
than JSON framing again: bounded one-object-ahead `git cat-file --batch`
prefetch may overlap Git inflation with current-blob parsing without changing
object count, order, or representation.

## 2026-08-24 pass-13 one-ahead-prefetch rejection

The exact frozen baseline profile supported the experiment: eight merged
cold-cache runs attributed 0.37 s / 17.05% cumulatively to `blobReader.read`,
0.29 s / 13.36% to `readFull`, and 0.35 s / 16.13% to
`newRecordLineSet`. The candidate precomputed the unchanged unique first-use
OID order and allowed exactly one response future. It synchronously claimed
the recycled spare before launching the reader, joined any in-flight response
on close, and retained the synchronous test API. A delayed-start mutation
blocked before `await` and failed the planted proof; the restored path passed.
Focused protocol/lifetime tests, the 1,776-event real Git differential, full
correlation and race suites, build, vet, non-vendor formatting, whitespace,
and exact-file UBS passed. Full-tree `gofmt -l .` remained red only for the
repository's pre-existing vendored files.

The first 12 pairs were a false positive: wall improved 17.97% with 12/12
wins, but CPU and wall variability were high. The predeclared seed-13013
balanced randomized 50-pair cohort instead measured user CPU 0.395735 s to
0.421655 s (+6.55% regression, paired-bootstrap improvement interval -9.79%
to -3.78%), total CPU 0.617503 s to 0.658907 s (+6.70%, interval -10.87% to
-3.11%), and wall 0.534044 s to 0.538772 s (+0.89%, interval -3.68% to
+1.62%). Candidate wall p95 rose from 0.636483 s to 0.655170 s, and worst wall
rose from 0.755022 s to 0.985104 s. Mean/p95/worst RSS all improved by less
than 0.1%, too small to offset the regressions. All 100 outputs shared the
exact normalized SHA-256
`f8f098b09e0a34b5257ae2f5cfc15da85a99ccc311cb348f6ec1dd9165a75853`.

The causal candidate profile placed 0.32 s beneath the new prefetch goroutine;
sampled `newRecordLineSet` rose from 0.35 s to 0.45 s, while `memmove` rose
0.15 s to 0.19 s. A goroutine/channel per object perturbed scheduling and cache
locality rather than hiding the read. The candidate is rejected and restored.
Evidence root: `/data/tmp/bv-p13-20260824.prefetch`; confirm TSV SHA-256
`daedb0c1e08222b6e667c57989ff4e2aa746d75a197f34253e4fc6338a59985a`.

Pass 14 tests the independently designed Go 1.25 Green Tea collector build.
It changes no source and must beat the same combined CPU/tail gates; a GC
experiment is not accepted merely because one synthetic heap regime likes it.

## 2026-08-24 pass-14 Green Tea GC rejection

The exact accepted source was built with the direct Go 1.25.5 toolchain and
`GOEXPERIMENT=greenteagc`; build metadata records `go1.25.5 X:greenteagc` and
the binary SHA-256 is
`fe50a53d9408fbfe175f56ab3680e79a13f40d4cbf0fad3e7e946bbf230fed24`.
An initial attempt through the host's older Go launcher panicked in its FIPS
toolchain-selection path and received zero credit. The direct pinned build used
pass-private caches, then passed ordinary/race correlation, build, and vet.

The 12-pair screen suggested user CPU -3.54%, total CPU -3.49%, and wall
-1.78%, so it advanced rather than being mistaken for a result. In the
balanced seed-14014 50-pair confirmation, mean user CPU moved from 0.384338 s
to 0.379151 s (-1.35%; paired-bootstrap 95% improvement interval -1.87% to
+4.61%). Total CPU moved 0.610030 s to 0.593323 s (-2.74%; interval -1.48% to
+7.24%), and wall moved 0.522907 s to 0.515039 s (-1.50%; interval -2.03% to
+5.06%). Candidate user/wall p95 regressed 3.55%/6.48%; mean RSS improved only
0.048%. All 100 confirmation outputs shared normalized SHA-256
`f8f098b09e0a34b5257ae2f5cfc15da85a99ccc311cb348f6ec1dd9165a75853`.

The predeclared keep gate required both user- and total-CPU lower confidence
bounds of at least 3%, plus non-regressing tails/RSS. It fails decisively.
Green Tea is rejected with no runtime/source change and no speedup claim.
Evidence root: `/data/tmp/bv-p14-20260824.greenteagc`; confirmation TSV
SHA-256:
`05a318b670fad7ab1c8c6d75ae767ec3372aa09d34425003e1764464cec19902`.

## 2026-08-24 pass-15 flat-record-table preimplementation rejection

The exact Pass 13 merged baseline provides the decision: out of 2.17 s total,
`newRecordLineSet` holds 0.35 s. Its line-by-line listing assigns 0.19 s to
mandatory `maphash.Bytes`/`aeshashbody`, 0.08 s to mandatory newline search,
0.01 s to map lookup, and 0.07 s to insertion/entry creation. The entire
replaceable residual is therefore only 0.08 s / 3.69%; named map assignment,
access, and overlapping grow/rehash frames are 1.84%, 0.46%, and 0.92%.

A perfect zero-cost replacement would have to erase more than 81% of that
residual to reach a 3% point estimate, leaving no credible room for its own
probing, allocation, zeroing, or resizing. Pass 11 is the empirical control:
removing 96.95% of target allocations improved the seam 4.40% but product user
CPU only 1.22%, with confidence crossing zero and slightly worse wall/RSS p95.

Canonical `/dp/alien_cs_graveyard` section 7.7 recommends Swiss-table layout
only when probe/layout/resize profiles support it. Go 1.25 already reports
Swiss-style control-group and rehash frames here; the dominant record-byte hash
would remain. A future exact table must aggregate solely by full 64-bit digest,
preserve the first representative, treat hash zero as occupied, and survive
wraparound/resize. Exact-line collision buckets would change current behavior.

The mutation gate therefore rejects implementation. No source, test, build, or
benchmark changed, and no speedup is claimed. Retry only after the profile
attributes at least 5% exclusive command CPU beyond hashing/newline scanning to
the container and a target prototype removes at least 20% of the full function.

## 2026-08-24 pass-16 strict-delta-cancellation counterexample

The corpus strongly activates a delta idea: 498 usable adjacent snapshot pairs
contain 174,428 record lines and hash 384,364,934 bytes. Canceling equal
whole-line prefixes/suffixes first would leave 152,056,507 bytes (-60.44%) and
remove 61.17% of hash calls. This maps directly to incremental computation and
view maintenance in canonical `/dp/alien_cs_graveyard` sections 6.1 and 8.15.
It also requires 768,686,298 bytes of equality comparison, roughly twice the
current hash input.

The rewrite nevertheless fails formally under current semantics. Choose
distinct record lines `A` and `B` with the same 64-bit digest. Old `[A]` and
new `[A,B]` produce digest counts one and two; both buckets retain first
representative `A`, so baseline synthesis adds `A`. Prefix cancellation removes
the shared `A`, leaving new `[B]`, so the candidate adds `B`. Hashing canceled
`A` to detect the collision gives up the advertised reduction.

This is a semantic counterexample, not a probabilistic collision concern: the
campaign's exactness oracle must cover forced hashes. Implementation was
therefore rejected before mutation. Retry requires either an independently
approved switch to exact-line identity or a construction that preserves digest
counts and first representatives without hashing canceled records. No speedup
is claimed.

## 2026-08-24 pass-17 title-validation-sink rejection

Correlation decodes `title` only to retain the current JSON acceptance boundary;
the value is not used to choose an event. A custom zero-sized sink looks
attractive, but pinned goccy/go-json v0.10.6 invokes `UnmarshalJSON` only after
`skipValue`, allocating and copying the complete value first. The sink must
rescan that copy to validate escapes. Normal string decoding instead validates
in place and aliases the existing top-level record copy. `TextUnmarshaler` is
also inexact because the decoder presents boolean `true` and string `"true"`
identically, and dropping the typed field exposes a laxer unknown-field skip.

The exact 2.17 s merged profile assigns the entire `parseBeadJSON` frame only
0.08 s / 3.69%. The corpus has 5,230 title fields and only 208 with escapes, so
the hook would add allocation/copy/interface work to 5,022 plain titles. Even a
physically free removal of the whole frame barely exceeds a 3% point estimate.

The designed oracle covers invalid numeric/boolean titles, invalid escapes and
Unicode hex, an invalid duplicate after a valid field, and accepted null,
surrogate, raw-invalid-UTF-8, and control-byte behavior. The only small exact
cleanup—dropping `Title` from the returned snapshot while still decoding it—
saves a 16-byte temporary header but no backing allocation or parser work.
Implementation is rejected at the profile/dependency-source gate; no speedup is
claimed.

## Pass 18 recommendation — transposed blob arena

Pass 6 proved that a 64 KiB `blobReader` transport buffer removes copy work
(`memmove` -45.8%) but also removes heap-goal runway (GC cycles +28.65%). The
current exact profile still assigns 17.05% to blob reads, 13.36% to `readFull`,
6.91% flat to `memmove`, and 5.99% flat to `memclr`, satisfying that retry.

The candidate transposes capacity rather than deleting it. With old reader
capacity `O=10 MiB`, new reader `N=64 KiB`, and first valid payload `S`, reserve
`O-N` extra capacity in that already recycled payload arena. Requested live
capacity remains `O+S = N+(S+O-N)`, while reads larger than `N` can bypass
bufio's copy. This is the region/arena lifetime idea from canonical
`/dp/alien_cs_graveyard` section 5.10 applied to Go heap pacing.

Keep only with exact behavior; a mutation-sensitive capacity/missing-object
proof; >=30% lower merged `memmove`; non-increasing GC cycles; and a balanced
50-pair lower confidence bound >=3% for both user and total CPU with stable
system CPU, wall tails, RSS, and syscall counts. A screen is not a claim.

## 2026-08-24 pass-18 transposed-blob-arena rejection

The mechanism worked. The 64 KiB transport plus 10,420,224-byte first-payload
runway passed its capacity/missing/recycle/alias oracle; zeroing the production
runway failed `got 0, want 10420224`. Exact behavior held through the
1,776/1,776 real-history differential, correlation ordinary/race, build, vet,
formatting, and scoped UBS.

Eight merged profiles moved total samples from 2.14 s to 2.03 s (-5.14%),
`memmove` 0.26 s to 0.10 s (-61.54%), `readFull` 0.32 s to 0.25 s (-21.88%),
and `blobReader.read` 0.35 s to 0.30 s (-14.29%). Fifteen traced pairs moved
mean GC cycles 12.27 to 11.47 (-6.52%; ten wins, five ties, zero losses), so
the arena transposition genuinely fixed Pass 6's GC-pacing regression.

It still failed the frozen product conjunction. A first 50-pair cohort was
excluded whole after unrelated system spikes drove total/wall CV over 50%.
The fresh seed-18019 cohort improved user CPU 5.61% with a +3.47% to +7.66%
interval, but baseline user CV was 11.62%. Total CPU improved 3.14% with a
-1.66% to +7.58% interval and CV 41.44%/31.58%; wall improved 3.29% with a
-0.47% to +6.83% interval and CV 31.83%/24.13%. RSS regressed 0.061%, with its
entire interval on the wrong side; mean reads increased 0.76% across two traces
per arm. All measured product/GC outputs remained normalized-exact.

The candidate is rejected by the campaign gate despite the real seam/user-CPU signal.
Evidence root: `/data/tmp/bv-p18-20260824.transposed-blob-arena`; credited TSV
SHA-256:
`3d028ca8cc06cc4bd8f69f2afea209221925c8077426e3bdd971d5de46fa5f5c`.

An independent 50-pair replay on `vmi1167313` began at load 0.67 with exact
binary and fixture hashes. User CPU improved 3.78%, but its interval crossed
zero and both CVs exceeded 14%. Total CPU regressed 5.08%, system CPU 10.14%,
and wall 5.14%; candidate wall p95 regressed 11.29%. All outputs remained
exact. Independent TSV SHA-256:
`940f79b1358ae55beabf47b5197a6f48a27be604c187744b0eb4dc801046994e`.

The earlier restoration sentence is superseded by shared-tree state: a
concurrent workflow committed and pushed the candidate as `66eafd5a` before
the restoration agent ran. The agent correctly preserved peer-owned HEAD.
Pass 18 remains rejected as a measured product claim even though its runtime
bytes are now externally retained; subsequent passes profile that actual HEAD.
