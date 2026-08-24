package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestHubListRepositoryWidthStaysStableAcrossStatusToggles(t *testing.T) {
	issues := []model.Issue{
		{ID: "long", Title: "Long repository", Status: model.StatusOpen, Labels: []string{"ctx:long"}},
		{ID: "short", Title: "Short repository", Status: model.StatusClosed, Labels: []string{"ctx:s"}},
		{
			ID:        strings.Repeat("very-long-id-", 5),
			Title:     "Badge-heavy hidden row",
			Status:    model.StatusClosed,
			Labels:    []string{"ctx:s"},
			Comments:  []*model.Comment{{ID: "comment", Text: "comment"}},
			IssueType: model.TypeTask,
		},
	}
	catalog := model.RepositoryCatalog{
		{ID: "ctx:long", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:s", Name: "s", Kind: model.RepositoryIdentityHubContext},
	}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(80, 10)
	m.quickWinSet = map[string]bool{issues[2].ID: true}
	m.unblocksMap = map[string][]string{issues[2].ID: {"hidden-a", "hidden-b"}}
	m.refreshRepositoryPresentation()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(Model)

	delegateWidth := func() int {
		t.Helper()
		nameWidth, _ := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
		return nameWidth
	}
	renderRows := func() {
		t.Helper()
		for _, row := range strings.Split(m.list.View(), "\n") {
			if width := lipgloss.Width(row); width > m.list.Width() {
				t.Fatalf("rendered row width = %d, terminal width = %d: %q", width, m.list.Width(), row)
			}
		}
	}

	stableWidth := delegateWidth()
	if stableWidth != lipgloss.Width("beads_viewer") {
		t.Fatalf("initial repository width = %d, want full width %d", stableWidth, lipgloss.Width("beads_viewer"))
	}
	if !strings.Contains(m.list.View(), "[beads_viewer]") {
		t.Fatalf("normal list omitted full repository label:\n%s", m.list.View())
	}
	renderRows()

	transitions := []struct {
		key       rune
		wantIDs   []string
		wantLabel string
	}{
		{key: 'o', wantIDs: []string{"long", "short", issues[2].ID}, wantLabel: "beads_viewer"},
		{key: 'o', wantIDs: []string{"long"}, wantLabel: "beads_viewer"},
		{key: 'c', wantIDs: []string{"short", issues[2].ID}, wantLabel: "s"},
		{key: 'c', wantIDs: []string{"long", "short", issues[2].ID}, wantLabel: "beads_viewer"},
		{key: 'r', wantIDs: []string{"long"}, wantLabel: "beads_viewer"},
		{key: 'r', wantIDs: []string{"long", "short", issues[2].ID}, wantLabel: "beads_viewer"},
	}
	for _, transition := range transitions {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{transition.key}})
		m = updated.(Model)
		if got := visibleIssueIDs(m); !reflect.DeepEqual(got, transition.wantIDs) {
			t.Fatalf("key %q visible IDs = %v, want %v", transition.key, got, transition.wantIDs)
		}
		if got := delegateWidth(); got != stableWidth {
			t.Fatalf("key %q changed repository width from %d to %d", transition.key, stableWidth, got)
		}
		renderRows()
		if transition.wantLabel != "" && !strings.Contains(m.list.View(), "["+transition.wantLabel+"]") {
			t.Fatalf("key %q list omitted repository label %q:\n%s", transition.key, transition.wantLabel, m.list.View())
		}
	}

	narrow := NewModel(issues, nil, "")
	narrow.hubConfigPath = "hub.yaml"
	narrow.repositoryCatalog = catalog
	narrow.list.SetSize(55, 10)
	narrow.refreshRepositoryPresentation()
	narrowWidth, _ := narrow.repositoryListColumnWidths(IssueDelegate{Theme: narrow.theme})
	if got := narrowWidth; got > stableWidth {
		t.Fatalf("narrow terminal widened repository column to %d from %d", got, stableWidth)
	}
	for _, row := range strings.Split(narrow.list.View(), "\n") {
		if width := lipgloss.Width(row); width > narrow.list.Width() {
			t.Fatalf("narrow rendered row width = %d, terminal width = %d: %q", width, narrow.list.Width(), row)
		}
	}
}

func TestHubListRepositoryWidthUsesActiveScopeOnly(t *testing.T) {
	issues := []model.Issue{
		{ID: "short", Title: "Short repository", Status: model.StatusOpen, Labels: []string{"ctx:s"}},
		{ID: "inbox", Title: "Contextless", Status: model.StatusOpen},
	}
	catalog := model.RepositoryCatalog{
		{ID: "ctx:s", Name: "s", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:inactive", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext},
	}

	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	scope, err := model.NewSelectedContextsHubScope([]string{"ctx:s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	nameWidth, _ := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if nameWidth != lipgloss.Width("s") {
		t.Fatalf("active short-scope repository width = %d, want %d", nameWidth, lipgloss.Width("s"))
	}
	if strings.Contains(m.list.View(), "beads_viewer") {
		t.Fatalf("inactive repository widened or leaked into short scope:\n%s", m.list.View())
	}

	contextless := NewModel(issues, nil, "")
	contextless.hubConfigPath = "hub.yaml"
	contextless.repositoryCatalog = catalog
	contextless.list.SetSize(120, 10)
	if err := contextless.SetHubScope(model.NewContextlessHubScope()); err != nil {
		t.Fatal(err)
	}
	nameWidth, _ = contextless.repositoryListColumnWidths(IssueDelegate{Theme: contextless.theme})
	if nameWidth != lipgloss.Width(contextlessRepositoryID) {
		t.Fatalf("contextless repository width = %d, want %d", nameWidth, lipgloss.Width(contextlessRepositoryID))
	}
}

func TestHubListExtraWidthUsesRenderedInactiveContexts(t *testing.T) {
	const activeID = "ctx:active"
	issues := []model.Issue{
		{ID: "multi-1", Title: "Multi-context", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{activeID}},
		{ID: "single1", Title: "Single-context", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{activeID}},
	}
	catalog := model.RepositoryCatalog{{ID: activeID, Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext}}
	for index := 0; index < 12; index++ {
		contextID := fmt.Sprintf("ctx:inactive-%02d", index)
		catalog = append(catalog, model.RepositoryCatalogEntry{
			ID: contextID, Name: fmt.Sprintf("repo-%02d", index), Kind: model.RepositoryIdentityHubContext,
		})
		issues[0].Labels = append(issues[0].Labels, contextID)
	}

	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	scope, err := model.NewSelectedContextsHubScope([]string{activeID})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}

	nameWidth, extraWidth := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if nameWidth != lipgloss.Width("beads_viewer") || extraWidth != lipgloss.Width("+12") {
		t.Fatalf("selected scope columns = name:%d extra:%d, want name:%d extra:%d", nameWidth, extraWidth, lipgloss.Width("beads_viewer"), lipgloss.Width("+12"))
	}
	view := m.list.View()
	if !strings.Contains(view, "[beads_viewer]") || !strings.Contains(view, "+12") {
		t.Fatalf("multi-context row omitted rendered repository metadata:\n%s", view)
	}
	wantPrefixWidth := -1
	for _, id := range []string{"multi-1", "single1"} {
		var row string
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, id) {
				row = line
				break
			}
		}
		if row == "" {
			t.Fatalf("missing rendered row for %s:\n%s", id, view)
		}
		priorityIndex := strings.Index(row, "P0")
		if priorityIndex < 0 {
			t.Fatalf("missing priority column for %s: %q", id, row)
		}
		prefixWidth := lipgloss.Width(row[:priorityIndex])
		if wantPrefixWidth < 0 {
			wantPrefixWidth = prefixWidth
		} else if prefixWidth != wantPrefixWidth {
			t.Fatalf("repository columns drifted for %s: got %d, want %d; row %q", id, prefixWidth, wantPrefixWidth, row)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > m.list.Width() {
			t.Fatalf("rendered row width = %d, terminal width = %d: %q", width, m.list.Width(), line)
		}
	}

	allItems := NewModel([]model.Issue{{ID: "only-active", Title: "Only active", Status: model.StatusOpen, Labels: []string{activeID}}}, nil, "")
	allItems.hubConfigPath = "hub.yaml"
	allItems.repositoryCatalog = catalog
	allItems.list.SetSize(120, 10)
	allNameWidth, allExtraWidth := allItems.repositoryListColumnWidths(IssueDelegate{Theme: allItems.theme})
	if allNameWidth != lipgloss.Width("beads_viewer") || allExtraWidth != 0 {
		t.Fatalf("all-items unused extra reservation = name:%d extra:%d, want name:%d extra:0", allNameWidth, allExtraWidth, lipgloss.Width("beads_viewer"))
	}
}
