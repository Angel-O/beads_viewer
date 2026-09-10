package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// repositoryScopeController owns the repository universe and the user's
// selection. Model deliberately embeds it so existing UI code keeps its
// field-level access while catalog reconciliation has one owner.
type repositoryScopeController struct {
	repositoryCatalog         repositorypkg.Catalog
	repositorySelection       repositorypkg.Selection
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
	repositoryLabelPredicate  analysis.LabelPredicate
	repositoryIssueResolver   IssueRepositoryResolver
}

func newRepositoryScopeController() repositoryScopeController {
	return repositoryScopeController{repositorySelection: repositorypkg.NewAllSelection()}
}

func (s *repositoryScopeController) reconcileRepositorySelection(usesHub bool) {
	if !usesHub || s.repositorySelection.Mode() != repositorypkg.SelectionSelected {
		return
	}
	s.repositorySelection = s.repositorySelection.Reconcile(s.repositoryCatalog)
	s.activeRepos = selectionMap(s.repositorySelection)
	if len(s.activeRepos) == len(s.repositoryCatalog) && s.repositorySelection.IncludesUnassigned() {
		s.activeRepos = nil
	}
}

func (s *repositoryScopeController) setRepositoryScope(selected map[string]bool, usesHub bool) error {
	s.defaultRepositorySet = true
	s.defaultRepositoryID = ""
	reconciled := repositorypkg.ReconcileSelection(selected, s.repositoryCatalog)
	if usesHub {
		if len(selected) == 0 {
			s.repositorySelection = repositorypkg.NewAllSelection()
			s.activeRepos = nil
			return nil
		}
		return s.setPickerRepositorySelection(reconciled, false)
	}
	if len(selected) == 0 || len(reconciled) == len(s.repositoryCatalog) {
		s.repositorySelection = repositorypkg.NewAllSelection()
		s.activeRepos = nil
		return nil
	}
	selection, err := repositorypkg.NewSelectedSelection(sortedRepoKeys(reconciled))
	if err != nil {
		return err
	}
	s.repositorySelection = selection
	s.activeRepos = reconciled
	return nil
}

func (s *repositoryScopeController) setPickerRepositorySelection(selected map[string]bool, includeUnassigned bool) error {
	s.defaultRepositorySet = true
	s.defaultRepositoryID = ""
	if len(selected) == 0 {
		if includeUnassigned {
			s.repositorySelection = repositorypkg.NewUnassignedSelection()
		} else {
			s.repositorySelection = repositorypkg.NewAllSelection()
		}
		s.activeRepos = nil
		return nil
	}
	if includeUnassigned && len(selected) == len(s.repositoryCatalog) {
		s.repositorySelection = repositorypkg.NewAllSelection()
		s.activeRepos = nil
		return nil
	}
	contexts := sortedRepoKeys(selected)
	var scope repositorypkg.Selection
	if includeUnassigned {
		var err error
		scope, err = repositorypkg.NewSelectedAndUnassignedSelection(contexts)
		if err != nil {
			return err
		}
	} else {
		var err error
		scope, err = repositorypkg.NewSelectedSelection(contexts)
		if err != nil {
			return err
		}
	}
	s.repositorySelection = scope
	s.activeRepos = repositorypkg.ReconcileSelection(selected, s.repositoryCatalog)
	return nil
}

func (s *repositoryScopeController) setRepositorySelection(scope repositorypkg.Selection, usesHub bool) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.Mode() == repositorypkg.SelectionSelected {
		available := make(map[string]bool, len(s.repositoryCatalog))
		for _, repository := range s.repositoryCatalog {
			available[repository.ID] = true
		}
		for _, id := range scope.IDs() {
			if !available[id] {
				return fmt.Errorf("repository is not available: %s", id)
			}
		}
	}
	s.defaultRepositorySet = true
	s.defaultRepositoryID = ""
	s.repositorySelection = scope.Clone()
	s.activeRepos = selectionMap(scope)
	if scope.Mode() != repositorypkg.SelectionSelected || usesHub && scope.IncludesUnassigned() && len(s.activeRepos) == len(s.repositoryCatalog) {
		s.activeRepos = nil
	}
	return nil
}

func (m *Model) applyInitialRepositorySelection(selection *repositorypkg.Selection) {
	if selection == nil || m.defaultRepositorySet || selection.Mode() == repositorypkg.SelectionSelected && !m.repositoryCatalogReady {
		return
	}
	if err := m.SetRepositorySelection(selection.Clone()); err != nil {
		m.statusMsg = fmt.Sprintf("Initial repository selection unavailable: %v", err)
		m.statusIsError = true
	}
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
		scope, err := repositorypkg.NewSelectedSelection([]string{repository.ID})
		if err != nil {
			return false
		}
		s.activeRepos = map[string]bool{repository.ID: true}
		s.repositorySelection = scope
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
	if !usesHub && s.activeRepos == nil {
		return true
	}
	ids := issueRepositoryIDs(issue, s.repositoryCatalog, s.repositoryIssueResolver)
	if (s.repositorySelection.Mode() == repositorypkg.SelectionUnassigned || s.repositorySelection.IncludesUnassigned()) && len(ids) == 0 && issueHasRepositoryContext(issue, s.repositoryCatalog, s.repositoryIssueResolver, s.repositoryLabelPredicate) {
		return false
	}
	if s.repositorySelection.Matches(ids) {
		return true
	}
	// Legacy workspace filtering keeps issues with no source/prefix visible.
	return workspaceMode && !usesHub && len(ids) == 0
}

func (s repositoryScopeController) repositoryCandidates(issues []model.Issue, workspaceMode, usesHub bool) []model.Issue {
	if (!usesHub && s.activeRepos == nil) || (usesHub && s.repositorySelection.Mode() == repositorypkg.SelectionAll) {
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

// SetRepositoryCatalogIssues provides the unfiltered issue universe used for
// stable total counts when the initial TUI view is recipe-filtered.
func (m *Model) SetRepositoryCatalogIssues(issues []model.Issue) {
	m.repositoryScopeController.setCatalogIssues(issues)
}

func (m Model) contextlessBeadCount() int {
	if m.contextlessCountReady {
		return m.contextlessBeadCountValue
	}
	issues := m.repositoryCatalogIssues
	if issues == nil {
		issues = m.issues
	}
	return contextlessIssueCount(issues, m.repositoryCatalog, m.issueRepositoryResolver(), m.labelPredicate())
}

func (m *Model) reloadRepositoryCatalog() error {
	if m.workspaceMode {
		beforeScope := m.RepositorySelection()
		beforeRepos := sortedRepoKeys(m.activeRepos)
		m.repositoryCatalog = workspaceRepositoryCatalog(m.availableRepos, m.workspaceRepos, m.issues)
		m.activeRepos = repositorypkg.ReconcileSelection(m.activeRepos, m.repositoryCatalog)
		contextSortFallback := m.normalizeContextSortMode()
		scopeChanged := !sameRepositorySelection(beforeScope, m.repositorySelection) || !slices.Equal(beforeRepos, sortedRepoKeys(m.activeRepos))
		if scopeChanged {
			m.refreshRepositoryCandidates()
		} else if contextSortFallback && m.list.Width() > 0 {
			m.sortListItems(m.list.Items())
			m.updateViewportContent()
		}
		if m.showRepoPicker {
			m.repoPicker.SetCatalog(m.repositoryCatalog)
		}
		m.board.SetRepositoryPresentation(m.repositoryCatalog, false, m.currentRepositoryID, m.activeRepos, m.labelPredicate())
		m.insightsPanel.SetRepositoryPresentation(m.repositoryCatalog, false, m.labelPredicate())
		return nil
	}
	if strings.TrimSpace(m.catalogPath()) == "" {
		return nil
	}
	issues := m.issues
	if m.repositoryCatalogIssues != nil {
		issues = m.repositoryCatalogIssues
	}
	loader := m.runtimeServices.CatalogLoader
	if loader == nil {
		return nil
	}
	catalog, err := loader(m.catalogPath(), issues)
	if err != nil {
		return err
	}
	beforeScope := m.RepositorySelection()
	beforeRepos := sortedRepoKeys(m.activeRepos)
	m.repositoryScopeController.setCatalog(catalog)
	m.applyInitialRepositorySelection(m.runtimeServices.InitialRepositorySelection)
	m.reconcileRepositorySelectionCatalog()
	contextSortFallback := m.normalizeContextSortMode()
	scopeChanged := !sameRepositorySelection(beforeScope, m.repositorySelection) || !slices.Equal(beforeRepos, sortedRepoKeys(m.activeRepos))
	if scopeChanged {
		m.refreshRepositoryCandidates()
	} else if contextSortFallback && m.list.Width() > 0 {
		m.sortListItems(m.list.Items())
		m.updateViewportContent()
	}
	if m.showRepoPicker {
		m.repoPicker.SetCatalog(m.repositoryCatalog)
		m.repoPicker.SetContextlessBeadCount(m.contextlessBeadCount())
	}
	m.board.SetRepositoryPresentation(catalog, true, m.currentRepositoryID, m.activeRepos, m.labelPredicate())
	m.insightsPanel.SetRepositoryPresentation(catalog, true, m.labelPredicate())
	return nil
}

func (m *Model) applyRepositoryCatalogUpdate(catalog repositorypkg.Catalog, generation uint64, changed, recovered bool, err error) {
	if m.workspaceMode || !m.repositoryScopeController.acceptCatalogGeneration(generation) {
		return
	}
	if err != nil {
		m.statusMsg = fmt.Sprintf("Repository catalog reload failed (will retry): %v", err)
		m.statusIsError = true
		return
	}
	if changed {
		beforeScope := m.RepositorySelection()
		beforeRepos := sortedRepoKeys(m.activeRepos)
		m.repositoryScopeController.setCatalog(catalog)
		m.applyInitialRepositorySelection(m.runtimeServices.InitialRepositorySelection)
		if m.usesHubScope() {
			m.reconcileRepositorySelectionCatalog()
		} else {
			m.activeRepos = repositorypkg.ReconcileSelection(m.activeRepos, m.repositoryCatalog)
		}
		contextSortFallback := m.normalizeContextSortMode()
		if m.showRepoPicker {
			m.repoPicker.SetCatalog(m.repositoryCatalog)
			m.repoPicker.SetContextlessBeadCount(m.contextlessBeadCount())
		}
		m.refreshRepositoryPresentation()
		defaultApplied := m.applyDefaultRepositoryScope()
		scopeChanged := !sameRepositorySelection(beforeScope, m.repositorySelection) || !slices.Equal(beforeRepos, sortedRepoKeys(m.activeRepos))
		if !defaultApplied && scopeChanged {
			m.refreshRepositoryCandidates()
		} else if contextSortFallback && m.list.Width() > 0 {
			m.sortListItems(m.list.Items())
			m.updateViewportContent()
		}
	}
	if recovered && (strings.HasPrefix(m.statusMsg, "Repository catalog load failed:") || strings.HasPrefix(m.statusMsg, "Repository catalog reload failed")) {
		m.statusMsg = ""
		m.statusIsError = false
	}
}

func (m *Model) applyRepositoryPickerSelection() *Model {
	selection, err := m.repoPicker.RepositorySelection()
	if err != nil {
		return m
	}
	selected := selection.IDs()
	if m.repoPickerOrigin == focusBacklog || m.repoPickerOrigin == focusGlobalIssues {
		return m.applyBacklogPickerSelection(selection)
	}
	focusAfterApply := m.repoPickerOrigin
	if m.repoPickerOrigin == focusScopePicker {
		focusAfterApply = focusScopePicker
	}
	if m.hubRepositoryMode {
		includeUnassigned := selection.IncludesUnassigned() || selection.Mode() == repositorypkg.SelectionUnassigned
		switch {
		case len(selected) == 0 && includeUnassigned:
			m.statusMsg = "Context: no-context"
		case len(selected) == 0 || len(selected) == len(m.repositoryCatalog) && includeUnassigned:
			m.statusMsg = "Context: all"
		case includeUnassigned:
			m.statusMsg = fmt.Sprintf("Context: %s, no-context", strings.Join(m.repositoryScopeNamesForIDs(selected), ", "))
		default:
			m.statusMsg = fmt.Sprintf("Context: %s", strings.Join(m.repositoryScopeNamesForIDs(selected), ", "))
		}
		m.statusIsError = false
		if err := m.SetRepositorySelection(selection); err != nil {
			m.statusMsg, m.statusIsError = err.Error(), true
		}
	} else {
		if len(selected) == 0 || len(selected) == len(m.repositoryCatalog) {
			m.statusMsg = "Context: all"
		} else {
			m.statusMsg = fmt.Sprintf("Context: %s", strings.Join(m.repositoryScopeNamesForIDs(selected), ", "))
		}
		m.statusIsError = false
		m.SetRepositoryScope(repositoryIDsMap(selected))
	}
	m.showRepoPicker = false
	m.focused = focusAfterApply
	if focusAfterApply == focusTree {
		m.rebuildRepositoryTree()
	}
	return m
}

func (m Model) repositoryScopeNamesForIDs(ids []string) []string {
	return m.repositoryScopeNames(repositoryIDsMap(ids))
}

func repositoryIDsMap(ids []string) map[string]bool {
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	return selected
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
	if m.runtimeServices.LabelPredicate != nil {
		return m.runtimeServices.LabelPredicate
	}
	return nil
}

func (m Model) issueRepositoryResolver() IssueRepositoryResolver {
	return m.runtimeServices.IssueRepositoryResolver
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
	if m.repositorySelection.Mode() == repositorypkg.SelectionSelected {
		for _, id := range m.repositorySelection.IDs() {
			effective[id] = struct{}{}
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
	switch m.repositorySelection.Mode() {
	case repositorypkg.SelectionUnassigned:
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

func contextlessIssueCount(issues []model.Issue, catalog repositorypkg.Catalog, resolver IssueRepositoryResolver, predicate analysis.LabelPredicate) int {
	count := 0
	for _, issue := range issues {
		if !issueHasRepositoryContext(issue, catalog, resolver, predicate) {
			count++
		}
	}
	return count
}

func selectionMap(selection repositorypkg.Selection) map[string]bool {
	if selection.Mode() != repositorypkg.SelectionSelected {
		return nil
	}
	selected := make(map[string]bool, len(selection.IDs()))
	for _, id := range selection.IDs() {
		selected[id] = true
	}
	return selected
}

func sameRepositorySelection(a, b repositorypkg.Selection) bool {
	return a.Mode() == b.Mode() && a.IncludesUnassigned() == b.IncludesUnassigned() && slices.Equal(a.IDs(), b.IDs())
}

func repositoryCatalogHasID(catalog repositorypkg.Catalog, id string) bool {
	for _, entry := range catalog {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func issueRepositoryIDs(issue model.Issue, catalog repositorypkg.Catalog, resolver IssueRepositoryResolver) []string {
	if resolver != nil {
		ids := append([]string(nil), resolver(issue)...)
		sort.Strings(ids)
		unique := ids[:0]
		for _, id := range ids {
			if id != "" && (len(unique) == 0 || unique[len(unique)-1] != id) {
				unique = append(unique, id)
			}
		}
		return unique
	}
	ids := make([]string, 0)
	workspaceKey := ""
	for _, entry := range catalog {
		matched := false
		if entry.Kind == repositorypkg.IdentityExact || entry.Kind == "" {
			for _, label := range issue.Labels {
				if label == entry.ID {
					matched = true
					break
				}
			}
		} else if entry.Kind == repositorypkg.IdentityPrefix {
			if workspaceKey == "" {
				workspaceKey = issueRepoKey(issue)
			}
			matched = workspaceKey == entry.ID
		}
		if matched {
			ids = append(ids, entry.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func issueHasRepositoryContext(issue model.Issue, catalog repositorypkg.Catalog, resolver IssueRepositoryResolver, predicate analysis.LabelPredicate) bool {
	if len(issueRepositoryIDs(issue, catalog, resolver)) > 0 {
		return true
	}
	if resolver != nil || predicate == nil {
		return false
	}
	for _, label := range issue.Labels {
		if !predicate(label) {
			return true
		}
	}
	return false
}

// repositoryPresentationForIssue is the single presentation policy entry
// point. The neutral label predicate is explicit; nil is valid for local mode
// and never turns label matching into repository resolution.
func repositoryPresentationForIssue(issue model.Issue, catalog repositorypkg.Catalog, hubMode bool, currentRepositoryID string, preferredRepositories map[string]bool, predicate analysis.LabelPredicate) issueRepositoryPresentation {
	presentation := issueRepositoryPresentation{Labels: issue.Labels}
	if !hubMode {
		return presentation
	}

	presentation.Labels = make([]string, 0, len(issue.Labels))
	contexts := make(map[string]bool)
	for _, label := range issue.Labels {
		if repositoryCatalogHasID(catalog, label) || predicate != nil && !predicate(label) {
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
		if !repositoryCatalogHasID(catalog, label) {
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
	return !m.workspaceMode && (m.hubRepositoryMode || strings.TrimSpace(m.catalogPath()) != "")
}

func (m *Model) decorateIssueItem(item *IssueItem) {
	if item == nil {
		return
	}
	presentation := repositoryPresentationForIssue(item.Issue, m.repositoryCatalog, m.hubRepositoryPresentation(), m.currentRepositoryID, m.activeRepos, m.labelPredicate())
	item.HubPresentation = m.hubRepositoryPresentation()
	item.RepositoryID = presentation.ID
	item.RepositoryName = presentation.Name
	item.RepositoryExtra = presentation.Extra
	item.RepositoryNames = presentation.Names
	item.PresentationLabels = presentation.Labels
}

func projectHubLabelHealth(result analysis.LabelAnalysisResult, catalog repositorypkg.Catalog, hubMode bool) analysis.LabelAnalysisResult {
	if !hubMode {
		return result
	}
	result.TotalLabels, result.HealthyCount, result.WarningCount, result.CriticalCount = 0, 0, 0, 0
	result.Labels = slicesDeleteContextHealth(result.Labels, catalog)
	result.Summaries = slicesDeleteContextSummaries(result.Summaries, catalog)
	result.AttentionNeeded = filterRepositoryLabels(result.AttentionNeeded, catalog)
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

func slicesDeleteContextHealth(values []analysis.LabelHealth, catalog repositorypkg.Catalog) []analysis.LabelHealth {
	filtered := make([]analysis.LabelHealth, 0, len(values))
	for _, value := range values {
		if !repositoryCatalogHasID(catalog, value.Label) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func slicesDeleteContextSummaries(values []analysis.LabelSummary, catalog repositorypkg.Catalog) []analysis.LabelSummary {
	filtered := make([]analysis.LabelSummary, 0, len(values))
	for _, value := range values {
		if !repositoryCatalogHasID(catalog, value.Label) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterRepositoryLabels(values []string, catalog repositorypkg.Catalog) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if !repositoryCatalogHasID(catalog, value) {
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
	predicate := m.labelPredicate()
	m.board.SetRepositoryPresentation(m.repositoryCatalog, hubMode, m.currentRepositoryID, m.activeRepos, predicate)
	m.insightsPanel.SetRepositoryPresentation(m.repositoryCatalog, hubMode, predicate)
}

func (m *Model) issueMatchesRepositoryScope(issue model.Issue) bool {
	m.repositoryScopeController.repositoryIssueResolver = m.issueRepositoryResolver()
	m.repositoryScopeController.repositoryLabelPredicate = m.labelPredicate()
	return m.repositoryScopeController.issueMatchesRepositoryScope(issue, m.workspaceMode, m.usesHubScope())
}

func (m *Model) repositoryCandidates() []model.Issue {
	m.repositoryScopeController.repositoryIssueResolver = m.issueRepositoryResolver()
	m.repositoryScopeController.repositoryLabelPredicate = m.labelPredicate()
	return m.repositoryScopeController.repositoryCandidates(m.issues, m.workspaceMode, m.usesHubScope())
}

func (m Model) repositoryScopeIsAll() bool {
	if m.usesHubScope() {
		return m.repositorySelection.Mode() == repositorypkg.SelectionAll
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

func (m *Model) reconcileRepositorySelectionCatalog() {
	m.repositoryScopeController.reconcileRepositorySelection(m.usesHubScope())
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

// SetRepositorySelection applies a validated neutral repository projection.
func (m *Model) SetRepositorySelection(scope repositorypkg.Selection) error {
	if err := m.repositoryScopeController.setRepositorySelection(scope, m.usesHubScope()); err != nil {
		return err
	}
	m.refreshRepositoryCandidates()
	m.refreshRepositoryPresentation()
	return nil
}

// RepositorySelection returns a detached neutral repository projection.
func (m Model) RepositorySelection() repositorypkg.Selection {
	return m.repositorySelection.Clone()
}

// RepositoryScope returns a defensive copy of the selected exact catalog IDs.
// Nil means all repositories.
func (m Model) RepositoryScope() map[string]bool {
	if m.usesHubScope() && m.repositorySelection.Mode() != repositorypkg.SelectionSelected {
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
		if !m.matchesIssueType(issue) {
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
		plan := projectExecutionPlan(m.analyzer.GetExecutionPlan(), m.activeTypeIssueIDs(), m.typeFilteredIssues(m.repositoryIssues))
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
		m.labelHealthCache = analysis.ComputeAllLabelHealth(m.typeFilteredIssues(m.repositoryIssues), cfg, time.Now().UTC(), m.analysis, m.labelPredicate())
		m.labelHealthCache = projectHubLabelHealth(m.labelHealthCache, m.repositoryCatalog, m.hubRepositoryPresentation())
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
	typeFilterActive := len(m.activeIssueTypes) > 0
	if m.usesHubScope() && m.repositorySelection.Mode() == repositorypkg.SelectionAll && !typeFilterActive {
		return report
	}
	if !m.usesHubScope() && m.repositorySelection.Mode() == repositorypkg.SelectionAll && !typeFilterActive {
		return report
	}
	ids := m.repositoryIssueIDs
	if typeFilterActive {
		ids = issueIDSet(m.typeFilteredIssues(m.repositoryIssues))
	}
	return projectHistoryReport(report, ids, m.repositorySelection)
}

// refreshAttentionView recomputes scoped label attention and updates both the
// navigable attention view and the insights presentation.
func (m *Model) refreshAttentionView() {
	cfg := analysis.DefaultLabelHealthConfig()
	m.attentionCache = analysis.ComputeLabelAttentionScores(m.typeFilteredIssues(m.repositoryIssues), cfg, time.Now().UTC(), m.labelPredicate())
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
	issues := m.typeFilteredIssues(m.repositoryIssues)
	flow := analysis.ComputeCrossLabelFlow(issues, cfg, m.labelPredicate())
	m.flowMatrix = NewFlowMatrixModel(m.theme)
	m.flowMatrix.SetData(&flow, issues)
	panelHeight := m.height - 2
	if panelHeight < 3 {
		panelHeight = 3
	}
	m.flowMatrix.SetSize(m.width, panelHeight)
}

func projectHistoryReport(report *correlation.HistoryReport, ids map[string]bool, selection repositorypkg.Selection) *correlation.HistoryReport {
	if report == nil {
		return nil
	}
	projected := *report
	projected.Histories = make(map[string]correlation.BeadHistory)
	projected.CommitIndex = make(correlation.CommitIndex)
	projected.Warnings = nil
	for _, warning := range report.Warnings {
		warningRepositories := []string(nil)
		if warning.Context != "" {
			warningRepositories = []string{warning.Context}
		}
		if selection.Matches(warningRepositories) {
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
