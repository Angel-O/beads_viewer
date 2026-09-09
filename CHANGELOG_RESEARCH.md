# Changelog research: v0.24.1 and its follow-ups

Scope: the complete `v0.24.0..v0.24.1` and `v0.24.1..40a7cd07` commit
windows. Earlier changelog entries are preserved, not re-audited by this update.
This is a bounded application of `changelog-md-workmanship`, requested after
publication of v0.24.1. Dates use UTC publication dates for GitHub Releases.

Evidence order: Git diffs and tag identities, live GitHub Release metadata,
checked-in Beads history, retained release receipts, then existing release
documentation. Public source links belong in the changelog; local execution
evidence supplements them here.

## September 9 dashboard follow-up

The additional `bv-oonu.13` repair follows the actual dependent-to-prerequisite
edges through Rust what-if/actionability/parallel-cut queries, SQLite export
metadata, both browser graph engines and the detail/priority templates. The
original Chromium bundle returns zero direct unblocks for a root that releases
one child; the repaired root reports one direct and two transitive unblocks.
Closed and tombstone prerequisite identities survive omitted issue rows. A
second real browser failure exposed calls to an unsupported renderer refresh
method; those now use the existing redraw helper. Reset cancels pending timers.
The cleanup regression also caught the renderer ticking after its graph was
released. Cleanup now stops rendering using the documented
[`pauseAnimation()` API](https://github.com/vasturiano/force-graph#render-control),
confirmed in the bundled source, and delayed zoom callbacks check graph identity.

The unchanged pinned rebuild harness produces identical glue and WASM from
two physical Rust homes and passes all five graph fixtures and negative
controls. Root re-execution passes 202 unit and 25 existing Rust golden tests;
the affected Go run passes 515 top-level tests with two existing export skips.
Both real Chromium simulation variants pass, including actual edge pixels,
displayed gains and reset. Browser setup failures (visible row count, CDP proxy
serialization and the default link-color accessor) are retained separately
from product counterexamples. Evidence: `/data/tmp/bv-whatif-20260909-5k4UlI`.
No version bump or publication accompanies this follow-up.
Final source commit: `40a7cd07d0dab2d8a2a7a8c0ee1874b120fe3d9a`; preceding
`aa5efbeb` only records the previous repair's evidence. A clean binary from
the final commit, both Chromium variants, the existing blocking-types journey,
515 Go tests and 227 Rust tests pass on fresh solo re-execution. The original
bundle still fails the exact direct-count assertion. `bv-oonu.13` closes after
that replay. UBS remains nonzero on inspected heuristics; no suppression or
golden regeneration was added. Fresh release metadata still identifies
v0.24.1 published September 8 at 00:28:07 UTC, neither draft nor prerelease;
repository Actions remain disabled.

The nine-commit window `b6e21d22..1c768eac` contains two runtime changes:
`55ec82b8` preserves producer workflow vocabulary and classifies blocking types;
`1c768eac` connects those types to the static dashboard's SQL and JavaScript
consumers and fixes reversed dependency keyboard navigation. `8ab18310` corrects
the test setup for the documented unset insight limit. The other six commits
(`953c831d`, `8477a01f`, `5003be9d`, `768a1db9`, `cf6fe649`, `3dc66c65`)
record earlier delivery, research and remaining proof. They add no runtime
capability. Source diffs and the existing Beads evidence establish these scopes.

The dashboard counterexample exports seven real JSONL issues into SQLite:
four active blocking dependents, one closed dependent and one informational
reference to a custom-status prerequisite. Original `55ec82b8` reports four
dependents but lists only two; Chromium fails the exact expected-ID assertion.
The repaired bundle includes all four, keeps only the informational reference
ready, preserves historical graph edges, and executes h/l navigation correctly.
The SQL regression covers eight relationship types and five endpoint lifecycle
pairs. Its first setup omitted analyzer metrics; that setup error and the first
failed full suite are retained, not counted as product counterexamples.

The final full export package has 328 top-level passes and two existing skips
(live GitHub Pages deployment and a Windows-specific path test). All 35 selected
page-export CLI tests pass. Build/vet and first-party formatting pass; UBS exits
1 on reviewed heuristics, so this is not a clean scanner claim. Evidence is in
`/data/tmp/bv-continuation-20260908-o5jLRf`; `bv-oonu.12` records the fresh solo
acceptance replay and its limits. No release, native-platform or complete
performance-matrix result follows from this repair. Fresh GitHub metadata still
reports published v0.24.1 at `2026-09-08T00:28:07Z`, neither draft nor prerelease;
Actions remain disabled. This extends the existing research memo only.
The changelog validator passes structural checks with the existing older-history
bare-hash warning. The documentation-only UBS attempt exits 3 because Markdown
is unsupported; it checks nothing and is not counted as a pass.

## September 8 Flow follow-up

Small-update window `7983ee3f..b6e21d22`, researched from all four commits:
`84c3774e` updates the previous changelog; `eb2a7c86` records the assessment and
Flow counterexamples; `ce982942` records tracker comments only; `b6e21d22`
implements directed relationship drilldowns, live endpoint inspection and the
identified documentation repairs. The complete runtime/test diff was reviewed,
then original acceptance replayed on exact `b6e21d22f6098a78553aed2a365526bf98730fd6`.
Beads `bv-apal.12` and `.13` are closed after solo verification; `.3` and `.4`
remain unfinished. No new release or tag was created. Fresh GitHub metadata
still identifies v0.24.1 as published at `2026-09-08T00:28:07Z`, neither draft
nor prerelease. Repository Actions are disabled.

The original rendered-model counterexample now passes unchanged. Complete
UI/analysis race and focused internal/docs/PTY execution produced 1,862
top-level passes and 19 explicitly retained pre-existing skips. A separate
post-claim P6 replay passes 27 top-level tests with no skips. These are solo
source and Linux terminal fixture checks, not independent-agent, full performance
matrix or native-target proof. Raw streams and failed setup attempts remain in
`/data/tmp/bv-reality-20260908-1umcHa`; the detailed honesty inventory is in `.12`.
The selected-file UBS scan exits 1 on reviewed heuristic findings; no clean
scanner result or suppressed rule is claimed. This existing memo records the
requested changelog provenance and retires as an active checklist with the update.

## Coverage and completion

- [x] Read the skill and its research, linking, and quality guidance; review
  project instructions, installation guidance, and existing changelog.
- [x] Verify the v0.24.0 and v0.24.1 tags and live publication dates.
- [x] Research every commit in the v0.24.1 release window, inspect runtime
  diffs and regressions, and connect the changes to Beads workstreams.
- [x] Distill that window into the live changelog with representative links.
- [x] Research all five post-tag commits through `7983ee3f`; distinguish
  installer delivery from the immutable tagged binaries and test evidence.
- [x] Distill the follow-up under Unreleased, including material limits.
- [x] Correct the README sentence that still described the newly pinned
  Windows installer's source option as the older `go install` implementation.
- [x] Check scope, dates, links, tracker references, and unchanged older history.
- [x] Run the skill validator, whitespace validation, and the required UBS
  attempt; record results. Delivery is tracked in documentation bead `bv-ie9m`.

## Findings

### Release window: 11 commits, distilled

Live GitHub metadata identifies v0.24.0 as published at
`2026-09-07T06:29:55Z` and v0.24.1 at `2026-09-08T00:28:07Z`; neither is a
draft or prerelease. Annotated tags resolve respectively to
`8c1f0011a78b4f291ff175af7276ae450f772cd2` and
`3e4e61c91a74dafe211d3f6a62f3c2919969657c`. GitHub's v0.24.1
`target_commitish` says `main`; the tag and binary receipts establish the
immutable revision, not that mutable field.

All commits in `v0.24.0..v0.24.1` are accounted for:

- `6c8a4474`: loaded source-hash reuse; inspected the full runtime diff and
  all 15 source/scope cases exercised by two robot commands.
- `f719c41a`: optional cache writes use the existing nonblocking lock;
  inspected real contention, entry preservation, later-publication tests,
  and the Windows test-fixture correction to capture open-handle identity.
- `3e4e61c9`: version, Nix, installation examples, release documentation,
  changelog, and release-tracking metadata.
- `59d98598`, `43c6cc62`, `cf7850d5`, `4a5a564f`, `f8fd09c0`, `578e34bf`,
  `51a15788`, `ce0d64ee`: Beads and bridge-plan evidence updates for the
  latency campaign. These do not add runtime behavior or close that campaign.

The resulting themes are preserved hash semantics, bounded optional-cache
publication, and verified release distribution. Homebrew commit `cae0685b`
and Scoop commit `4fddb86e` publish the five matching archive hashes.
The release bead was still in progress at the tag; its later closure is
linked to the checked-in record at `7983ee3f` (line 422). The latency bead
remains in progress at line 283.

Evidence retained in `/data/tmp/bv-release-v0.24.1-20260908`:

- `gate-result.json`: exit 0 on the clean tagged revision, 835.838 seconds.
- `publish/release-gate-receipt.json`: all five archives identify that revision,
  Go 1.25.5, CGO disabled, and unmodified source.
- Live release API: 14 published assets, including the receipt and Linux amd64
  SBOM, with no minisign asset.
- `performance-readback.json`: original campaign exit 1 retained; 144 outputs
  have only 72 `/version` differences (v0.23.0 baseline, v0.24.0 candidate).
  No new performance measurement or full acceptance is implied.

The changelog now corrects the stale scope and links the runtime commits,
published receipt, package-store commits, and precise tracker records.

### Post-tag window: five commits, distilled

- `3ca2176f`: function-local PowerShell progress suppression and correction of
  the native harness's obsolete top-level ID assertion. The robot binary's
  output schema did not change in this commit.
- `8bdfc079`: distribution and native-test evidence in Beads/release docs.
- `99d2066d`: despite its subject naming graph WASM, the actual diff appends
  comment 418 to **bv-oonu.10**, recording release distribution and remaining
  Windows/native-platform limits. No WASM implementation changed.
- `87756815`: four README installer URLs pin `3ca2176f`; native follow-up
  findings added to release documentation.
- `7983ee3f`: release closure and the complete default Windows suite's evidence;
  the optional source first-start failure and broader acceptance stay open.

The full installer and harness diffs establish that progress suppression is
post-tag. No Go runtime file differs between v0.24.1 and the research endpoint.
`windows-download-readback.json` records the default suite pass (28 logs,
eight capability results, five rejection reasons). `retained-source-startup.json`
explicitly labels the successful 1,742 ms invocation as a second diagnostic
attempt, not a replacement for the failed first-start result.

README still claimed the newly pinned installer used `go install`. Inspection
of `Install-FromSource` confirms a tagged checkout, `go build -mod=vendor`,
version checks, and revision checks; the installer is unchanged since
`3ca2176f`. Correcting that one sentence accompanies this changelog update.
The older XFetch expiry repair (`8285b6f6`) is an ancestor of v0.24.0 and is
not credited to v0.24.1.

All runtime and publication findings from both windows are distilled in
CHANGELOG.md. Older entries remain outside this audit. No additional runtime
change, new benchmark measurement, or new release publication is part of it.

## Validation

- The skill's `validate-changelog-md.py CHANGELOG.md` passes structural checks.
  Its sole warning concerns bare hashes in retained v0.24.0/older entries;
  those entries are outside this bounded historical audit.
- All 19 inline HTTP links in the updated introduction, timeline, Unreleased,
  and v0.24.1 sections returned HTTP 200. Checked with the repository-required
  User-Agent rather than the validator's hard-coded network User-Agent.
  Results: `/data/tmp/bv-release-v0.24.1-20260908/changelog-link-check.json`.
- All three pinned Beads line references resolve to the named records and
  stated statuses. Local research/release-documentation links resolve.
- A byte comparison confirms the v0.24.0 heading's following content through
  the reference footer is unchanged. `git diff --check` passes.
- UBS does not support Markdown/JSONL and exits 3 without scanning; this is
  not reported as a passing code scan. No Go code changed, so no Go test rerun
  was needed for this documentation update.
