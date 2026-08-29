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
}

const issueTypeIconSlotWidth = 2

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

	isSelected := index == m.Index()

	// ══════════════════════════════════════════════════════════════════════════
	// POLISHED ROW LAYOUT - Stripe-level visual hierarchy
	// Layout: [sel] [type] [prio-badge] [status-badge] [ID] [title...] [meta]
	// ══════════════════════════════════════════════════════════════════════════

	// Get all the data
	icon, iconColor := t.GetTypeIcon(string(i.Issue.IssueType))
	idStr := i.Issue.ID
	title := i.Issue.Title
	ageStr := FormatTimeRel(i.Issue.CreatedAt)
	commentCount := len(i.Issue.Comments)

	// Measure actual icon display width (emojis vary: 1-2 cells)
	iconDisplayWidth := lipgloss.Width(icon)
	typeIconSlotWidth := max(issueTypeIconSlotWidth, iconDisplayWidth)
	triageText := triageIndicatorText(i)
	triageSlotWidth := d.triageSlotWidth
	idStrWidth := lipgloss.Width(idStr)
	if idStrWidth > 35 {
		idStrWidth = 35
		idStr = truncateRunesHelper(idStr, 35, "…")
	}

	// Calculate widths for right-side columns (fixed)
	rightWidth := 0
	var rightParts []string

	// Show Age and Comments only if we have reasonable width
	if width > 60 {
		// Age - with subtle styling (using pre-computed style)
		rightParts = append(rightParts, t.MutedText.Render(fmt.Sprintf("%8s", ageStr)))
		rightWidth += 9

		// Comments with icon - use lipgloss.Width for accurate emoji measurement
		if commentCount > 0 {
			commentStr := fmt.Sprintf("💬%d", commentCount)
			rightParts = append(rightParts, t.InfoText.Render(commentStr))
			rightWidth += lipgloss.Width(commentStr) + 1 // +1 for spacing
		} else {
			rightParts = append(rightParts, "   ")
			rightWidth += 3
		}
	}

	// Sparkline (Graph Score) - visualization of importance
	if width > 120 {
		spark := RenderSparkline(i.GraphScore, 5)
		sparkColor := GetHeatmapColor(i.GraphScore, t)
		sparkStyle := t.Renderer.NewStyle().Foreground(sparkColor)
		rightParts = append(rightParts, sparkStyle.Render(spark))
		rightWidth += 6 // 5 + 1 spacing
	}

	// Assignee (if present and we have room)
	if width > 100 && i.Issue.Assignee != "" {
		assignee := truncateRunesHelper(i.Issue.Assignee, 12, "…")
		rightParts = append(rightParts, t.SecondaryText.Render("@"+padRight(assignee, 12)))
		rightWidth += 14
	}

	// Labels (if present and we have room) - render as mini tags
	labels := i.Issue.Labels
	if i.HubPresentation {
		labels = i.PresentationLabels
	}
	if width > 140 && len(labels) > 0 {
		labelStr := truncateRunesHelper(strings.Join(labels, ","), 20, "…")
		labelStyle := t.Renderer.NewStyle().
			Foreground(ColorPrimary).
			Background(ColorBgSubtle).
			Padding(0, 1)
		rightParts = append(rightParts, labelStyle.Render(labelStr))
		rightWidth += lipgloss.Width(labelStyle.Render(labelStr)) + 1
	}

	// Left side fixed columns with polished badges
	// [selector 2] [repo-badge 0-6] [icon 1-2] [prio-badge 3] [hint 1-2] [status-badge 6] [id dynamic] [space]
	// Use measured iconDisplayWidth instead of hardcoded value for proper alignment
	leftFixedWidth := 2 + typeIconSlotWidth + 1 // selector(2) + icon slot + space(1)

	// Repo badge width (workspace mode)
	var repoBadge string
	if d.ShowRepositories && d.RepositoryNameWidth > 0 && width > 45 {
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
		leftFixedWidth += lipgloss.Width(repoBadge) + 1
	} else if d.WorkspaceMode && i.RepoPrefix != "" {
		// Create a compact repo badge like [API] or [WEB]
		repoBadge = RenderRepoBadge(i.RepoPrefix)
		leftFixedWidth += lipgloss.Width(repoBadge) + 1
	}

	// Priority badge (polished)
	prioBadge := RenderPriorityBadge(i.Issue.Priority)
	prioBadgeWidth := lipgloss.Width(prioBadge)
	leftFixedWidth += prioBadgeWidth + 1

	// Priority hint indicator
	if d.ShowPriorityHints {
		leftFixedWidth += 2
	}

	// Status badge (polished)
	statusBadge := RenderStatusBadge(string(i.Issue.Status))
	statusBadgeWidth := lipgloss.Width(statusBadge)
	leftFixedWidth += statusBadgeWidth + 1

	// Search score badge (semantic/hybrid)
	var searchBadge string
	if d.ShowSearchScores && i.SearchScoreSet {
		scoreStr := fmt.Sprintf("%.2f", i.SearchScore)
		searchBadge = t.InfoBold.Render(fmt.Sprintf("[%s]", scoreStr))
		leftFixedWidth += lipgloss.Width(searchBadge) + 1
	}

	diffBadge := i.DiffStatus.Badge()
	if diffBadge != "" {
		leftFixedWidth += lipgloss.Width(diffBadge) + 1
	}

	// Reserve the shared visual-width slot for triage indicators. It is sized
	// from the list layout before rendering, so every row uses the same width.
	if triageSlotWidth > 0 {
		leftFixedWidth += triageSlotWidth + 1
	}

	// ID width - use actual visual width, but cap reasonably
	idWidth := idStrWidth

	// Keep the fixed columns within the row when a narrow pane has a long issue
	// ID. The title can shrink to a single cell before the ID is truncated.
	fixedWithoutID := leftFixedWidth
	// Leave room for the ID separator and the title's existing trailing layout
	// cells before allowing the ID to consume the remaining width.
	maxIDWidth := width - fixedWithoutID - rightWidth - 2
	if maxIDWidth < idWidth {
		idWidth = maxIDWidth
		if idWidth < 1 {
			idWidth = 1
		}
		idStr = truncateRunesHelper(idStr, idWidth, "…")
	}
	leftFixedWidth = fixedWithoutID + idWidth + 1

	// Title gets everything in between
	titleWidth := width - leftFixedWidth - rightWidth - 2
	if titleWidth < 1 {
		titleWidth = 1
	}

	// Truncate title if needed
	title = truncateRunesHelper(title, titleWidth, "…")

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

	// Repo badge (workspace mode)
	if repoBadge != "" {
		leftSide.WriteString(repoBadge)
		leftSide.WriteString(" ")
	}

	// Type icon with color
	leftSide.WriteString(t.Renderer.NewStyle().Foreground(iconColor).Render(icon))
	leftSide.WriteString(strings.Repeat(" ", typeIconSlotWidth-iconDisplayWidth))
	leftSide.WriteString(" ")

	// Priority badge (polished)
	leftSide.WriteString(prioBadge)
	leftSide.WriteString(" ")

	// Priority hint indicator (↑/↓) - using pre-computed styles
	if d.ShowPriorityHints && d.PriorityHints != nil {
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
	leftSide.WriteString(" ")

	// Search score badge (optional)
	if searchBadge != "" {
		leftSide.WriteString(searchBadge)
		leftSide.WriteString(" ")
	}

	// ID with secondary styling (using pre-computed style base)
	idStyle := t.SecondaryText
	if isSelected {
		idStyle = idStyle.Bold(true)
	}
	leftSide.WriteString(idStyle.Render(idStr))
	leftSide.WriteString(" ")

	// Diff badge (time-travel mode)
	if diffBadge != "" {
		leftSide.WriteString(diffBadge)
		leftSide.WriteString(" ")
	}

	// Title with emphasis when selected
	titleStyle := t.Renderer.NewStyle()
	if isSelected {
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else {
		titleStyle = titleStyle.Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E8E8E8"})
	}
	leftSide.WriteString(titleStyle.Render(title))

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
	rowStyle := t.Renderer.NewStyle().Width(width).MaxWidth(width)
	if isSelected {
		row = rowStyle.Background(t.Highlight).Render(row)
	} else {
		row = rowStyle.Render(row)
	}

	fmt.Fprint(w, row)
}
