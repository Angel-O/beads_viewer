#!/usr/bin/env bash
# benchmark.sh: the tracked bv benchmarks, their baseline, and the regression
# comparison used by scripts/release_gate.sh stage 8.
#
# Usage:
#   scripts/benchmark.sh baseline                 # regenerate benchmarks/baseline.txt (with provenance header)
#   scripts/benchmark.sh compare                  # run the tracked set, compare against the baseline, exit 1 above BENCH_PCT
#   scripts/benchmark.sh compare-files BASE CUR   # compare two existing `go test -bench` outputs
#   scripts/benchmark.sh run                      # run the tracked set into benchmarks/current.txt
#   scripts/benchmark.sh quick                    # one-shot subset for a fast local sanity check
#
# Environment:
#   BENCH_COUNT=5        -count per benchmark (medians are compared, so odd counts work best)
#   BENCH_PCT=20         regression threshold in percent on median sec/op
#   BENCH_DATASET=tests/testdata/benchmark/medium.jsonl
#                        frozen issue file the RealData_* benchmarks read (copied to a temp
#                        BEADS_DIR as issues.jsonl); the live tracker is never used, so the
#                        baseline does not drift as beads close
#   BENCH_USE_BENCHSTAT=1 prefer benchstat when it is installed (the built-in comparison is
#                        the default so the gate has no dependency outside the Go toolchain)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

BENCHMARK_DIR="benchmarks"
BASELINE_FILE="$BENCHMARK_DIR/baseline.txt"
CURRENT_FILE="$BENCHMARK_DIR/current.txt"
# The baseline is taken once with more repetitions; compare runs (gate stage 8)
# use fewer so the gate stays under ten minutes on the reference machine.
# BENCH_COUNT overrides both.
COUNT="${BENCH_COUNT:-5}"
COMPARE_COUNT="${BENCH_COUNT:-3}"
PCT="${BENCH_PCT:-20}"
DATASET="${BENCH_DATASET:-tests/testdata/benchmark/medium.jsonl}"

# The tracked set. Names are anchored so a new BenchmarkFoo_Sparse100Extra does not
# silently join the comparison; add it here deliberately.
TRACKED='^Benchmark(RealData_(FullTriage|FullAnalysis|GraphBuild)|FullAnalysis_(Sparse100|Dense100|ManyCycles20)|SnapshotSwap|KeyPressLatency|ListItemBuild|ParseIssuesPoolComparison)$'
PACKAGES=(./pkg/analysis ./pkg/ui ./pkg/loader)

mkdir -p "$BENCHMARK_DIR"

dataset_dir=""
# An if (not `[ ] &&`) so the trap's status never overrides the script's exit code.
cleanup() { if [ -n "$dataset_dir" ]; then rm -rf "$dataset_dir"; fi; }
trap cleanup EXIT

prepare_dataset() {
  [ -f "$DATASET" ] || { echo "benchmark: dataset $DATASET not found" >&2; exit 2; }
  dataset_dir="$(mktemp -d "${TMPDIR:-/tmp}/bv-bench-dataset.XXXXXX")"
  mkdir -p "$dataset_dir/.beads"
  cp "$DATASET" "$dataset_dir/.beads/issues.jsonl"
  export BEADS_DIR="$dataset_dir/.beads"
  export BV_NO_UPDATE_CHECK=1 BV_NO_BROWSER=1 BV_TEST_MODE=1 BV_NO_SAVED_CONFIG=1
}

provenance_header() {
  # Lines starting with '#' are ignored by benchstat and by the built-in comparison.
  local cpu os
  cpu="$(lscpu 2>/dev/null | awk -F: '/Model name/{gsub(/^[ \t]+/, "", $2); print $2; exit}')"
  [ -n "$cpu" ] || cpu="$(uname -p 2>/dev/null || echo unknown)"
  os="$(uname -sr 2>/dev/null || echo unknown)"
  cat <<EOF
# bv benchmark baseline
# generated_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)
# go: $(go version | cut -d' ' -f3)
# cpu: $cpu
# os: $os
# commit: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)
# dataset: $DATASET
# dataset_sha256: $(sha256sum "$DATASET" | cut -d' ' -f1)
# dataset_issues: $(wc -l < "$DATASET" | tr -d ' ')
# count: $COUNT
# tracked: $TRACKED
# command: go test -run '^\$' -bench '$TRACKED' -benchmem -count=$COUNT ${PACKAGES[*]}
# compare: scripts/benchmark.sh compare (median sec/op per benchmark, fails above BENCH_PCT=$PCT%)
EOF
}

run_tracked() {
  # run_tracked <output-file> <count>
  prepare_dataset
  go test -run '^$' -bench "$TRACKED" -benchmem -count="$2" "${PACKAGES[@]}" 2>&1 | tee -a "$1"
}

save_baseline() {
  echo "Regenerating $BASELINE_FILE (count=$COUNT, dataset=$DATASET)"
  provenance_header > "$BASELINE_FILE"
  run_tracked "$BASELINE_FILE" "$COUNT"
  echo "Baseline saved to $BASELINE_FILE"
}

run_benchmarks() {
  : > "$CURRENT_FILE"
  run_tracked "$CURRENT_FILE" "$COMPARE_COUNT"
  echo "Results saved to $CURRENT_FILE"
}

# compare_files BASE CUR: median ns/op per benchmark (the -N cpu suffix is
# stripped), delta in percent, worst regression versus BENCH_PCT.
compare_files() {
  local base="$1" cur="$2"
  [ -f "$base" ] || { echo "no baseline at $base (run scripts/benchmark.sh baseline)"; return 2; }
  [ -f "$cur" ] || { echo "no current results at $cur"; return 2; }
  awk -v pct="$PCT" '
    function median(arr, n,   i, j, t, sorted) {
      for (i = 1; i <= n; i++) sorted[i] = arr[i]
      for (i = 2; i <= n; i++) { t = sorted[i]; for (j = i - 1; j >= 1 && sorted[j] > t; j--) sorted[j + 1] = sorted[j]; sorted[j + 1] = t }
      if (n % 2) return sorted[(n + 1) / 2]
      return (sorted[n / 2] + sorted[n / 2 + 1]) / 2
    }
    function record(file, name, ns,   key) {
      sub(/-[0-9]+$/, "", name)
      key = file SUBSEP name
      count[key]++
      values[key, count[key]] = ns
      if (!(name in seen)) { seen[name] = 1; order[++n] = name }
    }
    FNR == 1 { file++ }
    # go test -bench lines: <name-cpus> <iterations> <value> ns/op [<B/op> <allocs/op>]
    /^Benchmark/ && $4 == "ns/op" { record(file, $1, $3 + 0) }
    END {
      worst = 0; missing = 0
      printf "%-42s %14s %14s %9s\n", "benchmark", "base ns/op", "current ns/op", "delta"
      for (i = 1; i <= n; i++) {
        name = order[i]
        kb = 1 SUBSEP name; kc = 2 SUBSEP name
        if (!(kb in count) || !(kc in count)) {
          printf "%-42s %14s %14s %9s\n", name, (kb in count) ? "present" : "MISSING", (kc in count) ? "present" : "MISSING", "n/a"
          missing++
          continue
        }
        for (j = 1; j <= count[kb]; j++) a[j] = values[kb, j]
        for (j = 1; j <= count[kc]; j++) b[j] = values[kc, j]
        mb = median(a, count[kb]); mc = median(b, count[kc])
        delta = (mb > 0) ? (mc - mb) / mb * 100 : 0
        if (delta > worst) worst = delta
        printf "%-42s %14.0f %14.0f %+8.1f%%\n", name, mb, mc, delta
      }
      printf "worst sec/op regression: %.1f%% (threshold %s%%)\n", worst, pct
      if (missing > 0) { printf "%d benchmark(s) missing on one side\n", missing; exit 1 }
      if (n == 0) { print "no benchmark lines found"; exit 1 }
      exit !(worst <= pct + 0)
    }
  ' "$base" "$cur"
}

compare_benchmarks() {
  [ -f "$BASELINE_FILE" ] || { echo "No baseline found at $BASELINE_FILE; run 'scripts/benchmark.sh baseline' first"; return 2; }
  run_benchmarks
  echo ""
  echo "=== Comparing against $BASELINE_FILE ==="
  if [ "${BENCH_USE_BENCHSTAT:-0}" = "1" ] && command -v benchstat >/dev/null 2>&1; then
    benchstat "$BASELINE_FILE" "$CURRENT_FILE" | tee "$BENCHMARK_DIR/compare.txt"
  fi
  compare_files "$BASELINE_FILE" "$CURRENT_FILE" | tee "$BENCHMARK_DIR/compare.txt"
  # tee masks the awk exit code; re-run silently for the verdict.
  compare_files "$BASELINE_FILE" "$CURRENT_FILE" >/dev/null
}

run_quick() {
  echo "Running quick benchmarks (one shot, tracked analysis subset)..."
  prepare_dataset
  go test -run '^$' -bench '^BenchmarkFullAnalysis_(Sparse100|Dense100|ManyCycles20)$' -benchmem -count=1 ./pkg/analysis 2>&1 | tee "$CURRENT_FILE"
}

case "${1:-run}" in
  baseline) save_baseline ;;
  compare) compare_benchmarks ;;
  compare-files) compare_files "${2:?base file}" "${3:?current file}" ;;
  quick) run_quick ;;
  run) run_benchmarks ;;
  *) echo "usage: $0 {baseline|compare|compare-files BASE CUR|run|quick}" >&2; exit 2 ;;
esac
