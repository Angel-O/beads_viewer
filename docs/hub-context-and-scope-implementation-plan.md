# Hub Context And Scope Implementation Plan

## Status

| Field | Value |
|---|---|
| Status | Approved; mandatory upstream P0 prerequisite identified |
| Inputs | Accepted requirements, MODEL-A, and MVP definition |
| Target | Hub mode through `wbd` and `wbv`; local mode remains unchanged |
| Authorization | Planning only; no production implementation is authorized by this document |

## Delivery Contract

The implementation must deliver all MVP workflows together. Partial support for
mutable membership, lossy todo round trips, scoped-only readiness, or
non-atomic lifecycle transitions is not releasable.

Use existing Beads structures:

| Hub semantic | Physical representation |
|---|---|
| Membership | Reserved `ctx:` labels |
| Todo | Configured custom `todo` issue type |
| Todo result | Work `discovered-from` todo |
| Epic coordination | Work `parent-child` epic |
| Correction | Replacement `supersedes` original; original closes |
| Source history | Existing external correlation ledger |

## Current Baseline

- `wbd create` always registers the current repository and injects one context.
- `wbd` reserves `ctx:` labels, but `wbd update --type` can currently change an
  existing issue's kind without revalidating whether its context count remains
  valid for the new kind.
- `wbd` has no explicit target set, contextless creation, todo type, or composite
  lifecycle mutation.
- Native `parent-child`, `discovered-from`, and `supersedes` are already accepted
  dependency types.
- TUI scope already supports exact registered-context intersection and preserves
  global blocker truth in core derived views.
- Empty and full-catalog TUI selections currently normalize to all items.
- Contextless has no distinct selector.
- Robot `--repo` filtering occurs before analysis and must not be reused for Hub
  scope because it can change dependency truth.
- Correlation append is already atomic and delta-validated, but it does not
  reject todos.
- Viewer issue data does not currently expose close reason or dedicated Hub
  lifecycle interpretations.

## Architecture

### Hub Policy Boundary

Add one reusable Hub policy layer consumed by `wbd`, correlation validation,
and Viewer semantic projection. It owns:

- issue-kind classification;
- extraction and validation of exact context sets;
- kind-specific cardinality;
- registered-context resolution;
- immutable reserved-label checks;
- todo direct-correlation exclusion;
- endpoint rules for todo result, epic parent-child, and supersession; and
- stable, structured policy errors.

The policy accepts proposed complete state and returns validated state. It does
not write files, invoke commands, infer membership from relationships, or depend
on caller location after target resolution.

### Store Mutation Boundary

Keep `wbd` as the only supported Hub mutation entry point. Enumerate every
exposed writer and route membership- or lifecycle-sensitive operations through
the policy before invoking Beads.

Simple writes may continue through one `bd` subprocess. Composite operations
must use one authoritative Beads transaction or an upstream primitive that is
demonstrably atomic. Process-level locking plus compensating close/delete is not
an acceptable substitute because it can expose partial durable state.

Beads v1.1.0 `create --graph` is sufficient for atomic todo-result creation: a
single-node graph can create the work, labels, and `discovered-from` edge to an
existing todo in one `RunInTransaction` call. It is not sufficient for
correction because graph apply cannot close an existing issue. `bd batch` can
close and add edges atomically, but its generated create ID cannot be referenced
by later operations. The existing `bd supersede` command is also unsuitable: it
writes the edge and status separately, stores original -> replacement rather
than the accepted replacement -> original direction, and does not preserve the
required close reason.

The mandatory P0 prerequisite is a narrow extension to the graph-apply plan:
accept existing-issue close operations with an ID and reason, and execute them
through `Transaction.CloseIssue` in the same transaction as node and edge
creation. Existing graph edge references already express replacement ->
original and all copied blocking edges. Hub implementation must require a Beads
client version containing this extension before exposing correction.

### Canonical Read Model

Load and analyze the complete Hub issue set once. Build immutable indexes for:

- issue by ID;
- context memberships;
- parent and children;
- todo and resulting work;
- original and replacement; and
- blocking relationships.

Apply read scope only to candidate projection. Relationship lookup, readiness,
and graph analysis always use the canonical indexes.

## Command Contract

Extend existing `wbd` commands rather than adding a second orchestration tool.

### Creation

| Intent | Contract |
|---|---|
| Default | Existing create form; derive and register current context |
| Explicit | Repeat `--context <ctx-id>`; selected contexts replace current context |
| Contextless | `--contextless`; valid only with `--type todo` |

Rules:

- `--context` and `--contextless` are mutually exclusive.
- Duplicate contexts are de-duplicated before cardinality validation.
- Explicit/contextless creation does not register or add the caller's current
  context as a side effect.
- `todo` permits zero or more contexts, `epic` one or more, and ordinary work
  exactly one.
- `decision` retains its existing current-context creation behavior but does not
  gain explicit, contextless, or multi-context targeting. `docs`, `question`,
  and other custom kinds remain unsupported until their policy is defined.
- Remove issue-type mutation from supported Hub update operations.
- Non-context labels remain caller-controlled; `ctx:` labels remain reserved.

### Todo Result

Extend creation with `--from-todo <todo-id>` for ordinary project work. Validate
the todo endpoint and atomically create the work with `discovered-from` pointing
to the todo. Existing generic `discovered-from` relationships remain valid; Hub
interprets the relation as `results-in` only when the endpoint kinds match.

Existing close/reopen commands retain manual todo lifecycle. Closing a todo does
not close resulting work.

### Epic Coordination

Use the existing dependency command and native single-parent semantics:

```text
wbd dep add <child-id> <epic-id> --type parent-child
```

When the parent is an epic, validate that the ordinary child's sole context is
in the epic's context set. Preserve ordinary existing parent-child behavior and
do not add multi-parent or Hub-specific relationship storage.

### Correction

Add one dedicated `wbd replace` workflow because generic create/dep/close calls
cannot truthfully report an atomic correction. It accepts the original ID, new
issue fields, and a complete valid target set, then atomically:

1. creates the replacement;
2. stores replacement `supersedes` original;
3. adds equivalent still-applicable blocking relationships so readiness is not
   released by identity correction; and
4. closes the original with a stable reason naming the replacement.

The original's membership, relationships, history, and correlations remain
unchanged. Generic `dep add --type supersedes` is removed from the supported Hub
surface so correction invariants cannot be bypassed. Routine reopen rejects a
superseded original.

### Correlation

Keep `wbd link <id> [commit]` and existing multiple-correlation behavior. Extend
the selected-bead query to include issue type and reject every todo before Git
resolution or ledger mutation. Context membership continues to limit the
eligible repository; unrelated existing records are parsed and preserved but
not semantically revalidated during append.

## Viewer Contract

### Scope State

Replace the implicit `nil/map` Hub scope with an explicit internal variant:

- `AllItems`;
- `SelectedContexts(non-empty set)`; or
- `Contextless`.

Keep the current UI normalization of empty/full-catalog selection to `AllItems`.
Do not add mixed context-plus-contextless selection. Contextless means no `ctx:`
labels at all; an unregistered `ctx:` label is invalid membership, not
contextless membership.

If current context is unavailable or unregistered, preserve the current
all-items fallback. Explicit scope still replaces that fallback.

### TUI

- Add a distinct contextless choice to the repository picker.
- Keep multi-context membership badges on every matching item.
- Build tree hierarchy from canonical parent-child indexes, then project visible
  nodes so a hidden parent is not silently rewritten as a genuine root.
- Detail surfaces show complete blockers and lifecycle relations, including
  out-of-scope endpoints.
- Board and tree indicate omitted relationship endpoints without promoting them
  into candidate cards/nodes.
- Display close reason for superseded originals.

### Robot

Add additive deterministic Hub fields without changing local-mode output:

```json
{
  "scope": {
    "mode": "all_items|contexts|contextless",
    "contexts": []
  }
}
```

`all_items` is the existing complete-universe projection: every loaded Hub issue
appears once, including contextless items. It is not an alias for selecting only
all registered contexts.

`wbv --hub` accepts repeatable explicit context scope or contextless scope and
rejects their combination. It passes canonical Hub scope separately from legacy
`--repo`; analysis runs globally and each robot result projects candidates only
after metrics/readiness are computed. Existing robot relationship fields remain
the primary contract. Where a current schema would otherwise hide a blocker,
parent, or lifecycle endpoint, add a deterministic boundary reference to the
affected result rather than a universal top-level relationship array. Boundary
references carry stable IDs, type, status, contexts, and in-scope state.

## Compatibility And Migration

### Capability Gate Results

The gate was executed against the exact upstream Beads v1.1.0 tag used by the
supported Homebrew client. A checkout-only embedded-Dolt contract test created
and updated a configured custom `todo`, exported it, imported it into a fresh
store, reloaded it through a new client process, and exported it again.

| Capability | Result | Evidence |
|---|---|---|
| Custom `todo` create and scalar update | Pass | `types.custom=todo`; title and priority survived reload |
| Zero, one, and many `ctx:` labels | Pass | Contextless, one-context, and two-context todos survived exactly |
| Unrelated labels | Pass | Each todo retained its non-context label through update and reload |
| Native relationships | Pass | `discovered-from`, `parent-child`, and replacement -> original `supersedes` retained type, direction, and endpoints |
| Close reason | Pass | The original remained closed with its replacement-specific reason |
| Atomic todo-result creation | Pass | `create --graph` executes node and edge creation inside `RunInTransaction` |
| Atomic correction through a public command | Fail | Storage can transact create/edge/close, but no v1.1.0 CLI command exposes that composition |
| Passive local-mode custom-type loading | Pass | Loader validation accepts every non-empty issue type; existing model tests cover custom types |

The round-trip contract test passed with:

```text
BEADS_TEST_EMBEDDED_DOLT=1 go test -tags gms_pure_go ./cmd/bd \
  -run '^TestHubCapabilityGateExportImportReload$' -count=1
```

Relevant v1.1.0 implementation evidence is in `cmd/bd/graph_apply.go`,
`cmd/bd/batch.go`, `cmd/bd/duplicate.go`, `internal/storage/storage.go`, and
`internal/storage/dolt/transaction.go`. The capability gate is complete: data
round trips and todo-result atomicity are supported, while P0 precisely closes
the correction boundary.

Extend Hub bootstrap/configuration to require `todo` in Beads custom types.

An existing Hub store that lacks the required custom type fails todo admission
with one precise remediation; it never aliases todo or creates partial data.

### Existing Data

- Do not rewrite existing labels, types, dependencies, or correlations.
- Report invalid kind/cardinality, unregistered membership, todo correlations,
  and malformed lifecycle edges through a read-only compatibility report.
- New writes obey current policy even when unrelated legacy findings exist.
- Existing valid one-context records require no migration.
- Registry retirement and destructive repair remain outside this delivery.

## Work Breakdown

| ID | Deliverable | Primary files | Depends on |
|---|---|---|---|
| P0 | Extend Beads graph apply with transactional existing-issue close operations and require the released client version | Upstream Beads `cmd/bd/graph_apply.go`, transaction/CLI tests; `wbd` client-version gate | None |
| P1 | Implement pure Hub kind/context/lifecycle policy with table-driven tests | `pkg/hub` | P0 |
| P2 | Extend `wbd` parsing and complete-state admission; remove type/context bypasses | `cmd/wbd/parser.go`, `cmd/wbd/app.go`, tests | P1 |
| P3 | Implement atomic todo-result and correction workflows using graph apply and native relationships | `cmd/wbd`, lifecycle contract tests | P0, P1, P2 |
| P4 | Enforce epic child context consistency through existing parent-child mutation | `cmd/wbd`, tests | P1, P2 |
| P5 | Reject todo correlations and add stale-unrelated-record append coverage | `pkg/correlation`, `cmd/wbd`, E2E tests | P1, P2 |
| P6 | Introduce explicit Hub scope variants and contextless projection over canonical analysis | `pkg/ui/repository_scope.go`, model/scope tests | P1 |
| P7 | Preserve hidden hierarchy/lifecycle evidence across TUI detail, tree, and board | `pkg/ui`, focused view tests | P3, P4, P6 |
| P8 | Add Hub robot scope inputs, post-analysis projection, and deterministic boundary references | `cmd/wbv`, `cmd/bv`, robot registry/tests | P6, P7 |
| P9 | Add compatibility report, full round-trip/E2E matrix, docs, and local-mode regression suite | `cmd/wbd`, `cmd/wbv`, `pkg/correlation`, `tests/e2e`, docs | P2-P8 |

Parallel execution after P0 and P1:

- Writer track: P2 -> P3/P4/P5.
- Read track: P6 -> P7 -> P8.
- P9 joins both tracks and is the release gate.

## Verification

Each task adds focused unit tests. The release gate runs:

```text
go test ./cmd/wbd ./cmd/wbv ./pkg/hub ./pkg/correlation ./pkg/ui
go test ./tests/e2e/...
go test ./... -race
go build ./...
go vet ./...
gofmt -l .
```

Required scenario coverage:

- every valid and invalid kind/context cardinality;
- omitted, explicit, and intentional contextless creation;
- writer attempts to mutate type or reserved context labels;
- todo result, manual close/reopen, and multiple resulting work items;
- cross-context epic hierarchy and invalid outside-context child;
- atomic correction, readiness preservation, and correlation attribution;
- zero, one, and multiple eligible correlations; todo rejection;
- current, explicit multi-context, contextless, and all-items scope;
- hidden blockers/parents across list, board, tree, detail, history, and robot;
- client export/import/reload and legacy local-mode parity; and
- concurrent create/correction/correlation operations under race detection.

## Rollout

1. Land and release P0, then enforce that minimum client version in `wbd`.
2. Land pure policy and writer admission before lifecycle or correlation changes.
3. Land read scope independently behind Hub-mode routing, without changing
   local-mode output.
4. Enable lifecycle workflows only when composite mutation tests prove atomicity.
5. Release only after P9 demonstrates the complete supported-client and
   cross-surface contract.

No partial fallback is permitted. The validated round trips and atomic
todo-result path may not be used to ship a partial MVP while atomic correction
remains unavailable. P0 is a release prerequisite, not optional follow-up work.
