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
		{"scope", "remove", "bead-1", "bead-2", "--scope", "work"},
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
		name     string
		args     []string
		mutation []string
	}{
		{name: "add", args: []string{"scope", "add", "bead-1", "bead-2", "--json"}, mutation: []string{"--db", "STORE", "--json", "scope", "add", "active", "bead-1", "bead-2"}},
		{name: "remove", args: []string{"scope", "remove", "bead-1", "bead-2", "--json"}, mutation: []string{"--db", "STORE", "--json", "scope", "remove", "active", "bead-1", "bead-2"}},
		{name: "move", args: []string{"scope", "move", "bead-1", "bead-2", "--json"}, mutation: []string{"--db", "STORE", "--json", "scope", "move", "active", "active", "bead-1", "bead-2"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			setResponses(t, map[string]string{
				"scope:active":           `{"id":"active"}`,
				"scope:" + testCase.name: `{}`,
			})
			code, _, stderr := test.run(testCase.args...)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			calls := test.calls()
			if len(calls) != 2 {
				t.Fatalf("calls=%#v", calls)
			}
			if !reflect.DeepEqual(calls[0].Args, []string{"--db", test.store, "--json", "scope", "active"}) {
				t.Fatalf("active call=%#v", calls[0].Args)
			}
			want := append([]string(nil), testCase.mutation...)
			want[1] = test.store
			if !reflect.DeepEqual(calls[1].Args, want) {
				t.Fatalf("mutation=%#v want=%#v", calls[1].Args, want)
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

func TestScopeExplicitIDsUseBackendPositionalContract(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "add", args: []string{"scope", "add", "bead-1", "bead-2", "--scope", "scope-a", "--json"}, want: []string{"--db", "STORE", "--json", "scope", "add", "scope-a", "bead-1", "bead-2"}},
		{name: "remove", args: []string{"scope", "remove", "bead-1", "bead-2", "--scope", "scope-a", "--json"}, want: []string{"--db", "STORE", "--json", "scope", "remove", "scope-a", "bead-1", "bead-2"}},
		{name: "move", args: []string{"scope", "move", "bead-1", "bead-2", "--source-scope", "scope-a", "--target-scope", "scope-b", "--json"}, want: []string{"--db", "STORE", "--json", "scope", "move", "scope-a", "scope-b", "bead-1", "bead-2"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			setResponses(t, map[string]string{"scope:" + testCase.name: `{}`})
			code, _, stderr := test.run(testCase.args...)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			want := append([]string(nil), testCase.want...)
			want[1] = test.store
			calls := test.calls()
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
				t.Fatalf("calls=%#v want=%#v", calls, want)
			}
			assertViewerSignal(t, test)
		})
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
	test := newAppTest(t, false)
	setResponses(t, map[string]string{"scope:active": `{"id":"active"}`})
	setExitCodes(t, map[string]int{"scope:add": 9})
	code, _, stderr := test.run("--json", "scope", "add", "bead-1")
	if code != 9 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if calls := test.calls(); len(calls) != 2 {
		t.Fatalf("calls=%#v", calls)
	}
	assertNoViewerSignal(t, test)
}

func TestBacklogListForwardsOpaqueCursorAndOutput(t *testing.T) {
	test := newAppTest(t, false)
	response := `{"issues":[{"id":"bead-1"}],"pagination":{"limit":2,"has_more":true,"next_cursor":"opaque:/+= token"}}
`
	setResponses(t, map[string]string{"list": response})
	code, stdout, stderr := test.run("backlog", "list", "--limit", "2", "--cursor", "opaque:/+= token", "--json")
	if code != 0 || stdout != response || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	calls := test.calls()
	want := []string{"--db", test.store, "--json", "list", "--unscoped", "--paginate", "--sort", "updated", "--limit", "2", "--cursor", "opaque:/+= token"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls=%#v want=%#v", calls, want)
	}
	assertNoViewerSignal(t, test)
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
