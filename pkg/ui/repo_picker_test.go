package ui

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func testRepositoryCatalog() model.RepositoryCatalog {
	return model.RepositoryCatalog{
		{ID: "ctx:alpha-123", Name: "alpha", Path: "/work/services/alpha", BeadCount: 12},
		{ID: "ctx:beta-456", Name: "beta", Path: "/work/services/beta", BeadCount: 3},
		{ID: "ctx:gamma-789", Name: "gamma", Path: "/work/tools/gamma", BeadCount: 0},
	}
}

func TestRepoPickerSelectionAndToggle(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(80, 24)

	// Default is all selected
	if got := len(m.SelectedRepos()); got != 3 {
		t.Fatalf("expected 3 selected repos by default, got %d", got)
	}

	// Toggle first repo off
	m.ToggleSelected()
	if got := len(m.SelectedRepos()); got != 2 {
		t.Fatalf("expected 2 selected after toggle, got %d", got)
	}

	// Select all
	m.SelectAll()
	if got := len(m.SelectedRepos()); got != 3 {
		t.Fatalf("expected 3 selected after SelectAll, got %d", got)
	}
}

func TestRepoPickerViewContainsRepos(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog()[:1], DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(60, 20)

	out := m.View()
	if !strings.Contains(out, "Repository Scope") {
		t.Fatalf("expected title in view, got:\n%s", out)
	}
	for _, want := range []string{"alpha", "ctx:alpha-123", "12"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in view, got:\n%s", want, out)
		}
	}
}

func TestRepoPickerMarksAuthoritativeCurrentRepository(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetHubScope(model.NewAllItemsHubScope())
	m.SetCurrentRepository("ctx:beta-456")
	m.SetSize(120, 24)

	out := m.View()
	if strings.Count(out, "current (3)") != 1 {
		t.Fatalf("current repository marker count = %d, want one:\n%s", strings.Count(out, "current (3)"), out)
	}
	if strings.Contains(out, "no-context current") {
		t.Fatalf("no-context row was marked current:\n%s", out)
	}

	m.MoveDown()
	if !strings.Contains(m.View(), "beta current (3)") || strings.Contains(m.View(), "alpha current") {
		t.Fatalf("marker moved with cursor:\n%s", m.View())
	}

	m.BeginSearch()
	m.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("beta")})
	if !strings.Contains(m.View(), "beta current (3)") {
		t.Fatalf("current marker was lost in filtered result:\n%s", m.View())
	}
}

func TestRepoPickerCurrentMarkerIsBoldWithoutChangingPlainText(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(renderer))
	m.SetHubScope(model.NewAllItemsHubScope())
	m.SetCurrentRepository("ctx:beta-456")
	m.SetSize(120, 24)

	view := m.View()
	var currentLine, nonCurrentLine string
	for _, line := range strings.Split(view, "\n") {
		plain := ansi.Strip(line)
		switch {
		case strings.Contains(plain, "beta current (3)"):
			currentLine = line
		case strings.Contains(plain, "gamma (0)"):
			nonCurrentLine = line
		}
	}
	if currentLine == "" || nonCurrentLine == "" {
		t.Fatalf("picker rows missing from view:\n%s", ansi.Strip(view))
	}
	if !strings.Contains(currentLine, "\x1b[1m current\x1b[0m") {
		t.Fatalf("current marker was not rendered bold: %q", currentLine)
	}
	if strings.Contains(nonCurrentLine, "\x1b[1m") {
		t.Fatalf("non-current repository row unexpectedly contains bold styling: %q", nonCurrentLine)
	}
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "beta current (3)") || strings.Contains(plain, "*") {
		t.Fatalf("plain picker content changed: %q", plain)
	}
}

func TestRepoPickerCurrentMarkerTruncatesPlainRowBeforeStyling(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)
	m := NewRepoPickerModel(model.RepositoryCatalog{{
		ID: "ctx:long", Name: "repository-long-name", BeadCount: 42,
	}}, DefaultTheme(renderer))
	scope, err := model.NewSelectedContextsHubScope([]string{"ctx:long"})
	if err != nil {
		t.Fatal(err)
	}
	m.SetHubScope(scope)
	m.SetCurrentRepository("ctx:long")
	m.SetSize(50, 10)
	m.MoveDown()

	view := m.View()
	plainView := ansi.Strip(view)
	wantRow := "▸ [x] repository-long-name current (42)"
	var plainRow string
	for _, line := range strings.Split(plainView, "\n") {
		if strings.Contains(line, "repository-long-name") {
			plainRow = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│"))
			break
		}
	}
	if plainRow != wantRow {
		t.Fatalf("ANSI-stripped current row = %q, want %q; full view:\n%s", plainRow, wantRow, plainView)
	}
	if strings.Contains(plainRow, "...") || strings.Contains(plainRow, "…") {
		t.Fatalf("current row gained unintended ellipsis: %q", plainRow)
	}
	if !strings.Contains(view, "\x1b[1m current\x1b[0m") {
		t.Fatalf("current marker lost bold styling: %q", view)
	}
}

func TestRepoPickerCurrentOnlyClearsOtherDraftChoices(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetHubScope(model.NewAllItemsHubScope())
	m.SetCurrentRepository("ctx:beta-456")
	m.SelectCurrent()

	selected := m.SelectedRepos()
	if len(selected) != 1 || !selected["ctx:beta-456"] || m.ContextlessSelected() || m.selectFuture {
		t.Fatalf("current-only draft = repos=%v contextless=%v future=%v", selected, m.ContextlessSelected(), m.selectFuture)
	}
}

func TestRepoPickerCurrentOnlyAbsentIsNoOp(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetHubScope(model.NewAllItemsHubScope())
	m.SetCurrentRepository("ctx:missing")
	before := m.SelectedRepos()
	beforeContextless := m.ContextlessSelected()
	m.SelectCurrent()

	if got := m.SelectedRepos(); len(got) != len(before) || !got["ctx:alpha-123"] || !got["ctx:beta-456"] || !got["ctx:gamma-789"] || m.ContextlessSelected() != beforeContextless {
		t.Fatalf("absent current changed draft: repos=%v contextless=%v", got, m.ContextlessSelected())
	}
}

func TestRepoPickerCurrentOnlyCancelAndApply(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha-123", "ctx:beta-456", "ctx:gamma-789")
	m.currentRepositoryID = "ctx:beta-456"
	if err := m.SetHubScope(model.NewAllItemsHubScope()); err != nil {
		t.Fatal(err)
	}
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetCurrentRepository(m.currentRepositoryID)
	m.repoPicker.SetHubScope(model.NewAllItemsHubScope())
	m.showRepoPicker = true
	m.focused = focusRepoPicker

	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if m.HubScope().Mode != model.HubScopeAllItems {
		t.Fatalf("cancel applied current-only draft: %#v", m.HubScope())
	}

	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetCurrentRepository(m.currentRepositoryID)
	m.repoPicker.SetHubScope(model.NewAllItemsHubScope())
	m.showRepoPicker = true
	m.focused = focusRepoPicker
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	scope := m.HubScope()
	if scope.Mode != model.HubScopeSelectedContexts || len(scope.Contexts) != 1 || scope.Contexts[0] != "ctx:beta-456" || scope.IncludeContextless {
		t.Fatalf("apply current-only scope = %#v", scope)
	}
}

func TestRepoPickerCurrentOnlyRemainsSearchText(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.repositoryCatalog = testRepositoryCatalog()
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetCurrentRepository("ctx:beta-456")
	m.repoPicker.BeginSearch()
	m.showRepoPicker = true

	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.repoPicker.SearchValue() != "c" || len(m.repoPicker.SelectedRepos()) != len(m.repositoryCatalog) {
		t.Fatalf("search input did not own c: query=%q selected=%v", m.repoPicker.SearchValue(), m.repoPicker.SelectedRepos())
	}
}

func TestRepoPickerCurrentOnlyHintFitsNormalWidth(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(120, 24)
	footer := "j/k: navigate | space: toggle | a: all | n: clear | c: current only | /: search | enter: apply | esc: cancel"
	if out := m.View(); !strings.Contains(out, footer) {
		t.Fatalf("normal picker footer was truncated:\n%s", out)
	}
}

func TestHubRepositoryPickerShowsContextlessBeadCount(t *testing.T) {
	issues := []model.Issue{
		{ID: "contextless-1"},
		{ID: "contextless-2", Labels: []string{"backend"}},
		{ID: "alpha", Labels: []string{"ctx:alpha"}},
		{ID: "unknown", Labels: []string{"ctx:unknown"}},
	}
	m := NewModel(issues, nil, "")
	m.ready = true
	m.hubRepositoryMode = true
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 1}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)
	if !m.showRepoPicker {
		t.Fatal("repository picker did not open")
	}
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (2)") {
		t.Fatalf("contextless picker row missing bead count:\n%s", picker)
	}
}

func TestHubRepositoryPickerRefreshesContextlessBeadCountWithSnapshot(t *testing.T) {
	initial := []model.Issue{{ID: "item", Labels: []string{"ctx:alpha"}}}
	m := NewModel(initial, nil, "")
	m.SetRepositoryCatalogIssues(initial)
	m.ready = true
	m.hubRepositoryMode = true
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 1}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (0)") {
		t.Fatalf("initial contextless picker row = %q", picker)
	}

	snapshot := NewSnapshotBuilder([]model.Issue{{ID: "item"}}).Build()
	updated, _ = m.Update(SnapshotReadyMsg{
		Snapshot:          snapshot,
		SnapshotVer:       1,
		Catalog:           model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 0}},
		CatalogGeneration: 1,
		CatalogAvailable:  true,
	})
	m = updated.(Model)
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (1)") {
		t.Fatalf("refreshed contextless picker row = %q", picker)
	}
}

func TestHubRepositoryPickerUsesCompleteCountForOpenOnlySnapshot(t *testing.T) {
	initial := []model.Issue{{ID: "item", Labels: []string{"ctx:alpha"}}}
	m := NewModel(initial, nil, "")
	m.SetRepositoryCatalogIssues(initial)
	m.ready = true
	m.hubRepositoryMode = true
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 1}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)

	completeIssues := []model.Issue{
		{ID: "open", Labels: []string{}},
		{ID: "closed", Status: model.StatusClosed, Labels: []string{}},
	}
	snapshot := NewSnapshotBuilder(completeIssues[:1]).Build()
	snapshot.LoadedOpenOnly = true
	updated, _ = m.Update(SnapshotReadyMsg{
		Snapshot:              snapshot,
		SnapshotVer:           1,
		Catalog:               model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 0}},
		ContextlessBeadCount:  contextlessIssueCount(completeIssues),
		ContextlessCountReady: true,
		CatalogGeneration:     1,
		CatalogAvailable:      true,
	})
	m = updated.(Model)
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (2)") {
		t.Fatalf("open-only picker row omitted closed contextless bead:\n%s", picker)
	}
}

func TestHubRepositoryPickerCatalogFailurePreservesFreshContextlessCount(t *testing.T) {
	initial := []model.Issue{{ID: "item", Labels: []string{"ctx:alpha"}}}
	m := NewModel(initial, nil, "")
	m.SetRepositoryCatalogIssues(initial)
	m.ready = true
	m.hubRepositoryMode = true
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 1}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)
	snapshot := NewSnapshotBuilder([]model.Issue{{ID: "open"}}).Build()
	snapshot.LoadedOpenOnly = true
	updated, _ = m.Update(SnapshotReadyMsg{
		Snapshot:              snapshot,
		SnapshotVer:           1,
		ContextlessBeadCount:  2,
		ContextlessCountReady: true,
		CatalogGeneration:     1,
		CatalogError:          errors.New("catalog unavailable"),
	})
	m = updated.(Model)
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (2)") {
		t.Fatalf("catalog failure lost fresh contextless count:\n%s", picker)
	}
	if !m.statusIsError {
		t.Fatal("catalog failure did not retain error state")
	}

	updated, _ = m.Update(RepositoryCatalogReadyMsg{
		Catalog:               model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 0}},
		ContextlessCountReady: true,
		Generation:            2,
		Recovered:             true,
	})
	m = updated.(Model)
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (0)") {
		t.Fatalf("catalog recovery did not propagate valid zero:\n%s", picker)
	}
	if m.statusIsError {
		t.Fatal("catalog recovery retained error state")
	}
}

func TestHubRepositoryPickerIgnoresStaleSnapshotContextlessCount(t *testing.T) {
	initial := []model.Issue{{ID: "item", Labels: []string{"ctx:alpha"}}}
	m := NewModel(initial, nil, "")
	m.SetRepositoryCatalogIssues(initial)
	m.ready = true
	m.hubRepositoryMode = true
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 1}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(Model)
	updated, _ = m.Update(RepositoryCatalogReadyMsg{
		Catalog:               model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 0}},
		ContextlessCountReady: true,
		Generation:            2,
	})
	m = updated.(Model)

	staleSnapshot := NewSnapshotBuilder([]model.Issue{{ID: "old"}}).Build()
	staleSnapshot.LoadedOpenOnly = true
	updated, _ = m.Update(SnapshotReadyMsg{
		Snapshot:              staleSnapshot,
		SnapshotVer:           1,
		ContextlessBeadCount:  7,
		ContextlessCountReady: true,
		CatalogGeneration:     1,
		CatalogAvailable:      true,
	})
	m = updated.(Model)
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (0)") {
		t.Fatalf("stale snapshot overwrote newer zero contextless count:\n%s", picker)
	}
}

func TestRepoPickerSearchesNamePathAndExactID(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{query: "gmma", want: "ctx:gamma-789"},
		{query: "services/beta", want: "ctx:beta-456"},
		{query: "ctx:alpha-123", want: "ctx:alpha-123"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
			m.BeginSearch()
			m.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.query)})
			if m.FilteredCount() != 1 || m.currentRepositoryID() != tt.want {
				t.Fatalf("query %q matched %d entries at %q", tt.query, m.FilteredCount(), m.currentRepositoryID())
			}
			m.ClearSearch()
			if m.IsSearching() || m.SearchValue() != "" || m.FilteredCount() != 3 {
				t.Fatalf("ClearSearch did not restore catalog: searching=%v query=%q count=%d", m.IsSearching(), m.SearchValue(), m.FilteredCount())
			}
		})
	}
}

func TestRepoPickerCatalogRefreshPreservesExactSelectionAndCursor(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetActiveRepos(map[string]bool{"ctx:alpha-123": true, "ctx:gamma-789": true})
	m.MoveDown()
	m.MoveDown()

	updated := model.RepositoryCatalog{
		{ID: "ctx:gamma-789", Name: "aardvark", BeadCount: 2},
		{ID: "ctx:alpha-123", Name: "alpha", BeadCount: 13},
		{ID: "ctx:new-000", Name: "new", BeadCount: 0},
	}
	m.SetCatalog(updated)
	selected := m.SelectedRepos()
	if m.currentRepositoryID() != "ctx:gamma-789" {
		t.Fatalf("cursor moved to %q, want surviving exact ID", m.currentRepositoryID())
	}
	if len(selected) != 2 || !selected["ctx:alpha-123"] || !selected["ctx:gamma-789"] || selected["ctx:new-000"] {
		t.Fatalf("subset draft after refresh = %#v", selected)
	}
}

func TestRepoPickerCatalogRefreshHonorsAllAndClearedDrafts(t *testing.T) {
	catalog := testRepositoryCatalog()[:1]
	added := append(append(model.RepositoryCatalog(nil), catalog...), model.RepositoryCatalogEntry{ID: "ctx:new", Name: "new"})

	all := NewRepoPickerModel(catalog, DefaultTheme(lipgloss.NewRenderer(nil)))
	all.SetCatalog(added)
	if !all.SelectedRepos()["ctx:new"] {
		t.Fatal("new repository should join an all-repositories draft")
	}

	reselected := NewRepoPickerModel(catalog, DefaultTheme(lipgloss.NewRenderer(nil)))
	reselected.ToggleSelected()
	reselected.ToggleSelected()
	reselected.SetCatalog(added)
	if !reselected.SelectedRepos()["ctx:new"] {
		t.Fatal("new repository should join a draft toggled back to all")
	}

	cleared := NewRepoPickerModel(catalog, DefaultTheme(lipgloss.NewRenderer(nil)))
	cleared.ClearSelection()
	cleared.SetCatalog(added)
	if len(cleared.SelectedRepos()) != 0 {
		t.Fatalf("new repository joined cleared draft: %#v", cleared.SelectedRepos())
	}
}

func TestRepoPickerContextlessChoiceTogglesIndependently(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetHubScope(model.NewAllItemsHubScope())
	if m.FilteredCount() != len(testRepositoryCatalog())+1 || !m.currentChoiceIsContextless() {
		t.Fatalf("contextless choice missing: count=%d current=%q", m.FilteredCount(), m.currentRepositoryID())
	}
	m.ToggleSelected()
	if m.ContextlessSelected() || len(m.SelectedRepos()) != len(testRepositoryCatalog()) {
		t.Fatalf("contextless toggle changed repositories: contextless=%v repos=%v", m.ContextlessSelected(), m.SelectedRepos())
	}
	m.MoveDown()
	m.ToggleSelected()
	if m.ContextlessSelected() || m.SelectedRepos()["ctx:alpha-123"] || len(m.SelectedRepos()) != 2 {
		t.Fatalf("repository toggle changed contextless: contextless=%v repos=%v", m.ContextlessSelected(), m.SelectedRepos())
	}
	m.MoveUp()
	m.ToggleSelected()
	if !m.ContextlessSelected() || len(m.SelectedRepos()) != 2 {
		t.Fatalf("contextless did not compose with subset: contextless=%v repos=%v", m.ContextlessSelected(), m.SelectedRepos())
	}
	m.SelectAll()
	if !m.ContextlessSelected() || len(m.SelectedRepos()) != len(testRepositoryCatalog()) {
		t.Fatalf("SelectAll did not select every checkbox: contextless=%v repos=%v", m.ContextlessSelected(), m.SelectedRepos())
	}
	m.ClearSelection()
	if m.ContextlessSelected() || len(m.SelectedRepos()) != 0 {
		t.Fatalf("ClearSelection did not clear every checkbox: contextless=%v repos=%v", m.ContextlessSelected(), m.SelectedRepos())
	}

	m.BeginSearch()
	m.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("contextless")})
	if m.FilteredCount() != 1 || !m.currentChoiceIsContextless() {
		t.Fatalf("contextless search results: count=%d current=%q", m.FilteredCount(), m.currentRepositoryID())
	}
	m.SetCatalog(append(testRepositoryCatalog(), model.RepositoryCatalogEntry{ID: "ctx:new", Name: "new"}))
	if !m.currentChoiceIsContextless() {
		t.Fatal("catalog refresh moved contextless cursor")
	}
	m.ClearSearch()
	m.BeginSearch()
	m.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("no-context")})
	if m.FilteredCount() != 1 || !m.currentChoiceIsContextless() {
		t.Fatalf("no-context search results: count=%d current=%q", m.FilteredCount(), m.currentRepositoryID())
	}
}

func TestRepoPickerAllItemsAppliesAndReopensWithEveryCheckboxChecked(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha-123", "ctx:beta-456", "ctx:gamma-789")
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetHubScope(model.NewContextlessHubScope())
	m.repoPicker.SelectAll()
	m = m.applyRepositoryPickerSelection()
	if scope := m.HubScope(); scope.Mode != model.HubScopeAllItems {
		t.Fatalf("all-checkbox scope = %#v", scope)
	}

	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetHubScope(m.HubScope())
	if !m.repoPicker.ContextlessSelected() || len(m.repoPicker.SelectedRepos()) != len(m.repositoryCatalog) {
		t.Fatalf("reopened all scope: contextless=%v repos=%v", m.repoPicker.ContextlessSelected(), m.repoPicker.SelectedRepos())
	}

	m.repoPicker.ToggleSelected()
	m = m.applyRepositoryPickerSelection()
	if scope := m.HubScope(); scope.Mode != model.HubScopeSelectedContexts || scope.IncludeContextless || len(scope.Contexts) != len(m.repositoryCatalog) {
		t.Fatalf("repositories-only scope applied as all items: %#v", scope)
	}
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetHubScope(m.HubScope())
	if m.repoPicker.ContextlessSelected() || len(m.repoPicker.SelectedRepos()) != len(m.repositoryCatalog) {
		t.Fatalf("reopened repositories-only scope: contextless=%v repos=%v", m.repoPicker.ContextlessSelected(), m.repoPicker.SelectedRepos())
	}
}

func TestRepoPickerFitsSmallTerminalWhileSearching(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(20, 8)
	m.BeginSearch()
	m.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alpha")})
	out := m.View()
	if width := lipgloss.Width(out); width > 20 {
		t.Fatalf("picker width = %d, want <= 20:\n%s", width, out)
	}
	if height := lipgloss.Height(out); height > 8 {
		t.Fatalf("picker height = %d, want <= 8:\n%s", height, out)
	}
}

func TestRepoPickerUltraCompactBoundariesAndNoMatches(t *testing.T) {
	for _, size := range []struct{ width, height int }{{1, 1}, {8, 4}, {13, 8}} {
		m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
		m.SetSize(size.width, size.height)
		if out := m.View(); lipgloss.Width(out) > size.width || lipgloss.Height(out) > size.height {
			t.Fatalf("picker at %dx%d rendered %dx%d:\n%s", size.width, size.height, lipgloss.Width(out), lipgloss.Height(out), out)
		}
	}

	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(20, 8)
	m.BeginSearch()
	m.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("no-match")})
	out := m.View()
	if !strings.Contains(out, "No mat") || lipgloss.Height(out) > 8 {
		t.Fatalf("tiny no-match picker lacks feedback or overflows:\n%s", out)
	}
}

func TestRepoPickerRequiredKeyBindings(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.repositoryCatalog = testRepositoryCatalog()
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.showRepoPicker = true

	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
	}
	for _, key := range keys {
		m = m.handleRepoPickerKeys(key)
	}
	if m.repoPicker.selectedIndex != 0 {
		t.Fatalf("navigation keys did not round trip to first row: %d", m.repoPicker.selectedIndex)
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeySpace})
	if m.repoPicker.SelectedRepos()["ctx:alpha-123"] {
		t.Fatal("Space did not toggle current repository")
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if len(m.repoPicker.SelectedRepos()) != len(m.repositoryCatalog) {
		t.Fatal("a did not select all repositories")
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyDown})
	if m.repoPicker.selectedIndex != 1 {
		t.Fatal("arrow navigation did not work while searching")
	}
}

func TestRepoPickerScrollsToCursor(t *testing.T) {
	catalog := make(model.RepositoryCatalog, 12)
	for i := range catalog {
		catalog[i] = model.RepositoryCatalogEntry{ID: itoa(i), Name: "repository-" + itoa(i)}
	}
	m := NewRepoPickerModel(catalog, DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(70, 14)
	for range 11 {
		m.MoveDown()
	}
	out := m.View()
	if !strings.Contains(out, "repository-11") || strings.Contains(out, "repository-0 ") {
		t.Fatalf("picker did not scroll to cursor:\n%s", out)
	}
	if !strings.Contains(out, "12/12") {
		t.Fatalf("picker missing scroll position:\n%s", out)
	}
	if lipgloss.Height(out) != 14 {
		t.Fatalf("picker height = %d, want 14", lipgloss.Height(out))
	}
}

func TestRepoPickerKeyFlowSearchEscapeAndEmptyApply(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.repositoryCatalog = testRepositoryCatalog()
	m.hubRepositoryMode = true
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.showRepoPicker = true
	m.focused = focusRepoPicker

	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if len(m.repoPicker.SelectedRepos()) != 0 {
		t.Fatal("n did not clear draft selection")
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("beta")})
	if !m.repoPicker.IsSearching() || m.repoPicker.FilteredCount() != 1 {
		t.Fatalf("search state = %v, matches = %d", m.repoPicker.IsSearching(), m.repoPicker.FilteredCount())
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.showRepoPicker || m.repoPicker.IsSearching() {
		t.Fatal("first Esc should clear search without closing picker")
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if m.showRepoPicker || m.RepositoryScope() != nil {
		t.Fatalf("empty apply should close picker and normalize to all: shown=%v scope=%#v", m.showRepoPicker, m.RepositoryScope())
	}
}

func TestLocalModeRepositoryPickerMessageRecommendsHub(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.ready = true
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	if m.showRepoPicker || !strings.Contains(m.statusMsg, "wbv --hub") {
		t.Fatalf("local w behavior: shown=%v message=%q", m.showRepoPicker, m.statusMsg)
	}
}

func TestWorkspaceRepositoryPickerUsesCatalogAndAppliesPrefixScope(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "api-1", SourceRepo: "api", Title: "API"},
		{ID: "web-1", SourceRepo: "web", Title: "Web"},
	}, nil, "")
	m.ready = true
	m.EnableWorkspaceMode(WorkspaceInfo{
		Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"},
		Repositories: []WorkspaceRepositoryInfo{
			{Name: "API Service", Path: "services/api", Prefix: "api"},
			{Name: "Web App", Path: "apps/web", Prefix: "web"},
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	if !m.showRepoPicker || m.repoPicker.FilteredCount() != 2 {
		t.Fatalf("workspace picker state: shown=%v entries=%d", m.showRepoPicker, m.repoPicker.FilteredCount())
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("services/api")})
	if m.repoPicker.FilteredCount() != 1 || m.repoPicker.currentRepositoryID() != "api" {
		t.Fatalf("workspace path search matched %d at %q", m.repoPicker.FilteredCount(), m.repoPicker.currentRepositoryID())
	}
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyEsc})
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m.repoPicker.ToggleSelected()
	m = m.handleRepoPickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.RepositoryScope()) != 1 || !m.RepositoryScope()["api"] {
		t.Fatalf("workspace scope = %#v, want api", m.RepositoryScope())
	}
}

func TestWorkspaceRepositoryPickerWCancelPreservesAppliedScopeAndSearchInput(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "api-1", SourceRepo: "api", Title: "API"},
		{ID: "web-1", SourceRepo: "web", Title: "Web"},
	}, nil, "")
	m.ready = true
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"}})
	m.SetRepositoryScope(map[string]bool{"api": true})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	m.repoPicker.SelectAll()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	if m.showRepoPicker || m.focused != focusList {
		t.Fatalf("second w did not cancel picker: shown=%v focus=%v", m.showRepoPicker, m.focused)
	}
	if scope := m.RepositoryScope(); len(scope) != 1 || !scope["api"] {
		t.Fatalf("w cancel applied draft repository scope: %#v", scope)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	if !m.showRepoPicker || !m.repoPicker.IsSearching() || m.repoPicker.SearchValue() != "w" {
		t.Fatalf("search did not own printable w: shown=%v searching=%v query=%q", m.showRepoPicker, m.repoPicker.IsSearching(), m.repoPicker.SearchValue())
	}
}

func TestWorkspaceRepositoryPickerWCancelRestoresInsights(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "api-1", SourceRepo: "api", Title: "API"},
		{ID: "web-1", SourceRepo: "web", Title: "Web"},
	}, nil, "")
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"api", "web"}})
	m.SetRepositoryScope(map[string]bool{"api": true})
	m.focused = focusInsights
	m.insightsPanel.focusedPanel = PanelKeystones
	m.insightsPanel.selectedIndex[PanelKeystones] = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	if !m.showRepoPicker || m.repoPickerOrigin != focusInsights {
		t.Fatalf("workspace picker did not open from Insights: shown=%v origin=%v", m.showRepoPicker, m.repoPickerOrigin)
	}
	m.repoPicker.ClearSelection()

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(Model)
	if m.showRepoPicker || m.focused != focusInsights {
		t.Fatalf("w cancel did not restore Insights: shown=%v focus=%v", m.showRepoPicker, m.focused)
	}
	if scope := m.RepositoryScope(); len(scope) != 1 || !scope["api"] {
		t.Fatalf("w cancel applied draft repository scope: %#v", scope)
	}
	if m.insightsPanel.focusedPanel != PanelKeystones || m.insightsPanel.selectedIndex[PanelKeystones] != 1 {
		t.Fatalf("w cancel changed Insights selection: panel=%v index=%d", m.insightsPanel.focusedPanel, m.insightsPanel.selectedIndex[PanelKeystones])
	}
}
