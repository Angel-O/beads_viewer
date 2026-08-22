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
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/export"
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
	run := func(scope model.HubScope, arguments ...string) map[string]any {
		t.Helper()
		scopeData, err := json.Marshal(scope)
		if err != nil {
			t.Fatal(err)
		}
		commandArguments := []string{"--history-mode", "external", "--hub-config", configPath}
		commandArguments = append(commandArguments, arguments...)
		commandArguments = append(commandArguments, "--format", "json")
		command := exec.Command(executable, commandArguments...)
		command.Dir = root
		command.Env = isolatedRobotTestEnv(root, string(scopeData))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("bv %v: %v\n%s", commandArguments, err, output)
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
	selected := run(selectedScope, "--robot-plan")
	contextless := run(model.NewContextlessHubScope(), "--robot-plan")
	allItems := run(model.NewAllItemsHubScope(), "--robot-plan")
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

	priority := run(selectedScope, "--robot-priority")
	recommendations := priority["recommendations"].([]any)
	prioritySummary := priority["summary"].(map[string]any)
	if int(prioritySummary["recommendations"].(float64)) != len(recommendations) {
		t.Fatalf("priority summary = %#v, recommendations = %d", prioritySummary, len(recommendations))
	}
	highConfidence := 0
	for _, raw := range recommendations {
		if raw.(map[string]any)["confidence"].(float64) >= 0.7 {
			highConfidence++
		}
	}
	if int(prioritySummary["high_confidence"].(float64)) != highConfidence {
		t.Fatalf("priority high confidence = %#v, want %d", prioritySummary, highConfidence)
	}

	forecast := run(selectedScope, "--robot-forecast", "all")
	forecasts := forecast["forecasts"].([]any)
	if int(forecast["forecast_count"].(float64)) != len(forecasts) {
		t.Fatalf("forecast count = %v, forecasts = %d", forecast["forecast_count"], len(forecasts))
	}

	labelOutput := run(selectedScope, "--robot-insights", "--label", first)
	labelContext, ok := labelOutput["label_context"].(map[string]any)
	if !ok || labelContext["label"] != first || int(labelContext["issue_count"].(float64)) != 3 {
		t.Fatalf("canonical label context = %#v", labelOutput["label_context"])
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
	mixed, err := model.NewSelectedContextsAndContextlessHubScope([]string{contextID})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		scope model.HubScope
		want  []string
	}{
		{name: "all items", scope: model.NewAllItemsHubScope(), want: []string{"selected", "contextless", "unregistered"}},
		{name: "contextless", scope: model.NewContextlessHubScope(), want: []string{"contextless"}},
		{name: "selected and contextless", scope: mixed, want: []string{"selected", "contextless"}},
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
			metadata := output["scope"].(map[string]any)
			if metadata["include_contextless"] != test.scope.IncludeContextless {
				t.Fatalf("scope metadata = %#v", metadata)
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

	labelHealth := map[string]any{
		"results": map[string]any{
			"labels": []any{map[string]any{
				"label":       "overall-health",
				"issue_count": float64(2),
				"issues":      []any{"hidden", "visible"},
			}},
			"summaries": []any{map[string]any{
				"label":       "overall-health",
				"issue_count": float64(2),
				"top_issue":   "hidden",
			}},
			"cross_label_flow": map[string]any{
				"total_cross_label_deps": float64(2),
				"dependencies": []any{map[string]any{
					"issue_count": float64(2),
					"issue_ids":   []any{"hidden", "visible"},
				}},
			},
		},
	}
	projection.project("robot-label-health", labelHealth)
	results := labelHealth["results"].(map[string]any)
	health := results["labels"].([]any)[0].(map[string]any)
	if health["issue_count"] != float64(2) || !reflect.DeepEqual(health["issues"], []any{"visible"}) {
		t.Fatalf("label health projection changed aggregate or leaked candidate: %#v", health)
	}
	summary := results["summaries"].([]any)[0].(map[string]any)
	if summary["issue_count"] != float64(2) || summary["top_issue"] != "visible" {
		t.Fatalf("label summary projection = %#v", summary)
	}
	flow := results["cross_label_flow"].(map[string]any)
	dependency := flow["dependencies"].([]any)[0].(map[string]any)
	if flow["total_cross_label_deps"] != float64(2) || dependency["issue_count"] != float64(2) || !reflect.DeepEqual(dependency["issue_ids"], []any{"visible"}) {
		t.Fatalf("label flow projection changed aggregate or leaked candidate: %#v", flow)
	}
}

func TestHubGraphTraversalCrossesHiddenIntermediary(t *testing.T) {
	selected := "ctx:" + "selected"
	hidden := "ctx:" + "hidden"
	issues := []model.Issue{
		{ID: "visible-a", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{selected}, Dependencies: []*model.Dependency{{DependsOnID: "hidden", Type: model.DepBlocks}}},
		{ID: "hidden", Status: model.StatusInProgress, IssueType: model.TypeBug, Labels: []string{hidden}, Dependencies: []*model.Dependency{{DependsOnID: "visible-b", Type: model.DepRelated}}},
		{ID: "visible-b", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{selected}},
	}
	scope, err := model.NewSelectedContextsHubScope([]string{selected})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := newHubScopeProjection(scope, issues, "")
	if err != nil {
		t.Fatal(err)
	}
	stats := analysis.NewAnalyzer(issues).Analyze()
	result, err := projection.exportGraph(issues, &stats, export.GraphExportConfig{
		Format: export.GraphFormatJSON,
		Root:   "visible-a",
		Depth:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{result.Adjacency.Nodes[0].ID, result.Adjacency.Nodes[1].ID}; !reflect.DeepEqual(got, []string{"visible-a", "visible-b"}) {
		t.Fatalf("visible nodes = %#v", got)
	}
	if len(result.Adjacency.Edges) != 0 {
		t.Fatalf("synthetic or hidden edge leaked: %#v", result.Adjacency.Edges)
	}
	refs := append([]export.GraphBoundaryReference(nil), result.Adjacency.Nodes[0].BoundaryRefs...)
	refs = append(refs, result.Adjacency.Nodes[1].BoundaryRefs...)
	if len(refs) != 2 {
		t.Fatalf("boundary refs = %#v", refs)
	}
	if refs[0].From != "visible-a" || refs[0].To != "hidden" || refs[0].EndpointID != "hidden" {
		t.Fatalf("first boundary ref = %#v", refs[0])
	}
	if refs[1].From != "hidden" || refs[1].To != "visible-b" || refs[1].RelationType != "related" {
		t.Fatalf("second boundary ref = %#v", refs[1])
	}

	dot, err := projection.exportGraph(issues, &stats, export.GraphExportConfig{
		Format: export.GraphFormatDOT,
		Root:   "visible-a",
		Depth:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dot.Graph, `"hidden"`) || !strings.Contains(dot.Graph, `"visible-b"`) {
		t.Fatalf("DOT graph did not project canonical traversal correctly:\n%s", dot.Graph)
	}
	if len(dot.Adjacency.Nodes) != 2 || len(dot.Adjacency.Nodes[0].BoundaryRefs)+len(dot.Adjacency.Nodes[1].BoundaryRefs) != 2 {
		t.Fatalf("DOT boundary evidence = %#v", dot.Adjacency)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var live map[string]any
	if err := json.Unmarshal(data, &live); err != nil {
		t.Fatal(err)
	}
	liveNode := live["adjacency"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
	liveBoundary := liveNode["boundary_refs"].([]any)[0].(map[string]any)
	graphSchema := generateRobotSchemas().Commands["robot-graph"]
	graphProperties := graphSchema["properties"].(map[string]interface{})
	adjacencyProperties := graphProperties["adjacency"].(map[string]interface{})["properties"].(map[string]interface{})
	nodeSchema := adjacencyProperties["nodes"].(map[string]interface{})["items"].(map[string]interface{})
	nodeProperties := nodeSchema["properties"].(map[string]interface{})
	boundarySchema := nodeProperties["boundary_refs"].(map[string]interface{})
	boundaryItem := boundarySchema["items"].(map[string]interface{})
	boundaryProperties := boundaryItem["properties"].(map[string]interface{})
	for key := range liveBoundary {
		if boundaryProperties[key] == nil {
			t.Fatalf("live boundary field %q missing from graph schema", key)
		}
	}
	required := boundaryItem["required"].([]string)
	for _, key := range []string{"relation_type", "endpoint_id", "issue_type", "status", "contexts", "in_scope", "from", "to"} {
		if !slices.Contains(required, key) {
			t.Fatalf("graph boundary schema does not require %q", key)
		}
	}
	if got := boundaryProperties["in_scope"].(map[string]interface{})["const"]; got != false {
		t.Fatalf("in_scope schema const = %#v", got)
	}
}

func TestHubCapacityPreservesCanonicalCriticalPath(t *testing.T) {
	selected := "ctx:" + "selected"
	issues := []model.Issue{
		{ID: "visible-a", Title: "Visible A", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{selected}},
		{ID: "hidden", Title: "Hidden", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:" + "other"}, Dependencies: []*model.Dependency{{DependsOnID: "visible-a", Type: model.DepBlocks}}},
		{ID: "visible-b", Title: "Visible B", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{selected}, Dependencies: []*model.Dependency{{DependsOnID: "hidden", Type: model.DepBlocks}}},
	}
	scope, err := model.NewSelectedContextsHubScope([]string{selected})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := newHubScopeProjection(scope, issues, "")
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	ctx := RobotContext{
		Issues:        issues,
		DataHash:      analysis.ComputeDataHash(issues),
		HubProjection: projection,
		Encoder: hubScopeRobotEncoder{
			base:       newJSONRobotEncoder(&encoded),
			command:    "robot-capacity",
			projection: projection,
		},
	}
	if err := handleRobotCapacity(ctx, phaseThreeRobotHandlerConfig{}); err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	path := output["critical_path"].([]any)
	if !reflect.DeepEqual(path, []any{"visible-a", "hidden", "visible-b"}) {
		t.Fatalf("critical path = %#v", path)
	}
	if int(output["critical_path_length"].(float64)) != len(path) {
		t.Fatalf("critical path length = %v, path = %#v", output["critical_path_length"], path)
	}
	stats := analysis.NewAnalyzer(issues).Analyze()
	wantSerialMinutes := 0
	for _, id := range []string{"visible-a", "hidden", "visible-b"} {
		eta, err := analysis.EstimateETAForIssue(issues, &stats, id, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		wantSerialMinutes += eta.EstimatedMinutes
	}
	if int(output["serial_minutes"].(float64)) != wantSerialMinutes {
		t.Fatalf("serial minutes = %v, want %d", output["serial_minutes"], wantSerialMinutes)
	}
	if output["parallel_minutes"].(float64) != output["total_minutes"].(float64)-output["serial_minutes"].(float64) {
		t.Fatalf("path-derived minute summaries disagree: %#v", output)
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
