package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
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
	inputWidth := width - 24
	if inputWidth > 56 {
		inputWidth = 56
	}
	if inputWidth < 12 {
		inputWidth = 12
	}
	m.searchInput.Width = inputWidth
}

// SetActiveRepos initializes selection from the currently active repo filter (nil = all).
func (m *RepoPickerModel) SetActiveRepos(active map[string]bool) {
	m.contextlessSelected = false
	if len(m.catalog) == 0 {
		m.selected = map[string]bool{}
		m.selectFuture = active == nil
		return
	}

	m.selected = make(map[string]bool, len(m.catalog))
	m.selectFuture = active == nil
	if active == nil {
		for _, repository := range m.catalog {
			m.selected[repository.ID] = true
		}
		return
	}

	for _, repository := range m.catalog {
		if active[repository.ID] {
			m.selected[repository.ID] = true
		}
	}
}

// SetHubScope enables the dedicated contextless choice and initializes the
// picker from an explicit Hub selector.
func (m *RepoPickerModel) SetHubScope(scope hub.HubScope) {
	m.showContextless = true
	contextlessSelected := scope.Mode == hub.HubScopeAllItems || scope.Mode == hub.HubScopeContextless || scope.IncludeContextless
	if scope.Mode == hub.HubScopeContextless {
		m.selected = make(map[string]bool)
		m.selectFuture = false
	} else if scope.Mode == hub.HubScopeSelectedContexts {
		selected := make(map[string]bool, len(scope.Contexts))
		for _, contextID := range scope.Contexts {
			selected[contextID] = true
		}
		m.SetActiveRepos(selected)
	} else {
		m.SetActiveRepos(nil)
	}
	m.contextlessSelected = contextlessSelected
	m.filterCatalog("")
}

// SetContextlessBeadCount updates the count shown for the no-context choice.
func (m *RepoPickerModel) SetContextlessBeadCount(count int) {
	if count < 0 {
		count = 0
	}
	m.contextlessBeadCount = count
}

// SetCurrentRepository identifies the authoritative repository for the picker.
func (m *RepoPickerModel) SetCurrentRepository(repositoryID string) {
	m.currentID = repositoryID
}

// SetCatalog refreshes picker options while preserving draft selection and the
// cursor by exact repository ID. New entries join only an all-repositories draft.
func (m *RepoPickerModel) SetCatalog(catalog repositorypkg.Catalog) {
	contextlessCursor := m.currentChoiceIsContextless()
	cursorID := m.currentRepositoryID()
	previousIndex := m.selectedIndex
	available := make(map[string]bool, len(catalog))
	for _, repository := range catalog {
		available[repository.ID] = true
		if m.selectFuture {
			m.selected[repository.ID] = true
		}
	}
	for id := range m.selected {
		if !available[id] {
			delete(m.selected, id)
		}
	}
	m.catalog = append(repositorypkg.Catalog(nil), catalog...)
	m.filterCatalog(cursorID)
	if contextlessCursor && m.contextlessMatch {
		m.selectedIndex = 0
	}
	if cursorID == "" || m.currentRepositoryID() != cursorID {
		m.selectedIndex = previousIndex
		m.clampSelection()
	}
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

// ToggleSelected toggles the selected state of the current repo.
func (m *RepoPickerModel) ToggleSelected() {
	if m.currentChoiceIsContextless() {
		m.contextlessSelected = !m.contextlessSelected
		return
	}
	id := m.currentRepositoryID()
	if id == "" {
		return
	}
	m.selected[id] = !m.selected[id]
	m.selectFuture = len(m.selected) == len(m.catalog)
	if m.selectFuture {
		for _, repository := range m.catalog {
			if !m.selected[repository.ID] {
				m.selectFuture = false
				break
			}
		}
	}
}

// SelectAll selects all repos.
func (m *RepoPickerModel) SelectAll() {
	m.contextlessSelected = m.showContextless
	for _, repository := range m.catalog {
		m.selected[repository.ID] = true
	}
	m.selectFuture = true
}

// ToggleAll switches between every available choice and an empty draft.
func (m *RepoPickerModel) ToggleAll() {
	allSelected := !m.showContextless || m.contextlessSelected
	for _, repository := range m.catalog {
		if !m.selected[repository.ID] {
			allSelected = false
			break
		}
	}
	if allSelected {
		m.ClearSelection()
		return
	}
	m.SelectAll()
}

// ClearSelection clears every visible checkbox. Applying an empty draft means all.
func (m *RepoPickerModel) ClearSelection() {
	m.contextlessSelected = false
	m.selected = make(map[string]bool)
	m.selectFuture = false
}

// SelectCurrent selects only the current repository in the draft.
func (m *RepoPickerModel) SelectCurrent() {
	if m.currentID == "" || m.currentID == contextlessRepositoryID {
		return
	}
	available := false
	for _, repository := range m.catalog {
		if repository.ID == m.currentID {
			available = true
			break
		}
	}
	if !available {
		return
	}

	m.selected = map[string]bool{m.currentID: true}
	m.selectFuture = false
	m.contextlessSelected = false
}

// SelectedRepos returns the selected repos as a map (repo -> true).
func (m RepoPickerModel) SelectedRepos() map[string]bool {
	out := make(map[string]bool)
	for _, repository := range m.catalog {
		if m.selected[repository.ID] {
			out[repository.ID] = true
		}
	}
	return out
}

func (m RepoPickerModel) ContextlessSelected() bool { return m.contextlessSelected }

func (m *RepoPickerModel) BeginSearch() {
	m.searching = true
	m.searchInput.Focus()
}

func (m RepoPickerModel) IsSearching() bool { return m.searching }

func (m *RepoPickerModel) ClearSearch() {
	m.searching = false
	m.searchInput.SetValue("")
	m.searchInput.Blur()
	m.filterCatalog("")
}

func (m *RepoPickerModel) UpdateSearch(msg tea.KeyMsg) {
	cursorID := m.currentRepositoryID()
	m.searchInput, _ = m.searchInput.Update(msg)
	m.filterCatalog(cursorID)
}

func (m RepoPickerModel) SearchValue() string { return m.searchInput.Value() }

func (m RepoPickerModel) FilteredCount() int { return m.choiceCount() }

func (m RepoPickerModel) choiceCount() int {
	count := len(m.filtered)
	if m.contextlessMatch {
		count++
	}
	return count
}

func (m RepoPickerModel) currentChoiceIsContextless() bool {
	return m.contextlessMatch && m.selectedIndex == 0
}

func (m RepoPickerModel) currentRepositoryID() string {
	index := m.selectedIndex
	if m.contextlessMatch {
		index--
	}
	if index < 0 || index >= len(m.filtered) {
		return ""
	}
	return m.filtered[index].ID
}

func (m *RepoPickerModel) filterCatalog(preferredID string) {
	query := strings.TrimSpace(m.searchInput.Value())
	m.contextlessMatch = m.showContextless && (query == "" || fuzzyScore("no-context contextless no repository", query) > 0)
	if query == "" {
		m.filtered = append(repositorypkg.Catalog(nil), m.catalog...)
	} else {
		type scoredRepository struct {
			repository repositorypkg.CatalogEntry
			score      int
		}
		matches := make([]scoredRepository, 0, len(m.catalog))
		for _, repository := range m.catalog {
			score := 0
			for _, candidate := range []string{repository.Name, repository.Path, repository.Detail, repository.ID} {
				if candidateScore := fuzzyScore(candidate, query); candidateScore > score {
					score = candidateScore
				}
			}
			if score > 0 {
				matches = append(matches, scoredRepository{repository: repository, score: score})
			}
		}
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].score != matches[j].score {
				return matches[i].score > matches[j].score
			}
			if matches[i].repository.Name != matches[j].repository.Name {
				return matches[i].repository.Name < matches[j].repository.Name
			}
			return matches[i].repository.ID < matches[j].repository.ID
		})
		m.filtered = make(repositorypkg.Catalog, len(matches))
		for i, match := range matches {
			m.filtered[i] = match.repository
		}
	}
	m.selectedIndex = 0
	offset := 0
	if m.contextlessMatch {
		offset = 1
	}
	for i, repository := range m.filtered {
		if repository.ID == preferredID {
			m.selectedIndex = i + offset
			break
		}
	}
	m.clampSelection()
}

func (m *RepoPickerModel) clampSelection() {
	if m.choiceCount() == 0 {
		m.selectedIndex = 0
		return
	}
	if m.selectedIndex >= m.choiceCount() {
		m.selectedIndex = m.choiceCount() - 1
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
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
	lines = append(lines, titleStyle.Render(truncateRunesHelper("Repository Scope", contentWidth, "...")))
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
