# Hub Context And Scope Requirements

## Document Status

| Field | Value |
|---|---|
| Status | Requirements synthesis complete; all 37 normative requirements are Confirmed |
| Inputs | `docs/hub-context-and-scope-discovery.md`; `docs/hub-context-and-scope-discovery-validation.md` |
| Scope | Externally observable Hub behavior for context membership, creation, lifecycle continuity, read scope, dependency truth, and explicit source correlation |
| Interpretation | Validated current state constrains the requirements; user-selected discovery direction supplies the desired behavior |
| Out of scope | Command syntax, relationship type, UI design, storage representation, local-mode policy, compatibility fallback, migration, MVP, and implementation planning |

This document does not select a final domain or storage model. It converts the
accepted discovery direction into atomic, traceable behavioral requirements and
records unresolved mechanisms separately in a non-normative boundary ledger.

## Source Aliases

| Alias | Source |
|---|---|
| `D` | `docs/hub-context-and-scope-discovery.md` |
| `DV` | `docs/hub-context-and-scope-discovery-validation.md` |
| `MVP` | User-selected direction during MVP definition |

Traceability uses stable section and row names rather than depending only on
line numbers. A direction source explains why behavior is desired. An evidence
source records a current constraint or capability but does not create a
requirement by itself. Where the selected behavior has no current workflow, the
evidence field says `Direction-only` rather than presenting intent as evidence.

## Glossary

| Term | Behavioral meaning |
|---|---|
| Context | Stable identity of a registered Hub repository |
| Registered context | Context available for explicit selection |
| Current context | Registered context derived from the caller's current worktree |
| Context membership | Project association selected when a bead is created |
| Contextless | Having zero context memberships |
| Ordinary project-work bead | Working category for implementation types `task`, `bug`, `feature`, and `chore`; not a proposed literal issue type |
| Todo | Durable Beads-native work item with zero or more contexts |
| Epic | Coordinating work item with one or more contexts |
| Explicit target | Registered context selected instead of the current-context default |
| Immutable membership | Context membership does not change after creation |
| Replacement | New correctly scoped bead created to correct mistaken placement |
| Supersession | Auditable indication that an original bead was replaced; no relationship type is selected here |
| Explicit correlation | Optional association between a directly correlatable bead and an immutable commit in an eligible repository |
| Directly correlatable bead | Any bead except a todo; resulting project work, not its source todo, owns commit correlations |
| Scope | Selected projection of Hub issues |
| Global dependency truth | Dependencies hidden by scope continue to govern readiness and graph semantics |

## Requirement Conventions

### Taxonomy

| Prefix | Responsibility |
|---|---|
| `HCS-INV-###` | Cross-cutting context and dependency invariants |
| `HCS-CRE-###` | Creation and registered-target selection |
| `HCS-LIF-###` | Durable lineage, correction, and coordination lifecycle |
| `HCS-SCP-###` | Read scope and candidate projection |
| `HCS-COR-###` | Explicit source-correlation behavior |
| `HCS-BND-###` | Non-normative deferred boundary |

IDs remain stable after assignment. Retired requirements retain their IDs with
`Superseded` status rather than being reused.

### Normative Language And Status

| Term | Rule |
|---|---|
| `SHALL` / `SHALL NOT` | Required externally observable behavior |
| `MAY` | Genuine caller-visible optionality |
| Proposed | Synthesized requirement awaiting parent review; no normative requirements remain in this status |
| Confirmed | Requirement explicitly accepted during requirements review |
| Deferred | Non-normative work assigned to a later phase |
| Superseded | Retained for traceability but no longer normative |

Every requirement contains one primary obligation. Examples are
interface-neutral and do not prescribe commands, fields, widgets, or storage.
Boundary records contain no normative language.

### Requirement Record

| Field | Purpose |
|---|---|
| ID and status | Stable identity and review state |
| Normative statement | One externally observable obligation |
| Positive example | Successful behavior in Given/When/Then form |
| Negative or boundary example | Rejection, prohibited interpretation, or edge behavior |
| Direction | User-selected discovery source |
| Evidence | Validated current-state constraint, when relevant |
| Coverage | Related use cases and baseline questions |
| Boundaries | Unresolved mechanisms that affect the requirement |

## Shared Invariants

| ID | Status | Normative statement | Positive example | Negative or boundary example | Direction | Evidence | Coverage | Boundaries |
|---|---|---|---|---|---|---|---|---|
| HCS-INV-001 | Confirmed | Every context selected for membership SHALL be a registered context. | Given registered A, when A is selected at creation, then A can become membership. | An unregistered identity cannot become membership. | `DV:User-Selected Discovery Direction/Creation targeting` | `DV:Viewer Representation/Hub registration` | UC-01, UC-06, UC-07; OQ-06 | HCS-BND-102 |
| HCS-INV-002 | Confirmed | A bead's complete context membership SHALL be established when the bead is created. | Given selected A and B for a todo, when creation succeeds, then its initial membership is A and B. | Creation cannot leave a partially assigned context set for later completion. | `DV:User-Selected Discovery Direction/Context lifecycle` | `D:Current Behavior/Context mutation` | UC-01, UC-02, UC-06, UC-07; OQ-04, OQ-05 | HCS-BND-104 |
| HCS-INV-003 | Confirmed | A bead's context membership SHALL remain unchanged after creation. | Given a bead created in A, when its later lifecycle is inspected, then its membership remains A. | Correction cannot rewrite the original membership. | `DV:User-Selected Discovery Direction/Context lifecycle` | `DV:Correlation Lifecycle/placement-history coupling` | UC-04, UC-05, UC-09; OQ-05, OQ-07 | HCS-BND-203 |
| HCS-INV-004 | Confirmed | An epic SHALL have at least one context membership. | Given epic E created for A, then E satisfies the minimum membership. | A zero-context epic is not valid. | `DV:User-Selected Discovery Direction/Epic` | `DV:Viewer Representation/multiple contexts` | UC-06; OQ-03 | HCS-BND-205 |
| HCS-INV-005 | Confirmed | A todo SHALL permit zero or more context memberships. | Given a neutral todo, then zero memberships is valid; given A and B, both memberships are valid. | Todo validity cannot depend on having exactly one context. | `DV:User-Selected Discovery Direction/Todo` | `DV:Upstream Beads Capabilities/zero and multiple labels` | UC-02, UC-03; OQ-01, OQ-02 | HCS-BND-104, HCS-BND-501 |
| HCS-INV-006 | Confirmed | An ordinary project-work bead SHALL have exactly one context membership. | Given a task, bug, feature, or chore created for A, then its membership is exactly A. | Zero or multiple contexts are not valid for those implementation types. | `DV:User-Selected Discovery Direction/Ordinary project work`; explicit requirements review | `DV:Viewer Representation/context cardinality` | UC-01, UC-07; OQ-04 | None |
| HCS-INV-007 | Confirmed | Applying a read scope SHALL NOT remove dependencies from global dependency truth. | Given visible W depends on hidden B, when B is excluded from candidates, then the dependency still exists for analysis. | Filtering B cannot erase the dependency. | `DV:User-Selected Discovery Direction/Dependency truth` | `DV:Current Viewer Presentation/hidden blockers` | UC-10; OQ-09 | HCS-BND-305 |
| HCS-INV-008 | Confirmed | Hiding a blocker through read scope SHALL NOT make blocked work ready. | Given W is blocked only by hidden B, when scope hides B, then W remains blocked. | W cannot become actionable solely because B is outside the projection. | `DV:User-Selected Discovery Direction/Dependency truth` | `DV:Current Viewer Presentation/canonical actionable truth` | UC-10; OQ-09 | HCS-BND-305 |

## Creation Requirements

| ID | Status | Normative statement | Positive example | Negative or boundary example | Direction | Evidence | Coverage | Boundaries |
|---|---|---|---|---|---|---|---|---|
| HCS-CRE-001 | Confirmed | When explicit context selection is omitted, creation SHALL derive its effective target from the current context. | Given current A, when ordinary work is created without explicit selection, then A is the effective target. | Omission cannot silently select unrelated B. | `DV:User-Selected Discovery Direction/Creation targeting` | `D:Current Behavior/Creation` | UC-01, UC-07; OQ-06 | HCS-BND-101, HCS-BND-304 |
| HCS-CRE-002 | Confirmed | When contexts are explicitly selected, creation SHALL derive complete membership from that selection rather than from the current context. | Given no current context and explicit registered A, when creation succeeds, then membership comes from A. | A differing current context cannot be added implicitly to the explicit selection. | `DV:User-Selected Discovery Direction/Creation targeting` | `D:Current Behavior/Context selection during creation` | UC-01, UC-06, UC-07; OQ-06 | HCS-BND-101, HCS-BND-102, HCS-BND-104 |
| HCS-CRE-003 | Confirmed | Creation SHALL distinguish an intentional contextless todo request from omission that invokes the current-context default. | Given current A and intentional contextless creation, then the todo has zero contexts. | Merely omitting explicit selection cannot be interpreted as contextless. | `DV:User-Selected Discovery Direction/Todo, Creation targeting` | `D:Current Behavior/Creation, Zero contexts` | UC-02, UC-03; OQ-02, OQ-06 | HCS-BND-103 |
| HCS-CRE-004 | Confirmed | A caller MAY create a contextless todo. | Given intentional zero-context capture, when creation succeeds, then the todo is contextless. | This outcome does not apply to an epic or ordinary project work. | `DV:User-Selected Discovery Direction/Todo` | `DV:Validated Current State/Upstream Beads Capabilities, Viewer Representation` | UC-02, UC-03; OQ-01, OQ-02 | HCS-BND-103, HCS-BND-501 |
| HCS-CRE-005 | Confirmed | A caller MAY create a todo with multiple explicitly selected registered contexts. | Given registered A and B, a todo can be created with both memberships. | Unregistered C cannot join the selection. | `DV:User-Selected Discovery Direction/Todo` | `DV:Upstream Beads Capabilities/zero and multiple labels` | UC-02, UC-03; OQ-01 | HCS-BND-104, HCS-BND-501 |
| HCS-CRE-006 | Confirmed | A caller MAY create an epic with multiple explicitly selected registered contexts. | Given registered A and B, an epic can be created for both. | A zero-context epic cannot be created. | `DV:User-Selected Discovery Direction/Epic` | `DV:Viewer Representation/multiple contexts` | UC-06; OQ-03 | HCS-BND-104 |
| HCS-CRE-007 | Confirmed | Creation SHALL NOT create a bead whose requested membership violates its applicable kind-specific cardinality invariant. | Given a two-context epic, creation may proceed. | Given zero contexts for an epic or two for ordinary work, no bead is created. | `DV:User-Selected Discovery Direction/Epic, Todo, Ordinary project work` | `DV:Use-Case Disposition/UC-02, UC-03, UC-06, UC-07` | UC-02, UC-03, UC-06, UC-07; OQ-01, OQ-03, OQ-04 | HCS-BND-104, HCS-BND-105 |
| HCS-CRE-008 | Confirmed | Creation SHALL NOT create a bead when any explicitly selected context is unregistered. | Given registered A, explicit A can be accepted. | Given unknown C, the request is rejected without creating a bead. | `DV:User-Selected Discovery Direction/Creation targeting` | `D:Policy Granularity Observations/Project targeting` | UC-01; OQ-06 | HCS-BND-102, HCS-BND-401 |
| HCS-CRE-009 | Confirmed | A non-contextless creation request that omits explicit targeting SHALL NOT create a bead when no registered current context is available. | Given registered current A, default creation can proceed. | Given no current context and no explicit target, creation fails without creating a bead. | `DV:User-Selected Discovery Direction/Creation targeting`; explicit requirements review | `D:Current Behavior/Context identity` | UC-01, UC-07; OQ-06 | HCS-BND-103, HCS-BND-304, HCS-BND-401 |

## Lifecycle Requirements

| ID | Status | Normative statement | Positive example | Negative or boundary example | Direction | Evidence | Coverage | Boundaries |
|---|---|---|---|---|---|---|---|---|
| HCS-LIF-001 | Confirmed | A todo SHALL retain its durable identity when it results in project-work beads. | Given todo T, when W is created from it, then T remains observable as T. | W cannot replace T's identity. | `DV:User-Selected Discovery Direction/Todo follow-on work` | Direction-only; no current follow-on workflow | UC-02, UC-11; OQ-01, OQ-12 | HCS-BND-201, HCS-BND-202 |
| HCS-LIF-002 | Confirmed | Each project-work bead resulting from a todo SHALL expose continuity with that todo. | Given T produces W, when W is inspected, then continuity with T is observable. | Similar text without durable continuity is insufficient. | `DV:User-Selected Discovery Direction/Todo follow-on work` | Direction-only; no current follow-on workflow | UC-04, UC-11; OQ-12 | HCS-BND-201 |
| HCS-LIF-003 | Confirmed | Correction of mistaken context placement SHALL create a distinct correctly scoped replacement bead. | Given misplaced O, when corrected, then distinct R represents the work with correct membership. | O cannot be changed in place. | `DV:User-Selected Discovery Direction/Correction` | `D:Current Behavior/Context mutation` | UC-05; OQ-05, OQ-07 | HCS-BND-203 |
| HCS-LIF-004 | Confirmed | The original bead SHALL remain addressable by its original identity after replacement. | Given O is replaced by R, when O is requested later, then O remains identifiable. | R cannot assume O's identity. | `DV:User-Selected Discovery Direction/Correction` | Direction-only; no current replacement workflow | UC-05; OQ-07 | HCS-BND-203 |
| HCS-LIF-005 | Confirmed | The original bead's recorded lifecycle history SHALL remain attributed to the original after replacement. | Given O has prior events, when R is created, then those events remain history of O. | Prior events cannot be rewritten as events of R. | `DV:User-Selected Discovery Direction/Correction` | Direction-only; no current replacement workflow | UC-05; OQ-07 | HCS-BND-203, HCS-BND-402 |
| HCS-LIF-006 | Confirmed | A corrected original bead SHALL expose an auditable closure or supersession outcome. | Given O was replaced, when O is inspected, then its corrected outcome is observable. | O cannot remain indistinguishable from active unreplaced work. | `DV:User-Selected Discovery Direction/Correction` | Direction-only; no current replacement workflow | UC-05; OQ-07 | HCS-BND-203, HCS-BND-204 |
| HCS-LIF-007 | Confirmed | A correction SHALL expose continuity between the original and replacement identities. | Given O and R, when the correction is reviewed, then both identities are associated with it. | An unexplained closure plus unrelated R is insufficient. | `DV:User-Selected Discovery Direction/Correction` | Direction-only; no current replacement workflow | UC-05; OQ-07 | HCS-BND-201, HCS-BND-203, HCS-BND-204 |
| HCS-LIF-008 | Confirmed | An epic SHALL expose its coordinating role for project work across its selected contexts. | Given E in A and B coordinates W1 and W2, when E is inspected, then that coordinating role is observable. | Shared context membership alone cannot imply that unrelated work is coordinated by E. | `DV:User-Selected Discovery Direction/Epic` | `D:UC-06`; `DV:Upstream Beads Capabilities/parent-child constraints` | UC-06, UC-07; OQ-03 | HCS-BND-205 |

## Scope Requirements

| ID | Status | Normative statement | Positive example | Negative or boundary example | Direction | Evidence | Coverage | Boundaries |
|---|---|---|---|---|---|---|---|---|
| HCS-SCP-001 | Confirmed | A Hub read initiated with a registered current context SHALL default to that context's scope. | Given current A and no explicit scope, then the read uses A. | It cannot silently default to unrelated B. | `DV:User-Selected Discovery Direction/Read scope` | `D:Current Behavior/Scoped listing`; `DV:Current Viewer Presentation/default scope` | UC-10; OQ-10 | HCS-BND-301, HCS-BND-304 |
| HCS-SCP-002 | Confirmed | A Hub read SHALL allow explicit selection of a registered context instead of the current-context default. | Given current A and registered B, when B is selected, then the read projects B. | Current A cannot override explicit B. | `DV:User-Selected Discovery Direction/Read scope` | `DV:Viewer Representation/catalog` | UC-10; OQ-06 | HCS-BND-301 |
| HCS-SCP-003 | Confirmed | A Hub read SHALL allow contextless items to be selected as a scope. | Given contextless todo T, when contextless scope is selected, then T is included. | T cannot require assignment to a repository merely to become selectable. | `DV:User-Selected Discovery Direction/Read scope` | `DV:Current Viewer Presentation/zero recognized contexts` | UC-02, UC-03, UC-10; OQ-02 | HCS-BND-301, HCS-BND-303 |
| HCS-SCP-004 | Confirmed | For context-scope matching, a read selecting registered contexts SHALL treat an item as matching when its membership intersects the selected contexts. | Given epic E in A and B, when A is selected, then E matches the context scope. | Matching cannot require every membership to be selected. | `DV:User-Selected Discovery Direction/Read scope` | `DV:Current Viewer Presentation/exact-union filter` | UC-06, UC-10; OQ-03 | HCS-BND-302, HCS-BND-303 |
| HCS-SCP-005 | Confirmed | A scoped read SHALL include each matching item at most once. | Given todo T in A and B, when both are selected, then T appears once. | Separate copies for A and B are not allowed. | `DV:User-Selected Discovery Direction/Read scope` | `DV:Current Viewer Presentation/exact-union filter` | UC-06, UC-10; OQ-03 | HCS-BND-302, HCS-BND-303 |

HCS-INV-007 and HCS-INV-008 govern dependency truth for every scoped read and
are not duplicated as scope requirements.

## Correlation Requirements

| ID | Status | Normative statement | Positive example | Negative or boundary example | Direction | Evidence | Coverage | Boundaries |
|---|---|---|---|---|---|---|---|---|
| HCS-COR-001 | Confirmed | A directly correlatable bead MAY be explicitly correlated with multiple immutable commits in an eligible repository. | Given project work B eligible in A, a caller can associate B with immutable commits X and Y in A. | Eligibility does not create a correlation automatically or limit the bead to one commit. | `DV:User-Selected Discovery Direction/Correlation` | `D:Working Vocabulary/External Hub correlation`; `DV:Correlation Lifecycle/persisted identity` | UC-08; OQ-08 | HCS-BND-402 |
| HCS-COR-002 | Confirmed | A directly correlatable bead's eligible repository set for explicit correlation SHALL equal its context membership set. | Given memberships A and B, then multiple commits in A and B are eligible. | Caller location or related work in C cannot add C. | `DV:User-Selected Discovery Direction/Correlation` | `D:Current Behavior/Commit correlation`; `DV:Correlation Lifecycle/membership validation` | UC-06, UC-07, UC-08; OQ-03, OQ-04, OQ-08 | HCS-BND-403 |
| HCS-COR-003 | Confirmed | An ineligible explicit-correlation attempt SHALL be rejected. | Given work scoped to A, when correlation is attempted in B, then the attempt is rejected. | Caller location in B cannot override membership. | `DV:User-Selected Discovery Direction/Correlation` | `D:Known Evidence/correlation-write membership`; `DV:Correlation Lifecycle/write validation` | UC-03, UC-06, UC-07, UC-08; OQ-08 | HCS-BND-401 |
| HCS-COR-004 | Confirmed | A rejected explicit-correlation attempt SHALL NOT create a correlation. | Given an ineligible attempt, when rejection completes, then no new association exists. | A rejected outcome with a persisted association is invalid. | `DV:User-Selected Discovery Direction/Correlation` | `DV:Validated Current State/Correlation Lifecycle/write validation` | UC-03, UC-08; OQ-08 | HCS-BND-401, HCS-BND-403 |
| HCS-COR-005 | Confirmed | A bead with zero explicit correlations SHALL remain valid. | Given eligible work with no correlations, when inspected, then it remains valid. | Absence of source history cannot force a correlation. | `DV:Use-Case Disposition/UC-08` | `D:UC-08`; `D:Candidate Lifecycle Examples/Source history` | UC-03, UC-08; OQ-08 | HCS-BND-402 |
| HCS-COR-006 | Confirmed | Correlation history SHALL continue to attribute an original bead's correlations to that original after a replacement is created. | Given O correlated with X, when R replaces O, then X remains attributed to O. | X cannot be reassigned to R merely because R is the replacement. | `DV:User-Selected Discovery Direction/Correction, Correlation` | Direction-only; no current replacement workflow | UC-05; OQ-07 | HCS-BND-402, HCS-BND-403 |
| HCS-COR-007 | Confirmed | A todo SHALL NOT own explicit commit correlations. | Given todo T results in task W, commit X is correlated with W while T retains only the `results-in` continuity. | Context membership on T cannot make T directly correlatable. | `MVP:Todo correlation` | Direction-only; current correlation code does not distinguish issue kinds | UC-02, UC-03, UC-08, UC-11; OQ-01, OQ-02, OQ-08 | HCS-BND-401, HCS-BND-402 |

## Explicit Non-Goals

These records are non-normative dispositions of superseded workflows. They do
not prohibit a future requirements revision from reopening a workflow with new
user direction.

| ID | Superseded workflow | Requirements-phase disposition | Source |
|---|---|---|---|
| HCS-NG-001 | Add or replace context membership after creation | Not part of the selected direction; membership is immutable | `DV:Use-Case Disposition/UC-04, UC-09` |
| HCS-NG-002 | Transfer an existing bead from one context to another | Replaced by new-bead correction and auditable closure or supersession | `DV:Use-Case Disposition/UC-05` |
| HCS-NG-003 | Promote a Viewer-owned intake record into a Bead | Reframed as a durable Beads-native todo linked to resulting project work | `DV:Use-Case Disposition/UC-11` |
| HCS-NG-004 | Distinguish initially unscoped from intentionally neutral using separate states | Both use the zero-context todo direction | `DV:Baseline Question Disposition/OQ-02` |
| HCS-NG-005 | Add a separate per-bead never-correlate policy | Kind and context determine eligibility, while actual correlation remains optional; todos are excluded by lifecycle role | `DV:Baseline Question Disposition/OQ-08` |

## Non-Normative Boundary Ledger

Boundary records identify decisions that cannot be smuggled into requirements.
They do not select a solution.

| ID | Category | Deferred question | Why deferred | Affected requirements | Later phase |
|---|---|---|---|---|---|
| HCS-BND-101 | Interface contract | How is effective default or explicit membership communicated after creation? | Command, UI, and machine-output contracts are outside this phase. | HCS-CRE-001, HCS-CRE-002 | Interface contract work |
| HCS-BND-102 | Target identity | What identity resolves a registered context, and what constitutes ambiguous selection? | Target grammar, syntax, and ambiguity handling remain unselected. | HCS-INV-001, HCS-CRE-002, HCS-CRE-008 | Requirements detail and model work |
| HCS-BND-103 | Contextless intent | How is intentional zero-context todo creation distinguished from omitted targeting? | Invocation representation remains unselected. | HCS-CRE-003, HCS-CRE-004, HCS-CRE-009 | Interface contract work |
| HCS-BND-104 | Context-set semantics | How are repeated, duplicate, or differently ordered selections interpreted? | Persisted representation and collection semantics remain unselected. | HCS-INV-002, HCS-INV-005, HCS-CRE-002, HCS-CRE-005, HCS-CRE-006, HCS-CRE-007 | Model work |
| HCS-BND-105 | Kind classification | What context cardinalities apply to `decision`, `docs`, `question`, and other custom non-epic/non-todo types? | Requirements review classified only `task`, `bug`, `feature`, and `chore` as ordinary implementation work. | Future kind-specific requirements | Requirements and model work |
| HCS-BND-201 | Relationship semantics | What mechanism represents todo-to-work and original-to-replacement continuity? | Relationship type and representation remain unselected. | HCS-LIF-001, HCS-LIF-002, HCS-LIF-007 | Model and contract work |
| HCS-BND-202 | Todo lifecycle | What completion outcome accompanies a todo after resulting project work exists? | Completion semantics were not selected during discovery. | HCS-LIF-001 | Lifecycle requirements work |
| HCS-BND-203 | Correction lifecycle | Which correction outcomes use closure, supersession, or both, and what audit details remain visible? | Detailed lifecycle and audit contracts remain unselected. | HCS-INV-003, HCS-LIF-003 through HCS-LIF-007 | Lifecycle requirements work |
| HCS-BND-204 | Supersession model | What formal association, if any, represents supersession? | Relationship type remains unselected. | HCS-LIF-006, HCS-LIF-007 | Model work |
| HCS-BND-205 | Epic coordination | What child semantics and context-consistency rules express epic coordination? | Relationship and child model remain unselected. | HCS-INV-004, HCS-LIF-008 | Requirements and model work |
| HCS-BND-301 | Scope representation | How are current, other registered, and contextless scopes exposed through interactive and machine interfaces? | UI and robot/API representation remain unselected. | HCS-SCP-001, HCS-SCP-002, HCS-SCP-003 | Interface contract work |
| HCS-BND-302 | Aggregate scope | What do empty selection, all registered contexts, and an all-items projection mean? | Current behavior and selected direction do not define a final aggregate contract. | HCS-SCP-004, HCS-SCP-005 | Scope requirements work |
| HCS-BND-303 | Mixed scope | Can contextless items be selected together with registered contexts, and what combined projection results? | Contextless selection is selected; mixed composition is not. | HCS-SCP-003, HCS-SCP-004, HCS-SCP-005 | Scope requirements work |
| HCS-BND-304 | Missing current context | What initial behavior applies when no registered current context can be derived? | Default, fallback, and interface policy remain unselected. | HCS-CRE-001, HCS-CRE-009, HCS-SCP-001 | Requirements and interface work |
| HCS-BND-305 | Hidden relationships | How are hidden blockers, parents, and dependency edges communicated in scoped outputs? | Global truth is selected; presentation remains unselected. | HCS-INV-007, HCS-INV-008 | UI and robot contract work |
| HCS-BND-401 | Rejection contract | What exact error and machine-readable outcome represents a rejected creation or correlation attempt? | Error shape and command/API representation remain unselected. | HCS-CRE-008, HCS-CRE-009, HCS-COR-003, HCS-COR-004, HCS-COR-007 | Interface contract work |
| HCS-BND-402 | History presentation | How do history surfaces expose optional absence and original/replacement attribution? | UI and robot representation remain unselected. | HCS-LIF-005, HCS-COR-001, HCS-COR-005 through HCS-COR-007 | History interface work |
| HCS-BND-403 | Existing correlations | How are existing records interpreted if they conflict with the selected immutable-membership requirements? | Existing-data compatibility, repair, and migration remain outside this phase. | HCS-COR-002, HCS-COR-004, HCS-COR-006 | Compatibility and migration analysis |
| HCS-BND-501 | Upstream compatibility | Which installed and supported Beads clients accept and preserve the selected todo behavior? | Released upstream evidence does not establish the supported client contract. | HCS-INV-005, HCS-CRE-004, HCS-CRE-005 | Compatibility analysis |
| HCS-BND-502 | Local mode | Which requirements, if any, apply outside Hub mode? | Local-mode policy remains deliberately unselected. | Entire requirement set | Later product requirements |
| HCS-BND-503 | Storage | What representation enforces kind-specific immutable context cardinality? | Storage and schema selection remain outside requirements synthesis. | HCS-INV-001 through HCS-INV-006 | Model and storage work |

## Reconciliation Notes

| Tension | Requirements interpretation |
|---|---|
| Baseline classification and transfer versus immutable contexts | Validation direction supersedes mutation with durable todo linkage or replacement correction. |
| Singular explicit target wording versus multi-context todo and epic | An explicit selection can supply the complete kind-valid context membership; its invocation and collection representation remain boundaries. |
| Omitted selection versus intentional contextless todo | They are distinct observable intents; their invocation representation remains HCS-BND-103. |
| Current all-scope visibility versus selectable contextless scope | Selectable contextless scope is confirmed behavior; aggregate and mixed selection semantics remain deferred. |
| Scoped candidate projection versus hidden dependency truth | Candidate visibility follows scope requirements; readiness and graph truth follow HCS-INV-007 and HCS-INV-008. |
| Correlation writer versus whole-ledger validation | New eligibility requirements are explicit; existing-record interpretation remains HCS-BND-403. |
| Todo context membership versus source ownership | Todo contexts control routing and scope, but resulting project work owns source correlations; no todo context makes it directly correlatable. |
| Upstream custom type support versus client portability | Beads-native todo is confirmed behavior; supported-client feasibility remains HCS-BND-501. |

## Direction Coverage

| Selected direction facet | Requirement coverage |
|---|---|
| Immutable context lifecycle | HCS-INV-002, HCS-INV-003 |
| Epic one-or-more contexts | HCS-INV-004, HCS-CRE-006, HCS-CRE-007 |
| Todo zero-or-more contexts | HCS-INV-005, HCS-CRE-003 through HCS-CRE-005, HCS-CRE-007 |
| Task, bug, feature, and chore exactly one context | HCS-INV-006, HCS-CRE-007 |
| Current or explicit creation targeting | HCS-CRE-001, HCS-CRE-002, HCS-CRE-008, HCS-CRE-009 |
| Replacement-based correction | HCS-LIF-003 through HCS-LIF-007 |
| Durable todo linked to project work | HCS-LIF-001, HCS-LIF-002 |
| Context-limited optional project-work correlation | HCS-COR-001 through HCS-COR-005, HCS-COR-007 |
| Current, other, and contextless read scopes | HCS-SCP-001 through HCS-SCP-005 |
| Global dependency truth | HCS-INV-007, HCS-INV-008 |

## Use-Case Coverage

| Use case | Coverage | Disposition |
|---|---|---|
| UC-01 | HCS-INV-001, HCS-INV-006, HCS-CRE-001, HCS-CRE-002, HCS-CRE-008, HCS-CRE-009 | Retained |
| UC-02 | HCS-INV-005, HCS-CRE-003 through HCS-CRE-005, HCS-LIF-001, HCS-COR-007 | Retained as durable todo capture |
| UC-03 | HCS-INV-005, HCS-CRE-003, HCS-CRE-004, HCS-SCP-003, HCS-COR-005, HCS-COR-007 | Retained as zero-context todo |
| UC-04 | HCS-LIF-002; HCS-NG-001 | Reframed as linked project work rather than context mutation |
| UC-05 | HCS-INV-003, HCS-LIF-003 through HCS-LIF-007, HCS-COR-006; HCS-NG-002 | Reframed as replacement correction |
| UC-06 | HCS-INV-004, HCS-CRE-006, HCS-LIF-008, HCS-SCP-004 | Retained as multi-context epic coordination |
| UC-07 | HCS-INV-006, HCS-CRE-001, HCS-CRE-002, HCS-CRE-007 | Retained as single-context ordinary work |
| UC-08 | HCS-COR-001 through HCS-COR-005, HCS-COR-007; HCS-NG-005 | Retained as optional explicit correlation on resulting project work |
| UC-09 | HCS-INV-003; HCS-NG-001 | Superseded as membership mutation |
| UC-10 | HCS-INV-007, HCS-INV-008, HCS-SCP-001 through HCS-SCP-005 | Retained |
| UC-11 | HCS-LIF-001, HCS-LIF-002, HCS-COR-007; HCS-NG-003 | Reframed as durable todo continuity without direct source ownership |

## Baseline-Question Coverage

| Question | Requirements or disposition | Remaining boundary |
|---|---|---|
| OQ-01 | HCS-INV-005, HCS-CRE-004, HCS-CRE-005 | HCS-BND-501 |
| OQ-02 | HCS-INV-005, HCS-SCP-003; HCS-NG-004 | HCS-BND-301, HCS-BND-303 |
| OQ-03 | HCS-INV-004, HCS-CRE-006, HCS-LIF-008 | HCS-BND-205 |
| OQ-04 | HCS-INV-006, HCS-CRE-007 | HCS-BND-105 |
| OQ-05 | HCS-INV-003; HCS-NG-001, HCS-NG-002 | HCS-BND-203 |
| OQ-06 | HCS-INV-001, HCS-CRE-001, HCS-CRE-002, HCS-CRE-008, HCS-CRE-009 | HCS-BND-102, HCS-BND-304 |
| OQ-07 | HCS-LIF-003 through HCS-LIF-007, HCS-COR-006; HCS-NG-002 | HCS-BND-203, HCS-BND-403 |
| OQ-08 | HCS-COR-001 through HCS-COR-005, HCS-COR-007; HCS-NG-005 | HCS-BND-401, HCS-BND-402 |
| OQ-09 | HCS-INV-007, HCS-INV-008 | HCS-BND-305 |
| OQ-10 | Hub requirements only | HCS-BND-502 |
| OQ-11 | HCS-INV-005, HCS-LIF-001; HCS-NG-003 | HCS-BND-501 |
| OQ-12 | HCS-LIF-001, HCS-LIF-002; HCS-NG-003 | HCS-BND-201, HCS-BND-202 |
| OQ-13 | HCS-NG-003 | HCS-BND-501 |

## Requirements-Phase Exit Criteria

| Criterion | Current result |
|---|---|
| Every selected-direction facet maps to confirmed requirements. | Pass |
| Every retained or reframed use case maps to confirmed requirements. | Pass |
| Superseded workflows map to explicit non-goals. | Pass |
| OQ-01 through OQ-13 map to requirements, a superseded disposition, or boundaries. | Pass |
| Every requirement is atomic, observable, status-tagged, and source-traceable. | Pass |
| Positive and negative examples exist for every requirement. | Pass |
| Duplicate and conflicting obligations have been reconciled. | Pass |
| Deferred mechanisms appear only in the boundary ledger. | Pass |
| No prohibited design or implementation decision is selected. | Pass |
| Review disposition is recorded for every normative requirement. | Pass; all 37 normative requirements are Confirmed |

Requirements synthesis is complete and accepted as the requirements baseline.
Deferred boundaries remain assigned to later phases and do not reduce the
Confirmed status of the normative requirements.
