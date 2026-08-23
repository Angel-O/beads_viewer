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
	composeWidth := func(m Model, showDetails bool) int {
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
			m = updated.(Model)
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
			tc.setup(&m)
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

	assertTreeBody := func(m Model) string {
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
	m = updated.(Model)
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
	m = updated.(Model)
	if m.showShortcutsSidebar || m.tree.GetSelectedID() != selectedID || !strings.Contains(ansi.Strip(assertTreeBody(m)), "┃") {
		t.Fatal("closing sidebar changed or hid bottom Tree selection")
	}
}
