package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// repositoryScopeController owns the repository universe and the user's
// selection. Model deliberately embeds it so existing UI code keeps its
// field-level access while catalog reconciliation has one owner.
type repositoryScopeController struct {
	repositoryCatalog         repositorypkg.Catalog
	hubScope                  hub.HubScope
	repositoryCatalogIssues   []model.Issue
	contextlessBeadCountValue int
	contextlessCountReady     bool
	repositoryIssues          []model.Issue
	repositoryIssueIDs        map[string]bool
	repositoryCatalogReady    bool
	currentRepositoryID       string
	defaultRepositoryID       string
	defaultRepositorySet      bool
	catalogGeneration         uint64
	activeRepos               map[string]bool
}

func newRepositoryScopeController() repositoryScopeController {
	return repositoryScopeController{hubScope: hub.NewAllItemsHubScope()}
}

func (s *repositoryScopeController) reconcileHubScopeCatalog(usesHub bool) {
	if !usesHub || s.hubScope.Mode != hub.HubScopeSelectedContexts {
		return
	}
	selected := make(map[string]bool, len(s.hubScope.Contexts))
	for _, contextID := range s.hubScope.Contexts {
		selected[contextID] = true
	}
	reconciled := repositorypkg.ReconcileSelection(selected, s.repositoryCatalog)
	if reconciled == nil {
		if s.hubScope.IncludeContextless {
			s.hubScope = hub.NewContextlessHubScope()
		} else {
			s.hubScope = hub.NewAllItemsHubScope()
		}
		s.activeRepos = nil
		return
	}
	if s.hubScope.IncludeContextless && len(reconciled) == len(s.repositoryCatalog) {
		s.hubScope = hub.NewAllItemsHubScope()
		s.activeRepos = nil
		return
	}
	var scope hub.HubScope
	var err error
	if s.hubScope.IncludeContextless {
		scope, err = hub.NewSelectedContextsAndContextlessHubScope(sortedRepoKeys(reconciled))
	} else {
		scope, err = hub.NewSelectedContextsHubScope(sortedRepoKeys(reconciled))
	}
	if err != nil {
		s.hubScope = hub.NewAllItemsHubScope()
		s.activeRepos = nil
		return
	}
	s.hubScope = scope
	s.activeRepos = reconciled
}

func (s *repositoryScopeController) setRepositoryScope(selected map[string]bool, usesHub bool) error {
	s.defaultRepositorySet = true
	s.defaultRepositoryID = ""
	reconciled := repositorypkg.ReconcileSelection(selected, s.repositoryCatalog)
	if usesHub {
		if len(selected) == 0 || len(reconciled) == 0 {
			s.hubScope = hub.NewAllItemsHubScope()
			s.activeRepos = nil
			return nil
		}
		return s.setHubRepositoryScope(reconciled, false)
	}
	if len(selected) == 0 || len(reconciled) == len(s.repositoryCatalog) {
		s.activeRepos = nil
	} else {
		s.activeRepos = reconciled
	}
	return nil
}

func (s *repositoryScopeController) setHubRepositoryScope(selected map[string]bool, includeContextless bool) error {
	s.defaultRepositorySet = true
	s.defaultRepositoryID = ""
	if len(selected) == 0 {
		if includeContextless {
			s.hubScope = hub.NewContextlessHubScope()
		} else {
			s.hubScope = hub.NewAllItemsHubScope()
		}
		s.activeRepos = nil
		return nil
	}
	if includeContextless && len(selected) == len(s.repositoryCatalog) {
		s.hubScope = hub.NewAllItemsHubScope()
		s.activeRepos = nil
		return nil
	}
	contexts := sortedRepoKeys(selected)
	var scope hub.HubScope
	var err error
	if includeContextless {
		scope, err = hub.NewSelectedContextsAndContextlessHubScope(contexts)
	} else {
		scope, err = hub.NewSelectedContextsHubScope(contexts)
	}
	if err != nil {
		return err
	}
	s.hubScope = scope
	s.activeRepos = repositorypkg.ReconcileSelection(selected, s.repositoryCatalog)
	return nil
}

func (s *repositoryScopeController) setHubScope(scope hub.HubScope, usesHub bool) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.Mode == hub.HubScopeSelectedContexts {
		available := make(map[string]bool, len(s.repositoryCatalog))
		for _, repository := range s.repositoryCatalog {
			if repository.Kind == repositorypkg.IdentityExact {
				available[repository.ID] = true
			}
		}
		for _, contextID := range scope.Contexts {
			if !available[contextID] {
				return fmt.Errorf("Hub context is not registered: %s", contextID)
			}
		}
	}
	s.defaultRepositorySet = true
	s.defaultRepositoryID = ""
	switch scope.Mode {
	case hub.HubScopeAllItems:
		s.hubScope = hub.NewAllItemsHubScope()
	case hub.HubScopeContextless:
		s.hubScope = hub.NewContextlessHubScope()
	default:
		s.hubScope = scope.Clone()
	}
	s.activeRepos = nil
	if usesHub && scope.Mode == hub.HubScopeSelectedContexts {
		s.activeRepos = make(map[string]bool, len(scope.Contexts))
		for _, contextID := range scope.Contexts {
			s.activeRepos[contextID] = true
		}
	}
	return nil
}

func (s *repositoryScopeController) applyDefault() bool {
	if s.defaultRepositorySet || s.defaultRepositoryID == "" || !s.repositoryCatalogReady {
		return false
	}
	s.defaultRepositorySet = true
	for _, repository := range s.repositoryCatalog {
		if repository.Kind != repositorypkg.IdentityExact || repository.ID != s.defaultRepositoryID {
			continue
		}
		scope, err := hub.NewSelectedContextsHubScope([]string{repository.ID})
		if err != nil {
			return false
		}
		s.activeRepos = map[string]bool{repository.ID: true}
		s.hubScope = scope
		return true
	}
	return false
}

func (s repositoryScopeController) usesHubScope(workspaceMode, hubRepositoryMode bool, catalogPath string) bool {
	if workspaceMode {
		return false
	}
	if hubRepositoryMode || strings.TrimSpace(catalogPath) != "" {
		return true
	}
	for _, repository := range s.repositoryCatalog {
		if repository.Kind == repositorypkg.IdentityExact {
			return true
		}
	}
	return false
}

func (s repositoryScopeController) issueMatchesRepositoryScope(issue model.Issue, workspaceMode, usesHub bool) bool {
	if usesHub {
		return s.hubScope.MatchesLabels(issue.Labels)
	}
	if s.activeRepos == nil {
		return true
	}

	workspaceKey := ""
	for _, repository := range s.repositoryCatalog {
		if !s.activeRepos[repository.ID] {
			continue
		}
		switch repository.Kind {
		case repositorypkg.IdentityExact:
			if repository.ID != strings.ToLower(repository.ID) || !strings.HasPrefix(repository.ID, "ctx:") {
				continue
			}
			for _, label := range issue.Labels {
				if label == repository.ID {
					return true
				}
			}
		case repositorypkg.IdentityPrefix:
			if workspaceKey == "" {
				workspaceKey = issueRepoKey(issue)
			}
			if workspaceKey == repository.ID {
				return true
			}
		}
	}

	// Legacy workspace filtering keeps issues with no source/prefix visible.
	return workspaceMode && workspaceKey == ""
}

func (s repositoryScopeController) repositoryCandidates(issues []model.Issue, workspaceMode, usesHub bool) []model.Issue {
	if (!usesHub && s.activeRepos == nil) || (usesHub && s.hubScope.Mode == hub.HubScopeAllItems) {
		return issues
	}
	candidates := make([]model.Issue, 0, len(issues))
	for _, issue := range issues {
		if s.issueMatchesRepositoryScope(issue, workspaceMode, usesHub) {
			candidates = append(candidates, issue)
		}
	}
	return candidates
}

func (s *repositoryScopeController) acceptCatalogGeneration(generation uint64) bool {
	if generation < s.catalogGeneration {
		return false
	}
	s.catalogGeneration = generation
	return true
}

func (s *repositoryScopeController) setCatalog(catalog repositorypkg.Catalog) {
	s.repositoryCatalog = append(repositorypkg.Catalog(nil), catalog...)
	s.repositoryCatalogReady = true
}

func (s *repositoryScopeController) setCatalogIssues(issues []model.Issue) {
	s.repositoryCatalogIssues = cloneIssuesForAsync(issues)
	s.contextlessCountReady = false
}

func (s *repositoryScopeController) setProjectedIssues(issues []model.Issue) {
	s.repositoryIssues = issues
	s.repositoryIssueIDs = issueIDSet(issues)
}

type issueRepositoryPresentation struct {
	ID     string
	Name   string
	Extra  int
	Names  []string
	Labels []string
}

func (m Model) labelPredicate() analysis.LabelPredicate {
	if m.hubRepositoryPresentation() {
		return hub.AdmitLabel
	}
	return nil
}

type hubRelationshipEvidence struct {
	Label    string
	Endpoint *model.Issue
}

const contextlessRepositoryID = "no-context"

func isContextSortMode(mode SortMode) bool {
	return mode == SortContextCreated || mode == SortContextPriority
}

func (m Model) effectiveHubContextIDs() map[string]struct{} {
	recognized := make(map[string]struct{}, len(m.repositoryCatalog))
	for _, repository := range m.repositoryCatalog {
		if repository.Kind == repositorypkg.IdentityExact {
			recognized[repository.ID] = struct{}{}
		}
	}

	effective := make(map[string]struct{}, len(recognized))
	if m.hubScope.Mode == hub.HubScopeSelectedContexts {
		for _, contextID := range m.hubScope.Contexts {
			effective[contextID] = struct{}{}
		}
	}
	for _, issue := range m.repositoryCandidates() {
		for _, label := range issue.Labels {
			if _, ok := recognized[label]; ok {
				effective[label] = struct{}{}
			}
		}
	}
	return effective
}

func (m Model) contextSortModesAvailable() bool {
	if m.workspaceMode || !m.usesHubScope() {
		return false
	}
	switch m.hubScope.Mode {
	case hub.HubScopeContextless:
		return false
	}
	return len(m.effectiveHubContextIDs()) >= 2
}

func (m *Model) normalizeContextSortMode() bool {
	if !isContextSortMode(m.sortMode) || m.contextSortModesAvailable() {
		return false
	}
	m.sortMode = SortDefault
	return true
}

func isHubContextLabel(label string) bool {
	return hub.IsContextLabel(label)
}

func contextlessIssueCount(issues []model.Issue) int {
	scope := hub.NewContextlessHubScope()
	count := 0
	for _, issue := range issues {
		if scope.MatchesLabels(issue.Labels) {
			count++
		}
	}
	return count
}

func repositoryPresentationForIssue(issue model.Issue, catalog repositorypkg.Catalog, hubMode bool, currentRepositoryID string, preferredRepositories map[string]bool) issueRepositoryPresentation {
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

	matches := make([]repositorypkg.CatalogEntry, 0, len(contexts))
	for _, repository := range catalog {
		if repository.Kind == repositorypkg.IdentityExact && contexts[repository.ID] {
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
	// Prefer the current repository, then the selected scope, then the
	// deterministic display-name/ID ordering below.
	candidates := matches
	currentFound := false
	for _, repository := range matches {
		if repository.ID == currentRepositoryID {
			candidates = []repositorypkg.CatalogEntry{repository}
			currentFound = true
			break
		}
	}
	if !currentFound && len(preferredRepositories) > 0 {
		preferred := make([]repositorypkg.CatalogEntry, 0, len(matches))
		for _, repository := range matches {
			if preferredRepositories[repository.ID] {
				preferred = append(preferred, repository)
			}
		}
		if len(preferred) > 0 {
			candidates = preferred
		}
	}

	primary := candidates[0]
	for _, repository := range candidates[1:] {
		if repository.Name < primary.Name || repository.Name == primary.Name && repository.ID < primary.ID {
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

func hubContextNames(issue model.Issue, catalog repositorypkg.Catalog) []string {
	namesByID := make(map[string]string, len(catalog))
	for _, repository := range catalog {
		if repository.Kind == repositorypkg.IdentityExact {
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
		return []string{contextlessRepositoryID}
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
	return !m.workspaceMode && strings.TrimSpace(m.catalogPath()) != ""
}

func (m *Model) decorateIssueItem(item *IssueItem) {
	if item == nil {
		return
	}
	presentation := repositoryPresentationForIssue(item.Issue, m.repositoryCatalog, m.hubRepositoryPresentation(), m.currentRepositoryID, m.activeRepos)
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
		if isContextSortMode(m.sortMode) {
			m.sortListItems(items)
		} else {
			m.setListItemsPreservingFilter(items)
		}
		m.updateListDelegate()
		m.updateViewportContent()
	}
	m.board.SetRepositoryPresentation(m.repositoryCatalog, hubMode, m.currentRepositoryID, m.activeRepos)
	m.insightsPanel.SetRepositoryPresentation(m.repositoryCatalog, hubMode)
}

func (m *Model) issueMatchesRepositoryScope(issue model.Issue) bool {
	return m.repositoryScopeController.issueMatchesRepositoryScope(issue, m.workspaceMode, m.usesHubScope())
}

func (m *Model) repositoryCandidates() []model.Issue {
	return m.repositoryScopeController.repositoryCandidates(m.issues, m.workspaceMode, m.usesHubScope())
}

func (m Model) repositoryScopeIsAll() bool {
	if m.usesHubScope() {
		return m.hubScope.Mode == hub.HubScopeAllItems
	}
	return m.activeRepos == nil
}

func (m Model) usesHubScope() bool {
	return m.repositoryScopeController.usesHubScope(m.workspaceMode, m.hubRepositoryMode, m.catalogPath())
}

func issueIDSet(issues []model.Issue) map[string]bool {
	ids := make(map[string]bool, len(issues))
	for _, issue := range issues {
		ids[issue.ID] = true
	}
	return ids
}

func repositoryCatalogIDs(catalog repositorypkg.Catalog) []string {
	ids := make([]string, 0, len(catalog))
	for _, repository := range catalog {
		ids = append(ids, repository.ID)
	}
	return ids
}

func (m *Model) reconcileHubScopeCatalog() {
	m.repositoryScopeController.reconcileHubScopeCatalog(m.usesHubScope())
}

// SetDefaultRepositoryScope applies an exact Hub context once the initial
// repository catalog is available. A single-entry catalog remains an explicit
// selection so repositories registered later do not silently join the scope.
func (m *Model) SetDefaultRepositoryScope(repositoryID string) bool {
	if repositoryID == "" || m.workspaceMode || !m.hubRepositoryMode {
		return false
	}
	if m.currentRepositoryID != repositoryID {
		m.currentRepositoryID = repositoryID
		m.refreshRepositoryPresentation()
	}
	if m.defaultRepositorySet {
		return false
	}
	m.defaultRepositoryID = repositoryID
	return m.applyDefaultRepositoryScope()
}

func (m *Model) applyDefaultRepositoryScope() bool {
	if !m.hubRepositoryMode || m.workspaceMode {
		return false
	}
	if !m.repositoryScopeController.applyDefault() {
		return false
	}
	m.refreshRepositoryCandidates()
	m.refreshRepositoryPresentation()
	return true
}

// SetRepositoryScope applies exact catalog IDs. Nil and an empty selection mean
// the complete universe; selecting every Hub repository excludes contextless items.
func (m *Model) SetRepositoryScope(selected map[string]bool) {
	if err := m.repositoryScopeController.setRepositoryScope(selected, m.usesHubScope()); err != nil {
		return
	}
	m.refreshRepositoryCandidates()
	m.refreshRepositoryPresentation()
}

func (m *Model) setHubRepositoryScope(selected map[string]bool, includeContextless bool) {
	if err := m.repositoryScopeController.setHubRepositoryScope(selected, includeContextless); err != nil {
		return
	}
	m.refreshRepositoryCandidates()
	m.refreshRepositoryPresentation()
}

// SetHubScope applies an explicit Hub candidate selector. Selected context IDs
// must be present in the current Hub repository catalog.
func (m *Model) SetHubScope(scope hub.HubScope) error {
	if err := m.repositoryScopeController.setHubScope(scope, m.usesHubScope()); err != nil {
		return err
	}
	m.refreshRepositoryCandidates()
	m.refreshRepositoryPresentation()
	return nil
}

// HubScope returns a detached explicit Hub selector.
func (m Model) HubScope() hub.HubScope {
	return m.hubScope.Clone()
}

// RepositoryScope returns a defensive copy of the selected exact catalog IDs.
// Nil means all repositories.
func (m Model) RepositoryScope() map[string]bool {
	if m.usesHubScope() && m.hubScope.Mode != hub.HubScopeSelectedContexts {
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
	m.repositoryScopeController.setProjectedIssues(m.repositoryCandidates())
	m.normalizeContextSortMode()
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
		if !issueHasUnresolvedBlockingDependency(*issue, m.issueMap) {
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
		allowed := true
		for _, id := range cycle {
			if !ids[id] {
				allowed = false
				break
			}
		}
		if allowed && len(cycle) > 0 {
			cycles = append(cycles, cycle)
		}
	}
	ins.Cycles = cycles
	return ins
}

func projectTopPicks(items []analysis.TopPick, ids map[string]bool) []analysis.TopPick {
	projected := make([]analysis.TopPick, 0, len(items))
	for _, item := range items {
		if ids[item.ID] {
			projected = append(projected, item)
		}
	}
	return projected
}

func projectRecommendations(items []analysis.Recommendation, ids map[string]bool) []analysis.Recommendation {
	projected := make([]analysis.Recommendation, 0, len(items))
	for _, item := range items {
		if !ids[item.ID] {
			continue
		}
		item.UnblocksIDs = projectStrings(item.UnblocksIDs, ids)
		item.BlockedBy = projectStrings(item.BlockedBy, ids)
		projected = append(projected, item)
	}
	return projected
}

func projectQuickWins(items []analysis.QuickWin, ids map[string]bool) []analysis.QuickWin {
	projected := make([]analysis.QuickWin, 0, len(items))
	for _, item := range items {
		if ids[item.ID] {
			item.UnblocksIDs = projectStrings(item.UnblocksIDs, ids)
			projected = append(projected, item)
		}
	}
	return projected
}

func projectBlockers(items []analysis.BlockerItem, ids map[string]bool) []analysis.BlockerItem {
	projected := make([]analysis.BlockerItem, 0, len(items))
	for _, item := range items {
		if ids[item.ID] {
			item.UnblocksIDs = projectStrings(item.UnblocksIDs, ids)
			item.BlockedBy = projectStrings(item.BlockedBy, ids)
			projected = append(projected, item)
		}
	}
	return projected
}

func projectTrackRecommendationGroups(groups []analysis.TrackRecommendationGroup, ids map[string]bool) []analysis.TrackRecommendationGroup {
	projected := make([]analysis.TrackRecommendationGroup, 0, len(groups))
	for _, group := range groups {
		group.Recommendations = projectRecommendations(group.Recommendations, ids)
		if group.TopPick != nil && ids[group.TopPick.ID] {
			topPick := *group.TopPick
			group.TopPick = &topPick
		} else {
			group.TopPick = nil
		}
		projected = append(projected, group)
	}
	return projected
}

func projectLabelRecommendationGroups(groups []analysis.LabelRecommendationGroup, ids map[string]bool) []analysis.LabelRecommendationGroup {
	projected := make([]analysis.LabelRecommendationGroup, 0, len(groups))
	for _, group := range groups {
		group.Recommendations = projectRecommendations(group.Recommendations, ids)
		if group.TopPick != nil && ids[group.TopPick.ID] {
			topPick := *group.TopPick
			group.TopPick = &topPick
		} else {
			group.TopPick = nil
		}
		projected = append(projected, group)
	}
	return projected
}

func projectTriageResult(triage analysis.TriageResult, ids map[string]bool) analysis.TriageResult {
	triage.Recommendations = projectRecommendations(triage.Recommendations, ids)
	triage.QuickWins = projectQuickWins(triage.QuickWins, ids)
	triage.BlockersToClear = projectBlockers(triage.BlockersToClear, ids)
	triage.QuickRef.TopPicks = projectTopPicks(triage.QuickRef.TopPicks, ids)
	triage.RecommendationsByTrack = projectTrackRecommendationGroups(triage.RecommendationsByTrack, ids)
	triage.RecommendationsByLabel = projectLabelRecommendationGroups(triage.RecommendationsByLabel, ids)
	return triage
}

func (m *Model) insightsIssueIDs() map[string]bool {
	ids := make(map[string]bool)
	issues := m.repositoryIssues
	if issues == nil {
		issues = m.issues
	}
	for _, issue := range issues {
		if m.repositoryIssueIDs != nil && !m.repositoryIssueIDs[issue.ID] {
			continue
		}
		if isClosedLikeStatus(issue.Status) {
			continue
		}
		if m.insightsStatusFilter == "ready" && !m.matchesFilter(issue, "ready") {
			continue
		}
		ids[issue.ID] = true
	}
	return ids
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
	insightsIDs := m.insightsIssueIDs()
	parentsWithOpenChildren := m.analyzer.ParentsWithOpenChildren()
	globalTopPicks := scopedTopPicks(triage.Recommendations, parentsWithOpenChildren, insightsIDs, 3)
	triage = projectTriageResult(triage, insightsIDs)
	triage.QuickRef.TopPicks = projectTopPicks(globalTopPicks, insightsIDs)
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

func scopedTopPicks(recommendations []analysis.Recommendation, parentsWithOpenChildren, candidateIDs map[string]bool, limit int) []analysis.TopPick {
	result := make([]analysis.TopPick, 0, limit)
	for _, recommendation := range recommendations {
		if !candidateIDs[recommendation.ID] {
			continue
		}
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

func (m *Model) refreshRepositoryDerivedViews() {
	if m.analyzer == nil || m.analysis == nil {
		return
	}
	m.revalidateInsightsDetail(m.insightsIssueIDs())
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
	} else if !m.showAttentionView {
		m.insightsPanel.SetActiveIssueIDs(m.insightsIssueIDs())
	}
	if m.focused == focusLabelDashboard {
		cfg := analysis.DefaultLabelHealthConfig()
		m.labelHealthCache = analysis.ComputeAllLabelHealth(m.repositoryIssues, cfg, time.Now().UTC(), m.analysis, m.labelPredicate())
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
	expanded := make(map[string]bool)
	for id, node := range m.tree.issueMap {
		if node != nil {
			expanded[id] = node.Expanded
		}
	}
	searchQuery := m.tree.searchQuery
	searchActive := m.tree.searchActive
	searchSubtrees := m.tree.searchSubtrees
	viewportOffset := m.tree.viewportOffset
	filteredIssues := m.filteredIssuesForActiveView()
	if m.usesHubScope() {
		m.tree.BuildProjected(filteredIssues, m.issueMap)
	} else {
		m.tree.Build(filteredIssues)
	}
	for id, isExpanded := range expanded {
		if node := m.tree.issueMap[id]; node != nil {
			node.Expanded = isExpanded
		}
	}
	m.tree.searchQuery = searchQuery
	m.tree.searchActive = searchActive
	m.tree.searchSubtrees = searchSubtrees
	m.tree.rebuildFlatList()
	m.tree.viewportOffset = viewportOffset
	if selectedID != "" && m.tree.SelectByID(selectedID) {
		m.tree.ensureCursorVisible()
		return
	}
	if strings.TrimSpace(searchQuery) != "" && len(m.tree.searchMatches) > 0 {
		m.tree.selectSearchMatch(0)
	} else {
		m.tree.ensureCursorVisible()
	}
}

func (m Model) repositoryHistoryReport(report *correlation.HistoryReport) *correlation.HistoryReport {
	if m.usesHubScope() && m.hubScope.Mode == hub.HubScopeAllItems {
		return report
	}
	repositories := m.activeRepos
	if m.usesHubScope() && m.hubScope.Mode == hub.HubScopeContextless {
		repositories = map[string]bool{}
	}
	if !m.usesHubScope() && repositories == nil {
		return report
	}
	return projectHistoryReport(report, m.repositoryIssueIDs, repositories)
}

// refreshAttentionView recomputes scoped label attention and updates both the
// navigable attention view and the insights presentation.
func (m *Model) refreshAttentionView() {
	cfg := analysis.DefaultLabelHealthConfig()
	m.attentionCache = analysis.ComputeLabelAttentionScores(m.repositoryIssues, cfg, time.Now().UTC(), m.labelPredicate())
	m.attentionCached = true
	m.attentionView.SetData(m.attentionCache)
	height := m.height - 1
	if height < 3 {
		height = 3
	}
	m.attentionView.SetSize(m.width, height)
	attText := RenderAttentionView(m.attentionCache, max(40, m.width-4))
	m.rebuildInsightsPanel()
	m.insightsPanel.labelAttention = m.attentionCache.Labels
	m.insightsPanel.extraText = attText
}

func (m *Model) refreshFlowMatrix() {
	cfg := analysis.DefaultLabelHealthConfig()
	flow := analysis.ComputeCrossLabelFlow(m.repositoryIssues, cfg, m.labelPredicate())
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
