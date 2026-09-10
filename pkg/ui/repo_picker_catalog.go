package ui

import "github.com/Dicklesworthstone/beads_viewer/pkg/repository"

// SetCatalog reconciles refreshed repository metadata with the draft. Selection
// is retained by exact ID, while new entries join only an all-repositories draft.
func (m *RepoPickerModel) SetCatalog(catalog repository.Catalog) {
	contextlessCursor := m.currentChoiceIsContextless()
	cursorID := m.currentRepositoryID()
	previousIndex := m.selectedIndex
	available := make(map[string]bool, len(catalog))
	for _, entry := range catalog {
		available[entry.ID] = true
		if m.selectFuture {
			m.selected[entry.ID] = true
		}
	}
	for id := range m.selected {
		if !available[id] {
			delete(m.selected, id)
		}
	}
	m.catalog = append(repository.Catalog(nil), catalog...)
	m.filterCatalog(cursorID)
	if contextlessCursor && m.contextlessMatch {
		m.selectedIndex = 0
	}
	if cursorID == "" || m.currentRepositoryID() != cursorID {
		m.selectedIndex = previousIndex
		m.clampSelection()
	}
}
