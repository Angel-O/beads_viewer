# Manual QA: UI Discovery and Hub Workflow Follow-ups

Date prepared: 2026-08-22

Status: Awaiting manual QA

Target branch: `feature/repository-aware-correlations`

Merged changes:

- PR #32, UI discovery and History follow-ups, merge commit
  `806d184cf613b19664f27503276cee9f04ac1a5e`
- PR #33, Hub workflow follow-ups, merge commit
  `62d29f5c35522fdb1714409cb43fb872c9810213`

This checklist is the manual product-acceptance gate for the merged changes. Record
failures with the view or command, exact keys or arguments, terminal size, expected
result, and actual result.

## Build The Merged Command Chain

Test the merged source rather than installed binaries. From the repository checkout:

```bash
cd /Users/specs/workspace/source/beads_viewer

test_bin="$(mktemp -d "${TMPDIR:-/tmp}/bv-manual-qa.XXXXXX")"
go build -o "$test_bin/bv" ./cmd/bv
go build -o "$test_bin/wbd" ./cmd/wbd
go build -o "$test_bin/wbv" ./cmd/wbv

export PATH="$test_bin:$PATH"
wbv --hub
```

The `PATH` override is required because `wbv` delegates to `bv` and `wbd` by
command name.

## Core UI QA

### List Type Filtering

- [ ] From List, press uppercase `I` and verify the issue-type picker opens.
- [ ] Verify the picker fits within the terminal and displays its controls.
- [ ] Use `j`/`k`, select two types with `Space`, and press `Enter`.
- [ ] Verify only those exact issue types remain. A title containing a type name
      must not count as that issue type.
- [ ] Combine the type filter with `o` for open issues, `/` text search, and a
      repository selection.
- [ ] Verify those filters intersect instead of replacing one another.
- [ ] Reopen with `I` and verify the applied types remain checked.
- [ ] Press `n`, then `Enter`, and verify only the type filter resets.
- [ ] Reopen, change the draft selection, press `Esc`, and verify the previously
      applied filter remains unchanged.

### Graph Search

- [ ] Press `g` to enter Graph.
- [ ] Verify the footer advertises `/`, `n`, `N`, `Enter`, and `Esc`.
- [ ] Press `/` and search by exact bead ID.
- [ ] Search by part of a title.
- [ ] While search input is active, type shortcut characters such as `?`, `;`,
      and `E`; verify they do not open Help, switch views, or trigger other
      global shortcuts.
- [ ] Press `Enter`; verify the first match becomes selected and the accepted
      query remains visible.
- [ ] Press `n` and `N`; verify matches cycle forward and backward and wrap at
      the ends.
- [ ] Search for guaranteed nonsense and verify a visible `no matches` state
      without leaving Graph.
- [ ] Press `Esc` and verify the query clears while Graph remains open.
- [ ] Press `Esc` again and verify Graph exits.
- [ ] Repeat under a restricted repository scope and verify out-of-scope beads
      never match.

### Tree Search And Help

- [ ] From List, press uppercase `E` and verify Tree opens.
- [ ] Verify normal Tree hints expose search and exit controls.
- [ ] Press `?` and verify Help documents Tree entry, navigation, `/`, `n`, `N`,
      and `Esc`.
- [ ] Collapse a branch containing a known nested bead.
- [ ] Press `/` and search for that child.
- [ ] Verify Tree reveals the child and enough ancestors to preserve hierarchy.
- [ ] Verify the search header shows the query and match position.
- [ ] Use `n` and `N` to cycle matches.
- [ ] Press `Enter`; verify the query remains applied and normal Tree navigation
      resumes.
- [ ] Press `Esc`; verify the query clears and the pre-search expansion state
      returns.
- [ ] Press `Esc` again and verify Tree exits.
- [ ] Search for guaranteed nonsense and verify the bounded `No Tree matches`
      state.
- [ ] While search input is active, type `?`, backtick, `;`, and `E`; verify no
      global action fires.
- [ ] Search for a parent with descendants, press `Enter`, then press `v`; verify
      Tree toggles between minimal results and the matched bead's full subtree.
- [ ] In subtree mode, verify descendants appear once, unrelated sibling branches
      remain hidden, and `n`/`N` plus the match counter use direct matches only.
- [ ] Type `v` before submitting a search and verify it becomes query text; after
      submission, verify footer and search chrome expose the scope toggle clearly.
- [ ] Clear the search and verify the pre-search expansion state and repository
      projection return unchanged.
- [ ] With Tree already open, toggle `o`, `c`, and `r` on and off; verify rows,
      status badges, and footer update immediately for open/closed/ready filters.
- [ ] Combine Tree status toggles with repository, label, and issue-type filters;
      verify all filters intersect and omitted-parent hierarchy remains coherent.
- [ ] Submit a Tree search, select minimal or subtree scope with `v`, then change
      status; verify query, scope, valid selection, expansion, and scroll persist.
- [ ] While entering Tree search text, type `o`, `c`, and `r`; verify they become
      query text rather than changing status.
- [ ] Verify `+` expands all Tree nodes and `-` collapses all, with matching
      footer, Help, and shortcuts-sidebar guidance.

### Insights

- [ ] Enter Insights from unfiltered, closed-only, and ready-only List/Board
      states; verify Insights always defaults to broad active work and never
      displays closed or tombstoned issues.
- [ ] Inspect Bottlenecks, other metric panels, cycles, recommendations,
      relationships, heat-map cells, drill-down, and detail content; verify no
      closed, tombstoned, or repository-excluded issue is reachable.
- [ ] Press `o` twice and verify OPEN toggles back to ACTIVE; press `r` twice
      and verify READY toggles back to ACTIVE. Switch directly between OPEN and
      READY, verify `c` does not enable a closed view, and confirm Help and
      shortcut chrome advertise only `o/r`.
- [ ] With a closed List filter preserved, open an active Insights issue using
      Enter in narrow and split layouts, then return with Escape or interact
      with List via Tab, keys, row clicks, header clicks, and empty padding;
      verify no stale detail remains and List/Board filters are unchanged.
- [ ] Refresh while a directly opened Insights issue closes, leaves scope, or
      disappears; verify the view returns to Insights without stale detail.
- [ ] Open the heat-map legend and verify `some` is gold/yellow, `many` is
      orange, `hot` is bright pink, and `max` is dark red/burgundy, with clear
      adjacent contrast and readable text.

### History

- [ ] Press lowercase `h` to open History.
- [ ] Verify normal History hints visibly include lowercase `h` to close and `v`
      to switch bead/Git modes.
- [ ] Press `v` twice; verify both modes render correctly and the second press
      returns to the original mode.
- [ ] Press `/`, enter a query that returns zero results, and press `Enter`.
- [ ] Verify the query and search row remain visible in the zero-result state.
- [ ] Verify the zero-result message is clear.
- [ ] Verify the surrounding layout does not jump when search is entered or
      submitted.
- [ ] Press `Esc`; verify the submitted query clears while History stays open.
- [ ] Press lowercase `h`; verify History closes.
- [ ] Reopen History, start search, and type `h`; verify it becomes query text
      rather than closing History.
- [ ] While search is active, type `?`, backtick, `;`, and `E`; verify no global
      overlay or view switch occurs.
- [ ] Submit a valid search, select a bead and commit, change mode with `v`, and
      change pane focus with `Tab`.
- [ ] If available, change file-tree expansion or filtering state.
- [ ] Press `Ctrl+R` after submitting the search.
- [ ] After refresh, verify the submitted query, bead/Git mode, valid bead and
      commit selection, pane focus, scroll position, filters, and file-tree
      state remain intact.

### Shortcuts Sidebar Reflow

Use a normal wide terminal, approximately 180-200 columns.

- [ ] In List, Board, Insights, and History, press `;` and verify the shortcuts
      sidebar opens without overlapping or interleaving the active view.
- [ ] Press `;` again in each view and verify the sidebar closes and the active
      view returns to full width.
- [ ] Repeat the open/close checks with `F2`.
- [ ] Keep the sidebar open while moving from List to Board, Insights, and
      History; verify it remains coherent and each view preserves its selection,
      focus, filters, mode, and other active state.
- [ ] Start History search and type `;`; verify it enters the query instead of
      toggling the sidebar.
- [ ] Exit History search and press `;`; verify normal sidebar toggling resumes
      without changing History state.
- [ ] At approximately 120 columns, open Tree with `E`, toggle the sidebar with
      `;` and `F2`, and verify it stays flush right while Tree fills the remaining
      width; long rows must remain single-line and selection must stay visible.
- [ ] With the Tree sidebar open, press `Esc` until quit confirmation appears;
      verify the confirmation is centered on the full terminal without the
      sidebar displacing it, then cancel and verify Tree/sidebar state returns.
- [ ] With the Tree sidebar open, press `?`; verify Help fills the terminal
      without sidebar interleaving, then close with both `?` and `Esc` and verify
      Tree focus, sidebar visibility, and Tree state return unchanged.
- [ ] While Help is open, press `;` and `F2`; verify neither key emits the
      shortcuts-sidebar banner or changes the sidebar state restored on close.
- [ ] From Help, press `Space` to open Tutorial, then press `;` and `F2`; verify
      Tutorial remains full-screen without sidebar interleaving or banners and
      closes back to the prior Tree/sidebar state.

### `no-context` Presentation

Use an existing contextless todo if one is available.

- [ ] Locate it in List and open its details.
- [ ] Verify user-facing text says exactly `no-context`, not `Contextless`.
- [ ] Open the repository picker and verify its option and scope status say
      `no-context`.
- [ ] Apply a mixed repository plus no-context scope and verify both are
      displayed clearly.
- [ ] Search the repository picker for `no-context` and verify it matches.
- [ ] Verify the legacy search alias `contextless` may match, but rendered labels
      remain `no-context`.

### Terminal Sizing

Repeat Graph search, Tree search, History zero results, and the type picker at
approximately `80x24`, `100x30`, and one very narrow terminal size.

- [ ] No panic occurs.
- [ ] Borders remain structurally intact.
- [ ] Search and status chrome stay within the terminal.
- [ ] The status line does not displace or corrupt the main content.
- [ ] Controls remain usable or degrade to a bounded compact presentation.
- [ ] At approximately `120x40` and `180x50`, open a Tree with little content
      and verify the global status line remains anchored to the terminal bottom.
- [ ] At the same sizes, open Tutorial directly and through Help; verify its
      bordered container spans the available terminal width and its global
      status line remains anchored to the bottom when page content is short.

## CLI QA

Keep the temporary build directory at the front of `PATH` for these checks:

```bash
export PATH="$test_bin:$PATH"
```

### Help

Run:

```bash
wbd --help
wbd create --help
wbd update --help
wbd list --help
wbd dep --help
wbd unlink --help
```

- [ ] Every help command exits successfully.
- [ ] Help is printed to stdout.
- [ ] Help does not mutate or require opening the Hub store.
- [ ] Displayed options match accepted options.
- [ ] Help explains explicit assignee behavior, assignment clearing, status-only
      preservation, and exact full-SHA unlinking.

### Assignees

Use a disposable test record:

```bash
wbd create "Manual QA assignee" --type task --assignee "qa-user" --json
wbd show <returned-id> --json
```

- [ ] `assignee` is `qa-user`.
- [ ] `assignee`, `owner`, and `created_by` remain distinct fields.

Then run:

```bash
wbd update <returned-id> --status in_progress --json
wbd show <returned-id> --json
```

- [ ] The status-only update preserves `qa-user`.

Then run:

```bash
wbd update <returned-id> --assignee "qa-user-2" --json
wbd update <returned-id> --assignee "" --json
wbd show <returned-id> --json
```

- [ ] Explicit reassignment works.
- [ ] An empty explicit assignee clears the assignment.
- [ ] Viewer refreshes after each successful mutation.
- [ ] TUI details show the assigned identity or `Unassigned` correctly.

Close the disposable record afterward:

```bash
wbd close <returned-id> --reason "Manual QA completed" --json
```

### Exact Correlation Removal

Do not run this against a real correlation. Use a disposable record:

```bash
sha="$(git rev-parse HEAD)"
wbd create "Manual QA correlation removal" --type task --json
wbd link <returned-id> "$sha"
wbd unlink <returned-id> "$sha"
wbd unlink <returned-id> "$sha"
```

- [ ] Link returns `"added":true`.
- [ ] The first unlink returns `"removed":true`.
- [ ] The second unlink returns `"removed":false` as an idempotent no-op.
- [ ] An abbreviated SHA is rejected without mutation.
- [ ] Unrelated correlations remain visible.
- [ ] History refreshes and no longer associates the disposable item with the
      commit.

Close the disposable record afterward:

```bash
wbd close <returned-id> --reason "Manual QA completed" --json
```

## Privacy And Closeout QA

Run:

```bash
bash skills/beads-hub-closeout/validate.sh
```

- [ ] Output is `private Hub closeout policy validation passed`.
- [ ] The closeout skill requires an eligible concrete record and a merged PR.
- [ ] It resolves the repository-designated reference branch and immutable full
      merge-result SHA.
- [ ] It requires a clean reference checkout and safe `--ff-only`
      synchronization before Hub mutation.
- [ ] It orders Hub mutation as `wbd link` followed by `wbd close`.
- [ ] It documents stop and recovery behavior without stash, reset, history
      rewriting, force-push, or direct private-ledger editing.
- [ ] The previously affected real History item retains its correct
      implementation correlation and no longer shows the erroneous base commit.

## Acceptance Priorities

Treat these areas as the highest risk:

1. History state preservation across refresh.
2. Active search input consuming keys before global routing.
3. Exact type-filter composition with existing filters and repository scope.
4. Exact correlation removal preserving every unrelated ledger record.
5. Search and picker behavior at constrained terminal dimensions.

## Findings

Record manual QA feedback here as it is reported.

| Status | Area | Reproduction | Expected | Actual | Follow-up |
|---|---|---|---|---|---|
| Resolved | History zero-result search | Submit a query with no matches | Empty-state guidance describes keys that work in the current search state | The view advertised `h` to close while active search interpreted `h` as text input | Manually verified and merged in PR #34 |
| Resolved | Issue Type Filter help | Open the picker at the normal QA terminal width | Every control, including `n: reset`, remains intelligible | The single-line footer truncated the reset and subsequent controls | Manually verified and merged in PR #34 |
| Resolved | Repository picker toggle | Press `w` from List, change draft scope, then press `w` again | The second `w` closes the picker without applying draft changes; `w` remains text during picker search | The key opened the picker but did not toggle it closed | Manually verified and merged in PR #35 |
| Resolved | List Help discoverability | Press `?` from List | Help visibly includes uppercase `E` for Tree and uppercase `I` for the exact issue-type picker | The rendered Help omitted both shortcuts | Manually verified and merged in PR #35 |
| Resolved | Status filter toggles | Press `c`, `o`, or `r`, then press the active key again | The first press applies that status and the second clears only the status filter | Active status keys could not toggle their filters off | Manually verified and merged in PR #35 |
| Resolved | Status filter footer | Apply, switch, and clear `c/o/r`, including with a label or recipe active | The normal shortcut footer remains visible immediately and the persistent OPEN/CLOSED/READY badge reflects the effective status filter | A transient message replaced the footer and the status badge was absent until another keypress; composed filters also omitted the badge | Manually verified and merged in PR #35 |
| Resolved | Issue Type Filter toggle | Press uppercase `I` to open the picker, change draft types, then press `I` again | The second `I` cancels the modal without applying draft changes | `I` opened the picker but did not toggle it closed | Manually verified and merged in PR #35 |
| Resolved | Issue Type Filter spacing | Open `I` at the normal QA terminal size | One blank row separates the last visible type from the wrapped controls | Options and controls appeared visually crowded | Manually verified and merged in PR #35 |
| Resolved | Shortcuts sidebar reflow | Press `;` in History, Board, or Insights at a normal wide terminal size | The active view reserves sidebar width and remains coherent; closing restores full width | Sidebar text and borders overlapped or interleaved the full-width view content | Manually verified; approved for merge |
| Resolved | Tree shortcuts sidebar composition | Open Tree with `E`, press `;`, then open Help, Tutorial, or navigate to quit confirmation | Tree fills the region left of a right-aligned sidebar; full-terminal modes exclude the sidebar and restore state when closed | The sidebar followed Tree's natural content width, then interleaved with Help and Tutorial and displaced quit confirmation | Manually verified and merged in PR #37 |
| Resolved | Tree and Tutorial terminal bounds | Open short-content Tree and Tutorial at normal tall/wide terminal sizes | Global status remains bottom-anchored and Tutorial's bordered container spans the available width | Status moved directly below short content and Tutorial left a large unused strip on the right | Manually verified and merged in PR #38 |
| Resolved | Tree search result scope | Submit a Tree query and press `v` | Users can toggle minimal hierarchy context or full matched subtrees while direct-match navigation remains stable | Tree search only showed direct matches and required ancestors | Manually verified and merged in PR #39 |
| Resolved | Tree status-filter routing | Open Tree, then toggle `o`, `c`, and `r` | Tree rows and status chrome immediately reflect the same composable status filters as List and Board | Tree remained stale after status changes and `o` was consumed by Expand All | Manually verified and merged in PR #42 |
| Resolved | List detail issue type | Open List details for issues with different classifications | Summary columns read ID, Type, Status, Priority, Assignee, Created, with canonical textual type values | Detail title showed only a type icon and the summary omitted textual classification | Manually verified and merged in PR #43 |
| Awaiting QA | Active-only Insights and heat-map contrast | Enter Insights from closed-filtered work and inspect metric panels and heat-map legend | Insights exposes only active work with independent `o/r` controls; high heat levels use distinct gold, orange, pink, and burgundy colors | Closed issues appeared in operational rankings and high-end heat colors were difficult to distinguish | Fix implemented and statically reviewed; awaiting manual QA |
