# Hub Context And Scope Model Evaluation

## Document Status

| Field | Value |
|---|---|
| Status | Model accepted; lifecycle predicates, epic coordination constraint, and delta-validated correlation writes selected |
| Inputs | `docs/hub-context-and-scope-discovery.md`; `docs/hub-context-and-scope-discovery-validation.md`; `docs/hub-context-and-scope-requirements.md` |
| Requirements baseline | All 37 `HCS-*` normative requirements are Confirmed and act as hard gates |
| Scope | Domain authority, persistence ownership, invariant enforcement, lifecycle relationships, scope algebra, dependency truth, correlation integrity, and Hub/local compatibility boundaries |
| Out of scope | Command syntax, detailed UI or robot shape, migration execution, MVP selection, implementation tasks, sequencing, and estimates |

This evaluation compares complete models rather than selecting attractive
components independently. A high weighted score cannot rescue a model that
fails a Confirmed requirement. The accepted model is a semantic and authority
decision, not approval to implement it.

## Evaluation Method

### Evidence Notation

| Prefix | Meaning |
|---|---|
| `R:` | Confirmed requirement |
| `E:` | Validated current-state evidence |
| `G:` | Demonstrated evidence or contract gap |
| `A:` | Assumption required by a candidate |
| `I:` | Inference from requirements and evidence |
| `U:` | Unresolved question retained after evaluation |
| `C-H`, `C-M`, `C-L` | High, medium, or low confidence |

### Evidence Register

| ID | Statement | Source | Confidence |
|---|---|---|---|
| E-001 | Beads issue kind and labels are preserved through the current Viewer loader; Hub membership is currently encoded only in labels. | `DV:Viewer Representation` | C-H |
| E-002 | Released Beads v0.3.2 accepts custom non-empty issue kinds and zero or multiple arbitrary labels, but `todo` is not a standard kind and installed/supported-client behavior is unproven. | `DV:Upstream Beads Capabilities` | C-H for v0.3.2; C-L for the supported-client set |
| E-003 | Upstream label mutation is broader than Hub policy, so immutability is an orchestration invariant rather than an upstream storage invariant. | `DV:Upstream Beads Capabilities`; `DV:Cross-Track Reconciliation` | C-H |
| E-004 | Hub registration, issue membership, repository scope, and correlation eligibility are separate current mechanisms. | `DV:Handoff Boundary` | C-H |
| E-005 | Current selected repositories form an intersection-based union projection; a matching multi-context issue appears once. | `DV:Current Viewer Presentation` | C-H |
| E-006 | Canonical lookups preserve hidden blocker truth in some views, while scoped board and tree reconstruction do not preserve equal relationship detail. | `DV:Current Viewer Presentation` | C-H |
| E-007 | Correlation records use bead ID, context, and full commit SHA. Full reads validate the whole ledger, but writes currently validate only the new target and parse existing records without equivalent semantic validation. | `DV:Correlation Lifecycle` | C-H |
| E-008 | Hub and local modes have different routing and history semantics; registration and the explicit external ledger are Hub-only today. | `DV:Product And Mode Boundaries` | C-H |
| E-009 | Supported upstream parent-child mutation is local and single-parent for the child; it does not by itself establish the required todo, replacement, or cross-context epic semantics. | `DV:Upstream Beads Capabilities` | C-H |

### Hard Gates

Every coherent candidate was checked against these gates before scoring.

| Gate | Required property | Primary requirements |
|---|---|---|
| G-01 | Only registered contexts can be selected for membership. | HCS-INV-001, HCS-CRE-008 |
| G-02 | Creation establishes one complete membership set or creates no bead. | HCS-INV-002, HCS-CRE-007 through HCS-CRE-009 |
| G-03 | Membership cannot change after creation. | HCS-INV-003 |
| G-04 | Cardinality follows issue kind, not graph position or presentation. | HCS-INV-004 through HCS-INV-006 |
| G-05 | A todo is a durable Beads-native identity, including at zero contexts. | HCS-INV-005, HCS-LIF-001 |
| G-06 | Replacement preserves the original identity, history, and correlations. | HCS-LIF-003 through HCS-LIF-006, HCS-COR-006 |
| G-07 | Todo, replacement, and epic continuity is explicit and auditable rather than inferred from text or shared membership. | HCS-LIF-002, HCS-LIF-007, HCS-LIF-008 |
| G-08 | Scope supports current, explicit registered, contextless, intersection, and de-duplicated selection semantics without assigning deferred aggregate or mixed-scope meanings. | HCS-SCP-001 through HCS-SCP-005; HCS-BND-302, HCS-BND-303 |
| G-09 | Scope projection cannot change dependency truth or readiness. | HCS-INV-007, HCS-INV-008 |
| G-10 | Directly correlatable beads may own zero or more commit correlations and use immutable context membership as their repository boundary; todos are never directly correlatable. | HCS-COR-001 through HCS-COR-003, HCS-COR-005, HCS-COR-007 |
| G-11 | Rejected creation and correlation attempts have no persisted side effect. | HCS-CRE-008, HCS-CRE-009, HCS-COR-004 |
| G-12 | Every supported writer and round-trip path preserves the selected kind, membership, and lifecycle semantics. | HCS-BND-501; E-001 through E-003 |
| G-13 | Hub semantics do not silently reinterpret local-mode records or history. | HCS-BND-502; E-008 |
| G-14 | Omitted targeting, explicit complete-set targeting, and intentional contextless todo creation remain distinct creation intents. | HCS-CRE-001 through HCS-CRE-006 |

G-12 is a conditional gate because the evidence identifies, but does not yet
populate, the supported-client contract. A candidate may survive only by making
that profile an explicit activation precondition. Assuming universal client
support is a gate failure.

### Weighted Criteria

Ratings use a 0-5 scale. Weighted points equal `rating / 5 * weight`.

| Criterion | Weight | Evaluation question |
|---|---:|---|
| Requirements fidelity | 25 | Does the model cover the Confirmed behavior without reinterpretation? |
| Invariant enforceability | 15 | Can invalid states be rejected before persistence at one authority boundary? |
| Source-of-truth coherence | 15 | Is each fact owned once, with derived views clearly subordinate? |
| Lifecycle and auditability | 10 | Are durable identity, replacement, and coordination explicit? |
| Scope and graph correctness | 10 | Are projection and canonical dependency truth separated? |
| Correlation integrity | 10 | Are eligibility, atomic rejection, and historical attribution coherent? |
| Compatibility | 10 | Can an exact supported-client and existing-data contract be stated? |
| Simplicity | 5 | Does the model minimize authorities, inferred semantics, and exceptional paths? |

## Candidate Taxonomy

| Dimension | Candidate | Summary | Gate result |
|---|---|---|---|
| Domain | DOM-1 kind-indexed context set | Issue kind selects the cardinality rule for one immutable set of registered context identities. | Pass |
| Domain | DOM-2 uniform context set plus role exceptions | One generic set is constrained by lifecycle relationship roles. | Conditional; greater coupling between placement and graph state |
| Domain | DOM-3 relationship-derived placement | Membership is inferred from parents, children, or other edges. | Fail G-02 through G-04 and G-10 |
| Relationships | REL-1 existing relationship names alone | Existing typed edges are reused without a separate semantic contract. | Fail G-07 because E-009 does not establish the required meanings |
| Relationships | REL-2 generic role envelope | A generic relationship record carries an explicit lifecycle role. | Pass, but role validity becomes a second interpretation layer |
| Relationships | REL-3 dedicated semantic predicates | Todo result, supersession, and epic coordination are distinct domain predicates; physical encoding remains a later contract. | Pass |
| Persistence | PER-1 Beads kind plus reserved context labels | Beads owns identity, kind, lifecycle, and membership; reserved labels carry the context set. | Conditional on G-12 profile |
| Persistence | PER-2 new native Beads fields | New upstream structures own context and lifecycle semantics. | Fail evidence gate G-12 on current evidence |
| Persistence | PER-3 Hub side metadata | Hub metadata owns membership or kind semantics beside a Beads issue. | Fail G-02, G-11, and G-12 on current evidence because cross-authority atomicity and round trips are unproven |
| Enforcement | ENF-1 atomic Hub admission | Hub validates effective target, registration, kind cardinality, reserved metadata, and the complete proposed write before persistence. | Pass |
| Enforcement | ENF-2 post-write repair | Invalid records are created and then repaired or removed. | Fail G-02 and G-11 |
| Scope | SCP-1 selector algebra plus global analysis | Scope is a set union selector; canonical analysis runs globally and results are projected afterward. | Pass |
| Scope | SCP-2 induced scoped graph | Dependencies and analysis are rebuilt from only visible candidates. | Fail G-09 |
| Scope | SCP-3 implicit all aliases | Every selector, including explicit contextless selection, collapses to an all-items view. | Fail G-08 |
| Correlation | COR-1 monotonic delta-validated external ledger | Each append validates kind eligibility, context membership, the proposed association, and identity uniqueness without reinterpreting unrelated existing records; complete-ledger validation remains an integrity/read concern. | Pass |
| Correlation | COR-2 correlation embedded in issue data | Beads records directly own commit associations. | Fail evidence gate G-12; no authoritative supported-client round trip is proven |
| Correlation | COR-3 full-ledger-gated external ledger | Every append requires every historical record to remain valid under current state. | Conditional; stronger than HCS-COR-001 through HCS-COR-007 and prematurely selects HCS-BND-403 policy |
| Compatibility | CMP-1 exact supported-client profile | Only empirically verified clients and round trips are supported; there is no semantic fallback. | Pass conditionally |
| Compatibility | CMP-2 optimistic custom-kind compatibility | Any client accepting arbitrary strings is assumed to preserve all semantics. | Fail G-12 |
| Local boundary | LOC-1 shared Hub/local semantics | Local records acquire Hub context and external-ledger meaning. | Fail G-13 |
| Local boundary | LOC-2 Hub-only semantics | Hub interprets the model; local mode remains behaviorally unchanged and may passively preserve otherwise valid data. | Pass |

## Coherent Model Comparison

Only candidates that survive all requirement gates, possibly with an explicit
G-12 activation condition, are scored. All three models use DOM-1, ENF-1,
SCP-1, PER-1, CMP-1, and LOC-2.

| Model | Relationship model | Correlation model | Fidelity | Enforceability | Authority | Lifecycle | Scope | Correlation | Compatibility | Simplicity | Total |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| MODEL-A | REL-3 dedicated predicates | COR-1 monotonic delta validation | 5 | 5 | 5 | 5 | 5 | 5 | 3 | 5 | **96** |
| MODEL-B | REL-3 dedicated predicates | COR-3 full-ledger gate | 4 | 4 | 5 | 5 | 5 | 4 | 3 | 3 | **84** |
| MODEL-C | REL-2 generic role envelope | COR-1 monotonic delta validation | 4 | 4 | 4 | 4 | 5 | 5 | 3 | 5 | **83** |

### Comparison Findings

| Finding | Basis |
|---|---|
| MODEL-A best preserves one authority per fact. | Beads owns issue facts, the Hub registry owns selectable repository identities, and the external ledger owns only correlations. `I:E-001,E-004,E-007` |
| MODEL-A treats append authorization and historical integrity as separate obligations. | HCS-COR-001 through HCS-COR-007 constrain the proposed association; interpretation of conflicting existing records remains deferred by HCS-BND-403. `R:HCS-COR-001..007`; `E:E-007` |
| MODEL-B provides stronger resulting-ledger validity but creates global write coupling. | An unrelated stale bead, context, or repository can reject an otherwise valid association, selecting compatibility policy not required by the baseline. `I:E-007`; `R:HCS-BND-403` |
| MODEL-C is viable but makes every lifecycle reader interpret a generic role envelope before it can establish continuity. | Dedicated predicates express the three materially different obligations directly. `R:HCS-LIF-002,HCS-LIF-007,HCS-LIF-008` |
| New native fields are not scored despite conceptual cleanliness. | No supported-client evidence shows that such fields survive all writers and round trips. `G:G-12`; `E:E-002` |
| Hub side metadata is not scored despite local controllability. | It makes the Beads-native todo incomplete or creates competing lifecycle and membership authorities. `G:G-05`; `E:E-004` |

## Accepted Model: MODEL-A

MODEL-A is accepted with overall confidence **C-M**. Its semantic fit is C-H;
its feasibility is C-M until the supported-client profile and enforcement
coverage are demonstrated.

### Authority Map

| Fact | Authority | Derived consumers |
|---|---|---|
| Bead identity, issue kind, status, content, lifecycle history | Beads issue record | Hub orchestration, Viewer, analysis, direct-correlation kind eligibility |
| Registered context identity and live repository location | Hub registry | Creation target resolution, scope selection, correlation repository access |
| Immutable context membership | Reserved context labels in the Beads issue | Scope matching, cardinality checks, eligible repositories for directly correlatable beads, catalog counts |
| Todo-result, supersession, and epic-coordination continuity | Dedicated semantic relationship predicates associated with durable bead IDs | Lifecycle and audit views; physical Beads encoding deferred |
| Explicit source correlation | External ledger record `(bead identity, context identity, full commit SHA)` | External history and correlation views |
| Read scope | Ephemeral selector, never persisted as membership | Candidate projection only |
| Dependency truth and readiness | Canonical full Hub issue graph | Scoped projections and all analysis surfaces |

No derived consumer may override its authority. `SourceRepo`, caller location,
relationship shape, shared text, and selected scope do not create membership.

### Context Set And Kind Policy

For each issue `i`, `C(i)` is a mathematical set of stable registered-context
identities. Order and duplicate input have no semantic meaning. At successful
creation, `C(i)` is complete and becomes immutable.

| Issue category | Valid membership cardinality |
|---|---|
| `todo` | Zero or more |
| `epic` | One or more |
| Ordinary project work: `task`, `bug`, `feature`, `chore` | Exactly one |
| `decision`, `docs`, `question`, and custom kinds | Not classified by this requirements baseline; no cardinality is inferred |

The unclassified kinds remain U-001 rather than being silently treated as
ordinary work, todo, or epic. Their existing records remain Beads records, but
MODEL-A does not claim Hub creation semantics for them.

### Creation Intent And Admission

Creation has three distinct semantic intents:

| Intent | Effective context set |
|---|---|
| Default target | The singleton registered current context |
| Explicit target set | Exactly the set of explicitly selected registered contexts |
| Intentional contextless | The empty set, valid only for `todo` |

Omission means default targeting and is never interpreted as intentional
contextlessness. A non-contextless default request without a registered current
context is invalid. The exact command or API representation of these intents is
deferred.

The Hub admission boundary validates the complete proposed state before writing:

1. Resolve each selected identity exactly through the registry.
2. Construct the complete de-duplicated context set.
3. Validate the issue kind and its cardinality.
4. Reject caller attempts to bypass or mutate reserved context metadata.
5. Persist the Beads issue only after every validation succeeds.

Admission is all-or-nothing. Post-creation mutation of issue content or status
cannot alter `C(i)`. Every supported Hub writer must pass through this boundary;
otherwise MODEL-A's immutability claim is not valid.

### Lifecycle Relationships

MODEL-A selects three dedicated semantic predicates without selecting their
physical dependency type, field shape, or command syntax:

| Predicate | Direction | Required meaning |
|---|---|---|
| `results-in` | todo -> project-work bead | The todo remains durable; the resulting work exposes continuity to it. |
| `superseded-by` | original -> replacement | The replacement has its own identity and correct immutable membership; the original retains identity, history, membership, and correlations and exposes a terminal supersession outcome. |
| `coordinates` | epic -> project-work bead | The epic explicitly identifies coordinated work; shared membership alone is insufficient. |

For `coordinates`, each coordinated ordinary project-work bead's sole context
must belong to the epic's context set. The relationship does not derive or
change either endpoint's membership. Todo completion after resulting work, the
exact status representation of supersession, and the physical encoding of all
three predicates remain separate contract work.

The dedicated predicates and the rule limiting coordinated ordinary work to
the epic's context set are accepted model decisions.

### Scope Algebra

A scope selector is one of `AllItems`, `SelectedContexts(K)`, or `Contextless`,
where `K` is a non-empty set of registered context identities. Under
`SelectedContexts`, an issue matches exactly when:

```text
C(i) intersects K
```

Under `Contextless`, an issue matches exactly when `C(i)` is empty. Set
semantics ensure each matching issue appears at most once. This phase does not
define a selector that composes `SelectedContexts` and `Contextless`.

| Selection | Semantic value |
|---|---|
| Current-context default | `SelectedContexts({current})` |
| One or more explicit contexts | `SelectedContexts(K)` |
| Contextless only | `Contextless` |
| Mixed registered and contextless | No selector selected; remains HCS-BND-303 |
| Empty interface selection | Not a distinct model selector in this phase; current Viewer normalization maps it to `AllItems` |
| All registered contexts | Whether this is distinct from `AllItems` remains HCS-BND-302 |
| All items | `AllItems`; includes every issue identity once without membership matching |

The selector algebra has no empty-selection value. Current Viewer behavior
normalizes an empty selection and a selection containing the complete catalog
to `AllItems`; this model preserves that behavior while leaving the final
aggregate contract in HCS-BND-302. Mixed registered/contextless scope and
missing-current behavior likewise remain HCS-BND-303 and HCS-BND-304 rather
than being selected here.

Scope is applied only to the candidate projection. Dependency resolution,
readiness, and graph metrics use the canonical full Hub issue graph first. A
hidden blocker remains a blocker. Presentation of hidden edges and hierarchy is
deferred, but no surface may recompute authoritative readiness from only visible
candidates.

### Correlation Integrity

For every non-todo issue `i`, the eligible repository set is exactly `C(i)`.
Correlation remains optional and each eligible issue may own multiple commit
correlations. A todo is never directly correlatable, regardless of whether its
context set is empty or non-empty. Its resulting project work owns any source
correlations while `results-in` preserves continuity to the todo.

The external ledger remains the sole correlation authority. An append validates
the proposed association's stable bead ID, immutable membership, registered
context identity, full SHA and commit resolution, plus ledger-wide identity
uniqueness. It then preserves every unrelated existing record and its original
attribution. An ineligible or otherwise invalid proposed association has no
side effect.

Complete-ledger semantic validation remains an integrity/read concern. An
unrelated stale historical record does not disable an otherwise valid append,
and a successful append does not certify or reinterpret that stale record.
Repair or migration of conflicting existing records remains HCS-BND-403.

The delta-validation approach is accepted because it preserves the established
`beads_viewer` separation between write authorization and complete-ledger
integrity. The append remains atomic and does not certify, repair, or reinterpret
unrelated history.

Replacement creates no correlation transfer. Existing records remain
attributed to the original identity. Repository unavailability may still be
reported separately from integrity failure; eligibility does not depend on the
caller's checkout.

### Compatibility Envelope

MODEL-A is active only for an exact supported-client profile whose tested paths
all preserve:

- the Beads-native `todo` kind;
- zero and multiple reserved context labels;
- unknown non-context labels without destructive normalization;
- immutable reserved-label policy at every supported Hub writer;
- the selected lifecycle relationship semantics; and
- deterministic issue and relationship round trips through the authoritative
  store and exported representation.

Acceptance of an arbitrary custom kind string is necessary but not sufficient.
There is no alias-to-task, Viewer-side todo, mutable-context, or sidecar fallback
inside this model. If no adequate profile can be demonstrated, MODEL-A remains
semantically selected but is not feasible to activate; that outcome requires a
new requirements decision rather than silent substitution.

Hub interpretation is opt-in through Hub orchestration. Local mode keeps its
existing routing and Git-history semantics. A local reader may passively retain
otherwise valid kinds or labels, but it does not acquire Hub registration,
membership cardinality, scope, or external-ledger semantics by analogy.

## Requirement Compliance Matrix

`Covered` means MODEL-A directly provides the obligation. `Conditional` means
the semantics are covered but activation depends on the exact compatibility or
writer-coverage evidence stated above.

| Requirement | Result | MODEL-A coverage | Evidence or condition |
|---|---|---|---|
| HCS-INV-001 | Covered | Admission resolves every selected identity through the Hub registry. | R:HCS-INV-001; E-004 |
| HCS-INV-002 | Covered | Admission constructs and validates the complete set before one write. | R:HCS-INV-002; G-02 |
| HCS-INV-003 | Conditional | Reserved context labels are immutable after creation across every supported Hub writer. | R:HCS-INV-003; A-001 |
| HCS-INV-004 | Covered | Epic policy requires one or more contexts. | R:HCS-INV-004 |
| HCS-INV-005 | Conditional | Beads-native todo policy permits zero or more contexts. | R:HCS-INV-005; E-002; A-002 |
| HCS-INV-006 | Covered | Task, bug, feature, and chore require exactly one context. | R:HCS-INV-006 |
| HCS-INV-007 | Covered | Canonical global graph precedes scope projection. | R:HCS-INV-007; E-006 |
| HCS-INV-008 | Covered | Readiness is computed globally, so hidden blockers remain effective. | R:HCS-INV-008; E-006 |
| HCS-CRE-001 | Covered | Omitted targeting resolves only to the singleton registered current context. | R:HCS-CRE-001 |
| HCS-CRE-002 | Covered | Explicit selection supplies the entire set and never adds current context. | R:HCS-CRE-002 |
| HCS-CRE-003 | Covered | Contextless is a distinct intent from omission/default. | R:HCS-CRE-003 |
| HCS-CRE-004 | Conditional | Intentional empty membership is valid for Beads-native todo. | R:HCS-CRE-004; A-002 |
| HCS-CRE-005 | Conditional | Explicit de-duplicated registered sets of any size are valid for todo. | R:HCS-CRE-005; A-002 |
| HCS-CRE-006 | Covered | Explicit registered sets of size one or more are valid for epic. | R:HCS-CRE-006 |
| HCS-CRE-007 | Covered | Kind policy is checked before persistence. | R:HCS-CRE-007; G-02 |
| HCS-CRE-008 | Covered | Any unresolved explicit identity rejects the complete request without a write. | R:HCS-CRE-008; G-11 |
| HCS-CRE-009 | Covered | Missing current context invalidates a non-contextless omitted-target request before persistence. | R:HCS-CRE-009; G-11 |
| HCS-LIF-001 | Conditional | `results-in` preserves the durable todo endpoint rather than replacing it. | R:HCS-LIF-001; A-003 |
| HCS-LIF-002 | Conditional | Resulting work exposes the dedicated `results-in` continuity predicate. | R:HCS-LIF-002; A-003 |
| HCS-LIF-003 | Covered | Correction creates a new identity with its own validated membership. | R:HCS-LIF-003 |
| HCS-LIF-004 | Covered | `superseded-by` does not replace or rewrite the original identity. | R:HCS-LIF-004 |
| HCS-LIF-005 | Covered | Beads history remains owned by the original issue identity. | R:HCS-LIF-005 |
| HCS-LIF-006 | Conditional | Original exposes a terminal supersession outcome; exact status encoding is deferred. | R:HCS-LIF-006; U-003 |
| HCS-LIF-007 | Conditional | `superseded-by` explicitly associates both durable identities. | R:HCS-LIF-007; A-003 |
| HCS-LIF-008 | Conditional | `coordinates` explicitly exposes epic coordination across its context set. | R:HCS-LIF-008; A-003 |
| HCS-SCP-001 | Covered | Default selector is the singleton registered current context. | R:HCS-SCP-001 |
| HCS-SCP-002 | Covered | Explicit registered selector replaces the default selector. | R:HCS-SCP-002 |
| HCS-SCP-003 | Covered | The dedicated `Contextless` selector includes zero-membership items. | R:HCS-SCP-003 |
| HCS-SCP-004 | Covered | Matching uses non-empty set intersection. | R:HCS-SCP-004; E-005 |
| HCS-SCP-005 | Covered | Set projection returns each issue identity at most once. | R:HCS-SCP-005; E-005 |
| HCS-COR-001 | Covered | External ledger accepts multiple optional immutable full-SHA records for directly correlatable beads. | R:HCS-COR-001; E-007 |
| HCS-COR-002 | Covered | A directly correlatable bead's eligible repository set is exactly its immutable context set. | R:HCS-COR-002 |
| HCS-COR-003 | Covered | Validation of the proposed association rejects an ineligible append. | R:HCS-COR-003; G-10 |
| HCS-COR-004 | Covered | The proposed association is appended only after its validation succeeds. | R:HCS-COR-004; G-11 |
| HCS-COR-005 | Covered | Empty correlation history is valid and requires no policy flag. | R:HCS-COR-005 |
| HCS-COR-006 | Covered | Records remain keyed to the original identity after `superseded-by`. | R:HCS-COR-006; E-007 |
| HCS-COR-007 | Covered | Todo kind makes direct correlation ineligible; resulting project work owns source correlations. | R:HCS-COR-007 |

All 37 requirements have a disposition. The Conditional rows do not weaken a
requirement; they identify evidence that must exist before the model can be
claimed as operationally enforceable.

## Boundary Dispositions

| Boundary | Disposition | MODEL-A decision or retained limit |
|---|---|---|
| HCS-BND-101 | Constrained/deferred | Default, explicit, and contextless intents are semantically distinct; their command, UI, and machine-output representation is deferred. |
| HCS-BND-102 | Resolved at model level | Membership uses exact stable registry identities. Aliases and target grammar are not assumed; interface ambiguity handling is deferred. |
| HCS-BND-103 | Constrained/deferred | Intentional contextless is a distinct creation variant valid only for todo; invocation syntax is deferred. |
| HCS-BND-104 | Resolved | Membership is a mathematical set: duplicates and order have no semantic effect. Invalid kind cardinality rejects the request. |
| HCS-BND-105 | Unresolved | No policy is inferred for decision, docs, question, or custom kinds. New requirements are needed. |
| HCS-BND-201 | Resolved at semantic level | Dedicated `results-in` and `superseded-by` predicates express continuity; physical representation is deferred. |
| HCS-BND-202 | Unresolved | MODEL-A does not select whether resulting work completes, closes, or leaves a todo open. |
| HCS-BND-203 | Constrained | Correction selects a terminal supersession outcome and preserves all original facts; exact status, audit fields, and transaction contract are deferred. |
| HCS-BND-204 | Resolved at semantic level | `superseded-by` is the formal directional association; physical encoding remains deferred. |
| HCS-BND-205 | Resolved at semantic level | `coordinates` is explicit, and each coordinated ordinary work context belongs to the epic's set. Physical relationship encoding and presentation are deferred. |
| HCS-BND-301 | Constrained/deferred | Current, explicit registered, and contextless scopes have exact semantic values; interface exposure is deferred. Mixed and aggregate semantics remain separate boundaries. |
| HCS-BND-302 | Unresolved | The internal algebra distinguishes `AllItems` from selected contexts, but aggregate interface meanings remain deferred. Current Viewer empty/full-catalog normalization to `AllItems` is preserved. |
| HCS-BND-303 | Unresolved | Contextless selection is supported; whether it composes with registered-context selection remains deferred. |
| HCS-BND-304 | Unresolved for reads | Creation follows HCS-CRE-009. Missing-current read behavior remains requirements and interface work. |
| HCS-BND-305 | Constrained/deferred | Global analysis before projection is authoritative; hidden-edge and hierarchy presentation is deferred. |
| HCS-BND-401 | Constrained/deferred | Rejection is side-effect free; exact error and machine-readable shape are deferred. |
| HCS-BND-402 | Constrained/deferred | Optional absence and original attribution are semantic facts; presentation is deferred. |
| HCS-BND-403 | Constrained/deferred | Appends preserve and do not reinterpret unrelated existing records. Their read interpretation, repair, and migration remain separate analysis. |
| HCS-BND-501 | Constrained; evidence required | Activation requires an exact tested supported-client profile. The current evidence does not yet populate that profile. |
| HCS-BND-502 | Resolved for this model | Semantics are Hub-only. Local behavior remains unchanged and gains no inferred Hub policy. |
| HCS-BND-503 | Resolved at authority level | Beads issue kind plus reserved context labels own the kind-indexed immutable set; exact schema mechanics and implementation remain deferred. |

## Assumptions, Gaps, And Unresolved Questions

### Required Assumptions

| ID | Assumption | Impact if false | Confidence |
|---|---|---|---|
| A-001 | Every supported Hub write path that can affect kind or labels can be routed through atomic admission and can reserve context metadata from later mutation. | Immutability and atomic cardinality cannot be guaranteed; MODEL-A fails G-02 and G-03. | C-M |
| A-002 | At least one supportable client profile preserves Beads-native `todo` and zero/multiple context labels across authoritative round trips. | Confirmed todo requirements are infeasible under PER-1; no fallback is preselected. | C-L |
| A-003 | Dedicated lifecycle predicate semantics can be persisted and preserved by that same profile without relying on text inference. | Continuity and coordination fail G-07. | C-M |
| A-004 | Stable context identity can remain distinct from a repository's mutable checkout path and temporary availability. | Historical membership and correlation identity can be accidentally tied to live location. | C-M |

### Evidence Gaps

| ID | Gap | Needed evidence, without prescribing implementation |
|---|---|---|
| G-SCP-001 | Cross-surface tests do not establish identical hidden-blocker truth for board, tree, graph, detail, history, and robot outputs. | Contract-level proof that every authoritative readiness/analysis surface uses the canonical graph before projection. |
| G-CMP-001 | Installed and supported Beads clients have not been identified and exercised for `todo`. | Versioned client matrix covering create, mutate unrelated fields, export, import, merge/reconcile, and reload. |
| G-CMP-002 | Relationship round-trip preservation is not established for the selected semantic predicates. | Evidence for a representation that preserves predicate identity and endpoints across every supported path. |
| G-ENF-001 | Current code evidence does not establish one complete Hub write boundary for all label and kind mutations. | Enumerated supported writer set with proof that no writer can bypass reserved-metadata validation. |
| G-COR-001 | Focused tests do not cover append behavior when an unrelated existing record is stale. | Contract evidence that a valid delta append preserves the unrelated record and does not claim to repair or certify complete-ledger integrity. |

### Unresolved Questions

| ID | Question | Owner phase |
|---|---|---|
| U-001 | What context cardinality applies to decision, docs, question, and custom kinds? | Requirements |
| U-002 | What lifecycle outcome follows when a todo has resulting project work? | Lifecycle requirements |
| U-003 | What concrete status and audit representation realizes terminal supersession? | Lifecycle and contract |
| U-004 | What physical Beads representation realizes each dedicated semantic predicate? | Model/storage contract |
| U-005 | What exact stable context key grammar and ambiguity contract is exposed to callers? | Target and interface contract |
| U-006 | How are hidden dependency, parent, and lifecycle relationships represented in scoped outputs? | UI and robot contract |
| U-007 | How are pre-existing invalid membership or ledger records repaired without reinterpretation? | Compatibility and migration analysis |
| U-008 | Which concrete client versions and round trips form the supported profile? | Compatibility analysis |
| U-009 | How does registry retirement affect live repository access while preserving immutable historical context identity? | Registry lifecycle requirements |

## Excluded-Concept Check

| Excluded or superseded concept | MODEL-A treatment |
|---|---|
| Mutable membership after creation | Not present; correction creates a replacement. |
| General transfer | Not present; `superseded-by` preserves both immutable records. |
| Viewer-owned intake or permanent todo | Not present; todo remains a Beads identity. |
| Separate unclassified versus neutral state | Not present; a zero-context todo is one state. |
| Separate per-bead never-correlate policy | Not present; kind and membership determine eligibility, todos are excluded by lifecycle role, and actual project-work correlation is optional. |
| Relationship-derived membership | Not present; relationships never create or change `C(i)`. |
| Scope-derived dependency truth | Not present; canonical graph truth precedes projection. |
| Local-mode reinterpretation | Not present; MODEL-A is Hub-only. |
| Compatibility fallback | Not present; failure to prove the profile blocks activation and returns to requirements review. |

## Decision Record

| Field | Decision |
|---|---|
| Accepted model | MODEL-A: kind-indexed immutable Beads context sets, atomic Hub admission, dedicated lifecycle predicates, explicit scope algebra over canonical global graph truth, monotonic delta-validated external correlations, exact client compatibility profile, and Hub-only semantics |
| Why selected | Highest requirements fidelity and authority coherence without assigning deferred aggregate-scope or existing-ledger policy |
| Rejected nearest alternative | MODEL-B globally gates valid new correlations on unrelated historical integrity; MODEL-C additionally introduces a generic lifecycle interpretation layer |
| Activation condition | A-001 through A-004 and G-SCP-001, G-CMP-001, G-CMP-002, G-ENF-001, and G-COR-001 require evidence; especially the exact supported-client profile |
| Review disposition | Dedicated lifecycle predicates, the epic context-consistency rule, and monotonic delta validation for correlation writes are accepted |
| Evaluation outcome | MODEL-A is accepted as the semantic model for later contract work |
| Not authorized by acceptance | Production changes, command or UI design, migration, MVP selection, implementation sequencing, or task creation |

## Phase-Exit Assessment

| Criterion | Result |
|---|---|
| Every Confirmed requirement acts as a hard gate and has a MODEL-A disposition. | Pass; 37 of 37 covered or explicitly conditional on feasibility evidence |
| Whole models, not isolated components, were compared. | Pass; three gate-surviving bundles were scored |
| Every requirements boundary has a disposition. | Pass; resolved, constrained/deferred, or unresolved |
| Current evidence, assumptions, inference, and unresolved questions remain distinguishable. | Pass; notation and dedicated registers are used |
| Source-of-truth ownership is singular for issue facts, registry facts, and correlation facts. | Pass in the accepted model |
| Scope cannot alter canonical dependency truth. | Pass in the accepted model; cross-surface evidence remains G-SCP-001 |
| Compatibility uncertainty is not hidden by a fallback. | Pass; exact client proof is an activation condition |
| Superseded workflows remain excluded. | Pass |
| Prohibited interface and implementation decisions remain unselected. | Pass |
| The model decision is recorded without authorizing implementation. | Pass |

MODEL-A is ready for later contract work. It is not ready for implementation
until its activation conditions are demonstrated.
