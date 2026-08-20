package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"

	"github.com/charmbracelet/bubbles/list"
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
	if m.RepositoryScope() != nil {
		t.Fatalf("full selection must normalize to all, got %v", m.RepositoryScope())
	}
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
