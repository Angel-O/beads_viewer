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
	blocks, err := inspectBlurbBlocks(content)
	if err != nil {
		return "", err
	}
	for _, block := range blocks {
		if block.version > BlurbVersion {
			return "", fmt.Errorf("refusing to replace bv agent blurb v%d with older v%d", block.version, BlurbVersion)
		}
	}

	content, err = removeLegacyBlurbsChecked(content)
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
	return AppendBlurb(content), nil
}

func removeBlurbsChecked(content string) (string, error) {
	if err := validateBlurbStructure(content); err != nil {
		return "", err
	}
	var err error
	content, err = removeLegacyBlurbsChecked(content)
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

func removeLegacyBlurbsChecked(content string) (string, error) {
	for ContainsLegacyBlurb(content) {
		withoutBlurb := RemoveLegacyBlurb(content)
		if withoutBlurb == content {
			return "", fmt.Errorf("malformed legacy bv agent blurb: unable to remove detected content")
		}
		content = withoutBlurb
	}
	return content, nil
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
	var fenceChar byte
	var fenceWidth int
	for start := 0; start < len(content); {
		relEnd := strings.IndexByte(content[start:], '\n')
		end := len(content)
		next := len(content)
		if relEnd >= 0 {
			end = start + relEnd
			next = end + 1
		}
		text := content[start:end]
		outside := fenceChar == 0
		if fenceChar != 0 {
			outside = false
			if char, width, rest, ok := markdownFenceRun(text); ok && char == fenceChar && width >= fenceWidth && strings.TrimSpace(rest) == "" {
				fenceChar = 0
				fenceWidth = 0
			}
		} else if char, width, _, ok := markdownFenceRun(text); ok {
			outside = false
			fenceChar = char
			fenceWidth = width
		}
		lines = append(lines, markdownLine{start: start, end: end, text: text, outsideFence: outside})
		start = next
	}
	return lines
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
	title := strings.TrimSpace(strings.TrimRight(trimmed[level:], "#"))
	return level, title, true
}

func findLegacyBlurb(content string) (legacyBlurbSpan, bool) {
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
		for j := endLine + 1; j < sectionEnd; j++ {
			trimmed := strings.TrimSpace(lines[j].text)
			if trimmed == "" {
				continue
			}
			if trimmed == "```" || trimmed == "~~~" {
				end = lines[j].end
			}
			break
		}
		return legacyBlurbSpan{start: line.start, end: end}, true
	}
	return legacyBlurbSpan{}, false
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
	return "\n"
}
