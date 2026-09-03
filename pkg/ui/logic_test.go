package ui

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/drift"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// White-box testing of UI model logic

func TestApplyRecipe_StatusFilter(t *testing.T) {
	issues := []model.Issue{
		{ID: "open", Status: model.StatusOpen},
		{ID: "closed", Status: model.StatusClosed},
		{ID: "tombstone", Status: model.StatusTombstone},
		{ID: "blocked", Status: model.StatusBlocked},
	}
	m := NewModel(issues, nil, "")

	r := &recipe.Recipe{
		Name: "closed-only",
		Filters: recipe.FilterConfig{
			Status: []string{"closed"},
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 2 {
		t.Fatalf("Expected 2 filtered issues, got %d", len(filtered))
	}
	got := map[string]bool{}
	for _, iss := range filtered {
		got[iss.ID] = true
	}
	if !got["closed"] || !got["tombstone"] {
		t.Errorf("Expected issues 'closed' and 'tombstone', got %+v", got)
	}
}

func TestApplyRecipe_PriorityFilter(t *testing.T) {
	issues := []model.Issue{
		{ID: "p1", Status: model.StatusOpen, Priority: 1},
		{ID: "p2", Status: model.StatusOpen, Priority: 2},
	}
	m := NewModel(issues, nil, "")

	r := &recipe.Recipe{
		Filters: recipe.FilterConfig{
			Priority: []int{1},
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 issue, got %d", len(filtered))
	}
	if filtered[0].ID != "p1" {
		t.Errorf("Expected p1, got %s", filtered[0].ID)
	}
}

func TestApplyRecipe_ActionableFilter(t *testing.T) {
	// A blocks B. B is blocked. A is open.
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen},
		{ID: "B", Status: model.StatusBlocked, Dependencies: []*model.Dependency{
			{DependsOnID: "A", Type: model.DepBlocks},
		}},
	}
	m := NewModel(issues, nil, "")

	yes := true
	r := &recipe.Recipe{
		Filters: recipe.FilterConfig{
			Actionable: &yes,
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 actionable issue, got %d", len(filtered))
	}
	if filtered[0].ID != "A" {
		t.Errorf("Expected A (actionable), got %s", filtered[0].ID)
	}
}

func TestRecipe_HasBlockersFilterLiveAndActiveView(t *testing.T) {
	issues := []model.Issue{
		{ID: "root", Status: model.StatusOpen},
		{ID: "unresolved", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "root", Type: model.DepBlocks},
		}},
		{ID: "closed-dependency", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "closed-blocker", Type: model.DepBlocks},
		}},
		{ID: "closed-blocker", Status: model.StatusClosed},
		{ID: "blocked-status-only", Status: model.StatusBlocked},
	}
	m := NewModel(issues, nil, "")

	for _, test := range []struct {
		name  string
		want  []string
		value bool
	}{
		{name: "true finds unresolved dependencies", value: true, want: []string{"unresolved"}},
		{name: "false excludes unresolved dependencies", value: false, want: []string{"root", "closed-dependency", "closed-blocker", "blocked-status-only"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &recipe.Recipe{Name: "blocker-filter", Filters: recipe.FilterConfig{HasBlockers: &test.value}}
			m.setActiveRecipe(r)
			m.applyRecipe(r)
			filtered := m.FilteredIssues()
			got := make([]string, 0, len(filtered))
			for _, issue := range filtered {
				got = append(got, issue.ID)
			}
			sort.Strings(got)
			want := append([]string(nil), test.want...)
			sort.Strings(want)
			requireIssueIDs(t, got, want...)

			m.currentFilter = "recipe:blocker-filter"
			activeView := m.filteredIssuesForActiveView()
			got = got[:0]
			for _, issue := range activeView {
				got = append(got, issue.ID)
			}
			sort.Strings(got)
			requireIssueIDs(t, got, want...)
		})
	}
}

func TestApplyRecipe_Sorting(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Priority: 2},
		{ID: "B", Priority: 1},
		{ID: "C", Priority: 3},
	}
	m := NewModel(issues, nil, "")

	r := &recipe.Recipe{
		Sort: recipe.SortConfig{
			Field:     "priority",
			Direction: "asc",
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 3 {
		t.Fatal("Expected 3 issues")
	}

	// Expect B(1), A(2), C(3)
	if filtered[0].ID != "B" {
		t.Errorf("Expected B first, got %s", filtered[0].ID)
	}
	if filtered[1].ID != "A" {
		t.Errorf("Expected A second, got %s", filtered[1].ID)
	}
	if filtered[2].ID != "C" {
		t.Errorf("Expected C third, got %s", filtered[2].ID)
	}
}

func TestRecipeSortCyclePreservesFilteredIssues(t *testing.T) {
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issues := []model.Issue{
		{ID: "old", Status: model.StatusOpen, Priority: 3, CreatedAt: date, Labels: []string{"focus"}},
		{ID: "new", Status: model.StatusInProgress, Priority: 1, CreatedAt: date.AddDate(0, 0, 2), Labels: []string{"focus"}},
		{ID: "closed", Status: model.StatusClosed, Priority: 2, CreatedAt: date.AddDate(0, 0, 3), Labels: []string{"focus"}},
		{ID: "draft", Status: model.StatusDraft, Priority: 0, CreatedAt: date.AddDate(0, 0, 4)},
	}
	tests := []struct {
		name     string
		recipe   *recipe.Recipe
		wantAsc  []string
		wantDesc []string
	}{
		{
			name: "bottlenecks status filter",
			recipe: &recipe.Recipe{
				Name:    "bottlenecks",
				Filters: recipe.FilterConfig{Status: []string{"open", "in_progress"}},
				Sort:    recipe.SortConfig{Field: "betweenness", Direction: "desc"},
			},
			wantAsc:  []string{"old", "new"},
			wantDesc: []string{"new", "old"},
		},
		{
			name:     "tag filter",
			recipe:   &recipe.Recipe{Name: "focused", Filters: recipe.FilterConfig{Tags: []string{"focus"}}},
			wantAsc:  []string{"old", "new", "closed"},
			wantDesc: []string{"closed", "new", "old"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(append([]model.Issue(nil), issues...), tt.recipe, "")
			for _, expected := range []struct {
				mode SortMode
				ids  []string
			}{
				{SortCreatedAsc, tt.wantAsc},
				{SortCreatedDesc, tt.wantDesc},
			} {
				updated, _ := m.Update(keyMsg("s"))
				m = updated.(*Model)
				if m.sortMode != expected.mode {
					t.Fatalf("sort mode = %v, want %v", m.sortMode, expected.mode)
				}
				got := m.FilteredIssues()
				if len(got) != len(expected.ids) {
					t.Fatalf("filtered issue count = %d, want %d", len(got), len(expected.ids))
				}
				for i, id := range expected.ids {
					if got[i].ID != id {
						t.Fatalf("issue %d = %q, want %q", i, got[i].ID, id)
					}
				}
			}
		})
	}
}

func contextSortCatalog() repositorypkg.Catalog {
	return repositorypkg.Catalog{
		{ID: "ctx:zeta", Name: "Alpha", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:alpha", Name: "Beta", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:gamma", Name: "Gamma", Kind: repositorypkg.IdentityExact},
	}
}

func contextSortIssue(id string, priority int, createdAt time.Time, labels ...string) model.Issue {
	return model.Issue{
		ID: id, Status: model.StatusOpen, Priority: priority, CreatedAt: createdAt, Labels: labels,
	}
}

func TestContextSortCreatedGroupsByCompleteContextSet(t *testing.T) {
	issues := []model.Issue{
		contextSortIssue("beta-old", 2, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "ctx:alpha"),
		contextSortIssue("no-old", 2, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)),
		contextSortIssue("alpha", 2, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "ctx:zeta"),
		contextSortIssue("multi-old", 2, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), "ctx:alpha", "ctx:zeta", "ctx:alpha"),
		contextSortIssue("beta-new", 2, time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), "ctx:alpha"),
		contextSortIssue("multi-new", 2, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), "ctx:zeta", "ctx:alpha"),
		contextSortIssue("gamma", 2, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "ctx:gamma"),
		contextSortIssue("multi-other", 2, time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), "ctx:gamma", "ctx:alpha"),
		contextSortIssue("unknown-context", 2, time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), "ctx:unknown"),
		contextSortIssue("no-new", 2, time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)),
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = contextSortCatalog()
	m.sortMode = SortContextCreated
	m.applyFilter()

	got := m.FilteredIssues()
	want := []string{"alpha", "beta-new", "beta-old", "gamma", "multi-new", "multi-old", "multi-other", "no-new", "unknown-context", "no-old"}
	if len(got) != len(want) {
		t.Fatalf("sorted issue count = %d, want %d", len(got), len(want))
	}
	for i, issue := range got {
		if issue.ID != want[i] {
			t.Fatalf("sorted issue %d = %q, want %q", i, issue.ID, want[i])
		}
	}
}

func TestContextSortPriorityUsesPriorityThenIDWithinGroups(t *testing.T) {
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issues := []model.Issue{
		contextSortIssue("beta-z", 2, date, "ctx:alpha"),
		contextSortIssue("beta-a", 2, date.AddDate(0, 0, 1), "ctx:alpha"),
		contextSortIssue("alpha", 3, date, "ctx:zeta"),
		contextSortIssue("multi-z", 1, date, "ctx:alpha", "ctx:zeta"),
		contextSortIssue("multi-a", 1, date.AddDate(0, 0, 1), "ctx:zeta", "ctx:alpha"),
		contextSortIssue("no", 0, date),
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = contextSortCatalog()
	m.sortMode = SortContextPriority
	m.applyFilter()

	got := m.FilteredIssues()
	want := []string{"alpha", "beta-a", "beta-z", "multi-a", "multi-z", "no"}
	for i, issue := range got {
		if issue.ID != want[i] {
			t.Fatalf("sorted issue %d = %q, want %q", i, issue.ID, want[i])
		}
	}
}

func TestContextSortModesAreReachableThroughSortCycle(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "alpha", Status: model.StatusOpen, Labels: []string{"ctx:zeta"}},
		{ID: "beta", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
	}, nil, "")
	m.repositoryCatalog = contextSortCatalog()
	want := []SortMode{
		SortCreatedAsc, SortCreatedDesc, SortPriority, SortUpdated,
		SortContextCreated, SortContextPriority, SortDefault,
	}
	for i, expected := range want {
		updated, _ := m.Update(keyMsg("s"))
		m = updated.(*Model)
		if m.sortMode != expected {
			t.Fatalf("sort cycle step %d = %v, want %v", i, m.sortMode, expected)
		}
	}
	if SortContextCreated.String() != "Ctx + Created" || SortContextPriority.String() != "Ctx + Priority" {
		t.Fatalf("context sort labels = %q and %q", SortContextCreated, SortContextPriority)
	}
}

func TestContextSortCycleSkipsModesWithoutMultipleHubContexts(t *testing.T) {
	tests := []struct {
		name    string
		catalog repositorypkg.Catalog
	}{
		{name: "zero", catalog: nil},
		{name: "one", catalog: contextSortCatalog()[:1]},
		{name: "one hub plus workspace", catalog: append(contextSortCatalog()[:1], repositorypkg.CatalogEntry{ID: "workspace", Name: "Workspace", Kind: repositorypkg.IdentityPrefix})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(nil, nil, "")
			m.repositoryCatalog = tt.catalog
			for i := 0; i < 10; i++ {
				updated, _ := m.Update(keyMsg("s"))
				m = updated.(*Model)
				if isContextSortMode(m.sortMode) {
					t.Fatalf("cycle step %d entered unavailable mode %v", i, m.sortMode)
				}
			}
		})
	}
}

func TestContextSortAvailabilityFollowsActiveHubScope(t *testing.T) {
	tests := []struct {
		name      string
		issues    []model.Issue
		scope     hub.HubScope
		workspace bool
		want      bool
	}{
		{name: "selected one with secondary effective context", issues: []model.Issue{{ID: "both", Labels: []string{"ctx:alpha", "ctx:beta"}}}, scope: mustSelectedContextsScope(t, "ctx:alpha"), want: true},
		{name: "selected one without secondary effective context", issues: []model.Issue{{ID: "alpha", Labels: []string{"ctx:alpha"}}}, scope: mustSelectedContextsScope(t, "ctx:alpha"), want: false},
		{name: "selected two with one unused", issues: []model.Issue{{ID: "alpha", Labels: []string{"ctx:alpha"}}}, scope: mustSelectedContextsScope(t, "ctx:alpha", "ctx:beta"), want: true},
		{name: "selected plus contextless without secondary", issues: []model.Issue{{ID: "none"}}, scope: mustSelectedContextsAndContextlessScope(t, "ctx:alpha"), want: false},
		{name: "selected plus contextless with secondary", issues: []model.Issue{{ID: "both", Labels: []string{"ctx:alpha", "ctx:beta"}}, {ID: "none"}}, scope: mustSelectedContextsAndContextlessScope(t, "ctx:alpha"), want: true},
		{name: "contextless only", issues: []model.Issue{{ID: "none"}}, scope: hub.NewContextlessHubScope(), want: false},
		{name: "all items ignores unused catalog contexts", issues: []model.Issue{{ID: "alpha", Labels: []string{"ctx:alpha"}}}, scope: hub.NewAllItemsHubScope(), want: false},
		{name: "all items with effective contexts", issues: []model.Issue{{ID: "both", Labels: []string{"ctx:alpha", "ctx:beta"}}}, scope: hub.NewAllItemsHubScope(), want: true},
		{name: "workspace", issues: []model.Issue{{ID: "both", Labels: []string{"ctx:alpha", "ctx:beta"}}}, scope: hub.NewAllItemsHubScope(), workspace: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(tt.issues, nil, "")
			m.hubRepositoryMode = true
			m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
			m.refreshRepositoryCandidates()
			m.workspaceMode = tt.workspace
			if err := m.SetHubScope(tt.scope); err != nil {
				t.Fatal(err)
			}
			if got := m.contextSortModesAvailable(); got != tt.want {
				t.Fatalf("context sort availability = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextSortCycleReachesModesFromEffectiveSelectedScope(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "both", Labels: []string{"ctx:alpha", "ctx:beta"}}}, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	if err := m.SetHubScope(mustSelectedContextsScope(t, "ctx:alpha")); err != nil {
		t.Fatal(err)
	}

	seen := make(map[SortMode]bool)
	for i := 0; i < 6; i++ {
		updated, _ := m.Update(keyMsg("s"))
		m = updated.(*Model)
		seen[m.sortMode] = true
	}
	if !seen[SortContextCreated] || !seen[SortContextPriority] {
		t.Fatalf("effective selected scope cycle did not reach both context modes: %v", seen)
	}
}

func mustSelectedContextsScope(t *testing.T, contexts ...string) hub.HubScope {
	t.Helper()
	scope, err := hub.NewSelectedContextsHubScope(contexts)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func mustSelectedContextsAndContextlessScope(t *testing.T, contexts ...string) hub.HubScope {
	t.Helper()
	scope, err := hub.NewSelectedContextsAndContextlessHubScope(contexts)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestNewModel_RecipeSortDescendingTieBreaksByID(t *testing.T) {
	issues := []model.Issue{
		{ID: "d", Priority: 1},
		{ID: "c", Priority: 1},
		{ID: "b", Priority: 1},
		{ID: "a", Priority: 1},
		{ID: "z", Priority: 2},
	}

	r := &recipe.Recipe{
		Sort: recipe.SortConfig{
			Field:     "priority",
			Direction: "desc",
		},
	}

	expected := []string{"z", "a", "b", "c", "d"}
	for _, perm := range [][]model.Issue{
		issues,
		{issues[4], issues[0], issues[1], issues[2], issues[3]},
		{issues[3], issues[2], issues[1], issues[0], issues[4]},
	} {
		m := NewModel(append([]model.Issue(nil), perm...), r, "")
		filtered := m.FilteredIssues()
		if len(filtered) != len(expected) {
			t.Fatalf("Expected %d issues, got %d", len(expected), len(filtered))
		}
		for i, want := range expected {
			if filtered[i].ID != want {
				t.Fatalf("Input order %v: position %d = %s, want %s", perm, i, filtered[i].ID, want)
			}
		}
	}
}

func TestAttentionView_CloseRestoresInsightsPanel(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Alpha", Status: model.StatusOpen, Priority: 1},
		{ID: "B", Title: "Beta", Status: model.StatusOpen, Priority: 2},
	}

	m := NewModel(issues, nil, "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(*Model)
	m.insightsPanel.focusedPanel = PanelCycles

	// Attention is an overlay on Insights: ] opens it from Insights, Esc
	// returns to the originating view, and the Insights cursor is preserved.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = updated.(*Model)
	if m.focused != focusInsights || !m.showAttentionView {
		t.Fatalf("Expected Attention overlay on Insights, got focus=%v shown=%v", m.focused, m.showAttentionView)
	}
	if !strings.Contains(m.View(), "Rank") {
		t.Fatal("Expected attention view to render its table")
	}
	if m.insightsPanel.focusedPanel != PanelCycles {
		t.Fatalf("Expected attention view to preserve the insights cursor %v, got %v", PanelCycles, m.insightsPanel.focusedPanel)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.showAttentionView {
		t.Fatal("Expected attention view to close")
	}
	if m.insightsPanel.focusedPanel != PanelCycles {
		t.Fatalf("Expected insights panel cursor preserved at %v, got %v", PanelCycles, m.insightsPanel.focusedPanel)
	}
	if m.insightsPanel.insights.Stats == nil {
		t.Fatal("Expected insights panel data restored after closing attention view")
	}
}

func TestTimeTravel_DiffBadgePropagation(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Manually inject diff state (simulating enterTimeTravelMode)
	m.timeTravelMode = true
	m.newIssueIDs = map[string]bool{"A": true}
	m.closedIssueIDs = map[string]bool{}
	m.modifiedIssueIDs = map[string]bool{}

	// Test getDiffStatus logic
	status := m.getDiffStatus("A")
	if status != DiffStatusNew {
		t.Errorf("Expected DiffStatusNew, got %v", status)
	}

	// Test propagation to list items via rebuild
	m.rebuildListWithDiffInfo()

	items := m.list.Items()
	if len(items) != 1 {
		t.Fatal("Expected 1 item")
	}

	item := items[0].(IssueItem)
	if item.DiffStatus != DiffStatusNew {
		t.Errorf("List item missing DiffStatusNew, got %v", item.DiffStatus)
	}
}

func TestFormatTimeRel(t *testing.T) {
	now := time.Now()
	tests := []struct {
		t        time.Time
		expected string
	}{
		{now, "now"},
		{now.Add(-10 * time.Minute), "10m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-25 * time.Hour), "1d ago"},
		{now.Add(-8 * 24 * time.Hour), "1w ago"},
		{now.Add(-60 * 24 * time.Hour), "2mo ago"},
		{time.Time{}, "unknown"},
	}

	for _, tt := range tests {
		got := FormatTimeRel(tt.t)
		if got != tt.expected {
			t.Errorf("FormatTimeRel(%v): expected %s, got %s", tt.t, tt.expected, got)
		}
	}
}

// TestModel_InitSkipsUpdateCheckWhenDisabled proves the startup release check
// honours the updater preference (#197): with BV_NO_UPDATE_CHECK set, Init
// schedules exactly one command fewer than with the default preferences.
func TestModel_InitSkipsUpdateCheckWhenDisabled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no saved config: defaults apply
	t.Setenv("BV_NO_SAVED_CONFIG", "")

	issues := []model.Issue{{ID: "A", Title: "Issue A", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask}}
	initCmdCount := func() int {
		m := NewModel(issues, nil, "")
		m.analysis = nil // WaitForPhase2Cmd then returns immediately if executed
		cmd := m.Init()
		if cmd == nil {
			return 0
		}
		// tea.Batch collapses a single command to itself; executing it is safe
		// here because every remaining startup command is non-blocking.
		if batch, ok := cmd().(tea.BatchMsg); ok {
			return len(batch)
		}
		return 1
	}

	t.Setenv("BV_NO_UPDATE_CHECK", "")
	enabled := initCmdCount()
	t.Setenv("BV_NO_UPDATE_CHECK", "1")
	disabled := initCmdCount()

	if enabled != disabled+1 {
		t.Fatalf("Init scheduled %d commands with the check enabled and %d disabled; want exactly one fewer when disabled", enabled, disabled)
	}
}

// TestRenderAlertsPanel_ShowsEverySeverityAndSuggestedAction renders the `!`
// panel with one alert per severity and checks the selected alert exposes
// its issue, partner, and suggested action while dismissed alerts vanish.
func TestRenderAlertsPanel_ShowsEverySeverityAndSuggestedAction(t *testing.T) {
	issues := []model.Issue{{ID: "A", Title: "Issue A", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask}}
	m := NewModel(issues, nil, "")
	m.width, m.height = 120, 40
	m.alerts = []drift.Alert{
		{Type: drift.AlertStaleIssue, Severity: drift.SeverityCritical, Message: "Issue A inactive for 45 days", IssueID: "A", SuggestedAction: "Update, close, or re-triage the issue"},
		{Type: drift.AlertPotentialDuplicate, Severity: drift.SeverityInfo, Message: "A looks like B", IssueID: "A", RelatedIssueID: "B", SuggestedAction: "Compare the two issues"},
		{Type: drift.AlertVelocityDrop, Severity: drift.SeverityWarning, Message: "Closed 1 issue(s) in the last 7 days vs 6 before", SuggestedAction: "Check for blocked work"},
	}
	m.alertsCritical, m.alertsWarning, m.alertsInfo = 1, 1, 1
	m.alertsCursor = 0

	view := m.renderAlertsPanel()
	for _, want := range []string{"3 total", "1 critical", "1 warning", "1 info", "⚠", "⚡", "ℹ", "Issue A inactive for 45 days", "Issue: A (press Enter to jump)", "Suggested: Update, close, or re-triage the issue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("alerts panel missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Related: B") {
		t.Fatalf("partner of an unselected alert must not render:\n%s", view)
	}

	m.alertsCursor = 1
	view = m.renderAlertsPanel()
	for _, want := range []string{"Related: B", "Suggested: Compare the two issues"} {
		if !strings.Contains(view, want) {
			t.Fatalf("selected duplicate alert missing %q:\n%s", want, view)
		}
	}

	m.dismissedAlerts = map[string]bool{alertKey(m.alerts[0]): true}
	view = m.renderAlertsPanel()
	if strings.Contains(view, "inactive for 45 days") {
		t.Fatalf("dismissed alert still rendered:\n%s", view)
	}

	m.alerts = nil
	if view := m.renderAlertsPanel(); !strings.Contains(view, "No active alerts") {
		t.Fatalf("empty panel should say so:\n%s", view)
	}
}

// The three bindings README documents that E5 added: Shift+Tab in Insights,
// n/N while time-travelling, and t in History.

func TestInsightsKeys_ShiftTabAndTabCyclePanels(t *testing.T) {
	issues := []model.Issue{{ID: "A", Title: "Issue A", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask}}
	m := NewModel(issues, nil, "")
	m.focused = focusInsights
	m.insightsPanel.focusedPanel = 1

	got := asModelPtr(t, must2(m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})))
	if got.insightsPanel.focusedPanel != 0 {
		t.Fatalf("shift+tab: focusedPanel=%d; want 0 (previous panel)", got.insightsPanel.focusedPanel)
	}
	got = asModelPtr(t, must2(got.Update(tea.KeyMsg{Type: tea.KeyShiftTab})))
	if got.insightsPanel.focusedPanel != PanelCount-1 {
		t.Fatalf("shift+tab from the first panel should wrap to %d, got %d", PanelCount-1, got.insightsPanel.focusedPanel)
	}
	got = asModelPtr(t, must2(got.Update(tea.KeyMsg{Type: tea.KeyTab})))
	if got.insightsPanel.focusedPanel != 0 {
		t.Fatalf("tab should wrap forward to 0, got %d", got.insightsPanel.focusedPanel)
	}
}

func TestTimeTravelKeys_NextPrevChangedIssue(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "A", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask},
		{ID: "B", Title: "B", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask},
		{ID: "C", Title: "C", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask},
		{ID: "D", Title: "D", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask},
	}
	m := NewModel(issues, nil, "")
	m.focused = focusList
	idAt := func(i int) string { return m.list.Items()[i].(IssueItem).Issue.ID }
	changed := map[string]bool{idAt(1): true, idAt(3): true}
	m.timeTravelMode = true
	m.timeTravelDiff = &analysis.SnapshotDiff{}
	m.modifiedIssueIDs = changed
	m.list.Select(0)

	press := func(key string) *Model {
		t.Helper()
		return asModelPtr(t, must2(m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})))
	}
	if got := press("n"); got.list.Index() != 1 {
		t.Fatalf("n: index=%d; want 1 (first changed issue after the cursor)", got.list.Index())
	}
	if got := press("n"); got.list.Index() != 3 {
		t.Fatalf("second n: index=%d; want 3", got.list.Index())
	}
	if got := press("n"); got.list.Index() != 1 {
		t.Fatalf("n past the last changed issue should wrap to 1, got %d", got.list.Index())
	}
	if got := press("N"); got.list.Index() != 3 {
		t.Fatalf("N should step backwards (wrapping) to 3, got %d", got.list.Index())
	}
	if !strings.Contains(m.statusMsg, "2") {
		t.Fatalf("status should report position among changed issues, got %q", m.statusMsg)
	}

	m.timeTravelMode = false
	m.list.Select(0)
	if got := press("n"); got.list.Index() != 0 {
		t.Fatalf("outside time-travel n must not move the cursor, got %d", got.list.Index())
	}
}

func TestHistoryKeys_TTogglesTimelinePane(t *testing.T) {
	issues := []model.Issue{{ID: "A", Title: "Issue A", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask}}
	m := NewModel(issues, nil, "")
	m.focused = focusHistory
	m.historyView.SetSize(160, 40) // wide: timeline on by default
	if !m.historyView.TimelineVisible() {
		t.Fatalf("precondition: timeline should be visible at 160 columns")
	}
	tKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}

	m, _ = m.handleHistoryKeys(tKey)
	if m.historyView.TimelineVisible() || !strings.Contains(m.statusMsg, "hidden") {
		t.Fatalf("t should hide the timeline: visible=%v status=%q", m.historyView.TimelineVisible(), m.statusMsg)
	}
	m, _ = m.handleHistoryKeys(tKey)
	if !m.historyView.TimelineVisible() || !strings.Contains(m.statusMsg, "shown") {
		t.Fatalf("t again should show the timeline: visible=%v status=%q", m.historyView.TimelineVisible(), m.statusMsg)
	}

	m.historyView.SetSize(90, 40) // narrow: no timeline possible
	m, _ = m.handleHistoryKeys(tKey)
	if !m.statusIsError || !strings.Contains(m.statusMsg, "100 columns") {
		t.Fatalf("narrow terminals should explain why t does nothing: err=%v status=%q", m.statusIsError, m.statusMsg)
	}
}

// TestListKeys_DocumentedViewToggles (E6): the bindings the README documents
// for the list view do what it says: a opens the actionable view, E the
// tree, ' the recipe picker, [ the label dashboard, x exports Markdown.
func TestListKeys_DocumentedViewToggles(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Issue A", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask, Labels: []string{"core"}},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeTask, Labels: []string{"ui"}, Dependencies: []*model.Dependency{{IssueID: "B", DependsOnID: "A", Type: model.DepBlocks}}},
	}
	fresh := func() *Model {
		m := NewModel(issues, nil, "")
		m.width, m.height = 120, 40
		m.focused = focusList
		return m
	}
	press := func(t *testing.T, m *Model, key string) *Model {
		t.Helper()
		var msg tea.KeyMsg
		if key == "esc" {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		return asModelPtr(t, must2(m.Update(msg)))
	}

	t.Run("a opens the actionable view and toggles back", func(t *testing.T) {
		m := press(t, fresh(), "a")
		if !m.isActionableView || m.focused != focusActionable {
			t.Fatalf("a: actionable=%v focused=%v", m.isActionableView, m.focused)
		}
		m = press(t, m, "a")
		if m.isActionableView || m.focused != focusList {
			t.Fatalf("second a should return to the list: actionable=%v focused=%v", m.isActionableView, m.focused)
		}
	})
	t.Run("E opens the tree view", func(t *testing.T) {
		m := press(t, fresh(), "E")
		if m.focused != focusTree {
			t.Fatalf("E: focused=%v; want tree", m.focused)
		}
		if m = press(t, m, "E"); m.focused != focusList {
			t.Fatalf("second E should return to the list, focused=%v", m.focused)
		}
	})
	t.Run("' opens the recipe picker", func(t *testing.T) {
		m := press(t, fresh(), "'")
		if !m.showRecipePicker || m.focused != focusRecipePicker {
			t.Fatalf("': picker=%v focused=%v", m.showRecipePicker, m.focused)
		}
	})
	t.Run("[ opens the label dashboard", func(t *testing.T) {
		m := press(t, fresh(), "[")
		if m.focused != focusLabelDashboard {
			t.Fatalf("[: focused=%v; want label dashboard", m.focused)
		}
		if m = press(t, m, "esc"); m.focused != focusList {
			t.Fatalf("esc should close the label dashboard, focused=%v", m.focused)
		}
	})
	t.Run("x exports Markdown into the working directory", func(t *testing.T) {
		dir := t.TempDir()
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(wd) })
		m := press(t, fresh(), "x")
		if !strings.HasPrefix(m.statusMsg, "✅ Exported") {
			t.Fatalf("x should export and confirm, status=%q", m.statusMsg)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var md bool
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				md = true
			}
		}
		if !md {
			t.Fatalf("no markdown file written to %s: %v", dir, entries)
		}
	})
}
