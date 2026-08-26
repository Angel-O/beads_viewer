// Package agents provides AGENTS.md integration for AI coding agents.
// It handles detection, content injection, and preference storage for
// automatically adding beads_viewer usage instructions to agent configuration files.
package agents

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BlurbVersion is the current version of the agent instructions blurb.
// Increment this when making breaking changes to the blurb format.
const BlurbVersion = 4

// BlurbStartMarker marks the beginning of injected agent instructions.
const BlurbStartMarker = "<!-- bv-agent-instructions-v4 -->"

// BlurbEndMarker marks the end of injected agent instructions.
const BlurbEndMarker = "<!-- end-bv-agent-instructions -->"

const blurbStartPrefix = "<!-- bv-agent-instructions-v"

// AgentBlurb contains the instructions to be appended to AGENTS.md files.
// This is the v4 blurb that combines bd/br workflow commands with bv robot triage.
const AgentBlurb = `<!-- bv-agent-instructions-v4 -->

---

## Beads Workflow Integration

This project uses a Beads tracker—either the Go ` + "`" + `bd` + "`" + ` CLI or the Rust ` + "`" + `br` + "`" + ` CLI—for issue tracking, plus [beads_viewer](https://github.com/Dicklesworthstone/beads_viewer) (` + "`" + `bv` + "`" + `) for graph-aware triage. Issues are stored in ` + "`" + `.beads/` + "`" + `. ` + "`" + `bv` + "`" + ` auto-discovers supported JSONL exports, including ` + "`" + `.beads/issues.jsonl` + "`" + ` and legacy ` + "`" + `.beads/beads.jsonl` + "`" + `.

**Choose the tracker CLI from this repository's instructions and configuration.** Use ` + "`" + `bd` + "`" + ` commands in a Go Beads workspace and ` + "`" + `br` + "`" + ` commands in a beads_rust workspace. Do not run both trackers against the same workspace or infer the tracker solely from the JSONL filename.

### Using bv as an AI sidecar

bv is a graph-aware triage engine for Beads projects. Instead of parsing .beads/issues.jsonl / .beads/beads.jsonl directly or hallucinating graph traversal, use robot flags for deterministic, dependency-aware outputs with precomputed metrics (PageRank, betweenness, critical path, cycles, HITS, eigenvector, k-core).

**Scope boundary:** bv handles *what to work on* (triage, priority, planning). The selected tracker CLI (` + "`" + `bd` + "`" + ` or ` + "`" + `br` + "`" + `) handles creating, claiming, modifying, and closing beads.

**CRITICAL: Use ONLY --robot-* flags. Bare bv launches an interactive TUI that blocks your session.**

#### The Workflow: Start With Triage

**` + "`" + `bv --robot-triage` + "`" + ` is your single entry point.** It returns everything you need in one call:
- ` + "`" + `quick_ref` + "`" + `: at-a-glance counts + top 3 picks
- ` + "`" + `recommendations` + "`" + `: ranked actionable items with scores, reasons, unblock info
- ` + "`" + `quick_wins` + "`" + `: low-effort high-impact items
- ` + "`" + `blockers_to_clear` + "`" + `: items that unblock the most downstream work
- ` + "`" + `project_health` + "`" + `: status/type/priority distributions, graph metrics
- ` + "`" + `commands` + "`" + `: copy-paste shell commands for next steps

` + "```" + `bash
bv --robot-triage        # THE MEGA-COMMAND: start here
bv --robot-next          # Minimal: just the single top pick + claim command

# Token-optimized output (TOON) for lower LLM context usage:
bv --robot-triage --format toon
` + "```" + `

Before claiming, verify current state with the selected tracker: ` + "`" + `br show <id> --json` + "`" + `/` + "`" + `br ready --json` + "`" + ` or ` + "`" + `bd show <id> --json` + "`" + `/` + "`" + `bd ready --json` + "`" + `. ` + "`" + `recommendations` + "`" + ` can include graph-important blocked or assigned work; only ` + "`" + `quick_ref.top_picks` + "`" + ` and non-empty ` + "`" + `claim_command` + "`" + ` fields represent claimable work.

#### Other bv Commands

| Command | Returns |
|---------|---------|
| ` + "`" + `--robot-plan` + "`" + ` | Parallel execution tracks with unblocks lists |
| ` + "`" + `--robot-priority` + "`" + ` | Priority misalignment detection with confidence |
| ` + "`" + `--robot-insights` + "`" + ` | Full metrics: PageRank, betweenness, HITS, eigenvector, critical path, cycles, k-core |
| ` + "`" + `--robot-alerts` + "`" + ` | Stale issues, blocking cascades, priority mismatches |
| ` + "`" + `--robot-suggest` + "`" + ` | Hygiene: duplicates, missing deps, label suggestions, cycle breaks |
| ` + "`" + `--robot-diff --diff-since <ref>` + "`" + ` | Changes since ref: new/closed/modified issues |
| ` + "`" + `--robot-graph [--graph-format=json\|dot\|mermaid]` + "`" + ` | Dependency graph export |

#### Scoping & Filtering

` + "```" + `bash
bv --robot-plan --label backend              # Scope to label's subgraph
bv --robot-insights --as-of HEAD~30          # Historical point-in-time
bv --recipe actionable --robot-plan          # Pre-filter: ready to work (no blockers)
bv --recipe high-impact --robot-triage       # Pre-filter: top PageRank scores
` + "```" + `

### Tracker Commands for Issue Management

Use exactly one command family, matching the tracker configured for the repository.

#### Rust beads_rust (` + "`" + `br` + "`" + `)

` + "```" + `bash
br ready --json                       # Show issues ready to work (no blockers)
br list --status=open --json          # All open issues
br show <id> --json                   # Full issue details with dependencies
br create --title="..." --type=task --priority=2 --json
br update <id> --status=in_progress --json
br close <id> --reason="Completed" --json
br close <id1> <id2> --reason="Completed" --json
br sync --flush-only                  # Export DB to JSONL after Beads mutations
` + "```" + `

#### Go Beads (` + "`" + `bd` + "`" + `)

` + "```" + `bash
bd ready --json                       # Show issues ready to work
bd show <id> --json                   # Full issue details
bd create "..." -t task -p 2 --json
bd update <id> --claim --json         # Atomically claim work
bd close <id> --json
bd dep add <issue> <depends-on>
bd export -o .beads/issues.jsonl        # Refresh the compatibility export read by bv
` + "```" + `

### Workflow Pattern

1. **Triage**: Run ` + "`" + `bv --robot-triage` + "`" + ` to find the highest-impact actionable work
2. **Verify**: Check the selected tracker's ` + "`" + `show` + "`" + `/` + "`" + `ready` + "`" + ` output before claiming
3. **Claim**: Use ` + "`" + `br update <id> --status=in_progress --json` + "`" + ` or ` + "`" + `bd update <id> --claim --json` + "`" + `
4. **Work**: Implement the task
5. **Complete**: Use the selected tracker's ` + "`" + `close` + "`" + ` command
6. **Refresh for bv**: Run ` + "`" + `br sync --flush-only` + "`" + ` or the ` + "`" + `bd export` + "`" + ` command above so the JSONL export is current

### Key Concepts

- **Dependencies**: Issues can block other issues. ` + "`" + `br ready --json` + "`" + ` and ` + "`" + `bd ready --json` + "`" + ` show unblocked work.
- **Priority**: P0=critical, P1=high, P2=medium, P3=low, P4=backlog (use numbers 0-4, not words)
- **Types**: task, bug, feature, epic, chore, docs, question
- **Blocking**: Use ` + "`" + `br dep add <issue> <depends-on>` + "`" + ` or ` + "`" + `bd dep add <issue> <depends-on>` + "`" + ` to add dependencies

### Git Policy

Tracker commands do not grant permission to commit or push application code. Follow this repository's own git and tracker instructions before staging, committing, syncing, or pushing. If the repository says "commit only when asked," that rule overrides any generic workflow advice.

<!-- end-bv-agent-instructions -->`

// SupportedAgentFiles lists the filenames that can contain agent instructions.
var SupportedAgentFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"agents.md",
	"claude.md",
}

// blurbVersionRegex validates a complete, standalone version marker after the
// surrounding line whitespace has been removed.
var blurbVersionRegex = regexp.MustCompile(`^<!-- bv-agent-instructions-v(\d+) -->$`)

// LegacyBlurbPatterns are markers that identify the old blurb format (pre-v1, no HTML markers).
var LegacyBlurbPatterns = []string{
	"### Using bv as an AI sidecar",
	"--robot-insights",
	"--robot-plan",
	"bv already computes the hard parts",
}

type markdownLine struct {
	start        int
	end          int
	text         string
	outsideFence bool
}

type markdownContainerKind uint8

const (
	markdownBlockquote markdownContainerKind = iota
	markdownList
)

type markdownContainer struct {
	kind   markdownContainerKind
	indent int
}

type markdownFence struct {
	char       byte
	width      int
	containers []markdownContainer
}

type blurbMarkerKind uint8

const (
	blurbStart blurbMarkerKind = iota
	blurbEnd
	blurbInvalidStart
)

type blurbMarker struct {
	kind       blurbMarkerKind
	version    int
	byteOffset int
	lineStart  int
	lineEnd    int
}

type blurbBlock struct {
	start   int
	end     int
	version int
}

type legacyBlurbSpan struct {
	start int
	end   int
}

// ContainsBlurb checks for a standalone versioned start marker outside fenced
// Markdown. Marker examples in code fences and inline prose are documentation,
// not installed instructions.
func ContainsBlurb(content string) bool {
	for _, marker := range scanBlurbMarkers(content) {
		if marker.kind == blurbStart || marker.kind == blurbInvalidStart {
			return true
		}
	}
	return false
}

// ContainsLegacyBlurb checks if the content contains the old-format blurb (pre-v1, no HTML markers).
// All identifying text must occur, in template order, inside one bounded
// Markdown section. Scattered references in unrelated sections are not a blurb.
func ContainsLegacyBlurb(content string) bool {
	_, ok := findLegacyBlurb(content)
	return ok
}

// ContainsAnyBlurb checks if the content contains either the current or legacy blurb format.
func ContainsAnyBlurb(content string) bool {
	return ContainsBlurb(content) || ContainsLegacyBlurb(content)
}

// GetBlurbVersion returns the highest version advertised by standalone markers
// outside fenced Markdown. Taking the maximum prevents an older first block
// from hiding a later blurb written by a newer bv binary.
func GetBlurbVersion(content string) int {
	maxVersion := 0
	for _, marker := range scanBlurbMarkers(content) {
		if marker.kind == blurbStart && marker.version > maxVersion {
			maxVersion = marker.version
		}
	}
	return maxVersion
}

// NeedsUpdate checks whether the content needs normalization or an updated
// blurb. Malformed marker structure and multiple complete versioned blocks both
// require attention even when the first marker advertises the current version.
func NeedsUpdate(content string) bool {
	count, err := inspectBlurbStructure(content)
	if err != nil || count > 1 {
		return true
	}
	if ContainsLegacyBlurb(content) {
		return true
	}
	if count == 0 {
		return false
	}
	return GetBlurbVersion(content) != BlurbVersion
}

// AppendBlurb appends the agent blurb to the given content.
func AppendBlurb(content string) string {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "\n"
	content += AgentBlurb
	content += "\n"
	return content
}

// RemoveBlurb removes all structurally valid versioned and legacy blurbs from
// the content. Malformed versioned markers are left byte-for-byte unchanged;
// file-writing callers use removeBlurbsChecked so they can surface the error.
func RemoveBlurb(content string) string {
	updated, err := removeBlurbsChecked(content)
	if err != nil {
		return content
	}
	return updated
}

func removeFirstVersionedBlurb(content string) string {
	blocks, err := inspectBlurbBlocks(content)
	if err != nil || len(blocks) == 0 {
		return content
	}
	return removeDelimitedBlurb(content, blocks[0].start, blocks[0].end)
}

// RemoveLegacyBlurb removes the old-format blurb (pre-v1, no HTML markers) from content.
func RemoveLegacyBlurb(content string) string {
	span, ok := findLegacyBlurb(content)
	if !ok {
		return content
	}
	return removeDelimitedBlurb(content, span.start, span.end)
}

// UpdateBlurb replaces existing, structurally valid blurbs with the current
// version. Malformed markers are left byte-for-byte unchanged; file-writing
// callers use updateBlurbChecked so they can surface the validation error.
func UpdateBlurb(content string) string {
	updated, err := updateBlurbChecked(content)
	if err != nil {
		return content
	}
	return updated
}

func updateBlurbChecked(content string) (string, error) {
	var err error
	content, err = prepareBlurbMutation(content, "replace")
	if err != nil {
		return "", err
	}
	for ContainsBlurb(content) {
		withoutBlurb := removeFirstVersionedBlurb(content)
		if withoutBlurb == content {
			return "", fmt.Errorf("malformed bv agent blurb: unable to remove validated marker")
		}
		content = withoutBlurb
	}
	updated := AppendBlurb(content)
	count, err := inspectBlurbStructure(updated)
	if err != nil {
		return "", fmt.Errorf("validate updated bv agent blurb: %w", err)
	}
	version := GetBlurbVersion(updated)
	if count != 1 || version != BlurbVersion {
		return "", fmt.Errorf("validate updated bv agent blurb: found %d standalone versioned blocks at v%d, want exactly one v%d block", count, version, BlurbVersion)
	}
	return updated, nil
}

func removeBlurbsChecked(content string) (string, error) {
	var err error
	content, err = prepareBlurbMutation(content, "remove")
	if err != nil {
		return "", err
	}
	for ContainsBlurb(content) {
		withoutBlurb := removeFirstVersionedBlurb(content)
		if withoutBlurb == content {
			return "", fmt.Errorf("malformed bv agent blurb: unable to remove validated marker")
		}
		content = withoutBlurb
	}
	return content, nil
}

// prepareBlurbMutation validates both the original Markdown view and a
// non-mutating view with recognized legacy blurbs removed. Historical legacy
// copies can end in a dangling bare fence: a later versioned blurb's own code
// fence may then make its end marker look stray in the original view. Inspect
// the legacy-free view before returning that original structural error so a
// hidden future-version blurb is still identified and protected. For all
// non-future content, the original structural error remains fail-closed.
func prepareBlurbMutation(content, action string) (string, error) {
	originalBlocks, originalStructureErr := inspectBlurbBlocks(content)
	if originalStructureErr == nil {
		if err := rejectFutureBlurbBlocks(originalBlocks, action); err != nil {
			return "", err
		}
	}

	withoutLegacy, ambiguousFencePreserved, realLegacyRemovals, err := removeLegacyBlurbsChecked(content)
	if err != nil {
		return "", err
	}
	revealedBlocks, revealedStructureErr := inspectBlurbBlocks(withoutLegacy)
	if err := rejectFutureBlurbBlocks(revealedBlocks, action); err != nil {
		return "", err
	}
	// An ambiguous adjacent fence is preserved by real removal because it may be
	// a user code-block opener. Inspect a hypothetical view that consumes that
	// historical delimiter, but never write it. Future-version refusal takes
	// precedence; any other marker visibility or legacy-removal-count ambiguity
	// fails closed rather than choosing which user bytes are installed content.
	if ambiguousFencePreserved || originalStructureErr != nil || revealedStructureErr != nil {
		analysisView, _, analysisLegacyRemovals, analysisErr := removeLegacyBlurbsCheckedWithPolicy(content, true)
		if analysisErr != nil {
			return "", analysisErr
		}
		analysisBlocks, analysisStructureErr := inspectBlurbBlocks(analysisView)
		if err := rejectFutureBlurbBlocks(analysisBlocks, action); err != nil {
			return "", err
		}
		if ambiguousFencePreserved && (len(scanBlurbMarkers(withoutLegacy)) > 0 || len(scanBlurbMarkers(analysisView)) > 0) {
			return "", fmt.Errorf("malformed bv agent blurb: ambiguous marker material hidden by preserved legacy fence; refusing to %s", action)
		}
		if ambiguousFencePreserved && realLegacyRemovals != analysisLegacyRemovals {
			return "", fmt.Errorf("malformed legacy bv agent blurb: ambiguous fence changes removal count from %d to %d; refusing to %s", realLegacyRemovals, analysisLegacyRemovals, action)
		}
		if analysisStructureErr != nil && originalStructureErr == nil && revealedStructureErr == nil {
			return "", analysisStructureErr
		}
	}
	if originalStructureErr != nil {
		return "", originalStructureErr
	}
	if revealedStructureErr != nil {
		return "", revealedStructureErr
	}
	return withoutLegacy, nil
}

func rejectFutureBlurbBlocks(blocks []blurbBlock, action string) error {
	for _, block := range blocks {
		if block.version > BlurbVersion {
			return fmt.Errorf("refusing to %s bv agent blurb v%d with older bv v%d", action, block.version, BlurbVersion)
		}
	}
	return nil
}

func removeLegacyBlurbsChecked(content string) (string, bool, int, error) {
	return removeLegacyBlurbsCheckedWithPolicy(content, false)
}

func removeLegacyBlurbsCheckedWithPolicy(content string, consumeAmbiguousFence bool) (string, bool, int, error) {
	ambiguousFencePreserved := false
	removed := 0
	for {
		span, ok, ambiguous := findLegacyBlurbWithPolicy(content, consumeAmbiguousFence)
		ambiguousFencePreserved = ambiguousFencePreserved || ambiguous
		if !ok {
			return content, ambiguousFencePreserved, removed, nil
		}
		withoutBlurb := removeDelimitedBlurb(content, span.start, span.end)
		if withoutBlurb == content {
			return "", ambiguousFencePreserved, removed, fmt.Errorf("malformed legacy bv agent blurb: unable to remove detected content")
		}
		content = withoutBlurb
		removed++
	}
}

func validateBlurbStructure(content string) error {
	_, err := inspectBlurbStructure(content)
	return err
}

func inspectBlurbStructure(content string) (int, error) {
	blocks, err := inspectBlurbBlocks(content)
	return len(blocks), err
}

func inspectBlurbBlocks(content string) ([]blurbBlock, error) {
	markers := scanBlurbMarkers(content)
	blocks := make([]blurbBlock, 0, len(markers)/2)
	var open *blurbMarker
	for i := range markers {
		marker := &markers[i]
		switch marker.kind {
		case blurbInvalidStart:
			return blocks, fmt.Errorf("malformed bv agent blurb: invalid start marker at byte %d", marker.byteOffset)
		case blurbStart:
			if open != nil {
				return blocks, fmt.Errorf("malformed bv agent blurb: nested start marker at byte %d", marker.byteOffset)
			}
			open = marker
		case blurbEnd:
			if open == nil {
				return blocks, fmt.Errorf("malformed bv agent blurb: end marker at byte %d has no start marker", marker.byteOffset)
			}
			blocks = append(blocks, blurbBlock{start: open.lineStart, end: marker.lineEnd, version: open.version})
			open = nil
		}
	}
	if open != nil {
		return blocks, fmt.Errorf("malformed bv agent blurb: start marker at byte %d has no end marker", open.byteOffset)
	}
	return blocks, nil
}

func scanBlurbMarkers(content string) []blurbMarker {
	lines := scanMarkdownLines(content)
	markers := make([]blurbMarker, 0, 2)
	for _, line := range lines {
		if !line.outsideFence {
			continue
		}
		trimmed, indent, ok := standaloneMarkdownText(line.text)
		if !ok {
			continue
		}
		marker := blurbMarker{
			byteOffset: line.start + indent,
			lineStart:  line.start,
			lineEnd:    line.end,
		}
		switch {
		case trimmed == BlurbEndMarker:
			marker.kind = blurbEnd
			markers = append(markers, marker)
		case strings.HasPrefix(trimmed, blurbStartPrefix):
			matches := blurbVersionRegex.FindStringSubmatch(trimmed)
			if len(matches) != 2 {
				marker.kind = blurbInvalidStart
				markers = append(markers, marker)
				continue
			}
			version, err := strconv.Atoi(matches[1])
			if err != nil {
				marker.kind = blurbInvalidStart
				markers = append(markers, marker)
				continue
			}
			marker.kind = blurbStart
			marker.version = version
			markers = append(markers, marker)
		}
	}
	return markers
}

func scanMarkdownLines(content string) []markdownLine {
	lines := make([]markdownLine, 0, strings.Count(content, "\n")+1)
	var fence markdownFence
	htmlCommentDepth := 0
	htmlRawTag := ""
	for start := 0; start < len(content); {
		end := start
		for end < len(content) && content[end] != '\n' && content[end] != '\r' {
			end++
		}
		next := end
		if next < len(content) {
			if content[next] == '\r' && next+1 < len(content) && content[next+1] == '\n' {
				next += 2
			} else {
				next++
			}
		}
		text := content[start:end]
		outside := true
		if fence.char != 0 {
			if strings.TrimSpace(text) == "" {
				// Blank lines remain inside list and blockquote fenced blocks even
				// when their container prefix is omitted.
				outside = false
			} else if remainder, ok := stripMarkdownContainers(text, fence.containers); ok {
				outside = false
				if char, width, rest, isFence := markdownFenceRun(remainder); isFence && char == fence.char && width >= fence.width && strings.TrimSpace(rest) == "" {
					fence = markdownFence{}
				}
			} else {
				// An unclosed fenced block inside a list/blockquote ends when its
				// containing block ends. Reprocess this line as top-level Markdown.
				fence = markdownFence{}
			}
		}
		if outside && htmlCommentDepth > 0 {
			htmlCommentDepth, _ = advanceHTMLCommentDepth(text, htmlCommentDepth)
			outside = false
		} else if outside && htmlRawTag != "" {
			if closesHTMLRawBlock(text, htmlRawTag) {
				htmlRawTag = ""
			}
			outside = false
		} else if outside {
			if tag := opensHTMLRawBlock(text); tag != "" {
				htmlRawTag = tag
				if closesHTMLRawBlock(text, tag) {
					htmlRawTag = ""
				}
				outside = false
			} else {
				var sawHTMLComment bool
				htmlCommentDepth, sawHTMLComment = advanceHTMLCommentDepth(text, 0)
				// Versioned blurb delimiters are themselves complete HTML comments,
				// so recognize a standalone delimiter at top level. Everything inside
				// an already-open multiline comment remains documentation, including
				// nested marker-shaped examples.
				if sawHTMLComment && !standaloneBlurbMarkerText(text) {
					outside = false
				}
			}
		}
		if outside {
			if char, width, rest, containers, ok := markdownFenceOpening(text); ok {
				// Backtick info strings cannot contain backticks in CommonMark.
				if char != '`' || !strings.Contains(rest, "`") {
					outside = false
					fence = markdownFence{char: char, width: width, containers: containers}
				}
			}
		}
		lines = append(lines, markdownLine{start: start, end: end, text: text, outsideFence: outside})
		start = next
	}
	return lines
}

func advanceHTMLCommentDepth(line string, depth int) (int, bool) {
	sawComment := false
	for pos := 0; pos < len(line); {
		switch {
		case strings.HasPrefix(line[pos:], "<!--"):
			depth++
			sawComment = true
			pos += len("<!--")
		case strings.HasPrefix(line[pos:], "-->"):
			if depth > 0 {
				depth--
				sawComment = true
			}
			pos += len("-->")
		default:
			pos++
		}
	}
	return depth, sawComment
}

func standaloneBlurbMarkerText(line string) bool {
	trimmed, _, ok := standaloneMarkdownText(line)
	if !ok {
		return false
	}
	return trimmed == BlurbEndMarker || strings.HasPrefix(trimmed, blurbStartPrefix)
}

func opensHTMLRawBlock(line string) string {
	trimmed, _, ok := standaloneMarkdownText(line)
	if !ok {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, tag := range [...]string{"pre", "script", "style", "textarea"} {
		prefix := "<" + tag
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		if len(lower) == len(prefix) {
			return tag
		}
		switch lower[len(prefix)] {
		case ' ', '\t', '>':
			return tag
		}
	}
	return ""
}

func closesHTMLRawBlock(line, tag string) bool {
	return strings.Contains(strings.ToLower(line), "</"+tag+">")
}

func markdownFenceOpening(line string) (byte, int, string, []markdownContainer, bool) {
	remainder, containers, ok := stripMarkdownOpeningContainers(line)
	if !ok {
		return 0, 0, "", nil, false
	}
	char, width, rest, ok := markdownFenceRun(remainder)
	return char, width, rest, containers, ok
}

// stripMarkdownOpeningContainers removes list and blockquote prefixes from a
// possible fence-opening line. List continuation indentation is retained as a
// sequence so closing fences and content can be recognized without treating
// ordinary four-space-indented code as a top-level fence.
func stripMarkdownOpeningContainers(line string) (string, []markdownContainer, bool) {
	line = strings.TrimSuffix(line, "\r")
	containers := make([]markdownContainer, 0, 2)
	pos := 0
	for {
		spaces := countLeadingSpaces(line[pos:])
		if spaces > 3 {
			return "", nil, false
		}
		pos += spaces
		if pos >= len(line) {
			return line[pos:], containers, true
		}

		if line[pos] == '>' {
			containers = append(containers, markdownContainer{kind: markdownBlockquote})
			pos++
			if pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
				pos++
			}
			continue
		}

		markerWidth, gapWidth, isList := markdownListMarker(line[pos:])
		if isList {
			containers = append(containers, markdownContainer{
				kind:   markdownList,
				indent: spaces + markerWidth + gapWidth,
			})
			pos += markerWidth + gapWidth
			continue
		}

		return line[pos:], containers, true
	}
}

func stripMarkdownContainers(line string, containers []markdownContainer) (string, bool) {
	line = strings.TrimSuffix(line, "\r")
	pos := 0
	for _, container := range containers {
		switch container.kind {
		case markdownBlockquote:
			spaces := countLeadingSpaces(line[pos:])
			if spaces > 3 {
				return "", false
			}
			pos += spaces
			if pos >= len(line) || line[pos] != '>' {
				return "", false
			}
			pos++
			if pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
				pos++
			}
		case markdownList:
			if container.indent <= 0 || len(line)-pos < container.indent {
				return "", false
			}
			for i := 0; i < container.indent; i++ {
				if line[pos+i] != ' ' {
					return "", false
				}
			}
			pos += container.indent
		}
	}
	return line[pos:], true
}

func markdownListMarker(line string) (int, int, bool) {
	if len(line) < 2 {
		return 0, 0, false
	}
	markerWidth := 0
	switch line[0] {
	case '-', '+', '*':
		markerWidth = 1
	default:
		for markerWidth < len(line) && markerWidth < 9 && line[markerWidth] >= '0' && line[markerWidth] <= '9' {
			markerWidth++
		}
		if markerWidth == 0 || markerWidth >= len(line) || (line[markerWidth] != '.' && line[markerWidth] != ')') {
			return 0, 0, false
		}
		markerWidth++
	}
	if markerWidth >= len(line) {
		return 0, 0, false
	}
	if line[markerWidth] == '\t' {
		return markerWidth, 1, true
	}
	if line[markerWidth] != ' ' {
		return 0, 0, false
	}
	gapWidth := countLeadingSpaces(line[markerWidth:])
	if gapWidth < 1 || gapWidth > 4 {
		return 0, 0, false
	}
	return markerWidth, gapWidth, true
}

func countLeadingSpaces(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func standaloneMarkdownText(line string) (string, int, bool) {
	line = strings.TrimSuffix(line, "\r")
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || (indent < len(line) && line[indent] == '\t') {
		return "", 0, false
	}
	return strings.TrimSpace(line[indent:]), indent, true
}

func markdownFenceRun(line string) (byte, int, string, bool) {
	trimmed, _, ok := standaloneMarkdownText(line)
	if !ok || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, "", false
	}
	char := trimmed[0]
	width := 0
	for width < len(trimmed) && trimmed[width] == char {
		width++
	}
	if width < 3 {
		return 0, 0, "", false
	}
	return char, width, trimmed[width:], true
}

func markdownHeading(line markdownLine) (int, string, bool) {
	if !line.outsideFence {
		return 0, "", false
	}
	trimmed, _, ok := standaloneMarkdownText(line.text)
	if !ok || trimmed == "" || trimmed[0] != '#' {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level > 6 || level == len(trimmed) || (trimmed[level] != ' ' && trimmed[level] != '\t') {
		return 0, "", false
	}
	body := strings.TrimRight(trimmed[level:], " \t")
	closingStart := len(body)
	for closingStart > 0 && body[closingStart-1] == '#' {
		closingStart--
	}
	// CommonMark only treats a trailing # run as an ATX closing sequence when
	// whitespace separates it from the heading text. Without that separator,
	// the hashes are literal title bytes and must participate in legacy matching.
	if closingStart < len(body) && closingStart > 0 && (body[closingStart-1] == ' ' || body[closingStart-1] == '\t') {
		body = strings.TrimRight(body[:closingStart], " \t")
	}
	title := strings.TrimSpace(body)
	return level, title, true
}

func findLegacyBlurb(content string) (legacyBlurbSpan, bool) {
	span, ok, _ := findLegacyBlurbWithPolicy(content, false)
	return span, ok
}

func findLegacyBlurbWithPolicy(content string, consumeAmbiguousFence bool) (legacyBlurbSpan, bool, bool) {
	lines := scanMarkdownLines(content)
	for i, line := range lines {
		level, title, ok := markdownHeading(line)
		if !ok || level != 3 || title != "Using bv as an AI sidecar" {
			continue
		}

		sectionEnd := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if nextLevel, _, isHeading := markdownHeading(lines[j]); isHeading && nextLevel <= level {
				sectionEnd = j
				break
			}
		}

		sawInsights := false
		sawPlan := false
		endLine := -1
		for j := i + 1; j < sectionEnd; j++ {
			if !lines[j].outsideFence {
				continue
			}
			text := lines[j].text
			sawInsights = sawInsights || strings.Contains(text, "--robot-insights")
			sawPlan = sawPlan || strings.Contains(text, "--robot-plan")
			if sawInsights && sawPlan && strings.Contains(text, "bv already computes the hard parts") {
				endLine = j
				break
			}
		}
		if endLine == -1 {
			continue
		}

		end := lines[endLine].end
		ambiguousFencePreserved := false
		// Some historical copies have a bare closing delimiter immediately
		// after the identifying sentence. Do not skip blank lines looking for
		// one: a later bare fence is an unrelated user code-block opener and
		// must be preserved.
		if next := endLine + 1; next < sectionEnd {
			char, width, rest, isFence := markdownFenceRun(lines[next].text)
			if isFence && strings.TrimSpace(rest) == "" {
				clearlyTrailing := legacyFenceIsClearlyTrailing(lines, next, sectionEnd, char, width)
				if consumeAmbiguousFence || clearlyTrailing {
					end = lines[next].end
				} else {
					ambiguousFencePreserved = true
				}
			}
		}
		return legacyBlurbSpan{start: line.start, end: end}, true, ambiguousFencePreserved
	}
	return legacyBlurbSpan{}, false, false
}

func legacyFenceIsClearlyTrailing(lines []markdownLine, opener, sectionEnd int, char byte, width int) bool {
	if hasMatchingFenceClose(lines, opener+1, sectionEnd, char, width) {
		return false
	}
	for i := opener + 1; i < sectionEnd; i++ {
		if strings.TrimSpace(lines[i].text) == "" {
			continue
		}
		// Any content after an unmatched candidate may belong to an unfinished
		// user fence, including lines that look like Markdown headings.
		return false
	}
	return true
}

func hasMatchingFenceClose(lines []markdownLine, start, end int, char byte, width int) bool {
	for i := start; i < end; i++ {
		candidateChar, candidateWidth, rest, ok := markdownFenceRun(lines[i].text)
		if ok && candidateChar == char && candidateWidth >= width && strings.TrimSpace(rest) == "" {
			return true
		}
	}
	return false
}

func removeDelimitedBlurb(content string, startIdx, endIdx int) string {
	prefixEnd := trimLineBreaksBefore(content, startIdx)
	suffixStart := trimLineBreaksAfter(content, endIdx)
	if prefixEnd > 0 && suffixStart < len(content) {
		separator := preferredLineBreak(content[prefixEnd:startIdx] + content[endIdx:suffixStart])
		return content[:prefixEnd] + separator + content[suffixStart:]
	}
	return content[:prefixEnd] + content[suffixStart:]
}

func trimLineBreaksBefore(content string, idx int) int {
	for idx > 0 {
		switch content[idx-1] {
		case '\n':
			idx--
			if idx > 0 && content[idx-1] == '\r' {
				idx--
			}
		case '\r':
			idx--
		default:
			return idx
		}
	}
	return idx
}

func trimLineBreaksAfter(content string, idx int) int {
	for idx < len(content) {
		switch content[idx] {
		case '\r':
			idx++
			if idx < len(content) && content[idx] == '\n' {
				idx++
			}
		case '\n':
			idx++
		default:
			return idx
		}
	}
	return idx
}

func preferredLineBreak(removedWhitespace string) string {
	if !strings.ContainsAny(removedWhitespace, "\r\n") {
		return ""
	}
	if strings.Contains(removedWhitespace, "\r\n") {
		return "\r\n"
	}
	if strings.Contains(removedWhitespace, "\r") && !strings.Contains(removedWhitespace, "\n") {
		return "\r"
	}
	return "\n"
}
