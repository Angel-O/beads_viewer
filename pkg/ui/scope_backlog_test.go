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
	if !containsText(view, "No active scope — press W to choose or create a scope, or B for Global issues.") {
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
	m.activeScope = &ScopeInfo{Name: "Today", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 7, MemberLimit: 100, Active: true}

	badge := strings.TrimSpace(ansi.Strip(m.renderScopeBadge()))
	if badge != "Today · 7/100" {
		t.Fatalf("active scope badge = %q, want %q", badge, "Today · 7/100")
	}
}

func TestScopeCountsSurviveLegacySnapshotAndSyncFromCatalog(t *testing.T) {
	initial := ScopeInfo{ID: "s1", Name: "Today", MemberCount: 4, MemberLimit: 37, MemberCountKnown: true, MemberLimitKnown: true, Active: true}
	m := NewModel(nil, nil, "", RuntimeServices{
		Scopes: ScopeServices{
			Load:         func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
			QueryCatalog: func(context.Context, ScopeCatalogQuery) (ScopeCatalogPage, error) { return ScopeCatalogPage{}, nil },
		},
		InitialScope: &ScopeSnapshot{Scopes: []ScopeInfo{initial}, Active: &initial},
	})
	updated, _ := m.Update(scopeSnapshotMsg{snapshot: ScopeSnapshot{
		Scopes: []ScopeInfo{{ID: "s1", Name: "Today"}},
		Active: &ScopeInfo{ID: "s1", Name: "Today", Active: true},
	}})
	m = updated.(*Model)
	if m.activeScope == nil || m.activeScope.MemberCount != 4 || m.activeScope.MemberLimit != 37 {
		t.Fatalf("legacy snapshot erased known scope counts: %#v", m.activeScope)
	}
	generation := m.scopePicker.BeginCatalogLoad()
	updated, _ = m.Update(scopeCatalogPageMsg{
		page:       ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "Today", MemberCount: 9, MemberCountKnown: true}}},
		generation: generation,
	})
	m = updated.(*Model)
	if m.activeScope == nil || m.activeScope.MemberCount != 9 || m.activeScope.MemberLimit != 37 {
		t.Fatalf("catalog did not update active count while preserving limit: %#v", m.activeScope)
	}
	if got := strings.TrimSpace(ansi.Strip(m.renderScopeBadge())); got != "Today · 9/37" {
		t.Fatalf("capacity badge = %q, want %q", got, "Today · 9/37")
	}
}

func TestScopeProgressRendersCompletedAgainstCompleteMemberCount(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(100, 20)
	picker.SetScopes([]ScopeInfo{
		{ID: "s1", Name: "Today", MemberCount: 10, CompletedCount: 3, MemberCountKnown: true, CompletedCountKnown: true},
	})
	view := ansi.Strip(picker.View())
	if strings.Count(view, "completed: 3/10") < 2 {
		t.Fatalf("completed/member progress missing from selector and member panel:\n%s", view)
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
			backlogQueries := 0
			m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
				Activate:   func(_ context.Context, id string) error { activations++; activatedID = id; return nil },
				Deactivate: func(context.Context) error { deactivations++; return nil },
				QueryBacklog: func(context.Context, BacklogQuery) (BacklogPage, error) {
					backlogQueries++
					return BacklogPage{}, nil
				},
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
			updated, refresh := m.Update(cmd())
			m = updated.(*Model)
			for _, message := range runUISemanticCommands(refresh) {
				updated, _ = m.Update(message)
				m = updated.(*Model)
			}
			if backlogQueries != 0 {
				t.Fatalf("scope toggle issued %d backlog queries", backlogQueries)
			}
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
	header, entry, detail := -1, -1, -1
	for index, line := range lines {
		switch {
		case strings.Contains(line, "Scopes"):
			header = index
		case strings.Contains(line, "▸ Today"):
			entry = index
		case strings.Contains(line, "created: 2026-01-02 · members: 2"):
			detail = index
		}
	}
	if header < 0 || entry < 0 || detail < 0 || entry != header+2 || detail != entry+1 {
		t.Fatalf("scope header spacing missing:\n%s", view)
	}
	for _, hint := range []string{"enter activate", "n new scope", "esc back", "enter move bead"} {
		if strings.Contains(view, hint) {
			t.Fatalf("scope picker retained local hint %q:\n%s", hint, view)
		}
	}
}

func TestScopeScreenUsesNarrowSelectorAndBottomGlobalIssues(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load: func(context.Context) (ScopeSnapshot, error) { return ScopeSnapshot{}, nil },
	}})
	m.width, m.height, m.showScopePicker, m.ready = 240, 30, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today", Active: true}})
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1", Title: "Member", Status: model.StatusOpen}}})
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global-1", Title: "Global issue", Status: model.StatusOpen}}}, 0)

	top := ansi.Strip(m.scopePicker.renderTopSplit(120, 14, true))
	if lipgloss.Width(top) != 120 || lipgloss.Height(top) != 14 {
		t.Fatalf("top split dimensions = %dx%d, want 120x14", lipgloss.Width(top), lipgloss.Height(top))
	}
	line := strings.Split(top, "\n")[1]
	if strings.Index(line, "Members")-strings.Index(line, "Scopes") < 20 {
		t.Fatalf("Scope selector was not significantly narrower than members: %q", line)
	}

	screen := ansi.Strip(m.renderScopeScreen())
	if !strings.Contains(screen, "Members · Today") || !strings.Contains(screen, "Global issues") || !strings.Contains(screen, "global-1") {
		t.Fatalf("Scope screen lost one of its panels:\n%s", screen)
	}
	if lipgloss.Height(screen) != m.height-1 || lipgloss.Width(screen) != m.width {
		t.Fatalf("Scope screen dimensions = %dx%d, want %dx%d", lipgloss.Width(screen), lipgloss.Height(screen), m.width, m.height-1)
	}
	if m.backlog.width != m.width-2 || m.backlog.height != (m.height-1-(m.height-1)/2)-2 {
		t.Fatalf("lower backlog viewport=%dx%d, want frame content %dx%d", m.backlog.width, m.backlog.height, m.width-2, (m.height-1-(m.height-1)/2)-2)
	}
	lines := strings.Split(screen, "\n")
	rightEdge := func(line string) int { return lipgloss.Width(strings.TrimRight(line, " ")) }
	if got, want := rightEdge(lines[0]), rightEdge(lines[(m.height-1)/2]); got != want {
		t.Fatalf("top/lower right edges=%d/%d, want aligned", got, want)
	}
}

func TestScopeTopSplitHighlightsOnlyTheFocusedPane(t *testing.T) {
	sentinel := "x"
	catalog, members := scopeTopSplitStyles(true, false)
	if catalog.Render(sentinel) != FocusedPanelStyle.Render(sentinel) || members.Render(sentinel) != PanelStyle.Render(sentinel) {
		t.Fatal("catalog focus did not highlight only the catalog")
	}
	catalog, members = scopeTopSplitStyles(true, true)
	if catalog.Render(sentinel) != PanelStyle.Render(sentinel) || members.Render(sentinel) != FocusedPanelStyle.Render(sentinel) {
		t.Fatal("member focus did not highlight only the members")
	}
	for _, memberFocused := range []bool{false, true} {
		catalog, members = scopeTopSplitStyles(false, memberFocused)
		if catalog.Render(sentinel) != PanelStyle.Render(sentinel) || members.Render(sentinel) != PanelStyle.Render(sentinel) {
			t.Fatal("Global issues focus retained a top-pane highlight")
		}
	}
}

func TestScopeTopSplitUsesDisplayedMemberRowsForPagingAndMoveHeading(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 120, 29, true, true
	m.focused = focusScopePicker
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	m.scopePicker.SetMoveTarget("Visible issue")
	items := make([]IssueItem, 27)
	for i := range items {
		items[i].Issue.ID = fmt.Sprintf("member-%02d", i)
		items[i].Issue.Title = fmt.Sprintf("Member %02d", i)
	}
	m.scopePicker.SetMembers(items)

	view := ansi.Strip(m.renderScopeScreen())
	if !strings.Contains(view, "Move: Visible issue") {
		t.Fatalf("top split lost move heading:\n%s", view)
	}
	if got, want := m.scopePicker.memberViewportRows(), 9; got != want {
		t.Fatalf("displayed member rows=%d, want %d", got, want)
	}
	updated, _ := m.Update(keyMsg("tab"))
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg("right"))
	m = updated.(*Model)
	if got := m.scopePicker.memberViewportStart; got != 9 {
		t.Fatalf("right moved member viewport by %d rows, want 9", got)
	}
	if !strings.Contains(ansi.Strip(m.renderScopeScreen()), "screen 2/3") {
		t.Fatalf("member indicator disagrees after right:\n%s", ansi.Strip(m.renderScopeScreen()))
	}
	updated, _ = m.Update(keyMsg("left"))
	m = updated.(*Model)
	if m.scopePicker.memberViewportStart != 0 || !strings.Contains(ansi.Strip(m.renderScopeScreen()), "screen 1/3") {
		t.Fatalf("left did not return to first displayed member screen: start=%d", m.scopePicker.memberViewportStart)
	}
}

func TestScopeTopSplitMemberViewportUsesAvailablePanelHeight(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	items := make([]IssueItem, 20)
	for i := range items {
		items[i].Issue.ID = fmt.Sprintf("member-%02d", i)
	}
	picker.SetMembers(items)

	const height = 19
	view := ansi.Strip(picker.renderTopSplit(100, height, true))
	if got, want := picker.memberViewportRows(), height-5; got != want {
		t.Fatalf("member viewport rows=%d, want all available panel rows=%d", got, want)
	}
	shown := 0
	for _, item := range items {
		if strings.Contains(view, item.Issue.ID) {
			shown++
		}
	}
	if shown != height-5 {
		t.Fatalf("rendered member rows=%d, want %d without a blank band below the table", shown, height-5)
	}
}

func TestScopeScreenRetainsGlobalPreviewAndFilters(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 140, 40, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	m.backlog.SetLabel("team")
	m.backlog.AddFilter("global")
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "global-1", Title: "Global issue", Status: model.StatusOpen,
		Description: "Global description", Labels: []string{"team"},
	}}}, 0)

	view := ansi.Strip(m.renderScopeScreen())
	for _, want := range []string{"Global issues", "search: global", "label: team", "CONTEXT", "LABELS", "DESCRIPTION"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Global issues panel lost %q:\n%s", want, view)
		}
	}
}

func TestEmbeddedOutOfScopeResizeKeepsRowsAndIndicatorsInAgreement(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.showScopePicker, m.ready = 160, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	issues := make([]model.Issue, 25)
	for i := range issues {
		issues[i] = model.Issue{ID: fmt.Sprintf("outside-%02d", i), Title: "Outside", Status: model.StatusOpen}
	}
	m.backlog.SetPage(BacklogPage{Issues: issues, HasMore: true, NextCursor: "next"}, 0)
	for _, height := range []int{30, 22} {
		m.height = height
		view := ansi.Strip(m.renderScopeScreen())
		lines := strings.Split(view, "\n")
		bodyHeight := height - 1
		topHeight := bodyHeight / 2
		rows := m.backlog.height - 2
		visibleScreens := (len(issues) + rows - 1) / rows
		header := lines[topHeight+1]
		want := fmt.Sprintf("screen 1/%d+ · result batch 1/1+", visibleScreens)
		if !strings.Contains(header, want) {
			t.Fatalf("height=%d header=%q, want %q", height, header, want)
		}
		shown := 0
		for _, line := range lines[topHeight:] {
			if strings.Contains(line, "outside-") {
				shown++
			}
		}
		if shown != min(rows, len(issues)) {
			t.Fatalf("height=%d rendered rows=%d, want %d with content height %d", height, shown, min(rows, len(issues)), m.backlog.height)
		}
	}

	m.backlog.NextPageCursor()
	m.backlog.SetPage(BacklogPage{Issues: issues}, 1)
	view := ansi.Strip(m.renderScopeScreen())
	if !strings.Contains(view, "result batch 2/2") || strings.Contains(view, "page 2") {
		t.Fatalf("embedded backend page indicator/footer mismatch:\n%s", view)
	}
}

func TestEmbeddedOutOfScopeResizeRealignsViewportBoundary(t *testing.T) {
	b := NewBacklogModel(testTheme())
	issues := make([]model.Issue, 30)
	for i := range issues {
		issues[i] = model.Issue{ID: fmt.Sprintf("outside-%02d", i), Title: "Outside"}
	}
	b.SetPage(BacklogPage{Issues: issues}, 0)
	b.SetSize(120, 9)
	if got := b.embeddedViewportRows(b.displayTitle()); got != 7 {
		t.Fatalf("initial embedded rows=%d, want 7", got)
	}
	b.viewportStart = 7
	b.selected = 8
	b.selectedIssueID = issues[8].ID
	b.SetSize(120, 13)
	if got := b.embeddedViewportRows(b.displayTitle()); got != 11 {
		t.Fatalf("resized embedded rows=%d, want 11", got)
	}
	if b.viewportStart != 0 || b.selected != 8 {
		t.Fatalf("resize viewport start=%d selected=%d, want boundary 0 with selected row retained", b.viewportStart, b.selected)
	}
	view := ansi.Strip(b.renderBacklog(b.displayTitle(), true))
	if !strings.Contains(view, "screen 1/3") || !strings.Contains(view, "outside-08") {
		t.Fatalf("resized viewport indicator/selection mismatch:\n%s", view)
	}
}

func TestEmbeddedOutOfScopeUsesOnlyLowerFramePadding(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 120, 30, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	m.scopeMembershipIDs = map[string][]string{"s1": {"member"}}
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "outside", Title: "Outside", Status: model.StatusOpen}}}, 0)
	lines := strings.Split(ansi.Strip(m.renderScopeScreen()), "\n")
	lowerTop := (m.height - 1) / 2
	if got := displayOffset(lines[lowerTop+1], "Unscoped issues"); got != 2 {
		t.Fatalf("embedded lower title offset=%d, want one-cell frame padding: %q", got, lines[lowerTop+1])
	}
	if got := displayOffset(lines[lowerTop+2], "CONTEXT"); got != 6 {
		t.Fatalf("embedded table header offset=%d, want lower-frame padding plus cursor cells: %q", got, lines[lowerTop+2])
	}
}

func TestEmbeddedOutOfScopeHeaderUsesPanelWidthForLongFilters(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 140, 30, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	m.backlog.SetContextFilter([]string{"ctx:long"}, false, []string{"context-name-that-fits"})
	m.backlog.status = "status-name-that-fits"
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "outside", Title: "Outside", Status: model.StatusOpen}}}, 0)
	lines := strings.Split(ansi.Strip(m.renderScopeScreen()), "\n")
	header := lines[(m.height-1)/2+1]
	if !strings.Contains(header, "screen 1/1 · result batch 1/1") {
		t.Fatalf("embedded long filter header clipped its indicators: %q", header)
	}
}

func TestEmbeddedOutOfScopeWidePreviewClipsTableHeaderToTableWidth(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetSize(120, 12)
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "outside", Title: "Outside", Description: "Preview", Status: model.StatusOpen,
	}}}, 0)
	columns := backlogTableColumnsFor(b.filteredItems, b.width-2)
	listWidth := backlogTableWidth(columns)
	if listWidth > (b.width-2)*2/3 {
		t.Fatalf("test table width=%d does not leave a wide preview in %d columns", listWidth, b.width)
	}
	lines := strings.Split(ansi.Strip(b.renderBacklog(b.displayTitle(), true)), "\n")
	var tableHeader string
	for _, line := range lines {
		if strings.Contains(line, "CONTEXT") && strings.Contains(line, "CREATED_AT") {
			tableHeader = line
			break
		}
	}
	if tableHeader == "" || !strings.Contains(strings.Join(lines, "\n"), "TITLE") {
		t.Fatalf("wide embedded backlog omitted table or preview:\n%s", strings.Join(lines, "\n"))
	}
	if got := lipgloss.Width(tableHeader); got != listWidth {
		t.Fatalf("embedded table header width=%d, want table width %d:\n%s", got, listWidth, tableHeader)
	}
}

func TestScopeScreenDispatchesGlobalControlsAfterTopPanes(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 240, 30, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{
		{ID: "global-1", Title: "First"}, {ID: "global-2", Title: "Second"},
	}}, 0)
	m.focused = focusScopePicker

	updated, _ := m.Update(keyMsg("tab"))
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg("tab"))
	m = updated.(*Model)
	if m.focused != focusGlobalIssues {
		t.Fatalf("tab did not move from members to Global issues: %s", m.focused)
	}
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(*Model)
	if issue := m.backlog.CurrentIssue(); issue == nil || issue.ID != "global-2" {
		t.Fatalf("Global issues j navigation selected %#v", issue)
	}
	updated, _ = m.Update(keyMsg("/"))
	m = updated.(*Model)
	updated, _ = m.Update(keyMsg("global"))
	m = updated.(*Model)
	if !m.backlog.Searching() || m.backlog.Filter() != "global" {
		t.Fatalf("Global issues search did not own input: searching=%t filter=%q", m.backlog.Searching(), m.backlog.Filter())
	}
}

func TestScopeGlobalIssuesSearchConsumesViewSwitchKeys(t *testing.T) {
	for _, key := range []string{"b", "B", "g"} {
		t.Run(key, func(t *testing.T) {
			m := NewModel(nil, nil, "")
			m.showScopePicker = true
			m.focused = focusGlobalIssues
			m.backlog.BeginSearch()
			m.backlog.AddFilter("prefix")

			updated, _ := m.Update(keyMsg(key))
			m = updated.(*Model)
			if !m.backlog.Searching() || m.backlog.Filter() != "prefix"+key {
				t.Fatalf("search key %q was not consumed: searching=%t filter=%q", key, m.backlog.Searching(), m.backlog.Filter())
			}
			if !m.showScopePicker || m.focused != focusGlobalIssues || m.isBoardView || m.isGraphView {
				t.Fatalf("search key %q changed Scope view: picker=%t focus=%s board=%t graph=%t", key, m.showScopePicker, m.focused, m.isBoardView, m.isGraphView)
			}
		})
	}
}

func TestGlobalIssuesBReturnsToList(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.showScopePicker = true
	m.scopePickerOrigin = focusBoard
	m.focused = focusGlobalIssues
	m.isBoardView = true
	m.isGraphView = true
	m.isActionableView = true
	m.isHistoryView = true
	m.isSprintView = true
	m.showDetails = true

	updated, cmd := m.Update(keyMsg("B"))
	m = updated.(*Model)
	if cmd != nil || m.showScopePicker || m.focused != focusList || m.isBoardView || m.isGraphView || m.isActionableView || m.isHistoryView || m.isSprintView || m.showDetails {
		t.Fatalf("Global issues B state: cmd=%t scope=%t focus=%s board=%t graph=%t actionable=%t history=%t sprint=%t details=%t", cmd != nil, m.showScopePicker, m.focused, m.isBoardView, m.isGraphView, m.isActionableView, m.isHistoryView, m.isSprintView, m.showDetails)
	}
}

func TestScopeLowerPanelFrameFollowsGlobalFocus(t *testing.T) {
	sentinel := "x"
	if scopeLowerPanelStyle(true).Render(sentinel) != FocusedPanelStyle.Render(sentinel) {
		t.Fatal("Global issues focus did not use the focused panel frame")
	}
	if scopeLowerPanelStyle(false).Render(sentinel) != PanelStyle.Render(sentinel) {
		t.Fatal("unfocused Global issues panel did not use the normal panel frame")
	}
}

func TestScopeGlobalIssuesUsesCompleteSelectedMembership(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 160, 32, true, true
	m.focused = focusGlobalIssues
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today", Name: "Today", MemberCount: 2}})
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-first", Title: "Member first"}}})
	updated, _ := m.Update(loadScopeMembershipCmd(ScopeServices{
		LoadDetails: func(context.Context, string) (ScopeDetails, error) {
			return ScopeDetails{Info: ScopeInfo{ID: "today", MemberCount: 2}, MemberIDs: []string{"member-first", "member-beyond-first-page"}}, nil
		},
	}, "today")())
	m = updated.(*Model)
	m.backlog.AddFilter(" ")
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{
		{ID: "member-first", Title: "Member first"},
		{ID: "member-beyond-first-page", Title: "Member beyond first page"},
		{ID: "outside", Title: "Outside issue"},
	}}, 0)
	m.backlog.Move(2)
	m.backlog.ScrollPreview(1)

	view := ansi.Strip(m.renderScopeScreen())
	if !strings.Contains(view, "Unscoped issues") || !strings.Contains(view, "outside") || len(m.backlog.filtered) != 1 || m.backlog.filtered[0].ID != "outside" {
		t.Fatalf("selected-scope complement was not applied:\n%s", view)
	}
	if issue := m.backlog.CurrentIssue(); issue == nil || issue.ID != "outside" || m.backlog.previewOffset != 1 {
		t.Fatalf("selection/preview changed while applying complement: issue=%#v preview=%d", issue, m.backlog.previewOffset)
	}
}

func TestSelectedCatalogScopeOwnsMembersAndComplementAcrossReopen(t *testing.T) {
	loadedScopeID := ""
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		LoadDetails: func(_ context.Context, scopeID string) (ScopeDetails, error) {
			loadedScopeID = scopeID
			return ScopeDetails{
				Info:   ScopeInfo{ID: scopeID, Name: strings.TrimPrefix(scopeID, "scope-")},
				Issues: []model.Issue{{ID: scopeID + "-member", Title: scopeID + " member"}},
			}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{
		{ID: "scope-a", Name: "A", Active: true},
		{ID: "scope-b", Name: "B"},
	}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.scopePicker.Move(1)
	m.activeScope = &ScopeInfo{ID: "scope-a", Name: "A", Active: true}
	m.scopeMembershipIDs = map[string][]string{
		"scope-a": {"scope-a-member"},
	}
	m.scopeSessionInitialized = true
	m.showScopePicker = true
	m.scopePickerOrigin = focusList
	m.focused = focusScopePicker
	m.ready = true
	m.width, m.height = 160, 32
	for _, message := range runUISemanticCommands(m.loadSelectedScopeDetails()) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	if loadedScopeID != "scope-b" {
		t.Fatalf("selected scope member load used %q, want scope-b", loadedScopeID)
	}
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{
		{ID: "scope-a-member", Title: "A member"},
		{ID: "scope-b-member", Title: "B member"},
		{ID: "outside", Title: "Outside"},
	}}, 0)

	assertComplement := func(stage string) {
		t.Helper()
		if selected := m.scopePicker.SelectedScopeID(); selected != "scope-b" {
			t.Fatalf("%s selected scope=%q, want scope-b", stage, selected)
		}
		if len(m.scopePicker.filteredMembers) != 1 || m.scopePicker.filteredMembers[0].Issue.ID != "scope-b-member" {
			t.Fatalf("%s members=%v, want scope-b-member", stage, m.scopePicker.filteredMembers)
		}
		view := ansi.Strip(m.renderScopeScreen())
		if !strings.Contains(view, "Members · B") || !strings.Contains(view, "Unscoped issues") {
			t.Fatalf("%s scope panels missing B selection:\n%s", stage, view)
		}
		got := make([]string, 0, len(m.backlog.filtered))
		for _, issue := range m.backlog.filtered {
			got = append(got, issue.ID)
		}
		requireIssueIDs(t, got, "scope-a-member", "outside")
	}

	assertComplement("initial")
	m.closeScopePicker()
	if cmd := m.openScopePicker(""); cmd != nil {
		t.Fatal("reopening Scope issued an unexpected command")
	}
	assertComplement("reopen")
}

func TestScopeGlobalIssuesFallsBackToAllIssuesWithoutSelectedScope(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 160, 32, true, true
	m.focused = focusGlobalIssues
	m.scopePicker.SetScopes(nil)
	m.scopeMembershipIDs = map[string][]string{"other": {"member"}}
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "member", Title: "Member"}, {ID: "outside", Title: "Outside"}}}, 0)

	view := ansi.Strip(m.renderScopeScreen())
	if !strings.Contains(view, "Global issues") || !strings.Contains(view, "member") || !strings.Contains(view, "outside") || strings.Contains(view, "Unscoped issues") {
		t.Fatalf("no-scope lower panel was not the all-issues fallback:\n%s", view)
	}
}

func TestScopeAddRefreshesCompleteSelectedMembership(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Load: func(context.Context) (ScopeSnapshot, error) {
			return ScopeSnapshot{Scopes: []ScopeInfo{{ID: "today", Name: "Today"}}}, nil
		},
		LoadDetails: func(context.Context, string) (ScopeDetails, error) {
			return ScopeDetails{Info: ScopeInfo{ID: "today", MemberCount: 2}, MemberIDs: []string{"member-old", "member-new"}}, nil
		},
	}})
	m.showScopePicker = true
	m.focused = focusGlobalIssues
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today", Name: "Today"}})
	m.scopeMembershipIDs = map[string][]string{"today": {"member-old"}}
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{
		{ID: "member-new", Title: "New member"},
		{ID: "outside", Title: "Outside issue"},
	}}, 0)

	cmds := m.refreshAfterScopeMutation(ScopeMutation{Kind: ScopeMutationAdd, ScopeID: "today"})().(tea.BatchMsg)
	for _, child := range cmds {
		if details, ok := child().(scopeDetailsMsg); ok {
			updated, _ := m.Update(details)
			m = updated.(*Model)
		}
	}
	if got := strings.Join(m.scopeMembershipIDs["today"], ","); got != "member-new,member-old" {
		t.Fatalf("refreshed complete membership=%q, want member-new,member-old", got)
	}
	view := ansi.Strip(m.renderScopeScreen())
	if !strings.Contains(view, "Unscoped issues") || len(m.backlog.filtered) != 1 || m.backlog.filtered[0].ID != "outside" {
		t.Fatalf("added member remained in Unscoped issues:\n%s", view)
	}
}

func TestScopeTerminologyUsesUnscopedMatchAddAndCtx(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 160, 32, true, true
	m.focused = focusGlobalIssues
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today", Name: "Today"}})
	m.scopeMembershipIDs = map[string][]string{"today": {"member"}}
	m.showShortcutsSidebar = true

	help := strings.ToLower(ansi.Strip(m.renderHelpOverlay()))
	if !strings.Contains(help, "unscoped issues") || !strings.Contains(help, "match-add issues") || strings.Contains(help, "global issues") {
		t.Fatalf("selected-scope help wording = %q", help)
	}
	view := strings.ToLower(ansi.Strip(m.View()))
	if !strings.Contains(view, "unscoped issues") || !strings.Contains(view, "match-add") || strings.Contains(view, "global issues") {
		t.Fatalf("selected-scope sidebar/view wording = %q", view)
	}
	globalFooter := strings.ToLower(ansi.Strip(m.renderFooter()))
	if !strings.Contains(globalFooter, "m match-add") || strings.Contains(globalFooter, "m add scope") {
		t.Fatalf("Scope issue-panel footer wording = %q", globalFooter)
	}

	m.focused = focusScopePicker
	m.scopePicker.memberFocused = true
	help = strings.ToLower(ansi.Strip(m.renderHelpOverlay()))
	footer := strings.ToLower(ansi.Strip(m.renderFooter()))
	for _, want := range []string{"cycle member ctx filter", "match-remove members"} {
		if !strings.Contains(help, want) {
			t.Fatalf("scope member help missing %q: %q", want, help)
		}
	}
	for _, want := range []string{"w ctx", "m match-remove", "tab unscoped issues"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("scope member footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(help, "repository filter") || strings.Contains(help, "epic or label") || strings.Contains(footer, "repository") || strings.Contains(footer, "match-add") || strings.Contains(footer, "epic/label") {
		t.Fatalf("scope terminology retained stale wording: help=%q footer=%q", help, footer)
	}
}

func TestScopeScreenReplacesBacklogNavigationDocumentation(t *testing.T) {
	for _, doc := range GetKeyBindingDocs() {
		if strings.Contains(strings.ToLower(doc.Context), "backlog") || strings.Contains(strings.ToLower(doc.Desc), "global backlog") {
			t.Fatalf("stale Backlog navigation documentation: %+v", doc)
		}
	}
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 240, 30, true, true
	for _, focus := range []focus{focusScopePicker, focusGlobalIssues} {
		m.focused = focus
		help := strings.ToLower(ansi.Strip(m.renderHelpOverlay()))
		if strings.Contains(help, "backlog") {
			t.Fatalf("focus %s retained Backlog help text: %s", focus, help)
		}
	}
	footer := strings.ToLower(ansi.Strip(m.renderFooter()))
	if strings.Contains(footer, "backlog") {
		t.Fatalf("Scope footer navigation = %q", footer)
	}
}

func TestScopePickerCatalogUsesCompactRowsWhenPanelIsConstrained(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(80, 15)
	picker.SetScopes([]ScopeInfo{{Name: "Today", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 2}})

	view := ansi.Strip(picker.View())
	if !strings.Contains(view, "▸ Today · 2026-01-02/2") {
		t.Fatalf("compact catalog row missing:\n%s", view)
	}
	if strings.Contains(view, "created:") || strings.Contains(view, "members:") {
		t.Fatalf("constrained catalog rendered rich details:\n%s", view)
	}
}

func TestScopeScreenKeepsProgressVisibleInNormalSplitWidth(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 120, 30, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{
		ID: "s1", Name: "A longer scope name", MemberCount: 10, CompletedCount: 3,
		MemberCountKnown: true, CompletedCountKnown: true,
	}})

	view := ansi.Strip(m.renderScopeScreen())
	lines := strings.Split(view, "\n")
	selectorLines := lines[:min(14, len(lines))]
	selector := make([]string, len(selectorLines))
	for i, line := range selectorLines {
		selector[i] = ansi.Truncate(line, 30, "")
	}
	if !strings.Contains(strings.Join(selector, "\n"), "completed: 3/10") {
		t.Fatalf("normal split scope selector hid progress:\n%s", strings.Join(selector, "\n"))
	}
}

func TestScopePickerCompactCatalogBoundsRowsAtNarrowViewport(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetSize(30, 15)
	picker.SetScopes([]ScopeInfo{
		{Name: "A very long active scope name", Active: true, CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 123},
		{Name: "Selected later", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), MemberCount: 4},
	})
	picker.Move(1)

	view := ansi.Strip(picker.View())
	if !strings.Contains(view, "Selected later") {
		t.Fatalf("selected subsequent scope was clipped from narrow catalog:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > 30 {
			t.Fatalf("narrow picker row width=%d, want <= 30: %q", width, line)
		}
	}
}

func TestScopePickerCatalogStylesSelectedNameAndActiveScope(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)
	theme := DefaultTheme(renderer)
	picker := NewScopePickerModel(theme)
	picker.SetSize(80, 20)
	picker.SetScopes([]ScopeInfo{
		{Name: "Earlier", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), MemberCount: 1},
		{Name: "Current", Active: true, CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), MemberCount: 2},
	})

	view := picker.View()
	if want := theme.Renderer.NewStyle().Foreground(theme.Primary).Bold(true).Render("Current"); !strings.Contains(view, want) {
		t.Fatalf("selected scope name lost selected styling: %q", view)
	}
	if want := theme.Renderer.NewStyle().Foreground(theme.Open).Bold(true).Render("  (active)"); !strings.Contains(view, want) {
		t.Fatalf("active scope marker lost active styling: %q", view)
	}
}

func TestScopePickerCatalogWindowUsesTwoLineRowBounds(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{
		{Name: "One"}, {Name: "Two"}, {Name: "Three"},
		{Name: "Four"}, {Name: "Five"}, {Name: "Six"},
	})
	picker.Move(5)

	for _, rows := range []int{5, 6, 7} {
		catalog := ansi.Strip(picker.renderCatalog("Scopes", 60, rows))
		visible := (rows - 2) / 2
		if got := strings.Count(catalog, "created:"); got > visible {
			t.Fatalf("rows=%d rendered %d rich rows, want at most %d:\n%s", rows, got, visible, catalog)
		}
		if !strings.Contains(catalog, "Six") || !strings.Contains(catalog, "members: 0") {
			t.Fatalf("rows=%d scrolled selected row out of viewport:\n%s", rows, catalog)
		}
		if height := lipgloss.Height(catalog); height > rows {
			t.Fatalf("rows=%d catalog height=%d:\n%s", rows, height, catalog)
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

func TestScopeScreenFromGlobalIssuesReturnsToListAndReopensGlobalIssues(t *testing.T) {
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
	if !m.showScopePicker || m.isBacklogView || m.focused != focusGlobalIssues || cmd != nil {
		t.Fatalf("B transition: scope=%t backlog=%t focus=%s cmd=%t", m.showScopePicker, m.isBacklogView, m.focused, cmd != nil)
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

func TestScopeMemberWideRowKeepsTitleAndLabelsVisible(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	const title = "Widest displayed title"
	const labels = "repository-aware,viewer-scope-counts"
	picker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "member-1", Title: "Visible title", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: strings.Split(labels, ",")}},
		{Issue: model.Issue{ID: "member-2", Title: title, Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"frontend"}}},
		{Issue: model.Issue{ID: "member-3", Title: "An offscreen title establishes the existing maximum title allocation", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"backend"}}},
	})
	const width = 120
	maximum := scopeMemberColumnsFor(picker.filteredMembers, width)
	if maximum.title <= lipgloss.Width(title) {
		t.Fatalf("test data did not establish a wider existing title maximum: columns=%+v", maximum)
	}
	view := ansi.Strip(picker.renderMembers(width, 5))
	rows := make([]string, 0, 2)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "member-1") || strings.Contains(line, "member-2") {
			rows = append(rows, line)
		}
	}
	if len(rows) != 2 || !strings.Contains(rows[0], "Visible title") || !strings.Contains(rows[1], title) || !strings.Contains(rows[0], labels) {
		t.Fatalf("wide member rows clipped fitting title or labels:\n%s", view)
	}
	if labelAt := displayOffset(rows[0], labels); labelAt >= displayOffset(rows[0], "Visible title")+maximum.title {
		t.Fatalf("labels retained the offscreen title gap: maximum=%+v row=%q", maximum, rows[0])
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
			if !containsText(guidance, "No active scope") || !containsText(guidance, "Global issues") {
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

func TestHiddenScopePickerRetainsSelectedMembersAcrossActiveScopeMutation(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryCatalog: func(_ context.Context, _ ScopeCatalogQuery) (ScopeCatalogPage, error) {
			return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "scope-a", Name: "A"}, {ID: "scope-b", Name: "B"}}}, nil
		},
		LoadDetails: func(_ context.Context, scopeID string) (ScopeDetails, error) {
			return ScopeDetails{Info: ScopeInfo{ID: scopeID}, Issues: []model.Issue{{ID: scopeID + "-member"}}}, nil
		},
	}})
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "scope-a", Name: "A"}, {ID: "scope-b", Name: "B"}})
	m.scopePicker.Move(1)
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "scope-b-member"}}})
	m.showScopePicker = false

	for _, message := range runUISemanticCommands(m.refreshAfterScopeMutation(ScopeMutation{Kind: ScopeMutationRemove, ScopeID: "scope-a"})) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	if m.scopeDetails == nil || m.scopeDetails.Info.ID != "scope-a" {
		t.Fatalf("active-scope details were not refreshed: %#v", m.scopeDetails)
	}
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != "scope-b-member" {
		t.Fatalf("hidden picker replaced selected scope B members: %#v", selected)
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

func TestPagedScopePickerUsesViewportScreensBeforeFetchingNextMemberBatch(t *testing.T) {
	issues := make([]model.Issue, 53)
	for i := range issues {
		issues[i] = model.Issue{ID: fmt.Sprintf("member-%02d", i), Title: fmt.Sprintf("Member %02d", i)}
	}
	var requests []ScopeMembersQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			requests = append(requests, query)
			start := 0
			if query.Cursor != "" {
				start = 50
			}
			end := min(start+scopePageSize, len(issues))
			return ScopeMembersPage{
				Scope:   ScopeInfo{ID: query.ScopeID, Name: "Today", MemberCount: len(issues)},
				Members: issues[start:end], HasMore: end < len(issues), NextCursor: "members-2",
			}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "Today", MemberCount: len(issues)}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.scopePicker.SetSize(100, 24)
	for _, message := range runUISemanticCommands(m.openScopePicker("")) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	m.scopePicker.memberFocused = true
	visible := m.scopePicker.memberViewportRows()
	if visible != 5 {
		t.Fatalf("member viewport rows=%d, want 5", visible)
	}
	if len(requests) != 1 || requests[0].Cursor != "" || m.scopePicker.SelectedMember().Issue.ID != "member-00" {
		t.Fatalf("initial member page: requests=%#v selected=%#v", requests, m.scopePicker.SelectedMember())
	}
	m.scopePicker.ToggleMemberMark()

	for i := 0; i < visible; i++ {
		updated, cmd := m.handleScopePickerKey(keyMsg("down"))
		m = updated
		if cmd != nil || len(requests) != 1 {
			t.Fatalf("row navigation unexpectedly fetched: cmd=%t requests=%#v", cmd != nil, requests)
		}
	}
	if m.scopePicker.memberViewportStart != visible || m.scopePicker.SelectedMember().Issue.ID != "member-05" {
		t.Fatalf("down navigation viewport start=%d selected=%#v, want %d/member-05", m.scopePicker.memberViewportStart, m.scopePicker.SelectedMember(), visible)
	}
	if view := ansi.Strip(m.scopePicker.View()); !strings.Contains(view, "screen 2/10+ · result batch 1/1+") {
		t.Fatalf("down navigation indicator=%q", view)
	}

	updated, cmd := m.handleScopePickerKey(keyMsg("right"))
	m = updated
	if cmd != nil || len(requests) != 1 || m.scopePicker.memberViewportStart != visible*2 || m.scopePicker.SelectedMember().Issue.ID != "member-10" {
		t.Fatalf("right after row navigation: cmd=%t requests=%d start=%d selected=%#v", cmd != nil, len(requests), m.scopePicker.memberViewportStart, m.scopePicker.SelectedMember())
	}
	if view := ansi.Strip(m.scopePicker.View()); !strings.Contains(view, "screen 3/10+ · result batch 1/1+") {
		t.Fatalf("right after row navigation indicator=%q", view)
	}
	updated, cmd = m.handleScopePickerKey(keyMsg("left"))
	m = updated
	if cmd != nil || len(requests) != 1 || m.scopePicker.memberViewportStart != visible || m.scopePicker.SelectedMember().Issue.ID != "member-05" || m.scopePicker.MemberMarkCount() != 1 {
		t.Fatalf("left after row navigation: cmd=%t requests=%d start=%d selected=%#v", cmd != nil, len(requests), m.scopePicker.memberViewportStart, m.scopePicker.SelectedMember())
	}
	if view := ansi.Strip(m.scopePicker.View()); !strings.Contains(view, "screen 2/10+ · result batch 1/1+") {
		t.Fatalf("left after row navigation indicator=%q", view)
	}

	for screen := 3; screen <= 10; screen++ {
		updated, cmd := m.handleScopePickerKey(keyMsg("right"))
		m = updated
		if cmd != nil || len(requests) != 1 {
			t.Fatalf("screen %d unexpectedly fetched: cmd=%t requests=%#v", screen, cmd != nil, requests)
		}
		want := fmt.Sprintf("member-%02d", (screen-1)*visible)
		if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != want {
			t.Fatalf("screen %d selected=%#v, want %s", screen, selected, want)
		}
	}

	updated, cmd = m.handleScopePickerKey(keyMsg("right"))
	m = updated
	if cmd == nil || len(requests) != 1 {
		t.Fatalf("boundary navigation: cmd=%t requests=%d, want one pending fetch", cmd != nil, len(requests))
	}
	updatedTea, _ := m.Update(cmd())
	m = updatedTea.(*Model)
	if len(requests) != 2 || requests[1].Cursor != "members-2" || requests[1].Limit != scopePageSize {
		t.Fatalf("boundary request=%#v, want cursor members-2 and limit %d", requests, scopePageSize)
	}
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != "member-50" {
		t.Fatalf("final screen selected=%#v, want member-50", selected)
	}
	if m.scopePicker.MemberMarkCount() != 0 {
		t.Fatalf("backend batch navigation retained old page mark count=%d", m.scopePicker.MemberMarkCount())
	}
	view := ansi.Strip(m.scopePicker.View())
	if !strings.Contains(view, "screen 1/1 · result batch 2/2") {
		t.Fatalf("final viewport/batch indicator=%q", view)
	}

	updated, cmd = m.handleScopePickerKey(keyMsg("left"))
	m = updated
	if cmd == nil || len(requests) != 2 {
		t.Fatalf("reverse batch navigation: cmd=%t requests=%d", cmd != nil, len(requests))
	}
	updatedTea, _ = m.Update(cmd())
	m = updatedTea.(*Model)
	if len(requests) != 3 || requests[2].Cursor != "" || requests[2].Limit != scopePageSize {
		t.Fatalf("reverse request=%#v, want first-page cursor and limit %d", requests, scopePageSize)
	}
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != "member-00" {
		t.Fatalf("previous page selected=%#v, want first incoming member", selected)
	}
	if view := ansi.Strip(m.scopePicker.View()); !strings.Contains(view, "screen 1/10+ · result batch 1/2+") {
		t.Fatalf("previous page indicator=%q", view)
	}

	m.scopePicker.SetMemberFilters("", "open", "")
	if m.scopePicker.memberViewportStart != 0 || m.scopePicker.SelectedMember().Issue.ID != "member-00" {
		t.Fatalf("member filter did not reset viewport: start=%d selected=%#v", m.scopePicker.memberViewportStart, m.scopePicker.SelectedMember())
	}
	m.scopePicker.SetSize(100, 23)
	if m.scopePicker.memberViewportStart != 0 {
		t.Fatalf("resize did not reset viewport start=%d", m.scopePicker.memberViewportStart)
	}
}

func TestPagedOutOfScopeUsesViewportScreensBeforeFetchingNextBatch(t *testing.T) {
	issues := make([]model.Issue, 53)
	for i := range issues {
		issues[i] = model.Issue{ID: fmt.Sprintf("outside-%02d", i), Title: fmt.Sprintf("Outside %02d", i)}
	}
	var requests []BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			requests = append(requests, query)
			start := 0
			if query.Cursor != "" {
				start = 50
			}
			end := min(start+backlogPageSize, len(issues))
			return BacklogPage{Issues: issues[start:end], HasMore: end < len(issues), NextCursor: "backlog-2"}, nil
		},
	}})
	m.width, m.height, m.showScopePicker, m.ready = 100, 24, true, true
	m.focused = focusGlobalIssues
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}})
	m.scopeMembershipIDs = map[string][]string{"s1": {"member"}}
	m.backlog.SetPage(BacklogPage{Issues: issues[:50], HasMore: true, NextCursor: "backlog-2"}, 0)
	header := func() string {
		return strings.Split(ansi.Strip(m.renderScopeScreen()), "\n")[(m.height-1)/2+1]
	}
	_ = header()
	visible := m.backlog.embeddedViewportRows(m.backlog.displayTitle())
	visibleScreens := (50 + visible - 1) / visible
	if selected := m.backlog.CurrentIssue(); selected == nil || selected.ID != "outside-00" {
		t.Fatalf("initial Out-of-scope selection=%#v", selected)
	}
	if !strings.Contains(header(), fmt.Sprintf("screen 1/%d+ · result batch 1/1+", visibleScreens)) {
		t.Fatalf("initial Out-of-scope indicator=%q", header())
	}

	for screen := 2; screen <= visibleScreens; screen++ {
		updated, cmd := m.handleBacklogKey(keyMsg("right"))
		m = updated
		if cmd != nil || m.backlog.viewportStart != (screen-1)*visible {
			t.Fatalf("screen %d navigation: cmd=%t start=%d", screen, cmd != nil, m.backlog.viewportStart)
		}
		if !strings.Contains(header(), fmt.Sprintf("screen %d/%d+ · result batch 1/1+", screen, visibleScreens)) {
			t.Fatalf("screen %d indicator=%q", screen, header())
		}
	}

	updated, cmd := m.handleBacklogKey(keyMsg("right"))
	m = updated
	if cmd == nil || m.backlog.viewportStart != (visibleScreens-1)*visible {
		t.Fatalf("batch boundary navigation: cmd=%t start=%d", cmd != nil, m.backlog.viewportStart)
	}
	updatedTea, _ := m.Update(cmd())
	m = updatedTea.(*Model)
	if len(requests) != 1 || requests[0].Cursor != "backlog-2" {
		t.Fatalf("next batch request=%#v", requests)
	}
	if selected := m.backlog.CurrentIssue(); selected == nil || selected.ID != "outside-50" {
		t.Fatalf("next batch selection=%#v, want outside-50", selected)
	}
	if !strings.Contains(header(), "screen 1/1 · result batch 2/2") {
		t.Fatalf("next batch indicator=%q", header())
	}

	updated, cmd = m.handleBacklogKey(keyMsg("left"))
	m = updated
	if cmd == nil || m.backlog.viewportStart != 0 {
		t.Fatalf("previous batch boundary: cmd=%t start=%d", cmd != nil, m.backlog.viewportStart)
	}
	updatedTea, _ = m.Update(cmd())
	m = updatedTea.(*Model)
	if len(requests) != 2 || requests[1].Cursor != "" {
		t.Fatalf("previous batch request=%#v", requests)
	}
	if selected := m.backlog.CurrentIssue(); selected == nil || selected.ID != "outside-00" {
		t.Fatalf("previous batch selection=%#v, want outside-00", selected)
	}
	if !strings.Contains(header(), fmt.Sprintf("screen 1/%d+ · result batch 1/2+", visibleScreens)) {
		t.Fatalf("previous batch indicator=%q", header())
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
	if m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.CatalogPageIndex() != 0 || m.scopePicker.MemberMarkCount() != 1 {
		t.Fatalf("normal return reset retained picker state: member=%d catalog=%d marks=%d", m.scopePicker.MemberPageIndex(), m.scopePicker.CatalogPageIndex(), m.scopePicker.MemberMarkCount())
	}
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "reset-me"}}}, 0)
	m.backlog.ToggleMark()
	m.backlog.Reset()
	m.scopePicker.ResetPaging()
	if m.backlog.CurrentIssue() != nil || m.backlog.MarkCount() != 0 || m.scopePicker.MemberMarkCount() != 0 {
		t.Fatal("explicit reset did not clear destructive session state")
	}
}

func TestNormalScopeViewSwitchesSuspendAndResumeWithoutReload(t *testing.T) {
	for _, tc := range []struct {
		name      string
		key       string
		focus     focus
		member    bool
		wantBoard bool
		wantGraph bool
	}{
		{name: "list", key: "B", focus: focusGlobalIssues},
		{name: "board", key: "b", focus: focusScopePicker, member: true, wantBoard: true},
		{name: "graph", key: "g", focus: focusScopePicker, member: true, wantGraph: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalogLoads, memberLoads, backlogLoads := 0, 0, 0
			m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
				QueryCatalog: func(context.Context, ScopeCatalogQuery) (ScopeCatalogPage, error) {
					catalogLoads++
					return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "Today"}}}, nil
				},
				QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
					memberLoads++
					return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID}, Members: []model.Issue{{ID: "member-1"}}}, nil
				},
				QueryBacklog: func(context.Context, BacklogQuery) (BacklogPage, error) {
					backlogLoads++
					return BacklogPage{Issues: []model.Issue{{ID: "global-1", Title: "keep me"}, {ID: "global-2", Title: "keep me too"}}}, nil
				},
			}})
			m.showScopePicker = true
			m.scopeSessionInitialized = true
			m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "Earlier"}, {ID: "s2", Name: "Selected"}, {ID: "s3", Name: "Later"}}
			m.scopePicker.SetScopes(m.scopeCatalog)
			m.scopePicker.Move(1)
			m.scopePicker.catalogPageIndex = 1
			m.scopePicker.catalogPageCursors = []string{"", "catalog-2"}
			m.scopePicker.catalogHasMore = true
			m.scopePicker.catalogNextCursor = "catalog-3"
			m.scopePicker.SetMembers([]IssueItem{
				{Issue: model.Issue{ID: "member-1", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryName: "repo"},
				{Issue: model.Issue{ID: "member-2", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryName: "repo"},
			})
			m.scopePicker.SetMemberFilters("repo", "open", model.TypeTask)
			m.scopePicker.MoveMember(1)
			m.scopePicker.ToggleMemberMark()
			m.scopePicker.memberFocused = tc.member
			m.scopePicker.memberPageIndex = 1
			m.scopePicker.memberPageCursors = []string{"", "members-2"}
			m.scopePicker.memberHasMore = true
			m.scopePicker.memberNextCursor = "members-3"
			m.scopePicker.memberViewportStart = 1
			m.backlog.SetLabel("team")
			m.backlog.AddFilter("keep")
			m.backlog.CycleStatus()
			m.backlog.SetContextFilter([]string{"ctx:alpha"}, true, []string{"alpha"})
			m.backlog.SetPage(BacklogPage{HasMore: true, NextCursor: "backlog-2"}, 0)
			m.backlog.NextPageCursor()
			m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global-1", Title: "keep me"}, {ID: "global-2", Title: "keep me too"}}}, 1)
			m.backlog.Move(1)
			m.backlog.ToggleMark()
			m.backlog.ScrollPreview(2)
			m.focused = tc.focus
			before := [3]int{catalogLoads, memberLoads, backlogLoads}
			wantScopeID := m.scopePicker.SelectedScopeID()
			wantMemberID := m.scopePicker.SelectedMember().Issue.ID
			wantIssueID := m.backlog.CurrentIssue().ID

			updated, cmd := m.Update(keyMsg(tc.key))
			m = updated.(*Model)
			if cmd != nil || m.showScopePicker || m.isBoardView != tc.wantBoard || m.isGraphView != tc.wantGraph {
				t.Fatalf("switch key=%q: cmd=%t scope=%t board=%t graph=%t", tc.key, cmd != nil, m.showScopePicker, m.isBoardView, m.isGraphView)
			}
			if updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); updated.(*Model).scopePicker.SelectedScopeID() != wantScopeID {
				t.Fatalf("hidden resize changed selected scope")
			} else {
				m = updated.(*Model)
			}

			if cmd = m.openScopePicker(""); cmd != nil {
				t.Fatalf("resume key=%q reloaded Scope", tc.key)
			}
			if !m.showScopePicker || m.focused != tc.focus || m.scopePicker.memberFocused != tc.member {
				t.Fatalf("resume key=%q lost pane focus: shown=%t focus=%s member=%t", tc.key, m.showScopePicker, m.focused, m.scopePicker.memberFocused)
			}
			if m.scopePicker.SelectedScopeID() != wantScopeID || m.scopePicker.CatalogPageIndex() != 1 || m.scopePicker.MemberPageIndex() != 1 || m.scopePicker.SelectedMember() == nil || m.scopePicker.SelectedMember().Issue.ID != wantMemberID || m.scopePicker.MemberMarkCount() != 1 {
				t.Fatalf("resume key=%q lost catalog/member state: scope=%q page=%d memberPage=%d member=%#v marks=%d", tc.key, m.scopePicker.SelectedScopeID(), m.scopePicker.CatalogPageIndex(), m.scopePicker.MemberPageIndex(), m.scopePicker.SelectedMember(), m.scopePicker.MemberMarkCount())
			}
			repositoryFilter, statusFilter, typeFilter := m.scopePicker.MemberFilters()
			if repositoryFilter != "repo" || statusFilter != "open" || typeFilter != model.TypeTask || m.scopePicker.memberViewportStart != 1 {
				t.Fatalf("resume key=%q lost member filters/viewport: %q/%q/%q start=%d", tc.key, repositoryFilter, statusFilter, typeFilter, m.scopePicker.memberViewportStart)
			}
			if m.backlog.Filter() != "keep" || m.backlog.Label() != "team" || m.backlog.Status() != "open" || m.backlog.PageIndex() != 1 || m.backlog.CurrentPageCursor() != "backlog-2" || m.backlog.MarkCount() != 1 || m.backlog.CurrentIssue() == nil || m.backlog.CurrentIssue().ID != wantIssueID || m.backlog.previewOffset != 2 || !m.backlog.IncludeContextless() {
				t.Fatalf("resume key=%q lost backlog state: filter=%q label=%q status=%q page=%d cursor=%q marks=%d current=%#v preview=%d contextless=%t", tc.key, m.backlog.Filter(), m.backlog.Label(), m.backlog.Status(), m.backlog.PageIndex(), m.backlog.CurrentPageCursor(), m.backlog.MarkCount(), m.backlog.CurrentIssue(), m.backlog.previewOffset, m.backlog.IncludeContextless())
			}
			if got := [3]int{catalogLoads, memberLoads, backlogLoads}; got != before {
				t.Fatalf("resume key=%q reloaded services: before=%v after=%v", tc.key, before, got)
			}
		})
	}
}

func TestScopeFilterEditorCleanupRetainsValues(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.backlog.BeginSearch()
	m.backlog.AddFilter("search-value")
	m.backlog.BeginLabelEdit()
	m.backlog.UpdateLabelInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("label-value")})

	m.endScopeFilterEditing()
	if m.backlog.Searching() || m.backlog.LabelEditing() {
		t.Fatalf("filter cleanup left editor active: search=%t label=%t", m.backlog.Searching(), m.backlog.LabelEditing())
	}
	if m.backlog.Filter() != "search-value" || m.backlog.LabelInputValue() != "label-value" {
		t.Fatalf("filter values changed while ending editors: filter=%q label=%q", m.backlog.Filter(), m.backlog.LabelInputValue())
	}
}

func TestMoveDestinationSetupAndCleanupLeaveRetainedScopeSessionIntact(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "move-me", Title: "Move me"}}, nil, "")
	m.scopeSessionInitialized = true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Today"}, {ID: "s2", Name: "Later"}})
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1"}}})
	m.scopePicker.memberFocused = true
	m.backlog.AddFilter("keep")
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global-1", Title: "keep me"}}}, 0)
	m.backlog.ToggleMark()
	m.focused = focusDetail

	if cmd := m.openScopePicker("move-me"); cmd != nil {
		t.Fatal("move destination unexpectedly loaded the normal session")
	}
	if !m.showScopePicker || m.scopePickerMoveIssue != "move-me" || m.scopePicker.memberFocused {
		t.Fatalf("move setup=%t issue=%q member=%t", m.showScopePicker, m.scopePickerMoveIssue, m.scopePicker.memberFocused)
	}
	m.closeScopePicker()
	if m.showScopePicker || m.scopePickerMoveIssue != "" || m.focused != focusDetail || !m.scopePicker.memberFocused {
		t.Fatalf("move cleanup did not restore normal session boundary: shown=%t issue=%q focus=%s member=%t", m.showScopePicker, m.scopePickerMoveIssue, m.focused, m.scopePicker.memberFocused)
	}
	if m.backlog.Filter() != "keep" || m.backlog.MarkCount() != 1 {
		t.Fatalf("move cleanup lost normal backlog state: filter=%q marks=%d", m.backlog.Filter(), m.backlog.MarkCount())
	}
}

func TestCancelledMoveRestoresExactRetainedPickerAfterScopeReentry(t *testing.T) {
	catalogLoads := 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryCatalog: func(context.Context, ScopeCatalogQuery) (ScopeCatalogPage, error) {
			catalogLoads++
			return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "destination-1", Name: "Destination one"}, {ID: "destination-2", Name: "Destination two"}}}, nil
		},
	}})
	normalCatalog := []ScopeInfo{{ID: "normal-1", Name: "Normal one"}, {ID: "normal-2", Name: "Normal two"}}
	m.scopeCatalog = append([]ScopeInfo(nil), normalCatalog...)
	m.scopePicker.SetScopes(normalCatalog)
	m.scopePicker.Move(1)
	m.scopePicker.SetMembers([]IssueItem{
		{Issue: model.Issue{ID: "normal-member-1", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryName: "repo"},
		{Issue: model.Issue{ID: "normal-member-2", Status: model.StatusOpen, IssueType: model.TypeTask}, RepositoryName: "repo"},
	})
	m.scopePicker.SetMemberFilters("repo", "open", model.TypeTask)
	m.scopePicker.MoveMember(1)
	m.scopePicker.ToggleMemberMark()
	m.scopePicker.memberFocused = true
	m.scopePicker.catalogPageIndex = 2
	m.scopePicker.catalogPageCursors = []string{"", "catalog-1", "catalog-2"}
	m.scopePicker.catalogHasMore = true
	m.scopePicker.catalogNextCursor = "catalog-3"
	m.scopePicker.memberPageIndex = 3
	m.scopePicker.memberPageCursors = []string{"", "member-1", "member-2", "member-3"}
	m.scopePicker.memberHasMore = true
	m.scopePicker.memberNextCursor = "member-4"
	m.scopePicker.memberViewportStart = 1
	m.scopePicker.memberContextFilter = []string{"ctx:normal"}
	m.scopePicker.memberMarkedIDs = map[string]bool{"normal-member-2": true}
	m.scopePicker.memberSelectedID = "normal-member-2"
	m.scopeSessionInitialized = true
	m.showScopePicker = true
	m.focused = focusScopePicker

	wantPicker := cloneScopePicker(m.scopePicker)
	wantCatalog := append([]ScopeInfo(nil), m.scopeCatalog...)

	updated, _ := m.Update(keyMsg("g"))
	m = updated.(*Model)
	if m.showScopePicker || !m.isGraphView {
		t.Fatalf("leaving Scope did not enter Graph: scope=%t graph=%t", m.showScopePicker, m.isGraphView)
	}

	move := m.openScopePicker("move-me")
	if move == nil {
		t.Fatal("move destination did not start its catalog request")
	}
	updated, _ = m.Update(move())
	m = updated.(*Model)
	// Simulate browsing the destination's member pane as well as its catalog.
	m.scopePicker.memberFocused = true
	m.scopePicker.SetMemberFilters("destination", "closed", model.TypeBug)
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "destination-member", IssueType: model.TypeBug}, RepositoryName: "destination"}})
	m.scopePicker.ToggleMemberMark()
	updated, cmd := m.Update(keyMsg("esc"))
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("cancelling move returned an unexpected command")
	}

	if !reflect.DeepEqual(m.scopePicker, wantPicker) || !reflect.DeepEqual(m.scopeCatalog, wantCatalog) {
		t.Fatalf("cancelled move changed retained picker state:\n got picker=%#v catalog=%#v\nwant picker=%#v catalog=%#v", m.scopePicker, m.scopeCatalog, wantPicker, wantCatalog)
	}
	if catalogLoads != 1 {
		t.Fatalf("move catalog loads=%d, want one destination load", catalogLoads)
	}
	if cmd := m.openScopePicker(""); cmd != nil {
		t.Fatal("Scope re-entry reloaded after cancelled move")
	}
	if !reflect.DeepEqual(m.scopePicker, wantPicker) || !reflect.DeepEqual(m.scopeCatalog, wantCatalog) {
		t.Fatal("Scope re-entry did not restore the exact retained picker state")
	}
}

func TestPagedScopePickerRejectsResponseFromChangedMemberFilterTuple(t *testing.T) {
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID}, Members: []model.Issue{{ID: "old-filter-member"}}}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	oldRequest := m.startScopeMembersPage("", 0)
	if oldRequest == nil {
		t.Fatal("initial member request missing")
	}
	m.scopePicker.SetMemberFilters("ctx:changed", "", "")
	updated, _ := m.Update(oldRequest())
	m = updated.(*Model)
	if len(m.scopePicker.members) != 0 || m.scopePicker.memberSelectedID != "" {
		t.Fatalf("old member-filter response was accepted: members=%#v selected=%q", m.scopePicker.members, m.scopePicker.memberSelectedID)
	}
}

// The request key is the member pane's complete server-owned filter tuple.
func TestScopeMemberFilterTupleRejectsEveryChangedComponent(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "repository", key: "w"},
		{name: "status", key: "o"},
		{name: "type", key: "I"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []ScopeMembersQuery
			m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
				QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
					requests = append(requests, query)
					return ScopeMembersPage{
						Scope:   ScopeInfo{ID: query.ScopeID},
						Members: []model.Issue{{ID: "fresh-" + tc.name}}, HasMore: true, NextCursor: "fresh-next",
					}, nil
				},
			}})
			m.showScopePicker, m.focused = true, focusScopePicker
			m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}})
			m.scopePicker.SetMemberContextCatalog(hubScopeCatalog("ctx:repo-a", "ctx:repo-b"))
			m.scopePicker.memberServerFiltering = true
			m.scopePicker.memberScopeID = "s1"
			m.scopePicker.SetMembers([]IssueItem{
				{Issue: model.Issue{ID: "member-1"}, RepositoryName: "repo-a"},
				{Issue: model.Issue{ID: "member-2"}, RepositoryName: "repo-b"},
				{Issue: model.Issue{ID: "member-3"}, RepositoryName: "repo-a"},
			})
			m.scopePicker.memberSelected = 2
			m.scopePicker.memberSelectedID = "member-3"
			m.scopePicker.memberMarkedIDs = map[string]bool{"member-3": true}
			m.scopePicker.memberViewportStart = 2
			m.scopePicker.memberPageIndex = 2
			m.scopePicker.memberPageCursors = []string{"", "members-2", "members-3"}
			m.scopePicker.memberHasMore = true
			m.scopePicker.memberNextCursor = "members-4"
			m.scopePicker.memberPageHistoryKey = m.scopePicker.memberFilterKey()
			m.scopePicker.memberRequestKey = m.scopePicker.memberPageHistoryKey
			m.scopePicker.memberFocused = true

			old := scopeMembersPageMsg{
				scopeID: "s1", cursor: "members-3", requestKey: m.scopePicker.memberRequestKey,
				generation: m.scopePicker.memberGeneration,
				page:       ScopeMembersPage{Scope: ScopeInfo{ID: "s1"}, Members: []model.Issue{{ID: "stale-" + tc.name}}},
			}
			updated, cmd := m.handleScopePickerKey(keyMsg(tc.key))
			m = updated
			if cmd == nil {
				t.Fatal("filter transition did not reload members")
			}
			if len(m.scopePicker.members) != 0 || m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.memberSelectedID != "" || m.scopePicker.MemberMarkCount() != 0 || m.scopePicker.memberViewportStart != 0 || !m.scopePicker.memberLoading {
				t.Fatalf("%s filter did not reset loaded page state: members=%#v page=%d selected=%q marks=%d viewport=%d loading=%t", tc.name, m.scopePicker.members, m.scopePicker.MemberPageIndex(), m.scopePicker.memberSelectedID, m.scopePicker.MemberMarkCount(), m.scopePicker.memberViewportStart, m.scopePicker.memberLoading)
			}
			updatedTea, _ := m.Update(old)
			m = updatedTea.(*Model)
			if len(m.scopePicker.members) != 0 || strings.Contains(ansi.Strip(m.scopePicker.View()), "stale-"+tc.name) {
				t.Fatalf("stale %s-filter response repopulated members: %#v", tc.name, m.scopePicker.members)
			}

			updatedTea, _ = m.Update(cmd())
			m = updatedTea.(*Model)
			if len(requests) != 1 || requests[0].Cursor != "" || requests[0].ScopeID != "s1" {
				t.Fatalf("fresh %s-filter request=%#v", tc.name, requests)
			}
			_, status, issueType := m.scopePicker.MemberFilters()
			if status == "closed" {
				status = "completed"
			}
			if requests[0].Status != status || requests[0].Type != string(issueType) || !reflect.DeepEqual(requests[0].Contexts, m.scopePicker.MemberContexts()) {
				t.Fatalf("fresh %s-filter tuple=%#v, want current filters status=%q type=%q contexts=%v", tc.name, requests[0], status, issueType, m.scopePicker.MemberContexts())
			}
			if m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.MemberMarkCount() != 0 || m.scopePicker.memberViewportStart != 0 || m.scopePicker.SelectedMember() == nil || m.scopePicker.SelectedMember().Issue.ID != "fresh-"+tc.name {
				t.Fatalf("fresh %s-filter page state: page=%d marks=%d viewport=%d selected=%#v", tc.name, m.scopePicker.MemberPageIndex(), m.scopePicker.MemberMarkCount(), m.scopePicker.memberViewportStart, m.scopePicker.SelectedMember())
			}
		})
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

func TestPagedMemberCountMergePreservesActiveScopeForDeactivation(t *testing.T) {
	deactivations := 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		Deactivate: func(context.Context) error { deactivations++; return nil },
	}})
	active := ScopeInfo{ID: "s1", Name: "Today", MemberCount: 4, Active: true}
	m.activeScope = &active
	m.scopeCatalog = []ScopeInfo{active}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.showScopePicker = true
	m.focused = focusScopePicker
	generation := m.scopePicker.BeginMemberLoad("s1")
	updated, _ := m.Update(scopeMembersPageMsg{
		page:       ScopeMembersPage{Scope: ScopeInfo{ID: "s1", Name: "Today", MemberCount: 5}},
		scopeID:    "s1",
		requestKey: m.scopePicker.memberRequestKey,
		generation: generation,
	})
	m = updated.(*Model)
	if !m.scopeCatalog[0].Active || m.scopePicker.Selected() == nil || !m.scopePicker.Selected().Active {
		t.Fatalf("member count merge cleared active scope: catalog=%#v picker=%#v", m.scopeCatalog, m.scopePicker.Selected())
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("Enter did not start deactivation for preserved active scope")
	}
	cmd()
	if deactivations != 1 {
		t.Fatalf("deactivations=%d, want 1", deactivations)
	}
}

func TestPagedCatalogCountMergePreservesExistingActiveFlag(t *testing.T) {
	m := NewModel(nil, nil, "")
	current := ScopeInfo{ID: "s1", Name: "Today", MemberCount: 4, Active: true}
	m.scopeCatalog = []ScopeInfo{current}
	m.scopePicker.SetScopes([]ScopeInfo{current})
	generation := m.scopePicker.BeginCatalogLoad()
	updated, _ := m.Update(scopeCatalogPageMsg{
		page:       ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "Today", MemberCount: 5}}},
		generation: generation,
	})
	m = updated.(*Model)
	if !m.scopeCatalog[0].Active || m.scopePicker.Selected() == nil || !m.scopePicker.Selected().Active {
		t.Fatalf("catalog count merge cleared active scope: catalog=%#v picker=%#v", m.scopeCatalog, m.scopePicker.Selected())
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
	selectedView := ansi.Strip(picker.renderMembers(100, 5))
	if !strings.Contains(selectedView, "ctx:api") || strings.Contains(selectedView, "repository:api") {
		t.Fatalf("selected member context filter wording = %q", selectedView)
	}
	picker.memberRepositoryFilter = ""
	picker.CycleMemberType()
	if len(picker.filteredMembers) != 1 || picker.filteredMembers[0].Issue.ID != "web-1" {
		t.Fatalf("type member filter = %#v", picker.filteredMembers)
	}
	view := ansi.Strip(picker.View())
	for _, want := range []string{"Scopes", "Members · Today", "ctx:all", "type:bug", "web-1"} {
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
	for _, want := range []string{"No active scope", "press W to choose or create a scope", "B for Global issues"} {
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
	for _, want := range []string{"TY", "No active scope", "Global issues"} {
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
		if !strings.Contains(footer, "enter toggle") || strings.Contains(footer, "m move") || strings.Contains(footer, "esc back") {
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
		{name: "scopes", focus: focusScopePicker, wants: []string{"Scopes", "Tab", "Switch to members / Global issues", "Enter", "Toggle active scope", "n", "Create inactive named scope"}},
		{name: "global issues", focus: focusBacklog, wants: []string{"Global issues", "n/p", "Next / previous page", "/", "ID/title search", "l", "Filter by exact label", "s", "Cycle status", "A", "Add selected bead to scope", "space", "Mark current", "M", "Add matching exact label/epic issues to active scope"}},
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
	for _, want := range []string{"Switch to Global issues", "Move member selection", "Filter members by status", "Cycle member type filter", "Cycle member ctx filter", "Mark current member", "Remove marked/current members", "Match-remove members"} {
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
	for _, want := range []string{"j/k members", "o/c/r status", "I type", "w ctx", "space mark", "R remove current", "M match-remove", "tab global issues", "W close"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("scope member footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "tab catalog") || strings.Contains(footer, "esc back") {
		t.Fatalf("scope member footer retained stale catalog destination: %q", footer)
	}
	if strings.Contains(footer, "enter toggle") || strings.Contains(footer, "n new") {
		t.Fatalf("scope member footer advertises catalog-only control: %q", footer)
	}

	m.scopePicker.SetMoveTarget("Visible bead")
	help = ansi.Strip(m.renderHelpOverlay())
	for _, want := range []string{"Switch to Global issues", "Move member selection", "Filter members by status"} {
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
	for _, want := range []string{"tab global issues", "j/k members", "o/c/r status"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("moving scope member footer missing %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "enter move") || strings.Contains(footer, "destination") || strings.Contains(footer, "esc back") {
		t.Fatalf("moving scope member footer advertises destination control: %q", footer)
	}
}

func TestScopeMoveDestinationFooterUsesMembersTab(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height = 240, 40
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopePicker.SetMoveTarget("Visible bead")

	footer := ansi.Strip(m.renderFooter())
	if !strings.Contains(footer, "tab members") || strings.Contains(footer, "tab global issues") {
		t.Fatalf("move destination footer has wrong Tab destination: %q", footer)
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
		"Global issues (List/Detail; in Scope)",
		"New inactive named scope (Scopes)",
		"Toggle active scope (Scopes)",
		"Add to active scope (L/D/Global)",
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
			for _, want := range []string{"Scopes", "W         Named scopes", "B         Global issues", "n         New inactive"} {
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
		{"B", "Global issues (List/Detail; in Scope)"},
		{"n", "New inactive named scope (Scopes)"},
		{"Enter", "Toggle active scope (Scopes)"},
		{"A", "Add to active scope (L/D/Global)"},
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

func TestGlobalIssuesEntryPointUsesScopeScreen(t *testing.T) {
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
	if !m.showScopePicker || m.isBacklogView || m.focused != focusGlobalIssues || cmd == nil {
		t.Fatalf("B did not open Global issues in Scope: picker=%t backlog=%t focus=%s cmd=%t", m.showScopePicker, m.isBacklogView, m.focused, cmd != nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if loads != 1 {
		t.Fatalf("backlog loads=%d, want 1", loads)
	}

	updated, _ = m.Update(keyMsg("W"))
	m = updated.(*Model)
	if m.showScopePicker || m.isBacklogView || m.focused != focusList {
		t.Fatalf("Scope W did not close Global issues: picker=%t backlog=%t focus=%s", m.showScopePicker, m.isBacklogView, m.focused)
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
	if !m.showScopePicker || m.focused != focusGlobalIssues || len(cursors) != 1 || cursors[0] != "" {
		t.Fatalf("Global issues open state: scope=%t focus=%s cursors=%v", m.showScopePicker, m.focused, cursors)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if len(cursors) != 2 || cursors[1] != "opaque-next" {
		t.Fatalf("next page cursors=%v", cursors)
	}
	if m.backlog.PageIndex() != 1 || m.backlog.loading || m.backlogLoading {
		t.Fatalf("next page response state: page=%d loading=%t/%t, want page=1 and idle", m.backlog.PageIndex(), m.backlog.loading, m.backlogLoading)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("previous page did not use the recorded opaque cursor")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if len(cursors) != 3 || cursors[2] != "" || m.backlog.PageIndex() != 0 || m.backlog.CurrentPageCursor() != "" {
		t.Fatalf("previous page cursors=%v page=%d current=%q", cursors, m.backlog.PageIndex(), m.backlog.CurrentPageCursor())
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(*Model)
	if cmd != nil || m.backlog.PageIndex() != 0 {
		t.Fatal("previous-page boundary issued a request")
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
	for _, want := range []string{"Global issues", "search: needle_", "label: team", "status: open"} {
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

func TestFailedScopeMutationPreservesFiltersSelectionPreviewAndMarks(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.isBacklogView, m.focused = true, focusBacklog
	m.backlog.SetLabel("team")
	m.backlog.AddFilter("needle")
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global-1", Title: "needle issue"}}}, 0)
	m.backlog.ScrollPreview(2)
	m.backlog.ToggleMark()
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today"}})
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1"}}})
	m.scopePicker.memberFocused = true
	m.scopePicker.ToggleMemberMark()

	updated, _ := m.Update(runScopeMutationCmd(ScopeMutation{Kind: ScopeMutationRemove, ScopeID: "today"}, false, func(context.Context) error {
		return errors.New("mutation unavailable")
	})())
	m = updated.(*Model)
	if m.backlog.Filter() != "needle" || m.backlog.Label() != "team" || m.backlog.MarkCount() != 1 || m.scopePicker.MemberMarkCount() != 1 {
		t.Fatalf("failed mutation changed filters/marks: backlog=%q/%q/%d members=%d", m.backlog.Filter(), m.backlog.Label(), m.backlog.MarkCount(), m.scopePicker.MemberMarkCount())
	}
	if issue := m.backlog.CurrentIssue(); issue == nil || issue.ID != "global-1" || m.backlog.previewOffset != 2 {
		t.Fatalf("failed mutation changed selection/preview: issue=%#v preview=%d", issue, m.backlog.previewOffset)
	}
}

func TestSuccessfulScopeMutationInvalidatesOnlyAffectedBacklogPage(t *testing.T) {
	var queries []BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			queries = append(queries, query)
			return BacklogPage{Issues: []model.Issue{{ID: "fresh"}}}, nil
		},
	}})
	m.isBacklogView, m.focused, m.showScopePicker = true, focusBacklog, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today"}})
	m.backlog.SetLabel("team")
	m.backlog.AddFilter("needle")
	m.backlog.SetPage(BacklogPage{HasMore: true, NextCursor: "cursor-1"}, 0)
	m.backlog.NextPageCursor()
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old"}}}, 1)
	m.backlog.ScrollPreview(2)
	m.backlog.ToggleMark()
	m.backlogPageGeneration = 8
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1"}}})
	m.scopePicker.memberFocused = true
	m.scopePicker.ToggleMemberMark()

	updated, cmd := m.Update(scopeMutationMsg{mutation: ScopeMutation{Kind: ScopeMutationAdd, ScopeID: "today"}, action: "add"})
	m = updated.(*Model)
	if cmd == nil || m.backlog.Filter() != "needle" || m.backlog.Label() != "team" || m.backlog.PageIndex() != 1 || m.backlog.CurrentPageCursor() != "cursor-1" {
		t.Fatalf("successful mutation lost page/filter state: cmd=%t filter=%q label=%q page=%d cursor=%q", cmd != nil, m.backlog.Filter(), m.backlog.Label(), m.backlog.PageIndex(), m.backlog.CurrentPageCursor())
	}
	if len(m.backlog.issues) != 0 || m.backlog.CurrentIssue() != nil || m.backlog.previewOffset != 0 || m.backlog.MarkCount() != 0 || len(m.scopePicker.members) != 0 || m.scopePicker.SelectedMember() != nil || m.scopePicker.MemberMarkCount() != 0 || !m.backlog.loading {
		t.Fatalf("affected page remained actionable: issues=%#v issue=%#v preview=%d members=%#v selected=%#v backlogMarks=%d memberMarks=%d loading=%t", m.backlog.issues, m.backlog.CurrentIssue(), m.backlog.previewOffset, m.scopePicker.members, m.scopePicker.SelectedMember(), m.backlog.MarkCount(), m.scopePicker.MemberMarkCount(), m.backlog.loading)
	}

	for _, child := range cmd().(tea.BatchMsg) {
		if response, ok := child().(backlogPageMsg); ok {
			updated, _ = m.Update(response)
			m = updated.(*Model)
		}
	}
	if len(queries) != 1 || queries[0].Cursor != "cursor-1" || len(m.backlog.issues) != 1 || m.backlog.issues[0].ID != "fresh" {
		t.Fatalf("mutation reloaded wrong backlog page: queries=%#v issues=%#v", queries, m.backlog.issues)
	}
}

func TestScopeResultErrorsExposeNoStaleAction(t *testing.T) {
	emptyBacklog := NewBacklogModel(testTheme())
	emptyBacklog.SetPage(BacklogPage{}, 0)
	if emptyBacklog.CurrentIssue() != nil || emptyBacklog.renderBacklogPreview(80) != "" {
		t.Fatal("empty backlog exposed an actionable issue or preview")
	}

	emptyPicker := NewScopePickerModel(testTheme())
	emptyPicker.SetScopes([]ScopeInfo{{ID: "today"}})
	emptyPicker.SetMembers(nil)
	if emptyPicker.SelectedMember() != nil {
		t.Fatal("empty member pane exposed an actionable selection")
	}

	catalog := NewScopePickerModel(testTheme())
	catalog.SetScopes([]ScopeInfo{{ID: "old", Name: "Old"}})
	catalogGeneration := catalog.BeginCatalogLoad()
	catalog.SetCatalogError(catalogGeneration, errors.New("catalog unavailable"))
	if catalog.Selected() != nil || !strings.Contains(ansi.Strip(catalog.renderCatalog("Scopes", 40, 6)), "Scopes unavailable") {
		t.Fatal("catalog error exposed a stale scope selection")
	}

	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old"}}}, 0)
	b.ToggleMark()
	b.ScrollPreview(3)
	b.SetError(errors.New("backend unavailable"))
	if b.CurrentIssue() != nil || b.MarkCount() != 0 || b.renderBacklogPreview(80) != "" || strings.Contains(ansi.Strip(b.View()), "old") {
		t.Fatalf("backlog error exposed stale state: issue=%#v marks=%d view=%q", b.CurrentIssue(), b.MarkCount(), ansi.Strip(b.View()))
	}

	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "today"}})
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1"}}})
	generation := picker.BeginMemberLoad("today")
	picker.SetMemberError("today", generation, errors.New("members unavailable"))
	if picker.SelectedMember() != nil || strings.Contains(ansi.Strip(picker.View()), "member-1") {
		t.Fatalf("member error exposed stale action: member=%#v view=%q", picker.SelectedMember(), ansi.Strip(picker.View()))
	}
}

func TestScopeRenderingIsIdempotentForRetainedInteractionState(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 120, 30, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "today", Name: "Today"}})
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member-1"}}, {Issue: model.Issue{ID: "member-2"}}})
	m.scopePicker.MoveMember(1)
	m.scopePicker.ToggleMemberMark()
	selectedID := m.scopePicker.SelectedMember().Issue.ID
	repositoryFilter, statusFilter, typeFilter := m.scopePicker.MemberFilters()

	first := m.View()
	second := m.View()
	if first != second {
		t.Fatalf("rendering changed output across identical renders")
	}
	gotRepository, gotStatus, gotType := m.scopePicker.MemberFilters()
	if selected := m.scopePicker.SelectedMember(); selected == nil || selected.Issue.ID != selectedID || m.scopePicker.MemberMarkCount() != 1 || gotRepository != repositoryFilter || gotStatus != statusFilter || gotType != typeFilter {
		t.Fatalf("rendering reset interaction state: selected=%#v marks=%d filters=%q/%q/%q", selected, m.scopePicker.MemberMarkCount(), gotRepository, gotStatus, gotType)
	}
}

func TestScopeMutationRefreshKeepsCurrentCatalogPageAndSelection(t *testing.T) {
	var queries []ScopeCatalogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryCatalog: func(_ context.Context, query ScopeCatalogQuery) (ScopeCatalogPage, error) {
			queries = append(queries, query)
			return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s2", Name: "Selected"}, {ID: "s3", Name: "Other"}}}, nil
		},
	}})
	m.showScopePicker = true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "Earlier"}, {ID: "s2", Name: "Selected"}})
	m.scopePicker.Move(1)
	m.scopePicker.catalogPageCursors = []string{"", "catalog-2"}
	m.scopePicker.catalogPageIndex = 1
	m.scopePicker.catalogHasMore = true
	m.scopePicker.catalogNextCursor = "catalog-3"
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global"}}}, 0)
	m.backlog.ToggleMark()

	for _, message := range runUISemanticCommands(m.refreshAfterScopeMutation(ScopeMutation{Kind: ScopeMutationCreate})) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	if len(queries) != 1 || queries[0].Cursor != "catalog-2" || m.scopePicker.CatalogPageIndex() != 1 || m.scopePicker.SelectedScopeID() != "s2" {
		t.Fatalf("mutation refresh moved catalog selection/page: queries=%#v page=%d selected=%q", queries, m.scopePicker.CatalogPageIndex(), m.scopePicker.SelectedScopeID())
	}
	if m.backlog.MarkCount() != 1 || m.backlog.CurrentIssue() == nil || m.backlog.CurrentIssue().ID != "global" {
		t.Fatalf("catalog refresh invalidated unrelated backlog interaction: marks=%d issue=%#v", m.backlog.MarkCount(), m.backlog.CurrentIssue())
	}
}

func TestSuccessfulScopeMutationClearsOnlySubmittedMarks(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.focused = focusList
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "submitted"}, {ID: "keep"}}}, 0)
	m.backlog.ToggleMark()
	m.backlog.Move(1)
	m.backlog.ToggleMark()
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "submitted"}}, {Issue: model.Issue{ID: "keep"}}})
	m.scopePicker.ToggleMemberMark()
	m.scopePicker.MoveMember(1)
	m.scopePicker.ToggleMemberMark()

	updated, _ := m.Update(scopeMutationMsg{
		mutation: ScopeMutation{Kind: ScopeMutationRemove, ScopeID: "today", IssueIDs: []string{"submitted"}},
		action:   "remove",
	})
	m = updated.(*Model)
	if got := strings.Join(m.backlog.MarkedIDs(), ","); got != "keep" {
		t.Fatalf("backlog marks=%q, want keep", got)
	}
	if got := strings.Join(m.scopePicker.MarkedMemberIDs(), ","); got != "keep" {
		t.Fatalf("member marks=%q, want keep", got)
	}
}

func TestSuccessfulScopeCreationRetainsUnrelatedMarks(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.isBacklogView, m.focused, m.showScopePicker = true, focusBacklog, true
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global"}}}, 0)
	m.backlog.ToggleMark()
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member"}}})
	m.scopePicker.ToggleMemberMark()

	updated, _ := m.Update(scopeMutationMsg{mutation: ScopeMutation{Kind: ScopeMutationCreate}, action: "create"})
	m = updated.(*Model)
	if m.backlog.MarkCount() != 1 || m.scopePicker.MemberMarkCount() != 1 {
		t.Fatalf("scope creation cleared unrelated marks: backlog=%d members=%d", m.backlog.MarkCount(), m.scopePicker.MemberMarkCount())
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
	for _, want := range []string{"Global issues", "ID", "TYPE", "PR", "STAT", "CONTEXT", "CREATED_AT", "OPEN", "backlog-1", "Readable backlog title"} {
		if !strings.Contains(view, want) {
			t.Fatalf("backlog view missing %q:\n%s", want, view)
		}
	}
	fullPreview := ansi.Strip(b.renderBacklogPreview(120))
	for _, want := range []string{"DESCRIPTION", "First line", "remain fully visible."} {
		if !strings.Contains(fullPreview, want) {
			t.Fatalf("full backlog preview missing %q:\n%s", want, fullPreview)
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
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	want := []string{"TITLE", "Readable title", "", "CONTEXT", "(none)", "", "LABELS", "(none)", "", "DESCRIPTION", "Description"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("backlog preview did not stack labels and values:\n%q", preview)
	}
}

func TestBacklogContextColumnUsesFriendlyNameAndExtraCount(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{ID: "backlog-1"}}}, 0)
	b.setPresentation([]IssueItem{{
		Issue:           b.issues[0],
		RepositoryName:  "beads_viewer",
		RepositoryExtra: 2,
		HubPresentation: true,
	}})

	columns := backlogTableColumnsFor(b.filteredItems, 120)
	header := ansi.Strip(renderBacklogTableHeader(columns))
	row := ansi.Strip(b.renderBacklogRow(b.filteredItems[0], false, columns, columns.width))
	if columns.contextWidth < lipgloss.Width("beads_viewer +2") {
		t.Fatalf("context column width=%d, want room for normal name and count", columns.contextWidth)
	}
	if !strings.Contains(header, "CONTEXT") || !strings.Contains(row, "beads_viewer +2") {
		t.Fatalf("backlog context column omitted friendly multi-context value:\nheader=%q\nrow=%q", header, row)
	}
	if strings.Index(header, "CONTEXT") > strings.Index(header, "ID") {
		t.Fatalf("backlog header placed context after ID: %q", header)
	}
	if strings.Contains(row, "ctx:api") || strings.Contains(row, "[") {
		t.Fatalf("backlog context column used special context formatting: %q", row)
	}
}

func TestScopePickerReconcilesSelectionsByIdentity(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1"}, {ID: "s2"}, {ID: "s3"}})
	picker.Move(1)
	picker.SetScopes([]ScopeInfo{{ID: "s3"}, {ID: "s2"}, {ID: "s1"}})
	if picker.SelectedScopeID() != "s2" || picker.selected != 1 {
		t.Fatalf("scope selection=%q index=%d, want s2/index 1", picker.SelectedScopeID(), picker.selected)
	}

	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "m1"}}, {Issue: model.Issue{ID: "m2"}}})
	picker.MoveMember(1)
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "m3"}}, {Issue: model.Issue{ID: "m2"}}})
	if selected := picker.SelectedMember(); selected == nil || selected.Issue.ID != "m2" {
		t.Fatalf("member selection=%#v, want m2 after reorder", selected)
	}
	picker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "m3"}}, {Issue: model.Issue{ID: "m4"}}})
	if selected := picker.SelectedMember(); selected == nil || selected.Issue.ID != "m3" {
		t.Fatalf("missing member selection=%#v, want first visible member m3", selected)
	}
	picker.SetScopes(nil)
	if picker.SelectedScopeID() != "" || picker.Selected() != nil {
		t.Fatalf("empty scope selection=%q/%#v, want none", picker.SelectedScopeID(), picker.Selected())
	}
}

func TestBacklogReconciliationKeepsSelectionPreviewAndMarksCoherent(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{ID: "a"}, {ID: "b"}, {ID: "c"}}}, 0)
	b.Move(1)
	b.ToggleMark()
	b.Move(1)
	b.ToggleMark()
	b.Move(-1)
	b.ScrollPreview(4)
	b.setPresentation([]IssueItem{{Issue: model.Issue{ID: "c"}}, {Issue: model.Issue{ID: "b"}}, {Issue: model.Issue{ID: "a"}}})
	if issue := b.CurrentIssue(); issue == nil || issue.ID != "b" || b.previewOffset != 4 || b.MarkCount() != 2 {
		t.Fatalf("passive reconciliation issue=%#v preview=%d marks=%d, want b/4/2", issue, b.previewOffset, b.MarkCount())
	}

	b.SetExcludedIDs([]string{"b"})
	if issue := b.CurrentIssue(); issue == nil || issue.ID != "c" || b.previewOffset != 0 || b.MarkCount() != 1 || strings.Join(b.MarkedIDs(), ",") != "c" {
		t.Fatalf("excluded selection issue=%#v preview=%d marks=%d ids=%v, want c/0/1/[c]", issue, b.previewOffset, b.MarkCount(), b.MarkedIDs())
	}
	b.SetLoading(true)
	if b.CurrentIssue() != nil || b.renderBacklogPreview(80) != "" {
		t.Fatal("loading backlog exposed an actionable selection or preview")
	}
}

func TestScopePickerResizeClampsViewportWithoutResettingState(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1"}})
	items := make([]IssueItem, 12)
	for i := range items {
		items[i].Issue.ID = fmt.Sprintf("m-%02d", i)
		items[i].RepositoryName = "repo"
	}
	picker.SetMembers(items)
	picker.SetMemberFilters("repo", "", "")
	for i := 0; i < 7; i++ {
		picker.MoveMember(1)
	}
	picker.ToggleMemberMark()
	selectedID := picker.SelectedMember().Issue.ID
	picker.SetSize(100, 18)
	if selected := picker.SelectedMember(); selected == nil || selected.Issue.ID != selectedID || picker.memberSelected == 0 || picker.MemberMarkCount() != 1 {
		t.Fatalf("resize selection=%#v index=%d marks=%d, want retained identity/index/mark", selected, picker.memberSelected, picker.MemberMarkCount())
	}
	if picker.memberViewportStart < 0 || picker.memberViewportStart >= len(picker.filteredMembers) {
		t.Fatalf("resize viewport start=%d outside members=%d", picker.memberViewportStart, len(picker.filteredMembers))
	}
}

func TestScopeScreenClampsWithSplitRowsWithoutMovingRetainedBoundary(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.width, m.height, m.showScopePicker, m.ready = 120, 40, true, true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}})
	items := make([]IssueItem, 30)
	for i := range items {
		items[i].Issue.ID = fmt.Sprintf("member-%02d", i)
	}
	m.scopePicker.SetMembers(items)
	for i := 0; i < 20; i++ {
		m.scopePicker.MoveMember(1)
	}
	m.scopePicker.memberViewportStart = 12
	m.renderScopeScreen()
	if m.scopePicker.memberViewportStart != 12 || m.scopePicker.memberSelectedID != "member-20" {
		t.Fatalf("scope render moved retained split boundary: start=%d selected=%q", m.scopePicker.memberViewportStart, m.scopePicker.memberSelectedID)
	}
}

func TestScopeSplitResizeClampsBeforeRenderingMemberIndicator(t *testing.T) {
	picker := NewScopePickerModel(testTheme())
	picker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}})
	items := make([]IssueItem, 30)
	for i := range items {
		items[i].Issue.ID = fmt.Sprintf("member-%02d", i)
	}
	picker.SetMembers(items)
	picker.MoveMember(20)
	picker.memberViewportStart = 14
	picker.memberSelected = 25
	picker.memberSelectedID = "member-25"
	if view := ansi.Strip(picker.renderTopSplit(100, 19, true)); !strings.Contains(view, "screen 2/3") {
		t.Fatalf("initial split indicator=%q, want screen 2/3", view)
	}
	view := ansi.Strip(picker.renderTopSplit(100, 13, true))
	if !strings.Contains(view, "screen 4/4") || !strings.Contains(view, "member-24") || strings.Contains(view, "member-14") {
		t.Fatalf("shrunk split indicator/rows disagree:\n%s", view)
	}
}

func TestBacklogPageCursorHistoryOwnsCompleteFilterTuple(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{HasMore: true, NextCursor: "old"}, 0)
	if got := b.NextPageCursor(); got != "old" {
		t.Fatalf("initial next cursor=%q, want old", got)
	}
	b.SetLabel("new-label")
	if got := b.CurrentPageCursor(); got != "" || b.PageIndex() != 0 {
		t.Fatalf("changed-filter cursor=%q page=%d, want empty/page 0", got, b.PageIndex())
	}
	if got := b.NextPageCursor(); got != "" {
		t.Fatalf("changed-filter next cursor=%q, want no cursor from old tuple", got)
	}
}

func TestBacklogPageResponseRejectsAChangedFilterTuple(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old"}}}, 0)
	generation := m.backlogPageGeneration
	oldQuery := m.backlogQuery("")
	response := loadBacklogPageCmd(ScopeServices{
		QueryBacklog: func(context.Context, BacklogQuery) (BacklogPage, error) {
			return BacklogPage{Issues: []model.Issue{{ID: "stale-query-row"}}}, nil
		},
	}, oldQuery, 0, generation)()
	m.backlog.SetLabel("new-label")
	updated, _ := m.Update(response)
	m = updated.(*Model)
	if len(m.backlog.issues) != 0 || strings.Contains(ansi.Strip(m.backlog.View()), "stale-query-row") {
		t.Fatalf("changed-filter response repopulated stale page: issues=%#v view=%q", m.backlog.issues, ansi.Strip(m.backlog.View()))
	}
}

func TestBacklogFilterTupleRejectsStaleResponsesForEveryFilter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*BacklogModel)
		want   BacklogQuery
	}{
		{name: "search", change: func(b *BacklogModel) { b.AddFilter("needle") }, want: BacklogQuery{Filter: "needle"}},
		{name: "label", change: func(b *BacklogModel) { b.SetLabel("team") }, want: BacklogQuery{Label: "team"}},
		{name: "status", change: func(b *BacklogModel) { b.CycleStatus() }, want: BacklogQuery{Status: "open"}},
		{name: "contexts", change: func(b *BacklogModel) { b.SetContextFilter([]string{"ctx:alpha"}, true, []string{"alpha"}) }, want: BacklogQuery{Contexts: []string{"ctx:alpha"}, IncludeContextless: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(nil, nil, "")
			m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old-query-row"}}}, 0)
			old := loadBacklogPageCmd(ScopeServices{
				QueryBacklog: func(context.Context, BacklogQuery) (BacklogPage, error) {
					return BacklogPage{Issues: []model.Issue{{ID: "stale-" + tc.name}}}, nil
				},
			}, m.backlogQuery(""), 0, m.backlogPageGeneration)()
			tc.change(&m.backlog)
			updated, _ := m.Update(old)
			m = updated.(*Model)
			if len(m.backlog.issues) != 0 || strings.Contains(ansi.Strip(m.backlog.View()), "stale-"+tc.name) {
				t.Fatalf("stale %s-filter response repopulated pane: issues=%#v view=%q", tc.name, m.backlog.issues, ansi.Strip(m.backlog.View()))
			}
			got := m.backlogQuery("")
			if got.Filter != tc.want.Filter || got.Label != tc.want.Label || got.Status != tc.want.Status || !reflect.DeepEqual(got.Contexts, tc.want.Contexts) || got.IncludeContextless != tc.want.IncludeContextless || got.Cursor != "" {
				t.Fatalf("new %s-filter query=%#v, want %#v", tc.name, got, tc.want)
			}
		})
	}
}

func TestScopeRefreshRetainsFiltersResetsPanesAndRejectsOldPages(t *testing.T) {
	var catalogQueries []ScopeCatalogQuery
	var memberQueries []ScopeMembersQuery
	var backlogQueries []BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryCatalog: func(_ context.Context, query ScopeCatalogQuery) (ScopeCatalogPage, error) {
			catalogQueries = append(catalogQueries, query)
			return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "One"}}}, nil
		},
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			memberQueries = append(memberQueries, query)
			return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID}, Members: []model.Issue{{ID: "new-member"}}}, nil
		},
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			backlogQueries = append(backlogQueries, query)
			return BacklogPage{Issues: []model.Issue{{ID: "new-global"}}}, nil
		},
	}})
	m.showScopePicker = true
	m.focused = focusGlobalIssues
	m.scopePicker.SetMemberFilters("repo", "open", model.TypeTask)
	m.backlog.SetLabel("team")
	m.backlog.AddFilter("needle")
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old-global"}}}, 2)
	m.backlog.ToggleMark()
	m.backlog.ScrollPreview(3)
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}})
	m.scopePicker.catalogPageIndex = 2
	m.scopePicker.memberPageIndex = 2
	m.scopePicker.memberViewportStart = 4
	m.scopePicker.ToggleMemberMark()
	oldPage := loadBacklogPageCmd(ScopeServices{}, m.backlogQuery("old-cursor"), 2, m.backlogPageGeneration)()

	updated, cmd := m.Update(keyMsg("ctrl+r"))
	m = updated.(*Model)
	repositoryFilter, statusFilter, typeFilter := m.scopePicker.MemberFilters()
	if cmd == nil || m.backlog.Filter() != "needle" || m.backlog.Label() != "team" || repositoryFilter != "repo" || statusFilter != "open" || typeFilter != model.TypeTask {
		t.Fatalf("refresh lost filters or command: cmd=%t filter=%q label=%q members=%q/%q/%q", cmd != nil, m.backlog.Filter(), m.backlog.Label(), repositoryFilter, statusFilter, typeFilter)
	}
	if len(m.backlog.issues) != 0 || m.backlog.PageIndex() != 0 || m.backlog.previewOffset != 0 || m.backlog.MarkCount() != 0 || m.scopePicker.CatalogPageIndex() != 0 || m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.memberViewportStart != 0 || m.scopePicker.MemberMarkCount() != 0 {
		t.Fatalf("refresh retained page-local state: backlog=%#v picker=%#v", m.backlog, m.scopePicker)
	}
	updated, _ = m.Update(oldPage)
	m = updated.(*Model)
	if len(m.backlog.issues) != 0 {
		t.Fatalf("old backlog page was accepted after refresh: %#v", m.backlog.issues)
	}
	for _, message := range runUISemanticCommands(cmd) {
		updated, next := m.Update(message)
		m = updated.(*Model)
		for _, child := range runUISemanticCommands(next) {
			updated, _ = m.Update(child)
			m = updated.(*Model)
		}
	}
	if len(catalogQueries) != 1 || catalogQueries[0].Cursor != "" || len(memberQueries) != 1 || memberQueries[0].Cursor != "" || len(backlogQueries) != 1 || backlogQueries[0].Cursor != "" {
		t.Fatalf("refresh did not request first pages: catalog=%#v members=%#v backlog=%#v", catalogQueries, memberQueries, backlogQueries)
	}
}

func TestScopeRefreshKeepsMembershipGenerationMonotonic(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.showScopePicker = true
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}})
	m.scopeMembershipScopeID = "s1"
	m.scopeMembershipGeneration = 41
	m.scopeMembershipLoading = true
	old := scopeMembershipMsg{scopeID: "s1", generation: 41, ids: []string{"stale"}}

	m.refreshScopeSession()
	if m.scopeMembershipGeneration != 41 {
		t.Fatalf("refresh reset membership generation to %d", m.scopeMembershipGeneration)
	}
	// The first post-refresh scope selection starts a newer membership request.
	m.scopeRefreshBacklogPending = false
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1", Name: "One"}})
	m.invalidateScopeComplement("s1")
	updated, _ := m.Update(old)
	m = updated.(*Model)
	if _, ok := m.scopeMembershipIDs["s1"]; ok {
		t.Fatalf("pre-refresh membership response enabled complement: %#v", m.scopeMembershipIDs)
	}
}

func TestPagedScopeRefreshReloadsActiveScopeSnapshot(t *testing.T) {
	catalogLoads, snapshotLoads := 0, 0
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryCatalog: func(context.Context, ScopeCatalogQuery) (ScopeCatalogPage, error) {
			catalogLoads++
			return ScopeCatalogPage{Scopes: []ScopeInfo{{ID: "s1", Name: "One"}}}, nil
		},
		Load: func(context.Context) (ScopeSnapshot, error) {
			snapshotLoads++
			return ScopeSnapshot{
				Scopes: []ScopeInfo{{ID: "s1", Name: "One", MemberCount: 3}},
				Active: &ScopeInfo{ID: "s1", Name: "One", MemberCount: 3, Active: true},
			}, nil
		},
	}})
	m.showScopePicker = true
	cmd := m.refreshScopeSession()
	for _, message := range runUISemanticCommands(cmd) {
		updated, next := m.Update(message)
		m = updated.(*Model)
		for _, child := range runUISemanticCommands(next) {
			updated, _ = m.Update(child)
			m = updated.(*Model)
		}
	}
	if catalogLoads != 1 || snapshotLoads != 1 || m.activeScope == nil || m.activeScope.ID != "s1" {
		t.Fatalf("paged refresh catalog=%d snapshot=%d active=%#v", catalogLoads, snapshotLoads, m.activeScope)
	}
}

func TestMemberFilterClearsOnlyMemberPaneAndReloadsFirstPage(t *testing.T) {
	var requests []ScopeMembersQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			requests = append(requests, query)
			return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID}, Members: []model.Issue{{ID: "fresh"}}}, nil
		},
	}})
	m.showScopePicker = true
	m.focused = focusScopePicker
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	for _, message := range runUISemanticCommands(m.openScopePicker("")) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "global"}}}, 1)
	m.scopePicker.memberFocused = true
	old := loadScopeMembersPageCmd(ScopeServices{}, ScopeMembersQuery{ScopeID: "s1"}, 0, m.scopePicker.memberGeneration, m.scopePicker.memberRequestKey)()
	updated, cmd := m.handleScopePickerKey(keyMsg("c"))
	m = updated
	if cmd == nil || len(m.scopePicker.members) != 0 || m.scopePicker.MemberPageIndex() != 0 || m.scopePicker.memberViewportStart != 0 || m.scopePicker.MemberMarkCount() != 0 {
		t.Fatalf("member filter did not invalidate member pane: cmd=%t picker=%#v", cmd != nil, m.scopePicker)
	}
	if len(m.backlog.issues) != 1 || m.backlog.issues[0].ID != "global" {
		t.Fatalf("member filter touched complement pane: %#v", m.backlog.issues)
	}
	updatedModel, _ := m.Update(old)
	m = updatedModel.(*Model)
	if len(m.scopePicker.members) != 0 {
		t.Fatalf("old member-filter page was accepted: %#v", m.scopePicker.members)
	}
	_ = cmd()
	if len(requests) != 2 || requests[1].Cursor != "" || requests[1].Status != "completed" {
		t.Fatalf("member filter request=%#v", requests)
	}
}

func TestOutOfScopeFilterClearsRowsAndResetsOnlyBacklogPane(t *testing.T) {
	var queries []BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			queries = append(queries, query)
			return BacklogPage{Issues: []model.Issue{{ID: "fresh"}}}, nil
		},
	}})
	m.isBacklogView = true
	m.focused = focusBacklog
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old"}}, HasMore: true, NextCursor: "next"}, 0)
	m.backlog.NextPageCursor()
	m.backlog.SetPage(BacklogPage{Issues: []model.Issue{{ID: "old-page"}}}, 1)
	m.backlog.ToggleMark()
	m.backlog.ScrollPreview(2)
	m.scopePicker.SetScopes([]ScopeInfo{{ID: "s1"}})
	m.scopePicker.SetMembers([]IssueItem{{Issue: model.Issue{ID: "member"}}})
	updated, _ := m.Update(keyMsg("/"))
	m = updated.(*Model)
	updated, cmd := m.Update(keyMsg("needle"))
	m = updated.(*Model)
	if cmd == nil || len(m.backlog.issues) != 0 || m.backlog.PageIndex() != 0 || m.backlog.previewOffset != 0 || m.backlog.MarkCount() != 0 || len(m.scopePicker.members) != 1 {
		t.Fatalf("out-of-scope filter boundary: cmd=%t backlog=%#v member=%#v", cmd != nil, m.backlog, m.scopePicker.members)
	}
	_ = cmd()
	if len(queries) != 1 || queries[0].Cursor != "" || queries[0].Filter != "needle" {
		t.Fatalf("out-of-scope query=%#v", queries)
	}
}

func TestSelectedScopeChangeResetsMemberAndComplementUntilMembershipCompletes(t *testing.T) {
	var memberQueries []ScopeMembersQuery
	var backlogQueries []BacklogQuery
	m := NewModel(nil, nil, "", RuntimeServices{Scopes: ScopeServices{
		QueryMembers: func(_ context.Context, query ScopeMembersQuery) (ScopeMembersPage, error) {
			memberQueries = append(memberQueries, query)
			return ScopeMembersPage{Scope: ScopeInfo{ID: query.ScopeID}, Members: []model.Issue{{ID: query.ScopeID + "-member"}}}, nil
		},
		LoadDetails: func(_ context.Context, scopeID string) (ScopeDetails, error) {
			return ScopeDetails{Info: ScopeInfo{ID: scopeID, MemberCount: 1}, MemberIDs: []string{scopeID + "-member"}}, nil
		},
		QueryBacklog: func(_ context.Context, query BacklogQuery) (BacklogPage, error) {
			backlogQueries = append(backlogQueries, query)
			return BacklogPage{Issues: []model.Issue{{ID: "outside"}}}, nil
		},
	}})
	m.scopeCatalog = []ScopeInfo{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.showScopePicker = true
	m.focused = focusScopePicker
	for _, message := range runUISemanticCommands(m.openScopePicker("")) {
		updated, _ := m.Update(message)
		m = updated.(*Model)
	}
	m.scopePicker.SetMemberFilters("repo", "open", model.TypeTask)
	m.backlog.SetLabel("team")
	oldGeneration, oldRequest := m.scopePicker.memberGeneration, m.scopePicker.memberRequestKey
	old := scopeMembersPageMsg{scopeID: "s1", requestKey: oldRequest, generation: oldGeneration, page: ScopeMembersPage{Scope: ScopeInfo{ID: "s1"}, Members: []model.Issue{{ID: "stale"}}}}

	updated, cmd := m.handleScopePickerKey(keyMsg("down"))
	m = updated
	repositoryFilter, statusFilter, typeFilter := m.scopePicker.MemberFilters()
	if cmd == nil || m.scopePicker.SelectedScopeID() != "s2" || repositoryFilter != "repo" || statusFilter != "open" || typeFilter != model.TypeTask {
		t.Fatalf("scope change lost selection/filters: cmd=%t scope=%q filters=%q/%q/%q", cmd != nil, m.scopePicker.SelectedScopeID(), repositoryFilter, statusFilter, typeFilter)
	}
	_, hasNewMembership := m.scopeMembershipIDs["s2"]
	if len(m.scopePicker.members) != 0 || hasNewMembership || len(m.backlog.issues) != 0 || m.globalIssuesTitle() != "Global issues" {
		t.Fatalf("scope change retained unavailable complement/member state: members=%#v membership=%#v backlog=%#v title=%q", m.scopePicker.members, m.scopeMembershipIDs, m.backlog.issues, m.globalIssuesTitle())
	}
	m.backlog.SetSize(80, 10)
	if !m.backlog.loading || !strings.Contains(ansi.Strip(m.backlog.View()), "Loading issues") {
		t.Fatalf("scope change did not expose a bounded unavailable complement: loading=%t view=%q", m.backlog.loading, ansi.Strip(m.backlog.View()))
	}
	updatedModel, _ := m.Update(old)
	m = updatedModel.(*Model)
	if len(m.scopePicker.members) != 0 {
		t.Fatalf("old scope member page was accepted: %#v", m.scopePicker.members)
	}
	for _, child := range runUISemanticCommands(cmd) {
		updatedModel, next := m.Update(child)
		m = updatedModel.(*Model)
		for _, message := range runUISemanticCommands(next) {
			updatedModel, _ = m.Update(message)
			m = updatedModel.(*Model)
		}
	}
	if _, ok := m.scopeMembershipIDs["s2"]; !ok || m.globalIssuesTitle() != "Unscoped issues" {
		t.Fatalf("complete membership did not enable complement: membership=%#v title=%q", m.scopeMembershipIDs, m.globalIssuesTitle())
	}
	if len(memberQueries) < 2 || memberQueries[len(memberQueries)-1].ScopeID != "s2" || len(backlogQueries) == 0 {
		t.Fatalf("scope change did not reload member/complement: members=%#v backlog=%#v", memberQueries, backlogQueries)
	}
}

func TestBacklogNarrowContextCellPreservesExtraCount(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{ID: "backlog-1", CreatedAt: time.Date(2026, 9, 5, 12, 34, 56, 0, time.UTC)}}}, 0)
	b.setPresentation([]IssueItem{{
		Issue:           b.issues[0],
		RepositoryName:  "frontend",
		RepositoryExtra: 2,
		HubPresentation: true,
	}})

	columns := backlogTableColumnsFor(b.filteredItems, 70)
	cell := backlogContextCell(b.filteredItems[0], columns.contextWidth)
	if !strings.Contains(cell, "+2") || lipgloss.Width(cell) > columns.contextWidth {
		t.Fatalf("narrow context cell=%q width=%d, want preserved +2 notation", cell, columns.contextWidth)
	}
	header := ansi.Strip(renderBacklogTableHeader(columns))
	row := ansi.Strip(b.renderBacklogRow(b.filteredItems[0], false, columns, columns.width))
	if got, want := displayOffset(header, "CREATED_AT"), displayOffset(row, formatBacklogCreatedAt(b.issues[0].CreatedAt)); got != want {
		t.Fatalf("created_at column shifted at narrow width: header=%d row=%d\nheader=%q\nrow=%q", got, want, header, row)
	}
}

func TestBacklogPreviewShowsContextAndLabels(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{
		ID: "backlog-1", Labels: []string{"ctx:api", "backend", "urgent"},
	}}}, 0)
	b.setPresentation([]IssueItem{
		{Issue: b.issues[0], RepositoryName: "api", RepositoryExtra: 2, RepositoryNames: []string{"api", "frontend", "beads_viewer"}, HubPresentation: true, PresentationLabels: []string{"backend", "urgent"}},
	})

	preview := ansi.Strip(b.renderBacklogPreview(80))
	for _, want := range []string{"CONTEXT", "api", "frontend", "beads_viewer", "LABELS", "backend, urgent"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("backlog preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "+2") || strings.Contains(preview, "ctx:api") {
		t.Fatalf("context label leaked into ordinary labels preview:\n%s", preview)
	}
	lines := strings.Split(preview, "\n")
	contextAt, labelsAt := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case "CONTEXT":
			contextAt = i
		case "LABELS":
			labelsAt = i
		}
	}
	if contextAt < 0 || labelsAt <= contextAt {
		t.Fatalf("preview omitted context section:\n%s", preview)
	}
	var gotContexts []string
	for _, line := range lines[contextAt+1 : labelsAt] {
		if value := strings.TrimSpace(line); value != "" {
			gotContexts = append(gotContexts, value)
		}
	}
	if !reflect.DeepEqual(gotContexts, []string{"api", "frontend", "beads_viewer"}) {
		t.Fatalf("preview context list=%v, want every full context name", gotContexts)
	}
}

func TestBacklogPreviewStacksEveryLabelAboveItsValue(t *testing.T) {
	b := NewBacklogModel(testTheme())
	b.SetPage(BacklogPage{Issues: []model.Issue{{Title: "Title", Description: "Description"}}}, 0)

	lines := strings.Split(ansi.Strip(b.renderBacklogPreview(80)), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	for _, label := range []string{"TITLE", "CONTEXT", "LABELS", "DESCRIPTION"} {
		for i, line := range lines {
			if line != label {
				continue
			}
			if i+1 >= len(lines) || lines[i+1] == "" {
				t.Fatalf("preview label %q was not followed by a value: %q", label, lines)
			}
			if strings.Contains(line, "  ") {
				t.Fatalf("preview label %q remained inline: %q", label, line)
			}
			goto found
		}
		t.Fatalf("preview omitted label %q: %q", label, lines)
	found:
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
		if strings.Contains(line, "TITLE") {
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
	if strings.Index(directTableHeader, "CONTEXT") != 4 || strings.Index(tableHeader, "CONTEXT") != 6 ||
		strings.Index(directTableHeader, "CONTEXT") > strings.Index(directTableHeader, "ID") ||
		strings.Index(tableHeader, "CONTEXT") > strings.Index(tableHeader, "ID") {
		t.Fatalf("table header context order/offsets: direct=%d/%d rendered=%d/%d, want independent cursor and mark cells: %q", strings.Index(directTableHeader, "CONTEXT"), strings.Index(directTableHeader, "ID"), strings.Index(tableHeader, "CONTEXT"), strings.Index(tableHeader, "ID"), tableHeader)
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
		if strings.Contains(line, "TITLE") {
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
	if !strings.Contains(m.View(), "Global issues") {
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
