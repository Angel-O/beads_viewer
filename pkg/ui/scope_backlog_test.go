package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestScopeFirstViewShowsNoActiveStateAndOpensChooser(t *testing.T) {
	loads := 0
	activations := 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load: func(context.Context) (ScopeSnapshot, error) {
			loads++
			return ScopeSnapshot{Scopes: []ScopeInfo{{ID: "s1", Name: "Today", MemberCount: 2}}}, nil
		},
		Activate: func(context.Context, string) error { activations++; return nil },
	}})
	updated, _ := m.Update(scopeSnapshotMsg{snapshot: ScopeSnapshot{
		Scopes: []ScopeInfo{{ID: "s1", Name: "Today", MemberCount: 2}},
	}})
	m = updated.(*Model)
	view := m.View()
	if !containsText(view, "No active scope — press W to choose or create a scope, or B for the global backlog.") {
		t.Fatalf("View() = %q, want compact no-active guidance", view)
	}
	if containsText(view, "No items") || !containsText(view, "TY") {
		t.Fatalf("View() = %q, want the underlying List context", view)
	}
	if strings.Count(ansi.Strip(view), "No active scope") != 1 || strings.Contains(ansi.Strip(m.renderFooter()), "press W to choose") {
		t.Fatalf("View() = %q, rendered guidance outside the List content", view)
	}
	if containsText(view, "Press W to choose a named scope") {
		t.Fatalf("View() = %q, retained the masking overlay guidance", view)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	m = updated.(*Model)
	if !m.showScopePicker || m.focused != focusScopePicker || cmd == nil {
		t.Fatalf("W opened picker=%t focus=%v cmd=%t", m.showScopePicker, m.focused, cmd != nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if loads != 1 || !containsText(m.scopePicker.View(), "Today") {
		t.Fatalf("scope load count=%d picker=%q", loads, m.scopePicker.View())
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("enter did not activate the selected scope")
	}
	updated, _ = m.Update(cmd())
	if activations != 1 {
		t.Fatalf("activations=%d, want 1", activations)
	}
}

func TestScopeRenderersStayWithinAssignedViewport(t *testing.T) {
	maxLineWidth := func(view string) int {
		maxWidth := 0
		for _, line := range strings.Split(view, "\n") {
			if width := lipgloss.Width(line); width > maxWidth {
				maxWidth = width
			}
		}
		return maxWidth
	}

	for _, width := range []int{80, 160} {
		t.Run("width-"+fmt.Sprint(width), func(t *testing.T) {
			picker := NewScopePickerModel(testTheme())
			picker.SetSize(width-36, 23)
			picker.SetScopes([]ScopeInfo{{Name: "Today", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 2, Active: true}})
			if got := maxLineWidth(picker.View()); got > width-36 {
				t.Fatalf("scope picker width = %d, want <= %d", got, width-36)
			}

			m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
				Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
			}})
			m.width, m.height, m.showShortcutsSidebar = width, 24, true
			if got, want := maxLineWidth(m.renderNoActiveScope(m.mainContentWidth())), m.mainContentWidth(); got > want {
				t.Fatalf("no-active scope width = %d, want <= %d", got, want)
			}
			guidance := m.renderNoActiveScope(m.mainContentWidth())
			guidance = strings.Join(strings.Fields(guidance), " ")
			if !containsText(guidance, "No active scope") || !containsText(guidance, "global backlog") {
				t.Fatal("no-active scope guidance was not rendered")
			}
		})
	}
}

func TestNoActiveScopeKeepsDetailContextVisible(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{
		InitialScope: &ScopeSnapshot{},
		Scopes: ScopeServices{Load: func(context.Context) (ScopeSnapshot, error) {
			return ScopeSnapshot{}, nil
		}},
	})
	m.showDetails = true
	m.updateViewportContent()

	view := ansi.Strip(m.View())
	for _, want := range []string{"No active scope", "press W to choose or create a scope", "B for the global backlog"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "No issues selected") || strings.Contains(ansi.Strip(m.renderFooter()), "press W to choose") {
		t.Fatalf("detail view left the old empty string or bottom guidance:\n%s", view)
	}
	if strings.Contains(view, "Press W to choose a named scope") {
		t.Fatalf("detail view retained the masking overlay guidance:\n%s", view)
	}

	m.showDetails = false
	m.isSplitView = true
	m.applyContentSizing()
	view = ansi.Strip(m.View())
	for _, want := range []string{"TY", "No active scope", "global backlog"} {
		if !strings.Contains(view, want) {
			t.Fatalf("split view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "No items") || strings.Contains(view, "No issues selected") {
		t.Fatalf("split view retained an old empty string:\n%s", view)
	}
	if strings.Count(view, "No active scope") != 2 || strings.Contains(ansi.Strip(m.renderFooter()), "press W to choose") {
		t.Fatalf("split view did not keep one message per panel:\n%s", view)
	}

	normal := NewModel(nil, nil, "")
	normal.updateViewportContent()
	normalView := ansi.Strip(normal.View())
	if !strings.Contains(normalView, "No items") || strings.Contains(normalView, "No active scope") {
		t.Fatalf("normal List empty state changed: %s", normalView)
	}
	normal.showDetails = true
	normal.updateViewportContent()
	normalView = ansi.Strip(normal.View())
	if !strings.Contains(normalView, "No issues selected") || strings.Contains(normalView, "No active scope") {
		t.Fatalf("normal Detail empty state changed: %s", normalView)
	}
}

func TestNoActiveScopeKeepsWrappedListAndDetailPanelsBounded(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{
		InitialScope: &ScopeSnapshot{},
		Scopes:       ScopeServices{Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil }},
	})
	m.width, m.height = 80, 12
	m.applyContentSizing()

	listView := ansi.Strip(m.renderListWithHeader())
	if !strings.Contains(listView, "No active scope") || strings.Contains(listView, "No items") || !strings.Contains(listView, "Page 1 of 1") {
		t.Fatalf("wrapped List empty state lost content:\n%s", listView)
	}
	for _, line := range strings.Split(listView, "\n") {
		if width := lipgloss.Width(line); width > m.mainContentWidth() {
			t.Fatalf("wrapped List line width = %d, want <= %d: %q", width, m.mainContentWidth(), line)
		}
	}

	m.isSplitView = true
	m.height = 16
	m.applyContentSizing()
	splitView := ansi.Strip(m.renderSplitView())
	if strings.Contains(splitView, "No items") || strings.Contains(splitView, "No issues selected") || !strings.Contains(splitView, "Page 1/1") {
		t.Fatalf("split empty state lost panel content:\n%s", splitView)
	}
	if strings.Count(splitView, "No active scope") != 2 {
		t.Fatalf("split view did not render one guidance message per panel:\n%s", splitView)
	}
	for _, line := range strings.Split(splitView, "\n") {
		if width := lipgloss.Width(line); width > m.mainContentWidth() {
			t.Fatalf("Detail panel overflowed assigned terminal width: %d > %d: %q", width, m.mainContentWidth(), line)
		}
	}
}

func TestHubNoActiveInitialRenderSkipsLoadingProjectionPath(t *testing.T) {
	m := NewModel(nil, nil, "/hub/.beads/issues.jsonl", RuntimeServices{
		RepositoryPresentation: true,
		InitialScope:           &ScopeSnapshot{},
		Scopes: ScopeServices{
			Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
		},
	})
	defer m.Stop()

	view := m.View()
	if !containsText(view, "No active scope") || containsText(view, "Loading beads") || containsText(view, "issues.jsonl") {
		t.Fatalf("initial Hub no-scope view = %q", view)
	}
}

func TestHubActiveEmptyInitialScopeIsNotNoActive(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{
		InitialScope: &ScopeSnapshot{Active: &ScopeInfo{ID: "empty", Name: "Empty", Active: true}},
		Scopes:       ScopeServices{Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil }},
	})
	defer m.Stop()
	if containsText(m.View(), "No active scope") {
		t.Fatal("active empty scope rendered as no active scope")
	}
}

func TestFirstScopeCreationStartsFromNamedScopesWithoutIssueSelection(t *testing.T) {
	var createdName string
	activations := 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load:     func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
		Create:   func(_ context.Context, name string) error { createdName = name; return nil },
		Activate: func(context.Context, string) error { activations++; return nil },
	}})

	updated, cmd := m.Update(keyMsg("W"))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("W did not open the named-scopes view")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg("n"))
	m = updated.(*Model)
	if !m.showScopeCreatePrompt || m.focused != focusScopeCreateInput || m.statusMsg == "No issue selected" {
		t.Fatalf("n did not open name prompt: prompt=%t focus=%s status=%q", m.showScopeCreatePrompt, m.focused, m.statusMsg)
	}
	for _, r := range "First scope" {
		updated, _ = m.Update(keyMsg(string(r)))
		m = updated.(*Model)
	}
	updated, cmd = m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd == nil || m.showScopeCreatePrompt || !m.showScopePicker || m.activeScope != nil {
		t.Fatalf("scope creation state: cmd=%t prompt=%t picker=%t active=%#v", cmd != nil, m.showScopeCreatePrompt, m.showScopePicker, m.activeScope)
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if createdName != "First scope" || activations != 0 {
		t.Fatalf("created name=%q activations=%d, want name-only inactive creation", createdName, activations)
	}
}

func TestScopeCreationValidationCancelAndFailureKeepPickerUsable(t *testing.T) {
	creates := 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Create: func(context.Context, string) error { creates++; return errors.New("backend unavailable") },
	}})
	m.showScopePicker = true
	m.focused = focusScopePicker

	updated, _ := m.Update(keyMsg("n"))
	m = updated.(*Model)
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd != nil || !m.showScopeCreatePrompt || !m.statusIsError || m.statusMsg != "Scope name cannot be empty" {
		t.Fatalf("empty name: cmd=%t prompt=%t error=%t status=%q", cmd != nil, m.showScopeCreatePrompt, m.statusIsError, m.statusMsg)
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(*Model)
	if m.showScopeCreatePrompt || !m.showScopePicker || m.focused != focusScopePicker {
		t.Fatalf("cancel changed picker state: prompt=%t picker=%t focus=%s", m.showScopeCreatePrompt, m.showScopePicker, m.focused)
	}

	updated, _ = m.Update(keyMsg("n"))
	m = updated.(*Model)
	m.scopeCreateInput.SetValue("Retry scope")
	updated, cmd = m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("valid name did not start creation")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if creates != 1 || !m.showScopePicker || m.statusMsg != "Scope create failed: backend unavailable" || !m.statusIsError {
		t.Fatalf("backend failure: creates=%d picker=%t status=%q error=%t", creates, m.showScopePicker, m.statusMsg, m.statusIsError)
	}
}

func TestScopePickerWTogglesToItsPriorView(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
	}})
	m.focused = focusDetail

	updated, _ := m.Update(keyMsg("W"))
	m = updated.(*Model)
	if !m.showScopePicker || m.focused != focusScopePicker {
		t.Fatalf("W did not open picker: shown=%t focus=%s", m.showScopePicker, m.focused)
	}
	updated, _ = m.Update(keyMsg("W"))
	m = updated.(*Model)
	if m.showScopePicker || m.focused != focusDetail || m.scopePickerMoveIssue != "" {
		t.Fatalf("second W left stale picker state: shown=%t focus=%s move=%q", m.showScopePicker, m.focused, m.scopePickerMoveIssue)
	}

	m.isBacklogView = true
	m.focused = focusBacklog
	m.openScopePicker("")
	updated, _ = m.Update(keyMsg("W"))
	m = updated.(*Model)
	if m.showScopePicker || !m.isBacklogView || m.focused != focusBacklog {
		t.Fatalf("W did not restore backlog origin: shown=%t backlog=%t focus=%s", m.showScopePicker, m.isBacklogView, m.focused)
	}
}

func TestScopePickerLowercaseWOpensContextPickerAndReturns(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = testRepositoryCatalog()
	m.showScopePicker = true
	m.focused = focusScopePicker

	updated, _ := m.Update(keyMsg("w"))
	m = updated.(*Model)
	if !m.showRepoPicker || m.focused != focusRepoPicker || m.repoPickerOrigin != focusScopePicker || !m.showScopePicker {
		t.Fatalf("scope lowercase w did not open Context picker: repo=%t focus=%s origin=%s scope=%t", m.showRepoPicker, m.focused, m.repoPickerOrigin, m.showScopePicker)
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(*Model)
	if m.showRepoPicker || m.focused != focusScopePicker || !m.showScopePicker {
		t.Fatalf("Context picker did not return to scope view: repo=%t focus=%s scope=%t", m.showRepoPicker, m.focused, m.showScopePicker)
	}
}

func TestContextPickerApplyFromScopeRestoresScopeFocus(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = testRepositoryCatalog()
	m.showScopePicker = true
	m.focused = focusScopePicker

	updated, _ := m.Update(keyMsg("w"))
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(*Model)

	if m.showRepoPicker || !m.showScopePicker || m.focused != focusScopePicker {
		t.Fatalf("Context apply left incorrect scope state: repo=%t scope=%t focus=%s", m.showRepoPicker, m.showScopePicker, m.focused)
	}
}

func TestScopePickerFooterIsIndependentOfEntryView(t *testing.T) {
	var want string
	for _, origin := range []focus{focusBoard, focusList, focusDetail} {
		m := NewModel(nil, nil, "")
		m.width, m.height = 240, 40
		m.focused = origin
		m.isBoardView = origin == focusBoard
		m.openScopePicker("")

		footer := ansi.Strip(m.renderFooter())
		if strings.Contains(footer, "1-4:col") {
			t.Fatalf("scope footer inherited Board column hint from %s: %q", origin, footer)
		}
		if !strings.Contains(footer, "enter activate") || strings.Contains(footer, "m move") {
			t.Fatalf("scope footer lost scope controls from %s: %q", origin, footer)
		}
		if want == "" {
			want = footer
		} else if footer != want {
			t.Fatalf("scope footer changed with entry origin %s:\nwant %q\n got %q", origin, want, footer)
		}
	}

	board := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}, nil, "")
	board.width, board.height, board.isBoardView = 240, 40, true
	boardFooter := ansi.Strip(board.renderFooter())
	for _, hint := range []string{"o/c/r:filter", "/:search"} {
		if !strings.Contains(boardFooter, hint) {
			t.Fatalf("ordinary Board footer lost supported hint %q: %q", hint, boardFooter)
		}
	}
}

func TestScopeAndBacklogHelpDocumentsSupportedControls(t *testing.T) {
	for _, tc := range []struct {
		name  string
		focus focus
		wants []string
	}{
		{name: "scopes", focus: focusScopePicker, wants: []string{"Scopes", "Enter", "Activate scope", "n", "Create inactive named scope", "B", "global backlog"}},
		{name: "backlog", focus: focusBacklog, wants: []string{"Backlog", "n/p", "Next / previous page", "/", "Filter backlog", "A", "Add selected bead to scope"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(nil, nil, "")
			m.width, m.height, m.focused = 240, 40, tc.focus
			help := ansi.Strip(m.renderHelpOverlay())
			for _, want := range tc.wants {
				if !strings.Contains(help, want) {
					t.Errorf("%s help missing %q:\n%s", tc.name, want, help)
				}
			}
			if strings.Contains(help, "1-4") {
				t.Fatalf("%s help retained obsolete column shortcut:\n%s", tc.name, help)
			}
			if tc.focus == focusScopePicker && strings.Contains(help, "Move selected bead") {
				t.Fatalf("scope help retained move action:\n%s", help)
			}
		})
	}
}

func TestGenericHelpShowsScopesWorkflowWithAccurateContexts(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.focused = 240, 40, focusList
	updated, _ := m.Update(keyMsg("?"))
	m = updated.(*Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("? did not open generic help: help=%t focus=%s", m.showHelp, m.focused)
	}
	help := ansi.Strip(m.renderHelpOverlay())
	for _, want := range []string{
		"Scopes",
		"Named scopes (List/Detail)",
		"Global backlog (List/Detail)",
		"New inactive named scope (Scopes)",
		"Activate scope (Scopes)",
		"Add to active scope (L/D/B)",
		"Remove from active scope (L/D)",
		"Move bead to another scope (L/D)",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("generic help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "nAdd comment") {
		t.Fatal("generic Scopes card misstates List n as scope creation")
	}
}

func TestGenericScopesHelpFitsRepresentativeWidths(t *testing.T) {
	for _, width := range []int{60, 80} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			m := NewModel(nil, nil, "")
			m.width, m.height, m.focused = width, 40, focusList
			updated, _ := m.Update(keyMsg("?"))
			m = updated.(*Model)
			view := ansi.Strip(m.View())
			for _, want := range []string{"Scopes", "W         Named scopes", "B         Global backlog", "n         New inactive"} {
				if !strings.Contains(view, want) {
					t.Fatalf("clipped generic help missing %q:\n%s", want, view)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("help line width %d exceeds terminal width %d: %q", got, width, line)
				}
			}
		})
	}
}

func TestGenericScopesHelpBalancesWideColumns(t *testing.T) {
	const width = 200
	m := NewModel(nil, nil, "")
	m.width, m.height, m.focused = width, 60, focusList
	updated, _ := m.Update(keyMsg("?"))
	m = updated.(*Model)
	view := ansi.Strip(m.View())

	for _, entry := range []struct{ key, desc string }{
		{"W", "Named scopes (List/Detail)"},
		{"B", "Global backlog (List/Detail)"},
		{"n", "New inactive named scope (Scopes)"},
		{"Enter", "Activate scope (Scopes)"},
		{"A", "Add to active scope (L/D/B)"},
		{"R", "Remove from active scope (L/D)"},
		{"m", "Move bead to another scope (L/D)"},
		{"s", "Cycle sort (Hub)"},
		{"x", "Export .md"},
		{"C", "Copy issue (List/Detail/Split)"},
	} {
		if !strings.Contains(view, fmt.Sprintf("%-10s%s", entry.key, entry.desc)) {
			t.Fatalf("wide generic help wrapped or lost Scopes entry %q: \n%s", entry.key, view)
		}
	}

	headings := []string{"◉ Scopes", "📊 Graph View", "🩺 Status", "📜 History", "🧭 Navigation", "💡 Insights", "⚡ List / Detail", "👁 Views", "🌐 Global", "🔍 List Filters & Sort"}
	columnCounts := make(map[int]int)
	for _, heading := range headings {
		found := false
		for _, line := range strings.Split(view, "\n") {
			if index := strings.Index(line, heading); index >= 0 {
				columnCounts[lipgloss.Width(line[:index])/(width/3)]++
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("wide generic help missing panel heading %q", heading)
		}
	}
	counts := make([]int, 0, len(columnCounts))
	for _, count := range columnCounts {
		counts = append(counts, count)
	}
	sort.Ints(counts)
	if fmt.Sprint(counts) != "[3 3 4]" {
		t.Fatalf("generic help columns are unbalanced: counts=%v", counts)
	}
}

func TestScopePickerBUsesGlobalBacklogJump(t *testing.T) {
	loads := 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadBacklog: func(context.Context, string, int) (BacklogPage, error) {
			loads++
			return BacklogPage{}, nil
		},
	}})
	m.showScopePicker = true
	m.scopePickerOrigin = focusList
	m.focused = focusScopePicker

	updated, cmd := m.Update(keyMsg("B"))
	m = updated.(*Model)
	if m.showScopePicker || !m.isBacklogView || m.focused != focusBacklog || cmd == nil {
		t.Fatalf("B did not use the global backlog jump: picker=%t backlog=%t focus=%s cmd=%t", m.showScopePicker, m.isBacklogView, m.focused, cmd != nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if loads != 1 {
		t.Fatalf("backlog loads=%d, want 1", loads)
	}

	updated, _ = m.Update(keyMsg("B"))
	m = updated.(*Model)
	if m.isBacklogView || m.focused != focusList {
		t.Fatalf("backlog-local B did not close the backlog: backlog=%t focus=%s", m.isBacklogView, m.focused)
	}

	m.isBacklogView = true
	m.focused = focusBacklog
	m.backlog.BeginSearch()
	updated, _ = m.Update(keyMsg("B"))
	m = updated.(*Model)
	if !m.backlog.Searching() || m.backlog.Filter() != "B" || !m.isBacklogView || m.focused != focusBacklog {
		t.Fatalf("backlog search lost B ownership: searching=%t filter=%q backlog=%t focus=%s", m.backlog.Searching(), m.backlog.Filter(), m.isBacklogView, m.focused)
	}
}

func TestScopeBacklogViewJumpsCloseOverlayAndNavigate(t *testing.T) {
	issues := []model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}
	keys := []struct {
		key   string
		focus focus
	}{
		{key: "b", focus: focusBoard},
		{key: "E", focus: focusTree},
		{key: "g", focus: focusGraph},
		{key: "[", focus: focusLabelDashboard},
		{key: "]", focus: focusAttention},
	}
	for _, tc := range keys {
		for _, origin := range []focus{focusScopePicker, focusBacklog} {
			t.Run(tc.key+"/"+origin.String(), func(t *testing.T) {
				m := NewModel(issues, nil, "")
				m.width, m.height, m.ready = 120, 30, true
				if origin == focusScopePicker {
					m.showScopePicker = true
					m.scopePickerOrigin = focusList
				} else {
					m.isBacklogView = true
				}
				m.focused = origin

				updated, _ := m.Update(keyMsg(tc.key))
				m = updated.(*Model)
				if m.showScopePicker || m.isBacklogView || m.focused != tc.focus {
					t.Fatalf("key %q from %s left overlay/view state: picker=%t backlog=%t focus=%s", tc.key, origin, m.showScopePicker, m.isBacklogView, m.focused)
				}
			})
		}
	}
}

func TestBacklogSearchKeepsViewJumpKeysAsQueryText(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadBacklog: func(context.Context, string, int) (BacklogPage, error) { return BacklogPage{}, nil },
	}})
	m.isBacklogView = true
	m.focused = focusBacklog
	m.backlog.BeginSearch()

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(*Model)
	if !m.backlog.Searching() || m.backlog.Filter() != "b" || !m.isBacklogView || m.focused != focusBacklog {
		t.Fatalf("active backlog search lost key ownership: searching=%t filter=%q backlog=%t focus=%s", m.backlog.Searching(), m.backlog.Filter(), m.isBacklogView, m.focused)
	}
}

func TestBacklogUsesOpaqueCursorAndResetsOnFilterChange(t *testing.T) {
	var cursors []string
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadBacklog: func(_ context.Context, cursor string, limit int) (BacklogPage, error) {
			if limit != backlogPageSize {
				t.Fatalf("limit=%d, want %d", limit, backlogPageSize)
			}
			cursors = append(cursors, cursor)
			return BacklogPage{Issues: []model.Issue{{ID: "b-1", Title: "Backlog", Status: model.StatusOpen}}, HasMore: cursor == "", NextCursor: "opaque-next"}, nil
		},
	}})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if !m.isBacklogView || len(cursors) != 1 || cursors[0] != "" {
		t.Fatalf("backlog open state=%t cursors=%v", m.isBacklogView, cursors)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if len(cursors) != 2 || cursors[1] != "opaque-next" {
		t.Fatalf("next page cursors=%v", cursors)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(*Model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("filter change did not reload the first page")
	}
	updated, _ = m.Update(cmd())
	if cursors[len(cursors)-1] != "" {
		t.Fatalf("filter did not reset cursor: %v", cursors)
	}
}

func TestBacklogCursorHistoryTruncatesAfterBacktracking(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{HasMore: true, NextCursor: "cursor-1"}, 0)
	if got := b.NextPageCursor(); got != "cursor-1" {
		t.Fatalf("first next cursor=%q", got)
	}
	b.SetPage(BacklogPage{HasMore: true, NextCursor: "cursor-2"}, 1)
	if got := b.NextPageCursor(); got != "cursor-2" {
		t.Fatalf("second next cursor=%q", got)
	}
	b.SetPage(BacklogPage{HasMore: true, NextCursor: "cursor-3"}, 2)
	if got := b.PreviousPageCursor(); got != "cursor-1" || b.PageIndex() != 1 {
		t.Fatalf("previous cursor=%q page=%d", got, b.PageIndex())
	}
	b.SetPage(BacklogPage{HasMore: true, NextCursor: "cursor-2"}, 1)
	if got := b.NextPageCursor(); got != "cursor-2" {
		t.Fatalf("revisited next cursor=%q", got)
	}
	if len(b.pageCursors) != 3 || b.pageCursors[2] != "cursor-2" {
		t.Fatalf("cursor history=%v, want [empty cursor-1 cursor-2]", b.pageCursors)
	}
}

func TestBacklogRenderKeepsSelectedRowVisibleWithinHeight(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(80, 8)
	issues := make([]model.Issue, 6)
	for i := range issues {
		issues[i] = model.Issue{ID: "b-" + string(rune('1'+i)), Title: "item"}
	}
	b.SetPage(BacklogPage{Issues: issues}, 0)
	b.selected = 5
	view := b.View()
	if !strings.Contains(view, "b-6") || strings.Contains(view, "b-1") {
		t.Fatalf("selected window is wrong:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 8 {
		t.Fatalf("backlog rendered %d lines, want <= 8:\n%s", lines, view)
	}
}

func TestScopeBacklogPreserveGlobalKeysAndSearchOwnership(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadBacklog: func(context.Context, string, int) (BacklogPage, error) { return BacklogPage{}, nil },
	}})
	m.isBacklogView = true
	m.focused = focusBacklog
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(*Model)
	if !m.backlog.Searching() {
		t.Fatal("backlog slash did not start search")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(*Model)
	if m.showHelp || m.backlog.Filter() != "?" {
		t.Fatalf("search input lost ownership: help=%t filter=%q", m.showHelp, m.backlog.Filter())
	}
	m.backlog.EndSearch()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(*Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("global help did not pass through backlog: help=%t focus=%s", m.showHelp, m.focused)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(*Model)
	if m.showHelp || m.focused != focusBacklog {
		t.Fatalf("help close did not restore backlog: help=%t focus=%s", m.showHelp, m.focused)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	m = updated.(*Model)
	if !m.showTutorial || m.focused != focusTutorial {
		t.Fatalf("global tutorial did not pass through backlog: tutorial=%t focus=%s", m.showTutorial, m.focused)
	}
}

func TestScopeMutationFailureDoesNotRefresh(t *testing.T) {
	loads, adds := 0, 0
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen, CreatedAt: time.Now()}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load: func(context.Context) (ScopeSnapshot, error) { loads++; return ScopeSnapshot{}, nil },
		Add:  func(context.Context, string, string) error { adds++; return errors.New("at capacity") },
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", MemberCount: 100, Active: true}
	m.activeScope = &active
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("A did not start mutation")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if adds != 1 || loads != 0 || !m.statusIsError || !containsText(m.statusMsg, "at capacity") {
		t.Fatalf("adds=%d loads=%d error=%t status=%q", adds, loads, m.statusIsError, m.statusMsg)
	}
}

func TestDirectScopeMutationRetainsOriginFocus(t *testing.T) {
	for _, focus := range []focus{focusDetail, focusBacklog} {
		m := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{Scopes: ScopeServices{
			Add: func(context.Context, string, string) error { return errors.New("failed") },
		}})
		active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
		m.activeScope = &active
		m.focused = focus
		if focus == focusBacklog {
			m.isBacklogView = true
			m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1", Title: "Bead"}}}, 0)
		}
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
		m = updated.(*Model)
		if cmd == nil {
			t.Fatalf("focus=%s did not start direct mutation", focus)
		}
		updated, _ = m.Update(cmd())
		m = updated.(*Model)
		if m.focused != focus || m.showScopePicker {
			t.Fatalf("focus=%s changed to %s, picker=%t", focus, m.focused, m.showScopePicker)
		}
	}
}

func TestScopePickerMutationRestoresItsOrigin(t *testing.T) {
	moved := false
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		Move: func(context.Context, string, string, string) error { moved = true; return nil },
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
	m.activeScope = &active
	m.focused = focusDetail
	m.scopeCatalog = []ScopeInfo{{ID: "s2", Name: "Later"}}
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s2", Name: "Later"}})
	m.openScopePicker("b-1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("scope picker move did not start mutation")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if !moved || m.focused != focusDetail || m.showScopePicker {
		t.Fatalf("move=%t focus=%s picker=%t", moved, m.focused, m.showScopePicker)
	}
}

func TestScopePickerMDoesNotMoveOrChangeTargetSelection(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{})
	m.focused = focusDetail
	active := ScopeInfo{ID: "s1", Name: "One", Active: true}
	m.activeScope = &active
	m.showScopePicker = true
	m.scopePickerOrigin = focusDetail
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.scopePicker.Move(1)
	target := m.scopePicker.Selected().ID
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(*Model)
	if cmd != nil || m.scopePickerMoveIssue != "" || m.scopePickerOrigin != focusDetail {
		t.Fatalf("move cmd=%t issue=%q origin=%s", cmd != nil, m.scopePickerMoveIssue, m.scopePickerOrigin)
	}
	if selected := m.scopePicker.Selected(); selected == nil || selected.ID != target {
		t.Fatalf("move changed target selection to %#v, want %q", selected, target)
	}
}

func TestMoveFromListOpensTargetedPickerAndMovesExactVisibleBead(t *testing.T) {
	var moved struct{ issue, source, target string }
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Visible bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		Move: func(_ context.Context, issue, source, target string) error {
			moved = struct{ issue, source, target string }{issue, source, target}
			return nil
		},
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
	m.activeScope = &active
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "Today", Active: true}, {ID: "s2", Name: "Later"}}

	updated, _ := m.Update(keyMsg("m"))
	m = updated.(*Model)
	if !m.showScopePicker || m.focused != focusScopePicker || !strings.Contains(m.scopePicker.View(), "Move: Visible bead") {
		t.Fatalf("move picker state: shown=%t focus=%s view=%q", m.showScopePicker, m.focused, m.scopePicker.View())
	}
	if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "enter move") || strings.Contains(footer, "enter activate") {
		t.Fatalf("move footer = %q", footer)
	}
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(*Model)
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("destination selection did not start move")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if moved.issue != "b-1" || moved.source != "s1" || moved.target != "s2" {
		t.Fatalf("move target = %+v, want b-1/s1/s2", moved)
	}
}

func TestMoveFromDetailUsesCurrentVisibleBead(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Detail bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		Move: func(context.Context, string, string, string) error { return nil },
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
	m.activeScope = &active
	m.scopeCatalog = []ScopeInfo{{ID: "s2", Name: "Later"}}
	m.focused = focusDetail
	m.showDetails = true

	updated, _ := m.Update(keyMsg("m"))
	m = updated.(*Model)
	if !m.showScopePicker || m.scopePickerMoveIssue != "b-1" || !strings.Contains(m.scopePicker.View(), "Move: Detail bead") {
		t.Fatalf("detail move state: shown=%t issue=%q view=%q", m.showScopePicker, m.scopePickerMoveIssue, m.scopePicker.View())
	}
}

func TestMoveRejectsStaleRetainedSelection(t *testing.T) {
	moves := 0
	m := NewModel([]model.Issue{{ID: "stale", Title: "Hidden bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		Move: func(context.Context, string, string, string) error { moves++; return nil },
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
	m.activeScope = &active
	m.list.SetItems(nil)
	m.pendingFilterTerm = "hidden"
	m.pendingSelectedID = "stale"

	updated, cmd := m.Update(keyMsg("m"))
	m = updated.(*Model)
	if cmd != nil || m.showScopePicker || !m.statusIsError || m.statusMsg != "No bead selected" || moves != 0 {
		t.Fatalf("stale selection acted: cmd=%t picker=%t error=%t status=%q moves=%d", cmd != nil, m.showScopePicker, m.statusIsError, m.statusMsg, moves)
	}
}

func TestMoveDestinationRequiresCurrentScopeSelection(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		Move: func(context.Context, string, string, string) error { return nil },
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
	m.activeScope = &active
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopePickerMoveIssue = "b-1"
	m.scopePicker.SetMoveTarget("Bead")

	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd != nil || !m.statusIsError || m.statusMsg != "No destination scope selected" {
		t.Fatalf("invalid destination acted: cmd=%t error=%t status=%q", cmd != nil, m.statusIsError, m.statusMsg)
	}
}

func TestScopeAndBacklogOverlaysOwnInputBeforeUnderlyingViews(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "b-1", Title: "Bead", Status: model.StatusOpen}}, nil, "", RuntimeServices{})
	m.isBacklogView = true
	m.focused = focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1", Title: "Bead"}}}, 0)
	m.backlog.selected = 0
	m.showHelp = true
	m.focused = focusHelp
	m.focusBeforeHelp = focusBacklog
	if !strings.Contains(m.View(), "Backlog") {
		t.Fatal("help overlay did not render over the backlog")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*Model)
	if m.backlog.selected != 0 || m.helpScroll == 0 {
		t.Fatalf("help input reached backlog: selected=%d helpScroll=%d", m.backlog.selected, m.helpScroll)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.showHelp || !m.isBacklogView || m.focused != focusBacklog {
		t.Fatalf("help dismissal changed backlog state: help=%t backlog=%t focus=%s", m.showHelp, m.isBacklogView, m.focused)
	}

	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}})
	m.showHelp = true
	m.focused = focusHelp
	m.focusBeforeHelp = focusScopePicker
	selected := m.scopePicker.selected
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*Model)
	if m.scopePicker.selected != selected {
		t.Fatalf("help input reached scope picker: selected=%d want=%d", m.scopePicker.selected, selected)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.showHelp || !m.showScopePicker || m.focused != focusScopePicker {
		t.Fatalf("help dismissal changed scope picker state: help=%t picker=%t focus=%s", m.showHelp, m.showScopePicker, m.focused)
	}

	m.showScopePicker = false
	m.showTutorial = true
	m.focused = focusTutorial
	m.backlog.selected = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*Model)
	if m.backlog.selected != 0 || !m.showTutorial {
		t.Fatalf("tutorial input reached backlog: selected=%d tutorial=%t", m.backlog.selected, m.showTutorial)
	}

	// Help remains globally reachable while the tutorial is active, as in the
	// existing overlay contract.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(*Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("help did not take precedence over tutorial: help=%t focus=%s", m.showHelp, m.focused)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.showHelp || !m.showTutorial || m.focused != focusTutorial {
		t.Fatalf("help dismissal changed tutorial state: help=%t tutorial=%t focus=%s", m.showHelp, m.showTutorial, m.focused)
	}

	m.showHelp = false
	m.showShortcutsSidebar = true
	m.shortcutsSidebar.ResetScroll()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(*Model)
	if m.shortcutsSidebar.scrollOffset != 0 {
		t.Fatalf("sidebar scroll controls did not reach sidebar: offset=%d", m.shortcutsSidebar.scrollOffset)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c did not remain globally handled")
	}
}

func containsText(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
