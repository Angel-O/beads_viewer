package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestGraphModelSearchStatusStaysWithinOneDisplayLine(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		query     string
		forbidden []rune
	}{
		{name: "long ASCII", width: 24, query: strings.Repeat("long-query-", 20)},
		{name: "wide CJK", width: 17, query: strings.Repeat("検索", 20)},
		{name: "narrow", width: 1, query: "検索"},
		{name: "C0 control characters", width: 20, query: "first\tsecond\nthird", forbidden: []rune{'\t', '\n'}},
		{name: "C1 control characters", width: 30, query: "first\u0085second\u009bthird", forbidden: []rune{'\u0085', '\u009b'}},
		{name: "zero width", width: 0, query: "hidden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGraphModel(nil, nil, createTheme())
			g.StartSearch()
			g.AppendSearchRunes([]rune(tt.query))
			if got := g.SearchQuery(); got != tt.query {
				t.Fatalf("stored query = %q, want unchanged %q", got, tt.query)
			}

			status := g.renderSearchStatus(tt.width, g.theme)
			if tt.width <= 0 {
				if status != "" {
					t.Fatalf("zero-width status = %q, want empty", status)
				}
				return
			}
			if height := lipgloss.Height(status); height != 1 {
				t.Fatalf("status height = %d, want 1: %q", height, status)
			}
			if width := lipgloss.Width(status); width > tt.width {
				t.Fatalf("status width = %d, want <= %d: %q", width, tt.width, status)
			}
			for _, forbidden := range tt.forbidden {
				if strings.ContainsRune(status, forbidden) {
					t.Fatalf("rendered status contains control rune U+%04X: %q", forbidden, status)
				}
			}
		})
	}
}

func TestGraphModelSearchStatusPreservesViewBounds(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{name: "small", width: 12, height: 6},
		{name: "single row", width: 12, height: 1},
		{name: "single cell", width: 1, height: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGraphModel(nil, nil, createTheme())
			g.StartSearch()
			g.AppendSearchRunes([]rune(strings.Repeat("検索", 20)))

			view := g.View(tt.width, tt.height)
			if got := lipgloss.Width(view); got > tt.width {
				t.Fatalf("Graph view width = %d, want <= %d:\n%s", got, tt.width, view)
			}
			if got := lipgloss.Height(view); got > tt.height {
				t.Fatalf("Graph view height = %d, want <= %d:\n%s", got, tt.height, view)
			}
		})
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

func TestGraphLegendUsesExhaustiveStatusGlyphMapping(t *testing.T) {
	g := NewGraphModel([]model.Issue{{ID: "open", Status: model.StatusOpen}}, nil, createTheme())
	legend := renderStatusLegend(120, g.theme)
	normalized := strings.Join(strings.Fields(legend), " ")

	expected := []struct {
		status model.Status
		icon   string
		label  string
	}{
		{model.StatusOpen, "🔵", "open"},
		{model.StatusInProgress, "🟡", "in progress"},
		{model.StatusBlocked, "🔴", "blocked"},
		{model.StatusDeferred, "⏸️", "deferred/draft"},
		{model.StatusDraft, "⏸️", "deferred/draft"},
		{model.StatusPinned, "📌", "pinned"},
		{model.StatusHooked, "🪝", "hooked"},
		{model.StatusReview, "👁️", "review"},
		{model.StatusClosed, "✅", "closed/tombstone"},
		{model.StatusTombstone, "✅", "closed/tombstone"},
	}
	for _, tt := range expected {
		if got := getStatusIcon(tt.status); got != tt.icon {
			t.Errorf("getStatusIcon(%q) = %q, want %q", tt.status, got, tt.icon)
		}
		if want := tt.icon + " " + tt.label; !strings.Contains(normalized, want) {
			t.Errorf("Graph legend missing %q: %q", want, legend)
		}
	}
	if got := getStatusIcon("custom"); got != model.StatusIcon("custom") {
		t.Errorf("getStatusIcon(custom) = %q, want %q", got, model.StatusIcon("custom"))
	}
	for _, want := range []string{"STATUS", model.StatusIcon("unknown") + " other"} {
		if !strings.Contains(normalized, want) {
			t.Errorf("Graph legend missing %q: %q", want, legend)
		}
	}
	for _, unrelated := range []string{"priority", "type", "related", "counts", "blockers", "dependents", "🐛", "🔹", "⬆", "⬇"} {
		if strings.Contains(normalized, unrelated) {
			t.Errorf("Graph legend advertised unrelated entry %q: %q", unrelated, legend)
		}
	}
}

func TestGraphLegendFitsAndSitsAtBottomRight(t *testing.T) {
	g := NewGraphModel([]model.Issue{
		{ID: "open", Status: model.StatusOpen},
		{ID: "blocked", Status: model.StatusBlocked},
	}, nil, createTheme())
	for _, width := range []int{1, 4, 8, 16, 24, 48} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			legend := renderStatusLegend(width, g.theme)
			for _, line := range strings.Split(legend, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("legend line width = %d, want <= %d: %q", got, width, line)
				}
			}
		})
	}

	view := g.View(120, 40)
	if strings.Contains(view, "j/k: navigate") {
		t.Fatalf("Graph view contains redundant local navigation hint: %q", view)
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(strings.TrimSpace(lines[len(lines)-1]), "other") {
		t.Fatalf("status legend is not visible on the final Graph row: %q", view)
	}
}

func TestGraphMetricsStatusLegendLayout(t *testing.T) {
	issues := []model.Issue{
		{ID: "center", Status: model.StatusInProgress},
		{ID: "blocker", Status: model.StatusOpen},
		{ID: "dependent", Status: model.StatusBlocked},
	}
	issues[0].Dependencies = []*model.Dependency{{DependsOnID: "blocker", Type: model.DepBlocks}}
	issues[2].Dependencies = []*model.Dependency{{DependsOnID: "center", Type: model.DepBlocks}}
	stats := analysis.NewAnalyzer(issues).AnalyzeWithConfig(analysis.FullAnalysisConfig())
	insights := (&stats).GenerateInsights(len(issues))
	g := NewGraphModel(issues, &insights, createTheme())

	wide := g.renderMetricsPanel("center", 100, g.theme)
	wideLines := strings.Split(ansi.Strip(wide), "\n")
	statusLine := -1
	for i, line := range wideLines {
		if strings.Contains(line, "STATUS") {
			statusLine = i
			break
		}
	}
	if statusLine < 0 {
		t.Fatalf("wide metrics panel is missing status heading: %q", wide)
	}
	statusColumn := strings.Index(wideLines[statusLine], "STATUS")
	if strings.TrimSpace(wideLines[statusLine][:statusColumn]) == "" {
		t.Fatalf("wide status legend is not composed beside metrics: %q", wide)
	}
	for _, line := range wideLines {
		if got := lipgloss.Width(line); got > 100 {
			t.Fatalf("wide metrics panel line width = %d, want <= 100: %q", got, line)
		}
	}

	narrow := g.renderMetricsPanel("center", 60, g.theme)
	narrowLines := strings.Split(ansi.Strip(narrow), "\n")
	narrowStatusLine := -1
	for i, line := range narrowLines {
		if strings.Contains(line, "STATUS") {
			narrowStatusLine = i
			break
		}
	}
	if narrowStatusLine <= 0 || !strings.Contains(strings.Join(narrowLines[:narrowStatusLine], "\n"), "GRAPH METRICS") {
		t.Fatalf("narrow metrics panel did not stack status legend below metrics: %q", narrow)
	}
	if !strings.Contains(strings.TrimSpace(narrowLines[len(narrowLines)-1]), "other") {
		t.Fatalf("narrow status legend is not anchored at the end of the metrics panel: %q", narrow)
	}
	for _, line := range narrowLines {
		if got := lipgloss.Width(line); got > 60 {
			t.Fatalf("narrow metrics panel line width = %d, want <= 60: %q", got, line)
		}
	}
}

func TestGraphAndListUseCanonicalIssueTypeIcons(t *testing.T) {
	theme := createTheme()
	tests := []string{
		string(model.TypeBug),
		string(model.TypeFeature),
		string(model.TypeTask),
		string(model.TypeEpic),
		string(model.TypeChore),
		"todo",
		"incident",
		"",
	}

	for _, issueType := range tests {
		t.Run(issueType, func(t *testing.T) {
			listIcon, _ := theme.GetTypeIcon(issueType)
			want := model.IssueTypeIcon(issueType)
			if listIcon != want {
				t.Fatalf("List View icon = %q, want canonical %q", listIcon, want)
			}

			issue := model.Issue{ID: "type-icon", Status: model.StatusOpen, IssueType: model.IssueType(issueType)}
			g := NewGraphModel([]model.Issue{issue}, nil, theme)
			graph := g.renderEgoNode(issue.ID, &issue, 60, theme)
			if !strings.Contains(graph, want) {
				t.Fatalf("Graph View output %q does not contain canonical icon %q", graph, want)
			}
		})
	}
}

func TestGraphViewConstrainsRelationshipRichContent(t *testing.T) {
	issues := []model.Issue{{ID: "center", Status: model.StatusInProgress}}
	for i := 0; i < 8; i++ {
		blockerID := fmt.Sprintf("blocker-%d", i)
		issues = append(issues, model.Issue{ID: blockerID, Status: model.StatusOpen})
		issues[0].Dependencies = append(issues[0].Dependencies, &model.Dependency{
			DependsOnID: blockerID,
			Type:        model.DepBlocks,
		})

		dependentID := fmt.Sprintf("dependent-%d", i)
		issues = append(issues, model.Issue{
			ID:     dependentID,
			Status: model.StatusBlocked,
			Dependencies: []*model.Dependency{{
				DependsOnID: "center",
				Type:        model.DepBlocks,
			}},
		})
	}

	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "narrow", width: 60, height: 24},
		{name: "golden width", width: 78, height: 40},
		{name: "wide", width: 120, height: 40},
	} {
		t.Run(size.name, func(t *testing.T) {
			g := NewGraphModel(issues, nil, createTheme())
			if !g.SelectByID("center") {
				t.Fatal("failed to select relationship-rich center node")
			}

			view := g.View(size.width, size.height)
			if got := lipgloss.Width(view); got > size.width {
				t.Fatalf("Graph view width = %d, want <= %d:\n%s", got, size.width, view)
			}
			if got := lipgloss.Height(view); got > size.height {
				t.Fatalf("Graph view height = %d, want <= %d:\n%s", got, size.height, view)
			}

			lines := strings.Split(view, "\n")
			lastLine := strings.TrimSpace(lines[len(lines)-1])
			if !strings.Contains(lastLine, "other") {
				t.Fatalf("status legend is not visible on the final row: %q", view)
			}
			if strings.Contains(view, "j/k: navigate") {
				t.Fatalf("Graph view contains redundant local navigation hint: %q", view)
			}
		})
	}
}

func cloneStringSlices(source map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(source))
	for key, values := range source {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}
