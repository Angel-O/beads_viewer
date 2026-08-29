package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func listHeaderAndRow(t *testing.T, view, issueID string) (string, string) {
	t.Helper()
	var header, row string
	for _, line := range strings.Split(view, "\n") {
		if header == "" && strings.Contains(line, "TY") && strings.Contains(line, "PR") {
			header = line
		}
		if row == "" && strings.Contains(line, issueID) {
			row = line
		}
	}
	if header == "" || row == "" {
		t.Fatalf("missing list header or row for %q:\n%s", issueID, view)
	}
	return header, row
}

func displayOffset(line, marker string) int {
	index := strings.Index(line, marker)
	if index < 0 {
		return -1
	}
	return lipgloss.Width(line[:index])
}

func hasMarkerAt(line, marker string, offset int) bool {
	for searchStart := 0; searchStart < len(line); {
		relativeIndex := strings.Index(line[searchStart:], marker)
		if relativeIndex < 0 {
			return false
		}
		index := searchStart + relativeIndex
		if lipgloss.Width(line[:index]) == offset {
			return true
		}
		searchStart = index + 1
	}
	return false
}

func assertListHeaderOffsets(t *testing.T, view, issueID string, markers map[string]string) {
	t.Helper()
	header, row := listHeaderAndRow(t, view, issueID)
	for headerMarker, rowMarker := range markers {
		headerOffset := displayOffset(header, headerMarker)
		rowOffset := displayOffset(row, rowMarker)
		if headerOffset < 0 || rowOffset < 0 {
			t.Fatalf("header/row marker missing: header %q row %q in:\n%s", headerMarker, rowMarker, view)
		}
		if headerOffset != rowOffset {
			t.Errorf("%s starts at cell %d, row %s starts at cell %d; header=%q row=%q", headerMarker, headerOffset, rowMarker, rowOffset, header, row)
		}
	}
}

func TestListHeadersShareIssueDelegateDisplayCellOffsets(t *testing.T) {
	created := time.Now().Add(-2 * time.Hour)
	issue := model.Issue{
		ID:        "header-1",
		Title:     "Header alignment",
		Status:    model.StatusOpen,
		IssueType: model.TypeFeature,
		Priority:  1,
		Assignee:  "alice",
		Labels:    []string{"one", "two"},
		Comments:  []*model.Comment{{ID: "comment-1", IssueID: "header-1", CreatedAt: created}},
		CreatedAt: created,
	}
	m := NewModel([]model.Issue{issue}, nil, "")
	items := m.list.Items()
	item := items[0].(IssueItem)
	item.IsQuickWin = true
	item.SearchScore = 0.75
	item.SearchScoreSet = true
	item.DiffStatus = DiffStatusNew
	items[0] = item
	m.list.SetItems(items)
	m.semanticSearchEnabled = true
	m.semanticHybridEnabled = true
	m.list.SetFilterText("Header")
	m.updateListDelegate()
	m.list.SetSize(180, 10)
	m.updateListDelegate()

	markers := map[string]string{
		"TY":    "✨",
		"PR":    "P1",
		"TR":    "⭐",
		"STAT":  "OPEN",
		"SCORE": "[0.75]",
		"ID":    "header-1",
		"DF":    "🆕",
		"TITLE": "Header alignment",
		"CMT":   "💬1",
		"GRAPH": RenderSparkline(item.GraphScore, 5),
		"ASGN":  "@alice",
		"LBL":   "one,two",
	}

	single := m.renderListWithHeader()
	assertListHeaderOffsets(t, single, issue.ID, markers)
	header, row := listHeaderAndRow(t, single, issue.ID)
	age := FormatTimeRel(created)
	ageColumnOffset := displayOffset(row, age)
	if displayOffset(header, "AGE") != ageColumnOffset {
		t.Fatalf("AGE header does not align with its fixed age cell: header=%d row-cell=%d", displayOffset(header, "AGE"), ageColumnOffset)
	}
	if strings.Contains(header, "TITLEAGE") {
		t.Fatalf("TITLE and AGE headers concatenated: %q", header)
	}

	m.isSplitView = true
	split := m.renderSplitView()
	assertListHeaderOffsets(t, split, issue.ID, markers)
}

func TestHubListHeaderShowsRepositoryWithoutWorkspaceMode(t *testing.T) {
	issue := model.Issue{ID: "hub-1", Title: "Hub header", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:alpha"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:alpha", Name: "alpha", Kind: model.RepositoryIdentityHubContext}}
	m.refreshRepositoryPresentation()
	m.list.SetSize(100, 10)
	m.updateListDelegate()

	header, row := listHeaderAndRow(t, m.renderListWithHeader(), issue.ID)
	if displayOffset(header, "CTX") != displayOffset(row, "[alpha]") {
		t.Fatalf("CTX header does not align with Hub badge: header=%q row=%q", header, row)
	}
	if !strings.Contains(header, "CTX") || strings.Contains(header, "REPO") {
		t.Fatalf("Hub header repository label = %q, want CTX only", header)
	}

	m.isSplitView = true
	header, row = listHeaderAndRow(t, m.renderSplitView(), issue.ID)
	if displayOffset(header, "CTX") != displayOffset(row, "[alpha]") {
		t.Fatalf("split CTX header does not align with Hub badge: header=%q row=%q", header, row)
	}
}

func TestListHeaderRemainsOneLineAndHidesUnavailableColumns(t *testing.T) {
	issue := model.Issue{ID: "narrow-1", Title: "Narrow", Status: model.StatusOpen, IssueType: model.TypeTask}
	m := NewModel([]model.Issue{issue}, nil, "")
	m.list.SetSize(50, 10)
	m.updateListDelegate()
	header, _ := listHeaderAndRow(t, m.renderListWithHeader(), issue.ID)
	if strings.Contains(header, "AGE") || strings.Contains(header, "GRAPH") || strings.Contains(header, "ASGN") || strings.Contains(header, "LBL") {
		t.Fatalf("narrow header exposed unavailable right-side columns: %q", header)
	}
	if strings.Contains(m.renderListWithHeader(), "\n\n") {
		t.Fatalf("narrow list header wrapped or introduced an extra blank row:\n%s", m.renderListWithHeader())
	}
}

func TestHeterogeneousListRowsUseOneHeaderContract(t *testing.T) {
	created := time.Now().Add(-3 * time.Hour)
	issues := []model.Issue{
		{
			ID: "alpha-1", Title: "work alpha", Status: model.StatusOpen, IssueType: model.TypeFeature,
			Priority: 1, Assignee: "alice", Labels: []string{"ctx:alpha", "ctx:beta", "frontend"},
			Comments: []*model.Comment{{ID: "a1"}, {ID: "a2"}}, CreatedAt: created,
		},
		{
			ID: strings.Repeat("beta-long-", 4), Title: "work beta", Status: model.StatusClosed, IssueType: model.TypeTask,
			Priority: 3, Labels: []string{"ctx:beta"}, CreatedAt: created,
		},
		{
			ID: "inbox-3", Title: "work inbox", Status: model.StatusInProgress, IssueType: model.TypeBug,
			Priority: 0, CreatedAt: created,
		},
	}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = model.RepositoryCatalog{
		{ID: "ctx:alpha", Name: "alpha", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:beta", Name: "beta", Kind: model.RepositoryIdentityHubContext},
	}
	m.refreshRepositoryPresentation()
	items := m.list.Items()
	for index := range items {
		item := items[index].(IssueItem)
		item.IsQuickWin, item.IsBlocker, item.UnblocksCount = false, false, 0
		switch item.Issue.ID {
		case "alpha-1":
			item.SearchScore, item.SearchScoreSet = 0.91, true
			item.DiffStatus, item.IsQuickWin = DiffStatusNew, true
		case "inbox-3":
			item.SearchScore, item.SearchScoreSet = 0.42, true
			item.DiffStatus, item.IsBlocker, item.UnblocksCount = DiffStatusModified, true, 7
		}
		items[index] = item
	}
	m.list.SetItems(items)
	m.semanticSearchEnabled = true
	m.semanticHybridEnabled = true
	m.list.SetFilterText("work")
	m.list.SetSize(180, 10)
	m.updateListDelegate()

	assertRows := func(view string) {
		t.Helper()
		header, alphaRow := listHeaderAndRow(t, view, "alpha-1")
		if !strings.Contains(header, "CTX") || strings.Contains(header, "TITLEAGE") {
			t.Fatalf("Hub or separated metadata header missing: %q", header)
		}
		for _, concatenated := range []string{"TYPR", "PRTR", "TRSTAT", "STATID", "IDSCORE", "TITLEAGE"} {
			if strings.Contains(header, concatenated) {
				t.Fatalf("header columns concatenated as %q: %q", concatenated, header)
			}
		}
		if !strings.Contains(alphaRow, "+1") {
			t.Fatalf("Hub multi-context row omitted repository +N: %q", alphaRow)
		}
		assertListHeaderOffsets(t, view, "alpha-1", map[string]string{
			"CTX":   "[alpha]",
			"TY":    "✨",
			"PR":    "P1",
			"TR":    "⭐",
			"STAT":  "OPEN",
			"SCORE": "[0.91]",
			"ID":    "alpha-1",
			"DF":    "🆕",
			"TITLE": "work alpha",
			"CMT":   "💬2",
			"ASGN":  "@alice",
			"LBL":   "frontend",
		})
		rowCases := []struct {
			id      string
			markers map[string]string
		}{
			{truncateRunesHelper("beta-long-beta-long-beta-long-beta-long-", 35, "…"), map[string]string{
				"TY": "🔧", "PR": "P3", "STAT": "DONE", "ID": truncateRunesHelper("beta-long-beta-long-beta-long-beta-long-", 35, "…"), "TITLE": "work beta",
			}},
			{"inbox-3", map[string]string{
				"TY": "🐛", "PR": "P0", "TR": "🔓7", "STAT": "PROG", "SCORE": "[0.42]", "ID": "inbox-3", "DF": "~", "TITLE": "work inbox",
			}},
		}
		for _, rowCase := range rowCases {
			_, row := listHeaderAndRow(t, view, rowCase.id)
			if rowCase.id != "alpha-1" && strings.Contains(row, "@") {
				t.Errorf("unassigned row %s rendered a stray assignee marker: %q", rowCase.id, row)
			}
			for headerMarker, rowMarker := range rowCase.markers {
				if displayOffset(header, headerMarker) != displayOffset(row, rowMarker) {
					t.Errorf("%s drifted for %s: header=%d row=%d", headerMarker, rowCase.id, displayOffset(header, headerMarker), displayOffset(row, rowMarker))
				}
			}
		}
		if strings.Contains(alphaRow, "TITLEAGE") {
			t.Fatalf("row metadata concatenated: %q", alphaRow)
		}
	}

	single := m.renderListWithHeader()
	assertRows(single)
	headerBefore, _ := listHeaderAndRow(t, single, "alpha-1")
	m.list.Select(1)
	longID := truncateRunesHelper(strings.Repeat("beta-long-", 4), 35, "…")
	headerAfter, _ := listHeaderAndRow(t, m.renderListWithHeader(), longID)
	if headerBefore != headerAfter {
		t.Fatalf("header changed when selection changed:\nbefore %q\nafter %q", headerBefore, headerAfter)
	}

	m.isSplitView = true
	assertRows(m.renderSplitView())
}

func TestNarrowDiffSlotReservesIDAndRightMetadata(t *testing.T) {
	created := time.Now().Add(-4 * time.Hour)
	longID := strings.Repeat("narrow-diff-", 4)
	issue := model.Issue{
		ID: longID, Title: "keep title visible", Status: model.StatusOpen, IssueType: model.TypeTask,
		Priority: 1, Comments: []*model.Comment{{ID: "comment-1"}}, CreatedAt: created,
	}
	item := IssueItem{Issue: issue}
	item.DiffStatus = DiffStatusNew
	theme := DefaultTheme(lipgloss.NewRenderer(nil))
	delegate := IssueDelegate{Theme: theme}
	l := list.New([]list.Item{item}, delegate, 0, 0)
	l.SetWidth(72)
	columns := delegate.issueListColumnsFor(l.VisibleItems(), l.Width())

	truncatedID := truncateRunesHelper(longID, 35, "…")
	header := renderIssueListHeader(columns)
	var buf strings.Builder
	delegate.Render(&buf, l, 0, item)
	row := buf.String()
	for headerMarker, rowMarker := range map[string]string{
		"ID": truncatedID, "DF": "🆕", "TITLE": "keep", "AGE": FormatTimeRel(created),
	} {
		if displayOffset(header, headerMarker) != displayOffset(row, rowMarker) {
			t.Fatalf("%s shifted in narrow diff row: header=%d row=%d\nheader=%q\nrow=%q", headerMarker, displayOffset(header, headerMarker), displayOffset(row, rowMarker), header, row)
		}
	}
	if lipgloss.Width(row) > l.Width()-1 {
		t.Fatalf("narrow diff row exceeded list width: %d > %d: %q", lipgloss.Width(row), l.Width()-1, row)
	}
	if !strings.Contains(row, "keep") {
		t.Fatalf("narrow diff row displaced its title: %q", row)
	}
}

func TestTitleAgeBoundaryKeepsOneDisplayCellGap(t *testing.T) {
	created := time.Now().Add(-5 * time.Hour)
	item := newTestIssueItem("id")
	item.Issue.Title = "boundary title"
	item.Issue.CreatedAt = created
	item.RepositoryID = "ctx:alpha"
	item.RepositoryName = "alpha"
	item.SearchScore = 0.75
	item.SearchScoreSet = true
	item.IsQuickWin = true

	theme := DefaultTheme(lipgloss.NewRenderer(nil))
	delegate := IssueDelegate{
		Theme:               theme,
		ShowPriorityHints:   true,
		PriorityHints:       map[string]*analysis.PriorityRecommendation{},
		ShowRepositories:    true,
		RepositoryNameWidth: 20,
		ShowSearchScores:    true,
		triageSlotWidth:     4,
	}
	listItems := []list.Item{item}
	l := list.New(listItems, delegate, 0, 0)

	for _, width := range []int{67, 68} {
		l.SetWidth(width)
		columns := delegate.issueListColumnsFor(l.VisibleItems(), l.Width())
		header := renderIssueListHeader(columns)
		var buf strings.Builder
		delegate.Render(&buf, l, 0, item)
		row := buf.String()
		age := FormatTimeRel(created)
		ageHeaderOffset := displayOffset(header, "AGE")
		ageRowOffset := displayOffset(row, age)
		if ageHeaderOffset != ageRowOffset {
			t.Errorf("width %d AGE offset header=%d row=%d", width, ageHeaderOffset, ageRowOffset)
		}
		if ageHeaderOffset < 0 || ageRowOffset < 0 {
			t.Fatalf("width %d omitted AGE: header=%q row=%q", width, header, row)
		}

		titleAtStart := hasMarkerAt(header, "TITLE", columns.titleStart)
		shortTitleAtStart := hasMarkerAt(header, "T", columns.titleStart)
		if width == 67 {
			if titleAtStart || shortTitleAtStart {
				t.Fatalf("width %d should omit TITLE when no safe cell remains: %q", width, header)
			}
			continue
		}
		if !shortTitleAtStart {
			t.Fatalf("width %d omitted shortened TITLE marker: %q", width, header)
		}
		shortTitleOffset := columns.titleStart
		if ageHeaderOffset-shortTitleOffset-lipgloss.Width("T") < 1 {
			t.Fatalf("width %d placed TITLE adjacent to AGE: header=%q", width, header)
		}
		if !strings.Contains(row, "…") {
			t.Fatalf("width %d did not shorten the title at the boundary: %q", width, row)
		}
	}
}

func TestListHeaderOptionalSlotsFollowSharedResponsiveLayout(t *testing.T) {
	issue := model.Issue{ID: "responsive-1", Title: "work", Status: model.StatusOpen, IssueType: model.TypeTask, Assignee: "alice", Labels: []string{"label"}}
	m := NewModel([]model.Issue{issue}, nil, "")
	for _, testCase := range []struct {
		width        int
		wantComments bool
		wantAssignee bool
		wantLabels   bool
	}{
		{width: 60},
		{width: 62, wantComments: true},
		{width: 101, wantComments: true},
		{width: 102, wantComments: true, wantAssignee: true},
		{width: 142, wantComments: true, wantAssignee: true, wantLabels: true},
	} {
		m.list.SetSize(testCase.width, 10)
		m.updateListDelegate()
		header, _ := listHeaderAndRow(t, m.renderListWithHeader(), issue.ID)
		if got := strings.Contains(header, "CMT"); got != testCase.wantComments {
			t.Errorf("width %d CMT visibility=%v, want %v: %q", testCase.width, got, testCase.wantComments, header)
		}
		if got := strings.Contains(header, "ASGN"); got != testCase.wantAssignee {
			t.Errorf("width %d ASGN visibility=%v, want %v: %q", testCase.width, got, testCase.wantAssignee, header)
		}
		if got := strings.Contains(header, "LBL"); got != testCase.wantLabels {
			t.Errorf("width %d LBL visibility=%v, want %v: %q", testCase.width, got, testCase.wantLabels, header)
		}
	}
}

func TestHubListRepositoryWidthStaysStableAcrossStatusToggles(t *testing.T) {
	issues := []model.Issue{
		{ID: "long", Title: "Long repository", Status: model.StatusOpen, Labels: []string{"ctx:long"}},
		{ID: "short", Title: "Short repository", Status: model.StatusClosed, Labels: []string{"ctx:s"}},
		{
			ID:        strings.Repeat("very-long-id-", 5),
			Title:     "Badge-heavy hidden row",
			Status:    model.StatusClosed,
			Labels:    []string{"ctx:s"},
			Comments:  []*model.Comment{{ID: "comment", Text: "comment"}},
			IssueType: model.TypeTask,
		},
	}
	catalog := model.RepositoryCatalog{
		{ID: "ctx:long", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:s", Name: "s", Kind: model.RepositoryIdentityHubContext},
	}
	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(80, 10)
	m.quickWinSet = map[string]bool{issues[2].ID: true}
	m.unblocksMap = map[string][]string{issues[2].ID: {"hidden-a", "hidden-b"}}
	m.refreshRepositoryPresentation()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(Model)

	delegateWidth := func() int {
		t.Helper()
		nameWidth, _ := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
		return nameWidth
	}
	renderRows := func() {
		t.Helper()
		for _, row := range strings.Split(m.list.View(), "\n") {
			if width := lipgloss.Width(row); width > m.list.Width() {
				t.Fatalf("rendered row width = %d, terminal width = %d: %q", width, m.list.Width(), row)
			}
		}
	}

	stableWidth := delegateWidth()
	if stableWidth != lipgloss.Width("beads_viewer") {
		t.Fatalf("initial repository width = %d, want full width %d", stableWidth, lipgloss.Width("beads_viewer"))
	}
	if !strings.Contains(m.list.View(), "[beads_viewer]") {
		t.Fatalf("normal list omitted full repository label:\n%s", m.list.View())
	}
	renderRows()

	transitions := []struct {
		key       rune
		wantIDs   []string
		wantLabel string
	}{
		{key: 'o', wantIDs: []string{"long", "short", issues[2].ID}, wantLabel: "beads_viewer"},
		{key: 'o', wantIDs: []string{"long"}, wantLabel: "beads_viewer"},
		{key: 'c', wantIDs: []string{"short", issues[2].ID}, wantLabel: "s"},
		{key: 'c', wantIDs: []string{"long", "short", issues[2].ID}, wantLabel: "beads_viewer"},
		{key: 'r', wantIDs: []string{"long"}, wantLabel: "beads_viewer"},
		{key: 'r', wantIDs: []string{"long", "short", issues[2].ID}, wantLabel: "beads_viewer"},
	}
	for _, transition := range transitions {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{transition.key}})
		m = updated.(Model)
		if got := visibleIssueIDs(m); !reflect.DeepEqual(got, transition.wantIDs) {
			t.Fatalf("key %q visible IDs = %v, want %v", transition.key, got, transition.wantIDs)
		}
		if got := delegateWidth(); got != stableWidth {
			t.Fatalf("key %q changed repository width from %d to %d", transition.key, stableWidth, got)
		}
		renderRows()
		if transition.wantLabel != "" && !strings.Contains(m.list.View(), "["+transition.wantLabel+"]") {
			t.Fatalf("key %q list omitted repository label %q:\n%s", transition.key, transition.wantLabel, m.list.View())
		}
	}

	narrow := NewModel(issues, nil, "")
	narrow.hubConfigPath = "hub.yaml"
	narrow.repositoryCatalog = catalog
	narrow.list.SetSize(55, 10)
	narrow.refreshRepositoryPresentation()
	narrowWidth, _ := narrow.repositoryListColumnWidths(IssueDelegate{Theme: narrow.theme})
	if got := narrowWidth; got > stableWidth {
		t.Fatalf("narrow terminal widened repository column to %d from %d", got, stableWidth)
	}
	for _, row := range strings.Split(narrow.list.View(), "\n") {
		if width := lipgloss.Width(row); width > narrow.list.Width() {
			t.Fatalf("narrow rendered row width = %d, terminal width = %d: %q", width, narrow.list.Width(), row)
		}
	}
}

func TestHubListRepositoryWidthUsesActiveScopeOnly(t *testing.T) {
	issues := []model.Issue{
		{ID: "short", Title: "Short repository", Status: model.StatusOpen, Labels: []string{"ctx:s"}},
		{ID: "inbox", Title: "Contextless", Status: model.StatusOpen},
	}
	catalog := model.RepositoryCatalog{
		{ID: "ctx:s", Name: "s", Kind: model.RepositoryIdentityHubContext},
		{ID: "ctx:inactive", Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext},
	}

	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	scope, err := model.NewSelectedContextsHubScope([]string{"ctx:s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	nameWidth, _ := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if nameWidth != lipgloss.Width("s") {
		t.Fatalf("active short-scope repository width = %d, want %d", nameWidth, lipgloss.Width("s"))
	}
	if strings.Contains(m.list.View(), "beads_viewer") {
		t.Fatalf("inactive repository widened or leaked into short scope:\n%s", m.list.View())
	}

	contextless := NewModel(issues, nil, "")
	contextless.hubConfigPath = "hub.yaml"
	contextless.repositoryCatalog = catalog
	contextless.list.SetSize(120, 10)
	if err := contextless.SetHubScope(model.NewContextlessHubScope()); err != nil {
		t.Fatal(err)
	}
	nameWidth, _ = contextless.repositoryListColumnWidths(IssueDelegate{Theme: contextless.theme})
	if nameWidth != lipgloss.Width(contextlessRepositoryID) {
		t.Fatalf("contextless repository width = %d, want %d", nameWidth, lipgloss.Width(contextlessRepositoryID))
	}
}

func TestHubListExtraWidthUsesRenderedInactiveContexts(t *testing.T) {
	const activeID = "ctx:active"
	issues := []model.Issue{
		{ID: "multi-1", Title: "Multi-context", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{activeID}},
		{ID: "single1", Title: "Single-context", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{activeID}},
	}
	catalog := model.RepositoryCatalog{{ID: activeID, Name: "beads_viewer", Kind: model.RepositoryIdentityHubContext}}
	for index := 0; index < 12; index++ {
		contextID := fmt.Sprintf("ctx:inactive-%02d", index)
		catalog = append(catalog, model.RepositoryCatalogEntry{
			ID: contextID, Name: fmt.Sprintf("repo-%02d", index), Kind: model.RepositoryIdentityHubContext,
		})
		issues[0].Labels = append(issues[0].Labels, contextID)
	}

	m := NewModel(issues, nil, "")
	m.hubConfigPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	scope, err := model.NewSelectedContextsHubScope([]string{activeID})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}

	nameWidth, extraWidth := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if nameWidth != lipgloss.Width("beads_viewer") || extraWidth != lipgloss.Width("+12") {
		t.Fatalf("selected scope columns = name:%d extra:%d, want name:%d extra:%d", nameWidth, extraWidth, lipgloss.Width("beads_viewer"), lipgloss.Width("+12"))
	}
	view := m.list.View()
	if !strings.Contains(view, "[beads_viewer]") || !strings.Contains(view, "+12") {
		t.Fatalf("multi-context row omitted rendered repository metadata:\n%s", view)
	}
	wantPrefixWidth := -1
	for _, id := range []string{"multi-1", "single1"} {
		var row string
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, id) {
				row = line
				break
			}
		}
		if row == "" {
			t.Fatalf("missing rendered row for %s:\n%s", id, view)
		}
		priorityIndex := strings.Index(row, "P0")
		if priorityIndex < 0 {
			t.Fatalf("missing priority column for %s: %q", id, row)
		}
		prefixWidth := lipgloss.Width(row[:priorityIndex])
		if wantPrefixWidth < 0 {
			wantPrefixWidth = prefixWidth
		} else if prefixWidth != wantPrefixWidth {
			t.Fatalf("repository columns drifted for %s: got %d, want %d; row %q", id, prefixWidth, wantPrefixWidth, row)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > m.list.Width() {
			t.Fatalf("rendered row width = %d, terminal width = %d: %q", width, m.list.Width(), line)
		}
	}

	allItems := NewModel([]model.Issue{{ID: "only-active", Title: "Only active", Status: model.StatusOpen, Labels: []string{activeID}}}, nil, "")
	allItems.hubConfigPath = "hub.yaml"
	allItems.repositoryCatalog = catalog
	allItems.list.SetSize(120, 10)
	allNameWidth, allExtraWidth := allItems.repositoryListColumnWidths(IssueDelegate{Theme: allItems.theme})
	if allNameWidth != lipgloss.Width("beads_viewer") || allExtraWidth != 0 {
		t.Fatalf("all-items unused extra reservation = name:%d extra:%d, want name:%d extra:0", allNameWidth, allExtraWidth, lipgloss.Width("beads_viewer"))
	}
}
