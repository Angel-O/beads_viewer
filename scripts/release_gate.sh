#!/usr/bin/env bash
# release_gate.sh: the single pre-release check. Every stage must pass; the
# gate prints one line per stage with its duration and writes the full log to
# tests/artifacts/release_gate_<timestamp>.log. CI (when enabled) and a local
# release run the same script so they cannot disagree.
#
# Stages:
#   1 gofmt            gofmt -l over repo Go files (vendor excluded) is empty
#   2 build+vet        go build ./... && go vet ./...
#   3 unit tests       go test ./... -race -count=1 (pkg/, cmd/, internal/)
#   4 e2e tests        go test ./tests/e2e -race -count=1
#   5 docs parity      go generate ./... leaves the tree unchanged
#   6 action pins      scripts/check_action_pins.sh (40-hex SHAs only)
#   7 vendor hashes    scripts/verify_vendor.sh (pkg/export vendored assets)
#   8 benchmarks       scripts/benchmark.sh compare, > 20% regression fails
#   9 robot smoke      scripts/robot_smoke.sh on this repo and a fixture
#  10 script tests     tests/scripts/*_test.sh for the gate's own helpers:
#                      the benchmark comparator always; the installer harness
#                      when pwsh is available (PWSH=/path or on PATH). The
#                      isomorphic-verify self-test clones and builds twice and
#                      stays a manual run (tests/scripts/verify_isomorphic_test.sh).
#
# Environment:
#   RELEASE_GATE_SKIP="7 8"        skip listed stages (each skip is logged as
#                                  SKIPPED and the gate still exits 0)
#   RELEASE_GATE_ALLOW_MISSING=1   a stage whose script does not exist yet is
#                                  logged as SKIPPED instead of failing
#   RELEASE_GATE_BENCH_PCT=20      regression threshold for stage 8
#   BV_SKIP_ENV_TESTS=1            passed through to tests that must skip on
#                                  machines without git/network (they log why)
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
export BV_NO_BROWSER=1 BV_TEST_MODE=1 BV_NO_SAVED_CONFIG=1 BV_NO_UPDATE_CHECK=1
export GOFLAGS="${GOFLAGS:-}"

mkdir -p tests/artifacts
ts="$(date -u +%Y%m%dT%H%M%SZ)"
log="tests/artifacts/release_gate_${ts}.log"
: >"$log"
echo "release gate $ts on $(git rev-parse --short HEAD 2>/dev/null || echo no-git) ($(go version))" | tee -a "$log"

skip_list=" ${RELEASE_GATE_SKIP:-} "
failed=()
skipped=()
gate_start=$(date +%s)

stage() {
  # stage <number> <name> <command...>
  local num="$1" name="$2"; shift 2
  local start end secs status
  if [[ "$skip_list" == *" $num "* ]]; then
    printf '%-2s %-14s SKIPPED (RELEASE_GATE_SKIP)\n' "$num" "$name" | tee -a "$log"
    skipped+=("$num $name")
    return 0
  fi
  start=$(date +%s)
  {
    echo "----- stage $num $name: $*"
    "$@"
  } >>"$log" 2>&1
  status=$?
  end=$(date +%s); secs=$((end - start))
  if [ "$status" -eq 0 ]; then
    printf '%-2s %-14s ok      %4ds\n' "$num" "$name" "$secs" | tee -a "$log"
  else
    printf '%-2s %-14s FAILED  %4ds  (exit %d, see %s)\n' "$num" "$name" "$secs" "$status" "$log" | tee -a "$log"
    failed+=("$num $name")
    tail -n 25 "$log" | sed 's/^/    /'
  fi
}

# Stage helpers that need more than one command.
check_gofmt() {
  local out
  out=$(gofmt -l cmd pkg internal tests 2>&1 | grep -v '^vendor/' || true)
  if [ -n "$out" ]; then
    echo "files need gofmt:"; echo "$out"; return 1
  fi
  echo "gofmt clean"
}

build_and_vet() { go build ./... && go vet ./...; }

unit_tests() {
  # tests/e2e builds the binary and takes its own stage.
  go test -race -count=1 $(go list ./... | grep -v '/tests/e2e$')
}

e2e_tests() { go test ./tests/e2e -race -count=1; }

docs_parity() {
  # Compare the working tree with itself before and after go generate, so
  # uncommitted work in progress does not count as drift; only what the
  # generators change does.
  local before after
  before=$( { git status --porcelain=v1 --untracked-files=all; git --no-pager diff; } | sha256sum)
  go generate ./... || return 1
  after=$( { git status --porcelain=v1 --untracked-files=all; git --no-pager diff; } | sha256sum)
  if [ "$before" != "$after" ]; then
    echo "go generate changed files (docs or tables out of date):"
    git status --porcelain=v1 --untracked-files=all
    return 1
  fi
  echo "generated files are up to date"
}

script_stage() {
  # script_stage <path> <args...>: run a helper script, honouring RELEASE_GATE_ALLOW_MISSING.
  local script="$1"; shift
  if [ ! -x "$script" ]; then
    if [ "${RELEASE_GATE_ALLOW_MISSING:-0}" = "1" ]; then
      echo "MISSING_OK $script"
      return 200
    fi
    echo "missing helper script $script (set RELEASE_GATE_ALLOW_MISSING=1 to skip while it is pending)"
    return 1
  fi
  "$script" "$@"
}

benchmarks() {
  # scripts/benchmark.sh runs the tracked set against the frozen dataset and
  # compares median sec/op per benchmark with benchmarks/baseline.txt; it
  # exits non-zero above the threshold or when a tracked benchmark is missing.
  [ -f benchmarks/baseline.txt ] || { echo "no benchmarks/baseline.txt (run scripts/benchmark.sh baseline)"; return 1; }
  BENCH_PCT="${RELEASE_GATE_BENCH_PCT:-20}" scripts/benchmark.sh compare
}

script_self_tests() {
  # The gate's own helpers have tests; a comparator that mis-parses a column
  # or an installer that stops failing closed must not pass silently.
  tests/scripts/benchmark_compare_test.sh || return 1
  local pwsh="${PWSH:-pwsh}"
  if command -v "$pwsh" >/dev/null 2>&1; then
    PWSH="$pwsh" tests/scripts/install_ps1_test.sh || return 1
  else
    echo "install_ps1_test skipped: pwsh not found (set PWSH=/path/to/pwsh to include it)"
  fi
}

# Stages that wrap script_stage translate its MISSING_OK sentinel (200) into a skip.
run_script_stage() {
  local num="$1" name="$2" script="$3"; shift 3
  if [[ "$skip_list" == *" $num "* ]]; then
    printf '%-2s %-14s SKIPPED (RELEASE_GATE_SKIP)\n' "$num" "$name" | tee -a "$log"
    skipped+=("$num $name"); return 0
  fi
  if [ ! -x "$script" ] && [ "${RELEASE_GATE_ALLOW_MISSING:-0}" = "1" ]; then
    printf '%-2s %-14s SKIPPED (missing %s; RELEASE_GATE_ALLOW_MISSING=1)\n' "$num" "$name" "$script" | tee -a "$log"
    skipped+=("$num $name"); return 0
  fi
  stage "$num" "$name" script_stage "$script" "$@"
}

stage 1 gofmt check_gofmt
stage 2 build+vet build_and_vet
stage 3 unit-tests unit_tests
stage 4 e2e-tests e2e_tests
stage 5 docs-parity docs_parity
run_script_stage 6 action-pins scripts/check_action_pins.sh
run_script_stage 7 vendor-hashes scripts/verify_vendor.sh
if [[ "$skip_list" == *" 8 "* ]]; then
  printf '%-2s %-14s SKIPPED (RELEASE_GATE_SKIP)\n' 8 benchmarks | tee -a "$log"; skipped+=("8 benchmarks")
else
  stage 8 benchmarks benchmarks
fi
run_script_stage 9 robot-smoke scripts/robot_smoke.sh
if [[ "$skip_list" == *" 10 "* ]]; then
  printf '%-2s %-14s SKIPPED (RELEASE_GATE_SKIP)\n' 10 script-tests | tee -a "$log"; skipped+=("10 script-tests")
else
  stage 10 script-tests script_self_tests
fi

total=$(( $(date +%s) - gate_start ))
echo "----- release gate finished in ${total}s: ${#failed[@]} failed, ${#skipped[@]} skipped (log: $log)" | tee -a "$log"
if [ ${#skipped[@]} -gt 0 ]; then
  printf '  skipped: %s\n' "${skipped[@]}" | tee -a "$log"
fi
if [ ${#failed[@]} -gt 0 ]; then
  printf '  FAILED: %s\n' "${failed[@]}" | tee -a "$log"
  exit 1
fi
echo "RELEASE GATE PASSED" | tee -a "$log"
