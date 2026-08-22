package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// TypePickerModel is an exact, multi-select issue-type picker.
type TypePickerModel struct {
	types         []model.IssueType
	selectedIndex int
	selected      map[model.IssueType]bool
	width         int
	height        int
	theme         Theme
}

func issueTypesFromIssues(issues []model.Issue, selected map[model.IssueType]bool) []model.IssueType {
	unique := make(map[model.IssueType]bool, len(selected))
	for _, issue := range issues {
		if strings.TrimSpace(string(issue.IssueType)) != "" {
			unique[issue.IssueType] = true
		}
	}
	for issueType := range selected {
		unique[issueType] = true
	}
	types := make([]model.IssueType, 0, len(unique))
	for issueType := range unique {
		types = append(types, issueType)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

func NewTypePickerModel(types []model.IssueType, selected map[model.IssueType]bool, theme Theme) TypePickerModel {
	m := TypePickerModel{theme: theme}
	m.SetTypes(types, selected)
	return m
}

func (m *TypePickerModel) SetTypes(types []model.IssueType, selected map[model.IssueType]bool) {
	unique := make(map[model.IssueType]bool, len(types)+len(selected))
	for _, issueType := range types {
		if strings.TrimSpace(string(issueType)) != "" {
			unique[issueType] = true
		}
	}
	for issueType := range selected {
		if strings.TrimSpace(string(issueType)) != "" {
			unique[issueType] = true
		}
	}
	m.types = m.types[:0]
	for issueType := range unique {
		m.types = append(m.types, issueType)
	}
	sort.Slice(m.types, func(i, j int) bool { return m.types[i] < m.types[j] })
	m.selected = make(map[model.IssueType]bool, len(m.types))
	if selected == nil {
		m.SelectAll()
	} else {
		for _, issueType := range m.types {
			if selected[issueType] {
				m.selected[issueType] = true
			}
		}
	}
	m.clampSelection()
}

func (m *TypePickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *TypePickerModel) MoveUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--
	}
}

func (m *TypePickerModel) MoveDown() {
	if m.selectedIndex < len(m.types)-1 {
		m.selectedIndex++
	}
}

func (m *TypePickerModel) ToggleSelected() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.types) {
		return
	}
	issueType := m.types[m.selectedIndex]
	m.selected[issueType] = !m.selected[issueType]
}

func (m *TypePickerModel) SelectAll() {
	m.selected = make(map[model.IssueType]bool, len(m.types))
	for _, issueType := range m.types {
		m.selected[issueType] = true
	}
}

func (m *TypePickerModel) ClearSelection() {
	m.selected = make(map[model.IssueType]bool)
}

func (m TypePickerModel) SelectedTypes() map[model.IssueType]bool {
	selected := make(map[model.IssueType]bool, len(m.selected))
	for _, issueType := range m.types {
		if m.selected[issueType] {
			selected[issueType] = true
		}
	}
	return selected
}

func (m TypePickerModel) AllSelected() bool {
	return len(m.types) > 0 && len(m.SelectedTypes()) == len(m.types)
}

func (m *TypePickerModel) clampSelection() {
	if len(m.types) == 0 {
		m.selectedIndex = 0
		return
	}
	if m.selectedIndex >= len(m.types) {
		m.selectedIndex = len(m.types) - 1
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
}

func (m *TypePickerModel) View() string {
	if m.width == 0 && m.height == 0 {
		m.width = 60
		m.height = 20
	}
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < 14 || m.height < 12 {
		label := "Types"
		if m.selectedIndex >= 0 && m.selectedIndex < len(m.types) {
			check := "[ ]"
			if m.selected[m.types[m.selectedIndex]] {
				check = "[x]"
			}
			label = check + " " + string(m.types[m.selectedIndex])
		}
		label = truncateRunesHelper(label, m.width, "…")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, label)
	}

	boxWidth := 54
	if maximum := m.width - 6; boxWidth > maximum {
		boxWidth = maximum
	}
	contentWidth := max(1, boxWidth-4)
	titleStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary).Bold(true)
	lines := []string{titleStyle.Render(truncateRunesHelper("Issue Type Filter", contentWidth, "...")), ""}

	controlHints := []string{
		"j/k: navigate",
		"space: toggle",
		"a: all",
		"n: reset",
		"enter: apply",
		"esc: cancel",
	}

	contentRows := max(m.height-4, 1)
	hintLines := wrapControlHints(controlHints, contentWidth)
	// Both the list and empty states reserve title, spacer, and content rows.
	maxHintRows := max(1, contentRows-3)
	if len(hintLines) > maxHintRows {
		if maxHintRows == 1 {
			hintLines = []string{truncateRunesHelper("…", contentWidth, "…")}
		} else {
			hintLines = append(hintLines[:maxHintRows-1], truncateRunesHelper("…", contentWidth, "…"))
		}
	}

	if len(m.types) == 0 {
		empty := truncateRunesHelper("No issue types loaded.", contentWidth, "...")
		lines = append(lines, m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary).Italic(true).Render(empty))
	} else {
		maxVisible := contentRows - 2 - len(hintLines)
		if maxVisible < 1 {
			maxVisible = 1
		}
		maxVisible = min(12, maxVisible)
		start := 0
		if m.selectedIndex >= maxVisible {
			start = m.selectedIndex - maxVisible + 1
		}
		end := min(len(m.types), start+maxVisible)
		for i := start; i < end; i++ {
			prefix := "  "
			style := m.theme.Renderer.NewStyle().Foreground(m.theme.Base.GetForeground())
			if i == m.selectedIndex {
				prefix = "▸ "
				style = style.Foreground(m.theme.Primary).Bold(true)
			}
			check := "[ ]"
			if m.selected[m.types[i]] {
				check = "[x]"
			}
			lines = append(lines, style.Render(truncateRunesHelper(prefix+check+" "+string(m.types[i]), contentWidth, "...")))
		}
	}

	for _, line := range hintLines {
		lines = append(lines, m.theme.Renderer.NewStyle().Foreground(ColorFooterHint).Italic(true).Render(line))
	}

	box := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Primary).
		Padding(1, 2).
		Width(boxWidth).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func wrapControlHints(hints []string, maxWidth int) []string {
	if len(hints) == 0 || maxWidth <= 0 {
		return nil
	}
	if maxWidth == 1 {
		return []string{"..."}
	}

	const sep = " • "
	var lines []string
	current := ""
	currentLen := 0
	for _, hint := range hints {
		hintLen := len([]rune(hint))
		if current == "" {
			if hintLen > maxWidth {
				lines = append(lines, truncateRunesHelper(hint, maxWidth, "…"))
				current = ""
				currentLen = 0
				continue
			}
			current = hint
			currentLen = hintLen
			continue
		}

		sepLen := len([]rune(sep))
		if currentLen+sepLen+hintLen <= maxWidth {
			current += sep + hint
			currentLen += sepLen + hintLen
			continue
		}
		lines = append(lines, current)
		current = hint
		currentLen = hintLen
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
