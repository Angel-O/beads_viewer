#!/bin/bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
# shellcheck source=beads-hub-prefix-internal.sh
. "$script_dir/beads-hub-prefix-internal.sh"

parent=$HOME/.local/share/beads/hub
store=$parent/.beads
ledger=$parent/correlations.jsonl
interactions=$store/interactions.jsonl
issues_export=$store/issues.jsonl
last_touched=$store/last-touched
config=$HOME/.config/bv/hub.yaml

die() {
  printf 'migrate-beads-hub-prefix: %s\n' "$*" >&2
  exit 1
}

command -v bd >/dev/null 2>&1 || die 'required command not found: bd'
command -v jq >/dev/null 2>&1 || die 'required command not found: jq'
[ -d "$store" ] && [ ! -L "$parent" ] || die "Hub store is missing or invalid: $store"
[ -f "$config" ] && [ ! -L "$config" ] || die "Hub config is missing or invalid: $config"
hub_prefix_validate_runtime_paths "$store" "$ledger" "$interactions" "$issues_export" "$last_touched" ||
  die 'a fixed Hub runtime path is invalid or symlinked'
jq -e --arg store "$store" --arg ledger "$ledger" '
  type == "object" and .version == 1 and .store == $store and .ledger == $ledger and
  (.repositories | type) == "object"
' "$config" >/dev/null || die "Hub config does not identify the fixed Hub paths: $config"

current_prefix=$(hub_prefix_detect "$store") || die 'cannot determine one valid persisted issue prefix'
target_prefix=$(hub_prefix_prompt "$current_prefix" migrate-beads-hub-prefix.sh) || exit 1
if [ "$target_prefix" = "$current_prefix" ]; then
  printf 'Hub prefix is already %s; no changes made.\n' "$current_prefix"
  exit 0
fi

confirmed_prefix=$(hub_prefix_detect "$store") || die 'cannot re-confirm the persisted issue prefix'
[ "$confirmed_prefix" = "$current_prefix" ] || die 'persisted issue prefix changed before backup'

umask 077
backup_base=$HOME/.local/share/beads/hub-prefix-backup-$(date +%Y%m%d%H%M%S)
backup=$backup_base
backup_index=0
while [ -e "$backup" ] || [ -L "$backup" ]; do
  backup_index=$((backup_index + 1))
  backup=$backup_base-$backup_index
done
mkdir "$backup"
cp -R "$parent" "$backup/hub"
cp "$config" "$backup/hub.yaml"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/migrate-beads-hub-prefix.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT
hub_prefix_migrate \
  "$store" "$ledger" "$current_prefix" "$target_prefix" \
  "$interactions" "$issues_export" "$last_touched" "$tmp_dir" ||
  die 'prefix migration failed; the pre-migration backup was preserved'

printf 'Migrated Hub prefix from %s-* to %s-*\n' "$current_prefix" "$target_prefix"
printf 'Backup: %s\n' "$backup"
printf '%s\n' 'Run migrate-beads-hub-prefix.sh again to change the Hub prefix later.'
