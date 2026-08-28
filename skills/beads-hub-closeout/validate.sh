#!/usr/bin/env bash

set -euo pipefail

require_text() {
  local file=$1 text=$2
  if ! grep -Fq -- "$text" "$file"; then
    printf 'missing required closeout policy in %s: %s\n' "$file" "$text" >&2
    return 1
  fi
}

require_before() {
  local file=$1 earlier=$2 later=$3 earlier_match later_match earlier_line later_line
  earlier_match=$(grep -nFm1 -- "$earlier" "$file") || return 1
  later_match=$(grep -nFm1 -- "$later" "$file") || return 1
  earlier_line=${earlier_match%%:*}
  later_line=${later_match%%:*}
  if (( earlier_line >= later_line )); then
    printf 'closeout ordering violation in %s: %s must precede %s\n' "$file" "$earlier" "$later" >&2
    return 1
  fi
}

validate_metadata_value() {
  local kind=$1 value=$2 prefix=$3
  if grep -Eq "${prefix}-[a-z0-9][a-z0-9-]*|ctx:[a-z0-9][a-z0-9-]*" <<<"$value"; then
    printf 'private Hub identity detected in Git %s metadata\n' "$kind" >&2
    return 1
  fi
}

validate_repository_metadata() {
  local repository=$1 base=${2:-} prefix=$3 branch commits messages tags commit commit_tags
  branch=$(git -C "$repository" symbolic-ref --quiet --short HEAD 2>/dev/null || true)
  if [[ -z $branch ]]; then
    printf '%s\n' 'cannot validate Git metadata without an active branch' >&2
    return 1
  fi
  if [[ -z $base ]]; then
    base=${CLOSEOUT_METADATA_BASE:-}
  fi
  if [[ -z $base ]]; then
    base=$(git -C "$repository" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)
  fi
  if [[ -z $base ]] || ! git -C "$repository" rev-parse --verify --quiet "$base^{commit}" >/dev/null; then
    printf '%s\n' 'cannot validate Git metadata without a valid upstream or reference' >&2
    return 1
  fi
  if ! git -C "$repository" merge-base --is-ancestor "$base" HEAD; then
    printf '%s\n' 'configured metadata reference is not an ancestor of HEAD' >&2
    return 1
  fi

  commits=$(git -C "$repository" rev-list --reverse "$base..HEAD")
  messages=$(git -C "$repository" log --format='%s%n%b' "$base..HEAD")
  tags=
  while IFS= read -r commit; do
    [[ -n $commit ]] || continue
    commit_tags=$(git -C "$repository" tag --points-at "$commit")
    if [[ -n $commit_tags ]]; then
      tags+=$'\n'$commit_tags
    fi
  done <<<"$commits"

  validate_metadata_value branch "$branch" "$prefix"
  validate_metadata_value tag "$tags" "$prefix"
  validate_metadata_value commit "$messages" "$prefix"
}

hub_prefix_valid() {
  local prefix=$1
  [[ ${#prefix} -le 32 && $prefix =~ ^[a-z]([a-z0-9-]{0,30}[a-z0-9])?$ && $prefix != *--* ]]
}

detect_hub_prefix() {
  local store output prefix
  store=${HOME:?HOME must be set}/.local/share/beads/hub/.beads
  for command in bd jq; do
    if ! command -v "$command" >/dev/null 2>&1; then
      printf 'cannot detect the Hub issue prefix: %s is required\n' "$command" >&2
      return 1
    fi
  done
  output=$(env \
    -u BD_DB \
    -u BEADS_DB \
    -u BD_GLOBAL \
    -u BEADS_DOLT_DATA_DIR \
    -u BEADS_DOLT_PORT \
    -u BEADS_DOLT_PROXIED_SERVER \
    -u BEADS_DOLT_SERVER_DATABASE \
    -u BEADS_DOLT_SERVER_HOST \
    -u BEADS_DOLT_SERVER_MODE \
    -u BEADS_DOLT_SERVER_PORT \
    -u BEADS_DOLT_SERVER_SOCKET \
    -u BEADS_DOLT_SHARED_SERVER \
    BEADS_DIR="$store" \
    bd --db "$store" --json config get issue_prefix 2>/dev/null) || {
      printf '%s\n' 'cannot detect the configured Hub issue prefix' >&2
      return 1
    }
  prefix=$(printf '%s\n' "$output" | jq -er '
    if type == "object" and .key == "issue_prefix" and
       .schema_version == 1 and
       (.value | type) == "string" and (.value | length) > 0 and
       (keys | sort) == ["key", "schema_version", "value"]
    then .value
    else error("invalid issue_prefix response")
    end
  ') || {
    printf '%s\n' 'cannot parse the configured Hub issue prefix' >&2
    return 1
  }
  if ! hub_prefix_valid "$prefix"; then
    printf '%s\n' 'the configured Hub issue prefix is invalid' >&2
    return 1
  fi
  printf '%s\n' "$prefix"
}

if [[ ${1:-} == "--metadata-only" ]]; then
  if [[ $# -lt 3 || $# -gt 4 ]] || ! hub_prefix_valid "$3"; then
    printf '%s\n' 'usage: validate.sh --metadata-only <repository> <issue-prefix> [reference]' >&2
    exit 2
  fi
  validate_repository_metadata "$2" "${4:-}" "$3"
  printf '%s\n' 'Git metadata privacy validation passed'
  exit 0
fi

repo_root=$(git rev-parse --show-toplevel)
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
skills_root=$(dirname -- "$script_dir")
hub_skill=$skills_root/beads-hub/SKILL.md
closeout_skill=$script_dir/SKILL.md
hub_prefix=$(detect_hub_prefix)

validate_repository_metadata "$repo_root" "" "$hub_prefix"

require_text "$hub_skill" '[`beads-hub-closeout`](../beads-hub-closeout/SKILL.md)'
require_text "$closeout_skill" '^[0-9a-fA-F]{40}$'
require_text "$closeout_skill" 'git merge-base --is-ancestor "$merge_sha" FETCH_HEAD'
require_text "$closeout_skill" 'pull --ff-only "$remote" "$reference_branch"'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd show "$bead_id" --json)'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd close "$bead_id"'
require_text "$closeout_skill" 'It reads the persisted Hub issue prefix at'
require_text "$closeout_skill" 'runtime and rejects that prefix and `ctx:` identities in the active branch'
require_before "$closeout_skill" '## Synchronize Reference' 'pull --ff-only "$remote" "$reference_branch"'
require_before "$closeout_skill" 'git merge-base --is-ancestor "$merge_sha" FETCH_HEAD' 'pull --ff-only "$remote" "$reference_branch"'
require_before "$closeout_skill" 'Recheck that the reference checkout is still on the resolved branch and' 'pull --ff-only "$remote" "$reference_branch"'
require_before "$closeout_skill" 'pull --ff-only "$remote" "$reference_branch"' '## Revalidate And Close'
require_before "$closeout_skill" '(cd -- "$reference_worktree" && wbd show "$bead_id" --json)' '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_before "$closeout_skill" '## Revalidate And Close' '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_before "$closeout_skill" '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")' '(cd -- "$reference_worktree" && wbd close "$bead_id"'

printf '%s\n' 'private Hub closeout policy validation passed'
