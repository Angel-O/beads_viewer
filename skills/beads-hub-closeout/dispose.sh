#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf 'usage: dispose.sh <recorded-branch>\n' >&2
  exit 2
fi

branch=$1
[[ $branch != -* ]] || {
  printf '%s\n' 'dispose failed: recorded branch is invalid' >&2
  exit 2
}
git check-ref-format "refs/heads/$branch" >/dev/null 2>&1 || {
  printf '%s\n' 'dispose failed: recorded branch is invalid' >&2
  exit 2
}

if ! git wtrm "$branch"; then
  printf '%s\n' 'dispose failed: worktree removal failed; worktree and local branch were retained' >&2
  exit 1
fi

if ! git branch -d "$branch"; then
  printf '%s\n' 'dispose partial success: worktree removed; local branch was retained' >&2
  exit 1
fi

printf '%s\n' 'dispose complete: recorded worktree and local branch removed'
