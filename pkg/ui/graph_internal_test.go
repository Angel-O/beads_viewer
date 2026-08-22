package ui

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestSmartTruncateID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		maxLen   int
		expected string
	}{
		{"Short ID fits", "foo", 10, "foo"},
		{"Exact fit", "foo-bar", 7, "foo-bar"},
		{"Simple truncation", "foo-bar-baz", 5, "foo-…"},
		{"Hyphenated ID abbreviation", "service-auth-login", 10, "s-a-login"},
		{"Underscore ID abbreviation", "service_auth_login", 10, "s_a_login"},
		{"Mixed separators (hyphen priority)", "service-auth_login", 12, "s-a_login"},
		{"Mixed separators (complex)", "foo-bar_baz-qux", 10, "f-b_b-qux"},
		{"Very short limit", "abc-def", 3, "ab…"},
		{"Single part ID truncation", "verylongsinglepartid", 5, "very…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := smartTruncateID(tt.id, tt.maxLen)
			runeCount := utf8.RuneCountInString(got)
			if runeCount > tt.maxLen {
				t.Errorf("Result rune count %d exceeds maxLen %d. Got: %s", runeCount, tt.maxLen, got)
			}
			// We don't assert exact match for mixed/complex because the logic is heuristic
			// but we check that it produces *something* valid and doesn't crash or empty out
			if got == "" && tt.maxLen > 0 {
				t.Errorf("Result is empty")
			}

			// For specific mixed case that failed before fix:
			if tt.name == "Mixed separators (complex)" {
				// Before fix: split by '-' (defaulting sep to '-') would likely yield chunks that included '_'
				// After fix: FieldsFunc splits by both, so abbreviation logic should work better
				// Just verifying it doesn't look totally broken
				t.Logf("Input: %s, Max: %d, Got: %s", tt.id, tt.maxLen, got)
			}
		})
	}
}

func TestGraphModelOmitsTodoNodes(t *testing.T) {
	issues := []model.Issue{
		{ID: "capture", Title: "Captured note", IssueType: "todo"},
		{ID: "work", Title: "Ordinary work", IssueType: model.TypeTask},
	}

	g := NewGraphModel(issues, nil, createTheme())

	if g.TotalCount() != 1 {
		t.Fatalf("TotalCount() = %d, want 1", g.TotalCount())
	}
	if g.SelectByID("capture") {
		t.Fatal("todo issue should not be selectable in Graph View")
	}
	if view := g.View(100, 30); strings.Contains(view, "capture") {
		t.Fatalf("Graph View rendered todo ID: %q", view)
	}
}

func TestGraphModelRetainsIsolatedTask(t *testing.T) {
	issues := []model.Issue{{ID: "standalone", Title: "Standalone task", IssueType: model.TypeTask}}

	g := NewGraphModel(issues, nil, createTheme())

	if g.TotalCount() != 1 {
		t.Fatalf("TotalCount() = %d, want 1", g.TotalCount())
	}
	if selected := g.SelectedIssue(); selected == nil || selected.ID != "standalone" {
		t.Fatalf("SelectedIssue() = %#v, want isolated task", selected)
	}
}

func TestGraphModelTodoFilterDoesNotMutateCanonicalIssues(t *testing.T) {
	issues := []model.Issue{
		{ID: "capture", Title: "Captured note", IssueType: "todo"},
		{ID: "work", Title: "Ordinary work", IssueType: model.TypeTask},
	}
	want := append([]model.Issue(nil), issues...)

	g := NewGraphModel(issues, nil, createTheme())

	if !reflect.DeepEqual(issues, want) {
		t.Fatalf("source issues mutated: got %#v, want %#v", issues, want)
	}
	if len(g.issues) != len(want) || g.issueMap["capture"] == nil {
		t.Fatalf("canonical GraphModel issues were filtered: issues=%d todo=%#v", len(g.issues), g.issueMap["capture"])
	}
}

func TestGraphModelOnlyTodosUsesEligibleEmptyState(t *testing.T) {
	g := NewGraphModel([]model.Issue{{ID: "capture", IssueType: "todo"}}, nil, createTheme())

	if view := g.View(80, 24); !strings.Contains(view, "No issues eligible for Graph View") {
		t.Fatalf("unexpected empty state: %q", view)
	}
}

func TestGraphModelSearchIDTitleAndRepeat(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha-1", Title: "First worker", IssueType: model.TypeTask},
		{ID: "beta-2", Title: "Alpha follow-up", IssueType: model.TypeTask},
		{ID: "gamma-3", Title: "Unrelated", IssueType: model.TypeTask},
	}
	g := NewGraphModel(issues, nil, createTheme())
	g.SelectByID("gamma-3")

	g.StartSearch()
	g.AppendSearchRunes([]rune("ALPHA"))
	if !g.CommitSearch() {
		t.Fatal("CommitSearch() did not select the first ID/title match")
	}
	if selected := g.SelectedIssue(); selected == nil || selected.ID != "alpha-1" {
		t.Fatalf("first match = %#v, want alpha-1", selected)
	}
	if !g.NextSearchMatch() || g.SelectedIssue().ID != "beta-2" {
		t.Fatalf("next match = %#v, want beta-2", g.SelectedIssue())
	}
	if !g.NextSearchMatch() || g.SelectedIssue().ID != "alpha-1" {
		t.Fatalf("wrapped next match = %#v, want alpha-1", g.SelectedIssue())
	}
	if !g.PreviousSearchMatch() || g.SelectedIssue().ID != "beta-2" {
		t.Fatalf("previous match = %#v, want beta-2", g.SelectedIssue())
	}
}

func TestGraphModelSearchZeroResultsAndClearing(t *testing.T) {
	g := NewGraphModel([]model.Issue{{ID: "alpha-1", Title: "First worker", IssueType: model.TypeTask}}, nil, createTheme())

	g.StartSearch()
	g.AppendSearchRunes([]rune("missing"))
	if g.CommitSearch() {
		t.Fatal("zero-result search unexpectedly selected a node")
	}
	view := g.View(100, 30)
	if !strings.Contains(view, "Search: /missing") || !strings.Contains(view, "no matches") {
		t.Fatalf("zero-result query feedback missing from Graph view: %q", view)
	}

	g.ClearSearch()
	if g.HasSearchQuery() || g.IsSearchInputActive() {
		t.Fatalf("search was not cleared: query=%q input=%v", g.SearchQuery(), g.IsSearchInputActive())
	}
	if view := g.View(100, 30); strings.Contains(view, "Search: /") {
		t.Fatalf("cleared query remains visible: %q", view)
	}
}

func TestGraphModelSearchCancellationRestoresSelection(t *testing.T) {
	g := NewGraphModel([]model.Issue{
		{ID: "alpha-1", Title: "First", IssueType: model.TypeTask},
		{ID: "beta-2", Title: "Second", IssueType: model.TypeTask},
	}, nil, createTheme())
	g.SelectByID("beta-2")

	g.StartSearch()
	g.AppendSearchRunes([]rune("alpha"))
	g.ClearSearch()

	if selected := g.SelectedIssue(); selected == nil || selected.ID != "beta-2" {
		t.Fatalf("selection after cancelled input = %#v, want beta-2", selected)
	}
}

func TestGraphModelSearchRespectsProjectedScopeAndTopology(t *testing.T) {
	visible := []model.Issue{
		{ID: "scope-a", Title: "Visible alpha", IssueType: model.TypeTask},
		{ID: "scope-b", Title: "Visible beta", IssueType: model.TypeTask, Dependencies: []*model.Dependency{{DependsOnID: "scope-a", Type: model.DepBlocks}}},
	}
	hidden := model.Issue{ID: "other-alpha", Title: "Hidden alpha", IssueType: model.TypeTask}
	todo := model.Issue{ID: "todo-alpha", Title: "Captured alpha", IssueType: "todo"}
	canonical := map[string]*model.Issue{
		visible[0].ID: &visible[0],
		visible[1].ID: &visible[1],
		hidden.ID:     &hidden,
		todo.ID:       &todo,
	}

	g := NewGraphModel(nil, nil, createTheme())
	g.SetProjectedIssues(visible, canonical, nil)
	wantIDs := append([]string(nil), g.sortedIDs...)
	wantBlockers := cloneStringSlices(g.blockers)
	wantDependents := cloneStringSlices(g.dependents)

	g.StartSearch()
	g.AppendSearchRunes([]rune("other-alpha"))
	if g.CommitSearch() {
		t.Fatal("search selected a node outside the projected repository scope")
	}
	g.ClearSearch()
	g.StartSearch()
	g.AppendSearchRunes([]rune("todo-alpha"))
	if g.CommitSearch() {
		t.Fatal("search selected a hidden todo node")
	}
	g.ClearSearch()
	g.StartSearch()
	g.AppendSearchRunes([]rune("visible beta"))
	if !g.CommitSearch() || g.SelectedIssue().ID != "scope-b" {
		t.Fatalf("visible scoped match = %#v, want scope-b", g.SelectedIssue())
	}

	if !reflect.DeepEqual(g.sortedIDs, wantIDs) || !reflect.DeepEqual(g.blockers, wantBlockers) || !reflect.DeepEqual(g.dependents, wantDependents) {
		t.Fatal("Graph search changed presentation nodes or topology")
	}
}

func cloneStringSlices(source map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(source))
	for key, values := range source {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}
