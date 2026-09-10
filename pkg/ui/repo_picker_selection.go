package ui

import "github.com/Dicklesworthstone/beads_viewer/pkg/repository"

// SetActiveRepos initializes the draft from exact repository IDs; nil selects all.
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
		for _, entry := range m.catalog {
			m.selected[entry.ID] = true
		}
		return
	}
	for _, entry := range m.catalog {
		if active[entry.ID] {
			m.selected[entry.ID] = true
		}
	}
}

// SetRepositorySelection projects a neutral selection into the picker draft.
func (m *RepoPickerModel) SetRepositorySelection(selection repository.Selection) {
	m.showContextless = true
	contextlessSelected := selection.Mode() == repository.SelectionAll ||
		selection.Mode() == repository.SelectionUnassigned || selection.IncludesUnassigned()
	switch selection.Mode() {
	case repository.SelectionUnassigned:
		m.selected = make(map[string]bool)
		m.selectFuture = false
	case repository.SelectionSelected:
		selected := make(map[string]bool, len(selection.IDs()))
		for _, id := range selection.IDs() {
			selected[id] = true
		}
		m.SetActiveRepos(selected)
	default:
		m.SetActiveRepos(nil)
	}
	m.contextlessSelected = contextlessSelected
	m.filterCatalog("")
}

// SetContextlessBeadCount updates the count shown for unassigned items.
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

// ToggleSelected toggles the selected state of the current choice.
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
		for _, entry := range m.catalog {
			if !m.selected[entry.ID] {
				m.selectFuture = false
				break
			}
		}
	}
}

// SelectAll selects every repository and the unassigned choice when shown.
func (m *RepoPickerModel) SelectAll() {
	m.contextlessSelected = m.showContextless
	for _, entry := range m.catalog {
		m.selected[entry.ID] = true
	}
	m.selectFuture = true
}

// ToggleAll switches between every available choice and an empty draft.
func (m *RepoPickerModel) ToggleAll() {
	allSelected := !m.showContextless || m.contextlessSelected
	for _, entry := range m.catalog {
		if !m.selected[entry.ID] {
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

// SelectCurrent selects only the authoritative current repository.
func (m *RepoPickerModel) SelectCurrent() {
	if m.currentID == "" || m.currentID == contextlessRepositoryID {
		return
	}
	for _, entry := range m.catalog {
		if entry.ID != m.currentID {
			continue
		}
		m.selected = map[string]bool{m.currentID: true}
		m.selectFuture = false
		m.contextlessSelected = false
		return
	}
}

// SelectedRepos returns selected exact repository IDs as a detached map.
func (m RepoPickerModel) SelectedRepos() map[string]bool {
	selected := make(map[string]bool)
	for _, entry := range m.catalog {
		if m.selected[entry.ID] {
			selected[entry.ID] = true
		}
	}
	return selected
}

// RepositorySelection converts the draft into the neutral repository value.
func (m RepoPickerModel) RepositorySelection() (repository.Selection, error) {
	selected := m.SelectedRepos()
	if len(selected) == 0 {
		if m.contextlessSelected {
			return repository.NewUnassignedSelection(), nil
		}
		return repository.NewAllSelection(), nil
	}
	if m.contextlessSelected && len(selected) == len(m.catalog) {
		return repository.NewAllSelection(), nil
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	if m.contextlessSelected {
		return repository.NewSelectedAndUnassignedSelection(ids)
	}
	return repository.NewSelectedSelection(ids)
}

func (m RepoPickerModel) ContextlessSelected() bool { return m.contextlessSelected }
