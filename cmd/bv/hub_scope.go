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
	scope     model.HubScope
	issues    map[string]model.Issue
	inScopeID map[string]bool
	label     string
}

type hubBoundaryReference struct {
	RelationType string          `json:"relation_type"`
	EndpointID   string          `json:"endpoint_id"`
	IssueType    model.IssueType `json:"issue_type"`
	Status       model.Status    `json:"status"`
	Contexts     []string        `json:"contexts"`
	InScope      bool            `json:"in_scope"`
}

type hubScopeRobotEncoder struct {
	base       robotEncoder
	command    string
	projection *hubScopeProjection
}

func parseHubRobotScope(raw, configPath string, hubMode, robotMode bool) (*model.HubScope, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if !hubMode || !robotMode {
		return nil, fmt.Errorf("BV_WBV_HUB_SCOPE is valid only for Hub robot invocations")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var scope model.HubScope
	if err := decoder.Decode(&scope); err != nil {
		return nil, fmt.Errorf("decoding wrapper Hub scope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decoding wrapper Hub scope: trailing JSON data")
	}
	if err := scope.Validate(); err != nil {
		return nil, fmt.Errorf("validating wrapper Hub scope: %w", err)
	}
	if scope.Mode == model.HubScopeSelectedContexts {
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

func newHubScopeProjection(scope model.HubScope, issues []model.Issue, label string) (*hubScopeProjection, error) {
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

func hubScopeContains(scope model.HubScope, issue model.Issue) bool {
	switch scope.Mode {
	case model.HubScopeAllItems:
		return true
	case model.HubScopeContextless:
		for _, label := range issue.Labels {
			if strings.HasPrefix(label, "ctx:") {
				return false
			}
		}
		return true
	case model.HubScopeSelectedContexts:
		selected := make(map[string]bool, len(scope.Contexts))
		for _, contextID := range scope.Contexts {
			selected[contextID] = true
		}
		for _, label := range issue.Labels {
			if selected[label] {
				return true
			}
		}
	}
	return false
}

func (p *hubScopeProjection) issuesInScope(issues []model.Issue) []model.Issue {
	if p == nil || (p.scope.Mode == model.HubScopeAllItems && p.label == "") {
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

func (e hubScopeRobotEncoder) Encode(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshaling scoped robot output: %w", err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		return fmt.Errorf("decoding scoped robot output: %w", err)
	}
	e.projection.project(e.command, output)
	return e.base.Encode(output)
}

func (p *hubScopeProjection) project(command string, output map[string]any) {
	output["scope"] = map[string]any{
		"mode":     string(p.scope.Mode),
		"contexts": append([]string(nil), p.scope.Contexts...),
	}

	switch command {
	case "robot-plan":
		p.projectPlan(output)
	case "robot-priority":
		p.filterObjectArray(output, "recommendations", true, "issue_id")
	case "robot-insights":
		p.projectInsights(output)
	case "robot-graph":
		p.projectGraph(output)
	case "robot-forecast":
		p.filterObjectArray(output, "forecasts", false, "issue_id")
	case "robot-capacity":
		p.filterStringArray(output, "actionable")
		p.filterObjectArray(output, "bottlenecks", false, "id")
		if actionable, ok := output["actionable"].([]any); ok {
			output["actionable_count"] = len(actionable)
		}
	case "robot-label-health":
		p.projectLabelHealth(output)
	case "robot-label-flow":
		if flow, ok := output["flow"].(map[string]any); ok {
			p.projectLabelFlow(flow)
		}
	case "robot-sprint-list":
		p.projectSprintList(output)
	case "robot-sprint-show":
		if sprint, ok := output["sprint"].(map[string]any); ok {
			p.filterStringArray(sprint, "bead_ids")
		}
	case "robot-triage", "robot-triage-by-track", "robot-triage-by-label":
		p.projectTriage(output)
	}
}

func (p *hubScopeProjection) projectLabelHealth(output map[string]any) {
	results, ok := output["results"].(map[string]any)
	if !ok {
		return
	}
	labels, _ := results["labels"].([]any)
	topIssueByLabel := make(map[string]string)
	for _, rawLabel := range labels {
		if label, ok := rawLabel.(map[string]any); ok {
			p.filterStringArray(label, "issues")
			issues, _ := label["issues"].([]any)
			if len(issues) > 0 {
				if name, ok := label["label"].(string); ok {
					topIssueByLabel[name], _ = issues[0].(string)
				}
			}
		}
	}
	if summaries, ok := results["summaries"].([]any); ok {
		for _, rawSummary := range summaries {
			summary, ok := rawSummary.(map[string]any)
			if !ok {
				continue
			}
			label, _ := summary["label"].(string)
			if topIssue := topIssueByLabel[label]; topIssue != "" {
				summary["top_issue"] = topIssue
			} else {
				delete(summary, "top_issue")
			}
		}
	}
	if flow, ok := results["cross_label_flow"].(map[string]any); ok {
		p.projectLabelFlow(flow)
	}
}

func (p *hubScopeProjection) projectLabelFlow(flow map[string]any) {
	dependencies, _ := flow["dependencies"].([]any)
	for _, rawDependency := range dependencies {
		if dependency, ok := rawDependency.(map[string]any); ok {
			p.filterStringArray(dependency, "issue_ids")
		}
	}
}

func (p *hubScopeProjection) projectPlan(output map[string]any) {
	plan, ok := output["plan"].(map[string]any)
	if !ok {
		return
	}
	tracks, _ := plan["tracks"].([]any)
	projected := tracks[:0]
	highestImpactID := ""
	highestImpactCount := -1
	for _, rawTrack := range tracks {
		track, ok := rawTrack.(map[string]any)
		if !ok {
			continue
		}
		p.filterObjectArray(track, "items", true, "id")
		if items, _ := track["items"].([]any); len(items) > 0 {
			for _, rawItem := range items {
				item := rawItem.(map[string]any)
				id := objectID(item, "id")
				unblocks, _ := item["unblocks"].([]any)
				if len(unblocks) > highestImpactCount || (len(unblocks) == highestImpactCount && (highestImpactID == "" || id < highestImpactID)) {
					highestImpactID = id
					highestImpactCount = len(unblocks)
				}
			}
			projected = append(projected, track)
		}
	}
	plan["tracks"] = projected
	if summary, ok := plan["summary"].(map[string]any); ok {
		summary["highest_impact"] = highestImpactID
		if highestImpactCount < 0 {
			highestImpactCount = 0
		}
		summary["unblocks_count"] = highestImpactCount
		if highestImpactID == "" {
			summary["impact_reason"] = "No actionable item in scope"
		} else {
			summary["impact_reason"] = fmt.Sprintf("Unblocks %d issue(s)", highestImpactCount)
		}
	}
}

func (p *hubScopeProjection) projectInsights(output map[string]any) {
	for _, key := range []string{"Bottlenecks", "Keystones", "Influencers", "Hubs", "Authorities", "Cores", "Slack", "top_what_ifs"} {
		p.filterObjectArray(output, key, false, "ID", "id", "issue_id")
	}
	for _, key := range []string{"Articulation", "Orphans"} {
		p.filterStringArray(output, key)
	}
	if stats, ok := output["full_stats"].(map[string]any); ok {
		for _, key := range []string{"pagerank", "betweenness", "eigenvector", "hubs", "authorities", "critical_path_score", "core_number", "slack"} {
			if values, ok := stats[key].(map[string]any); ok {
				for id := range values {
					if !p.inScopeID[id] {
						delete(values, id)
					}
				}
			}
		}
		p.filterStringArray(stats, "articulation_points")
	}
	if advanced, ok := output["advanced_insights"].(map[string]any); ok {
		p.projectNestedCandidates(advanced)
	}
}

func (p *hubScopeProjection) projectNestedCandidates(value map[string]any) {
	for key, raw := range value {
		switch nested := raw.(type) {
		case map[string]any:
			p.projectNestedCandidates(nested)
		case []any:
			filtered := nested[:0]
			for _, entry := range nested {
				object, ok := entry.(map[string]any)
				if !ok {
					filtered = append(filtered, entry)
					continue
				}
				id := objectID(object, "id", "issue_id")
				if id == "" || p.inScopeID[id] {
					p.projectNestedCandidates(object)
					filtered = append(filtered, object)
				}
			}
			value[key] = filtered
		}
	}
}

func (p *hubScopeProjection) projectGraph(output map[string]any) {
	adjacency, ok := output["adjacency"].(map[string]any)
	if !ok {
		return
	}
	p.filterObjectArray(adjacency, "nodes", false, "id")
	edges, _ := adjacency["edges"].([]any)
	filtered := edges[:0]
	for _, rawEdge := range edges {
		edge, ok := rawEdge.(map[string]any)
		if !ok {
			continue
		}
		from, _ := edge["from"].(string)
		to, _ := edge["to"].(string)
		if p.inScopeID[from] && p.inScopeID[to] {
			filtered = append(filtered, edge)
		}
	}
	adjacency["edges"] = filtered
	if nodes, ok := adjacency["nodes"].([]any); ok {
		output["nodes"] = len(nodes)
	}
	output["edges"] = len(filtered)
}

func (p *hubScopeProjection) projectSprintList(output map[string]any) {
	sprints, _ := output["sprints"].([]any)
	for _, rawSprint := range sprints {
		if sprint, ok := rawSprint.(map[string]any); ok {
			p.filterStringArray(sprint, "bead_ids")
		}
	}
}

func (p *hubScopeProjection) projectTriage(output map[string]any) {
	target := output
	if triage, ok := output["triage"].(map[string]any); ok {
		target = triage
	}
	for _, key := range []string{"recommendations", "quick_wins", "blockers_to_clear"} {
		p.filterObjectArray(target, key, true, "id")
	}
	if quickRef, ok := target["quick_ref"].(map[string]any); ok {
		p.filterObjectArray(quickRef, "top_picks", true, "id")
		topPicks, _ := quickRef["top_picks"].([]any)
		topID := ""
		if len(topPicks) > 0 {
			topID = objectID(topPicks[0].(map[string]any), "id")
		}
		if commands, ok := target["commands"].(map[string]any); ok {
			if topID == "" {
				commands["claim_top"] = "CI=1 br ready --json  # No top pick available"
				commands["show_top"] = "CI=1 br ready --json  # No top pick available"
			} else {
				commands["claim_top"] = fmt.Sprintf("CI=1 br update %s --status in_progress --json", topID)
				commands["show_top"] = fmt.Sprintf("CI=1 br show %s --json", topID)
			}
		}
	}
	p.projectTriageAlerts(target)
	for _, groupKey := range []string{"recommendations_by_track", "recommendations_by_label"} {
		groups, _ := target[groupKey].([]any)
		projected := groups[:0]
		for _, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				continue
			}
			p.filterObjectArray(group, "recommendations", true, "id")
			if topPick, ok := group["top_pick"].(map[string]any); ok {
				id := objectID(topPick, "id")
				if !p.inScopeID[id] {
					delete(group, "top_pick")
					delete(group, "claim_command")
				} else if refs := p.hiddenOpenBlockers(id); len(refs) > 0 {
					topPick["boundary_refs"] = refs
				}
			}
			if recommendations, _ := group["recommendations"].([]any); len(recommendations) > 0 {
				projected = append(projected, group)
			}
		}
		target[groupKey] = projected
	}
}

func (p *hubScopeProjection) projectTriageAlerts(target map[string]any) {
	alerts, ok := target["alerts"].([]any)
	if !ok {
		return
	}
	projected := alerts[:0]
	for _, rawAlert := range alerts {
		alert, ok := rawAlert.(map[string]any)
		if !ok {
			continue
		}
		if id := objectID(alert, "issue_id"); id != "" {
			if p.inScopeID[id] {
				projected = append(projected, alert)
			}
			continue
		}
		if _, exists := alert["issue_ids"]; exists {
			p.filterStringArray(alert, "issue_ids")
			if ids, _ := alert["issue_ids"].([]any); len(ids) > 0 {
				projected = append(projected, alert)
			}
			continue
		}
		projected = append(projected, alert)
	}
	target["alerts"] = projected
}

func (p *hubScopeProjection) filterObjectArray(parent map[string]any, key string, boundaryRefs bool, idKeys ...string) {
	items, ok := parent[key].([]any)
	if !ok {
		return
	}
	filtered := items[:0]
	seen := make(map[string]bool, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		id := objectID(item, idKeys...)
		if id == "" || !p.inScopeID[id] || seen[id] {
			continue
		}
		seen[id] = true
		if boundaryRefs {
			if refs := p.hiddenOpenBlockers(id); len(refs) > 0 {
				item["boundary_refs"] = refs
			}
		}
		filtered = append(filtered, item)
	}
	parent[key] = filtered
}

func (p *hubScopeProjection) filterStringArray(parent map[string]any, key string) {
	items, ok := parent[key].([]any)
	if !ok {
		return
	}
	filtered := items[:0]
	seen := make(map[string]bool, len(items))
	for _, rawID := range items {
		id, ok := rawID.(string)
		if ok && p.inScopeID[id] && !seen[id] {
			filtered = append(filtered, id)
			seen[id] = true
		}
	}
	parent[key] = filtered
}

func objectID(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if id, ok := item[key].(string); ok {
			return id
		}
	}
	return ""
}

func (p *hubScopeProjection) hiddenOpenBlockers(issueID string) []hubBoundaryReference {
	issue, ok := p.issues[issueID]
	if !ok {
		return nil
	}
	refs := make([]hubBoundaryReference, 0)
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
		refs = append(refs, hubBoundaryReference{
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
