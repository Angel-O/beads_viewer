package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestHubRobotInsightsFiltersCandidatesBeforeTopKCap(t *testing.T) {
	selectedContext := "ctx:alpha"
	otherContext := "ctx:beta"
	issues := []model.Issue{{
		ID:        "visible",
		Title:     "Visible candidate",
		Status:    model.StatusOpen,
		IssueType: model.TypeTask,
		Labels:    []string{selectedContext},
	}}
	issues = append(issues, model.Issue{
		ID:        "hidden-dependent",
		Status:    model.StatusOpen,
		IssueType: model.TypeTask,
		Labels:    []string{otherContext},
		Dependencies: []*model.Dependency{{
			IssueID:     "hidden-dependent",
			DependsOnID: "visible",
			Type:        model.DepBlocks,
		}},
	})
	for hubIndex := 0; hubIndex < 6; hubIndex++ {
		hubID := "hidden-hub-" + string(rune('a'+hubIndex))
		issues = append(issues, model.Issue{
			ID:        hubID,
			Status:    model.StatusOpen,
			IssueType: model.TypeTask,
			Labels:    []string{otherContext},
		})
		for dependentIndex := 0; dependentIndex < 2; dependentIndex++ {
			dependentID := hubID + "-dependent-" + string(rune('a'+dependentIndex))
			issues = append(issues, model.Issue{
				ID:        dependentID,
				Status:    model.StatusOpen,
				IssueType: model.TypeTask,
				Labels:    []string{otherContext},
				Dependencies: []*model.Dependency{{
					IssueID:     dependentID,
					DependsOnID: hubID,
					Type:        model.DepBlocks,
				}},
			})
		}
	}

	scope, err := model.NewSelectedContextsHubScope([]string{selectedContext})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := newHubScopeProjection(scope, issues, "")
	if err != nil {
		t.Fatal(err)
	}

	var encoded bytes.Buffer
	ctx := RobotContext{
		Issues:                issues,
		DataHash:              analysis.ComputeDataHash(issues),
		DataHashMatchesIssues: true,
		Encoder:               newJSONRobotEncoder(&encoded),
		Stdout:                &encoded,
		HubProjection:         projection,
	}
	if err := handleRobotInsights(ctx, phaseThreeRobotHandlerConfig{}); err != nil {
		t.Fatal(err)
	}

	var output struct {
		AdvancedInsights struct {
			TopKSet struct {
				Items []struct {
					ID           string   `json:"id"`
					MarginalGain int      `json:"marginal_gain"`
					Unblocks     []string `json:"unblocks"`
				} `json:"items"`
			} `json:"topk_set"`
		} `json:"advanced_insights"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatalf("decode robot-insights output: %v\n%s", err, encoded.Bytes())
	}
	items := output.AdvancedInsights.TopKSet.Items
	if len(items) != 1 || items[0].ID != "visible" {
		t.Fatalf("Hub-scoped top-k candidates = %#v, want only visible", output.AdvancedInsights.TopKSet.Items)
	}
	if items[0].MarginalGain != 1 || !slices.Equal(items[0].Unblocks, []string{"hidden-dependent"}) {
		t.Fatalf("visible top-k full-graph impact = gain:%d unblocks:%v, want gain:1 unblocks:[hidden-dependent]", items[0].MarginalGain, items[0].Unblocks)
	}
}
