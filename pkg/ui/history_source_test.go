package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/internal/datasource"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// writeFileAt writes content to path and sets its mtime (and atime) to the given
// time so tests can deterministically reproduce sub-second mtime skew between the
// SQLite DB and the JSONL export.
func writeFileAt(t *testing.T, path string, content []byte, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestLoadHistoryWithProviderOffFeedsTUIModel(t *testing.T) {
	cmd := loadHistoryWithProviderCmd(
		[]model.Issue{{ID: "work-1", Title: "Work", Status: model.StatusOpen, IssueType: model.TypeBug}},
		"",
		correlation.NewDisabledProvider(),
	)
	msg, ok := cmd().(HistoryLoadedMsg)
	if !ok {
		t.Fatalf("unexpected message type %T", cmd())
	}
	if msg.Error != nil {
		t.Fatalf("off provider returned error: %v", msg.Error)
	}
	if msg.Report == nil || msg.Report.GitRange != "history disabled" || len(msg.Report.Histories) != 1 {
		t.Fatalf("unexpected off-mode TUI report: %+v", msg.Report)
	}
	if got := msg.Report.Histories["work-1"].IssueType; got != string(model.TypeBug) {
		t.Fatalf("History conversion lost issue type: got %q, want %q", got, model.TypeBug)
	}
}

func TestHistoryStartupUsesSuppliedProviderOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	m := NewModel([]model.Issue{{
		ID: "work-1", Title: "Work", Status: model.StatusOpen, IssueType: model.TypeBug,
	}}, nil, "", RuntimeServices{HistoryProvider: correlation.NewDisabledProvider()})
	defer m.cancelHistoryLoad()

	cmd := m.startHistoryLoad()
	if cmd == nil {
		t.Fatal("startup history command was not scheduled")
	}
	result := cmd()
	msg, ok := result.(HistoryLoadedMsg)
	if !ok {
		t.Fatalf("unexpected message type %T", result)
	}
	if msg.Error != nil {
		t.Fatalf("supplied provider returned error outside repository: %v", msg.Error)
	}
	if msg.Report == nil || msg.Report.GitRange != "history disabled" {
		t.Fatalf("unexpected startup history report: %+v", msg.Report)
	}
}

// TestResolveHistoryCorrelationPath_PrefersJSONLOverDB is the bv #171 regression:
// when the smart data-source selector hands the History view the SQLite DB path
// (because beads.db is a few milliseconds newer than issues.jsonl after a normal
// `br sync`), the correlator must still follow the git-tracked JSONL — git history
// of the binary DB yields zero lifecycle events, so every correlation would be
// lost. resolveHistoryCorrelationPath redirects DB (or any non-JSONL) selections
// to the sibling JSONL while leaving JSONL selections untouched.
func TestResolveHistoryCorrelationPath_PrefersJSONLOverDB(t *testing.T) {
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	dbPath := filepath.Join(beadsDir, "beads.db")

	// Reproduce the exact trigger: DB mtime 41ms NEWER than the JSONL.
	base := time.Now().Add(-time.Hour)
	writeFileAt(t, jsonlPath, []byte(`{"id":"bv-1","title":"x","status":"open"}`+"\n"), base)
	writeFileAt(t, dbPath, []byte("SQLite format 3\x00"), base.Add(41*time.Millisecond))

	// Sanity check: confirm the freshest-mtime selector really does pick the DB
	// under this skew (the bug's trigger). If it didn't, the regression below
	// would pass vacuously.
	sources, err := datasource.DiscoverSources(datasource.DiscoveryOptions{
		BeadsDir: beadsDir,
		RepoPath: repo,
	})
	if err != nil {
		t.Fatalf("DiscoverSources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("expected at least one discovered source")
	}
	if sources[0].Path != dbPath {
		t.Fatalf("precondition not met: expected freshest-mtime selection to be the DB %q, got %q (sources=%+v)", dbPath, sources[0].Path, sources)
	}

	// The fix: even though the DB was selected, History correlation must follow
	// the JSONL.
	got := resolveHistoryCorrelationPath(dbPath, repo)
	if got != jsonlPath {
		t.Fatalf("expected correlation path %q (JSONL), got %q (DB-derived selection must redirect to JSONL)", jsonlPath, got)
	}
}

// TestResolveHistoryCorrelationPath_KeepsJSONLSelection verifies that when the
// selector already chose a JSONL (e.g. JSONL is the freshest source, or the
// `touch issues.jsonl` workaround was applied), the path is preserved unchanged.
func TestResolveHistoryCorrelationPath_KeepsJSONLSelection(t *testing.T) {
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	writeFileAt(t, jsonlPath, []byte(`{"id":"bv-1","title":"x","status":"open"}`+"\n"), time.Now())

	if got := resolveHistoryCorrelationPath(jsonlPath, repo); got != jsonlPath {
		t.Fatalf("JSONL selection must be preserved: want %q, got %q", jsonlPath, got)
	}

	// Case-insensitive extension match (e.g. .JSONL) is also preserved.
	upper := filepath.Join(beadsDir, "issues.JSONL")
	if got := resolveHistoryCorrelationPath(upper, repo); got != upper {
		t.Fatalf("uppercase .JSONL selection must be preserved: want %q, got %q", upper, got)
	}
}

// TestResolveHistoryCorrelationPath_FallsBackWhenNoJSONL verifies graceful
// degradation: when the selected source is a DB but no JSONL exists alongside it
// (or anywhere standard), the original path is returned so the correlator's own
// default-file resolution still runs rather than panicking or returning "".
func TestResolveHistoryCorrelationPath_FallsBackWhenNoJSONL(t *testing.T) {
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	dbPath := filepath.Join(beadsDir, "beads.db")
	writeFileAt(t, dbPath, []byte("SQLite format 3\x00"), time.Now())

	if got := resolveHistoryCorrelationPath(dbPath, repo); got != dbPath {
		t.Fatalf("with no JSONL present, original path must be preserved: want %q, got %q", dbPath, got)
	}
}

// TestResolveHistoryCorrelationPath_EmptyPath verifies that an empty selection
// (workspace mode) is passed straight through so the correlator discovers the
// standard beads files itself.
func TestResolveHistoryCorrelationPath_EmptyPath(t *testing.T) {
	if got := resolveHistoryCorrelationPath("", t.TempDir()); got != "" {
		t.Fatalf("empty path must be preserved, got %q", got)
	}
}

// TestLoadHistoryCmd_HonoursFeedbackStore proves the History view's data
// source applies stored correlation feedback exactly like --robot-history
// (C5): a rejected (commit, bead) pair is absent from the loaded report.
func TestLoadHistoryCmd_HonoursFeedbackStore(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
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
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	write := func(status string) {
		t.Helper()
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"bv-1","title":"One","status":"`+status+`","priority":1,"issue_type":"task"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write issues.jsonl: %v", err)
		}
	}
	git("init")
	write("open")
	git("add", ".beads/issues.jsonl")
	git("commit", "-m", "seed bv-1")
	write("in_progress")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	git("add", ".beads/issues.jsonl", "main.go")
	git("commit", "-m", "feat(bv-1): start work")
	workSHA := git("rev-parse", "HEAD")

	issues := []model.Issue{{ID: "bv-1", Title: "One", Status: model.StatusInProgress, Priority: 1, IssueType: model.TypeTask}}
	load := func() *correlation.HistoryReport {
		t.Helper()
		msg := LoadHistoryCmd(issues, jsonlPath, context.Background(), uint64(1), uint64(1))()
		loaded, ok := msg.(HistoryLoadedMsg)
		if !ok {
			t.Fatalf("LoadHistoryCmd returned %T, want HistoryLoadedMsg", msg)
		}
		if loaded.Error != nil {
			t.Fatalf("history load error: %v", loaded.Error)
		}
		return loaded.Report
	}

	before := load()
	var listed bool
	for _, c := range before.Histories["bv-1"].Commits {
		if c.SHA == workSHA {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("precondition: commit %s should correlate to bv-1 before feedback: %+v", workSHA[:7], before.Histories["bv-1"].Commits)
	}

	store := correlation.NewFeedbackStore(beadsDir)
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.Reject(workSHA, "bv-1", "tester", 0.9, "not really bv-1"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	after := load()
	for _, c := range after.Histories["bv-1"].Commits {
		if c.SHA == workSHA {
			t.Fatalf("History view still lists rejected commit %s for bv-1", workSHA[:7])
		}
	}
	if fa := after.Stats.FeedbackApplied; fa == nil || fa.Rejected != 1 {
		t.Fatalf("stats.feedback_applied=%+v; want rejected=1", fa)
	}
	if _, stillIndexed := after.CommitIndex[workSHA]; stillIndexed {
		t.Fatalf("commit_index still contains rejected commit %s: %v", workSHA[:7], after.CommitIndex[workSHA])
	}
}
