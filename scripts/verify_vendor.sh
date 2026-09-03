#!/usr/bin/env bash
# verify_vendor.sh: every file under pkg/export/viewer_assets/vendor/ must be
# listed in MANIFEST.json with a matching sha256, and every manifest entry
# must exist. Exit 1 on any mismatch, unlisted file, or missing file.
# Run by scripts/release_gate.sh (stage 7); pkg/export/vendor_manifest_test.go
# performs the same check in Go so `go test` catches drift too.
#
# Usage: scripts/verify_vendor.sh [vendor-dir]   (default: pkg/export/viewer_assets/vendor)
set -euo pipefail

cd "$(dirname "$0")/.."
dir="${1:-pkg/export/viewer_assets/vendor}"
manifest="$dir/MANIFEST.json"
[ -f "$manifest" ] || { echo "verify_vendor: missing $manifest" >&2; exit 1; }
command -v jq >/dev/null || { echo "verify_vendor: jq is required" >&2; exit 2; }

problems=0
listed=0
while IFS=$'\t' read -r name want; do
  listed=$((listed + 1))
  path="$dir/$name"
  if [ ! -f "$path" ]; then
    echo "MISSING: $name is in the manifest but not on disk"
    problems=$((problems + 1)); continue
  fi
  got=$(sha256sum "$path" | awk '{print $1}')
  if [ "$got" != "$want" ]; then
    echo "MISMATCH: $name sha256 $got != manifest $want"
    problems=$((problems + 1))
  fi
done < <(jq -r '.files[] | [.name, .sha256] | @tsv' "$manifest")

while IFS= read -r f; do
  base=$(basename "$f")
  [ "$base" = "MANIFEST.json" ] && continue
  if ! jq -e --arg n "$base" '.files[] | select(.name == $n)' "$manifest" >/dev/null; then
    echo "UNLISTED: $base is on disk but not in the manifest"
    problems=$((problems + 1))
  fi
done < <(find "$dir" -maxdepth 1 -type f | sort)

echo "verify_vendor: $listed manifest entries checked, $problems problem(s)"
[ "$problems" -eq 0 ]
