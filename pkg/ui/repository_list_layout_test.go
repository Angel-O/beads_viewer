package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
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

func listTitleWidth(columns *issueListColumns) int {
	return max(columns.width-columns.titleStart-columns.rightWidth-1, 0)
}

func listColumnStarts(columns *issueListColumns) []int {
	return []int{
		columns.repoStart,
		columns.typeStart,
		columns.priorityStart,
		columns.triageStart,
		columns.statusStart,
		columns.searchStart,
		columns.idStart,
		columns.diffStart,
		columns.titleStart,
		columns.ageStart,
		columns.commentsStart,
		columns.graphStart,
		columns.assigneeStart,
		columns.labelsStart,
	}
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
	age := formatIssueListAge(created)
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
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = repositorypkg.Catalog{{ID: "ctx:alpha", Name: "alpha", Kind: repositorypkg.IdentityExact}}
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
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = repositorypkg.Catalog{
		{ID: "ctx:alpha", Name: "alpha", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:beta", Name: "beta", Kind: repositorypkg.IdentityExact},
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
			"AGE":   formatIssueListAge(created),
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
	if columns.width != l.Width()-1 {
		t.Fatalf("standalone narrow contract width = %d, want guarded width %d", columns.width, l.Width()-1)
	}

	truncatedID := truncateRunesHelper(longID, 35, "…")
	header := renderIssueListHeader(columns)
	var buf strings.Builder
	delegate.Render(&buf, l, 0, item)
	row := buf.String()
	for _, marker := range []string{"AGE", "CMT"} {
		if !strings.Contains(header, marker) {
			t.Fatalf("narrow header omitted required %s cell: %q", marker, header)
		}
	}
	for headerMarker, rowMarker := range map[string]string{
		"ID": truncatedID, "DF": "🆕", "TITLE": "keep", "AGE": formatIssueListAge(created), "CMT": "💬1",
	} {
		headerOffset := displayOffset(header, headerMarker)
		rowOffset := displayOffset(row, rowMarker)
		if headerOffset < 0 || rowOffset < 0 {
			t.Fatalf("narrow %s marker missing: header=%q row=%q", headerMarker, header, row)
		}
		if headerOffset != rowOffset {
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

	for _, width := range []int{63, 64} {
		l.SetWidth(width)
		columns := delegate.issueListColumnsFor(l.VisibleItems(), l.Width())
		header := renderIssueListHeader(columns)
		var buf strings.Builder
		delegate.Render(&buf, l, 0, item)
		row := buf.String()
		age := formatIssueListAge(created)
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
		if width == 63 {
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
		{width: 100, wantComments: true},
		{width: 101, wantComments: true, wantAssignee: true},
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

func TestIssueListRowsRespectGuardedAndBoundedRightEdges(t *testing.T) {
	item := newTestIssueItem("edge-1")
	item.Issue.Title = "right edge"
	item.Issue.CreatedAt = time.Now().Add(-2 * time.Hour)
	item.Issue.Comments = make([]*model.Comment, 10)
	theme := DefaultTheme(lipgloss.NewRenderer(nil))

	for _, testCase := range []struct {
		name         string
		useFullWidth bool
		listWidth    int
	}{
		{name: "guarded terminal list", listWidth: 180},
		{name: "bounded body", useFullWidth: true, listWidth: 180},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			delegate := IssueDelegate{Theme: theme, useFullWidth: testCase.useFullWidth}
			l := list.New([]list.Item{item}, delegate, 0, 0)
			l.SetWidth(testCase.listWidth)
			columns := delegate.issueListColumnsFor(l.Items(), l.Width())
			var buf strings.Builder
			delegate.Render(&buf, l, 0, item)
			row := buf.String()
			if got := lipgloss.Width(row); got > testCase.listWidth {
				t.Fatalf("row width = %d, list width = %d: %q", got, testCase.listWidth, row)
			}
			if got := lipgloss.Width(row); got > columns.width {
				t.Fatalf("row width = %d, contract width = %d: %q", got, columns.width, row)
			}
			if testCase.useFullWidth && columns.width != testCase.listWidth {
				t.Fatalf("bounded contract width = %d, want %d", columns.width, testCase.listWidth)
			}
			if !testCase.useFullWidth && columns.width != testCase.listWidth-1 {
				t.Fatalf("guarded contract width = %d, want %d", columns.width, testCase.listWidth-1)
			}
		})
	}
}

func TestIssueListRightEdgeContractAcrossViewLayouts(t *testing.T) {
	issue := model.Issue{
		ID: "layout-edge", Title: "layout edge", Status: model.StatusOpen,
		IssueType: model.TypeTask, CreatedAt: time.Now().Add(-2 * time.Hour),
		Comments: []*model.Comment{{ID: "one"}},
	}
	assertLayout := func(t *testing.T, m Model, view string) {
		t.Helper()
		header, row := listHeaderAndRow(t, view, issue.ID)
		for headerMarker, rowMarker := range map[string]string{
			"AGE": "2h", "CMT": "💬1",
		} {
			if got, want := displayOffset(row, rowMarker), displayOffset(header, headerMarker); got != want {
				t.Fatalf("%s offset = %d, want %d", rowMarker, got, want)
			}
		}
	}
	assertManagedWidth := func(t *testing.T, m Model) {
		t.Helper()
		delegate := m.issueListDelegate()
		if !delegate.useFullWidth {
			t.Fatal("model-managed List did not enable full-width mode")
		}
		if got, want := delegate.columns.width, m.list.Width(); got != want {
			t.Fatalf("model-managed List width contract = %d, want full bounded width %d", got, want)
		}
		if got, want := lipgloss.Width(renderIssueListHeader(delegate.columns)), m.list.Width(); got != want {
			t.Fatalf("model-managed header width = %d, want %d", got, want)
		}
		var buf strings.Builder
		delegate.Render(&buf, m.list, 0, m.list.Items()[0])
		if got, want := lipgloss.Width(buf.String()), m.list.Width(); got != want {
			t.Fatalf("model-managed row width = %d, want %d", got, want)
		}
	}

	t.Run("single", func(t *testing.T) {
		m := sizedModel(t, []model.Issue{issue}, 80, 24)
		if m.isSplitView {
			t.Fatal("width 80 unexpectedly selected split view")
		}
		assertManagedWidth(t, m)
		assertLayout(t, m, m.renderListWithHeader())
	})

	t.Run("split", func(t *testing.T) {
		m := sizedModel(t, []model.Issue{issue}, 160, 24)
		if !m.isSplitView {
			t.Fatal("width 160 did not select split view")
		}
		assertManagedWidth(t, m)
		view := m.renderSplitView()
		assertLayout(t, m, view)
		header, row := listHeaderAndRow(t, view, issue.ID)
		listContentRightEdge := m.list.Width() + 1 // one cell after the left border
		if got, want := displayOffset(header, "CMT")+lipgloss.Width("CMT"), listContentRightEdge; got != want {
			t.Fatalf("split CMT header ends at cell %d, want list content edge %d: %q", got, want, header)
		}
		if got, want := displayOffset(row, "💬1")+lipgloss.Width("💬1"), listContentRightEdge; got != want {
			t.Fatalf("split CMT value ends at cell %d, want list content edge %d: %q", got, want, row)
		}
	})

	t.Run("narrow", func(t *testing.T) {
		m := sizedModel(t, []model.Issue{issue}, 72, 24)
		if m.isSplitView {
			t.Fatal("width 72 unexpectedly selected split view")
		}
		assertManagedWidth(t, m)
		assertLayout(t, m, m.renderListWithHeader())
	})

	t.Run("shortcuts sidebar", func(t *testing.T) {
		m := sizedModel(t, []model.Issue{issue}, 80, 24)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")})
		m = updated.(Model)
		if !m.showShortcutsSidebar {
			t.Fatal("shortcuts sidebar did not open")
		}
		assertManagedWidth(t, m)
		assertLayout(t, m, m.renderListWithHeader())
	})
}

func TestSplitPaneReclaimsOnlyListSpacerAtNonDefaultRatios(t *testing.T) {
	for _, ratio := range []float64{0.2, 0.8} {
		t.Run(fmt.Sprintf("ratio-%.1f", ratio), func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(3), 160, 24)
			m.splitPaneRatio = ratio
			m.recalculateSplitPaneSizes()

			allocatedWidth := int(float64(m.mainContentWidth()-8) * ratio)
			if got, want := m.list.Width(), allocatedWidth+2; got != want {
				t.Fatalf("list content width = %d, want prior allocation %d plus two reclaimed cells", got, want)
			}
			if got, want := m.viewport.Width, m.mainContentWidth()-8-allocatedWidth; got != want {
				t.Fatalf("detail content width = %d, want %d", got, want)
			}

			wantListPanelWidth := allocatedWidth + 4
			if got, want := m.list.Width()+2, wantListPanelWidth; got != want {
				t.Fatalf("list panel width = %d, want unchanged divider at %d", got, want)
			}
			if got := m.handleLeftClick(wantListPanelWidth-1, 0).focused; got != focusList {
				t.Fatalf("click before list boundary focused %v, want focusList", got)
			}
			if got := m.handleLeftClick(wantListPanelWidth, 0).focused; got != focusDetail {
				t.Fatalf("click at list boundary focused %v, want focusDetail", got)
			}

			m.applyContentSizing()
			if got, want := m.list.Width(), allocatedWidth+2; got != want {
				t.Fatalf("applyContentSizing list width = %d, want %d", got, want)
			}
			if got, want := m.viewport.Width, m.mainContentWidth()-8-allocatedWidth; got != want {
				t.Fatalf("applyContentSizing detail width = %d, want %d", got, want)
			}
		})
	}
}

func TestListMetadataBudgetImprovesTitleWidthAndIgnoresCommentPopulation(t *testing.T) {
	item := newTestIssueItem("metadata-1")
	item.Issue.Title = "A title with room to grow"
	theme := DefaultTheme(lipgloss.NewRenderer(nil))
	delegate := IssueDelegate{Theme: theme}
	l := list.New([]list.Item{item}, delegate, 100, 10)

	shortComments := delegate.issueListColumnsFor(l.Items(), l.Width())
	item.Issue.Comments = make([]*model.Comment, 999)
	longComments := delegate.issueListColumnsFor([]list.Item{item}, l.Width())
	legacyRightWidth := 8 + 1 + 3 // Previous age cell plus one-comment CMT cell.

	if shortComments.commentsWidth != issueListCommentsWidth || longComments.commentsWidth != issueListCommentsWidth {
		t.Fatalf("comment width was not fixed: short=%d long=%d", shortComments.commentsWidth, longComments.commentsWidth)
	}
	if shortComments.rightWidth != longComments.rightWidth {
		t.Fatalf("comment population changed right-side width: short=%d long=%d", shortComments.rightWidth, longComments.rightWidth)
	}
	if gain := listTitleWidth(shortComments) - (shortComments.width - shortComments.titleStart - legacyRightWidth - 1); gain < 2 {
		t.Fatalf("title width gain = %d cells, want at least 2: columns=%+v", gain, shortComments)
	}
}

func TestListMetadataValuesAlignAndKeepOldAgesLegible(t *testing.T) {
	created := time.Now().Add(-100 * 365 * 24 * time.Hour)
	items := []IssueItem{
		newTestIssueItem("metadata-age"),
		newTestIssueItem("metadata-many-comments"),
	}
	items[0].Issue.CreatedAt = created
	items[0].Issue.Comments = nil
	items[1].Issue.Comments = make([]*model.Comment, 1000)
	listItems := []list.Item{items[0], items[1]}
	theme := DefaultTheme(lipgloss.NewRenderer(nil))
	delegate := IssueDelegate{Theme: theme}
	l := list.New(listItems, delegate, 100, 10)
	l.SetWidth(100)
	columns := delegate.issueListColumnsFor(l.Items(), l.Width())
	header := renderIssueListHeader(columns)

	now := time.Date(2025, time.February, 28, 12, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name    string
		created time.Time
		want    string
	}{
		{name: "leap-day anniversary", created: time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC), want: "12mo"},
		{name: "old calendar age", created: time.Date(now.Year()-300, now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), want: "300y"},
		{name: "large calendar age", created: time.Date(now.Year()-1000, now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), want: "999+"},
	} {
		if got := formatIssueListAgeAt(testCase.created, now); got != testCase.want {
			t.Errorf("%s List age = %q, want %q", testCase.name, got, testCase.want)
		}
		if strings.Contains(formatIssueListAgeAt(testCase.created, now), "…") {
			t.Errorf("%s List age was truncated: %q", testCase.name, formatIssueListAgeAt(testCase.created, now))
		}
	}
	if got := formatIssueListComments(1000); got != "💬+" {
		t.Fatalf("large comment count = %q, want compact marker", got)
	}
	for _, testCase := range []struct {
		count int
		want  string
	}{
		{count: 0, want: ""},
		{count: 1, want: "💬1"},
		{count: 9, want: "💬9"},
		{count: 10, want: "💬+"},
	} {
		if got := formatIssueListComments(testCase.count); got != testCase.want {
			t.Errorf("List comment count %d = %q, want %q", testCase.count, got, testCase.want)
		}
	}
	for _, count := range []int{1, 9, 10} {
		item := items[1]
		item.Issue.Comments = make([]*model.Comment, count)
		parts := delegate.renderRightParts(item, theme, columns)
		if len(parts) < 2 || lipgloss.Width(parts[1]) != issueListCommentsWidth {
			t.Errorf("comment count %d escaped fixed CMT cell: parts=%q", count, parts)
		}
	}

	for index, item := range items {
		var buf strings.Builder
		delegate.Render(&buf, l, index, item)
		row := buf.String()
		age := formatIssueListAge(item.Issue.CreatedAt)
		comments := formatIssueListComments(len(item.Issue.Comments))
		if displayOffset(header, "AGE") != displayOffset(row, age) {
			t.Errorf("row %d AGE is not aligned: header=%d row=%d", index, displayOffset(header, "AGE"), displayOffset(row, age))
		}
		if comments == "" {
			rightParts := delegate.renderRightParts(item, theme, columns)
			if len(rightParts) < 2 || lipgloss.Width(rightParts[1]) != columns.commentsWidth || strings.TrimSpace(rightParts[1]) != "" {
				t.Errorf("row %d did not preserve its blank CMT slot: parts=%q", index, rightParts)
			}
		} else if displayOffset(header, "CMT") != displayOffset(row, comments) {
			t.Errorf("row %d CMT is not aligned: header=%d row=%d", index, displayOffset(header, "CMT"), displayOffset(row, comments))
		}
	}
	ageOffset := displayOffset(header, "AGE")
	commentOffset := displayOffset(header, "CMT")
	if commentOffset-ageOffset-lipgloss.Width("AGE") < 1 {
		t.Fatalf("AGE/CMT header separator is missing: AGE=%d CMT=%d header=%q", ageOffset, commentOffset, header)
	}
}

func TestListMetadataColumnsStayStableAcrossNavigationFiltersAndScopes(t *testing.T) {
	issues := []model.Issue{
		{ID: "stable-long-id", Title: "Open item", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{"ctx:short"}, Assignee: "alice", Comments: []*model.Comment{{ID: "one"}}},
		{ID: "s", Title: "Closed item", Status: model.StatusClosed, IssueType: model.TypeBug, Labels: []string{"ctx:long"}, Comments: make([]*model.Comment, 999)},
	}
	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = repositorypkg.Catalog{
		{ID: "ctx:short", Name: "s", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:long", Name: "long-repository", Kind: repositorypkg.IdentityExact},
	}
	m.list.SetSize(120, 1)
	m.refreshRepositoryPresentation()

	contract := func() (int, int, int) {
		t.Helper()
		columns := m.issueListDelegate().columns
		return columns.titleStart, columns.ageStart, columns.commentsStart
	}
	wantTitle, wantAge, wantComments := contract()

	m.list.Select(1)
	if title, age, comments := contract(); title != wantTitle || age != wantAge || comments != wantComments {
		t.Fatalf("selection moved List columns: got title=%d age=%d cmt=%d, want title=%d age=%d cmt=%d", title, age, comments, wantTitle, wantAge, wantComments)
	}
	m.list.NextPage()
	if title, age, comments := contract(); title != wantTitle || age != wantAge || comments != wantComments {
		t.Fatalf("page navigation moved List columns: got title=%d age=%d cmt=%d, want title=%d age=%d cmt=%d", title, age, comments, wantTitle, wantAge, wantComments)
	}

	m.list.SetFilterText("Closed")
	m.updateListDelegate()
	if title, age, comments := contract(); title != wantTitle || age != wantAge || comments != wantComments {
		t.Fatalf("filter moved List columns: got title=%d age=%d cmt=%d, want title=%d age=%d cmt=%d", title, age, comments, wantTitle, wantAge, wantComments)
	}

	scope, err := hub.NewSelectedContextsHubScope([]string{"ctx:short"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	if title, age, comments := contract(); title != wantTitle || age != wantAge || comments != wantComments {
		t.Fatalf("repository scope moved List columns: got title=%d age=%d cmt=%d, want title=%d age=%d cmt=%d", title, age, comments, wantTitle, wantAge, wantComments)
	}
}

func TestListPaginationFooterMatchesPaginatorAfterArrowNavigation(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		width     int
		wantSplit bool
	}{
		{name: "single-column", width: 80},
		{name: "split", width: 120, wantSplit: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			m := sizedModel(t, mouseTestIssues(60), testCase.width, 24)
			if m.isSplitView != testCase.wantSplit {
				t.Fatalf("isSplitView = %v, want %v", m.isSplitView, testCase.wantSplit)
			}
			if m.list.Paginator.TotalPages < 3 {
				t.Fatalf("test setup produced %d pages, want at least 3", m.list.Paginator.TotalPages)
			}

			assertFooter := func(wantPage int) {
				t.Helper()
				items := m.list.VisibleItems()
				start, end := m.list.Paginator.GetSliceBounds(len(items))
				page := m.list.Paginator.Page + 1
				if page != wantPage {
					t.Fatalf("current page = %d, want %d", page, wantPage)
				}
				startItem, endItem := 0, 0
				if start < end {
					startItem, endItem = start+1, end
				}
				view := m.renderListWithHeader()
				if m.isSplitView {
					view = m.renderSplitView()
				}
				want := fmt.Sprintf("%d-%d of %d", startItem, endItem, len(items))
				if !strings.Contains(view, want) {
					t.Fatalf("page %d footer missing %q:\n%s", page, want, view)
				}
				pageLabel := fmt.Sprintf("Page %d of %d", page, m.list.Paginator.TotalPages)
				if m.isSplitView {
					pageLabel = fmt.Sprintf("Page %d/%d", page, m.list.Paginator.TotalPages)
				}
				if !strings.Contains(view, pageLabel) {
					t.Fatalf("rendered page label missing %q:\n%s", pageLabel, view)
				}
				if start < end {
					id := fmt.Sprintf("bv-%03d", start)
					if !strings.Contains(view, id) {
						t.Fatalf("page %d does not render first paginator item %q:\n%s", page, id, view)
					}
				}
			}

			assertFooter(1)
			for _, step := range []struct {
				key  string
				page int
			}{
				{key: "right", page: 2},
				{key: "right", page: 3},
				{key: "left", page: 2},
				{key: "left", page: 1},
			} {
				updated, _ := m.Update(keyMsg(step.key))
				m = updated.(Model)
				assertFooter(step.page)
			}
		})
	}
}

func TestListLayoutUsesCanonicalIssuesWhenFiltersRemoveWidestMetadata(t *testing.T) {
	wideID := strings.Repeat("widest-id-", 5)
	issues := []model.Issue{
		{ID: wideID, Title: "Wide metadata owner", Status: model.StatusOpen, IssueType: model.TypeBug, Labels: []string{"ctx:wide", "sole-label"}, Assignee: "sole-assignee"},
		{ID: "narrow", Title: "Narrow metadata", Status: model.StatusClosed, IssueType: model.TypeTask, Labels: []string{"ctx:narrow"}},
	}
	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = repositorypkg.Catalog{
		{ID: "ctx:wide", Name: "wide-repository", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:narrow", Name: "narrow", Kind: repositorypkg.IdentityExact},
	}
	m.timeTravelMode = true
	m.newIssueIDs = map[string]bool{wideID: true}
	m.quickWinSet = map[string]bool{wideID: true}
	m.unblocksMap = map[string][]string{wideID: {"one", "two", "three"}}
	m.list.SetSize(180, 5)
	items := m.list.Items()
	for index, item := range items {
		issueItem := item.(IssueItem)
		if issueItem.Issue.ID == wideID {
			issueItem.DiffStatus = DiffStatusNew
			issueItem.IsQuickWin = true
			issueItem.UnblocksCount = 3
			items[index] = issueItem
		}
	}
	m.list.SetItems(items)
	m.refreshRepositoryPresentation()

	contract := func() []int {
		t.Helper()
		return listColumnStarts(m.issueListDelegate().columns)
	}
	want := contract()

	m.currentFilter = "closed"
	m.applyFilter()
	if got := contract(); !reflect.DeepEqual(got, want) {
		t.Fatalf("status filter changed List starts: got %v, want %v", got, want)
	}

	m.currentFilter = "all"
	m.activeIssueTypes = map[model.IssueType]bool{model.TypeTask: true}
	m.applyFilter()
	if got := contract(); !reflect.DeepEqual(got, want) {
		t.Fatalf("type filter changed List starts: got %v, want %v", got, want)
	}

	m.activeIssueTypes = nil
	m.currentFilter = "recipe:stable"
	r := &recipe.Recipe{Filters: recipe.FilterConfig{Status: []string{"closed"}}}
	m.activeRecipe = r
	m.applyRecipe(r)
	if got := contract(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recipe filter changed List starts: got %v, want %v", got, want)
	}

	m.activeRecipe = nil
	m.currentFilter = "all"
	scope, err := hub.NewSelectedContextsHubScope([]string{"ctx:narrow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	if got := contract(); !reflect.DeepEqual(got, want) {
		t.Fatalf("scope filter changed List starts after removing widest ID/metadata owners: got %v, want %v", got, want)
	}
}

func TestHubListExtraWidthUsesCanonicalIssuesAfterLargestIssueLeavesScope(t *testing.T) {
	wideID := "wide-contexts"
	issues := []model.Issue{
		{ID: wideID, Title: "Wide contexts", Status: model.StatusOpen, Labels: []string{"ctx:wide"}},
		{ID: "narrow-context", Title: "Narrow context", Status: model.StatusOpen, Labels: []string{"ctx:narrow"}},
	}
	catalog := repositorypkg.Catalog{
		{ID: "ctx:wide", Name: "wide", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:narrow", Name: "narrow", Kind: repositorypkg.IdentityExact},
	}
	for index := 0; index < 12; index++ {
		contextID := fmt.Sprintf("ctx:extra-%02d", index)
		catalog = append(catalog, repositorypkg.CatalogEntry{ID: contextID, Name: contextID, Kind: repositorypkg.IdentityExact})
		issues[0].Labels = append(issues[0].Labels, contextID)
	}

	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	m.refreshRepositoryPresentation()
	_, wantExtra := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if wantExtra != lipgloss.Width("+12") {
		t.Fatalf("initial canonical +N width = %d, want %d", wantExtra, lipgloss.Width("+12"))
	}

	scope, err := hub.NewSelectedContextsHubScope([]string{"ctx:narrow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	_, gotExtra := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if gotExtra != wantExtra {
		t.Fatalf("+N width changed after removing largest multi-context issue: got %d, want %d", gotExtra, wantExtra)
	}
	if got := visibleIssueIDs(m); !reflect.DeepEqual(got, []string{"narrow-context"}) {
		t.Fatalf("scope retained the wide multi-context issue: %v", got)
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
	catalog := repositorypkg.Catalog{
		{ID: "ctx:long", Name: "beads_viewer", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:s", Name: "s", Kind: repositorypkg.IdentityExact},
	}
	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
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
	narrow.runtimeServices.CatalogPath = "hub.yaml"
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

func TestHubListRepositoryWidthUsesStableCatalogPolicy(t *testing.T) {
	issues := []model.Issue{
		{ID: "short", Title: "Short repository", Status: model.StatusOpen, Labels: []string{"ctx:s"}},
		{ID: "inbox", Title: "Contextless", Status: model.StatusOpen},
	}
	catalog := repositorypkg.Catalog{
		{ID: "ctx:s", Name: "s", Kind: repositorypkg.IdentityExact},
		{ID: "ctx:inactive", Name: "beads_viewer", Kind: repositorypkg.IdentityExact},
	}

	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	scope, err := hub.NewSelectedContextsHubScope([]string{"ctx:s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetHubScope(scope); err != nil {
		t.Fatal(err)
	}
	nameWidth, _ := m.repositoryListColumnWidths(IssueDelegate{Theme: m.theme})
	if nameWidth != 12 {
		t.Fatalf("stable repository width = %d, want 12", nameWidth)
	}
	if strings.Contains(m.list.View(), "beads_viewer") {
		t.Fatalf("inactive repository widened or leaked into short scope:\n%s", m.list.View())
	}

	contextless := NewModel(issues, nil, "")
	contextless.runtimeServices.CatalogPath = "hub.yaml"
	contextless.repositoryCatalog = catalog
	contextless.list.SetSize(120, 10)
	if err := contextless.SetHubScope(hub.NewContextlessHubScope()); err != nil {
		t.Fatal(err)
	}
	nameWidth, _ = contextless.repositoryListColumnWidths(IssueDelegate{Theme: contextless.theme})
	if nameWidth != lipgloss.Width("beads_viewer") {
		t.Fatalf("stable contextless repository width = %d, want %d", nameWidth, lipgloss.Width("beads_viewer"))
	}
}

func TestHubListExtraWidthUsesRenderedInactiveContexts(t *testing.T) {
	const activeID = "ctx:active"
	issues := []model.Issue{
		{ID: "multi-1", Title: "Multi-context", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{activeID}},
		{ID: "single1", Title: "Single-context", Status: model.StatusOpen, IssueType: model.TypeTask, Labels: []string{activeID}},
	}
	catalog := repositorypkg.Catalog{{ID: activeID, Name: "beads_viewer", Kind: repositorypkg.IdentityExact}}
	for index := 0; index < 12; index++ {
		contextID := fmt.Sprintf("ctx:inactive-%02d", index)
		catalog = append(catalog, repositorypkg.CatalogEntry{
			ID: contextID, Name: fmt.Sprintf("repo-%02d", index), Kind: repositorypkg.IdentityExact,
		})
		issues[0].Labels = append(issues[0].Labels, contextID)
	}

	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.repositoryCatalog = catalog
	m.list.SetSize(120, 10)
	scope, err := hub.NewSelectedContextsHubScope([]string{activeID})
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
	allItems.runtimeServices.CatalogPath = "hub.yaml"
	allItems.repositoryCatalog = catalog
	allItems.list.SetSize(120, 10)
	allNameWidth, allExtraWidth := allItems.repositoryListColumnWidths(IssueDelegate{Theme: allItems.theme})
	if allNameWidth != lipgloss.Width("beads_viewer") || allExtraWidth != 0 {
		t.Fatalf("all-items unused extra reservation = name:%d extra:%d, want name:%d extra:0", allNameWidth, allExtraWidth, lipgloss.Width("beads_viewer"))
	}
}
