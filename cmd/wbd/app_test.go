package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

type childCall struct {
	Name string            `json:"name"`
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Dir  string            `json:"dir"`
	Plan json.RawMessage   `json:"plan,omitempty"`
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) {
	return 0, errors.New("synthetic stdout failure")
}

func TestMain(m *testing.M) {
	if os.Getenv("WBD_FAKE_CHILD") == "1" {
		fakeChild()
		return
	}
	os.Exit(m.Run())
}

func fakeChild() {
	directory, err := os.Getwd()
	if err != nil {
		os.Exit(98)
	}
	environment := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		environment[name] = value
	}
	call := childCall{
		Name: filepath.Base(os.Args[0]),
		Args: append([]string(nil), os.Args[1:]...),
		Env:  environment,
		Dir:  directory,
	}
	if key := fakeCommandKey(call.Args); key == "create:graph" {
		for index, argument := range call.Args {
			if argument == "--graph" && index+1 < len(call.Args) {
				call.Plan, _ = os.ReadFile(call.Args[index+1])
				break
			}
		}
	}
	file, err := os.OpenFile(os.Getenv("WBD_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(97)
	}
	err = json.NewEncoder(file).Encode(call)
	_ = file.Close()
	if err != nil {
		os.Exit(96)
	}
	if os.Getenv("WBD_CREATE_STORE") == "1" && len(call.Args) > 0 && call.Args[0] == "init" {
		if err := os.MkdirAll(os.Getenv("BEADS_DIR"), 0o700); err != nil {
			os.Exit(95)
		}
	}
	responses := make(map[string]string)
	if raw := os.Getenv("WBD_RESPONSES"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &responses); err != nil {
			os.Exit(94)
		}
	}
	key := fakeCommandKey(call.Args)
	response := responses[key]
	if response == "" && strings.HasPrefix(key, "show:") {
		id := strings.TrimPrefix(key, "show:")
		response = fmt.Sprintf(`[{"id":%q,"title":"Issue","status":"open","priority":2,"issue_type":"task"}]`, id)
	}
	if response != "" {
		_, _ = io.WriteString(os.Stdout, response)
	}
	code, _ := strconv.Atoi(os.Getenv("WBD_CHILD_EXIT"))
	exitCodes := make(map[string]int)
	if raw := os.Getenv("WBD_EXIT_CODES"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &exitCodes); err != nil {
			os.Exit(93)
		}
	}
	if commandCode, ok := exitCodes[key]; ok {
		code = commandCode
	}
	os.Exit(code)
}

func fakeCommandKey(arguments []string) string {
	for len(arguments) > 0 {
		switch arguments[0] {
		case "--db":
			arguments = arguments[2:]
		case "--readonly":
			arguments = arguments[1:]
		case "--json":
			arguments = arguments[1:]
		default:
			if arguments[0] == "show" && len(arguments) > 1 {
				return "show:" + arguments[1]
			}
			if arguments[0] == "config" && len(arguments) > 1 {
				return "config:" + arguments[1]
			}
			if arguments[0] == "create" && len(arguments) > 1 && arguments[1] == "--graph" {
				return "create:graph"
			}
			if (arguments[0] == "close" || arguments[0] == "reopen") && len(arguments) > 1 {
				return arguments[0] + ":" + arguments[1]
			}
			return arguments[0]
		}
	}
	return ""
}

func TestCreateRegistersAndForwardsExactArgumentsAndEnvironment(t *testing.T) {
	test := newAppTest(t, true)
	for _, name := range []string{
		"BD_DB", "BEADS_DB", "BD_GLOBAL", "BEADS_DOLT_DATA_DIR", "BEADS_DOLT_PORT",
		"BEADS_DOLT_PROXIED_SERVER", "BEADS_DOLT_SERVER_DATABASE", "BEADS_DOLT_SERVER_HOST",
		"BEADS_DOLT_SERVER_MODE", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_SERVER_SOCKET",
		"BEADS_DOLT_SHARED_SERVER", "BEADS_DIR", "BV_NO_GITIGNORE",
	} {
		t.Setenv(name, "host-value")
	}

	code, _, stderr := test.run("--json", "create", "Ship it", "--priority=P1", "--labels", "backend,urgent", "--description", "details")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	calls := test.calls()
	if len(calls) != 1 || calls[0].Name != "bd" {
		t.Fatalf("calls = %#v", calls)
	}
	context := contextForTest(t, test.repository)
	want := []string{
		"--db", test.store, "--json", "create", "--labels", context, "Ship it",
		"--priority", "P1", "--labels", "backend,urgent", "--description", "details",
	}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("bd args = %#v, want %#v", calls[0].Args, want)
	}
	assertIsolatedEnvironment(t, calls[0].Env, test.store, false)
	config := readTestConfig(t, test.config)
	if config.Repositories[context].Path != test.repository {
		t.Fatalf("registered repository = %#v", config.Repositories)
	}
}

func TestCreateAndNewForwardExplicitAssignee(t *testing.T) {
	for _, command := range []string{"create", "new"} {
		t.Run(command, func(t *testing.T) {
			test := newAppTest(t, true)
			setResponses(t, map[string]string{command: `[{"id":"work-1","assignee":"agent-7","owner":"team-owner","created_by":"audit-actor"}]`})
			code, stdout, stderr := test.run(command, "Assigned work", "--assignee", "agent-7", "--json")
			if code != 0 || stderr != "" {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			for _, field := range []string{`"assignee":"agent-7"`, `"owner":"team-owner"`, `"created_by":"audit-actor"`} {
				if !strings.Contains(stdout, field) {
					t.Errorf("JSON output dropped %s: %s", field, stdout)
				}
			}
			calls := test.calls()
			if len(calls) != 1 || !slices.Contains(calls[0].Args, "--assignee") || !slices.Contains(calls[0].Args, "agent-7") {
				t.Fatalf("calls = %#v", calls)
			}
			if slices.Contains(calls[0].Args, "--owner") || slices.Contains(calls[0].Args, "--created-by") {
				t.Fatalf("assignee was mapped to audit metadata: %#v", calls[0].Args)
			}
		})
	}
}

func TestCreateTargetingAndAdmission(t *testing.T) {
	t.Run("explicit target does not register current", func(t *testing.T) {
		test := newAppTest(t, true)
		writeHubConfig(t, test, map[string]string{"ctx:target": "/target"})
		code, _, stderr := test.run("create", "Explicit", "--context", "ctx:target", "--context", "ctx:target", "--type", "task")
		if code != 0 {
			t.Fatalf("run code = %d, stderr = %q", code, stderr)
		}
		calls := test.calls()
		want := []string{"--db", test.store, "create", "--labels", "ctx:target", "Explicit", "--type", "task"}
		if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
		if got := readTestConfig(t, test.config).Repositories; len(got) != 1 {
			t.Fatalf("explicit create registered current repository: %#v", got)
		}
	})

	t.Run("contextless todo", func(t *testing.T) {
		test := newAppTest(t, true)
		writeHubConfig(t, test, map[string]string{"ctx:target": "/target"})
		setResponses(t, map[string]string{"config:get": `{"key":"types.custom","value":"todo"}`})
		code, _, stderr := test.run("create", "Inbox", "--type", "todo", "--contextless")
		if code != 0 {
			t.Fatalf("run code = %d, stderr = %q", code, stderr)
		}
		want := []string{"--db", test.store, "create", "Inbox", "--type", "todo"}
		if calls := test.calls(); len(calls) != 2 || fakeCommandKey(calls[0].Args) != "config:get" || !reflect.DeepEqual(calls[1].Args, want) {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
		if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
			t.Fatalf("successful todo creation did not signal Viewer: %v", err)
		}
	})

	t.Run("invalid cardinality fails before mutation", func(t *testing.T) {
		test := newAppTest(t, true)
		writeHubConfig(t, test, map[string]string{"ctx:target": "/target"})
		code, _, stderr := test.run("--json", "create", "Invalid", "--type", "task", "--contextless")
		if code != 1 || !strings.Contains(stderr, `"code":"invalid_cardinality"`) {
			t.Fatalf("run code = %d, stderr = %q", code, stderr)
		}
		if calls := test.calls(); len(calls) != 0 {
			t.Fatalf("invalid admission mutated store: %#v", calls)
		}
	})

	t.Run("unregistered target fails before mutation", func(t *testing.T) {
		test := newAppTest(t, true)
		writeHubConfig(t, test, map[string]string{"ctx:target": "/target"})
		code, _, stderr := test.run("--json", "create", "Invalid", "--context", "ctx:unknown")
		if code != 1 || !strings.Contains(stderr, `"code":"unregistered_context"`) {
			t.Fatalf("run code = %d, stderr = %q", code, stderr)
		}
		if calls := test.calls(); len(calls) != 0 {
			t.Fatalf("unregistered target mutated store: %#v", calls)
		}
	})

	t.Run("decision permits only default current", func(t *testing.T) {
		test := newAppTest(t, true)
		code, _, stderr := test.run("create", "Decision", "--type", "decision")
		if code != 0 {
			t.Fatalf("default-current decision code = %d, stderr = %q", code, stderr)
		}
		test = newAppTest(t, true)
		writeHubConfig(t, test, map[string]string{"ctx:target": "/target"})
		code, _, stderr = test.run("--json", "create", "Decision", "--type", "decision", "--context", "ctx:target")
		if code != 1 || len(test.calls()) != 0 {
			t.Fatalf("explicit decision code = %d, stderr = %q, calls = %#v", code, stderr, test.calls())
		}
	})
}

func TestCreateFromTodoUsesOneGraphMutation(t *testing.T) {
	test := newAppTest(t, true)
	writeHubConfig(t, test, map[string]string{"ctx:target": "/target"})
	setResponses(t, map[string]string{
		"show:todo-1":  `[{"id":"todo-1","title":"Capture","status":"open","priority":2,"issue_type":"todo"}]`,
		"create:graph": `{"ids":{"result":"work-1"}}`,
	})
	code, stdout, stderr := test.run("create", "Implement", "--type", "task", "--context", "ctx:target", "--from-todo", "todo-1", "--labels", "team", "--assignee", "agent-7")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "work-1") {
		t.Fatalf("stdout = %q", stdout)
	}
	calls := test.calls()
	if len(calls) != 2 || fakeCommandKey(calls[0].Args) != "show:todo-1" || fakeCommandKey(calls[1].Args) != "create:graph" {
		t.Fatalf("calls = %#v", calls)
	}
	var plan graphPlan
	if err := json.Unmarshal(calls[1].Plan, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 1 || !reflect.DeepEqual(plan.Nodes[0].Labels, []string{"team", "ctx:target"}) || plan.Nodes[0].Assignee != "agent-7" {
		t.Fatalf("graph nodes = %#v", plan.Nodes)
	}
	if len(plan.Edges) != 1 || plan.Edges[0].Type != "discovered-from" || plan.Edges[0].ToID != "todo-1" {
		t.Fatalf("graph edges = %#v", plan.Edges)
	}
}

func TestUpdateAssigneeSetClearAndPreservation(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		wantTail  []string
	}{
		{name: "assign", arguments: []string{"update", "work-1", "--assignee", "agent-7", "--json"}, wantTail: []string{"update", "work-1", "--assignee", "agent-7"}},
		{name: "reassign", arguments: []string{"update", "work-1", "--assignee=agent-8"}, wantTail: []string{"update", "work-1", "--assignee", "agent-8"}},
		{name: "clear separate", arguments: []string{"update", "work-1", "--assignee", "", "--json"}, wantTail: []string{"update", "work-1", "--assignee", ""}},
		{name: "clear equals", arguments: []string{"update", "work-1", "--assignee="}, wantTail: []string{"update", "work-1", "--assignee", ""}},
		{name: "status preserves", arguments: []string{"update", "work-1", "--status", "in_progress"}, wantTail: []string{"update", "work-1", "--status", "in_progress"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			code, _, stderr := test.run(testCase.arguments...)
			if code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			calls := test.calls()
			mutation := calls[len(calls)-1]
			if !reflect.DeepEqual(mutation.Args[len(mutation.Args)-len(testCase.wantTail):], testCase.wantTail) {
				t.Fatalf("calls = %#v, want tail %#v", calls, testCase.wantTail)
			}
			if testCase.name == "status preserves" && slices.Contains(mutation.Args, "--assignee") {
				t.Fatalf("status-only update changed assignee: %#v", mutation.Args)
			}
			if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
				t.Fatalf("successful assignee mutation did not signal Viewer: %v", err)
			}
		})
	}
}

func TestReplaceCopiesOpenBlockingContinuityThenCloses(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:new": "/new"})
	setResponses(t, map[string]string{
		"show:old-1":   `[{"id":"old-1","title":"Original","description":"Details","status":"open","priority":1,"issue_type":"task","labels":["ctx:old","team"],"dependencies":[{"id":"blocker-open","status":"open","dependency_type":"blocks"},{"id":"blocker-closed","status":"closed","dependency_type":"blocks"},{"id":"related","status":"open","dependency_type":"related"}],"dependents":[{"id":"dependent-open","status":"in_progress","dependency_type":"blocks"},{"id":"dependent-closed","status":"closed","dependency_type":"blocks"}] }]`,
		"create:graph": `{"ids":{"replacement":"new-1"}}`,
		"close:old-1":  `{}`,
	})
	code, stdout, stderr := test.run("replace", "old-1", "--context", "ctx:new")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "new-1") {
		t.Fatalf("stdout = %q", stdout)
	}
	calls := test.calls()
	if len(calls) != 3 || fakeCommandKey(calls[1].Args) != "create:graph" || fakeCommandKey(calls[2].Args) != "close:old-1" {
		t.Fatalf("calls = %#v", calls)
	}
	if !reflect.DeepEqual(calls[2].Args[len(calls[2].Args)-2:], []string{"--reason", "Superseded by new-1"}) {
		t.Fatalf("close args = %#v", calls[2].Args)
	}
	if !slices.Contains(calls[2].Args, "--force") {
		t.Fatalf("correction close did not force the prevalidated transition: %#v", calls[2].Args)
	}
	var plan graphPlan
	if err := json.Unmarshal(calls[1].Plan, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 1 || plan.Nodes[0].Title != "Original" || plan.Nodes[0].Description != "Details" || plan.Nodes[0].Priority != 1 {
		t.Fatalf("replacement inheritance = %#v", plan.Nodes)
	}
	if !reflect.DeepEqual(plan.Nodes[0].Labels, []string{"team", "ctx:new"}) {
		t.Fatalf("replacement labels = %#v", plan.Nodes[0].Labels)
	}
	wantEdges := map[string]bool{
		"replacement||old-1|supersedes":      true,
		"replacement||blocker-open|blocks":   true,
		"|dependent-open|replacement|blocks": true,
	}
	for _, edge := range plan.Edges {
		delete(wantEdges, edge.FromKey+"|"+edge.FromID+"|"+firstNonEmpty(edge.ToID, edge.ToKey)+"|"+edge.Type)
	}
	if len(wantEdges) != 0 || len(plan.Edges) != 3 {
		t.Fatalf("graph edges = %#v, missing = %#v", plan.Edges, wantEdges)
	}
}

func TestReplaceCloseFailureNamesPersistedReplacement(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:new": "/new"})
	setResponses(t, map[string]string{
		"show:old-1":   `[{"id":"old-1","title":"Original","status":"open","priority":2,"issue_type":"task","labels":["ctx:old"]}]`,
		"create:graph": `{"ids":{"replacement":"new-1"}}`,
	})
	setExitCodes(t, map[string]int{"close:old-1": 8})
	code, _, stderr := test.run("replace", "old-1", "--context", "ctx:new")
	if code != 1 || !strings.Contains(stderr, "replacement new-1 was created") {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	if calls := test.calls(); len(calls) != 3 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestReplaceRejectsDecisionExplicitTarget(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:new": "/new"})
	setResponses(t, map[string]string{
		"show:decision-1": `[{"id":"decision-1","title":"Decision","status":"open","priority":2,"issue_type":"decision","labels":["ctx:old"]}]`,
	})
	code, _, stderr := test.run("--json", "replace", "decision-1", "--context", "ctx:new")
	if code != 1 || !strings.Contains(stderr, `"code":"invalid_supersession"`) {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if calls := test.calls(); len(calls) != 1 || fakeCommandKey(calls[0].Args) != "show:decision-1" {
		t.Fatalf("decision replacement mutated store: %#v", calls)
	}
}

func TestEpicParentAndSupersededReopenPrevalidation(t *testing.T) {
	t.Run("outside epic context", func(t *testing.T) {
		test := newAppTest(t, false)
		setResponses(t, map[string]string{
			"show:child-1": `[{"id":"child-1","status":"open","issue_type":"task","labels":["ctx:outside"]}]`,
			"show:epic-1":  `[{"id":"epic-1","status":"open","issue_type":"epic","labels":["ctx:inside"]}]`,
		})
		code, _, _ := test.run("dep", "add", "child-1", "epic-1", "--type", "parent-child")
		if code != 1 || len(test.calls()) != 2 {
			t.Fatalf("code = %d, calls = %#v", code, test.calls())
		}
	})

	t.Run("valid epic child delegates", func(t *testing.T) {
		test := newAppTest(t, false)
		setResponses(t, map[string]string{
			"show:child-1": `[{"id":"child-1","status":"open","issue_type":"task","labels":["ctx:inside"]}]`,
			"show:epic-1":  `[{"id":"epic-1","status":"open","issue_type":"epic","labels":["ctx:inside","ctx:other"]}]`,
		})
		code, _, stderr := test.run("dep", "add", "child-1", "epic-1", "--type", "parent-child")
		if code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		calls := test.calls()
		if len(calls) != 3 || fakeCommandKey(calls[2].Args) != "dep" {
			t.Fatalf("calls = %#v", calls)
		}
	})

	t.Run("superseded issue", func(t *testing.T) {
		test := newAppTest(t, false)
		setResponses(t, map[string]string{
			"show:old-1": `[{"id":"old-1","status":"closed","issue_type":"task","dependents":[{"id":"new-1","status":"open","issue_type":"task","dependency_type":"supersedes"}]}]`,
		})
		code, _, stderr := test.run("reopen", "old-1")
		if code != 1 || !strings.Contains(stderr, "cannot be routinely reactivated") || len(test.calls()) != 1 {
			t.Fatalf("code = %d, stderr = %q, calls = %#v", code, stderr, test.calls())
		}
	})
}

func TestClosedIssueStatusReactivationGuard(t *testing.T) {
	for _, status := range []string{"open", "in_progress", "blocked", "deferred"} {
		t.Run("superseded_to_"+status, func(t *testing.T) {
			test := newAppTest(t, false)
			setResponses(t, map[string]string{
				"show:original-1": `[{"id":"original-1","status":"closed","issue_type":"task","dependents":[{"id":"replacement-1","status":"open","issue_type":"task","dependency_type":"supersedes"}]}]`,
			})
			code, _, stderr := test.run("--json", "update", "original-1", "--status", status)
			if code != 1 || !strings.Contains(stderr, `"code":"invalid_supersession"`) {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			calls := test.calls()
			if len(calls) != 1 || fakeCommandKey(calls[0].Args) != "show:original-1" {
				t.Fatalf("rejected reactivation reached mutation child: %#v", calls)
			}
			assertNoViewerSignal(t, test)
		})
	}

	t.Run("ordinary closed issue", func(t *testing.T) {
		test := newAppTest(t, false)
		setResponses(t, map[string]string{
			"show:ordinary-1": `[{"id":"ordinary-1","status":"closed","issue_type":"task"}]`,
		})
		code, _, stderr := test.run("update", "ordinary-1", "--status", "open")
		if code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		calls := test.calls()
		if len(calls) != 2 || fakeCommandKey(calls[0].Args) != "show:ordinary-1" || fakeCommandKey(calls[1].Args) != "update" {
			t.Fatalf("ordinary reactivation calls = %#v", calls)
		}
		if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
			t.Fatalf("ordinary reactivation did not signal Viewer: %v", err)
		}
	})

	t.Run("incomplete incoming supersession fails closed", func(t *testing.T) {
		test := newAppTest(t, false)
		setResponses(t, map[string]string{
			"show:original-1": `[{"id":"original-1","status":"closed","issue_type":"task","dependents":[{"id":"replacement-1","dependency_type":"supersedes"}]}]`,
		})
		code, _, stderr := test.run("update", "original-1", "--status", "open")
		if code != 1 || !strings.Contains(stderr, "incomplete incoming supersession relation") {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if calls := test.calls(); len(calls) != 1 || fakeCommandKey(calls[0].Args) != "show:original-1" {
			t.Fatalf("incomplete relation reached mutation child: %#v", calls)
		}
		assertNoViewerSignal(t, test)
	})
}

func TestDependencyRemovalProtectsLifecycleContinuity(t *testing.T) {
	tests := []struct {
		name       string
		sourceKind string
		targetKind string
		relation   string
	}{
		{name: "replacement to original supersedes", sourceKind: "task", targetKind: "task", relation: "supersedes"},
		{name: "project work to todo discovered-from", sourceKind: "feature", targetKind: "todo", relation: "discovered-from"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			source := fmt.Sprintf(`[{"id":"source-1","status":"open","issue_type":%q,"dependencies":[{"id":"target-1","dependency_type":"related"},{"id":"target-1","dependency_type":%q}]}]`, testCase.sourceKind, testCase.relation)
			target := fmt.Sprintf(`[{"id":"target-1","status":"open","issue_type":%q}]`, testCase.targetKind)
			setResponses(t, map[string]string{"show:source-1": source, "show:target-1": target})
			code, _, stderr := test.run("--json", "dep", "remove", "source-1", "target-1")
			if code != 1 || !strings.Contains(stderr, `"code":"protected_lifecycle_edge"`) {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			calls := test.calls()
			if len(calls) != 2 || fakeCommandKey(calls[0].Args) != "show:source-1" || fakeCommandKey(calls[1].Args) != "show:target-1" {
				t.Fatalf("protected removal reached mutation child: %#v", calls)
			}
			assertNoViewerSignal(t, test)
		})
	}
}

func TestDependencyRemovalPreservesOrdinaryRelations(t *testing.T) {
	tests := []struct {
		name         string
		sourceKind   string
		targetKind   string
		dependencies string
		targetRead   bool
	}{
		{name: "reverse supersession direction", sourceKind: "task", targetKind: "task", dependencies: `[],"dependents":[{"id":"target-1","dependency_type":"supersedes"}]`},
		{name: "cross-kind supersedes", sourceKind: "task", targetKind: "bug", dependencies: `[{"id":"target-1","dependency_type":"supersedes"}]`, targetRead: true},
		{name: "generic discovered-from", sourceKind: "task", targetKind: "task", dependencies: `[{"id":"target-1","dependency_type":"discovered-from"}]`, targetRead: true},
		{name: "reverse discovered-from direction", sourceKind: "todo", targetKind: "task", dependencies: `[{"id":"target-1","dependency_type":"discovered-from"}]`, targetRead: true},
		{name: "blocks", sourceKind: "task", targetKind: "task", dependencies: `[{"id":"target-1","dependency_type":"blocks"}]`},
		{name: "related", sourceKind: "task", targetKind: "task", dependencies: `[{"id":"target-1","dependency_type":"related"}]`},
		{name: "parent-child", sourceKind: "task", targetKind: "epic", dependencies: `[{"id":"target-1","dependency_type":"parent-child"}]`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			source := fmt.Sprintf(`[{"id":"source-1","status":"open","issue_type":%q,"dependencies":%s}]`, testCase.sourceKind, testCase.dependencies)
			target := fmt.Sprintf(`[{"id":"target-1","status":"open","issue_type":%q}]`, testCase.targetKind)
			setResponses(t, map[string]string{"show:source-1": source, "show:target-1": target})
			code, _, stderr := test.run("dep", "remove", "source-1", "target-1")
			if code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			calls := test.calls()
			wantCalls := 2
			if testCase.targetRead {
				wantCalls = 3
			}
			if len(calls) != wantCalls || fakeCommandKey(calls[len(calls)-1].Args) != "dep" {
				t.Fatalf("ordinary removal calls = %#v", calls)
			}
			if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
				t.Fatalf("ordinary removal did not signal Viewer: %v", err)
			}
		})
	}
}

func TestCompatibilityReportsAcceptedFindingClasses(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:known": "/known"})
	if err := os.MkdirAll(filepath.Dir(test.app.paths.Ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(test.app.paths.Ledger, []byte(`{"bead_id":"todo-1","context":"ctx:known","commit":"abc"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setResponses(t, map[string]string{
		"list":             `[{"id":"bad-kind"},{"id":"bad-count"},{"id":"bad-context"},{"id":"todo-1"},{"id":"new-1"},{"id":"old-1"}]`,
		"show:bad-kind":    `[{"id":"bad-kind","status":"open","issue_type":"question","labels":["ctx:known"]}]`,
		"show:bad-count":   `[{"id":"bad-count","status":"open","issue_type":"task","dependencies":[{"id":"old-1","dependency_type":"discovered-from"}]}]`,
		"show:bad-context": `[{"id":"bad-context","status":"open","issue_type":"todo","labels":["ctx:missing-a","ctx:missing-b"]}]`,
		"show:todo-1":      `[{"id":"todo-1","status":"open","issue_type":"todo"}]`,
		"show:new-1":       `[{"id":"new-1","status":"open","issue_type":"task","labels":["ctx:known"],"dependencies":[{"id":"old-1","dependency_type":"supersedes"}]}]`,
		"show:old-1":       `[{"id":"old-1","status":"closed","issue_type":"bug","labels":["ctx:known"]}]`,
	})
	code, stdout, stderr := test.run("compatibility", "--json")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	for _, code := range []string{"invalid_kind", "invalid_cardinality", "unregistered_context", "todo_correlation", "malformed_lifecycle_edge"} {
		if !strings.Contains(stdout, `"code":"`+code+`"`) {
			t.Errorf("output missing %s: %s", code, stdout)
		}
	}
	var report struct {
		Findings []compatibilityFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 7 {
		t.Fatalf("findings = %#v, want 7 independent violations", report.Findings)
	}
	var missingContexts []string
	for index, finding := range report.Findings {
		if index > 0 {
			previous := report.Findings[index-1]
			left := previous.Code + "\x00" + previous.IssueID + "\x00" + previous.Related + "\x00" + previous.Value
			right := finding.Code + "\x00" + finding.IssueID + "\x00" + finding.Related + "\x00" + finding.Value
			if left > right {
				t.Fatalf("findings are not deterministic: %#v", report.Findings)
			}
		}
		if finding.Code == "unregistered_context" {
			missingContexts = append(missingContexts, finding.Value)
		}
	}
	if !reflect.DeepEqual(missingContexts, []string{"ctx:missing-a", "ctx:missing-b"}) {
		t.Fatalf("unregistered findings = %#v", missingContexts)
	}
	for _, call := range test.calls() {
		key := fakeCommandKey(call.Args)
		if key != "list" && !strings.HasPrefix(key, "show:") {
			t.Fatalf("compatibility performed mutation: %#v", call)
		}
	}
}

func TestCompatibilityUnreadableAuthoritativeDataFails(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:known": "/known"})
	if err := os.MkdirAll(filepath.Dir(test.app.paths.Ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(test.app.paths.Ledger, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setResponses(t, map[string]string{"list": `[]`})
	code, _, stderr := test.run("compatibility", "--json")
	if code != 1 || !strings.Contains(stderr, "authoritative correlation ledger") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}

func TestParserRejectsRemovedAndInvalidCompositions(t *testing.T) {
	tests := [][]string{
		{"update", "item-1", "--type", "bug"},
		{"dep", "add", "item-1", "item-2", "--type", "supersedes"},
		{"create", "Title", "--context", "ctx:a", "--contextless"},
		{"create", "Title", "--type", "custom-kind"},
		{"replace", "item-1", "--context", "ctx:a", "--from-todo", "todo-1"},
		{"compatibility"},
	}
	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			if _, err := parse(arguments); err == nil {
				t.Fatalf("parse(%#v) succeeded", arguments)
			}
		})
	}
}

func TestListScopingAndAllContexts(t *testing.T) {
	t.Run("scoped registers and owns first label", func(t *testing.T) {
		test := newAppTest(t, true)
		code, _, stderr := test.run("list", "--label", "team", "--status=open,blocked", "--limit", "25", "--json")
		if code != 0 {
			t.Fatalf("run code = %d, stderr = %q", code, stderr)
		}
		context := contextForTest(t, test.repository)
		want := []string{"--db", test.store, "--json", "list", "--label", context, "--label", "team", "--status", "open,blocked", "--limit", "25"}
		if calls := test.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
			t.Fatalf("calls = %#v, want args %#v", calls, want)
		}
	})

	t.Run("all contexts does not register", func(t *testing.T) {
		test := newAppTest(t, false)
		code, _, stderr := test.run("list", "--all-contexts", "--ready")
		if code != 0 {
			t.Fatalf("run code = %d, stderr = %q", code, stderr)
		}
		want := []string{"--db", test.store, "list", "--ready"}
		if calls := test.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
			t.Fatalf("calls = %#v, want args %#v", calls, want)
		}
		if _, err := os.Stat(test.config); !os.IsNotExist(err) {
			t.Fatalf("all-context list unexpectedly configured hub: %v", err)
		}
	})
}

func TestLinkDelegatesToBVWithRegistrationAndIsolation(t *testing.T) {
	test := newAppTest(t, true)
	t.Setenv("BD_DB", "wrong")
	t.Setenv("BEADS_DIR", "wrong")
	t.Setenv("BV_NO_GITIGNORE", "wrong")
	code, _, stderr := test.run("link", "bead-12", "HEAD~2")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	context := contextForTest(t, test.repository)
	want := []string{"correlate", "add", "--bead", "bead-12", "--repo", context, "--commit", "HEAD~2", "--hub-config", test.config}
	calls := test.calls()
	if len(calls) != 2 || fakeCommandKey(calls[0].Args) != "show:bead-12" || calls[1].Name != "bv" || !reflect.DeepEqual(calls[1].Args, want) {
		t.Fatalf("calls = %#v, want args %#v", calls, want)
	}
	assertIsolatedEnvironment(t, calls[1].Env, test.store, true)
	if data, err := os.ReadFile(hub.ChangeSignalPath(test.app.paths)); err != nil || len(data) == 0 {
		t.Fatalf("Viewer signal missing after link: data=%q err=%v", data, err)
	}
}

func TestLinkRejectsTodoBeforeCorrelation(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{
		"show:todo-1": `[{"id":"todo-1","status":"open","issue_type":"todo"}]`,
	})
	code, _, stderr := test.run("link", "todo-1")
	if code != 1 || !strings.Contains(stderr, "todo cannot own a direct commit correlation") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if calls := test.calls(); len(calls) != 1 || calls[0].Name != "bd" {
		t.Fatalf("todo link delegated correlation mutation: %#v", calls)
	}
}

func TestLinkSignalsCommittedAdditionWhenBVDurabilityCheckFails(t *testing.T) {
	test := newAppTest(t, true)
	context := contextForTest(t, test.repository)
	writeHubConfig(t, test, map[string]string{context: test.repository})
	response := fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":"0123456789abcdef0123456789abcdef01234567"},"added":true,"durability_error":"synthetic post-rename durability failure"}`+"\n", context)
	setResponses(t, map[string]string{"correlate": response})
	setExitCodes(t, map[string]int{"correlate": 7})

	code, stdout, _ := test.run("link", "item-alpha", "HEAD")
	if code != 7 || stdout != response {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
		t.Fatalf("committed addition did not signal Viewer after durability error: %v", err)
	}
}

func TestLinkNonzeroUnconfirmedAdditionDoesNotSignal(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name     string
		response func(string) string
	}{
		{name: "missing durability marker", response: func(context string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":%q},"added":true}`+"\n", context, sha)
		}},
		{name: "missing commit", response: func(context string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q},"added":true,"durability_error":"synthetic failure"}`+"\n", context)
		}},
		{name: "malformed commit", response: func(context string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":"not-a-full-sha"},"added":true,"durability_error":"synthetic failure"}`+"\n", context)
		}},
		{name: "wrong bead", response: func(context string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-other","context":%q,"commit":%q},"added":true,"durability_error":"synthetic failure"}`+"\n", context, sha)
		}},
		{name: "wrong context", response: func(string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":"ctx:synthetic-other","commit":%q},"added":true,"durability_error":"synthetic failure"}`+"\n", sha)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, true)
			context := contextForTest(t, test.repository)
			writeHubConfig(t, test, map[string]string{context: test.repository})
			setResponses(t, map[string]string{"correlate": testCase.response(context)})
			setExitCodes(t, map[string]int{"correlate": 7})

			code, _, _ := test.run("link", "item-alpha", "HEAD")
			if code != 7 {
				t.Fatalf("code=%d", code)
			}
			assertNoViewerSignal(t, test)
		})
	}
}

func TestUnlinkDelegatesExactTupleAndSignalsOnlyOnRemoval(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, testCase := range []struct {
		name    string
		removed bool
	}{
		{name: "removed", removed: true},
		{name: "not found", removed: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, true)
			context := contextForTest(t, test.repository)
			writeHubConfig(t, test, map[string]string{context: test.repository})
			response := fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":%q},"removed":%t}`+"\n", context, sha, testCase.removed)
			setResponses(t, map[string]string{"correlate": response})
			code, stdout, stderr := test.run("unlink", "item-alpha", sha)
			if code != 0 || stderr != "" || stdout != response {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			want := []string{"correlate", "remove", "--bead", "item-alpha", "--repo", context, "--commit", sha, "--hub-config", test.config}
			calls := test.calls()
			if len(calls) != 2 || fakeCommandKey(calls[0].Args) != "show:item-alpha" || calls[1].Name != "bv" || !reflect.DeepEqual(calls[1].Args, want) {
				t.Fatalf("calls = %#v, want bv args %#v", calls, want)
			}
			assertIsolatedEnvironment(t, calls[1].Env, test.store, true)
			_, signalErr := os.Stat(hub.ChangeSignalPath(test.app.paths))
			if testCase.removed && signalErr != nil {
				t.Fatalf("removed correlation did not signal Viewer: %v", signalErr)
			}
			if !testCase.removed && !os.IsNotExist(signalErr) {
				t.Fatalf("not-found correlation signaled Viewer: %v", signalErr)
			}
		})
	}
}

func TestUnlinkSignalsConfirmedRemovalWhenOutputFails(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, testCase := range []struct {
		name    string
		removed bool
	}{
		{name: "removed", removed: true},
		{name: "not found", removed: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, true)
			context := contextForTest(t, test.repository)
			writeHubConfig(t, test, map[string]string{context: test.repository})
			response := fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":%q},"removed":%t}`+"\n", context, sha, testCase.removed)
			setResponses(t, map[string]string{"correlate": response})
			var stderr bytes.Buffer
			test.app.stdout = failingOutput{}
			test.app.stderr = &stderr
			code := test.app.run([]string{"unlink", "item-alpha", sha})
			if code != 1 || !strings.Contains(stderr.String(), "synthetic stdout failure") {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
			_, signalErr := os.Stat(hub.ChangeSignalPath(test.app.paths))
			if testCase.removed && signalErr != nil {
				t.Fatalf("confirmed removal did not signal after output failure: %v", signalErr)
			}
			if !testCase.removed && !os.IsNotExist(signalErr) {
				t.Fatalf("not-found result signaled after output failure: %v", signalErr)
			}
		})
	}
}

func TestUnlinkSignalsCommittedRemovalWhenBVDurabilityCheckFails(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	test := newAppTest(t, true)
	context := contextForTest(t, test.repository)
	writeHubConfig(t, test, map[string]string{context: test.repository})
	response := fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":%q},"removed":true,"durability_error":"synthetic post-rename durability failure"}`+"\n", context, sha)
	setResponses(t, map[string]string{"correlate": response})
	setExitCodes(t, map[string]int{"correlate": 7})

	code, stdout, _ := test.run("unlink", "item-alpha", sha)
	if code != 7 || stdout != response {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
		t.Fatalf("committed removal did not signal Viewer after durability error: %v", err)
	}
}

func TestUnlinkNonzeroSuccessShapedResultWithoutDurabilityDoesNotSignal(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, testCase := range []struct {
		name     string
		response func(string) string
	}{
		{name: "missing durability marker", response: func(context string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":%q},"removed":true}`+"\n", context, sha)
		}},
		{name: "wrong commit", response: func(context string) string {
			return fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":"89abcdef0123456789abcdef0123456789abcdef"},"removed":true,"durability_error":"synthetic failure"}`+"\n", context)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, true)
			context := contextForTest(t, test.repository)
			writeHubConfig(t, test, map[string]string{context: test.repository})
			setResponses(t, map[string]string{"correlate": testCase.response(context)})
			setExitCodes(t, map[string]int{"correlate": 7})

			code, _, _ := test.run("unlink", "item-alpha", sha)
			if code != 7 {
				t.Fatalf("code=%d", code)
			}
			assertNoViewerSignal(t, test)
		})
	}
}

func TestUnlinkUsesSharedContextFromLinkedWorktree(t *testing.T) {
	const sha = "89abcdef0123456789abcdef0123456789abcdef"
	test := newAppTest(t, true)
	context := contextForTest(t, test.repository)
	writeHubConfig(t, test, map[string]string{context: test.repository})
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, test.repository, "worktree", "add", "--detach", linked, "HEAD")
	canonicalLinked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	test.app.dir = canonicalLinked
	response := fmt.Sprintf(`{"correlation":{"bead_id":"item-alpha","context":%q,"commit":%q},"removed":false}`+"\n", context, sha)
	setResponses(t, map[string]string{"correlate": response})
	code, _, stderr := test.run("unlink", "item-alpha", sha)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	calls := test.calls()
	if len(calls) != 2 || calls[1].Dir != canonicalLinked || requestValue(calls[1].Args, "--repo", "") != context {
		t.Fatalf("linked worktree calls = %#v", calls)
	}
}

func TestUnlinkRejectsBroadOrAmbiguousRequests(t *testing.T) {
	for _, arguments := range [][]string{
		{"unlink", "item-alpha"},
		{"unlink", "item-alpha", "0123456"},
		{"unlink", "item-alpha", "0123456789abcdef0123456789abcdef01234567", "extra"},
		{"--json", "unlink", "item-alpha", "0123456789abcdef0123456789abcdef01234567"},
	} {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			test := newAppTest(t, true)
			code, _, stderr := test.run(arguments...)
			if code != 1 || stderr == "" {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			if calls := test.calls(); len(calls) != 0 {
				t.Fatalf("rejected unlink delegated: %#v", calls)
			}
		})
	}
}

func TestUnlinkRejectsTodoBeforeCorrelation(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{
		"show:todo-alpha": `[{"id":"todo-alpha","status":"open","issue_type":"todo"}]`,
	})
	code, _, stderr := test.run("unlink", "todo-alpha", "0123456789abcdef0123456789abcdef01234567")
	if code != 1 || !strings.Contains(stderr, "todo cannot own a direct commit correlation") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if calls := test.calls(); len(calls) != 1 || calls[0].Name != "bd" {
		t.Fatalf("todo unlink delegated correlation mutation: %#v", calls)
	}
}

func TestBootstrapUsesDefaultPrefixAndExactSequence(t *testing.T) {
	test := newAppTestWithoutStore(t)
	t.Setenv("WBD_CREATE_STORE", "1")
	code, _, stderr := test.run("bootstrap")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	calls := test.calls()
	want := [][]string{
		{"metrics", "off"},
		{"init", "--prefix", "bead", "--non-interactive", "--skip-hooks", "--skip-agents"},
		{"--db", test.store, "config", "set", "types.custom", "todo"},
		{"--db", test.store, "config", "set", "export.auto", "false"},
		{"--db", test.store, "config", "set", "export.git-add", "false"},
		{"--db", test.store, "config", "set", "dolt.auto-push", "false"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	parent := filepath.Dir(test.store)
	parent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	for index := range want {
		if !reflect.DeepEqual(calls[index].Args, want[index]) || calls[index].Dir != parent {
			t.Fatalf("call %d = %#v, want args %#v in %q", index, calls[index], want[index], parent)
		}
		assertIsolatedEnvironment(t, calls[index].Env, test.store, false)
	}
	if _, err := os.Stat(test.config); err != nil {
		t.Fatalf("bootstrap config: %v", err)
	}
}

func TestBootstrapExistingStoreMergesCustomTypesWithoutReinitializing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: "todo"},
		{name: "one", value: "review", want: "review,todo"},
		{name: "multiple with whitespace", value: "review, docs", want: "review,docs,todo"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			setResponses(t, map[string]string{"config:get": fmt.Sprintf(`{"key":"types.custom","value":%q}`, testCase.value)})
			code, stdout, stderr := test.run("bootstrap")
			if code != 0 || stdout != "Hub store ready: todo issue type enabled.\n" {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
			calls := test.calls()
			want := [][]string{
				{"--db", test.store, "--readonly", "--json", "config", "get", "types.custom"},
				{"--db", test.store, "--json", "config", "set", "types.custom", testCase.want},
			}
			if len(calls) != len(want) {
				t.Fatalf("calls = %#v", calls)
			}
			for index := range want {
				if !reflect.DeepEqual(calls[index].Args, want[index]) {
					t.Fatalf("call %d = %#v, want %#v", index, calls[index].Args, want[index])
				}
			}
			if _, err := os.Stat(test.config); err != nil {
				t.Fatalf("existing-store bootstrap did not ensure Hub config: %v", err)
			}
			assertNoViewerSignal(t, test)
		})
	}
}

func TestBootstrapExistingStoreIsIdempotent(t *testing.T) {
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"config:get": `{"key":"types.custom","value":"todo"}`})
	for attempt := 0; attempt < 2; attempt++ {
		code, stdout, stderr := test.run("bootstrap")
		if code != 0 || stdout != "Hub store ready: todo issue type already enabled.\n" {
			t.Fatalf("attempt %d: code = %d, stdout = %q, stderr = %q", attempt, code, stdout, stderr)
		}
	}
	for _, call := range test.calls() {
		if fakeCommandKey(call.Args) != "config:get" {
			t.Fatalf("idempotent bootstrap wrote configuration: %#v", call)
		}
	}
	if _, err := os.Stat(test.config); err != nil {
		t.Fatalf("idempotent bootstrap did not ensure Hub config: %v", err)
	}
}

func TestBootstrapExistingStoreFailuresNeverCleanStore(t *testing.T) {
	tests := []struct {
		name            string
		responses       map[string]string
		exitCodes       map[string]int
		malformedConfig bool
		message         string
		calls           int
	}{
		{name: "query failure", exitCodes: map[string]int{"config:get": 7}, message: "reading existing custom issue types", calls: 1},
		{name: "malformed query", responses: map[string]string{"config:get": `{`}, message: "decoding custom issue types", calls: 1},
		{name: "config failure", responses: map[string]string{"config:get": `{"key":"types.custom","value":"review"}`}, malformedConfig: true, message: "loading hub config", calls: 1},
		{name: "set failure", responses: map[string]string{"config:get": `{"key":"types.custom","value":"review"}`}, exitCodes: map[string]int{"config:set": 8}, message: "enabling todo issue type", calls: 2},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			marker := filepath.Join(test.store, "preserve-me")
			if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			if testCase.malformedConfig {
				if err := os.MkdirAll(filepath.Dir(test.config), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(test.config, []byte("not: [valid"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			setResponses(t, testCase.responses)
			setExitCodes(t, testCase.exitCodes)
			code, _, stderr := test.run("bootstrap")
			if code != 1 || !strings.Contains(stderr, testCase.message) {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "unchanged" {
				t.Fatalf("existing store was cleaned: data=%q err=%v", data, err)
			}
			calls := test.calls()
			if len(calls) != testCase.calls {
				t.Fatalf("calls = %#v", calls)
			}
			for _, call := range calls {
				if fakeCommandKey(call.Args) == "init" {
					t.Fatalf("existing store was reinitialized: %#v", calls)
				}
			}
			assertNoViewerSignal(t, test)
		})
	}
}

func TestTodoCreateRequiresCapabilityBeforeTargetResolution(t *testing.T) {
	tests := []struct {
		name       string
		arguments  []string
		json       bool
		unreadable bool
	}{
		{name: "omitted target", arguments: []string{"create", "Capture", "--type", "todo"}},
		{name: "contextless human", arguments: []string{"create", "Capture", "--type", "todo", "--contextless"}, json: false},
		{name: "explicit JSON", arguments: []string{"--json", "create", "Capture", "--type", "todo", "--context", "unavailable-target"}, json: true},
		{name: "unreadable capability", arguments: []string{"create", "Capture", "--type", "todo"}, unreadable: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, true)
			setResponses(t, map[string]string{"config:get": `{"key":"types.custom","value":""}`})
			if testCase.unreadable {
				setExitCodes(t, map[string]int{"config:get": 7})
			}
			code, _, stderr := test.run(testCase.arguments...)
			if code != 1 || !strings.Contains(stderr, "run 'wbd bootstrap' to enable it") {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			if testCase.json && (!strings.Contains(stderr, `"code":"invalid_request"`) || !json.Valid([]byte(stderr))) {
				t.Fatalf("JSON error = %q", stderr)
			}
			calls := test.calls()
			if len(calls) != 1 || fakeCommandKey(calls[0].Args) != "config:get" {
				t.Fatalf("todo rejection reached registration or mutation: %#v", calls)
			}
			if !slices.Contains(calls[0].Args, "--readonly") {
				t.Fatalf("todo capability query was not read-only: %#v", calls[0].Args)
			}
			if _, err := os.Stat(test.config); !os.IsNotExist(err) {
				t.Fatalf("todo rejection registered current context: %v", err)
			}
			assertNoViewerSignal(t, test)
		})
	}
}

func TestHelpExplainsIssueTypesAndTargetingWithoutStore(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			test := newAppTestWithoutStore(t)
			code, stdout, stderr := test.run(flag)
			if code != 0 || stderr != "" {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			for _, want := range []string{"Capture something not yet concrete project work", "--contextless", "--from-todo", "cannot own commit correlations", "link", "unlink", "exact full SHA", "idempotent"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("help does not contain %q:\n%s", want, stdout)
				}
			}
			if calls := test.calls(); len(calls) != 0 {
				t.Fatalf("help invoked child commands: %#v", calls)
			}
		})
	}
}

func TestEveryCommandHelpWorksWithoutStoreOrSideEffects(t *testing.T) {
	for _, path := range commandOrder {
		for _, flag := range []string{"--help", "-h"} {
			t.Run(strings.ReplaceAll(path, " ", "_")+flag, func(t *testing.T) {
				test := newAppTestWithoutStore(t)
				arguments := append(strings.Split(path, " "), flag)
				code, stdout, stderr := test.run(arguments...)
				if code != 0 || stderr != "" {
					t.Fatalf("code = %d, stderr = %q", code, stderr)
				}
				spec := commandSpecs[path]
				if !strings.Contains(stdout, "Usage: "+spec.usage) || !strings.Contains(stdout, "--help") {
					t.Fatalf("help for %q does not reflect specification:\n%s", path, stdout)
				}
				for _, option := range spec.options {
					if !strings.Contains(stdout, option.name) {
						t.Errorf("help for %q omitted %s:\n%s", path, option.name, stdout)
					}
				}
				if calls := test.calls(); len(calls) != 0 {
					t.Fatalf("help invoked child commands: %#v", calls)
				}
				if _, err := os.Stat(test.store); !os.IsNotExist(err) {
					t.Fatalf("help created or inspected a usable store: %v", err)
				}
				if _, err := os.Stat(test.config); !os.IsNotExist(err) {
					t.Fatalf("help changed Hub configuration: %v", err)
				}
				assertNoViewerSignal(t, test)
			})
		}
	}
}

func TestHelpPrecedesArgumentsJSONAndExecutableChecks(t *testing.T) {
	tests := [][]string{
		{"--json", "list", "--help"},
		{"create", "ignored title", "--bad-option", "-h"},
		{"dep", "add", "ignored-a", "ignored-b", "--help"},
	}
	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			test := newAppTestWithoutStore(t)
			t.Setenv("PATH", t.TempDir())
			code, stdout, stderr := test.run(arguments...)
			if code != 0 || stdout == "" || stderr != "" || len(test.calls()) != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q calls=%#v", code, stdout, stderr, test.calls())
			}
		})
	}
}

func TestCommandSpecificationDrivesValueOptionParsing(t *testing.T) {
	values := map[string]string{
		"--prefix": "item", "--description": "details", "--type": "task", "--priority": "2",
		"--assignee": "agent-7", "--labels": "team", "--context": "ctx:test", "--from-todo": "todo-1",
		"--title": "title", "--status": "open", "--label": "team", "--limit": "20", "--add-label": "team",
		"--reason": "done", "--author": "agent-7", "--file": "notes.txt",
	}
	for _, path := range commandOrder {
		spec := commandSpecs[path]
		for _, option := range spec.options {
			if option.value == "" || path == "bootstrap" {
				continue
			}
			value, ok := values[option.name]
			if !ok {
				t.Fatalf("test needs a valid value for %s %s", path, option.name)
			}
			flag, got, consumed, matched, err := optionValueFor(path, option.name, []string{value})
			if err != nil || !matched || consumed != 1 || flag != option.name || got != value {
				t.Errorf("%s %s parsed as flag=%q value=%q consumed=%d matched=%v err=%v", path, option.name, flag, got, consumed, matched, err)
			}
		}
	}

	flag, value, _, matched, err := optionValueFor("update", "--assignee=", nil)
	if err != nil || !matched || flag != "--assignee" || value != "" {
		t.Fatalf("clear-assignee specification not honored: flag=%q value=%q matched=%v err=%v", flag, value, matched, err)
	}
}

func TestDocumentedBooleanOptionsAreAcceptedByParser(t *testing.T) {
	tests := [][]string{
		{"create", "Title", "--contextless", "--json"},
		{"new", "Title", "--contextless", "--json"},
		{"replace", "old-1", "--contextless", "--json"},
		{"list", "--ready", "--all-contexts", "--json"},
		{"show", "work-1", "--json"},
		{"update", "work-1", "--title", "Title", "--json"},
		{"dep", "add", "work-1", "work-2", "--json"},
		{"dep", "remove", "work-1", "work-2", "--json"},
		{"close", "work-1", "--json"},
		{"reopen", "work-1", "--json"},
		{"comments", "add", "work-1", "A comment", "--json"},
		{"compatibility", "--json"},
	}
	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			if _, err := parse(arguments); err != nil {
				t.Fatalf("documented invocation rejected: %v", err)
			}
		})
	}
}

func TestSingularCommentCommandIsNotSupported(t *testing.T) {
	if _, err := parse([]string{"comment", "work-1", "Nope"}); err == nil {
		t.Fatal("singular comment command was accepted")
	}
}

func TestCommentsAddValidatesIssueBeforeMutation(t *testing.T) {
	t.Run("forwards comment and signals Viewer", func(t *testing.T) {
		test := newAppTest(t, true)
		context := contextForTest(t, test.repository)
		writeHubConfig(t, test, map[string]string{context: test.repository})
		setResponses(t, map[string]string{
			"show:work-1": fmt.Sprintf(`[{"id":"work-1","status":"open","issue_type":"task","labels":[%q]}]`, context),
		})

		code, _, stderr := test.run("--json", "comments", "add", "work-1", "Needs review", "--author", "agent-7")
		if code != 0 || stderr != "" {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		calls := test.calls()
		if len(calls) != 2 {
			t.Fatalf("calls = %#v", calls)
		}
		want := []string{"--db", test.store, "--json", "comments", "add", "work-1", "Needs review", "--author", "agent-7"}
		if !reflect.DeepEqual(calls[1].Args, want) {
			t.Fatalf("comment args = %#v, want %#v", calls[1].Args, want)
		}
		if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
			t.Fatalf("successful comment did not signal Viewer: %v", err)
		}
	})

	t.Run("rejects invalid context before mutation", func(t *testing.T) {
		test := newAppTest(t, true)
		writeHubConfig(t, test, map[string]string{"ctx:registered": test.repository})
		setResponses(t, map[string]string{
			"show:work-1": `[{"id":"work-1","status":"open","issue_type":"task","labels":["ctx:missing"]}]`,
		})

		code, _, stderr := test.run("--json", "comments", "add", "work-1", "Nope")
		if code != 1 || !strings.Contains(stderr, `"code":"unregistered_context"`) {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if calls := test.calls(); len(calls) != 1 {
			t.Fatalf("invalid comment mutated store: %#v", calls)
		}
		assertNoViewerSignal(t, test)
	})
}

func TestAssigneeJSONSurvivesScopedAndAllContextReads(t *testing.T) {
	for _, arguments := range [][]string{{"list", "--json"}, {"list", "--all-contexts", "--json"}, {"show", "work-1", "--json"}} {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			test := newAppTest(t, true)
			key := "list"
			if arguments[0] == "show" {
				key = "show:work-1"
			}
			setResponses(t, map[string]string{key: `[{"id":"work-1","assignee":"agent-7","owner":"team-owner","created_by":"audit-actor"}]`})
			code, stdout, stderr := test.run(arguments...)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			for _, field := range []string{`"assignee":"agent-7"`, `"owner":"team-owner"`, `"created_by":"audit-actor"`} {
				if !strings.Contains(stdout, field) {
					t.Errorf("output dropped %s: %s", field, stdout)
				}
			}
		})
	}
}

func TestRejectsRoutingOverridesBeforeDelegation(t *testing.T) {
	tests := [][]string{
		{"create", "title", "--db", "/tmp/other"},
		{"list", "--db=/tmp/other"},
		{"show", "bead-1", "--config", "other"},
		{"update", "bead-1", "--status", "open", "--db", "other"},
		{"dep", "add", "bead-1", "bead-2", "--db=other"},
		{"close", "bead-1", "--db", "other"},
		{"link", "bead-1", "--hub-config"},
	}
	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			test := newAppTest(t, true)
			code, _, stderr := test.run(arguments...)
			if code != 1 || stderr == "" {
				t.Fatalf("run code = %d, stderr = %q", code, stderr)
			}
			if calls := test.calls(); len(calls) != 0 {
				t.Fatalf("rejected route delegated: %#v", calls)
			}
		})
	}
}

func TestRejectsWrapperOwnedLabelsAndUnsafeValues(t *testing.T) {
	tests := [][]string{
		{"create", "title", "--labels", "ctx:other"},
		{"update", "bead-1", "--add-label", " ctx:other"},
		{"create", "title", "--labels", `quoted"label`},
		{"list", "--limit", "1001"},
		{"update", "bead-1", "--status", "closed"},
		{"create", "title", "--assignee", "bad\nidentity"},
		{"update", "bead-1", "--assignee", "bad\tidentity"},
		{"show", "-bead-1"},
	}
	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			test := newAppTest(t, true)
			code, _, _ := test.run(arguments...)
			if code != 1 || len(test.calls()) != 0 {
				t.Fatalf("unsafe invocation code = %d, calls = %#v", code, test.calls())
			}
		})
	}
}

func TestAssigneeValidationRejectsPseudoIdentitiesBeforeDelegation(t *testing.T) {
	invalid := []struct {
		name  string
		value string
	}{
		{name: "spaces", value: "   "},
		{name: "unicode whitespace", value: "\u2003\u00a0"},
		{name: "escape", value: "agent\x1b[31m"},
		{name: "delete", value: "agent\x7f"},
	}
	for _, command := range []string{"create", "update"} {
		for _, testCase := range invalid {
			t.Run(command+"_"+testCase.name, func(t *testing.T) {
				arguments := []string{command, "work-1", "--assignee", testCase.value}
				if command == "create" {
					arguments[1] = "Title"
				}
				if _, err := parse(arguments); err == nil {
					t.Fatalf("parse accepted assignee %q", testCase.value)
				}

				test := newAppTest(t, true)
				code, _, stderr := test.run(arguments...)
				if code != 1 || stderr == "" {
					t.Fatalf("code = %d, stderr = %q", code, stderr)
				}
				if calls := test.calls(); len(calls) != 0 {
					t.Fatalf("invalid assignee delegated to child: %#v", calls)
				}
			})
		}
	}

	request, err := parse([]string{"update", "work-1", "--assignee="})
	if err != nil {
		t.Fatalf("clear assignee rejected: %v", err)
	}
	if !reflect.DeepEqual(request.args, []string{"--assignee", ""}) {
		t.Fatalf("clear assignee args = %#v", request.args)
	}
}

func TestChildExitCodeIsPropagated(t *testing.T) {
	test := newAppTest(t, true)
	t.Setenv("WBD_CHILD_EXIT", "37")
	code, _, stderr := test.run("show", "bead-1")
	if code != 37 {
		t.Fatalf("run code = %d, want 37", code)
	}
	if stderr != "" {
		t.Fatalf("wrapper added stderr for child failure: %q", stderr)
	}
}

func TestSuccessfulMutationsSignalViewer(t *testing.T) {
	for _, arguments := range [][]string{
		{"register"},
		{"configure"},
		{"list"},
		{"create", "New issue"},
		{"update", "bead-1", "--status", "in_progress"},
		{"dep", "add", "bead-1", "bead-2"},
		{"close", "bead-1", "--reason", "done"},
		{"reopen", "bead-1", "--reason", "again"},
	} {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			test := newAppTest(t, true)
			if code, _, stderr := test.run(arguments...); code != 0 {
				t.Fatalf("run code = %d, stderr = %q", code, stderr)
			}
			if data, err := os.ReadFile(hub.ChangeSignalPath(test.app.paths)); err != nil || len(data) == 0 {
				t.Fatalf("Viewer signal missing: data=%q err=%v", data, err)
			}
		})
	}
}

func TestUnchangedRegistrationDoesNotAdvanceViewerSignal(t *testing.T) {
	test := newAppTest(t, true)
	if code, _, stderr := test.run("register"); code != 0 {
		t.Fatalf("first register code = %d, stderr = %q", code, stderr)
	}
	path := hub.ChangeSignalPath(test.app.paths)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := test.run("register"); code != 0 {
		t.Fatalf("second register code = %d, stderr = %q", code, stderr)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("unchanged registration advanced signal: first=%q second=%q", first, second)
	}
}

func TestReadsAndFailedMutationsDoNotSignalViewer(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		arguments []string
		childExit string
	}{
		{name: "show", arguments: []string{"show", "bead-1"}},
		{name: "failed update", arguments: []string{"update", "bead-1", "--status", "open"}, childExit: "9"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, true)
			t.Setenv("WBD_CHILD_EXIT", testCase.childExit)
			test.run(testCase.arguments...)
			if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); !os.IsNotExist(err) {
				t.Fatalf("unexpected Viewer signal: %v", err)
			}
		})
	}
}

type appTest struct {
	t          *testing.T
	home       string
	store      string
	config     string
	repository string
	callsPath  string
	app        *app
}

func newAppTest(t *testing.T, repository bool) *appTest {
	t.Helper()
	test := newAppTestWithoutStore(t)
	if err := os.MkdirAll(test.store, 0o700); err != nil {
		t.Fatal(err)
	}
	if repository {
		test.repository = newGitRepository(t)
		test.app.dir = test.repository
	} else {
		test.repository = t.TempDir()
		test.app.dir = test.repository
	}
	return test
}

func newAppTestWithoutStore(t *testing.T) *appTest {
	t.Helper()
	home := t.TempDir()
	bin := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bd", "bv"} {
		if err := os.Symlink(executable, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	callsPath := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_FAKE_CHILD", "1")
	t.Setenv("WBD_CALLS", callsPath)
	t.Setenv("WBD_CHILD_EXIT", "0")
	application, err := newApp(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return &appTest{
		t:         t,
		home:      home,
		store:     filepath.Join(home, ".local", "share", "beads", "hub", ".beads"),
		config:    filepath.Join(home, ".config", "bv", "hub.yaml"),
		callsPath: callsPath,
		app:       application,
	}
}

func (test *appTest) run(arguments ...string) (int, string, string) {
	test.t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	test.app.stdout = stdout
	test.app.stderr = stderr
	return test.app.run(arguments), stdout.String(), stderr.String()
}

func (test *appTest) calls() []childCall {
	test.t.Helper()
	file, err := os.Open(test.callsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		test.t.Fatal(err)
	}
	defer file.Close()
	var calls []childCall
	decoder := json.NewDecoder(file)
	for {
		var call childCall
		err := decoder.Decode(&call)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			test.t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	git(t, repository, "init")
	git(t, repository, "config", "user.name", "WBD Test")
	git(t, repository, "config", "user.email", "wbd@example.test")
	readme := filepath.Join(repository, "README")
	if err := os.WriteFile(readme, []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repository, "add", "README")
	git(t, repository, "commit", "-m", "initial")
	git(t, repository, "remote", "add", "origin", "git@github.com:Example/Project_Name.git")
	canonical, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func git(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	args := append([]string{"-C", directory}, arguments...)
	command := exec.Command("git", args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func contextForTest(t *testing.T, repository string) string {
	t.Helper()
	context, err := hub.Context(repository)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func assertIsolatedEnvironment(t *testing.T, environment map[string]string, store string, viewer bool) {
	t.Helper()
	for name := range isolatedVariables {
		if name == "BEADS_DIR" {
			continue
		}
		if _, exists := environment[name]; exists {
			t.Errorf("isolated environment retained %s=%q", name, environment[name])
		}
	}
	if environment["BEADS_DIR"] != store {
		t.Errorf("BEADS_DIR = %q, want %q", environment["BEADS_DIR"], store)
	}
	if viewer {
		if environment["BV_NO_GITIGNORE"] != "1" {
			t.Errorf("BV_NO_GITIGNORE = %q, want 1", environment["BV_NO_GITIGNORE"])
		}
	} else if environment["BV_NO_GITIGNORE"] != os.Getenv("BV_NO_GITIGNORE") {
		t.Errorf("bd changed BV_NO_GITIGNORE from %q to %q", os.Getenv("BV_NO_GITIGNORE"), environment["BV_NO_GITIGNORE"])
	}
}

type testConfig struct {
	Repositories map[string]struct {
		Path string `json:"path"`
	} `json:"repositories"`
}

func readTestConfig(t *testing.T, path string) testConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config testConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func writeHubConfig(t *testing.T, test *appTest, repositories map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(test.config), 0o700); err != nil {
		t.Fatal(err)
	}
	wire := map[string]any{
		"version": 1,
		"store":   test.store,
		"ledger":  test.app.paths.Ledger,
	}
	repositoryWire := make(map[string]map[string]string, len(repositories))
	for context, path := range repositories {
		repositoryWire[context] = map[string]string{"path": path}
	}
	wire["repositories"] = repositoryWire
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(test.config, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func setResponses(t *testing.T, responses map[string]string) {
	t.Helper()
	data, err := json.Marshal(responses)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WBD_RESPONSES", string(data))
}

func setExitCodes(t *testing.T, codes map[string]int) {
	t.Helper()
	data, err := json.Marshal(codes)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WBD_EXIT_CODES", string(data))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func assertNoViewerSignal(t *testing.T, test *appTest) {
	t.Helper()
	if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); !os.IsNotExist(err) {
		t.Fatalf("unexpected Viewer signal: %v", err)
	}
}
