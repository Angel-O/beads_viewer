package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
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

func TestActiveScopeBadgeIsCompact(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
	}})
	m.activeScope = &ScopeInfo{Name: "Today", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 7, Active: true}

	badge := strings.TrimSpace(ansi.Strip(m.renderScopeBadge()))
	if badge != "Today · 7/100" {
		t.Fatalf("active scope badge = %q, want %q", badge, "Today · 7/100")
	}
}

func TestScopePickerEnterTogglesActiveScopeAndPreservesInactiveActivation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active bool
	}{
		{name: "active deactivates", active: true},
		{name: "inactive activates", active: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			activations, deactivations := 0, 0
			activatedID := ""
			m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
				Activate:   func(_ context.Context, id string) error { activations++; activatedID = id; return nil },
				Deactivate: func(context.Context) error { deactivations++; return nil },
			}})
			m.showScopePicker = true
			m.scopePickerOrigin = focusList
			m.focused = focusScopePicker
			m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today", Active: true}, {ID: "s2", Name: "Later"}})
			active := ScopeInfo{ID: "s1", Name: "Today", Active: true}
			m.activeScope = &active
			if !tc.active {
				m.scopePicker.Move(1)
			}

			updated, cmd := m.Update(keyMsg("enter"))
			m = updated.(*Model)
			if cmd == nil {
				t.Fatal("enter did not start scope mutation")
			}
			updated, _ = m.Update(cmd())
			m = updated.(*Model)
			if m.showScopePicker || m.focused != focusList {
				t.Fatalf("scope picker state: shown=%t focus=%s", m.showScopePicker, m.focused)
			}
			if tc.active {
				if deactivations != 1 || activations != 0 {
					t.Fatalf("active Enter calls: activate=%d deactivate=%d", activations, deactivations)
				}
				updated, _ = m.Update(scopeSnapshotMsg{snapshot: ScopeSnapshot{Scopes: []ScopeInfo{{ID: "s1", Name: "Today"}}}})
				m = updated.(*Model)
				if m.activeScope != nil {
					t.Fatalf("active scope = %#v, want nil after deactivation", m.activeScope)
				}
			} else if activations != 1 || deactivations != 0 || activatedID != "s2" {
				t.Fatalf("inactive Enter calls: activate=%d deactivate=%d id=%q", activations, deactivations, activatedID)
			}
		})
	}
}

func TestScopePickerViewOmitsLocalHintsAndSpacesHeader(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(80, 20)
	picker.SetScopes([]ScopeInfo{{Name: "Today", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 2}})
	view := ansi.Strip(picker.View())
	lines := strings.Split(view, "\n")
	header, entry := -1, -1
	for index, line := range lines {
		switch {
		case strings.Contains(line, "Scopes"):
			header = index
		case strings.Contains(line, "> Today · 2026-01-02/2"):
			entry = index
		}
	}
	if header < 0 || entry < 0 || entry != header+2 {
		t.Fatalf("scope header spacing missing:\n%s", view)
	}
	for _, hint := range []string{"enter activate", "n new scope", "esc back", "enter move bead"} {
		if strings.Contains(view, hint) {
			t.Fatalf("scope picker retained local hint %q:\n%s", hint, view)
		}
	}
}

func TestBacklogContextPickerIsolatedFromGenericScope(t *testing.T) {
	var got BacklogQuery
	m := NewModel([]model.Issue{
		{ID: "alpha-item", Labels: []string{"ctx:alpha"}},
		{ID: "beta-item", Labels: []string{"ctx:beta"}},
	}, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			got = query
			return BacklogPage{Issues: []model.Issue{{ID: "beta-item", Labels: []string{"ctx:beta"}}}}, nil
		},
	}})
	m.hubRepositoryMode = true
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha", "ctx:beta")
	generic, err := hub.NewSelectedContextsHubScope([]string{"ctx:alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(generic); err != nil {
		t.Fatal(err)
	}
	wantGenericIDs := visibleIssueIDs(m)
	wantGenericScope := m.HubScope()
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old"}}, HasMore: true, NextCursor: "old-cursor"}, 1)
	m.backlog.ToggleMark()
	m.backlogPageGeneration = 4

	updated, _ := m.Update(keyMsg("w"))
	m = updated.(*Model)
	if !m.showRepoPicker || m.repoPickerOrigin != focusBacklog {
		t.Fatalf("backlog context picker state: shown=%v origin=%v", m.showRepoPicker, m.repoPickerOrigin)
	}
	m.repoPicker.ClearSelection()
	m.repoPicker.MoveDown()
	m.repoPicker.ToggleSelected()
	m.repoPicker.MoveUp()
	m.repoPicker.ToggleSelected()
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("backlog context apply did not reload")
	}
	for _, msg := range runUISemanticCommands(cmd) {
		updated, _ = m.Update(msg)
		m = updated.(*Model)
	}

	if !reflect.DeepEqual(m.HubScope(), wantGenericScope) || !reflect.DeepEqual(visibleIssueIDs(m), wantGenericIDs) {
		t.Fatalf("backlog apply changed generic scope/list: scope=%#v ids=%v", m.HubScope(), visibleIssueIDs(m))
	}
	if got.Contexts == nil || !reflect.DeepEqual(got.Contexts, []string{"ctx:alpha"}) || !got.IncludeContextless {
		t.Fatalf("backlog context query=%#v, want alpha plus contextless", got)
	}
	if m.showRepoPicker || m.focused != focusBacklog || m.backlog.PageIndex() != 0 || m.backlog.CurrentPageCursor() != "" || m.backlogPageGeneration != 5 || m.backlog.MarkCount() != 0 {
		t.Fatalf("backlog apply state: picker=%v focus=%v page=%d cursor=%q generation=%d marks=%d", m.showRepoPicker, m.focused, m.backlog.PageIndex(), m.backlog.CurrentPageCursor(), m.backlogPageGeneration, m.backlog.MarkCount())
	}
	m.backlog.SetSize(80, 12)
	if view := ansi.Strip(m.backlog.View()); !strings.Contains(view, "contexts: ctx:alpha, no-context") {
		t.Fatalf("backlog heading omitted active context: %s", view)
	}
}

func TestBacklogContextQueryUsesAllForEmptyDraftAndContextlessOption(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.repositoryCatalog = hubScopeCatalog("ctx:alpha")
	if got := m.backlogQuery(""); !reflect.DeepEqual(got.Contexts, []string(nil)) || got.IncludeContextless {
		t.Fatalf("empty backlog context query=%#v, want all", got)
	}
	m.backlog.SetContextFilter(nil, true, nil)
	got := m.backlogQuery("")
	if got.Contexts != nil || !got.IncludeContextless {
		t.Fatalf("contextless backlog query=%#v, want contextless-only", got)
	}
}

func TestBacklogPageIndicatorIsOnlyCenteredPageNumber(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(60, 10)
	b.SetPage(BacklogPage{}, 2)
	page := ansi.Strip(b.renderBacklogPage(40))
	if strings.TrimSpace(page) != "page 3" || strings.Contains(page, "·") {
		t.Fatalf("page indicator=%q, want only page 3", page)
	}
	if left := strings.Index(page, "page 3"); left != (40-lipgloss.Width("page 3"))/2 {
		t.Fatalf("page indicator offset=%d, want centered offset=%d: %q", left, (40-lipgloss.Width("page 3"))/2, page)
	}
}

func TestBacklogPageIndicatorUsesTheTableWidth(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(120, 12)
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "b-1", Title: "Readable title", Status: model.StatusOpen,
		IssueType: model.TypeTask, Priority: 1,
	}}}, 0)

	contentWidth := b.width - 4
	columns := backlogTableColumnsFor(b.filteredItems, contentWidth)
	listWidth := backlogTableWidth(columns)
	view := ansi.Strip(b.View())
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "page 1") {
			want := 2 + (listWidth-lipgloss.Width("page 1"))/2
			if got := strings.Index(line, "page 1"); got != want {
				t.Fatalf("page offset=%d, want %d under list width %d: %q", got, want, listWidth, line)
			}
			return
		}
	}
	t.Fatalf("backlog view omitted page indicator:\n%s", view)
}

func TestScopePickerFromBacklogReturnsToListAndReopensBacklog(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load:        func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
		LoadBacklog: func(context.Context, string, int) (BacklogPage, error) { return BacklogPage{}, nil },
	}})
	m.isBacklogView, m.focused = true, focusBacklog

	updated, _ := m.Update(keyMsg("W"))
	m = updated.(*Model)
	if !m.showScopePicker || m.isBacklogView || m.scopePickerOrigin != focusList {
		t.Fatalf("W transition: picker=%t backlog=%t origin=%s", m.showScopePicker, m.isBacklogView, m.scopePickerOrigin)
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(*Model)
	if m.showScopePicker || m.isBacklogView || m.focused != focusList {
		t.Fatalf("Esc transition: picker=%t backlog=%t focus=%s", m.showScopePicker, m.isBacklogView, m.focused)
	}
	updated, cmd := m.Update(keyMsg("B"))
	m = updated.(*Model)
	if !m.isBacklogView || m.focused != focusBacklog || cmd == nil {
		t.Fatalf("B transition: backlog=%t focus=%s cmd=%t", m.isBacklogView, m.focused, cmd != nil)
	}
}

func TestScopePickerFramesStayBoundedAndFollowTabFocus(t *testing.T) {
	profile := lipgloss.DefaultRenderer().ColorProfile()
	defer lipgloss.SetColorProfile(profile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(80, 20)
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "b-1", Title: "Member"}}})
	catalogFocused := picker.View()
	picker.memberFocused = true
	membersFocused := picker.View()
	if lipgloss.Width(catalogFocused) > 80 || lipgloss.Height(catalogFocused) > 20 || lipgloss.Width(membersFocused) > 80 || lipgloss.Height(membersFocused) > 20 {
		t.Fatalf("scope picker exceeded assigned bounds")
	}
	if strings.Count(catalogFocused, "╭") != 2 || strings.Count(membersFocused, "╭") != 2 {
		t.Fatalf("scope picker did not frame both panels")
	}
	if catalogFocused == membersFocused {
		t.Fatal("Tab focus did not change panel presentation")
	}
}

func TestScopePickerHeight18ShowsMemberRowInsideFrame(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(100, 18)
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1", Title: "Visible member", Status: model.StatusOpen}}})
	picker.memberFocused = true
	view := picker.View()
	if !strings.Contains(ansi.Strip(view), "member-1") {
		t.Fatalf("height-18 member panel clipped its first row:\n%s", view)
	}
	if lipgloss.Height(view) > 18 || lipgloss.Width(view) > 100 {
		t.Fatalf("height-18 picker exceeded bounds: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}
}

func TestScopePickerHeight18UsesInnerPaddingAndBottomFrame(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(100, 18)
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1", Title: "Visible member", Status: model.StatusOpen}}})
	view := ansi.Strip(picker.View())
	lines := strings.Split(view, "\n")
	lastBottom := -1
	for index, line := range lines {
		if strings.Contains(line, "╰") {
			lastBottom = index
		}
	}
	if lastBottom < len(lines)-2 {
		t.Fatalf("members frame ended too far above bottom (line %d of %d):\n%s", lastBottom, len(lines), view)
	}
	if !strings.Contains(view, "│ Scopes") || !strings.Contains(view, "│ Members · Today") {
		t.Fatalf("frame content did not retain one-cell horizontal padding:\n%s", view)
	}
}

func TestScopePickerFullWidthRowsAndFramesKeepAssignedHeight(t *testing.T) {
	const width, height = 100, 18
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(width, height)
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1", Title: "Visible member", Status: model.StatusOpen}}})

	view := ansi.Strip(picker.View())
	if lipgloss.Width(view) != width || lipgloss.Height(view) != height {
		t.Fatalf("scope picker dimensions = %dx%d, want %dx%d", lipgloss.Width(view), lipgloss.Height(view), width, height)
	}
	for _, line := range strings.Split(view, "\n") {
		left, right := strings.IndexRune(line, '╭'), strings.LastIndex(line, "╮")
		if left >= 0 && right > left && lipgloss.Width(line[left:right+len("╮")]) != width-4 {
			t.Fatalf("panel frame width = %d, want %d: %q", lipgloss.Width(line[left:right+len("╮")]), width-4, line)
		}
	}
	memberView := ansi.Strip(picker.renderMembers(width-8, 6))
	for _, line := range strings.Split(memberView, "\n") {
		if strings.Contains(line, "member-1") && lipgloss.Width(line) != width-8 {
			t.Fatalf("member row width = %d, want %d: %q", lipgloss.Width(line), width-8, line)
		}
	}
}

func TestScopeMemberRowsUseLocalBoundedOrderAndLabels(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	now := time.Now()
	picker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "short", Title: "A deliberately long member title that must truncate", Status: model.StatusOpen, IssueType: model.TypeTask, Priority: 1, CreatedAt: now.Add(-2 * time.Hour), Labels: []string{"ctx:one", "backend"}}, RepositoryName: "one", RepositoryExtra: 1, HubPresentation: true, PresentationLabels: []string{"ctx:one", "backend"}},
		{Issue: model.Issue{ID: "long-id", Title: "Other", Status: model.StatusOpen, IssueType: model.TypeTask, Priority: 1, CreatedAt: now.Add(-3 * time.Hour), Labels: []string{"ctx:two", "frontend"}}, RepositoryName: "two", RepositoryExtra: 10, HubPresentation: true, PresentationLabels: []string{"ctx:two", "frontend"}},
	})
	view := ansi.Strip(picker.renderMembers(70, 8))
	rows := make([]string, 0, 2)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "short") || strings.Contains(line, "long-id") {
			rows = append(rows, line)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("member rows=%d:\n%s", len(rows), view)
	}
	ordered := []string{"one +1", "TASK", "P1", "OPEN", "short", "2h", "A deliberately", "backend"}
	previous := -1
	for _, value := range ordered {
		at := strings.Index(rows[0], value)
		if at <= previous {
			t.Fatalf("member row order lost at %q:\n%s", value, rows[0])
		}
		previous = at
	}
	for _, column := range []string{"OPEN", "short", "long-id"} {
		if len(rows) == 2 && displayOffset(rows[0], column) >= 0 && displayOffset(rows[1], column) >= 0 && column != "short" && column != "long-id" && displayOffset(rows[0], column) != displayOffset(rows[1], column) {
			t.Fatalf("%s column is not aligned:\n%s", column, view)
		}
	}
	if strings.Contains(view, "ctx:one") || !strings.Contains(view, "backend") || !strings.Contains(view, "…") {
		t.Fatalf("member renderer labels/title handling incorrect:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 70 {
			t.Fatalf("member row exceeded width: %d: %q", lipgloss.Width(line), line)
		}
	}
}

func TestScopeMemberRowsRestoreStyledVisualSemantics(t *testing.T) {
	profile := lipgloss.DefaultRenderer().ColorProfile()
	defer lipgloss.SetColorProfile(profile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	picker := NewScopePickerModel(DefaultTheme(renderer))
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{
		Issue:        model.Issue{ID: "member-1", Title: "Member title", Status: model.StatusOpen, IssueType: model.TypeTask, Priority: 1, Labels: []string{"backend"}},
		RepositoryID: "ctx:alpha", RepositoryName: "alpha", HubPresentation: true, PresentationLabels: []string{"backend"},
	}})
	view := picker.renderMembers(100, 5)
	icon, _ := picker.theme.GetTypeIcon(string(model.TypeTask))
	for _, want := range []string{
		"▸ ", icon, RenderRepositoryBadge("ctx:alpha", "alpha"),
		RenderPriorityBadge(1), RenderStatusBadge("open"),
		itemLabelStyle().Render("backend"),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("member view missing styled visual %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "\x1b[") {
		t.Fatalf("member view lost ANSI styling:\n%s", view)
	}
}

func TestScopeMemberTitleAndLabelColumnsAreNaturalAndBounded(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "one", Title: "Short", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"backend"}}},
		{Issue: model.Issue{ID: "two", Title: "A deliberately long title that must truncate", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"backend"}}},
	})
	const width = 70
	columns := scopeMemberColumnsFor(picker.filteredMembers, width)
	if columns.title >= width/2 || columns.labels < lipgloss.Width(itemLabelStyle().Render("backend")) {
		t.Fatalf("natural title/label widths = %+v", columns)
	}
	rows := make([]string, 0, 2)
	for index, item := range picker.filteredMembers {
		rows = append(rows, ansi.Strip(picker.renderMemberRow(item, index == picker.memberSelected, columns, width)))
	}
	labelOffsets := make([]int, 0, len(rows))
	for _, row := range rows {
		titleAt := displayOffset(row, "Short")
		if titleAt < 0 {
			titleAt = displayOffset(row, "A deliberately")
		}
		labelAt := displayOffset(row, "backend")
		if titleAt < 0 || labelAt <= titleAt+columns.title || labelAt > titleAt+columns.title+3 {
			t.Fatalf("label did not follow bounded title cell: title=%d label=%d columns=%+v row=%q", titleAt, labelAt, columns, row)
		}
		labelOffsets = append(labelOffsets, labelAt)
	}
	if !strings.Contains(rows[1], "…") {
		t.Fatalf("long title was not truncated:\n%s", strings.Join(rows, "\n"))
	}
	if labelOffsets[0] != labelOffsets[1] {
		t.Fatalf("labels are not left-aligned: %d and %d", labelOffsets[0], labelOffsets[1])
	}
}

func TestScopeMemberHeaderCallsContextColumnContext(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1", Title: "Member", Status: model.StatusOpen}}})
	view := ansi.Strip(picker.renderMembers(100, 5))
	if !strings.Contains(view, "CONTEXT") || strings.Contains(view, "REPOSITORY") {
		t.Fatalf("member header context label = %q", view)
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

func TestScopePickerLoadsSelectedMembersAndRejectsStaleResponses(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadDetails: func(_ context.Context, scopeID string) (ScopeDetails, error) {
			return ScopeDetails{Info: ScopeInfo{ID: scopeID}, Issues: []model.Issue{{ID: scopeID + "-member", Title: scopeID, Status: model.StatusOpen}}}, nil
		},
	}})
	m.focused = focusList
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}}
	first := m.openScopePicker("")
	if first == nil {
		t.Fatal("initial selected scope did not start a details load")
	}
	updated, second := m.handleScopePickerKey(keyMsg("down"))
	m = updated
	if second == nil || m.scopePicker.SelectedScopeID() != "s2" {
		t.Fatalf("selection movement: cmd=%t scope=%q", second != nil, m.scopePicker.SelectedScopeID())
	}
	var next tea.Model
	next, _ = m.Update(second())
	m = next.(*Model)
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != "s2-member" {
		t.Fatalf("current details selected member = %#v, want s2-member", selected)
	}
	next, _ = m.Update(first())
	m = next.(*Model)
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != "s2-member" {
		t.Fatalf("stale details replaced current member = %#v", selected)
	}
}

func TestPagedScopePickerBuildsIndependentCatalogAndMemberRequests(t *testing.T) {
	var catalogs []ScopeCatalogQuery
	var members []ScopeMembersQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryCatalog: func(_ context.Context, query ScopeCatalogQuery) (ScopeCatalogPage, error) {
			catalogs = append(catalogs, query)
			return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "Today", MemberCount: 4}}, HasMore: true, NextCursor: "catalog-2"}, nil
		},
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			members = append(members, query)
			return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID, Name: "Today", MemberCount: 4}, Members: []model.Issue{{ID: "m-1", Title: "Member", Status: model.StatusOpen}}, HasMore: true, NextCursor: "members-2"}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "Today"}}
	m.scopePicker.SetScopes(m.scopeCatalog)

	for _, message := range runUISemanticCommands(m.openScopePicker("")) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	if len(catalogs) != 1 || catalogs[0].Cursor != "" || catalogs[0].Limit != scopePageSize {
		t.Fatalf("catalog requests = %#v", catalogs)
	}
	if len(members) != 1 || members[0].ScopeID != "s1" || members[0].Cursor != "" || members[0].Limit != scopePageSize {
		t.Fatalf("member requests = %#v", members)
	}

	updated, _ := m.handleScopePickerKey(keyMsg("tab"))
	m = updated
	updated, _ = m.handleScopePickerKey(keyMsg("space"))
	m = updated
	if m.scopePicker.MemberMarkCount() != 1 {
		t.Fatalf("page-local mark count=%d, want 1", m.scopePicker.MemberMarkCount())
	}
	updated, memberNext := m.handleScopePickerKey(keyMsg("n"))
	m = updated
	if memberNext == nil {
		t.Fatal("member page request missing")
	}
	updatedTea, _ := m.Update(memberNext())
	m = updatedTea.(*Model)
	if m.scopePicker.MemberMarkCount() != 0 {
		t.Fatalf("member page retained mark count=%d", m.scopePicker.MemberMarkCount())
	}
	updated, _ = m.handleScopePickerKey(keyMsg("tab"))
	m = updated
	updated, catalogNext := m.handleScopePickerKey(keyMsg("right"))
	m = updated
	if catalogNext == nil || m.scopePicker.CatalogPageIndex() != 1 {
		t.Fatalf("catalog next page: cmd=%t index=%d", catalogNext != nil, m.scopePicker.CatalogPageIndex())
	}
	updatedTea, _ = m.Update(catalogNext())
	m = updatedTea.(*Model)
	if len(catalogs) != 2 || catalogs[1].Cursor != "catalog-2" || len(members) != 2 {
		t.Fatalf("catalog navigation requests: catalogs=%#v members=%#v", catalogs, members)
	}

	m.scopePicker.memberRepositoryFilter = "ctx:alpha"
	m.scopePicker.memberStatusFilter = "ready"
	m.scopePicker.memberTypeFilter = model.TypeTask
	filterRequest := m.startScopeMembersPage("", 0)
	if filterRequest == nil || m.scopePicker.MemberPageIndex() != 0 {
		t.Fatalf("member filter reset: cmd=%t index=%d", filterRequest != nil, m.scopePicker.MemberPageIndex())
	}
	updatedTea, _ = m.Update(filterRequest())
	m = updatedTea.(*Model)
	if len(members) != 3 || members[2].Cursor != "" || members[2].Status != "ready" || members[2].Type != string(model.TypeTask) || !reflect.DeepEqual(members[2].Contexts, []string{"ctx:alpha"}) {
		t.Fatalf("member navigation request = %#v", members)
	}
}

func TestPagedScopePickerFiltersResetPageAndRejectStaleMembers(t *testing.T) {
	var requests []ScopeMembersQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			requests = append(requests, query)
			return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID, Name: query.ScopeID}, Members: []model.Issue{{ID: query.ScopeID + "-member"}}}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}}
	first := m.openScopePicker("")
	if first == nil {
		t.Fatal("initial member request missing")
	}
	updated, second := m.handleScopePickerKey(keyMsg("down"))
	m = updated
	if second == nil {
		t.Fatal("second member request missing")
	}
	updatedTea, _ := m.Update(second())
	m = updatedTea.(*Model)
	updatedTea, _ = m.Update(first())
	m = updatedTea.(*Model)
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != "s2-member" {
		t.Fatalf("stale member response selected %#v", selected)
	}

	// A server-owned filter starts a fresh member page and never filters the
	// already loaded page locally.
	m.scopePicker.memberFocused = true
	updated, filterRequest := m.handleScopePickerKey(keyMsg("c"))
	m = updated
	if filterRequest == nil || m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.MemberMarkCount() != 0 {
		t.Fatalf("filter reset: cmd=%t page=%d marks=%d", filterRequest != nil, m.scopePicker.MemberPageIndex(), m.scopePicker.MemberMarkCount())
	}
	updatedTea, _ = m.Update(filterRequest())
	m = updatedTea.(*Model)
	if len(requests) != 3 || requests[2].Status != "completed" || requests[2].Cursor != "" {
		t.Fatalf("filter request = %#v", requests)
	}

	m.scopePicker.memberFocused = true
	updated, _ = m.handleScopePickerKey(keyMsg("space"))
	m = updated
	if m.scopePicker.MemberMarkCount() != 1 {
		t.Fatalf("mark count=%d, want 1", m.scopePicker.MemberMarkCount())
	}
	m.closeScopePicker()
	if m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.CatalogPageIndex() != 0 || m.scopePicker.MemberMarkCount() != 0 {
		t.Fatalf("return did not reset picker paging/marks: member=%d catalog=%d marks=%d", m.scopePicker.MemberPageIndex(), m.scopePicker.CatalogPageIndex(), m.scopePicker.MemberMarkCount())
	}
}

func TestPagedScopePickerPreservesActiveScopeAcrossCatalogPages(t *testing.T) {
	m := NewModel(nil, nil, "")
	active := ScopeInfo{ID: "s2", Name: "Active", Active: true}
	m.activeScope = &active
	generation := m.scopePicker.BeginCatalogLoad()
	updated, _ := m.Update(scopeCatalogPageMsg{
		page:       ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "Other", Active: true}, {ID: "s2", Name: "Active"}}},
		generation: generation,
	})
	m = updated.(*Model)
	if m.activeScope == nil || m.activeScope.ID != "s2" {
		t.Fatalf("active scope = %#v, want s2", m.activeScope)
	}
	if m.scopeCatalog[0].Active || !m.scopeCatalog[1].Active {
		t.Fatalf("catalog active flags = %#v, want only s2 active", m.scopeCatalog)
	}
}

func TestPagedScopePickerUsesCanonicalFilterChoicesAndBackendClosedStatus(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetMemberContextCatalog(hubScopeCatalog("ctx:alpha"))
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "one", IssueType: model.TypeBug}}})
	picker.SetMemberServerFiltering(true)
	picker.CycleMemberRepository()
	if contexts := picker.MemberContexts(); !reflect.DeepEqual(contexts, []string{"ctx:alpha"}) {
		t.Fatalf("contexts = %#v, want registered alpha context", contexts)
	}

	var got ScopeMembersQuery
	m := NewModel([]model.Issue{{ID: "main", IssueType: "decision"}}, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			got = query
			return ScopeMembersPage{Scope: ScopeInfo{ID: "s1"}}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.scopePicker.SetMemberFilters("", "closed", "")
	cmd := m.startScopeMembersPage("", 0)
	if cmd == nil {
		t.Fatal("member page command missing")
	}
	updated, _ := m.Update(cmd())
	m = updated.(*Model)
	m.scopePicker.CycleMemberType()
	cmd = m.startScopeMembersPage("", 0)
	if cmd == nil {
		t.Fatal("filtered member page command missing")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if got.Status != "completed" {
		t.Fatalf("backend status = %q, want completed", got.Status)
	}
	if got.Type != string(model.TypeBug) {
		t.Fatalf("backend type = %q, want canonical type bug", got.Type)
	}
}

func TestScopePickerMemberNavigationFiltersAndRegions(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(100, 24)
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "api-1", Title: "API", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryName: "api"},
		{Issue: model.Issue{ID: "web-1", Title: "Web", Status: model.StatusClosed, IssueType: model.TypeBug}, RepositoryName: "web"},
	})
	picker.memberFocused = true
	picker.MoveMember(1)
	if selected := picker.SelectedMember(); selected == nil || selected.Issue.ID != "web-1" {
		t.Fatalf("member selection = %#v, want web-1", selected)
	}
	picker.ToggleMemberStatus("closed")
	if len(picker.filteredMembers) != 1 || picker.filteredMembers[0].Issue.ID != "web-1" {
		t.Fatalf("closed member filter = %#v", picker.filteredMembers)
	}
	picker.ToggleMemberStatus("closed")
	picker.CycleMemberRepository()
	if len(picker.filteredMembers) != 1 || picker.filteredMembers[0].Issue.ID != "api-1" {
		t.Fatalf("repository member filter = %#v", picker.filteredMembers)
	}
	picker.memberRepositoryFilter = ""
	picker.CycleMemberType()
	if len(picker.filteredMembers) != 1 || picker.filteredMembers[0].Issue.ID != "web-1" {
		t.Fatalf("type member filter = %#v", picker.filteredMembers)
	}
	view := ansi.Strip(picker.View())
	for _, want := range []string{"Scopes", "Members · Today", "repository:all", "type:bug", "web-1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("member picker missing %q:\n%s", want, view)
		}
	}
}

func TestScopePickerMemberRenderHidesAssigneeWithoutChangingIssue(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{
		ID: "member-1", Title: "Assigned member", Status: model.StatusOpen,
		IssueType: model.TypeTask, Assignee: "agent-7",
	}}})

	view := ansi.Strip(picker.renderMembers(120, 5))
	if strings.Contains(view, "@agent-7") {
		t.Fatalf("member browser rendered assignee:\n%s", view)
	}
	if got := picker.members[0].Issue.Assignee; got != "agent-7" {
		t.Fatalf("member issue assignee = %q, want agent-7", got)
	}
}

func TestScopePickerMemberRenderAlignsMixedRepositoryExtras(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	picker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "member-1", Title: "One", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryID: "ctx:one", RepositoryName: "one", RepositoryExtra: 1, HubPresentation: true},
		{Issue: model.Issue{ID: "member-2", Title: "Two", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryID: "ctx:two", RepositoryName: "two", RepositoryExtra: 10, HubPresentation: true},
	})

	view := ansi.Strip(picker.renderMembers(120, 6))
	rows := make([]string, 0, 2)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "member-") {
			rows = append(rows, line)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("member rows = %d, want 2:\n%s", len(rows), view)
	}
	for _, marker := range []string{"OPEN", "member-"} {
		if got := displayOffset(rows[0], marker); got != displayOffset(rows[1], marker) {
			t.Fatalf("%s starts are not aligned: %d and %d\n%s", marker, displayOffset(rows[0], marker), displayOffset(rows[1], marker), view)
		}
	}
}

func TestGenerationlessScopeDetailsPopulateMemberBrowser(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{})
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})

	updated, _ := m.Update(scopeDetailsMsg{scopeID: "s1", details: ScopeDetails{
		Info:   ScopeInfo{ID: "s1", Name: "Today"},
		Issues: []model.Issue{{ID: "b-1", Title: "Member", Status: model.StatusOpen}},
	}})
	m = updated.(*Model)
	if member := m.scopePicker.SelectedMember(); member == nil || member.Issue.ID != "b-1" {
		t.Fatalf("generationless details did not populate selected member: %#v", member)
	}
	view := ansi.Strip(m.scopePicker.View())
	if !strings.Contains(view, "Members · Today") || !strings.Contains(view, "b-1") {
		t.Fatalf("member browser omitted supplied details:\n%s", view)
	}
	updated, _ = m.Update(keyMsg("tab"))
	m = updated.(*Model)
	footer := ansi.Strip(m.renderFooter())
	if !m.scopePicker.MemberFocused() || !strings.Contains(footer, "space mark") || !strings.Contains(footer, "R re") {
		t.Fatalf("member browser did not expose usable focus controls: focused=%t footer=%q", m.scopePicker.MemberFocused(), footer)
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
	if m.showScopePicker || m.isBacklogView || m.focused != focusList {
		t.Fatalf("W did not close backlog before restoring list origin: shown=%t backlog=%t focus=%s", m.showScopePicker, m.isBacklogView, m.focused)
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
		if !strings.Contains(footer, "enter toggle") || strings.Contains(footer, "m move") {
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
		{name: "scopes", focus: focusScopePicker, wants: []string{"Scopes", "Tab", "Switch to members", "Enter", "Toggle active scope", "n", "Create inactive named scope", "B", "global backlog"}},
		{name: "backlog", focus: focusBacklog, wants: []string{"Backlog", "n/p", "Next / previous page", "/", "ID/title search", "l", "Filter by exact label", "s", "Cycle status", "A", "Add selected bead to scope", "space", "Mark current", "M", "Add matching exact label/epic issues to active scope"}},
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

func TestScopeMemberHelpAndFooterDescribeEffectiveControls(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height = 240, 40
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopePicker.memberFocused = true
	help := ansi.Strip(m.renderHelpOverlay())
	for _, want := range []string{"Switch to scope catalog", "Move member selection", "Filter members by status", "Cycle member type filter", "Cycle member repository filter", "Mark current member", "Remove marked/current members", "Remove members by epic or label"} {
		if !strings.Contains(help, want) {
			t.Fatalf("scope member help missing %q:\n%s", want, help)
		}
	}
	for _, unavailable := range []string{"Toggle active scope", "Create inactive named scope"} {
		if strings.Contains(help, unavailable) {
			t.Fatalf("scope member help advertises catalog-only control %q:\n%s", unavailable, help)
		}
	}

	footer := ansi.Strip(m.renderFooter())
	for _, want := range []string{"tab catalog", "j/k members", "o/c/r status", "I type", "w repository", "space mark", "R remove current", "M epic/label", "B backlog", "W close"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("scope member footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "enter toggle") || strings.Contains(footer, "n new") {
		t.Fatalf("scope member footer advertises catalog-only control: %q", footer)
	}

	m.scopePicker.SetMoveTarget("Visible bead")
	help = ansi.Strip(m.renderHelpOverlay())
	for _, want := range []string{"Switch to scope catalog", "Move member selection", "Filter members by status"} {
		if !strings.Contains(help, want) {
			t.Fatalf("moving scope member help missing %q:\n%s", want, help)
		}
	}
	for _, unavailable := range []string{"Move destination scope", "Move selected bead"} {
		if strings.Contains(help, unavailable) {
			t.Fatalf("moving scope member help advertises destination control %q:\n%s", unavailable, help)
		}
	}
	footer = ansi.Strip(m.renderFooter())
	for _, want := range []string{"tab catalog", "j/k members", "o/c/r status"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("moving scope member footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "enter move") || strings.Contains(footer, "destination") {
		t.Fatalf("moving scope member footer advertises destination control: %q", footer)
	}
}

func TestBacklogHelpAndFooterDescribePreviewAndBatchControls(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height = 240, 40
	m.isBacklogView = true
	m.focused = focusBacklog
	help := ansi.Strip(m.renderHelpOverlay())
	for _, want := range []string{"PgUp/Dn", "Scroll preview", "Mark current bead", "Next / previous page", "ID/title search", "Filter by exact label", "Cycle status", "Add selected bead to scope (or all marked)", "Add matching exact label/epic issues to active scope"} {
		if !strings.Contains(help, want) {
			t.Fatalf("backlog help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "Add by epic or label") {
		t.Fatalf("backlog help retains ambiguous match wording:\n%s", help)
	}
	footer := ansi.Strip(m.renderFooter())
	for _, want := range []string{"pgup/dn preview", "space mark", "n/p page", "/ filter", "A add current", "M add scope", "W scopes", "B list"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("backlog footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "B/esc/q list") || strings.Contains(footer, "esc/q list") {
		t.Fatalf("backlog footer expands the return hint: %q", footer)
	}

	m.backlog.BeginSearch()
	footer = ansi.Strip(m.renderFooter())
	for _, want := range []string{"type filter", "backspace delete", "enter/esc done"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("backlog filter footer missing %q: %q", want, footer)
		}
	}
	for _, unavailable := range []string{"space mark", "n/p page", "A add"} {
		if strings.Contains(footer, unavailable) {
			t.Fatalf("backlog filter footer advertises view control %q: %q", unavailable, footer)
		}
	}
}

func TestBacklogScopeMatchPromptNamesExactMatchAction(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height = 100, 30
	prompt := ansi.Strip(m.renderScopeMatchPrompt())
	for _, want := range []string{"Add matching exact label/epic issues to active scope", "Enter label:name or epic:id"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("scope match prompt missing %q: %q", want, prompt)
		}
	}
}

func TestScopeMatchPromptNamesRemovalFromSelectedScope(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height = 100, 30
	m.scopeMatchAction = "remove"
	prompt := ansi.Strip(m.renderScopeMatchPrompt())
	if !strings.Contains(prompt, "Remove matching exact label/epic issues from selected scope") {
		t.Fatalf("removal scope match prompt is inaccurate: %q", prompt)
	}
	if strings.Contains(prompt, "Add matching exact label/epic issues to active scope") {
		t.Fatalf("removal scope match prompt retains add wording: %q", prompt)
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
		"Toggle active scope (Scopes)",
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
		{"Enter", "Toggle active scope (Scopes)"},
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

func TestBacklogLocalNavigationDismissesReloadStatusButPreservesErrors(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1"}}}, 0)

	m.statusMsg = "Reloaded 1 issues"
	m.statusIsError = false
	updated, _ := m.Update(keyMsg("j"))
	m = updated.(*Model)
	if m.statusMsg != "" || m.statusIsError {
		t.Fatalf("local navigation retained reload status=%q error=%v", m.statusMsg, m.statusIsError)
	}

	m.statusMsg = "Backlog load failed: unavailable"
	m.statusIsError = true
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(*Model)
	if m.statusMsg != "Backlog load failed: unavailable" || !m.statusIsError {
		t.Fatalf("local navigation changed action error=%q error=%v", m.statusMsg, m.statusIsError)
	}

	m.statusMsg = "Scope add succeeded"
	m.statusIsError = false
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(*Model)
	if m.statusMsg != "Scope add succeeded" || m.statusIsError {
		t.Fatalf("local navigation changed successful action=%q error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestBacklogFooterOmitsSnapshotStatsButOrdinaryFooterShowsThem(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width = 240
	m.countOpen, m.countReady, m.countBlocked, m.countClosed = 1, 2, 3, 4
	wantStats := "○1 ◉2 ◈3 ●4"

	ordinary := ansi.Strip(m.renderFooter())
	if !strings.Contains(ordinary, wantStats) {
		t.Fatalf("ordinary footer missing snapshot stats %q: %q", wantStats, ordinary)
	}

	m.isBacklogView = true
	backlog := ansi.Strip(m.renderFooter())
	if strings.Contains(backlog, wantStats) {
		t.Fatalf("backlog footer retained snapshot stats %q: %q", wantStats, backlog)
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

func TestBacklogStatusCycleForwardsExactFilterAndResetsPaging(t *testing.T) {
	var got BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			got = query
			return BacklogPage{}, nil
		},
	}})
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetPage(BacklogPage{HasMore: true, NextCursor: "opaque-next"}, 0)
	m.backlog.NextPageCursor()
	m.backlog.SetPage(BacklogPage{}, 1)
	m.backlogPageGeneration = 9

	updated, cmd := m.Update(keyMsg("s"))
	m = updated.(*Model)
	if cmd == nil || m.backlog.Status() != "open" || m.backlog.PageIndex() != 0 || m.backlog.CurrentPageCursor() != "" || m.backlogPageGeneration != 10 {
		t.Fatalf("status change state: cmd=%t status=%q page=%d cursor=%q generation=%d", cmd != nil, m.backlog.Status(), m.backlog.PageIndex(), m.backlog.CurrentPageCursor(), m.backlogPageGeneration)
	}
	_ = cmd()
	if got.Status != "open" || got.Cursor != "" {
		t.Fatalf("status query=%#v, want open on first page", got)
	}

	for _, want := range []string{"in_progress", "blocked", "deferred", "closed", "all"} {
		m.backlog.CycleStatus()
		if got := m.backlog.Status(); got != want {
			t.Fatalf("status cycle=%q, want %q", got, want)
		}
	}
}

func TestBacklogLabelInputAppliesExactOrdinaryLabelAndResetsPaging(t *testing.T) {
	var got BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			got = query
			return BacklogPage{}, nil
		},
	}})
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetLabel("old")
	m.backlog.SetPage(BacklogPage{HasMore: true, NextCursor: "opaque-next"}, 0)
	m.backlog.NextPageCursor()
	m.backlog.SetPage(BacklogPage{}, 1)
	m.backlogPageGeneration = 3

	updated, _ := m.Update(keyMsg("l"))
	m = updated.(*Model)
	if !m.backlog.LabelEditing() || m.backlog.LabelInputValue() != "old" {
		t.Fatalf("label editor state: editing=%t value=%q", m.backlog.LabelEditing(), m.backlog.LabelInputValue())
	}
	for range 3 {
		updated, _ = m.Update(keyMsg("backspace"))
		m = updated.(*Model)
	}
	updated, _ = m.Update(keyMsg("team"))
	m = updated.(*Model)
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd == nil || m.backlog.LabelEditing() || m.backlog.Label() != "team" || m.backlog.PageIndex() != 0 || m.backlog.CurrentPageCursor() != "" || m.backlogPageGeneration != 4 {
		t.Fatalf("label change state: cmd=%t editing=%t label=%q page=%d cursor=%q generation=%d", cmd != nil, m.backlog.LabelEditing(), m.backlog.Label(), m.backlog.PageIndex(), m.backlog.CurrentPageCursor(), m.backlogPageGeneration)
	}
	_ = cmd()
	if got.Label != "team" || got.Status != "" || got.Cursor != "" {
		t.Fatalf("label query=%#v, want exact label on first page", got)
	}

	updated, _ = m.Update(keyMsg("l"))
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg(","))
	m = updated.(*Model)
	updated, cmd = m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd != nil || !m.backlog.LabelEditing() || !m.statusIsError || m.statusMsg != "Enter one ordinary label" {
		t.Fatalf("invalid label state: cmd=%t editing=%t error=%t status=%q", cmd != nil, m.backlog.LabelEditing(), m.statusIsError, m.statusMsg)
	}
}

func TestBacklogViewShowsActiveSearchLabelAndStatus(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(120, 12)
	b.BeginSearch()
	b.AddFilter("needle")
	b.SetLabel("team")
	b.CycleStatus()

	view := ansi.Strip(b.View())
	for _, want := range []string{"Global backlog", "search: needle_", "label: team", "status: open"} {
		if !strings.Contains(view, want) {
			t.Fatalf("backlog view missing active filter %q:\n%s", want, view)
		}
	}
}

func TestTypedBacklogQueryCarriesBoundedRequest(t *testing.T) {
	want := BacklogQuery{Filter: "alpha", Cursor: "opaque/token", Limit: backlogPageSize}
	var got BacklogQuery
	cmd := loadBacklogPageCmd(ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			got = query
			return BacklogPage{}, nil
		},
	}, want, 2, 7)

	raw := cmd()
	msg, ok := raw.(backlogPageMsg)
	if !ok {
		t.Fatalf("typed backlog command returned %T", raw)
	}
	if !reflect.DeepEqual(got, want) || msg.cursor != want.Cursor || msg.index != 2 || msg.generation != 7 {
		t.Fatalf("query=%#v message=%#v, want query=%#v and matching paging metadata", got, msg, want)
	}
}

func TestTypedScopeMutationCarriesBatchAndMoveFields(t *testing.T) {
	want := ScopeMutation{
		Kind:          ScopeMutationMove,
		IssueIDs:      []string{"b-1", "b-2"},
		SourceScopeID: "today",
		TargetScopeID: "later",
	}
	msg, ok := runScopeMutationCmd(want, true, nil)().(scopeMutationMsg)
	if !ok {
		t.Fatalf("typed mutation command returned unexpected message")
	}
	if msg.mutation.Kind != want.Kind || strings.Join(msg.mutation.IssueIDs, ",") != "b-1,b-2" ||
		msg.mutation.SourceScopeID != want.SourceScopeID || msg.mutation.TargetScopeID != want.TargetScopeID ||
		!msg.restoreFocus || msg.action != string(want.Kind) {
		t.Fatalf("mutation message=%#v, want typed mutation=%#v", msg, want)
	}
}

func TestBacklogAddRefreshPreservesCurrentPageRequest(t *testing.T) {
	var got BacklogQuery
	var gotPage backlogPageMsg
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			got = query
			return BacklogPage{}, nil
		},
	}})
	m.isBacklogView = true
	m.backlog.Reset()
	m.backlog.AddFilter("alpha")
	m.backlog.SetPage(BacklogPage{HasMore: true, NextCursor: "cursor-1"}, 0)
	m.backlog.NextPageCursor()
	m.backlog.SetPage(BacklogPage{}, 1)
	if got := m.backlog.CurrentPageCursor(); got != "cursor-1" {
		t.Fatalf("current backlog cursor=%q, want cursor-1", got)
	}
	m.backlogPageGeneration = 4

	cmd := m.refreshAfterScopeMutation(ScopeMutation{Kind: ScopeMutationAdd})
	for _, child := range cmd().(tea.BatchMsg) {
		if page, ok := child().(backlogPageMsg); ok {
			gotPage = page
			break
		}
	}
	if want := (BacklogQuery{Filter: "alpha", Cursor: "cursor-1", Limit: backlogPageSize}); !reflect.DeepEqual(got, want) {
		t.Fatalf("backlog add refresh query=%#v, want %#v", got, want)
	}
	if gotPage.index != 1 || gotPage.generation != 5 || m.backlog.PageIndex() != 1 || m.backlogPageGeneration != 5 {
		t.Fatalf("backlog state page=%d generation=%d message=%#v, want page=1 generation=5", m.backlog.PageIndex(), m.backlogPageGeneration, gotPage)
	}
}

func TestScopeRemoveRefreshLoadsDetailsAndPreservesThemOnFailure(t *testing.T) {
	old := ScopeDetails{Info: ScopeInfo{ID: "today"}, MemberIDs: []string{"b-1"}}
	newDetails := ScopeDetails{Info: ScopeInfo{ID: "today"}, MemberIDs: []string{"b-2"}}
	var loaded string
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadDetails: func(_ context.Context, scopeID string) (ScopeDetails, error) {
			loaded = scopeID
			return newDetails, nil
		},
	}})
	m.scopeDetails = &old
	cmds := m.refreshAfterScopeMutation(ScopeMutation{Kind: ScopeMutationRemove, ScopeID: "today"})().(tea.BatchMsg)
	for _, child := range cmds {
		if details, ok := child().(scopeDetailsMsg); ok {
			updated, _ := m.Update(details)
			m = updated.(*Model)
		}
	}
	if loaded != "today" || m.scopeDetails == nil || strings.Join(m.scopeDetails.MemberIDs, ",") != "b-2" {
		t.Fatalf("loaded scope=%q details=%#v, want today with b-2", loaded, m.scopeDetails)
	}

	m.runtimeServices.Scopes.LoadDetails = func(context.Context, string) (ScopeDetails, error) {
		return ScopeDetails{}, errors.New("details unavailable")
	}
	cmds = m.refreshAfterScopeMutation(ScopeMutation{Kind: ScopeMutationRemove, ScopeID: "today"})().(tea.BatchMsg)
	for _, child := range cmds {
		if details, ok := child().(scopeDetailsMsg); ok {
			updated, _ := m.Update(details)
			m = updated.(*Model)
		}
	}
	if m.scopeDetails == nil || strings.Join(m.scopeDetails.MemberIDs, ",") != "b-2" {
		t.Fatalf("failed details load replaced existing details: %#v", m.scopeDetails)
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

func TestBacklogRenderUsesBoundedColumnsAndFullPreview(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(120, 12)
	created := time.Now().Add(-2 * time.Hour)
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID:          "backlog-1",
		Title:       "Readable backlog title",
		Description: "First line of a deliberately long description that must remain fully visible.",
		Status:      model.StatusOpen,
		IssueType:   model.TypeFeature,
		Priority:    1,
		CreatedAt:   created,
	}}}, 0)
	b.setPresentation([]IssueItem{{
		Issue:           b.issues[0],
		RepositoryID:    "ctx:api",
		RepositoryName:  "api",
		HubPresentation: true,
	}})
	b.setDelegate(IssueDelegate{
		Theme:               testTheme(),
		ShowRepositories:    true,
		RepositoryNameWidth: 3,
		useFullWidth:        true,
	})

	view := ansi.Strip(b.View())
	for _, want := range []string{"Global backlog", "ID", "TYPE", "PR", "STAT", "CREATED_AT", "OPEN", "backlog-1", "Readable backlog title", "DESCRIPTION", "First line", "remain fully visible."} {
		if !strings.Contains(view, want) {
			t.Fatalf("backlog view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "AGE") || strings.Contains(view, "CMT") || strings.Contains(view, "GRAPH") || strings.Contains(view, "[api]") {
		t.Fatalf("backlog view inherited ordinary List metadata:\n%s", view)
	}
	columns := backlogTableColumnsFor(b.filteredItems, 120)
	row := ansi.Strip(b.renderBacklogList(columns, 120, 1))
	if strings.Contains(row, "Readable backlog title") {
		t.Fatalf("backlog row includes title:\n%s", row)
	}
	header := renderBacklogTableHeader(columns)
	if strings.Index(header, "ID") > strings.Index(header, "CREATED_AT") {
		t.Fatalf("backlog header places ID after created_at: %q", header)
	}
	if !strings.Contains(row, formatBacklogCreatedAt(created)) {
		t.Fatalf("backlog row did not preserve exact created_at:\n%s", row)
	}
	if strings.Contains(view, "…") {
		t.Fatalf("backlog preview truncated full description:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 120 {
			t.Fatalf("backlog line width = %d, want <= 120: %q", lipgloss.Width(line), line)
		}
	}
}

func TestBacklogHeaderUsesBrightForegroundOnDarkTerminal(t *testing.T) {
	savedProfile := TermProfile
	defer func() { TermProfile = savedProfile }()
	TermProfile = colorprofile.TrueColor

	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	renderer.SetHasDarkBackground(true)
	b := NewBacklogModel(DefaultTheme(renderer))
	header := b.renderBacklogHeader("Global backlog", backlogTableColumns{width: 32})

	if !strings.Contains(header, "38;2;255;255;255") {
		t.Fatalf("backlog header did not render bright white foreground: %q", header)
	}
	if strings.Contains(header, "38;2;40;42;54") {
		t.Fatalf("backlog header retained near-black foreground: %q", header)
	}
}

func TestBacklogPreviewSeparatesTitleAndDescription(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{Title: "Readable title", Description: "Description"}}}, 0)

	preview := ansi.Strip(b.renderBacklogPreview(80))
	lines := strings.Split(preview, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "TITLE  Readable title" || lines[1] != "" || strings.TrimSpace(lines[2]) != "DESCRIPTION  Description" {
		t.Fatalf("backlog preview omitted blank title/description separator: %q", preview)
	}
}

func TestBacklogSplitUsesNaturalTableWidthAndAlignedHeader(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(120, 12)
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "backlog-1", Title: "Readable title", Description: "Description",
		Status: model.StatusOpen, IssueType: model.TypeFeature, Priority: 1,
		CreatedAt: time.Date(2026, 9, 5, 12, 34, 56, 0, time.UTC),
	}}}, 0)

	contentWidth := b.width - 4
	columns := backlogTableColumnsFor(b.filteredItems, contentWidth)
	naturalWidth := backlogTableWidth(columns)
	fitWidth := contentWidth * 2 / 3
	if naturalWidth >= fitWidth {
		t.Fatalf("test table width=%d must fit within existing split bound %d", naturalWidth, fitWidth)
	}

	view := ansi.Strip(b.View())
	lines := strings.Split(view, "\n")
	var tableHeader, previewTitle string
	for _, line := range lines {
		if strings.Contains(line, "ID") && strings.Contains(line, "CREATED_AT") {
			tableHeader = line
		}
		if strings.Contains(line, "TITLE  Readable title") {
			previewTitle = line
		}
	}
	if tableHeader == "" || previewTitle == "" {
		t.Fatalf("split backlog omitted table header or preview:\n%s", view)
	}
	columns.width = naturalWidth
	header := ansi.Strip(b.renderBacklogHeader("Global backlog", columns))
	directTableHeader := strings.Split(header, "\n")[1]
	for _, line := range strings.Split(header, "\n") {
		if got := lipgloss.Width(line); got != naturalWidth {
			t.Fatalf("backlog header width=%d, want list width %d: %q", got, naturalWidth, line)
		}
	}
	if got := lipgloss.Width(previewTitle[:strings.Index(previewTitle, "TITLE")]); got != naturalWidth+4 {
		t.Fatalf("preview starts at cell %d, want natural list width plus padding/gap %d:\n%s", got, naturalWidth+4, view)
	}
	if strings.Index(directTableHeader, "ID") != 4 || strings.Index(tableHeader, "ID") != 6 {
		t.Fatalf("table header ID offsets: direct=%d rendered=%d, want independent cursor and mark cells: %q", strings.Index(directTableHeader, "ID"), strings.Index(tableHeader, "ID"), tableHeader)
	}
}

func TestBacklogStatusColumnIsFixedAndTruncatesBeforePadding(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "b-1", IssueType: model.TypeTask, Priority: 1,
		Status: model.Status("future-status-name"),
	}}}, 0)
	columns := backlogTableColumnsFor(b.filteredItems, 80)
	if columns.statusWidth != backlogStatusWidth {
		t.Fatalf("status width=%d, want %d", columns.statusWidth, backlogStatusWidth)
	}
	if header := renderBacklogTableHeader(columns); !strings.Contains(header, "STATUS     ") {
		t.Fatalf("status header was not fixed-width STATUS: %q", header)
	}
	row := ansi.Strip(b.renderBacklogRow(b.filteredItems[0], false, columns, 80))
	if !strings.Contains(row, "FUTURE-STA…") || strings.Contains(row, "FUTURE-STATUS-NAME") {
		t.Fatalf("status was not truncated before padding: %q", row)
	}
}

func TestBacklogPagedRowsKeepFixedColumnsAndTruncateBeforePadding(t *testing.T) {
	created := time.Date(2026, 9, 5, 12, 34, 56, 0, time.UTC)
	pages := []model.Issue{
		{ID: "short-id", IssueType: model.TypeTask, Priority: 1, Status: model.Status("future-status-name"), CreatedAt: created},
		{ID: "long-backlog-identifier-0123456789", IssueType: model.IssueType("custom-long-type"), Priority: 1, Status: model.Status("future-status-name"), CreatedAt: created},
	}
	var firstStarts [3]int
	for i, issue := range pages {
		b := NewBacklogModel(testTheme())
		b.SetPage(BacklogPage{Issues: []model.Issue{issue}}, i)
		columns := backlogTableColumnsFor(b.filteredItems, 120)
		if columns.idWidth != backlogIDWidth || columns.typeWidth != backlogTypeWidth {
			t.Fatalf("page %d widths = (%d, %d), want fixed (%d, %d)", i+1, columns.idWidth, columns.typeWidth, backlogIDWidth, backlogTypeWidth)
		}
		row := ansi.Strip(b.renderBacklogRow(b.filteredItems[0], false, columns, 120))
		wantID := truncateRunesHelper(issue.ID, backlogIDWidth, "…")
		wantType := truncateRunesHelper(string(issue.IssueType), backlogTypeWidth, "…")
		wantStatus := truncateRunesHelper(strings.ToUpper(string(issue.Status)), backlogStatusWidth, "…")
		wantCreated := formatBacklogCreatedAt(created)
		values := []string{wantType, wantStatus, wantCreated}
		var starts [3]int
		for j, value := range values {
			starts[j] = displayOffset(row, value)
			if starts[j] < 0 {
				t.Fatalf("page %d omitted column %q: %q", i+1, value, row)
			}
		}
		if wantID != issue.ID && strings.Contains(row, issue.ID) {
			t.Fatalf("page %d rendered full ID instead of %q: %q", i+1, wantID, row)
		}
		if wantType != string(issue.IssueType) && strings.Contains(row, string(issue.IssueType)) {
			t.Fatalf("page %d rendered full TYPE instead of %q: %q", i+1, wantType, row)
		}
		if i == 0 {
			firstStarts = starts
		} else {
			for j, name := range []string{"TYPE", "STATUS", "CREATED_AT"} {
				if starts[j] != firstStarts[j] {
					t.Fatalf("page %d %s start=%d changed from page 1 start=%d: %q", i+1, name, starts[j], firstStarts[j], row)
				}
			}
		}
	}
}

func TestBacklogCursorAndMarkCellsAreIndependentAndHighlightSelectedRows(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1"}}}, 0)
	b.ToggleMark()
	columns := backlogTableColumnsFor(b.filteredItems, 40)
	item := b.filteredItems[0]
	item.Marked = true
	selected := b.renderBacklogRow(item, true, columns, 40)
	unselected := b.renderBacklogRow(b.filteredItems[0], false, columns, 40)
	stripped := ansi.Strip(selected)
	if !strings.HasPrefix(stripped, "▸ ✓ ") {
		t.Fatalf("selected marked row prefix=%q, want independent cursor and mark cells", stripped)
	}
	if selected == unselected {
		t.Fatal("selected row lost its highlight")
	}
}

func TestBacklogPageIndicatorHasOneSeparatorAndStaysBounded(t *testing.T) {
	const width, height = 60, 10
	b := NewBacklogModel(testTheme())
	b.SetSize(width, height)
	b.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1", Title: "Preview", Description: "Details"}}}, 0)
	lines := strings.Split(ansi.Strip(b.View()), "\n")
	page := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "page 1" {
			page = i
			break
		}
		if lipgloss.Width(line) > width {
			t.Fatalf("backlog line width=%d exceeds %d: %q", lipgloss.Width(line), width, line)
		}
	}
	if page < 1 || strings.TrimSpace(lines[page-1]) != "" {
		t.Fatalf("page indicator lacked one blank separator: page=%d\n%s", page, strings.Join(lines, "\n"))
	}
	if lipgloss.Height(b.View()) > height {
		t.Fatalf("backlog height=%d exceeds %d", lipgloss.Height(b.View()), height)
	}
}

func TestFormatBacklogCreatedAtUsesThreeLetterMonthsAndAlignedWidth(t *testing.T) {
	originalLocal := time.Local
	time.Local = time.FixedZone("test-local", -8*60*60)
	defer func() { time.Local = originalLocal }()

	cases := []struct {
		name    string
		created time.Time
		want    string
	}{
		{name: "September", created: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC), want: "Sat 26 Sep - 16:00"},
		{name: "May", created: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC), want: "Tue 26 May - 16:00"},
	}

	issues := make([]model.Issue, 0, len(cases))
	for _, tc := range cases {
		if got := formatBacklogCreatedAt(tc.created); got != tc.want {
			t.Errorf("%s local created date=%q, want %q", tc.name, got, tc.want)
		}
		issues = append(issues, model.Issue{ID: tc.name, CreatedAt: tc.created})
	}
	if got, want := lipgloss.Width(formatBacklogCreatedAt(cases[0].created)), lipgloss.Width(formatBacklogCreatedAt(cases[1].created)); got != want {
		t.Fatalf("September display width=%d, May display width=%d; want equal widths", got, want)
	}

	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: issues}, 0)
	columns := backlogTableColumnsFor(b.filteredItems, 80)
	dateStart := -1
	for i, tc := range cases {
		row := ansi.Strip(b.renderBacklogRow(b.filteredItems[i], false, columns, 80))
		got := displayOffset(row, tc.want)
		if got < 0 {
			t.Fatalf("%s date missing from row: %q", tc.name, row)
		}
		if dateStart < 0 {
			dateStart = got
		} else if got != dateStart {
			t.Fatalf("%s date starts at %d, want aligned start %d: %q", tc.name, got, dateStart, row)
		}
	}
	if got := formatBacklogCreatedAt(time.Time{}); got != "n/a" {
		t.Fatalf("zero created date=%q, want n/a", got)
	}
}

func TestBacklogMarkAcceptsPhysicalSpaceInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "bubble tea key space", key: tea.KeyMsg{Type: tea.KeySpace}},
		{name: "space rune", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}},
		{name: "synthetic space", key: keyMsg("space")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(nil, nil, "")
			m.isBacklogView, m.focused = true, focusBacklog
			m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1"}}}, 0)
			updated, _ := m.Update(tc.key)
			if got := updated.(*Model).backlog.MarkCount(); got != 1 {
				t.Fatalf("mark count=%d, want 1 for %s", got, tc.name)
			}
		})
	}
}

func TestBacklogMovesPreviewBelowWhenExactTableDoesNotFit(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(80, 12)
	created := time.Date(2026, 9, 5, 12, 34, 56, 0, time.UTC)
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "backlog-1234567890", Title: "Readable title", Description: "Description", Status: model.StatusOpen,
		IssueType: model.TypeFeature, Priority: 1, CreatedAt: created,
	}}}, 0)

	lines := strings.Split(ansi.Strip(b.View()), "\n")
	timestampLine, titleLine := -1, -1
	for index, line := range lines {
		if strings.Contains(line, formatBacklogCreatedAt(created)) {
			timestampLine = index
		}
		if strings.Contains(line, "TITLE  Readable title") {
			titleLine = index
		}
	}
	if timestampLine < 0 || titleLine <= timestampLine {
		t.Fatalf("preview was not moved below the complete table: timestamp=%d title=%d:\n%s", timestampLine, titleLine, strings.Join(lines, "\n"))
	}
}

func TestBacklogPreviewIsBoundedAndScrollable(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(80, 8)
	description := "first line " + strings.Repeat("middle ", 100) + "FINALTAIL"
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "backlog-1234567890", Title: "Readable title", Description: description, Status: model.StatusOpen,
		IssueType: model.TypeTask, CreatedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}}}, 0)

	full := ansi.Strip(b.renderBacklogPreview(76))
	if !strings.Contains(full, "Readable title") || !strings.Contains(full, "FINALTAIL") {
		t.Fatalf("complete preview content was lost: %q", full)
	}
	view := ansi.Strip(b.View())
	if !strings.Contains(view, "CREATED_AT") || strings.Count(view, "\n")+1 > 8 {
		t.Fatalf("preview pushed the backlog outside its allocation:\n%s", view)
	}
	b.ScrollPreview(10000)
	scrolled := ansi.Strip(b.View())
	if !strings.Contains(scrolled, "FINALTAIL") || strings.Count(scrolled, "\n")+1 > 8 {
		t.Fatalf("preview did not scroll within its allocation:\n%s", scrolled)
	}

	m := NewModel(nil, nil, "")
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "backlog-1", Description: description}}}, 0)
	updated, _ := m.Update(keyMsg("pgdown"))
	if updated.(*Model).backlog.previewOffset == 0 {
		t.Fatal("page-down did not scroll the backlog preview")
	}
}

func TestBacklogMarksSubmitOneBatchAndPreserveMarksOnFailure(t *testing.T) {
	var got ScopeMutation
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Mutate: func(_ context.Context, mutation ScopeMutation) error {
			got = mutation
			return errors.New("capacity")
		},
	}})
	m.activeScope = &ScopeInfo{ID: "today", Active: true}
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1"}, {ID: "b-2"}}}, 0)
	for _, key := range []string{"space", "j", "space", "A"} {
		updated, cmd := m.Update(keyMsg(key))
		m = updated.(*Model)
		if key == "A" {
			if cmd == nil {
				t.Fatal("marked add did not start")
			}
			updated, _ = m.Update(cmd())
			m = updated.(*Model)
		}
	}
	if got.Kind != ScopeMutationAdd || got.ScopeID != "today" || strings.Join(got.IssueIDs, ",") != "b-1,b-2" {
		t.Fatalf("batch mutation=%#v, want add today [b-1 b-2]", got)
	}
	if m.backlog.MarkCount() != 2 {
		t.Fatalf("failed mutation cleared marks: %d", m.backlog.MarkCount())
	}
	if !strings.Contains(ansi.Strip(m.backlog.View()), "✓") {
		t.Fatal("selected marked backlog row was not visibly marked")
	}
}

func TestScopeMemberMarksSubmitOneBatchRemoveAndClearOnFilter(t *testing.T) {
	var got ScopeMutation
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Mutate: func(_ context.Context, mutation ScopeMutation) error { got = mutation; return nil },
	}})
	m.activeScope = &ScopeInfo{ID: "today", Active: true}
	m.showScopePicker, m.focused = true, focusScopePicker
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today", Name: "Today"}})
	m.scopePicker.memberFocused = true
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "b-1"}}, {Issue: model.Issue{ID: "b-2"}}})
	for _, key := range []string{"space", "j", "space", "R"} {
		updated, cmd := m.Update(keyMsg(key))
		m = updated.(*Model)
		if key == "R" {
			if cmd == nil {
				t.Fatal("marked remove did not start")
			}
			updated, _ = m.Update(cmd())
			m = updated.(*Model)
		}
	}
	if got.Kind != ScopeMutationRemove || got.ScopeID != "today" || strings.Join(got.IssueIDs, ",") != "b-1,b-2" {
		t.Fatalf("batch mutation=%#v, want remove today [b-1 b-2]", got)
	}
	if m.scopePicker.MemberMarkCount() != 0 {
		t.Fatal("successful member mutation retained marks")
	}
	m.scopePicker.memberFocused = true
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "b-1"}}})
	m.scopePicker.ToggleMemberMark()
	m.scopePicker.ToggleMemberStatus("open")
	if m.scopePicker.MemberMarkCount() != 0 {
		t.Fatal("member filter retained marks")
	}
}

func TestScopeMemberMarksAcceptPhysicalSpaceInputsAndSubmitBatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "bubble tea key space", key: tea.KeyMsg{Type: tea.KeySpace}},
		{name: "space rune", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got ScopeMutation
			m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
				Mutate: func(_ context.Context, mutation ScopeMutation) error { got = mutation; return nil },
			}})
			m.showScopePicker, m.focused = true, focusScopePicker
			m.scopePicker.SetScopes([]ScopeInfo{{ID: "today", Name: "Today"}})
			m.scopePicker.memberFocused = true
			m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "b-1"}}, {Issue: model.Issue{ID: "b-2"}}})

			for _, key := range []tea.KeyMsg{tc.key, keyMsg("j"), tc.key} {
				updated, _ := m.Update(key)
				m = updated.(*Model)
			}
			updated, cmd := m.Update(keyMsg("R"))
			m = updated.(*Model)
			if cmd == nil {
				t.Fatal("marked remove did not start")
			}
			updated, _ = m.Update(cmd())
			m = updated.(*Model)

			if got.Kind != ScopeMutationRemove || got.ScopeID != "today" || strings.Join(got.IssueIDs, ",") != "b-1,b-2" {
				t.Fatalf("batch mutation=%#v, want remove today [b-1 b-2]", got)
			}
		})
	}
}

func TestScopeMatchPromptRoutesEpicOrLabelAndCancelPreservesMarks(t *testing.T) {
	var got ScopeMutation
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		MutateMatching: func(_ context.Context, mutation ScopeMutation) error { got = mutation; return nil },
	}})
	m.activeScope = &ScopeInfo{ID: "today", Active: true}
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "b-1"}}}, 0)
	m.backlog.ToggleMark()
	updated, cmd := m.Update(keyMsg("M"))
	m = updated.(*Model)
	if !m.showScopeMatchPrompt || m.backlog.MarkCount() != 1 {
		t.Fatalf("match prompt state: cmd=%t prompt=%t marks=%d", cmd != nil, m.showScopeMatchPrompt, m.backlog.MarkCount())
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(*Model)
	if m.showScopeMatchPrompt || m.backlog.MarkCount() != 1 {
		t.Fatal("cancel changed the marked backlog state")
	}
	updated, _ = m.Update(keyMsg("M"))
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg("label:team"))
	m = updated.(*Model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("semantic match did not start")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if got.Kind != ScopeMutationAdd || got.ScopeID != "today" || got.Label != "team" || got.EpicID != "" {
		t.Fatalf("semantic mutation=%#v", got)
	}
	if m.backlog.MarkCount() != 0 {
		t.Fatal("successful semantic mutation retained marks")
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
	if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "enter move") || strings.Contains(footer, "enter toggle") {
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
