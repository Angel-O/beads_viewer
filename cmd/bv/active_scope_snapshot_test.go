package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func writeFakeWBD(t *testing.T, output, calls string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wbd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WBD_CALLS\"\nif [ \"$2\" = active ]; then printf '%s' \"$WBD_ACTIVE\"; else printf '%s' \"$WBD_SHOW\"; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(path)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_CALLS", calls)
	t.Setenv("WBD_ACTIVE", output)
	t.Setenv("WBD_SHOW", `{"id":"scope-a","members":[{"id":"A"},{"issue_id":"B"}]}`)
}

func TestHubScopeMemberLoaderUsesOnlyPublicScopeCommands(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	writeFakeWBD(t, `{"id":"scope-a"}`, calls)
	loader := newHubScopeMemberLoader(t.TempDir())
	got, err := loader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("scope members = %#v, want [A B]", got)
	}
	callsData, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(callsData) != "scope active --json\nscope show scope-a --json\n" {
		t.Fatalf("wbd calls = %q", callsData)
	}
}

func TestHubScopeSnapshotLoaderCarriesActiveIdentityAndBoundedMembers(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	writeFakeWBD(t, `{"id":"scope-a","name":"Active scope","created_on":"2026-09-05","state":"active","member_limit":100}`, calls)
	snapshot, err := newHubScopeSnapshotLoader(t.TempDir())(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Active == nil || snapshot.Active.ID != "scope-a" || snapshot.Active.Name != "Active scope" || snapshot.Active.MemberCount != 2 || snapshot.Active.MemberLimit != 100 {
		t.Fatalf("active scope = %#v", snapshot.Active)
	}
	if !reflect.DeepEqual(snapshot.MemberIDs, []string{"A", "B"}) {
		t.Fatalf("scope members = %#v", snapshot.MemberIDs)
	}
}

func TestHubScopeMemberLoaderTreatsAbsentActiveScopeAsEmpty(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	path := filepath.Join(t.TempDir(), "wbd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WBD_CALLS\"\nprintf '%s' 'no active scope' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(path)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_CALLS", calls)
	got, err := newHubScopeMemberLoader(t.TempDir())(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("absent active scope members = %#v, want nil", got)
	}
}

func TestHubStartupResolvesScopeBeforeIssueLoad(t *testing.T) {
	var calls []string
	snapshot, issues, err := loadHubStartupIssues(context.Background(), func(context.Context) (hubScopeSnapshot, error) {
		calls = append(calls, "scope")
		return hubScopeSnapshot{}, nil
	}, func() ([]model.Issue, error) {
		calls = append(calls, "issues")
		return nil, fmt.Errorf("poisoned projection read")
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Active != nil || issues != nil {
		t.Fatalf("no-active startup = %#v/%#v, want empty state", snapshot.Active, issues)
	}
	if !reflect.DeepEqual(calls, []string{"scope"}) {
		t.Fatalf("startup calls = %#v, want scope only", calls)
	}
}

func TestHubStartupLoadsAndBoundsActiveScope(t *testing.T) {
	var calls []string
	snapshot, issues, err := loadHubStartupIssues(context.Background(), func(context.Context) (hubScopeSnapshot, error) {
		calls = append(calls, "scope")
		return hubScopeSnapshot{Active: &RobotActiveScope{ID: "scope-a"}, MemberIDs: []string{"A"}}, nil
	}, func() ([]model.Issue, error) {
		calls = append(calls, "issues")
		return []model.Issue{{ID: "A"}, {ID: "B"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"scope", "issues"}) {
		t.Fatalf("startup calls = %#v, want scope then issues", calls)
	}
	if snapshot.Active == nil {
		t.Fatal("active startup lost active scope")
	}
	if got := snapshotIssueIDs(issues); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("active startup issues = %#v, want [A]", got)
	}
}

func TestHubStartupLoadsActiveEmptyScope(t *testing.T) {
	loaded := false
	snapshot, issues, err := loadHubStartupIssues(context.Background(), func(context.Context) (hubScopeSnapshot, error) {
		return hubScopeSnapshot{Active: &RobotActiveScope{ID: "empty"}}, nil
	}, func() ([]model.Issue, error) {
		loaded = true
		return []model.Issue{{ID: "outside"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !loaded || snapshot.Active == nil || issues == nil || len(issues) != 0 {
		t.Fatalf("active empty startup = loaded %t, active %#v, issues %#v", loaded, snapshot.Active, issues)
	}
}

func TestFilterHubScopeIssuesIsMembershipOnly(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Labels: []string{"ctx:alpha"}, Dependencies: []*model.Dependency{{DependsOnID: "B"}, {DependsOnID: "C"}}},
		{ID: "B", Labels: []string{"ctx:beta"}},
		{ID: "C", Labels: []string{"ctx:alpha"}},
	}
	got := filterHubScopeIssues(issues, []string{"A", "C", "C"})
	if len(got) != 2 || got[0].ID != "A" || got[1].ID != "C" {
		t.Fatalf("filtered issues = %#v, want only explicit members A and C", got)
	}
	if len(got[0].Dependencies) != 1 || got[0].Dependencies[0].DependsOnID != "C" {
		t.Fatalf("filtered dependencies = %#v, want only in-scope C", got[0].Dependencies)
	}
}

func TestFilterHubRobotSelectionStaysWithinActiveMembers(t *testing.T) {
	selection, err := hub.NewSelectedContextsAndContextlessHubScope([]string{"ctx:alpha"})
	if err != nil {
		t.Fatal(err)
	}
	issues := []model.Issue{
		{ID: "alpha", Labels: []string{"ctx:alpha"}},
		{ID: "beta", Labels: []string{"ctx:beta"}},
		{ID: "plain", Labels: []string{}},
	}
	got := filterHubRobotSelection(filterHubScopeIssues(issues, []string{"alpha", "beta", "plain"}), selection)
	if ids := snapshotIssueIDs(got); !reflect.DeepEqual(ids, []string{"alpha", "plain"}) {
		t.Fatalf("bounded wbv selection = %#v, want alpha and plain", ids)
	}
}

func TestBoundHubSprintMembershipUsesRobotIssueSlice(t *testing.T) {
	sprints := []model.Sprint{{ID: "sprint-1", BeadIDs: []string{"active", "hidden"}}}
	boundHubSprintMembership(sprints, RobotContext{
		HubMode: true,
		ActiveScope: &RobotActiveScope{
			ID: "scope-a",
		},
		Issues: []model.Issue{{ID: "active"}},
	})
	if !reflect.DeepEqual(sprints[0].BeadIDs, []string{"active"}) {
		t.Fatalf("bounded sprint members = %#v, want [active]", sprints[0].BeadIDs)
	}
}

func TestHubRobotAndExportsUseActiveSnapshotAndEmptyWithoutOne(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	store := filepath.Join(root, ".local", "share", "beads", "hub", ".beads")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, ".config", "bv", "hub.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("version: 1\nstore: "+store+"\nledger: "+filepath.Join(root, "ledger.jsonl")+"\nrepositories: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	issues := []model.Issue{
		{ID: "active", Title: "Active member", Status: model.StatusOpen, IssueType: model.TypeTask, Dependencies: []*model.Dependency{{IssueID: "active", DependsOnID: "hidden", Type: model.DepBlocks}}},
		{ID: "hidden", Title: "Hidden member", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	var lines strings.Builder
	encoder := json.NewEncoder(&lines)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(store, "issues.jsonl"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	writeFakeWBD(t, `{"id":"scope-a","name":"Active scope","created_on":"2026-09-05","state":"active","member_limit":100}`, filepath.Join(root, "wbd-calls"))
	t.Setenv("WBD_SHOW", `{"id":"scope-a","name":"Active scope","members":[{"id":"active"}]}`)
	executable := buildTestBinary(t)
	run := func(arguments ...string) ([]byte, error) {
		t.Helper()
		command := exec.Command(executable, arguments...)
		command.Dir = root
		command.Env = append(isolatedRobotTestEnv(root, ""), `BV_WBV_HUB_SCOPE={"mode":"all_items","contexts":[]}`)
		return command.CombinedOutput()
	}
	activeOutput, err := run("--history-mode", "external", "--hub-config", config, "--robot-plan", "--format", "json")
	if err != nil {
		t.Fatalf("active robot-plan: %v\n%s", err, activeOutput)
	}
	var activePayload map[string]any
	if err := json.Unmarshal(activeOutput, &activePayload); err != nil {
		t.Fatal(err)
	}
	if activePayload["scope"].(map[string]any)["id"] != "scope-a" || activePayload["scope"].(map[string]any)["member_count"] != float64(1) {
		t.Fatalf("active robot scope = %#v", activePayload["scope"])
	}
	if strings.Contains(string(activeOutput), "boundary_refs") || strings.Contains(string(activeOutput), "contexts") || strings.Contains(string(activeOutput), "hidden") {
		t.Fatalf("active robot retained old scope projection: %s", activeOutput)
	}
	if got := planItemIDs(activePayload); !reflect.DeepEqual(got, []string{"active"}) {
		t.Fatalf("active robot plan IDs = %#v", got)
	}
	scopeProperties := generateRobotSchemas().Commands["robot-plan"]["properties"].(map[string]interface{})["scope"].(map[string]interface{})["properties"].(map[string]interface{})
	for _, retired := range []string{"mode", "contexts", "include_contextless"} {
		if _, ok := scopeProperties[retired]; ok {
			t.Fatalf("retired scope property %q remains in robot schema", retired)
		}
	}

	activeMarkdown := filepath.Join(root, "active.md")
	if output, err := run("--history-mode", "external", "--hub-config", config, "--export-md", activeMarkdown); err != nil {
		t.Fatalf("active markdown export: %v\n%s", err, output)
	}
	markdown, err := os.ReadFile(activeMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "Active member") || strings.Contains(string(markdown), "Hidden member") {
		t.Fatalf("active markdown was not bounded: %s", markdown)
	}

	nonHubGraph := filepath.Join(root, "non-hub-empty.svg")
	nonHubCommand := exec.Command(executable, "--history-mode", "external", "--hub-config", config, "--label", "missing", "--export-graph", nonHubGraph)
	nonHubCommand.Dir = root
	nonHubCommand.Env = isolatedRobotTestEnv(root, "")
	if output, err := nonHubCommand.CombinedOutput(); err == nil || !strings.Contains(string(output), "No issues to export") {
		t.Fatalf("non-Hub empty graph export = %v\n%s", err, output)
	}

	noActivePath := filepath.Join(t.TempDir(), "wbd")
	if err := os.WriteFile(noActivePath, []byte("#!/bin/sh\nif [ \"$2\" = active ]; then printf '%s' 'no active scope' >&2; exit 1; fi\nprintf '%s' \"$WBD_SHOW\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(noActivePath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	noActiveOutput, err := run("--history-mode", "external", "--hub-config", config, "--robot-plan", "--format", "json")
	if err != nil {
		t.Fatalf("no-active robot-plan: %v\n%s", err, noActiveOutput)
	}
	var noActivePayload map[string]any
	if err := json.Unmarshal(noActiveOutput, &noActivePayload); err != nil {
		t.Fatal(err)
	}
	if _, ok := noActivePayload["scope"]; ok || len(planItemIDs(noActivePayload)) != 0 {
		t.Fatalf("no-active robot output = %#v", noActivePayload)
	}
	emptyMarkdown := filepath.Join(root, "empty.md")
	if output, err := run("--history-mode", "external", "--hub-config", config, "--export-md", emptyMarkdown); err != nil {
		t.Fatalf("no-active markdown export: %v\n%s", err, output)
	}
	emptyGraph := filepath.Join(root, "empty.svg")
	if output, err := run("--history-mode", "external", "--hub-config", config, "--export-graph", emptyGraph); err != nil {
		t.Fatalf("no-active graph export: %v\n%s", err, output)
	}
}

func isolatedRobotTestEnv(home, scope string) []string {
	blocked := []string{"HOME=", "BEADS_DIR=", "BEADS_DB=", "BD_DB=", "BV_WBV_HUB_MODE=", "BV_WBV_HUB_SCOPE=", "BV_NO_GITIGNORE=", "BV_NO_CACHE="}
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
		for _, rawItem := range track["items"].([]any) {
			ids = append(ids, rawItem.(map[string]any)["id"].(string))
		}
	}
	sort.Strings(ids)
	return ids
}

func snapshotIssueIDs(issues []model.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}
