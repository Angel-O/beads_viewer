package ui

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// Build a minimal issue item used across delegate tests.
func newTestIssueItem(id string) IssueItem {
	now := time.Now().Add(-2 * time.Hour) // deterministic-ish age string (e.g. "2h")
	return IssueItem{
		Issue: model.Issue{
			ID:        id,
			Title:     "Short title for testing",
			Status:    model.StatusOpen,
			IssueType: model.TypeFeature,
			Priority:  1,
			Assignee:  "alice",
			Labels:    []string{"one", "two"},
			Comments: []*model.Comment{
				{ID: "1", IssueID: id, Author: "bob", Text: "hello", CreatedAt: now},
			},
			CreatedAt: now,
		},
		DiffStatus: DiffStatusNone,
		RepoPrefix: "",
	}
}

func TestIssueDelegate_RenderWorkspaceWithPriorityHints(t *testing.T) {
	item := newTestIssueItem("api-123")
	item.RepoPrefix = "api"         // exercise workspace badge branch
	item.DiffStatus = DiffStatusNew // exercise diff badge branch
	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))

	delegate := IssueDelegate{
		Theme:             theme,
		ShowPriorityHints: true,
		PriorityHints: map[string]*analysis.PriorityRecommendation{
			item.Issue.ID: {IssueID: item.Issue.ID, Direction: "increase"},
		},
		WorkspaceMode: true,
	}

	items := []list.Item{item}
	l := list.New(items, delegate, 0, 0)
	l.SetWidth(120) // wide enough to render right-side columns

	var buf bytes.Buffer
	delegate.Render(&buf, l, 0, item)
	out := buf.String()

	if !strings.Contains(out, "api-123") {
		t.Fatalf("render output missing issue id: %q", out)
	}
	if !strings.Contains(out, "↑") {
		t.Fatalf("render output missing priority hint arrow: %q", out)
	}
	if !strings.Contains(out, "[API]") {
		t.Fatalf("render output missing repo badge [API]: %q", out)
	}
	if !strings.Contains(out, "🆕") {
		t.Fatalf("render output missing diff badge for new item: %q", out)
	}
	if !strings.Contains(out, "💬1") {
		t.Fatalf("render output missing comment count badge: %q", out)
	}
}

func TestIssueDelegate_RenderFallsBackWidthAndNoPanic(t *testing.T) {
	item := newTestIssueItem("TASK-1")
	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{Theme: theme}

	l := list.New([]list.Item{item}, delegate, 0, 0) // width defaults to 0 → delegate fallback

	var buf bytes.Buffer
	delegate.Render(&buf, l, 0, item)
	out := buf.String()

	if out == "" {
		t.Fatal("render output should not be empty")
	}
	if !strings.Contains(out, "TASK-1") {
		t.Fatalf("render output missing id after fallback width handling: %q", out)
	}
}

func TestIssueDelegate_RenderUltraWide(t *testing.T) {
	item := newTestIssueItem("WIDE-1")
	// Assignee and Labels require width thresholds >100 and >140
	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{Theme: theme}

	l := list.New([]list.Item{item}, delegate, 0, 0)
	l.SetWidth(160) // Ultra-wide

	var buf bytes.Buffer
	delegate.Render(&buf, l, 0, item)
	out := buf.String()

	if !strings.Contains(out, "@alice") {
		t.Fatalf("ultra-wide output missing assignee @alice: %q", out)
	}
	if !strings.Contains(out, "one,two") { // joined labels
		t.Fatalf("ultra-wide output missing labels 'one,two': %q", out)
	}
}

func TestIssueDelegate_RenderNarrow(t *testing.T) {
	item := newTestIssueItem("NARROW-1")
	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{Theme: theme}

	l := list.New([]list.Item{item}, delegate, 0, 0)
	l.SetWidth(50) // Very narrow

	var buf bytes.Buffer
	delegate.Render(&buf, l, 0, item)
	out := buf.String()

	if !strings.Contains(out, "NARROW-1") {
		t.Fatalf("narrow output missing id: %q", out)
	}
	// Should NOT contain right-side metadata
	if strings.Contains(out, "@alice") {
		t.Fatalf("narrow output should hide assignee: %q", out)
	}
	if strings.Contains(out, "💬") {
		t.Fatalf("narrow output should hide comments count: %q", out)
	}
}

func TestIssueDelegate_WideAssigneeKeepsSingleRowWidth(t *testing.T) {
	item := newTestIssueItem("WIDE-ASSIGNEE")
	item.Issue.Assignee = "仓库维护者"
	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{Theme: theme}
	l := list.New([]list.Item{item}, delegate, 110, 10)

	var buf bytes.Buffer
	delegate.Render(&buf, l, 0, item)
	out := buf.String()
	if strings.Contains(out, "\n") {
		t.Fatalf("wide assignee wrapped a one-line delegate row: %q", out)
	}
	if width := lipgloss.Width(out); width > 109 {
		t.Fatalf("wide assignee row width = %d, want <= 109: %q", width, out)
	}
}

func TestIssueDelegate_FixedTypeAndTriageSlotsAlignColumns(t *testing.T) {
	items := []IssueItem{
		newTestIssueItem("id-a"),
		newTestIssueItem("界-id"),
		newTestIssueItem("id-three"),
		newTestIssueItem("id-four"),
		newTestIssueItem("id-five"),
	}
	items[1].IsQuickWin = true
	items[2].IsBlocker = true
	items[2].UnblocksCount = 12
	items[3].UnblocksCount = 3
	items[4].Issue.IssueType = model.IssueType("unknown")
	for index := range items {
		items[index].Issue.Title = fmt.Sprintf("Title %d", index+1)
	}

	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{Theme: theme}
	listItems := make([]list.Item, len(items))
	for index := range items {
		listItems[index] = items[index]
	}
	l := list.New(listItems, delegate, 0, 0)
	l.SetWidth(120)
	delegate.triageSlotWidth = delegate.triageSlotWidthFor(listItems, l.Width())

	var statusColumn, idColumn, titleColumn int
	wantIndicators := []string{"", "⭐", "🔓12", "↪3", ""}
	for index, item := range items {
		var buf bytes.Buffer
		delegate.Render(&buf, l, index, item)
		row := buf.String()

		statusAt := strings.Index(row, "OPEN")
		idAt := strings.Index(row, item.Issue.ID)
		titleAt := strings.Index(row, item.Issue.Title)
		if statusAt < 0 || idAt < 0 || titleAt < 0 {
			t.Fatalf("row %d omitted a column: %q", index, row)
		}
		if indicator := wantIndicators[index]; indicator != "" && !strings.Contains(row, indicator) {
			t.Fatalf("row %d omitted triage indicator %q: %q", index, indicator, row)
		}
		gotStatus := lipgloss.Width(row[:statusAt])
		gotID := lipgloss.Width(row[:idAt])
		gotTitle := lipgloss.Width(row[:titleAt])
		if index == 0 {
			statusColumn, idColumn, titleColumn = gotStatus, gotID, gotTitle
			continue
		}
		if gotStatus != statusColumn || gotID != idColumn || gotTitle != titleColumn {
			t.Fatalf("row %d columns = status:%d id:%d title:%d, want status:%d id:%d title:%d; row %q", index, gotStatus, gotID, gotTitle, statusColumn, idColumn, titleColumn, row)
		}
	}
}

func TestIssueDelegate_NarrowTriageSlotsPreserveRowBudget(t *testing.T) {
	items := []IssueItem{
		newTestIssueItem("a"),
		newTestIssueItem("b"),
		newTestIssueItem("c"),
		newTestIssueItem("d"),
		newTestIssueItem("e"),
	}
	items[1].IsQuickWin = true
	items[2].IsBlocker = true
	items[2].UnblocksCount = 123456789
	items[3].UnblocksCount = 45
	items[4].Issue.IssueType = model.IssueType("unknown")
	titles := []string{"¤", "§", "¶", "µ", "×"}
	for index := range items {
		items[index].Issue.Title = titles[index]
		items[index].Issue.Comments = nil
		items[index].Issue.Assignee = ""
		items[index].Issue.Labels = nil
	}

	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{Theme: theme}
	listItems := make([]list.Item, len(items))
	for index := range items {
		listItems[index] = items[index]
	}
	const listWidth = 24
	l := list.New(listItems, delegate, 0, 0)
	l.SetWidth(listWidth)
	delegate.triageSlotWidth = delegate.triageSlotWidthFor(listItems, l.Width())

	var statusColumn, idColumn, titleColumn int
	wantIndicators := []string{"", "⭐", "🔓", "↪45", ""}
	for index, item := range items {
		var buf bytes.Buffer
		delegate.Render(&buf, l, index, item)
		row := buf.String()
		if strings.Contains(row, "\n") {
			t.Fatalf("row %d wrapped at narrow width: %q", index, row)
		}
		if width := lipgloss.Width(row); width > listWidth-1 {
			t.Fatalf("row %d width = %d, want <= %d: %q", index, width, listWidth-1, row)
		}

		if indicator := wantIndicators[index]; indicator != "" && !strings.Contains(row, indicator) {
			t.Fatalf("row %d omitted narrow triage indicator %q: %q", index, indicator, row)
		}
		if index == 2 && !strings.Contains(row, "…") {
			t.Fatalf("multi-digit blocker indicator was not visibly truncated: %q", row)
		}
		if index == len(items)-1 && !strings.Contains(row, "•") {
			t.Fatalf("fallback type icon omitted: %q", row)
		}
		statusAt := strings.Index(row, "OPEN")
		idAt := strings.Index(row, item.Issue.ID)
		titleAt := strings.Index(row, item.Issue.Title)
		if statusAt < 0 || idAt < 0 {
			t.Fatalf("row %d omitted status or ID marker: %q", index, row)
		}
		gotStatus := lipgloss.Width(row[:statusAt])
		gotID := lipgloss.Width(row[:idAt])
		gotTitle := -1
		if titleAt >= 0 {
			gotTitle = lipgloss.Width(row[:titleAt])
		}
		if index == 0 {
			statusColumn, idColumn, titleColumn = gotStatus, gotID, gotTitle
			continue
		}
		if gotStatus != statusColumn || gotID != idColumn || gotTitle != titleColumn {
			t.Fatalf("row %d columns = status:%d id:%d title:%d, want status:%d id:%d title:%d; row %q", index, gotStatus, gotID, gotTitle, statusColumn, idColumn, titleColumn, row)
		}
	}
}

func TestIssueDelegate_NarrowSharedSlotAccountsForOptionalPrefix(t *testing.T) {
	items := []IssueItem{
		newTestIssueItem("idA"),
		newTestIssueItem("idB"),
		newTestIssueItem("idC"),
	}
	titles := []string{"¤", "§", "¶"}
	for index := range items {
		items[index].Issue.Title = titles[index]
	}
	items[1].IsQuickWin = true
	items[2].IsBlocker = true
	items[2].UnblocksCount = 123456789
	for index := range items {
		items[index].Issue.Comments = nil
		items[index].Issue.Assignee = ""
		items[index].Issue.Labels = nil
		items[index].DiffStatus = DiffStatusNew
		items[index].SearchScore = 0.75
		items[index].SearchScoreSet = true
		items[index].RepositoryID = "ctx:beta"
		items[index].RepositoryName = "beta"
		items[index].RepositoryExtra = 1
	}

	theme := DefaultTheme(lipgloss.NewRenderer(os.Stdout))
	delegate := IssueDelegate{
		Theme:                theme,
		ShowRepositories:     true,
		RepositoryNameWidth:  12,
		RepositoryExtraWidth: 2,
		ShowSearchScores:     true,
	}
	listItems := make([]list.Item, len(items))
	for index := range items {
		listItems[index] = items[index]
	}
	const listWidth = 51
	l := list.New(listItems, delegate, 0, 0)
	l.SetWidth(listWidth)
	delegate.triageSlotWidth = delegate.triageSlotWidthFor(listItems, l.Width())
	if delegate.triageSlotWidth != 4 {
		t.Fatalf("shared triage slot width = %d, want 4", delegate.triageSlotWidth)
	}

	var statusColumn, idColumn, titleColumn int
	wantIndicators := []string{"", "⭐", "🔓"}
	for index, item := range items {
		var buf bytes.Buffer
		delegate.Render(&buf, l, index, item)
		row := buf.String()
		if strings.Contains(row, "\n") || lipgloss.Width(row) > listWidth-1 {
			t.Fatalf("row %d exceeded narrow row budget: %q", index, row)
		}
		if indicator := wantIndicators[index]; indicator != "" && !strings.Contains(row, indicator) {
			t.Fatalf("row %d omitted indicator %q: %q", index, indicator, row)
		}
		if index == 2 {
			if !strings.Contains(row, "…") || !strings.Contains(row, "[0.75]") || !strings.Contains(row, "🆕") {
				t.Fatalf("row %d did not preserve truncated triage/search/diff metadata: %q", index, row)
			}
		}

		statusAt := strings.Index(row, "OPEN")
		idAt := strings.Index(row, item.Issue.ID)
		titleAt := strings.Index(row, item.Issue.Title)
		if statusAt < 0 || idAt < 0 {
			t.Fatalf("row %d omitted status or ID marker: %q", index, row)
		}
		gotStatus := lipgloss.Width(row[:statusAt])
		gotID := lipgloss.Width(row[:idAt])
		gotTitle := -1
		if titleAt >= 0 {
			gotTitle = lipgloss.Width(row[:titleAt])
		}
		if index == 0 {
			statusColumn, idColumn, titleColumn = gotStatus, gotID, gotTitle
			continue
		}
		if gotStatus != statusColumn || gotID != idColumn || gotTitle != titleColumn {
			t.Fatalf("row %d columns = status:%d id:%d title:%d, want status:%d id:%d title:%d; row %q", index, gotStatus, gotID, gotTitle, statusColumn, idColumn, titleColumn, row)
		}
	}
}

func TestIssueDelegate_TriageSlotRecalculatesOnResizeAndReplacement(t *testing.T) {
	item := newTestIssueItem("resize-id")
	item.Issue.Title = "resize-title"
	item.IsBlocker = true
	item.UnblocksCount = 123456789

	m := NewModel(nil, nil, "")
	m.list.SetItems([]list.Item{item})
	m.list.SetSize(24, 4)
	m.updateListDelegate()
	narrow := m.list.View()
	if !strings.Contains(narrow, "🔓") || !strings.Contains(narrow, "…") {
		t.Fatalf("narrow configured delegate did not retain a truncated indicator: %q", narrow)
	}

	m.list.SetSize(80, 4)
	m.updateListDelegate()
	wide := m.list.View()
	if !strings.Contains(wide, "🔓123456789") {
		t.Fatalf("resized delegate did not recalculate the full indicator slot: %q", wide)
	}

	replacement := newTestIssueItem("replacement-id")
	replacement.Issue.Title = "replacement-title"
	m.list.SetItems([]list.Item{replacement})
	m.updateListDelegate()
	if strings.Contains(m.list.View(), "🔓") {
		t.Fatalf("item replacement retained stale triage content: %q", m.list.View())
	}
}
