package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/export"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

type hubScopeProjection struct {
	scope     hub.HubScope
	issues    map[string]model.Issue
	inScopeID map[string]bool
	label     string
}

func parseHubRobotScope(raw, configPath string, hubMode, robotMode bool) (*hub.HubScope, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if !hubMode || !robotMode {
		return nil, fmt.Errorf("BV_WBV_HUB_SCOPE is valid only for Hub robot invocations")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var scope hub.HubScope
	if err := decoder.Decode(&scope); err != nil {
		return nil, fmt.Errorf("decoding wrapper Hub scope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decoding wrapper Hub scope: trailing JSON data")
	}
	if err := scope.Validate(); err != nil {
		return nil, fmt.Errorf("validating wrapper Hub scope: %w", err)
	}
	if scope.Mode == hub.HubScopeSelectedContexts {
		config, err := hub.Resolve(configPath)
		if err != nil {
			return nil, fmt.Errorf("loading registered Hub contexts: %w", err)
		}
		for _, contextID := range scope.Contexts {
			if _, registered := config.Repositories[contextID]; !registered {
				return nil, fmt.Errorf("Hub context is not registered: %s", contextID)
			}
		}
	}
	clone := scope.Clone()
	return &clone, nil
}

func newHubScopeProjection(scope hub.HubScope, issues []model.Issue, label string) (*hubScopeProjection, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	projection := &hubScopeProjection{
		scope:     scope.Clone(),
		issues:    make(map[string]model.Issue, len(issues)),
		inScopeID: make(map[string]bool, len(issues)),
		label:     strings.TrimSpace(label),
	}
	for _, issue := range issues {
		projection.issues[issue.ID] = issue
		projection.inScopeID[issue.ID] = hubScopeContains(scope, issue) && issueHasExactLabel(issue, projection.label)
	}
	return projection, nil
}

func issueHasExactLabel(issue model.Issue, label string) bool {
	if label == "" {
		return true
	}
	for _, candidate := range issue.Labels {
		if candidate == label {
			return true
		}
	}
	return false
}

func hubScopeContains(scope hub.HubScope, issue model.Issue) bool {
	return scope.MatchesLabels(issue.Labels)
}

func (p *hubScopeProjection) issuesInScope(issues []model.Issue) []model.Issue {
	if p == nil || (p.scope.Mode == hub.HubScopeAllItems && p.label == "") {
		return issues
	}
	result := make([]model.Issue, 0, len(issues))
	for _, issue := range issues {
		if p.inScopeID[issue.ID] {
			result = append(result, issue)
		}
	}
	return result
}

func (p *hubScopeProjection) candidateFilter() analysis.CandidatePredicate {
	if p == nil {
		return nil
	}
	return func(issueID string) bool { return p.inScopeID[issueID] }
}

// decorateRobotResult is the Hub adapter for the generic robot result hook.
// It mutates typed command results directly; robot dispatch never needs to
// know how Hub scope is represented.
func (p *hubScopeProjection) decorateRobotResult(command string, result RobotResult) error {
	if p == nil {
		return nil
	}
	switch output := result.(type) {
	case *robotPlanOutput:
		p.decorateScope(&output.Scope)
		p.projectPlanResult(&output.Plan)
	case *robotPriorityOutput:
		p.decorateScope(&output.Scope)
		for i := range output.Recommendations {
			output.Recommendations[i].BoundaryRefs = p.robotBoundaryRefs(output.Recommendations[i].IssueID)
		}
	case *robotInsightsOutput:
		p.decorateScope(&output.Scope)
	case *robotGraphOutput:
		p.decorateScope(&output.Scope)
		if output.stats != nil {
			projected, err := p.exportGraph(output.issues, output.stats, output.config)
			if err != nil {
				return fmt.Errorf("projecting graph result: %w", err)
			}
			output.GraphExportResult = projected
		} else {
			p.projectGraphResult(output.GraphExportResult)
		}
	case *robotForecastOutput:
		p.decorateScope(&output.Scope)
		forecasts := output.Forecasts[:0]
		for _, forecast := range output.Forecasts {
			if p.inScopeID[forecast.IssueID] {
				forecasts = append(forecasts, forecast)
			}
		}
		output.Forecasts = forecasts
	case *robotLabelHealthOutput:
		p.decorateScope(&output.Scope)
		p.projectLabelHealthResult(&output.Results)
	case *robotLabelFlowOutput:
		p.decorateScope(&output.Scope)
		p.projectLabelFlowResult(&output.Flow)
	case *robotAttentionOutput:
		p.decorateScope(&output.Scope)
	case *robotBlockerChainOutput:
		p.decorateScope(&output.Scope)
	case *robotSprintListOutput:
		p.decorateScope(&output.Scope)
		p.projectSprintListResult(output.Sprints)
	case *robotSprintShowOutput:
		p.decorateScope(&output.Scope)
		if output.Sprint != nil {
			p.projectSprintBeadIDs(&output.Sprint.BeadIDs)
		}
	case *robotCapacityOutput:
		p.decorateScope(&output.Scope)
	case *robotTriageOutput:
		p.decorateScope(&output.Scope)
		p.projectTriageResult(&output.Triage)
	case *briefTriageOutput:
		p.decorateScope(&output.Scope)
		output.QuickRef.TopPicks = p.projectTriageTopPicks(output.QuickRef.TopPicks)
		filtered := output.Recommendations[:0]
		for _, recommendation := range output.Recommendations {
			if p.inScopeID[recommendation.ID] {
				recommendation.BoundaryRefs = p.robotBoundaryRefs(recommendation.ID)
				filtered = append(filtered, recommendation)
			}
		}
		output.Recommendations = filtered
		quickWins := output.QuickWins[:0]
		for _, item := range output.QuickWins {
			if p.inScopeID[item.ID] {
				item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
				quickWins = append(quickWins, item)
			}
		}
		output.QuickWins = quickWins
		blockers := output.BlockersToClear[:0]
		for _, item := range output.BlockersToClear {
			if p.inScopeID[item.ID] {
				item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
				blockers = append(blockers, item)
			}
		}
		output.BlockersToClear = blockers
	}
	return nil
}

func (p *hubScopeProjection) decorateScope(scope **robotScopeMetadata) {
	*scope = &robotScopeMetadata{
		Mode:               string(p.scope.Mode),
		Contexts:           append([]string(nil), p.scope.Contexts...),
		IncludeContextless: p.scope.IncludeContextless,
	}
}

func (p *hubScopeProjection) robotBoundaryRefs(issueID string) []robotBoundaryReference {
	return p.hiddenOpenBlockers(issueID)
}

func (p *hubScopeProjection) projectPlanResult(plan *robotExecutionPlan) {
	for i := range plan.Tracks {
		items := plan.Tracks[i].Items[:0]
		for _, item := range plan.Tracks[i].Items {
			if !p.inScopeID[item.ID] {
				continue
			}
			item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
			items = append(items, item)
		}
		plan.Tracks[i].Items = items
	}
	tracks := plan.Tracks[:0]
	highestID := ""
	highestCount := -1
	for _, track := range plan.Tracks {
		if len(track.Items) == 0 {
			continue
		}
		tracks = append(tracks, track)
		for _, item := range track.Items {
			if len(item.UnblocksIDs) > highestCount || (len(item.UnblocksIDs) == highestCount && (highestID == "" || item.ID < highestID)) {
				highestID = item.ID
				highestCount = len(item.UnblocksIDs)
			}
		}
	}
	plan.Tracks = tracks
	plan.Summary.HighestImpact = highestID
	if highestCount < 0 {
		highestCount = 0
	}
	plan.Summary.UnblocksCount = highestCount
	if highestID == "" {
		plan.Summary.ImpactReason = "No actionable item in scope"
	} else {
		plan.Summary.ImpactReason = fmt.Sprintf("Unblocks %d issue(s)", highestCount)
	}
}

func (p *hubScopeProjection) projectGraphResult(result *export.GraphExportResult) {
	if result == nil {
		return
	}
	result.Adjacency = p.projectGraphAdjacency(result.Adjacency)
	result.Nodes = len(result.Adjacency.Nodes)
	result.Edges = len(result.Adjacency.Edges)
}

func (p *hubScopeProjection) projectLabelHealthResult(results *analysis.LabelAnalysisResult) {
	for i := range results.Labels {
		results.Labels[i].Issues = p.projectIDs(results.Labels[i].Issues)
	}
	for i := range results.Summaries {
		if issues := p.projectIDsForLabel(results.Summaries[i].Label, results.Labels); len(issues) > 0 {
			results.Summaries[i].TopIssue = issues[0]
		} else {
			results.Summaries[i].TopIssue = ""
		}
	}
	if results.CrossLabelFlow != nil {
		p.projectLabelFlowResult(results.CrossLabelFlow)
	}
}

func (p *hubScopeProjection) projectIDs(ids []string) []string {
	filtered := ids[:0]
	for _, id := range ids {
		if p.inScopeID[id] {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func (p *hubScopeProjection) projectIDsForLabel(label string, labels []analysis.LabelHealth) []string {
	for _, entry := range labels {
		if entry.Label == label {
			return entry.Issues
		}
	}
	return nil
}

func (p *hubScopeProjection) projectLabelFlowResult(flow *analysis.CrossLabelFlow) {
	for i := range flow.Dependencies {
		flow.Dependencies[i].IssueIDs = p.projectIDs(flow.Dependencies[i].IssueIDs)
	}
}

func (p *hubScopeProjection) projectSprintListResult(sprints []model.Sprint) {
	for i := range sprints {
		p.projectSprintBeadIDs(&sprints[i].BeadIDs)
	}
}

func (p *hubScopeProjection) projectSprintBeadIDs(ids *[]string) {
	filtered := (*ids)[:0]
	for _, id := range *ids {
		if p.inScopeID[id] {
			filtered = append(filtered, id)
		}
	}
	*ids = filtered
}

func (p *hubScopeProjection) projectTriageResult(result *robotTriageResult) {
	result.QuickRef.TopPicks = p.projectTriageTopPicks(result.QuickRef.TopPicks)
	result.Recommendations = p.projectTriageRecommendations(result.Recommendations)
	quickWins := result.QuickWins[:0]
	for _, item := range result.QuickWins {
		if p.inScopeID[item.ID] {
			item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
			quickWins = append(quickWins, item)
		}
	}
	result.QuickWins = quickWins
	blockers := result.BlockersToClear[:0]
	for _, item := range result.BlockersToClear {
		if p.inScopeID[item.ID] {
			item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
			blockers = append(blockers, item)
		}
	}
	result.BlockersToClear = blockers
	for i := range result.RecommendationsByTrack {
		result.RecommendationsByTrack[i].Recommendations = p.projectTriageRecommendations(result.RecommendationsByTrack[i].Recommendations)
		if pick := result.RecommendationsByTrack[i].TopPick; pick != nil {
			if !p.inScopeID[pick.ID] {
				result.RecommendationsByTrack[i].TopPick = nil
				result.RecommendationsByTrack[i].ClaimCommand = ""
			} else {
				pick.BoundaryRefs = p.robotBoundaryRefs(pick.ID)
			}
		}
	}
	for i := range result.RecommendationsByLabel {
		result.RecommendationsByLabel[i].Recommendations = p.projectTriageRecommendations(result.RecommendationsByLabel[i].Recommendations)
		if pick := result.RecommendationsByLabel[i].TopPick; pick != nil {
			if !p.inScopeID[pick.ID] {
				result.RecommendationsByLabel[i].TopPick = nil
				result.RecommendationsByLabel[i].ClaimCommand = ""
			} else {
				pick.BoundaryRefs = p.robotBoundaryRefs(pick.ID)
			}
		}
	}
	alerts := result.Alerts[:0]
	for _, alert := range result.Alerts {
		if alert.IssueID != "" && !p.inScopeID[alert.IssueID] {
			continue
		}
		hadIssueIDs := len(alert.IssueIDs) > 0
		alert.IssueIDs = p.projectIDs(alert.IssueIDs)
		if hadIssueIDs && len(alert.IssueIDs) == 0 {
			continue
		}
		alerts = append(alerts, alert)
	}
	result.Alerts = alerts
	if len(result.QuickRef.TopPicks) == 0 {
		result.Commands.ClaimTop = "CI=1 br ready --json  # No top pick available"
		result.Commands.ShowTop = "CI=1 br ready --json  # No top pick available"
	} else {
		id := result.QuickRef.TopPicks[0].ID
		result.Commands.ClaimTop = fmt.Sprintf("CI=1 br update %s --status in_progress --json", id)
		result.Commands.ShowTop = fmt.Sprintf("CI=1 br show %s --json", id)
	}
}

func (p *hubScopeProjection) projectTriageTopPicks(items []robotTriageTopPick) []robotTriageTopPick {
	filtered := items[:0]
	for _, item := range items {
		if p.inScopeID[item.ID] {
			item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (p *hubScopeProjection) projectTriageRecommendations(items []robotTriageRecommendation) []robotTriageRecommendation {
	filtered := items[:0]
	for _, item := range items {
		if p.inScopeID[item.ID] {
			item.BoundaryRefs = p.robotBoundaryRefs(item.ID)
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (p *hubScopeProjection) exportGraph(issues []model.Issue, stats *analysis.GraphStats, config export.GraphExportConfig) (*export.GraphExportResult, error) {
	canonicalConfig := config
	canonicalConfig.Format = export.GraphFormatJSON
	canonicalConfig.Label = ""
	canonical, err := export.ExportGraph(issues, stats, canonicalConfig)
	if err != nil {
		return nil, err
	}

	focused := make(map[string]bool)
	if canonical.Adjacency != nil {
		for _, node := range canonical.Adjacency.Nodes {
			focused[node.ID] = true
		}
	}
	visibleIssues := make([]model.Issue, 0, len(focused))
	for _, issue := range issues {
		if focused[issue.ID] && p.inScopeID[issue.ID] {
			visibleIssues = append(visibleIssues, issue)
		}
	}

	renderConfig := config
	renderConfig.Label = ""
	renderConfig.Root = ""
	renderConfig.Depth = 0
	result, err := export.ExportGraph(visibleIssues, stats, renderConfig)
	if err != nil {
		return nil, err
	}
	result.DataHash = config.DataHash
	result.FiltersApplied = canonical.FiltersApplied
	if result.FiltersApplied == nil {
		result.FiltersApplied = make(map[string]string)
	}
	if p.label != "" {
		result.FiltersApplied["label"] = p.label
	}
	result.Adjacency = p.projectGraphAdjacency(canonical.Adjacency)
	result.Nodes = len(result.Adjacency.Nodes)
	result.Edges = len(result.Adjacency.Edges)
	return result, nil
}

func (p *hubScopeProjection) projectGraphAdjacency(canonical *export.AdjacencyGraph) *export.AdjacencyGraph {
	result := &export.AdjacencyGraph{Nodes: []export.AdjacencyNode{}, Edges: []export.AdjacencyEdge{}}
	if canonical == nil {
		return result
	}
	nodeIndex := make(map[string]int)
	for _, node := range canonical.Nodes {
		if p.inScopeID[node.ID] {
			nodeIndex[node.ID] = len(result.Nodes)
			result.Nodes = append(result.Nodes, node)
		}
	}
	for _, edge := range canonical.Edges {
		fromVisible := p.inScopeID[edge.From]
		toVisible := p.inScopeID[edge.To]
		if fromVisible && toVisible {
			result.Edges = append(result.Edges, edge)
			continue
		}
		if fromVisible == toVisible {
			continue
		}
		visibleID, hiddenID := edge.From, edge.To
		if !fromVisible {
			visibleID, hiddenID = edge.To, edge.From
		}
		hidden, exists := p.issues[hiddenID]
		if !exists {
			continue
		}
		index := nodeIndex[visibleID]
		result.Nodes[index].BoundaryRefs = append(result.Nodes[index].BoundaryRefs, export.GraphBoundaryReference{
			RelationType: edge.Type,
			EndpointID:   hiddenID,
			IssueType:    string(hidden.IssueType),
			Status:       string(hidden.Status),
			Contexts:     issueContexts(hidden),
			InScope:      false,
			From:         edge.From,
			To:           edge.To,
		})
	}
	for i := range result.Nodes {
		sort.Slice(result.Nodes[i].BoundaryRefs, func(left, right int) bool {
			first := result.Nodes[i].BoundaryRefs[left]
			second := result.Nodes[i].BoundaryRefs[right]
			if first.From != second.From {
				return first.From < second.From
			}
			if first.To != second.To {
				return first.To < second.To
			}
			return first.RelationType < second.RelationType
		})
	}
	return result
}

func (p *hubScopeProjection) hiddenOpenBlockers(issueID string) []robotBoundaryReference {
	issue, ok := p.issues[issueID]
	if !ok {
		return nil
	}
	refs := make([]robotBoundaryReference, 0)
	for _, dependency := range issue.Dependencies {
		if dependency == nil || !dependency.Type.IsBlocking() || p.inScopeID[dependency.DependsOnID] {
			continue
		}
		blocker, exists := p.issues[dependency.DependsOnID]
		if !exists || blocker.Status == model.StatusClosed || blocker.Status == model.StatusTombstone {
			continue
		}
		relationType := string(dependency.Type)
		if relationType == "" {
			relationType = string(model.DepBlocks)
		}
		refs = append(refs, robotBoundaryReference{
			RelationType: relationType,
			EndpointID:   blocker.ID,
			IssueType:    blocker.IssueType,
			Status:       blocker.Status,
			Contexts:     issueContexts(blocker),
			InScope:      false,
		})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].RelationType != refs[j].RelationType {
			return refs[i].RelationType < refs[j].RelationType
		}
		return refs[i].EndpointID < refs[j].EndpointID
	})
	return refs
}

func issueContexts(issue model.Issue) []string {
	seen := make(map[string]bool)
	contexts := make([]string, 0)
	for _, label := range issue.Labels {
		if strings.HasPrefix(label, "ctx:") && !seen[label] {
			seen[label] = true
			contexts = append(contexts, label)
		}
	}
	sort.Strings(contexts)
	return contexts
}
