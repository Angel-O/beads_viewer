package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FlowMatrixModel renders an interactive dependency flow dashboard
// showing how labels block each other with visual impact indicators
type FlowMatrixModel struct {
	flow         *analysis.CrossLabelFlow
	issues       []model.Issue
	labelStats   []labelFlowStats
	cursor       int
	scrollOffset int
	width        int
	height       int
	theme        Theme
	ready        bool

	// Drill-down state
	showDrilldown   bool
	drilldownIssues []model.Issue
	drilldownCursor int
	drilldownScroll int
	drilldownTitle  string
}

// labelFlowStats holds computed stats for a single label
type labelFlowStats struct {
	Label           string
	OutgoingCount   int      // How many issues in other labels this blocks
	IncomingCount   int      // How many issues from other labels block this
	OutgoingLabels  []string // Labels this blocks
	IncomingLabels  []string // Labels that block this
	BottleneckScore float64  // Normalized blocking power
	IsBottleneck    bool     // In top bottlenecks
	OnCriticalPath  bool
}

// NewFlowMatrixModel creates a new flow matrix dashboard
func NewFlowMatrixModel(theme Theme) FlowMatrixModel {
	return FlowMatrixModel{
		theme: theme,
	}
}

// SetData initializes the model with flow data
func (m *FlowMatrixModel) SetData(flow *analysis.CrossLabelFlow, issues []model.Issue) {
	m.flow = flow
	m.issues = issues
	m.ready = flow != nil && len(flow.Labels) > 0
	m.computeStats()
}

// UpdateIssueComments refreshes flow drilldown copies without recomputing flow
// metrics or changing dashboard navigation.
func (m *FlowMatrixModel) UpdateIssueComments(issueID string, comments []*model.Comment) {
	updateIssueComments(m.issues, issueID, comments)
	updateIssueComments(m.drilldownIssues, issueID, comments)
}

// SetSize sets the available rendering dimensions
func (m *FlowMatrixModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// computeStats builds per-label statistics from the flow matrix
func (m *FlowMatrixModel) computeStats() {
	if m.flow == nil || len(m.flow.Labels) == 0 {
		m.labelStats = nil
		return
	}

	labels := m.flow.Labels
	n := len(labels)
	matrix := m.flow.FlowMatrix

	// Validate matrix dimensions to prevent panics
	if len(matrix) < n {
		m.labelStats = nil
		return
	}
	for i := 0; i < n; i++ {
		if len(matrix[i]) < n {
			m.labelStats = nil
			return
		}
	}

	// Build index for quick lookup
	labelIndex := make(map[string]int, n)
	for i, l := range labels {
		labelIndex[l] = i
	}

	// Compute stats for each label
	stats := make([]labelFlowStats, n)
	maxOutgoing := 0

	for i, label := range labels {
		stats[i].Label = label

		// Outgoing: row i sums
		for j := 0; j < n; j++ {
			if i != j && matrix[i][j] > 0 {
				stats[i].OutgoingCount += matrix[i][j]
				stats[i].OutgoingLabels = append(stats[i].OutgoingLabels, labels[j])
			}
		}

		// Incoming: column i sums
		for j := 0; j < n; j++ {
			if i != j && matrix[j][i] > 0 {
				stats[i].IncomingCount += matrix[j][i]
				stats[i].IncomingLabels = append(stats[i].IncomingLabels, labels[j])
			}
		}

		if stats[i].OutgoingCount > maxOutgoing {
			maxOutgoing = stats[i].OutgoingCount
		}
	}

	// Compute bottleneck scores (normalized)
	for i := range stats {
		if maxOutgoing > 0 {
			stats[i].BottleneckScore = float64(stats[i].OutgoingCount) / float64(maxOutgoing)
		}
	}

	// Mark bottlenecks (top N or those in BottleneckLabels)
	bottleneckSet := make(map[string]bool)
	for _, bl := range m.flow.BottleneckLabels {
		bottleneckSet[bl] = true
	}
	for i := range stats {
		stats[i].IsBottleneck = bottleneckSet[stats[i].Label]
	}

	// Sort by outgoing count (highest blocking power first)
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].OutgoingCount != stats[j].OutgoingCount {
			return stats[i].OutgoingCount > stats[j].OutgoingCount
		}
		return stats[i].Label < stats[j].Label
	})

	m.labelStats = stats
}

// Update handles keyboard input
func (m *FlowMatrixModel) Update(msg tea.KeyMsg) tea.Cmd {
	if m.showDrilldown {
		return m.updateDrilldown(msg)
	}

	key := msg.String()

	switch key {
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "g", "home":
		m.cursor = 0
		m.scrollOffset = 0
	case "G", "end":
		if len(m.labelStats) > 0 {
			m.cursor = len(m.labelStats) - 1
			m.ensureVisible()
		}
	case "enter":
		if m.flow != nil && m.flow.TotalCrossLabelDeps > 0 && m.cursor < len(m.labelStats) {
			m.openDrilldown()
		}
	case "ctrl+d":
		m.moveCursor(m.visibleRows() / 2)
	case "ctrl+u":
		m.moveCursor(-m.visibleRows() / 2)
	}

	return nil
}

func (m *FlowMatrixModel) updateDrilldown(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "esc", "q":
		m.showDrilldown = false
	case "j", "down":
		if m.drilldownCursor < len(m.drilldownIssues)-1 {
			m.drilldownCursor++
			m.ensureDrilldownVisible()
		}
	case "k", "up":
		if m.drilldownCursor > 0 {
			m.drilldownCursor--
			m.ensureDrilldownVisible()
		}
	}
	return nil
}

func (m *FlowMatrixModel) moveCursor(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.labelStats) {
		m.cursor = len(m.labelStats) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()
}

func (m *FlowMatrixModel) ensureVisible() {
	visible := m.visibleRows()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visible {
		m.scrollOffset = m.cursor - visible + 1
	}
}

func (m *FlowMatrixModel) ensureDrilldownVisible() {
	visible := m.height - 8
	if visible < 3 {
		visible = 3
	}
	if m.drilldownCursor < m.drilldownScroll {
		m.drilldownScroll = m.drilldownCursor
	}
	if m.drilldownCursor >= m.drilldownScroll+visible {
		m.drilldownScroll = m.drilldownCursor - visible + 1
	}
}

func (m *FlowMatrixModel) visibleRows() int {
	rows := m.height - 4 // Flow header plus labels panel header and separator
	if rows < 3 {
		rows = 3
	}
	return rows
}

func (m *FlowMatrixModel) openDrilldown() {
	if m.flow == nil || m.flow.TotalCrossLabelDeps == 0 || m.cursor >= len(m.labelStats) {
		return
	}
	selectedLabel := m.labelStats[m.cursor].Label

	// Use only the issues participating in cross-label blocking pairs. An issue
	// can carry the selected label without being part of the selected flow.
	issueIDs := make(map[string]struct{})
	for _, dep := range m.flow.Dependencies {
		for _, pair := range dep.BlockingPairs {
			if pair.BlockerLabel != selectedLabel && pair.BlockedLabel != selectedLabel {
				continue
			}
			if pair.BlockerID != "" {
				issueIDs[pair.BlockerID] = struct{}{}
			}
			if pair.BlockedID != "" {
				issueIDs[pair.BlockedID] = struct{}{}
			}
		}
	}

	ids := make([]string, 0, len(issueIDs))
	for id := range issueIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	issueByID := make(map[string]model.Issue, len(m.issues))
	for _, issue := range m.issues {
		issueByID[issue.ID] = issue
	}
	relevant := make([]model.Issue, 0, len(ids))
	for _, id := range ids {
		if issue, ok := issueByID[id]; ok {
			relevant = append(relevant, issue)
		}
	}

	m.drilldownIssues = relevant
	m.drilldownCursor = 0
	m.drilldownScroll = 0
	m.drilldownTitle = fmt.Sprintf("Cross-label issues involving: %s", selectedLabel)
	m.showDrilldown = true
}

// SelectedLabel returns the currently selected label (for drill-down from parent)
func (m *FlowMatrixModel) SelectedLabel() string {
	if m.cursor < len(m.labelStats) {
		return m.labelStats[m.cursor].Label
	}
	return ""
}

// View renders the flow matrix dashboard
func (m FlowMatrixModel) View() string {
	if !m.ready || m.flow == nil || m.flow.TotalCrossLabelDeps == 0 {
		return m.renderFullHeight(m.renderEmptyState())
	}

	if m.showDrilldown {
		return m.renderFullHeight(m.renderDrilldown())
	}

	// Reserve exactly three cells for the spaces and separator between panels.
	leftWidth, rightWidth := m.flowPanelWidths()

	// Build panels
	leftPanel := m.renderLabelsPanel(leftWidth)
	rightPanel := m.renderDetailPanel(rightWidth)

	// Header
	header := m.renderHeader()

	// Join panels side by side
	leftLines := strings.Split(leftPanel, "\n")
	rightLines := strings.Split(rightPanel, "\n")

	// Normalize heights
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}
	for len(leftLines) < maxLines {
		leftLines = append(leftLines, strings.Repeat(" ", leftWidth))
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, strings.Repeat(" ", rightWidth))
	}
	for i := range leftLines {
		leftLines[i] = fitFlowLine(leftLines[i], leftWidth)
	}
	for i := range rightLines {
		rightLines[i] = fitFlowLine(rightLines[i], rightWidth)
	}

	var body strings.Builder
	separator := m.theme.Renderer.NewStyle().
		Foreground(m.theme.Border).
		Render("│")

	for i := 0; i < maxLines; i++ {
		body.WriteString(leftLines[i])
		body.WriteString(" ")
		body.WriteString(separator)
		body.WriteString(" ")
		body.WriteString(rightLines[i])
		if i < maxLines-1 {
			body.WriteString("\n")
		}
	}

	return m.renderFullHeight(lipgloss.JoinVertical(lipgloss.Left, header, body.String()))
}

func (m FlowMatrixModel) renderFullHeight(content string) string {
	content = fitFlowContent(content, m.width)
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		MaxHeight(m.height).
		Render(content)
}

func (m FlowMatrixModel) flowPanelWidths() (int, int) {
	if m.width < 5 {
		return max(m.width, 0), 0
	}
	available := m.width - 3
	left := available * 35 / 100
	if left < 1 {
		left = 1
	}
	if left >= available {
		left = available - 1
	}
	return left, available - left
}

func fitFlowLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Truncate(line, width, "")
	if padding := width - lipgloss.Width(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

func fitFlowContent(content string, width int) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = fitFlowLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func (m FlowMatrixModel) renderEmptyState() string {
	return strings.Join([]string{
		m.theme.Base.Render("No open cross-label blocking dependencies"),
		"Flow counts open blocking cross-label dependencies between labels.",
		"There is nothing to drill into for a label yet.",
	}, "\n")
}

func (m FlowMatrixModel) renderHeader() string {
	titleStyle := m.theme.Renderer.NewStyle().
		Bold(true).
		Foreground(m.theme.Primary).
		PaddingRight(2)

	statsStyle := m.theme.Renderer.NewStyle().
		Foreground(m.theme.Subtext)

	title := titleStyle.Render("DEPENDENCY FLOW")
	stats := statsStyle.Render(fmt.Sprintf("│ %d labels │ %d cross-label deps │ %d bottlenecks",
		len(m.flow.Labels),
		m.flow.TotalCrossLabelDeps,
		len(m.flow.BottleneckLabels)))

	headerLine := lipgloss.JoinHorizontal(lipgloss.Left, title, stats)

	borderStyle := m.theme.Renderer.NewStyle().
		Foreground(m.theme.Border)

	return lipgloss.JoinVertical(lipgloss.Left,
		fitFlowLine(headerLine, m.width),
		fitFlowLine(borderStyle.Render(strings.Repeat("─", max(m.width, 0))), m.width))
}

func (m FlowMatrixModel) renderLabelsPanel(width int) string {
	var b strings.Builder

	// Panel header
	headerStyle := m.theme.Renderer.NewStyle().
		Bold(true).
		Foreground(m.theme.Secondary)
	b.WriteString(fitFlowLine(headerStyle.Render("LABELS (by blocking power)"), width))
	b.WriteString("\n")

	// Separator
	sepStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)
	b.WriteString(fitFlowLine(sepStyle.Render(strings.Repeat("─", max(width, 0))), width))
	b.WriteString("\n")

	// Find max for bar scaling
	maxOut := 1
	for _, s := range m.labelStats {
		if s.OutgoingCount > maxOut {
			maxOut = s.OutgoingCount
		}
	}

	// Visible rows
	visible := m.visibleRows()
	start := m.scrollOffset
	end := start + visible
	if end > len(m.labelStats) {
		end = len(m.labelStats)
	}

	for i := start; i < end; i++ {
		stat := m.labelStats[i]
		isSelected := i == m.cursor

		// Build the row
		row := m.renderLabelRow(stat, isSelected, maxOut, width)
		b.WriteString(row)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	// Pad remaining space
	for i := end - start; i < visible; i++ {
		b.WriteString("\n")
		b.WriteString(strings.Repeat(" ", width))
	}

	return fitFlowContent(b.String(), width)
}

func (m FlowMatrixModel) renderLabelRow(stat labelFlowStats, selected bool, maxOut, totalWidth int) string {
	if totalWidth <= 0 {
		return ""
	}

	countStr := fmt.Sprintf("%d", stat.OutgoingCount)
	barWidth := max(totalWidth-12-lipgloss.Width(countStr)-2, 0)
	labelWidth := min(12, max(totalWidth-lipgloss.Width(countStr)-barWidth-2, 0))
	label := ansi.Truncate(stat.Label, labelWidth, "")

	// Color based on bottleneck status
	var labelColor lipgloss.AdaptiveColor
	if stat.IsBottleneck {
		labelColor = m.theme.Blocked // Red for bottlenecks
	} else if stat.BottleneckScore > 0.5 {
		labelColor = m.theme.Feature // Orange for high impact
	} else if stat.OutgoingCount > 0 {
		labelColor = m.theme.Task // Yellow for some impact
	} else {
		labelColor = m.theme.Subtext // Gray for no impact
	}

	labelStyle := m.theme.Renderer.NewStyle().
		Foreground(labelColor)
	labelField := labelStyle.Render(label + strings.Repeat(" ", max(labelWidth-lipgloss.Width(label), 0)))

	// Bar visualization
	barFilled := 0
	if maxOut > 0 {
		barFilled = stat.OutgoingCount * barWidth / maxOut
	}
	if barFilled > barWidth {
		barFilled = barWidth
	}

	// Use different characters for bar intensity
	bar := ""
	if barFilled > 0 {
		bar = strings.Repeat("█", barFilled)
	}
	barEmpty := strings.Repeat("░", barWidth-barFilled)

	barStyle := m.theme.Renderer.NewStyle().Foreground(labelColor)
	emptyStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)

	// Assemble row
	row := fmt.Sprintf("%s %s%s %s",
		labelField,
		barStyle.Render(bar),
		emptyStyle.Render(barEmpty),
		countStr)
	row = fitFlowLine(row, totalWidth)

	// Selection highlight
	if selected {
		selectStyle := m.theme.Renderer.NewStyle().
			Background(m.theme.Highlight).
			Width(totalWidth)
		row = selectStyle.Render(row)
	}

	return row
}

func (m FlowMatrixModel) renderDetailPanel(width int) string {
	var b strings.Builder

	if m.cursor >= len(m.labelStats) {
		return fitFlowLine("Select a label", width)
	}

	stat := m.labelStats[m.cursor]

	// Panel header
	headerStyle := m.theme.Renderer.NewStyle().
		Bold(true).
		Foreground(m.theme.Primary)

	b.WriteString(fitFlowLine(headerStyle.Render(stat.Label), width))
	b.WriteString("\n")

	// Separator
	sepStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)
	b.WriteString(fitFlowLine(sepStyle.Render(strings.Repeat("─", max(width, 0))), width))
	b.WriteString("\n\n")

	// Stats summary
	summaryStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	b.WriteString(summaryStyle.Render("IMPACT SUMMARY"))
	b.WriteString("\n")

	// Blocking power indicator
	scoreBar := m.renderScoreBar(stat.BottleneckScore, 20)
	scoreLabel := "Low"
	scoreColor := m.theme.Open
	if stat.BottleneckScore > 0.7 {
		scoreLabel = "HIGH"
		scoreColor = m.theme.Blocked
	} else if stat.BottleneckScore > 0.3 {
		scoreLabel = "Medium"
		scoreColor = m.theme.Feature
	}
	scoreStyle := m.theme.Renderer.NewStyle().Foreground(scoreColor).Bold(true)
	b.WriteString(fmt.Sprintf("  Blocking Power: %s %s\n", scoreBar, scoreStyle.Render(scoreLabel)))

	if stat.IsBottleneck {
		bottleneckStyle := m.theme.Renderer.NewStyle().
			Foreground(m.theme.Blocked).
			Bold(true)
		b.WriteString(bottleneckStyle.Render("  ⚠ BOTTLENECK"))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Two-column layout for blocks/blocked by
	halfWidth := max((width-4)/2, 0)

	// BLOCKS section
	blocksHeader := m.theme.Renderer.NewStyle().
		Foreground(m.theme.Blocked).
		Bold(true).
		Render(fmt.Sprintf("BLOCKS → (%d)", stat.OutgoingCount))

	blockedByHeader := m.theme.Renderer.NewStyle().
		Foreground(m.theme.InProgress).
		Bold(true).
		Render(fmt.Sprintf("← BLOCKED BY (%d)", stat.IncomingCount))

	b.WriteString(fmt.Sprintf("%-*s  %s\n", halfWidth, blocksHeader, blockedByHeader))

	// List entries
	maxEntries := 6
	outLabels := stat.OutgoingLabels
	inLabels := stat.IncomingLabels

	// Get counts for each label
	outCounts := m.getFlowCounts(stat.Label, outLabels, true)
	inCounts := m.getFlowCounts(stat.Label, inLabels, false)

	for i := 0; i < maxEntries; i++ {
		leftStr := ""
		rightStr := ""

		if i < len(outLabels) {
			count := outCounts[outLabels[i]]
			miniBar := m.miniBar(count, 5)
			leftStr = fmt.Sprintf("  %s %s (%d)", miniBar, outLabels[i], count)
		}
		if i < len(inLabels) {
			count := inCounts[inLabels[i]]
			miniBar := m.miniBar(count, 5)
			rightStr = fmt.Sprintf("  %s %s (%d)", miniBar, inLabels[i], count)
		}

		b.WriteString(fmt.Sprintf("%-*s  %s\n", halfWidth, leftStr, rightStr))
	}

	if len(outLabels) > maxEntries || len(inLabels) > maxEntries {
		moreStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext).Italic(true)
		leftMore := ""
		rightMore := ""
		if len(outLabels) > maxEntries {
			leftMore = fmt.Sprintf("  +%d more", len(outLabels)-maxEntries)
		}
		if len(inLabels) > maxEntries {
			rightMore = fmt.Sprintf("  +%d more", len(inLabels)-maxEntries)
		}
		b.WriteString(fmt.Sprintf("%-*s  %s\n", halfWidth, moreStyle.Render(leftMore), moreStyle.Render(rightMore)))
	}

	b.WriteString("\n")

	return fitFlowContent(b.String(), width)
}

func (m FlowMatrixModel) getFlowCounts(sourceLabel string, targetLabels []string, outgoing bool) map[string]int {
	counts := make(map[string]int)
	if m.flow == nil {
		return counts
	}

	// Build label index
	labelIndex := make(map[string]int)
	for i, l := range m.flow.Labels {
		labelIndex[l] = i
	}

	sourceIdx, ok := labelIndex[sourceLabel]
	if !ok {
		return counts
	}

	for _, target := range targetLabels {
		targetIdx, ok := labelIndex[target]
		if !ok {
			continue
		}
		if outgoing {
			counts[target] = m.flow.FlowMatrix[sourceIdx][targetIdx]
		} else {
			counts[target] = m.flow.FlowMatrix[targetIdx][sourceIdx]
		}
	}
	return counts
}

func (m FlowMatrixModel) renderScoreBar(score float64, width int) string {
	filled := int(score * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}

	var color lipgloss.AdaptiveColor
	if score > 0.7 {
		color = m.theme.Blocked
	} else if score > 0.3 {
		color = m.theme.Feature
	} else {
		color = m.theme.Open
	}

	barStyle := m.theme.Renderer.NewStyle().Foreground(color)
	emptyStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)

	return barStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", width-filled))
}

func (m FlowMatrixModel) miniBar(count, maxWidth int) string {
	if count <= 0 {
		return strings.Repeat("·", maxWidth)
	}

	// Scale linearly for visualization
	filled := count
	if filled > maxWidth {
		filled = maxWidth
	}

	var color lipgloss.AdaptiveColor
	if count >= 5 {
		color = m.theme.Blocked
	} else if count >= 2 {
		color = m.theme.Feature
	} else {
		color = m.theme.Task
	}

	barStyle := m.theme.Renderer.NewStyle().Foreground(color)
	return barStyle.Render(strings.Repeat("■", filled)) + strings.Repeat("·", maxWidth-filled)
}

func (m FlowMatrixModel) renderDrilldown() string {
	var b strings.Builder

	// Header
	headerStyle := m.theme.Renderer.NewStyle().
		Bold(true).
		Foreground(m.theme.Primary)

	b.WriteString(headerStyle.Render(m.drilldownTitle))
	b.WriteString(fmt.Sprintf(" (%d issues)\n", len(m.drilldownIssues)))

	borderStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)
	b.WriteString(fitFlowLine(borderStyle.Render(strings.Repeat("─", max(m.width, 0))), m.width))
	b.WriteString("\n\n")

	if len(m.drilldownIssues) == 0 {
		b.WriteString("No issues found")
		return fitFlowContent(b.String(), m.width)
	}

	// Visible rows. The shared parent footer is outside this model.
	visible := m.height - 4
	if visible < 3 {
		visible = 3
	}
	start := m.drilldownScroll
	end := start + visible
	if end > len(m.drilldownIssues) {
		end = len(m.drilldownIssues)
	}

	for i := start; i < end; i++ {
		iss := m.drilldownIssues[i]
		selected := i == m.drilldownCursor

		// Status indicator
		statusColor := m.theme.GetStatusColor(string(iss.Status))
		statusStyle := m.theme.Renderer.NewStyle().Foreground(statusColor)
		statusIndicator := "●"

		// Issue line
		idStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
		titleStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Base.GetForeground())

		title := iss.Title
		row := fmt.Sprintf("%s %s %s",
			statusStyle.Render(statusIndicator),
			idStyle.Render(iss.ID),
			titleStyle.Render(title))
		row = fitFlowLine(row, m.width)

		if selected {
			selectStyle := m.theme.Renderer.NewStyle().
				Background(m.theme.Highlight).
				Width(m.width)
			row = selectStyle.Render(row)
		}

		b.WriteString(row)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return fitFlowContent(b.String(), m.width)
}

// MoveUp moves the cursor up by one
func (m *FlowMatrixModel) MoveUp() {
	if m.showDrilldown {
		if m.drilldownCursor > 0 {
			m.drilldownCursor--
			m.ensureDrilldownVisible()
		}
	} else {
		m.moveCursor(-1)
	}
}

// MoveDown moves the cursor down by one
func (m *FlowMatrixModel) MoveDown() {
	if m.showDrilldown {
		if m.drilldownCursor < len(m.drilldownIssues)-1 {
			m.drilldownCursor++
			m.ensureDrilldownVisible()
		}
	} else {
		m.moveCursor(1)
	}
}

// OpenDrilldown opens the drill-down view for the selected label
func (m *FlowMatrixModel) OpenDrilldown() {
	m.openDrilldown()
}

// GoToStart moves cursor to the first item
func (m *FlowMatrixModel) GoToStart() {
	if m.showDrilldown {
		m.drilldownCursor = 0
		m.drilldownScroll = 0
	} else {
		m.cursor = 0
		m.scrollOffset = 0
	}
}

// GoToEnd moves cursor to the last item
func (m *FlowMatrixModel) GoToEnd() {
	if m.showDrilldown {
		if len(m.drilldownIssues) > 0 {
			m.drilldownCursor = len(m.drilldownIssues) - 1
			m.ensureDrilldownVisible()
		}
	} else {
		if len(m.labelStats) > 0 {
			m.cursor = len(m.labelStats) - 1
			m.ensureVisible()
		}
	}
}

// SelectedDrilldownIssue returns the currently selected issue in drilldown mode
func (m *FlowMatrixModel) SelectedDrilldownIssue() *model.Issue {
	if !m.showDrilldown || m.drilldownCursor >= len(m.drilldownIssues) {
		return nil
	}
	return &m.drilldownIssues[m.drilldownCursor]
}

// FlowMatrixView is the legacy function for backward compatibility
// It now returns a simple text summary pointing to the interactive view
func FlowMatrixView(flow analysis.CrossLabelFlow, width int) string {
	if len(flow.Labels) == 0 {
		return "No cross-label dependencies found"
	}

	var b strings.Builder
	b.WriteString("DEPENDENCY FLOW SUMMARY\n")
	b.WriteString(strings.Repeat("─", 40))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Labels: %d\n", len(flow.Labels)))
	b.WriteString(fmt.Sprintf("Cross-label dependencies: %d\n", flow.TotalCrossLabelDeps))
	b.WriteString(fmt.Sprintf("Bottleneck labels: %v\n", flow.BottleneckLabels))

	return b.String()
}
