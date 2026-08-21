#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_bin="$(mktemp -d "${TMPDIR:-/tmp}/wbv-local.XXXXXX")"

(
    cd "$repo_root"
    go build -o "$test_bin/bv" ./cmd/bv
    go build -o "$test_bin/wbd" ./cmd/wbd
    go build -o "$test_bin/wbv" ./cmd/wbv
)

PATH="$test_bin:$PATH" "$test_bin/wbv" --hub "$@"
