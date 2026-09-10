package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// RepoPickerModel represents the repository scope picker overlay.
type RepoPickerModel struct {
	catalog              repositorypkg.Catalog
	filtered             repositorypkg.Catalog
	currentID            string
	selectedIndex        int
	selected             map[string]bool // exact repository ID -> selected
	selectFuture         bool
	showContextless      bool
	contextlessBeadCount int
	contextlessSelected  bool
	contextlessMatch     bool
	searching            bool
	searchInput          textinput.Model
	width                int
	height               int
	theme                Theme
}

// NewRepoPickerModel creates a repository picker with all entries selected.
func NewRepoPickerModel(catalog repositorypkg.Catalog, theme Theme) RepoPickerModel {
	input := textinput.New()
	input.Placeholder = "name, path, or exact ID"
	input.CharLimit = 200
	input.Blur()

	m := RepoPickerModel{
		catalog:       append(repositorypkg.Catalog(nil), catalog...),
		selectedIndex: 0,
		selected:      make(map[string]bool, len(catalog)),
		selectFuture:  true,
		searchInput:   input,
		theme:         theme,
	}
	for _, repository := range m.catalog {
		m.selected[repository.ID] = true
	}
	m.filterCatalog("")
	return m
}

// SetSize updates the picker dimensions.
func (m *RepoPickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.setSearchWidth(width)
}

// MoveUp moves selection up.
func (m *RepoPickerModel) MoveUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--
	}
}

// MoveDown moves selection down.
func (m *RepoPickerModel) MoveDown() {
	if m.selectedIndex < m.choiceCount()-1 {
		m.selectedIndex++
	}
}

// View renders the repo picker overlay.
func (m *RepoPickerModel) View() string {
	if m.width == 0 {
		m.width = 60
	}
	if m.height == 0 {
		m.height = 20
	}
	if m.width < 14 || m.height < 5 {
		width := max(1, m.width)
		height := max(1, m.height)
		label := "Repos"
		if m.searching {
			label = "/ " + m.searchInput.Value()
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			truncateRunesHelper(label, width, ""))
	}

	t := m.theme

	// Calculate box dimensions
	boxWidth := 106
	if maximum := m.width - 6; boxWidth > maximum {
		boxWidth = maximum
	}
	if boxWidth < 8 {
		boxWidth = 8
	}
	contentWidth := max(1, boxWidth-4)

	var lines []string
	spacious := m.height >= 12
	compactSearch := m.height < 10

	titleStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)
	lines = append(lines, titleStyle.Render(truncateRunesHelper("Context", contentWidth, "...")))
	if spacious {
		lines = append(lines, "")
	}

	if m.searching {
		if compactSearch {
			query := "/ " + m.searchInput.Value()
			lines = append(lines, t.Renderer.NewStyle().Foreground(t.Secondary).Render(
				truncateRunesHelper(query, contentWidth, "..."),
			))
		} else {
			m.searchInput.Width = max(1, contentWidth-8)
			inputStyle := t.Renderer.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(t.Secondary).
				Padding(0, 1).
				Width(max(4, contentWidth-4))
			lines = append(lines, inputStyle.Render(m.searchInput.View()))
		}
	}

	if m.choiceCount() == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Secondary).Italic(true)
		message := "No repositories available."
		if len(m.catalog) > 0 {
			message = "No matching repositories."
		}
		if !m.searching || m.height >= 8 {
			lines = append(lines, emptyStyle.Render(truncateRunesHelper(message, contentWidth, "...")))
		}
	} else {
		lineBudget := m.height - 4 // modal border and vertical padding
		fixedLines := 1            // title
		if spacious {
			fixedLines += 2 // title and footer spacers
		}
		if m.searching {
			fixedLines++
			if !compactSearch {
				fixedLines += 2 // bordered search input
			}
		}
		showDetails := m.height >= 12
		rowHeight := 1
		if showDetails {
			rowHeight = 2
		}
		maxVisible := (lineBudget - fixedLines) / rowHeight
		if maxVisible < 0 {
			maxVisible = 0
		}
		if maxVisible > 10 {
			maxVisible = 10
		}
		choiceCount := m.choiceCount()
		showPosition := choiceCount > maxVisible && maxVisible > 0
		if showPosition && lineBudget-fixedLines-maxVisible*rowHeight < 1 {
			maxVisible--
			showPosition = maxVisible > 0
		}
		start := 0
		if m.selectedIndex >= maxVisible {
			start = m.selectedIndex - maxVisible + 1
		}
		end := start + maxVisible
		if end > choiceCount {
			end = choiceCount
		}
		for i := start; i < end; i++ {
			isCursor := i == m.selectedIndex

			nameStyle := t.Renderer.NewStyle().Foreground(t.Base.GetForeground())
			if isCursor {
				nameStyle = nameStyle.Foreground(t.Primary).Bold(true)
			}

			prefix := "  "
			if isCursor {
				prefix = "▸ "
			}
			if m.contextlessMatch && i == 0 {
				check := "[ ]"
				if m.contextlessSelected {
					check = "[x]"
				}
				count := fmt.Sprintf(" (%d)", m.contextlessBeadCount)
				lines = append(lines, renderRepoPickerRow(t, nameStyle, contentWidth, prefix, check, "no-context", count, false))
				if showDetails {
					detailStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
					lines = append(lines, detailStyle.Render("      No repository context"))
				}
				continue
			}
			repositoryIndex := i
			if m.contextlessMatch {
				repositoryIndex--
			}
			repository := m.filtered[repositoryIndex]
			isSelected := m.selected[repository.ID]
			check := "[ ]"
			if isSelected {
				check = "[x]"
			}

			count := fmt.Sprintf(" (%d)", repository.BeadCount)
			current := repository.ID == m.currentID && repository.ID != contextlessRepositoryID
			lines = append(lines, renderRepoPickerRow(t, nameStyle, contentWidth, prefix, check, repository.Name, count, current))

			if showDetails {
				detail := repository.ID
				if repository.Path != "" && repository.Path != repository.ID {
					detail += "  " + repository.Path
				} else if repository.Detail != "" && repository.Detail != repository.ID {
					detail += "  " + repository.Detail
				}
				detailStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
				lines = append(lines, detailStyle.Render("      "+truncateRunesHelper(detail, max(1, contentWidth-6), "...")))
			}
		}
		if showPosition {
			lines = append(lines, t.Renderer.NewStyle().Foreground(t.Secondary).Render(
				fmt.Sprintf("  %d/%d", m.selectedIndex+1, choiceCount),
			))
		}
	}

	content := strings.Join(lines, "\n")

	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2).
		Width(boxWidth)
	box := boxStyle.Render(content)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}
