#!/usr/bin/env bash
# build_graph_wasm.sh: rebuild the vendored graph WASM from bv-graph-wasm/
# using the two tools wasm-pack wraps, so the build is pinned and inspectable:
#   1. cargo build --release --target wasm32-unknown-unknown   (Cargo.lock pins crates)
#   2. wasm-bindgen --target web --out-name bv_graph           (CLI version must equal
#                                                               the wasm-bindgen crate in Cargo.lock)
#   3. wasm-opt -Os                                             (binaryen; optional)
# It writes bv-graph-wasm/pkg/{bv_graph.js,bv_graph_bg.wasm} (git-ignored) and
# prints the SHA-256 of the result next to the vendored file's, plus every
# tool version, so the comparison can be recorded in vendor/MANIFEST.json.
#
# Usage: scripts/build_graph_wasm.sh [--no-opt] [--offline]
# Env:   WASM_BINDGEN=/path/to/wasm-bindgen (default: on PATH)
#        WASM_OPT=/path/to/wasm-opt         (default: on PATH; skipped if absent)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
crate="$root/bv-graph-wasm"
vendored="$root/pkg/export/viewer_assets/vendor/bv_graph_bg.wasm"
out="$crate/pkg"
opt=1
offline=""
for arg in "$@"; do
  case "$arg" in
    --no-opt) opt=0 ;;
    --offline) offline="--offline" ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

bindgen="${WASM_BINDGEN:-wasm-bindgen}"
wasmopt="${WASM_OPT:-wasm-opt}"
command -v cargo >/dev/null || { echo "cargo not found"; exit 2; }
command -v "$bindgen" >/dev/null || { echo "wasm-bindgen CLI not found (set WASM_BINDGEN)"; exit 2; }

want_bindgen="$(awk '/^name = "wasm-bindgen"$/{getline; sub(/version = "/, "", $0); sub(/"$/, "", $0); print; exit}' "$crate/Cargo.lock")"
have_bindgen="$("$bindgen" --version | awk '{print $2}')"
if [ "$want_bindgen" != "$have_bindgen" ]; then
  echo "wasm-bindgen CLI $have_bindgen does not match the crate's wasm-bindgen $want_bindgen (Cargo.lock); the generated glue would not match" >&2
  exit 2
fi

echo "== toolchain"
echo "rustc:        $(rustc --version)"
echo "cargo:        $(cargo --version)"
echo "wasm-bindgen: $have_bindgen"
if [ "$opt" -eq 1 ] && command -v "$wasmopt" >/dev/null; then
  echo "wasm-opt:     $("$wasmopt" --version)"
else
  echo "wasm-opt:     (not run)"
  opt=0
fi

echo "== cargo build"
(cd "$crate" && cargo build --release --target wasm32-unknown-unknown $offline 2>&1 | tail -3)
raw="$crate/target/wasm32-unknown-unknown/release/bv_graph_wasm.wasm"
[ -f "$raw" ] || { echo "expected $raw after cargo build"; exit 1; }

echo "== wasm-bindgen"
mkdir -p "$out"
"$bindgen" --target web --out-dir "$out" --out-name bv_graph "$raw"
built="$out/bv_graph_bg.wasm"

if [ "$opt" -eq 1 ]; then
  echo "== wasm-opt -Os"
  "$wasmopt" -Os -o "$out/bv_graph_bg.opt.wasm" "$built"
  mv "$out/bv_graph_bg.opt.wasm" "$built"
fi

echo "== result"
printf 'built:    %s  %s bytes  %s\n' "$(sha256sum "$built" | cut -d' ' -f1)" "$(wc -c < "$built" | tr -d ' ')" "$built"
if [ -f "$vendored" ]; then
  printf 'vendored: %s  %s bytes  %s\n' "$(sha256sum "$vendored" | cut -d' ' -f1)" "$(wc -c < "$vendored" | tr -d ' ')" "$vendored"
  if cmp -s "$built" "$vendored"; then
    echo "REPRODUCIBLE: built artifact is byte-identical to the vendored file"
  else
    echo "DIFFERENT: built artifact differs from the vendored file (record both hashes and this toolchain in vendor/MANIFEST.json)"
  fi
fi
