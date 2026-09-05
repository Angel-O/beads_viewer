package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type externalHistoryFixture struct {
	root       string
	storeRoot  string
	configPath string
	ledgerPath string
	repoA      string
	repoB      string
	shaA       string
	shaB       string
	sharedSHA  string
	renameSHA  string
	pathEnv    string
	bdLog      string
}

type externalHistoryPayload struct {
	Stats struct {
		TotalCommits int `json:"total_commits"`
	} `json:"stats"`
	Histories map[string]struct {
		Events  []json.RawMessage `json:"events"`
		Commits []struct {
			Repository string `json:"repository"`
		} `json:"commits"`
	} `json:"histories"`
	Warnings []struct {
		Code                string `json:"code"`
		Context             string `json:"context"`
		Reason              string `json:"reason"`
		SkippedCorrelations int    `json:"skipped_correlations"`
		Message             string `json:"message"`
	} `json:"warnings"`
}

func decodeExternalHistoryPayload(t *testing.T, out []byte) externalHistoryPayload {
	t.Helper()
	var payload externalHistoryPayload
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode external history: %v\n%s", err, out)
	}
	return payload
}

func createExternalHistoryFixture(t *testing.T) externalHistoryFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("external history fixture uses a POSIX fake bd executable")
	}
	root := t.TempDir()
	storeRoot := filepath.Join(root, "global-store")
	beadsDir := filepath.Join(storeRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	issues := strings.Join([]string{
		`{"id":"work-1","title":"Cross repo work","status":"in_progress","priority":1,"issue_type":"task","labels":["ctx:repo-a-111","ctx:repo-b-222"]}`,
		`{"id":"work-2","title":"Shared commit work","status":"open","priority":2,"issue_type":"task","labels":["ctx:repo-a-111"]}`,
		`{"id":"work-3","title":"Uncorrelated global work","status":"open","priority":3,"issue_type":"task","labels":["ctx:repo-b-222"]}`,
		`{"id":"work-todo-contextual","title":"Contextual todo","status":"open","priority":2,"issue_type":"todo","labels":["ctx:repo-a-111"]}`,
		`{"id":"work-todo-contextless","title":"Contextless todo","status":"open","priority":2,"issue_type":"todo","labels":[]}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(issues), 0o600); err != nil {
		t.Fatal(err)
	}

	createRepo := func(name, content string) (string, string, string) {
		repo := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		runGit := func(args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=External Test", "GIT_AUTHOR_EMAIL=external@example.com",
				"GIT_COMMITTER_NAME=External Test", "GIT_COMMITTER_EMAIL=external@example.com",
				"GIT_AUTHOR_DATE=2026-01-02T03:04:05Z", "GIT_COMMITTER_DATE=2026-01-02T03:04:05Z")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
			return strings.TrimSpace(string(out))
		}
		runGit("init", "--quiet")
		runGit("commit", "--quiet", "--allow-empty", "-m", "shared root")
		sharedSHA := runGit("rev-parse", "HEAD")
		if err := os.WriteFile(filepath.Join(repo, "src", "config.ts"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if name == "repo-a" {
			if err := os.WriteFile(filepath.Join(repo, "src", "helper.ts"), []byte("export const helper = true;\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		runGit("add", "src")
		runGit("commit", "--quiet", "-m", "implement shared configuration")
		return repo, runGit("rev-parse", "HEAD"), sharedSHA
	}
	repoA, shaA, sharedSHA := createRepo("repo-a", "export const repo = 'a';\nexport const one = 1;\nexport const two = 2;\nexport const three = 3;\n")
	repoB, shaB, sharedSHAB := createRepo("repo-b", "export const repo = 'b';\n")
	if sharedSHA != sharedSHAB {
		t.Fatalf("fixture root commits should share SHA text: %s != %s", sharedSHA, sharedSHAB)
	}
	if err := os.Rename(filepath.Join(repoA, "src", "config.ts"), filepath.Join(repoA, "src", "settings.ts")); err != nil {
		t.Fatal(err)
	}
	settings, err := os.OpenFile(filepath.Join(repoA, "src", "settings.ts"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.WriteString("export const renamed = true;\n"); err != nil {
		settings.Close()
		t.Fatal(err)
	}
	if err := settings.Close(); err != nil {
		t.Fatal(err)
	}
	renameGit := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repoA}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=External Test", "GIT_AUTHOR_EMAIL=external@example.com",
			"GIT_COMMITTER_NAME=External Test", "GIT_COMMITTER_EMAIL=external@example.com",
			"GIT_AUTHOR_DATE=2026-01-04T03:04:05Z", "GIT_COMMITTER_DATE=2026-01-04T03:04:05Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git rename %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	renameGit("add", "-A")
	renameGit("commit", "--quiet", "-m", "rename repository configuration")
	renameSHA := renameGit("rev-parse", "HEAD")

	ledgerPath := filepath.Join(root, "private", "correlations.jsonl")
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := fmt.Sprintf(
		"{\"bead_id\":\"work-1\",\"context\":\"ctx:repo-a-111\",\"commit\":%q}\n"+
			"{\"bead_id\":\"work-2\",\"context\":\"ctx:repo-a-111\",\"commit\":%q}\n"+
			"{\"bead_id\":\"work-1\",\"context\":\"ctx:repo-b-222\",\"commit\":%q}\n"+
			"{\"bead_id\":\"work-1\",\"context\":\"ctx:repo-a-111\",\"commit\":%q}\n"+
			"{\"bead_id\":\"work-1\",\"context\":\"ctx:repo-b-222\",\"commit\":%q}\n"+
			"{\"bead_id\":\"work-1\",\"context\":\"ctx:repo-a-111\",\"commit\":%q}\n", shaA, shaA, shaB, sharedSHA, sharedSHA, renameSHA)
	if err := os.WriteFile(ledgerPath, []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "hub.yaml")
	config := fmt.Sprintf("version: 1\nstore: %q\nledger: %q\nrepositories:\n  ctx:repo-a-111:\n    path: %q\n  ctx:repo-b-222:\n    path: %q\n", beadsDir, ledgerPath, repoA, repoB)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bdLog := filepath.Join(root, "bd-invocations.log")
	bdScript := `#!/bin/sh
if [ -n "$BV_FAKE_BD_LOG" ]; then
  printf '%s\n' "$*" >> "$BV_FAKE_BD_LOG"
fi
bead=""
for arg in "$@"; do
  case "$arg" in work-*) bead="$arg";; esac
done
case " $* " in
  *" show "*)
	issue_type=task
	case "$bead" in
	  work-1) labels='["ctx:repo-a-111","ctx:repo-b-222"]' ;;
	  work-2) labels='["ctx:repo-a-111"]' ;;
	  work-3) labels='["ctx:repo-b-222"]' ;;
	  work-custom) issue_type=review; labels='["ctx:repo-a-111"]' ;;
	  work-kind-absent) printf '[{"id":"%s","labels":["ctx:repo-a-111"]}]\n' "$bead"; exit 0 ;;
	  work-kind-empty) issue_type=; labels='["ctx:repo-a-111"]' ;;
	  work-kind-null) printf '[{"id":"%s","issue_type":null,"labels":["ctx:repo-a-111"]}]\n' "$bead"; exit 0 ;;
	  work-todo-contextual) issue_type=todo; labels='["ctx:repo-a-111"]' ;;
	  work-todo-contextless) issue_type=todo; labels='[]' ;;
	  work-missing) printf '[]\n'; exit 0 ;;
	  work-query-fail) echo "store query failed" >&2; exit 42 ;;
	  *) echo "unknown bead: $bead" >&2; exit 1 ;;
	esac
	printf '[{"id":"%s","issue_type":"%s","labels":%s}]\n' "$bead" "$issue_type" "$labels"
    exit 0
    ;;
esac
printf '{"schema_version":1,"issues":['
separator=
for arg in "$@"; do
  case "$arg" in
    work-*)
      bead="$arg"
      printf '%s{"issue_id":"%s","snapshots":[{"CommitHash":"dolt-closed-%s","Committer":"Lifecycle Bot","CommitDate":"2026-01-03T03:04:05Z","Issue":{"id":"%s","status":"in_progress","issue_type":"task"}},{"CommitHash":"dolt-created-%s","Committer":"Lifecycle Bot","CommitDate":"2026-01-01T03:04:05Z","Issue":{"id":"%s","status":"open","issue_type":"task"}}]}' "$separator" "$bead" "$bead" "$bead" "$bead" "$bead"
      separator=,
      ;;
  esac
done
printf ']}\n'
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(bdScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return externalHistoryFixture{root: root, storeRoot: storeRoot, configPath: configPath, ledgerPath: ledgerPath, repoA: repoA, repoB: repoB, shaA: shaA, shaB: shaB, sharedSHA: sharedSHA, renameSHA: renameSHA, pathEnv: binDir + string(os.PathListSeparator) + os.Getenv("PATH"), bdLog: bdLog}
}

func (f externalHistoryFixture) command(t *testing.T, bv string, args ...string) ([]byte, error) {
	t.Helper()
	return f.commandFrom(t, bv, f.storeRoot, args...)
}

func (f externalHistoryFixture) commandFrom(t *testing.T, bv, dir string, args ...string) ([]byte, error) {
	t.Helper()
	return f.commandFromEnv(t, bv, dir, nil, args...)
}

func (f externalHistoryFixture) commandFromEnv(t *testing.T, bv, dir string, extraEnv []string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(bv, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+f.pathEnv, "BV_NO_CACHE=1", "BV_FAKE_BD_LOG="+f.bdLog)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd.CombinedOutput()
}

func TestExternalConfigStoreDrivesIssueLoading(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	unrelated := filepath.Join(fixture.root, "unrelated")
	if err := os.MkdirAll(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit := exec.Command("git", "-C", unrelated, "init", "--quiet")
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init unrelated cwd: %v\n%s", err, out)
	}
	worktreeBeads := filepath.Join(unrelated, ".git", "beads-worktrees", "newer")
	if err := os.MkdirAll(worktreeBeads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeBeads, "issues.jsonl"), []byte(`{"id":"wrong-worktree","title":"Wrong","status":"open","priority":0,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := fixture.commandFrom(t, bv, unrelated, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("external history from unrelated cwd failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"work-1"`) || !strings.Contains(string(out), `"work-3"`) || strings.Contains(string(out), "wrong-worktree") {
		t.Fatalf("configured store did not drive issue loading: %s", out)
	}

	override := filepath.Join(fixture.root, "override.jsonl")
	if err := os.WriteFile(override, []byte(`{"id":"override-1","title":"Explicit DB","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = fixture.commandFrom(t, bv, unrelated, "--robot-graph", "--history-mode", "external", "--hub-config", fixture.configPath, "--db", override)
	if err != nil {
		t.Fatalf("explicit --db precedence failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "override-1") || strings.Contains(string(out), "work-1") {
		t.Fatalf("explicit --db did not override config.store: %s", out)
	}
}

func TestOldWorkConfigFlagIsNotAccepted(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	out, err := fixture.command(t, bv, "--robot-history", "--work-config", fixture.configPath)
	if err == nil || !strings.Contains(string(out), "unknown flag: --work-config") {
		t.Fatalf("old custom flag remains accepted: err=%v output=%s", err, out)
	}
}

func TestExternalStoreRejectsAlternateIssueSources(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	for _, args := range [][]string{
		{"--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath, "--workspace", filepath.Join(fixture.root, "workspace.yaml")},
		{"--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath, "--as-of", "HEAD"},
	} {
		out, err := fixture.command(t, bv, args...)
		if err == nil || !strings.Contains(string(out), "config.store is authoritative") {
			t.Fatalf("external mode accepted alternate issue source %v: err=%v output=%s", args, err, out)
		}
	}
}

func TestExternalHistoryAllowsMissingLedger(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	missingLedger := filepath.Join(fixture.root, "private", "not-created.jsonl")
	configData, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.configPath, []byte(strings.Replace(string(configData), fixture.ledgerPath, missingLedger, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(fixture.root, "missing-ledger-cwd")
	if err := os.MkdirAll(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := fixture.commandFrom(t, bv, unrelated, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("missing ledger should load as empty: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "not a git repository") || !strings.Contains(string(out), `"total_commits":0`) || !strings.Contains(string(out), `"work-1"`) {
		t.Fatalf("missing ledger produced fallback/error instead of empty source history: %s", out)
	}
	out, err = fixture.commandFrom(t, bv, unrelated, "--robot-file-hotspots", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("missing ledger should permit empty hotspot history: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "not a git repository") || !strings.Contains(string(out), `"hotspots":[]`) {
		t.Fatalf("missing ledger did not produce empty file history: %s", out)
	}
}

func TestExternalCorrelationRemovalStopsReaderSurfacingExactTuple(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	remove := []string{"correlate", "remove", "--bead", "work-2", "--repo", "ctx:repo-a-111", "--commit", fixture.shaA, "--hub-config", fixture.configPath}
	out, err := fixture.command(t, bv, remove...)
	if err != nil {
		t.Fatalf("remove correlation: %v\n%s", err, out)
	}
	var result struct {
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal(out, &result); err != nil || !result.Removed {
		t.Fatalf("removal result: %v, payload=%s", err, out)
	}

	historyOut, err := fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("history after removal: %v\n%s", err, historyOut)
	}
	payload := decodeExternalHistoryPayload(t, historyOut)
	if payload.Stats.TotalCommits != 5 || len(payload.Histories["work-2"].Commits) != 0 {
		t.Fatalf("removed tuple still surfaced: %+v", payload)
	}

	out, err = fixture.command(t, bv, remove...)
	if err != nil {
		t.Fatalf("idempotent remove: %v\n%s", err, out)
	}
	if err := json.Unmarshal(out, &result); err != nil || result.Removed {
		t.Fatalf("not-found result: %v, payload=%s", err, out)
	}
	wantNotFound := fmt.Sprintf("{\"correlation\":{\"bead_id\":\"work-2\",\"context\":\"ctx:repo-a-111\",\"commit\":%q},\"removed\":false}\n", fixture.shaA)
	if string(out) != wantNotFound {
		t.Fatalf("not-found JSON = %q, want %q", out, wantNotFound)
	}
}

func TestExternalHistoryDoesNotProbeUnusedOrUnselectedRepositories(t *testing.T) {
	bv := buildBvBinary(t)
	t.Run("unused configured context", func(t *testing.T) {
		fixture := createExternalHistoryFixture(t)
		config, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		missing := filepath.Join(fixture.root, "unused-missing")
		config = append(config, []byte(fmt.Sprintf("  ctx:unused-333:\n    path: %q\n", missing))...)
		if err := os.WriteFile(fixture.configPath, config, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
		if err != nil {
			t.Fatalf("unused stale context failed history: %v\n%s", err, out)
		}
		if payload := decodeExternalHistoryPayload(t, out); len(payload.Warnings) != 0 || payload.Stats.TotalCommits != 5 {
			t.Fatalf("unused context was probed or changed history: %+v", payload)
		}
	})

	t.Run("selected bead excludes another context", func(t *testing.T) {
		fixture := createExternalHistoryFixture(t)
		config, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		config = []byte(strings.Replace(string(config), fixture.repoB, filepath.Join(fixture.root, "missing-repo-b"), 1))
		if err := os.WriteFile(fixture.configPath, config, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := fixture.command(t, bv, "--bead-history", "work-2", "--history-mode", "external", "--hub-config", fixture.configPath)
		if err != nil {
			t.Fatalf("selected history probed unrelated stale context: %v\n%s", err, out)
		}
		payload := decodeExternalHistoryPayload(t, out)
		if len(payload.Warnings) != 0 || payload.Stats.TotalCommits != 1 || len(payload.Histories["work-2"].Events) != 2 {
			t.Fatalf("selected history was incomplete or warned: %+v", payload)
		}
	})
}

func TestExternalHistoryReturnsPartialReportsForUnavailableRepositories(t *testing.T) {
	bv := buildBvBinary(t)
	t.Run("one unavailable context", func(t *testing.T) {
		fixture := createExternalHistoryFixture(t)
		config, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		missing := filepath.Join(fixture.root, "missing-repo-a")
		config = []byte(strings.Replace(string(config), fixture.repoA, missing, 1))
		if err := os.WriteFile(fixture.configPath, config, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
		if err != nil {
			t.Fatalf("partial external history failed: %v\n%s", err, out)
		}
		payload := decodeExternalHistoryPayload(t, out)
		if payload.Stats.TotalCommits != 2 || len(payload.Warnings) != 1 || len(payload.Histories["work-1"].Events) != 2 {
			t.Fatalf("unexpected partial report: %+v", payload)
		}
		warning := payload.Warnings[0]
		if warning.Code != "external_repository_unavailable" || warning.Context != "ctx:repo-a-111" || warning.Reason != "not_found" || warning.SkippedCorrelations != 4 {
			t.Fatalf("unexpected warning: %+v", warning)
		}
		if strings.Contains(string(out), missing) || strings.Contains(warning.Message, fixture.root) {
			t.Fatalf("warning leaked private checkout path: %s", out)
		}

		triageOut, err := fixture.command(t, bv, "--robot-triage", "--history-mode", "external", "--hub-config", fixture.configPath, "--robot-history-timeout-ms", "0")
		if err != nil {
			t.Fatalf("partial robot triage failed: %v\n%s", err, triageOut)
		}
		var triage struct {
			Triage struct {
				Meta struct {
					HistoryStatus   string            `json:"history_status"`
					HistoryWarnings []json.RawMessage `json:"history_warnings"`
				} `json:"meta"`
			} `json:"triage"`
		}
		if err := json.Unmarshal(triageOut, &triage); err != nil {
			t.Fatalf("decode triage: %v\n%s", err, triageOut)
		}
		if triage.Triage.Meta.HistoryStatus != "partial" || len(triage.Triage.Meta.HistoryWarnings) != 1 {
			t.Fatalf("triage did not expose partial history: %s", triageOut)
		}

		hotspotsOut, err := fixture.command(t, bv, "--robot-file-hotspots", "--history-mode", "external", "--hub-config", fixture.configPath)
		if err != nil {
			t.Fatalf("partial file hotspots failed: %v\n%s", err, hotspotsOut)
		}
		var hotspots struct {
			HistoryWarnings []json.RawMessage `json:"history_warnings"`
		}
		if err := json.Unmarshal(hotspotsOut, &hotspots); err != nil {
			t.Fatalf("decode hotspots: %v\n%s", err, hotspotsOut)
		}
		if len(hotspots.HistoryWarnings) != 1 || strings.Contains(string(hotspotsOut), missing) {
			t.Fatalf("history-derived output hid warning or leaked path: %s", hotspotsOut)
		}

		explainOut, err := fixture.command(t, bv, "--robot-explain-correlation", "ctx:repo-a-111@"+fixture.shaA+":work-1", "--history-mode", "external", "--hub-config", fixture.configPath)
		if err == nil {
			t.Fatalf("explain should not claim an omitted commit is available: %s", explainOut)
		}
		var explanation struct {
			Status          string            `json:"status"`
			HistoryWarnings []json.RawMessage `json:"history_warnings"`
		}
		if err := json.Unmarshal(explainOut, &explanation); err != nil {
			t.Fatalf("decode partial explanation: %v\n%s", err, explainOut)
		}
		if explanation.Status != "history_partial" || len(explanation.HistoryWarnings) != 1 {
			t.Fatalf("partial explanation hid repository warning: %s", explainOut)
		}
	})

	t.Run("all applicable contexts unavailable", func(t *testing.T) {
		fixture := createExternalHistoryFixture(t)
		config, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		configText := strings.Replace(string(config), fixture.repoA, filepath.Join(fixture.root, "missing-a"), 1)
		configText = strings.Replace(configText, fixture.repoB, filepath.Join(fixture.root, "missing-b"), 1)
		if err := os.WriteFile(fixture.configPath, []byte(configText), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
		if err != nil {
			t.Fatalf("lifecycle-only partial history failed: %v\n%s", err, out)
		}
		payload := decodeExternalHistoryPayload(t, out)
		if payload.Stats.TotalCommits != 0 || len(payload.Warnings) != 2 || len(payload.Histories["work-1"].Events) != 2 || len(payload.Histories["work-2"].Events) != 2 {
			t.Fatalf("all-unavailable report lost lifecycle data or warnings: %+v", payload)
		}
		if payload.Warnings[0].Context != "ctx:repo-a-111" || payload.Warnings[0].SkippedCorrelations != 4 || payload.Warnings[1].Context != "ctx:repo-b-222" || payload.Warnings[1].SkippedCorrelations != 2 {
			t.Fatalf("warnings were not deterministic: %+v", payload.Warnings)
		}
	})
}

func TestExternalHistorySelectedQuerySkipsUnknownLedgerReference(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	file, err := os.OpenFile(fixture.ledgerPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintf(file, "{\"bead_id\":\"unknown-bead\",\"context\":\"ctx:repo-b-222\",\"commit\":%q}\n", fixture.shaB)
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	out, err := fixture.command(t, bv, "--bead-history", "work-2", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("selected query failed on stale unrelated ledger record: %v\n%s", err, out)
	}
	payload := decodeExternalHistoryPayload(t, out)
	commits := payload.Histories["work-2"].Commits
	if len(commits) != 1 || commits[0].Repository != "ctx:repo-a-111" {
		t.Fatalf("selected query lost valid ledger correlation: %#v", commits)
	}
}

func TestHistoryOffDiscoversConfigStoreWithoutProviders(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	home := filepath.Join(fixture.root, "off-home")
	configDir := filepath.Join(home, ".config", "bv")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "hub.yaml"), configData, 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(fixture.root, "off-unrelated")
	if err := os.MkdirAll(filepath.Join(unrelated, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	localIssue := `{"id":"local-only","title":"Local","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(filepath.Join(unrelated, ".beads", "issues.jsonl"), []byte(localIssue), 0o600); err != nil {
		t.Fatal(err)
	}

	failBin := filepath.Join(fixture.root, "off-fail-bin")
	if err := os.MkdirAll(failBin, 0o755); err != nil {
		t.Fatal(err)
	}
	providerLog := filepath.Join(fixture.root, "off-provider.log")
	providerScript := fmt.Sprintf("#!/bin/sh\nprintf 'invoked %%s %%s\\n' \"$0\" \"$*\" >> %q\nexit 97\n", providerLog)
	for _, name := range []string{"git", "bd"} {
		if err := os.WriteFile(filepath.Join(failBin, name), []byte(providerScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	autoEnv := []string{"HOME=" + home, "BV_NO_GITIGNORE=1"}
	out, err := fixture.commandFromEnv(t, bv, unrelated, autoEnv, "--robot-history")
	if err != nil {
		t.Fatalf("auto mode with conventional config failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"work-1"`) || strings.Contains(string(out), "local-only") {
		t.Fatalf("auto mode did not retain Hub-first behavior: %s", out)
	}

	env := []string{"HOME=" + home, "PATH=" + failBin + string(os.PathListSeparator) + os.Getenv("PATH"), "BV_NO_GITIGNORE=1"}
	out, err = fixture.commandFromEnv(t, bv, unrelated, env, "--robot-history", "--history-mode", "off")
	if err != nil {
		t.Fatalf("off mode with conventional config failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"work-1"`) || strings.Contains(string(out), "local-only") || !strings.Contains(string(out), `"git_range":"history disabled"`) {
		t.Fatalf("off mode did not load configured global store: %s", out)
	}
	if data, readErr := os.ReadFile(providerLog); readErr == nil && len(data) > 0 {
		t.Fatalf("off mode invoked a history provider: %s", data)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}

	out, err = fixture.commandFromEnv(t, bv, unrelated, []string{"HOME=" + home}, "--robot-graph", "--history-mode", "git")
	if err != nil {
		t.Fatalf("git mode local issue load failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "local-only") || strings.Contains(string(out), "work-1") {
		t.Fatalf("git mode unexpectedly adopted conventional hub config: %s", out)
	}
}

func TestExternalHistoryUsesRealRepositoriesAndBeadsLifecycle(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	out, err := fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("external history failed: %v\n%s", err, out)
	}
	var payload struct {
		GitRange               string `json:"git_range"`
		LatestCommitSHA        string `json:"latest_commit_sha"`
		LatestCommitRepository string `json:"latest_commit_repository"`
		Stats                  struct {
			TotalCommits int `json:"total_commits"`
		} `json:"stats"`
		Histories map[string]struct {
			Events []struct {
				EventType string `json:"event_type"`
			} `json:"events"`
			Commits []struct {
				Repository string `json:"repository"`
				SHA        string `json:"sha"`
				Message    string `json:"message"`
				Method     string `json:"method"`
				Files      []struct {
					Repository, Path, Action string
					Insertions               int `json:"insertions"`
				} `json:"files"`
			} `json:"commits"`
		} `json:"histories"`
		CommitIndex map[string][]string `json:"commit_index"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if payload.GitRange != "external hub history" || payload.Stats.TotalCommits != 5 {
		t.Fatalf("unexpected external summary: range=%q commits=%d", payload.GitRange, payload.Stats.TotalCommits)
	}
	if payload.LatestCommitRepository == "" || strings.HasPrefix(payload.LatestCommitSHA, "dolt-") {
		t.Fatalf("latest source commit was polluted by lifecycle identity: %q %q", payload.LatestCommitRepository, payload.LatestCommitSHA)
	}
	work1 := payload.Histories["work-1"]
	if len(work1.Events) != 2 || work1.Events[0].EventType != "created" || work1.Events[1].EventType != "claimed" {
		t.Fatalf("unexpected lifecycle events: %+v", work1.Events)
	}
	if len(work1.Commits) != 5 {
		t.Fatalf("work-1 commits=%d, want 5", len(work1.Commits))
	}
	contexts := map[string]bool{}
	for _, commit := range work1.Commits {
		contexts[commit.Repository] = true
		if commit.Method != "external_ledger" {
			t.Fatalf("external association used wrong method: %+v", commit)
		}
		if commit.Message == "shared root" || commit.Message == "rename repository configuration" {
			continue
		}
		if commit.Message != "implement shared configuration" {
			t.Fatalf("commit did not contain real Git metadata: %+v", commit)
		}
		foundConfig := false
		for _, file := range commit.Files {
			if file.Path == "src/config.ts" && file.Repository == commit.Repository && file.Insertions > 0 {
				foundConfig = true
			}
		}
		if !foundConfig {
			t.Fatalf("commit did not contain namespaced config.ts stats: %+v", commit)
		}
	}
	if !contexts["ctx:repo-a-111"] || !contexts["ctx:repo-b-222"] {
		t.Fatalf("missing repository identities: %v", contexts)
	}
	if len(payload.CommitIndex["ctx:repo-a-111:"+fixture.shaA]) != 2 || len(payload.CommitIndex["ctx:repo-b-222:"+fixture.shaB]) != 1 {
		t.Fatalf("repository-aware commit index missing mappings: %v", payload.CommitIndex)
	}
	if len(payload.CommitIndex["ctx:repo-a-111:"+fixture.sharedSHA]) != 1 || len(payload.CommitIndex["ctx:repo-b-222:"+fixture.sharedSHA]) != 1 {
		t.Fatalf("same SHA text collided across repositories: %v", payload.CommitIndex)
	}
	foundRename := false
	for _, commit := range work1.Commits {
		if commit.SHA != fixture.renameSHA {
			continue
		}
		for _, file := range commit.Files {
			if file.Action == "R" && file.Path == "src/settings.ts" && file.Insertions == 1 {
				foundRename = true
			}
		}
	}
	if !foundRename {
		t.Fatalf("external report did not preserve rename destination/action/stats: %+v", work1.Commits)
	}
	logData, err := os.ReadFile(fixture.bdLog)
	if err != nil {
		t.Fatal(err)
	}
	bulkInvocation := fmt.Sprintf("--db %s --readonly history work-1 work-2 --json", filepath.Join(fixture.storeRoot, ".beads"))
	if strings.Count(string(logData), bulkInvocation) != 1 {
		t.Fatalf("lifecycle provider queried the wrong beads: %s", logData)
	}
	if err := os.WriteFile(fixture.bdLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = fixture.command(t, bv, "--bead-history", "work-3", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("lifecycle-only bead history failed: %v\n%s", err, out)
	}
	logData, err = os.ReadFile(fixture.bdLog)
	if err != nil {
		t.Fatal(err)
	}
	selectedInvocation := fmt.Sprintf("--db %s --readonly history work-3 --json", filepath.Join(fixture.storeRoot, ".beads"))
	if strings.Count(string(logData), selectedInvocation) != 1 {
		t.Fatalf("selected lifecycle-only bead query was not scoped: %s", logData)
	}
}

func TestExternalHistoryFileQueriesAreNamespaced(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	out, err := fixture.command(t, bv, "--robot-file-hotspots", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("hotspots failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"file_path":"ctx:repo-a-111:src/config.ts"`) || !strings.Contains(string(out), `"file_path":"ctx:repo-b-222:src/config.ts"`) {
		t.Fatalf("hotspots did not namespace duplicate paths: %s", out)
	}
	out, err = fixture.command(t, bv, "--robot-file-beads", "ctx:repo-a-111:src/config.ts", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("file-beads failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"total_beads":2`) {
		t.Fatalf("file-beads did not return both explicitly correlated beads: %s", out)
	}
	out, err = fixture.command(t, bv, "--robot-file-relations", "ctx:repo-a-111:src/config.ts", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("file-relations failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"file_path":"ctx:repo-a-111:src/helper.ts"`) {
		t.Fatalf("co-change output was not repository-aware: %s", out)
	}
}

func TestHistoryOffDoesNotInvokeGitOrBeads(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	binDir := filepath.Join(fixture.root, "fail-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"git", "bd"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 97\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fixture.pathEnv = binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	out, err := fixture.command(t, bv, "--robot-history", "--history-mode", "off")
	if err != nil {
		t.Fatalf("history off failed or invoked a provider: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "not a git repository") || !strings.Contains(string(out), `"git_range":"history disabled"`) {
		t.Fatalf("unexpected off output: %s", out)
	}
}

func TestCorrelateAddResolvesFullSHAAndDeduplicates(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	if err := os.WriteFile(fixture.ledgerPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"correlate", "add", "--bead", "work-1", "--repo", fixture.repoA, "--commit", "HEAD", "--hub-config", fixture.configPath}
	out, err := fixture.command(t, bv, args...)
	if err != nil {
		t.Fatalf("correlate add failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"added":true`) || !strings.Contains(string(out), fixture.renameSHA) {
		t.Fatalf("unexpected add output: %s", out)
	}
	out, err = fixture.command(t, bv, args...)
	if err != nil {
		t.Fatalf("duplicate correlate add failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"added":false`) {
		t.Fatalf("duplicate was not suppressed: %s", out)
	}
	data, err := os.ReadFile(fixture.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(string(data)), "\n") != 0 || !strings.Contains(string(data), fixture.renameSHA) {
		t.Fatalf("ledger should contain exactly one full-SHA record: %s", data)
	}
}

func TestCorrelateAddAllowsCustomIssueType(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	if err := os.WriteFile(fixture.ledgerPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := fixture.command(t, bv, "correlate", "add", "--bead", "work-custom", "--repo", "ctx:repo-a-111", "--commit", "HEAD", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("custom issue type should be eligible: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"added":true`) {
		t.Fatalf("unexpected custom issue type output: %s", out)
	}
}

func TestCorrelateAddRejectsIneligibleIssuesBeforeGitAndLedgerAccess(t *testing.T) {
	bv := buildBvBinary(t)
	tests := []struct {
		name       string
		bead       string
		want       string
		existing   []byte
		useMissing bool
	}{
		{name: "missing issue type", bead: "work-kind-absent", want: "does not provide a non-empty issue_type", existing: []byte("{malformed legacy ledger}\n")},
		{name: "empty issue type", bead: "work-kind-empty", want: "does not provide a non-empty issue_type", existing: []byte("{malformed legacy ledger}\n")},
		{name: "null issue type", bead: "work-kind-null", want: "does not provide a non-empty issue_type", existing: []byte("{malformed legacy ledger}\n")},
		{name: "contextual todo with existing ledger", bead: "work-todo-contextual", want: "is a todo and cannot be correlated", existing: []byte("{malformed legacy ledger}\n")},
		{name: "contextless todo with absent ledger", bead: "work-todo-contextless", want: "is a todo and cannot be correlated", useMissing: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := createExternalHistoryFixture(t)
			ledgerPath := fixture.ledgerPath
			if test.useMissing {
				ledgerPath = filepath.Join(fixture.root, "not-created", "private", "correlations.jsonl")
				config := fmt.Sprintf("version: 1\nstore: %q\nledger: %q\nrepositories:\n  ctx:repo-a-111:\n    path: %q\n  ctx:repo-b-222:\n    path: %q\n", filepath.Join(fixture.storeRoot, ".beads"), ledgerPath, fixture.repoA, fixture.repoB)
				if err := os.WriteFile(fixture.configPath, []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(ledgerPath, test.existing, 0o600); err != nil {
				t.Fatal(err)
			}

			gitLog := filepath.Join(fixture.root, "git-invocations.log")
			gitScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$BV_FAKE_GIT_LOG\"\nexit 88\n"
			if err := os.WriteFile(filepath.Join(fixture.root, "bin", "git"), []byte(gitScript), 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := fixture.commandFromEnv(t, bv, fixture.storeRoot, []string{"BV_FAKE_GIT_LOG=" + gitLog}, "correlate", "add", "--bead", test.bead, "--repo", "ctx:repo-a-111", "--commit", "HEAD", "--hub-config", fixture.configPath)
			if err == nil || !strings.Contains(string(out), test.want) {
				t.Fatalf("expected %q rejection, got err=%v output=%s", test.want, err, out)
			}
			if _, statErr := os.Stat(gitLog); !os.IsNotExist(statErr) {
				t.Fatalf("ineligible issue rejection invoked Git: %v", statErr)
			}
			if _, statErr := os.Stat(ledgerPath + ".lock"); !os.IsNotExist(statErr) {
				t.Fatalf("ineligible issue rejection accessed ledger lock: %v", statErr)
			}
			if test.useMissing {
				if _, statErr := os.Stat(filepath.Dir(ledgerPath)); !os.IsNotExist(statErr) {
					t.Fatalf("ineligible issue rejection created ledger directory: %v", statErr)
				}
			} else {
				got, readErr := os.ReadFile(ledgerPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(got) != string(test.existing) {
					t.Fatalf("ineligible issue rejection changed ledger bytes: got %q want %q", got, test.existing)
				}
			}
		})
	}
}

func TestCorrelateAddDeduplicatesWhitespaceEquivalentLegacyIdentity(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	legacy := fmt.Sprintf("{\"bead_id\":\" work-1 \",\"context\":\" ctx:repo-a-111 \",\"commit\":\"  %s  \",\"legacy_metadata\":{\"keep\":true}}\n", strings.ToUpper(fixture.renameSHA))
	if err := os.WriteFile(fixture.ledgerPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := fixture.command(t, bv, "correlate", "add", "--bead", "work-1", "--repo", "ctx:repo-a-111", "--commit", "HEAD", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("whitespace-equivalent duplicate failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"added":false`) {
		t.Fatalf("whitespace-equivalent duplicate was not suppressed: %s", out)
	}
	data, err := os.ReadFile(fixture.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != legacy {
		t.Fatalf("duplicate changed legacy ledger bytes: got %q want %q", data, legacy)
	}
}

func TestCorrelateAddPreservesUnrelatedLegacyRecord(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	legacy := fmt.Sprintf("{\"bead_id\":\"retired-work\",\"context\":\"ctx:retired-repo\",\"commit\":%q,\"legacy_metadata\":{\"keep\":true}}\n", fixture.shaA)
	if err := os.WriteFile(fixture.ledgerPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := fixture.command(t, bv, "correlate", "add", "--bead", "work-1", "--repo", "ctx:repo-a-111", "--commit", "HEAD", "--hub-config", fixture.configPath)
	if err != nil {
		t.Fatalf("correlate add with unrelated legacy record failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(fixture.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), legacy) {
		t.Fatalf("legacy record was not preserved verbatim: %s", data)
	}
	if strings.Count(strings.TrimSpace(string(data)), "\n") != 1 || !strings.Contains(string(data), fixture.renameSHA) {
		t.Fatalf("ledger should contain the legacy record and one valid delta: %s", data)
	}
}

func TestCorrelateAddValidatesBeadContextBeforeLedgerMutation(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)
	if err := os.WriteFile(fixture.ledgerPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, bead, repo, want string
	}{
		{name: "unknown bead", bead: "work-missing", repo: "ctx:repo-a-111", want: "was not found"},
		{name: "mismatched context", bead: "work-2", repo: "ctx:repo-b-222", want: "does not carry context label"},
		{name: "query failure", bead: "work-query-fail", repo: "ctx:repo-a-111", want: "validating bead"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out, err := fixture.command(t, bv, "correlate", "add", "--bead", test.bead, "--repo", test.repo, "--commit", "HEAD", "--hub-config", fixture.configPath)
			if err == nil || !strings.Contains(string(out), test.want) {
				t.Fatalf("expected %q error, got err=%v output=%s", test.want, err, out)
			}
			data, readErr := os.ReadFile(fixture.ledgerPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(data) != 0 {
				t.Fatalf("failed validation mutated ledger: %s", data)
			}
		})
	}
}

func TestExternalHistoryDiagnosticsDoNotFallback(t *testing.T) {
	bv := buildBvBinary(t)
	fixture := createExternalHistoryFixture(t)

	badLedger := fmt.Sprintf("{\"bead_id\":\"work-1\",\"context\":\"ctx:missing-999\",\"commit\":%q}\n", fixture.shaA)
	if err := os.WriteFile(fixture.ledgerPath, []byte(badLedger), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err == nil {
		t.Fatalf("expected invalid context failure: %s", out)
	}
	if !strings.Contains(string(out), "undefined context") || strings.Contains(string(out), "not a git repository") {
		t.Fatalf("diagnostic was not actionable or fell back to store Git: %s", out)
	}

	badLedger = `{"bead_id":"work-1","context":"ctx:repo-a-111","commit":"deadbee"}` + "\n"
	if err := os.WriteFile(fixture.ledgerPath, []byte(badLedger), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err == nil || !strings.Contains(string(out), "must be a full 40- or 64-character") || strings.Contains(string(out), "not a git repository") {
		t.Fatalf("abbreviated commit diagnostic was not actionable: %s", out)
	}

	badLedger = `{"bead_id":"work-1","context":"ctx:repo-a-111","commit":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}` + "\n"
	if err := os.WriteFile(fixture.ledgerPath, []byte(badLedger), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = fixture.command(t, bv, "--robot-history", "--history-mode", "external", "--hub-config", fixture.configPath)
	if err == nil || !strings.Contains(string(out), "absent from repository") || strings.Contains(string(out), "not a git repository") {
		t.Fatalf("missing full commit diagnostic was not actionable: %s", out)
	}
}
