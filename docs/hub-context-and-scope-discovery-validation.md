# Hub Context And Scope Discovery Validation

## Document Status

| Field | Value |
|---|---|
| Status | Discovery-validation synthesis complete; ready for parent review, but not approved requirements or a selected implementation model |
| Baseline | `docs/hub-context-and-scope-discovery.md` |
| Purpose | Validate current-state evidence, record the user-selected discovery direction separately, and bound later requirements and model work |
| Evidence date | 2026-08-21 |
| Decision rule | Current-state evidence describes what exists; recorded user direction describes what later work should evaluate; neither alone finalizes a contract or implementation |
| Out of scope | Command syntax, relationship type, UI details, local compatibility, contingency implementation, schema, migration, MVP, sequencing, and estimates |

This phase validates the baseline without replacing it. The `Validated Current
State` section is evidence-backed description. The `User-Selected Discovery
Direction` section records questionnaire outcomes that guide later work but are
not yet detailed requirements, a final domain model, or an implementation plan.

## Track Provenance

Five independent read-only technical audits used one shared baseline brief and
non-overlapping scopes. The requirements inventory remains user-led.

| Track | Scope owner | Authoritative surfaces | Explicit exclusions |
|---|---|---|---|
| A1 Requirements inventory | Coordinator and user | Existing conversation and baseline use cases | No inferred preferences, feasibility decisions, or requirement finalization |
| A2 Product boundary | Mode audit | `docs/external-history.md`, `cmd/wbv/main.go`, focused tests, minimal `cmd/bv/main.go` wiring | Model cardinality, UI rendering, correlation internals, and upstream capability |
| A3 Upstream Beads constraints | Upstream contract audit | Immutable `beads_rust` v0.3.2 source/docs at commit `4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9`, current source comparison at `29afe25c93e07e47f8d53bbf92547659b0d1fb97` | Hub wrapper policy, Viewer behavior, and product desirability |
| A4 Current representation | Model/loader/Hub audit | `pkg/model`, `pkg/loader`, `pkg/hub` and focused tests | Wrapper policy, UI projection, correlation, and upstream guarantees |
| A5 Current Viewer UX | Presentation and interaction audit | `pkg/ui` repository scope, picker, board, tree, detail, history, and focused tests; minimal launch wiring | Storage/model feasibility, correlation validity, upstream capability, and proposed UX |
| A6 Correlation lifecycle | External-history integrity audit | `pkg/correlation`, external-history E2E tests, and correlation sections of `docs/external-history.md` | Membership command design, general UI, upstream labels, and model selection |

No technical track reported a direct contradiction of the baseline. Several
cross-layer tensions and previously unstated asymmetries were found and are
recorded below.

## Validated Current State

### Product And Mode Boundaries

| Finding | Status | Evidence | Consequence for discovery |
|---|---|---|---|
| `wbv` mode selection and `bv --history-mode` select different axes. `wbv auto` prefers a valid local issue source; direct `bv auto` selects external history when a Hub config is found. | Confirmed | `cmd/wbv/main.go:83-110`; `cmd/bv/main.go:8294-8336`; `cmd/wbv/main_test.go:89-132`; `cmd/bv/main_test.go:2349-2390` | Hub/local orchestration and history provider are separate evidence axes. |
| `wbv --local` requires a Git worktree and valid local issue source, uses Git history, and does not invoke `wbd`. | Confirmed | `cmd/wbv/main.go:93-125,156-160,177-188`; `docs/external-history.md:71-91` | Registration, the fixed Hub store, and the private external ledger are currently Hub-only mechanisms. |
| `wbv --hub` runs Hub configuration, selects the fixed Hub store, and invokes external history. | Confirmed | `cmd/wbv/main.go:126-178`; `cmd/wbv/main_test.go:115-131` | Hub orchestration is stronger than merely selecting an issue source. |
| `bv --history-mode off` disables history but can still use the Hub store and repository scope. | Confirmed | `cmd/bv/main.go:1712-1719,1899-1907,8324-8328`; `docs/external-history.md:27-30` | No-history, repository-neutral, and local-mode concepts are distinct. |
| Local Git history infers associations from repository history; external history uses explicit repository-qualified ledger records and has no fallback on integrity failure. | Confirmed | `docs/external-history.md:14-30,237-283` | Provider semantics differ; local compatibility is deferred beyond this phase. |

The inspected mode code does not decide whether classification maturity,
neutral placement, work kind, graph role, or correlation policy should be
portable across Hub and local modes. Local compatibility is therefore deferred
rather than inferred from current behavior.

### Upstream Beads Capabilities

The applicable upstream version could not be matched to the installed binary
without invoking it. Evidence therefore describes released `beads_rust` v0.3.2
and notes where current upstream source agrees.

| Finding | Status | Evidence | Consequence for discovery |
|---|---|---|---|
| Upstream v0.3.2 names `task`, `bug`, `feature`, `epic`, `chore`, `docs`, and `question`, and accepts lowercased custom non-empty issue types subject to validation. | Confirmed for v0.3.2; installed version open | [`src/model/mod.rs:191-264`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/model/mod.rs#L191-L264); [`src/validation/mod.rs:227-239`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/validation/mod.rs#L227-L239) | `todo` is structurally possible as a custom type in this version, but is not a standard type or proven portable across clients. |
| Zero labels and multiple arbitrary labels are valid; the current validator caps an issue at 64 labels. | Confirmed for v0.3.2 | [`src/model/mod.rs:621-627`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/model/mod.rs#L621-L627); [`src/validation/mod.rs:241-257`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/validation/mod.rs#L241-L257) | Zero or multiple `ctx:`-shaped labels are structurally possible upstream; Hub meaning and safety remain wrapper policy. |
| Upstream exposes label add, remove, replace-all, and rename operations. Add/remove are idempotent and replacement can produce an empty set. | Confirmed for v0.3.2 | [`src/cli/mod.rs:1282-1292`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/cli/mod.rs#L1282-L1292); [`src/storage/sqlite.rs:12132-12200,12396-12443,12636-12739`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/storage/sqlite.rs#L12132-L12200) | The baseline mutation gap is an orchestration restriction, not an upstream inability to change labels. |
| Supported parent-child mutation gives a child at most one local parent; parent type need not be `epic`; external parent-child endpoints are rejected. | Confirmed for v0.3.2 | [`src/storage/sqlite.rs:11245-11324`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/storage/sqlite.rs#L11245-L11324); [`src/cli/commands/dep.rs:297-318`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/src/cli/commands/dep.rs#L297-L318) | A coordinating parent is structurally possible, but cross-workspace hierarchy constraints and Hub single-store interpretation require separate analysis. |
| JSONL preserves labels and dependencies, while SQLite mutation, auto-flush, import, merge, reconcile, and rebuild have different authority and failure semantics. | Confirmed for v0.3.2 | [`tests/jsonl_import_export.rs:20-115`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/tests/jsonl_import_export.rs#L20-L115); [`docs/SYNC_SAFETY.md:20-32`](https://github.com/Dicklesworthstone/beads_rust/blob/4104c31e79bf806f53e2eba0a4cd2ba6c594f8b9/docs/SYNC_SAFETY.md#L20-L32) | These remain distinct authority boundaries for later storage analysis. |

Upstream help text enumerates standard types while implementation accepts
custom types. This is a documentation/implementation tension, not evidence that
a custom type is a stable cross-client contract.

### Viewer Representation

| Finding | Status | Evidence | Consequence for discovery |
|---|---|---|---|
| Context membership is encoded only in `Issue.Labels`; `SourceRepo` is separate and is not linked to Hub contexts. | Confirmed | `pkg/model/types.go:67-94` | Membership, repository registration, and `SourceRepo` cannot be treated as one current field. |
| `Issue.Validate` imposes no label syntax, uniqueness, registration, or cardinality constraint. | Confirmed | `pkg/model/types.go:153-170`; `pkg/model/types_test.go:359-451` | Zero, one, multiple, duplicate, and unregistered context labels are representable at this layer. |
| The loader preserves labels and dependencies; it does not inject or remove contexts. | Confirmed | `pkg/loader/loader.go:776-849,1215-1232`; `pkg/loader/pool_test.go:12-77` | Parser acceptance is broader than supported workflow construction. |
| The Viewer knows five issue kinds but accepts any non-empty kind; it knows four dependency roles, while issue loading does not validate dependency roles. | Confirmed | `pkg/model/types.go:214-241,295-320`; `pkg/model/types_test.go:103-195,419-428` | Locally known, locally valid, loadable, and upstream-standard are different classifications. |
| Hub registration stores a context-to-path entry and does not mutate issue membership. Catalog counts use exact registered-label intersection and count a multi-context issue in each matching repository. | Confirmed | `pkg/hub/hub.go:65-99,460-480`; `pkg/hub/hub_test.go:236-274,437-461` | Registration, membership, and catalog discoverability are separate dimensions. |
| Hub config validation enforces a trimmed non-empty `ctx:` key but not the full `<repo>-<hash>` form stated by its diagnostic. | Confirmed | `pkg/hub/hub.go:559-568`; `pkg/hub/hub_test.go:146-175` | The current diagnostic is stronger than the implemented grammar. No stronger identity rule is validated here. |

Dependencies carry issue IDs but no endpoint repository identity
(`pkg/model/types.go:243-250`). The audited layers therefore do not establish
how context ownership authorizes or disambiguates graph edges.

### Current Viewer Presentation

| Finding | Status | Evidence | Consequence for discovery |
|---|---|---|---|
| Selected Hub repositories form an exact union: an issue appears when any selected context matches. All scope bypasses membership filtering. | Confirmed | `pkg/ui/repository_scope.go:151-195`; `pkg/ui/repository_scope_test.go:50-76` | A multi-context issue appears once in every matching scope; a zero-context issue appears only in all scope. |
| Empty picker selection and selecting the entire catalog both normalize to all repositories. There is no explicit zero-repository scope. | Confirmed | `pkg/ui/repository_scope.go:241-267`; `pkg/ui/repo_picker.go:146-168` | Zero selected repositories and zero issue memberships are unrelated states. |
| Lowercase `ctx:` labels are hidden in Hub presentation. One registered context becomes a friendly repository name; dense multi-context views show one name plus `+N`; details show all recognized names. | Confirmed | `pkg/ui/repository_scope.go:22-59`; `pkg/ui/delegate.go:177-193`; `pkg/ui/model.go:7975-7983`; `pkg/ui/repository_scope_test.go:336-408` | Zero-context and unregistered-context items currently have no explicit classification badge. |
| Scope precedes normal filters and recomputes candidate counts and derived views. Hidden blockers still affect actionable truth through canonical lookup. | Confirmed | `pkg/ui/model.go:7195-7303`; `pkg/ui/repository_scope.go:270-323`; `pkg/ui/graph.go:121-135`; `pkg/ui/repository_scope_test.go:226-325` | Project visibility and graph relevance are not identical. |
| Board dependency lookup is rebuilt from scoped candidates, while graph and main/insights detail retain canonical lookup. | Confirmed code path; focused cross-scope board test absent | `pkg/ui/board.go:461-476,1443-1463`; `pkg/ui/graph.go:121-135`; `pkg/ui/model.go:8081-8085` | Current surfaces can show different amounts of information about the same hidden dependency. |
| Tree reconstruction uses scoped candidates; a visible child whose parent is hidden can become a displayed root without an omission marker. | Confirmed code consequence; focused transition test absent | `pkg/ui/repository_scope.go:578-630`; `pkg/ui/tree.go:252-315` | Current hierarchy presentation can change structurally under scope. |
| History is admitted by visible issue ID. Once a multi-context issue is admitted, its commits from other repository contexts remain in its projected history. | Confirmed code path; focused multi-context test absent | `pkg/ui/repository_scope.go:633-726`; `pkg/ui/history.go:2631-2641,3267-3308` | Issue scope and commit-repository scope are not currently the same projection. |
| The picker changes session visibility only. Viewer exposes no repository assignment interaction; terminal issue editing treats labels as read-only comments. | Confirmed existing interaction; absence established from inspected dispatch | `pkg/ui/repo_picker.go:114-245`; `pkg/ui/model.go:4989-5039,9212-9232` | Current Viewer does not provide classification or transfer interactions. |

The board, graph, tree, detail, and history differences are current-state
asymmetries. This phase does not classify them as defects or requirements.

### Correlation Lifecycle

| Finding | Status | Evidence | Consequence for discovery |
|---|---|---|---|
| A new correlation requires a configured repository, a live bead carrying that context label, and a resolvable full commit; the persisted identity is bead ID, context, and full SHA. | Confirmed | `pkg/correlation/hub_config.go:46-100,230-235`; `pkg/correlation/beads_history.go:27-46` | Current membership authorizes writes, while branch names are not historical identity. |
| Full load validates every ledger record before selected-bead filtering. Each record must reference a loaded bead, configured context, current matching label, valid full SHA syntax, and a unique triple. | Confirmed | `pkg/correlation/hub_config.go:245-301`; `tests/e2e/external_history_test.go:459-477`; `docs/external-history.md:203-207` | One stale membership can block unrelated external-history reads. |
| Adding membership leaves existing records valid and only enables later writes. Removing a membership referenced by a record makes full external-history loading fatal. | Confirmed by current control flow; direct transition tests absent | `pkg/correlation/hub_config.go:280-289,354-360`; `pkg/correlation/beads_history.go:40-45` | Current placement and historical evidence are coupled. |
| Replacing context A with B does not reinterpret A records. A B write can succeed because the writer validates only its target and parses existing records without full semantic validation; later reads still fail on stale A records. | Confirmed by current control flow; direct test absent | `pkg/correlation/hub_config.go:77-128,245-301` | Write acceptance does not guarantee that the complete ledger remains readable. |
| Multiple current memberships and records are accepted when every record context is configured and still present on the bead. | Confirmed | `pkg/correlation/beads_history.go:40-45`; `pkg/correlation/hub_config.go:280-289`; `tests/e2e/external_history_test.go:68-72,587-624` | Correlation representation is broader than wrapper construction policy. |
| Missing or unavailable applicable repositories can produce partial reports, but malformed membership, missing commits in available repositories, or provider/data-integrity failures are fatal. | Confirmed | `pkg/correlation/external_git.go:41-76,118-221`; `tests/e2e/external_history_test.go:356-456`; `docs/external-history.md:262-283` | Availability and integrity failures are intentionally different classes today. |
| Context plus full SHA is durable source identity, but context-to-checkout path is live configuration and is not revalidated against the checkout origin during load. | Confirmed | `pkg/correlation/hub_config.go:230-263`; `pkg/correlation/external_git.go:43-48,185-203`; comparison with `pkg/hub/hub.go:355-378` | Stable repository identity and mutable repository location are separate later-phase inputs. |

## Cross-Track Reconciliation

| Apparent conflict | Reconciliation | Status |
|---|---|---|
| Upstream supports zero/multiple labels, but supported Hub workflows do not construct zero/multiple contexts. | Upstream and the Viewer describe representation; `wbd` describes the supported orchestration boundary. | Confirmed layer distinction |
| Upstream accepts custom types, Viewer recognizes five types, and the baseline wrapper permits a different finite set. | Standard upstream type, custom upstream type, locally known Viewer type, and wrapper-allowed type are different contracts. | Confirmed layer distinction; installed-version compatibility open |
| Viewer and correlation code can consume multiple contexts, while ordinary context mutation is prohibited. | Read/presentation and correlation containment checks are broader than mutation policy. | Confirmed layer distinction |
| Repository scope hides context-free issues, while hidden blockers can still affect readiness and some detail views. | Candidate projection and global graph truth are separate current mechanisms. | Confirmed current asymmetry; selected direction preserves global truth and defers presentation |
| Correlation writer can accept a new-context record after transfer while loader rejects the stale old-context record. | Write authorization checks the target record; load integrity checks the complete ledger. | Confirmed current asymmetry; transfer is not part of the selected direction |
| Hub config error text specifies a hash-shaped context, while validation checks only a non-empty `ctx:` prefix. | The diagnostic documents an expected convention that the inspected validator does not enforce. | Confirmed implementation/documentation mismatch |

Current-state evidence alone does not impose meaning on `todo`, make epics
multi-context, or derive project placement from graph role. The following
direction comes from the completed user questionnaire, not from those technical
capabilities.

## User-Selected Discovery Direction

These outcomes settle the direction of this discovery phase. Later requirements
and model work still need to make the boundaries precise and testable.

`Ordinary project-work bead` is a working category for existing non-epic,
non-todo work kinds, not a proposed literal issue type.

| Facet | Selected direction | Deferred boundary |
|---|---|---|
| Context lifecycle | Context membership is immutable and kind-specific after creation. | Validation timing and persisted representation |
| Epic | An epic carries one or more explicitly selected registered contexts. | Relationship semantics, child consistency, and presentation |
| Todo | A Beads-native todo carries zero or more explicitly selected registered contexts. Zero contexts covers both initially unscoped and intentionally repository-neutral work. | Upstream/client compatibility and detailed lifecycle semantics |
| Ordinary project work | An ordinary project-work bead carries exactly one context. | Exact set of included issue kinds |
| Creation targeting | Creation uses the current context implicitly or an explicitly selected registered target. | Command syntax, ambiguity handling, and error contract |
| Correction | A mistaken context is corrected with a new correctly scoped replacement and an auditable close or supersession of the original. The original context and history remain unchanged. | Relationship type and lifecycle details |
| Todo follow-on work | A todo remains durable when it leads to project work; resulting project-work beads link back to it rather than replacing it. | Relationship type and completion semantics |
| Correlation | Context membership is the repository-eligibility boundary for optional explicit correlation. Zero-context todos are ineligible; scoped records are eligible only in their selected contexts. | Enforcement and compatibility with existing ledger behavior |
| Read scope | The default scope is the current context, with selection of other registered contexts and contextless items. | UI and robot/API representation |
| Dependency truth | Repository filtering does not remove hidden dependencies from readiness or graph truth. | How hidden relationships are presented |

### Use-Case Disposition

| Use case | Discovery disposition |
|---|---|
| UC-01 | Retained: implicit current-context creation plus explicit registered targeting from elsewhere |
| UC-02 | Retained as creation of a durable zero-context Beads-native todo |
| UC-03 | Retained as the same zero-context todo representation, selectable through contextless scope |
| UC-04 | Superseded as a context-mutation workflow; later project work is created separately and linked to the durable todo |
| UC-05 | Superseded as transfer; correction uses replacement and close or supersession while preserving the original record |
| UC-06 | Retained with an epic carrying one or more immutable contexts |
| UC-07 | Retained with exactly one immutable context for ordinary project-work beads |
| UC-08 | Retained without a separate never-correlate flag; eligibility follows immutable context membership and actual correlation stays optional |
| UC-09 | Superseded as membership mutation; targeting occurs at creation |
| UC-10 | Retained with current-context default, selectable other/contextless scopes, and global dependency truth |
| UC-11 | Reframed: there is no cross-store promotion in the selected direction; durable todo and resulting project work remain linked records |

Questions formerly asking whether to choose mutable classification, transfer,
Viewer-owned intake, a separate neutral state, or a never-correlate policy are
closed for discovery validation. Questions about syntax, relationship type, UI
details, local compatibility, compatibility contingencies, and acceptance-test
wording move to later requirements or model work.

## Baseline Question Disposition

| Open question | Evidence added by validation | State after validation |
|---|---|---|
| OQ-01 `todo` meaning | Upstream v0.3.2 accepts custom types, so `todo` is structurally possible but not standard or proven portable. No current layer couples type to placement or correlation. | Discovery direction selected: durable Beads-native todo with zero-or-more immutable contexts; compatibility belongs to later requirements work |
| OQ-02 unclassified versus neutral UI | Both currently collapse to zero recognized contexts: visible in all scope, hidden in explicit scopes, and without a repository/classification badge. | Discovery direction selected: one zero-context todo state; presentation belongs to later requirements work |
| OQ-03 cross-project parent placement | Model permits labels independently of graph role. Upstream parent-child is single-parent/local through supported mutation. Scoped tree can omit hidden hierarchy nodes. | Discovery direction selected: epic carries one-or-more immutable contexts; relationship and child-consistency semantics move later |
| OQ-04 implementation-bead cardinality | Upstream, model, catalog, UI, and correlation code can represent multiple contexts; wrapper policy does not construct them. | Discovery direction selected: ordinary project-work bead exactly one; epic and todo are explicit cardinality exceptions |
| OQ-05 operation scope | Current explicit-ID, creation/listing, and UI interaction boundaries are asymmetric. | Discovery scope narrowed to immutable creation-time placement; authorization details for creation and existing explicit-ID operations move later |
| OQ-06 selecting a project from elsewhere | Registered catalog identities and paths exist; no inspected current creation flow offers explicit target selection. | Discovery direction selected: explicit registered target; syntax, ambiguity, and errors move later |
| OQ-07 transfer and old correlations | Removing old membership makes old-context ledger records fatal under current code. | Transfer is superseded; correction preserves the original context/history and creates a replacement, with relationship details deferred |
| OQ-08 never-correlate policy | History mode and actual correlation state are separate; no explicit prohibition is modeled. | Discovery direction selected: no separate prohibition; context limits eligibility and correlation remains optional |
| OQ-09 graph participation | Hidden blockers affect readiness and canonical detail, while scoped board/tree/history projections differ. | Discovery direction selected: preserve global dependency truth; presentation and contract tests move later |
| OQ-10 Hub-only versus local | Registration, global routing, explicit external ledger, and Hub refresh are Hub-only today. | Local compatibility is deliberately not finalized in this phase and moves to later requirements work |
| OQ-11 Viewer intake versus permanent todo | No inspected Viewer-owned intake or permanent-todo store exists. | Viewer-owned storage is not part of the selected direction; Beads-native todo proceeds to compatibility validation |
| OQ-12 promotion integrity | No promotion lifecycle exists. | Cross-store promotion is superseded by durable todo plus linked project work; relationship and lifecycle semantics move later |
| OQ-13 Viewer metadata for todo semantics | Current model has no dedicated classification metadata. | Viewer sidecar semantics are not part of the selected direction; no contingency implementation is selected in this phase |

## Later Requirements And Model Work

The completed questionnaire removes these items from discovery validation. They
remain technical work because the selected direction does not define their
contracts.

| Work area | Current evidence or direction | Deferred work |
|---|---|---|
| Upstream compatibility | Released `beads_rust` v0.3.2 accepts custom types, but the installed version and other clients were not validated. | Establish the supported version/client contract for Beads-native todo. |
| Ordinary-kind boundary | Ordinary project-work beads have exactly one context in the selected direction. | Identify which existing non-epic, non-todo kinds belong to that category. |
| Registered targeting | Registration and issue membership are separate, and config validation accepts broader keys than the origin-derived convention. | Define authoritative target identity, validation, ambiguity, and errors without selecting syntax here. |
| Epic relationships | Epics carry one-or-more contexts; upstream parent-child behavior has locality and cardinality constraints. | Define relationship and child-consistency semantics without selecting a relationship type here. |
| Todo follow-on work | A durable todo and resulting project-work bead remain separate linked records. | Define linkage, lifecycle, and completion semantics. |
| Correction | Context is immutable and mistakes produce a replacement while preserving the original. | Define close/supersession behavior, audit expectations, and relationship semantics. |
| Correlation | Context limits eligibility, while current ledger loading couples historical validity to live membership. | Define enforcement and existing-data behavior for immutable contexts; no transfer design is needed. |
| Scope contracts | Current scope hides contextless items unless all repositories are selected and presents multi-context records differently by view. | Define selectable contextless scope and deterministic UI/robot behavior. |
| Dependency projection | Current canonical graph paths preserve hidden blocker truth, while board and tree projections differ. | Specify and test global truth across scoped outputs without choosing presentation details here. |
| Local compatibility | Current Hub and local modes have different routing and history semantics. | Decide later which selected concepts, if any, apply in local mode. |
| Compatibility and migration | Existing one-context data, robot consumers, and correlation integrity remain compatibility surfaces. | Analyze non-destructive interpretation and contract impact after requirements and model semantics exist. |

Viewer-owned intake, permanent Viewer todo storage, mutable classification,
general transfer, and a separate never-correlate flag are not active handoff
alternatives under the selected direction. This phase does not choose a
contingency implementation if compatibility work later challenges that
direction.

## Handoff Boundary

Later work receives both the selected direction and these current-state
constraints:

- Storage and loader representation is broader than the selected kind-specific
  cardinalities, so representability does not establish policy.
- Upstream custom-type capability is version-specific evidence, not yet a
  supported-client contract.
- Repository registration, issue membership, scope projection, and correlation
  eligibility are separate current mechanisms.
- Correlation write validation and full-ledger load validation have different
  integrity boundaries.
- Candidate visibility and global dependency truth already diverge in some
  scoped views.
- `wbv` orchestration mode and `bv` history mode are separate current axes;
  local compatibility remains undecided.

Later requirements and model work may make these inputs precise. This phase does
not select command syntax, relationship type, UI details, local behavior,
contingency implementation, migration, MVP, or implementation sequence.

## Phase-Exit Assessment

| Exit criterion | Result | Basis |
|---|---|---|
| Current behavior is evidence-backed and layer-specific. | Pass | A2-A6 findings cite repository lines or immutable upstream evidence. |
| User direction is distinguishable from current-state capability. | Pass | Direction has its own section and is not presented as implemented behavior. |
| Candidate use cases have a discovery disposition. | Pass | UC-01 through UC-11 are retained, reframed, or superseded without leaving stale questionnaire prompts. |
| Baseline open questions are covered. | Pass | OQ-01 through OQ-13 identify either the selected direction or the later-phase boundary. |
| Cross-layer contradictions and asymmetries are reconciled. | Pass | Representation versus orchestration, scope versus graph truth, and correlation write versus load are explicitly separated. |
| Remaining technical work is bounded without implementation planning. | Pass | Compatibility, semantics, contracts, and testing concerns are assigned to later requirements/model work. |
| Prohibited design detail remains unselected. | Pass | No command syntax, relationship type, UI design, local policy, contingency implementation, MVP, or implementation sequence is chosen. |
| Parent acceptance of phase exit is recorded. | Pass | Parent review accepted the synthesis after questionnaire reconciliation and document checks. |

Discovery validation has sufficient evidence and direction to exit. That exit
authorizes later requirements and model work only; it does not approve a final
contract or implementation.
