package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/version"
	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdateInsightsHeatmapKey(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)
	if m.focused != focusInsights {
		t.Fatalf("expected Insights focus, got %v", m.focused)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	if !m.insightsPanel.showHeatmap {
		t.Fatal("expected m to enable the Insights heatmap")
	}
	if m.insightsPanel.focusedPanel != PanelPriority {
		t.Fatalf("expected heatmap panel focus, got %v", m.insightsPanel.focusedPanel)
	}
	if !strings.Contains(m.View(), "Priority Heatmap") {
		t.Fatal("expected rendered Insights view to contain the priority heatmap")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	if m.insightsPanel.showHeatmap {
		t.Fatal("expected second m to disable the Insights heatmap")
	}
}

func TestUpdateInsightsHeatmapKeyFromHelp(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if !m.showHelp || m.focusBeforeHelp != focusInsights {
		t.Fatal("expected help overlay opened from Insights")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	if m.showHelp || m.focused != focusInsights {
		t.Fatal("expected m to close help and restore Insights focus")
	}
	if !m.insightsPanel.showHeatmap {
		t.Fatal("expected m in Insights help to enable the heatmap")
	}
}

func TestUpdateInsightsCalculationKeyReportsNarrowLayout(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)
	if m.insightsPanel.showCalculation {
		t.Fatal("expected x to hide calculation proof")
	}
	if !strings.Contains(m.statusMsg, "hidden") {
		t.Fatalf("expected hidden status, got %q", m.statusMsg)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)
	if !m.insightsPanel.showCalculation {
		t.Fatal("expected x from Insights help to enable calculation proof")
	}
	if !strings.Contains(m.statusMsg, "widen terminal") {
		t.Fatalf("expected narrow-layout guidance, got %q", m.statusMsg)
	}
}

// Cover additional branches in Model.Update for quit/help/tab handling and update notices.
func TestUpdateHelpQuitAndTabFocus(t *testing.T) {
	issues := []model.Issue{
		{ID: "1", Title: "One", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Make model ready and split view
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)

	// Help toggle via ? then dismiss with another key
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("expected help overlay shown")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)
	if m.showHelp || m.focused != focusList {
		t.Fatalf("expected help overlay dismissed")
	}

	// Tab should flip focus in split view
	if m.focused != focusList {
		t.Fatalf("expected list focus before tab")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focused != focusDetail {
		t.Fatalf("expected detail focus after tab")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focused != focusList {
		t.Fatalf("expected list focus after second tab")
	}

	// Escape should show quit confirm, 'y' should issue tea.Quit
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if !m.showQuitConfirm {
		t.Fatalf("expected quit confirm after esc")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatalf("expected quit command on confirm quit")
	}
}

func TestUpdateMsgSetsUpdateAvailable(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	updated, _ := m.Update(UpdateMsg{TagName: "v9.9.9", URL: "https://example"})
	m = updated.(Model)
	if !m.updateAvailable || m.updateTag != "v9.9.9" {
		t.Fatalf("update flag not set")
	}
}

func TestUpdateMsgIgnoresCurrentVersion(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, nil, "")
	updated, _ := m.Update(UpdateMsg{TagName: version.Version, URL: "https://example"})
	m = updated.(Model)

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
	m = updated.(Model)

	if m.updateAvailable || m.updateTag != "" || m.updateURL != "" {
		t.Fatalf("equal-version update message should clear stale notice: available=%v tag=%q url=%q",
			m.updateAvailable, m.updateTag, m.updateURL)
	}
}

func TestHistoryViewToggle(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Test Issue", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Make model ready
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)

	// h should toggle history view on
	if m.isHistoryView {
		t.Fatalf("history view should be off initially")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(Model)

	if !m.isHistoryView {
		t.Fatalf("expected history view to be on after h key")
	}
	if m.focused != focusHistory {
		t.Fatalf("expected focus to be on history, got %v", m.focused)
	}

	// h again should toggle off
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(Model)

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
	m = updated.(Model)

	// Enter history view
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(Model)

	// Esc should close history view
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.isHistoryView {
		t.Fatalf("expected history view to be closed after Esc")
	}

	// Re-enter and test 'c' key cycles confidence
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(Model)

	initialConf := m.historyView.GetMinConfidence()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)

	if m.historyView.GetMinConfidence() == initialConf {
		t.Fatalf("expected confidence to change after 'c' key")
	}
}

func TestLabelDashboardFromSplitViewRendersAndReturns(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Visible split issue", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "bv-2", Title: "Another issue", Status: model.StatusOpen, Labels: []string{"frontend"}},
	}
	m := NewModel(issues, nil, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	m.isSplitView = true
	m.focused = focusList

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = updated.(Model)

	if m.focused != focusLabelDashboard {
		t.Fatalf("expected label dashboard focus, got %v", m.focused)
	}
	view := m.View()
	if !strings.Contains(view, "backend") || !strings.Contains(view, "frontend") {
		t.Fatalf("expected label dashboard after one keypress, got %q", view)
	}
	if strings.Contains(view, "Visible split issue") {
		t.Fatalf("split panes remained visible over label dashboard: %q", view)
	}
	if !strings.Contains(view, "j/k nav") || !strings.Contains(view, "d drilldown") || !strings.Contains(view, "filter") {
		t.Fatalf("expected label dashboard controls immediately, got %q", view)
	}
	if !strings.Contains(view, "[/F3 close") {
		t.Fatalf("expected label dashboard toggle-close hint, got %q", view)
	}
	if strings.Contains(view, "tab focus") {
		t.Fatalf("split-view hints leaked into label dashboard: %q", view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) != 40 || !strings.Contains(lines[len(lines)-1], "j/k nav") {
		t.Fatalf("expected dashboard footer on terminal bottom row, lines=%d last=%q", len(lines), lines[len(lines)-1])
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.focused != focusList || !m.isSplitView {
		t.Fatalf("expected escape to restore split list, focus=%v split=%v", m.focused, m.isSplitView)
	}
	if view := m.View(); !strings.Contains(view, "Visible split issue") {
		t.Fatalf("expected split view after exiting label dashboard, got %q", view)
	}
}

func TestFlowMatrixFooterStaysOnBottomRow(t *testing.T) {
	issues := []model.Issue{
		{ID: "backend", Title: "Backend", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "frontend", Title: "Frontend", Status: model.StatusOpen, Labels: []string{"frontend"}, Dependencies: []*model.Dependency{{DependsOnID: "backend", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	if m.focused != focusFlowMatrix {
		t.Fatalf("expected Flow focus, got %v", m.focused)
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 40 {
		t.Fatalf("Flow view lines = %d, want terminal height 40", len(lines))
	}
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "j/k nav") || !strings.Contains(footer, "tab panel") || !strings.Contains(footer, "f close") {
		t.Fatalf("bottom row missing Flow hints: %q", footer)
	}
	if strings.Contains(footer, "L:labels") || strings.Contains(footer, "h:detail") || strings.Contains(footer, "tab focus") {
		t.Fatalf("underlying list/split hints leaked into Flow footer: %q", footer)
	}
}

func TestLabelDashboardToggleCloseFromSplitView(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Visible split issue", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "bv-2", Title: "Another issue", Status: model.StatusOpen, Labels: []string{"frontend"}},
	}

	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "left bracket", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")}},
		{name: "F3", key: tea.KeyMsg{Type: tea.KeyF3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(issues, nil, "")
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			m = updated.(Model)
			m.isSplitView = true
			m.focused = focusList
			originalFilter := m.currentFilter

			updated, _ = m.Update(tt.key)
			m = updated.(Model)
			if m.focused != focusLabelDashboard {
				t.Fatalf("expected label dashboard focus after opening with %s, got %v", tt.name, m.focused)
			}

			updated, _ = m.Update(tt.key)
			m = updated.(Model)
			if m.focused != focusList || !m.isSplitView {
				t.Fatalf("expected %s to restore split list, focus=%v split=%v", tt.name, m.focused, m.isSplitView)
			}
			if m.currentFilter != originalFilter {
				t.Fatalf("%s changed filter from %q to %q", tt.name, originalFilter, m.currentFilter)
			}
			if m.showLabelDrilldown || m.labelDrilldownLabel != "" || len(m.labelDrilldownIssues) != 0 {
				t.Fatalf("%s opened drilldown while closing dashboard", tt.name)
			}
			if m.showLabelHealthDetail || m.labelHealthDetail != nil {
				t.Fatalf("%s opened label detail while closing dashboard", tt.name)
			}
			if view := m.View(); !strings.Contains(view, "Visible split issue") {
				t.Fatalf("expected restored split view after %s, got %q", tt.name, view)
			}
		})
	}
}

func TestLabelDashboardDrilldownUsesVisibleSelection(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Backend issue", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "bv-2", Title: "Frontend issue", Status: model.StatusOpen, Labels: []string{"frontend"}},
	}
	m := NewModel(issues, nil, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 4})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)

	selected := m.labelDashboard.labels[m.labelDashboard.cursor].Label
	other := m.labelDashboard.labels[1-m.labelDashboard.cursor].Label
	view := m.View()
	if !strings.Contains(view, selected) || strings.Contains(view, other) {
		t.Fatalf("dashboard view does not show only selected label %q: %q", selected, view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(Model)
	if !m.showLabelDrilldown {
		t.Fatal("expected drilldown overlay")
	}
	if m.labelDrilldownLabel != selected {
		t.Fatalf("drilldown label=%q, want visible selection %q", m.labelDrilldownLabel, selected)
	}
	for _, issue := range m.labelDrilldownIssues {
		if !slices.Contains(issue.Labels, selected) {
			t.Fatalf("drilldown included issue %q without selected label %q", issue.ID, selected)
		}
	}
}
