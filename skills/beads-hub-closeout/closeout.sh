#!/usr/bin/env bash

set -euo pipefail

die() {
  printf 'closeout failed: %s\n' "$1" >&2
  exit 1
}

if [[ $# -ne 2 ]]; then
  printf 'usage: closeout.sh <private-work-item-id> <pr-selector>\n' >&2
  exit 2
fi

work_item=$1
pr_selector=$2
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P) || die 'cannot locate closeout skill'
repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || die 'not a Git checkout'

pr_json=$(gh pr view "$pr_selector" --json state,mergedAt,mergeCommit,baseRefName 2>/dev/null) ||
  die 'cannot read pull request'
pr_fields=$(jq -er -r '
  if type == "object" and
     (.state | type) == "string" and .state == "MERGED" and
     (.mergedAt | type) == "string" and (.mergedAt | length) > 0 and
     (.mergeCommit | type) == "object" and
     (.mergeCommit.oid | type) == "string" and
     (.mergeCommit.oid | test("^[0-9a-fA-F]{40}$")) and
     (.baseRefName | type) == "string" and (.baseRefName | length) > 0
  then [.state, .mergedAt, (.mergeCommit.oid | ascii_downcase), .baseRefName] | @tsv
  else error("malformed merged pull request")
  end
' <<<"$pr_json" 2>/dev/null) || die 'pull request is not a valid merged result'
IFS=$'\t' read -r pr_state merged_at merge_sha base_branch <<<"$pr_fields"
[[ $pr_state == MERGED && -n $merged_at && ${#merge_sha} -eq 40 && -n $base_branch ]] ||
  die 'pull request is not a valid merged result'
git check-ref-format "refs/heads/$base_branch" >/dev/null 2>&1 || die 'pull request base branch is invalid'

current_branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null) || die 'checkout must be on a branch'
[[ $current_branch == "$base_branch" ]] || die 'checkout is not on the pull request base branch'
[[ -z $(git status --porcelain=v1 --untracked-files=all) ]] || die 'checkout is not clean'

git fetch --no-tags origin "refs/heads/$base_branch" >/dev/null 2>&1 || die 'cannot fetch the pull request base branch'
git rev-parse --verify --quiet FETCH_HEAD^{commit} >/dev/null || die 'fetched base is not a commit'
git merge-base --is-ancestor HEAD FETCH_HEAD >/dev/null 2>&1 || die 'current HEAD is not in the fetched base history'
git merge-base --is-ancestor "$merge_sha" FETCH_HEAD >/dev/null 2>&1 || die 'merge result is not in the fetched base history'

"$script_dir/validate.sh" --metadata-range "$repo_root" HEAD FETCH_HEAD >/dev/null 2>&1 ||
  die 'Git metadata privacy validation failed'

show_json=$(wbd show "$work_item" --json 2>/dev/null) || die 'Hub work item lookup failed'
item_status=$(jq -er --arg item "$work_item" '
  if type == "array" and length == 1 and
     (.[0] | type) == "object" and .[0].id == $item and
     (.[0].issue_type | type) == "string" and
     (.[0].issue_type == "task" or .[0].issue_type == "bug" or
      .[0].issue_type == "feature" or .[0].issue_type == "chore") and
     (.[0].labels | type) == "array" and
     ([.[0].labels[] | select(type == "string" and startswith("ctx:"))] | length) == 1 and
     (.[0].status | type) == "string" and
     (.[0].status == "open" or .[0].status == "in_progress" or
      .[0].status == "blocked" or .[0].status == "deferred" or
      .[0].status == "closed")
  then .[0].status
  else error("ineligible Hub work item")
  end
' <<<"$show_json" 2>/dev/null) || die 'Hub work item is not an eligible concrete record'

wbd link "$work_item" "$merge_sha" >/dev/null 2>&1 || die 'Hub correlation failed'
if [[ $item_status != closed ]]; then
  wbd close "$work_item" --reason 'Merged and correlated' --json >/dev/null 2>&1 ||
    die 'Hub closure failed after correlation'
  closure='closed'
else
  closure='already closed'
fi

printf 'closeout complete: merge %s on base branch %s; item %s.\n' "$merge_sha" "$base_branch" "$closure"
