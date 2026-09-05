#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'qa-viewer-active-scope: %s\n' "$*" >&2
  exit 1
}

[ "$#" -ge 2 ] && [ "$#" -le 4 ] || die "usage: $0 <beads-checkout> <viewer-checkout> [beads-branch] [viewer-branch]"

beads_root=$(CDPATH= cd -- "$1" && pwd)
viewer_root=$(CDPATH= cd -- "$2" && pwd)
beads_branch=${3:-${BEADS_BRANCH:-scopes-integration}}
viewer_branch=${4:-${VIEWER_BRANCH:-scopes-integration}}

for command in git go jq; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
[ -f "$beads_root/go.mod" ] || die "not a Beads checkout: $beads_root"
[ -f "$viewer_root/go.mod" ] || die "not a Viewer checkout: $viewer_root"

check_branch() {
  local root=$1 branch=$2
  [ "$(git -C "$root" branch --show-current)" = "$branch" ] ||
    die "$root is not on branch $branch; this script never switches branches"
  git -C "$root" rev-parse --verify "refs/heads/$branch" >/dev/null ||
    die "branch $branch is not present in $root"
}

check_clean() {
  local root=$1
  [ -z "$(git -C "$root" status --porcelain --untracked-files=all)" ] ||
    die "$root has dirty or untracked files; this script refuses to build it"
}

check_branch "$beads_root" "$beads_branch"
check_branch "$viewer_root" "$viewer_branch"
check_clean "$beads_root"
check_clean "$viewer_root"

qa_root=$(mktemp -d "${TMPDIR:-/tmp}/bv-active-scope-qa.XXXXXX")
trap 'rm -rf "$qa_root"' EXIT
bin_dir=$qa_root/bin
fixture=$qa_root/fixture
mkdir -p "$bin_dir" "$fixture" "$qa_root/home"

printf 'Building exact integration branches:\n'
printf '  Beads  %s (%s)\n' "$(git -C "$beads_root" rev-parse --short HEAD)" "$beads_branch"
printf '  Viewer %s (%s)\n' "$(git -C "$viewer_root" rev-parse --short HEAD)" "$viewer_branch"
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

export HOME=$qa_root/home
export PATH="$bin_dir:$PATH"
unset BEADS_DB BD_DB BEADS_DIR BV_WBV_HUB_MODE BV_WBV_HUB_SCOPE

(
  cd "$fixture"
  bd init --prefix qa --non-interactive --skip-hooks --skip-agents >/dev/null
  wbd bootstrap --prefix qa >/dev/null
	bead_id=$(wbd create 'Active scope Viewer QA' --type todo --contextless --json | jq -er 'if type == "array" then .[0].id else .id end')
  scope_id=$(wbd scope create 'qa-scope' 'Active scope QA' --activate --json | jq -er 'if type == "array" then .[0].id else .id end')
  wbd scope add "$bead_id" --scope "$scope_id" --json >/dev/null
  printf 'Prepared disposable active scope %s with member %s\n' "$scope_id" "$bead_id"
)

printf '\nRobot smoke (uses the same bounded active-scope snapshot as Viewer):\n'
(cd "$fixture" && wbv --hub --robot-plan)

printf '\nLaunching interactive Viewer scope QA with isolated binaries and HOME:\n'
printf '  PATH=%s wbv --hub\n' "$bin_dir"
(cd "$fixture" && wbv --hub)
