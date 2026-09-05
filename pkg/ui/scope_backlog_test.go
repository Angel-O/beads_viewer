package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	if got := m.View(); !containsText(got, "No active scope") {
		t.Fatalf("View() = %q, want no-active state", got)
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

func TestScopePickerMoveKeepsOriginAndTargetSelection(t *testing.T) {
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
	if cmd != nil || m.scopePickerMoveIssue != "b-1" || m.scopePickerOrigin != focusDetail {
		t.Fatalf("move cmd=%t issue=%q origin=%s", cmd != nil, m.scopePickerMoveIssue, m.scopePickerOrigin)
	}
	if selected := m.scopePicker.Selected(); selected == nil || selected.ID != target {
		t.Fatalf("move changed target selection to %#v, want %q", selected, target)
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
	if !strings.Contains(m.View(), "Navigation") {
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
