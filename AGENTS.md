# Working Rules

## Precedence and safety

- Direct user instructions take precedence over these defaults.
- Never delete, reset, clean, overwrite, stash, commit, push, or rewrite
  history unless explicitly requested. Prefer read-only inspection.
- Treat concurrent worktree changes as owned work: do not revert, hide, or
  overwrite them.

## Project guidance

- Follow [`docs/project-focus.md`](docs/project-focus.md) for the reference branch and workflow.
- Before upstream sync, read [`docs/fork-maintenance.md`](docs/fork-maintenance.md) and search for `UPSTREAM INTEGRATION BOUNDARY`.
- Use Go Modules only. Keep dependency changes in `go.mod`/`go.sum`; never
  edit `go.sum` manually.
- Make the smallest focused edit in the existing file. Avoid new abstractions,
  duplicate implementations, broad cleanup, and unrelated formatting.
- Do not change behavior outside the supplied scope.

## Project structure

- `cmd/bv` contains the main Viewer CLI and composition boundaries.
- `cmd/wbd` and `cmd/wbv` contain Hub writer and wrapper CLIs.
- `pkg/analysis` contains graph analysis and triage; `pkg/loader` and
  `pkg/model` contain data loading and core data types.
- `pkg/correlation` and `pkg/hub` contain history correlation and Hub seams.
- `pkg/ui` contains TUI models and runtime services.
- `pkg/search`, `pkg/export`, and `pkg/watcher` contain search, export, and
  filesystem-watching features.
- `tests/e2e` contains end-to-end coverage; `docs` contains project guidance.

## Validation

### Unit testing

- Run focused affected-package tests first; run broad tests when appropriate to
  the scope.

### Formatting and static checks

- Run `gofmt -l` on changed Go files, `go build ./...`, and `go vet ./...`.

### Integration testing

- Run exactly `go test ./tests/e2e` with a minimum five-minute timeout; the
  normal run takes about 129 seconds.
- Set `BV_NO_BROWSER` or `BV_TEST_MODE` for tests. Browser-opening tests must
  remain gated by those variables and must never open a browser.
- Report exact commands, exit statuses, skips, failures, and pre-existing
  blockers. Do not fix failures outside the requested scope.
