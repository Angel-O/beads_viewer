package ui

import (
	"sort"
	"strings"

	repository "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *RepoPickerModel) setSearchWidth(width int) {
	inputWidth := width - 24
	if inputWidth > 56 {
		inputWidth = 56
	}
	if inputWidth < 12 {
		inputWidth = 12
	}
	m.searchInput.Width = inputWidth
}

// BeginSearch focuses the picker search input.
func (m *RepoPickerModel) BeginSearch() {
	m.searching = true
	m.searchInput.Focus()
}

func (m RepoPickerModel) IsSearching() bool { return m.searching }

// ClearSearch removes the query and restores the complete catalog.
func (m *RepoPickerModel) ClearSearch() {
	m.searching = false
	m.searchInput.SetValue("")
	m.searchInput.Blur()
	m.filterCatalog("")
}

// UpdateSearch applies one key message and keeps the current repository when possible.
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
		m.filtered = append(repository.Catalog(nil), m.catalog...)
	} else {
		type scoredRepository struct {
			repository repository.CatalogEntry
			score      int
		}
		matches := make([]scoredRepository, 0, len(m.catalog))
		for _, entry := range m.catalog {
			score := 0
			for _, candidate := range []string{entry.Name, entry.Path, entry.Detail, entry.ID} {
				if candidateScore := fuzzyScore(candidate, query); candidateScore > score {
					score = candidateScore
				}
			}
			if score > 0 {
				matches = append(matches, scoredRepository{repository: entry, score: score})
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
		m.filtered = make(repository.Catalog, len(matches))
		for i, match := range matches {
			m.filtered[i] = match.repository
		}
	}
	m.selectedIndex = 0
	offset := 0
	if m.contextlessMatch {
		offset = 1
	}
	for i, entry := range m.filtered {
		if entry.ID == preferredID {
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
