# External History

`bv` can combine a private global Beads/Dolt store with source history from
multiple real Git checkouts. The three inputs stay separate:

- Bead lifecycle events come from `bd history` against the configured store.
- Source commit metadata, files, renames, and line statistics come from the
  configured Git checkouts.
- Bead-to-source-commit associations come from a private JSONL ledger.

Neither the hub configuration nor the ledger is written into a source
repository.

## History Modes

Use `--history-mode auto|git|external|off`:

- `auto` is the default. An explicit `--hub-config` selects `external`.
  Otherwise, `~/.config/bv/hub.yaml` selects `external` when it exists;
  if it does not exist, `auto` preserves the existing single-repository `git`
  behavior.
- `git` uses the existing Git reconstruction of committed Beads JSONL history.
  It uses current/`--db` issue data and does not auto-adopt a hub config.
- `external` requires a hub configuration and never runs Git against the
  Beads store or JSONL parent directory. Its configured `store` is also the
  authoritative Viewer issue source.
- `off` performs no Git or Beads history loading. History reports are empty,
  while issue loading, the board, and graph analysis remain available. An
  explicit or conventionally discovered hub config still supplies the global
  issue store; without one, normal current/`--db` loading is preserved.

An explicit `--hub-config PATH` takes precedence over conventional discovery.
There is no environment-variable equivalent.

Issue-source precedence when external/off mode has a hub config is `--db`,
then the hub config's `store`. The configured store overrides ambient
`BEADS_DB` and `BEADS_DIR` so
running `bv` from an unrelated checkout cannot silently load that checkout's
issues. The explicit `--db` flag retains its existing highest precedence.
`--workspace` and `--as-of` are rejected in external or off mode when a hub
config supplies the store, because they would replace that authoritative store
with a different issue source.

## Hub Commands And Local Mode

`wbd` is the Hub-only Beads command boundary. Initialize the private store and
version-1 Viewer config explicitly, optionally selecting a store-wide prefix:

```bash
wbd bootstrap
wbd bootstrap --prefix team
```

For a missing store, bootstrap performs the one-time initialization above. For
an existing store, the same command never reinitializes or cleans the store; it
adds `todo` to the installed client's existing custom issue types only when
needed, preserves the other configured types, and ensures the Viewer Hub config
exists. Repeating it is safe and reports that todo support is already enabled.

The fixed paths are:

- Store: `~/.local/share/beads/hub/.beads`
- Correlation ledger: `~/.local/share/beads/hub/correlations.jsonl`
- Viewer config: `~/.config/bv/hub.yaml`

From an origin-backed source checkout, `wbd context` prints its credential-free
`ctx:` label. `wbd register` records the durable primary checkout that shares
the current worktree's Git common directory, with the current non-bare worktree
as a safe fallback. `wbd configure` reconciles an existing registration.
Creation without a target registers the checkout as needed and uses its current
context. Repeat `--context <context>` to supply a complete explicit target set,
or use `--contextless` for a contextless `todo`; neither form registers or adds
the current checkout. Todos accept zero or more contexts, epics one or more,
and `task`, `bug`, `feature`, and `chore` exactly one. A `decision` retains only
the default-current creation form. Context labels are immutable after creation,
and issue type is not an update field. Todo creation first checks the store's
custom-type capability without registering or creating an issue. If unavailable,
run `wbd bootstrap` explicitly to enable it; creation never aliases todo to task
or performs setup automatically.

`wbd create <title> --from-todo <todo-id>` atomically creates ordinary project
work and its native `discovered-from` continuity relation. Todo close and reopen
remain manual. An epic may parent ordinary project work only when the child's
context belongs to the epic. `wbd replace <original-id> --context <context>`
prevalidates the request, creates a correctly targeted replacement with native
supersession and applicable open blocking continuity, and then closes the
original with `Superseded by <replacement-id>`. Success is reported only after
the close. If that final close rarely fails, the error names the already-created
replacement explicitly; decisions do not use this explicit-target path.

`wbd link <bead-id> [commit]` adds a source correlation, defaulting to `HEAD`.
`wbd unlink <bead-id> <full-commit-sha>` removes only the exact correlation in
the current registered repository context. Both return JSON; unlink reports
`"removed":false` when the tuple is absent and signals Viewer only when it
actually removes the correlation. Todos cannot own direct correlations.
`wbd compatibility --json` reports supported
legacy policy findings without repair and exits successfully when findings are
present. `wbd show`, scalar `update`, `dep`, `close`, and allowed `reopen`
operations use the same Hub store. Use the supported wrapper commands rather
than passing arbitrary `bd` options; direct `wbd init` and alternate
database/config paths are intentionally unavailable.

Without a mode selector, `wbv` selects local mode only when the current Git
worktree root has a real, non-symlink `.beads` directory containing a valid
Viewer issue source (SQLite, supported JSONL, a routed store, or a recognized
bd/Dolt workspace). Otherwise it selects the Hub. Empty `.beads` directories
and directories containing only unrelated files do not turn a checkout into a
local workspace. A linked worktree uses only `.beads` at that worktree's own
root; it does not inherit local data from another worktree that shares the Git
common directory.

Use `wbv --local` or `wbv --hub` as the first argument to force a mode.
`--local` requires a valid local issue source. `--hub` always selects the Hub,
even when local data exists or is malformed. Unsafe markers, broken redirects,
and malformed canonical issue sources produce an actionable error in automatic
or explicit local mode instead of silently selecting another data source. Run
`wbv --help` for a concise summary of these rules.

Local mode runs Viewer with Git history from the worktree and never calls
`wbd`, registers the checkout, or migrates local issue data. Hub mode runs
`wbd configure`, sets the fixed Hub store, and invokes Viewer with external
history and the fixed Hub config. `wbd` remains Hub-only regardless of the
current checkout.

### Testing Unreleased Hub Viewer Changes

Build all three wrapper-chain commands from the source checkout before testing
an unreleased TUI change:

```bash
test_bin="$(mktemp -d "${TMPDIR:-/tmp}/wbv-local.XXXXXX")"
go build -o "$test_bin/bv" ./cmd/bv
go build -o "$test_bin/wbd" ./cmd/wbd
go build -o "$test_bin/wbv" ./cmd/wbv
```

Then change to the repository context where Viewer should run and prepend the
temporary directory to `PATH`:

```bash
cd /path/to/source/repository
PATH="$test_bin:$PATH" "$test_bin/wbv" --hub
```

The `PATH` override is required because `wbv` delegates to `bv` and `wbd` by
command name. Running only `go run ./cmd/wbv --hub` can therefore compile the
new wrapper while still launching old installed `bv` or `wbd` binaries. This
workflow tests the complete local command chain without installing or releasing
it.

## Repository-Only Migrations

The migration scripts under `scripts/` are manual repository tools. They are
never run automatically, and they do not import a source repository's local
`.beads` store into the Hub.

For an existing Hub, run the repeatable prefix-only migration:

```bash
bash scripts/migrate-beads-hub-prefix.sh
```

It reads the one persisted prefix, prompts with that prefix as the default, and
makes no changes or backup when the answer is unchanged. Before a real change,
it backs up the complete Hub parent and `hub.yaml` under
`~/.local/share/beads/hub-prefix-backup-<timestamp>`. It then uses `bd
rename-prefix`, refreshes `issues.jsonl`, and changes only top-level `bead_id`
in the correlation ledger and top-level `issue_id` in `interactions.jsonl`.
Nested values are deliberately untouched. A simple matching `last-touched`
value is updated as well.

For the legacy private store at `~/.local/share/beads/work/.beads`, and only
when no Hub destination or Hub config exists, run the one-time path migration:

```bash
bash scripts/migrate-beads-work-to-hub.sh
```

It requires the legacy prefix to be `work`, defaults the new prefix to `bead`,
preserves repository registrations in the version-1 config, rewrites its fixed
store and ledger paths, and creates
`~/.local/share/beads/work-to-hub-backup-<timestamp>` before mutation. After it
succeeds, use the repeatable prefix migration for later naming changes rather
than running the path migration again.

Both commands require `bash`, `bd`, and `jq`. They reject missing, malformed,
or unsupported configs; unexpected fixed paths; invalid prefixes; conflicting
destinations; and symlinked stores, configs, ledgers, interactions, exports, or
`last-touched` files before mutation. Calls to `bd` clear ambient Beads and Dolt
database/server variables and set only the fixed store. Backups are private
(`umask 077`) and are preserved if a later migration step fails. Do not point
these scripts at alternate paths or run them against repository-local stores.

## Hub Configuration

The private YAML configuration is versioned. Relative paths are resolved from
the configuration file's directory. A leading `~/` is expanded using the
current user's home directory; other shell expansion is not performed.

```yaml
version: 1
store: ~/.local/share/beads/hub/.beads
ledger: ~/.local/share/beads/hub/correlations.jsonl
repositories:
  ctx:project-a-5365b77092:
    path: ~/workspace/source/project-a
  ctx:project-b-a81fc92e10:
    path: ~/workspace/source/project-b
```

Repository contexts must exactly match the `ctx:` labels carried by correlated
beads. `repositories: {}` is valid before the first source repository is
registered; with an empty ledger, Viewer still loads the global board and
reports empty source history.

## Correlation Ledger

The ledger is append-friendly JSONL with one record per association:

```json
{"bead_id":"bead-a3f2dd","context":"ctx:project-a-5365b77092","commit":"0123456789abcdef0123456789abcdef01234567"}
{"bead_id":"bead-a3f2dd","context":"ctx:project-b-a81fc92e10","commit":"89abcdef0123456789abcdef0123456789abcdef"}
{"bead_id":"bead-b917ce","context":"ctx:project-a-5365b77092","commit":"0123456789abcdef0123456789abcdef01234567"}
```

This supports multiple commits per bead, multiple beads per commit, and
multiple repositories. Ledger commits must be hexadecimal Git commit IDs.
If the configured ledger does not exist, it is treated as empty; the first
successful `bv correlate add` creates it atomically. Existing malformed or
unreadable ledgers remain errors.
Ledger commits must be complete 40-character SHA-1 or 64-character SHA-256
object IDs; abbreviations are rejected. External loading resolves each ID to a real commit in its configured checkout
and fails rather than falling back to Git history in the global store.

Viewer always validates the complete config and ledger before applying a
selected-bead filter. Malformed YAML or JSONL, unsupported versions, unknown
beads, undefined or mismatched contexts, invalid full SHAs, and duplicate
correlations are therefore fatal even when the invalid record belongs to a
different bead than `--bead-history` selected.

Commit identity is `<context>:<full-sha>`. File identity is
`<context>:<path>`, for example
`ctx:project-a-5365b77092:src/config.ts`. Robot history includes the context in
each external commit and file object. File hotspots, file-to-bead lookup, and
co-change analysis use the namespaced file identity.

## Adding Correlations

Use the writer to resolve a checkout/ref and atomically update only the private
ledger:

```bash
bv correlate add \
  --bead bead-a3f2dd \
  --repo ctx:project-a-5365b77092 \
  --commit HEAD \
  --hub-config ~/.config/bv/hub.yaml
```

`--repo` also accepts the configured checkout path. The command resolves the
ref to an immutable full SHA, validates that the repository is configured, and
uses read-only `bd show` to verify that the bead exists in the configured store
and carries the selected context label. Validation failures do not mutate the
ledger. The command
reports `"added":false` without duplicating an existing association. Ledger
replacement holds an inter-process lock and uses a same-directory temporary
file, `fsync`, atomic rename, and parent-directory sync.

## Removing Correlations

Use the safe Hub wrapper from the repository that owns the correlation:

```bash
wbd unlink item-a3f2dd 0123456789abcdef0123456789abcdef01234567
```

Removal requires an eligible non-todo bead, the current registered repository
context, and an immutable full 40- or 64-character commit SHA. It does not
accept a ref, abbreviated SHA, omitted tuple field, context override, wildcard,
or bead-wide/repository-wide deletion. The exact bead/context/SHA tuple is
matched case-insensitively for hexadecimal SHA text. If duplicate physical rows
encode that same logical tuple, all are removed so readers cannot surface the
deleted correlation again; unrelated records retain their original bytes and
ordering.

The operation uses the same private-ledger lock, configured-store eligibility
check, malformed-ledger rejection, and atomic rewrite as addition. A missing
tuple is a successful idempotent no-op with deterministic JSON:

```json
{"correlation":{"bead_id":"item-a3f2dd","context":"ctx:project-a-5365b77092","commit":"0123456789abcdef0123456789abcdef01234567"},"removed":false}
```

## Lifecycle Provider

External history invokes the installed Beads CLI as:

```text
bd --db <store> --readonly history --ids-stdin --json
```

Viewer sends the selected exact IDs as newline-delimited stdin and requires the
Beads bulk History capability. The response is the schema-versioned grouped
envelope with one snapshot group per requested ID. Older CLIs and storage
backends without bulk History support fail with an upgrade/capability error;
Viewer never falls back to one subprocess per ID.

This capability is currently unreleased. External History requires a Beads
build containing bulk `history --ids-stdin` support until an upstream release
includes it.

Each group contains Dolt snapshots with `CommitHash`, `Committer`, `CommitDate`,
and nested `Issue` state. `bv` orders those snapshots chronologically and
derives created, claimed, closed, reopened, and modified lifecycle events from
status transitions. Dolt commit hashes stay on lifecycle events; they are never
treated as source Git commits.

For an unfiltered report, lifecycle history is requested in one bulk call only
for unique beads that have validated ledger correlations. A selected
`--bead-history` still
loads that bead's lifecycle even when it has no source correlation. This avoids
both unrelated issue reads and one `bd` subprocess per selected issue.

Repository context plus full commit SHA is the authoritative immutable source
identity. Branch names are deliberately not persisted: a commit may be
reachable from multiple branches, and branch names are mutable and non-unique.
Callers that need current reachability can query the configured checkout with
Git at that time instead of treating a branch as commit metadata.

After global validation, Viewer probes only repositories used by applicable
correlations: every ledger record for an unfiltered report, or the selected
bead's records for `--bead-history`. Configured repositories with no applicable
records are not probed and do not produce warnings.

If an applicable checkout is missing, unreadable, not a directory, or not a Git
worktree, Viewer skips that context and returns a successful partial report.
Lifecycle events and source commits from available contexts remain present.
The report's `warnings` array contains one deterministic structured warning per
skipped context, including its context label, stable reason, and skipped
correlation count; warning messages do not expose private checkout paths. The
History TUI marks these reports as partial, and history-derived robot outputs
carry the same diagnostics as `history_warnings` where needed. If every
applicable checkout is unavailable, the report still contains lifecycle events
and warnings with zero source commits.

Data-integrity and provider failures remain fail-fast. A full ledger commit
missing from an otherwise valid checkout, malformed Git metadata, changed-file
or line-stat extraction failure, unavailable Git executable, cancellation,
timeout, or Beads lifecycle-provider failure returns an error and never falls
back to legacy Git history. Viewer does not prune or rewrite stale registrations
or ledger records.

## Target-Machine QA

Build the fork without replacing the installed Viewer:

```bash
go build -o /tmp/bv-external-history ./cmd/bv
```

From a source checkout that is not the global Beads store, verify the global
board loads with history disabled and without Git/Beads history warnings:

```bash
/tmp/bv-external-history --history-mode off \
  --hub-config ~/.config/bv/hub.yaml --robot-graph
```

Choose a bead carrying one of the configured `ctx:` labels and correlate its
real source commit. Running the command twice must return `"added":true` and
then `"added":false` without duplicating the ledger record:

```bash
/tmp/bv-external-history correlate add \
  --bead bead-a3f2dd \
  --repo ctx:project-a-5365b77092 \
  --commit HEAD \
  --hub-config ~/.config/bv/hub.yaml
```

Verify lifecycle events, repository-aware commits, namespaced files, and file
statistics in robot output:

```bash
/tmp/bv-external-history --history-mode external \
  --hub-config ~/.config/bv/hub.yaml \
  --robot-history --bead-history bead-a3f2dd

/tmp/bv-external-history --history-mode external \
  --hub-config ~/.config/bv/hub.yaml \
  --robot-file-hotspots
```

Finally launch the TUI from the same unrelated source checkout. Confirm the
board shows the global store, History opens without the old not-a-Git-repository
warning, lifecycle events come from Beads, and commit/file details show the
configured repository context and real source paths:

```bash
/tmp/bv-external-history --hub-config ~/.config/bv/hub.yaml
```
