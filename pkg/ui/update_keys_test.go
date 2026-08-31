package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
	"github.com/Dicklesworthstone/beads_viewer/pkg/version"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterShortcutLabelsMatchDispatch(t *testing.T) {
	issues := []model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen, Labels: []string{"backend"}}}

	list := NewModel(issues, nil, "")
	list.width = 240
	list.height = 40
	footer := ansi.Strip(list.renderFooter())
	for _, stale := range []string{"L:labels", "h:detail"} {
		if strings.Contains(footer, stale) {
			t.Errorf("List footer retains stale shortcut %q: %q", stale, footer)
		}
	}
	if !strings.Contains(footer, ";:shortcuts") {
		t.Errorf("List footer missing shortcuts-sidebar hint: %q", footer)
	}

	updated, _ := list.Update(keyMsg("b"))
	board := updated.(Model)
	if footer := ansi.Strip(board.renderFooter()); strings.Contains(footer, "L:labels") || !strings.Contains(footer, "tab:detail") || !strings.Contains(footer, "e:cycle empty") {
		t.Fatalf("Board footer has incorrect contextual shortcuts: %q", footer)
	}

	insights := NewModel(issues, nil, "")
	insights.width = 240
	insights.height = 40
	updated, _ = insights.Update(keyMsg("i"))
	insights = updated.(Model)
	footer = ansi.Strip(insights.renderFooter())
	for _, want := range []string{"i:list"} {
		if !strings.Contains(footer, want) {
			t.Errorf("Insights footer missing %q: %q", want, footer)
		}
	}
	for _, stale := range []string{"]/F4 attention", "f flow"} {
		if strings.Contains(footer, stale) {
			t.Errorf("Insights footer advertises cross-view shortcut %q: %q", stale, footer)
		}
	}

	history := NewModel(issues, nil, "")
	history.width = 240
	history.height = 40
	updated, _ = history.Update(keyMsg("h"))
	history = updated.(Model)
	footer = ansi.Strip(history.renderFooter())
	if !strings.Contains(footer, "h:list") || strings.Contains(footer, "h/esc/q close") || strings.Contains(footer, "H close") {
		t.Fatalf("History footer has incorrect close shortcut: %q", footer)
	}

	help := NewModel(issues, nil, "")
	help.width = 240
	help.height = 40
	updated, _ = help.Update(keyMsg("?"))
	help = updated.(Model)
	footer = ansi.Strip(help.renderFooter())
	for _, want := range []string{"j/k scroll", "space tutorial", "?/esc/q close"} {
		if !strings.Contains(footer, want) {
			t.Errorf("Help footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "any key") {
		t.Fatalf("Help footer claims every key closes it: %q", footer)
	}

	detail := NewModel(issues, nil, "")
	detail.width = 80
	detail.height = 40
	updated, _ = detail.Update(keyMsg("enter"))
	detail = updated.(Model)
	footer = ansi.Strip(detail.renderFooter())
	for _, stale := range []string{"y ID", "x export", "Ctrl+R refresh"} {
		if strings.Contains(footer, stale) {
			t.Errorf("Detail footer is too expansive and contains %q: %q", stale, footer)
		}
	}
	if !strings.Contains(footer, "C full issue") || !strings.Contains(footer, "O edit") {
		t.Errorf("Detail footer lost primary controls: %q", footer)
	}

	split := NewModel(issues, nil, "")
	split.width = 240
	split.height = 40
	split.isSplitView = true
	footer = ansi.Strip(split.renderFooter())
	for _, stale := range []string{"y ID", "x export", "Ctrl+R refresh"} {
		if strings.Contains(footer, stale) {
			t.Errorf("Split footer is too expansive and contains %q: %q", stale, footer)
		}
	}
	if !strings.Contains(footer, "tab focus") || !strings.Contains(footer, "C full issue") {
		t.Errorf("Split footer lost primary controls: %q", footer)
	}
}

func TestAlternateViewFootersUseListReturnHints(t *testing.T) {
	issues := []model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen}}
	cases := []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{name: "tree", setup: func(m *Model) { m.focused = focusTree }, want: "E:list"},
		{name: "graph", setup: func(m *Model) { m.isGraphView = true }, want: "g:list"},
		{name: "board", setup: func(m *Model) { m.isBoardView = true }, want: "b:list"},
		{name: "actionable", setup: func(m *Model) { m.isActionableView = true }, want: "a:list"},
		{name: "history", setup: func(m *Model) { m.isHistoryView = true }, want: "h:list"},
		{name: "sprint", setup: func(m *Model) { m.isSprintView = true }, want: "P/Esc/q:list"},
		{name: "insights", setup: func(m *Model) { m.focused = focusInsights }, want: "i:list"},
		{name: "labels", setup: func(m *Model) { m.focused = focusLabelDashboard }, want: "[/F3:list"},
		{name: "flow", setup: func(m *Model) { m.focused = focusFlowMatrix }, want: "f:list"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(issues, nil, "")
			m.width = 240
			m.height = 40
			tc.setup(&m)
			footer := ansi.Strip(m.renderFooter())
			if !strings.Contains(footer, tc.want) {
				t.Fatalf("footer missing %q: %q", tc.want, footer)
			}
			if strings.Contains(footer, "E:exit") || strings.Contains(footer, "h/esc/q close") {
				t.Fatalf("footer contains stale return hint: %q", footer)
			}
		})
	}
}

func TestSearchFootersDoNotAdvertiseConsumedReturnKeys(t *testing.T) {
	issues := []model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen}}

	graph := NewModel(issues, nil, "")
	graph.isGraphView = true
	graph.graphView.StartSearch()
	if footer := ansi.Strip(graph.renderFooter()); strings.Contains(footer, "g:list") {
		t.Fatalf("Graph search footer advertises a consumed return key: %q", footer)
	}

	board := NewModel(issues, nil, "")
	board.isBoardView = true
	board.board.StartSearch()
	boardFooter := ansi.Strip(board.renderFooter())
	if strings.Contains(boardFooter, "b:list") || strings.Contains(boardFooter, "tab:detail") || strings.Contains(boardFooter, "e:cycle empty") {
		t.Fatalf("Board search footer advertises normal board controls: %q", boardFooter)
	}
	for _, want := range []string{"type search", "enter done", "esc canc"} {
		if !strings.Contains(boardFooter, want) {
			t.Fatalf("Board search footer missing %q: %q", want, boardFooter)
		}
	}
	if strings.Contains(boardFooter, "n/N") {
		t.Fatalf("active Board search footer advertises match navigation: %q", boardFooter)
	}
	board.board.AppendSearchRunes([]rune("Issue"))
	board.board.FinishSearch()
	if footer := ansi.Strip(board.renderFooter()); !strings.Contains(footer, "n/N:match") {
		t.Fatalf("committed Board search footer missing match navigation: %q", footer)
	}

	history := NewModel(issues, nil, "")
	history.isHistoryView = true
	history.historyView.StartSearch()
	if footer := ansi.Strip(history.renderFooter()); strings.Contains(footer, "h:list") {
		t.Fatalf("History search footer advertises a consumed return key: %q", footer)
	}
}

func TestHelpUsesAttentionAndSprintOriginControls(t *testing.T) {
	issues := []model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen, Labels: []string{"backend"}}}

	attention := NewModel(issues, nil, "")
	updated, _ := attention.Update(keyMsg("]"))
	attention = updated.(Model)
	updated, _ = attention.Update(keyMsg("?"))
	attention = updated.(Model)
	attentionHelp := ansi.Strip(attention.renderHelpOverlay())
	for _, want := range []string{"1-9", "Close Attention", "Esc / q"} {
		if !strings.Contains(attentionHelp, want) {
			t.Errorf("Attention Help missing %q: %q", want, attentionHelp)
		}
	}
	for _, stale := range []string{"Home/G", "Tab", "j/k"} {
		if strings.Contains(attentionHelp, stale) {
			t.Errorf("Attention Help advertises unrelated control %q: %q", stale, attentionHelp)
		}
	}

	sprint := NewModel(issues, nil, "")
	sprint.isSprintView = true
	sprint.focused = focusSprint
	updated, _ = sprint.Update(keyMsg("?"))
	sprint = updated.(Model)
	sprintHelp := ansi.Strip(sprint.renderHelpOverlay())
	for _, want := range []string{"j /", "k /", "P / Esc", "Close Sprint"} {
		if !strings.Contains(sprintHelp, want) {
			t.Errorf("Sprint Help missing %q: %q", want, sprintHelp)
		}
	}
	for _, stale := range []string{"Home/G", "Tab", "Enter", "Actionable"} {
		if strings.Contains(sprintHelp, stale) {
			t.Errorf("Sprint Help advertises unrelated control %q: %q", stale, sprintHelp)
		}
	}
}

func TestDetailDispatchAllowsDocumentedSharedActions(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen}}, nil, "")
	updated, _ := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusDetail {
		t.Fatalf("expected Detail focus, got %v", m.focused)
	}

	updated, _ = m.Update(keyMsg("C"))
	m = updated.(Model)
	if m.statusMsg == "" {
		t.Fatal("Detail swallowed documented C copy action")
	}

	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	if m.focused != focusGraph || !m.isGraphView {
		t.Fatalf("Detail swallowed documented Graph switch: focus=%v graph=%v", m.focused, m.isGraphView)
	}
}

func TestDetailDispatchAllowsSelfUpdate(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen}}, nil, "")
	m.updateAvailable = true
	m.updateTag = "v9.9.9"
	updated, _ := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusDetail {
		t.Fatalf("expected Detail focus, got %v", m.focused)
	}

	updated, _ = m.Update(keyMsg("U"))
	m = updated.(Model)
	if !m.showUpdateModal || m.focused != focusUpdateModal {
		t.Fatalf("Detail swallowed documented self-update action: modal=%v focus=%v", m.showUpdateModal, m.focused)
	}
}

func TestDetailDispatchRejectsListOnlyCommands(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen, Labels: []string{"backend"}}}, nil, "")
	updated, _ := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusDetail {
		t.Fatalf("expected Detail focus, got %v", m.focused)
	}

	for _, key := range []string{"'", "l", "I", "w", "o", "c", "r", "s", "S", "V", "U", "f", "[", "]"} {
		t.Run(key, func(t *testing.T) {
			beforeFilter := m.currentFilter
			beforeSort := m.sortMode
			updated, _ := m.Update(keyMsg(key))
			result := updated.(Model)
			if result.focused != focusDetail || result.showRecipePicker || result.showLabelPicker || result.showTypePicker || result.showRepoPicker || result.isActionableView || result.isBoardView || result.isGraphView || result.isHistoryView || result.focused == focusFlowMatrix || result.focused == focusLabelDashboard || result.showAttentionView {
				t.Fatalf("Detail accepted List-only command: focus=%v recipe=%v label=%v type=%v repo=%v", result.focused, result.showRecipePicker, result.showLabelPicker, result.showTypePicker, result.showRepoPicker)
			}
			if result.currentFilter != beforeFilter || result.sortMode != beforeSort {
				t.Fatalf("Detail List-only command changed list state: filter=%q sort=%v", result.currentFilter, result.sortMode)
			}
		})
	}
}

func TestSplitHelpOmitsListEnterAndEscape(t *testing.T) {
	m := sizedModel(t, []model.Issue{{ID: "bv-1", Title: "Issue", Status: model.StatusOpen}}, 120, 30)
	m.isSplitView = true
	m.focused = focusList
	updated, _ := m.Update(keyMsg("?"))
	m = updated.(Model)
	help := ansi.Strip(m.renderHelpOverlay())
	if strings.Contains(help, "Select / open") || strings.Contains(help, "Back / close") {
		t.Fatalf("Split Help retained List-only Enter/Esc guidance:\n%s", help)
	}
	if !strings.Contains(help, "Tab") {
		t.Fatalf("Split Help lost pane-switch guidance:\n%s", help)
	}
}

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
	if !strings.Contains(help, "E         Tree view") || !strings.Contains(help, "I         Exact issue-type picker") || !strings.Contains(help, "← / →") {
		t.Fatalf("List help lacks Tree or exact type shortcut:\n%s", help)
	}
}

func selectListIssueForTest(t *testing.T, m *Model, id string) {
	t.Helper()
	for i, item := range m.list.Items() {
		if issue, ok := item.(IssueItem); ok && issue.Issue.ID == id {
			m.list.Select(i)
			return
		}
	}
	t.Fatalf("test issue %q not found in list", id)
}

func TestGraphTransitionSynchronizesListSelection(t *testing.T) {
	issues := []model.Issue{
		{ID: "zeta", Title: "Zeta", Status: model.StatusOpen, Priority: 3, IssueType: model.TypeTask},
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeBug},
		{ID: "target", Title: "Target", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeFeature},
	}
	m := NewModel(issues, nil, "")
	selectListIssueForTest(t, &m, "target")

	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "target" {
		t.Fatalf("List -> Graph selection = %#v, want target", selected)
	}
}

func TestTreeTransitionSynchronizesListSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		selectedID string
		wantID     string
		wantOffset int
	}{
		{name: "selected matching bead", selectedID: "tree-b-other", wantID: "tree-b-other", wantOffset: 1},
		{name: "bead omitted by tree projection", selectedID: "tree-c-cycle", wantID: "tree-a-target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel([]model.Issue{
				{ID: "tree-a-target", Title: "Target issue", Status: model.StatusOpen, Priority: 0, IssueType: model.TypeTask, Labels: []string{"focus"}},
				{ID: "tree-b-other", Title: "Other issue", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask, Labels: []string{"focus"}},
				{ID: "tree-c-cycle", Title: "Cycle issue", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeTask, Labels: []string{"focus"}, Dependencies: []*model.Dependency{{IssueID: "tree-c-cycle", DependsOnID: "tree-d-cycle", Type: model.DepParentChild}}},
				{ID: "tree-d-cycle", Title: "Cycle issue", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeTask, Labels: []string{"focus"}, Dependencies: []*model.Dependency{{IssueID: "tree-d-cycle", DependsOnID: "tree-c-cycle", Type: model.DepParentChild}}},
			}, nil, "")
			m.width, m.height = 120, 4
			r := &recipe.Recipe{Name: "focused", Filters: recipe.FilterConfig{Tags: []string{"focus"}}}
			m.setActiveRecipe(r)
			m.applyRecipe(r)
			selectListIssueForTest(t, &m, tc.selectedID)

			updated, _ := m.Update(keyMsg("E"))
			m = updated.(Model)

			if m.focused != focusTree {
				t.Fatalf("List -> Tree focus=%v, want tree", m.focused)
			}
			if selected := m.tree.GetSelectedID(); selected != tc.wantID {
				t.Fatalf("List -> Tree selection = %q, want %q", selected, tc.wantID)
			}
			if offset := m.tree.GetViewportOffset(); offset != tc.wantOffset {
				t.Fatalf("List -> Tree viewport offset = %d, want %d", offset, tc.wantOffset)
			}
		})
	}
}

func TestGraphToTreeRevealsCollapsedDescendant(t *testing.T) {
	issues := []model.Issue{
		{ID: "tree-root", Title: "Tree root", Status: model.StatusOpen, Priority: 0, IssueType: model.TypeEpic},
		{ID: "tree-parent", Title: "Tree parent", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeEpic, Dependencies: []*model.Dependency{{DependsOnID: "tree-root", Type: model.DepParentChild}}},
		{ID: "tree-target", Title: "Tree target", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeTask, Dependencies: []*model.Dependency{{DependsOnID: "tree-parent", Type: model.DepParentChild}}},
	}

	m := NewModel(issues, nil, "")
	m.width, m.height = 120, 4
	m.tree.Build(issues)
	m.tree.issueMap["tree-parent"].Expanded = false
	m.tree.rebuildFlatList()
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if !m.graphView.SelectByID("tree-target") {
		t.Fatal("test graph selection failed")
	}

	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	if m.focused != focusTree || m.tree.GetSelectedID() != "tree-target" {
		t.Fatalf("Graph -> Tree selection = %q, focus=%v, want tree-target/tree", m.tree.GetSelectedID(), m.focused)
	}
	if !m.tree.issueMap["tree-parent"].Expanded || m.tree.GetViewportOffset() == 0 || !strings.Contains(ansi.Strip(m.tree.View()), "tree-target") {
		t.Fatalf("Graph -> Tree selection was not visible: offset=%d view=%q", m.tree.GetViewportOffset(), m.tree.View())
	}
}

func TestGraphToTreeUsesProjectionFallback(t *testing.T) {
	issues := []model.Issue{
		{ID: "aaa-excluded", Title: "Excluded root", Status: model.StatusOpen, Priority: 0},
		{ID: "tree-root", Title: "Tree root", Status: model.StatusOpen, Priority: 1, Labels: []string{"focus"}},
		{ID: "tree-cycle-a", Title: "Cycle A", Status: model.StatusOpen, Labels: []string{"focus"}, Dependencies: []*model.Dependency{{DependsOnID: "tree-cycle-b", Type: model.DepParentChild}}},
		{ID: "tree-cycle-b", Title: "Cycle B", Status: model.StatusOpen, Labels: []string{"focus"}, Dependencies: []*model.Dependency{{DependsOnID: "tree-cycle-a", Type: model.DepParentChild}}},
	}
	m := NewModel(issues, nil, "")
	r := &recipe.Recipe{Name: "focused", Filters: recipe.FilterConfig{Tags: []string{"focus"}}}
	m.setActiveRecipe(r)
	m.applyRecipe(r)
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if !m.graphView.SelectByID("tree-cycle-a") {
		t.Fatal("test graph selection failed")
	}

	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	if m.focused != focusTree || m.tree.GetSelectedID() != "tree-root" {
		t.Fatalf("Graph -> Tree fallback = %q, focus=%v, want tree-root/tree", m.tree.GetSelectedID(), m.focused)
	}
}

func TestTreeToGraphTransfersSelectionAndViewport(t *testing.T) {
	issues := make([]model.Issue, 0, 21)
	for i := 0; i < 20; i++ {
		issues = append(issues, model.Issue{
			ID: fmt.Sprintf("graph-node-%02d", i), Title: "Graph node", Status: model.StatusOpen,
			Priority: 1, IssueType: model.TypeTask,
		})
	}
	issues = append(issues, model.Issue{ID: "graph-target", Title: "Graph target", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask})

	m := NewModel(issues, nil, "")
	m.width, m.height = 120, 8
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	if !m.tree.SelectByID("graph-target") {
		t.Fatal("test tree selection failed")
	}

	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	if m.focused != focusTree {
		t.Fatalf("first Tree graph shortcut changed focus to %v", m.focused)
	}
	updated, _ = m.Update(comboTickMsg{key: "g"})
	m = updated.(Model)
	if m.focused != focusGraph || m.graphView.SelectedIssue() == nil || m.graphView.SelectedIssue().ID != "graph-target" {
		t.Fatalf("Tree -> Graph selection = %#v, focus=%v, want graph-target/graph", m.graphView.SelectedIssue(), m.focused)
	}
	m.graphView.View(m.width, m.height)
	if m.graphView.scrollOffset == 0 {
		t.Fatalf("Tree -> Graph selection did not move destination viewport: offset=%d", m.graphView.scrollOffset)
	}
}

func TestTreeToGraphUsesProjectionFallback(t *testing.T) {
	issues := []model.Issue{
		{ID: "todo-note", Title: "Captured note", Status: model.StatusOpen, IssueType: "todo", Labels: []string{"focus"}},
		{ID: "aaa-excluded", Title: "Excluded issue", Status: model.StatusOpen, Priority: 0},
		{ID: "visible-a", Title: "Visible A", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"focus"}},
		{ID: "visible-b", Title: "Visible B", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"focus"}},
	}
	m := NewModel(issues, nil, "")
	r := &recipe.Recipe{Name: "focused", Filters: recipe.FilterConfig{Tags: []string{"focus"}}}
	m.setActiveRecipe(r)
	m.applyRecipe(r)
	selectListIssueForTest(t, &m, "todo-note")
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	if m.tree.GetSelectedID() != "todo-note" {
		t.Fatalf("Tree setup selection = %q, want todo-note", m.tree.GetSelectedID())
	}
	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	updated, _ = m.Update(comboTickMsg{key: "g"})
	m = updated.(Model)
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "visible-a" {
		t.Fatalf("Tree -> Graph fallback = %#v, want visible-a", selected)
	}
}

func TestGraphTransitionSynchronizesGraphSelectionBackToList(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "target", Title: "Target", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	m := NewModel(issues, nil, "")
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if !m.graphView.SelectByID("target") {
		t.Fatal("test graph selection failed")
	}

	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	if selected := m.list.SelectedItem(); selected == nil {
		t.Fatal("List selection is nil after Graph -> List")
	} else if item, ok := selected.(IssueItem); !ok || item.Issue.ID != "target" {
		t.Fatalf("Graph -> List selection = %#v, want target", selected)
	}
}

func TestGraphExitKeysSynchronizeGraphSelectionBackToList(t *testing.T) {
	for _, key := range []string{"g", "q", "esc"} {
		t.Run(key, func(t *testing.T) {
			m := NewModel([]model.Issue{
				{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, IssueType: model.TypeTask},
				{ID: "target", Title: "Target", Status: model.StatusOpen, IssueType: model.TypeTask},
			}, nil, "")
			updated, _ := m.Update(keyMsg("g"))
			m = updated.(Model)
			if !m.graphView.SelectByID("target") {
				t.Fatal("test graph selection failed")
			}

			updated, _ = m.Update(keyMsg(key))
			m = updated.(Model)
			if m.isGraphView || m.focused != focusList {
				t.Fatalf("Graph exit key %q left focus=%v graph=%v", key, m.focused, m.isGraphView)
			}
			if selected := m.selectedListIssueID(); selected != "target" {
				t.Fatalf("Graph exit key %q selected list bead %q, want target", key, selected)
			}
		})
	}
}

func TestGraphEnterSelectsVisibleFilteredBead(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "before", Title: "Before", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "target", Title: "Target", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "after", Title: "After", Status: model.StatusOpen, IssueType: model.TypeTask},
	}, nil, "")
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if !m.graphView.SelectByID("target") {
		t.Fatal("test graph selection failed")
	}
	m.list.SetFilterText("target")
	if len(m.list.VisibleItems()) != 1 {
		t.Fatalf("test filter returned %d visible items, want 1", len(m.list.VisibleItems()))
	}

	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusDetail || m.selectedListIssueID() != "target" {
		t.Fatalf("filtered Graph -> Enter selected focus=%v bead=%q, want detail/target", m.focused, m.selectedListIssueID())
	}
	if !strings.Contains(m.viewport.View(), "Target") {
		t.Fatalf("filtered Graph -> Enter omitted target detail: %s", m.viewport.View())
	}
}

func graphModelWithExcludedListFilter(t *testing.T) Model {
	t.Helper()
	m := NewModel([]model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "target", Title: "Target", Status: model.StatusOpen, IssueType: model.TypeTask},
	}, nil, "")
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if !m.graphView.SelectByID("target") {
		t.Fatal("test graph selection failed")
	}
	m.list.SetFilterText("alpha")
	if m.selectedListIssueID() != "alpha" {
		t.Fatalf("test setup selected %q, want alpha", m.selectedListIssueID())
	}
	return m
}

func TestGraphEnterSelectsBeadExcludedByListTextFilter(t *testing.T) {
	m := graphModelWithExcludedListFilter(t)
	if len(m.list.VisibleItems()) != 1 {
		t.Fatalf("test filter returned %d visible items, want 1", len(m.list.VisibleItems()))
	}

	updated, _ := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusDetail || m.selectedListIssueID() != "target" {
		t.Fatalf("filtered Graph -> Enter selected focus=%v bead=%q, want detail/target", m.focused, m.selectedListIssueID())
	}
	if m.list.FilterValue() != "" {
		t.Fatalf("incompatible list text filter remained %q", m.list.FilterValue())
	}
	if !strings.Contains(m.viewport.View(), "Target") {
		t.Fatalf("filtered Graph -> Enter omitted target detail: %s", m.viewport.View())
	}
}

func TestGraphExitKeysSelectBeadExcludedByListTextFilter(t *testing.T) {
	for _, key := range []string{"g", "q", "esc"} {
		t.Run(key, func(t *testing.T) {
			m := graphModelWithExcludedListFilter(t)
			updated, _ := m.Update(keyMsg(key))
			m = updated.(Model)
			if m.isGraphView || m.focused != focusList {
				t.Fatalf("Graph exit key %q left focus=%v graph=%v", key, m.focused, m.isGraphView)
			}
			if m.list.FilterValue() != "" || m.selectedListIssueID() != "target" {
				t.Fatalf("Graph exit key %q left filter=%q selected=%q, want cleared/target", key, m.list.FilterValue(), m.selectedListIssueID())
			}
		})
	}
}

func TestGraphTransitionUsesFirstNodeWhenListSelectionIsFilteredOut(t *testing.T) {
	issues := []model.Issue{
		{ID: "todo-note", Title: "Captured note", Status: model.StatusOpen, IssueType: "todo"},
		{ID: "visible-a", Title: "Visible A", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "visible-b", Title: "Visible B", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	m := NewModel(issues, nil, "")
	selectListIssueForTest(t, &m, "todo-note")

	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "visible-a" {
		t.Fatalf("filtered List -> Graph selection = %#v, want visible-a fallback", selected)
	}
	if selected := m.selectedListIssueID(); selected != "todo-note" {
		t.Fatalf("filtered List selection changed to %q, want todo-note", selected)
	}
}

func TestGraphSelectionRoundTripToDetailPreservesBeadIdentity(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Title: "Alpha", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "target", Title: "Target", Status: model.StatusOpen, IssueType: model.TypeFeature},
	}
	m := NewModel(issues, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	selectListIssueForTest(t, &m, "target")

	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)

	if m.FocusState() != "detail" {
		t.Fatalf("Graph -> Enter did not open detail: focus=%s", m.FocusState())
	}
	if selected := m.selectedListIssueID(); selected != "target" {
		t.Fatalf("round-trip list selection = %q, want target", selected)
	}
	if !strings.Contains(m.viewport.View(), "Target") {
		t.Fatalf("round-trip detail omitted target bead: %s", m.viewport.View())
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
	if !strings.Contains(view, "[/F3:list") {
		t.Fatalf("expected label dashboard return hint, got %q", view)
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
	if !strings.Contains(footer, "j/k nav") || !strings.Contains(footer, "⏎ drill") || !strings.Contains(footer, "f:list") {
		t.Fatalf("bottom row missing Flow hints: %q", footer)
	}
	if strings.Count(view, "j/k nav") != 1 || strings.Contains(view, "Press Enter to see issues") {
		t.Fatalf("Flow should have one shared help legend and no internal hint: %q", view)
	}
	if strings.Contains(footer, "L:labels") || strings.Contains(footer, "h:detail") || strings.Contains(strings.ToLower(footer), "tab") {
		t.Fatalf("underlying list/split hints leaked into Flow footer: %q", footer)
	}
}

func TestFlowMatrixZeroFlowFooterOmitsDrilldown(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "backend", Title: "Backend", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "frontend", Title: "Frontend", Status: model.StatusOpen, Labels: []string{"frontend"}},
	}, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)

	lines := strings.Split(m.View(), "\n")
	footer := lines[len(lines)-1]
	if strings.Contains(footer, "drill") || strings.Contains(footer, "⏎") {
		t.Fatalf("zero-flow footer advertises unavailable drilldown: %q", footer)
	}
	if !strings.Contains(footer, "j/k nav") || !strings.Contains(footer, "esc back") || !strings.Contains(footer, "f:list") {
		t.Fatalf("zero-flow footer lost available Flow controls: %q", footer)
	}
}

func TestFlowMatrixDrilldownFooterDescribesIssueNavigation(t *testing.T) {
	issues := []model.Issue{
		{ID: "backend", Title: "Backend", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "frontend", Title: "Frontend", Status: model.StatusOpen, Labels: []string{"frontend"}, Dependencies: []*model.Dependency{{DependsOnID: "backend", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.flowMatrix.showDrilldown {
		t.Fatal("Enter did not open Flow drilldown")
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 40 {
		t.Fatalf("Flow drilldown lines = %d, want terminal height 40", len(lines))
	}
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "⏎ jump/open") {
		t.Fatalf("drilldown footer missing selected-issue action: %q", footer)
	}
	if strings.Contains(footer, "⏎ drill") {
		t.Fatalf("drilldown footer still advertises label drilldown: %q", footer)
	}
	if !strings.Contains(footer, "esc back") || !strings.Contains(footer, "f close") {
		t.Fatalf("drilldown footer lacks state-aware close controls: %q", footer)
	}
}

func TestFlowEnterDoesNotOpenIssueExcludedByListFilter(t *testing.T) {
	issues := []model.Issue{
		{ID: "backend", Title: "Backend", Status: model.StatusOpen, Labels: []string{"backend"}},
		{ID: "frontend", Title: "Frontend", Status: model.StatusOpen, Labels: []string{"frontend"}, Dependencies: []*model.Dependency{{DependsOnID: "backend", Type: model.DepBlocks}}},
		{ID: "unrelated", Title: "Unrelated", Status: model.StatusClosed},
	}
	m := NewModel(issues, nil, "")
	m.statusFilter = "closed"
	m.applyFilter()
	if got := m.selectedListIssueID(); got != "unrelated" {
		t.Fatalf("filtered List selection = %q, want unrelated", got)
	}

	m.focused = focusFlowMatrix
	m.refreshFlowMatrix()
	m.flowMatrix.OpenDrilldown()
	for i, issue := range m.flowMatrix.drilldownIssues {
		if issue.Status == model.StatusOpen {
			m.flowMatrix.drilldownCursor = i
			break
		}
	}
	selected := m.flowMatrix.SelectedDrilldownIssue()
	if selected == nil || selected.ID == "unrelated" {
		t.Fatalf("Flow drilldown did not select an excluded issue: %#v", selected)
	}

	updated, _ := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusFlowMatrix || !m.flowMatrix.showDrilldown || m.showDetails {
		t.Fatalf("Flow opened stale Detail selection: focus=%v drilldown=%v details=%v selected=%q", m.focused, m.flowMatrix.showDrilldown, m.showDetails, m.selectedListIssueID())
	}
}

func TestFlowHelpDescribesStateAwareEnterAction(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.focused = focusFlowMatrix

	topLevel := ansi.Strip(m.renderHelpOverlay())
	if !strings.Contains(topLevel, "Drill into label") || strings.Contains(topLevel, "Open / jump to issue") {
		t.Fatalf("top-level Flow Help has incorrect Enter guidance:\n%s", topLevel)
	}

	m.flowMatrix.showDrilldown = true
	drilldown := ansi.Strip(m.renderHelpOverlay())
	if !strings.Contains(drilldown, "Open / jump to issue") || strings.Contains(drilldown, "Drill into label") {
		t.Fatalf("Flow drilldown Help has incorrect Enter guidance:\n%s", drilldown)
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

func TestInsightsEnterShowsDirectDetailAndPreservesClosedFilter(t *testing.T) {
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
	if m.focused != focusDetail || !m.showDetails || m.insightsDetailID != "active" {
		t.Fatalf("Enter did not bind direct Insights detail: focus=%v details=%v id=%q", m.focused, m.showDetails, m.insightsDetailID)
	}
	if !strings.Contains(m.viewport.View(), "Active") {
		t.Fatalf("direct Insights detail omitted active issue: %s", m.viewport.View())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.focused != focusInsights || m.showDetails || m.insightsDetailID != "" || m.currentFilter != "closed" {
		t.Fatalf("leaving direct detail did not preserve Insights/filter state: focus=%v details=%v id=%q filter=%q", m.focused, m.showDetails, m.insightsDetailID, m.currentFilter)
	}
}

func TestInsightsDirectDetailClearsOnSplitListInteraction(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "active", Title: "Active", Status: model.StatusOpen},
		{ID: "closed", Title: "Closed", Status: model.StatusClosed},
	}, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)
	m.insightsPanel.insights.Bottlenecks = []analysis.InsightItem{{ID: "active"}}
	m.insightsPanel.focusedPanel = PanelBottlenecks
	m.insightsPanel.selectedIndex[PanelBottlenecks] = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.isSplitView || m.insightsDetailID != "active" {
		t.Fatalf("split direct detail setup failed: split=%v id=%q", m.isSplitView, m.insightsDetailID)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focused != focusList || m.insightsDetailID != "" {
		t.Fatalf("Tab leaked direct detail into List: focus=%v id=%q", m.focused, m.insightsDetailID)
	}

	m.insightsDetailID = "active"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.insightsDetailID != "" {
		t.Fatal("List selection retained direct Insights detail binding")
	}

	m.insightsDetailID = "active"
	m.focused = focusDetail
	m = m.handleLeftClick(5, m.listChromeLines())
	if m.focused != focusList || m.insightsDetailID != "" {
		t.Fatalf("list click leaked direct detail: focus=%v id=%q", m.focused, m.insightsDetailID)
	}

	m.insightsDetailID = "active"
	m.focused = focusDetail
	m = m.handleLeftClick(5, 1) // Header chrome, not a selectable row.
	if m.focused != focusList || m.insightsDetailID != "" {
		t.Fatalf("header click leaked direct detail: focus=%v id=%q", m.focused, m.insightsDetailID)
	}
	if !strings.Contains(m.viewport.View(), "Closed") {
		t.Fatalf("header click left stale direct detail visible: %s", m.viewport.View())
	}
}

func TestInsightsDirectDetailClearsWhenIssueClosesOnRefresh(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "active", Title: "Active", Status: model.StatusOpen},
		{ID: "closed", Title: "Closed", Status: model.StatusClosed},
	}, nil, "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(Model)
	m.insightsPanel.insights.Bottlenecks = []analysis.InsightItem{{ID: "active"}}
	m.insightsPanel.focusedPanel = PanelBottlenecks
	m.insightsPanel.selectedIndex[PanelBottlenecks] = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.insightsDetailID != "active" {
		t.Fatalf("direct detail setup failed: %q", m.insightsDetailID)
	}

	m.issues[0].Status = model.StatusClosed
	m.refreshRepositoryCandidates()
	if m.insightsDetailID != "" || m.showDetails || m.focused != focusInsights {
		t.Fatalf("closed refresh retained direct detail: id=%q details=%v focus=%v", m.insightsDetailID, m.showDetails, m.focused)
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

func TestRecipePickerToggleCancelRestoresOriginAndDraft(t *testing.T) {
	issues := []model.Issue{
		{ID: "one", Title: "One", Status: model.StatusOpen},
		{ID: "two", Title: "Two", Status: model.StatusInProgress},
	}
	origins := []struct {
		name  string
		focus focus
		board bool
	}{
		{name: "list", focus: focusList},
		{name: "board", focus: focusBoard, board: true},
		{name: "insights", focus: focusInsights},
	}

	for _, origin := range origins {
		t.Run(origin.name, func(t *testing.T) {
			m := NewModel(issues, nil, "")
			active := m.recipeLoader.Get("default")
			m.setActiveRecipe(active)
			m.applyRecipe(active)
			m.focused = origin.focus
			m.isBoardView = origin.board
			m.list.Select(1)
			appliedIndex := func() int {
				m.resetRecipePicker()
				return m.recipePicker.SelectedIndex()
			}()
			appliedFilter := m.currentFilter
			appliedListIndex := m.list.Index()

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
			m = updated.(Model)
			if !m.showRecipePicker || m.focused != focusRecipePicker || m.recipePickerOrigin != origin.focus {
				t.Fatalf("recipe picker open state: shown=%v focus=%v origin=%v", m.showRecipePicker, m.focused, m.recipePickerOrigin)
			}
			m = m.handleRecipePickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			if m.recipePicker.SelectedIndex() == appliedIndex {
				t.Fatal("recipe draft did not move before cancel")
			}

			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
			m = updated.(Model)
			if m.showRecipePicker || m.focused != origin.focus || m.isBoardView != origin.board {
				t.Fatalf("recipe cancel did not restore origin: shown=%v focus=%v board=%v", m.showRecipePicker, m.focused, m.isBoardView)
			}
			if m.activeRecipe == nil || m.activeRecipe.Name != "default" || m.currentFilter != appliedFilter || m.list.Index() != appliedListIndex {
				t.Fatalf("recipe cancel changed applied state: recipe=%v filter=%q index=%d", m.activeRecipe, m.currentFilter, m.list.Index())
			}

			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
			m = updated.(Model)
			if m.recipePicker.SelectedIndex() != appliedIndex {
				t.Fatalf("recipe draft survived cancel: got index %d, want %d", m.recipePicker.SelectedIndex(), appliedIndex)
			}
		})
	}
}

func TestRecipeShortcutRemainsListSearchInput(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "one", Title: "One", Status: model.StatusOpen}}, nil, "")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'")})
	m = updated.(Model)
	if m.showRecipePicker || m.list.FilterInput.Value() != "'" {
		t.Fatalf("recipe shortcut escaped active list search: shown=%v query=%q", m.showRecipePicker, m.list.FilterInput.Value())
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
