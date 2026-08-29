package ui

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func hubScopeCatalog(ids ...string) model.RepositoryCatalog {
	catalog := make(model.RepositoryCatalog, 0, len(ids))
	for _, id := range ids {
		catalog = append(catalog, model.RepositoryCatalogEntry{ID: id, Name: id, Kind: model.RepositoryIdentityHubContext})
	}
	return catalog
}

func visibleIssueIDs(m Model) []string {
	ids := make([]string, 0, len(m.list.Items()))
	for _, item := range m.list.Items() {
		ids = append(ids, item.(IssueItem).Issue.ID)
	}
	return ids
}

func requireIssueIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("issue IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("issue IDs = %v, want %v", got, want)
		}
	}
}

func TestRepositoryScopeHubExactMultiContextAndAllSemantics(t *testing.T) {
	issues := []model.Issue{
		{ID: "a", Title: "A", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "both", Title: "Both", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "ctx:beta"}},
		{ID: "upper", Title: "Upper", Status: model.StatusOpen, Labels: []string{"CTX:ALPHA"}},
		{ID: "none", Title: "None", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")

	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})
	requireIssueIDs(t, visibleIssueIDs(m), "a", "both")

	m.SetRepositoryScope(map[string]bool{"ctx:beta": true})
	requireIssueIDs(t, visibleIssueIDs(m), "both")

	m.SetRepositoryScope(map[string]bool{})
	if m.RepositoryScope() != nil {
		t.Fatalf("empty applied selection must normalize to all, got %v", m.RepositoryScope())
	}
	requireIssueIDs(t, visibleIssueIDs(m), "a", "both", "none", "upper")

	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true, "ctx:beta": true})
	if scope := m.HubScope(); scope.Mode != model.HubScopeSelectedContexts || scope.IncludeContextless {
		t.Fatalf("full repository selection must exclude contextless, got %#v", scope)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "a", "both")
}

func TestHubScopeExplicitVariantsAndUnregisteredMembership(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "both", Title: "Both", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "ctx:beta"}},
		{ID: "contextless", Title: "Contextless", Status: model.StatusOpen, IssueType: "todo"},
		{ID: "unregistered", Title: "Unregistered", Status: model.StatusOpen, Labels: []string{"ctx:unknown"}},
	}
	m := NewModel(issues, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")

	if err := m.SetHubScope(model.NewAllItemsHubScope()); err != nil {
		t.Fatal(err)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha", "both", "contextless", "unregistered")

	selected, err := model.NewSelectedContextsHubScope([]string{"ctx:beta", "ctx:alpha", "ctx:beta"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(selected); err != nil {
		t.Fatal(err)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha", "both")
	if got := strings.Join(m.HubScope().Contexts, ","); got != "ctx:alpha,ctx:beta" {
		t.Fatalf("selected contexts = %q", got)
	}

	if err := m.SetHubScope(model.NewContextlessHubScope()); err != nil {
		t.Fatal(err)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "contextless")
	if m.HubScope().Mode != model.HubScopeContextless {
		t.Fatalf("scope = %#v", m.HubScope())
	}

	unknown, err := model.NewSelectedContextsHubScope([]string{"ctx:unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(unknown); err == nil {
		t.Fatal("unregistered explicit context was accepted")
	}
}

func TestHubScopeMixedContextAndContextlessIsUnionWithoutDuplicates(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "both", Title: "Both", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "ctx:beta"}},
		{ID: "contextless", Title: "Contextless", Status: model.StatusOpen},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	scope, err := model.NewSelectedContextsAndContextlessHubScope([]string{"ctx:alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha", "both", "contextless")
}

func TestContextlessScopePersistsAcrossCatalogAndSnapshotRefresh(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "none", Title: "None", Status: model.StatusOpen, IssueType: "todo"},
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
	}, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha")
	if err := m.SetHubScope(model.NewContextlessHubScope()); err != nil {
		t.Fatal(err)
	}

	m.applyRepositoryCatalogUpdate(hubScopeCatalog("ctx:alpha", "ctx:beta"), 1, true, false, nil)
	if m.HubScope().Mode != model.HubScopeContextless {
		t.Fatalf("catalog refresh changed scope to %#v", m.HubScope())
	}

	snapshot := NewSnapshotBuilder([]model.Issue{
		{ID: "none-new", Title: "None", Status: model.StatusOpen, IssueType: "todo"},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}).Build()
	updated, _ := m.Update(SnapshotReadyMsg{Snapshot: snapshot, SnapshotVer: 1})
	m = updated.(Model)
	if m.HubScope().Mode != model.HubScopeContextless {
		t.Fatalf("snapshot refresh changed scope to %#v", m.HubScope())
	}
	requireIssueIDs(t, visibleIssueIDs(m), "none-new")
}

func TestDefaultRepositoryScopeSynchronousCatalog(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "hub.yaml")
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:alpha": "/alpha", "ctx:beta": "/beta"})
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.SetHistoryProvider(correlation.HistoryModeExternal, configPath)
	if !m.SetDefaultRepositoryScope("ctx:alpha") {
		t.Fatal("synchronous catalog did not apply the current repository")
	}
	if scope := m.RepositoryScope(); len(scope) != 1 || !scope["ctx:alpha"] {
		t.Fatalf("scope = %#v, want ctx:alpha", scope)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha")
}

func TestDefaultRepositoryScopeWaitsForAsyncCatalog(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}, nil, "")
	m.hubRepositoryMode = true
	if m.SetDefaultRepositoryScope("ctx:alpha") {
		t.Fatal("default applied before the initial catalog arrived")
	}
	updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 1, Catalog: hubScopeCatalog("ctx:alpha", "ctx:beta")})
	m = updated.(Model)
	if scope := m.RepositoryScope(); len(scope) != 1 || !scope["ctx:alpha"] {
		t.Fatalf("async scope = %#v, want ctx:alpha", scope)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha")
}

func TestRejectedHubScopePreservesPendingDefault(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}, nil, "")
	m.hubRepositoryMode = true
	if m.SetDefaultRepositoryScope("ctx:alpha") {
		t.Fatal("default applied before the initial catalog arrived")
	}

	beforeScope := m.HubScope()
	beforeDefaultSet := m.defaultRepositorySet
	beforeDefaultID := m.defaultRepositoryID
	unknown, err := model.NewSelectedContextsHubScope([]string{"ctx:unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(unknown); err == nil {
		t.Fatal("unregistered explicit context was accepted")
	}
	afterScope := m.HubScope()
	if afterScope.Mode != beforeScope.Mode || strings.Join(afterScope.Contexts, ",") != strings.Join(beforeScope.Contexts, ",") {
		t.Fatalf("rejected scope changed Hub scope: before=%#v after=%#v", beforeScope, afterScope)
	}
	if m.defaultRepositorySet != beforeDefaultSet || m.defaultRepositoryID != beforeDefaultID {
		t.Fatalf("rejected scope changed pending default: set %v -> %v, ID %q -> %q",
			beforeDefaultSet, m.defaultRepositorySet, beforeDefaultID, m.defaultRepositoryID)
	}

	updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 1, Catalog: hubScopeCatalog("ctx:alpha", "ctx:beta")})
	m = updated.(Model)
	if scope := m.HubScope(); scope.Mode != model.HubScopeSelectedContexts || len(scope.Contexts) != 1 || scope.Contexts[0] != "ctx:alpha" {
		t.Fatalf("pending current-context default did not apply: %#v", scope)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha")
}

func TestPendingDefaultDoesNotOverridePickerChoiceOnCatalogRecovery(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
		{ID: "gamma", Title: "Gamma", Status: model.StatusOpen, Labels: []string{"ctx:gamma"}},
	}

	t.Run("all", func(t *testing.T) {
		m := NewModel(issues, nil, "")
		m.hubRepositoryMode = true
		if m.SetDefaultRepositoryScope("ctx:alpha") {
			t.Fatal("default applied before the initial catalog arrived")
		}
		m.repoPicker = NewRepoPickerModel(nil, m.theme)
		m = m.applyRepositoryPickerSelection()

		updated, _ := m.Update(RepositoryCatalogReadyMsg{
			Generation: 1,
			Catalog:    hubScopeCatalog("ctx:alpha", "ctx:beta", "ctx:gamma"),
		})
		m = updated.(Model)
		if m.RepositoryScope() != nil {
			t.Fatalf("recovery replaced explicit all scope: %#v", m.RepositoryScope())
		}
		requireIssueIDs(t, visibleIssueIDs(m), "alpha", "beta", "gamma")
	})

	t.Run("subset", func(t *testing.T) {
		m := NewModel(issues, nil, "")
		m.hubRepositoryMode = true
		if m.SetDefaultRepositoryScope("ctx:alpha") {
			t.Fatal("default applied before the initial catalog arrived")
		}
		m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
		m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
		m.repoPicker.ClearSelection()
		m.repoPicker.MoveDown()
		m.repoPicker.ToggleSelected()
		m = m.applyRepositoryPickerSelection()

		updated, _ := m.Update(RepositoryCatalogReadyMsg{
			Generation: 1,
			Catalog:    hubScopeCatalog("ctx:alpha", "ctx:beta", "ctx:gamma"),
		})
		m = updated.(Model)
		if scope := m.RepositoryScope(); len(scope) != 1 || !scope["ctx:beta"] {
			t.Fatalf("recovery replaced explicit subset scope: %#v", scope)
		}
		requireIssueIDs(t, visibleIssueIDs(m), "beta")
	})
}

func TestDefaultRepositoryScopeFallbacksLeaveAll(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.hubRepositoryMode = true
	updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 1, Catalog: hubScopeCatalog("ctx:alpha", "ctx:beta")})
	m = updated.(Model)
	if m.SetDefaultRepositoryScope("ctx:unregistered") || m.RepositoryScope() != nil {
		t.Fatalf("unregistered context changed scope: %#v", m.RepositoryScope())
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha", "beta")
}

func TestDefaultRepositoryScopeSingleCatalogStaysExplicitOnGrowth(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}, nil, "")
	m.hubRepositoryMode = true
	updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 1, Catalog: hubScopeCatalog("ctx:alpha")})
	m = updated.(Model)
	if !m.SetDefaultRepositoryScope("ctx:alpha") {
		t.Fatal("single-entry default was not applied")
	}
	updated, _ = m.Update(RepositoryCatalogReadyMsg{Generation: 2, Catalog: hubScopeCatalog("ctx:alpha", "ctx:beta")})
	m = updated.(Model)
	if scope := m.RepositoryScope(); len(scope) != 1 || !scope["ctx:alpha"] {
		t.Fatalf("scope after catalog growth = %#v, want explicit ctx:alpha", scope)
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha")
}

func TestRepositoryPickerChoiceOverridesDefaultAcrossRefresh(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
		{ID: "gamma", Title: "Gamma", Status: model.StatusOpen, Labels: []string{"ctx:gamma"}},
	}, nil, "")
	m.hubRepositoryMode = true
	updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 1, Catalog: hubScopeCatalog("ctx:alpha", "ctx:beta")})
	m = updated.(Model)
	m.SetDefaultRepositoryScope("ctx:alpha")
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetActiveRepos(m.activeRepos)
	m.repoPicker.ClearSelection()
	m = m.applyRepositoryPickerSelection()
	if m.RepositoryScope() != nil {
		t.Fatalf("empty picker choice = %#v, want all", m.RepositoryScope())
	}

	updated, _ = m.Update(RepositoryCatalogReadyMsg{Generation: 2, Catalog: hubScopeCatalog("ctx:alpha", "ctx:beta", "ctx:gamma")})
	m = updated.(Model)
	if m.RepositoryScope() != nil {
		t.Fatalf("refresh reapplied default over user choice: %#v", m.RepositoryScope())
	}
	requireIssueIDs(t, visibleIssueIDs(m), "alpha", "beta", "gamma")
}

func TestRepositoryScopeComposesAfterScopeAndKeepsHiddenBlockerTruth(t *testing.T) {
	issues := []model.Issue{
		{ID: "visible", Title: "Visible", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "work"}, Dependencies: []*model.Dependency{{DependsOnID: "hidden", Type: model.DepBlocks}}},
		{ID: "hidden", Title: "Hidden", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	m.currentFilter = "ready"
	m.applyFilter()
	requireIssueIDs(t, visibleIssueIDs(m))
	if m.issueMap["hidden"] == nil {
		t.Fatal("canonical issue map lost hidden blocker")
	}

	m.currentFilter = "label:work"
	m.applyFilter()
	requireIssueIDs(t, visibleIssueIDs(m), "visible")
	if m.graphView.issueMap["hidden"] == nil {
		t.Fatal("graph details cannot resolve hidden blocker")
	}

	actionable := projectExecutionPlan(m.analyzer.GetExecutionPlan(), m.repositoryIssueIDs, m.repositoryIssues)
	if actionable.TotalActionable != 0 || len(actionable.Tracks) != 0 {
		t.Fatalf("hidden blocker incorrectly became actionable: %+v", actionable)
	}

	r := &recipe.Recipe{Name: "actionable", Filters: recipe.FilterConfig{Actionable: boolPointer(true)}}
	m.setActiveRecipe(r)
	m.applyRecipe(r)
	requireIssueIDs(t, visibleIssueIDs(m))

	blocked := &recipe.Recipe{Name: "blocked", Filters: recipe.FilterConfig{HasBlockers: boolPointer(true)}}
	m.setActiveRecipe(blocked)
	m.applyRecipe(blocked)
	requireIssueIDs(t, visibleIssueIDs(m), "visible")

	unblocked := &recipe.Recipe{Name: "unblocked", Filters: recipe.FilterConfig{HasBlockers: boolPointer(false)}}
	m.setActiveRecipe(unblocked)
	m.applyRecipe(unblocked)
	requireIssueIDs(t, visibleIssueIDs(m))
}

func TestHubFocusedRelationshipBoundaryEvidence(t *testing.T) {
	issues := []model.Issue{
		{ID: "todo", Title: "Captured idea", Status: model.StatusOpen, IssueType: "todo"},
		{ID: "work", Title: "Visible work", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}, Dependencies: []*model.Dependency{
			{DependsOnID: "blocker", Type: model.DepBlocks},
			{DependsOnID: "todo", Type: model.DepDiscoveredFrom},
		}},
		{ID: "blocker", Title: "Hidden blocker", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
		{ID: "original", Title: "Original", Status: model.StatusClosed, CloseReason: "Superseded after placement correction", Labels: []string{"ctx:beta"}},
		{ID: "replacement", Title: "Replacement", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}, Dependencies: []*model.Dependency{{DependsOnID: "original", Type: model.DepSupersedes}}},
	}
	m := NewModel(issues, nil, "")
	m.hubRepositoryMode = true
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	work := m.hubRelationshipMarkdown(*m.issueMap["work"])
	if !containsAll(work, "Blocked by", "Hidden blocker", "Result of todo", "Captured idea", "contexts: ctx:beta", "contexts: no-context", "out of scope") {
		t.Fatalf("work relationship evidence:\n%s", work)
	}
	replacement := m.hubRelationshipMarkdown(*m.issueMap["replacement"])
	if !containsAll(replacement, "Supersedes original", "Original", "contexts: ctx:beta", "out of scope") {
		t.Fatalf("replacement relationship evidence:\n%s", replacement)
	}
	original := m.hubRelationshipMarkdown(*m.issueMap["original"])
	if !containsAll(original, "Superseded by", "Replacement", "Close reason", "Superseded after placement correction", "contexts: ctx:alpha") {
		t.Fatalf("original relationship evidence:\n%s", original)
	}
	if !strings.Contains(m.hubRelationshipMarkdown(*m.issueMap["todo"]), "Results in") {
		t.Fatal("todo did not expose resulting work")
	}
}

func TestHubTreeAndBoardMarkHiddenCanonicalEndpoints(t *testing.T) {
	issues := []model.Issue{
		{ID: "child", Title: "Visible child", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}, Dependencies: []*model.Dependency{
			{DependsOnID: "parent", Type: model.DepParentChild},
			{DependsOnID: "blocker", Type: model.DepBlocks},
		}},
		{ID: "parent", Title: "Hidden parent", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
		{ID: "blocker", Title: "Hidden blocker", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
		{ID: "source", Title: "Visible source", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "dependent", Title: "Hidden dependent", Status: model.StatusOpen, Labels: []string{"ctx:beta"}, Dependencies: []*model.Dependency{{DependsOnID: "source", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	m.focused = focusTree
	m.rebuildRepositoryTree()
	treeView := m.tree.View()
	if !containsAll(treeView, "Visible child", "parent out of scope") || strings.Contains(treeView, "Hidden parent") {
		t.Fatalf("projected tree:\n%s", treeView)
	}

	card := m.board.renderCard(*m.issueMap["child"], 48, false, 0, 0)
	if !containsAll(card, "Hidden bloc", "[out]") || !m.board.hasOpenBlocker(*m.issueMap["child"]) {
		t.Fatalf("projected board card:\n%s", card)
	}
	expanded := m.board.renderExpandedCard(*m.issueMap["child"], 70, 0, 0)
	if !containsAll(expanded, "Hidden blocker", "out of scope") {
		t.Fatalf("expanded board card:\n%s", expanded)
	}
	sourceCard := m.board.renderCard(*m.issueMap["source"], 48, false, 0, 0)
	if !containsAll(sourceCard, "→1", "[out]") {
		t.Fatalf("board card lost hidden dependent evidence:\n%s", sourceCard)
	}
}

func boolPointer(value bool) *bool { return &value }

func TestRepositoryScopeProjectsDerivedViews(t *testing.T) {
	now := time.Now().UTC()
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusInProgress, Labels: []string{"ctx:alpha", "backend"}, UpdatedAt: now.Add(-5 * 24 * time.Hour)},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta", "frontend"}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	if m.board.TotalCount() != 1 {
		t.Fatalf("board count = %d, want 1", m.board.TotalCount())
	}
	requireIssueIDs(t, m.graphView.sortedIDs, "alpha")

	m.isActionableView = true
	m.refreshRepositoryDerivedViews()
	if m.actionableView.plan.TotalActionable != 1 {
		t.Fatalf("actionable plan count = %d, want 1", m.actionableView.plan.TotalActionable)
	}
	m.focused = focusTree
	m.refreshRepositoryDerivedViews()
	if len(m.tree.issueMap) != 1 || m.tree.issueMap["alpha"] == nil {
		t.Fatalf("tree was not scoped: %v", m.tree.issueMap)
	}
	m.focused = focusInsights
	m.showAttentionView = false
	m.refreshRepositoryDerivedViews()
	for _, id := range m.insightsPanel.insights.Orphans {
		if id != "alpha" {
			t.Fatalf("insights contain out-of-scope ID %q", id)
		}
	}
	m.focused = focusLabelDashboard
	m.showAttentionView = true
	m.selectedSprint = &model.Sprint{Name: "Scoped", BeadIDs: []string{"alpha", "beta"}}
	m.historyReport = &correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{
		"alpha": {BeadID: "alpha", Commits: []correlation.CorrelatedCommit{{SHA: "a", Author: "A"}}},
		"beta":  {BeadID: "beta", Commits: []correlation.CorrelatedCommit{{SHA: "b", Author: "B"}}},
	}, CommitIndex: correlation.CommitIndex{"a": {"alpha"}, "b": {"beta"}}}
	m.historyView = NewHistoryModel(nil, m.theme)
	m.refreshRepositoryDerivedViews()

	for _, health := range m.labelHealthCache.Labels {
		if health.Label == "frontend" {
			t.Fatalf("label health includes out-of-scope label: %+v", m.labelHealthCache.Labels)
		}
	}
	if len(m.historyView.beadIDs) != 1 || m.historyView.beadIDs[0] != "alpha" {
		t.Fatalf("history IDs = %v, want [alpha]", m.historyView.beadIDs)
	}
	m.focused = focusFlowMatrix
	m.showAttentionView = false
	m.refreshRepositoryDerivedViews()
	if len(m.flowMatrix.issues) != 1 || m.flowMatrix.issues[0].ID != "alpha" {
		t.Fatalf("flow issues were not scoped: %v", m.flowMatrix.issues)
	}
	if got := m.renderSprintDashboard(); !containsAll(got, "0/1", "alpha") || containsAll(got, "beta") {
		t.Fatalf("sprint dashboard was not scoped:\n%s", got)
	}
	if m.countOpen != 1 || m.countReady != 1 || m.countClosed != 0 {
		t.Fatalf("scoped counts = open:%d ready:%d closed:%d", m.countOpen, m.countReady, m.countClosed)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}

func TestHubRepositoryPresentationIsStableFriendlyAndNonMutating(t *testing.T) {
	issue := model.Issue{
		ID:     "multi",
		Title:  "Multi repository",
		Status: model.StatusOpen,
		Labels: []string{"ctx:zeta", "work", "Ctx:upper", "ctx:Mixed", "ctx:alpha", "myctx:keep"},
	}
	catalog := model.RepositoryCatalog{
		{ID: "ctx:zeta", Name: "teams/zeta/service", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:alpha", Name: "teams/alpha/service", Kind: model.RepositoryIdentityHubContext},
	}

	presentation := repositoryPresentationForIssue(issue, catalog, true, nil)
	if presentation.ID != "ctx:alpha" || presentation.Name != "teams/alpha/service" || presentation.Extra != 1 {
		t.Fatalf("presentation = %+v", presentation)
	}
	if got := strings.Join(presentation.Names, ","); got != "teams/alpha/service,teams/zeta/service" {
		t.Fatalf("repository names = %q", got)
	}
	if got := strings.Join(presentation.Labels, ","); got != "work,Ctx:upper,myctx:keep" {
		t.Fatalf("visible labels = %q", got)
	}
	if got := strings.Join(issue.Labels, ","); got != "ctx:zeta,work,Ctx:upper,ctx:Mixed,ctx:alpha,myctx:keep" {
		t.Fatalf("source labels mutated: %q", got)
	}
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(encoded), `"ctx:alpha"`, `"ctx:Mixed"`, `"Ctx:upper"`, `"myctx:keep"`) {
		t.Fatalf("raw JSON/robot label contract changed: %s", encoded)
	}

	item := IssueItem{Issue: issue}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.decorateIssueItem(&item)
	filterValue := item.FilterValue()
	if !containsAll(filterValue, "teams/alpha/service", "teams/zeta/service", "Ctx:upper", "myctx:keep") || strings.Contains(filterValue, "ctx:alpha") {
		t.Fatalf("fuzzy display tokens = %q", filterValue)
	}
}

func TestHubListBadgePrefersSelectedRepositoryThenAscendingDisplayName(t *testing.T) {
	issue := model.Issue{
		ID: "shared", Title: "Multi-context item", Status: model.StatusOpen,
		Labels: []string{"ctx:repo-a", "ctx:repo-b"},
	}
	catalog := model.RepositoryCatalog{
		{ID: "ctx:repo-a", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:repo-b", Name: "dotfiles", Kind: model.RepositoryIdentityHubContext},
	}
	tests := []struct {
		name     string
		selected map[string]bool
		want     string
	}{
		{name: "dotfiles only", selected: map[string]bool{"ctx:repo-b": true}, want: "dotfiles"},
		{name: "beads viewer only", selected: map[string]bool{"ctx:repo-a": true}, want: "beads_viewer"},
		{name: "both selected", selected: map[string]bool{"ctx:repo-a": true, "ctx:repo-b": true}, want: "beads_viewer"},
		{name: "all items fallback", want: "beads_viewer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel([]model.Issue{issue}, nil, "")
			m.hubConfigPath = "hub.yaml"
			m.repositoryCatalog = catalog
			m.SetRepositoryScope(tt.selected)
			m.refreshRepositoryPresentation()

			row := m.list.View()
			if !strings.Contains(row, "["+tt.want+"]") || !strings.Contains(row, "+1") {
				t.Fatalf("list row = %q, want [%s] +1", row, tt.want)
			}
			if strings.Contains(row, "[beads_viewer]") == (tt.want != "beads_viewer") {
				t.Fatalf("list row used wrong primary repository: %q", row)
			}
		})
	}

	t.Run("display name tie uses ID", func(t *testing.T) {
		presentation := repositoryPresentationForIssue(
			model.Issue{Labels: []string{"ctx:repo-b", "ctx:repo-a"}},
			model.RepositoryCatalog{
				{ID: "ctx:repo-b", Name: "same", Kind: model.RepositoryIdentityHubContext},
				{ID: "ctx:repo-a", Name: "same", Kind: model.RepositoryIdentityHubContext},
			},
			true,
			map[string]bool{"ctx:repo-a": true, "ctx:repo-b": true},
		)
		if presentation.ID != "ctx:repo-a" || presentation.Extra != 1 {
			t.Fatalf("presentation = %+v, want ctx:repo-a with +1", presentation)
		}
	})
}

func TestHubRepositoryPresentationAcrossListBoardAndInsights(t *testing.T) {
	issue := model.Issue{ID: "one", Title: "One", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "backend"}}
	catalog := model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha/service", Kind: model.RepositoryIdentityHubContext}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.refreshRepositoryPresentation()

	listDetail := m.viewport.View()
	if !containsAll(listDetail, "Repositories:", "alpha/service", "Labels:", "backend") || strings.Contains(listDetail, "ctx:alpha") {
		t.Fatalf("list detail presentation:\n%s", listDetail)
	}

	card := m.board.renderCard(issue, 42, false, 0, 0)
	if !strings.Contains(card, "[alpha/s…]") || strings.Contains(card, "ctx:") {
		t.Fatalf("board card presentation:\n%s", card)
	}
	for _, line := range strings.Split(card, "\n") {
		if width := lipgloss.Width(line); width > 46 {
			t.Fatalf("board card line width = %d: %q", width, line)
		}
	}

	m.insightsPanel.issueMap = m.issueMap
	insightsDetail := m.insightsPanel.buildDetailMarkdown("one")
	if !containsAll(insightsDetail, "Repositories:", "alpha/service", "Labels:", "backend") || strings.Contains(insightsDetail, "ctx:alpha") {
		t.Fatalf("insights detail presentation:\n%s", insightsDetail)
	}
}

func TestHubCatalogChangeInvalidatesBoardAndInsightsPresentationCaches(t *testing.T) {
	issue := model.Issue{ID: "one", Title: "One", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}}
	oldCatalog := model.RepositoryCatalog{{ID: "ctx:alpha", Name: "old/name", Kind: model.RepositoryIdentityHubContext}}
	newCatalog := model.RepositoryCatalog{{ID: "ctx:alpha", Name: "new/name", Kind: model.RepositoryIdentityHubContext}}

	theme := DefaultTheme(lipgloss.NewRenderer(io.Discard))
	board := NewBoardModel([]model.Issue{issue}, theme)
	board.SetRepositoryPresentation(oldCatalog, true)
	board.ShowDetail()
	_ = board.renderDetailPanel(80, 30)
	if !strings.Contains(board.detailVP.View(), "old/name") {
		t.Fatalf("initial board detail missing old name: %s", board.detailVP.View())
	}
	board.SetRepositoryPresentation(newCatalog, true)
	_ = board.renderDetailPanel(80, 30)
	if !strings.Contains(board.detailVP.View(), "new/name") || strings.Contains(board.detailVP.View(), "old/name") {
		t.Fatalf("board detail cache stale: %s", board.detailVP.View())
	}

	issueMap := map[string]*model.Issue{"one": &issue}
	insights := NewInsightsModel(analysis.Insights{Bottlenecks: []analysis.InsightItem{{ID: "one"}}}, issueMap, theme)
	insights.SetRepositoryPresentation(oldCatalog, true)
	insights.updateDetailContent()
	insights.SetRepositoryPresentation(newCatalog, true)
	if !strings.Contains(insights.detailContent, "new/name") || strings.Contains(insights.detailContent, "old/name") {
		t.Fatalf("insights detail cache stale: %s", insights.detailContent)
	}
}

func TestHubContextCleanupIsTUIOnlyAndExact(t *testing.T) {
	issues := []model.Issue{{
		ID:     "one",
		Title:  "One",
		Status: model.StatusOpen,
		Labels: []string{"ctx:alpha", "Ctx:upper", "myctx:keep", "backend"},
	}}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", Kind: model.RepositoryIdentityHubContext}}
	m.refreshRepositoryPresentation()

	extraction := analysis.ExtractLabels(m.repositoryIssues)
	labels := filterHubContextLabels(extraction.Labels)
	if got := strings.Join(labels, ","); got != "Ctx:upper,backend,myctx:keep" {
		t.Fatalf("picker labels = %q", got)
	}
	health := analysis.ComputeAllLabelHealth(issues, analysis.DefaultLabelHealthConfig(), time.Now().UTC(), m.analysis)
	health = projectHubLabelHealth(health, true)
	if health.TotalLabels != 3 || len(health.Labels) != 3 {
		t.Fatalf("projected health metadata = %+v", health)
	}
	for _, label := range health.Labels {
		if label.Label == "ctx:alpha" {
			t.Fatalf("context label remained in health: %+v", health.Labels)
		}
	}
	if got := strings.Join(m.issues[0].Labels, ","); got != "ctx:alpha,Ctx:upper,myctx:keep,backend" {
		t.Fatalf("canonical labels changed: %q", got)
	}
}

func TestRenderAttentionViewUsesProvidedResult(t *testing.T) {
	result := analysis.LabelAttentionResult{Labels: []analysis.LabelAttentionScore{
		{Label: "second", AttentionScore: 9},
		{Label: "first", AttentionScore: 1},
	}}
	view := RenderAttentionView(result, 80)
	if strings.Index(view, "second") > strings.Index(view, "first") {
		t.Fatalf("attention display reordered cached result:\n%s", view)
	}
}

func TestHubCatalogRefreshPreservesActiveFuzzyResults(t *testing.T) {
	issue := model.Issue{ID: "one", Title: "Stable title", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "old/name", Kind: model.RepositoryIdentityHubContext}}
	m.refreshRepositoryPresentation()
	m.list.SetFilterText("Stable")
	if len(m.list.VisibleItems()) != 1 {
		t.Fatalf("initial fuzzy matches = %d", len(m.list.VisibleItems()))
	}

	m.applyRepositoryCatalogUpdate(model.RepositoryCatalog{{ID: "ctx:alpha", Name: "new/name", Kind: model.RepositoryIdentityHubContext}}, 1, true, false, nil)
	if len(m.list.VisibleItems()) != 1 || m.list.SelectedItem().(IssueItem).Issue.ID != "one" {
		t.Fatalf("catalog refresh lost fuzzy result: %+v", m.list.VisibleItems())
	}
	if !strings.Contains(m.list.VisibleItems()[0].FilterValue(), "new/name") {
		t.Fatalf("catalog refresh left stale fuzzy tokens: %q", m.list.VisibleItems()[0].FilterValue())
	}

	reloaded := model.Issue{ID: "two", Title: "Stable title", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}}
	updated, _ := m.Update(SnapshotReadyMsg{Snapshot: NewSnapshotBuilder([]model.Issue{reloaded}).Build(), SnapshotVer: 1})
	m = updated.(Model)
	if len(m.list.VisibleItems()) != 1 || m.list.SelectedItem().(IssueItem).Issue.ID != "two" {
		t.Fatalf("snapshot reload lost fuzzy result: %+v", m.list.VisibleItems())
	}
}

func TestHubCatalogRefreshResortsActiveContextModes(t *testing.T) {
	issues := []model.Issue{
		{ID: "one", Title: "Issue one", Status: model.StatusOpen, Labels: []string{"ctx:one"}},
		{ID: "two", Title: "Issue two", Status: model.StatusOpen, Labels: []string{"ctx:two"}},
	}
	oldCatalog := model.RepositoryCatalog{
		{ID: "ctx:one", Name: "Zulu", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:two", Name: "Alpha", Kind: model.RepositoryIdentityHubContext},
	}
	newCatalog := model.RepositoryCatalog{
		{ID: "ctx:one", Name: "Alpha", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:two", Name: "Zulu", Kind: model.RepositoryIdentityHubContext},
	}

	for _, mode := range []SortMode{SortContextCreated, SortContextPriority} {
		for _, filtered := range []bool{false, true} {
			name := mode.String()
			if filtered {
				name += "/filtered"
			}
			t.Run(name, func(t *testing.T) {
				m := NewModel(issues, nil, "")
				m.hubConfigPath = "hub.yaml"
				m.repositoryCatalog = oldCatalog
				m.sortMode = mode
				m.applyFilter()
				requireIssueIDs(t, visibleIssueIDs(m), "two", "one")
				if filtered {
					m.list.SetFilterText("")
				}
				m.list.Select(1)
				if selected := m.list.SelectedItem().(IssueItem).Issue.ID; selected != "one" {
					t.Fatalf("selected before catalog refresh = %q, want one", selected)
				}

				updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 1, Catalog: newCatalog})
				m = updated.(Model)
				requireIssueIDs(t, visibleIssueIDs(m), "one", "two")
				if selected := m.list.SelectedItem().(IssueItem).Issue.ID; selected != "one" {
					t.Fatalf("selected after catalog refresh = %q, want one", selected)
				}
			})
		}
	}
}

func TestHubLabelPickerAndAttentionActionsExcludeContextMetadata(t *testing.T) {
	issues := []model.Issue{{
		ID: "one", Title: "One", Status: model.StatusOpen,
		Labels: []string{"ctx:alpha", "Ctx:upper", "myctx:keep", "backend"},
	}}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", Kind: model.RepositoryIdentityHubContext}}
	m.refreshRepositoryPresentation()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if got := strings.Join(m.labelPicker.allLabels, ","); got != "Ctx:upper,backend,myctx:keep" {
		t.Fatalf("Hub label picker labels = %q", got)
	}
	m.showLabelPicker = false
	m.focused = focusList
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = updated.(Model)
	for _, health := range m.labelDashboard.labels {
		if strings.HasPrefix(health.Label, "ctx:") {
			t.Fatalf("label dashboard contains context metadata: %+v", m.labelDashboard.labels)
		}
	}
	m.focused = focusList

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = updated.(Model)
	for _, score := range m.attentionCache.Labels {
		if strings.HasPrefix(score.Label, "ctx:") {
			t.Fatalf("attention cache contains context metadata: %+v", m.attentionCache.Labels)
		}
	}
	if len(m.attentionCache.Labels) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
		m = updated.(Model)
		if strings.HasPrefix(m.currentFilter, "label:ctx:") {
			t.Fatalf("attention action selected context metadata: %q", m.currentFilter)
		}
	}
}

func TestRepositoryBadgeKeepsLocalFormatAndKeysHubColorByIdentity(t *testing.T) {
	if got := RenderRepoBadge("backend-service"); !strings.Contains(got, "[BACK]") {
		t.Fatalf("local repository badge changed: %q", got)
	}
	if GetRepoColor("ctx:alpha") == GetRepoColor("ctx:gamma") {
		t.Fatal("test identities unexpectedly share a color")
	}
	if got := RenderRepositoryBadge("ctx:viewer", "beads_viewer"); !strings.Contains(got, "[beads_viewer]") {
		t.Fatalf("Hub badge did not show full friendly name: %q", got)
	}
	if got := RenderRepositoryBadge("ctx:discovery", "mcp-discovery"); !strings.Contains(got, "[mcp-discovery]") {
		t.Fatalf("Hub badge did not show full mcp-discovery name: %q", got)
	}
	if got := RenderRepositoryBadgeCompact("ctx:long", "readable-repository-name", 10); got != "[readable-…]" {
		t.Fatalf("constrained Hub badge was not readably truncated: %q", got)
	}
	if got := RenderRepositoryBadgeCompact("ctx:wide", "仓库名称", 5); lipgloss.Width(got) != 7 || !strings.Contains(got, "…") {
		t.Fatalf("wide constrained Hub badge width = %d, badge %q", lipgloss.Width(got), got)
	}
}

func TestHubListRowShowsFullCommonRepositoryName(t *testing.T) {
	issue := model.Issue{ID: "issue-7td", Title: "Badge fix", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{
		ID: "ctx:alpha", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext,
	}}
	m.refreshRepositoryPresentation()
	row := m.list.View()
	if !strings.Contains(row, "[beads_viewer]") || strings.Contains(row, "bead…") {
		t.Fatalf("normal list row did not show full repository name: %q", row)
	}
}

func TestHubContextlessListRowShowsNoContextBadge(t *testing.T) {
	issue := model.Issue{ID: "todo-1", Title: "Inbox", Status: model.StatusOpen, IssueType: "todo"}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha")
	m.refreshRepositoryPresentation()
	if row := m.list.View(); !strings.Contains(row, "[no-context]") {
		t.Fatalf("contextless list row missing repository badge: %q", row)
	}
	if detail := m.viewport.View(); !strings.Contains(detail, "Repositories:") || !strings.Contains(detail, "no-context") || strings.Contains(detail, "Contextless") {
		t.Fatalf("contextless detail changed: %q", detail)
	}
}

func TestHubNoContextPickerStatusAndScopeBadges(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetHubScope(model.NewContextlessHubScope())
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context") || strings.Contains(picker, "Contextless") {
		t.Fatalf("picker presentation = %q", picker)
	}

	m = m.applyRepositoryPickerSelection()
	if m.statusMsg != "Repository scope: no-context" {
		t.Fatalf("pure scope status = %q", m.statusMsg)
	}
	if badge := m.renderRepositoryScopeBadge(80); !strings.Contains(badge, "no-context") || strings.Contains(badge, "Contextless") {
		t.Fatalf("pure scope badge = %q", badge)
	}

	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetHubScope(model.NewContextlessHubScope())
	m.repoPicker.MoveDown()
	m.repoPicker.ToggleSelected()
	m = m.applyRepositoryPickerSelection()
	if m.statusMsg != "Repository scope: ctx:alpha, no-context" {
		t.Fatalf("mixed scope status = %q", m.statusMsg)
	}
	if badge := m.renderRepositoryScopeBadge(80); !strings.Contains(badge, "no-context") || strings.Contains(badge, "CONTEXTLESS") || strings.Contains(badge, "Contextless") {
		t.Fatalf("mixed scope badge = %q", badge)
	}
}

func TestHubListRowConstrainsLongMultiContextBadge(t *testing.T) {
	issue := model.Issue{ID: "issue-7td", Title: "Badge fix", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "ctx:beta"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{
		{ID: "ctx:alpha", Name: "exceptionally-long-repository-name", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:beta", Name: "beta", Kind: model.RepositoryIdentityHubContext},
	}
	m.list.SetSize(50, 10)
	m.refreshRepositoryPresentation()
	row := m.list.View()
	if !containsAll(row, "…", "+1", "issue-7td") {
		t.Fatalf("narrow list row lost constrained badge metadata or ID: %q", row)
	}
}

func TestHubListRowsAlignSharedRepositoryColumn(t *testing.T) {
	issues := []model.Issue{
		{ID: "beads-id", Title: "Beads", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:beads"}},
		{ID: "dotfiles-id", Title: "Dotfiles", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:dotfiles"}},
		{ID: "long-id", Title: "Long", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:long"}},
		{ID: "mcp-id", Title: "MCP", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:mcp"}},
		{ID: "multi-id", Title: "Multi", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:dotfiles", "ctx:mcp"}},
	}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{
		{ID: "ctx:beads", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:dotfiles", Name: "dotfiles", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:long", Name: "an-extraordinarily-long-repository-name", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:mcp", Name: "mcp-discovery", Kind: model.RepositoryIdentityHubContext},
	}
	m.refreshRepositoryPresentation()
	nameWidth, extraWidth := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if nameWidth != 16 || extraWidth != 2 {
		t.Fatalf("repository columns = name:%d extra:%d, want 16 and 2", nameWidth, extraWidth)
	}

	view := m.list.View()
	if !containsAll(view, "[dotfiles]", "[beads_viewer]", "[mcp-discovery]", "[an-extraordinar…]", "+1") {
		t.Fatalf("repository rows missing expected badges:\n%s", view)
	}
	wantPrefixWidth := -1
	for _, id := range []string{"beads-id", "dotfiles-id", "long-id", "mcp-id", "multi-id"} {
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
			t.Fatalf("post-badge priority column drift for %s: got %d, want %d; row %q", id, prefixWidth, wantPrefixWidth, row)
		}
	}
}

func TestHubListColumnSuppressesWhenMetadataConsumesWidth(t *testing.T) {
	issue := model.Issue{
		ID: "an-extremely-long-visible-issue-identifier-that-is-truncated", Title: "Title",
		Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:dotfiles", "ctx:mcp"},
	}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{
		{ID: "ctx:dotfiles", Name: "dotfiles", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:mcp", Name: "mcp-discovery", Kind: model.RepositoryIdentityHubContext},
	}
	m.list.SetSize(45, 10)
	m.refreshRepositoryPresentation()
	delegate := IssueDelegate{Theme: m.theme}
	nameWidth, extraWidth := m.repositoryListColumnWidths(delegate)
	if nameWidth != 0 || extraWidth != 0 {
		t.Fatalf("overfull row repository columns = name:%d extra:%d, want suppressed", nameWidth, extraWidth)
	}
	view := m.list.View()
	if strings.Contains(view, "[dotfiles]") {
		t.Fatalf("narrow row did not suppress repository badge: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > m.list.Width() {
			t.Fatalf("narrow row width = %d, terminal width = %d: %q", width, m.list.Width(), line)
		}
	}
}

func TestHubListColumnRefreshesAfterSplitPaneResize(t *testing.T) {
	issue := model.Issue{ID: "resize-id", Title: "Resize", Status: model.StatusOpen, Labels: []string{"ctx:long"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{
		ID: "ctx:long", Name: "an-extraordinarily-long-repository-name", Kind: model.RepositoryIdentityHubContext,
	}}
	m.refreshRepositoryPresentation()
	m.width, m.height, m.isSplitView = 160, 40, true
	m.splitPaneRatio = 0.7
	m.recalculateSplitPaneSizes()
	if !strings.Contains(m.list.View(), "[an-extraordinar…]") {
		t.Fatalf("wide split pane did not render capped repository badge: %q", m.list.View())
	}
	m.splitPaneRatio = 0.2
	m.recalculateSplitPaneSizes()
	if strings.Contains(m.list.View(), "[an-") {
		t.Fatalf("narrow split pane retained stale repository column: %q", m.list.View())
	}
}

func TestHubRepositoryBadgeFitsNarrowBoardCard(t *testing.T) {
	issue := model.Issue{ID: "very-long-issue-id", Title: "Title", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "ctx:beta", "backend"}}
	board := NewBoardModel([]model.Issue{issue}, DefaultTheme(lipgloss.NewRenderer(io.Discard)))
	board.SetRepositoryPresentation(model.RepositoryCatalog{
		{ID: "ctx:alpha", Name: "alpha/service", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:beta", Name: "beta/service", Kind: model.RepositoryIdentityHubContext},
	}, true)
	card := board.renderCard(issue, 20, false, 0, 0)
	if !containsAll(card, "ver…", "+1") {
		t.Fatalf("narrow repository badge displaced issue ID or multi-context count: %q", card)
	}
	for _, line := range strings.Split(card, "\n") {
		if width := lipgloss.Width(line); width > 24 {
			t.Fatalf("narrow board card line width = %d: %q", width, line)
		}
	}
}

func TestRepositoryScopeSurvivesPhase2AndSnapshotReload(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Title: "Beta", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	m.analysis.WaitForPhase2()
	updated, _ := m.Update(Phase2ReadyMsg{Stats: m.analysis, Insights: m.analysis.GenerateInsights(len(m.issues))})
	m = updated.(Model)
	requireIssueIDs(t, visibleIssueIDs(m), "alpha")
	requireIssueIDs(t, m.graphView.sortedIDs, "alpha")

	reloaded := []model.Issue{
		{ID: "alpha-2", Title: "Alpha 2", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta-2", Title: "Beta 2", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	snapshot := NewSnapshotBuilder(reloaded).Build()
	updated, _ = m.Update(SnapshotReadyMsg{Snapshot: snapshot, SnapshotVer: 1})
	m = updated.(Model)
	requireIssueIDs(t, visibleIssueIDs(m), "alpha-2")
	if !m.RepositoryScope()["ctx:alpha"] {
		t.Fatalf("scope changed across snapshot reload: %v", m.RepositoryScope())
	}
}

func TestRepositoryScopeSurvivesSynchronousReload(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "beads.jsonl")
	data := "{\"id\":\"alpha-new\",\"title\":\"Alpha\",\"status\":\"open\",\"priority\":1,\"issue_type\":\"task\",\"labels\":[\"ctx:alpha\"]}\n" +
		"{\"id\":\"beta-new\",\"title\":\"Beta\",\"status\":\"open\",\"priority\":1,\"issue_type\":\"task\",\"labels\":[\"ctx:beta\"]}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write reload fixture: %v", err)
	}
	m := NewModel([]model.Issue{{ID: "alpha-old", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}}}, nil, path)
	if m.watcher != nil {
		defer m.watcher.Stop()
	}
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	updated, _ := m.Update(FileChangedMsg{})
	m = updated.(Model)
	if m.statusIsError {
		t.Fatalf("synchronous reload failed: %s", m.statusMsg)
	}
	if got := visibleIssueIDs(m); len(got) != 1 || got[0] != "alpha-new" {
		t.Fatalf("visible IDs = %v, want [alpha-new]; issues=%+v candidates=%+v scope=%v catalog=%+v", got, m.issues, m.repositoryIssues, m.RepositoryScope(), m.repositoryCatalog)
	}
	if !m.RepositoryScope()["ctx:alpha"] {
		t.Fatalf("scope changed across synchronous reload: %v", m.RepositoryScope())
	}
}

func TestRepositoryScopeInvalidatesSemanticCandidateCache(t *testing.T) {
	search := NewSemanticSearch()
	search.SetIDs([]string{"alpha", "beta"})
	firstGeneration := search.Snapshot().Generation
	search.SetCachedResults("query", []list.Rank{{Index: 1}})
	search.SetIDs([]string{"alpha"})

	if search.Snapshot().Generation == firstGeneration {
		t.Fatal("candidate generation did not advance")
	}
	if _, exists := search.getCache().results["query"]; exists {
		t.Fatal("semantic cache survived candidate scope change")
	}
}

func TestRepositoryScopeInvalidatesSemanticGenerationWhenDocsChange(t *testing.T) {
	search := NewSemanticSearch()
	search.SetIDs([]string{"alpha"})
	search.SetDocs(map[string]string{"alpha": "old title"})
	firstGeneration := search.Snapshot().Generation
	search.SetCachedResults("query", []list.Rank{{Index: 0}})
	search.SetDocs(map[string]string{"alpha": "new title"})

	if search.Snapshot().Generation == firstGeneration {
		t.Fatal("document-only candidate change did not advance generation")
	}
	if _, exists := search.getCache().results["query"]; exists {
		t.Fatal("semantic cache survived document-only candidate change")
	}
}

func TestRepositoryScopeTriageRanksWithinSelectedCandidates(t *testing.T) {
	issues := []model.Issue{
		{ID: "selected", Title: "Selected", Status: model.StatusOpen, Priority: 4, Labels: []string{"ctx:alpha"}},
		{ID: "global-1", Title: "Global 1", Status: model.StatusOpen, Priority: 0, Labels: []string{"ctx:beta"}},
		{ID: "global-2", Title: "Global 2", Status: model.StatusOpen, Priority: 0, Labels: []string{"ctx:beta"}},
		{ID: "global-3", Title: "Global 3", Status: model.StatusOpen, Priority: 0, Labels: []string{"ctx:beta"}},
		{ID: "global-4", Title: "Global 4", Status: model.StatusOpen, Priority: 0, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})
	triage := m.scopedTriage()

	if len(triage.Recommendations) != 1 || triage.Recommendations[0].ID != "selected" {
		t.Fatalf("scoped recommendations = %+v, want selected candidate", triage.Recommendations)
	}
	if len(triage.QuickRef.TopPicks) != 1 || triage.QuickRef.TopPicks[0].ID != "selected" {
		t.Fatalf("scoped top picks = %+v, want selected candidate", triage.QuickRef.TopPicks)
	}
}

func TestRepositoryScopeTriageKeepsExternalBlockerForTopPickEligibility(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha-work", Title: "Alpha work", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}, Dependencies: []*model.Dependency{
			{DependsOnID: "beta-blocker", Type: model.DepBlocks},
		}},
		{ID: "beta-blocker", Title: "Beta blocker", Status: model.StatusOpen, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	triage := m.scopedTriage()
	if len(triage.Recommendations) != 1 || triage.Recommendations[0].ID != "alpha-work" {
		t.Fatalf("scoped recommendations = %+v, want alpha-work", triage.Recommendations)
	}
	if len(triage.Recommendations[0].BlockedBy) != 0 {
		t.Fatalf("display projection retained external blocker: %v", triage.Recommendations[0].BlockedBy)
	}
	if len(triage.QuickRef.TopPicks) != 0 {
		t.Fatalf("externally blocked issue became a top pick: %+v", triage.QuickRef.TopPicks)
	}
}

func TestRepositoryScopeTriageKeepsParentBlockedByExternalOpenChild(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha-parent", Title: "Alpha parent", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta-child", Title: "Beta child", Status: model.StatusOpen, Labels: []string{"ctx:beta"}, Dependencies: []*model.Dependency{
			{DependsOnID: "alpha-parent", Type: model.DepParentChild},
		}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})

	triage := m.scopedTriage()
	if len(triage.Recommendations) != 1 || triage.Recommendations[0].ID != "alpha-parent" {
		t.Fatalf("scoped recommendations = %+v, want alpha-parent", triage.Recommendations)
	}
	if len(triage.QuickRef.TopPicks) != 0 {
		t.Fatalf("parent with external open child became a top pick: %+v", triage.QuickRef.TopPicks)
	}
}

func TestInsightsProjectionDropsClosedAndTombstonedIssues(t *testing.T) {
	issues := []model.Issue{
		{ID: "active", Title: "Active", Status: model.StatusOpen},
		{ID: "closed", Title: "Closed", Status: model.StatusClosed},
		{ID: "tombstone", Title: "Tombstone", Status: model.StatusTombstone},
	}
	m := NewModel(issues, nil, "")
	stats := analysis.NewGraphStatsForTest(
		nil, nil, nil, nil, nil,
		map[string]float64{"active": 0, "closed": 2, "tombstone": 3},
		nil, nil, nil, 0, nil,
	)
	m.insightsPanel.SetInsights(analysis.Insights{
		Bottlenecks:  []analysis.InsightItem{{ID: "active"}, {ID: "closed"}, {ID: "tombstone"}},
		Keystones:    []analysis.InsightItem{{ID: "active"}, {ID: "closed"}},
		Influencers:  []analysis.InsightItem{{ID: "active"}, {ID: "tombstone"}},
		Hubs:         []analysis.InsightItem{{ID: "active"}, {ID: "closed"}},
		Authorities:  []analysis.InsightItem{{ID: "active"}, {ID: "tombstone"}},
		Cores:        []analysis.InsightItem{{ID: "active"}, {ID: "closed"}},
		Articulation: []string{"active", "tombstone"},
		Slack:        []analysis.InsightItem{{ID: "active"}, {ID: "closed"}},
		Orphans:      []string{"active", "tombstone"},
		Cycles:       [][]string{{"active", "closed", "active"}, {"active", "active"}},
		Stats:        stats,
	})
	m.insightsPanel.SetTopPicks([]analysis.TopPick{{ID: "active"}, {ID: "closed"}, {ID: "tombstone"}})
	m.insightsPanel.SetRecommendations([]analysis.Recommendation{
		{ID: "active", UnblocksIDs: []string{"closed", "active"}, BlockedBy: []string{"tombstone"}},
		{ID: "closed"},
		{ID: "tombstone"},
	}, "test")
	m.insightsPanel.heatmapIssues = []string{"closed", "active"}
	m.insightsPanel.heatmapDrill = true
	m.insightsPanel.heatmapRow = 0
	m.insightsPanel.SetActiveIssueIDs(m.insightsIssueIDs())
	if got := m.insightsPanel.HeatmapSelectedIssueID(); got != "active" {
		t.Fatalf("selection projection = %q, want active", got)
	}
	m.insightsPanel.ToggleHeatmap()

	for _, item := range m.insightsPanel.insights.Bottlenecks {
		if item.ID != "active" {
			t.Fatalf("projected bottlenecks retained %q", item.ID)
		}
	}
	for _, items := range [][]analysis.InsightItem{
		m.insightsPanel.insights.Keystones,
		m.insightsPanel.insights.Influencers,
		m.insightsPanel.insights.Hubs,
		m.insightsPanel.insights.Authorities,
		m.insightsPanel.insights.Cores,
		m.insightsPanel.insights.Slack,
	} {
		for _, item := range items {
			if item.ID != "active" {
				t.Fatalf("projected metric retained %q", item.ID)
			}
		}
	}
	if len(m.insightsPanel.insights.Cycles) != 1 || !reflect.DeepEqual(m.insightsPanel.insights.Cycles[0], []string{"active", "active"}) {
		t.Fatalf("projected cycles = %v", m.insightsPanel.insights.Cycles)
	}
	for _, id := range append(append(append([]string{}, m.insightsPanel.insights.Articulation...), m.insightsPanel.insights.Orphans...), m.insightsPanel.insights.Cycles[0]...) {
		if id != "active" {
			t.Fatalf("projected string insight retained %q", id)
		}
	}
	if len(m.insightsPanel.topPicks) != 1 || m.insightsPanel.topPicks[0].ID != "active" {
		t.Fatalf("projected top picks = %+v", m.insightsPanel.topPicks)
	}
	if len(m.insightsPanel.recommendations) != 1 || m.insightsPanel.recommendations[0].ID != "active" {
		t.Fatalf("projected recommendations = %+v", m.insightsPanel.recommendations)
	}
	if got := m.insightsPanel.recommendations[0].UnblocksIDs; len(got) != 1 || got[0] != "active" {
		t.Fatalf("projected recommendation unblocks = %v", got)
	}
	if len(m.insightsPanel.heatmapIssueMap) == 0 {
		t.Fatal("expected heat-map cache")
	}
	for _, row := range m.insightsPanel.heatmapIssueMap {
		for _, ids := range row {
			for _, id := range ids {
				if id != "active" {
					t.Fatalf("heat-map cache retained %q", id)
				}
			}
		}
	}
}

func TestInsightsStatusScopeIsIndependentFromListAndBoard(t *testing.T) {
	issues := []model.Issue{
		{ID: "ready", Title: "Ready", Status: model.StatusOpen},
		{ID: "blocked", Title: "Blocked", Status: model.StatusBlocked},
		{ID: "closed", Title: "Closed", Status: model.StatusClosed},
		{ID: "tombstone", Title: "Tombstone", Status: model.StatusTombstone},
	}
	m := NewModel(issues, nil, "")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)
	if m.currentFilter != "closed" {
		t.Fatalf("list did not enter closed filter: %q", m.currentFilter)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)
	if m.focused != focusInsights || m.insightsStatusFilter != "" {
		t.Fatalf("Insights inherited List state: focus=%v status=%q", m.focused, m.insightsStatusFilter)
	}
	if len(m.insightsPanel.insights.Bottlenecks) > 0 && m.insightsPanel.insights.Bottlenecks[0].ID == "closed" {
		t.Fatal("Insights inherited closed selection")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if m.insightsStatusFilter != "ready" || len(m.insightsIssueIDs()) != 1 || !m.insightsIssueIDs()["ready"] {
		t.Fatalf("ready scope = %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if m.insightsStatusFilter != "" || len(m.insightsIssueIDs()) != 2 {
		t.Fatalf("active scope after ready toggle = %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(Model)
	if m.insightsStatusFilter != "open" || len(m.insightsIssueIDs()) != 2 || !strings.Contains(m.renderFooter(), "OPEN") {
		t.Fatalf("o did not enable open scope: %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(Model)
	if m.insightsStatusFilter != "" || len(m.insightsIssueIDs()) != 2 || !strings.Contains(m.renderFooter(), "ACTIVE") {
		t.Fatalf("o did not clear to broad active scope: %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if m.insightsStatusFilter != "ready" || len(m.insightsIssueIDs()) != 1 {
		t.Fatalf("r did not enable ready scope after open toggle: %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(Model)
	if m.insightsStatusFilter != "open" || len(m.insightsIssueIDs()) != 2 || !strings.Contains(m.renderFooter(), "OPEN") {
		t.Fatalf("o did not switch ready scope to open: %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if m.insightsStatusFilter != "ready" || len(m.insightsIssueIDs()) != 1 {
		t.Fatalf("r did not switch open scope to ready: %q, ids=%v", m.insightsStatusFilter, m.insightsIssueIDs())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)
	if m.insightsStatusFilter != "ready" || m.currentFilter != "closed" {
		t.Fatalf("Insights c disturbed state: insights=%q list=%q", m.insightsStatusFilter, m.currentFilter)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = updated.(Model)
	if m.focused != focusList {
		t.Fatalf("Insights escape focus = %v, want list", m.focused)
	}
	if got := visibleIssueIDs(m); len(got) != 2 || got[0] != "closed" || got[1] != "tombstone" {
		t.Fatalf("List filter was not preserved: %v", got)
	}

	boardModel := NewModel(issues, nil, "")
	updated, _ = boardModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	boardModel = updated.(Model)
	updated, _ = boardModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	boardModel = updated.(Model)
	updated, _ = boardModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	boardModel = updated.(Model)
	if boardModel.focused != focusInsights || len(boardModel.insightsIssueIDs()) != 2 {
		t.Fatalf("Insights from closed Board was not broad active: focus=%v ids=%v", boardModel.focused, boardModel.insightsIssueIDs())
	}
}

func TestInsightsProjectionSurvivesPhaseAndSnapshotRefresh(t *testing.T) {
	issues := []model.Issue{
		{ID: "active", Status: model.StatusOpen},
		{ID: "closed", Status: model.StatusClosed},
		{ID: "tombstone", Status: model.StatusTombstone},
	}
	m := NewModel(issues, nil, "")
	m.focused = focusInsights
	m.insightsStatusFilter = "ready"
	m.analysis.WaitForPhase2()
	updated, _ := m.Update(Phase2ReadyMsg{Stats: m.analysis, Insights: m.analysis.GenerateInsights(len(m.issues))})
	m = updated.(Model)
	if len(m.insightsPanel.topPicks) > 0 && m.insightsPanel.topPicks[0].ID != "active" {
		t.Fatalf("phase refresh top pick = %+v", m.insightsPanel.topPicks)
	}
	snapshot := NewSnapshotBuilder(issues).Build()
	updated, _ = m.Update(SnapshotReadyMsg{Snapshot: snapshot, SnapshotVer: 1})
	m = updated.(Model)
	for _, pick := range m.insightsPanel.topPicks {
		if pick.ID == "closed" || pick.ID == "tombstone" {
			t.Fatalf("snapshot refresh reintroduced closed top pick %q", pick.ID)
		}
	}
	if m.insightsStatusFilter != "ready" {
		t.Fatalf("snapshot refresh changed Insights status scope: %q", m.insightsStatusFilter)
	}
}

func TestRepositoryScopeEmptySnapshotClearsHistory(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "alpha", Status: model.StatusOpen}}, nil, "")
	m.historyReport = &correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{
		"alpha": {BeadID: "alpha"},
	}}
	m.historyView.SetReport(m.historyReport)
	updated, _ := m.Update(SnapshotReadyMsg{Snapshot: NewSnapshotBuilder(nil).Build(), SnapshotVer: 1})
	m = updated.(Model)

	if m.historyReport != nil || m.historyView.report != nil {
		t.Fatalf("empty reload retained stale history: report=%v view=%v", m.historyReport, m.historyView.report)
	}
}

func TestRepositoryHistoryProjectionPreservesCommitIdentity(t *testing.T) {
	now := time.Now()
	report := &correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{
		"alpha": {BeadID: "alpha", Commits: []correlation.CorrelatedCommit{{Repository: "ctx:alpha", SHA: "shared", Message: "alpha", Timestamp: now}}},
		"beta":  {BeadID: "beta", Commits: []correlation.CorrelatedCommit{{Repository: "ctx:beta", SHA: "shared", Message: "beta", Timestamp: now.Add(time.Minute)}}},
	}}
	h := NewHistoryModel(report, testTheme())
	h.ToggleViewMode()
	for index, commit := range h.GetFilteredCommitList() {
		if commit.Repository == "ctx:alpha" && commit.SHA == "shared" {
			h.selectedGitCommit = index
		}
	}

	projected := projectHistoryReport(report, map[string]bool{"alpha": true}, map[string]bool{"ctx:alpha": true})
	h.SetReport(projected)

	commit := h.SelectedGitCommit()
	if commit == nil || commit.Repository != "ctx:alpha" || commit.SHA != "shared" {
		t.Fatalf("repository projection did not preserve repository-qualified commit selection: %#v", commit)
	}
}

func TestScopedIssueParticipatesInCrossRepositoryLabelCycle(t *testing.T) {
	issues := []model.Issue{
		{ID: "visible", Labels: []string{"work"}, Dependencies: []*model.Dependency{{DependsOnID: "hidden", Type: model.DepBlocks}}},
		{ID: "hidden", Labels: []string{"work"}, Dependencies: []*model.Dependency{{DependsOnID: "visible", Type: model.DepBlocks}}},
	}
	subgraph := analysis.ComputeLabelSubgraph(issues, "work")
	if !scopedIssueParticipatesInLabelCycle(subgraph, map[string]bool{"visible": true}) {
		t.Fatal("cross-repository cycle involving visible issue was not detected")
	}

	hiddenOnly := []model.Issue{
		{ID: "visible", Labels: []string{"work"}},
		{ID: "hidden-a", Labels: []string{"work"}, Dependencies: []*model.Dependency{{DependsOnID: "hidden-b", Type: model.DepBlocks}}},
		{ID: "hidden-b", Labels: []string{"work"}, Dependencies: []*model.Dependency{{DependsOnID: "hidden-a", Type: model.DepBlocks}}},
	}
	subgraph = analysis.ComputeLabelSubgraph(hiddenOnly, "work")
	if scopedIssueParticipatesInLabelCycle(subgraph, map[string]bool{"visible": true}) {
		t.Fatal("hidden-only cycle was attributed to visible issue")
	}
}

func TestRepositoryScopeLegacyWorkspaceSourcePrefixRegression(t *testing.T) {
	issues := []model.Issue{
		{ID: "ambiguous-id", SourceRepo: "backend-service", Status: model.StatusOpen},
		{ID: "orphan", Status: model.StatusOpen},
		{ID: "web-item", SourceRepo: "web", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 2, RepoPrefixes: []string{"backend-service-", "web-"}})
	m.SetRepositoryScope(map[string]bool{"backend-service": true})

	// SourceRepo remains authoritative, and legacy unscoped workspace rows remain visible.
	requireIssueIDs(t, visibleIssueIDs(m), "ambiguous-id", "orphan")
}

func TestRepositoryScopeClearingOtherFiltersPreservesScope(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}},
		{ID: "beta", Status: model.StatusClosed, Labels: []string{"ctx:beta"}},
	}
	m := NewModel(issues, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	m.SetRepositoryScope(map[string]bool{"ctx:alpha": true})
	m.currentFilter = "closed"
	m.applyFilter()
	m.clearAllFilters()

	requireIssueIDs(t, visibleIssueIDs(m), "alpha")
	if !m.RepositoryScope()["ctx:alpha"] {
		t.Fatalf("clearAllFilters cleared repository scope: %v", m.RepositoryScope())
	}
}
