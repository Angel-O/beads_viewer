# Vendored asset provenance

The static dashboard that `bv --export-pages` produces ships a set of
third-party JavaScript libraries, two WebAssembly modules, and two fonts from
`pkg/export/viewer_assets/vendor/`. They are embedded into the `bv` binary
(`pkg/export/viewer_embed.go`, `//go:embed viewer_assets`) and copied into
every exported bundle, so a tampered or silently replaced file would reach
every dashboard viewer. This document states how those files are tracked.

## The manifest

`pkg/export/viewer_assets/vendor/MANIFEST.json` lists every file with:

| Field | Meaning |
|-------|---------|
| `name` | File name inside the vendor directory |
| `upstream` | The project the file comes from |
| `version` | Version read from the artifact's own header or embedded strings; `unknown` when the artifact carries no marker (the hash still pins the exact bytes) |
| `license` | SPDX-style license of the upstream project |
| `sha256` | SHA-256 of the file as shipped |
| `source_url` | Where the file can be fetched or rebuilt from |
| `build_command` | `none (published build)` for upstream releases, or the command that produces the file from source |
| `reviewed_by` / `date` (top level) | Who last verified the entries and when |

The manifest is itself embedded, so an exported bundle carries its own
provenance record.

## Verification

Two checks read the manifest and recompute every hash:

- `scripts/verify_vendor.sh [dir]` fails on a hash mismatch, a listed file
  that is missing, or a file on disk that the manifest does not name. It is
  stage 7 of `scripts/release_gate.sh`.
- `pkg/export/vendor_manifest_test.go` performs the same check under
  `go test`, and additionally proves the check is not vacuous by verifying a
  copy with one flipped byte.

Replacing an asset therefore requires updating its manifest entry (hash,
version, source, date, reviewer) in the same change; the gate blocks
anything else.

## Rebuilding the in-repo WebAssembly

`bv_graph.js` and `bv_graph_bg.wasm` are built from `bv-graph-wasm/` (Rust)
with `cd bv-graph-wasm && make build-release`, which runs
`wasm-pack build --target web --release` and, when installed, `wasm-opt -Os`.

`scripts/build_graph_wasm.sh` is the pinned rebuild without `wasm-pack`: it
runs `cargo build --release --target wasm32-unknown-unknown` (crate versions
from `Cargo.lock`), refuses a `wasm-bindgen` CLI whose version differs from
the `wasm-bindgen` crate in `Cargo.lock` (0.2.121 today), runs `wasm-opt -Os`
when binaryen is present, and prints the built and vendored SHA-256 side by
side with every tool version so the outcome can be recorded here and in
`MANIFEST.json`. Run it once with network for `cargo fetch`, then `--offline`.
On 2026-09-03 it could not complete on the shared VM: the remote compilation
hook (`rch`) claims every `cargo build`, its workers lack the wasm target, and
its config refuses local fallback, so the comparison is still owed.

Reproducibility status (2026-09-02): a local `wasm-pack` rebuild on 2026-09-01
produced a different hash from the shipped `bv_graph_bg.wasm`
(`67c14abd…` versus `fb2c84ee…`), and `wasm-pack` is not installed on the
current reference machine, so the shipped module is **not yet reproducibly
tied to its source**. The manifest records this in the artifact's `version`
field rather than claiming otherwise. Closing that gap needs a pinned
`rust-toolchain.toml` and `wasm-pack` version in `bv-graph-wasm/`, a rebuild
whose hash matches the manifest (or a documented, accepted difference with
the builder's toolchain versions recorded), and a gate stage that rebuilds and
compares. Until then, treat `bv_graph_bg.wasm` as a reviewed binary whose
bytes are pinned by hash but whose source correspondence has not been
demonstrated.

## Adding or upgrading an asset

1. Fetch the release artifact from the upstream release page (never a
   mutable branch URL), or rebuild from source with the recorded command.
2. Compute `sha256sum` and update the manifest entry: version, license,
   source URL, hash, date, reviewer.
3. Run `scripts/verify_vendor.sh` and `go test ./pkg/export -run VendorManifest`.
4. Mention the upgrade in the change description with the upstream release
   notes link.
