#!/bin/bash

# Internal implementation shared by the repository-only Hub migration scripts.

hub_prefix_valid() {
  local prefix=$1
  case "$prefix" in *--*) return 1 ;; esac
  [ "${#prefix}" -le 32 ] && [[ $prefix =~ ^[a-z]([a-z0-9-]{0,30}[a-z0-9])?$ ]]
}

hub_prefix_prompt() {
  local default_prefix=$1 migration_command=$2 target_prefix
  printf '%s\n' \
    'Choose the store-wide prefix for every Beads ID in the Hub.' >&2
  printf 'You can change this prefix later by running %s.\n' "$migration_command" >&2
  while true; do
    printf 'Target Beads prefix [%s]: ' "$default_prefix" >&2
    if ! IFS= read -r target_prefix; then
      printf '\n' >&2
      printf '%s\n' 'beads-hub prefix migration: end of input while reading target prefix' >&2
      return 1
    fi
    target_prefix=${target_prefix:-$default_prefix}
    if hub_prefix_valid "$target_prefix"; then
      printf '%s\n' "$target_prefix"
      return 0
    fi
    printf '%s\n' \
      'Invalid prefix: use 1-32 lowercase ASCII letters, digits, or hyphens; start with a letter, end with a letter or digit, and do not use consecutive hyphens.' >&2
  done
}

hub_prefix_run_bd() {
  local store=$1
  shift
  env \
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
    bd --db "$store" "$@"
}

hub_prefix_detect() {
  local store=$1 output prefix
  output=$(hub_prefix_run_bd "$store" --json config get issue_prefix) || return
  prefix=$(printf '%s\n' "$output" | jq -er '
    if type == "object" and .key == "issue_prefix" and
       .schema_version == 1 and
       (.value | type) == "string" and (.value | length) > 0 and
       (keys | sort) == ["key", "schema_version", "value"]
    then .value
    else error("invalid issue_prefix response")
    end
  ') || return
  hub_prefix_valid "$prefix" || return 1
  printf '%s\n' "$prefix"
}

hub_prefix_validate_runtime_paths() {
  local store=$1 ledger=$2 interactions=$3 issues_export=$4 last_touched=$5 path label
  [ -d "$store" ] && [ ! -L "$store" ] || return 1
  for label in ledger interactions issues-export last-touched; do
    case "$label" in
      ledger) path=$ledger ;;
      interactions) path=$interactions ;;
      issues-export) path=$issues_export ;;
      last-touched) path=$last_touched ;;
    esac
    if [ -e "$path" ] || [ -L "$path" ]; then
      [ -f "$path" ] && [ ! -L "$path" ] || return 1
    fi
  done
}

hub_prefix_migrate() {
  local store=$1 ledger=$2 source_prefix=$3 target_prefix=$4
  local interactions=$5 issues_export=$6 last_touched=$7 tmp_dir=$8
  local ledger_tmp=$tmp_dir/correlations.jsonl
  local interactions_tmp=$tmp_dir/interactions.jsonl
  local last_touched_tmp=$tmp_dir/last-touched
  local last_touched_value
  local ledger_exists=false interactions_exists=false last_touched_rewrite=false

  hub_prefix_valid "$source_prefix" && hub_prefix_valid "$target_prefix" || return 1
  hub_prefix_validate_runtime_paths "$store" "$ledger" "$interactions" "$issues_export" "$last_touched" || return 1

  if [ -f "$ledger" ]; then
    ledger_exists=true
    jq -c --arg source_prefix "$source_prefix-" --arg target_prefix "$target_prefix-" '
      if (.bead_id | type) == "string" and (.bead_id | startswith($source_prefix)) then
        .bead_id = ($target_prefix + (.bead_id | ltrimstr($source_prefix)))
      else
        .
      end
    ' "$ledger" >"$ledger_tmp" || return
    chmod 0600 "$ledger_tmp"
  fi

  if [ -f "$interactions" ]; then
    interactions_exists=true
    jq -c --arg source_prefix "$source_prefix-" --arg target_prefix "$target_prefix-" '
      if (.issue_id | type) == "string" and (.issue_id | startswith($source_prefix)) then
        .issue_id = ($target_prefix + (.issue_id | ltrimstr($source_prefix)))
      else
        .
      end
    ' "$interactions" >"$interactions_tmp" || return
    chmod 0600 "$interactions_tmp"
  fi

  if [ -f "$last_touched" ]; then
    last_touched_value=$(<"$last_touched")
    case "$last_touched_value" in
      "$source_prefix"-?*)
        case "$last_touched_value" in
          *[[:space:]]*) ;;
          *) last_touched_rewrite=true ;;
        esac
        ;;
    esac
  fi
  if $last_touched_rewrite; then
    printf '%s-%s\n' "$target_prefix" "${last_touched_value#"$source_prefix"-}" >"$last_touched_tmp"
    chmod 0600 "$last_touched_tmp"
  fi

  local persisted_prefix
  persisted_prefix=$(hub_prefix_detect "$store") || return
  [ "$persisted_prefix" = "$source_prefix" ] || return 1
  hub_prefix_run_bd "$store" rename-prefix "$target_prefix" || return
  hub_prefix_run_bd "$store" export -o "$issues_export" || return

  if $ledger_exists; then
    mv "$ledger_tmp" "$ledger"
  fi
  if $interactions_exists; then
    mv "$interactions_tmp" "$interactions"
  fi
  if $last_touched_rewrite; then
    mv "$last_touched_tmp" "$last_touched"
  fi
}
