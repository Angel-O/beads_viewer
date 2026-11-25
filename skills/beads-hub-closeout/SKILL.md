---
name: beads-hub-closeout
description: Safely correlate and close concrete private Hub work after a merged PR or accepted integration-branch commit, without exposing private identities in Git metadata or disturbing worktrees.
---

# Private Beads Hub Closeout

Load `beads-hub` first. This skill closes concrete work only; do not claim,
review, merge, push, or dispose of worker environments.

## Interface and privacy

Use either the existing merged-PR form:

```sh
skills/beads-hub-closeout/closeout.sh <private-item-id> <github-pull-request-url>
```

or the accepted integration-commit form:

```sh
skills/beads-hub-closeout/closeout.sh <private-item-id> --commit <full-integration-sha> <recorded-reference-branch>
```

For the PR form, the full GitHub URL is passed unchanged to `gh pr view`; the
script fetches and verifies the merged result on the PR base as before. For the
commit form, run from a clean checkout already on the recorded local reference
branch. The script verifies the exact full commit is reachable from that
branch; it does not fetch or require a push, and it uses the supplied commit
rather than the branch tip.

The work-item ID is runtime-private. It and any `ctx:` identities must not
appear in Git metadata, fixtures, documentation, diagnostics, or the final
report. Hub command output is suppressed; successful output reports only the
full selected commit SHA, public reference branch, and non-sensitive state.

`validate.sh` reads the persisted Hub issue prefix at runtime. Metadata
validation checks the active branch and the selected commit's message and tags
without printing a match.

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

Both variants require a clean checkout on the designated reference branch, run
Git metadata privacy validation, and perform exactly one parsed
`wbd show <private-item-id> --json`. Require one exact concrete record (`task`,
`bug`, `feature`, or `chore`) with one `ctx:` label. A closed record is allowed
for an idempotent rerun.

The PR form retains its existing merged-PR verification: fetch only the PR base
from `origin`, prove `HEAD` and the merge-result SHA are ancestors of
`FETCH_HEAD`, then correlate the merge-result SHA.

The commit form requires the supplied full SHA to name an available non-root
commit in the recorded local reference branch's history. The checked-out branch
must match, but may be ahead of the selected commit. It does not fetch or change
refs, and validates metadata on the selected commit itself.

After delivery and privacy checks, run `wbd link` for the selected SHA. Only
when linking succeeds, close an item not already closed. Failures remain
fail-closed: do not retry, undo, or alter Hub state.
