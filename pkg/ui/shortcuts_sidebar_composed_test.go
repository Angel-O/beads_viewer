package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestShortcutsSidebarComposedWidthFitsTerminal is a stronger companion to
// TestShortcutsSidebarReservesLayoutWidth. That test measures m.View(), whose
// final lipgloss clamp to m.width hides any over-wide composition inside a
// string buffer — exactly the failure mode (#168) where a real terminal wraps
// the overflow back into the panes. This test instead reconstructs the
// body+sidebar JoinHorizontal BEFORE the final clamp and asserts it fits within
// m.width, so it catches reservation-math drift (e.g. forgetting the sidebar's
// rendered border columns, or a body path that ignores mainContentWidth()).
func TestShortcutsSidebarComposedWidthFitsTerminal(t *testing.T) {
	maxLineWidth := func(s string) int {
		mx := 0
		for _, ln := range strings.Split(s, "\n") {
			if w := lipgloss.Width(ln); w > mx {
				mx = w
			}
		}
		return mx
	}

	// composeWidth rebuilds the same body+sidebar join View() performs, before
	// the final full-screen clamp, for the currently-focused list/detail body.
	composeWidth := func(m *Model, showDetails bool) int {
		var body string
		if m.isSplitView {
			body = m.renderSplitView()
		} else if showDetails {
			body = m.viewport.View()
		} else {
			body = m.renderListWithHeader()
		}
		m.shortcutsSidebar.SetFocus(m.focused)
		m.shortcutsSidebar.SetSize(m.shortcutsSidebar.Width(), m.height-2)
		sidebar := m.shortcutsSidebar.View()
		return maxLineWidth(lipgloss.JoinHorizontal(lipgloss.Top, body, sidebar))
	}

	cases := []struct {
		name      string
		w, h      int
		wantSplit bool
	}{
		{"split_narrow_110", 110, 30, true},
		{"split_120", 120, 30, true},
		{"split_wide_200", 200, 40, true},
		{"mobile_80", 80, 30, false},
		{"mobile_60", 60, 24, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(40), tc.w, tc.h)
			if m.isSplitView != tc.wantSplit {
				t.Fatalf("w=%d isSplitView=%v want %v", tc.w, m.isSplitView, tc.wantSplit)
			}

			// Open the sidebar via the real `;` key path.
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
			m = updated.(*Model)
			if !m.showShortcutsSidebar {
				t.Fatalf("`;` did not enable the shortcuts sidebar")
			}

			// List/board body and (mobile) detail body must both fit once the
			// sidebar column is appended.
			if cw := composeWidth(m, false); cw > m.width {
				t.Errorf("list body + sidebar composed width %d exceeds terminal width %d (#168 overflow)", cw, m.width)
			}
			if !m.isSplitView {
				if cw := composeWidth(m, true); cw > m.width {
					t.Errorf("detail body + sidebar composed width %d exceeds terminal width %d (#168 overflow)", cw, m.width)
				}
			}
		})
	}
}

func TestShortcutsSidebarFullScreenViewsKeepSidebarVisible(t *testing.T) {
	maxLineWidth := func(s string) int {
		mx := 0
		for _, ln := range strings.Split(s, "\n") {
			if w := lipgloss.Width(ln); w > mx {
				mx = w
			}
		}
		return mx
	}

	cases := []struct {
		name  string
		setup func(*Model)
	}{
		{
			name: "board",
			setup: func(m *Model) {
				m.isBoardView = true
				m.focused = focusBoard
			},
		},
		{
			name: "insights",
			setup: func(m *Model) {
				m.focused = focusInsights
			},
		},
		{
			name: "history",
			setup: func(m *Model) {
				m.historyView = NewHistoryModel(createTestHistoryReport(), testTheme())
				m.isHistoryView = true
				m.focused = focusHistory
			},
		},
		{
			name: "tree",
			setup: func(m *Model) {
				m.tree.Build(m.repositoryIssues)
				m.focused = focusTree
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(40), 200, 40)
			m.showShortcutsSidebar = true
			tc.setup(m)
			m.applyContentSizing()

			view := m.View()
			if maxLineWidth(view) > m.width {
				t.Fatalf("%s view exceeds terminal width", tc.name)
			}
			if !strings.Contains(view, "Shortcuts") {
				t.Fatalf("%s view clipped the open shortcuts sidebar", tc.name)
			}

			wantWidth := m.mainContentWidth()
			switch tc.name {
			case "tree":
				treeBody := m.renderTreeBody()
				if got := maxLineWidth(treeBody); got != wantWidth {
					t.Fatalf("Tree width = %d, want reserved content width %d", got, wantWidth)
				}
			case "insights":
				if m.insightsPanel.width != wantWidth {
					t.Fatalf("Insights width = %d, want reserved content width %d", m.insightsPanel.width, wantWidth)
				}
			case "history":
				if m.historyView.width != wantWidth {
					t.Fatalf("History width = %d, want reserved content width %d", m.historyView.width, wantWidth)
				}
			}
		})
	}
}

func TestTimeTravelShortcutsSidebarComposition(t *testing.T) {
	maxLineWidth := func(s string) int {
		mx := 0
		for _, line := range strings.Split(s, "\n") {
			if width := lipgloss.Width(line); width > mx {
				mx = width
			}
		}
		return mx
	}

	for _, test := range []struct {
		name  string
		width int
	}{
		{name: "wide", width: 120},
		{name: "narrow", width: 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(2), test.width, 30)
			updated, _ := m.Update(keyMsg("t"))
			m = updated.(*Model)
			if !m.showTimeTravelPrompt || m.showShortcutsSidebar {
				t.Fatalf("opening time-travel prompt changed initial state: prompt=%v sidebar=%v", m.showTimeTravelPrompt, m.showShortcutsSidebar)
			}
			updated, _ = m.Update(keyMsg(";"))
			m = updated.(*Model)
			if !m.showTimeTravelPrompt || !m.showShortcutsSidebar {
				t.Fatalf("semicolon did not keep the time-travel prompt/sidebar state: prompt=%v sidebar=%v", m.showTimeTravelPrompt, m.showShortcutsSidebar)
			}
			if test.width == 80 {
				updated, _ = m.Update(keyMsg("HEAD~1"))
				m = updated.(*Model)
			}

			// Rebuild the same pre-clamp composition performed by View(). This catches
			// narrow prompt content overflowing before View() truncates it.
			m.shortcutsSidebar.SetFocus(m.focused)
			m.shortcutsSidebar.SetSize(m.shortcutsSidebar.Width(), m.height-2)
			composed := lipgloss.JoinHorizontal(lipgloss.Top, m.renderTimeTravelPrompt(), m.shortcutsSidebar.View())
			plainComposed := ansi.Strip(composed)
			if !strings.Contains(plainComposed, "Shortcuts") || !strings.Contains(plainComposed, "Navigation") {
				t.Fatalf("sidebar title/content missing from time-travel composition:\n%s", plainComposed)
			}
			if got := maxLineWidth(composed); got > m.width {
				t.Fatalf("time-travel/sidebar pre-clamp composition exceeds terminal width: got %d, want at most %d", got, m.width)
			}

			view := m.View()
			if !strings.Contains(ansi.Strip(view), "Navigation") {
				t.Fatal("time-travel prompt clipped the shortcuts sidebar content")
			}
		})
	}
}

func TestTreeSidebarBoundsLongProjectedRowsAtNormalWidths(t *testing.T) {
	maxLineWidth := func(s string) int {
		maxWidth := 0
		for _, line := range strings.Split(s, "\n") {
			if width := lipgloss.Width(line); width > maxWidth {
				maxWidth = width
			}
		}
		return maxWidth
	}

	width := 120
	issues := make([]model.Issue, 0, 40)
	for i := 0; i < 39; i++ {
		id := fmt.Sprintf("repository-item-with-a-realistically-long-id-%02d", i)
		issues = append(issues, model.Issue{
			ID: id, Title: "A title with enough realistic detail to exceed the Tree row width",
			Status: model.StatusOpen, Priority: 1, IssueType: model.TypeTask,
			Dependencies: []*model.Dependency{{IssueID: id, DependsOnID: "external-parent-with-a-long-id", Type: model.DepParentChild}},
		})
	}
	issues = append(issues, model.Issue{
		ID: "bottom-selected-item-with-a-realistically-long-id", Title: "Bottom selected issue with a long explanatory title",
		Status: model.StatusOpen, Priority: 2, IssueType: model.TypeTask,
		Dependencies: []*model.Dependency{{IssueID: "bottom-selected-item-with-a-realistically-long-id", DependsOnID: "external-parent-with-a-long-id", Type: model.DepParentChild}},
	})

	m := sizedModel(t, issues, width, 30)
	m.tree.BuildProjected(m.repositoryIssues, map[string]*model.Issue{
		"external-parent-with-a-long-id": {ID: "external-parent-with-a-long-id", Title: "External parent"},
	})
	m.focused = focusTree
	m.applyContentSizing()
	m.tree.JumpToBottom()
	selectedID := m.tree.GetSelectedID()
	if selectedID != "bottom-selected-item-with-a-realistically-long-id" {
		t.Fatalf("bottom selection = %q", selectedID)
	}
	if !strings.Contains(ansi.Strip(m.tree.View()), "[parent out of scope]") {
		t.Fatal("projected-parent marker missing from Tree rows")
	}

	assertTreeBody := func(m *Model) string {
		body := m.renderTreeBody()
		if got, want := len(strings.Split(body, "\n")), len(strings.Split(m.tree.View(), "\n")); got != want {
			t.Fatalf("Tree rows wrapped: got %d rendered lines, want %d", got, want)
		}
		for _, line := range strings.Split(body, "\n") {
			if lipgloss.Width(line) > m.mainContentWidth() {
				t.Fatalf("Tree line exceeds content width %d: %d", m.mainContentWidth(), lipgloss.Width(line))
			}
		}
		return body
	}
	assertTreeBody(m)

	updated, _ := m.Update(keyMsg(";"))
	m = updated.(*Model)
	if !m.showShortcutsSidebar {
		t.Fatal("semicolon did not open Tree sidebar")
	}
	after := assertTreeBody(m)
	plainAfter := ansi.Strip(after)
	if m.tree.GetSelectedID() != selectedID || !strings.Contains(plainAfter, "┃") || !strings.Contains(plainAfter, "bottom-selected-item") {
		t.Fatalf("bottom Tree selection changed or became invisible after toggle: selected=%q", m.tree.GetSelectedID())
	}

	m.shortcutsSidebar.SetFocus(m.focused)
	m.shortcutsSidebar.SetSize(m.shortcutsSidebar.Width(), m.height-2)
	composed := lipgloss.JoinHorizontal(lipgloss.Top, after, m.shortcutsSidebar.View())
	if got := maxLineWidth(composed); got != m.width {
		t.Fatalf("Tree/sidebar composition width = %d, want terminal width %d", got, m.width)
	}
	for _, line := range strings.Split(ansi.Strip(composed), "\n") {
		if index := strings.Index(line, "Shortcuts"); index >= 0 && index < m.mainContentWidth() {
			t.Fatalf("sidebar starts before reserved Tree width %d: %d", m.mainContentWidth(), index)
		}
	}

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(*Model)
	if m.showShortcutsSidebar || m.tree.GetSelectedID() != selectedID || !strings.Contains(ansi.Strip(assertTreeBody(m)), "┃") {
		t.Fatal("closing sidebar changed or hid bottom Tree selection")
	}
}

func TestFullScreenTreeAndTutorialLayoutAtNormalSize(t *testing.T) {
	lastLine := func(view string) string {
		lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
		return lines[len(lines)-1]
	}

	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "120x40", width: 120, height: 40},
		{name: "180x50", width: 180, height: 50},
	} {
		t.Run(size.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(2), size.width, size.height)
			updated, _ := m.Update(keyMsg("E"))
			m = updated.(*Model)
			treeView := m.View()
			if got := lipgloss.Height(treeView); got != size.height {
				t.Fatalf("Tree view height = %d, want %d", got, size.height)
			}
			if !strings.Contains(ansi.Strip(lastLine(treeView)), "issues") {
				t.Fatalf("global status line was not anchored to the last row in Tree view:\n%s", treeView)
			}

			updated, _ = m.Update(keyMsg(";"))
			m = updated.(*Model)
			updated, _ = m.Update(keyMsg("?"))
			m = updated.(*Model)
			updated, _ = m.Update(keyMsg(" "))
			m = updated.(*Model)
			tutorialView := m.View()
			if !m.showTutorial || !m.showShortcutsSidebar {
				t.Fatalf("Help-to-Tutorial did not preserve full-screen/sidebar state: tutorial=%v sidebar=%v", m.showTutorial, m.showShortcutsSidebar)
			}
			if got := lipgloss.Height(tutorialView); got != size.height {
				t.Fatalf("Tutorial view height = %d, want %d", got, size.height)
			}
			if !strings.Contains(ansi.Strip(lastLine(tutorialView)), "issues") {
				t.Fatalf("global status line was not anchored to the last row in Tutorial view:\n%s", tutorialView)
			}

			lines := strings.Split(strings.TrimRight(tutorialView, "\n"), "\n")
			if got := lipgloss.Width(lines[0]); got != size.width || !strings.HasSuffix(ansi.Strip(lines[0]), "╮") {
				t.Fatalf("Tutorial top border did not span the terminal width: width=%d want=%d line=%q", got, size.width, lines[0])
			}
		})
	}
}

func TestShortcutsSidebarKeepsLongIssueRowsSingleLine(t *testing.T) {
	issues := []model.Issue{
		{
			ID:        "repository-item-with-a-realistically-long-id-00",
			Title:     "A title with enough realistic detail to exceed the narrowed list row",
			Status:    model.StatusOpen,
			Priority:  1,
			IssueType: model.TypeTask,
			Comments:  []*model.Comment{{ID: "comment-1", Text: "A comment"}},
		},
	}
	m := sizedModel(t, issues, 220, 30)
	items := m.list.Items()
	item := items[0].(IssueItem)
	item.IsQuickWin = false
	item.IsBlocker = false
	item.UnblocksCount = 0
	item.DiffStatus = DiffStatusNew
	items[0] = item
	m.list.SetItems(items)
	m.updateListDelegate()
	countNonEmptyRows := func() int {
		count := 0
		for _, row := range strings.Split(ansi.Strip(m.list.View()), "\n") {
			if strings.TrimSpace(row) != "" {
				count++
			}
		}
		return count
	}
	if got := countNonEmptyRows(); got != 1 {
		t.Fatalf("long issue row wrapped before opening sidebar: got %d non-empty lines, want 1:\n%s", got, m.list.View())
	}
	updated, _ := m.Update(keyMsg(";"))
	m = updated.(*Model)
	if !m.showShortcutsSidebar {
		t.Fatal("semicolon did not open shortcuts sidebar")
	}
	if !strings.Contains(m.list.View(), "💬1") {
		t.Fatalf("comment count indicator disappeared from the narrowed row:\n%s", m.list.View())
	}
	if !strings.Contains(m.list.View(), "🆕") {
		t.Fatalf("diff badge disappeared from the narrowed row:\n%s", m.list.View())
	}

	rows := strings.Split(ansi.Strip(m.list.View()), "\n")
	if got := countNonEmptyRows(); got != 1 {
		t.Fatalf("long issue row wrapped after opening sidebar: got %d non-empty lines, want 1:\n%s", got, m.list.View())
	}
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			if width := lipgloss.Width(row); width > m.list.Width() {
				t.Fatalf("long issue row exceeds list width %d: got %d: %q", m.list.Width(), width, row)
			}
		}
	}
}

func TestUnderfilledNormalViewsAnchorGlobalStatuslineAtBottom(t *testing.T) {
	lastLine := func(view string) string {
		lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
		return lines[len(lines)-1]
	}

	cases := []struct {
		name        string
		withSidebar bool
		setup       func(*Model)
		want        string
		wantFirst   string
	}{
		{
			name: "actionable",
			setup: func(m *Model) {
				m.isActionableView = true
				m.focused = focusActionable
			},
			want:      "ACTIONABLE ITEMS",
			wantFirst: "ACTIONABLE ITEMS",
		},
		{
			name:        "actionable_with_shortcuts_sidebar",
			withSidebar: true,
			setup: func(m *Model) {
				m.isActionableView = true
				m.focused = focusActionable
			},
			want:      "ACTIONABLE ITEMS",
			wantFirst: "ACTIONABLE ITEMS",
		},
		{
			name: "sprint",
			setup: func(m *Model) {
				m.isSprintView = true
				m.focused = focusSprint
				m.sprintViewText = "Sprint content"
			},
			want: "Sprint content",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(2), 120, 30)
			tc.setup(m)
			m.showShortcutsSidebar = tc.withSidebar
			m.applyContentSizing()

			view := m.View()
			if got := lipgloss.Height(view); got != m.height {
				t.Fatalf("view height = %d, want %d", got, m.height)
			}
			if !strings.Contains(ansi.Strip(lastLine(view)), "issues") {
				t.Fatalf("global status line was not anchored to the last row:\n%s", view)
			}
			if tc.wantFirst != "" && !strings.Contains(ansi.Strip(strings.Split(view, "\n")[0]), tc.wantFirst) {
				t.Fatalf("view content %q did not start on the first row:\n%s", tc.wantFirst, view)
			}
			if !strings.Contains(ansi.Strip(view), tc.want) {
				t.Fatalf("view lost established content %q:\n%s", tc.want, view)
			}
			if tc.withSidebar && !strings.Contains(ansi.Strip(view), "Shortcuts") {
				t.Fatalf("shortcuts sidebar was not composed with the view:\n%s", view)
			}
		})
	}
}
