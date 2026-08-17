package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	code, _ := strconv.Atoi(os.Getenv("WBD_CHILD_EXIT"))
	os.Exit(code)
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
	if len(calls) != 1 || calls[0].Name != "bv" || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls = %#v, want args %#v", calls, want)
	}
	assertIsolatedEnvironment(t, calls[0].Env, test.store, true)
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

func TestReadsAndFailedMutationsDoNotSignalViewer(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		arguments []string
		childExit string
	}{
		{name: "list", arguments: []string{"list"}},
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
