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
| NE-20260823-12 | Per-candidate transitive-unblock simulation is the next material robot-insights bottleneck. | The exact 540-issue registry profile contained no named transitive-simulation frame. An isolated batch measured about 6.96 ms/op, while the counter itself was about 1.45% of sampled CPU and roughly 85 us per batch; that benchmark also overstates the registry path because completed stats are already supplied. | Retry if a low-load target profile attributes at least 5% CPU or 1 ms/invocation to this work, or a representative larger/deeper graph promotes it into the top five frames. |
| NE-20260823-13 | Replacing full metric-map copy/sort with top-k selection is presently a material 540-issue improvement. | Full-stats assembly does eight 540-entry copies and sorts plus eight 200-entry result maps, with about 104 KB in sortable slices, but accepted profiles do not identify it as a material frame. The fresh execution gate failed at 1-minute load 14.01, so no timing was invented. | Retry when a low-load registry profile attributes the path to at least 5% CPU or 1 ms/invocation, or a representative 5,000+ issue workload attributes at least 5% of command time or allocations to it. |
| NE-20260823-14 | A naive full-suite run in the RCH mirror is a valid repository gate. | RCH places its default test temp root below the mirrored repository, intentionally omits `.git`, and may run as root. The first runs therefore discovered the mirror's real `.beads`, failed VCS stamping, and invalidated permission-denied behavior; those runs are red and receive no credit. | Use an external `TMPDIR`, disable VCS stamping only for the Git-less mirror, and select a non-root worker; then require both ordinary and race suites to pass. |

Current positive profile anchors, all on low-load `hz1` unless stated otherwise:

- `--profile-startup`: 540 issues, 685 blocking edges, 32.76 ms including load.
- `--robot-insights`: 0.21-0.31 s over six cold-process/warm-cache runs;
  all reported graph metrics computed, no timeouts.
- RCH `hz2` microbenchmarks: JSONL load 7.67-10.31 ms and 13.12 MB/op;
  graph construction 0.333-0.373 ms and 358 KB/op; TUI layered-1k rebuild
  1.23-1.33 ms; export layered-1k layout 1.26-1.51 ms.
- These anchors rank investigation targets. They are not before/after speedup claims.

### Retry dispositions

- **NE-20260823-07 satisfied for registry-backed robot commands:** the planted
  pre-fix case produced a zero-byte profile, while the repaired path produced
  valid robot JSON and a non-empty profile readable by `go tool pprof`. The
  invalid-destination path still failed nonzero before success output. This
  enables CPU attribution for registry robot commands only; direct legacy
  `os.Exit` branches remain unproven.
- **NE-20260823-02 satisfied:** an explicit disable-cache analysis mode now
  bypasses all three analysis-cache layers and remains absent from robot
  serialization. On low-load `hz2`, fresh full analysis measured
  36.63-39.96 ms/op rather than the contaminated 0.565-0.742 ms/op; a
  mutation-sensitive test proves default reuse and fresh recomputation.
- **NE-20260823-03 satisfied:** analyzer construction, hash seeding, and context
  cancellation now occur outside the cache read-hit benchmark timer. On the
  same low-load worker, measured allocations fell from about 92.5k to 56.7k
  per operation and bytes from about 16.35 MB to 11.5 MB. The remaining
  25.89-29.71 ms/op is the read/decode/reconstruction target, not a claimed
  product improvement.

### Current CPU attribution

- A first repaired profile on `hz1` was rejected because the 1-minute load was
  11.50 immediately before execution; its samples receive no campaign credit.
- The exact same Linux binary and 540-issue JSONL were copied to a worker whose
  fresh 1-minute load was 1.16. The warm-cache `--robot-insights` profile was
  readable and collected 390 ms of CPU samples: repeated `Analyze()` calls
  beneath `TopWhatIfDeltas` accounted for about 70 ms cumulative, and robot
  disk-cache read/decode accounted for about 70 ms cumulative. These are
  attribution anchors, not yet speedup claims.

### Runtime dispositions

- **TopWhat repeated analysis accepted and repaired:** the planted pre-fix
  540-issue batch took 4.4108 s and allocated 1.648 GB because each issue
  decoded completed stats again. Reusing caller stats reduced same-worker
  iterations to 3.203-16.564 ms and 1.444-4.931 MB, preserved normalized robot
  output exactly, and removed TopWhat/cache-decode frames from the next CPU
  profile. The remaining advanced-insights analysis is a separate candidate.
- **Flat-copy post result rejected:** an intermediate remote copy placed files
  at the wrong level, so the command exercised old nested source and later
  mixed packages during vet. Those results receive no credit; the corrected
  relative-copy tree and exact hashes produced the accepted comparison.
- **Repeated structural hashing in registry insights rejected:** robot mode
  selects the data-hash/disk-cache branch, and an exact-source low-load profile
  had zero `graphStructureHash` samples. Retry only after a control-flow change
  or an admissible profile shows at least 5% CPU or 1 ms/invocation there.
- **Advanced-insights reanalysis accepted and repaired:** cycle-break generation
  decoded analysis again only to read the completed cycle list. Passing the
  handler's existing stats preserved normalized robot output exactly; the next
  low-load profile contained no advanced-insights analysis/cache-decode frame.
- **Convergence accepted after two zero-change passes:** transitive simulation
  and bounded metric-map assembly were both real, bounded costs, but neither
  cleared the campaign's measured materiality threshold. The repeated-skill
  rule therefore stops the loop after pass 8 rather than treating the 50-pass
  cap as a mutation quota.
- **NE-20260823-14 satisfied:** on non-root `hz1`, both the full ordinary and
  race suites passed with `TMPDIR=/tmp` and `GOFLAGS=-buildvcs=false`. These are
  functional gates for the exact mirrored source, not performance measurements.

## 2026-08-24 continuation negative evidence

| ID | Rejected or deferred claim | Evidence | Retry predicate |
|---|---|---|---|
| NE-20260824-01 | Default auto-discovery in the public checkout exercises triage correlation. | The default command loaded 682 issues with zero open items and left `history_status` absent, while explicit `.beads/issues.jsonl` loaded 540 issues with 19 open and `history_status=ok`. The default run is excluded from correlation profiling. | Retry only after source selection is pinned to a verified open-issue dataset, or after a separate datasource investigation explains and intentionally validates the 682-issue source. |
| NE-20260824-02 | Current warm triage and robot-next timings support a precise latency comparison. | One hundred warm samples produced CV 17.12% for triage and 22.09% for robot-next, above the 10% publication gate. | Retry on a quieter/pinned host with interleaved sides and CV below 10%; until then these values are advisory boundaries only. |
| NE-20260824-03 | Raw deterministic triage JSON hashes match without normalization. | With `SOURCE_DATE_EPOCH` pinned, diffs still contained per-metric `status.*.ms` values (for example PageRank and betweenness timing). Product fields were stable in the inspected diff. | Normalize only `generated_at`, `compute_time_ms`, `elapsed_ms`, and per-metric `ms`, then require byte-identical hashes and inspect any remaining diff. |
| NE-20260824-04 | Cold history cost is primarily graph analysis or final encoding. | A valid 540-issue/19-open CPU profile put 67.92% cumulative CPU in `GenerateReportCached`, 62.26% in history extraction, and 58.49% in snapshot extraction. `/usr/bin/time` showed 0.86 s wall and 109,144 KiB RSS; `strace` observed eight process executions and heavy pipe traffic. | Reconsider only if a same-fixture post-change profile moves correlation below graph/encoding or a different representative workload ranks another subsystem first. |
| NE-20260824-05 | Reusing evicted blob buffers produces a defensible cold-triage latency win. | On low-load `ovh-b`, a 20-pair high-resolution run improved mean wall by only 0.85% (14/20 faster, one-sided sign p=0.0577) while p95 and worst were slightly worse. A fresh 40-pair run improved mean by 0.83%, split pairs 20/20 (p=0.5627), and regressed median, p95, and worst. | Retry only if a materially different representative history or storage regime makes allocation/GC dominate wall, then require interleaved CV below 10%, significant paired results, and non-regressing tails. |
| NE-20260824-06 | Blob-buffer reuse lowers peak RSS. | Twenty untraced matched runs measured 109,397.8 KiB baseline versus 109,513.6 KiB candidate mean maximum RSS, a small 0.106% increase. The one-spare design is bounded, but no footprint win was observed. | Retry with an allocation/heap profile or a history whose blob-size distribution makes peak live storage sensitive to reuse; do not infer RSS from fewer GC cycles. |
| NE-20260824-07 | A naive RCH `go test ./...` result is a valid continuation full-suite gate. | The changed correlation package passed, but the aggregate run failed from RCH-specific current-directory discovery, privileged permission semantics, missing Git/VCS metadata, and timing-sensitive shared-worker tests. On direct low-load execution every package except the Git-requiring loader case passed, including E2E; the loader package passed after placing the exact candidate bytes in a new Git-backed tree. | Credit a future aggregate RCH run only with stable cwd/TMPDIR, non-root execution, Git semantics accounted for, and acceptable worker load; otherwise compose explicit per-environment package results without relabeling the failed aggregate green. |
| NE-20260824-08 | Inline record-line entries plus exact collision buckets improve the measured cold history path. | Although all semantic and build gates passed and normalized output was exact, 20 low-load interleaved pairs regressed mean wall 5.62%, median 4.62%, p95 8.11%, worst 6.97%, and mean user CPU 6.69%. Pre-sizing and exact comparison added about two full-history memory passes. The candidate was fully restored. | Retry only with a design that removes per-entry allocation without a separate line-count scan or ordinary-path full-line comparison; require lower CPU/allocation and non-regressing median/p95/worst on the same fixture. |
| NE-20260824-09 | Correlation report-cache codec/layout is a material next cold-triage optimization. | The accepted 2.06 s merged profile has a 103 ms 5% threshold and names no report-cache/codec function at that level or at 1 ms/invocation. The cache already uses goccy JSON, avoids hit rewrites, and is bounded; preserved one-entry files were 772,000 and 673,954 bytes. A double serialization exists on misses but lacks material attribution. | Retry when a representative cold or multi-entry-cache profile attributes at least 5% sampled CPU or 1 ms/invocation to a specific cache read/write/codec lever. |
| NE-20260824-10 | The continuation has a clean full aggregate race result. | Correlation race passed. In full race attempts, RCH lacked a GUI editor for two UI tests, a copied low-load source tree lacked repository-root Git/Beads discovery for one loader test, and the exact local tree ran at load 39.38 and hit one five-second CLI timeout. All unaffected packages passed, but no aggregate run was clean. | Retry on a low-load non-root full Git checkout with GUI/editor expectations satisfied, canonical temp-path semantics, and no mirrored-cwd contamination; require one clean aggregate command. |
| NE-20260824-11 | Reusing size-checked correlation cache payloads as `json.RawMessage` improves the cold full-miss path. | The retry predicate for NE-20260824-09 became true at 7.22% focused CPU, but the tested formulation did not convert it into a win. Twenty low-load alternating pairs moved mean user CPU only -0.85% (11 wins, one tie, eight losses; p=0.3238; both CVs above 17%), increased mean RSS 0.129%, and produced no publishable wall result. Five merged profiles moved the focused cache path from 70 ms/1.75 s (4.00%) to 90 ms/1.81 s (4.97%). All 50 normalized outputs remained exact; the candidate was restored. | Retry only with a serializer or cache layout that reuses preflight bytes without `RawMessage` validation/compaction copying. Require lower focused CPU/allocation plus statistically defensible, non-regressing user CPU, wall tails, and RSS on the same fixture. |
| NE-20260824-12 | One passing aggregate ordinary suite proves `TestRace_DataConsistency` is stable. | The first non-root Git-backed full suite failed because two of five concurrent raw `--robot-next` outputs differed; the test compares timing-bearing JSON byte-for-byte. Five focused replays and one full-suite retry passed, so this is an intermittent test-or-product nondeterminism signal, not a deterministic cache-restoration regression. | Reproduce with raw artifacts retained, identify whether only documented timing fields differ, and make the oracle compare deterministic semantic fields without weakening concurrency coverage. Until then, preserve the initial failure beside later greens. |
| NE-20260824-13 | Storing `recordLineEntry` inline in the existing Go map materially improves cold history extraction. | The formulation removed per-entry pointers without the rejected pre-scan or line comparison and preserved all 60 measured normalized outputs. Thirty low-load alternating pairs moved mean user CPU only 0.71% (95% paired-bootstrap interval -7.33% to +7.99%; 16 wins/14 losses), left wall effectively flat (-0.10%), and left RSS flat/slightly higher (+0.018%). Fifteen traced pairs reduced mean GC cycles 12.53 to 11.93 (-4.79%; sign p=0.0547). Eight merged profiles reduced `newRecordLineSet` cumulative samples 0.65 s to 0.57 s but increased map assignment/access and `memclr`; the candidate was restored exactly. | Retry only with a cache-denser representation that avoids both heap objects and large inline slice-header copies, such as pointer-free packed blob offsets or a measured flat table. Require at least 3% defensible end-to-end CPU improvement, lower target allocation/GC work, and non-regressing wall tails/RSS. |
| NE-20260824-14 | Shrinking the snapshot `blobReader` from 10 MiB to 64 KiB converts its measured copy reduction into a defensible cold-triage win. | The intended direct-read mechanism worked: eight merged profiles reduced `memmove` from 0.48 s to 0.26 s and `NewReaderSize` from 0.08 s to 0.01 s. But removing the 10 MiB allocation lowered Go's heap goal and increased mean GC cycles 28.65% (12.33 to 15.87), losing all 15 trace pairs; background-GC samples rose 0.45 s to 0.62 s. Thirty low-load alternating pairs improved mean user CPU 4.82%, but split 15/1/14 with a 95% improvement interval of -2.17% to +11.50%; wall improved only 1.09%, RSS 0.031%, and reads increased about 90 per run. Exact normalized behavior held and the candidate was restored byte-for-byte. | Retry only after a separately measured heap-pacing policy makes the smaller allocation neutral to GC frequency, or test a buffer-size policy that preserves the copy reduction without increasing GC cycles. Require lower target copy/allocation cost, lower-bound user-CPU gain of at least 3%, and non-regressing GC, tails, RSS, and syscall counts. |
| NE-20260824-15 | Eliminating `parseRawDiffLines` scanner allocation materially improves cold triage. | The direct parser was behavior-exact and mutation-sensitive, made its one-record seam 46.3x faster, cut 65,920 to 224 B/op, and reduced 15-pair mean GC cycles 16.95% (13 wins, two ties, zero losses). Yet 100 quiet-host cold pairs improved mean user CPU only 1.41%, with a 95% interval of -1.17% to +4.22% and 46/14/40 wins/ties/losses. User median/p95 and wall were unchanged, RSS was flat, and sys-time p95/p99 worsened. The old parser occupied only 0.02 s cumulative in eight merged profiles. The 94-line production rewrite was restored exactly. | Retry only when a representative profile attributes at least 5% sampled CPU or 1 ms/invocation to this parser, or a materially larger followed history makes scanner allocation a primary product resource. Require a defensible command-level CPU/latency/resource win, not only fewer allocations or GC cycles. |
| NE-20260824-16 | Replacing the serial loader's 10 MiB transport buffer with a stat-sized/64 KiB reader improves measured loading or cold triage. | The candidate preserved the historical record cap, passed an executable old/new differential oracle, and a planted `>` to `>=` boundary mutation failed. It reduced mean loader allocation from 13,110,564 to 3,995,973 B/op (-69.52%), but 30 alternating direct-loader pairs regressed mean time 12.11% (26 losses, four wins; paired-bootstrap improvement interval -17.44% to -7.46%). Thirty cold-triage pairs regressed mean user CPU 17.00% and wall 6.50%; all 60 normalized outputs matched, and RSS was flat. Fifteen traced pairs increased mean GC cycles from 10.60 to 25.07 (+136.48%). The smaller allocation again lowered the heap goal and amplified collection work; both source and tests were restored to their exact initial hashes. | Retry only with a separately proven heap-pacing policy or a transport design that retains the useful heap-goal runway without zeroing/copying the full 10 MiB. Require lower allocation volume and non-increasing GC cycles plus a lower-bound product CPU gain of at least 3% with non-regressing wall tails/RSS. |
| NE-20260824-17 | Moving valid `Dependency.UnmarshalJSON` records from stdlib to goccy improves cold triage because the method is 17.44% of direct-loader CPU. | The candidate preserved aliases, duplicate-key order, errors, receiver atomicity, invalid UTF-8 replacement, and all 60 normalized product outputs. Thirty direct-loader pairs improved mean time 13.00% (95% interval 9.84% to 16.12%), B/op 0.35%, and allocations/op 33.63%. But dependency decoding occurs in the one-time full loader, not correlation's partial `parseBeadJSON`. Thirty exact-source cold-triage pairs regressed mean user CPU 2.12% (95% improvement interval -7.21% to +2.84%), wall 0.58%, and RSS 0.055%; mean GC cycles rose 1.91%. The candidate was restored exactly. | Retry for a loader-dominant workload or persistent mode only after a product profile gives the dependency method at least 5% command CPU or 1 ms per command. A direct-loader win alone is insufficient for the default cold-triage path. |
| NE-20260824-18 | A previously verified quiet-host checkout remains source-exact across later passes. | Git index comparison found the same 4,127 tracked paths but different committed blobs for `extractor_snapshot.go` and its differential test; the host had reverted to an older snapshot implementation while a reused binary still contained the accepted local runtime. Two product cohorts were excluded. Exact files were recopied, production hashes rechecked, and both sides rebuilt with the same command before the credited Pass 9 cohort. | Before every new binary pair, compare tracked-path and blob indexes, then hash every locally modified production file in both trees. Reuse an old binary only when its complete runtime-source manifest matches the candidate baseline. |
| NE-20260824-19 | Raising the runtime heap target with a fixed `GOGC=400` improves the accepted cold-triage product path. | The unset-versus-explicit-100 control was equivalent over 50 high-resolution pairs, and a planted `GOGC=50` negative lost all 12 coarse user-CPU pairs. `GOGC=400` then reduced mean user CPU 5.07%, but its 95% lower bound was only 2.82%, below the 3% gate. Mean system CPU regressed 16.72%; mean wall regressed 3.90% with 34/50 losses and a wholly negative improvement interval (-6.68% to -1.20%); wall p95 rose 10.61% and worst rose 1.62%. All 100 normalized outputs matched. No runtime default or source was changed. | Retry only as an explicit workload-specific operator tuning study, or after a structural allocation change materially alters the live-heap/GC curve. Require a lower-bound product CPU gain of at least 3% together with non-regressing total CPU, wall median/p95/worst, RSS, and GC behavior across representative workloads. |

### Closure of NE-20260824-10

The retry predicate was satisfied after the pass-4 restoration: a complete
non-root Git-backed tree ran `go test ./... -race` with Go 1.25.5,
`BV_NO_BROWSER=1`, `BV_TEST_MODE=1`, and pre-run one-minute load 5.19. Every
package, including `tests/e2e`, passed. Earlier contaminated race attempts stay
in the ledger as environment evidence; they are no longer the final proof
boundary for this runtime.

### 2026-08-24 accepted profile anchor

- Exact runtime source: `1632071f34a05b17208f66799077f2777c05340d`
  (runtime-identical to current `07bd43eb`), Go 1.25.5, Linux/amd64.
- Fixture SHA-256:
  `17b9bf86204f3f637072635bc78e5cb6435a7c14512d3a6210416c28d5216aa9`.
- Cold application-cache triage, N=20: p50 0.781806 s, p95 0.842383 s,
  worst observed 0.871028 s, CV 8.62%.
- Top actionable allocation signal beneath cold snapshot extraction: 100 ms
  large allocation, 70 ms memory clearing, and 70 ms background GC. The first
  pass may target buffer reuse only if differential history/robot output and
  cold same-host measurements remain green.

### 2026-08-24 pass-1 disposition

- Buffer reuse was accepted for CPU/GC efficiency at `22305d12`, not for
  latency or RSS. Twenty untraced interleaved pairs reduced mean user CPU
  14.8% with 19 lower pairs and one tie; ten traced pairs reduced mean GC
  cycles 51.6%; five merged profiles reduced sampled CPU 18.6%.
- Normalized robot output remained exact at SHA-256
  `fb437df32493bde94019d8c9aa784a8681841545ffa4faa03337fb14e9d279f4`.
- The accepted post-change profile still attributes 26.7% cumulative samples
  to `newRecordLineSet` and 12.6% flat samples to AES-backed map hashing. That
  residual promotes record-line-set hashing/allocation to the next pass; it is
  an investigation target, not a promised win.
- The first inline-entry/collision-safe formulation was rejected at pass 2 and
  restored exactly. Its normalized behavior hash was
  `32aaadc67dd872e359d903863c36dc48cbc9ad7f113487129f3ce3b8b0aad10f`,
  but exact behavior does not offset the observed wall and CPU regressions.

### 2026-08-24 independent final replay

- Ten fresh low-load interleaved pairs independently reproduced mean user CPU
  0.647 s to 0.573 s (-11.44%, 10/10 wins, p=0.00098). Five traced pairs
  reproduced mean GC cycles 25.8 to 13.0 (-49.61%).
- Wall -2.26% missed paired significance (8/10, p=0.0547); peak RSS increased
  0.177%. These corroborate the ledger's no-latency and no-RSS claims.
- All 20 normalized outputs matched at SHA-256
  `5992ff99901b1c5abf8ccf3b1a9e3d2490a6f0eda1742cad938e7b3ff9809918`.
- Runtime `22305d12` is accepted for approximately 11-15% lower user CPU and
  about 50% fewer GC cycles on this workload. The final verifier's frozen HEAD
  predated the committed pass-3 disposition, so its convergence verdict was
  partial even though the pass-3 agent independently returned zero change.
