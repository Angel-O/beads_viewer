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
beads. Duplicate contexts, unreadable checkouts, and non-Git checkout paths are
errors in external mode. `repositories: {}` is valid before the first source
repository is registered; with an empty ledger, Viewer still loads the global
board and reports empty source history.

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

## Lifecycle Provider

External history invokes the installed Beads CLI as:

```text
bd --db <store> --readonly history <bead-id> --json
```

The current Beads response is a sequence of Dolt snapshots containing
`CommitHash`, `Committer`, `CommitDate`, and nested `Issue` state. `bv` orders
those snapshots chronologically and derives created, claimed, closed, reopened,
and modified lifecycle events from status transitions. Dolt commit hashes stay
on lifecycle events; they are never treated as source Git commits.

For an unfiltered report, lifecycle history is requested only for unique beads
that have validated ledger correlations. A selected `--bead-history` still
loads that bead's lifecycle even when it has no source correlation. This avoids
one `bd` subprocess for every unrelated issue in a global store.

Repository context plus full commit SHA is the authoritative immutable source
identity. Branch names are deliberately not persisted: a commit may be
reachable from multiple branches, and branch names are mutable and non-unique.
Callers that need current reachability can query the configured checkout with
Git at that time instead of treating a branch as commit metadata.

External loading is fail-fast. A malformed config or ledger, unknown context,
missing commit, unreadable repository, or Beads history failure returns an
actionable error and does not trigger legacy Git fallback.

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
