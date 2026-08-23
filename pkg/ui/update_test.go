package ui

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"
)

func makeReloadBDWorkspace(t *testing.T, initialExport string) (string, string) {
	t.Helper()
	root := t.TempDir()
	beadsDir := filepath.Join(root, ".beads")
	if err := os.MkdirAll(filepath.Join(beadsDir, "embeddeddolt"), 0o755); err != nil {
		t.Fatalf("create fake Dolt workspace: %v", err)
	}
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(initialExport), 0o644); err != nil {
		t.Fatalf("write initial compatibility export: %v", err)
	}
	return root, issuesPath
}

func installReloadFakeBD(t *testing.T, root, payload string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bd uses a POSIX shell script")
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create fake bd bin directory: %v", err)
	}
	payloadPath := filepath.Join(binDir, "payload.jsonl")
	if err := os.WriteFile(payloadPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write fake bd payload: %v", err)
	}
	script := "#!/bin/sh\nif [ \"$1\" != \"export\" ] || [ \"$2\" != \"-o\" ]; then exit 2; fi\ncat '" + payloadPath + "' > \"$3\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return payloadPath
}

func installFailingReloadFakeBD(t *testing.T, root, payload string) {
	t.Helper()
	payloadPath := installReloadFakeBD(t, root, payload)
	binDir := filepath.Dir(payloadPath)
	script := "#!/bin/sh\ncat '" + payloadPath + "' > \"$3\"\nsleep 0.1\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write failing fake bd: %v", err)
	}
}

func TestModelUpdateHistoryPartialAndFatalState(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "A", Title: "Alpha", Status: model.StatusOpen}}, nil, "")
	m.width, m.height = 120, 40

	updated, _ := m.Update(HistoryLoadedMsg{Error: errors.New("provider failed")})
	failed := updated.(Model)
	if !failed.historyLoadFailed || !failed.statusIsError || failed.historyLoading {
		t.Fatalf("fatal history load did not set failure state: failed=%v error=%v loading=%v", failed.historyLoadFailed, failed.statusIsError, failed.historyLoading)
	}

	report := &correlation.HistoryReport{
		Histories:   map[string]correlation.BeadHistory{},
		CommitIndex: correlation.CommitIndex{},
		Warnings: []correlation.HistoryWarning{{
			Code: correlation.HistoryWarningExternalRepositoryUnavailable, Context: "ctx:repo-a-111", Reason: "not_found", SkippedCorrelations: 1, Message: "Source history is unavailable.",
		}},
	}
	updated, _ = failed.Update(HistoryLoadedMsg{Report: report})
	partial := updated.(Model)
	if partial.historyLoadFailed || partial.statusIsError || partial.historyView.report != report {
		t.Fatalf("partial history report was not installed normally: failed=%v error=%v installed=%v", partial.historyLoadFailed, partial.statusIsError, partial.historyView.report == report)
	}
}

func TestModelUpdateHistoryRefreshReconcilesExistingView(t *testing.T) {
	report := createTestHistoryReport()
	m := NewModel([]model.Issue{{ID: "bv-1", Title: "Alpha", Status: model.StatusOpen}}, nil, "")
	m.width, m.height = 120, 40
	m.historyReport = report
	m.historyView.SetReport(report)
	m.historyView.StartSearchWithMode(searchModeCommit)
	m.historyView.searchInput.SetValue("auth")
	m.historyView.applySearchFilter()
	m.historyView.FinishSearch()
	m.historyView.ToggleViewMode()

	refreshed := createTestHistoryReport()
	updated, _ := m.Update(HistoryLoadedMsg{Report: refreshed})
	m = updated.(Model)

	if m.historyView.SearchQuery() != "auth" || m.historyView.searchMode != searchModeCommit || m.historyView.IsSearchActive() || !m.historyView.IsGitMode() {
		t.Fatalf("HistoryLoadedMsg replaced stable view state: query=%q mode=%v active=%v git=%v", m.historyView.SearchQuery(), m.historyView.searchMode, m.historyView.IsSearchActive(), m.historyView.IsGitMode())
	}
	if m.historyView.report != refreshed {
		t.Fatal("HistoryLoadedMsg did not install refreshed report")
	}
}

// exercise Phase2Ready and FileChanged branches of Update for coverage.
func TestModelUpdatePhase2AndFileChanged(t *testing.T) {
	issues := []model.Issue{{ID: "A", Title: "Alpha", Status: model.StatusOpen}}
	m := NewModel(issues, nil, "")
	m.width, m.height = 120, 40

	// Phase2ReadyMsg should rebuild insights/graph without error
	ins := m.analysis.GenerateInsights(len(issues))
	updated, _ := m.Update(Phase2ReadyMsg{Stats: m.analysis, Insights: ins})
	m2 := updated.(Model)
	if m2.insightsPanel.insights.Stats == nil {
		t.Fatalf("expected insights to be regenerated")
	}
	if len(m2.priorityHints) == 0 {
		t.Fatalf("expected priority hints populated after Phase2Ready")
	}

	// FileChangedMsg with empty beadsPath should simply re-arm watcher (no panic)
	if updated2, cmd := m2.Update(FileChangedMsg{}); updated2.(Model).statusMsg != m2.statusMsg {
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

func TestInsightsToggleCalculationRefreshesCachedDetail(t *testing.T) {
	stats := analysis.NewGraphStatsForTest(nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, nil)
	ins := analysis.Insights{
		Bottlenecks: []analysis.InsightItem{{ID: "B"}},
		Stats:       stats,
	}
	m := NewInsightsModel(ins, map[string]*model.Issue{
		"B": {ID: "B", Title: "Bottleneck", Status: model.StatusOpen},
	}, DefaultTheme(nil))
	m.SetSize(180, 50)
	m.updateDetailContent()
	withCalculation := m.detailContent
	if withCalculation == "" {
		t.Fatal("expected initial cached detail")
	}

	m.ToggleCalculation()
	withoutCalculation := m.detailContent
	if withoutCalculation == withCalculation {
		t.Fatal("expected calculation toggle to refresh cached detail")
	}

	m.ToggleCalculation()
	if m.detailContent != withCalculation {
		t.Fatal("expected enabling calculation proof to restore cached detail")
	}
}

func TestInsightsItemNavigationWhileHeatmapEnabled(t *testing.T) {
	ins := analysis.Insights{
		Cores: []analysis.InsightItem{{ID: "C1"}, {ID: "C2"}},
	}
	m := NewModel(nil, nil, "")
	m.focused = focusInsights
	m.insightsPanel = NewInsightsModel(ins, map[string]*model.Issue{
		"C1": {ID: "C1", Title: "Core one"},
		"C2": {ID: "C2", Title: "Core two"},
	}, DefaultTheme(nil))
	m.insightsPanel.focusedPanel = PanelCores
	m.insightsPanel.showHeatmap = true

	m = m.handleInsightsKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if selected := m.insightsPanel.SelectedIssueID(); selected != "C2" {
		t.Fatalf("selected issue = %q, want C2", selected)
	}

	m = m.handleInsightsKeys(tea.KeyMsg{Type: tea.KeyUp})
	if selected := m.insightsPanel.SelectedIssueID(); selected != "C1" {
		t.Fatalf("selected issue = %q, want C1", selected)
	}
}

func TestInsightsHeatmapDrillRefreshesWithScoreAndCellChanges(t *testing.T) {
	stats := analysis.NewGraphStatsForTest(
		nil, nil, nil, nil, nil,
		map[string]float64{"A": 0, "B": 5},
		nil, nil, nil, 0, nil,
	)
	m := NewInsightsModel(analysis.Insights{Stats: stats}, map[string]*model.Issue{
		"A": {ID: "A", Title: "A", Status: model.StatusOpen},
		"B": {ID: "B", Title: "B", Status: model.StatusOpen},
	}, DefaultTheme(nil))
	m.SetActiveIssueIDs(map[string]bool{"A": true, "B": true})
	m.SetTopPicks([]analysis.TopPick{{ID: "A", Score: 0.6}, {ID: "B", Score: 0.8}})
	m.ToggleHeatmap()
	m.heatmapCol = 3
	m.HeatmapEnter()
	if got := m.HeatmapSelectedIssueID(); got != "A" {
		t.Fatalf("initial drill issue = %q, want A", got)
	}

	// Moving A to another score bucket invalidates the selected cell and exits
	// drill-down rather than retaining the old cell's issue list.
	m.SetTopPicks([]analysis.TopPick{{ID: "A", Score: 0.9}, {ID: "B", Score: 0.8}})
	if m.IsHeatmapDrillDown() || m.HeatmapSelectedIssueID() != "" {
		t.Fatalf("stale drill-down survived score change: drill=%v issue=%q", m.IsHeatmapDrillDown(), m.HeatmapSelectedIssueID())
	}
	m.HeatmapMoveRight()
	m.HeatmapEnter()
	if got := m.HeatmapSelectedIssueID(); got != "A" {
		t.Fatalf("moved-cell drill issue = %q, want A", got)
	}

	// A same-bucket score update repopulates the active drill list and keeps its
	// index valid.
	m.SetTopPicks([]analysis.TopPick{{ID: "A", Score: 0.95}, {ID: "B", Score: 0.8}})
	if !m.IsHeatmapDrillDown() || m.HeatmapSelectedIssueID() != "A" {
		t.Fatalf("same-cell drill list was not refreshed: drill=%v issue=%q", m.IsHeatmapDrillDown(), m.HeatmapSelectedIssueID())
	}
}

func TestUpdateFileChangedReloadsSelection(t *testing.T) {
	data := `{"id":"ONE","title":"One","status":"open"}`
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
	m2 := updated.(Model)
	if m2.statusIsError {
		t.Fatalf("expected successful reload, got error %q", m2.statusMsg)
	}
}

func TestUpdateForcedRefreshRegeneratesBDExport(t *testing.T) {
	stale := `{"id":"STALE","title":"Stale export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	fresh := `{"id":"FRESH","title":"Fresh export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, stale)
	installReloadFakeBD(t, root, fresh)

	m := NewModel([]model.Issue{{ID: "STALE", Title: "Stale export", Status: model.StatusOpen}}, nil, issuesPath)
	if m.watcher != nil {
		defer m.watcher.Stop()
	}
	updated, _ := m.Update(FileChangedMsg{refreshBDExport: true})
	m2 := updated.(Model)
	if m2.statusIsError {
		t.Fatalf("forced reload failed: %s", m2.statusMsg)
	}
	if len(m2.issues) != 1 || m2.issues[0].ID != "FRESH" {
		t.Fatalf("forced reload used stale export: %#v", m2.issues)
	}
}

func TestUpdateForcedRefreshReportsBDExportFailure(t *testing.T) {
	stale := `{"id":"STALE","title":"Stale export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, stale)
	installFailingReloadFakeBD(t, root, "partial export\n")

	m := NewModel([]model.Issue{{ID: "STALE", Title: "Stale export", Status: model.StatusOpen}}, nil, issuesPath)
	if m.watcher == nil {
		t.Fatal("expected file watcher")
	}
	defer m.watcher.Stop()
	if !m.watcher.IsStarted() {
		if err := m.watcher.Start(); err != nil {
			t.Fatalf("start watcher: %v", err)
		}
	}
	queuedAt := time.Now()
	updated, _ := m.Update(FileChangedMsg{refreshBDExport: true})
	m2 := updated.(Model)
	if !m2.statusIsError || !strings.Contains(m2.statusMsg, "bd export failed") {
		t.Fatalf("expected explicit bd export error, got %q", m2.statusMsg)
	}
	if len(m2.issues) != 1 || m2.issues[0].ID != "STALE" {
		t.Fatalf("failed export should retain current issues: %#v", m2.issues)
	}
	select {
	case <-m2.watcher.Changed():
		t.Fatal("failed export leaked a compatibility-file watcher event")
	case <-time.After(300 * time.Millisecond):
	}
	updated, _ = m2.Update(FileChangedMsg{observedAt: queuedAt})
	m3 := updated.(Model)
	if !m3.statusIsError || len(m3.issues) != 1 || m3.issues[0].ID != "STALE" {
		t.Fatalf("queued export event replaced the reload error: status=%q issues=%#v", m3.statusMsg, m3.issues)
	}
}

func TestForceRefreshKeysRequestBDExport(t *testing.T) {
	for name, keyType := range map[string]tea.KeyType{"ctrl+r": tea.KeyCtrlR, "f5": tea.KeyF5} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			issuesPath := filepath.Join(root, "issues.jsonl")
			if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open"}`+"\n"), 0o644); err != nil {
				t.Fatalf("write issues: %v", err)
			}
			m := NewModel(nil, nil, issuesPath)
			if m.watcher != nil {
				defer m.watcher.Stop()
			}

			_, cmd := m.Update(tea.KeyMsg{Type: keyType})
			if cmd == nil {
				t.Fatal("force refresh key returned no command")
			}
			cmdMsg := cmd()
			if msg, ok := cmdMsg.(FileChangedMsg); ok {
				if !msg.refreshBDExport {
					t.Fatal("force refresh did not request bd export")
				}
				return
			}
			batch, ok := cmdMsg.(tea.BatchMsg)
			if !ok {
				t.Fatalf("force refresh command returned unexpected %T", cmdMsg)
			}
			for _, batchCmd := range batch {
				if msg, ok := batchCmd().(FileChangedMsg); ok {
					if !msg.refreshBDExport {
						t.Fatal("force refresh did not request bd export")
					}
					return
				}
			}
			t.Fatal("force refresh batch contained no FileChangedMsg")
		})
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
	m2 := updated.(Model)
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
