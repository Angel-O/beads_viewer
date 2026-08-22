# Hub Context And Scope Implementation Plan

## Status

| Field | Value |
|---|---|
| Status | Implemented on repository-local integration lanes; final release evidence complete |
| Inputs | Accepted requirements, MODEL-A, and MVP definition |
| Target | Hub mode through `wbd` and `wbv`; local mode remains unchanged |
| Authorization | Records the implemented contract and final integration scope |

## Delivery Contract

The implementation delivers the MVP through repository-local wrapper policy,
canonical Viewer projection, and the existing external correlation ledger.
Mutable membership, lossy todo handling, and scoped-only readiness remain
invalid outcomes.

Use existing Beads structures:

| Hub semantic | Physical representation |
|---|---|
| Membership | Reserved `ctx:` labels |
| Todo | Configured custom `todo` issue type |
| Todo result | Work `discovered-from` todo |
| Epic coordination | Work `parent-child` epic |
| Correction | Replacement `supersedes` original; original closes |
| Source history | Existing external correlation ledger |

## Implemented Baseline

- `pkg/hub` owns kind, cardinality, registration, immutable membership, and
  lifecycle endpoint policy.
- `wbd` supports current, explicit, and intentional contextless creation,
  durable todo results, epic-child validation, replacement correction,
  correlation admission, and read-only compatibility findings.
- `wbv` resolves current, explicit, contextless, and all-items Hub robot scope
  without reusing legacy `--repo` filtering.
- `bv` analyzes the canonical Hub graph before candidate projection, preserves
  one canonical data hash, and emits deterministic boundary evidence where a
  scoped result references a hidden endpoint.
- Local mode retains its existing schemas and does not acquire Hub scope or
  boundary fields.

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

Simple writes continue through one `bd` subprocess. Todo-result creation uses
the installed client's graph-create primitive so the work and
`discovered-from` relation are one graph mutation. Placement correction is an
ordered wrapper-local operation because the installed public client cannot
combine graph creation and closing an existing issue in one transaction. No
upstream modification or client version gate is introduced.

Correction therefore has an explicit partial-failure boundary: prevalidation
finishes before mutation; graph-create persists the replacement,
replacement-to-original `supersedes`, and applicable open `blocks` continuity;
the wrapper then force-closes the original with the exact reason `Superseded by
<replacement-id>`. The command reports success only after that close. On the
rare close failure, it returns an error that names the persisted replacement ID
so an operator can inspect and complete the outcome without guessing.

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

The dedicated `wbd replace` workflow accepts the original ID, new issue fields,
and a complete valid target set, then performs this ordered sequence:

1. Prevalidate the original, replacement kind, complete target set, and all
   relationships needed for continuity.
2. Graph-create the distinct replacement, replacement-to-original
   `supersedes`, and applicable open incoming and outgoing `blocks` continuity.
3. Force-close the original with `Superseded by <replacement-id>`.
4. Report success only after close; if close fails, report the persisted
   replacement ID explicitly.

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
- `SelectedContexts(non-empty set, include-contextless)`; or
- `Contextless`.

Normalize empty selection and full-catalog plus contextless selection to
`AllItems`. Registered contexts and contextless membership otherwise compose as
an independent union. Contextless means no `ctx:` labels at all; an unregistered
`ctx:` label is invalid membership, not contextless membership.

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
    "contexts": [],
    "include_contextless": false
  }
}
```

`all_items` is the existing complete-universe projection: every loaded Hub issue
appears once, including contextless items. It is not an alias for selecting only
all registered contexts.

`wbv --hub` accepts repeatable explicit context scope and an independently
composable contextless scope. It passes canonical Hub scope separately from legacy
`--repo`; analysis runs globally and each robot result projects candidates only
after metrics/readiness are computed. Existing robot relationship fields remain
the primary contract. Where a current schema would otherwise hide a blocker,
parent, or lifecycle endpoint, add a deterministic boundary reference to the
affected result rather than a universal top-level relationship array. Boundary
references carry stable IDs, type, status, contexts, and in-scope state.

## Compatibility And Migration

### Installed-Client Contract

Hub bootstrap configures `todo` as a custom type and uses the installed
supported `bd` for native issue, relationship, graph-create, close, and export
behavior. Final E2E coverage builds `wbd`, `wbv`, and `bv` in test-owned
temporary space and gives each scenario an isolated `HOME`, Hub store, config,
ledger, and source repositories. It does not modify upstream Beads, inspect a
user Hub, open a browser, or enforce a client version gate.

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

| Lane | Delivered behavior | Primary evidence |
|---|---|---|
| Domain policy | Kind, cardinality, membership, lifecycle, and correlation predicates | `pkg/hub`, focused tests |
| Writer | Admission, todo result, epic child, ordered correction, lifecycle guards, compatibility report | `cmd/wbd`, focused tests |
| Viewer | Explicit Hub scope variants and lifecycle/boundary presentation | `pkg/model`, `pkg/ui`, focused tests |
| Robot | `wbv` routing plus canonical post-analysis `bv` projection | `cmd/wbv`, `cmd/bv`, focused tests |
| Correlation | Repository-aware eligibility and todo rejection without append | `pkg/correlation`, `cmd/wbd`, E2E tests |
| Integration | Real compiled clients, installed `bd`, isolated stores, local parity, compile-linked 37-requirement evidence matrix | `tests/e2e/hub_context_scope_e2e_test.go` |
| Documentation | Public behavior and implemented correction boundary | this plan, `docs/external-history.md` |

## Verification

Each task adds focused unit tests. The release gate runs:

```text
go test ./cmd/wbd ./cmd/wbv ./cmd/bv ./pkg/hub ./pkg/correlation ./pkg/ui
go test ./tests/e2e -run 'TestHubContextScopeRequirementEvidence|TestRealHub'
go test ./...
go test ./... -race
go build ./...
go vet ./...
git ls-files -z '*.go' ':!:vendor/**' | xargs -0 gofmt -l
git diff --check
```

Required scenario coverage:

- focused policy and parser permutations remain in package tests;
- vertical creation covers omitted, explicit, contextless, multi-context, and
  rejected no-write outcomes;
- vertical lifecycle covers durable todo continuity, valid and invalid epic
  children, and ordered correction with blocking and correlation continuity;
- vertical robot reads cover current, explicit aggregate, contextless, and
  all-items scope with one canonical hash, de-duplication, readiness, and hidden
  blocker evidence;
- vertical correlation covers eligible append plus todo and wrong-context
  rejection without append;
- deterministic compatibility findings are read-only; and
- representative local `wbv` and direct `bv` output retains no Hub-only scope
  or boundary fields.

Broad concurrency stress, recovery, migration, repair, browser, upstream
capability, and exhaustive cross-view matrices remain outside this lane.

## Rollout

1. Keep the reviewed policy, writer, Viewer, correlation, and robot commits
   unchanged except for integration defects demonstrated by real-client tests.
2. Land the final E2E and documentation lane as focused repository-local commits.
3. Run focused vertical tests, then the complete test, race, build, vet,
   formatting, diagnostics, diff, and scanner gates.
4. Release the combined branch only when those gates and the private-ID metadata
   audit pass.

There is no upstream release dependency or client version gate. The documented
ordered-correction partial-failure boundary is the implemented operator contract.
