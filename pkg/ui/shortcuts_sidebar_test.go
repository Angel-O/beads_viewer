package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestNewShortcutsSidebar(t *testing.T) {
	theme := Theme{Renderer: lipgloss.DefaultRenderer()}
	sidebar := NewShortcutsSidebar(theme)

	if sidebar.width != 34 {
		t.Errorf("Expected width 34, got %d", sidebar.width)
	}
	if sidebar.context != "list" {
		t.Errorf("Expected context 'list', got %q", sidebar.context)
	}
}

func TestShortcutsSidebarSetContext(t *testing.T) {
	theme := Theme{Renderer: lipgloss.DefaultRenderer()}
	sidebar := NewShortcutsSidebar(theme)

	sidebar.SetContext("graph")
	if sidebar.context != "graph" {
		t.Errorf("Expected context 'graph', got %q", sidebar.context)
	}

	sidebar.SetContext("insights")
	if sidebar.context != "insights" {
		t.Errorf("Expected context 'insights', got %q", sidebar.context)
	}
}

func TestGraphShortcutsOmitHorizontalScroll(t *testing.T) {
	sidebar := NewShortcutsSidebar(Theme{Renderer: lipgloss.DefaultRenderer()})
	sidebar.SetContext("graph")
	for _, section := range sidebar.allSections() {
		if section.title != "Graph" {
			continue
		}
		for _, item := range section.items {
			if item.key == "H/L" || strings.Contains(item.desc, "Scroll ←/→") {
				t.Fatalf("Graph shortcuts advertise non-functional horizontal scrolling: %#v", item)
			}
		}
		return
	}
	t.Fatal("Graph shortcuts section not found")
}

func TestShortcutsSidebarScrolling(t *testing.T) {
	theme := Theme{Renderer: lipgloss.DefaultRenderer()}
	sidebar := NewShortcutsSidebar(theme)

	// Initial scroll offset should be 0
	if sidebar.scrollOffset != 0 {
		t.Errorf("Expected initial scroll 0, got %d", sidebar.scrollOffset)
	}

	// Scroll down
	sidebar.ScrollDown()
	if sidebar.scrollOffset != 1 {
		t.Errorf("Expected scroll 1 after ScrollDown, got %d", sidebar.scrollOffset)
	}

	// Scroll up
	sidebar.ScrollUp()
	if sidebar.scrollOffset != 0 {
		t.Errorf("Expected scroll 0 after ScrollUp, got %d", sidebar.scrollOffset)
	}

	// Scroll up at top should stay at 0
	sidebar.ScrollUp()
	if sidebar.scrollOffset != 0 {
		t.Errorf("Expected scroll 0 at top, got %d", sidebar.scrollOffset)
	}

	// Page down
	sidebar.ScrollPageDown()
	if sidebar.scrollOffset != 10 {
		t.Errorf("Expected scroll 10 after PageDown, got %d", sidebar.scrollOffset)
	}

	// Page up
	sidebar.ScrollPageUp()
	if sidebar.scrollOffset != 0 {
		t.Errorf("Expected scroll 0 after PageUp, got %d", sidebar.scrollOffset)
	}

	// Reset
	sidebar.scrollOffset = 5
	sidebar.ResetScroll()
	if sidebar.scrollOffset != 0 {
		t.Errorf("Expected scroll 0 after Reset, got %d", sidebar.scrollOffset)
	}
}

func TestShortcutsSidebarView(t *testing.T) {
	theme := Theme{
		Renderer:  lipgloss.DefaultRenderer(),
		Primary:   lipgloss.AdaptiveColor{Light: "#00ff00", Dark: "#00ff00"},
		Secondary: lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"},
		Base:      lipgloss.NewStyle(),
	}
	sidebar := NewShortcutsSidebar(theme)
	sidebar.SetSize(28, 30)

	view := sidebar.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}

	// Should contain title
	if !strings.Contains(view, "Shortcuts") {
		t.Error("Expected view to contain 'Shortcuts'")
	}

	// Should contain Navigation section
	if !strings.Contains(view, "Navigation") {
		t.Error("Expected view to contain 'Navigation'")
	}
}

func TestShortcutsSidebarContextFiltering(t *testing.T) {
	theme := Theme{
		Renderer:  lipgloss.DefaultRenderer(),
		Primary:   lipgloss.AdaptiveColor{Light: "#00ff00", Dark: "#00ff00"},
		Secondary: lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"},
		Base:      lipgloss.NewStyle(),
	}

	// Test graph context
	sidebar := NewShortcutsSidebar(theme)
	sidebar.SetSize(28, 50)
	sidebar.SetContext("graph")
	view := sidebar.View()

	if !strings.Contains(view, "Graph") {
		t.Error("Expected graph context to show Graph section")
	}

	// Test insights context
	sidebar.SetContext("insights")
	view = sidebar.View()

	if !strings.Contains(view, "Insights") {
		t.Error("Expected insights context to show Insights section")
	}
}

func TestShortcutsSidebarTreeDocumentsStatusAndAllActions(t *testing.T) {
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetSize(34, 40)
	sidebar.SetFocus(focusTree)

	view := sidebar.View()
	for _, expected := range []string{"+/-", "o/c/r", "E"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Tree sidebar missing %q: %s", expected, view)
		}
	}
	if strings.Contains(view, "Tab") {
		t.Fatal("Tree sidebar advertises Tab even though Tree does not own it")
	}
}

func TestContextFromFocus(t *testing.T) {
	tests := []struct {
		focus    focus
		expected string
	}{
		{focusList, "list"},
		{focusDetail, "detail"},
		{focusBoard, "board"},
		{focusGraph, "graph"},
		{focusInsights, "insights"},
		{focusHistory, "history"},
		{focusActionable, "actionable"},
		{focusLabelDashboard, "label"},
		{focusFlowMatrix, "flow"},
		{focusSprint, "sprint"},
		{focusAttention, "attention"},
		{focusHelp, "list"}, // Default fallback
	}

	for _, tt := range tests {
		got := ContextFromFocus(tt.focus)
		if got != tt.expected {
			t.Errorf("ContextFromFocus(%d) = %q, want %q", tt.focus, got, tt.expected)
		}
	}
}

func TestShortcutsSidebarWidth(t *testing.T) {
	theme := Theme{Renderer: lipgloss.DefaultRenderer()}
	sidebar := NewShortcutsSidebar(theme)

	if sidebar.Width() != 34 {
		t.Errorf("Expected Width() = 34, got %d", sidebar.Width())
	}
}

// TestShortcutsSidebar_MatchesRegistry verifies that sidebar uses registry bindings
// when available, falling back to hardcoded data when registry is empty (bv-xl6g).
func TestShortcutsSidebar_MatchesRegistry(t *testing.T) {
	theme := Theme{
		Renderer:  lipgloss.DefaultRenderer(),
		Primary:   lipgloss.AdaptiveColor{Light: "#00ff00", Dark: "#00ff00"},
		Secondary: lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"},
		Base:      lipgloss.NewStyle(),
	}

	t.Run("uses hardcoded when registry empty", func(t *testing.T) {
		sidebar := NewShortcutsSidebar(theme)
		sidebar.SetSize(34, 40)
		registry := NewKeyRegistry() // Empty registry
		sidebar.SetKeyRegistry(registry)
		sidebar.SetFocus(focusList)

		view := sidebar.View()
		// Should use hardcoded sections - expect Navigation
		if !strings.Contains(view, "Navigation") {
			t.Error("Expected hardcoded 'Navigation' section when registry empty")
		}
		sidebar.SetFocus(focusBoard)
		view = sidebar.View()
		if !strings.Contains(view, "Empty columns") {
			t.Error("Expected hardcoded Board section to document empty-column cycle")
		}
	})

	t.Run("uses registry when bindings exist", func(t *testing.T) {
		sidebar := NewShortcutsSidebar(theme)
		sidebar.SetSize(34, 40)
		registry := NewKeyRegistry()

		// Register test bindings with a unique category
		registry.RegisterBinding(KeyBinding{
			Focus:    focusList,
			Key:      "test-key",
			Desc:     "Test action",
			Category: "TestCategory",
		})

		sidebar.SetKeyRegistry(registry)
		sidebar.SetFocus(focusList)

		view := sidebar.View()
		// Should use registry bindings - expect TestCategory
		if !strings.Contains(view, "TestCategory") {
			t.Error("Expected registry 'TestCategory' section when bindings registered")
		}
		if !strings.Contains(view, "test-key") {
			t.Error("Expected 'test-key' from registry bindings")
		}
	})

	t.Run("SetFocus updates both focus and context", func(t *testing.T) {
		sidebar := NewShortcutsSidebar(theme)
		sidebar.SetFocus(focusGraph)

		if sidebar.focusHint != focusGraph {
			t.Errorf("Expected focusHint = focusGraph, got %v", sidebar.focusHint)
		}
		if sidebar.context != "graph" {
			t.Errorf("Expected context = 'graph', got %q", sidebar.context)
		}
	})
}

func TestShortcutsSidebarTreeCoverageIsCompact(t *testing.T) {
	theme := Theme{Renderer: lipgloss.DefaultRenderer(), Base: lipgloss.NewStyle()}
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(theme)
	sidebar.SetSize(34, 40)
	sidebar.SetKeyRegistry(registry)
	sidebar.SetFocus(focusTree)
	view := sidebar.View()

	for _, expected := range []string{"Search Tree", "Next search match", "Previous search match", "Toggle search scope", "Exit Tree", "Help overlay"} {
		if !strings.Contains(view, expected) {
			t.Errorf("Tree shortcuts sidebar missing %q", expected)
		}
	}
	if strings.Contains(view, "Self-update") || strings.Contains(view, "Copy issue ID") {
		t.Fatal("Tree shortcuts sidebar includes unrelated actions")
	}
	if strings.Contains(view, "Tab") {
		t.Fatal("registry-backed Tree sidebar advertises Tab")
	}
}

func TestShortcutsSidebarHidesListEnterInSplitView(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetSize(34, 40)
	sidebar.SetKeyRegistry(registry)
	sidebar.SetFocus(focusList)

	view := sidebar.View()
	if !strings.Contains(view, "enter") {
		t.Fatal("List sidebar omitted the registry-backed Enter binding")
	}
	for _, expected := range []string{"up", "down", "left", "right", "Move up", "Move down", "Previous page", "Next page"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("List sidebar omitted active arrow binding %q: %s", expected, view)
		}
	}

	sidebar.SetSplitView(true)
	view = sidebar.View()
	if strings.Contains(view, "enter") {
		t.Fatal("Split List sidebar advertises Enter even though it is a no-op")
	}
}

func TestShortcutsSidebarShowsOnlyActiveScrollControl(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetSize(34, 60)
	sidebar.SetKeyRegistry(registry)

	for _, focus := range []focus{focusBoard, focusInsights} {
		sidebar.SetFocus(focus)
		sections := sidebar.sectionsFromRegistry()
		var hasSidebarScroll bool
		for _, section := range sections {
			for _, item := range section.items {
				if item.key == "ctrl+j/k" && item.desc == "Scroll sidebar" {
					hasSidebarScroll = true
				}
				if item.key == "ctrl+j" || item.key == "ctrl+k" || strings.Contains(item.desc, "Scroll detail") {
					t.Fatalf("%v sidebar retains an underlying detail-scroll binding: %#v", focus, item)
				}
			}
		}
		if !hasSidebarScroll {
			t.Fatalf("%v sidebar lacks its active ctrl+j/k scroll binding", focus)
		}
	}

	sidebar.SetFocus(focusList)
	if view := sidebar.View(); !strings.Contains(view, "ctrl+j/k scroll") {
		t.Fatalf("sidebar footer does not identify its actual scroll keys: %q", view)
	}
}

func TestShortcutsSidebarShowsDedicatedScopeAndBacklogBindings(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetSize(34, 60)
	sidebar.SetKeyRegistry(registry)

	sidebar.SetFocus(focusScopePicker)
	scopeView := sidebar.View()
	for _, expected := range []string{"enter", "Toggle active scope"} {
		if !strings.Contains(scopeView, expected) {
			t.Fatalf("scope sidebar missing %q:\n%s", expected, scopeView)
		}
	}
	if strings.Contains(scopeView, "Move selected") {
		t.Fatalf("scope sidebar retained move action:\n%s", scopeView)
	}

	sidebar.SetFocus(focusBacklog)
	backlogView := sidebar.View()
	for _, expected := range []string{"n", "Next backlog page", "p", "Previous backlog page", "A", "Add to scope"} {
		if !strings.Contains(backlogView, expected) {
			t.Fatalf("backlog sidebar missing %q:\n%s", expected, backlogView)
		}
	}
}

func TestShortcutsSidebarAttentionUsesRegistryNavigation(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetSize(34, 60)
	sidebar.SetKeyRegistry(registry)
	sidebar.SetFocus(focusAttention)

	view := sidebar.View()
	for _, expected := range []string{"j", "k", "up", "down", "home", "G", "Filter List by", "Close Attention", "esc / q", "Help overlay", "Shortcuts sidebar"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Attention registry sidebar missing %q:\n%s", expected, view)
		}
	}
}

func TestShortcutsSidebarOnlyShowsSplitTabForListAndDetail(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetSize(34, 60)
	sidebar.SetKeyRegistry(registry)

	for _, focus := range []focus{focusList, focusDetail, focusTree} {
		sidebar.SetFocus(focus)
		sidebar.SetSplitView(false)
		for _, section := range sidebar.sectionsFromRegistry() {
			for _, item := range section.items {
				if item.key == "tab" || item.key == "<" || item.key == ">" {
					t.Fatalf("normal %v sidebar advertises Split-only binding: %#v", focus, item)
				}
			}
		}
	}

	for _, focus := range []focus{focusList, focusDetail} {
		sidebar.SetFocus(focus)
		sidebar.SetSplitView(true)
		found := map[string]bool{}
		for _, section := range sidebar.sectionsFromRegistry() {
			for _, item := range section.items {
				if item.desc == "Switch panes in Split" || item.key == "<" || item.key == ">" {
					found[item.key] = true
				}
			}
		}
		for _, key := range []string{"tab", "<", ">"} {
			if !found[key] {
				t.Fatalf("Split %v sidebar lacks pane-switch binding %q", focus, key)
			}
		}
	}
}

func TestShortcutsSidebarDetailOmitsRestrictedCommands(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetKeyRegistry(registry)
	sidebar.SetFocus(focusDetail)

	for _, section := range sidebar.sectionsFromRegistry() {
		for _, item := range section.items {
			if item.key == "p" || item.key == "!" || item.desc == "Priority hints" || item.desc == "Alerts panel" {
				t.Fatalf("Detail sidebar advertises restricted command: %#v", item)
			}
		}
	}
}

func TestShortcutsSidebarGroupsGlobalAliases(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetKeyRegistry(registry)
	sidebar.SetFocus(focusList)

	var global []shortcutItem
	for _, section := range sidebar.sectionsFromRegistry() {
		if section.title == "Global" || section.title == "Actions" {
			global = append(global, section.items...)
		}
	}
	for _, alias := range []string{"F2", ";", "ctrl+r", "f5"} {
		for _, item := range global {
			if item.key == alias {
				t.Fatalf("sidebar renders ungrouped alias %q: %#v", alias, item)
			}
		}
	}
	for _, want := range []string{"F2/;", "Ctrl+R/F5"} {
		found := false
		for _, item := range global {
			if item.key == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("sidebar missing grouped alias %q: %#v", want, global)
		}
	}
}

func TestShortcutsSidebarGraphAliasesAreGrouped(t *testing.T) {
	registry := NewKeyRegistry()
	m := Model{keyRegistry: registry}
	m.registerKeyBindings()
	sidebar := NewShortcutsSidebar(testTheme())
	sidebar.SetKeyRegistry(registry)
	sidebar.SetFocus(focusGraph)

	var graphItems []shortcutItem
	for _, section := range sidebar.sectionsFromRegistry() {
		if section.title == "Graph" {
			graphItems = append(graphItems, section.items...)
		}
	}
	for _, alias := range []string{"pgup", "pgdown", "PgUp", "PgDn", "Esc", "esc"} {
		if alias == "esc" {
			continue
		}
		for _, item := range graphItems {
			if item.key == alias {
				t.Fatalf("Graph sidebar renders ungrouped physical alias %q: %#v", alias, item)
			}
		}
	}
	var grouped int
	var esc int
	for _, item := range graphItems {
		if item.key == "pgup/pgdown" {
			grouped++
		}
		if item.key == "esc" {
			esc++
		}
	}
	if grouped != 1 {
		t.Fatalf("Graph sidebar has %d grouped page-scroll rows, want 1: %#v", grouped, graphItems)
	}
	if esc != 1 {
		t.Fatalf("Graph sidebar has %d lowercase escape rows, want 1: %#v", esc, graphItems)
	}
}

// TestShortcutsSidebarReservesLayoutWidth is the regression test for issue #168:
// toggling the shortcuts sidebar (`;`) must reserve its own fixed-width column so
// the main list/detail panes reflow into the remaining width. Previously the body
// was rendered at the full terminal width and the sidebar was appended after it,
// producing a composed layout wider than the terminal — which a real terminal
// then wraps back into the panes, interleaving the sidebar with issue rows.
//
// It is driven through the real key path + full View() so it catches any future
// drift between the body sizing and the sidebar width. The invariant asserted is
// layout-level (no rendered line exceeds m.width) and so does not depend on a
// live TTY.
func TestShortcutsSidebarReservesLayoutWidth(t *testing.T) {
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
		name      string
		w, h      int
		wantSplit bool
	}{
		{"split_narrow_110", 110, 30, true},
		{"split_120", 120, 30, true},
		{"split_wide_200", 200, 40, true},
		{"mobile_80", 80, 30, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(40), tc.w, tc.h)
			if m.isSplitView != tc.wantSplit {
				t.Fatalf("w=%d isSplitView=%v want %v", tc.w, m.isSplitView, tc.wantSplit)
			}

			// Before toggling, the layout must already fit (baseline).
			if mw := maxLineWidth(m.View()); mw > m.width {
				t.Fatalf("baseline (no sidebar): max line width %d > terminal width %d", mw, m.width)
			}

			// Toggle the sidebar on via the real `;` key path.
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
			m = updated.(*Model)
			if !m.showShortcutsSidebar {
				t.Fatalf("`;` did not enable the shortcuts sidebar")
			}

			view := m.View()
			if mw := maxLineWidth(view); mw > m.width {
				t.Errorf("with sidebar open: max line width %d exceeds terminal width %d (#168 overflow)", mw, m.width)
			}

			// The reflow must keep the sidebar content actually visible rather
			// than clipping it off the right edge: the body should have shrunk by
			// at least the sidebar's reserved column.
			wantBodyWidth := m.width - m.shortcutsSidebar.Width()
			if m.isSplitView {
				// list inner width + 2 (the panel border) is the list
				// panel; it must be well within the reserved body width.
				if m.list.Width()+2 > wantBodyWidth {
					t.Errorf("list panel width %d does not leave room for sidebar (reserved body %d)", m.list.Width()+2, wantBodyWidth)
				}
			} else if m.list.Width() > wantBodyWidth {
				t.Errorf("mobile list width %d exceeds reserved body width %d", m.list.Width(), wantBodyWidth)
			}

			// Toggling off restores the full-width layout.
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
			m = updated.(*Model)
			if m.showShortcutsSidebar {
				t.Fatalf("`;` did not disable the shortcuts sidebar")
			}
			if mw := maxLineWidth(m.View()); mw > m.width {
				t.Errorf("after closing sidebar: max line width %d exceeds terminal width %d", mw, m.width)
			}
		})
	}
}
