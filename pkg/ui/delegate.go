package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// IssueDelegate renders issue items in the list
type IssueDelegate struct {
	Theme                Theme
	ShowPriorityHints    bool
	PriorityHints        map[string]*analysis.PriorityRecommendation
	WorkspaceMode        bool // When true, shows repo prefix badges
	ShowRepositories     bool // When true, shows Hub repository badges
	RepositoryNameWidth  int  // Shared Hub repository name column width in cells
	RepositoryExtraWidth int  // Shared Hub +N sub-column width in cells
	ShowSearchScores     bool // Show semantic/hybrid score badge when search is active
	triageSlotWidth      int  // Shared visual width for the current list layout
	columns              *issueListColumns
}

const issueTypeIconSlotWidth = 2

type issueListColumns struct {
	width int

	repoWidth     int
	typeWidth     int
	priorityWidth int
	triageWidth   int
	statusWidth   int
	searchWidth   int
	idWidth       int
	diffWidth     int
	commentsWidth int
	assigneeWidth int
	labelsWidth   int
	rightWidth    int

	repoStart     int
	typeStart     int
	priorityStart int
	hintsStart    int
	triageStart   int
	statusStart   int
	searchStart   int
	idStart       int
	diffStart     int
	titleStart    int

	ageStart      int
	commentsStart int
	graphStart    int
	assigneeStart int
	labelsStart   int
	showRepo      bool
	showHints     bool
	showTriage    bool
	showSearch    bool
	showDiff      bool
	showAge       bool
	showComments  bool
	showGraph     bool
	showAssignee  bool
	showLabels    bool
	rightParts    []issueListRightPart
}

// issueListColumns is the single list-level layout contract. It is computed
// from all visible items, then consumed by both Render and the list header.
func (d IssueDelegate) issueListColumnsFor(items []list.Item, listWidth int) *issueListColumns {
	if listWidth <= 0 {
		listWidth = 80
	}
	rowWidth := max(listWidth-1, 1)
	columns := &issueListColumns{width: rowWidth, typeWidth: issueTypeIconSlotWidth}
	issueItems := make([]IssueItem, 0, len(items))
	for _, item := range items {
		i, ok := item.(IssueItem)
		if !ok {
			continue
		}
		issueItems = append(issueItems, i)
		icon, _ := d.Theme.GetTypeIcon(string(i.Issue.IssueType))
		columns.typeWidth = max(columns.typeWidth, lipgloss.Width(icon))
		columns.priorityWidth = max(columns.priorityWidth, lipgloss.Width(RenderPriorityBadge(i.Issue.Priority)))
		columns.statusWidth = max(columns.statusWidth, lipgloss.Width(RenderStatusBadge(string(i.Issue.Status))))
		if i.DiffStatus.Badge() != "" {
			columns.showDiff = true
			columns.diffWidth = max(columns.diffWidth, lipgloss.Width(i.DiffStatus.Badge()))
		}
		if d.ShowSearchScores && i.SearchScoreSet {
			columns.showSearch = true
			columns.searchWidth = max(columns.searchWidth, lipgloss.Width(fmt.Sprintf("[%.2f]", i.SearchScore)))
		}
		if i.Issue.Assignee != "" {
			columns.showAssignee = true
		}
		labels := i.Issue.Labels
		if i.HubPresentation {
			labels = i.PresentationLabels
		}
		if len(labels) > 0 {
			columns.showLabels = true
		}
	}
	columns.priorityWidth = max(columns.priorityWidth, 1)
	columns.statusWidth = max(columns.statusWidth, 1)
	columns.showHints = d.ShowPriorityHints && d.PriorityHints != nil
	columns.showTriage = d.triageSlotWidth > 0
	columns.showRepo = d.ShowRepositories && d.RepositoryNameWidth > 0 && rowWidth > 45
	if !columns.showRepo && d.WorkspaceMode {
		for _, i := range issueItems {
			if i.RepoPrefix != "" {
				columns.showRepo = true
				break
			}
		}
	}
	if columns.showRepo {
		if d.ShowRepositories && d.RepositoryNameWidth > 0 && rowWidth > 45 {
			columns.repoWidth = d.RepositoryNameWidth + 2 + d.RepositoryExtraWidth
		} else {
			for _, i := range issueItems {
				columns.repoWidth = max(columns.repoWidth, lipgloss.Width(RenderRepoBadge(i.RepoPrefix)))
			}
		}
	}

	columns.showAge = rowWidth > 60
	columns.showComments = rowWidth > 60
	columns.showGraph = rowWidth > 120
	if columns.showAssignee && rowWidth > 100 {
		columns.assigneeWidth = 13
	} else {
		columns.showAssignee = false
	}
	if !columns.showLabels || rowWidth <= 140 {
		columns.showLabels = false
	}
	columns.commentsWidth = 3
	columns.labelsWidth = 0
	for _, i := range issueItems {
		if columns.showComments && len(i.Issue.Comments) > 0 {
			columns.commentsWidth = max(columns.commentsWidth, lipgloss.Width(fmt.Sprintf("💬%d", len(i.Issue.Comments))))
		}
		if columns.showLabels {
			labels := i.Issue.Labels
			if i.HubPresentation {
				labels = i.PresentationLabels
			}
			if len(labels) > 0 {
				labelStr := truncateRunesHelper(strings.Join(labels, ","), 20, "…")
				labelStyle := d.Theme.Renderer.NewStyle().Padding(0, 1)
				columns.labelsWidth = max(columns.labelsWidth, lipgloss.Width(labelStyle.Render(labelStr)))
			}
		}
	}

	cursor := 2 // selection indicator
	if columns.showRepo {
		columns.repoStart = cursor
		cursor += columns.repoWidth + 1
	}
	columns.typeStart = cursor
	cursor += columns.typeWidth + 1
	columns.priorityStart = cursor
	cursor += columns.priorityWidth + 1
	if columns.showHints {
		columns.hintsStart = cursor
		cursor += 2
	}
	if columns.showTriage {
		columns.triageStart = cursor
		cursor += d.triageSlotWidth + 1
	}
	columns.statusStart = cursor
	cursor += columns.statusWidth + 1
	if columns.showSearch {
		columns.searchStart = cursor
		cursor += columns.searchWidth + 1
	}
	columns.idStart = cursor
	for _, i := range issueItems {
		columns.idWidth = max(columns.idWidth, min(lipgloss.Width(i.Issue.ID), 35))
	}
	columns.idWidth = max(columns.idWidth, 1)
	columns.triageWidth = d.triageSlotWidth
	columns.rightParts = d.issueListRightParts(rowWidth, columns)
	columns.rightWidth = issueListRightWidth(columns.rightParts)
	diffReserve := 0
	if columns.showDiff {
		diffReserve = columns.diffWidth + 1
	}
	maxIDWidth := rowWidth - cursor - columns.rightWidth - diffReserve - 2
	if maxIDWidth < columns.idWidth {
		columns.idWidth = max(maxIDWidth, 1)
	}
	cursor += columns.idWidth + 1
	if columns.showDiff {
		columns.diffStart = cursor
		cursor += columns.diffWidth + 1
	}
	columns.titleStart = cursor
	for index := range columns.rightParts {
		part := &columns.rightParts[index]
		switch part.kind {
		case "age":
			columns.ageStart = part.start
		case "comments":
			columns.commentsStart = part.start
		case "graph":
			columns.graphStart = part.start
		case "assignee":
			columns.assigneeStart = part.start
		case "labels":
			columns.labelsStart = part.start + 1
		}
	}
	return columns
}

type issueListRightPart struct {
	kind    string
	start   int
	width   int
	visible bool
}

func (d IssueDelegate) issueListRightParts(rowWidth int, columns *issueListColumns) []issueListRightPart {
	parts := make([]issueListRightPart, 0, 5)
	if rowWidth > 60 {
		parts = append(parts,
			issueListRightPart{kind: "age", width: 8, visible: true},
			issueListRightPart{kind: "comments", width: columns.commentsWidth, visible: true},
		)
	}
	if rowWidth > 120 {
		parts = append(parts, issueListRightPart{kind: "graph", width: 5, visible: true})
	}
	if columns.showAssignee {
		parts = append(parts, issueListRightPart{kind: "assignee", width: columns.assigneeWidth, visible: true})
	}
	if columns.showLabels {
		parts = append(parts, issueListRightPart{kind: "labels", width: columns.labelsWidth, visible: true})
	}
	width := issueListRightWidth(parts)
	start := rowWidth - width
	for index := range parts {
		parts[index].start = start
		start += parts[index].width
		if index < len(parts)-1 {
			start++
		}
	}
	return parts
}

func issueListRightWidth(parts []issueListRightPart) int {
	width := 0
	for index := range parts {
		width += parts[index].width
		if index < len(parts)-1 {
			width++
		}
	}
	return width
}

func (d IssueDelegate) columnsFor(m list.Model) *issueListColumns {
	width := m.Width()
	if width <= 0 {
		width = 80
	}
	if d.columns != nil && d.columns.width == max(width-1, 1) {
		return d.columns
	}
	return d.issueListColumnsFor(m.VisibleItems(), width)
}

func (d IssueDelegate) renderRightParts(i IssueItem, t Theme, columns *issueListColumns) []string {
	parts := make([]string, 0, len(columns.rightParts))
	for _, part := range columns.rightParts {
		var value string
		switch part.kind {
		case "age":
			age := truncateRunesHelper(FormatTimeRel(i.Issue.CreatedAt), part.width, "…")
			value = t.MutedText.Render(padRight(age, part.width))
		case "comments":
			if len(i.Issue.Comments) > 0 {
				value = t.InfoText.Render(fmt.Sprintf("💬%d", len(i.Issue.Comments)))
			} else {
				value = strings.Repeat(" ", part.width)
			}
		case "graph":
			spark := RenderSparkline(i.GraphScore, 5)
			value = t.Renderer.NewStyle().Foreground(GetHeatmapColor(i.GraphScore, t)).Render(spark)
		case "assignee":
			if i.Issue.Assignee == "" {
				value = strings.Repeat(" ", part.width)
			} else {
				assignee := truncateRunesHelper(i.Issue.Assignee, 12, "…")
				value = t.SecondaryText.Render("@" + padRight(assignee, 12))
			}
		case "labels":
			labels := i.Issue.Labels
			if i.HubPresentation {
				labels = i.PresentationLabels
			}
			if len(labels) > 0 {
				labelStyle := t.Renderer.NewStyle().
					Foreground(ColorPrimary).
					Background(ColorBgSubtle).
					Padding(0, 1)
				value = labelStyle.Render(truncateRunesHelper(strings.Join(labels, ","), 20, "…"))
			} else {
				value = strings.Repeat(" ", part.width)
			}
		}
		valueWidth := lipgloss.Width(value)
		if valueWidth < part.width {
			value += strings.Repeat(" ", part.width-valueWidth)
		}
		parts = append(parts, value)
	}
	return parts
}

func renderIssueListHeader(columns *issueListColumns) string {
	line := []rune(strings.Repeat(" ", columns.width))
	place := func(offset int, labels ...string) {
		for _, label := range labels {
			if offset < 0 || offset+len([]rune(label)) > len(line) {
				continue
			}
			fits := true
			for index := range label {
				if line[offset+index] != ' ' {
					fits = false
					break
				}
			}
			if fits {
				copy(line[offset:], []rune(label))
				return
			}
		}
	}

	if columns.showRepo {
		place(columns.repoStart, "CTX", "C")
	}
	place(columns.typeStart, "TY")
	place(columns.priorityStart, "PR")
	if columns.showHints {
		place(columns.hintsStart, "H")
	}
	if columns.showTriage {
		place(columns.triageStart, "TR")
	}
	place(columns.statusStart, "STAT")
	if columns.showSearch {
		place(columns.searchStart, "SCORE")
	}
	place(columns.idStart, "ID")
	if columns.showDiff {
		place(columns.diffStart, "DF")
	}
	if columns.showAge {
		place(columns.ageStart, "AGE")
	}
	if columns.showComments {
		place(columns.commentsStart, "CMT")
	}
	if columns.showGraph {
		place(columns.graphStart, "GRAPH")
	}
	if columns.showAssignee {
		place(columns.assigneeStart, "ASGN")
	}
	if columns.showLabels {
		place(columns.labelsStart, "LBL")
	}
	rightStart := columns.width - columns.rightWidth
	for _, label := range []string{"TITLE", "T"} {
		labelWidth := lipgloss.Width(label)
		if columns.rightWidth > 0 && columns.titleStart+labelWidth+1 > rightStart {
			continue
		}
		place(columns.titleStart, label)
		break
	}
	return string(line)
}

func triageIndicatorText(i IssueItem) string {
	if i.IsQuickWin {
		return "⭐"
	}
	if i.IsBlocker && i.UnblocksCount > 0 {
		return fmt.Sprintf("🔓%d", i.UnblocksCount)
	}
	if i.UnblocksCount > 0 {
		return fmt.Sprintf("↪%d", i.UnblocksCount)
	}
	return ""
}

func triageIndicatorContentWidth(items []list.Item) int {
	width := 0
	for _, item := range items {
		i, ok := item.(IssueItem)
		if !ok {
			continue
		}
		width = max(width, lipgloss.Width(triageIndicatorText(i)))
	}
	return width
}

func (d IssueDelegate) triageSlotWidthFor(items []list.Item, width int) int {
	contentWidth := triageIndicatorContentWidth(items)
	if contentWidth == 0 {
		return 0
	}

	if width <= 0 {
		width = 80
	}
	rowWidth := width - 1 // Render's edge-wrap guard
	maxReserve := 0
	for _, item := range items {
		i, ok := item.(IssueItem)
		if !ok {
			continue
		}
		maxReserve = max(maxReserve, d.triagePrefixWidth(i, rowWidth)+d.triageRightWidth(i, rowWidth))
	}

	// Leave room for the shared slot separator, minimum ID/title cells, the ID
	// separator, and the two existing trailing layout cells.
	maxSlotWidth := rowWidth - maxReserve - 6
	return min(contentWidth, max(maxSlotWidth, 0))
}

func (d IssueDelegate) triagePrefixWidth(i IssueItem, width int) int {
	icon, _ := d.Theme.GetTypeIcon(string(i.Issue.IssueType))
	leftWidth := 2 + max(issueTypeIconSlotWidth, lipgloss.Width(icon)) + 1

	if d.ShowRepositories && d.RepositoryNameWidth > 0 && width > 45 {
		leftWidth += d.RepositoryNameWidth + 2 + d.RepositoryExtraWidth + 1
	} else if d.WorkspaceMode && i.RepoPrefix != "" {
		leftWidth += lipgloss.Width(RenderRepoBadge(i.RepoPrefix)) + 1
	}

	leftWidth += lipgloss.Width(RenderPriorityBadge(i.Issue.Priority)) + 1
	if d.ShowPriorityHints {
		leftWidth += 2
	}
	leftWidth += lipgloss.Width(RenderStatusBadge(string(i.Issue.Status))) + 1
	if d.ShowSearchScores && i.SearchScoreSet {
		leftWidth += lipgloss.Width(fmt.Sprintf("[%.2f]", i.SearchScore)) + 1
	}
	if badge := i.DiffStatus.Badge(); badge != "" {
		leftWidth += lipgloss.Width(badge) + 1
	}
	return leftWidth
}

func (d IssueDelegate) triageRightWidth(i IssueItem, width int) int {
	rightWidth := 0
	if width > 60 {
		rightWidth += 9
		if len(i.Issue.Comments) > 0 {
			rightWidth += lipgloss.Width(fmt.Sprintf("💬%d", len(i.Issue.Comments))) + 1
		} else {
			rightWidth += 3
		}
	}
	if width > 120 {
		rightWidth += 6
	}
	if width > 100 && i.Issue.Assignee != "" {
		rightWidth += 14
	}
	labels := i.Issue.Labels
	if i.HubPresentation {
		labels = i.PresentationLabels
	}
	if width > 140 && len(labels) > 0 {
		labelStr := truncateRunesHelper(strings.Join(labels, ","), 20, "…")
		labelStyle := d.Theme.Renderer.NewStyle().
			Foreground(ColorPrimary).
			Background(ColorBgSubtle).
			Padding(0, 1)
		rightWidth += lipgloss.Width(labelStyle.Render(labelStr)) + 1
	}
	return rightWidth
}

func (d IssueDelegate) Height() int {
	return 1
}

func (d IssueDelegate) Spacing() int {
	return 0
}

func (d IssueDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d IssueDelegate) rowWidthWithoutRepository(i IssueItem, width int) int {
	icon, _ := d.Theme.GetTypeIcon(string(i.Issue.IssueType))
	leftWidth := 2 + max(issueTypeIconSlotWidth, lipgloss.Width(icon)) + 1
	leftWidth += lipgloss.Width(RenderPriorityBadge(i.Issue.Priority)) + 1
	if d.ShowPriorityHints {
		leftWidth += 2
	}
	if d.triageSlotWidth > 0 {
		leftWidth += d.triageSlotWidth + 1
	} else if triage := triageIndicatorText(i); triage != "" {
		leftWidth += lipgloss.Width(triage) + 1
	}
	leftWidth += lipgloss.Width(RenderStatusBadge(string(i.Issue.Status))) + 1
	if d.ShowSearchScores && i.SearchScoreSet {
		leftWidth += lipgloss.Width(fmt.Sprintf("[%.2f]", i.SearchScore)) + 1
	}
	leftWidth += min(lipgloss.Width(i.Issue.ID), 35) + 1
	if badge := i.DiffStatus.Badge(); badge != "" {
		leftWidth += lipgloss.Width(badge) + 1
	}

	rightWidth := 0
	if width > 60 {
		rightWidth += 9
		if len(i.Issue.Comments) > 0 {
			rightWidth += lipgloss.Width(fmt.Sprintf("💬%d", len(i.Issue.Comments))) + 1
		} else {
			rightWidth += 3
		}
	}
	if width > 120 {
		rightWidth += 6
	}
	if width > 100 && i.Issue.Assignee != "" {
		rightWidth += 14
	}
	labels := i.Issue.Labels
	if i.HubPresentation {
		labels = i.PresentationLabels
	}
	if width > 140 && len(labels) > 0 {
		labelStr := truncateRunesHelper(strings.Join(labels, ","), 20, "…")
		labelStyle := d.Theme.Renderer.NewStyle().Padding(0, 1)
		rightWidth += lipgloss.Width(labelStyle.Render(labelStr)) + 1
	}
	return leftWidth + rightWidth + 2 + 5
}

func (d IssueDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(IssueItem)
	if !ok {
		return
	}

	t := d.Theme
	width := m.Width()
	if width <= 0 {
		width = 80
	}
	// Reduce width by 1 to prevent terminal wrapping on the exact edge
	width = width - 1
	columns := d.columnsFor(m)

	isSelected := index == m.Index()

	// ══════════════════════════════════════════════════════════════════════════
	// POLISHED ROW LAYOUT - Stripe-level visual hierarchy
	// Layout: [sel] [type] [prio-badge] [status-badge] [ID] [title...] [meta]
	// ══════════════════════════════════════════════════════════════════════════

	// Get all the data
	icon, iconColor := t.GetTypeIcon(string(i.Issue.IssueType))
	idStr := i.Issue.ID
	title := i.Issue.Title

	// Measure actual icon display width (emojis vary: 1-2 cells)
	iconDisplayWidth := lipgloss.Width(icon)
	typeIconSlotWidth := columns.typeWidth
	triageText := triageIndicatorText(i)
	triageSlotWidth := columns.triageWidth

	// Render every list-level right-side slot. Empty item values remain padded
	// so later metadata columns cannot shift between rows.
	rightParts := d.renderRightParts(i, t, columns)
	rightWidth := columns.rightWidth

	// Left side fixed columns with polished badges. Every optional slot is
	// reserved from the list-level contract, including empty item values.
	leftFixedWidth := columns.titleStart

	var repoBadge string
	if columns.showRepo && d.ShowRepositories {
		extra := ""
		if i.RepositoryExtra > 0 {
			extra = fmt.Sprintf("+%d", i.RepositoryExtra)
		}
		if i.RepositoryID == "" {
			repoBadge = strings.Repeat(" ", d.RepositoryNameWidth+2)
		} else if lipgloss.Width(i.RepositoryName) <= d.RepositoryNameWidth {
			repoBadge = RenderRepositoryBadge(i.RepositoryID, i.RepositoryName)
		} else {
			repoBadge = RenderRepositoryBadgeCompact(i.RepositoryID, i.RepositoryName, d.RepositoryNameWidth)
		}
		repoBadge += strings.Repeat(" ", max(d.RepositoryNameWidth+2-lipgloss.Width(repoBadge), 0))
		repoBadge += padRight(extra, d.RepositoryExtraWidth)
	} else if columns.showRepo {
		repoBadge = RenderRepoBadge(i.RepoPrefix)
	}
	if columns.showRepo {
		repoBadge += strings.Repeat(" ", max(columns.repoWidth-lipgloss.Width(repoBadge), 0))
	}

	// Priority badge (polished)
	prioBadge := RenderPriorityBadge(i.Issue.Priority)
	statusBadge := RenderStatusBadge(string(i.Issue.Status))
	diffBadge := i.DiffStatus.Badge()
	if lipgloss.Width(idStr) > columns.idWidth {
		idStr = truncateRunesHelper(idStr, columns.idWidth, "…")
	}

	// Keep one blank display cell before right-side metadata. If the fixed
	// columns consume the available width, omit the title rather than letting
	// it run into AGE or another right-side column.
	titleWidth := width - leftFixedWidth - rightWidth - 1
	if titleWidth < 0 {
		titleWidth = 0
	}

	// Truncate title if needed
	if titleWidth > 0 {
		title = truncateRunesHelper(title, titleWidth, "…")
	} else {
		title = ""
	}

	// Pad title to fill space
	currentWidth := lipgloss.Width(title)
	if currentWidth < titleWidth {
		title = title + strings.Repeat(" ", titleWidth-currentWidth)
	}

	// ══════════════════════════════════════════════════════════════════════════
	// BUILD THE ROW
	// ══════════════════════════════════════════════════════════════════════════
	var leftSide strings.Builder

	// Selection indicator with accent color (using pre-computed style)
	if isSelected {
		leftSide.WriteString(t.PrimaryBold.Render("▸ "))
	} else {
		leftSide.WriteString("  ")
	}

	// Repository cell (local workspace or Hub context presentation).
	if columns.showRepo {
		leftSide.WriteString(repoBadge)
		leftSide.WriteString(strings.Repeat(" ", max(columns.repoWidth-lipgloss.Width(repoBadge), 0)))
		leftSide.WriteString(" ")
	}

	// Type icon with color
	leftSide.WriteString(t.Renderer.NewStyle().Foreground(iconColor).Render(icon))
	leftSide.WriteString(strings.Repeat(" ", typeIconSlotWidth-iconDisplayWidth))
	leftSide.WriteString(" ")

	// Priority badge (polished)
	leftSide.WriteString(prioBadge)
	leftSide.WriteString(strings.Repeat(" ", columns.priorityWidth-lipgloss.Width(prioBadge)))
	leftSide.WriteString(" ")

	// Priority hint indicator (↑/↓) - using pre-computed styles
	if columns.showHints {
		if hint, ok := d.PriorityHints[i.Issue.ID]; ok {
			if hint.Direction == "increase" {
				leftSide.WriteString(t.PriorityUpArrow.Render("↑"))
			} else if hint.Direction == "decrease" {
				leftSide.WriteString(t.PriorityDownArrow.Render("↓"))
			}
		} else {
			leftSide.WriteString(" ")
		}
		leftSide.WriteString(" ")
	}

	// Triage indicators (bv-151): Quick win ⭐ and Unblocks count 🔓 - using pre-computed styles
	if triageSlotWidth > 0 {
		triageDisplayText := truncateRunesHelper(triageText, triageSlotWidth, "…")
		triageIndicator := triageDisplayText
		switch {
		case i.IsQuickWin:
			triageIndicator = t.TriageStar.Render(triageDisplayText)
		case i.IsBlocker && i.UnblocksCount > 0:
			triageIndicator = t.TriageUnblocks.Render(triageDisplayText)
		case i.UnblocksCount > 0:
			triageIndicator = t.TriageUnblocksAlt.Render(triageDisplayText)
		}
		leftSide.WriteString(triageIndicator)
		leftSide.WriteString(strings.Repeat(" ", triageSlotWidth-lipgloss.Width(triageDisplayText)))
		leftSide.WriteString(" ")
	}

	// Status badge (polished)
	leftSide.WriteString(statusBadge)
	leftSide.WriteString(strings.Repeat(" ", columns.statusWidth-lipgloss.Width(statusBadge)))
	leftSide.WriteString(" ")

	// Search score badge (optional)
	if columns.showSearch {
		if i.SearchScoreSet {
			searchBadge := t.InfoBold.Render(fmt.Sprintf("[%.2f]", i.SearchScore))
			leftSide.WriteString(searchBadge)
			leftSide.WriteString(strings.Repeat(" ", max(columns.searchWidth-lipgloss.Width(searchBadge), 0)))
		} else {
			leftSide.WriteString(strings.Repeat(" ", columns.searchWidth))
		}
		leftSide.WriteString(" ")
	}

	// ID with secondary styling (using pre-computed style base)
	idStyle := t.SecondaryText
	if isSelected {
		idStyle = idStyle.Bold(true)
	}
	leftSide.WriteString(idStyle.Render(idStr))
	leftSide.WriteString(strings.Repeat(" ", max(columns.idWidth-lipgloss.Width(idStr), 0)))
	leftSide.WriteString(" ")

	// Diff badge (time-travel mode)
	if columns.showDiff {
		leftSide.WriteString(diffBadge)
		leftSide.WriteString(strings.Repeat(" ", max(columns.diffWidth-lipgloss.Width(diffBadge), 0)))
		leftSide.WriteString(" ")
	}

	// Title with emphasis when selected
	titleStyle := t.Renderer.NewStyle()
	if isSelected {
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else {
		titleStyle = titleStyle.Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E8E8E8"})
	}
	if titleWidth > 0 {
		leftSide.WriteString(titleStyle.Render(title))
	}

	// Right side
	rightSide := strings.Join(rightParts, " ")

	// Combine: left + padding + right
	leftLen := lipgloss.Width(leftSide.String())
	rightLen := lipgloss.Width(rightSide)
	padding := width - leftLen - rightLen
	if padding < 0 {
		padding = 0
	}

	// Construct the row string
	row := leftSide.String() + strings.Repeat(" ", padding) + rightSide

	// Apply row background for selection and clamp width
	rowStyle := t.Renderer.NewStyle().Inline(true).Width(width).MaxWidth(width)
	if isSelected {
		row = rowStyle.Background(t.Highlight).Render(row)
	} else {
		row = rowStyle.Render(row)
	}

	fmt.Fprint(w, row)
}
