package ui

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/bubbles/list"
	_ "modernc.org/sqlite"
)

// exercise Phase2Ready and FileChanged branches of Update for coverage.
func TestModelUpdatePhase2AndFileChanged(t *testing.T) {
	issues := []model.Issue{{ID: "A", Title: "Alpha", Status: model.StatusOpen}}
	m := NewModel(issues, nil, "")
	m.width, m.height = 120, 40

	// Phase2ReadyMsg should rebuild insights/graph without error
	ins := m.analysis.GenerateInsights(len(issues))
	updated, _ := m.Update(Phase2ReadyMsg{Stats: m.analysis, Insights: ins})
	m2 := updated.(*Model)
	if m2.insightsPanel.insights.Stats == nil {
		t.Fatalf("expected insights to be regenerated")
	}
	if len(m2.priorityHints) == 0 {
		t.Fatalf("expected priority hints populated after Phase2Ready")
	}

	// FileChangedMsg with empty beadsPath should simply re-arm watcher (no panic)
	if updated2, cmd := m2.Update(FileChangedMsg{}); updated2.(*Model).statusMsg != m2.statusMsg {
		_ = cmd // command may be nil; just ensure no panic and type matches
	}
}

type badItem struct{}

func (badItem) Title() string       { return "bad" }
func (badItem) Description() string { return "bad" }
func (badItem) FilterValue() string { return "bad" }

func TestCopyIssueToClipboardInvalidItem(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.list.SetItems([]list.Item{badItem{}})
	m.list.Select(0)
	m.copyIssueToClipboard()
	if !m.statusIsError || m.statusMsg == "" {
		t.Fatalf("expected error copying invalid item, got %q", m.statusMsg)
	}
}

func TestEnterTimeTravelModeGracefulFailure(t *testing.T) {
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	_ = os.Chdir(tmp)

	m := NewModel(nil, nil, "")
	m.enterTimeTravelMode("HEAD")
	if !m.statusIsError {
		t.Fatalf("expected error when not in git repo")
	}
}

func TestInsightsCurrentPanelItemCount(t *testing.T) {
	ins := analysis.Insights{
		Bottlenecks:  []analysis.InsightItem{{ID: "B"}},
		Keystones:    []analysis.InsightItem{{ID: "K"}},
		Influencers:  []analysis.InsightItem{{ID: "I"}},
		Hubs:         []analysis.InsightItem{{ID: "H"}},
		Authorities:  []analysis.InsightItem{{ID: "A"}},
		Cores:        []analysis.InsightItem{{ID: "C"}},
		Articulation: []string{"ART"},
		Slack:        []analysis.InsightItem{{ID: "S"}},
		Cycles:       [][]string{{"X", "Y"}},
		Stats:        analysis.NewGraphStatsForTest(nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, nil),
	}
	m := NewInsightsModel(ins, map[string]*model.Issue{}, DefaultTheme(nil))
	m.SetTopPicks([]analysis.TopPick{{ID: "P1", Score: 1.0}})
	counts := []int{m.currentPanelItemCount()}
	for i := 0; i < int(PanelCount)-1; i++ {
		m.NextPanel()
		counts = append(counts, m.currentPanelItemCount())
	}
	for idx, c := range counts {
		if c == 0 {
			t.Fatalf("panel %d reported zero items unexpectedly", idx)
		}
	}
}

func TestUpdateFileChangedReloadsSelection(t *testing.T) {
	t.Setenv("BV_BACKGROUND_MODE", "0")
	data := `{"id":"ONE","title":"One","status":"open","issue_type":"task"}`
	tmp := t.TempDir()
	beads := filepath.Join(tmp, "beads.jsonl")
	if err := os.WriteFile(beads, []byte(data), 0644); err != nil {
		t.Fatalf("write beads: %v", err)
	}
	m := NewModel(nil, nil, beads)
	m.list.SetItems([]list.Item{IssueItem{Issue: model.Issue{ID: "ONE", Title: "One", Status: model.StatusOpen}}})
	m.list.Select(0)

	updated, cmd := m.Update(FileChangedMsg{})
	_ = cmd
	m2 := updated.(*Model)
	if m2.statusIsError {
		t.Fatalf("expected successful reload, got error %q", m2.statusMsg)
	}
	if !m2.historyLoading || m2.historyLoadRequestGeneration == 0 ||
		m2.historyLoadDataGeneration != m2.semanticDataGeneration {
		t.Fatalf(
			"sync reload did not own a current history refresh: loading=%v data=%d current=%d request=%d",
			m2.historyLoading,
			m2.historyLoadDataGeneration,
			m2.semanticDataGeneration,
			m2.historyLoadRequestGeneration,
		)
	}
}

func historyReportWithIssue(id, title string) *correlation.HistoryReport {
	return &correlation.HistoryReport{
		Histories: map[string]correlation.BeadHistory{
			id: {
				BeadID: id,
				Title:  title,
				Commits: []correlation.CorrelatedCommit{
					{SHA: "abc123", ShortSHA: "abc123", Message: title},
				},
			},
		},
	}
}

func TestHistoryLoadRejectsStaleCompletionAfterSnapshotSwap(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "OLD", Title: "Old", Status: model.StatusOpen}}, nil, "")
	if cmd := m.startHistoryLoad(); cmd == nil {
		t.Fatal("initial history request was not scheduled")
	}
	oldDataGeneration := m.historyLoadDataGeneration
	oldRequestGeneration := m.historyLoadRequestGeneration

	snapshot := NewSnapshotBuilder([]model.Issue{{ID: "NEW", Title: "New", Status: model.StatusOpen}}).Build()
	updated, _ := m.Update(SnapshotReadyMsg{Snapshot: snapshot, SnapshotVer: 1})
	m = updated.(*Model)
	if !m.historyLoading {
		t.Fatal("snapshot swap did not start a current history request")
	}
	if m.historyLoadRequestGeneration == oldRequestGeneration {
		t.Fatal("snapshot swap reused the previous history request generation")
	}

	staleReport := historyReportWithIssue("OLD", "Old")
	updated, _ = m.Update(HistoryLoadedMsg{
		DataGeneration:    oldDataGeneration,
		RequestGeneration: oldRequestGeneration,
		Report:            staleReport,
	})
	m = updated.(*Model)
	if m.historyView.report == staleReport {
		t.Fatal("stale history completion replaced the current history view")
	}
	if !m.historyLoading {
		t.Fatal("stale history completion cleared current request ownership")
	}

	currentReport := historyReportWithIssue("NEW", "New")
	updated, _ = m.Update(HistoryLoadedMsg{
		DataGeneration:    m.historyLoadDataGeneration,
		RequestGeneration: m.historyLoadRequestGeneration,
		Report:            currentReport,
	})
	m = updated.(*Model)
	if m.historyLoading {
		t.Fatal("accepted history completion left the request active")
	}
	if m.historyView.report != currentReport {
		t.Fatal("current history completion was not installed")
	}
}

func TestHistoryRefreshPreservesActiveSearchSession(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "NEW", Title: "Beta work", Status: model.StatusOpen}}, nil, "")
	m.historyView = NewHistoryModel(historyReportWithIssue("OLD", "Alpha work"), m.theme)
	m.isHistoryView = true
	m.focused = focusHistory
	focusCmd := m.historyView.StartSearch()
	m.historyView.searchInput.SetValue("beta")
	m.historyView.lastSearchQuery = "beta"
	m.historyView.applySearchFilter()
	_ = m.beginEmbeddedTextInputSession(embeddedTextInputHistorySearch, focusCmd)
	session := m.embeddedTextInputSession

	if cmd := m.startHistoryLoad(); cmd == nil {
		t.Fatal("history refresh was not scheduled")
	}
	refreshed := historyReportWithIssue("NEW", "Beta work")
	updated, _ := m.Update(HistoryLoadedMsg{
		DataGeneration:    m.historyLoadDataGeneration,
		RequestGeneration: m.historyLoadRequestGeneration,
		Report:            refreshed,
	})
	m = updated.(*Model)

	if got := m.historyView.SearchQuery(); got != "beta" {
		t.Fatalf("search query = %q, want beta", got)
	}
	if !m.historyView.IsSearchActive() || !m.historyView.searchInput.Focused() {
		t.Fatal("history refresh dropped active search focus")
	}
	if !m.embeddedTextInputSessionIsActive(session) {
		t.Fatal("history refresh invalidated the active embedded-input session")
	}
	if len(m.historyView.beadIDs) != 1 || m.historyView.beadIDs[0] != "NEW" {
		t.Fatalf("refreshed search results = %#v, want [NEW]", m.historyView.beadIDs)
	}
}

func TestUpdateFileChangedReloadsSQLiteSource(t *testing.T) {
	t.Setenv("BV_BACKGROUND_MODE", "0")

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "beads.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL
		);
		INSERT INTO issues (id, title, status) VALUES ('SQLITE-1', 'SQLite issue', 'open');
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed sqlite: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	m := NewModel(nil, nil, dbPath)
	if m.watcher != nil {
		defer m.watcher.Stop()
	}

	updated, _ := m.Update(FileChangedMsg{})
	m2 := updated.(*Model)
	if m2.statusIsError {
		t.Fatalf("expected successful sqlite reload, got error %q", m2.statusMsg)
	}
	if len(m2.issues) != 1 || m2.issues[0].ID != "SQLITE-1" {
		t.Fatalf("unexpected sqlite reload issues: %#v", m2.issues)
	}
}

func TestLoadIssuesForReloadSQLiteHonorsIssueFilter(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "beads.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL
		);
		INSERT INTO issues (id, title, status) VALUES
			('OPEN-1', 'Open issue', 'open'),
			('CLOSED-1', 'Closed issue', 'closed');
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed sqlite: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	loaded, err := loadIssuesForReload(dbPath, loader.ParseOptions{
		IssueFilter: func(issue *model.Issue) bool {
			return issue.Status != model.StatusClosed
		},
	})
	if err != nil {
		t.Fatalf("load sqlite reload issues: %v", err)
	}
	if len(loaded.Issues) != 1 || loaded.Issues[0].ID != "OPEN-1" {
		t.Fatalf("unexpected filtered sqlite issues: %#v", loaded.Issues)
	}
}

func TestBackgroundWorkerCountsSQLiteRowsInsteadOfBinaryNewlines(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "beads.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL
		);
		CREATE TABLE binary_noise (payload BLOB);
		INSERT INTO issues (id, title, status) VALUES
			('OPEN-1', 'Open issue', 'open'),
			('CLOSED-1', 'Closed issue', 'closed');
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed sqlite: %v", err)
	}
	if _, err := db.Exec("INSERT INTO binary_noise(payload) VALUES (?)", strings.Repeat("\n", 20_050)); err != nil {
		_ = db.Close()
		t.Fatalf("seed sqlite newline noise: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	if binaryLines, err := countJSONLLines(dbPath); err != nil || binaryLines < 20_000 {
		t.Fatalf("fixture does not expose binary-newline miscount: lines=%d err=%v", binaryLines, err)
	}
	if issueCount, err := countIssuesForReload(dbPath); err != nil || issueCount != 2 {
		t.Fatalf("SQLite issue count=%d err=%v, want 2", issueCount, err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: dbPath})
	if err != nil {
		t.Fatalf("NewBackgroundWorker: %v", err)
	}
	defer worker.Stop()
	snapshot := worker.buildSnapshot(false)
	if snapshot == nil {
		t.Fatal("SQLite background snapshot is nil")
	}
	defer snapshot.releasePooledIssues()
	if snapshot.DatasetTier != datasetTierSmall || snapshot.SourceIssueCountHint != 2 {
		t.Fatalf("SQLite snapshot tier/count=%v/%d, want small/2", snapshot.DatasetTier, snapshot.SourceIssueCountHint)
	}
	if snapshot.LoadedOpenOnly || snapshot.TruncatedCount != 0 {
		t.Fatalf("SQLite snapshot was spuriously truncated: openOnly=%v truncated=%d", snapshot.LoadedOpenOnly, snapshot.TruncatedCount)
	}
	if len(snapshot.Issues) != 2 {
		t.Fatalf("SQLite snapshot loaded %d issues, want both open and closed rows", len(snapshot.Issues))
	}
}

func TestNewModel_SetsTreeBeadsDirFromBeadsPath(t *testing.T) {
	tmp := t.TempDir()
	beads := filepath.Join(tmp, "beads.jsonl")
	if err := os.WriteFile(beads, []byte(`{"id":"ONE","title":"One","status":"open"}`+"\n"), 0644); err != nil {
		t.Fatalf("write beads: %v", err)
	}

	m := NewModel(nil, nil, beads)
	if m.watcher != nil {
		m.watcher.Stop()
	}

	if got, want := m.tree.beadsDir, filepath.Dir(beads); got != want {
		t.Fatalf("expected tree beadsDir %q, got %q", want, got)
	}
}
