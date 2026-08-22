#!/usr/bin/env bash

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
policy=$repo_root/AGENTS.md
hub_skill=$repo_root/skills/beads-hub/SKILL.md
closeout_skill=$repo_root/skills/beads-hub-closeout/SKILL.md

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
  local kind=$1 value=$2
  if grep -Eq 'global-[a-z0-9][a-z0-9-]*|ctx:[a-z0-9][a-z0-9-]*' <<<"$value"; then
    printf 'private Hub identity detected in Git %s metadata\n' "$kind" >&2
    return 1
  fi
}

validate_repository_metadata() {
  local repository=$1 branch tags message
  branch=$(git -C "$repository" symbolic-ref --quiet --short HEAD 2>/dev/null || true)
  tags=$(git -C "$repository" tag --points-at HEAD)
  message=$(git -C "$repository" show -s --format='%s%n%b' HEAD)
  validate_metadata_value branch "$branch"
  validate_metadata_value tag "$tags"
  validate_metadata_value commit "$message"
}

# Keep the regression fixtures invented and out of diagnostics. The same
# function validates repository metadata below.
for fixture in 'docs/hub-closeout' 'release-candidate' 'fix(hub): harden closeout privacy'; do
  if ! validate_metadata_value synthetic "$fixture"; then
    printf '%s\n' 'ID-free synthetic metadata was incorrectly rejected' >&2
    exit 1
  fi
done
for fixture in 'feature/global-synthetic' 'ctx:sample-context-012345' 'fix: synthetic global-example'; do
  if validate_metadata_value synthetic "$fixture" >/dev/null 2>&1; then
    printf '%s\n' 'synthetic private metadata pattern was not detected' >&2
    exit 1
  fi
done

validate_repository_metadata "$repo_root"

require_text "$policy" 'Private Hub IDs and `ctx:` identities stay in private Hub operations'
require_text "$policy" 'never include private Hub identifiers'
require_text "$hub_skill" '[`beads-hub-closeout`](../beads-hub-closeout/SKILL.md)'
require_text "$closeout_skill" '^[0-9a-fA-F]{40}$'
require_text "$closeout_skill" 'git merge-base --is-ancestor "$merge_sha" FETCH_HEAD'
require_text "$closeout_skill" 'pull --ff-only "$remote" "$reference_branch"'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd show "$bead_id" --json)'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd close "$bead_id"'
require_text "$closeout_skill" 'It rejects private identity patterns in the active branch, any tags'
require_text "$closeout_skill" 'at `HEAD`, and the `HEAD` commit subject and body'
require_before "$closeout_skill" '## Synchronize Reference' 'pull --ff-only "$remote" "$reference_branch"'
require_before "$closeout_skill" 'git merge-base --is-ancestor "$merge_sha" FETCH_HEAD' 'pull --ff-only "$remote" "$reference_branch"'
require_before "$closeout_skill" 'Recheck that the reference checkout is still on the resolved branch and' 'pull --ff-only "$remote" "$reference_branch"'
require_before "$closeout_skill" 'pull --ff-only "$remote" "$reference_branch"' '## Revalidate And Close'
require_before "$closeout_skill" '(cd -- "$reference_worktree" && wbd show "$bead_id" --json)' '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_before "$closeout_skill" '## Revalidate And Close' '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_before "$closeout_skill" '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")' '(cd -- "$reference_worktree" && wbd close "$bead_id"'

if grep -Fq '| Commit messages | Include `br-###`' "$policy"; then
  printf '%s\n' 'conflicting issue-ID commit guidance remains' >&2
  exit 1
fi

printf '%s\n' 'private Hub closeout policy validation passed'
