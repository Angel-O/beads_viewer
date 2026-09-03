package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Scoping flags (--label, --recipe, --repo, --as-of) must be honoured by every
// issue-backed robot command, and every payload must say which scope applied.
// Reality check 2026-09-01 found five commands reloading the working tree and
// silently ignoring all four; this matrix is the regression gate. Commands are
// enumerated from --robot-capabilities so a new command is covered automatically.

type capabilityCommand struct {
	Name         string `json:"name"`
	Flag         string `json:"flag"`
	NeedsIssues  bool   `json:"needs_issues"`
	NeedsGit     bool   `json:"needs_git"`
	NeedsSprint  bool   `json:"needs_sprint"`
	NeedsBase    bool   `json:"needs_baseline"`
	MutatesState bool   `json:"mutates_state"`
}

func loadCapabilities(t *testing.T, bv string) []capabilityCommand {
	t.Helper()
	cmd := exec.Command(bv, "--robot-capabilities")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("--robot-capabilities: %v", err)
	}
	var payload struct {
		Commands []capabilityCommand `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if len(payload.Commands) < 20 {
		t.Fatalf("capabilities lists only %d commands", len(payload.Commands))
	}
	return payload.Commands
}

// scopingSkip lists commands the matrix cannot drive generically: they need
// git history, a sprint file, a baseline, extra arguments with side effects,
// or produce no issue data at all.
var scopingSkip = map[string]string{
	"robot-drift":               "requires --check-drift",
	"robot-diff":                "requires --diff-since (covered by robot_diff_test.go)",
	"robot-search":              "requires --search (covered by robot_search_test.go)",
	"robot-confirm-correlation": "mutates the feedback store",
	"robot-reject-correlation":  "mutates the feedback store",
	"robot-explain-correlation": "needs a real commit sha",
	"robot-sprint-show":         "needs a sprint id (needs_sprint)",
}

// argvFor turns a capabilities flag string such as "--robot-blocker-chain ISSUE_ID"
// into argv with placeholders substituted for the fixture.
func argvFor(flag, inScopeID string) []string {
	parts := strings.Fields(flag)
	for i, p := range parts {
		switch p {
		case "ISSUE_ID":
			parts[i] = inScopeID
		case "README.md":
			parts[i] = "README.md"
		}
	}
	return parts
}

type scopedRun struct {
	exit    error
	stdout  string
	stderr  string
	payload map[string]json.RawMessage
}

func runScoped(t *testing.T, bv, dir string, args ...string) scopedRun {
	t.Helper()
	cmd := exec.Command(bv, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	r := scopedRun{exit: err, stdout: out.String(), stderr: errb.String()}
	if err == nil {
		_ = json.Unmarshal(out.Bytes(), &r.payload)
	}
	return r
}

func scopeOf(t *testing.T, r scopedRun) (map[string]any, bool) {
	t.Helper()
	raw, ok := r.payload["scope"]
	if !ok {
		return nil, false
	}
	var scope map[string]any
	if err := json.Unmarshal(raw, &scope); err != nil {
		t.Fatalf("scope decode: %v", err)
	}
	return scope, true
}

func TestRobotScoping_LabelRecipeRepoHonouredByEveryCommand(t *testing.T) {
	bv := buildBvBinary(t)
	repoDir := t.TempDir()
	// api-1 (backend, open, unblocked) blocks api-2 (backend); web-9 (frontend, open).
	writeIssuesJSONL(t, repoDir, strings.Join([]string{
		`{"id":"api-1","title":"Backend root","status":"open","issue_type":"task","priority":1,"labels":["backend"],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z"}`,
		`{"id":"api-2","title":"Backend follow-up","status":"open","issue_type":"task","priority":2,"labels":["backend"],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z","dependencies":[{"issue_id":"api-2","depends_on_id":"api-1","type":"blocks"}]}`,
		`{"id":"web-9","title":"Frontend only","status":"open","issue_type":"task","priority":2,"labels":["frontend"],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z"}`,
	}, "\n")+"\n")
	// The repo filter also honours source_repo; ids carry the api-/web- prefixes.

	cases := []struct {
		name       string
		flags      []string
		scopeKey   string
		scopeValue string
		excluded   string // an issue id that must not appear anywhere in the payload
	}{
		{"label", []string{"--label", "backend"}, "label", "backend", "web-9"},
		{"recipe", []string{"--recipe", "actionable"}, "recipe", "actionable", "api-2"},
		{"repo", []string{"--repo", "api"}, "repo", "api", "web-9"},
	}

	commands := loadCapabilities(t, bv)
	covered := 0
	for _, c := range commands {
		if !c.NeedsIssues || c.NeedsGit || c.NeedsSprint || c.NeedsBase || c.MutatesState {
			continue
		}
		if reason, skip := scopingSkip[c.Name]; skip {
			t.Logf("skip %s: %s", c.Name, reason)
			continue
		}
		for _, tc := range cases {
			args := append(argvFor(c.Flag, "api-1"), tc.flags...)
			r := runScoped(t, bv, repoDir, args...)
			t.Logf("%s %s: exit=%v stderr=%q", c.Name, tc.name, r.exit, strings.TrimSpace(r.stderr))
			if r.exit != nil {
				t.Errorf("%s with %v: exit %v\n%s%s", c.Name, tc.flags, r.exit, r.stdout, r.stderr)
				continue
			}
			if r.payload == nil {
				t.Errorf("%s with %v: stdout is not a JSON object:\n%s", c.Name, tc.flags, r.stdout)
				continue
			}
			scope, ok := scopeOf(t, r)
			if !ok || scope[tc.scopeKey] != tc.scopeValue {
				t.Errorf("%s with %v: envelope scope = %v, want %s=%s", c.Name, tc.flags, scope, tc.scopeKey, tc.scopeValue)
			}
			if strings.Contains(r.stdout, `"`+tc.excluded+`"`) {
				t.Errorf("%s with %v: payload mentions out-of-scope issue %s:\n%s", c.Name, tc.flags, tc.excluded, truncate(r.stdout, 800))
			}
			covered++
		}
	}
	if covered < 30 {
		t.Fatalf("matrix covered only %d (command, scope) pairs; expected at least 30", covered)
	}
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestRobotScoping_AsOfHonouredOrDeclaredUnsupported(t *testing.T) {
	bv := buildBvBinary(t)
	repoDir := t.TempDir()
	beadsDir := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	old := `{"id":"hist-old","title":"Old","status":"open","issue_type":"task","priority":1,"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z"}`
	newer := `{"id":"hist-new","title":"New","status":"open","issue_type":"task","priority":1,"created_at":"2026-08-03T00:00:00Z","updated_at":"2026-08-04T00:00:00Z"}`
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(old+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", ".beads/issues.jsonl")
	git("commit", "-q", "-m", "old")
	oldSHA := git("rev-parse", "HEAD")
	if err := os.WriteFile(issuesPath, []byte(old+"\n"+newer+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".beads/issues.jsonl")
	git("commit", "-q", "-m", "new")

	commands := loadCapabilities(t, bv)
	honoured, declared := 0, 0
	for _, c := range commands {
		if !c.NeedsIssues || c.NeedsBase || c.MutatesState {
			continue
		}
		if reason, skip := scopingSkip[c.Name]; skip {
			t.Logf("skip %s: %s", c.Name, reason)
			continue
		}
		args := append(argvFor(c.Flag, "hist-old"), "--as-of", oldSHA)
		r := runScoped(t, bv, repoDir, args...)
		t.Logf("%s --as-of: exit=%v stderr=%q", c.Name, r.exit, strings.TrimSpace(r.stderr))
		if r.exit != nil {
			// Commands needing sprint files or git correlation data may fail on
			// this bare fixture; that is fine as long as they did not silently
			// answer from HEAD. Nothing more to assert without a payload.
			if !c.NeedsSprint && !c.NeedsGit {
				t.Errorf("%s --as-of: exit %v\n%s%s", c.Name, r.exit, r.stdout, r.stderr)
			}
			continue
		}
		if r.payload == nil {
			t.Errorf("%s --as-of: not a JSON object:\n%s", c.Name, r.stdout)
			continue
		}
		var asOf, asOfCommit, sourceKind string
		_ = json.Unmarshal(r.payload["as_of"], &asOf)
		_ = json.Unmarshal(r.payload["as_of_commit"], &asOfCommit)
		_ = json.Unmarshal(r.payload["source_kind"], &sourceKind)
		if asOf != oldSHA || !shaRE.MatchString(asOfCommit) || sourceKind != "git" {
			t.Errorf("%s --as-of: envelope as_of=%q as_of_commit=%q source_kind=%q", c.Name, asOf, asOfCommit, sourceKind)
		}
		scope, _ := scopeOf(t, r)
		unsupported, _ := scope["unsupported"].([]any)
		declaresAsOf := false
		for _, u := range unsupported {
			if u == "as_of" {
				declaresAsOf = true
			}
		}
		switch {
		case c.NeedsGit || c.NeedsSprint:
			if !declaresAsOf {
				t.Errorf("%s reads live history/sprint files and must declare as_of unsupported, got scope %v", c.Name, scope)
			}
			declared++
		default:
			if declaresAsOf {
				t.Errorf("%s analyses ctx.Issues and must not declare as_of unsupported", c.Name)
			}
			if strings.Contains(r.stdout, `"hist-new"`) {
				t.Errorf("%s --as-of %s answered from HEAD (mentions hist-new):\n%s", c.Name, oldSHA[:7], truncate(r.stdout, 800))
			}
			honoured++
		}
	}
	if honoured < 12 {
		t.Fatalf("only %d commands proved to honour --as-of; expected at least 12", honoured)
	}
	t.Logf("honoured=%d declared-unsupported=%d", honoured, declared)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
