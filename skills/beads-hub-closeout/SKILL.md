---
name: beads-hub-closeout
description: Safely correlate and close concrete private Hub work after a pull request is merged, without exposing private identities in Git metadata or disturbing worktrees.
---

# Private Beads Hub Closeout

Load `beads-hub` first and follow its command boundary. This workflow is only
for post-merge closeout of a concrete private item. It is not a claim, review,
merge, push, or pull-request workflow.

## Privacy Boundary

- Treat the real bead ID and every `ctx:` identity as private runtime input.
  They may appear in direct `wbd` arguments, private command output, and private
  conversation only.
- Never copy a private identity into a branch or tag name, commit message,
  source, test, fixture, documentation, release note, pull-request title, body,
  comment, label, or other Git-visible metadata. Do not derive Git names from a
  private identity. Use an ID-free description such as `docs/hub-closeout` and
  an ID-free conventional commit subject.
- Do not print private values into validation logs or the final closeout report.
  Keep shell variables containing them local to the closeout process.
- Run `skills/beads-hub-closeout/validate.sh` from the intended checkout before
  closeout. It rejects private identity patterns in every local branch and tag
  name and in commit subjects and bodies reachable from those refs, without
  printing the matched value. Its tests use isolated synthetic repositories.
- Never modify repository `.beads`, hooks, ignores, exports, Hub configuration,
  ledgers, or agent routing files directly. Use only `wbd` for the two Hub
  mutations authorized below.

## Non-Destructive Rules

- Never stash, reset, clean, rewrite shared history, force-push, or push. Never
  switch branches or remove worktrees as part of closeout.
- Do not run reviewers, review lanes, peer review, or PR mutation commands.
- Treat every dirty worktree as owned work. Do not edit, stage, discard, move,
  or temporarily hide any of its changes.
- Use `gh`, `git`, and `wbd show` read-only until all preconditions pass. The
  only Hub mutations are `wbd link` followed by `wbd close`.

## Preconditions

Accept the bead ID, pull-request selector, and optional remote/reference branch
as private runtime inputs. Do not infer the bead ID from Git metadata.

1. Run `wbd show "$bead_id" --json`. Require exactly one matching record whose
   `issue_type` is `task`, `bug`, `feature`, or `chore`, with exactly one
   `ctx:` label. Reject todos, epics, decisions, missing records, malformed
   responses, and ineligible context cardinality. An already-closed record is
   allowed only as an idempotent rerun; otherwise require a state that `wbd
   close` can close. The later `wbd link` call remains authoritative for current
   repository registration and context eligibility.
2. Resolve the reference branch in this strict order, taking the first
   available value: an explicit user instruction; an explicit repository policy
   naming the reference/default branch; the merged PR's `baseRefName`; the
   selected remote's default branch. Query the remote default without changing
   it, using its existing symbolic `refs/remotes/<remote>/HEAD` or
   `git ls-remote --symref <remote> HEAD`. Never guess from the working branch,
   bead ID, or a hard-coded branch name.
3. Resolve the PR with a read-only `gh pr view` query requesting `state`,
   `mergedAt`, `mergeCommit`, `baseRefName`, and `url`. Require `state` to be
   `MERGED`, `mergedAt` to be non-null, and `mergeCommit.oid` to match exactly
   `^[0-9a-fA-F]{40}$`. Normalize it to lowercase. This full merge-result SHA,
   not a feature tip, abbreviated SHA, or local `HEAD`, is the correlation.
4. Do not replace a branch selected from a higher-precedence source merely
   because a lower-precedence source differs. Verify that the exact selected
   branch exists on the selected remote with
   `git ls-remote --exit-code --heads "$remote" "refs/heads/$reference_branch"`.
5. Locate an existing worktree already checked out on the reference branch by
   inspecting `git worktree list --porcelain`. Require that checkout to be clean
   according to `git status --porcelain=v1 --untracked-files=all`. If none is
   available or it is dirty, stop before `wbd link`; do not create, switch,
   clean, stash, or modify a worktree.

Keep the selected remote, reference branch, reference checkout, and merge SHA
fixed after preflight. If concurrent activity invalidates any of them, stop and
rerun preflight rather than substituting new values.

## Synchronize Reference

1. Fetch only the selected reference branch without tags:
   `git fetch --no-tags "$remote" "refs/heads/$reference_branch"`.
2. Resolve `FETCH_HEAD^{commit}` to a full SHA, verify the merge object with
   `git cat-file -e "$merge_sha^{commit}"`, then require
   `git merge-base --is-ancestor "$merge_sha" FETCH_HEAD`. A missing object or
   nonzero ancestry result stops closeout before any Hub mutation.
3. Recheck that the reference checkout is still on the resolved branch and
   clean. Stop without Hub mutation if its branch, path, or status changed.
4. Synchronize the clean reference checkout with
   `git -C "$reference_worktree" pull --ff-only "$remote" "$reference_branch"`.
   Any non-fast-forward result or pull error stops closeout before Hub mutation;
   never repair it with stash, reset, checkout, rebase, merge, force, or history
   rewriting.
5. After the pull, require the checkout to remain on the resolved branch and be
   clean. Resolve its `HEAD` to a full SHA and require
   `git -C "$reference_worktree" merge-base --is-ancestor "$merge_sha" HEAD`.
   This synchronized, verified checkout is the only permitted working directory
   for the Hub operations below.

## Revalidate And Close

1. Immediately before the first Hub mutation, revalidate that the reference
   worktree path is unchanged, is still on the resolved branch, is clean, and
   still contains the merge result. Stop if any check differs from the
   synchronized state.
2. From the resolved reference worktree, run
   `(cd -- "$reference_worktree" && wbd show "$bead_id" --json)` and repeat every
   eligibility check. Running from this checkout binds `wbd` repository-context
   discovery to the reference repository rather than the caller's directory.
3. Recheck the reference worktree's path, branch, cleanliness, and merge
   reachability once more. Then, from that same checkout, run
   `(cd -- "$reference_worktree" && wbd link "$bead_id" "$merge_sha")`. Check
   its exit status and JSON result. Do not close on any link error or ambiguous
   response. The exact duplicate correlation is a successful no-op and makes
   this step idempotent.
4. If the refreshed item is not closed, run
   `(cd -- "$reference_worktree" && wbd close "$bead_id" --reason "Merged and correlated" --json)`.
   Only do this after successful correlation. From the same checkout, re-run
   `wbd show "$bead_id" --json` and require `status` to be `closed`. If it was
   already closed, do not close it again.

## Idempotence And Partial Failures

- Any fetch, reachability, cleanliness, branch, or ff-only synchronization
  failure occurs before `wbd link` and leaves the Hub unchanged. A successful
  pull may advance the clean reference checkout before a later phase fails.
- `wbd link` deduplicates the same bead, context, and full commit. A rerun after
  synchronization can safely repeat the already-up-to-date ff-only pull and
  retry that exact link before deciding whether closure is still needed.
- If revalidation or linking fails after synchronization, report the reference
  checkout as synchronized and the Hub as unchanged. Never move Hub mutation
  ahead of synchronization to avoid this partial Git-only state.
- If linking succeeds and closure fails, preserve the correlation, report the
  item as still open, and rerun from preflight. The reference checkout is
  already synchronized; never close by another route.
- If final verification cannot prove the correlation, closed state, merge
  reachability, or synchronized clean checkout, report exactly which phase
  succeeded without exposing private identities. Never claim full closeout from
  a partial result.

## Safe Report

Report the full merge-result SHA, resolved reference branch, whether correlation
was new or already present, closure state, and the pre-mutation ff-only pull
result. Distinguish synchronized-but-Hub-unchanged and correlated-but-still-open
partial states. Omit the bead ID, contexts, private titles, and private
descriptions.
