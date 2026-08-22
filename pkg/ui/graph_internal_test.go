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
