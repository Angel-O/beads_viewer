package ui

import (
	"encoding/json"
	"io"
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

	presentation := repositoryPresentationForIssue(issue, catalog, true)
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
	if got := RenderRepositoryBadge("ctx:beads-viewer-c67191f28f", "beads_viewer"); !strings.Contains(got, "[beads_viewer]") {
		t.Fatalf("Hub badge did not show full friendly name: %q", got)
	}
	if got := RenderRepositoryBadge("ctx:mcp-discovery-e7538468e4", "mcp-discovery"); !strings.Contains(got, "[mcp-discovery]") {
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
	issue := model.Issue{ID: "global-7td", Title: "Badge fix", Status: model.StatusOpen, Labels: []string{"ctx:beads-viewer-c67191f28f"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{
		ID: "ctx:beads-viewer-c67191f28f", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext,
	}}
	m.refreshRepositoryPresentation()
	row := m.list.View()
	if !strings.Contains(row, "[beads_viewer]") || strings.Contains(row, "bead…") {
		t.Fatalf("normal list row did not show full repository name: %q", row)
	}
}

func TestHubListRowConstrainsLongMultiContextBadge(t *testing.T) {
	issue := model.Issue{ID: "global-7td", Title: "Badge fix", Status: model.StatusOpen, Labels: []string{"ctx:alpha", "ctx:beta"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{
		{ID: "ctx:alpha", Name: "exceptionally-long-repository-name", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:beta", Name: "beta", Kind: model.RepositoryIdentityHubContext},
	}
	m.list.SetSize(50, 10)
	m.refreshRepositoryPresentation()
	row := m.list.View()
	if !containsAll(row, "…", "+1", "global-7td") {
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
	m.list.SetSize(60, 10)
	m.refreshRepositoryPresentation()
	delegate := IssueDelegate{Theme: m.theme}
	nameWidth, extraWidth := m.repositoryListColumnWidths(delegate)
	if nameWidth != 0 || extraWidth != 0 {
		t.Fatalf("overfull row repository columns = name:%d extra:%d, want suppressed", nameWidth, extraWidth)
	}
	view := m.list.View()
	if strings.Contains(view, "[dotfiles]") || !strings.Contains(view, "an-extremely-long-visible-issue-id…") {
		t.Fatalf("overfull row did not suppress badge while preserving ID: %q", view)
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
