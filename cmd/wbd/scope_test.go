package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScopeParserExposesPublicSubcommands(t *testing.T) {
	for _, arguments := range [][]string{
		{"scope", "create", "scope-work", "Work", "--activate", "--json"},
		{"scope", "list", "--json"},
		{"scope", "show", "work"},
		{"scope", "active", "--json"},
		{"scope", "activate", "work"},
		{"scope", "deactivate", "--json"},
		{"scope", "add", "bead-1", "--scope", "work"},
		{"scope", "remove", "--label", "work", "--scope", "work"},
		{"scope", "move", "bead-1", "--source-scope", "old", "--target-scope", "new"},
		{"backlog", "list", "--limit", "10", "--cursor", "opaque:/+= token", "--json"},
	} {
		if _, err := parse(arguments); err != nil {
			t.Errorf("parse(%v) = %v", arguments, err)
		}
	}
}

func TestScopeOmittedTargetsResolveActiveScope(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "add", args: []string{"scope", "add", "bead-1", "--json"}},
		{name: "remove", args: []string{"scope", "remove", "bead-1", "--json"}},
		{name: "move", args: []string{"scope", "move", "bead-1", "bead-2", "--json"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, testCase.name != "move")
			setResponses(t, map[string]string{
				"scope:active": `{"id":"active"}`,
				"scope:move":   `{}`,
				"scope:show":   `{"issues":[{"id":"bead-1"}]}`,
				"list":         `[{"id":"bead-1"}]`,
			})
			code, _, stderr := test.run(testCase.args...)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			calls := test.calls()
			if testCase.name == "move" {
				if len(calls) != 2 || !reflect.DeepEqual(calls[0].Args, []string{"--db", test.store, "--json", "scope", "active"}) ||
					!reflect.DeepEqual(calls[1].Args, []string{"--db", test.store, "--json", "scope", "move", "active", "active", "bead-1", "bead-2"}) {
					t.Fatalf("calls=%#v", calls)
				}
			} else {
				want := [][]string{{"--db", test.store, "--json", "scope", "active"}, {"--db", test.store, "--json", "list"}}
				if testCase.name == "remove" {
					want = append(want, []string{"--db", test.store, "--json", "scope", "show", "active"})
				}
				want = append(want, []string{"--db", test.store, "--json", "scope", testCase.name, "active", "bead-1"})
				if len(calls) != len(want) {
					t.Fatalf("calls=%#v", calls)
				}
				for index, expected := range want {
					if index == 1 && testCase.name == "remove" {
						continue
					}
					if index == 1 && testCase.name == "add" {
						if !reflect.DeepEqual(calls[index].Args[:4], []string{"--db", test.store, "--json", "list"}) {
							t.Fatalf("list call=%#v", calls[index].Args)
						}
						continue
					}
					if index == 2 && testCase.name == "remove" {
						if !reflect.DeepEqual(calls[index].Args[:4], []string{"--db", test.store, "--json", "list"}) {
							t.Fatalf("list call=%#v", calls[index].Args)
						}
						continue
					}
					if !reflect.DeepEqual(calls[index].Args, expected) {
						t.Fatalf("call[%d]=%#v want %#v", index, calls[index].Args, expected)
					}
				}
			}
			assertViewerSignal(t, test)
		})
	}
}

func TestScopeExplicitMoveDoesNotReadOrSignalTwice(t *testing.T) {
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"scope:move": `{"moved":true}`})
	code, stdout, stderr := test.run("scope", "move", "bead-1", "--source-scope", "old", "--target-scope", "new", "--json")
	if code != 0 || stdout != `{"moved":true}` || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"--db", test.store, "--json", "scope", "move", "old", "new", "bead-1"}) {
		t.Fatalf("calls=%#v", calls)
	}
	assertViewerSignal(t, test)
}

func TestScopeExplicitIDResolvesBeforeOneMutation(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{"list": `[{"id":"bead-1"},{"id":"bead-2"}]`, "scope:add": `{"added":2}`})
	code, stdout, stderr := test.run("scope", "add", "bead-1", "bead-2", "--scope", "scope-a", "--json")
	if code != 0 || stdout != `{"operation":"add","matched":2,"changed":2}`+"\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	context := contextForTest(t, test.repository)
	wantList := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--unscoped", "--limit", "0", "--label", context, "--id", "bead-1,bead-2"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "add", "scope-a", "bead-1", "bead-2"}
	if len(calls) != 2 || !reflect.DeepEqual(calls[0].Args, wantList) || !reflect.DeepEqual(calls[1].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
	assertViewerSignal(t, test)
}

func TestScopeSemanticParserRequiresExactlyOneTarget(t *testing.T) {
	for _, arguments := range [][]string{
		{"scope", "add", "one", "--label", "team"},
		{"scope", "remove", "--epic", "epic-1", "--label", "team"},
		{"scope", "add", "--label", "ctx:repo"},
	} {
		if _, err := parse(arguments); err == nil {
			t.Errorf("parse(%v) unexpectedly succeeded", arguments)
		}
	}
	if _, err := parse([]string{"scope", "add", "one", "two"}); err != nil {
		t.Fatalf("explicit ID collection rejected: %v", err)
	}
}

func TestScopeAddLabelUsesCurrentRepositoryFiltersAndOneMultiIDMutation(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{"list": `[{"id":"b"},{"id":"a"}]`, "scope:add": `{"added":2}`})
	code, stdout, stderr := test.run("scope", "add", "--label", "team", "--status", "open,blocked", "--type", "task", "--scope", "work", "--json")
	if code != 0 || stdout != `{"operation":"add","matched":2,"changed":2}`+"\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	context := contextForTest(t, test.repository)
	calls := test.calls()
	wantList := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--unscoped", "--limit", "0", "--label", context, "--label", "team", "--status", "open,blocked", "--type", "task"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "add", "work", "a", "b"}
	if len(calls) != 2 || !reflect.DeepEqual(calls[0].Args, wantList) || !reflect.DeepEqual(calls[1].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestScopeSemanticParserSupportsBacklogContextSelection(t *testing.T) {
	request, err := parse([]string{"scope", "add", "--label", "team", "--context", "ctx:a", "--context", "ctx:b", "--contextless", "--status", "open,blocked", "--type", "task", "--scope", "work", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.scopeContexts, []string{"ctx:a", "ctx:b"}) || !request.scopeContextless ||
		request.scopeStatus != "open,blocked" || request.scopeType != "task" {
		t.Fatalf("parsed scope request = %#v", request)
	}
}

func TestScopeAddEpicUsesExplicitParentChildRelationships(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{"list": `[{"id":"child-1","parent":"epic-1"},{"id":"grandchild-1","parent":"child-1"},{"id":"dotted-1"}]`, "scope:add": `{}`})
	code, _, stderr := test.run("scope", "add", "--epic", "epic-1", "--scope", "work", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	context := contextForTest(t, test.repository)
	calls := test.calls()
	wantList := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--unscoped", "--limit", "0", "--label", context}
	wantRelationships := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--all", "--include-all-types", "--limit", "0"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "add", "work", "child-1", "grandchild-1"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[0].Args, wantList) || !reflect.DeepEqual(calls[1].Args, wantRelationships) || !reflect.DeepEqual(calls[2].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestScopeAddEpicIntersectsSelectedCandidatesWithGlobalDescendants(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:selected": "/selected", "ctx:other": "/other"})
	setResponses(t, map[string]string{"list": `[{"id":"child-1","parent":"ancestor-1","labels":["ctx:selected"]},{"id":"ancestor-1","labels":["ctx:other"]},{"id":"unrelated","labels":["ctx:selected"]}]`, "scope:add": `{}`})
	code, stdout, stderr := test.run("scope", "add", "--epic", "ancestor-1", "--context", "ctx:selected", "--status", "open", "--type", "task", "--scope", "work", "--json")
	if code != 0 || stdout != `{"operation":"add","matched":1,"changed":1}`+"\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	wantCandidates := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--unscoped", "--limit", "0", "--label-any", "ctx:selected", "--status", "open", "--type", "task"}
	wantParents := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--all", "--include-all-types", "--limit", "0"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "add", "work", "child-1"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[0].Args, wantCandidates) || !reflect.DeepEqual(calls[1].Args, wantParents) || !reflect.DeepEqual(calls[2].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestScopeRemoveResolvesOnlySelectedScopeMembers(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{"scope:show": `{"issues":[{"id":"b"},{"id":"a"},{"id":"outside"}]}`, "list": `[{"id":"b"},{"id":"a"}]`, "scope:remove": `{"removed":2}`})
	code, stdout, stderr := test.run("scope", "remove", "--label", "team", "--status", "open", "--type", "task", "--scope", "work", "--json")
	if code != 0 || stdout != `{"operation":"remove","matched":2,"changed":2}`+"\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	context := contextForTest(t, test.repository)
	calls := test.calls()
	wantList := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--id", "a,b,outside", "--limit", "0", "--label", context, "--label", "team", "--status", "open", "--type", "task"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "remove", "work", "a", "b"}
	if len(calls) != 3 || fakeCommandKey(calls[0].Args) != "scope:show" || !reflect.DeepEqual(calls[1].Args, wantList) || !reflect.DeepEqual(calls[2].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestScopeSemanticMutationUsesSelectedContextsAndContextless(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{"ctx:a": "/a", "ctx:b": "/b"})
	setResponses(t, map[string]string{"list": `[{"id":"b"},{"id":"a"}]`, "scope:add": `{}`})
	code, stdout, stderr := test.run("scope", "add", "--label", "team", "--context", "ctx:a", "--context", "ctx:b", "--contextless", "--status", "open,blocked", "--type", "task", "--scope", "work", "--json")
	if code != 0 || stdout != `{"operation":"add","matched":2,"changed":2}`+"\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	wantList := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--unscoped", "--limit", "0", "--label-any", "ctx:a,ctx:b", "--or-no-label-prefix", "ctx:", "--label", "team", "--status", "open,blocked", "--type", "task"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "add", "work", "a", "b"}
	if len(calls) != 2 || !reflect.DeepEqual(calls[0].Args, wantList) || !reflect.DeepEqual(calls[1].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestScopeRemoveSemanticMutationSupportsContextlessSelection(t *testing.T) {
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"scope:show": `{"issues":[{"id":"b"},{"id":"a"}]}`, "list": `[{"id":"b"},{"id":"a"}]`, "scope:remove": `{}`})
	code, stdout, stderr := test.run("scope", "remove", "--label", "team", "--contextless", "--status", "open", "--type", "todo", "--scope", "work", "--json")
	if code != 0 || stdout != `{"operation":"remove","matched":2,"changed":2}`+"\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	wantList := []string{"--db", test.store, "--json", "list", "--no-directory-labels", "--id", "a,b", "--limit", "0", "--or-no-label-prefix", "ctx:", "--label", "team", "--status", "open", "--type", "todo"}
	wantMutation := []string{"--db", test.store, "--json", "scope", "remove", "work", "a", "b"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[1].Args, wantList) || !reflect.DeepEqual(calls[2].Args, wantMutation) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestScopeWrappersAgainstProductionBackend(t *testing.T) {
	backend := os.Getenv("WBD_BACKEND_BD")
	if backend == "" {
		t.Skip("backend bd not provided; set WBD_BACKEND_BD to a compatible executable")
	}
	backend, err := filepath.Abs(backend)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backend); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("WBD_BACKEND_BD must name an executable bd binary: %s", backend)
	}

	bin := t.TempDir()
	wbd := filepath.Join(bin, "wbd")
	build := exec.Command("go", "build", "-o", wbd, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build wbd: %v\n%s", err, output)
	}
	if err := os.Symlink(backend, filepath.Join(bin, "bd")); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	store := filepath.Join(home, ".local", "share", "beads", "hub", ".beads")
	environment := append(os.Environ(), "HOME="+home, "BEADS_DIR="+store, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := t.TempDir()
	// The minimal CGO-disabled bd build uses its supported proxied store mode.
	if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
		t.Fatal(err)
	}
	runIntegrationCommand(t, environment, filepath.Dir(store), filepath.Join(bin, "bd"), "init", "--proxied-server")

	created := runIntegrationCommand(t, environment, workspace, wbd, "scope", "create", "scope-a", "Scope A", "--activate", "--json")
	var scope struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.stdout, &scope); err != nil || scope.ID != "scope-a" {
		t.Fatalf("scope create: err=%v stdout=%s stderr=%s", err, created.stdout, created.stderr)
	}
	active := runIntegrationCommand(t, environment, workspace, wbd, "scope", "active", "--json")
	if err := json.Unmarshal(active.stdout, &scope); err != nil || scope.ID != "scope-a" {
		t.Fatalf("scope active: err=%v stdout=%s stderr=%s", err, active.stdout, active.stderr)
	}

	issue := integrationIssueID(t, runIntegrationCommand(t, environment, workspace, filepath.Join(bin, "bd"), "--db", store, "create", "scope wrapper issue", "--json").stdout)
	runIntegrationCommand(t, environment, workspace, wbd, "scope", "add", issue, "--json")
	backlog := runIntegrationCommand(t, environment, workspace, wbd, "backlog", "list", "--limit", "10", "--json")
	var page any
	if err := json.Unmarshal(backlog.stdout, &page); err != nil {
		t.Fatalf("backlog list: err=%v stdout=%s stderr=%s", err, backlog.stdout, backlog.stderr)
	}
}

func TestScopeReadsAndCreatePreserveStableBackendOutput(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		response string
		wantArgs []string
		signal   bool
	}{
		{name: "list human", args: []string{"scope", "list"}, response: "NAME\nwork\n", wantArgs: []string{"scope", "list"}},
		{name: "show JSON", args: []string{"scope", "show", "scope-work", "--json"}, response: `{"id":"scope-work"}`, wantArgs: []string{"--json", "scope", "show", "scope-work"}},
		{name: "active JSON", args: []string{"scope", "active", "--json"}, response: `{"id":"scope-work"}`, wantArgs: []string{"--json", "scope", "active"}},
		{name: "activate human", args: []string{"scope", "activate", "scope-work"}, response: "Activated scope: scope-work\n", wantArgs: []string{"scope", "activate", "scope-work"}, signal: true},
		{name: "deactivate human", args: []string{"scope", "deactivate"}, response: "Deactivated scope\n", wantArgs: []string{"scope", "deactivate"}, signal: true},
		{name: "create human", args: []string{"scope", "create", "scope-work", "Work", "--activate"}, response: "Created scope: scope-work\n", wantArgs: []string{"scope", "create", "scope-work", "Work", "--activate"}, signal: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			setResponses(t, map[string]string{"scope:" + testCase.args[1]: testCase.response})
			code, stdout, stderr := test.run(testCase.args...)
			if code != 0 || stdout != testCase.response || stderr != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			calls := test.calls()
			want := append([]string{"--db", test.store}, testCase.wantArgs...)
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
				t.Fatalf("calls=%#v want=%#v", calls, want)
			}
			if testCase.signal {
				assertViewerSignal(t, test)
			} else {
				assertNoViewerSignal(t, test)
			}
		})
	}
}

func TestFailedScopeMutationDoesNotSignal(t *testing.T) {
	test := newAppTest(t, true)
	setResponses(t, map[string]string{"scope:active": `{"id":"active"}`, "list": `[{"id":"bead-1"}]`})
	setExitCodes(t, map[string]int{"scope:add": 9})
	code, stdout, stderr := test.run("--json", "scope", "add", "bead-1")
	if code != 9 || stdout != "" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if calls := test.calls(); len(calls) != 3 {
		t.Fatalf("calls=%#v", calls)
	}
	assertNoViewerSignal(t, test)
}

func TestBacklogListForwardsOpaqueCursorAndOutput(t *testing.T) {
	test := newAppTest(t, false)
	response := `{"issues":[{"id":"bead-1","title":"Backlog","status":"open","priority":2,"issue_type":"task","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z"}],"pagination":{"limit":2,"has_more":true,"next_cursor":"opaque:/+= token"}}
`
	setResponses(t, map[string]string{"list": response})
	code, stdout, stderr := test.run("backlog", "list", "--limit", "2", "--cursor", "opaque:/+= token", "--json")
	wantOutput := `{"issues":[{"id":"bead-1","title":"Backlog","description":"","status":"open","priority":2,"issue_type":"task","assignee":"","labels":[],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z","closed_at":null}],"pagination":{"limit":2,"has_more":true,"next_cursor":"opaque:/+= token"}}
`
	if code != 0 || stdout != wantOutput || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	want := []string{"--db", test.store, "--json", "list", "--unscoped", "--sort", "updated", "--paginate", "--limit", "2", "--cursor", "opaque:/+= token"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls=%#v want=%#v", calls, want)
	}
	assertNoViewerSignal(t, test)
}

func TestBacklogParserSupportsRepeatableContextAndBoundedFilters(t *testing.T) {
	request, err := parse([]string{"backlog", "list", "--context", "ctx:a", "--context", "ctx:b", "--contextless", "--filter", "opaque title", "--status", "open,blocked", "--type", "task", "--sort", "priority-asc", "--limit", "2", "--cursor", "opaque:/+= token", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.backlogContexts, []string{"ctx:a", "ctx:b"}) || !request.backlogContextless || request.backlogFilter != "opaque title" ||
		request.backlogStatus != "open,blocked" || request.backlogType != "task" || request.backlogSort != "priority-asc" ||
		request.backlogLimit != 2 || request.backlogCursor != "opaque:/+= token" {
		t.Fatalf("parsed backlog request = %#v", request)
	}
}

func TestBacklogListValidatesContextsBeforeBackendAndTranslatesFilters(t *testing.T) {
	t.Run("unknown context is rejected before delegation", func(t *testing.T) {
		test := newAppTest(t, false)
		writeHubConfig(t, test, map[string]string{"ctx:known": "/known"})
		code, _, stderr := test.run("backlog", "list", "--context", "ctx:missing", "--limit", "1", "--json")
		if code != 1 || !strings.Contains(stderr, "ctx:missing") || !strings.Contains(stderr, "not registered") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if calls := test.calls(); len(calls) != 0 {
			t.Fatalf("unknown context was delegated: %#v", calls)
		}
	})

	t.Run("filters are delegated before pagination", func(t *testing.T) {
		test := newAppTest(t, false)
		writeHubConfig(t, test, map[string]string{"ctx:a": "/a", "ctx:b": "/b"})
		setResponses(t, map[string]string{"list": `{"issues":[],"pagination":{"limit":2,"has_more":false}}`})
		code, stdout, stderr := test.run("backlog", "list", "--context", "ctx:a", "--context", "ctx:b", "--contextless", "--filter", "opaque title", "--status", "open,blocked", "--type", "task", "--sort", "priority-asc", "--limit", "2", "--cursor", "opaque:/+= token", "--json")
		if code != 0 || stdout != `{"issues":[],"pagination":{"limit":2,"has_more":false}}`+"\n" || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		want := []string{"--db", test.store, "--json", "list", "--unscoped", "--label-any", "ctx:a,ctx:b", "--or-no-label-prefix", "ctx:", "--filter", "opaque title", "--status", "open,blocked", "--type", "task", "--sort", "priority", "--paginate", "--limit", "2", "--cursor", "opaque:/+= token"}
		if calls := test.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
			t.Fatalf("calls=%#v want=%#v", calls, want)
		}
	})

	t.Run("contextless alone uses only the prefix-negative selector", func(t *testing.T) {
		test := newAppTest(t, false)
		setResponses(t, map[string]string{"list": `{"issues":[],"pagination":{"limit":1,"has_more":false}}`})
		code, _, stderr := test.run("backlog", "list", "--contextless", "--limit", "1", "--json")
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		want := []string{"--db", test.store, "--json", "list", "--unscoped", "--or-no-label-prefix", "ctx:", "--sort", "updated", "--paginate", "--limit", "1"}
		if calls := test.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
			t.Fatalf("calls=%#v want=%#v", calls, want)
		}
	})
}

func TestBacklogListForwardsFilterBeforePagination(t *testing.T) {
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"list": `{"issues":[],"pagination":{"limit":2,"has_more":false}}`})
	code, _, stderr := test.run("backlog", "list", "--filter", " bv-123 ", "--limit", "2", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	want := []string{"--db", test.store, "--json", "list", "--unscoped", "--filter", "bv-123", "--sort", "updated", "--paginate", "--limit", "2"}
	if calls := test.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls=%#v want=%#v", calls, want)
	}
}

func TestBacklogListWhitespaceFilterIsBlank(t *testing.T) {
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"list": `{"issues":[],"pagination":{"limit":2,"has_more":false}}`})
	code, _, stderr := test.run("backlog", "list", "--filter", "   ", "--limit", "2", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	want := []string{"--db", test.store, "--json", "list", "--unscoped", "--sort", "updated", "--paginate", "--limit", "2"}
	if calls := test.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls=%#v want=%#v", calls, want)
	}
}

func TestBacklogJSONRequiresPositiveLimit(t *testing.T) {
	for _, arguments := range [][]string{
		{"backlog", "list", "--json"},
		{"backlog", "list", "--limit", "0", "--json"},
	} {
		if _, err := parse(arguments); err == nil {
			t.Errorf("parse(%v) unexpectedly succeeded", arguments)
		}
	}
}

func TestScopeActiveErrorsAreMappedClearly(t *testing.T) {
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"scope:active": `{}`})
	code, _, stderr := test.run("--json", "scope", "add", "bead-1")
	if code != 1 || !strings.Contains(stderr, `"code":"invalid_request"`) || !strings.Contains(stderr, "no active scope ID") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if calls := test.calls(); len(calls) != 1 {
		t.Fatalf("calls=%#v", calls)
	}
	assertNoViewerSignal(t, test)
}
