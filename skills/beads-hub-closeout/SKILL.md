---
name: beads-hub-closeout
description: Safely correlate and close concrete private Hub work after a pull request is merged, without exposing private identities in Git metadata or disturbing worktrees.
---

# Private Beads Hub Closeout

Load `beads-hub` first. This skill is only the small post-merge closeout command;
it does not claim, review, merge, push, or manage worktrees.

## Interface and privacy

Run the bundled script with exactly two positional inputs:

```sh
skills/beads-hub-closeout/closeout.sh <private-work-item-id> <pr-selector>
```

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

1. Read the PR with `gh pr view --json state,mergedAt,mergeCommit,baseRefName`.
   Require a merged state, non-empty merge time, a valid 40-character
   merge-result SHA, and a valid base branch. Normalize the SHA to lowercase.
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
