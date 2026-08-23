package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/version"
	tea "github.com/charmbracelet/bubbletea"
)

func TestListHelpRendersTreeAndExactTypePickerShortcuts(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width = 160
	m.height = 40
	m.focused = focusList

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("? did not open List help: shown=%v focus=%v", m.showHelp, m.focused)
	}
	help := m.renderHelpOverlay()
	if !strings.Contains(help, "E         Tree view") || !strings.Contains(help, "I         Exact issue-type picker") {
		t.Fatalf("List help lacks Tree or exact type shortcut:\n%s", help)
	}
}

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

func TestAttentionViewToggleCloseFromSplitView(t *testing.T) {
	issues := []model.Issue{{
		ID: "bv-1", Title: "Visible split issue", Status: model.StatusOpen, Labels: []string{"backend"},
	}}
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "right bracket", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}},
		{name: "F4", key: tea.KeyMsg{Type: tea.KeyF4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(issues, nil, "")
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			m = updated.(Model)
			m.isSplitView = true
			m.focused = focusList

			updated, _ = m.Update(tt.key)
			m = updated.(Model)
			if !m.showAttentionView || m.focused != focusInsights {
				t.Fatalf("expected Attention to open with %s, shown=%v focus=%v", tt.name, m.showAttentionView, m.focused)
			}

			updated, _ = m.Update(tt.key)
			m = updated.(Model)
			if m.showAttentionView || m.focused != focusList || !m.isSplitView {
				t.Fatalf("expected %s to restore split list, shown=%v focus=%v split=%v", tt.name, m.showAttentionView, m.focused, m.isSplitView)
			}
			if m.insightsPanel.extraText != "" {
				t.Fatalf("expected %s to clear Attention content", tt.name)
			}
			if view := m.View(); !strings.Contains(view, "Visible split issue") {
				t.Fatalf("expected split view after closing with %s, got %q", tt.name, view)
			}
		})
	}
}

func TestAttentionViewConsumesInsightsStatusKeys(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen}}, nil, "")
	m.currentFilter = "label:keep"
	m.statusFilter = "closed"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = updated.(Model)
	if !m.showAttentionView || m.focused != focusInsights {
		t.Fatalf("Attention did not open: shown=%v focus=%v", m.showAttentionView, m.focused)
	}
	for _, key := range []string{"o", "r", "c"} {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(Model)
		if m.currentFilter != "label:keep" || m.statusFilter != "closed" || !m.showAttentionView || m.focused != focusInsights {
			t.Fatalf("Attention key %q changed shared state: filter=%q status=%q shown=%v focus=%v", key, m.currentFilter, m.statusFilter, m.showAttentionView, m.focused)
		}
	}
}

func TestInsightsEnterDoesNotShowStaleDetailFromClosedFilter(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "active", Title: "Active", Status: model.StatusOpen},
		{ID: "closed", Title: "Closed", Status: model.StatusClosed},
	}, nil, "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)
	if m.focused != focusInsights || m.currentFilter != "closed" {
		t.Fatalf("closed-filter Insights entry failed: focus=%v filter=%q", m.focused, m.currentFilter)
	}
	m.insightsPanel.insights.Bottlenecks = []analysis.InsightItem{{ID: "active"}}
	m.insightsPanel.focusedPanel = PanelBottlenecks
	m.insightsPanel.selectedIndex[PanelBottlenecks] = 0

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.focused != focusInsights || m.showDetails {
		t.Fatalf("Enter showed stale detail for unavailable active ID: focus=%v details=%v", m.focused, m.showDetails)
	}
}

func TestAttentionViewEscapeAndQRestoreOrigin(t *testing.T) {
	issues := []model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen, Labels: []string{"backend"}}}
	origins := []struct {
		name       string
		focus      focus
		graph      bool
		board      bool
		actionable bool
		history    bool
		sprint     bool
		split      bool
	}{
		{name: "split list", focus: focusList, split: true},
		{name: "Insights", focus: focusInsights},
		{name: "Graph", focus: focusGraph, graph: true},
		{name: "Board", focus: focusBoard, board: true},
		{name: "Tree", focus: focusTree},
		{name: "Actionable", focus: focusActionable, actionable: true},
		{name: "History", focus: focusHistory, history: true},
		{name: "Sprint", focus: focusSprint, sprint: true},
		{name: "Flow Matrix", focus: focusFlowMatrix},
	}
	closeKeys := []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune("q")},
	}

	for _, origin := range origins {
		for _, closeKey := range closeKeys {
			name := origin.name + "/" + closeKey.String()
			t.Run(name, func(t *testing.T) {
				m := NewModel(issues, nil, "")
				m.focused = origin.focus
				m.isGraphView = origin.graph
				m.isBoardView = origin.board
				m.isActionableView = origin.actionable
				m.isHistoryView = origin.history
				m.isSprintView = origin.sprint
				m.isSplitView = origin.split

				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
				m = updated.(Model)
				if !m.showAttentionView || m.attentionOrigin != origin.focus {
					t.Fatalf("expected Attention origin %v, shown=%v origin=%v", origin.focus, m.showAttentionView, m.attentionOrigin)
				}

				updated, _ = m.Update(closeKey)
				m = updated.(Model)
				if m.showAttentionView || m.focused != origin.focus {
					t.Fatalf("expected return to %v, shown=%v focus=%v", origin.focus, m.showAttentionView, m.focused)
				}
				if m.isGraphView != origin.graph || m.isBoardView != origin.board ||
					m.isActionableView != origin.actionable || m.isHistoryView != origin.history ||
					m.isSprintView != origin.sprint || m.isSplitView != origin.split {
					t.Fatalf("origin flags not restored for %s", origin.name)
				}
			})
		}
	}
}

func TestAttentionViewNumericKeyFiltersAndTransitionsToList(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Title: "Backend one", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "bv-2", Title: "Backend two", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "bv-3", Title: "Frontend only", Status: model.StatusOpen, Labels: []string{"frontend"}},
	}
	m := NewModel(issues, nil, "")
	m.focused = focusBoard
	m.isBoardView = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = updated.(Model)
	selectedLabel := m.attentionCache.Labels[1].Label

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = updated.(Model)
	if m.currentFilter != "label:"+selectedLabel {
		t.Fatalf("filter=%q, want label:%s", m.currentFilter, selectedLabel)
	}
	if m.showAttentionView || m.focused != focusList || m.isBoardView {
		t.Fatalf("expected filtered List, shown=%v focus=%v board=%v", m.showAttentionView, m.focused, m.isBoardView)
	}
	if m.insightsPanel.extraText != "" || m.attentionOrigin != focusList {
		t.Fatal("expected Attention overlay state to be cleared")
	}

	wantCount := 0
	for _, issue := range issues {
		if slices.Contains(issue.Labels, selectedLabel) {
			wantCount++
		}
	}
	if len(m.list.Items()) != wantCount || m.board.TotalCount() != wantCount || m.graphView.TotalCount() != wantCount {
		t.Fatalf("filter propagation counts: list=%d board=%d graph=%d, want %d", len(m.list.Items()), m.board.TotalCount(), m.graphView.TotalCount(), wantCount)
	}
	selected, ok := m.list.SelectedItem().(IssueItem)
	if !ok || !strings.Contains(m.viewport.View(), selected.Issue.ID) {
		t.Fatalf("detail content did not follow filtered List selection: %q", m.viewport.View())
	}
}

func TestAttentionViewInvalidNumericKeyDoesNothing(t *testing.T) {
	tests := []struct {
		name   string
		issues []model.Issue
		key    string
	}{
		{name: "empty", key: "1"},
		{name: "out of range", issues: []model.Issue{{ID: "bv-1", Title: "One", Status: model.StatusOpen, Labels: []string{"backend"}}}, key: "2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(tt.issues, nil, "")
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
			m = updated.(Model)
			beforeFilter := m.currentFilter
			beforeItems := len(m.list.Items())

			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			m = updated.(Model)
			if !m.showAttentionView || m.focused != focusInsights {
				t.Fatalf("invalid numeric key closed Attention: shown=%v focus=%v", m.showAttentionView, m.focused)
			}
			if m.currentFilter != beforeFilter || len(m.list.Items()) != beforeItems {
				t.Fatalf("invalid numeric key mutated filter state: filter=%q items=%d", m.currentFilter, len(m.list.Items()))
			}
		})
	}
}

func TestAttentionViewSparseContentAnchorsContextualFooter(t *testing.T) {
	issues := []model.Issue{{
		ID: "bv-1", Title: "Issue", Status: model.StatusOpen, Labels: []string{"backend"},
	}}
	m := NewModel(issues, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	m.isSplitView = true
	m.focused = focusList

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "ATTENTION") || !strings.Contains(view, "1-9 filter list by ranked label") ||
		!strings.Contains(view, "]/F4 close") || !strings.Contains(view, "esc/q back") {
		t.Fatalf("expected Attention identity and controls, got %q", view)
	}
	if strings.Contains(view, "h/l panels") || strings.Contains(view, "tab focus") {
		t.Fatalf("underlying view hints leaked into Attention: %q", view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) != 40 || !strings.Contains(lines[len(lines)-1], "]/F4 close") {
		t.Fatalf("expected Attention footer on terminal bottom row, lines=%d last=%q", len(lines), lines[len(lines)-1])
	}
}
