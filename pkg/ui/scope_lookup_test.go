package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func lookupIssue(id, title string) IssueLookupResult {
	return IssueLookupResult{Issue: model.Issue{ID: id, Title: title, Status: model.StatusOpen, Priority: 2, IssueType: model.TypeTask}}
}

func TestScopeCtrlGLookupIsHubOnlyAndOwnsInput(t *testing.T) {
	lookups := 0
	m := NewModel(nil, nil, "", RuntimeServices{RepositoryPresentation: true, Scopes: ScopeServices{
		LookupIssue: func(context.Context, string) (IssueLookupResult, error) {
			lookups++
			return lookupIssue("lookup-1", "Found"), nil
		},
	}})
	m.showScopePicker = true
	m.focused = focusGlobalIssues
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(*Model)
	if !m.showIssueLookup || m.focused != focusIssueLookup {
		t.Fatalf("Ctrl+G state: shown=%t focus=%s", m.showIssueLookup, m.focused)
	}
	m.issueLookupInput.SetValue("lookup-1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if cmd == nil || !m.issueLookupLoading {
		t.Fatalf("lookup start: cmd=%t loading=%t", cmd != nil, m.issueLookupLoading)
	}
	updated, resultCmd := m.Update(cmd())
	m = updated.(*Model)
	for _, message := range runUISemanticCommands(resultCmd) {
		updated, _ = m.Update(message)
		m = updated.(*Model)
	}
	if lookups != 1 || m.issueLookupResult == nil || m.issueLookupResult.Issue.Title != "Found" {
		t.Fatalf("lookup result: calls=%d result=%#v", lookups, m.issueLookupResult)
	}
	if m.focused != focusIssueLookup {
		t.Fatalf("unscoped result left overlay focus at %s", m.focused)
	}
	if got := m.CurrentContext(); got != ContextIssueLookup {
		t.Fatalf("context=%s, want issue lookup", got)
	}
	local := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LookupIssue: func(context.Context, string) (IssueLookupResult, error) { return lookupIssue("lookup-1", "Found"), nil },
	}})
	local.showScopePicker = true
	local.focused = focusScopePicker
	updated, _ = local.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if updated.(*Model).showIssueLookup {
		t.Fatal("Ctrl+G opened lookup outside interactive Hub mode")
	}
}

func TestIssueLookupScopedResultAddsTransientScopeAndLoadsFirstMembersPage(t *testing.T) {
	queries := 0
	var query ScopeMembersQuery
	scope := ScopeInfo{ID: "scope-2", Name: "Later"}
	m := NewModel(nil, nil, "", RuntimeServices{RepositoryPresentation: true, Scopes: ScopeServices{
		LookupIssue: func(context.Context, string) (IssueLookupResult, error) {
			result := lookupIssue("lookup-1", "Found")
			result.Scope = &scope
			return result, nil
		},
		QueryMembers: func(_ context.Context, got ScopeMembersQuery) (ScopeMembersPage, error) {
			queries++
			query = got
			return ScopeMembersPage{Scope: scope}, nil
		},
	}})
	m.showScopePicker = true
	m.focused = focusGlobalIssues
	m.scopeCatalog = []ScopeInfo{{ID: "scope-1", Name: "Today"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.openLookupForTest(t)
	m.issueLookupInput.SetValue("lookup-1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	updated, resultCmd := m.Update(cmd())
	m = updated.(*Model)
	for _, message := range runUISemanticCommands(resultCmd) {
		updated, _ = m.Update(message)
		m = updated.(*Model)
	}
	if m.scopePicker.SelectedScopeID() != "scope-1" || queries != 0 || m.focused != focusIssueLookup {
		t.Fatalf("lookup result navigated before explicit Enter: scope=%q queries=%d focus=%s", m.scopePicker.SelectedScopeID(), queries, m.focused)
	}
	updated, resultCmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	for _, message := range runUISemanticCommands(resultCmd) {
		updated, _ = m.Update(message)
		m = updated.(*Model)
	}
	if m.showIssueLookup || m.CurrentContext() == ContextIssueLookup || !m.showScopePicker || m.scopePicker.SelectedScopeID() != "scope-2" || m.focused != focusScopePicker {
		t.Fatalf("scoped lookup state: shown=%t context=%s scopeShown=%t scope=%q focus=%s", m.showIssueLookup, m.CurrentContext(), m.showScopePicker, m.scopePicker.SelectedScopeID(), m.focused)
	}
	if queries != 1 || query.ScopeID != "scope-2" || query.Cursor != "" || query.Limit != scopePageSize {
		t.Fatalf("member query count=%d query=%#v", queries, query)
	}
	if m.activeScope != nil {
		t.Fatal("lookup activated a scope")
	}
}

func TestIssueLookupSelectedScopeSelectsOnlyLoadedMember(t *testing.T) {
	queries := 0
	m := NewModel(nil, nil, "", RuntimeServices{RepositoryPresentation: true, Scopes: ScopeServices{
		LookupIssue: func(context.Context, string) (IssueLookupResult, error) {
			result := lookupIssue("lookup-1", "Found")
			result.Scope = &ScopeInfo{ID: "scope-1", Name: "Today"}
			return result, nil
		},
		QueryMembers: func(context.Context, ScopeMembersQuery) (ScopeMembersPage, error) {
			queries++
			return ScopeMembersPage{}, nil
		},
	}})
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopeCatalog = []ScopeInfo{{ID: "scope-1", Name: "Today"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.scopePicker.memberScopeID = "scope-1"
	m.scopePicker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "other-1", Title: "Other"}},
		{Issue: model.Issue{ID: "lookup-1", Title: "Found"}},
	})
	m.scopePicker.SelectMemberByID("other-1")
	beforeMember := m.scopePicker.memberSelectedID
	m.openLookupForTest(t)
	m.issueLookupInput.SetValue("lookup-1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if m.scopePicker.memberSelectedID != beforeMember || queries != 0 || m.focused != focusIssueLookup {
		t.Fatalf("loaded member was selected before explicit Enter: selected=%q queries=%d focus=%s", m.scopePicker.memberSelectedID, queries, m.focused)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if queries != 0 || m.scopePicker.memberSelectedID != "lookup-1" {
		t.Fatalf("loaded member selection: queries=%d selected=%q", queries, m.scopePicker.memberSelectedID)
	}
}

func TestIssueLookupEscapeRejectsLateResponse(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{RepositoryPresentation: true, Scopes: ScopeServices{
		LookupIssue: func(context.Context, string) (IssueLookupResult, error) { return lookupIssue("lookup-1", "Found"), nil },
	}})
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.openLookupForTest(t)
	m.issueLookupInput.SetValue("lookup-1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	generation := m.issueLookupGeneration
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = updated.(*Model)
	if !m.showIssueLookup || m.focused != focusIssueLookup || !m.showScopePicker {
		t.Fatalf("uppercase B escaped lookup overlay: shown=%t focus=%s scope=%t", m.showIssueLookup, m.focused, m.showScopePicker)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.showIssueLookup || m.focused != focusScopePicker {
		t.Fatalf("escape state: shown=%t focus=%s", m.showIssueLookup, m.focused)
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if m.issueLookupResult != nil || m.showIssueLookup {
		t.Fatalf("late response changed closed overlay: result=%#v generation=%d", m.issueLookupResult, generation)
	}
}

func TestIssueLookupErrorLeavesScopePanelStateIntact(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{RepositoryPresentation: true, Scopes: ScopeServices{
		LookupIssue: func(context.Context, string) (IssueLookupResult, error) {
			return IssueLookupResult{}, errors.New("not found")
		},
	}})
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopeCatalog = []ScopeInfo{{ID: "scope-1", Name: "Today"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	active := ScopeInfo{ID: "scope-1", Name: "Today", Active: true}
	m.activeScope = &active
	m.openLookupForTest(t)
	m.issueLookupInput.SetValue("missing-1")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if m.issueLookupError != "not found" || m.scopePicker.SelectedScopeID() != "scope-1" || m.activeScope.ID != "scope-1" {
		t.Fatalf("lookup error changed scope state: error=%q selected=%q active=%#v", m.issueLookupError, m.scopePicker.SelectedScopeID(), m.activeScope)
	}
	if !strings.Contains(m.renderIssueLookup(), "not found") {
		t.Fatalf("lookup error is not rendered: %q", m.renderIssueLookup())
	}
}

func (m *Model) openLookupForTest(t *testing.T) {
	t.Helper()
	if cmd := m.openIssueLookup(); cmd == nil && !m.showIssueLookup {
		t.Fatal("lookup overlay did not open")
	}
}
