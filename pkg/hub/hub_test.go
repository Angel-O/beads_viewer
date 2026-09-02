package hub

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		Store:  filepath.Join(home, ".local/share/beads/hub/.beads"),
		Config: filepath.Join(home, ".config/bv/hub.yaml"),
		Ledger: filepath.Join(home, ".local/share/beads/hub/correlations.jsonl"),
	}
	if paths != want {
		t.Fatalf("DefaultPaths() = %#v, want %#v", paths, want)
	}
}

func TestHubScopeAndLabelAdmissionAreCaseSensitive(t *testing.T) {
	selected, err := NewSelectedContextsHubScope([]string{"ctx:z", "ctx:a", "ctx:z"})
	if err != nil || !selected.MatchesLabels([]string{"ctx:a"}) || selected.MatchesLabels([]string{"ctx:other"}) {
		t.Fatalf("selected scope = %#v, err = %v", selected, err)
	}
	if selected.MatchesLabels([]string{"Ctx:a"}) {
		t.Fatal("uppercase context unexpectedly matched")
	}
	if IsContextLabel("Ctx:a") || !IsContextLabel("ctx:a") || !AdmitLabel("Ctx:a") || AdmitLabel("ctx:a") {
		t.Fatal("Hub context classification is not exact and lowercase")
	}
	if !NewContextlessHubScope().MatchesLabels([]string{"Ctx:a"}) {
		t.Fatal("uppercase ctx label should be contextless to Hub")
	}
}

func TestAdmitIssueCardinalityAndRegistration(t *testing.T) {
	registered := map[string]Repository{"ctx:a": {}, "ctx:b": {}}
	tests := []struct {
		name     string
		kind     string
		contexts []string
		want     []string
		code     PolicyCode
	}{
		{name: "contextless todo", kind: "todo", want: []string{}},
		{name: "deduplicated todo", kind: "todo", contexts: []string{"ctx:b", "ctx:a", "ctx:b"}, want: []string{"ctx:a", "ctx:b"}},
		{name: "multi-context epic", kind: "epic", contexts: []string{"ctx:b", "ctx:a"}, want: []string{"ctx:a", "ctx:b"}},
		{name: "contextless epic", kind: "epic", code: PolicyInvalidCardinality},
		{name: "single project", kind: "task", contexts: []string{"ctx:a"}, want: []string{"ctx:a"}},
		{name: "multi project", kind: "bug", contexts: []string{"ctx:a", "ctx:b"}, code: PolicyInvalidCardinality},
		{name: "decision", kind: "decision", contexts: []string{"ctx:a"}, want: []string{"ctx:a"}},
		{name: "unsupported", kind: "question", contexts: []string{"ctx:a"}, code: PolicyInvalidKind},
		{name: "unregistered", kind: "task", contexts: []string{"ctx:nope"}, code: PolicyUnregisteredContext},
		{name: "invalid context identity", kind: "todo", contexts: []string{"not-a-context"}, code: PolicyUnregisteredContext},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := AdmitIssue(test.kind, test.contexts, []string{"ordinary"}, registered)
			if test.code != "" {
				var policyErr *PolicyError
				if !errors.As(err, &policyErr) || policyErr.Code != test.code {
					t.Fatalf("error = %v, want policy code %q", err, test.code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Contexts, test.want) {
				t.Fatalf("contexts = %#v, want %#v", got.Contexts, test.want)
			}
			wantLabels := append([]string{"ordinary"}, test.want...)
			if !reflect.DeepEqual(got.Labels, wantLabels) {
				t.Fatalf("labels = %#v, want %#v", got.Labels, wantLabels)
			}
		})
	}
}

func TestAdmitIssueRejectsReservedLabels(t *testing.T) {
	_, err := AdmitIssue("task", []string{"ctx:a"}, []string{"team", " ctx:other"}, map[string]Repository{"ctx:a": {}})
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) || policyErr.Code != PolicyReservedLabel {
		t.Fatalf("error = %v, want reserved-label policy error", err)
	}
}

func TestLifecycleEndpointPolicy(t *testing.T) {
	projectA := IssueState{ID: "work-a", Kind: "task", Labels: []string{"ctx:a"}}
	projectB := IssueState{ID: "work-b", Kind: "bug", Labels: []string{"ctx:b"}}
	todo := IssueState{ID: "todo", Kind: "todo"}
	epic := IssueState{ID: "epic", Kind: "epic", Labels: []string{"ctx:a", "ctx:b"}}

	if err := ValidateTodoResult(todo, projectA); err != nil {
		t.Fatalf("valid todo result: %v", err)
	}
	if err := ValidateTodoResult(projectA, projectB); err == nil {
		t.Fatal("project source accepted as todo result")
	}
	if err := ValidateCorrelationOwner(todo); err == nil {
		t.Fatal("todo accepted as direct correlation owner")
	}
	if err := ValidateCorrelationOwner(projectA); err != nil {
		t.Fatalf("project correlation owner: %v", err)
	}
	if err := ValidateEpicChild(epic, projectB); err != nil {
		t.Fatalf("valid epic child: %v", err)
	}
	if err := ValidateEpicChild(epic, IssueState{ID: "outside", Kind: "task", Labels: []string{"ctx:c"}}); err == nil {
		t.Fatal("outside-context epic child accepted")
	}
	if err := ValidateEpicChild(projectA, todo); err != nil {
		t.Fatalf("non-epic behavior was constrained: %v", err)
	}
	if err := ValidateSupersession(IssueState{ID: "new", Kind: "task"}, projectA); err != nil {
		t.Fatalf("valid supersession: %v", err)
	}
	if err := ValidateSupersession(IssueState{ID: "new", Kind: "bug"}, projectA); err == nil {
		t.Fatal("cross-kind supersession accepted")
	}
	if err := ValidateLifecycleRemoval(IssueState{ID: "new", Kind: "task"}, projectA, "supersedes"); err == nil {
		t.Fatal("valid supersession edge was removable")
	}
	if err := ValidateLifecycleRemoval(projectA, todo, "discovered-from"); err == nil {
		t.Fatal("valid todo-result edge was removable")
	}
	if err := ValidateLifecycleRemoval(projectA, projectB, "discovered-from"); err != nil {
		t.Fatalf("generic discovered-from edge was protected: %v", err)
	}
	if err := ValidateLifecycleRemoval(projectA, projectB, "blocks"); err != nil {
		t.Fatalf("ordinary blocking edge was protected: %v", err)
	}
}

func TestSignalChangeAtomicallyAdvancesGeneration(t *testing.T) {
	parent := t.TempDir()
	paths := Paths{Store: filepath.Join(parent, ".beads")}
	if got, want := ChangeSignalPath(paths), filepath.Join(parent, changeSignalName); got != want {
		t.Fatalf("ChangeSignalPath() = %q, want %q", got, want)
	}
	if err := SignalChange(paths); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(ChangeSignalPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if err := SignalChange(paths); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(ChangeSignalPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || string(first) == string(second) {
		t.Fatalf("generation did not advance: first=%q second=%q", first, second)
	}
	if info, err := os.Stat(ChangeSignalPath(paths)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("change signal permissions: info=%v err=%v", info, err)
	}
}

func TestSemanticCacheDirLivesBesideStore(t *testing.T) {
	parent := t.TempDir()
	paths := Paths{Store: filepath.Join(parent, ".beads")}
	if got, want := SemanticCacheDir(paths), filepath.Join(parent, semanticCacheDirName); got != want {
		t.Fatalf("SemanticCacheDir() = %q, want %q", got, want)
	}
}

func TestDefaultPathsRequiresHOME(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows resolves the home directory from USERPROFILE")
	}
	t.Setenv("HOME", "")
	if _, err := DefaultPaths(); err == nil {
		t.Fatal("DefaultPaths() succeeded without HOME")
	}
}

func TestDefaultPathsUsesWindowsUserProfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific home directory fallback")
	}
	home := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", home)
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "bv", "hub.yaml"); paths.Config != want {
		t.Fatalf("DefaultPaths().Config = %q, want %q", paths.Config, want)
	}
}

func TestEnsureConfigDeterministicAndPreservesRepositories(t *testing.T) {
	paths := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.Config), 0o755); err != nil {
		t.Fatal(err)
	}
	input := `{"version":1,"store":"old","ledger":"old","repositories":{"ctx:z":{"path":"/z"},"ctx:a":{"path":"/a"}}}`
	if err := os.WriteFile(paths.Config, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := EnsureConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if config.Store != paths.Store || config.Ledger != paths.Ledger || len(config.Repositories) != 2 {
		t.Fatalf("unexpected config: %#v", config)
	}
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"ledger\": " + quoted(paths.Ledger) + ",\n" +
		"  \"repositories\": {\n" +
		"    \"ctx:a\": {\n      \"path\": \"/a\"\n    },\n" +
		"    \"ctx:z\": {\n      \"path\": \"/z\"\n    }\n" +
		"  },\n" +
		"  \"store\": " + quoted(paths.Store) + ",\n" +
		"  \"version\": 1\n}\n"
	if string(data) != want {
		t.Fatalf("config bytes:\n%s\nwant:\n%s", data, want)
	}
	info, err := os.Stat(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}

	before := info.ModTime()
	if _, err := EnsureConfig(paths); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before) {
		t.Fatal("unchanged config was replaced")
	}
}

func TestEnsureConfigStrictValidation(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"array", `[]`},
		{"unknown field", `{"version":1,"extra":true}`},
		{"missing version", `{}`},
		{"wrong version", `{"version":2}`},
		{"repositories scalar", `{"version":1,"repositories":[]}`},
		{"context prefix", `{"version":1,"repositories":{"repo":{"path":"/repo"}}}`},
		{"entry scalar", `{"version":1,"repositories":{"ctx:repo":"/repo"}}`},
		{"entry extra field", `{"version":1,"repositories":{"ctx:repo":{"path":"/repo","name":"repo"}}}`},
		{"empty path", `{"version":1,"repositories":{"ctx:repo":{"path":""}}}`},
		{"trailing value", `{"version":1} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := testPaths(t)
			if err := os.MkdirAll(filepath.Dir(paths.Config), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.Config, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := EnsureConfig(paths); err == nil {
				t.Fatal("EnsureConfig() accepted invalid config")
			}
		})
	}
}

func TestEnsureConfigAcceptsNullRepositoriesAndNumericVersionOne(t *testing.T) {
	paths := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.Config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config, []byte(`{"version":1e0,"repositories":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := EnsureConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Repositories) != 0 {
		t.Fatalf("repositories = %#v, want empty", config.Repositories)
	}
}

func TestLoadAndResolveYAMLConfigPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "hub.yaml")
	data := "version: 1\n" +
		"store: .beads\n" +
		"ledger: ~/private/correlations.jsonl\n" +
		"repositories:\n" +
		"  ctx:repo-123:\n" +
		"    path: ../repository\n"
	if err := os.WriteFile(configPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Store != ".beads" || loaded.Repositories["ctx:repo-123"].Path != "../repository" {
		t.Fatalf("Load() changed configured paths: %#v", loaded)
	}

	resolved, err := Resolve(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != configPath {
		t.Fatalf("Resolve().Path = %q, want %q", resolved.Path, configPath)
	}
	if resolved.Store != filepath.Join(configDir, ".beads") {
		t.Fatalf("Resolve().Store = %q", resolved.Store)
	}
	if resolved.Ledger != filepath.Join(home, "private", "correlations.jsonl") {
		t.Fatalf("Resolve().Ledger = %q", resolved.Ledger)
	}
	if got := resolved.Repositories["ctx:repo-123"].Path; got != filepath.Clean(filepath.Join(configDir, "../repository")) {
		t.Fatalf("resolved repository = %q", got)
	}
}

func TestLoadRepositoryCatalogIncludesRegisteredRepositoriesAndCountsContexts(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "hub.yaml")
	data := "version: 1\n" +
		"store: store\n" +
		"ledger: missing/correlations.jsonl\n" +
		"repositories:\n" +
		"  ctx:alpha:\n    path: teams/alpha/service\n" +
		"  ctx:beta:\n    path: teams/beta/service\n" +
		"  ctx:zero:\n    path: tools/empty\n"
	if err := os.WriteFile(configPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	issues := []model.Issue{
		{ID: "one", Labels: []string{"ctx:alpha", "ctx:beta"}},
		{ID: "two", Labels: []string{"ctx:alpha", "ctx:alpha"}},
		{ID: "ignored", Labels: []string{"ctx:unknown"}},
	}
	catalog, err := LoadRepositoryCatalog(configPath, issues)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]repository.CatalogEntry, len(catalog))
	for _, entry := range catalog {
		byID[entry.ID] = entry
	}
	if len(byID) != 3 || byID["ctx:zero"].BeadCount != 0 {
		t.Fatalf("zero-bead repository missing: %#v", byID)
	}
	if byID["ctx:alpha"].BeadCount != 2 || byID["ctx:beta"].BeadCount != 1 {
		t.Fatalf("context counts = alpha:%d beta:%d", byID["ctx:alpha"].BeadCount, byID["ctx:beta"].BeadCount)
	}
	if byID["ctx:alpha"].Name != "alpha/service" || byID["ctx:beta"].Name != "beta/service" {
		t.Fatalf("collision names = %q, %q", byID["ctx:alpha"].Name, byID["ctx:beta"].Name)
	}
	if byID["ctx:alpha"].Path != filepath.Join(root, "teams/alpha/service") || byID["ctx:alpha"].Detail != byID["ctx:alpha"].Path {
		t.Fatalf("resolved path/detail = %#v", byID["ctx:alpha"])
	}
}

func TestEnsureConfigRejectsSymlinkAndNonRegularFile(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		paths := testPaths(t)
		if err := os.MkdirAll(filepath.Dir(paths.Config), 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte(`{"version":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, paths.Config); err != nil {
			t.Fatal(err)
		}
		if _, err := EnsureConfig(paths); err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
			t.Fatalf("EnsureConfig() error = %v", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		paths := testPaths(t)
		if err := os.MkdirAll(paths.Config, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := EnsureConfig(paths); err == nil || !strings.Contains(err.Error(), "must be a regular file") {
			t.Fatalf("EnsureConfig() error = %v", err)
		}
	})
}

func TestContextEquivalentOrigins(t *testing.T) {
	repository := newRepository(t)
	origins := []string{
		"git@GitHub.COM:Owner/Example_Repo.git",
		"ssh://git@github.com/owner/example_repo.git",
		"https://github.com/OWNER/example_repo.git",
		"https://user@github.com/owner/example_repo.git/?ignored=true#fragment",
	}
	var expected string
	for _, origin := range origins {
		gitRun(t, repository, "remote", "remove", "origin", allowFailure())
		gitRun(t, repository, "remote", "add", "origin", origin)
		context, err := Context(repository)
		if err != nil {
			t.Fatalf("Context(%q): %v", origin, err)
		}
		if expected == "" {
			expected = context
		} else if context != expected {
			t.Fatalf("Context(%q) = %q, want %q", origin, context, expected)
		}
	}
	if expected != "ctx:example-repo-0d176f2778" {
		t.Fatalf("Context() = %q, want exact reference identity", expected)
	}
}

func TestContextLinkedWorktreeMatchesPrimary(t *testing.T) {
	primary := newRepository(t)
	gitRun(t, primary, "remote", "add", "origin", "git@github.com:owner/repository.git")
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, primary, "worktree", "add", "-b", "context-linked", linked)

	primaryContext, err := Context(primary)
	if err != nil {
		t.Fatal(err)
	}
	linkedContext, err := Context(linked)
	if err != nil {
		t.Fatal(err)
	}
	if linkedContext != primaryContext {
		t.Fatalf("linked context = %q, want %q", linkedContext, primaryContext)
	}
}

func TestContextRejectsIneligibleDirectories(t *testing.T) {
	if _, err := Context(t.TempDir()); err == nil {
		t.Fatal("Context accepted a directory outside Git")
	}
	repository := newRepository(t)
	if _, err := Context(repository); err == nil {
		t.Fatal("Context accepted a repository without origin")
	}
}

func TestContextRejectsUnsupportedOrigins(t *testing.T) {
	repository := newRepository(t)
	for _, origin := range []string{"/local/repository", "ssh://example.com"} {
		gitRun(t, repository, "remote", "remove", "origin", allowFailure())
		gitRun(t, repository, "remote", "add", "origin", origin)
		if _, err := Context(repository); err == nil {
			t.Fatalf("Context() accepted %q", origin)
		}
	}
}

func TestDurableRepositoryRootAndRegisterUsePrimaryCheckout(t *testing.T) {
	primary := newRepository(t)
	gitRun(t, primary, "remote", "add", "origin", "git@github.com:owner/repository.git")
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, primary, "worktree", "add", "-b", "linked-branch", linked)

	root, err := DurableRepositoryRoot(linked)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(primary)
	if err != nil {
		t.Fatal(err)
	}
	if root != wantRoot {
		t.Fatalf("DurableRepositoryRoot() = %q, want primary %q", root, wantRoot)
	}

	paths := testPaths(t)
	registration, err := Register(paths, linked)
	if err != nil {
		t.Fatal(err)
	}
	if registration.Root != wantRoot {
		t.Fatalf("Register().Root = %q, want %q", registration.Root, wantRoot)
	}
	config := readConfig(t, paths.Config)
	if got := config.Repositories[registration.Context].Path; got != wantRoot {
		t.Fatalf("registered path = %q, want %q", got, wantRoot)
	}
}

func TestDurableRepositoryRootFallsBackForBarePrimary(t *testing.T) {
	base := t.TempDir()
	bare := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", bare)
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "init")
	configureGitIdentity(t, seed)
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "add", "README")
	gitRun(t, seed, "commit", "-m", "seed")
	gitRun(t, seed, "remote", "add", "origin", bare)
	gitRun(t, seed, "push", "origin", "HEAD:main")
	gitRun(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")
	linked := filepath.Join(base, "linked")
	gitRun(t, bare, "worktree", "add", linked, "main")

	root, err := DurableRepositoryRoot(linked)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	if root != want {
		t.Fatalf("DurableRepositoryRoot() = %q, want local fallback %q", root, want)
	}
}

func TestRegisterPreservesOtherRepositories(t *testing.T) {
	repository := newRepository(t)
	gitRun(t, repository, "remote", "add", "origin", "https://example.com/team/new.git")
	paths := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.Config), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"store":"old","ledger":"old","repositories":{"ctx:existing":{"path":"/existing"}}}`
	if err := os.WriteFile(paths.Config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	registration, err := Register(paths, repository)
	if err != nil {
		t.Fatal(err)
	}
	config := readConfig(t, paths.Config)
	if config.Repositories["ctx:existing"].Path != "/existing" {
		t.Fatal("Register() did not preserve existing repository")
	}
	if config.Repositories[registration.Context].Path != registration.Root {
		t.Fatal("Register() did not write current repository")
	}
	if config.Store != paths.Store || config.Ledger != paths.Ledger {
		t.Fatal("Register() did not normalize fixed paths")
	}
}

func TestConfigureRegistersEligibleRepositoryAndFallsBackOtherwise(t *testing.T) {
	eligible := newRepository(t)
	gitRun(t, eligible, "remote", "add", "origin", "https://example.com/team/repo.git")
	paths := testPaths(t)
	registration, err := Configure(paths, eligible)
	if err != nil {
		t.Fatal(err)
	}
	if registration == nil {
		t.Fatal("Configure() did not register eligible repository")
	}

	local := t.TempDir()
	fallbackPaths := testPaths(t)
	registration, err = Configure(fallbackPaths, local)
	if err != nil {
		t.Fatal(err)
	}
	if registration != nil {
		t.Fatalf("Configure() registration = %#v outside repository", registration)
	}
	config := readConfig(t, fallbackPaths.Config)
	if len(config.Repositories) != 0 {
		t.Fatalf("fallback repositories = %#v, want empty", config.Repositories)
	}
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	home := t.TempDir()
	parent := filepath.Join(home, ".local", "share", "beads", "hub")
	return Paths{
		Store:  filepath.Join(parent, ".beads"),
		Config: filepath.Join(home, ".config", "bv", "hub.yaml"),
		Ledger: filepath.Join(parent, "correlations.jsonl"),
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	gitRun(t, repository, "init")
	configureGitIdentity(t, repository)
	if err := os.WriteFile(filepath.Join(repository, "README"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "README")
	gitRun(t, repository, "commit", "-m", "initial")
	return repository
}

func configureGitIdentity(t *testing.T, repository string) {
	t.Helper()
	gitRun(t, repository, "config", "user.name", "Hub Test")
	gitRun(t, repository, "config", "user.email", "hub@example.test")
}

type gitOption bool

func allowFailure() gitOption { return true }

func gitRun(t *testing.T, directory string, arguments ...any) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	allow := false
	args := make([]string, 0, len(arguments)+2)
	args = append(args, "-C", directory)
	for _, argument := range arguments {
		if option, ok := argument.(gitOption); ok {
			allow = bool(option)
			continue
		}
		args = append(args, argument.(string))
	}
	command := exec.Command("git", args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil && !allow {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func readConfig(t *testing.T, path string) Config {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config, err := parseConfig(data, path)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Store  string `json:"store"`
		Ledger string `json:"ledger"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	config.Store = wire.Store
	config.Ledger = wire.Ledger
	return config
}

func quoted(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
