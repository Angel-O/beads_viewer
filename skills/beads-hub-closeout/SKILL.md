---
name: beads-hub-closeout
description: Safely correlate and close concrete private Hub work after a pull request is merged, without exposing private identities in Git metadata or disturbing worktrees.
---

# Private Beads Hub Closeout

Load `beads-hub` first. This skill is only the small post-merge closeout command;
do not claim, review, merge, or push. After successful merged closeout,
you must ask the disposal questionnaire when an eligible recorded dedicated
implementation worktree exists, as described below.

## Interface and privacy

Run the bundled script with exactly two positional inputs:

```sh
skills/beads-hub-closeout/closeout.sh <private-work-item-id> <github-pull-request-url>
```

`<github-pull-request-url>` must be a full URL in the form
`https://github.com/<owner>/<repository>/pull/<number>`. Bare numbers and other
unqualified selectors are rejected before any GitHub, Git, or Hub command runs;
the accepted URL is passed unchanged to `gh pr view`.

The work-item ID is runtime-private. It and any `ctx:` identities must not
appear in Git metadata, fixtures, documentation, diagnostics, or the final
report. The script suppresses Hub command output and reports only the full
merge SHA, public PR base branch, and non-sensitive state.

`validate.sh` reads the persisted Hub issue prefix at runtime. Its range mode
checks the active branch, commits in `HEAD..FETCH_HEAD`, and tags on those
commits without printing a match.

## Epic dispatch

An epic is not a closeout target: do not run `closeout.sh`, or perform PR, Git,
fetch, or privacy-validation steps. Instead, for each explicit, already-known
relevant concrete-child full merge SHA, run this direct link from that child
repository's registered context:

```sh
wbd link <epic-id> <full-merge-sha>
```

Do not discover descendants or correlations, infer commit SHAs, add an
aggregation script, or add a new `wbd` feature. Close the epic only after its
relevant concrete children are closed and their merge commits are linked to it.
Concrete-item closeout behavior below is unchanged.

## Required flow

The script derives the base branch only from the merged PR response and accepts
no branch or remote arguments.

1. Read the full PR URL with `gh pr view --json
   state,mergedAt,mergeCommit,baseRefName`. Require a merged state, non-empty
   merge time, a valid 40-character merge-result SHA, and a valid base branch.
   Normalize the SHA to lowercase.
2. Require the current checkout to be clean and already on that PR base branch.
   Never find another worktree, switch branches, or update `HEAD`.
3. Fetch only `refs/heads/<base>` from `origin` with `--no-tags`.
4. Prove both `HEAD` and the merge-result SHA are ancestors of `FETCH_HEAD`.
5. Run `validate.sh --metadata-range <repository> HEAD FETCH_HEAD`.
6. Run exactly one parsed `wbd show <work-item-id> --json`. Require one exact
   concrete record (`task`, `bug`, `feature`, or `chore`) with one `ctx:` label.
   A closed record is allowed for an idempotent rerun.
7. Run `wbd link <work-item-id> <full-merge-sha>`. Only after link succeeds,
   run `wbd close ... --reason "Merged and correlated" --json` when the parsed
   record was not already closed.
8. For a concrete item only, require the checkout to still be clean immediately
   before exactly one `git pull --ff-only origin <base-branch>`. Report normal
   closeout success only after that pull succeeds, including whether the item
   was closed or already closed. If the clean-state check or pull fails, report
   partial success because correlation/closure already succeeded; do not retry,
   undo, or alter Hub state.

The final pull is a local reference synchronization convenience, not a closure
precondition. Epic dispatch remains documentation-only and never runs this pull.

## Dedicated worktree disposal

The bundled script remains non-interactive and never removes a worktree. If
closeout fails or is incomplete, report that failure or partial success and do
not ask or invoke cleanup. Only after it reports a successful merged closeout
with `closeout complete`, you must ask this questionnaire for the recorded
dedicated implementation worktree associated with the just-closed concrete
item. Use only the branch and path recorded by runtime orchestration; do not
discover another target:

```text
Dispose of the recorded dedicated implementation worktree?
  Branch: <recorded-branch>
  Path:   <recorded-worktree-path>
  Choice: Yes (recommended/default) | No
```

This is a Yes/No questionnaire. Require an explicit, unambiguous `Yes`
selection; the displayed default must not turn an unanswered prompt into
consent. On `No`, no response, or an ambiguous response, do not invoke cleanup;
report that cleanup was skipped and that the successful closeout and worktree
are retained. If the recorded dedicated worktree is missing or otherwise
ineligible, do not offer another target or invoke cleanup; report that cleanup
was skipped because no eligible recorded target is available.

Only for an explicit `Yes`, invoke the bundled disposal helper with the
recorded branch name, never the filesystem path:

```sh
skills/beads-hub-closeout/dispose.sh <recorded-branch>
```

Do not offer or remove the reference checkout, an arbitrary worktree, or a
manually selected path. The helper runs `git wtrm <recorded-branch>` first and
then `git branch -d <recorded-branch>`; it never deletes a remote branch. If
worktree removal fails, report that the worktree and local branch were retained.
If local branch deletion fails after worktree removal, report that
the worktree was removed and the local branch was retained. In either case,
report the cleanup failure; the completed closeout remains completed and must
not be undone.
