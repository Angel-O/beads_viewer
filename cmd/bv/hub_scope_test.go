package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestHubScopeProjectionCandidateSemanticsAndBoundaryReferences(t *testing.T) {
	contextPrefix := "ctx:"
	first := contextPrefix + "alpha"
	second := contextPrefix + "beta"
	issues := []model.Issue{
		{
			ID:        "visible",
			Title:     "Visible",
			Status:    model.StatusOpen,
			IssueType: model.TypeTask,
			Labels:    []string{first},
			Dependencies: []*model.Dependency{
				{DependsOnID: "hidden-z", Type: model.DepBlocks},
				{DependsOnID: "hidden-a", Type: model.DepBlocks},
			},
		},
		{ID: "hidden-z", Status: model.StatusOpen, IssueType: model.TypeBug, Labels: []string{second}},
		{ID: "hidden-a", Status: model.StatusInProgress, IssueType: model.TypeTask, Labels: []string{second, first + "-other"}},
		{ID: "multi", Status: model.StatusOpen, IssueType: model.TypeEpic, Labels: []string{second, first}},
		{ID: "contextless", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	scope, err := model.NewSelectedContextsHubScope([]string{first, first})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := newHubScopeProjection(scope, issues, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, actionable := range analysis.NewAnalyzer(issues).GetActionableIssues() {
		if actionable.ID == "visible" {
			t.Fatal("out-of-scope open blockers made visible work actionable")
		}
	}
	output := map[string]any{
		"data_hash": analysis.ComputeDataHash(issues),
		"triage": map[string]any{
			"recommendations": []any{
				map[string]any{"id": "hidden-z"},
				map[string]any{"id": "multi"},
				map[string]any{"id": "visible"},
				map[string]any{"id": "multi"},
			},
			"quick_ref": map[string]any{
				"top_picks": []any{map[string]any{"id": "visible"}, map[string]any{"id": "hidden-z"}},
			},
		},
	}
	projection.project("robot-triage", output)

	metadata := output["scope"].(map[string]any)
	if metadata["mode"] != "contexts" || !reflect.DeepEqual(metadata["contexts"], []string{first}) {
		t.Fatalf("scope metadata = %#v", metadata)
	}
	recommendations := output["triage"].(map[string]any)["recommendations"].([]any)
	if got := objectIDs(recommendations); !reflect.DeepEqual(got, []string{"multi", "visible"}) {
		t.Fatalf("projected recommendations = %#v", got)
	}
	visible := recommendations[1].(map[string]any)
	refs := visible["boundary_refs"].([]hubBoundaryReference)
	if got := []string{refs[0].EndpointID, refs[1].EndpointID}; !reflect.DeepEqual(got, []string{"hidden-a", "hidden-z"}) {
		t.Fatalf("boundary order = %#v", got)
	}
	for _, ref := range refs {
		if ref.InScope || ref.RelationType != "blocks" || !sort.StringsAreSorted(ref.Contexts) || !slices.Contains(ref.Contexts, second) {
			t.Fatalf("boundary ref = %#v", ref)
		}
	}
}

func TestHubRobotScopeEndToEndAndLegacyRepoIsolation(t *testing.T) {
	root := t.TempDir()
	beadsDir := filepath.Join(root, ".beads")
	if err := os.MkdirAll(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := "ctx:" + "alpha"
	second := "ctx:" + "beta"
	issues := []model.Issue{
		{ID: "alpha-ready", Title: "Ready", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{first}},
		{ID: "alpha-blocked", Title: "Blocked", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{first}, Dependencies: []*model.Dependency{{IssueID: "alpha-blocked", DependsOnID: "beta-blocker", Type: model.DepBlocks}}},
		{ID: "beta-blocker", Title: "Hidden blocker", Status: model.StatusOpen, IssueType: model.TypeBug, Labels: []string{second}},
		{ID: "shared-ready", Title: "Shared", Status: model.StatusOpen, IssueType: model.TypeEpic, Labels: []string{second, first}},
		{ID: "unowned-ready", Title: "Unowned", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	var lines bytes.Buffer
	encoder := json.NewEncoder(&lines)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), lines.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "hub.json")
	config := hub.Config{
		Version: hub.ConfigVersion,
		Store:   beadsDir,
		Ledger:  filepath.Join(root, "ledger.jsonl"),
		Repositories: map[string]hub.Repository{
			first:  {Path: filepath.Join(root, "alpha")},
			second: {Path: filepath.Join(root, "beta")},
		},
	}
	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}

	executable := buildTestBinary(t)
	run := func(scope model.HubScope, extra ...string) map[string]any {
		t.Helper()
		scopeData, err := json.Marshal(scope)
		if err != nil {
			t.Fatal(err)
		}
		arguments := []string{"--history-mode", "external", "--hub-config", configPath, "--robot-plan", "--format", "json"}
		arguments = append(arguments, extra...)
		command := exec.Command(executable, arguments...)
		command.Dir = root
		command.Env = isolatedRobotTestEnv(root, string(scopeData))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("bv %v: %v\n%s", arguments, err, output)
		}
		var payload map[string]any
		if err := json.Unmarshal(output, &payload); err != nil {
			t.Fatalf("decode output: %v\n%s", err, output)
		}
		return payload
	}

	selectedScope, err := model.NewSelectedContextsHubScope([]string{first})
	if err != nil {
		t.Fatal(err)
	}
	selected := run(selectedScope)
	contextless := run(model.NewContextlessHubScope())
	allItems := run(model.NewAllItemsHubScope())
	if selected["data_hash"] != contextless["data_hash"] || selected["data_hash"] != allItems["data_hash"] {
		t.Fatalf("canonical data hash varied by scope: selected=%v contextless=%v all=%v", selected["data_hash"], contextless["data_hash"], allItems["data_hash"])
	}
	if got := planItemIDs(selected); !reflect.DeepEqual(got, []string{"alpha-ready", "shared-ready"}) {
		t.Fatalf("selected plan items = %#v", got)
	}
	if got := planItemIDs(contextless); !reflect.DeepEqual(got, []string{"unowned-ready"}) {
		t.Fatalf("contextless plan items = %#v", got)
	}
	if got := planItemIDs(allItems); !slices.Contains(got, "beta-blocker") || !slices.Contains(got, "unowned-ready") {
		t.Fatalf("all-items plan omitted loaded issues: %#v", got)
	}

	localCommand := exec.Command(executable, "--history-mode", "off", "--robot-plan", "--repo", "alpha", "--format", "json")
	localCommand.Dir = root
	localCommand.Env = isolatedRobotTestEnv(root, "")
	localData, err := localCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy --repo: %v\n%s", err, localData)
	}
	var local map[string]any
	if err := json.Unmarshal(localData, &local); err != nil {
		t.Fatal(err)
	}
	if _, exists := local["scope"]; exists {
		t.Fatalf("legacy local output acquired Hub scope: %#v", local)
	}
	wantHash := analysis.ComputeDataHash(filterByRepo(issues, "alpha"))
	if local["data_hash"] != wantHash {
		t.Fatalf("legacy --repo data hash = %v, want %s", local["data_hash"], wantHash)
	}
}

func isolatedRobotTestEnv(home, scope string) []string {
	blocked := []string{"HOME=", "BEADS_DIR=", "BEADS_DB=", "BD_DB=", "BV_WBV_HUB_SCOPE=", "BV_NO_GITIGNORE=", "BV_NO_CACHE="}
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		skip := false
		for _, prefix := range blocked {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			environment = append(environment, entry)
		}
	}
	return append(environment, "HOME="+home, "BV_WBV_HUB_SCOPE="+scope, "BV_NO_GITIGNORE=1", "BV_NO_CACHE=1")
}

func planItemIDs(output map[string]any) []string {
	plan := output["plan"].(map[string]any)
	tracks, _ := plan["tracks"].([]any)
	var ids []string
	for _, rawTrack := range tracks {
		track := rawTrack.(map[string]any)
		ids = append(ids, objectIDs(track["items"].([]any))...)
	}
	sort.Strings(ids)
	return ids
}

func TestHubScopeProjectionIsHubOnlyAndSchemaDocumented(t *testing.T) {
	var local bytes.Buffer
	encoder := newJSONRobotEncoder(&local)
	if err := encoder.Encode(map[string]any{"data_hash": "canonical", "items": []string{"unchanged"}}); err != nil {
		t.Fatal(err)
	}
	var localOutput map[string]any
	if err := json.Unmarshal(local.Bytes(), &localOutput); err != nil {
		t.Fatal(err)
	}
	if _, exists := localOutput["scope"]; exists {
		t.Fatalf("local output acquired Hub scope metadata: %#v", localOutput)
	}

	schemas := generateRobotSchemas()
	for _, command := range []string{
		"robot-plan", "robot-priority", "robot-insights", "robot-graph",
		"robot-label-health", "robot-label-flow", "robot-label-attention",
		"robot-blocker-chain", "robot-sprint-list", "robot-sprint-show",
		"robot-forecast", "robot-capacity", "robot-triage",
	} {
		properties := schemas.Commands[command]["properties"].(map[string]interface{})
		if properties["scope"] == nil {
			t.Fatalf("%s schema missing optional Hub scope metadata", command)
		}
		if robotCommandDocs()[command].HubScope == "" {
			t.Fatalf("%s registry docs missing Hub scope contract", command)
		}
	}
}

func TestWrapperHubScopeTransportIsNarrowAndStrict(t *testing.T) {
	all := `{"mode":"all_items","contexts":[]}`
	if _, err := parseHubRobotScope(all, "", false, true); err == nil {
		t.Fatal("local mode accepted wrapper Hub scope transport")
	}
	if _, err := parseHubRobotScope(all+` {}`, "", true, true); err == nil {
		t.Fatal("wrapper Hub scope accepted trailing JSON")
	}
	scope, err := parseHubRobotScope(all, "", true, true)
	if err != nil || scope == nil || scope.Mode != model.HubScopeAllItems {
		t.Fatalf("valid wrapper scope = %#v, err = %v", scope, err)
	}
}

func TestHubScopeProjectionVariantsAndCanonicalHash(t *testing.T) {
	contextID := "ctx:" + "selected"
	issues := []model.Issue{
		{ID: "selected", Labels: []string{contextID}},
		{ID: "contextless"},
		{ID: "unregistered", Labels: []string{"ctx:" + "unknown"}},
	}
	canonicalHash := analysis.ComputeDataHash(issues)
	tests := []struct {
		name  string
		scope model.HubScope
		want  []string
	}{
		{name: "all items", scope: model.NewAllItemsHubScope(), want: []string{"selected", "contextless", "unregistered"}},
		{name: "contextless", scope: model.NewContextlessHubScope(), want: []string{"contextless"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection, err := newHubScopeProjection(test.scope, issues, "")
			if err != nil {
				t.Fatal(err)
			}
			output := map[string]any{
				"data_hash": canonicalHash,
				"forecasts": []any{
					map[string]any{"issue_id": "selected"},
					map[string]any{"issue_id": "contextless"},
					map[string]any{"issue_id": "unregistered"},
				},
			}
			projection.project("robot-forecast", output)
			if output["data_hash"] != canonicalHash {
				t.Fatalf("data hash changed: %v", output["data_hash"])
			}
			if got := objectIDs(output["forecasts"].([]any)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("forecast IDs = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestHubScopeProjectionPlanGraphAndGlobalAggregates(t *testing.T) {
	contextID := "ctx:" + "selected"
	issues := []model.Issue{
		{ID: "visible", Labels: []string{contextID}},
		{ID: "hidden", Labels: []string{"ctx:" + "other"}},
	}
	scope, err := model.NewSelectedContextsHubScope([]string{contextID})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := newHubScopeProjection(scope, issues, "")
	if err != nil {
		t.Fatal(err)
	}

	plan := map[string]any{
		"plan": map[string]any{
			"total_actionable": float64(2),
			"tracks": []any{
				map[string]any{"track_id": "one", "items": []any{map[string]any{"id": "hidden"}}},
				map[string]any{"track_id": "two", "items": []any{map[string]any{"id": "visible"}}},
			},
		},
	}
	projection.project("robot-plan", plan)
	projectedPlan := plan["plan"].(map[string]any)
	if projectedPlan["total_actionable"] != float64(2) {
		t.Fatalf("global total changed: %#v", projectedPlan)
	}
	tracks := projectedPlan["tracks"].([]any)
	if len(tracks) != 1 || tracks[0].(map[string]any)["track_id"] != "two" {
		t.Fatalf("tracks = %#v", tracks)
	}

	graph := map[string]any{
		"nodes": float64(2),
		"edges": float64(2),
		"adjacency": map[string]any{
			"nodes": []any{map[string]any{"id": "hidden"}, map[string]any{"id": "visible"}},
			"edges": []any{
				map[string]any{"from": "visible", "to": "hidden"},
				map[string]any{"from": "visible", "to": "visible"},
			},
		},
	}
	projection.project("robot-graph", graph)
	adjacency := graph["adjacency"].(map[string]any)
	if got := objectIDs(adjacency["nodes"].([]any)); !reflect.DeepEqual(got, []string{"visible"}) {
		t.Fatalf("nodes = %#v", got)
	}
	if graph["nodes"] != 1 || graph["edges"] != 1 {
		t.Fatalf("graph counts = nodes:%v edges:%v", graph["nodes"], graph["edges"])
	}
}

func objectIDs(items []any) []string {
	ids := make([]string, 0, len(items))
	for _, raw := range items {
		item := raw.(map[string]any)
		ids = append(ids, objectID(item, "id", "issue_id"))
	}
	return ids
}
