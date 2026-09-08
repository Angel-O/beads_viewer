package ui

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/lipgloss"
)

func boardOrderTime(day int) time.Time {
	return time.Date(2026, time.January, day, 0, 0, 0, 0, time.UTC)
}

func boardOrderIDs(issues []model.Issue) []string {
	ids := make([]string, len(issues))
	for i := range issues {
		ids[i] = issues[i].ID
	}
	return ids
}

func boardCardLeadingIndent(card string) int {
	for _, line := range strings.Split(card, "\n") {
		if strings.Contains(line, "╭") || strings.Contains(line, "╔") {
			return len(line) - len(strings.TrimLeft(line, " "))
		}
	}
	return -1
}

func boardCardTopRows(composed string) []int {
	var rows []int
	for row, line := range strings.Split(composed, "\n") {
		if strings.Contains(line, "╭") || strings.Contains(line, "╔") {
			rows = append(rows, row)
		}
	}
	return rows
}

func boardOrderFixture(mode SwimLaneMode) []model.Issue {
	childType := model.TypeTask
	if mode == SwimByType {
		childType = model.TypeEpic
	}
	priority := 1
	if mode != SwimByPriority {
		priority = 2
	}
	childPriority := 0
	if mode == SwimByPriority {
		childPriority = priority
	}
	return []model.Issue{
		{ID: "child-new", Status: model.StatusOpen, Priority: childPriority, IssueType: childType, CreatedAt: boardOrderTime(3), Dependencies: []*model.Dependency{{DependsOnID: "epic", Type: model.DepParentChild}}},
		{ID: "child-old", Status: model.StatusOpen, Priority: childPriority, IssueType: childType, CreatedAt: boardOrderTime(1), Dependencies: []*model.Dependency{{DependsOnID: "epic", Type: model.DepParentChild}}},
		{ID: "other", Status: model.StatusOpen, Priority: priority, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(4)},
		{ID: "epic", Status: model.StatusOpen, Priority: priority, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(2)},
	}
}

func TestGroupIssuesByModeOrdersDirectChildrenAfterEpic(t *testing.T) {
	for _, mode := range []SwimLaneMode{SwimByStatus, SwimByPriority, SwimByType} {
		t.Run(modeName(mode), func(t *testing.T) {
			columns := groupIssuesByMode(boardOrderFixture(mode), mode)
			var got []string
			for _, column := range columns {
				if len(column) > 0 {
					got = boardOrderIDs(column)
					break
				}
			}
			want := []string{"other", "epic", "child-new", "child-old"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("order = %v, want %v", got, want)
			}
		})
	}
}

func TestBoardOrderKeepsBaselineWhenParentIsFiltered(t *testing.T) {
	issues := boardOrderFixture(SwimByStatus)
	filtered := []model.Issue{issues[0], issues[1], issues[2]}
	b := NewBoardModel(filtered, Theme{})
	if got, want := boardOrderIDs(b.columns[ColOpen]), []string{"child-new", "child-old", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered order = %v, want %v", got, want)
	}
}

func TestOrderParentChildGroupsSingleChild(t *testing.T) {
	issues := []model.Issue{
		{ID: "child", Status: model.StatusOpen, Priority: 0, CreatedAt: boardOrderTime(3), Dependencies: []*model.Dependency{{DependsOnID: "epic", Type: model.DepParentChild}}},
		{ID: "other", Status: model.StatusOpen, Priority: 1, CreatedAt: boardOrderTime(4)},
		{ID: "epic", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(2)},
	}
	got := groupIssuesByMode(issues, SwimByStatus)[ColOpen]
	if want := []string{"other", "epic", "child"}; !reflect.DeepEqual(boardOrderIDs(got), want) {
		t.Fatalf("single-child order = %v, want %v", boardOrderIDs(got), want)
	}
}

func TestOrderParentChildGroupsDoesNotGroupTransitiveDescendants(t *testing.T) {
	issues := []model.Issue{
		{ID: "grandchild", Status: model.StatusOpen, Priority: 0, CreatedAt: boardOrderTime(4), Dependencies: []*model.Dependency{{DependsOnID: "child", Type: model.DepParentChild}}},
		{ID: "child", Status: model.StatusOpen, Priority: 1, CreatedAt: boardOrderTime(1), Dependencies: []*model.Dependency{{DependsOnID: "epic", Type: model.DepParentChild}}},
		{ID: "other", Status: model.StatusOpen, Priority: 2, CreatedAt: boardOrderTime(3)},
		{ID: "epic", Status: model.StatusOpen, Priority: 3, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(2)},
	}
	got := groupIssuesByMode(issues, SwimByStatus)[ColOpen]
	if want := []string{"grandchild", "other", "epic", "child"}; !reflect.DeepEqual(boardOrderIDs(got), want) {
		t.Fatalf("transitive-descendant order = %v, want %v", boardOrderIDs(got), want)
	}
}

func TestOrderParentChildGroupsNestedEpicIsUnsupported(t *testing.T) {
	issues := []model.Issue{
		{ID: "grandchild", Status: model.StatusOpen, Priority: 0, CreatedAt: boardOrderTime(4), Dependencies: []*model.Dependency{{DependsOnID: "nested-epic", Type: model.DepParentChild}}},
		{ID: "nested-epic", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(1), Dependencies: []*model.Dependency{{DependsOnID: "outer-epic", Type: model.DepParentChild}}},
		{ID: "other", Status: model.StatusOpen, Priority: 2, CreatedAt: boardOrderTime(3)},
		{ID: "outer-epic", Status: model.StatusOpen, Priority: 3, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(2)},
		{ID: "later", Status: model.StatusOpen, Priority: 4, CreatedAt: boardOrderTime(5)},
	}
	first := groupIssuesByMode(append([]model.Issue(nil), issues...), SwimByStatus)[ColOpen]
	second := groupIssuesByMode(append([]model.Issue(nil), issues...), SwimByStatus)[ColOpen]
	got := boardOrderIDs(first)
	if !reflect.DeepEqual(got, boardOrderIDs(second)) {
		t.Fatalf("unsupported nested relationship is not deterministic: %v vs %v", got, boardOrderIDs(second))
	}
	if len(got) != len(issues) {
		t.Fatalf("unsupported nested relationship dropped cards: got %d, want %d", len(got), len(issues))
	}
	if want := []string{"other", "outer-epic", "nested-epic"}; !reflect.DeepEqual(got[:3], want) {
		t.Fatalf("top-level epic group = %v, want %v", got[:3], want)
	}
	if got[3] == "grandchild" {
		t.Fatal("nested epic's child was recursively grouped")
	}
}

func TestBoardOrderMatchesPrecomputedSnapshotForAllModes(t *testing.T) {
	for _, mode := range []SwimLaneMode{SwimByStatus, SwimByPriority, SwimByType} {
		issues := boardOrderFixture(mode)
		snapshot := NewSnapshotBuilder(append([]model.Issue(nil), issues...)).Build()
		filteredPath := NewBoardModel(nil, Theme{})
		snapshotPath := NewBoardModel(nil, Theme{})
		for current := SwimByStatus; current < mode; current++ {
			filteredPath.CycleSwimLaneMode()
			snapshotPath.CycleSwimLaneMode()
		}
		filteredPath.SetIssues(append([]model.Issue(nil), issues...))
		snapshotPath.SetSnapshot(snapshot)
		if !reflect.DeepEqual(snapshotPath.columns, filteredPath.columns) {
			t.Fatalf("mode %s snapshot columns differ from filtered path:\n got %#v\nwant %#v", modeName(mode), snapshotPath.columns, filteredPath.columns)
		}
	}
}

func TestBoardGroupVisualTreatmentAcrossModes(t *testing.T) {
	for _, mode := range []SwimLaneMode{SwimByStatus, SwimByPriority, SwimByType} {
		t.Run(modeName(mode), func(t *testing.T) {
			issues := boardOrderFixture(mode)
			issues[0].Title = "Child new"
			issues[1].Title = "Child old"
			issues[2].Title = "Other"
			issues[3].Title = "Parent"
			b := NewBoardModel(issues, DefaultTheme(lipgloss.NewRenderer(io.Discard)))
			for current := SwimByStatus; current < mode; current++ {
				b.CycleSwimLaneMode()
			}

			var renderedCards string
			var sawParent, sawFirstChild, sawLastChild bool
			var parentCard, firstChildCard, childCard, ordinaryCard string
			var parentIssue, childIssue model.Issue
			var parentCol, parentRow, childCol, childRow int
			for col := range b.columns {
				for row, issue := range b.columns[col] {
					card := b.renderCard(issue, 20, false, col, row)
					renderedCards += card
					switch issue.ID {
					case "epic":
						parentCard, parentIssue, parentCol, parentRow = card, issue, col, row
					case "child-new":
						firstChildCard = card
					case "child-old":
						childCard, childIssue, childCol, childRow = card, issue, col, row
					case "other":
						ordinaryCard = card
					}
					parent, child, last := boardCardGroupRole(issue, b.columns[col], row)
					sawParent = sawParent || issue.ID == "epic" && parent
					sawFirstChild = sawFirstChild || issue.ID == "child-new" && child && !last
					sawLastChild = sawLastChild || issue.ID == "child-old" && child && last
				}
			}
			if !sawParent || !sawFirstChild || !sawLastChild {
				t.Fatal("flat group roles did not preserve parent, child, and final-child separation")
			}
			if got, want := boardCardLeadingIndent(childCard), boardCardLeadingIndent(parentCard)+2; got != want || boardCardLeadingIndent(ordinaryCard) != boardCardLeadingIndent(parentCard) {
				t.Fatalf("normal card indentation = child:%d parent:%d ordinary:%d", got, boardCardLeadingIndent(parentCard), boardCardLeadingIndent(ordinaryCard))
			}
			parentExpanded := b.renderExpandedCard(parentIssue, 20, parentCol, parentRow)
			childExpanded := b.renderExpandedCard(childIssue, 20, childCol, childRow)
			if got, want := boardCardLeadingIndent(childExpanded), boardCardLeadingIndent(parentExpanded)+2; got != want {
				t.Fatalf("expanded child indentation = %d, want %d", got, want)
			}
			rows := boardCardTopRows(lipgloss.JoinVertical(lipgloss.Left, parentCard, firstChildCard, childCard, ordinaryCard))
			if len(rows) != 4 {
				t.Fatalf("composed card top rows = %v, want four cards", rows)
			}
			if got, want := rows[1]-rows[0], rows[2]-rows[1]; got != want {
				t.Fatalf("composed intra-group gaps differ: parent-child=%d sibling=%d", got, want)
			}
			if got, want := rows[1]-rows[0], lipgloss.Height(parentCard); got != want {
				t.Fatalf("parent-to-first-child gap = %d, want touching cards at %d", got, want)
			}
			if got, want := rows[3]-rows[2], rows[2]-rows[1]+2; got != want {
				t.Fatalf("final-child separation = %d, want %d", got, want)
			}
			view := b.View(40, 24)
			if strings.Count(renderedCards, "◆") != 1 || strings.Count(renderedCards, "↳") != 2 {
				t.Fatalf("group markers missing from rendered cards:\n%s", renderedCards)
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > 40 {
					t.Fatalf("board line overflows constrained width: %d > 40: %q", lipgloss.Width(line), line)
				}
			}
			if !b.SelectIssueByID("child-old") {
				t.Fatal("could not select direct child for expanded rendering")
			}
			b.ToggleExpand()
			for _, line := range strings.Split(b.View(40, 24), "\n") {
				if lipgloss.Width(line) > 40 {
					t.Fatalf("expanded board line overflows constrained width: %d > 40: %q", lipgloss.Width(line), line)
				}
			}

			solo := model.Issue{ID: "solo", Title: "Solo", Status: model.StatusOpen, IssueType: model.TypeEpic}
			soloBoard := NewBoardModel([]model.Issue{solo}, DefaultTheme(lipgloss.NewRenderer(io.Discard)))
			soloCard := soloBoard.renderCard(solo, 20, false, ColOpen, 0)
			if strings.Contains(soloCard, "◆") || strings.Contains(soloCard, "↳") {
				t.Fatalf("epic without visible children received group markers: %s", soloCard)
			}
		})
	}
}

func TestBoardGroupVisualTreatmentLeavesNestedRelationshipsFlat(t *testing.T) {
	issues := []model.Issue{
		{ID: "grandchild", Title: "Grandchild", Status: model.StatusOpen, Priority: 0, Dependencies: []*model.Dependency{{DependsOnID: "nested-epic", Type: model.DepParentChild}}},
		{ID: "nested-epic", Title: "Nested", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeEpic, Dependencies: []*model.Dependency{{DependsOnID: "outer-epic", Type: model.DepParentChild}}},
		{ID: "outer-epic", Title: "Outer", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeEpic},
	}
	b := NewBoardModel(issues, DefaultTheme(lipgloss.NewRenderer(io.Discard)))
	view := b.View(80, 24)
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("nested board line overflows constrained width: %d > 80: %q", lipgloss.Width(line), line)
		}
	}
	var renderedCards string
	for col := range b.columns {
		for row, issue := range b.columns[col] {
			renderedCards += b.renderCard(issue, 20, false, col, row)
		}
	}
	if strings.Count(renderedCards, "◆") != 1 || strings.Count(renderedCards, "↳") != 1 {
		t.Fatalf("nested relationship received special presentation:\n%s", renderedCards)
	}
}

func TestOrderParentChildGroupsHandlesMalformedRelationships(t *testing.T) {
	issues := []model.Issue{
		{ID: "multi", Status: model.StatusOpen, Priority: 0, CreatedAt: boardOrderTime(1), Dependencies: []*model.Dependency{
			{DependsOnID: "epic-z", Type: model.DepParentChild},
			{DependsOnID: "epic-a", Type: model.DepParentChild},
			{DependsOnID: "missing", Type: model.DepParentChild},
			nil,
		}},
		{ID: "epic-z", Status: model.StatusOpen, Priority: 1, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(2)},
		{ID: "epic-a", Status: model.StatusOpen, Priority: 2, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(3)},
		{ID: "cycle-a", Status: model.StatusOpen, Priority: 3, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(4), Dependencies: []*model.Dependency{{DependsOnID: "cycle-b", Type: model.DepParentChild}}},
		{ID: "cycle-b", Status: model.StatusOpen, Priority: 4, IssueType: model.TypeEpic, CreatedAt: boardOrderTime(5), Dependencies: []*model.Dependency{{DependsOnID: "cycle-a", Type: model.DepParentChild}}},
	}

	first := groupIssuesByMode(append([]model.Issue(nil), issues...), SwimByStatus)[ColOpen]
	second := groupIssuesByMode(append([]model.Issue(nil), issues...), SwimByStatus)[ColOpen]
	if !reflect.DeepEqual(boardOrderIDs(first), boardOrderIDs(second)) {
		t.Fatalf("malformed relationship ordering is not deterministic: %v vs %v", boardOrderIDs(first), boardOrderIDs(second))
	}
	if len(first) != len(issues) {
		t.Fatalf("malformed relationship ordering dropped cards: got %d, want %d", len(first), len(issues))
	}
	if got, want := boardOrderIDs(first), []string{"epic-z", "epic-a", "multi", "cycle-a", "cycle-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("malformed relationship order = %v, want %v", got, want)
	}
}

func modeName(mode SwimLaneMode) string {
	switch mode {
	case SwimByPriority:
		return "priority"
	case SwimByType:
		return "type"
	default:
		return "status"
	}
}
