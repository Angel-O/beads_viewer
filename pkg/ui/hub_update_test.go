package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCommentsAddRefreshesHubSnapshotAndShowsCount(t *testing.T) {
	initial := `{"id":"A","title":"Alpha","status":"open","issue_type":"task"}` + "\n"
	updated := `{"id":"A","title":"Alpha","status":"open","issue_type":"task","comments":[{"id":"c1","issue_id":"A","author":"tester","text":"looks good","created_at":"2026-08-26T00:00:00Z"}]}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, initial)
	payloadPath := installReloadFakeBD(t, root, initial)
	configPath := filepath.Join(root, "hub.yaml")
	if err := os.WriteFile(configPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewModel([]model.Issue{{ID: "A", Title: "Alpha", Status: model.StatusOpen, IssueType: model.TypeTask}}, nil, "")
	m.width, m.height = 120, 40
	m.beadsPath = issuesPath
	m.runtimeServices.CatalogPath = configPath
	m.hubRepositoryMode = true
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     issuesPath,
		CatalogPath:   configPath,
		DebounceDelay: time.Millisecond,
		IdleGC:        &IdleGCConfig{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.backgroundWorker = worker
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()

	m.SetCommentRunner(func(string, string) error {
		if err := os.WriteFile(payloadPath, []byte(updated), 0o644); err != nil {
			return err
		}
		return nil
	})
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updatedModel.(*Model)
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("looks good")})
	m = updatedModel.(*Model)
	updatedModel, submitCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updatedModel.(*Model)
	if submitCmd == nil {
		t.Fatal("comment submission returned no command")
	}
	updatedModel, refreshCmd := m.Update(submitCmd())
	m = updatedModel.(*Model)
	if refreshCmd == nil {
		t.Fatal("successful Hub comment returned no refresh command")
	}

	var ready SnapshotReadyMsg
	if message := refreshCmd(); message != nil {
		ready, _ = message.(SnapshotReadyMsg)
	}
	if ready.Snapshot == nil {
		ready = waitForSnapshotReady(t, worker.Messages())
	}
	updatedModel, _ = m.Update(ready)
	m = updatedModel.(*Model)
	issue := m.issueMap["A"]
	if issue == nil || len(issue.Comments) != 1 || issue.Comments[0].Text != "looks good" {
		t.Fatalf("refreshed comments = %#v, want one persisted comment", issue)
	}
	if !strings.Contains(m.list.View(), "💬1") {
		t.Fatalf("list did not show refreshed comment count: %q", m.list.View())
	}
}
