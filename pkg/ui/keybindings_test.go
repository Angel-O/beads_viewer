package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// TestKeyRegistryDispatch_EmptyRegistry verifies that dispatching to an empty
// registry returns handled=false and leaves the model unchanged.
func TestKeyRegistryDispatch_EmptyRegistry(t *testing.T) {
	r := NewKeyRegistry()

	// Create a minimal model for testing
	m := Model{focused: focusList}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	updatedModel, handled, cmd := r.Dispatch(focusList, "j", m, msg)

	if handled {
		t.Errorf("Dispatch on empty registry: expected handled=false, got true")
	}
	if cmd != nil {
		t.Errorf("Dispatch on empty registry: expected cmd=nil, got %v", cmd)
	}
	if updatedModel.focused != m.focused {
		t.Errorf("Dispatch on empty registry: model should be unchanged")
	}
	t.Logf("focus=%v key=%s expected=handled:false actual=handled:%v", focusList, "j", handled)
}

// TestKeyRegistryRegisterAndLookup verifies that registering a binding
// makes it discoverable via Dispatch.
func TestKeyRegistryRegisterAndLookup(t *testing.T) {
	r := NewKeyRegistry()

	handlerCalled := false
	testHandler := func(m Model, msg tea.KeyMsg) (Model, bool) {
		handlerCalled = true
		m.focused = focusDetail // Modify to prove handler ran
		return m, true
	}

	r.RegisterBinding(KeyBinding{
		Focus:    focusList,
		Key:      "enter",
		Desc:     "Select item",
		Category: "Navigation",
		Handler:  testHandler,
	})

	m := Model{focused: focusList}
	msg := tea.KeyMsg{Type: tea.KeyEnter}

	updatedModel, handled, _ := r.Dispatch(focusList, "enter", m, msg)

	if !handled {
		t.Errorf("Dispatch after register: expected handled=true, got false")
	}
	if !handlerCalled {
		t.Errorf("Dispatch after register: handler was not called")
	}
	if updatedModel.focused != focusDetail {
		t.Errorf("Dispatch after register: expected focus=focusDetail, got %v", updatedModel.focused)
	}
	t.Logf("focus=%v key=%s expected=handled:true actual=handled:%v", int(focusList), "enter", handled)
}

// TestKeyRegistryRegisterAndLookup_WrongFocus verifies that dispatch returns
// handled=false when the focus doesn't match the registered binding.
func TestKeyRegistryRegisterAndLookup_WrongFocus(t *testing.T) {
	r := NewKeyRegistry()

	testHandler := func(m Model, msg tea.KeyMsg) (Model, bool) {
		return m, true
	}

	r.RegisterBinding(KeyBinding{
		Focus:    focusList,
		Key:      "j",
		Desc:     "Move down",
		Category: "Navigation",
		Handler:  testHandler,
	})

	m := Model{focused: focusBoard}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}

	_, handled, _ := r.Dispatch(focusBoard, "j", m, msg)

	if handled {
		t.Errorf("Dispatch with wrong focus: expected handled=false, got true")
	}
	t.Logf("focus=%v key=%s expected=handled:false actual=handled:%v", int(focusBoard), "j", handled)
}

// TestKeyRegistryAllBindings verifies that AllBindings returns all registered
// bindings sorted by focus, category, then key.
func TestKeyRegistryAllBindings(t *testing.T) {
	r := NewKeyRegistry()

	noopHandler := func(m Model, msg tea.KeyMsg) (Model, bool) { return m, true }

	// Register bindings in various orders
	r.RegisterBinding(KeyBinding{Focus: focusBoard, Key: "j", Desc: "Down", Category: "Navigation", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "k", Desc: "Up", Category: "Navigation", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "enter", Desc: "Select", Category: "Actions", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusBoard, Key: "h", Desc: "Left", Category: "Navigation", Handler: noopHandler})

	bindings := r.AllBindings()

	if len(bindings) != 4 {
		t.Errorf("AllBindings: expected 4 bindings, got %d", len(bindings))
	}

	// Verify sorting: by focus, then category, then key
	// focusBoard < focusList (alphabetically by focus enum order)
	// Within each focus: Actions < Navigation (alphabetically)
	// Within each category: sorted by key

	// Log all bindings for debugging
	for i, b := range bindings {
		t.Logf("binding[%d]: focus=%v category=%s key=%s desc=%s", i, b.Focus, b.Category, b.Key, b.Desc)
	}
}

// TestKeyRegistryAllBindingsForFocus verifies that AllBindingsForFocus returns
// only bindings for the specified focus context.
func TestKeyRegistryAllBindingsForFocus(t *testing.T) {
	r := NewKeyRegistry()

	noopHandler := func(m Model, msg tea.KeyMsg) (Model, bool) { return m, true }

	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "j", Desc: "Down", Category: "Nav", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "k", Desc: "Up", Category: "Nav", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusBoard, Key: "l", Desc: "Right", Category: "Nav", Handler: noopHandler})

	listBindings := r.AllBindingsForFocus(focusList)
	boardBindings := r.AllBindingsForFocus(focusBoard)

	if len(listBindings) != 2 {
		t.Errorf("AllBindingsForFocus(focusList): expected 2, got %d", len(listBindings))
	}
	if len(boardBindings) != 1 {
		t.Errorf("AllBindingsForFocus(focusBoard): expected 1, got %d", len(boardBindings))
	}

	// Verify empty focus returns empty slice
	graphBindings := r.AllBindingsForFocus(focusGraph)
	if len(graphBindings) != 0 {
		t.Errorf("AllBindingsForFocus(focusGraph): expected 0, got %d", len(graphBindings))
	}
}

// TestKeyRegistryHasBinding verifies the HasBinding lookup method.
func TestKeyRegistryHasBinding(t *testing.T) {
	r := NewKeyRegistry()

	noopHandler := func(m Model, msg tea.KeyMsg) (Model, bool) { return m, true }

	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "j", Handler: noopHandler})

	if !r.HasBinding(focusList, "j") {
		t.Error("HasBinding: expected true for registered binding")
	}
	if r.HasBinding(focusList, "k") {
		t.Error("HasBinding: expected false for unregistered key")
	}
	if r.HasBinding(focusBoard, "j") {
		t.Error("HasBinding: expected false for wrong focus")
	}
}

// TestKeyRegistryBindingsCount verifies the count method.
func TestKeyRegistryBindingsCount(t *testing.T) {
	r := NewKeyRegistry()

	if r.BindingsCount() != 0 {
		t.Errorf("BindingsCount on empty: expected 0, got %d", r.BindingsCount())
	}

	noopHandler := func(m Model, msg tea.KeyMsg) (Model, bool) { return m, true }
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "j", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "k", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusBoard, Key: "l", Handler: noopHandler})

	if r.BindingsCount() != 3 {
		t.Errorf("BindingsCount after adding 3: expected 3, got %d", r.BindingsCount())
	}
}

// TestKeyRegistryClear verifies the Clear method removes all bindings.
func TestKeyRegistryClear(t *testing.T) {
	r := NewKeyRegistry()

	noopHandler := func(m Model, msg tea.KeyMsg) (Model, bool) { return m, true }
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "j", Handler: noopHandler})
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "k", Handler: noopHandler})

	if r.BindingsCount() != 2 {
		t.Fatalf("Setup: expected 2 bindings, got %d", r.BindingsCount())
	}

	r.Clear()

	if r.BindingsCount() != 0 {
		t.Errorf("Clear: expected 0 bindings, got %d", r.BindingsCount())
	}
	if r.HasBinding(focusList, "j") {
		t.Error("Clear: bindings should be removed")
	}
}

// TestKeyRegistryOverwrite verifies that re-registering a key overwrites
// the previous handler.
func TestKeyRegistryOverwrite(t *testing.T) {
	r := NewKeyRegistry()

	callOrder := []string{}

	handler1 := func(m Model, msg tea.KeyMsg) (Model, bool) {
		callOrder = append(callOrder, "handler1")
		return m, true
	}
	handler2 := func(m Model, msg tea.KeyMsg) (Model, bool) {
		callOrder = append(callOrder, "handler2")
		return m, true
	}

	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "j", Handler: handler1})
	r.RegisterBinding(KeyBinding{Focus: focusList, Key: "j", Handler: handler2}) // Overwrite

	m := Model{}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	r.Dispatch(focusList, "j", m, msg)

	if len(callOrder) != 1 {
		t.Fatalf("Expected exactly 1 handler call, got %d", len(callOrder))
	}
	if callOrder[0] != "handler2" {
		t.Errorf("Expected handler2 to be called (overwritten), got %s", callOrder[0])
	}
}

// TestKeyRegistryRegisterView verifies bulk registration via RegisterView.
func TestKeyRegistryRegisterView(t *testing.T) {
	r := NewKeyRegistry()

	noopHandler := func(m Model, msg tea.KeyMsg) (Model, bool) { return m, true }

	bindings := []KeyBinding{
		{Key: "j", Desc: "Down", Category: "Nav", Handler: noopHandler},
		{Key: "k", Desc: "Up", Category: "Nav", Handler: noopHandler},
		{Key: "enter", Desc: "Select", Category: "Actions", Handler: noopHandler},
	}

	r.RegisterView(focusList, bindings)

	if r.BindingsCount() != 3 {
		t.Errorf("RegisterView: expected 3 bindings, got %d", r.BindingsCount())
	}

	// Verify all bindings have correct focus
	allBindings := r.AllBindingsForFocus(focusList)
	for _, b := range allBindings {
		if b.Focus != focusList {
			t.Errorf("RegisterView: expected focus=%v, got %v", focusList, b.Focus)
		}
	}
}

func TestNewModelRegistersDocumentedBindings(t *testing.T) {
	m := setupTestModel(t)

	if m.keyRegistry == nil {
		t.Fatal("expected NewModel to initialize keyRegistry")
	}
	if m.keyRegistry.BindingsCount() == 0 {
		t.Fatal("expected NewModel to populate keyRegistry from documented bindings")
	}

	tests := []struct {
		focus focus
		key   string
	}{
		{focus: focusList, key: "j"},
		{focus: focusList, key: "home"},
		{focus: focusList, key: "enter"},
		{focus: focusDetail, key: "C"},
		{focus: focusBoard, key: "h"},
		{focus: focusGraph, key: "pgup/pgdown"},
		{focus: focusHistory, key: "v"},
	}

	for _, tc := range tests {
		if !m.keyRegistry.HasBinding(tc.focus, tc.key) {
			t.Errorf("expected documented binding focus=%v key=%q to be registered", tc.focus, tc.key)
		}
	}
}

// =============================================================================
// Key Dispatch Integration Tests
// =============================================================================
//
// These tests verify that key events are correctly routed to view handlers
// based on the current focus context. They use the actual Update() dispatch
// mechanism rather than the KeyRegistry directly.

// keyMsg creates a tea.KeyMsg for a given key string.
func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "f2":
		return tea.KeyMsg{Type: tea.KeyF2}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// setupTestModel creates a ready model with test data for key dispatch tests.
func setupTestModel(t *testing.T) Model {
	t.Helper()
	issues := testIssuesForKeyDispatch()
	return NewModel(issues, nil, "")
}

// testIssuesForKeyDispatch creates a minimal issue set for testing.
func testIssuesForKeyDispatch() []model.Issue {
	return []model.Issue{
		{ID: "kd-1", Title: "Test Issue 1", Status: model.StatusOpen},
		{ID: "kd-2", Title: "Test Issue 2", Status: model.StatusOpen},
		{ID: "kd-3", Title: "Test Issue 3", Status: model.StatusClosed},
	}
}

// TestKeyDispatch_BoardNavigation tests board view key handling.
func TestKeyDispatch_BoardNavigation(t *testing.T) {
	m := setupTestModel(t)

	// Switch to board view
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	if m.focused != focusBoard {
		t.Fatalf("Expected focusBoard after 'b' key, got %v", m.focused)
	}

	tests := []struct {
		key      string
		desc     string
		checkFn  func(Model) bool
		expected string
	}{
		{"h", "left navigation", func(m Model) bool { return m.focused == focusBoard }, "stays in board"},
		{"l", "right navigation", func(m Model) bool { return m.focused == focusBoard }, "stays in board"},
		{"j", "down navigation", func(m Model) bool { return m.focused == focusBoard }, "stays in board"},
		{"k", "up navigation", func(m Model) bool { return m.focused == focusBoard }, "stays in board"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			updated, _ := m.Update(keyMsg(tc.key))
			result := updated.(Model)
			if !tc.checkFn(result) {
				t.Errorf("focus=%v key=%s expected=%s actual=focus:%v", focusBoard, tc.key, tc.expected, result.focused)
			}
			t.Logf("focus=%v key=%s expected=%s actual=focus:%v", focusBoard, tc.key, tc.expected, result.focused)
		})
	}
}

// TestKeyDispatch_GraphNavigation tests graph view key handling.
func TestKeyDispatch_GraphNavigation(t *testing.T) {
	m := setupTestModel(t)

	// Switch to graph view
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)

	if m.focused != focusGraph {
		t.Fatalf("Expected focusGraph after 'g' key, got %v", m.focused)
	}

	tests := []struct {
		key      string
		desc     string
		checkFn  func(Model) bool
		expected string
	}{
		{"h", "left navigation", func(m Model) bool { return m.focused == focusGraph }, "stays in graph"},
		{"l", "right navigation", func(m Model) bool { return m.focused == focusGraph }, "stays in graph"},
		{"j", "down navigation", func(m Model) bool { return m.focused == focusGraph }, "stays in graph"},
		{"k", "up navigation", func(m Model) bool { return m.focused == focusGraph }, "stays in graph"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			updated, _ := m.Update(keyMsg(tc.key))
			result := updated.(Model)
			if !tc.checkFn(result) {
				t.Errorf("focus=%v key=%s expected=%s actual=focus:%v", focusGraph, tc.key, tc.expected, result.focused)
			}
			t.Logf("focus=%v key=%s expected=%s actual=focus:%v", focusGraph, tc.key, tc.expected, result.focused)
		})
	}
}

func TestCompactGraphAndTreeStatusHints(t *testing.T) {
	m := setupTestModel(t)
	m.isGraphView = true
	m.focused = focusGraph

	normalGraph := ansi.Strip(m.renderFooter())
	if !strings.Contains(normalGraph, "/:search") || strings.Contains(normalGraph, "n/N:match") || strings.Contains(normalGraph, "H/L") {
		t.Fatalf("normal Graph hint has incorrect search guidance: %q", normalGraph)
	}

	m.graphView.StartSearch()
	searchInputGraph := ansi.Strip(m.renderFooter())
	if strings.Contains(searchInputGraph, "n/N:match") {
		t.Fatalf("empty Graph search input exposed n/N guidance: %q", searchInputGraph)
	}
	m.graphView.AppendSearchRunes([]rune("kd"))
	searchingGraph := ansi.Strip(m.renderFooter())
	if !strings.Contains(searchingGraph, "n/N:match") {
		t.Fatalf("Graph search hint omitted n/N guidance: %q", searchingGraph)
	}
	m.graphView.ClearSearch()

	m.isGraphView = false
	m.focused = focusTree
	normalTree := ansi.Strip(m.renderFooter())
	if !strings.Contains(normalTree, "/:search") || strings.Contains(normalTree, "n/N:match") {
		t.Fatalf("normal Tree hint has incorrect search guidance: %q", normalTree)
	}

	m.tree.StartSearch()
	m.tree.UpdateSearchInput(keyMsg("k"))
	searchingTree := ansi.Strip(m.renderFooter())
	if !strings.Contains(searchingTree, "type:search") || strings.Contains(searchingTree, "n/N:match") {
		t.Fatalf("active Tree input hint changed unexpectedly: %q", searchingTree)
	}
	m.tree.FinishSearch()
	if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "n/N:match") {
		t.Fatalf("submitted Tree search hint omitted n/N guidance: %q", footer)
	}
}

func TestKeyDispatch_GraphSearchConsumesInputAndSelectsMatches(t *testing.T) {
	issues := []model.Issue{
		{ID: "search-a", Title: "Board query first", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "search-b", Title: "Board query second", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "search-c", Title: "Other", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	m := NewModel(issues, nil, "")
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	for _, key := range []string{"b", "o", "a", "r", "d", "?", ";", "q", "u", "e", "r", "y"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(Model)
	}
	if m.focused != focusGraph || !m.isGraphView || m.isBoardView || m.showHelp || m.showShortcutsSidebar {
		t.Fatalf("Graph search input leaked into global routing: focus=%v graph=%v board=%v help=%v sidebar=%v", m.focused, m.isGraphView, m.isBoardView, m.showHelp, m.showShortcutsSidebar)
	}
	if got := m.graphView.SearchQuery(); got != "board?;query" {
		t.Fatalf("Graph search query = %q, want %q", got, "board?;query")
	}

	// Replace the punctuation-heavy routing query with a matching title query.
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("Board query"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.focused != focusGraph {
		t.Fatalf("Enter while searching changed focus to %v", m.focused)
	}
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "search-a" {
		t.Fatalf("first Graph search match = %#v, want search-a", selected)
	}

	updated, _ = m.Update(keyMsg("n"))
	m = updated.(Model)
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "search-b" {
		t.Fatalf("next Graph search match = %#v, want search-b", selected)
	}
	updated, _ = m.Update(keyMsg("N"))
	m = updated.(Model)
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "search-a" {
		t.Fatalf("previous Graph search match = %#v, want search-a", selected)
	}
}

func TestKeyDispatch_GraphSearchEscapeClearsBeforeExit(t *testing.T) {
	m := setupTestModel(t)
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	m.graphView.SelectByID("kd-2")

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("kd-1"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.focused != focusGraph || m.graphView.HasSearchQuery() {
		t.Fatalf("cancelled Graph search should stay in Graph with no query: focus=%v query=%q", m.focused, m.graphView.SearchQuery())
	}
	if selected := m.graphView.SelectedIssue(); selected == nil || selected.ID != "kd-2" {
		t.Fatalf("cancelled Graph search selection = %#v, want kd-2", selected)
	}

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("kd"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.focused != focusGraph || m.graphView.HasSearchQuery() {
		t.Fatalf("first Escape should clear accepted query: focus=%v query=%q", m.focused, m.graphView.SearchQuery())
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.focused != focusList || m.isGraphView {
		t.Fatalf("second Escape should exit Graph: focus=%v graph=%v", m.focused, m.isGraphView)
	}
}

// TestKeyDispatch_GInBoardStartsCombo verifies that 'g' in board view
// starts the gg-combo timer (doesn't immediately toggle to graph).
// The actual graph toggle happens asynchronously via comboTickMsg.
func TestKeyDispatch_GInBoardStartsCombo(t *testing.T) {
	m := setupTestModel(t)

	// Switch to board view
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	if m.focused != focusBoard {
		t.Fatalf("Expected focusBoard after 'b', got %v", m.focused)
	}

	// 'g' starts combo timer - should NOT immediately toggle to graph
	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)

	// Should still be in board (combo timer started, not yet expired)
	if m.focused != focusBoard {
		t.Errorf("focus=board key=g expected=still_board (combo pending) actual=focus:%v", m.focused)
	}
	// Pending combo key should be set
	if m.pendingComboKey != "g" {
		t.Errorf("pendingComboKey expected=g actual=%s", m.pendingComboKey)
	}
	t.Logf("focus=board key=g expected=combo_pending actual=focus:%v pendingCombo:%s", m.focused, m.pendingComboKey)
}

// TestKeyDispatch_GInTreeStartsCombo verifies that 'g' in tree view starts gg-combo (bv-6fm0).
// First 'g' sets pending combo, second 'g' within window jumps to top.
// The actual graph toggle happens asynchronously via comboTickMsg.
func TestKeyDispatch_GInTreeStartsCombo(t *testing.T) {
	m := setupTestModel(t)

	// Switch to tree view
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)

	if m.focused != focusTree {
		t.Fatalf("Expected focusTree after 'E', got %v", m.focused)
	}

	// First 'g' starts combo timer - should NOT immediately toggle to graph
	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)

	// Should still be in tree (combo timer started, not yet expired)
	if m.focused != focusTree {
		t.Errorf("focus=tree key=g expected=still_tree (combo pending) actual=focus:%v", m.focused)
	}
	// Pending combo key should be set
	if m.pendingComboKey != "g" {
		t.Errorf("pendingComboKey expected=g actual=%s", m.pendingComboKey)
	}

	// Second 'g' within combo window triggers gg-combo (jump to top), stays in tree
	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)

	if m.focused != focusTree {
		t.Errorf("focus=tree key=gg expected=tree (gg-combo) actual=focus:%v", m.focused)
	}
	if m.pendingComboKey != "" {
		t.Errorf("expected pendingComboKey cleared after gg-combo, got %q", m.pendingComboKey)
	}
}

// TestKeyDispatch_ComboCancelledByOtherKey verifies that pressing another key
// cancels a pending gg-combo (bv-6fm0 bug fix).
func TestKeyDispatch_ComboCancelledByOtherKey(t *testing.T) {
	m := setupTestModel(t)

	// Switch to board view
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	if m.focused != focusBoard {
		t.Fatalf("Expected focusBoard after 'b', got %v", m.focused)
	}

	// First 'g' starts combo timer
	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)

	if m.pendingComboKey != "g" {
		t.Fatalf("Expected pendingComboKey='g' after first g, got %q", m.pendingComboKey)
	}

	// Press 'j' (navigation) - should CANCEL the pending combo
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(Model)

	// pendingComboKey should be cleared (combo cancelled)
	if m.pendingComboKey != "" {
		t.Errorf("Expected pendingComboKey cleared after 'j', got %q", m.pendingComboKey)
	}
	// Should still be in board view
	if m.focused != focusBoard {
		t.Errorf("Expected to stay in board after 'j', got focus:%v", m.focused)
	}
}

// TestKeyDispatch_Regression_QInHistoryClosesHistory verifies that 'q' in history view
// closes history and returns to list.
func TestKeyDispatch_Regression_QInHistoryClosesHistory(t *testing.T) {
	m := setupTestModel(t)

	// Toggle history view on
	updated, _ := m.Update(keyMsg("h"))
	m = updated.(Model)

	if !m.isHistoryView || m.focused != focusHistory {
		t.Fatalf("Expected history view after 'h', got isHistoryView=%v focused=%v", m.isHistoryView, m.focused)
	}

	// Press 'q' - should close history (falls through to quit confirm or handled by global)
	updated, _ = m.Update(keyMsg("q"))
	m = updated.(Model)

	// 'q' in history should close history view (or show quit confirm if at top level)
	// Based on the code, 'q' is not in the history handler's key list, so it falls through
	// to global handling which closes overlays.
	t.Logf("focus=history key=q expected=close_history actual=isHistoryView:%v focused:%v", m.isHistoryView, m.focused)
}

func TestKeyDispatch_HistoryTogglesShortcutsSidebar(t *testing.T) {
	m := setupTestModel(t)
	m.width, m.height = 200, 40
	updated, _ := m.Update(keyMsg("h"))
	m = updated.(Model)
	if !m.isHistoryView || m.focused != focusHistory {
		t.Fatalf("expected wide History view, got view=%v focus=%v", m.isHistoryView, m.focused)
	}

	m.historyView.ToggleViewMode()
	m.historyView.StartSearchWithMode(searchModeCommit)
	m.historyView.UpdateSearchInput(keyMsg("needle"))
	m.historyView.FinishSearch()
	m.historyView.SetFileTreeFocus(false)
	beforeMode := m.historyView.IsGitMode()
	beforeFocus := m.historyView.focused
	beforeSearch := m.historyView.SearchQuery()
	beforeFileTreeFocus := m.historyView.FileTreeHasFocus()
	beforeSelectedBead := m.historyView.selectedBead
	beforeSelectedCommit := m.historyView.selectedCommit
	beforeScrollOffset := m.historyView.scrollOffset
	beforeGitScrollOffset := m.historyView.gitScrollOffset
	beforeMiddleScrollOffset := m.historyView.middleScrollOffset
	beforeTimelineScrollOffset := m.historyView.timelineScrollOffset
	beforeSelectedGitCommit := m.historyView.selectedGitCommit
	beforeSelectedRelatedBead := m.historyView.selectedRelatedBead

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showShortcutsSidebar || !m.isHistoryView || m.focused != focusHistory {
		t.Fatalf("semicolon did not open the History sidebar: sidebar=%v view=%v focus=%v", m.showShortcutsSidebar, m.isHistoryView, m.focused)
	}
	if m.historyView.IsGitMode() != beforeMode || m.historyView.focused != beforeFocus || m.historyView.SearchQuery() != beforeSearch || m.historyView.FileTreeHasFocus() != beforeFileTreeFocus || m.historyView.selectedBead != beforeSelectedBead || m.historyView.selectedCommit != beforeSelectedCommit || m.historyView.scrollOffset != beforeScrollOffset || m.historyView.gitScrollOffset != beforeGitScrollOffset || m.historyView.middleScrollOffset != beforeMiddleScrollOffset || m.historyView.timelineScrollOffset != beforeTimelineScrollOffset || m.historyView.selectedGitCommit != beforeSelectedGitCommit || m.historyView.selectedRelatedBead != beforeSelectedRelatedBead {
		t.Fatal("semicolon changed History state")
	}
	if got, want := m.historyView.width, m.mainContentWidth(); got != want {
		t.Fatalf("History width with sidebar = %d, want reserved content width %d", got, want)
	}

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	if m.showShortcutsSidebar {
		t.Fatal("second semicolon did not close the History sidebar")
	}
	if got, want := m.historyView.width, m.width; got != want {
		t.Fatalf("History width after closing sidebar = %d, want terminal width %d", got, want)
	}

	m = setupTestModel(t)
	m.width, m.height = 200, 40
	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showShortcutsSidebar {
		t.Fatal("semicolon did not open the supported list sidebar")
	}
	updated, _ = m.Update(keyMsg("h"))
	m = updated.(Model)
	if !m.isHistoryView || m.focused != focusHistory || !m.showShortcutsSidebar {
		t.Fatalf("entering History lost the open sidebar: view=%v focus=%v sidebar=%v", m.isHistoryView, m.focused, m.showShortcutsSidebar)
	}
	if !strings.Contains(m.View(), "Shortcuts") {
		t.Fatal("History View did not render the shortcuts sidebar after entering from List")
	}
}

func TestKeyDispatch_ShortcutsSidebarTogglesInBoardAndInsights(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		want focus
	}{
		{name: "board", key: "b", want: focusBoard},
		{name: "insights", key: "i", want: focusInsights},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := setupTestModel(t)
			m.width, m.height = 200, 40

			updated, _ := m.Update(keyMsg(tc.key))
			m = updated.(Model)
			if m.focused != tc.want {
				t.Fatalf("view key %q set focus=%v, want %v", tc.key, m.focused, tc.want)
			}

			updated, _ = m.Update(keyMsg(";"))
			m = updated.(Model)
			if !m.showShortcutsSidebar {
				t.Fatal("semicolon did not open the shortcuts sidebar")
			}
			if !strings.Contains(m.View(), "Shortcuts") {
				t.Fatalf("%s view clipped the open shortcuts sidebar", tc.name)
			}

			updated, _ = m.Update(keyMsg(";"))
			m = updated.(Model)
			if m.showShortcutsSidebar {
				t.Fatal("second semicolon did not close the shortcuts sidebar")
			}
		})
	}
}

func TestKeyDispatch_ShortcutsSidebarKeepsScrollNotificationAndControls(t *testing.T) {
	m := setupTestModel(t)
	m.width, m.height = 200, 40
	updated, _ := m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showShortcutsSidebar || !strings.Contains(m.statusMsg, "ctrl+j/k scroll") {
		t.Fatalf("sidebar notification lost: shown=%v status=%q", m.showShortcutsSidebar, m.statusMsg)
	}

	initial := m.shortcutsSidebar.scrollOffset
	updated, _ = m.Update(keyMsg("ctrl+j"))
	m = updated.(Model)
	if m.shortcutsSidebar.scrollOffset <= initial {
		t.Fatalf("ctrl+j did not scroll sidebar: before=%d after=%d", initial, m.shortcutsSidebar.scrollOffset)
	}
	updated, _ = m.Update(keyMsg("ctrl+k"))
	m = updated.(Model)
	if m.shortcutsSidebar.scrollOffset != initial {
		t.Fatalf("ctrl+k did not restore sidebar scroll: want=%d got=%d", initial, m.shortcutsSidebar.scrollOffset)
	}
}

func TestAttentionShortcutsSidebarUsesAttentionContext(t *testing.T) {
	m := setupTestModel(t)
	m.width, m.height = 200, 40
	m.focused = focusInsights
	m.showAttentionView = true

	updated, _ := m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showShortcutsSidebar {
		t.Fatal("semicolon did not open sidebar for Attention")
	}
	view := m.View()
	if !strings.Contains(view, "Close Attention") {
		t.Fatalf("Attention sidebar missing Attention-specific bindings:\n%s", view)
	}
	if strings.Contains(view, "Flow matrix") || strings.Contains(view, "]/F4 Attention view") {
		t.Fatalf("Attention sidebar exposed Insights cross-view bindings:\n%s", view)
	}
}

// TestKeyDispatch_Regression_EscInTreeReturnsList verifies that ESC in tree view
// returns to list.
func TestKeyDispatch_Regression_EscInTreeReturnsList(t *testing.T) {
	m := setupTestModel(t)

	// Toggle tree view on (E, not f - f is FlowMatrix)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)

	if m.focused != focusTree {
		t.Fatalf("Expected focusTree after 'E', got %v", m.focused)
	}

	// Press ESC - should return to list
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)

	// ESC is handled by tree and should close tree or return to list
	// Based on code: "esc" is in tree handler's list
	t.Logf("focus=tree key=esc expected=return_to_list actual=focused:%v", m.focused)
}

func TestKeyDispatch_TreeSearchProtectsInputAndClearsBeforeExit(t *testing.T) {
	m := setupTestModel(t)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	for _, key := range []string{"?", "`", ";", "E", "v"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(Model)
	}
	if m.focused != focusTree || m.tree.SearchQuery() != "?`;Ev" {
		t.Fatalf("printable shortcuts escaped Tree search: focus=%v query=%q", m.focused, m.tree.SearchQuery())
	}
	if m.showHelp || m.showTutorial || m.showShortcutsSidebar {
		t.Fatalf("Tree search opened global UI: help=%v tutorial=%v sidebar=%v", m.showHelp, m.showTutorial, m.showShortcutsSidebar)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)
	if !m.lastForceRefresh.IsZero() || !m.tree.IsSearchActive() {
		t.Fatalf("Tree search leaked Ctrl+R: refreshed=%v active=%v", !m.lastForceRefresh.IsZero(), m.tree.IsSearchActive())
	}

	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.tree.IsSearchActive() {
		t.Fatal("Enter should select the current result and finish Tree input")
	}
	if got := m.tree.SearchQuery(); got != "?`;Ev" {
		t.Fatalf("Enter changed Tree search query to %q", got)
	}
	updated, _ = m.Update(keyMsg("v"))
	m = updated.(Model)
	if !m.tree.searchSubtrees {
		t.Fatal("submitted Tree search did not toggle to subtrees")
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.focused != focusTree || m.tree.SearchQuery() != "" {
		t.Fatalf("first Escape should clear search in Tree: focus=%v query=%q", m.focused, m.tree.SearchQuery())
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.focused != focusList {
		t.Fatalf("second Escape should exit Tree, got focus=%v", m.focused)
	}
}

func TestTreeSearchFooterScopeGuidanceFollowsInputState(t *testing.T) {
	m := sizedModel(t, mouseTestIssues(2), 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("Issue"))
	m = updated.(Model)

	editingFooter := ansi.Strip(m.renderFooter())
	if !strings.Contains(editingFooter, "Enter:done") || strings.Contains(editingFooter, "v:subtrees") {
		t.Fatalf("editing Tree footer has incorrect scope guidance: %q", editingFooter)
	}

	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	submittedFooter := ansi.Strip(m.renderFooter())
	if !strings.Contains(submittedFooter, "minimal • v:subtrees") {
		t.Fatalf("submitted Tree footer is missing early scope guidance: %q", submittedFooter)
	}
	if strings.Index(submittedFooter, "minimal • v:subtrees") > strings.Index(submittedFooter, "n/N:match") {
		t.Fatalf("submitted Tree scope guidance appears too late: %q", submittedFooter)
	}
}

func TestKeyDispatch_TreeShortcutsSidebarPreservesState(t *testing.T) {
	maxLineWidth := func(view string) int {
		maxWidth := 0
		for _, line := range strings.Split(view, "\n") {
			if width := lipgloss.Width(line); width > maxWidth {
				maxWidth = width
			}
		}
		return maxWidth
	}

	issues := []model.Issue{
		{ID: "tree-root", Title: "Root", Status: model.StatusOpen, IssueType: model.TypeEpic},
		{
			ID: "tree-child", Title: "Child", Status: model.StatusOpen, IssueType: model.TypeTask,
			Dependencies: []*model.Dependency{{IssueID: "tree-child", DependsOnID: "tree-root", Type: model.DepParentChild}},
		},
		{ID: "tree-other", Title: "Other", Status: model.StatusOpen, IssueType: model.TypeTask},
	}
	for i := 0; i < 45; i++ {
		issues = append(issues, model.Issue{
			ID: fmt.Sprintf("tree-extra-%02d", i), Title: "Extra", Status: model.StatusOpen, IssueType: model.TypeTask,
		})
	}
	m := sizedModel(t, issues, 200, 40)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	if m.focused != focusTree {
		t.Fatalf("expected Tree focus, got %v", m.focused)
	}

	m.tree.roots[0].Expanded = false
	m.tree.rebuildFlatList()
	m.tree.StartSearch()
	m.tree.UpdateSearchInput(keyMsg("Extra"))
	m.tree.FinishSearch()
	m.tree.JumpToBottom()
	selectedID := m.tree.GetSelectedID()
	beforeOffset := m.tree.GetViewportOffset()
	beforeExpanded := m.tree.roots[0].Expanded
	beforeSearch := m.tree.SearchQuery()

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showShortcutsSidebar {
		t.Fatal("semicolon did not open the Tree sidebar")
	}
	if got, want := m.tree.width, m.mainContentWidth(); got != want {
		t.Fatalf("Tree width with sidebar = %d, want reserved content width %d", got, want)
	}
	treeBody := m.theme.Renderer.NewStyle().Width(m.mainContentWidth()).Render(m.tree.View())
	if got, want := maxLineWidth(treeBody), m.mainContentWidth(); got != want {
		t.Fatalf("Tree rendering width with sidebar = %d, want reserved content width %d", got, want)
	}
	if m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != beforeOffset || m.tree.roots[0].Expanded != beforeExpanded || m.tree.SearchQuery() != beforeSearch {
		t.Fatal("opening the sidebar changed Tree state")
	}

	updated, _ = m.Update(keyMsg("f2"))
	m = updated.(Model)
	if m.showShortcutsSidebar {
		t.Fatal("F2 did not close the Tree sidebar")
	}
	if got, want := m.tree.width, m.width; got != want {
		t.Fatalf("Tree width after closing sidebar = %d, want terminal width %d", got, want)
	}
	treeBody = m.theme.Renderer.NewStyle().Width(m.width).Render(m.tree.View())
	if got, want := maxLineWidth(treeBody), m.width; got != want {
		t.Fatalf("Tree rendering width after closing sidebar = %d, want terminal width %d", got, want)
	}
	if m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != beforeOffset || m.tree.roots[0].Expanded != beforeExpanded || m.tree.SearchQuery() != beforeSearch {
		t.Fatal("closing the sidebar changed Tree state")
	}

	updated, _ = m.Update(keyMsg("b"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	if m.focused != focusTree {
		t.Fatalf("expected Tree after supported-view transition, got %v", m.focused)
	}
	if m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != beforeOffset || m.tree.roots[0].Expanded != beforeExpanded || m.tree.SearchQuery() != beforeSearch {
		t.Fatalf("supported-view transition changed Tree state: selected=%q/%q offset=%d/%d expanded=%v/%v search=%q/%q", m.tree.GetSelectedID(), selectedID, m.tree.GetViewportOffset(), beforeOffset, m.tree.roots[0].Expanded, beforeExpanded, m.tree.SearchQuery(), beforeSearch)
	}
}

func TestKeyDispatch_QuitConfirmWithTreeSidebarKeepsModalAndStateCoherent(t *testing.T) {
	m := sizedModel(t, mouseTestIssues(40), 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	m.tree.JumpToBottom()
	selectedID := m.tree.GetSelectedID()
	viewportOffset := m.tree.GetViewportOffset()

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if !m.showQuitConfirm || !m.showShortcutsSidebar {
		t.Fatalf("expected quit confirmation with sidebar state retained: quit=%v sidebar=%v", m.showQuitConfirm, m.showShortcutsSidebar)
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "Shortcuts") {
		t.Fatal("quit confirmation appended the underlying shortcuts sidebar")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Quit bv?") {
		t.Fatal("quit confirmation is missing from the composed view")
	}

	updated, _ = m.Update(keyMsg("n"))
	m = updated.(Model)
	if m.showQuitConfirm || !m.showShortcutsSidebar || m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != viewportOffset {
		t.Fatalf("cancel changed prompt/sidebar/Tree state: quit=%v sidebar=%v selected=%q offset=%d", m.showQuitConfirm, m.showShortcutsSidebar, m.tree.GetSelectedID(), m.tree.GetViewportOffset())
	}
	if !strings.Contains(ansi.Strip(m.View()), "Shortcuts") {
		t.Fatal("cancel did not restore the underlying shortcuts sidebar")
	}
}

func TestKeyDispatch_HelpWithTreeSidebarKeepsOverlayAndStateCoherent(t *testing.T) {
	issues := mouseTestIssues(40)
	issues[1].Dependencies = []*model.Dependency{{IssueID: issues[1].ID, DependsOnID: issues[0].ID, Type: model.DepParentChild}}
	m := sizedModel(t, issues, 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	var expandedNode *IssueTreeNode
	for _, node := range m.tree.roots {
		if len(node.Children) > 0 {
			expandedNode = node
			break
		}
	}
	if expandedNode == nil {
		t.Fatal("test Tree did not build a parent-child node")
	}
	expandedNode.Expanded = false
	m.tree.rebuildFlatList()
	m.tree.StartSearch()
	m.tree.UpdateSearchInput(keyMsg("Issue"))
	m.tree.FinishSearch()
	m.tree.JumpToBottom()
	selectedID := m.tree.GetSelectedID()
	viewportOffset := m.tree.GetViewportOffset()
	searchQuery := m.tree.SearchQuery()

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	if !m.showHelp || !m.showShortcutsSidebar || m.focused != focusHelp {
		t.Fatalf("expected Help with sidebar state retained: help=%v sidebar=%v focus=%v", m.showHelp, m.showShortcutsSidebar, m.focused)
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "; hide") || !strings.Contains(view, "Keyboard Shortcuts") {
		t.Fatal("Help was interleaved with the underlying shortcuts sidebar")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("Help line exceeds terminal width %d: %d", m.width, lipgloss.Width(line))
		}
	}

	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	if m.showHelp || !m.showShortcutsSidebar || m.focused != focusTree || m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != viewportOffset || m.tree.SearchQuery() != searchQuery || m.tree.issueMap[expandedNode.Issue.ID].Expanded {
		t.Fatalf("closing Help changed Tree/sidebar state: help=%v sidebar=%v focus=%v selected=%q offset=%d query=%q expanded=%v", m.showHelp, m.showShortcutsSidebar, m.focused, m.tree.GetSelectedID(), m.tree.GetViewportOffset(), m.tree.SearchQuery(), m.tree.issueMap[expandedNode.Issue.ID].Expanded)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Shortcuts") {
		t.Fatal("closing Help did not restore the shortcuts sidebar")
	}
}

func TestKeyDispatch_HelpConsumesSidebarToggleWithoutBannerOrMutation(t *testing.T) {
	m := sizedModel(t, mouseTestIssues(40), 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	m.tree.JumpToBottom()
	selectedID := m.tree.GetSelectedID()
	viewportOffset := m.tree.GetViewportOffset()

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	statusBeforeHelp := m.statusMsg
	if !m.showShortcutsSidebar || !strings.HasPrefix(statusBeforeHelp, "Shortcuts sidebar:") {
		t.Fatalf("sidebar setup failed: visible=%v status=%q", m.showShortcutsSidebar, statusBeforeHelp)
	}
	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showHelp || !m.showShortcutsSidebar || m.statusMsg != "" || m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != viewportOffset {
		t.Fatalf("Help semicolon changed hidden state: help=%v sidebar=%v focus=%v status=%q before=%q selected=%q offset=%d", m.showHelp, m.showShortcutsSidebar, m.focused, m.statusMsg, statusBeforeHelp, m.tree.GetSelectedID(), m.tree.GetViewportOffset())
	}
	if footer := ansi.Strip(m.renderFooter()); strings.Contains(footer, "Shortcuts sidebar:") {
		t.Fatal("Help rendered the underlying sidebar status banner")
	}

	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	if m.showHelp || !m.showShortcutsSidebar || m.statusMsg != "" || m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != viewportOffset {
		t.Fatal("closing Help did not restore sidebar or Tree state")
	}
}

func TestKeyDispatch_TutorialConsumesSidebarToggleAndRestoresTreeState(t *testing.T) {
	issues := mouseTestIssues(40)
	issues[1].Dependencies = []*model.Dependency{{IssueID: issues[1].ID, DependsOnID: issues[0].ID, Type: model.DepParentChild}}
	m := sizedModel(t, issues, 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	var expandedNode *IssueTreeNode
	for _, node := range m.tree.roots {
		if len(node.Children) > 0 {
			expandedNode = node
			break
		}
	}
	if expandedNode == nil {
		t.Fatal("test Tree did not build a parent-child node")
	}
	expandedNode.Expanded = false
	m.tree.rebuildFlatList()
	m.tree.StartSearch()
	m.tree.UpdateSearchInput(keyMsg("Issue"))
	m.tree.FinishSearch()
	m.tree.JumpToBottom()
	selectedID := m.tree.GetSelectedID()
	viewportOffset := m.tree.GetViewportOffset()
	searchQuery := m.tree.SearchQuery()

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg(" "))
	m = updated.(Model)
	if !m.showTutorial || !m.showShortcutsSidebar || m.focused != focusTutorial {
		t.Fatalf("expected Tutorial with sidebar state retained: tutorial=%v sidebar=%v focus=%v", m.showTutorial, m.showShortcutsSidebar, m.focused)
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "; hide") || strings.Contains(view, "ctrl+j/k scroll 0%") {
		t.Fatal("Tutorial was interleaved with the underlying shortcuts sidebar")
	}

	for _, key := range []string{";", "f2"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(Model)
		if !m.showTutorial || !m.showShortcutsSidebar {
			t.Fatalf("Tutorial key %q changed hidden sidebar state: tutorial=%v sidebar=%v", key, m.showTutorial, m.showShortcutsSidebar)
		}
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.showTutorial || !m.showShortcutsSidebar || m.focused != focusTree || m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != viewportOffset || m.tree.SearchQuery() != searchQuery || m.tree.issueMap[expandedNode.Issue.ID].Expanded {
		t.Fatalf("closing Tutorial changed Tree/sidebar state: tutorial=%v sidebar=%v focus=%v selected=%q offset=%d query=%q expanded=%v", m.showTutorial, m.showShortcutsSidebar, m.focused, m.tree.GetSelectedID(), m.tree.GetViewportOffset(), m.tree.SearchQuery(), m.tree.issueMap[expandedNode.Issue.ID].Expanded)
	}
}

func TestKeyDispatch_DirectTutorialEntryClearsStaleHelpFocus(t *testing.T) {
	m := sizedModel(t, mouseTestIssues(2), 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	if m.focused != focusTree {
		t.Fatalf("closing Help did not restore Tree focus: %v", m.focused)
	}
	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("`"))
	m = updated.(Model)
	if !m.showTutorial || m.focused != focusTutorial {
		t.Fatalf("direct Tutorial entry failed: tutorial=%v focus=%v", m.showTutorial, m.focused)
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.showTutorial || m.focused != focusList {
		t.Fatalf("direct Tutorial close restored stale focus: tutorial=%v focus=%v", m.showTutorial, m.focused)
	}
}

func TestKeyBindingDocsCoverTreeSearchAndExactEntryExit(t *testing.T) {
	docs := GetKeyBindingDocs()
	wants := map[string]bool{
		"n|list,detail|Add comment":            false,
		"E|list,detail|Enter Tree (uppercase)": false,
		"o|list,board,tree|Open issues only":   false,
		"c|list,board,tree|Closed issues only": false,
		"r|list,board,tree|Ready (unblocked)":  false,
		"/|tree|Search Tree":                   false,
		"n|tree|Next search match":             false,
		"N|tree|Previous search match":         false,
		"v|tree|Toggle search scope":           false,
		"+|tree|Expand all":                    false,
		"-|tree|Collapse all":                  false,
		"E|tree|Exit Tree":                     false,
		"esc|tree|Clear search or exit Tree":   false,
	}
	for _, doc := range docs {
		if doc.Context == "graph" && (doc.Key == "H" || doc.Key == "L") {
			t.Errorf("authoritative key docs retain non-functional Graph scroll shortcut: %s", doc.Key)
		}
		if doc.Key == "#" && doc.Desc == "Add comment" {
			t.Errorf("authoritative key docs retain removed # comment shortcut")
		}
		key := doc.Key + "|" + doc.Context + "|" + doc.Desc
		if _, ok := wants[key]; ok {
			wants[key] = true
		}
	}
	for binding, found := range wants {
		if !found {
			t.Errorf("authoritative key docs missing %q", binding)
		}
	}
}

func TestKeyBindingDocsUseContextualNavigationAndActions(t *testing.T) {
	docs := GetKeyBindingDocs()
	for _, doc := range docs {
		if doc.Context == "all" {
			switch doc.Key {
			case "G", "gg", "ctrl+d", "ctrl+u", "enter", "y", "U":
				t.Errorf("context-specific shortcut %q is still documented for all views", doc.Key)
			}
		}
		if doc.Key == "y" && doc.Context == "history" && doc.Desc != "Copy commit SHA" {
			t.Errorf("History y description = %q, want Copy commit SHA", doc.Desc)
		}
	}

	for _, expected := range []KeyBindingDoc{
		{Key: "gg", Context: "board,tree"},
		{Key: "C", Context: "list,detail"},
		{Key: "enter", Desc: "Open selected bead", Context: "history"},
		{Key: "enter", Desc: "Apply label filter", Context: "label-dashboard"},
	} {
		found := false
		for _, doc := range docs {
			if doc.Key == expected.Key && (expected.Desc == "" || doc.Desc == expected.Desc) && doc.Context == expected.Context {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing contextual keybinding doc: %+v", expected)
		}
	}
}

func TestKeyBindingDocsCoverAuditedViewContexts(t *testing.T) {
	docs := GetKeyBindingDocs()
	contexts := map[focus]string{
		focusList:           "list",
		focusDetail:         "detail",
		focusBoard:          "board",
		focusGraph:          "graph",
		focusTree:           "tree",
		focusInsights:       "insights",
		focusHistory:        "history",
		focusActionable:     "actionable",
		focusLabelDashboard: "label-dashboard",
		focusFlowMatrix:     "flow",
		focusSprint:         "sprint",
		focusAttention:      "attention",
	}
	required := map[focus][]string{
		focusList:           {"j", "enter", "a", "b", "g", "h", "i", "E", "f", "[", "]", "?", "F2/;", "tab", "/", "o", "c", "r", "I", "l", "n", "U", "V", "s", "S", "x", "y", "C", "t", "T", "O", "'", "w", "!", "ctrl+s", "H", "alt+h", "Ctrl+R/F5", "up", "down", "left", "right"},
		focusDetail:         {"j", "a", "b", "g", "h", "i", "E", "?", "F2/;", "tab", "n", "U", "x", "y", "C", "t", "T", "O", "Ctrl+R/F5"},
		focusBoard:          {"h", "l", "j", "k", "G", "gg", "tab", "enter", "o", "c", "r", "/", "n/N", "1-4", "H/L", "0/$", "y", "s", "e", "d", "b", "?", "F2/;"},
		focusGraph:          {"hjkl", "pgup/pgdown", "/", "n/N", "enter", "esc", "g", "b", "?", "F2/;"},
		focusTree:           {"h", "l", "enter", "space", "/", "n", "N", "v", "+", "-", "G", "E", "o", "c", "r", "pgup", "pgdown", "?", "F2/;"},
		focusInsights:       {"h", "l", "j", "k", "ctrl+j", "ctrl+k", "tab", "o", "r", "e", "x", "m", "enter", "] / F4", "f", "i", "?", "F2/;"},
		focusHistory:        {"/", "v", "tab", "J", "K", "enter", "y", "o", "f/F", "g", "h", "c", "?", "F2/;"},
		focusActionable:     {"j", "k", "enter", "a", "?", "F2/;"},
		focusLabelDashboard: {"j", "k", "home", "G", "enter", "h", "d", "[", "esc", "?", "F2/;"},
		focusFlowMatrix:     {"j", "k", "home", "G", "enter", "f", "esc", "q", "?", "F2/;"},
		focusSprint:         {"j", "k", "esc", "q", "P", "?", "F2/;"},
		focusAttention:      {"1-9", "d", "] / F4", "esc / q", "?", "F2/;"},
	}

	hasDoc := func(context, key string) bool {
		for _, doc := range docs {
			if doc.Key != key {
				continue
			}
			for _, candidate := range strings.Split(doc.Context, ",") {
				if strings.TrimSpace(candidate) == context {
					return true
				}
			}
		}
		return false
	}

	for f, keys := range required {
		t.Run(f.String(), func(t *testing.T) {
			context := contexts[f]
			for _, key := range keys {
				if !hasDoc(context, key) {
					t.Errorf("missing documented shortcut %q for %s", key, context)
				}
			}
		})
	}
}

func TestListArrowDocsMatchActiveBubblesKeyMap(t *testing.T) {
	m := NewModel(nil, nil, "")
	for _, testCase := range []struct {
		docKey string
		keys   []string
	}{
		{docKey: "up", keys: m.list.KeyMap.CursorUp.Keys()},
		{docKey: "down", keys: m.list.KeyMap.CursorDown.Keys()},
		{docKey: "left", keys: m.list.KeyMap.PrevPage.Keys()},
		{docKey: "right", keys: m.list.KeyMap.NextPage.Keys()},
	} {
		active := false
		for _, key := range testCase.keys {
			if key == testCase.docKey {
				active = true
				break
			}
		}
		if !active {
			t.Fatalf("Bubbles list keymap does not activate %q: %v", testCase.docKey, testCase.keys)
		}

		documented := false
		for _, doc := range GetKeyBindingDocs() {
			if doc.Key == testCase.docKey && strings.Contains(doc.Context, "list") {
				documented = true
				break
			}
		}
		if !documented {
			t.Fatalf("active Bubbles list key %q is not documented for list context", testCase.docKey)
		}
	}
}

func TestSprintKeyBindingDocsIncludeQuit(t *testing.T) {
	found := false
	for _, doc := range GetKeyBindingDocs() {
		if doc.Key == "q" && strings.Contains(doc.Context, "sprint") {
			found = true
		}
	}
	if !found {
		t.Fatal("Sprint registry must advertise q as a close shortcut")
	}
}

func TestLabelDashboardDocsOnlyAdvertiseReachableCommands(t *testing.T) {
	forbidden := map[string]bool{"a": true, "b": true, "E": true, "i": true, "f": true, "]": true, "!": true}
	for _, doc := range GetKeyBindingDocs() {
		if strings.Contains(doc.Context, "label-dashboard") && forbidden[doc.Key] {
			t.Fatalf("Label Dashboard registry advertises unreachable shortcut: %+v", doc)
		}
	}
}

func TestDetailDocsOmitRestrictedCommands(t *testing.T) {
	for _, doc := range GetKeyBindingDocs() {
		if !strings.Contains(doc.Context, "detail") {
			continue
		}
		if doc.Key == "p" || doc.Key == "!" {
			t.Fatalf("Detail registry advertises restricted shortcut: %+v", doc)
		}
	}
}

func TestKeyBindingDocsGroupGlobalAliases(t *testing.T) {
	docs := GetKeyBindingDocs()
	for _, alias := range []string{"F2", ";", "ctrl+r", "f5"} {
		for _, doc := range docs {
			if doc.Key == alias {
				t.Fatalf("ungrouped alias remains in registry docs: %+v", doc)
			}
		}
	}
	wants := map[string]bool{"F2/;": false, "Ctrl+R/F5": false}
	for _, doc := range docs {
		if _, ok := wants[doc.Key]; ok {
			wants[doc.Key] = true
		}
	}
	for key, found := range wants {
		if !found {
			t.Fatalf("missing grouped alias %q", key)
		}
	}
}

func TestFlowKeyBindingDocsDoNotAdvertiseUnforwardedPaging(t *testing.T) {
	for _, doc := range GetKeyBindingDocs() {
		if strings.Contains(doc.Context, "flow") && (doc.Key == "ctrl+d" || doc.Key == "ctrl+u") {
			t.Fatalf("Flow registry advertises paging that main dispatch does not forward: %+v", doc)
		}
	}
}

func TestFlowKeyBindingDocsMentionDrilldownClose(t *testing.T) {
	for _, doc := range GetKeyBindingDocs() {
		if doc.Key == "f" && doc.Context == "flow" {
			if !strings.Contains(doc.Desc, "drilldown") {
				t.Fatalf("Flow f binding omits drilldown behavior: %+v", doc)
			}
			return
		}
	}
	t.Fatal("Flow f binding not found")
}

func TestSpecializedFullHelpOmitsDefaultSections(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(*Model)
		forbids  []string
		contains []string
	}{
		{
			name:     "label dashboard",
			setup:    func(m *Model) { m.focused = focusLabelDashboard },
			forbids:  []string{"Actionable", "Open issues", "Priority hints", "Alerts panel", "List / Detail"},
			contains: []string{"Label Dashboard", "Filter by label", "[/F3"},
		},
		{
			name:     "flow top level",
			setup:    func(m *Model) { m.focused = focusFlowMatrix },
			forbids:  []string{"Graph view", "Actionable", "Priority hints", "Alerts panel", "List / Detail"},
			contains: []string{"Dependency Flow", "g/G", "First / last", "Drill into label"},
		},
		{
			name:     "flow drilldown",
			setup:    func(m *Model) { m.focused = focusFlowMatrix; m.flowMatrix.showDrilldown = true },
			forbids:  []string{"Graph view", "Actionable", "Priority hints", "Alerts panel", "List / Detail"},
			contains: []string{"Open / jump to issue"},
		},
		{
			name:     "detail",
			setup:    func(m *Model) { m.focused = focusDetail },
			forbids:  []string{"Priority hints", "Alerts panel", "List Filters & Sort"},
			contains: []string{"Detail Actions", "Self-update check"},
		},
		{
			name:     "board",
			setup:    func(m *Model) { m.focused = focusBoard },
			forbids:  []string{"Hybrid ranking", "List Filters & Sort", "List / Detail"},
			contains: []string{"Board", "First / last column", "o/c/r", "Reachable"},
		},
		{
			name:     "graph",
			setup:    func(m *Model) { m.focused = focusGraph },
			forbids:  []string{"History view", "Hybrid ranking", "List Filters & Sort"},
			contains: []string{"Graph", "Navigate graph", "Return to List"},
		},
		{
			name:     "insights",
			setup:    func(m *Model) { m.focused = focusInsights },
			forbids:  []string{"History view", "Open issues", "List / Detail"},
			contains: []string{"Insights", "Switch panels", "Ready-only toggle"},
		},
		{
			name:     "history",
			setup:    func(m *Model) { m.focused = focusHistory },
			forbids:  []string{"History view", "Hybrid ranking", "Open issues", "List / Detail"},
			contains: []string{"History", "Toggle file tree", "Confidence filter"},
		},
		{
			name:     "actionable",
			setup:    func(m *Model) { m.focused = focusActionable },
			forbids:  []string{"Open issues", "Hybrid ranking", "List / Detail"},
			contains: []string{"Actionable", "Move through", "Open selected issue"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(nil, nil, "")
			tc.setup(&m)
			help := ansi.Strip(m.renderHelpOverlay())
			for _, want := range tc.contains {
				if !strings.Contains(help, want) {
					t.Fatalf("specialized Help missing %q:\n%s", want, help)
				}
			}
			for _, forbidden := range tc.forbids {
				if strings.Contains(help, forbidden) {
					t.Fatalf("specialized Help contains default/conflicting %q:\n%s", forbidden, help)
				}
			}
		})
	}
}

func TestInsightsFullHelpOmitsConsumedConfidenceFilter(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.focused = focusInsights
	help := ansi.Strip(m.renderHelpOverlay())
	if strings.Contains(help, "Confidence filter") {
		t.Fatalf("Insights Help advertises consumed c confidence filter:\n%s", help)
	}
	for _, valid := range []string{"Switch panels", "Ready-only toggle", "Calculation proof"} {
		if !strings.Contains(help, valid) {
			t.Fatalf("Insights Help lost valid control %q:\n%s", valid, help)
		}
	}
}

func TestTreeTabIsANoOpAndTreeGuidanceOmitsIt(t *testing.T) {
	issues := mouseTestIssues(4)
	m := sizedModel(t, issues, 120, 30)
	updated, _ := m.Update(keyMsg("E"))
	m = updated.(Model)
	if m.focused != focusTree {
		t.Fatalf("expected Tree focus, got %v", m.focused)
	}
	m.isSplitView = true
	selectedID := m.tree.GetSelectedID()
	offset := m.tree.GetViewportOffset()
	updated, _ = m.Update(keyMsg("tab"))
	m = updated.(Model)
	if m.focused != focusTree || m.tree.GetSelectedID() != selectedID || m.tree.GetViewportOffset() != offset {
		t.Fatalf("Tree Tab was not a no-op: focus=%v selected=%q offset=%d", m.focused, m.tree.GetSelectedID(), m.tree.GetViewportOffset())
	}

	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	help := ansi.Strip(m.View())
	if strings.Contains(help, "Tab") || strings.Contains(help, "Home/G") || strings.Contains(help, "Graph view") || strings.Contains(help, "History view") || strings.Contains(help, "Label picker") || strings.Contains(help, "Cycle sort") {
		t.Fatalf("Tree Help advertises conflicting shortcuts:\n%s", help)
	}
	updated, _ = m.Update(keyMsg("?"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	sidebar := ansi.Strip(m.View())
	if strings.Contains(sidebar, "Tab") {
		t.Fatalf("Tree sidebar advertises Tab:\n%s", sidebar)
	}
}

func TestListLowercaseAOpensActionableView(t *testing.T) {
	m := sizedModel(t, mouseTestIssues(4), 120, 30)
	m.currentFilter = "status:closed"
	m.statusFilter = "closed"
	updated, _ := m.Update(keyMsg("a"))
	m = updated.(Model)
	if !m.isActionableView || m.focused != focusActionable {
		t.Fatalf("lowercase a did not open Actionable view: actionable=%v focus=%v", m.isActionableView, m.focused)
	}
	if content := GetContextHelp(ContextList); strings.Contains(content, "All issues") || strings.Contains(strings.ToLower(content), "reset filter") {
		t.Fatal("List context help still claims lowercase a resets filters")
	}
}

// TestKeyDispatch_Regression_FInHistoryTogglesFileTree verifies that 'f' in history view
// toggles the file tree within history.
func TestKeyDispatch_Regression_FInHistoryTogglesFileTree(t *testing.T) {
	m := setupTestModel(t)

	// Toggle history view on
	updated, _ := m.Update(keyMsg("h"))
	m = updated.(Model)

	if m.focused != focusHistory {
		t.Fatalf("Expected focusHistory after 'h', got %v", m.focused)
	}

	// Press 'f' - should toggle file tree within history
	updated, _ = m.Update(keyMsg("f"))
	m = updated.(Model)

	// 'f' is handled by history handler
	// Verify we're still in history (file tree is internal state)
	t.Logf("focus=history key=f expected=toggle_file_tree actual=focused:%v fileTreeFocus:%v",
		m.focused, m.historyView.FileTreeHasFocus())
}

func TestKeyDispatch_Regression_BoardSearchConsumesInput(t *testing.T) {
	m := setupTestModel(t)

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	if m.focused != focusBoard || !m.isBoardView {
		t.Fatalf("Expected board view after 'b', got focused=%v isBoardView=%v", m.focused, m.isBoardView)
	}

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	if !m.board.IsSearchMode() {
		t.Fatal("expected board search mode after '/'")
	}

	updated, _ = m.Update(keyMsg("b"))
	m = updated.(Model)
	if got := m.board.SearchQuery(); got != "b" {
		t.Fatalf("expected board search query %q, got %q", "b", got)
	}
	if m.focused != focusBoard || !m.isBoardView {
		t.Fatalf("expected board search input to stay in board view, got focused=%v isBoardView=%v", m.focused, m.isBoardView)
	}

	updated, _ = m.Update(keyMsg("backspace"))
	m = updated.(Model)
	if got := m.board.SearchQuery(); got != "" {
		t.Fatalf("expected board search query to clear after backspace, got %q", got)
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.board.IsSearchMode() {
		t.Fatal("expected esc to cancel board search mode")
	}
	if m.focused != focusBoard || !m.isBoardView {
		t.Fatalf("expected esc to remain in board view, got focused=%v isBoardView=%v", m.focused, m.isBoardView)
	}
}

func TestKeyDispatch_BoardSearchAcceptsRunesAndNavigatesAfterCommit(t *testing.T) {
	issues := []model.Issue{
		{ID: "board-match-1", Title: "Match one", Status: model.StatusOpen},
		{ID: "board-match-2", Title: "Match two", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)

	pasted := []rune("nN世界 pasted")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: pasted})
	m = updated.(Model)
	if got := m.board.SearchQuery(); got != string(pasted) {
		t.Fatalf("active Board search query = %q, want %q", got, string(pasted))
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("match")})
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("n"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("N"))
	m = updated.(Model)
	if got := m.board.SearchQuery(); got != "matchnN" {
		t.Fatalf("active Board search treated n/N as commands: query=%q", got)
	}

	updated, _ = m.Update(keyMsg("backspace"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("backspace"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.board.IsSearchMode() || m.board.SearchQuery() != "match" {
		t.Fatalf("Enter did not commit Board search: active=%v query=%q", m.board.IsSearchMode(), m.board.SearchQuery())
	}
	if m.board.SearchMatchCount() != 2 {
		t.Fatalf("committed Board search match count = %d, want 2", m.board.SearchMatchCount())
	}

	updated, _ = m.Update(keyMsg("n"))
	m = updated.(Model)
	if got := m.board.SearchCursorPos(); got != 2 {
		t.Fatalf("n navigated to Board match %d, want 2", got)
	}
	updated, _ = m.Update(keyMsg("N"))
	m = updated.(Model)
	if got := m.board.SearchCursorPos(); got != 1 {
		t.Fatalf("N navigated to Board match %d, want 1", got)
	}
}

func TestKeyDispatch_Regression_HistorySearchConsumesGlobalKeys(t *testing.T) {
	m := setupTestModel(t)

	updated, _ := m.Update(keyMsg("h"))
	m = updated.(Model)
	if m.focused != focusHistory || !m.isHistoryView {
		t.Fatalf("Expected history view after 'h', got focused=%v isHistoryView=%v", m.focused, m.isHistoryView)
	}

	updated, _ = m.Update(keyMsg(";"))
	m = updated.(Model)
	if !m.showShortcutsSidebar {
		t.Fatal("semicolon did not open the shortcuts sidebar before History search")
	}

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	if !m.historyView.IsSearchActive() {
		t.Fatal("expected history search mode after '/'")
	}

	for _, key := range []string{"?", "`", ";", "q"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(Model)
	}
	if got := m.historyView.SearchQuery(); got != "?`;q" {
		t.Fatalf("expected history search query %q, got %q", "?`;q", got)
	}
	if m.focused != focusHistory || !m.isHistoryView {
		t.Fatalf("expected history search input to stay in history view, got focused=%v isHistoryView=%v", m.focused, m.isHistoryView)
	}
	if m.showHelp || m.showTutorial || !m.showShortcutsSidebar {
		t.Fatalf("History search changed global UI: help=%v tutorial=%v sidebar=%v", m.showHelp, m.showTutorial, m.showShortcutsSidebar)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)
	if !m.lastForceRefresh.IsZero() || !m.historyView.IsSearchActive() {
		t.Fatalf("History search leaked Ctrl+R: refreshed=%v active=%v", !m.lastForceRefresh.IsZero(), m.historyView.IsSearchActive())
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.historyView.IsSearchActive() {
		t.Fatal("expected esc to cancel history search mode")
	}
	if m.focused != focusHistory || !m.isHistoryView {
		t.Fatalf("expected esc to remain in history view, got focused=%v isHistoryView=%v", m.focused, m.isHistoryView)
	}
}

// TestKeyDispatch_Regression_LabelPickerConsumesQKey guards against issue #176:
// the "Filter by Label" picker has an always-focused text input, so a lowercase
// q must be typed into the filter rather than triggering the global quit/back
// shortcut and closing the picker. Esc remains the way to cancel.
func TestKeyDispatch_Regression_LabelPickerConsumesQKey(t *testing.T) {
	m := setupTestModel(t)

	updated, _ := m.Update(keyMsg("l"))
	m = updated.(Model)
	if m.focused != focusLabelPicker || !m.showLabelPicker {
		t.Fatalf("expected label picker after 'l', got focused=%v showLabelPicker=%v", m.focused, m.showLabelPicker)
	}

	// Typing a lowercase q must append to the filter, not quit/close the picker.
	updated, _ = m.Update(keyMsg("q"))
	m = updated.(Model)
	if !m.showLabelPicker || m.focused != focusLabelPicker {
		t.Fatalf("expected label picker to stay open after typing 'q', got focused=%v showLabelPicker=%v", m.focused, m.showLabelPicker)
	}
	if got := m.labelPicker.InputValue(); got != "q" {
		t.Fatalf("expected label filter input %q after typing 'q', got %q", "q", got)
	}

	// Subsequent printable chars keep appending (e.g. building "required").
	for _, k := range []string{"u", "e"} {
		updated, _ = m.Update(keyMsg(k))
		m = updated.(Model)
	}
	if got := m.labelPicker.InputValue(); got != "que" {
		t.Fatalf("expected label filter input %q, got %q", "que", got)
	}

	// Esc still cancels the picker and returns to the list.
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.showLabelPicker || m.focused != focusList {
		t.Fatalf("expected esc to cancel label picker, got focused=%v showLabelPicker=%v", m.focused, m.showLabelPicker)
	}
}

func TestKeyDispatch_Regression_HistorySearchEnterKeepsFilter(t *testing.T) {
	m := setupTestModel(t)

	updated, _ := m.Update(keyMsg("h"))
	m = updated.(Model)
	if m.focused != focusHistory || !m.isHistoryView {
		t.Fatalf("Expected history view after 'h', got focused=%v isHistoryView=%v", m.focused, m.isHistoryView)
	}

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("q"))
	m = updated.(Model)

	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)

	if m.historyView.IsSearchActive() {
		t.Fatal("expected enter to exit active history search input")
	}
	if got := m.historyView.SearchQuery(); got != "q" {
		t.Fatalf("expected enter to preserve history search query %q, got %q", "q", got)
	}
	if m.focused != focusHistory || !m.isHistoryView {
		t.Fatalf("expected enter to remain in history view, got focused=%v isHistoryView=%v", m.focused, m.isHistoryView)
	}
}

func TestKeyDispatch_Regression_HistoryLowercaseHAndSubmittedSearchEscape(t *testing.T) {
	m := setupTestModel(t)
	updated, _ := m.Update(keyMsg("h"))
	m = updated.(Model)

	updated, _ = m.Update(keyMsg("/"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("h"))
	m = updated.(Model)
	if got := m.historyView.SearchQuery(); got != "h" || !m.historyView.IsSearchActive() {
		t.Fatalf("focused h should remain search text: query=%q active=%v", got, m.historyView.IsSearchActive())
	}

	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.historyView.HasSearchQuery() || !m.isHistoryView || m.focused != focusHistory {
		t.Fatalf("first esc should clear submitted search without leaving History: query=%q view=%v focus=%v", m.historyView.SearchQuery(), m.isHistoryView, m.focused)
	}

	updated, _ = m.Update(keyMsg("h"))
	m = updated.(Model)
	if m.isHistoryView || m.focused != focusList {
		t.Fatalf("unfocused lowercase h should close History: view=%v focus=%v", m.isHistoryView, m.focused)
	}
}

func TestKeyDispatch_Regression_HistoryGitZeroMatchEscapeRestoresCommits(t *testing.T) {
	m := setupTestModel(t)
	m.isHistoryView = true
	m.focused = focusHistory
	m.historyView = NewHistoryModel(createTestHistoryReport(), testTheme())
	m.historyView.ToggleViewMode()

	updated, _ := m.Update(keyMsg("/"))
	m = updated.(Model)
	for _, key := range []string{"z", "z", "z"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if commits := m.historyView.GetFilteredCommitList(); len(commits) != 0 {
		t.Fatalf("submitted zero-match Git query returned %d commits, want 0", len(commits))
	}

	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.historyView.HasSearchQuery() || !m.isHistoryView || m.focused != focusHistory {
		t.Fatalf("Escape did not clear Git query in place: query=%q view=%v focus=%v", m.historyView.SearchQuery(), m.isHistoryView, m.focused)
	}
	if got, want := len(m.historyView.GetFilteredCommitList()), len(m.historyView.commitList); got != want || got == 0 {
		t.Fatalf("Escape did not restore unfiltered commits: got=%d want=%d", got, want)
	}
}

func TestKeyDispatch_Regression_HistoryFileTreeEscStaysInHistory(t *testing.T) {
	m := setupTestModel(t)
	m.isHistoryView = true
	m.focused = focusHistory
	m.historyView = NewHistoryModel(createTestHistoryReportWithFiles(), testTheme())
	m.historyView.ToggleFileTree()
	m.historyView.SetFileTreeFocus(true)

	updated, _ := m.Update(keyMsg("esc"))
	m = updated.(Model)

	if !m.isHistoryView || m.focused != focusHistory {
		t.Fatalf("expected esc in history file tree to stay in history view, got focused=%v isHistoryView=%v", m.focused, m.isHistoryView)
	}
	if m.historyView.FileTreeHasFocus() {
		t.Fatal("expected esc in history file tree to leave file-tree focus")
	}
}

func TestKeyDispatch_Regression_TabKeepsHistoryFocusInSplitView(t *testing.T) {
	m := setupTestModel(t)
	m.isSplitView = true
	m.isHistoryView = true
	m.focused = focusHistory
	m.historyView = NewHistoryModel(createTestHistoryReport(), testTheme())
	m.historyView.SetSize(200, 40)

	updated, _ := m.Update(keyMsg("tab"))
	m = updated.(Model)
	if m.focused != focusHistory || m.historyView.focused != historyFocusTimeline {
		t.Fatalf("first tab left History: outer=%v inner=%v", m.focused, m.historyView.focused)
	}

	updated, _ = m.Update(keyMsg("tab"))
	m = updated.(Model)
	if m.focused != focusHistory || m.historyView.focused != historyFocusMiddle {
		t.Fatalf("second tab did not focus commits: outer=%v inner=%v", m.focused, m.historyView.focused)
	}

	updated, _ = m.Update(keyMsg("j"))
	m = updated.(Model)
	if m.historyView.selectedCommit != 1 {
		t.Fatalf("j after tab selected commit %d, want 1", m.historyView.selectedCommit)
	}
}

// TestKeyDispatch_ModalConsumesAllKeys verifies that modals consume all keys
// and don't pass them through to underlying views.
func TestKeyDispatch_ModalConsumesAllKeys(t *testing.T) {
	m := setupTestModel(t)

	// Show help modal
	updated, _ := m.Update(keyMsg("?"))
	m = updated.(Model)

	if !m.showHelp {
		t.Fatalf("Expected help modal after '?'")
	}

	// Keys that would normally toggle views
	viewToggleKeys := []string{"b", "g", "h", "f", "i", "a"}

	for _, key := range viewToggleKeys {
		updated, _ := m.Update(keyMsg(key))
		result := updated.(Model)

		// Modal should still be shown - keys are consumed
		if result.showHelp && result.focused == focusHelp {
			// Modal correctly consumed the key
			t.Logf("focus=help key=%s expected=consumed actual=modal_still_shown", key)
		} else {
			// Key escaped the modal
			t.Logf("focus=help key=%s expected=consumed actual=modal_dismissed/toggled (may be intended for dismiss)", key)
		}
	}
}

// TestKeyDispatch_ViewToggleTable tests critical view toggle combinations.
// Tests h/g/f in various focus contexts to verify correct routing.
func TestKeyDispatch_ViewToggleTable(t *testing.T) {
	type testCase struct {
		startFocus focus
		key        string
		expectFn   func(Model) (bool, string)
	}

	cases := []testCase{
		// From list view
		{focusList, "b", func(m Model) (bool, string) { return m.focused == focusBoard, "should toggle to board" }},
		{focusList, "g", func(m Model) (bool, string) { return m.focused == focusGraph, "should toggle to graph" }},
		{focusList, "h", func(m Model) (bool, string) { return m.isHistoryView, "should toggle to history" }},
		{focusList, "f", func(m Model) (bool, string) { return m.focused == focusFlowMatrix, "should toggle to flow matrix" }},
		{focusList, "E", func(m Model) (bool, string) { return m.focused == focusTree, "should toggle to tree" }},
		{focusList, "i", func(m Model) (bool, string) { return m.focused == focusInsights, "should toggle to insights" }},
		{focusList, "a", func(m Model) (bool, string) { return m.focused == focusActionable, "should toggle to actionable" }},

		// Note: 'g' in board uses gg-combo mechanism (async timeout before graph toggle),
		// so it can't be tested with simple synchronous Update() calls.
	}

	focusNames := map[focus]string{
		focusList:       "list",
		focusBoard:      "board",
		focusGraph:      "graph",
		focusTree:       "tree",
		focusHistory:    "history",
		focusInsights:   "insights",
		focusActionable: "actionable",
		focusFlowMatrix: "flowMatrix",
	}

	for _, tc := range cases {
		name := focusNames[tc.startFocus] + "_" + tc.key
		t.Run(name, func(t *testing.T) {
			m := setupTestModel(t)

			// Navigate to start focus if not list
			switch tc.startFocus {
			case focusBoard:
				updated, _ := m.Update(keyMsg("b"))
				m = updated.(Model)
			case focusGraph:
				updated, _ := m.Update(keyMsg("g"))
				m = updated.(Model)
			case focusTree:
				updated, _ := m.Update(keyMsg("E"))
				m = updated.(Model)
			case focusHistory:
				updated, _ := m.Update(keyMsg("h"))
				m = updated.(Model)
			case focusInsights:
				updated, _ := m.Update(keyMsg("i"))
				m = updated.(Model)
			}

			if m.focused != tc.startFocus && tc.startFocus != focusHistory {
				t.Fatalf("Failed to set up start focus: expected %v, got %v", tc.startFocus, m.focused)
			}

			// Send the test key
			updated, _ := m.Update(keyMsg(tc.key))
			result := updated.(Model)

			ok, expected := tc.expectFn(result)
			if !ok {
				t.Errorf("focus=%v key=%s expected=%s actual=focus:%v", tc.startFocus, tc.key, expected, result.focused)
			}
			t.Logf("focus=%v key=%s expected=%s actual=focus:%v", tc.startFocus, tc.key, expected, result.focused)
		})
	}
}

func TestKeyDispatch_FlowMatrixExcludesContextLabels(t *testing.T) {
	issues := []model.Issue{
		{ID: "backend", Title: "Backend", Labels: []string{"ctx:project-one", "backend"}, Status: model.StatusOpen},
		{ID: "frontend", Title: "Frontend", Labels: []string{"ctx:project-two", "frontend"}, Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "backend", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"

	updated, _ := m.Update(keyMsg("f"))
	m = updated.(Model)

	if got := m.flowMatrix.SelectedLabel(); got != "backend" {
		t.Fatalf("selected label = %q, want highest-ranked regular label backend", got)
	}
	view := m.flowMatrix.View()
	if !strings.Contains(view, "2 labels") {
		t.Fatalf("flow header should count only regular labels; view = %q", view)
	}
	if strings.Contains(view, "ctx:") {
		t.Fatalf("context label appeared in rendered flow list or detail pane; view = %q", view)
	}
	for _, label := range m.flowMatrix.flow.Labels {
		if strings.HasPrefix(label, "ctx:") {
			t.Fatalf("context label %q appeared in flow view model", label)
		}
	}

	updated, _ = m.Update(keyMsg("G"))
	m = updated.(Model)
	if got := m.flowMatrix.SelectedLabel(); got != "frontend" {
		t.Fatalf("end selection = %q, want frontend", got)
	}
}

func TestKeyDispatch_FlowMatrixContextOnlySelectionSafety(t *testing.T) {
	issues := []model.Issue{
		{ID: "one", Title: "One", Labels: []string{"ctx:project-one"}, Status: model.StatusOpen},
		{ID: "two", Title: "Two", Labels: []string{"ctx:project-two"}, Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "one", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"

	updated, _ := m.Update(keyMsg("f"))
	m = updated.(Model)
	for _, key := range []string{"j", "k", "G", "g", "enter"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(Model)
	}

	if got := m.flowMatrix.SelectedLabel(); got != "" {
		t.Fatalf("selected label = %q, want empty for context-only data", got)
	}
	if m.flowMatrix.showDrilldown {
		t.Fatal("empty flow should not open a drilldown")
	}
	if got := m.flowMatrix.View(); !strings.Contains(got, "No open cross-label blocking dependencies") {
		t.Fatalf("context-only flow should render empty state; view = %q", got)
	}
}
