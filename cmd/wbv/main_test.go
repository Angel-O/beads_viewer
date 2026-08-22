package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

type childRecord struct {
	Args []string          `json:"args"`
	Dir  string            `json:"dir"`
	Env  map[string]string `json:"env"`
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_WBV_HELPER_PROCESS") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	command := os.Args[separator+1]
	record := childRecord{Args: os.Args[separator+2:], Env: make(map[string]string)}
	record.Dir, _ = os.Getwd()
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			record.Env[parts[0]] = parts[1]
		}
	}
	data, _ := json.Marshal(record)
	_ = os.WriteFile(filepath.Join(os.Getenv("WBV_TEST_RECORDS"), command+".json"), data, 0o600)
	exitCode, _ := strconv.Atoi(os.Getenv("WBV_" + strings.ToUpper(command) + "_EXIT"))
	os.Exit(exitCode)
}

func TestLocalModeDelegatesExactArgumentsAndSanitizedEnvironment(t *testing.T) {
	fixture := newFixture(t, "git", "bv")
	repository := filepath.Join(fixture.root, "repository")
	fixture.makeLocalStore(t, repository)
	t.Setenv("WBV_GIT_ROOT", repository)
	for name := range sanitizedEnvironment {
		t.Setenv(name, "unsafe")
	}
	t.Setenv("BV_NO_GITIGNORE", "wrong")
	t.Setenv("BV_NO_CACHE", "wrong")

	code, stderr := fixture.run("--local", "--robot-priority", "--label", "backend", "--robot-min-confidence", "0.75", "--robot-max-results", "010")
	if code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr)
	}
	record := fixture.record(t, "bv")
	wantArgs := []string{"--history-mode", "git", "--robot-priority", "--label", "backend", "--robot-min-confidence", "0.75", "--robot-max-results", "010", "--format", "json"}
	if !reflect.DeepEqual(record.Args, wantArgs) {
		t.Fatalf("bv args = %#v, want %#v", record.Args, wantArgs)
	}
	wantDirectory, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	if record.Dir != wantDirectory {
		t.Fatalf("bv directory = %q, want %q", record.Dir, wantDirectory)
	}
	for name := range sanitizedEnvironment {
		if _, exists := record.Env[name]; exists {
			t.Errorf("sanitized variable %s reached bv", name)
		}
	}
	if record.Env["BV_NO_GITIGNORE"] != "1" || record.Env["BV_NO_CACHE"] != "1" {
		t.Fatalf("forced environment = BV_NO_GITIGNORE=%q BV_NO_CACHE=%q", record.Env["BV_NO_GITIGNORE"], record.Env["BV_NO_CACHE"])
	}
	if _, err := os.Stat(filepath.Join(fixture.records, "wbd.json")); !os.IsNotExist(err) {
		t.Fatal("local mode invoked wbd")
	}
}

func TestAutoLocalAndExplicitHubAreIsolated(t *testing.T) {
	fixture := newFixture(t, "git", "bd", "bv", "wbd")
	repository := filepath.Join(fixture.root, "repository")
	fixture.makeLocalStore(t, repository)
	t.Setenv("WBV_GIT_ROOT", repository)
	fixture.makeHubStore(t)
	configDir := filepath.Join(fixture.home, ".config", "bv")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "hub.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stderr := fixture.run("--robot-triage", "--brief", "--robot-not-ready-labels", "waiting")
	if code != 0 {
		t.Fatalf("auto-local code = %d, stderr = %q", code, stderr)
	}
	local := fixture.record(t, "bv")
	if local.Env["BEADS_DIR"] != "" || local.Args[1] != "git" {
		t.Fatalf("auto-local leaked Hub state: args=%#v BEADS_DIR=%q", local.Args, local.Env["BEADS_DIR"])
	}
	if err := os.WriteFile(filepath.Join(repository, ".beads", "issues.jsonl"), []byte("malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stderr = fixture.run("--hub", "--robot-forecast", "all", "--forecast-agents", "64", "--forecast-label", "backend")
	if code != 0 {
		t.Fatalf("hub code = %d, stderr = %q", code, stderr)
	}
	wbd := fixture.record(t, "wbd")
	if !reflect.DeepEqual(wbd.Args, []string{"configure"}) {
		t.Fatalf("wbd args = %#v", wbd.Args)
	}
	hubRecord := fixture.record(t, "bv")
	wantPrefix := []string{"--history-mode", "external", "--hub-config", filepath.Join(fixture.home, ".config/bv/hub.yaml")}
	if !reflect.DeepEqual(hubRecord.Args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("Hub bv args = %#v", hubRecord.Args)
	}
	wantStore := filepath.Join(fixture.home, ".local/share/beads/hub/.beads")
	if hubRecord.Env["BEADS_DIR"] != wantStore {
		t.Fatalf("BEADS_DIR = %q, want %q", hubRecord.Env["BEADS_DIR"], wantStore)
	}
}

func TestAutoUsesHubWithoutValidLocalStore(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "no marker"},
		{
			name: "irrelevant marker",
			setup: func(t *testing.T, repository string) {
				marker := filepath.Join(repository, ".beads")
				if err := os.MkdirAll(marker, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(marker, "memories.jsonl"), []byte(`{"_type":"memory","id":"m1"}`+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "empty unrelated JSONL",
			setup: func(t *testing.T, repository string) {
				marker := filepath.Join(repository, ".beads")
				if err := os.MkdirAll(marker, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(marker, "memories.jsonl"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "issue-shaped metadata JSONL",
			setup: func(t *testing.T, repository string) {
				marker := filepath.Join(repository, ".beads")
				if err := os.MkdirAll(marker, 0o700); err != nil {
					t.Fatal(err)
				}
				content := `{"_type":"memory","id":"m1","title":"memory","status":"open"}` + "\n"
				if err := os.WriteFile(filepath.Join(marker, "memories.jsonl"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t, "git", "bd", "bv", "wbd")
			repository := filepath.Join(fixture.root, "repository")
			if err := os.MkdirAll(repository, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.setup != nil {
				test.setup(t, repository)
			}
			t.Setenv("WBV_GIT_ROOT", repository)
			fixture.makeHubStore(t)

			code, stderr := fixture.run("--robot-plan")
			if code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			record := fixture.record(t, "bv")
			if len(record.Args) < 2 || record.Args[1] != "external" {
				t.Fatalf("auto mode args = %#v, want external history", record.Args)
			}
			if _, err := os.Stat(filepath.Join(fixture.records, "wbd.json")); err != nil {
				t.Fatalf("auto Hub mode did not configure wbd: %v", err)
			}
		})
	}
}

func TestHubRobotScopeRouting(t *testing.T) {
	contextPrefix := "ctx:"
	first := contextPrefix + "alpha"
	second := contextPrefix + "beta"
	fixture := newFixture(t, "git", "bd", "bv", "wbd")
	fixture.makeHubStore(t)
	fixture.makeHubConfig(t, first, second)

	code, stderr := fixture.run("--hub", "--context", second, "--context", first, "--context", second, "--robot-plan")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	record := fixture.record(t, "bv")
	for _, argument := range record.Args {
		if argument == "--context" || argument == "--contextless" || argument == "--repo" {
			t.Fatalf("public scope flag leaked into bv arguments: %#v", record.Args)
		}
	}
	var scope struct {
		Mode     string   `json:"mode"`
		Contexts []string `json:"contexts"`
	}
	if err := json.Unmarshal([]byte(record.Env["BV_WBV_HUB_SCOPE"]), &scope); err != nil {
		t.Fatalf("scope environment is not JSON: %v", err)
	}
	if scope.Mode != "contexts" || !reflect.DeepEqual(scope.Contexts, []string{first, second}) {
		t.Fatalf("scope = %#v", scope)
	}
}

func TestHubRobotScopeDefaultsAndContextless(t *testing.T) {
	contextID := "ctx:" + "current"
	t.Run("registered current", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		fixture.makeHubStore(t)
		fixture.makeHubConfig(t, contextID)
		t.Setenv("WBV_TEST_CONTEXT", contextID)
		if code, stderr := fixture.run("--hub", "--robot-insights"); code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if got := fixture.record(t, "bv").Env["BV_WBV_HUB_SCOPE"]; !strings.Contains(got, `"mode":"contexts"`) || !strings.Contains(got, contextID) {
			t.Fatalf("scope = %q", got)
		}
	})

	t.Run("unavailable current falls back to all items", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		fixture.makeHubStore(t)
		fixture.makeHubConfig(t, contextID)
		t.Setenv("WBV_TEST_CONTEXT", "ctx:"+"unregistered")
		if code, stderr := fixture.run("--hub", "--robot-plan"); code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if got := fixture.record(t, "bv").Env["BV_WBV_HUB_SCOPE"]; got != `{"mode":"all_items","contexts":[]}` {
			t.Fatalf("scope = %q", got)
		}
	})

	t.Run("contextless", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		fixture.makeHubStore(t)
		if code, stderr := fixture.run("--hub", "--contextless", "--robot-plan"); code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if got := fixture.record(t, "bv").Env["BV_WBV_HUB_SCOPE"]; got != `{"mode":"contextless","contexts":[]}` {
			t.Fatalf("scope = %q", got)
		}
	})

	t.Run("context and contextless union", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		fixture.makeHubStore(t)
		fixture.makeHubConfig(t, contextID)
		if code, stderr := fixture.run("--hub", "--context", contextID, "--contextless", "--robot-plan"); code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if got := fixture.record(t, "bv").Env["BV_WBV_HUB_SCOPE"]; got != `{"mode":"contexts","contexts":["ctx:current"],"include_contextless":true}` {
			t.Fatalf("scope = %q", got)
		}
	})
}

func TestHubRobotScopeRejectsInvalidSelections(t *testing.T) {
	registered := "ctx:" + "registered"
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unregistered", args: []string{"--hub", "--context", "ctx:" + "missing", "--robot-plan"}, want: "not registered"},
		{name: "missing", args: []string{"--hub", "--robot-plan", "--context"}, want: "missing value for --context"},
		{name: "unsafe", args: []string{"--hub", "--context", "-unsafe", "--robot-plan"}, want: "invalid value"},
		{name: "local", args: []string{"--local", "--context", registered, "--robot-plan"}, want: "only with Hub mode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t, "git", "bd", "bv", "wbd")
			fixture.makeHubStore(t)
			fixture.makeHubConfig(t, registered)
			if test.name == "local" {
				repository := filepath.Join(fixture.root, "repository")
				fixture.makeLocalStore(t, repository)
				t.Setenv("WBV_GIT_ROOT", repository)
			}
			code, stderr := fixture.run(test.args...)
			if code != 1 || !strings.Contains(stderr, test.want) {
				t.Fatalf("code = %d, stderr = %q, want %q", code, stderr, test.want)
			}
			if _, err := os.Stat(filepath.Join(fixture.records, "bv.json")); !os.IsNotExist(err) {
				t.Fatal("rejected scope invoked bv")
			}
		})
	}
}

func TestHubScopeRoutesAcrossEverySupportedRobotCommand(t *testing.T) {
	invocations := [][]string{
		{"--robot-plan"},
		{"--robot-priority"},
		{"--robot-insights"},
		{"--robot-graph"},
		{"--robot-label-health"},
		{"--robot-label-flow"},
		{"--robot-label-attention"},
		{"--robot-blocker-chain", "issue"},
		{"--robot-sprint-list"},
		{"--robot-sprint-show", "sprint"},
		{"--robot-forecast", "all"},
		{"--robot-capacity"},
		{"--robot-triage", "--brief"},
	}
	for _, invocation := range invocations {
		name := strings.TrimPrefix(invocation[0], "--")
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t, "git", "bd", "bv", "wbd")
			fixture.makeHubStore(t)
			arguments := append([]string{"--hub", "--contextless"}, invocation...)
			if code, stderr := fixture.run(arguments...); code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			record := fixture.record(t, "bv")
			if record.Env["BV_WBV_HUB_SCOPE"] != `{"mode":"contextless","contexts":[]}` {
				t.Fatalf("scope transport = %q", record.Env["BV_WBV_HUB_SCOPE"])
			}
		})
	}
}

func TestInteractiveHubPassesAutomaticRefreshSignal(t *testing.T) {
	fixture := newFixture(t, "git", "bd", "bv", "wbd")
	fixture.makeHubStore(t)
	code, stderr := fixture.runInteractive("--hub")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	record := fixture.record(t, "bv")
	want := filepath.Join(fixture.home, ".local/share/beads/hub/viewer-generation")
	if record.Env["BV_HUB_CHANGE_SIGNAL"] != want {
		t.Fatalf("BV_HUB_CHANGE_SIGNAL = %q, want %q", record.Env["BV_HUB_CHANGE_SIGNAL"], want)
	}
}

func TestHubAutomaticRefreshCanBeDisabledAndNeverLeaksToLocalMode(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		fixture.makeHubStore(t)
		t.Setenv("BV_HUB_AUTO_REFRESH", "0")
		if code, stderr := fixture.runInteractive("--hub"); code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if got := fixture.record(t, "bv").Env["BV_HUB_CHANGE_SIGNAL"]; got != "" {
			t.Fatalf("disabled Hub signal leaked to bv: %q", got)
		}
	})

	t.Run("local", func(t *testing.T) {
		fixture := newFixture(t, "git", "bv")
		repository := filepath.Join(fixture.root, "repository")
		fixture.makeLocalStore(t, repository)
		t.Setenv("WBV_GIT_ROOT", repository)
		t.Setenv("BV_HUB_CHANGE_SIGNAL", "hostile-value")
		if code, stderr := fixture.runInteractive("--local"); code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		if got := fixture.record(t, "bv").Env["BV_HUB_CHANGE_SIGNAL"]; got != "" {
			t.Fatalf("Hub signal leaked to local mode: %q", got)
		}
	})
}

func TestMalformedLocalStoreFailsInsteadOfSelectingHub(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "malformed JSON", content: "not json\n"},
		{name: "non-issue record", content: `{"_type":"memory","id":"m1","title":"memory","status":"open"}` + "\n"},
		{name: "invalid issue", content: `{"id":null,"title":"invalid","status":"open"}` + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t, "git", "bd", "bv", "wbd")
			repository := filepath.Join(fixture.root, "repository")
			marker := filepath.Join(repository, ".beads")
			if err := os.MkdirAll(marker, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(marker, "issues.jsonl"), []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WBV_GIT_ROOT", repository)
			fixture.makeHubStore(t)

			code, stderr := fixture.run("--robot-plan")
			if code != 1 || !strings.Contains(stderr, "has no valid issue source") || !strings.Contains(stderr, "issues.jsonl") {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(fixture.records, "wbd.json")); !os.IsNotExist(err) {
				t.Fatal("malformed local store silently selected Hub")
			}
		})
	}
}

func TestNoncanonicalIssueSourcePreservesLocalMode(t *testing.T) {
	fixture := newFixture(t, "git", "bd", "bv", "wbd")
	repository := filepath.Join(fixture.root, "repository")
	marker := filepath.Join(repository, ".beads")
	if err := os.MkdirAll(marker, 0o700); err != nil {
		t.Fatal(err)
	}
	issue := `{"id":"CUSTOM-1","title":"Custom source","status":"open","priority":2,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(filepath.Join(marker, "custom.jsonl"), []byte(issue), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WBV_GIT_ROOT", repository)
	fixture.makeHubStore(t)

	code, stderr := fixture.run("--robot-plan")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	record := fixture.record(t, "bv")
	if len(record.Args) < 2 || record.Args[1] != "git" {
		t.Fatalf("auto mode args = %#v, want local Git history", record.Args)
	}
}

func TestLocalStoreRedirectsAreResolvedDeliberately(t *testing.T) {
	t.Run("valid target", func(t *testing.T) {
		root := t.TempDir()
		marker := filepath.Join(root, "repository", ".beads")
		target := filepath.Join(root, "shared", ".beads")
		if err := os.MkdirAll(marker, 0o700); err != nil {
			t.Fatal(err)
		}
		makeLocalStore(t, filepath.Dir(target))
		if err := os.WriteFile(filepath.Join(marker, "redirect"), []byte(target+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		valid, err := validLocalStore(marker)
		if err != nil || !valid {
			t.Fatalf("redirected store valid = %t, err = %v", valid, err)
		}
	})

	t.Run("missing target", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), ".beads")
		if err := os.MkdirAll(marker, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(marker, "redirect"), []byte("../missing/.beads\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		valid, err := validLocalStore(marker)
		if err == nil || valid || !strings.Contains(err.Error(), "redirect target not found") {
			t.Fatalf("broken redirect valid = %t, err = %v", valid, err)
		}
	})
}

func TestLinkedWorktreeUsesOnlyItsOwnLocalStore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	mainRepository := filepath.Join(root, "main")
	linkedRepository := filepath.Join(root, "linked")
	if err := os.Mkdir(mainRepository, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainRepository, "init")
	runGit(t, mainRepository, "config", "user.email", "wbv@example.invalid")
	runGit(t, mainRepository, "config", "user.name", "wbv test")
	runGit(t, mainRepository, "commit", "--allow-empty", "-m", "initial")
	runGit(t, mainRepository, "worktree", "add", "-b", "linked", linkedRepository)
	makeLocalStore(t, mainRepository)

	wantRoot, err := filepath.EvalSymlinks(linkedRepository)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveGitRoot(linkedRepository); got != wantRoot {
		t.Fatalf("linked worktree root = %q, want %q", got, wantRoot)
	}
	valid, err := validLocalStore(filepath.Join(linkedRepository, ".beads"))
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("linked worktree inherited the main worktree's local store")
	}

	makeLocalStore(t, linkedRepository)
	valid, err = validLocalStore(filepath.Join(linkedRepository, ".beads"))
	if err != nil || !valid {
		t.Fatalf("linked worktree's own store valid = %t, err = %v", valid, err)
	}
}

func TestHelpExplainsDefaultModeSelection(t *testing.T) {
	fixture := newFixture(t)
	code, stdout, stderr := fixture.runWithOutput("--help")
	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"valid Viewer issue source", "Otherwise wbv uses the", "--hub", "linked worktree"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestRejectsUnsafeInvocationsBeforeHubConfigure(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"selector position", []string{"--robot-plan", "--hub"}, "mode selector must be the first argument"},
		{"selector value", []string{"--hub=yes", "--robot-plan"}, "mode selector does not take a value: --hub"},
		{"unsupported primary", []string{"--hub", "--robot-history"}, "unsupported Viewer invocation"},
		{"triage requires brief", []string{"--hub", "--robot-triage"}, "--robot-triage requires --brief"},
		{"unsafe value", []string{"--hub", "--robot-plan", "--label", "-danger"}, "invalid value for --label"},
		{"control value", []string{"--hub", "--robot-graph", "--graph-root", "bad\tvalue"}, "invalid control character"},
		{"duplicate", []string{"--hub", "--robot-graph", "--graph-depth", "1", "--graph-depth", "2"}, "duplicate Viewer option"},
		{"range", []string{"--hub", "--robot-capacity", "--agents", "65"}, "must be between 1 and 64"},
		{"wrong option", []string{"--hub", "--robot-plan", "--graph-depth", "2"}, "is not supported with --robot-plan"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t, "git", "bd", "bv", "wbd")
			fixture.makeHubStore(t)
			code, stderr := fixture.run(test.args...)
			if code != 1 || !strings.Contains(stderr, test.want) {
				t.Fatalf("code = %d, stderr = %q, want %q", code, stderr, test.want)
			}
			if _, err := os.Stat(filepath.Join(fixture.records, "wbd.json")); !os.IsNotExist(err) {
				t.Fatal("rejected invocation ran wbd configure")
			}
			if _, err := os.Stat(filepath.Join(fixture.records, "bv.json")); !os.IsNotExist(err) {
				t.Fatal("rejected invocation ran bv")
			}
		})
	}
}

func TestLocalMarkerSafetyAndBareTerminalRestriction(t *testing.T) {
	t.Run("symlink marker", func(t *testing.T) {
		fixture := newFixture(t, "git", "bv")
		repository := filepath.Join(fixture.root, "repository")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(fixture.root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(repository, ".beads")); err != nil {
			t.Fatal(err)
		}
		t.Setenv("WBV_GIT_ROOT", repository)
		code, stderr := fixture.run("--local", "--robot-plan")
		if code != 1 || !strings.Contains(stderr, "must not be a symlink") {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
	})

	t.Run("bare non-terminal", func(t *testing.T) {
		fixture := newFixture(t, "git", "bv")
		repository := filepath.Join(fixture.root, "repository")
		fixture.makeLocalStore(t, repository)
		t.Setenv("WBV_GIT_ROOT", repository)
		code, stderr := fixture.run("--local")
		if code != 1 || !strings.Contains(stderr, "bare wbv requires an interactive terminal") {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
	})
}

func TestMissingHubPreconditionsAndDependencies(t *testing.T) {
	t.Run("store", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		code, stderr := fixture.run("--hub", "--robot-plan")
		if code != 1 || !strings.Contains(stderr, "store is missing; run 'wbd bootstrap'") {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
	})
	t.Run("store is not a directory", func(t *testing.T) {
		fixture := newFixture(t, "git", "bd", "bv", "wbd")
		store := filepath.Join(fixture.home, ".local/share/beads/hub/.beads")
		if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stderr := fixture.run("--hub", "--robot-plan")
		if code != 1 || !strings.Contains(stderr, "Hub store path is not a directory") {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
	})
	for _, missing := range []string{"bd", "bv", "wbd"} {
		t.Run(missing, func(t *testing.T) {
			commands := []string{"git", "bd", "bv", "wbd"}
			available := make([]string, 0, len(commands)-1)
			for _, command := range commands {
				if command != missing {
					available = append(available, command)
				}
			}
			fixture := newFixture(t, available...)
			fixture.makeHubStore(t)
			code, stderr := fixture.run("--hub", "--robot-plan")
			if code != 1 || !strings.Contains(stderr, "required command not found: "+missing) {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
		})
	}
}

func TestChildExitCodesPropagate(t *testing.T) {
	fixture := newFixture(t, "git", "bd", "bv", "wbd")
	fixture.makeHubStore(t)
	t.Setenv("WBV_WBD_EXIT", "23")
	code, _ := fixture.run("--hub", "--robot-plan")
	if code != 23 {
		t.Fatalf("wbd exit code = %d, want 23", code)
	}
	t.Setenv("WBV_WBD_EXIT", "0")
	t.Setenv("WBV_BV_EXIT", "37")
	code, _ = fixture.run("--hub", "--robot-plan")
	if code != 37 {
		t.Fatalf("bv exit code = %d, want 37", code)
	}
}

func TestChildSignalExitCodePropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal semantics")
	}
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	code, ok := childExitCode(err)
	if !ok || code != 143 {
		t.Fatalf("childExitCode() = (%d, %t), want (143, true)", code, ok)
	}
}

type fixture struct {
	t       *testing.T
	root    string
	home    string
	bin     string
	records string
}

func newFixture(t *testing.T, commands ...string) fixture {
	t.Helper()
	root := t.TempDir()
	result := fixture{t: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"), records: filepath.Join(root, "records")}
	for _, directory := range []string{result.home, result.bin, result.records} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		script := fmt.Sprintf("#!/bin/sh\nif [ %q = git ]; then printf '%%s\\n' \"$WBV_GIT_ROOT\"; exit 0; fi\nexec \"$WBV_TEST_BINARY\" -test.run=TestHelperProcess -- %q \"$@\"\n", command, command)
		if err := os.WriteFile(filepath.Join(result.bin, command), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", result.home)
	t.Setenv("PATH", result.bin)
	t.Setenv("GO_WANT_WBV_HELPER_PROCESS", "1")
	t.Setenv("WBV_TEST_BINARY", os.Args[0])
	t.Setenv("WBV_TEST_RECORDS", result.records)
	t.Setenv("WBV_GIT_ROOT", "")
	t.Setenv("WBV_WBD_EXIT", "0")
	t.Setenv("WBV_BV_EXIT", "0")
	return result
}

func (f fixture) makeHubStore(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(f.home, ".local/share/beads/hub/.beads"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) makeHubConfig(t *testing.T, contexts ...string) {
	t.Helper()
	repositories := make(map[string]hub.Repository, len(contexts))
	for index, contextID := range contexts {
		repositories[contextID] = hub.Repository{Path: filepath.Join(f.root, fmt.Sprintf("repository-%d", index))}
	}
	config := hub.Config{
		Version:      hub.ConfigVersion,
		Store:        filepath.Join(f.home, ".local/share/beads/hub/.beads"),
		Ledger:       filepath.Join(f.home, ".local/share/beads/hub/correlations.jsonl"),
		Repositories: repositories,
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(f.home, ".config/bv/hub.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) makeLocalStore(t *testing.T, repository string) {
	t.Helper()
	makeLocalStore(t, repository)
}

func makeLocalStore(t *testing.T, repository string) {
	t.Helper()
	marker := filepath.Join(repository, ".beads")
	if err := os.MkdirAll(marker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marker, "issues.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func (f fixture) run(arguments ...string) (int, string) {
	code, _, stderr := f.runWithOutput(arguments...)
	return code, stderr
}

func (f fixture) runWithOutput(arguments ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	r := runner{
		stdin:       strings.NewReader(""),
		stdout:      &stdout,
		stderr:      &stderr,
		directory:   f.root,
		interactive: func() bool { return false },
		hubContext: func(string) (string, error) {
			if contextID := os.Getenv("WBV_TEST_CONTEXT"); contextID != "" {
				return contextID, nil
			}
			return "", fmt.Errorf("test context unavailable")
		},
	}
	return r.run(arguments), stdout.String(), stderr.String()
}

func (f fixture) runInteractive(arguments ...string) (int, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	r := runner{
		stdin:       strings.NewReader(""),
		stdout:      &stdout,
		stderr:      &stderr,
		directory:   f.root,
		interactive: func() bool { return true },
		hubContext: func(string) (string, error) {
			return "", fmt.Errorf("test context unavailable")
		},
	}
	return r.run(arguments), stderr.String()
}

func (f fixture) record(t *testing.T, command string) childRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.records, command+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var record childRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}
