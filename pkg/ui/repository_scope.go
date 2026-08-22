package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

type issueRepositoryPresentation struct {
	ID     string
	Name   string
	Extra  int
	Names  []string
	Labels []string
}

type hubRelationshipEvidence struct {
	Label    string
	Endpoint *model.Issue
}

const contextlessRepositoryID = "no-context"

func isHubContextLabel(label string) bool {
	return strings.HasPrefix(label, "ctx:")
}

func repositoryPresentationForIssue(issue model.Issue, catalog model.RepositoryCatalog, hubMode bool, preferredRepositories map[string]bool) issueRepositoryPresentation {
	presentation := issueRepositoryPresentation{Labels: issue.Labels}
	if !hubMode {
		return presentation
	}

	presentation.Labels = make([]string, 0, len(issue.Labels))
	contexts := make(map[string]bool)
	for _, label := range issue.Labels {
		if isHubContextLabel(label) {
			contexts[label] = true
			continue
		}
		presentation.Labels = append(presentation.Labels, label)
	}

	matches := make([]model.RepositoryCatalogEntry, 0, len(contexts))
	for _, repository := range catalog {
		if repository.Kind == model.RepositoryIdentityHubContext && contexts[repository.ID] {
			matches = append(matches, repository)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	if len(matches) == 0 {
		if len(contexts) == 0 {
			presentation.ID = contextlessRepositoryID
			presentation.Name = contextlessRepositoryID
		}
		return presentation
	}
	primary := matches[0]
	for _, repository := range matches {
		if len(preferredRepositories) > 0 && !preferredRepositories[repository.ID] {
			continue
		}
		if len(preferredRepositories) > 0 && !preferredRepositories[primary.ID] || repository.Name > primary.Name || repository.Name == primary.Name && repository.ID > primary.ID {
			primary = repository
		}
	}
	presentation.ID = primary.ID
	presentation.Name = primary.Name
	presentation.Extra = len(matches) - 1
	presentation.Names = make([]string, 0, len(matches))
	for _, repository := range matches {
		presentation.Names = append(presentation.Names, repository.Name)
	}
	return presentation
}

func hubContextNames(issue model.Issue, catalog model.RepositoryCatalog) []string {
	namesByID := make(map[string]string, len(catalog))
	for _, repository := range catalog {
		if repository.Kind == model.RepositoryIdentityHubContext {
			name := repository.Name
			if name == "" {
				name = repository.ID
			}
			namesByID[repository.ID] = name
		}
	}
	contexts := make([]string, 0)
	for _, label := range issue.Labels {
		if !isHubContextLabel(label) {
			continue
		}
		name := namesByID[label]
		if name == "" {
			name = label
		}
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)
	if len(contexts) == 0 {
		return []string{"contextless"}
	}
	return contexts
}

func (m Model) hubRelationshipMarkdown(issue model.Issue) string {
	if !m.hubRepositoryPresentation() {
		return ""
	}
	evidence := make([]hubRelationshipEvidence, 0)
	for _, dependency := range issue.Dependencies {
		if dependency == nil {
			continue
		}
		endpoint := m.issueMap[dependency.DependsOnID]
		if endpoint == nil {
			continue
		}
		label := ""
		switch dependency.Type {
		case model.DepBlocks, "":
			label = "Blocked by"
		case model.DepDiscoveredFrom:
			label = "Result of todo"
		case model.DepSupersedes:
			label = "Supersedes original"
		case model.DepParentChild:
			label = "Parent"
		}
		if label != "" {
			evidence = append(evidence, hubRelationshipEvidence{Label: label, Endpoint: endpoint})
		}
	}
	supersededOriginal := false
	for i := range m.issues {
		candidate := &m.issues[i]
		for _, dependency := range candidate.Dependencies {
			if dependency == nil || dependency.DependsOnID != issue.ID {
				continue
			}
			label := ""
			switch dependency.Type {
			case model.DepBlocks, "":
				label = "Blocks"
			case model.DepDiscoveredFrom:
				label = "Results in"
			case model.DepSupersedes:
				label = "Superseded by"
				supersededOriginal = true
			case model.DepParentChild:
				label = "Child"
			}
			if label != "" {
				evidence = append(evidence, hubRelationshipEvidence{Label: label, Endpoint: candidate})
			}
		}
	}
	if len(evidence) == 0 && !(supersededOriginal && issue.CloseReason != "") {
		return ""
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Label != evidence[j].Label {
			return evidence[i].Label < evidence[j].Label
		}
		return evidence[i].Endpoint.ID < evidence[j].Endpoint.ID
	})

	var sb strings.Builder
	sb.WriteString("### Relationships\n")
	if supersededOriginal && issue.CloseReason != "" {
		sb.WriteString(fmt.Sprintf("- **Close reason:** %s\n", issue.CloseReason))
	}
	for _, relation := range evidence {
		endpoint := relation.Endpoint
		boundary := ""
		if !m.issueMatchesRepositoryScope(*endpoint) {
			boundary = " _(out of scope)_"
		}
		sb.WriteString(fmt.Sprintf("- **%s:** `%s` %s (%s; contexts: %s)%s\n",
			relation.Label, endpoint.ID, endpoint.Title, endpoint.Status,
			strings.Join(hubContextNames(*endpoint, m.repositoryCatalog), ", "), boundary))
	}
	sb.WriteString("\n")
	return sb.String()
}

func (m *Model) hubRepositoryPresentation() bool {
	return !m.workspaceMode && strings.TrimSpace(m.hubConfigPath) != ""
}

func (m *Model) decorateIssueItem(item *IssueItem) {
	if item == nil {
		return
	}
	presentation := repositoryPresentationForIssue(item.Issue, m.repositoryCatalog, m.hubRepositoryPresentation(), m.activeRepos)
	item.HubPresentation = m.hubRepositoryPresentation()
	item.RepositoryID = presentation.ID
	item.RepositoryName = presentation.Name
	item.RepositoryExtra = presentation.Extra
	item.RepositoryNames = presentation.Names
	item.PresentationLabels = presentation.Labels
}

func projectHubLabelHealth(result analysis.LabelAnalysisResult, hubMode bool) analysis.LabelAnalysisResult {
	if !hubMode {
		return result
	}
	result.TotalLabels, result.HealthyCount, result.WarningCount, result.CriticalCount = 0, 0, 0, 0
	result.Labels = slicesDeleteContextHealth(result.Labels)
	result.Summaries = slicesDeleteContextSummaries(result.Summaries)
	result.AttentionNeeded = filterHubContextLabels(result.AttentionNeeded)
	result.TotalLabels = len(result.Labels)
	for _, health := range result.Labels {
		switch health.HealthLevel {
		case analysis.HealthLevelHealthy:
			result.HealthyCount++
		case analysis.HealthLevelWarning:
			result.WarningCount++
		case analysis.HealthLevelCritical:
			result.CriticalCount++
		}
	}
	return result
}

func slicesDeleteContextHealth(values []analysis.LabelHealth) []analysis.LabelHealth {
	filtered := make([]analysis.LabelHealth, 0, len(values))
	for _, value := range values {
		if !isHubContextLabel(value.Label) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func slicesDeleteContextSummaries(values []analysis.LabelSummary) []analysis.LabelSummary {
	filtered := make([]analysis.LabelSummary, 0, len(values))
	for _, value := range values {
		if !isHubContextLabel(value.Label) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterHubContextLabels(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if !isHubContextLabel(value) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func (m *Model) refreshRepositoryPresentation() {
	hubMode := m.hubRepositoryPresentation()
	if m.list.Width() > 0 {
		items := m.list.Items()
		for i := range items {
			item, ok := items[i].(IssueItem)
			if !ok {
				continue
			}
			m.decorateIssueItem(&item)
			items[i] = item
		}
		m.setListItemsPreservingFilter(items)
		m.updateListDelegate()
		m.updateViewportContent()
	}
	m.board.SetRepositoryPresentation(m.repositoryCatalog, hubMode)
	m.insightsPanel.SetRepositoryPresentation(m.repositoryCatalog, hubMode)
}

func (m *Model) issueMatchesRepositoryScope(issue model.Issue) bool {
	if m.usesHubScope() {
		return m.hubScope.MatchesLabels(issue.Labels)
	}
	if m.activeRepos == nil {
		return true
	}

	workspaceKey := ""
	for _, repository := range m.repositoryCatalog {
		if !m.activeRepos[repository.ID] {
			continue
		}
		switch repository.Kind {
		case model.RepositoryIdentityHubContext:
			if repository.ID != strings.ToLower(repository.ID) || !strings.HasPrefix(repository.ID, "ctx:") {
				continue
			}
			for _, label := range issue.Labels {
				if label == repository.ID {
					return true
				}
			}
		case model.RepositoryIdentityWorkspacePrefix:
			if workspaceKey == "" {
				workspaceKey = issueRepoKey(issue)
			}
			if workspaceKey == repository.ID {
				return true
			}
		}
	}

	// Legacy workspace filtering did not hide issues lacking a source/prefix.
	return m.workspaceMode && workspaceKey == ""
}

func (m *Model) repositoryCandidates() []model.Issue {
	if (!m.usesHubScope() && m.activeRepos == nil) || (m.usesHubScope() && m.hubScope.Mode == model.HubScopeAllItems) {
		return m.issues
	}
	candidates := make([]model.Issue, 0, len(m.issues))
	for _, issue := range m.issues {
		if m.issueMatchesRepositoryScope(issue) {
			candidates = append(candidates, issue)
		}
	}
	return candidates
}

func (m Model) repositoryScopeIsAll() bool {
	if m.usesHubScope() {
		return m.hubScope.Mode == model.HubScopeAllItems
	}
	return m.activeRepos == nil
}

func (m Model) usesHubScope() bool {
	if m.workspaceMode {
		return false
	}
	if m.hubRepositoryMode || strings.TrimSpace(m.hubConfigPath) != "" {
		return true
	}
	for _, repository := range m.repositoryCatalog {
		if repository.Kind == model.RepositoryIdentityHubContext {
			return true
		}
	}
	return false
}

func issueIDSet(issues []model.Issue) map[string]bool {
	ids := make(map[string]bool, len(issues))
	for _, issue := range issues {
		ids[issue.ID] = true
	}
	return ids
}

func repositoryCatalogIDs(catalog model.RepositoryCatalog) []string {
	ids := make([]string, 0, len(catalog))
	for _, repository := range catalog {
		ids = append(ids, repository.ID)
	}
	return ids
}

func (m *Model) reconcileHubScopeCatalog() {
	if !m.usesHubScope() || m.hubScope.Mode != model.HubScopeSelectedContexts {
		return
	}
	selected := make(map[string]bool, len(m.hubScope.Contexts))
	for _, contextID := range m.hubScope.Contexts {
		selected[contextID] = true
	}
	reconciled := model.ReconcileRepositorySelection(selected, m.repositoryCatalog)
	if reconciled == nil {
		if m.hubScope.IncludeContextless {
			m.hubScope = model.NewContextlessHubScope()
		} else {
			m.hubScope = model.NewAllItemsHubScope()
		}
		m.activeRepos = nil
		return
	}
	if m.hubScope.IncludeContextless && len(reconciled) == len(m.repositoryCatalog) {
		m.hubScope = model.NewAllItemsHubScope()
		m.activeRepos = nil
		return
	}
	var scope model.HubScope
	var err error
	if m.hubScope.IncludeContextless {
		scope, err = model.NewSelectedContextsAndContextlessHubScope(sortedRepoKeys(reconciled))
	} else {
		scope, err = model.NewSelectedContextsHubScope(sortedRepoKeys(reconciled))
	}
	if err != nil {
		m.hubScope = model.NewAllItemsHubScope()
		m.activeRepos = nil
		return
	}
	m.hubScope = scope
	m.activeRepos = reconciled
}

// SetDefaultRepositoryScope applies an exact Hub context once the initial
// repository catalog is available. A single-entry catalog remains an explicit
// selection so repositories registered later do not silently join the scope.
func (m *Model) SetDefaultRepositoryScope(repositoryID string) bool {
	if repositoryID == "" || m.workspaceMode || !m.hubRepositoryMode || m.defaultRepositorySet {
		return false
	}
	m.defaultRepositoryID = repositoryID
	return m.applyDefaultRepositoryScope()
}

func (m *Model) applyDefaultRepositoryScope() bool {
	if m.defaultRepositorySet || m.defaultRepositoryID == "" || !m.repositoryCatalogReady {
		return false
	}
	m.defaultRepositorySet = true
	for _, repository := range m.repositoryCatalog {
		if repository.Kind != model.RepositoryIdentityHubContext || repository.ID != m.defaultRepositoryID {
			continue
		}
		m.activeRepos = map[string]bool{repository.ID: true}
		scope, err := model.NewSelectedContextsHubScope([]string{repository.ID})
		if err != nil {
			return false
		}
		m.hubScope = scope
		m.refreshRepositoryCandidates()
		return true
	}
	return false
}

// SetRepositoryScope applies exact catalog IDs. Nil and an empty selection mean
// the complete universe; selecting every Hub repository excludes contextless items.
func (m *Model) SetRepositoryScope(selected map[string]bool) {
	m.defaultRepositorySet = true
	m.defaultRepositoryID = ""
	reconciled := model.ReconcileRepositorySelection(selected, m.repositoryCatalog)
	if m.usesHubScope() {
		if len(selected) == 0 || len(reconciled) == 0 {
			_ = m.SetHubScope(model.NewAllItemsHubScope())
			return
		}
		contexts := sortedRepoKeys(reconciled)
		scope, err := model.NewSelectedContextsHubScope(contexts)
		if err == nil {
			_ = m.SetHubScope(scope)
		}
		return
	}
	if len(selected) == 0 || len(reconciled) == len(m.repositoryCatalog) {
		m.activeRepos = nil
	} else {
		m.activeRepos = reconciled
	}
	m.refreshRepositoryCandidates()
}

func (m *Model) setHubRepositoryScope(selected map[string]bool, includeContextless bool) {
	if len(selected) == 0 {
		if includeContextless {
			_ = m.SetHubScope(model.NewContextlessHubScope())
		} else {
			_ = m.SetHubScope(model.NewAllItemsHubScope())
		}
		return
	}
	if includeContextless && len(selected) == len(m.repositoryCatalog) {
		_ = m.SetHubScope(model.NewAllItemsHubScope())
		return
	}
	contexts := sortedRepoKeys(selected)
	var scope model.HubScope
	var err error
	if includeContextless {
		scope, err = model.NewSelectedContextsAndContextlessHubScope(contexts)
	} else {
		scope, err = model.NewSelectedContextsHubScope(contexts)
	}
	if err == nil {
		_ = m.SetHubScope(scope)
	}
}

// SetHubScope applies an explicit Hub candidate selector. Selected context IDs
// must be present in the current Hub repository catalog.
func (m *Model) SetHubScope(scope model.HubScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.Mode == model.HubScopeSelectedContexts {
		available := make(map[string]bool, len(m.repositoryCatalog))
		for _, repository := range m.repositoryCatalog {
			if repository.Kind == model.RepositoryIdentityHubContext {
				available[repository.ID] = true
			}
		}
		for _, contextID := range scope.Contexts {
			if !available[contextID] {
				return fmt.Errorf("Hub context is not registered: %s", contextID)
			}
		}
	}
	m.defaultRepositorySet = true
	m.defaultRepositoryID = ""
	switch scope.Mode {
	case model.HubScopeAllItems:
		m.hubScope = model.NewAllItemsHubScope()
	case model.HubScopeContextless:
		m.hubScope = model.NewContextlessHubScope()
	default:
		m.hubScope = scope.Clone()
	}
	m.activeRepos = nil
	if scope.Mode == model.HubScopeSelectedContexts {
		m.activeRepos = make(map[string]bool, len(scope.Contexts))
		for _, contextID := range scope.Contexts {
			m.activeRepos[contextID] = true
		}
	}
	m.refreshRepositoryCandidates()
	return nil
}

// HubScope returns a detached explicit Hub selector.
func (m Model) HubScope() model.HubScope {
	return m.hubScope.Clone()
}

// RepositoryScope returns a defensive copy of the selected exact catalog IDs.
// Nil means all repositories.
func (m Model) RepositoryScope() map[string]bool {
	if m.usesHubScope() && m.hubScope.Mode != model.HubScopeSelectedContexts {
		return nil
	}
	if m.activeRepos == nil {
		return nil
	}
	selected := make(map[string]bool, len(m.activeRepos))
	for id, enabled := range m.activeRepos {
		if enabled {
			selected[id] = true
		}
	}
	return selected
}

func (m *Model) refreshRepositoryCandidates() {
	m.syncRepositoryCandidates()
	m.recomputeRepositoryCounts()
	m.labelHealthCached = false
	m.attentionCached = false
	m.labelDrilldownCache = make(map[string][]model.Issue)
	if m.analysis == nil || m.analyzer == nil {
		return
	}

	if m.activeRecipe != nil {
		m.applyRecipe(m.activeRecipe)
	} else {
		m.applyFilter()
	}

	m.alerts, m.alertsCritical, m.alertsWarning, m.alertsInfo = computeAlerts(m.repositoryIssues, m.analysis, m.analyzer)
	m.dismissedAlerts = make(map[string]bool)
	m.showAlertsPanel = false
	m.refreshRepositoryDerivedViews()
}

func (m *Model) syncRepositoryCandidates() {
	m.repositoryIssues = m.repositoryCandidates()
	m.repositoryIssueIDs = issueIDSet(m.repositoryIssues)
}

func (m *Model) recomputeRepositoryCounts() {
	m.countOpen, m.countReady, m.countBlocked, m.countClosed = 0, 0, 0, 0
	for i := range m.repositoryIssues {
		issue := &m.repositoryIssues[i]
		if isClosedLikeStatus(issue.Status) {
			m.countClosed++
			continue
		}
		m.countOpen++
		if issue.Status == model.StatusBlocked {
			m.countBlocked++
			continue
		}
		blocked := false
		for _, dep := range issue.Dependencies {
			if dep == nil || !dep.Type.IsBlocking() {
				continue
			}
			if blocker, exists := m.issueMap[dep.DependsOnID]; exists && !isClosedLikeStatus(blocker.Status) {
				blocked = true
				break
			}
		}
		if !blocked {
			m.countReady++
		}
	}
}

func projectInsightItems(items []analysis.InsightItem, ids map[string]bool) []analysis.InsightItem {
	projected := make([]analysis.InsightItem, 0, len(items))
	for _, item := range items {
		if ids[item.ID] {
			projected = append(projected, item)
		}
	}
	return projected
}

func projectStrings(items []string, ids map[string]bool) []string {
	projected := make([]string, 0, len(items))
	for _, id := range items {
		if ids[id] {
			projected = append(projected, id)
		}
	}
	return projected
}

func projectInsights(ins analysis.Insights, ids map[string]bool) analysis.Insights {
	ins.Bottlenecks = projectInsightItems(ins.Bottlenecks, ids)
	ins.Keystones = projectInsightItems(ins.Keystones, ids)
	ins.Influencers = projectInsightItems(ins.Influencers, ids)
	ins.Hubs = projectInsightItems(ins.Hubs, ids)
	ins.Authorities = projectInsightItems(ins.Authorities, ids)
	ins.Cores = projectInsightItems(ins.Cores, ids)
	ins.Articulation = projectStrings(ins.Articulation, ids)
	ins.Slack = projectInsightItems(ins.Slack, ids)
	ins.Orphans = projectStrings(ins.Orphans, ids)
	cycles := make([][]string, 0, len(ins.Cycles))
	for _, cycle := range ins.Cycles {
		for _, id := range cycle {
			if ids[id] {
				cycles = append(cycles, cycle)
				break
			}
		}
	}
	ins.Cycles = cycles
	return ins
}

func projectLabelPageRank(result analysis.LabelPageRankResult, ids map[string]bool) analysis.LabelPageRankResult {
	result.Scores = projectFloatScores(result.Scores, ids)
	result.Normalized = projectFloatScores(result.Normalized, ids)
	result.CoreOnly = projectFloatScores(result.CoreOnly, ids)
	top := make([]analysis.RankedIssue, 0, len(result.TopIssues))
	for _, issue := range result.TopIssues {
		if ids[issue.ID] {
			top = append(top, issue)
		}
	}
	result.TopIssues = top
	result.IssueCount = len(result.Scores)
	result.CoreCount = len(result.CoreOnly)
	result.MaxScore, result.MinScore = 0, 0
	first := true
	for _, score := range result.Scores {
		if first || score > result.MaxScore {
			result.MaxScore = score
		}
		if first || score < result.MinScore {
			result.MinScore = score
		}
		first = false
	}
	return result
}

func projectFloatScores(scores map[string]float64, ids map[string]bool) map[string]float64 {
	projected := make(map[string]float64)
	for id, score := range scores {
		if ids[id] {
			projected[id] = score
		}
	}
	return projected
}

func projectLabelCriticalPath(result analysis.LabelCriticalPathResult, ids map[string]bool) analysis.LabelCriticalPathResult {
	path := make([]string, 0, len(result.Path))
	titles := make([]string, 0, len(result.PathTitles))
	for i, id := range result.Path {
		if !ids[id] {
			continue
		}
		path = append(path, id)
		if i < len(result.PathTitles) {
			titles = append(titles, result.PathTitles[i])
		} else {
			titles = append(titles, "")
		}
	}
	heights := make(map[string]int)
	for id, height := range result.AllHeights {
		if ids[id] {
			heights[id] = height
		}
	}
	result.Path = path
	result.PathTitles = titles
	result.PathLength = len(path)
	result.AllHeights = heights
	result.IssueCount = len(heights)
	result.MaxHeight = 0
	for _, height := range heights {
		if height > result.MaxHeight {
			result.MaxHeight = height
		}
	}
	return result
}

func scopedIssueParticipatesInLabelCycle(subgraph analysis.LabelSubgraph, ids map[string]bool) bool {
	for start := range ids {
		if _, exists := subgraph.IssueMap[start]; !exists {
			continue
		}
		visited := make(map[string]bool)
		var reachesStart func(string) bool
		reachesStart = func(current string) bool {
			for _, next := range subgraph.Adjacency[current] {
				if next == start {
					return true
				}
				if visited[next] {
					continue
				}
				visited[next] = true
				if reachesStart(next) {
					return true
				}
			}
			return false
		}
		visited[start] = true
		if reachesStart(start) {
			return true
		}
	}
	return false
}

func projectExecutionPlan(plan analysis.ExecutionPlan, ids map[string]bool, issues []model.Issue) analysis.ExecutionPlan {
	projected := analysis.ExecutionPlan{}
	for _, track := range plan.Tracks {
		items := make([]analysis.PlanItem, 0, len(track.Items))
		for _, item := range track.Items {
			if ids[item.ID] {
				items = append(items, item)
				if len(item.UnblocksIDs) > projected.Summary.UnblocksCount {
					projected.Summary.HighestImpact = item.ID
					projected.Summary.UnblocksCount = len(item.UnblocksIDs)
					projected.Summary.ImpactReason = fmt.Sprintf("Unblocks %d issue(s)", len(item.UnblocksIDs))
				}
			}
		}
		if len(items) > 0 {
			track.Items = items
			projected.Tracks = append(projected.Tracks, track)
			projected.TotalActionable += len(items)
		}
	}
	for _, issue := range issues {
		if !isClosedLikeStatus(issue.Status) {
			projected.TotalBlocked++
		}
	}
	projected.TotalBlocked -= projected.TotalActionable
	return projected
}

func (m *Model) scopedTriage() analysis.TriageResult {
	triage := analysis.ComputeTriageFromAnalyzer(m.analyzer, m.analysis, m.issues, analysis.TriageOptions{
		TopN:      len(m.issues),
		QuickWinN: len(m.issues),
		BlockerN:  len(m.issues),
	}, time.Now())
	triage.Recommendations = filterRecommendations(triage.Recommendations, m.repositoryIssueIDs)
	parentsWithOpenChildren := make(map[string]bool)
	for _, issue := range m.issues {
		if isClosedLikeStatus(issue.Status) {
			continue
		}
		for _, dependency := range issue.Dependencies {
			if dependency != nil && dependency.Type == model.DepParentChild {
				parentsWithOpenChildren[dependency.DependsOnID] = true
			}
		}
	}
	triage.QuickRef.TopPicks = scopedTopPicks(triage.Recommendations, parentsWithOpenChildren, 3)
	if len(triage.Recommendations) > 10 {
		triage.Recommendations = triage.Recommendations[:10]
	}
	actionable := make(map[string]bool)
	for _, issue := range m.analyzer.GetActionableIssues() {
		actionable[issue.ID] = true
	}
	triage.QuickRef.OpenCount = 0
	triage.QuickRef.ActionableCount = 0
	triage.QuickRef.BlockedCount = 0
	triage.QuickRef.InProgressCount = 0
	triage.QuickRef.NotClosedCount = 0
	for _, issue := range m.repositoryIssues {
		switch issue.Status {
		case model.StatusOpen:
			triage.QuickRef.OpenCount++
		case model.StatusBlocked:
			triage.QuickRef.BlockedCount++
		case model.StatusInProgress:
			triage.QuickRef.InProgressCount++
		}
		if !isClosedLikeStatus(issue.Status) {
			triage.QuickRef.NotClosedCount++
			if actionable[issue.ID] {
				triage.QuickRef.ActionableCount++
			}
		}
	}
	triage.QuickRef.NotActionableCount = triage.QuickRef.NotClosedCount - triage.QuickRef.ActionableCount
	return triage
}

func scopedTopPicks(recommendations []analysis.Recommendation, parentsWithOpenChildren map[string]bool, limit int) []analysis.TopPick {
	result := make([]analysis.TopPick, 0, limit)
	for _, recommendation := range recommendations {
		if recommendation.Status != string(model.StatusOpen) || recommendation.Type == string(model.TypeEpic) ||
			recommendation.Assignee != "" || len(recommendation.BlockedBy) > 0 || parentsWithOpenChildren[recommendation.ID] {
			continue
		}
		result = append(result, analysis.TopPick{
			ID: recommendation.ID, Title: recommendation.Title, Score: recommendation.Score,
			Reasons: recommendation.Reasons, Unblocks: len(recommendation.UnblocksIDs),
		})
		if len(result) == limit {
			break
		}
	}
	return result
}

func filterRecommendations(items []analysis.Recommendation, ids map[string]bool) []analysis.Recommendation {
	result := make([]analysis.Recommendation, 0, len(items))
	for _, item := range items {
		if ids[item.ID] {
			result = append(result, item)
		}
	}
	return result
}

func (m *Model) refreshRepositoryDerivedViews() {
	if m.analyzer == nil || m.analysis == nil {
		return
	}
	if m.isActionableView {
		plan := projectExecutionPlan(m.analyzer.GetExecutionPlan(), m.repositoryIssueIDs, m.repositoryIssues)
		m.actionableView = NewActionableModel(plan, m.theme)
		m.actionableView.SetSize(m.width, m.height-2)
	}
	if m.focused == focusTree {
		m.rebuildRepositoryTree()
		m.tree.SetSize(m.width, m.height-2)
	}
	if m.focused == focusInsights && !m.showAttentionView {
		m.rebuildInsightsPanel()
	}
	if m.focused == focusLabelDashboard {
		cfg := analysis.DefaultLabelHealthConfig()
		m.labelHealthCache = analysis.ComputeAllLabelHealth(m.repositoryIssues, cfg, time.Now().UTC(), m.analysis)
		m.labelHealthCache = projectHubLabelHealth(m.labelHealthCache, m.hubRepositoryPresentation())
		m.labelHealthCached = true
		m.labelDashboard.SetData(m.labelHealthCache.Labels)
	}
	if m.showAttentionView {
		m.refreshAttentionView()
	}
	if m.focused == focusFlowMatrix {
		m.refreshFlowMatrix()
	}
	if m.selectedSprint != nil {
		m.sprintViewText = m.renderSprintDashboard()
	}
	if m.historyReport != nil {
		m.historyView.SetReport(m.repositoryHistoryReport(m.historyReport))
	}
}

func (m *Model) rebuildRepositoryTree() {
	selectedID := ""
	if selected := m.tree.SelectedIssue(); selected != nil {
		selectedID = selected.ID
	}
	if m.usesHubScope() {
		m.tree.BuildProjected(m.repositoryIssues, m.issueMap)
	} else {
		m.tree.Build(m.repositoryIssues)
	}
	if selectedID == "" {
		return
	}
	for i, node := range m.tree.flatList {
		if node != nil && node.Issue != nil && node.Issue.ID == selectedID {
			m.tree.cursor = i
			m.tree.ensureCursorVisible()
			return
		}
	}
}

func (m Model) repositoryHistoryReport(report *correlation.HistoryReport) *correlation.HistoryReport {
	if m.usesHubScope() && m.hubScope.Mode == model.HubScopeAllItems {
		return report
	}
	repositories := m.activeRepos
	if m.usesHubScope() && m.hubScope.Mode == model.HubScopeContextless {
		repositories = map[string]bool{}
	}
	if !m.usesHubScope() && repositories == nil {
		return report
	}
	return projectHistoryReport(report, m.repositoryIssueIDs, repositories)
}

func (m *Model) refreshAttentionView() {
	cfg := analysis.DefaultLabelHealthConfig()
	m.attentionCache = analysis.ComputeLabelAttentionScores(m.repositoryIssues, cfg, time.Now().UTC())
	m.attentionCached = true
	attText := RenderAttentionView(m.attentionCache, max(40, m.width-4))
	m.rebuildInsightsPanel()
	m.insightsPanel.labelAttention = m.attentionCache.Labels
	m.insightsPanel.extraText = attText
}

func (m *Model) refreshFlowMatrix() {
	cfg := analysis.DefaultLabelHealthConfig()
	flow := analysis.ComputeCrossLabelFlow(m.repositoryIssues, cfg)
	m.flowMatrix = NewFlowMatrixModel(m.theme)
	m.flowMatrix.SetData(&flow, m.repositoryIssues)
	panelHeight := m.height - 2
	if panelHeight < 3 {
		panelHeight = 3
	}
	m.flowMatrix.SetSize(m.width, panelHeight)
}

func projectHistoryReport(report *correlation.HistoryReport, ids map[string]bool, repositories map[string]bool) *correlation.HistoryReport {
	if report == nil {
		return nil
	}
	projected := *report
	projected.Histories = make(map[string]correlation.BeadHistory)
	projected.CommitIndex = make(correlation.CommitIndex)
	projected.Warnings = nil
	for _, warning := range report.Warnings {
		if warning.Context == "" || repositories[warning.Context] {
			projected.Warnings = append(projected.Warnings, warning)
		}
	}
	methods := make(map[string]int)
	authors := make(map[string]bool)
	commits := make(map[string]bool)
	var cycleTimes []time.Duration
	for id, history := range report.Histories {
		if !ids[id] {
			continue
		}
		projected.Histories[id] = history
		for _, commit := range history.Commits {
			commits[commit.Repository+"\x00"+commit.SHA] = true
			authors[commit.Author] = true
			methods[commit.Method.String()]++
		}
		for _, event := range history.Events {
			authors[event.Author] = true
		}
		if history.CycleTime != nil && history.CycleTime.ClaimToClose != nil {
			cycleTimes = append(cycleTimes, *history.CycleTime.ClaimToClose)
		}
	}
	for sha, beadIDs := range report.CommitIndex {
		for _, id := range beadIDs {
			if ids[id] {
				projected.CommitIndex[sha] = append(projected.CommitIndex[sha], id)
			}
		}
	}
	projected.Stats.TotalBeads = len(projected.Histories)
	projected.Stats.BeadsWithCommits = 0
	for _, history := range projected.Histories {
		if len(history.Commits) > 0 {
			projected.Stats.BeadsWithCommits++
		}
	}
	projected.Stats.TotalCommits = len(commits)
	projected.Stats.UniqueAuthors = len(authors)
	projected.Stats.MethodDistribution = methods
	projected.Stats.AvgCommitsPerBead = 0
	if projected.Stats.BeadsWithCommits > 0 {
		projected.Stats.AvgCommitsPerBead = float64(len(commits)) / float64(projected.Stats.BeadsWithCommits)
	}
	projected.Stats.AvgCycleTimeDays = nil
	if len(cycleTimes) > 0 {
		var total time.Duration
		for _, cycleTime := range cycleTimes {
			total += cycleTime
		}
		average := total.Hours() / 24 / float64(len(cycleTimes))
		projected.Stats.AvgCycleTimeDays = &average
	}
	return &projected
}
