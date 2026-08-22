package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
)

func typeFilterIssues() []model.Issue {
	return []model.Issue{
		{ID: "api-bug", Title: "Needle failure", Status: model.StatusOpen, IssueType: model.TypeBug, Labels: []string{"urgent"}},
		{ID: "api-task", Title: "Bug guide needle", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"urgent"}},
		{ID: "api-custom", Title: "Needle decision", Status: model.StatusOpen, IssueType: "decision", Labels: []string{"urgent"}},
		{ID: "api-closed", Title: "Needle closed", Status: model.StatusClosed, IssueType: model.TypeBug, Labels: []string{"urgent"}},
		{ID: "web-bug", Title: "Needle web", Status: model.StatusOpen, IssueType: model.TypeBug, Labels: []string{"urgent"}},
	}
}

func TestTypePickerAppliesExactMultiSelectionAndShowsActiveState(t *testing.T) {
	m := NewModel(typeFilterIssues(), nil, "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("I")})
	m = updated.(Model)
	if !m.showTypePicker || m.focused != focusTypePicker {
		t.Fatalf("type picker did not open: shown=%v focus=%v", m.showTypePicker, m.focused)
	}

	// Sorted choices are bug, decision, task. Start with all selected and remove task.
	m.typePicker.MoveDown()
	m.typePicker.MoveDown()
	m.typePicker.ToggleSelected()
	m = m.handleTypePickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if got := strings.Join(m.activeIssueTypeNames(), ","); got != "bug,decision" {
		t.Fatalf("active issue types = %q", got)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug", "api-custom", "web-bug", "api-closed")
	m.statusMsg = ""
	if footer := m.renderFooter(); !strings.Contains(footer, "TYPE bug,decision") {
		t.Fatalf("active type badge missing: %q", footer)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("I")})
	m = updated.(Model)
	m.typePicker.MoveDown()
	m.typePicker.ToggleSelected()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("I")})
	m = updated.(Model)
	if m.showTypePicker || m.focused != focusList {
		t.Fatalf("uppercase I did not cancel type picker: shown=%v focus=%v", m.showTypePicker, m.focused)
	}
	if got := strings.Join(m.activeIssueTypeNames(), ","); got != "bug,decision" {
		t.Fatalf("uppercase I applied draft type selection: %q", got)
	}
}

func TestTypePickerLeavesBlankRowBeforeWrappedHints(t *testing.T) {
	picker := NewTypePickerModel([]model.IssueType{model.TypeBug, model.TypeTask, "decision"}, nil, DefaultTheme(lipgloss.NewRenderer(nil)))
	picker.SetSize(70, 20)

	lines := strings.Split(picker.View(), "\n")
	for i, line := range lines {
		if strings.Contains(line, string(model.TypeTask)) {
			blankRow := ""
			if i+1 < len(lines) {
				blankRow = strings.Trim(strings.TrimSpace(ansi.Strip(lines[i+1])), "│ ")
			}
			if i+2 >= len(lines) || blankRow != "" || !strings.Contains(ansi.Strip(lines[i+2]), "j/k: navigate") {
				t.Fatalf("type picker lacks blank row before hints:\n%s", picker.View())
			}
			return
		}
	}
	t.Fatalf("type picker did not render task option:\n%s", picker.View())
}

func TestTypeFilterComposesWithStatusLabelRepositoryRecipeAndText(t *testing.T) {
	m := NewModel(typeFilterIssues(), nil, "")
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"}})
	m.SetRepositoryScope(map[string]bool{"api": true})
	m.activeIssueTypes = map[model.IssueType]bool{model.TypeBug: true}
	m.currentFilter = "open"
	m.applyFilter()
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug")

	m.currentFilter = "label:urgent"
	m.applyFilter()
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug", "api-closed")

	r := &recipe.Recipe{Name: "open-only", Filters: recipe.FilterConfig{Status: []string{"open"}}}
	m.setActiveRecipe(r)
	m.applyRecipe(r)
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug")

	m.list.SetFilterText("needle")
	if m.list.FilterState() != list.FilterApplied || m.list.FilterValue() != "needle" {
		t.Fatalf("text filter was not preserved: state=%v value=%q", m.list.FilterState(), m.list.FilterValue())
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug")
}

func TestStatusKeysToggleAndPreserveComposedListFilters(t *testing.T) {
	m := NewModel(typeFilterIssues(), nil, "")
	m.ready = true
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"}})
	m.SetRepositoryScope(map[string]bool{"api": true})
	m.activeIssueTypes = map[model.IssueType]bool{model.TypeBug: true}
	m.currentFilter = "label:urgent"
	m.applyFilter()
	m.list.SetFilterText("needle")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)
	if m.currentFilter != "label:urgent" || m.statusFilter != "closed" {
		t.Fatalf("closed toggle changed composed filters: base=%q status=%q", m.currentFilter, m.statusFilter)
	}
	if m.list.FilterValue() != "needle" || len(m.RepositoryScope()) != 1 || !m.RepositoryScope()["api"] || !m.activeIssueTypes[model.TypeBug] {
		t.Fatalf("closed toggle changed unrelated filters: text=%q repos=%v types=%v", m.list.FilterValue(), m.RepositoryScope(), m.activeIssueTypes)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-closed")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(Model)
	if m.statusFilter != "open" {
		t.Fatalf("switch to open status = %q", m.statusFilter)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(Model)
	if m.statusFilter != "" || m.currentFilter != "label:urgent" {
		t.Fatalf("second open did not clear only status: base=%q status=%q", m.currentFilter, m.statusFilter)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug", "api-closed")
}

func TestStatusFilterBadgeRendersWithComposedFilters(t *testing.T) {
	t.Run("label", func(t *testing.T) {
		m := NewModel(typeFilterIssues(), nil, "")
		m.width = 200
		m.currentFilter = "label:urgent"
		m.applyFilter()

		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
		m = updated.(Model)
		if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "CLOSED") {
			t.Fatalf("label + closed footer missing status badge: %q", footer)
		}

		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
		m = updated.(Model)
		if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "OPEN") {
			t.Fatalf("label + open footer missing status badge: %q", footer)
		}

		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
		m = updated.(Model)
		footer := ansi.Strip(m.renderFooter())
		if strings.Contains(footer, "OPEN") || strings.Contains(footer, "CLOSED") || strings.Contains(footer, "READY") || !strings.Contains(footer, "label:urgent") {
			t.Fatalf("cleared label status changed footer filter: %q", footer)
		}
	})

	t.Run("recipe", func(t *testing.T) {
		m := NewModel(typeFilterIssues(), nil, "")
		m.width = 200
		r := &recipe.Recipe{Name: "urgent", Filters: recipe.FilterConfig{Tags: []string{"urgent"}}}
		m.setActiveRecipe(r)
		m.applyRecipe(r)

		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
		m = updated.(Model)
		if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "CLOSED") {
			t.Fatalf("recipe + closed footer missing status badge: %q", footer)
		}

		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
		m = updated.(Model)
		if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "OPEN") {
			t.Fatalf("recipe + open footer missing status badge: %q", footer)
		}

		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
		m = updated.(Model)
		footer := ansi.Strip(m.renderFooter())
		if strings.Contains(footer, "OPEN") || strings.Contains(footer, "CLOSED") || strings.Contains(footer, "READY") || !strings.Contains(footer, "URGENT") {
			t.Fatalf("cleared recipe status changed footer filter: %q", footer)
		}
	})
}

func TestStatusKeysRenderNormalFooterImmediately(t *testing.T) {
	footerLine := func(view string) string {
		view = strings.TrimRight(view, "\n")
		if index := strings.LastIndex(view, "\n"); index >= 0 {
			return view[index+1:]
		}
		return view
	}

	for _, testCase := range []struct {
		name  string
		key   string
		badge string
	}{
		{name: "closed", key: "c", badge: "CLOSED"},
		{name: "open", key: "o", badge: "OPEN"},
		{name: "ready", key: "r", badge: "READY"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			m := NewModel(typeFilterIssues(), nil, "")
			m.width, m.height = 200, 40
			m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"}})
			m.SetRepositoryScope(map[string]bool{"api": true})

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(testCase.key)})
			m = updated.(Model)
			view := ansi.Strip(m.View())
			footer := footerLine(view)
			if !strings.Contains(footer, "📦 api") || !strings.Contains(footer, testCase.badge) {
				t.Fatalf("immediate %s view missing persistent badges:\n%s", testCase.key, view)
			}
			if !strings.Contains(footer, "⏎ details") || !strings.Contains(footer, "Ctrl+R refresh") {
				t.Fatalf("immediate %s view missing normal shortcut hints:\n%s", testCase.key, view)
			}
			if strings.Contains(footer, "Filter: ") {
				t.Fatalf("immediate %s view retained transient status message:\n%s", testCase.key, view)
			}

			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(testCase.key)})
			m = updated.(Model)
			view = ansi.Strip(m.View())
			footer = footerLine(view)
			for _, badge := range []string{"CLOSED", "OPEN", "READY"} {
				if strings.Contains(footer, badge) {
					t.Fatalf("cleared %s view retained status badge %q:\n%s", testCase.key, badge, view)
				}
			}
			if !strings.Contains(footer, "⏎ details") || !strings.Contains(footer, "Ctrl+R refresh") {
				t.Fatalf("cleared %s view missing normal shortcut hints:\n%s", testCase.key, view)
			}
		})
	}
}

func TestStatusTogglePreservesRecipeAndBoardContract(t *testing.T) {
	m := NewModel(typeFilterIssues(), nil, "")
	r := &recipe.Recipe{Name: "urgent", Filters: recipe.FilterConfig{Tags: []string{"urgent"}}}
	m.setActiveRecipe(r)
	m.applyRecipe(r)

	m = m.handleListKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.activeRecipe != r || m.currentFilter != "recipe:urgent" || m.statusFilter != "closed" {
		t.Fatalf("list status toggle changed recipe: active=%p base=%q status=%q", m.activeRecipe, m.currentFilter, m.statusFilter)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-closed")

	m = m.handleBoardKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.activeRecipe != r || m.currentFilter != "recipe:urgent" || m.statusFilter != "" {
		t.Fatalf("board second toggle did not clear only status: active=%p base=%q status=%q", m.activeRecipe, m.currentFilter, m.statusFilter)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug", "api-custom", "api-task", "web-bug", "api-closed")
}

func TestTypeFilterResetPreservesUnrelatedScopeAndSearch(t *testing.T) {
	m := NewModel(typeFilterIssues(), nil, "")
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"}})
	m.SetRepositoryScope(map[string]bool{"api": true})
	m.currentFilter = "open"
	m.activeIssueTypes = map[model.IssueType]bool{model.TypeBug: true}
	m.applyFilter()
	m.list.SetFilterText("needle")

	m.typePicker = NewTypePickerModel(issueTypesFromIssues(m.issues, m.activeIssueTypes), m.activeIssueTypes, m.theme)
	m.typePicker.ClearSelection()
	m = m.handleTypePickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if m.activeIssueTypes != nil || m.currentFilter != "open" {
		t.Fatalf("type reset changed filters: types=%v status=%q", m.activeIssueTypes, m.currentFilter)
	}
	if scope := m.RepositoryScope(); len(scope) != 1 || !scope["api"] {
		t.Fatalf("type reset changed repository scope: %#v", scope)
	}
	if m.list.FilterValue() != "needle" {
		t.Fatalf("type reset changed text search: %q", m.list.FilterValue())
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug", "api-custom", "api-task")
}

func TestTypeFilterResetPreservesRecipe(t *testing.T) {
	m := NewModel(typeFilterIssues()[:2], nil, "")
	r := &recipe.Recipe{Name: "open-only", Filters: recipe.FilterConfig{Status: []string{"open"}}}
	m.setActiveRecipe(r)
	m.applyRecipe(r)
	m.activeIssueTypes = map[model.IssueType]bool{model.TypeBug: true}
	m.applyRecipe(r)

	m.typePicker = NewTypePickerModel(issueTypesFromIssues(m.issues, m.activeIssueTypes), m.activeIssueTypes, m.theme)
	m.typePicker.ClearSelection()
	m = m.handleTypePickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if m.activeRecipe != r || m.currentFilter != "recipe:open-only" {
		t.Fatalf("type reset changed recipe: active=%p want=%p filter=%q", m.activeRecipe, r, m.currentFilter)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "api-bug", "api-task")
}

func TestTypeFilterSurvivesSnapshotRefresh(t *testing.T) {
	m := NewModel(typeFilterIssues()[:2], nil, "")
	m.activeIssueTypes = map[model.IssueType]bool{model.TypeBug: true}
	m.applyFilter()

	snapshot := NewSnapshotBuilder([]model.Issue{
		{ID: "new-bug", Title: "New bug", Status: model.StatusOpen, IssueType: model.TypeBug},
		{ID: "new-kind", Title: "New kind", Status: model.StatusOpen, IssueType: "incident"},
	}).Build()
	updated, _ := m.Update(SnapshotReadyMsg{Snapshot: snapshot})
	m = updated.(Model)
	if !m.activeIssueTypes[model.TypeBug] {
		t.Fatalf("active type selection was lost: %#v", m.activeIssueTypes)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "new-bug")

	m.typePicker = NewTypePickerModel(issueTypesFromIssues(m.issues, m.activeIssueTypes), m.activeIssueTypes, m.theme)
	if view := m.typePicker.View(); !strings.Contains(view, "incident") {
		t.Fatalf("refreshed arbitrary issue type missing from picker: %q", view)
	}
}

func TestBareTypeNameRemainsFuzzySearchText(t *testing.T) {
	m := NewModel(typeFilterIssues(), nil, "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bug")})
	m = updated.(Model)
	if len(m.activeIssueTypes) != 0 || m.list.FilterValue() != "bug" {
		t.Fatalf("bare text became a type filter: types=%v text=%q", m.activeIssueTypes, m.list.FilterValue())
	}
	visible := m.list.VisibleItems()
	foundTask := false
	for _, item := range visible {
		if item.(IssueItem).Issue.ID == "api-task" {
			foundTask = true
		}
	}
	if !foundTask {
		t.Fatalf("ordinary fuzzy search omitted task whose title contains bug: %#v", visible)
	}
}

func TestTypePickerRenderingFitsTinyAndBoundaryAllocations(t *testing.T) {
	wideType := model.IssueType("事故調査種別")
	picker := NewTypePickerModel(
		[]model.IssueType{model.TypeBug, wideType},
		map[model.IssueType]bool{wideType: true},
		DefaultTheme(lipgloss.NewRenderer(nil)),
	)
	picker.MoveDown()

	tests := []struct {
		name          string
		width, height int
	}{
		{name: "single cell", width: 1, height: 1},
		{name: "narrow compact", width: 7, height: 4},
		{name: "width boundary short", width: 14, height: 5},
		{name: "width boundary tall compact", width: 14, height: 8},
		{name: "minimum modal", width: 14, height: 9},
		{name: "wide name narrow modal", width: 18, height: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			picker.SetSize(tt.width, tt.height)
			view := picker.View()
			if width := lipgloss.Width(view); width > tt.width {
				t.Fatalf("rendered width = %d, allocation = %d:\n%s", width, tt.width, view)
			}
			if height := lipgloss.Height(view); height > tt.height {
				t.Fatalf("rendered height = %d, allocation = %d:\n%s", height, tt.height, view)
			}
		})
	}

	selected := picker.SelectedTypes()
	if len(selected) != 1 || !selected[wideType] {
		t.Fatalf("rendering changed exact selection: %#v", selected)
	}
}

func TestTypePickerFooterHintsDoNotTruncateAtWideModalWidth(t *testing.T) {
	picker := NewTypePickerModel(
		[]model.IssueType{model.TypeBug, model.TypeTask, model.TypeFeature},
		map[model.IssueType]bool{model.TypeBug: true, model.TypeTask: true, model.TypeFeature: true},
		DefaultTheme(lipgloss.NewRenderer(nil)),
	)
	picker.SetSize(100, 20)

	view := picker.View()
	for _, hint := range []string{"j/k: navigate", "space: toggle", "a: all", "n: reset", "enter: apply", "esc: cancel"} {
		if !strings.Contains(view, hint) {
			t.Fatalf("expected wrapped footer to include control hint %q in picker view", hint)
		}
	}
	if strings.Count(view, "…") > 1 {
		t.Fatalf("footer appears truncated in modal width=100; view=%q", view)
	}
}

func TestTypePickerFooterFitsAtMinimumFullModalSize(t *testing.T) {
	picker := NewTypePickerModel(
		[]model.IssueType{model.TypeBug, model.TypeTask, model.TypeFeature},
		map[model.IssueType]bool{model.TypeBug: true},
		DefaultTheme(lipgloss.NewRenderer(nil)),
	)

	for _, width := range []int{14, 18, 24} {
		picker.SetSize(width, 12)
		view := picker.View()
		if got := lipgloss.Width(view); got > width {
			t.Fatalf("width %d rendered width = %d:\n%s", width, got, view)
		}
		if got := lipgloss.Height(view); got > 12 {
			t.Fatalf("width %d rendered height = %d:\n%s", width, got, view)
		}
	}
}

func TestEmptyTypePickerFitsAtMinimumFullModalSize(t *testing.T) {
	picker := NewTypePickerModel(nil, nil, DefaultTheme(lipgloss.NewRenderer(nil)))
	picker.SetSize(14, 12)

	view := picker.View()
	if got := lipgloss.Width(view); got > 14 {
		t.Fatalf("empty picker rendered width = %d:\n%s", got, view)
	}
	if got := lipgloss.Height(view); got > 12 {
		t.Fatalf("empty picker rendered height = %d:\n%s", got, view)
	}
}
