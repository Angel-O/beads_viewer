package main

import (
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/export"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// RobotResult is the typed value handed to an optional result adapter before
// it is encoded. It deliberately has no JSON or Hub concerns.
type RobotResult interface {
	robotResult()
}

// RobotResultDecorator is an additive presentation hook. The command handler
// still constructs its normal typed contract; an adapter may amend that value
// before the configured encoder sees it.
type RobotResultDecorator func(command string, result RobotResult) error

type robotScopeMetadata struct {
	Mode               string   `json:"mode"`
	Contexts           []string `json:"contexts"`
	IncludeContextless bool     `json:"include_contextless"`
}

type robotBoundaryReference struct {
	RelationType string          `json:"relation_type"`
	EndpointID   string          `json:"endpoint_id"`
	IssueType    model.IssueType `json:"issue_type"`
	Status       model.Status    `json:"status"`
	Contexts     []string        `json:"contexts"`
	InScope      bool            `json:"in_scope"`
}

type robotPlanItem struct {
	analysis.PlanItem
	BoundaryRefs []robotBoundaryReference `json:"boundary_refs,omitempty"`
}

type robotExecutionTrack struct {
	TrackID string          `json:"track_id"`
	Items   []robotPlanItem `json:"items"`
	Reason  string          `json:"reason"`
}

type robotExecutionPlan struct {
	Tracks          []robotExecutionTrack `json:"tracks"`
	TotalActionable int                   `json:"total_actionable"`
	TotalBlocked    int                   `json:"total_blocked"`
	Summary         analysis.PlanSummary  `json:"summary"`
}

type robotPlanOutput struct {
	GeneratedAt    string                  `json:"generated_at"`
	DataHash       string                  `json:"data_hash"`
	AsOf           string                  `json:"as_of,omitempty"`
	AsOfCommit     string                  `json:"as_of_commit,omitempty"`
	AnalysisConfig analysis.AnalysisConfig `json:"analysis_config"`
	Status         analysis.MetricStatus   `json:"status"`
	LabelScope     string                  `json:"label_scope,omitempty"`
	LabelContext   *analysis.LabelHealth   `json:"label_context,omitempty"`
	Plan           robotExecutionPlan      `json:"plan"`
	UsageHints     []string                `json:"usage_hints"`
	Scope          *robotScopeMetadata     `json:"scope,omitempty"`
}

func (*robotPlanOutput) robotResult() {}

type robotPriorityRecommendation struct {
	analysis.EnhancedPriorityRecommendation
	BoundaryRefs []robotBoundaryReference `json:"boundary_refs,omitempty"`
}

type robotPriorityFilters struct {
	MinConfidence float64 `json:"min_confidence,omitempty"`
	MaxResults    int     `json:"max_results"`
	ByLabel       string  `json:"by_label,omitempty"`
	ByAssignee    string  `json:"by_assignee,omitempty"`
}

type robotPrioritySummary struct {
	TotalIssues     int `json:"total_issues"`
	Recommendations int `json:"recommendations"`
	HighConfidence  int `json:"high_confidence"`
}

type robotPriorityOutput struct {
	GeneratedAt       string                        `json:"generated_at"`
	DataHash          string                        `json:"data_hash"`
	AsOf              string                        `json:"as_of,omitempty"`
	AsOfCommit        string                        `json:"as_of_commit,omitempty"`
	AnalysisConfig    analysis.AnalysisConfig       `json:"analysis_config"`
	Status            analysis.MetricStatus         `json:"status"`
	LabelScope        string                        `json:"label_scope,omitempty"`
	LabelContext      *analysis.LabelHealth         `json:"label_context,omitempty"`
	Recommendations   []robotPriorityRecommendation `json:"recommendations"`
	FieldDescriptions map[string]string             `json:"field_descriptions"`
	Filters           robotPriorityFilters          `json:"filters"`
	Summary           robotPrioritySummary          `json:"summary"`
	Usage             []string                      `json:"usage_hints"`
	Scope             *robotScopeMetadata           `json:"scope,omitempty"`
}

func (*robotPriorityOutput) robotResult() {}

type robotFullStats struct {
	PageRank          map[string]float64 `json:"pagerank"`
	Betweenness       map[string]float64 `json:"betweenness"`
	Eigenvector       map[string]float64 `json:"eigenvector"`
	Hubs              map[string]float64 `json:"hubs"`
	Authorities       map[string]float64 `json:"authorities"`
	CriticalPathScore map[string]float64 `json:"critical_path_score"`
	CoreNumber        map[string]int     `json:"core_number"`
	Slack             map[string]float64 `json:"slack"`
	Articulation      []string           `json:"articulation_points"`
}

type robotInsightsOutput struct {
	GeneratedAt    string                  `json:"generated_at"`
	DataHash       string                  `json:"data_hash"`
	LoadStats      *RobotLoadStats         `json:"load_stats,omitempty"`
	AsOf           string                  `json:"as_of,omitempty"`
	AsOfCommit     string                  `json:"as_of_commit,omitempty"`
	AnalysisConfig analysis.AnalysisConfig `json:"analysis_config"`
	Status         analysis.MetricStatus   `json:"status"`
	LabelScope     string                  `json:"label_scope,omitempty"`
	LabelContext   *analysis.LabelHealth   `json:"label_context,omitempty"`
	analysis.Insights
	FullStats        robotFullStats             `json:"full_stats"`
	TopWhatIfs       []analysis.WhatIfEntry     `json:"top_what_ifs,omitempty"`
	AdvancedInsights *analysis.AdvancedInsights `json:"advanced_insights,omitempty"`
	UsageHints       []string                   `json:"usage_hints"`
	Scope            *robotScopeMetadata        `json:"scope,omitempty"`
}

func (*robotInsightsOutput) robotResult() {}

type robotGraphOutput struct {
	*export.GraphExportResult
	Scope  *robotScopeMetadata `json:"scope,omitempty"`
	issues []model.Issue       `json:"-"`
	stats  *analysis.GraphStats
	config export.GraphExportConfig
}

func (*robotGraphOutput) robotResult() {}

type robotForecastSummary struct {
	TotalMinutes  int       `json:"total_minutes"`
	TotalDays     float64   `json:"total_days"`
	AvgConfidence float64   `json:"avg_confidence"`
	EarliestETA   time.Time `json:"earliest_eta"`
	LatestETA     time.Time `json:"latest_eta"`
}

type robotForecastOutput struct {
	RobotEnvelope
	Agents        int                    `json:"agents"`
	Filters       map[string]string      `json:"filters,omitempty"`
	ForecastCount int                    `json:"forecast_count"`
	Forecasts     []analysis.ETAEstimate `json:"forecasts"`
	Summary       *robotForecastSummary  `json:"summary,omitempty"`
	Scope         *robotScopeMetadata    `json:"scope,omitempty"`
}

func (*robotForecastOutput) robotResult() {}

type robotLabelHealthOutput struct {
	GeneratedAt    string                       `json:"generated_at"`
	DataHash       string                       `json:"data_hash"`
	AnalysisConfig analysis.LabelHealthConfig   `json:"analysis_config"`
	Results        analysis.LabelAnalysisResult `json:"results"`
	UsageHints     []string                     `json:"usage_hints"`
	Scope          *robotScopeMetadata          `json:"scope,omitempty"`
}

func (*robotLabelHealthOutput) robotResult() {}

type robotLabelFlowOutput struct {
	GeneratedAt string                     `json:"generated_at"`
	DataHash    string                     `json:"data_hash"`
	LoadStats   *RobotLoadStats            `json:"load_stats,omitempty"`
	Flow        analysis.CrossLabelFlow    `json:"flow"`
	Config      analysis.LabelHealthConfig `json:"analysis_config"`
	UsageHints  []string                   `json:"usage_hints"`
	Scope       *robotScopeMetadata        `json:"scope,omitempty"`
}

func (*robotLabelFlowOutput) robotResult() {}

type robotAttentionLabel struct {
	Rank            int     `json:"rank"`
	Label           string  `json:"label"`
	AttentionScore  float64 `json:"attention_score"`
	NormalizedScore float64 `json:"normalized_score"`
	Reason          string  `json:"reason"`
	OpenCount       int     `json:"open_count"`
	BlockedCount    int     `json:"blocked_count"`
	StaleCount      int     `json:"stale_count"`
	PageRankSum     float64 `json:"pagerank_sum"`
	VelocityFactor  float64 `json:"velocity_factor"`
}

type robotAttentionOutput struct {
	GeneratedAt string                `json:"generated_at"`
	DataHash    string                `json:"data_hash"`
	LoadStats   *RobotLoadStats       `json:"load_stats,omitempty"`
	Limit       int                   `json:"limit"`
	TotalLabels int                   `json:"total_labels"`
	Labels      []robotAttentionLabel `json:"labels"`
	UsageHints  []string              `json:"usage_hints"`
	Scope       *robotScopeMetadata   `json:"scope,omitempty"`
}

func (*robotAttentionOutput) robotResult() {}

type robotBlockerChainOutput struct {
	RobotEnvelope
	Result *analysis.BlockerChainResult `json:"result"`
	Scope  *robotScopeMetadata          `json:"scope,omitempty"`
}

func (*robotBlockerChainOutput) robotResult() {}

type robotSprintListOutput struct {
	RobotEnvelope
	SprintCount int                 `json:"sprint_count"`
	Sprints     []model.Sprint      `json:"sprints"`
	Scope       *robotScopeMetadata `json:"scope,omitempty"`
}

func (*robotSprintListOutput) robotResult() {}

type robotSprintShowOutput struct {
	RobotEnvelope
	Sprint *model.Sprint       `json:"sprint"`
	Scope  *robotScopeMetadata `json:"scope,omitempty"`
}

func (*robotSprintShowOutput) robotResult() {}

type robotCapacityBottleneck struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	BlocksCount int      `json:"blocks_count"`
	Blocks      []string `json:"blocks,omitempty"`
}

type robotCapacityOutput struct {
	RobotEnvelope
	Agents            int                       `json:"agents"`
	Label             string                    `json:"label,omitempty"`
	OpenIssueCount    int                       `json:"open_issue_count"`
	TotalMinutes      int                       `json:"total_minutes"`
	TotalDays         float64                   `json:"total_days"`
	SerialMinutes     int                       `json:"serial_minutes"`
	ParallelMinutes   int                       `json:"parallel_minutes"`
	ParallelizablePct float64                   `json:"parallelizable_pct"`
	EstimatedDays     float64                   `json:"estimated_days"`
	CriticalPathLen   int                       `json:"critical_path_length"`
	CriticalPath      []string                  `json:"critical_path,omitempty"`
	ActionableCount   int                       `json:"actionable_count"`
	Actionable        []string                  `json:"actionable,omitempty"`
	Bottlenecks       []robotCapacityBottleneck `json:"bottlenecks,omitempty"`
	Scope             *robotScopeMetadata       `json:"scope,omitempty"`
}

func (*robotCapacityOutput) robotResult() {}

type robotTriageOutput struct {
	GeneratedAt string                 `json:"generated_at"`
	DataHash    string                 `json:"data_hash"`
	LoadStats   *RobotLoadStats        `json:"load_stats,omitempty"`
	AsOf        string                 `json:"as_of,omitempty"`
	AsOfCommit  string                 `json:"as_of_commit,omitempty"`
	Triage      robotTriageResult      `json:"triage"`
	Feedback    *analysis.FeedbackJSON `json:"feedback,omitempty"`
	UsageHints  []string               `json:"usage_hints"`
	Scope       *robotScopeMetadata    `json:"scope,omitempty"`
}

func (*robotTriageOutput) robotResult() {}

type robotTriageTopPick struct {
	analysis.TopPick
	BoundaryRefs []robotBoundaryReference `json:"boundary_refs,omitempty"`
}

type robotTriageRecommendation struct {
	analysis.Recommendation
	BoundaryRefs []robotBoundaryReference `json:"boundary_refs,omitempty"`
}

type robotTriageQuickWin struct {
	analysis.QuickWin
	BoundaryRefs []robotBoundaryReference `json:"boundary_refs,omitempty"`
}

type robotTriageBlocker struct {
	analysis.BlockerItem
	BoundaryRefs []robotBoundaryReference `json:"boundary_refs,omitempty"`
}

type robotTriageQuickRef struct {
	analysis.QuickRef
	TopPicks []robotTriageTopPick `json:"top_picks"`
}

type robotTriageTrackGroup struct {
	TrackID         string                      `json:"track_id"`
	Reason          string                      `json:"reason"`
	Recommendations []robotTriageRecommendation `json:"recommendations"`
	TopPick         *robotTriageTopPick         `json:"top_pick,omitempty"`
	ClaimCommand    string                      `json:"claim_command,omitempty"`
	TotalUnblocks   int                         `json:"total_unblocks"`
}

type robotTriageLabelGroup struct {
	Label           string                      `json:"label"`
	Recommendations []robotTriageRecommendation `json:"recommendations"`
	TopPick         *robotTriageTopPick         `json:"top_pick,omitempty"`
	ClaimCommand    string                      `json:"claim_command,omitempty"`
	TotalUnblocks   int                         `json:"total_unblocks"`
}

type robotTriageResult struct {
	analysis.TriageResult
	QuickRef               robotTriageQuickRef         `json:"quick_ref"`
	Recommendations        []robotTriageRecommendation `json:"recommendations"`
	QuickWins              []robotTriageQuickWin       `json:"quick_wins"`
	BlockersToClear        []robotTriageBlocker        `json:"blockers_to_clear"`
	RecommendationsByTrack []robotTriageTrackGroup     `json:"recommendations_by_track,omitempty"`
	RecommendationsByLabel []robotTriageLabelGroup     `json:"recommendations_by_label,omitempty"`
}

func robotTriageResultFromAnalysis(result analysis.TriageResult) robotTriageResult {
	converted := robotTriageResult{TriageResult: result}
	converted.QuickRef = robotTriageQuickRefFromAnalysis(result.QuickRef)
	for _, recommendation := range result.Recommendations {
		converted.Recommendations = append(converted.Recommendations, robotTriageRecommendation{Recommendation: recommendation})
	}
	for _, quickWin := range result.QuickWins {
		converted.QuickWins = append(converted.QuickWins, robotTriageQuickWin{QuickWin: quickWin})
	}
	for _, blocker := range result.BlockersToClear {
		converted.BlockersToClear = append(converted.BlockersToClear, robotTriageBlocker{BlockerItem: blocker})
	}
	for _, group := range result.RecommendationsByTrack {
		converted.RecommendationsByTrack = append(converted.RecommendationsByTrack, robotTriageTrackGroup{
			TrackID: group.TrackID, Reason: group.Reason, ClaimCommand: group.ClaimCommand, TotalUnblocks: group.TotalUnblocks,
			Recommendations: robotTriageRecommendations(group.Recommendations), TopPick: robotTriageTopPickPointer(group.TopPick),
		})
	}
	for _, group := range result.RecommendationsByLabel {
		converted.RecommendationsByLabel = append(converted.RecommendationsByLabel, robotTriageLabelGroup{
			Label: group.Label, ClaimCommand: group.ClaimCommand, TotalUnblocks: group.TotalUnblocks,
			Recommendations: robotTriageRecommendations(group.Recommendations), TopPick: robotTriageTopPickPointer(group.TopPick),
		})
	}
	return converted
}

func robotTriageQuickRefFromAnalysis(ref analysis.QuickRef) robotTriageQuickRef {
	converted := robotTriageQuickRef{QuickRef: ref}
	if ref.TopPicks != nil {
		converted.TopPicks = make([]robotTriageTopPick, 0, len(ref.TopPicks))
	}
	for _, pick := range ref.TopPicks {
		converted.TopPicks = append(converted.TopPicks, robotTriageTopPick{TopPick: pick})
	}
	return converted
}

func robotTriageQuickWins(items []analysis.QuickWin) []robotTriageQuickWin {
	result := make([]robotTriageQuickWin, 0, len(items))
	for _, item := range items {
		result = append(result, robotTriageQuickWin{QuickWin: item})
	}
	return result
}

func robotTriageBlockers(items []analysis.BlockerItem) []robotTriageBlocker {
	result := make([]robotTriageBlocker, 0, len(items))
	for _, item := range items {
		result = append(result, robotTriageBlocker{BlockerItem: item})
	}
	return result
}

func robotTriageRecommendations(items []analysis.Recommendation) []robotTriageRecommendation {
	result := make([]robotTriageRecommendation, 0, len(items))
	for _, item := range items {
		result = append(result, robotTriageRecommendation{Recommendation: item})
	}
	return result
}

func robotTriageTopPickPointer(pick *analysis.TopPick) *robotTriageTopPick {
	if pick == nil {
		return nil
	}
	converted := &robotTriageTopPick{TopPick: *pick}
	return converted
}

func robotExecutionPlanFromAnalysis(plan analysis.ExecutionPlan) robotExecutionPlan {
	result := robotExecutionPlan{
		TotalActionable: plan.TotalActionable,
		TotalBlocked:    plan.TotalBlocked,
		Summary:         plan.Summary,
		Tracks:          make([]robotExecutionTrack, 0, len(plan.Tracks)),
	}
	for _, track := range plan.Tracks {
		converted := robotExecutionTrack{TrackID: track.TrackID, Reason: track.Reason, Items: make([]robotPlanItem, 0, len(track.Items))}
		for _, item := range track.Items {
			converted.Items = append(converted.Items, robotPlanItem{PlanItem: item})
		}
		result.Tracks = append(result.Tracks, converted)
	}
	return result
}
