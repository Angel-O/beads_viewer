#!/bin/bash
set -euo pipefail

die() {
  printf 'qa-wbd-migrate: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 1 ] || die "usage: $0 <beads-store-copy-worktree>"

script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
viewer_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
beads_root=$(CDPATH= cd -- "$1" && pwd)

for command in find git go grep jq shasum sort stat; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
[ -f "$beads_root/cmd/bd/store_copy.go" ] || die "Beads worktree does not contain cmd/bd/store_copy.go: $beads_root"

qa_root=$(mktemp -d "${TMPDIR:-/tmp}/wbd-migration-qa.XXXXXX")
bin_dir=$qa_root/bin
mkdir -p "$bin_dir"

printf 'Building disposable binaries...\n'
(
  cd "$beads_root"
  CGO_ENABLED=1 go build -tags=gms_pure_go -o "$bin_dir/bd" ./cmd/bd
)
(
  cd "$viewer_root"
  go build -o "$bin_dir/wbd" ./cmd/wbd
)

export PATH="$bin_dir:$PATH"
unset BD_DB BD_JSON_ENVELOPE BEADS_DB BD_GLOBAL BEADS_DOLT_DATA_DIR BEADS_DOLT_PORT
unset BEADS_DOLT_PROXIED_SERVER BEADS_DOLT_SERVER_DATABASE BEADS_DOLT_SERVER_HOST
unset BEADS_DOLT_SERVER_MODE BEADS_DOLT_SERVER_PORT BEADS_DOLT_SERVER_SOCKET
unset BEADS_DOLT_SHARED_SERVER BEADS_DIR

json_id() {
  jq -er 'if type == "array" then .[0].id else .id end'
}

tree_digest() {
  local path=$1
  (
    cd "$path"
    find . -type f -print | LC_ALL=C sort | while IFS= read -r file; do
      printf '%s\0' "$file"
      shasum -a 256 "$file" | cut -d ' ' -f 1
    done
  ) | shasum -a 256 | cut -d ' ' -f 1
}

file_digest() {
  local path=$1
  if [ -f "$path" ]; then
    shasum -a 256 "$path" | cut -d ' ' -f 1
  else
    printf 'missing\n'
  fi
}

private_state_digest() {
  {
    tree_digest "$hub_store"
    file_digest "$hub_config"
    file_digest "$hub_ledger"
    file_digest "$viewer_signal"
  } | shasum -a 256 | cut -d ' ' -f 1
}

setup_fixture() {
  local root=$1 origin=$2
  export HOME=$root/home
  source_repo=$root/source
  hub_parent=$HOME/.local/share/beads/hub
  hub_store=$hub_parent/.beads
  hub_config=$HOME/.config/bv/hub.yaml
  hub_ledger=$hub_parent/correlations.jsonl
  viewer_signal=$hub_parent/viewer-generation
  mkdir -p "$HOME" "$source_repo"

  git -C "$source_repo" init -q -b main
  git -C "$source_repo" config user.name 'Migration QA'
  git -C "$source_repo" config user.email 'migration-qa@example.invalid'
  git -C "$source_repo" remote add origin "$origin"
  git -C "$source_repo" commit --allow-empty -q -m 'Initialize migration fixture'

  (
    cd "$source_repo"
    bd init --prefix src --non-interactive --skip-hooks --skip-agents >/dev/null
    wbd bootstrap --prefix hub >/dev/null
    wbd register >/dev/null
  )

  open_id=$(bd --db "$source_repo/.beads" create 'Open migration fixture' --labels preserved-label --json | json_id)
  closed_id=$(bd --db "$source_repo/.beads" create 'Closed migration fixture' --labels second-label --json | json_id)
  bd --db "$source_repo/.beads" dep add "$open_id" "$closed_id" --json >/dev/null
  bd --db "$source_repo/.beads" comments add "$open_id" 'Preserved comment' --json >/dev/null
  bd --db "$source_repo/.beads" close "$closed_id" --reason 'QA fixture' --json >/dev/null
  printf '{"id":"qa-interaction","kind":"tool","created_at":"2026-01-02T03:04:05Z","issue_id":"%s"}\n' "$open_id" >"$source_repo/.beads/interactions.jsonl"

  printf 'fixture\n' >"$source_repo/fixture.txt"
  git -C "$source_repo" add fixture.txt
  git -C "$source_repo" commit -q -m 'Add migration fixture' -m "Refs: $open_id"
  explicit_commit=$(git -C "$source_repo" rev-parse HEAD)

  unrelated_id=$(
    cd "$source_repo"
    wbd create 'Existing unrelated Hub issue' --labels existing-hub-record --json | json_id
  )
  unrelated_before=$(cd "$source_repo" && wbd show "$unrelated_id" --json)
}

verify_backup() {
  local backup=$1
  [ -d "$backup/source/.beads" ] || die "backup omitted source store: $backup"
  [ -d "$backup/hub/.beads" ] || die "backup omitted Hub store: $backup"
  [ -f "$backup/hub.yaml" ] || die "backup omitted Hub config: $backup"
  [ -f "$backup/checksums.sha256" ] || die "backup omitted checksums: $backup"
  [ "$(stat -f '%Lp' "$backup")" = 700 ] || die "backup is not mode 0700: $backup"
  [ "$(stat -f '%Lp' "$backup/checksums.sha256")" = 600 ] || die "checksums are not mode 0600: $backup"
  (cd "$backup" && shasum -a 256 -c checksums.sha256 >/dev/null)
}

verify_import() {
  local output=$1
  local context mapped_open mapped_closed
  context=$(printf '%s\n' "$output" | jq -er '.context')
  mapped_open=$(printf '%s\n' "$output" | jq -er --arg old "$open_id" '.backend.issue_map[$old]')
  mapped_closed=$(printf '%s\n' "$output" | jq -er --arg old "$closed_id" '.backend.issue_map[$old]')

  (
    cd "$source_repo"
    wbd show "$mapped_open" --expand-dependencies --json
  ) | jq -e --arg context "$context" --arg dependency "$mapped_closed" '
    .[0] as $issue |
    ($issue.labels | index("imported") != null and index("preserved-label") != null and index($context) != null) and
    ($issue.dependencies | length == 1 and .[0].id == $dependency and .[0].dependency_type == "blocks")
  ' >/dev/null
  (
    cd "$source_repo"
    wbd show "$mapped_closed" --json
  ) | jq -e --arg context "$context" '
    .[0].status == "closed" and
    (.[0].labels | index("imported") != null and index("second-label") != null and index($context) != null)
  ' >/dev/null
  (
    cd "$source_repo"
    wbd comments "$mapped_open" --json
  ) | jq -e 'length == 1 and .[0].text == "Preserved comment"' >/dev/null
  jq -e --arg id "$mapped_open" --arg commit "$explicit_commit" '
    select(.bead_id == $id and .commit == $commit)
  ' "$hub_ledger" >/dev/null
  grep -F '"issue_id":"'"$mapped_open"'"' "$hub_store/interactions.jsonl" >/dev/null ||
    die 'mapped interaction was not installed'
  [ "$(cd "$source_repo" && wbd show "$unrelated_id" --json)" = "$unrelated_before" ] ||
    die 'unrelated Hub issue changed'
}

run_happy_path() {
  local root=$qa_root/happy dry apply repeat source_before state_before signal_before backup mapped_open ledger_count
  mkdir -p "$root"
  setup_fixture "$root" 'https://example.invalid/acme/migration-happy.git'
  source_before=$(tree_digest "$source_repo/.beads")
  state_before=$(private_state_digest)
  signal_before=$(file_digest "$viewer_signal")

  dry=$(cd "$source_repo" && wbd migrate --dry-run --json)
  printf '%s\n' "$dry" | jq -e '
    .phase == "dry-run" and .source_issue_count == 2 and .exact_correlation_count == 1 and
    (.labels | index("imported")) != null
  ' >/dev/null
  [ ! -e "$(printf '%s\n' "$dry" | jq -er '.backup_path')" ] || die 'dry-run created its planned backup'
  [ "$(private_state_digest)" = "$state_before" ] || die 'dry-run changed private Hub state'
  [ "$(tree_digest "$source_repo/.beads")" = "$source_before" ] || die 'dry-run changed source store'

  apply=$(cd "$source_repo" && wbd migrate --apply --json)
  printf '%s\n' "$apply" | jq -e '
    .phase == "apply" and .backend.applied == true and .backend.issues_imported == 2 and
    (.backend.issue_map | length) == 2 and .correlations.planned == 1 and .correlations.added == 1 and
    .verification.issues_verified == 2 and .verification.source_unchanged == true
  ' >/dev/null
  backup=$(printf '%s\n' "$apply" | jq -er '.backup_path')
  verify_backup "$backup"
  verify_import "$apply"
  [ "$(tree_digest "$source_repo/.beads")" = "$source_before" ] || die 'apply changed source store'
  [ "$(file_digest "$viewer_signal")" != "$signal_before" ] || die 'successful apply did not signal Viewer'

  repeat=$(cd "$source_repo" && wbd migrate --apply --json)
  printf '%s\n' "$repeat" | jq -e '
    .backend.applied == false and .backend.issues_imported == 0 and .backend.history_imported == 0 and
    .backend.events_imported == 0 and .backend.provenance_imported == 0 and
    .correlations.added == 0 and .correlations.existing == .correlations.planned and
    .verification.source_unchanged == true
  ' >/dev/null
  mapped_open=$(printf '%s\n' "$repeat" | jq -er --arg old "$open_id" '.backend.issue_map[$old]')
  ledger_count=$(jq -s --arg id "$mapped_open" --arg commit "$explicit_commit" '[.[] | select(.bead_id == $id and .commit == $commit)] | length' "$hub_ledger")
  [ "$ledger_count" = 1 ] || die "repeat apply duplicated correlation: $ledger_count records"
  [ "$(tree_digest "$source_repo/.beads")" = "$source_before" ] || die 'repeat apply changed source store'
  printf 'Happy path: PASS\n'
}

run_recovery_path() {
  local root=$qa_root/recovery source_before signal_before imported retry repeat
  local -a failed_backups
  mkdir -p "$root"
  setup_fixture "$root" 'https://example.invalid/acme/migration-recovery.git'
  source_before=$(tree_digest "$source_repo/.beads")
  printf '{malformed\n' >"$hub_ledger"
  signal_before=$(file_digest "$viewer_signal")

  if (cd "$source_repo" && wbd migrate --apply --json >"$root/failed.stdout" 2>"$root/failed.stderr"); then
    die 'malformed-ledger apply unexpectedly succeeded'
  fi
  grep -F 'incomplete apply' "$root/failed.stderr" >/dev/null || die 'failed apply did not report incomplete application'
  imported=$(bd --db "$hub_store" --readonly --json list --all --include-all-types --label imported --limit 0 | jq 'length')
  [ "$imported" = 2 ] || die "failed apply did not retain atomic database import: $imported issues"
  [ "$(file_digest "$viewer_signal")" = "$signal_before" ] || die 'incomplete apply signaled Viewer'
  [ "$(tree_digest "$source_repo/.beads")" = "$source_before" ] || die 'incomplete apply changed source store'
  failed_backups=("$hub_parent"/wbd-migrate-backup-*)
  [ -e "${failed_backups[0]}" ] || die 'incomplete apply did not retain a backup'
  verify_backup "${failed_backups[0]}"
  grep -Fx '{malformed' "${failed_backups[0]}/correlations.jsonl" >/dev/null ||
    die 'incomplete apply backup omitted the correlation ledger'

  : >"$hub_ledger"
  retry=$(cd "$source_repo" && wbd migrate --apply --json)
  printf '%s\n' "$retry" | jq -e '
    .backend.applied == false and .correlations.added == .correlations.planned and
    .verification.source_unchanged == true
  ' >/dev/null
  verify_backup "$(printf '%s\n' "$retry" | jq -er '.backup_path')"
  verify_import "$retry"

  repeat=$(cd "$source_repo" && wbd migrate --apply --json)
  printf '%s\n' "$repeat" | jq -e '
    .backend.applied == false and .correlations.added == 0 and
    .correlations.existing == .correlations.planned and .verification.source_unchanged == true
  ' >/dev/null
  printf 'Recovery path: PASS\n'
}

run_happy_path
run_recovery_path

printf 'wbd migrate disposable QA: PASS\n'
printf 'Fixtures retained at: %s\n' "$qa_root"
