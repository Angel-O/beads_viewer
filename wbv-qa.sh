#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'wbv-qa: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 0 ] || die 'this script does not accept arguments'

viewer_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
viewer_branch=local-integration/tui-scope-lookup
beads_branch=delivery/named-scope-detail
beads_root="$viewer_root/../../beads/named-scope-detail"
[ -e "$beads_root/.git" ] || die "Beads checkout not found: $beads_root"

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

check_branch "$viewer_root" "$viewer_branch"
check_branch "$beads_root" "$beads_branch"
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
