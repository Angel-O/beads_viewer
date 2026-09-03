# Releasing bv

The release gate is the only definition of "shippable". Nothing is tagged,
published, or pushed to the Homebrew tap or Scoop bucket unless
`scripts/release_gate.sh` passed on the exact commit being released.

## The gate

```bash
scripts/release_gate.sh                     # everything, ~12 minutes on the reference machine (8 of them stage 8)
RELEASE_GATE_SKIP="8" scripts/release_gate.sh   # ~4 minutes: skip the benchmark comparison for quick loops (see below)
```

Stages, in order: `gofmt`, `go build` + `go vet`, unit tests with `-race`,
e2e tests with `-race`, docs parity (`go generate` must not change the
tree), GitHub Actions pin check (`scripts/check_action_pins.sh`), vendored
asset hashes (`scripts/verify_vendor.sh` against `MANIFEST.json`), benchmark
comparison (`scripts/benchmark.sh compare` against `benchmarks/baseline.txt`,
fails above 20% best-of-N sec/op regression on any tracked benchmark), and the
robot smoke (`scripts/robot_smoke.sh`: every robot command on this repository
and on a synthetic fixture).

Each stage prints its duration; the full log lands in
`tests/artifacts/release_gate_<timestamp>.log` (git-ignored). A failed stage
prints its last 25 log lines. `RELEASE_GATE_SKIP="n m"` skips stages by
number and says so in the summary; `RELEASE_GATE_ALLOW_MISSING=1` turns a
helper script that does not exist yet into a logged skip instead of a
failure. Skips are visible in the summary line, so a "passed" gate with
skips is not the same as a clean pass.

Stage 8 has no dependency outside the Go toolchain: `scripts/benchmark.sh`
runs the ten tracked benchmarks (`BenchmarkRealData_*`, `BenchmarkFullAnalysis_*`,
`BenchmarkSnapshotSwap`, `BenchmarkKeyPressLatency`, `BenchmarkListItemBuild`,
`BenchmarkParseIssuesPoolComparison`) against the frozen dataset
`tests/testdata/benchmark/medium.jsonl`, four rounds per package alternating
between the baseline commit's tree and HEAD with the pair order swapped each
round (always running one tree first biased identical code by 10-20%), and
compares the best observed `ns/op` of each side (contention only inflates
samples, so the minimum is the closest to the uncontended time). The stored
`benchmarks/baseline.txt` carries a
provenance header (date, Go version, CPU, OS, commit, dataset hash) and is
regenerated only on the reference machine with `scripts/benchmark.sh baseline`.
`BENCH_PCT` (gate: `RELEASE_GATE_BENCH_PCT`) sets the threshold;
`tests/scripts/benchmark_compare_test.sh` proves the comparison turns red on a
benchmark doubled in every sample, stays green on one contended sample, and
turns red on a missing benchmark. Because a stored baseline cannot
tell host drift from a code regression on a shared machine (on 2026-09-03 the
same code read +38% against the stored file and 0% against a fresh build of
the baseline commit), `compare` first builds and runs the tracked set for the
commit named in the baseline header, in a detached worktree, and judges HEAD
against that contemporaneous run; the stored file is the fallback when that
commit is not in the clone (`BENCH_REFERENCE=stored` forces it). A stage-8
failure is therefore a code regression relative to the baseline commit, not
a busy host, and is never a licence to raise the threshold.

## Where the gate runs

- **Locally, before every release.** This is mandatory and is the step that
  actually protects releases today, because the GitHub Actions workflows are
  disabled (`gh workflow list --all` shows `disabled_manually` for CI,
  Release, Fuzz, and Flake Update since 2026-08-16).
- **In CI, once re-enabled.** `.github/workflows/ci.yml` already runs the
  same script (`RELEASE_GATE_SKIP="8" RELEASE_GATE_ALLOW_MISSING=1`) and
  uploads the gate log as an artifact, so re-enabling the CI workflow makes
  the local and remote checks identical. Whether to re-enable is the
  maintainer's call: it costs Actions minutes and Codecov uploads, and it
  is the only way the README badge becomes live again.

## Release steps

1. Confirm the working tree is clean and on `main` at the commit to release.
2. Run `scripts/release_gate.sh` and keep the log.
3. Update `CHANGELOG.md` (move the `Unreleased` items under the new version
   with the date) and commit.
4. Tag `vX.Y.Z` and build the artifacts with goreleaser
   (`goreleaser release --clean`, or the maintainer's local release tool);
   the archive names, `checksums.txt`, manifest, and SBOM come from
   `.goreleaser.yaml`.
5. Publish the GitHub Release, then push `main` to `master` as AGENTS.md
   requires.
6. Verify the published artifacts once: `install.sh` on a clean machine
   (it verifies `checksums.txt` and fails closed), `bv --version`, and
   `bv --robot-capabilities | jq .version`.

## What is not covered

- The gate does not run on Windows. `install.ps1` installs the
  checksum-verified release zip and `tests/scripts/install_ps1_test.sh`
  proves it fails closed, but that harness has only run under PowerShell 7 on
  Linux; step 6 below should include one Windows run of the real release.
- The gate has no browser. `scripts/dashboard_browser_smoke.sh` loads the
  exported dashboard in a headless Chromium (`BV_HEADLESS_BROWSER=/path/to/chrome`)
  and fails on any Content-Security-Policy refusal or uncaught error while
  requiring the app's boot markers; run it before a release when a Chromium
  is at hand, and always after touching `index.html`, `head_init.js`, or
  the CSP.
- The vendored `bv_graph_bg.wasm` is pinned by hash but not yet rebuilt
  reproducibly from source (`docs/PROVENANCE.md`).
