package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/version"
	tea "github.com/charmbracelet/bubbletea"
)

// Cover additional branches in Model.Update for quit/help/tab handling and update notices.
func TestUpdateHelpQuitAndTabFocus(t *testing.T) {
	issues := []model.Issue{
		{ID: "1", Title: "One", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Make model ready and split view
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(*Model)

	// Help toggle via ? then dismiss with another key
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(*Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("expected help overlay shown")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(*Model)
	if m.showHelp || m.focused != focusList {
		t.Fatalf("expected help overlay dismissed")
	}

	// Tab should flip focus in split view
	if m.focused != focusList {
		t.Fatalf("expected list focus before tab")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(*Model)
	if m.focused != focusDetail {
		t.Fatalf("expected detail focus after tab")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(*Model)
	if m.focused != focusList {
		t.Fatalf("expected list focus after second tab")
	}

	// Escape should show quit confirm, 'y' should issue tea.Quit
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if !m.showQuitConfirm {
		t.Fatalf("expected quit confirm after esc")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatalf("expected quit command on confirm quit")
	}
}

func TestHelpRestoresExactUnderlyingFocus(t *testing.T) {
	tests := []struct {
		name  string
		focus focus
		setup func(*Model)
	}{
		{name: "list", focus: focusList},
		{name: "tree", focus: focusTree},
		{name: "split detail", focus: focusDetail, setup: func(m *Model) { m.isSplitView = true }},
		{name: "tutorial", focus: focusTutorial, setup: func(m *Model) { m.showTutorial = true }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
			m.focused = tt.focus
			if tt.setup != nil {
				tt.setup(m)
			}

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
			m = updated.(*Model)
			if m.focused != focusHelp {
				t.Fatalf("focus after opening help=%v, want help", m.focused)
			}
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = updated.(*Model)
			if m.focused != tt.focus {
				t.Fatalf("focus after closing help=%v, want %v", m.focused, tt.focus)
			}
		})
	}
}

func TestUpdateMsgSetsUpdateAvailable(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	updated, _ := m.Update(UpdateMsg{TagName: "v9.9.9", URL: "https://example"})
	m = updated.(*Model)
	if !m.updateAvailable || m.updateTag != "v9.9.9" {
		t.Fatalf("update flag not set")
	}
}

func TestUpdateMsgIgnoresCurrentVersion(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	updated, _ := m.Update(UpdateMsg{TagName: version.Version, URL: "https://example"})
	m = updated.(*Model)

	if m.updateAvailable || m.updateTag != "" || m.updateURL != "" {
		t.Fatalf("current-version update message should be ignored: available=%v tag=%q url=%q",
			m.updateAvailable, m.updateTag, m.updateURL)
	}
}

func TestUpdateMsgClearsStaleEqualVersionNotice(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	m.updateAvailable = true
	m.updateTag = "v9.9.9"
	m.updateURL = "https://example/old"

	updated, _ := m.Update(UpdateMsg{TagName: version.Version, URL: "https://example/current"})
	m = updated.(*Model)

	if m.updateAvailable || m.updateTag != "" || m.updateURL != "" {
		t.Fatalf("equal-version update message should clear stale notice: available=%v tag=%q url=%q",
			m.updateAvailable, m.updateTag, m.updateURL)
	}
}

func TestUpdateCompleteClearsNoticeOnlyAfterSuccess(t *testing.T) {
	tests := []struct {
		name      string
		success   bool
		version   string
		wantClear bool
	}{
		{name: "successful install", success: true, version: "v9.9.9", wantClear: true},
		{name: "failed install", success: false, version: "v9.9.9", wantClear: false},
		{name: "stale successful install", success: true, version: "v9.9.8", wantClear: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
			m.updateAvailable = true
			m.updateTag = "v9.9.9"
			m.updateURL = "https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v9.9.9"

			updated, _ := m.Update(UpdateCompleteMsg{Success: tt.success, NewVersion: tt.version})
			m = updated.(*Model)

			if tt.wantClear {
				if m.updateAvailable || m.updateTag != "" || m.updateURL != "" {
					t.Fatalf("successful update left a repeatable notice: available=%v tag=%q url=%q",
						m.updateAvailable, m.updateTag, m.updateURL)
				}
				return
			}
			if !m.updateAvailable || m.updateTag != "v9.9.9" || m.updateURL == "" {
				t.Fatalf("failed update cleared the retry notice: available=%v tag=%q url=%q",
					m.updateAvailable, m.updateTag, m.updateURL)
			}
		})
	}
}

func TestUpdateStateRefreshesVisibleDetailNotice(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	m.isSplitView = true
	m.viewport.Width = 120
	m.viewport.Height = 40
	m.updateViewportContent()

	updated, _ := m.Update(UpdateMsg{
		TagName: "v9.9.9",
		URL:     "https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v9.9.9",
	})
	m = updated.(*Model)
	if view := m.viewport.View(); !strings.Contains(view, "v9.9.9") {
		t.Fatalf("visible detail pane did not refresh after update discovery: %q", view)
	}

	updated, _ = m.Update(UpdateCompleteMsg{Success: true, NewVersion: "v9.9.9"})
	m = updated.(*Model)
	if view := m.viewport.View(); strings.Contains(view, "v9.9.9") {
		t.Fatalf("visible detail pane retained completed update notice: %q", view)
	}
}

func TestUpdateModalTickIsForwardedByModel(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	m.showUpdateModal = true
	m.updateModal = NewUpdateModal("v9.9.9", "", m.theme)
	m.updateModal.state = UpdateStateDownloading

	updated, cmd := m.Update(updateTickMsg(time.Now()))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("parent model dropped the update modal's repaint tick")
	}
	if m.updateModal.state != UpdateStateDownloading {
		t.Fatalf("tick changed update state to %v", m.updateModal.state)
	}
}

func TestHistoryViewToggle(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Test Issue", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Make model ready
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(*Model)

	// h should toggle history view on
	if m.isHistoryView {
		t.Fatalf("history view should be off initially")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(*Model)

	if !m.isHistoryView {
		t.Fatalf("expected history view to be on after h key")
	}
	if m.focused != focusHistory {
		t.Fatalf("expected focus to be on history, got %v", m.focused)
	}

	// h again should toggle off
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(*Model)

	if m.isHistoryView {
		t.Fatalf("expected history view to be off after second h key")
	}
	if m.focused != focusList {
		t.Fatalf("expected focus to be back on list, got %v", m.focused)
	}
}

func TestHistoryViewKeys(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Test Issue", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Make model ready
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(*Model)

	// Enter history view
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(*Model)

	// Esc should close history view
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)

	if m.isHistoryView {
		t.Fatalf("expected history view to be closed after Esc")
	}

	// Re-enter and test 'c' key cycles confidence
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(*Model)

	initialConf := m.historyView.GetMinConfidence()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(*Model)

	if m.historyView.GetMinConfidence() == initialConf {
		t.Fatalf("expected confidence to change after 'c' key")
	}
}

func TestForceRefreshUsesExistingBackgroundWorkerWaiter(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	m.backgroundWorker = worker

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("force refresh started an additional worker-channel waiter")
	}
	if m.statusMsg != "Refreshing…" || m.statusIsError {
		t.Fatalf("refresh status=%q error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestForceRefreshDetachesStoppedBackgroundWorker(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	worker.Stop()

	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	m.backgroundWorker = worker

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("unavailable stopped-worker refresh unexpectedly scheduled work")
	}
	if m.backgroundWorker != nil {
		t.Fatal("stopped background worker remained installed")
	}
	if m.statusMsg != "Refresh unavailable" || !m.statusIsError {
		t.Fatalf("stopped-worker refresh status=%q error=%v", m.statusMsg, m.statusIsError)
	}
}
