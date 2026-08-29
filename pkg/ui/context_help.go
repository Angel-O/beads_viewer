package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ContextHelpContent contains compact help content for each context.
// This is used when user triggers context-specific help (e.g., double-tap backtick).
// Content should fit on one screen (~20 lines) without scrolling.
var ContextHelpContent = map[Context]string{
	ContextList:            contextHelpList,
	ContextGraph:           contextHelpGraph,
	ContextTree:            contextHelpTree,
	ContextBoard:           contextHelpBoard,
	ContextInsights:        contextHelpInsights,
	ContextFlowMatrix:      contextHelpFlowMatrix,
	ContextHistory:         contextHelpHistory,
	ContextDetail:          contextHelpDetail,
	ContextSplit:           contextHelpSplit,
	ContextFilter:          contextHelpFilter,
	ContextLabelPicker:     contextHelpLabelPicker,
	ContextRepoPicker:      contextHelpRepoPicker,
	ContextTypePicker:      contextHelpTypePicker,
	ContextRecipePicker:    contextHelpRecipePicker,
	ContextHelp:            contextHelpHelp,
	ContextTimeTravelInput: contextHelpTimeTravelInput,
	ContextTimeTravel:      contextHelpTimeTravel,
	ContextLabelDashboard:  contextHelpLabelDashboard,
	ContextAttention:       contextHelpAttention,
	ContextAgentPrompt:     contextHelpAgentPrompt,
	ContextCassSession:     contextHelpCassSession,
}

// GetContextHelp returns the help content for a given context.
// Falls back to generic help if the context has no specific content.
func GetContextHelp(ctx Context) string {
	if content, ok := ContextHelpContent[ctx]; ok {
		return content
	}
	return contextHelpGeneric
}

// RenderContextHelp renders the context-specific help modal.
// This is a compact modal (~60 chars wide) that shows quick reference info.
func RenderContextHelp(ctx Context, theme Theme, width, height int) string {
	content := GetContextHelp(ctx)

	r := theme.Renderer

	// Modal dimensions - compact
	modalWidth := 60
	if modalWidth > width-4 {
		modalWidth = width - 4
	}
	if modalWidth < 0 {
		modalWidth = 0
	}

	// Title
	titleStyle := r.NewStyle().
		Bold(true).
		Foreground(theme.Primary)

	// Content style
	contentStyle := r.NewStyle().
		Foreground(theme.Subtext)

	// Footer hint
	footerStyle := r.NewStyle().
		Foreground(ColorFooterHint).
		Italic(true)

	// Build content
	var b strings.Builder
	b.WriteString(titleStyle.Render("Quick Reference"))
	b.WriteString("\n")
	b.WriteString(r.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", max(modalWidth-4, 0))))
	b.WriteString("\n\n")
	b.WriteString(contentStyle.Render(content))
	b.WriteString("\n\n")
	b.WriteString(footerStyle.Render("Press ` for full tutorial │ Esc to close"))

	// Wrap in modal style
	modalStyle := r.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Secondary).
		Padding(1, 2).
		Width(modalWidth)

	return modalStyle.Render(b.String())
}

type compactNavigationView uint8

const (
	compactNavigationGraph compactNavigationView = iota
	compactNavigationTree
)

type compactNavigationState struct {
	view          compactNavigationView
	searchInput   bool
	searchQuery   bool
	searchSubtree bool
}

func renderCompactNavigationHint(state compactNavigationState) string {
	if state.view == compactNavigationGraph {
		if state.searchInput {
			hint := "type:search • Enter:done • Esc:clear"
			if state.searchQuery {
				hint += " • n/N:match"
			}
			return hint
		}
		hint := "hjkl:nav • PgUp/Dn:page • /:search • Enter:view • g:list"
		if state.searchQuery {
			hint += " • n/N:match"
		}
		return hint
	}

	if state.searchInput {
		return "type:search • Enter:done • Escape:clear"
	}
	if state.searchQuery {
		scopeHint := "minimal • v:subtrees"
		if state.searchSubtree {
			scopeHint = "subtrees • v:minimal"
		}
		return scopeHint + " • n/N:match • Esc:clear"
	}
	return "j/k:move • h/l:fold • o/c/r:filter • +/-:all • /:search • E:list • ?:help"
}

// =============================================================================
// CONTEXT-SPECIFIC HELP CONTENT (bv-4swd)
// =============================================================================

const contextHelpList = `## List View

**Navigation**
  j/k       Move up/down
  Enter     View issue details
  Home/G    Jump to top/bottom

**Filtering**
  o         Open issues only
  c         Closed issues only
  r         Ready (no blockers)
  I         Exact issue-type picker
  /         Fuzzy search
  Ctrl+S    Semantic search (AI)
  H         Hybrid ranking
  Alt+H     Hybrid preset

**Switch Views**
  E         Enter Tree view (uppercase E)
  a         Actionable view
  b         Board view
  g         Graph view
  i         Insights panel
  h         History view

**Actions**
  n         Add comment
  U         Self-update bv
  V         Preview cass sessions`

const contextHelpGraph = `## Graph View

**Navigation**
  j/k       Navigate nodes vertically
  h/l       Navigate siblings
  Enter     View selected issue

**Search**
  /         Search bead ID or title
  Enter     Select first match
  n/N       Next/previous match
  Esc       Cancel/clear, then exit

**Understanding the Graph**
• Arrows point TO what's blocked
  (A → B means A blocks B)
• The metrics panel lists every status glyph
  used by the node list`

const contextHelpTree = `## Tree View

**Navigation**
  j/k       Move up/down
  h/l       Collapse/expand or visit parent/child
  Enter     Toggle expansion; select during search
  Space     Toggle expansion
  gg/G      Jump to top/bottom
  +/-       Expand/collapse all

**Filtering**
  o/c/r     Open/closed/ready status filter

**Understanding the Tree**
• The status legend lists every glyph used by tree rows

**Search**
  /         Search this Tree by ID or title
  n/N       Next/previous match
  Escape    Clear or cancel search first
  Matches retain their hierarchy ancestors

**Exit**
  E / Escape  Exit Tree (uppercase E)`

const contextHelpBoard = `## Board View

**Navigation**
  h/l       Move between columns
  j/k       Move within column
  1-4       Jump to column by number
  H/L       Jump to first/last column
  gg/G      Go to top/bottom of column

**Filtering**
  o/c/r     Filter: open/closed/ready

**Search**
  /         Start search
  n/N       Next/prev match

**Grouping**
  s         Cycle: Status/Priority/Type
**Visual Indicators** (card borders)
  🔴 Red     Has blockers
  🟡 Yellow  High-impact (blocks others)
  🟢 Green   Ready to work

**Actions**
  Tab       Toggle detail panel
	  e         Cycle empty-column mode
  Ctrl+j/k  Scroll detail panel
  V         Preview cass sessions
  y         Copy issue ID
  Enter     View issue details
  Esc       Return to List view`

const contextHelpInsights = `## Insights Panel

**Navigation**
  h/l       Switch between panels
  j/k       Move within panel
  Ctrl+j/k  Scroll detail section
  Tab       Next panel

**Filtering**
  o         Active work (default)
  r         Ready-only; toggle off for active work

**Heatmap** (Priority × Depth grid)
  m         Toggle heatmap view
  Arrows    Navigate cells
  Enter     Drill into cell

**Details**
  e         Toggle explanations
  x         Toggle calculations

**Attention Indicators**
• Stale: Open too long
• Blocked chains: Bottlenecks
• Priority inversions: Low blocking high

	Enter     Open issue or heatmap cell
	] / F4    Attention view
	f         Flow matrix
	Esc       Return to previous view`

const contextHelpFlowMatrix = `## Dependency Flow

**Navigation**
  j/k       Move between labels
  g/G       Jump to first/last label
  Enter     Drill into the selected label

**Understanding Flow**
Flow counts open blocking dependencies
that cross between different labels.
The detail panel shows incoming and
outgoing label totals.

**Exit**
  Esc / q   Return to the issue list (or close drilldown)
  f         Close Flow or its drilldown`

const contextHelpHistory = `## History View

**Navigation**
  j/k       Navigate primary pane
  J/K       Navigate secondary pane
  Tab       Cycle focus (list→detail→files)
  Enter     Jump to selected bead

**View Modes**
  v         Toggle Bead/Git mode
  f         Toggle file tree panel
  /         Search commits/beads
  c         Cycle confidence filter

**Causality Markers**
  🎯 Direct   Commit mentions bead ID
  🔗 Temporal Within time window
  📁 File     Touches associated files

**Actions**
	 y         Copy commit SHA
	 o         Open commit in browser
	 h / Esc / q  Return to list`

const contextHelpDetail = `## Detail View

**Navigation**
	 j/k       Scroll content
	 Esc       Return to previous view

**Switch Views**
	 a/b/g/h/i  Actionable/board/graph/history/insights
	 E          Tree view
	 t/T        Time travel / quick HEAD~5

**Actions**
	 n         Add comment
	 O         Open in editor
	 C         Copy full issue
	 x         Export markdown
	 y         Copy issue ID

**Info Shown**
• Full description (markdown)
• Dependencies
• Labels and metadata`

const contextHelpSplit = `## Split View

Split view is selected automatically when the terminal is
wider than 100 columns.

**Focus**
	 Tab       Switch panes
	 <         Shrink list pane
	 >         Expand list pane

**Left Pane (List)**
  j/k       Navigate issues

**Right Pane (Detail)**
  j/k       Scroll content

**Actions**
	 C         Copy full issue
	 x         Export markdown
	 y         Copy issue ID

Tip: Detail updates as you navigate`

const contextHelpFilter = `## Filter Mode

Type to edit the active List search filter.

**Input**
	 /         Start fuzzy search
	 Type      Add search text
	 Backspace Remove search text
	 Enter     Apply the filter
	 Esc       Cancel the filter`

const contextHelpLabelPicker = `## Label Picker

**Navigation**
  j/k       Move selection
  Enter     Filter List by selected label
  Esc       Cancel

**Search**
  Type      Filter labels by text`

const contextHelpRepoPicker = `## Repository Scope

**Navigation**
  j/k       Move selection
  ↑/↓       Move selection
  Space     Toggle repository
  Enter     Apply scope
  Esc       Cancel

**Actions**
  a         Toggle all / none
  c         Select current repository only
  /         Search name, path, or exact ID

While searching, Esc clears search first.`

const contextHelpTypePicker = `## Issue Type Filter

**Navigation**
  j/k       Move selection
  Space     Toggle exact issue type
  Enter     Apply selection
  Esc       Cancel

**Actions**
  a         Toggle all / none

Composes with status, labels, repositories, recipes,
and text search.`

const contextHelpRecipePicker = `## Recipe Picker

**Navigation**
  j/k       Move selection
  Enter     Apply recipe
  Esc       Cancel

**Recipes**
Pre-configured filters and sorts:
• Sprint Ready
• Blocked Items
• By Priority
• Recently Updated`

const contextHelpHelp = `## Help Overlay

You're looking at the help overlay!

**Navigation**
	 j/k       Scroll help content
	 Space     Open full tutorial
	 Esc/?/q   Close this overlay
	 Other keys  Dismiss and return

**Other Help**
  ` + "`" + `         Full tutorial (any time)`

const contextHelpTimeTravel = `## Time Travel Mode

**Currently Viewing**: Past state

This is read-only - you're viewing
how the project looked at a specific
point in history.

**Navigation**
  j/k       Navigate issues
  Enter     View issue detail

**Exit**
  t / T     Return to present

Tip: Use History view (h) to pick
different points in time`

const contextHelpTimeTravelInput = `## Time Travel Input

Enter a git revision to compare with the current state.

**Input**
	 Type      Enter a revision
	 Enter     Compare with that revision
	 Esc       Cancel

Empty input uses HEAD~5.`

const contextHelpLabelDashboard = `## Label Dashboard

**Overview**
Shows all labels with:
• Issue counts per label
• Health indicators
• Usage trends

**Navigation**
  j/k       Move selection
  Enter     Filter List by selected label
  h         View label health
  d         Open label issue drilldown
  [ / F3    Close dashboard
  Esc       Return to list
`

const contextHelpAttention = `## Attention View

**Ranked Labels**

Labels are sorted by attention score based on:
• Dependency centrality (PageRank contribution)
• Stale-to-open issue ratio
• Downstream block impact
• Recent closure velocity (lower increases attention)

**Actions**
  1-9       Filter List by ranked label
            (filter persists across views)
  ] / F4    Toggle Attention closed
  Esc / q   Back to the previous view`

const contextHelpAgentPrompt = `## Agent Instructions Prompt

Offers to add beads_viewer instructions
to AGENTS.md or CLAUDE.md.

**Navigation**
  h/l       Move between choices
  Tab       Move to next choice
  Enter     Confirm selected choice
  Space     Confirm selected choice

**Direct Choices**
  y         Add instructions
  n         Decline
  d         Don't ask again
  Esc / q   Decline`

const contextHelpGeneric = `## Quick Reference

**Global Keys**
  ?         Help overlay
  ` + "`" + `         Full tutorial
  Esc       Close/back
  q         Quit

**Navigation**
  j/k       Move up/down
  h/l       Contextual left/right navigation
  Enter     Select/open

**Views**
	 b/g/i/h   Switch views from List/Detail
	 ;         Shortcuts sidebar`

const contextHelpCassSession = `## Cass Session Preview

Shows coding sessions correlated with
the selected bead via cass search.

**Navigation**
  j/k       Move between sessions
  Enter     Close modal
  Esc/V/q   Close modal

**Actions**
  y         Copy cass search command

**Match Types**
  ID        Direct bead ID match
  File      Modified same files
  Title     Keyword similarity

Sessions ranked by relevance score.
Only shown when cass is installed.`
