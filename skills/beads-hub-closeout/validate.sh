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

# Exercise the privacy pattern with invented values only; real Hub identities
# must never be added to tracked validation data.
synthetic_metadata='feature/global-synthetic ctx:sample-context-012345'
if ! grep -Eq 'global-[a-z0-9-]+|ctx:[a-z0-9-]+' <<<"$synthetic_metadata"; then
  printf '%s\n' 'synthetic private metadata pattern was not detected' >&2
  exit 1
fi
if grep -Eq 'global-[a-z0-9-]+|ctx:[a-z0-9-]+' <<<'docs/hub-closeout'; then
  printf '%s\n' 'ID-free metadata was incorrectly rejected' >&2
  exit 1
fi

require_text "$policy" 'Private Hub IDs and `ctx:` identities stay in private Hub operations'
require_text "$policy" 'never include private Hub identifiers'
require_text "$hub_skill" '[`beads-hub-closeout`](../beads-hub-closeout/SKILL.md)'
require_text "$closeout_skill" '^[0-9a-fA-F]{40}$'
require_text "$closeout_skill" 'git merge-base --is-ancestor "$merge_sha" FETCH_HEAD'
require_text "$closeout_skill" 'pull --ff-only "$remote" "$reference_branch"'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd show "$bead_id" --json)'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")'
require_text "$closeout_skill" '(cd -- "$reference_worktree" && wbd close "$bead_id"'
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
