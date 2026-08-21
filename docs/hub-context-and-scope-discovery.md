# Hub Context And Scope Discovery

## Document Status

| Field | Value |
|---|---|
| Status | Discovery; not an approved proposal or implementation plan |
| Scope | Hub issue context, project membership, creation, classification, transfer, correlation, and presentation |
| Purpose | Preserve current findings and coordinate further investigation before requirements are finalized |
| Update rule | Mark statements as confirmed, provisional, or open; do not silently turn a hypothesis into a requirement |
| Out of scope for now | Final command names, storage schema, migration design, and implementation sequencing |

This is a living document. It records limitations and use cases without assuming
that the current `ctx:` representation, the Hub workflow, or the legacy local
model must remain unchanged. Sections are intentionally separable so multiple
agents can investigate them in parallel.

## Working Vocabulary

These terms describe the discussion; they are not a proposed schema.

| Term | Working meaning | Status |
|---|---|---|
| Context | The current `ctx:` label derived from a Git worktree's supported `origin`; registration records it in Hub configuration | Confirmed current concept |
| Project membership | The association that makes a bead count as belonging to a project | Confirmed current behavior |
| Project scope | The repository selection used to include or exclude beads in CLI and Viewer results | Confirmed current behavior |
| Unclassified bead | A bead whose project placement has not yet been determined | Provisional product concept |
| Repository-neutral bead | A bead intentionally associated with no repository | Provisional product concept |
| Project-owned bead | A bead associated with one project | Provisional user-facing term |
| Cross-project parent | A coordinating bead above project-owned child beads | Provisional product concept |
| External Hub correlation | An explicit association between a bead and an immutable commit in a registered repository | Confirmed current concept for external history |

`Unclassified` and `repository-neutral` may eventually need different
representations: one may be awaiting classification while the other may be in
its intended final state. The distinction is recorded here but is not settled.

## Current Behavior

| Area | Current CLI behavior | Current Viewer behavior | Consequence | Confidence |
|---|---|---|---|---|
| Context identity | `wbd context` derives one `ctx:` value from the current Git worktree's `origin` | Registered contexts become repository catalog entries | Repository identity is tied to a registered source checkout | Confirmed |
| Creation | `wbd create` and `wbd new` register the current checkout and inject its context label | Viewer consumes the resulting bead as project-associated | Creation requires entering the target repository checkout | Confirmed |
| Context selection during creation | There is no supported explicit target-project or no-project creation mode | Not applicable | Invocation directory determines initial project membership | Confirmed |
| Context mutation | User-supplied `ctx:` labels are rejected as wrapper-owned; update has no context removal operation | Existing labels are treated as source data | A bead cannot be classified, transferred, or made cross-project through supported `wbd` operations | Confirmed |
| Scoped listing | `wbd list` adds the current context as a label filter; `--all-contexts` removes that implicit filter | Repository selection filters by exact context labels | Beads with no context disappear from explicit project scopes | Confirmed |
| Explicit-ID access | `show`, `update`, `close`, `reopen`, and dependency operations target the Hub store by ID without a current-context ownership check | Analysis retains global dependency context, while displayed candidates and derived views are generally projected to the selected repository scope | Core mutation policy is broader than creation and listing policy | Confirmed |
| Multiple contexts | The supported `wbd` mutation surface cannot construct this state | Model, repository catalog, and scope projection can represent a bead with multiple context labels | Representation is more flexible than the orchestration boundary | Confirmed |
| Zero contexts | Supported `wbd` creation cannot construct this state | All-repository views can represent it; explicit Hub repository scopes hide it | A global inbox is representable but not supported as a creation workflow | Confirmed |
| Commit correlation | `wbd link` uses the current repository context and requires the bead to carry that context | External history validates the correlation context against current bead labels | Current membership also controls correlation eligibility and historical ledger validity | Confirmed |
| Local mode | Repository-local Beads data remains independent of the Hub wrapper | `wbv` can select local mode and use local Git history | Hub changes may not map directly onto the legacy local model | Confirmed |

## Concerns Currently Carried By `ctx:`

| Concern | Concrete CLI example | Concrete UI example | Why it is distinct |
|---|---|---|---|
| Project membership | Creating from a checkout injects that checkout's context label | Repository catalog counts the bead under that project | Describes where the bead belongs |
| Scoped visibility | Default `wbd list` filters by the current context | Selecting a repository shows beads with its exact context label | Describes which projection is currently visible, not ownership itself |
| Context mutation control | `wbd update <id> --add-label ctx:...` is rejected | Context labels are hidden from ordinary label presentation | Protects an internal namespace and integrity boundary |
| Correlation eligibility | `wbd link <id> HEAD` rejects a bead that lacks the current repository context | History aggregates only validated repository-qualified correlations | Governs whether source history can be attached |
| Creation default | The working directory supplies the only creation context | The created bead immediately appears as work for that repository | Provides convenience, but also forces early classification |

The same metadata participates in all five behaviors, but the policies are not
uniform. Creation and default listing depend on the current repository, while
several explicit-ID mutations are Hub-wide. Context mutation is prohibited
rather than expressed as a narrower operation such as classification or
transfer.

## Use Case Inventory

The expected behavior column records the user's intent at discovery time. It
does not imply that the mechanism or terminology has been selected.

| ID | Use case | Example | Expected behavior | Current gap | Requirement status |
|---|---|---|---|---|---|
| UC-01 | Create for a known project from anywhere | Capture project A work while the shell is in project B or outside both checkouts | Select project A without navigating to its checkout; this use case does not require multi-project membership | Creation target is only the current checkout | Candidate |
| UC-02 | Capture before knowing the project | Record an initial idea as a todo | Create without prematurely assigning a repository | Creation always injects the current context | Candidate |
| UC-03 | Keep intentionally repository-neutral work | Track a self-assessment or action on an external platform | Retain the bead without requiring source control or a commit | No supported zero-context creation workflow | Candidate |
| UC-04 | Classify an unclassified bead | Decide that a captured idea belongs to project A | Add project membership after creation through a controlled operation | Context is immutable through `wbd` | Candidate |
| UC-05 | Transfer project ownership | Realize that work belongs in project B rather than project A | Move the bead without navigating between project checkouts or editing raw labels | No supported replacement/removal operation | Candidate |
| UC-06 | Coordinate cross-project work | Track an initiative spanning projects A and B | Use a coordinating parent with project-owned child beads | Parent ownership and scope semantics are not defined | Candidate |
| UC-07 | Preserve single-project implementation ownership | Implement separate changes in projects A and B | Keep each executable child bead associated with one project | Model permits multiple contexts but orchestration does not distinguish parent and child policy | Candidate |
| UC-08 | Avoid source correlation | Keep personal or external-platform work out of Git history | Allow zero correlations without treating the bead as invalid | Already possible in practice, but not independently modelled as a policy | Partially supported |
| UC-09 | Manipulate membership from the target scope | Bring an existing bead into the current project | Permit a validated membership operation without arbitrary `ctx:` editing | All context mutation is rejected | Candidate |
| UC-10 | Read the Hub from any project | Open Viewer and inspect global or repository-scoped work | Preserve current global read and project-filter behavior | Existing behavior provides much of this path | Supported baseline |
| UC-11 | Promote a captured idea into a Bead | Refine a Viewer-managed intake item until its type and project are known | Convert it without silently losing identity, notes, timestamps, or failure state | Viewer has no separate intake store or promotion lifecycle | Candidate |

## Candidate Lifecycle Examples

These examples are descriptive, not normative.

| Stage | Work kind | Project placement | Relationships | Source history |
|---|---|---|---|---|
| Initial capture | Todo or another lightweight kind | Unclassified | None required | None required |
| Project classification | Task, feature, bug, or retained todo | One project | Optional dependencies | May remain empty |
| Cross-project coordination | Epic or another parent kind | Global, multiple projects, or an administrative home; unresolved | Project-owned children | Parent may have no direct commits |
| Personal final state | Todo or task | Repository-neutral | Optional non-project relationships | None expected |
| Transfer | Existing kind | Old project to new project | Existing graph remains attached | Historical-correlation semantics unresolved |

This lifecycle exposes at least three independent dimensions: work kind,
project placement, and source history. A `todo` type could be useful for initial
capture, but this document does not assume that `todo` automatically means
unclassified, repository-neutral, or uncorrelated.

## Policy Granularity Observations

| Policy area | Current granularity | Observed limitation | Narrower policy to investigate |
|---|---|---|---|
| Project targeting | Implicit current checkout only | Directory navigation is required even when the desired project is known | Explicit selection among registered projects |
| Context labels | All `ctx:` mutation is forbidden | Safe classification and transfer are blocked with unsafe arbitrary edits | Structured membership operations that continue to reserve raw `ctx:` labels |
| Core mutations | Explicit IDs are generally Hub-wide | Policy differs from current-project creation and listing | Operation-specific scope and authorization semantics |
| Cross-project membership | Model accepts multiple contexts; wrapper accepts none from callers | No distinction between an implementation bead and a coordinating parent | Role- or relationship-aware rules, if the underlying Beads model permits them |
| Correlation validation | Current membership must match every ledger context | Transferring membership can invalidate historical records | Separate current placement from historical source identity, if safe |
| No-project work | Not creatable through `wbd` | Capture and personal workflows inherit an unrelated checkout context | Explicit unclassified or neutral placement |

The narrower-policy column contains investigation prompts, not accepted
solutions.

## Model Dimensions To Keep Separate During Discovery

| Dimension | Question answered | Example values | Current representation | Open concern |
|---|---|---|---|---|
| Work kind | What sort of item is this? | bug, feature, task, epic, chore, decision, possible todo | Beads issue type | `todo` support depends on the upstream Beads contract and may overlap with task/chore |
| Classification maturity | Has project placement been decided? | unclassified, classified | Not explicit | Could be inferred from no context, but that may conflict with intentionally neutral work |
| Project placement | Where does this work belong? | none, one project, coordinating scope | `ctx:` labels | Cardinality and parent semantics are unresolved |
| Graph role | How does this item organize other work? | leaf, parent, child, blocker | Dependencies, including parent-child | Whether graph role should constrain context cardinality is unresolved |
| Correlation state | What source history is attached? | none or explicit repository-qualified commits | Private correlation ledger | Current validation couples it to present context labels |
| Correlation policy | May source history ever be attached? | unspecified, allowed, prohibited | Not explicit | Absence of a link may already be sufficient; hard prohibition may be unnecessary |
| Visibility | Where should the item appear? | global inbox, project scope, both | Derived from repository filtering | Unclassified and intentionally neutral items may need different presentation |

## Storage Ownership Alternatives

These alternatives explore where pre-classification or todo state could live.
They are not mutually exclusive, selected, or ordered by preference.

| Alternative | Source of truth | Possible role | What it avoids | What it introduces | Status |
|---|---|---|---|---|---|
| Beads-native existing type | Beads stores the complete issue using a currently supported type | Represent an unclassified or neutral item as a task, chore, or other existing type | Upstream type changes and a second issue store | A way to create context-free Beads and distinguish capture intent | Open |
| Beads record with Viewer metadata | Beads stores identity and lifecycle; Viewer stores additional classification or presentation metadata keyed by bead ID | Present an existing Bead as a todo or unclassified item | Reimplementing Beads lifecycle and requiring an upstream `todo` type | Sidecar consistency, backup, migration, and missing-metadata behavior | Open |
| Viewer-managed intake | Viewer stores a lightweight pre-Bead record and later promotes it | Capture an idea before its project and durable Beads type are known | Premature classification and upstream schema changes | Promotion identity, history transfer, partial-failure recovery, and a new writable Viewer store | Open |
| Viewer-managed permanent todo | Viewer stores and manages the entire record for its lifetime | Support personal or external work that never becomes a Bead | Forcing every tracked item into the Beads model | A second lifecycle authority, merged search/UI, backup, concurrency, and possible graph integration | Open |
| Upstream `todo` type | Beads stores todo as a first-class issue type | Use one lifecycle and source of truth for all tracked work | Sidecar or intake storage | Upstream coordination and compatibility across Beads clients and versions | Open; not the focus of current investigation |

The Viewer-managed intake and permanent-todo alternatives differ materially.
A narrow intake record can omit most Beads behavior and end at promotion. A
permanent record may eventually need status, search, history, dependencies,
robot output, recovery, and analysis semantics, making Viewer a second work
tracking authority rather than only a consumer and orchestrator of Beads.

Promotion also creates a consistency boundary. Investigation should account
for stable identity or explicit ID replacement, timestamp and note transfer,
retries, and failures between Bead creation and marking the intake record as
converted. No requirement to support dependencies or graph participation before
promotion has been established.

## Compatibility Surfaces

| Surface | What exists today | Investigation needed | Flexibility or constraint |
|---|---|---|---|
| Upstream Beads issue types | `wbd` currently permits bug, feature, task, epic, chore, and decision | Determine whether upstream storage, export, validation, and clients can accept `todo` | `beads_viewer` consumes Beads and cannot assume unilateral schema control |
| Upstream label model | Issues expose labels as a general collection | Confirm supported add/remove/replace operations and persistence semantics | Context may remain encoded as labels if orchestration supplies safer operations |
| Upstream dependencies | Parent-child and other cross-ID relations are available through `wbd dep` | Confirm cross-project/global parent behavior in storage and all consumers | Cross-project coordination may be expressible without multi-context leaf beads |
| Hub wrapper | Owns store routing, context injection, option allowlisting, and change signaling | Determine the smallest safe surface for explicit targeting and membership mutation | Wrapper policy is locally controllable |
| Viewer-owned storage | No separate intake or permanent-todo store exists today | Determine persistence, synchronization, backup, identity, and source-of-truth boundaries for each storage alternative | A narrow intake store and a full second tracker have very different costs |
| Viewer model | Represents zero or multiple context labels and a global issue universe | Audit every view, robot output, count, filter, and analysis projection | Existing representation is more flexible than current creation workflows |
| Correlation ledger | Stores bead ID, repository context, and immutable commit ID | Define behavior when project placement changes | Historical integrity currently depends on live labels |
| Local mode | Uses repository-local Beads and Git history without Hub routing | Determine which concepts are Hub-only and which should be portable | Legacy behavior must not be accidentally reinterpreted |
| Agent orchestration | Agent callers use the supported `wbd` and `wbv` command boundaries | Audit safe command contracts, context selection, and mutation validation | Agent-facing policy must evolve with the supported CLI, not bypass it |

## Investigation Workstreams

Each workstream should report evidence and alternatives. Investigation does not
authorize implementation. Wave A evidence collection can proceed in parallel.
Wave B synthesizes those findings; Wave C evaluates downstream impact after
candidate semantics exist.

| Wave | Workstream | Questions | Primary surfaces | Expected output | Depends on | State | Owner |
|---|---|---|---|---|---|---|---|
| A | Requirements inventory | Are all capture, classification, transfer, project, parent, and neutral workflows represented? Which are distinct? | This document, user workflows | Expanded use-case and acceptance-example tables | None | Open | Unassigned |
| A | Product direction | Which concepts belong only to Hub mode? Which should work in local mode? | `docs/external-history.md`, mode selection | Compatibility matrix and product-boundary options | None | Open | Unassigned |
| A | Upstream Beads constraints | Which types, label mutations, dependencies, and migrations are supported by Beads? | Installed Beads CLI contracts and upstream documentation | Evidence-backed capability and limitation report | None | Open | Unassigned |
| A | Current model audit | What states can current labels, dependencies, loaders, and Hub configuration represent? | `pkg/model`, loaders, Hub config | Current invariants and representational limits | None | Open | Unassigned |
| A | Current Viewer UX audit | How are zero-context, one-context, multi-context, parent, and child beads presented today? | `pkg/ui`, repository catalog and scope | Current-state view and interaction inventory | None | Open | Unassigned |
| A | Current correlation lifecycle | What validations run when correlations are written and loaded, and what breaks if membership changes? | `pkg/correlation`, external history | Current state-transition and integrity analysis | None | Open | Unassigned |
| B | Domain-model alternatives | Can current labels express the workflows safely, or is separate metadata required? | Wave A reports | Alternatives with invariants and compatibility impact | Requirements, product, upstream, model, correlation | Open | Unassigned |
| B | Storage-ownership alternatives | Should capture state live in Beads, Viewer metadata, a temporary intake store, or a permanent Viewer-managed record? | Wave A reports, persistence and loader boundaries | Source-of-truth alternatives with lifecycle and failure analysis | Requirements, product, upstream, model | Open | Unassigned |
| B | CLI and orchestration alternatives | How could callers select a registered project without changing directories? What membership operations could remain safe? | `cmd/wbd`, supported agent command contracts | Command-flow alternatives and authorization analysis | Requirements, upstream, domain model | Open | Unassigned |
| B | Viewer UX alternatives | How could unclassified, neutral, project-owned, and coordinating parents appear and be manipulated? | Current UX audit and domain alternatives | View-state options and interaction sketches | Requirements, product, domain model | Open | Unassigned |
| C | Robot/API contracts | How could scopes and classification appear without breaking deterministic consumers? | Robot registry and JSON contracts | Contract impact and compatibility options | Domain and UX alternatives | Open | Unassigned |
| C | Graph and analysis | How could global parents and cross-project dependencies affect readiness, triage, planning, and filtering? | `pkg/analysis`, scoped projections | Algorithm and projection impact report | Domain alternatives | Open | Unassigned |
| C | Migration | How could existing one-context Hub beads and legacy local beads be interpreted? | Hub store, loaders, migration tooling | Non-destructive migration alternatives | Product and domain alternatives | Open | Unassigned |
| C | Testing | Which invariant, integration, TUI, robot, and upstream-compatibility tests would be needed? | Existing test suites | Test matrix tied to candidate behavior | All candidate semantics | Open | Unassigned |

## Known Evidence

| Finding | Evidence |
|---|---|
| Creation injects the registered current context | `cmd/wbd/app.go` create/new dispatch |
| Default listing filters by current context | `cmd/wbd/app.go` list dispatch |
| Context-label mutation is rejected | `cmd/wbd/parser.go` label validation |
| Supported update has no label-removal operation | `cmd/wbd/parser.go` update parsing |
| Viewer can present and filter multiple Hub contexts | `pkg/ui/repository_scope.go` |
| Explicit Hub scope hides context-free issues | `pkg/ui/repository_scope.go` scope matching |
| External-correlation writes require matching context membership | `pkg/correlation/beads_history.go` and `pkg/correlation/hub_config.go` write path |
| Full-ledger loading requires every correlation context to remain on its bead | `pkg/correlation/hub_config.go` load validation |
| Hub and local history modes intentionally remain separate | `docs/external-history.md` |

## Open Questions Log

Questions remain open until the document records evidence and an explicit
decision. Their presence does not imply equal priority.

| ID | Question | Related use cases | State | Notes |
|---|---|---|---|---|
| OQ-01 | Is `todo` a distinct durable work kind, a capture-stage marker, or unnecessary if an unclassified task is sufficient? | UC-02, UC-03 | Open | Must include upstream type compatibility |
| OQ-02 | How should the UI distinguish temporarily unclassified from intentionally repository-neutral work? | UC-02, UC-03 | Open | Both currently appear as zero-context beads |
| OQ-03 | Does a cross-project parent carry multiple memberships, remain global, or derive participating projects from children? | UC-06, UC-07 | Open | No preferred model recorded |
| OQ-04 | Should ordinary implementation beads be constrained to exactly one project? | UC-01, UC-07 | Open | No preferred cardinality recorded |
| OQ-05 | Which operations, if any, should require current-project membership rather than explicit bead ID? | UC-04, UC-05, UC-09 | Open | Current policies are asymmetric |
| OQ-06 | How is an explicit project selected when the caller is outside its checkout? | UC-01 | Open | Must avoid ambiguous names and arbitrary context injection |
| OQ-07 | What does transfer do to historical correlations from the old project? | UC-05 | Open | Current ledger validation would reject a stale membership |
| OQ-08 | Is an explicit never-correlate policy valuable, or is having no correlations enough? | UC-03, UC-08 | Open | Do not infer this from `todo` or zero context |
| OQ-09 | Should global/unclassified work participate in project readiness and blocker calculations? | UC-02, UC-03, UC-06 | Open | Hidden dependencies already remain relevant in scoped analysis |
| OQ-10 | Which changes can be Hub-only without distorting or breaking local mode? | All | Open | Local mode has no equivalent orchestration boundary |
| OQ-11 | Should Viewer own only temporary intake records or also permanent todo lifecycles? | UC-02, UC-03, UC-11 | Open | These imply different product and persistence boundaries |
| OQ-12 | If an intake item becomes a Bead, how are identity, timestamps, notes, retries, and partial failures handled? | UC-04, UC-11 | Open | Promotion is a cross-store transition if Viewer owns intake |
| OQ-13 | Could Viewer metadata safely add todo semantics to an otherwise ordinary Bead without becoming a second lifecycle authority? | UC-02, UC-03 | Open | Requires sidecar consistency and missing-metadata behavior |

## Agent Investigation Protocol

| Rule | Guidance |
|---|---|
| Preserve status | Label findings as confirmed, provisional, contradicted, or open |
| Cite evidence | Include file paths, line ranges, commands, upstream documentation, or reproducible output |
| Separate layers | State whether a limitation comes from upstream Beads, `wbd`, Viewer, correlation storage, UI, or agent orchestration |
| Avoid implementation drift | Do not edit production behavior as part of a discovery workstream |
| Record alternatives | Describe multiple viable interpretations when evidence does not select one |
| Protect compatibility | Include Hub mode, local mode, robot contracts, and existing data in impact reports |
| Keep tracking private | Do not place private tracker identifiers in this document, branches, commits, or pull-request metadata |

Suggested report shape:

| Field | Content |
|---|---|
| Workstream | Name from the investigation table |
| Finding | One falsifiable statement |
| Status | Confirmed, provisional, contradicted, or open |
| Evidence | Source and relevant location |
| Impact | Requirements, model, UI, orchestration, compatibility, or testing |
| Follow-up | Remaining uncertainty without proposing implementation prematurely |

## Change Log

| Date | Change |
|---|---|
| 2026-08-21 | Initial discovery document capturing current behavior, use cases, policy granularity, compatibility surfaces, and parallel investigation areas |
| 2026-08-21 | Added Beads-native, Viewer-metadata, temporary-intake, permanent-todo, and upstream-type storage alternatives |
