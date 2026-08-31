#!/usr/bin/env bash

set -euo pipefail

validate_metadata_value() {
  local kind=$1 value=$2 prefix=$3
  if grep -Eq "${prefix}-[a-z0-9][a-z0-9-]*|ctx:[a-z0-9][a-z0-9-]*" <<<"$value"; then
    printf 'private Hub identity detected in Git %s metadata\n' "$kind" >&2
    return 1
  fi
}

validate_repository_metadata() {
  local repository=$1 base=$2 tip=$3 prefix=$4 branch commits messages tags commit commit_tags
  branch=$(git -C "$repository" symbolic-ref --quiet --short HEAD 2>/dev/null || true)
  if [[ -z $branch ]]; then
    printf '%s\n' 'cannot validate Git metadata without an active branch' >&2
    return 1
  fi
  if [[ -z $base ]] || ! git -C "$repository" rev-parse --verify --quiet "$base^{commit}" >/dev/null ||
     ! git -C "$repository" rev-parse --verify --quiet "$tip^{commit}" >/dev/null; then
    printf '%s\n' 'cannot validate Git metadata without a valid range' >&2
    return 1
  fi
  if ! git -C "$repository" merge-base --is-ancestor "$base" "$tip" >/dev/null 2>&1; then
    printf '%s\n' 'metadata range base is not an ancestor of its tip' >&2
    return 1
  fi

  commits=$(git -C "$repository" rev-list --reverse "$base..$tip") || {
    printf '%s\n' 'cannot read the metadata commit range' >&2
    return 1
  }
  messages=$(git -C "$repository" log --format='%s%n%b' "$base..$tip") || {
    printf '%s\n' 'cannot read the metadata commit messages' >&2
    return 1
  }
  tags=
  while IFS= read -r commit; do
    [[ -n $commit ]] || continue
    commit_tags=$(git -C "$repository" tag --points-at "$commit") || {
      printf '%s\n' 'cannot read metadata tags' >&2
      return 1
    }
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
  if [[ $# -ne 5 ]] || ! hub_prefix_valid "$3"; then
    printf '%s\n' 'usage: validate.sh --metadata-only <repository> <issue-prefix> <base> <tip>' >&2
    exit 2
  fi
  validate_repository_metadata "$2" "$4" "$5" "$3"
  printf '%s\n' 'Git metadata privacy validation passed'
  exit 0
fi

if [[ ${1:-} == "--metadata-range" ]]; then
  if [[ $# -ne 4 ]]; then
    printf '%s\n' 'usage: validate.sh --metadata-range <repository> <base> <tip>' >&2
    exit 2
  fi
  hub_prefix=$(detect_hub_prefix)
  validate_repository_metadata "$2" "$3" "$4" "$hub_prefix"
  printf '%s\n' 'Git metadata privacy validation passed'
  exit 0
fi

repo_root=$(git rev-parse --show-toplevel)
hub_prefix=$(detect_hub_prefix)

base=${CLOSEOUT_METADATA_BASE:-$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)}
tip=${CLOSEOUT_METADATA_TIP:-HEAD}
validate_repository_metadata "$repo_root" "$base" "$tip" "$hub_prefix"

printf '%s\n' 'private Hub closeout privacy validation passed'
