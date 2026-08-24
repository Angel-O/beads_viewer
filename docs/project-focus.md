# Project Focus

The current integration and reference branch is:

```text
feature/repository-aware-correlations
```

Use this branch, rather than `main`, as the base and PR target for current work.

## Git Workflow

- Fetch and update `feature/repository-aware-correlations` before creating a worktree.
- Create task branches and worktrees from its latest tip.
- Target pull requests at `feature/repository-aware-correlations`.
- After a pull request merges, update the reference-branch checkout before starting dependent work.
- Do not merge or retarget this work to `main` unless the user explicitly ends the feature-integration phase.

## Synchronization Baseline

The last upstream commit integrated into the reference branch is:

```text
975b9e38dc9e82fb73b775e73a0d57f1b161092a
```

This upstream cutoff is distinct from the newer reference-branch tip used to
base task branches. Update it whenever another upstream synchronization lands.

## Current Architecture Focus

The active feature area is repository-aware Beads Hub integration:

- Hub repository registration and stable `ctx:` identities.
- Bead-to-source commit correlations across repositories.
- `wbd` and `wbv` routing, live refresh, and private Hub configuration.
- Repository-aware TUI scope and presentation.
- Deterministic robot output that preserves existing contracts.

Fork-specific behavior should be implemented as additive modules connected by
thin integration seams. See `docs/fork-maintenance.md` for the design,
verification, and upstream-synchronization policy.

Useful starting points:

- `docs/external-history.md`
- `docs/fork-maintenance.md`
- `cmd/wbd`
- `cmd/wbv`
- `pkg/hub`
- `pkg/correlation`
- `pkg/ui`
