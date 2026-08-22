# Hub Context And Scope MVP

## Status

| Field | Value |
|---|---|
| Status | MVP accepted for implementation planning |
| Inputs | Accepted requirements and MODEL-A |
| Goal | Deliver the complete core Hub context lifecycle without extending local-mode semantics |
| Non-goal | Select command syntax, UI styling, implementation sequence, or estimates |

## MVP Outcome

The MVP lets a user capture work before repository ownership is known, create
repository-owned implementation work, coordinate work across repositories,
correct placement mistakes without rewriting history, view scoped work without
changing dependency truth, and attach source commits to the work that owns them.

The MVP covers all 37 confirmed requirements. Deferred boundary behavior remains
outside the MVP unless explicitly resolved below.

## Included Workflows

### Creation And Membership

- `task`, `bug`, `feature`, and `chore` have exactly one immutable context.
- `todo` has zero or more immutable contexts.
- `epic` has one or more immutable contexts.
- Omitted targeting uses the registered current context.
- Explicit targeting supplies the complete context set and does not add the
  current context implicitly.
- Intentional contextless creation is valid only for `todo`.
- Every context must be registered before creation.
- Kind, context cardinality, and reserved labels are validated before one
  all-or-nothing write.
- Every supported Hub writer preserves immutable context membership.

### Todo To Project Work

- A todo remains a durable Beads record when it produces project work.
- Resulting project work stores a native nonblocking `discovered-from` relation
  to the todo.
- Hub reads present that validated relation as `todo results-in project work`.
- A todo may produce more than one project-work bead.
- Closing a todo is manual and means decomposition or handoff is complete; it
  does not imply that resulting work is complete.
- A todo may be reopened if additional decomposition is needed.
- Creating resulting work and its required continuity relation cannot report
  success with only one of those facts persisted.

### Epic Coordination

- Existing native single-parent `parent-child` relationships represent epic
  coordination; no new relationship system is introduced.
- A multi-context epic may parent ordinary project work only when the child's
  sole context belongs to the epic's context set.
- The relationship does not derive or change either endpoint's membership.
- Existing Beads parent-child readiness, hierarchy, and lifecycle behavior is
  preserved rather than replaced by Hub-specific alternatives.

### Placement Correction

- A placement mistake creates a new correctly scoped bead; membership is never
  edited or transferred.
- The replacement stores native `supersedes` from replacement to original.
- Hub reads present the inverse semantic relation: original `superseded-by`
  replacement.
- The original closes with an auditable correction reason and remains
  addressable with its membership, history, relationships, and correlations.
- Correlations are never copied or transferred to the replacement.
- Correction cannot make unfinished dependent work ready merely because the
  original identity closed.
- The correction is all-or-nothing from the caller's perspective.
- Ordinary reopen is not a reversal mechanism for a superseded original.

### Read Scope And Dependency Truth

- A registered current context is the default Hub read scope.
- A caller may explicitly select one or more registered contexts instead.
- A caller may select contextless items alone or together with registered contexts.
- Context matching uses set intersection and returns each issue once.
- Scope filters candidates only. Readiness, blockers, and graph truth are
  computed from the complete Hub graph before projection.
- Multi-context membership remains visible on scoped items.
- Hidden blockers remain observable as blockers.
- Empty selection and full-catalog plus contextless selection normalize to all
  items; full-catalog without contextless remains an explicit context scope.

### Source Correlation

- Every non-todo bead may own zero or more immutable commit correlations.
- Its eligible repository set is exactly its immutable context set.
- A todo never owns commit correlations; its resulting project work owns them.
- A rejected or duplicate correlation creates no new ledger record.
- Each append validates the proposed association atomically without
  reinterpreting unrelated historical records.
- Existing correlations remain attributed to the original after correction.

## Physical Compatibility Contract

| Semantic fact | MVP representation |
|---|---|
| Context membership | Reserved `ctx:` labels on the Beads issue |
| Todo kind | Configured Beads-native custom `todo` type |
| Todo result | Resulting work `discovered-from` todo |
| Epic coordination | Ordinary work `parent-child` epic |
| Supersession | Replacement `supersedes` original plus terminal original status |
| Source correlation | External ledger keyed by bead ID, context, and full commit SHA |

Activation requires an explicit supported-client profile proving:

- `todo` survives creation, unrelated mutation, export, import, and reload;
- zero, one, and multiple reserved context labels survive the same round trips;
- `discovered-from`, `parent-child`, and `supersedes` preserve their type,
  direction, and endpoints;
- unrelated labels are preserved;
- every supported Hub writer enforces reserved-label immutability; and
- local mode continues to read these native records without acquiring Hub
  policy.

There is no fallback that aliases `todo` to `task`, stores membership in a Hub
sidecar, or silently drops lifecycle relationships.

## Explicit Exclusions

- Context policy for `decision`, `docs`, `question`, and other custom kinds.
- Direct commit correlation on todos.
- Membership mutation or transfer.
- Automatic todo closure or closure coupled to resulting-work completion.
- Multiple epic parents or a new coordination relationship type.
- Nested-epic coordination, progress rollups, or epic completion policy.
- New aggregate-scope semantics beyond current Viewer normalization.
- Automatic repair or reinterpretation of conflicting existing records.
- Supersession reversal.
- Changes to legacy local-mode behavior.

## Migration Boundary

- Existing valid records remain valid without rewriting.
- The MVP does not mutate existing context labels to satisfy new cardinality
  rules.
- Existing conflicting todo correlations, memberships, or lifecycle edges are
  reported rather than silently repaired.
- Registry retirement and historical-context repair remain separate work.
- New writes must satisfy the MVP contract even when legacy records remain.

## Success Criteria

1. A user can create ordinary work for the current or another registered
   context, and invalid targeting leaves no bead behind.
2. A user can create contextless, single-context, and multi-context todos, and
   all supported round trips preserve their type and complete context set.
3. A todo can produce one or more repository-owned work items while remaining
   durable and traceable through `results-in`.
4. A multi-context epic can coordinate single-parent work in each member
   context and cannot parent ordinary work outside its context set.
5. A correction preserves the original audit trail and does not falsely release
   blocked work.
6. Current, explicit, and contextless reads return the correct candidates once
   while hidden blockers continue to govern readiness.
7. A non-todo bead can own multiple correlations in each eligible repository;
   an ineligible repository or any todo is rejected without a ledger append.
8. Hub behavior passes the supported-client round-trip matrix and local mode
   retains its existing behavior.

## Phase Exit

The MVP definition is complete when the included workflows, exclusions,
compatibility profile, migration boundary, and success criteria are accepted.
Acceptance authorizes implementation planning, not implementation.
