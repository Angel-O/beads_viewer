#!/usr/bin/env bash
# benchmark_compare_test.sh: proves scripts/benchmark.sh compare-files (the
# comparison behind release-gate stage 8) turns red on a real regression and
# stays green on noise inside the threshold. Uses synthetic `go test -bench`
# output so it runs in well under a second and does not depend on machine speed.
# Usage: tests/scripts/benchmark_compare_test.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bv-bench-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

# Three runs per benchmark, like a real -count=3 file, with a provenance header
# and the goos/pkg lines go test prints; the comparison must ignore all of them.
cat > "$tmp/base.txt" <<'EOF'
# bv benchmark baseline
# generated_at: 2026-09-02T00:00:00Z
goos: linux
goarch: amd64
pkg: github.com/Dicklesworthstone/beads_viewer/pkg/analysis
BenchmarkRealData_FullTriage-64      100   1000000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   1020000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100    990000 ns/op   500 B/op   10 allocs/op
BenchmarkSnapshotSwap-64            1000     50000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000     52000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000     49000 ns/op   100 B/op    2 allocs/op
PASS
EOF

# Within threshold: +10% on one benchmark, a little faster on the other.
cat > "$tmp/noise.txt" <<'EOF'
BenchmarkRealData_FullTriage-64      100   1100000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   1090000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   1110000 ns/op   500 B/op   10 allocs/op
BenchmarkSnapshotSwap-64            1000     48000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000     47000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000     49000 ns/op   100 B/op    2 allocs/op
EOF

# Regression: SnapshotSwap doubled in every run. The comparison takes the best
# observed time per side (contention only inflates samples), so a regression
# has to show in all samples, and a slow outlier on the current side must NOT
# by itself turn the comparison red (that is the noise the minimum absorbs).
cat > "$tmp/slow.txt" <<'EOF'
BenchmarkRealData_FullTriage-64      100   1000000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   1000000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   1000000 ns/op   500 B/op   10 allocs/op
BenchmarkSnapshotSwap-64            1000    100000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000     98000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000    105000 ns/op   100 B/op    2 allocs/op
EOF

# Contention: one inflated sample on the current side, the others at par.
cat > "$tmp/contended.txt" <<'EOF'
BenchmarkRealData_FullTriage-64      100   1000000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   2600000 ns/op   500 B/op   10 allocs/op
BenchmarkRealData_FullTriage-64      100   1010000 ns/op   500 B/op   10 allocs/op
BenchmarkSnapshotSwap-64            1000     50000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000    140000 ns/op   100 B/op    2 allocs/op
BenchmarkSnapshotSwap-64            1000     49500 ns/op   100 B/op    2 allocs/op
EOF

# Missing: a tracked benchmark disappeared from the current run.
cat > "$tmp/missing.txt" <<'EOF'
BenchmarkRealData_FullTriage-64      100   1000000 ns/op   500 B/op   10 allocs/op
EOF

fail=0
check() {
  # check <name> <expected exit> <current file>
  local name="$1" want="$2" cur="$3" rc=0
  set +e
  out="$(BENCH_PCT=20 "$root/scripts/benchmark.sh" compare-files "$tmp/base.txt" "$cur" 2>&1)"
  rc=$?
  set -e
  if [ "$rc" -ne "$want" ]; then
    echo "FAIL: $name exited $rc, want $want"; echo "$out" | sed 's/^/    /'; fail=1
  else
    echo "ok: $name (exit $rc)"
  fi
}

check "identical files pass" 0 "$tmp/base.txt"
check "noise inside 20% passes" 0 "$tmp/noise.txt"
check "doubled in every run fails" 1 "$tmp/slow.txt"
check "one contended sample passes" 0 "$tmp/contended.txt"
check "missing benchmark fails" 1 "$tmp/missing.txt"

# The reported worst regression must come from the best observed samples
# (98000 vs 49000 = +100.0%), not from the slowest ones.
worst="$(BENCH_PCT=20 "$root/scripts/benchmark.sh" compare-files "$tmp/base.txt" "$tmp/slow.txt" 2>&1 | awk '/worst sec\/op regression/{print $4}' || true)"
case "$worst" in
  100.0%) echo "ok: worst regression reported as $worst" ;;
  *) echo "FAIL: worst regression reported as '$worst', want 100.0%"; fail=1 ;;
esac

# A lower threshold turns the noise file red, proving BENCH_PCT is honoured.
set +e
BENCH_PCT=5 "$root/scripts/benchmark.sh" compare-files "$tmp/base.txt" "$tmp/noise.txt" >/dev/null 2>&1
rc=$?
set -e
if [ "$rc" -eq 1 ]; then echo "ok: BENCH_PCT=5 rejects +10%"; else echo "FAIL: BENCH_PCT=5 exited $rc, want 1"; fail=1; fi

if [ "$fail" -eq 0 ]; then
  echo "benchmark_compare_test: PASS"
fi
exit "$fail"
