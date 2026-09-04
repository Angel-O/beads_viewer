package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LabelDashboardModel renders a lightweight table of label health
type LabelDashboardModel struct {
	labels       []analysis.LabelHealth
	cursor       int
	scrollOffset int // Index of the first visible row
	width        int
	height       int
	theme        Theme
}

const labelDashboardHeaderRows = 2

const (
	labelDashboardColumnDivider            = " | "
	labelDashboardSelectionDecorationWidth = 2
)

func NewLabelDashboardModel(theme Theme) LabelDashboardModel {
	return LabelDashboardModel{theme: theme}
}

func (m *LabelDashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *LabelDashboardModel) SetData(labels []analysis.LabelHealth) {
	m.labels = labels
	// Sort by health level (critical first), then blocked desc, then health asc, then name
	sort.SliceStable(m.labels, func(i, j int) bool {
		li, lj := m.labels[i], m.labels[j]
		levelRank := func(l string) int {
			switch l {
			case analysis.HealthLevelCritical:
				return 0
			case analysis.HealthLevelWarning:
				return 1
			default:
				return 2
			}
		}
		ri, rj := levelRank(li.HealthLevel), levelRank(lj.HealthLevel)
		if ri != rj {
			return ri < rj
		}
		if li.Blocked != lj.Blocked {
			return li.Blocked > lj.Blocked
		}
		if li.Health != lj.Health {
			return li.Health < lj.Health
		}
		return li.Label < lj.Label
	})
	if m.cursor >= len(labels) {
		m.cursor = len(labels) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	}
}

func (m LabelDashboardModel) visibleRows() int {
	visibleRows := m.height - labelDashboardHeaderRows
	if visibleRows < 1 {
		return 1
	}
	return visibleRows
}

// Update handles navigation keys; returns selected label on enter
func (m *LabelDashboardModel) Update(msg tea.KeyMsg) (string, tea.Cmd) {
	visibleRows := m.visibleRows()

	switch msg.String() {
	case "j", "down":
		if m.cursor < len(m.labels)-1 {
			m.cursor++
			// Scroll down if moving past bottom
			if m.cursor >= m.scrollOffset+visibleRows {
				m.scrollOffset = m.cursor - visibleRows + 1
			}
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			// Scroll up if moving past top
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
		}
	case "home":
		m.cursor = 0
		m.scrollOffset = 0
	case "G", "end":
		if len(m.labels) > 0 {
			m.cursor = len(m.labels) - 1
			// Scroll to bottom
			if len(m.labels) > visibleRows {
				m.scrollOffset = len(m.labels) - visibleRows
			} else {
				m.scrollOffset = 0
			}
		}
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.labels) {
			return m.labels[m.cursor].Label, nil
		}
	}
	return "", nil
}

func (m LabelDashboardModel) View() string {
	criticalCount := 0
	warningCount := 0
	for _, label := range m.labels {
		switch label.HealthLevel {
		case analysis.HealthLevelCritical:
			criticalCount++
		case analysis.HealthLevelWarning:
			warningCount++
		}
	}

	var b strings.Builder
	metadata := fmt.Sprintf("LABEL HEALTH │ %d labels │ critical %d │ warning %d", len(m.labels), criticalCount, warningCount)
	b.WriteString(m.theme.Base.Bold(true).Foreground(m.theme.Primary).Render(metadata))
	b.WriteString("\n")

	if len(m.labels) == 0 {
		b.WriteString("No labels found")
		return lipgloss.NewStyle().Width(m.width).Height(m.height).MaxHeight(m.height).Render(b.String())
	}

	headers := []string{"Label", "Health", "Blocked", "Velocity 7d/30d", "Stale"}
	widths := m.computeColumnWidths(headers)
	headerLine := m.renderRow(headers, widths, true, false)
	b.WriteString(headerLine)
	b.WriteString("\n")

	visibleRows := m.visibleRows()
	start := m.scrollOffset
	end := start + visibleRows
	if end > len(m.labels) {
		end = len(m.labels)
	}

	for i := start; i < end; i++ {
		lh := m.labels[i]
		row := m.getRowCells(lh)
		selected := i == m.cursor
		b.WriteString(m.renderRow(row, widths, false, selected))
		if i != end-1 {
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().Width(m.width).Height(m.height).MaxHeight(m.height).Render(b.String())
}

// getRowCells returns the fully rendered (colored) cells for a label row
func (m LabelDashboardModel) getRowCells(lh analysis.LabelHealth) []string {
	return []string{
		m.renderLabelCell(lh),
		m.renderHealthCell(lh),
		m.renderBlockedCell(lh),
		fmt.Sprintf("%d/%d", lh.Velocity.ClosedLast7Days, lh.Velocity.ClosedLast30Days),
		fmt.Sprintf("%d", lh.Freshness.StaleCount),
	}
}

func (m LabelDashboardModel) computeColumnWidths(headers []string) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, lh := range m.labels {
		cells := m.getRowCells(lh)
		for i, c := range cells {
			w := lipgloss.Width(c)
			if w > widths[i] {
				widths[i] = w
			}
		}
	}

	// Reserve the selected row's border and left padding. Any remaining room
	// belongs to the final column so earlier cell starts stay fixed.
	total := labelDashboardSelectionDecorationWidth + lipgloss.Width(labelDashboardColumnDivider)*(len(headers)-1)
	for _, w := range widths {
		total += w
	}
	if m.width > 0 && total > m.width {
		excess := total - m.width
		if excess >= widths[0]-4 {
			widths[0] = 4
		} else {
			widths[0] -= excess
		}
		total = labelDashboardSelectionDecorationWidth + lipgloss.Width(labelDashboardColumnDivider)*(len(headers)-1)
		for _, w := range widths {
			total += w
		}
	}
	return widths
}

func (m LabelDashboardModel) renderRow(cells []string, widths []int, header bool, selected bool) string {
	var parts []string
	for i, cell := range cells {
		// Keep each fixed-width cell on one line so narrow tables stay aligned.
		style := lipgloss.NewStyle().Inline(true).Width(widths[i]).MaxWidth(widths[i])
		parts = append(parts, style.Render(cell))
	}
	if header {
		// Header padding would offset every cell from the row columns.
		return m.theme.Header.Padding(0).Render(" " + strings.Join(parts, labelDashboardColumnDivider))
	}
	if selected {
		background := lipgloss.NewStyle().Background(m.theme.Selected.GetBackground())
		if m.theme.Renderer != nil {
			background = m.theme.Renderer.NewStyle().Background(m.theme.Selected.GetBackground())
		}
		selectedParts := make([]string, 0, len(parts))
		for _, part := range parts {
			content := strings.TrimRight(part, " ")
			selectedPart := background.Render(content)
			selectedPart += background.Render(part[len(content):])
			selectedParts = append(selectedParts, selectedPart)
		}
		row := strings.Join(selectedParts, background.Render(labelDashboardColumnDivider))
		return m.theme.Selected.Render(row)
	}
	return m.theme.Base.Render(" " + strings.Join(parts, labelDashboardColumnDivider))
}

func (m LabelDashboardModel) renderLabelCell(lh analysis.LabelHealth) string {
	indicator := ""
	if lh.HealthLevel == analysis.HealthLevelCritical {
		indicator = " !"
	} else if lh.Blocked > 0 {
		indicator = " ⛔"
	}
	return lh.Label + indicator
}

func (m LabelDashboardModel) renderHealthCell(lh analysis.LabelHealth) string {
	barWidth := 10
	filled := int(float64(barWidth) * float64(lh.Health) / 100.0)
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	filledStr := strings.Repeat("█", filled)
	blankStr := strings.Repeat("░", barWidth-filled)
	bar := filledStr + blankStr

	style := m.theme.Base
	switch lh.HealthLevel {
	case analysis.HealthLevelHealthy:
		style = style.Foreground(m.theme.Open)
	case analysis.HealthLevelWarning:
		style = style.Foreground(m.theme.Feature) // orange-ish
	default:
		style = style.Foreground(m.theme.Blocked)
	}

	return fmt.Sprintf("%3d %s", lh.Health, style.Render(bar))
}

func (m LabelDashboardModel) renderBlockedCell(lh analysis.LabelHealth) string {
	if lh.Blocked == 0 {
		return "0"
	}
	return m.theme.Base.Foreground(m.theme.Blocked).Bold(true).Render(fmt.Sprintf("%d", lh.Blocked))
}
