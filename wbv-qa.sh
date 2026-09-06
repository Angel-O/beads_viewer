#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'wbv-qa: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 0 ] || die 'this script does not accept arguments'

viewer_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
beads_root=$(CDPATH= cd -- "$viewer_root/../../beads/integration-backlog-backend" 2>/dev/null && pwd) ||
  die "Beads checkout not found: $viewer_root/../../beads/integration-backlog-backend"

check_branch() {
  local root=$1 branch=$2
  [ "$(git -C "$root" branch --show-current)" = "$branch" ] ||
    die "$root is not on branch $branch"
  git -C "$root" rev-parse --verify "refs/heads/$branch" >/dev/null 2>&1 ||
    die "branch $branch is not present in $root"
}

check_clean() {
  local root=$1
  [ -z "$(git -C "$root" status --porcelain --untracked-files=all)" ] ||
    die "$root has dirty or untracked files"
}

check_branch "$viewer_root" integration/backlog-viewer
check_branch "$beads_root" integration/backlog-backend
check_clean "$viewer_root"
check_clean "$beads_root"

bin_dir=$(mktemp -d "${TMPDIR:-/tmp}/wbv-qa.XXXXXX")
trap 'rm -rf "$bin_dir"' EXIT

(
  cd "$beads_root"
  CGO_ENABLED=1 go build -tags=gms_pure_go -o "$bin_dir/bd" ./cmd/bd
)
(
  cd "$viewer_root"
  go build -o "$bin_dir/bv" ./cmd/bv
  go build -o "$bin_dir/wbd" ./cmd/wbd
  go build -o "$bin_dir/wbv" ./cmd/wbv
)

export PATH="$bin_dir:$PATH"
cd "$viewer_root"
wbv --hub
