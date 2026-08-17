#!/bin/bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
# shellcheck source=beads-hub-prefix-internal.sh
. "$script_dir/beads-hub-prefix-internal.sh"

old_parent=$HOME/.local/share/beads/work
old_store=$old_parent/.beads
old_ledger=$old_parent/correlations.jsonl
issues_export=$old_store/issues.jsonl
interactions=$old_store/interactions.jsonl
last_touched=$old_store/last-touched
old_config=$HOME/.config/bv/work-beads.yaml
new_parent=$HOME/.local/share/beads/hub
new_store=$new_parent/.beads
new_ledger=$new_parent/correlations.jsonl
new_config=$HOME/.config/bv/hub.yaml
backup=$HOME/.local/share/beads/work-to-hub-backup-$(date +%Y%m%d%H%M%S)

die() {
  printf 'migrate-beads-work-to-hub: %s\n' "$*" >&2
  exit 1
}

command -v bd >/dev/null 2>&1 || die 'required command not found: bd'
command -v jq >/dev/null 2>&1 || die 'required command not found: jq'
[ -d "$old_store" ] && [ ! -L "$old_parent" ] || die "legacy store is missing or invalid: $old_store"
[ -f "$old_config" ] && [ ! -L "$old_config" ] || die "legacy config is missing or invalid: $old_config"
hub_prefix_validate_runtime_paths "$old_store" "$old_ledger" "$interactions" "$issues_export" "$last_touched" ||
  die 'a fixed legacy runtime path is invalid or symlinked'
[ ! -e "$new_parent" ] && [ ! -L "$new_parent" ] || die "hub destination already exists: $new_parent"
[ ! -e "$new_config" ] && [ ! -L "$new_config" ] || die "hub config already exists: $new_config"
[ ! -e "$backup" ] || die "backup destination already exists: $backup"

source_prefix=$(hub_prefix_detect "$old_store") || die 'cannot determine one valid persisted legacy issue prefix'
[ "$source_prefix" = work ] || die "legacy store prefix must be work, found: $source_prefix"

target_prefix=$(hub_prefix_prompt bead migrate-beads-hub-prefix.sh) || exit 1

confirmed_prefix=$(hub_prefix_detect "$old_store") || die 'cannot re-confirm the persisted legacy issue prefix'
[ "$confirmed_prefix" = work ] || die 'persisted legacy issue prefix changed before backup'

umask 077
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/migrate-beads-work-to-hub.XXXXXX")
config_tmp=$tmp_dir/hub.yaml
trap 'rm -rf "$tmp_dir"' EXIT

jq -e \
  --arg old_store "$old_store" \
  --arg old_ledger "$old_ledger" \
  --arg store "$new_store" \
  --arg ledger "$new_ledger" '
  if type != "object" or .version != 1 or
     .store != $old_store or .ledger != $old_ledger or
     (.repositories | type) != "object" then
    error("invalid legacy config")
  else
    .store = $store | .ledger = $ledger
  end
' "$old_config" >"$config_tmp" || die "cannot rewrite legacy config: $old_config"

mkdir "$backup"
cp -R "$old_parent" "$backup/work"
cp "$old_config" "$backup/work-beads.yaml"

hub_prefix_migrate \
  "$old_store" "$old_ledger" work "$target_prefix" \
  "$interactions" "$issues_export" "$last_touched" "$tmp_dir" ||
  die 'prefix migration failed; the pre-migration backup was preserved'
mv "$old_parent" "$new_parent"
chmod 0600 "$config_tmp"
mv "$config_tmp" "$new_config"
rm "$old_config"

printf 'Migrated work-* to %s-* in %s\n' "$target_prefix" "$new_store"
printf 'Hub config: %s\n' "$new_config"
printf 'Backup: %s\n' "$backup"
