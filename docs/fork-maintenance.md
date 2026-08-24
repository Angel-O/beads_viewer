# Fork Maintenance Strategy

This fork should remain easy to synchronize with upstream while supporting
repository-aware Beads Hub features. The default design is additive: put fork
behavior in dedicated modules and connect it to upstream-owned code through
the smallest practical integration seam.

## Design Rule

For each fork feature:

1. Put policy, state transformations, and feature-specific rendering in a
   dedicated module.
2. Keep existing core files responsible for orchestration only.
3. Integrate with a small function or method call at the boundary.
4. Keep existing public behavior unchanged unless the feature explicitly
   changes that contract.
5. Put feature contracts in dedicated test modules instead of continually
   extending upstream-owned test files.

In Go, methods for one type can live in multiple files in the same package.
Use that property to keep feature policy out of large core files without
introducing wrappers or changing callers.

## Conflict Budget

The **upstream cutoff** is the exact commit from the upstream branch that was
last integrated into the fork's reference branch. It is distinct from the
reference-branch tip used as the base for a task branch. Record the cutoff when
performing each upstream synchronization; do not substitute the newer task
base when measuring the fork's total divergence.

After fetching upstream, this command can verify the current common ancestor:

```bash
git merge-base HEAD upstream/main
```

Treat the recorded synchronization commit as authoritative when merges,
cherry-picks, or an advanced upstream remote make ancestry ambiguous.

Review every change against both the upstream cutoff and the task-branch base.
A well-isolated change normally has:

- New feature modules containing most production logic.
- New feature test modules containing most contract coverage.
- Few modified existing production files.
- Small, declarative changes at each existing-file boundary.
- No copied upstream functions, compatibility shims, or duplicate policy.
- No unrelated formatting, renaming, or cleanup in upstream-owned files.

The goal is not zero changed lines in existing files. A feature must still be
invoked somewhere. The goal is to make those lines obvious and mechanically
easy to reconcile during an upstream merge.

Use these commands when reviewing the conflict surface:

```bash
git diff --name-status <upstream-cutoff>..HEAD
git diff --numstat <upstream-cutoff>..HEAD
git diff --check <upstream-cutoff>..HEAD
```

Inspect every modified existing file and record why its boundary change is
unavoidable. New files should not hide duplicated or obsolete implementations.

## Boundary Shape

Prefer a boundary that supplies explicit inputs and receives a result:

```go
layout := repositoryLayoutFor(scope, catalog, issues, width)
delegate.SetRepositoryLayout(layout)
```

Avoid embedding feature policy inside a core update loop:

```go
// Avoid: repository discovery, filtering, sizing, and rendering policy
// implemented inline in a large core model method.
```

The boundary may use an unexported package-level function or a method defined
in the feature module. Do not add an interface solely to claim isolation when
there is only one implementation.

## Contract Tests

Add tests around the integration boundary, not just around internal helpers.
Tests should prove that the real caller still honors the feature contract.

For UI work, cover:

- Normal and constrained widths.
- Repeated state transitions that could change layout.
- Plain-text output before adding ANSI assertions.
- Fixed metadata that must survive truncation.
- Non-feature modes that must remain unchanged.

For robot output, cover filtering and caps in the order users observe them,
while ensuring scores still use the canonical full graph when required.

Keep these tests in dedicated feature test files when possible. Modify an
existing upstream test only when its old assertion directly contradicts the
new contract.

## Upstream Synchronization

Before starting work:

1. Record the upstream cutoff commit.
2. Update the repository's reference branch according to
   `docs/project-focus.md`.
3. Create the task branch from that exact reference tip.
4. Identify likely upstream-owned conflict hotspots before editing.

Before proposing integration:

1. Run focused contract tests repeatedly.
2. Run package tests, race checks where relevant, build, vet, and formatting.
3. Compare the final tree with the recorded upstream cutoff.
4. Confirm moved logic exists in one place only.
5. Review every remaining existing-file modification as an integration seam.

Preserve commit identity when external history or correlation data depends on
it. Do not rebase or rewrite such history merely to make the graph look clean.
A merge-based synchronization can have better operational affinity than a
smaller-looking rewritten history.

## Legitimate Exceptions

Direct changes to upstream-owned files are appropriate when:

- The upstream implementation itself contains the bug.
- A public contract or data model must change.
- The feature requires lifecycle wiring that cannot run from a separate file.
- Moving policy leaves obsolete code that must be removed.
- An existing test encodes behavior that the approved feature intentionally
  replaces.

Keep the exception narrow and explain it in the pull request. Do not retain
dead code, duplicate implementations, or compatibility wrappers to avoid a
small honest conflict.

## Current Example

The repository-aware UI follows this pattern:

- `pkg/ui/repository_list_layout.go` owns repository-column policy extracted
  from `pkg/ui/repository_scope.go`, while the existing delegate update in
  `pkg/ui/model.go` keeps its original method-call seam unchanged.
- `pkg/ui/repo_picker_row.go` owns fixed-field allocation, truncation, and
  marker rendering while `RepoPickerModel.View` only supplies row data.
- Dedicated test modules own width, toggle, ANSI, and Hub graph contracts.
- The older inline policy is removed rather than retained as a duplicate
  implementation.

This structure does not guarantee conflict-free merges, but it limits likely
conflicts to small, reviewable boundaries and keeps feature behavior additive.

## Review Checklist

- Is most new logic in dedicated feature modules?
- Are existing-file edits limited to invocation, lifecycle, or intentional
  contract changes?
- Is each policy implemented exactly once?
- Do integration tests exercise the real boundary?
- Are unrelated modes and outputs unchanged?
- Does the cutoff diff show any avoidable upstream-owned edits?
- Is history preservation compatible with correlation and audit requirements?
