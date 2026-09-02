package ui

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestBuildDetailMarkdownShowsExternalRef verifies the detail panel surfaces a
// populated external_ref (issue #172), mirroring `br show`'s "Ref:" line.
func TestBuildDetailMarkdownShowsExternalRef(t *testing.T) {
	ref := "docs/specs/foo.md"
	issues := []model.Issue{
		{ID: "bv-1", Title: "With ref", Status: model.StatusOpen, ExternalRef: &ref},
		{ID: "bv-2", Title: "No ref", Status: model.StatusOpen},
		{ID: "bv-3", Title: "Empty ref", Status: model.StatusOpen, ExternalRef: new(string)},
	}
	issueMap := make(map[string]*model.Issue, len(issues))
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	m := NewInsightsModel(analysis.Insights{}, issueMap, Theme{})

	withRef := m.buildDetailMarkdown("bv-1")
	if !strings.Contains(withRef, "External Ref") || !strings.Contains(withRef, ref) {
		t.Fatalf("detail for bv-1 should show external ref %q; got:\n%s", ref, withRef)
	}

	// Absent and empty-string refs must not render the row.
	if got := m.buildDetailMarkdown("bv-2"); strings.Contains(got, "External Ref") {
		t.Fatalf("detail for bv-2 (nil ref) should not show External Ref; got:\n%s", got)
	}
	if got := m.buildDetailMarkdown("bv-3"); strings.Contains(got, "External Ref") {
		t.Fatalf("detail for bv-3 (empty ref) should not show External Ref; got:\n%s", got)
	}
}

func TestInsightsDetailProjectsRelationshipsToActiveScope(t *testing.T) {
	issues := []model.Issue{
		{ID: "selected", Title: "Selected", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "active-dependency", Type: model.DepBlocks},
			{DependsOnID: "closed-dependency", Type: model.DepBlocks},
			{DependsOnID: "excluded-dependency", Type: model.DepBlocks},
		}},
		{ID: "active-dependency", Title: "Active dependency", Status: model.StatusOpen},
		{ID: "closed-dependency", Title: "Closed dependency", Status: model.StatusClosed},
		{ID: "excluded-dependency", Title: "Excluded dependency", Status: model.StatusOpen},
		{ID: "active-dependent", Title: "Active dependent", Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "selected", Type: model.DepBlocks}}},
		{ID: "closed-dependent", Title: "Closed dependent", Status: model.StatusClosed, Dependencies: []*model.Dependency{{DependsOnID: "selected", Type: model.DepBlocks}}},
		{ID: "excluded-dependent", Title: "Excluded dependent", Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "selected", Type: model.DepBlocks}}},
	}
	issueMap := make(map[string]*model.Issue, len(issues))
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}
	stats := analysis.NewGraphStatsForTest(
		nil, nil, nil, nil, nil,
		map[string]float64{"selected": 3, "active-dependency": 2, "closed-dependency": 9},
		nil, nil, nil, 0, nil,
	)
	m := NewInsightsModel(analysis.Insights{Stats: stats}, issueMap, Theme{})
	m.SetActiveIssueIDs(map[string]bool{
		"selected": true, "active-dependency": true, "active-dependent": true,
	})
	m.insights.Stats = stats
	m.focusedPanel = PanelBottlenecks

	for _, content := range []string{m.buildDetailMarkdown("selected"), m.renderCalculationProofMD("selected")} {
		if !strings.Contains(content, "Active dependency") || !strings.Contains(content, "Active dependent") {
			t.Fatalf("active relationships missing from detail:\n%s", content)
		}
		for _, disallowed := range []string{"Closed dependency", "Excluded dependency", "Closed dependent", "Excluded dependent"} {
			if strings.Contains(content, disallowed) {
				t.Fatalf("detail leaked %q:\n%s", disallowed, content)
			}
		}
	}
	if got := m.buildDetailMarkdown("selected"); !strings.Contains(got, "### Dependencies (1)") {
		t.Fatalf("detail relationship count was not projected:\n%s", got)
	}

	m.focusedPanel = PanelKeystones
	chain := m.renderCalculationProofMD("selected")
	if !strings.Contains(chain, "Active dependency") || strings.Contains(chain, "Closed dependency") {
		t.Fatalf("impact chain was not projected:\n%s", chain)
	}
}

func TestIssueDetailSectionOrder(t *testing.T) {
	issues := []model.Issue{
		{
			ID:                 "selected",
			Title:              "Selected",
			Status:             model.StatusOpen,
			Description:        "Human description",
			Design:             "Human design",
			AcceptanceCriteria: "Human acceptance",
			Notes:              "Human notes",
			Labels:             []string{"backend"},
			Dependencies:       []*model.Dependency{{DependsOnID: "dependency", Type: model.DepBlocks}},
			Comments:           []*model.Comment{{Author: "author", Text: "Human comment"}},
		},
		{ID: "dependency", Title: "Dependency", Status: model.StatusOpen},
	}

	m := NewModel(issues, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 200})
	m = updated.(Model)
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.historyView.SetReport(&correlation.HistoryReport{
		Histories: map[string]correlation.BeadHistory{
			"selected": {
				BeadID:  "selected",
				Commits: []correlation.CorrelatedCommit{{ShortSHA: "history-commit", Message: "history message"}},
			},
		},
	})
	m.list.SetItems([]list.Item{IssueItem{
		Issue:           *m.issueMap["selected"],
		TriageScore:     0.8,
		TriageReason:    "Agent reason",
		SearchScore:     0.7,
		SearchTextScore: 0.6,
		SearchScoreSet:  true,
		SearchComponents: map[string]float64{
			"pagerank": 0.5,
		},
	}})
	m.list.SetFilterText("")
	m.list.Select(0)
	m.semanticSearchEnabled = true
	m.semanticHybridEnabled = true
	m.updateViewportContent()

	content := ansi.Strip(m.viewport.View())
	orderedSections := []string{
		"ID",
		"Context:",
		"Labels:",
		"Description",
		"Comments",
		"Design Notes",
		"Acceptance Criteria",
		"Notes",
		"Relationships",
		"Dependency Graph:",
		"📜 History",
		"🎯 Triage Insights",
		"🔎 Search Scores",
		"Graph Analysis",
	}
	previous := -1
	for _, section := range orderedSections {
		index := detailSectionOffset(content, section)
		if index < 0 {
			t.Fatalf("detail is missing %q:\n%s", section, content)
		}
		if index <= previous {
			t.Fatalf("detail section %q rendered out of order:\n%s", section, content)
		}
		previous = index
	}
}

func TestIssueDetailSectionOrderOmitsOptionalSections(t *testing.T) {
	m := NewModel([]model.Issue{{
		ID:                 "sparse",
		Title:              "Sparse",
		Status:             model.StatusOpen,
		Design:             "Design without a description",
		AcceptanceCriteria: "Acceptance without a description",
		Notes:              "Notes without a description",
		Comments:           []*model.Comment{{Author: "author", Text: "Comment without a description"}},
	}}, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 32, Height: 200})
	m = updated.(Model)
	if m.renderer.width != m.viewport.Width {
		t.Fatalf("narrow detail renderer width = %d, viewport width = %d", m.renderer.width, m.viewport.Width)
	}
	m.list.SetItems([]list.Item{IssueItem{Issue: m.issues[0]}})
	m.list.Select(0)
	m.updateViewportContent()

	content := ansi.Strip(m.viewport.View())
	if strings.Contains(content, "Description") {
		t.Fatalf("sparse detail rendered an absent description section:\n%s", content)
	}
	previous := detailSectionOffset(content, "Comments")
	for _, section := range []string{"Design Notes", "Acceptance Criteria", "Notes"} {
		index := detailSectionOffset(content, section)
		if previous < 0 || index < 0 || index <= previous {
			t.Fatalf("sparse detail sections are missing or out of order:\n%s", content)
		}
		previous = index
	}
	if strings.Contains(content, "Relationships") {
		t.Fatalf("sparse detail rendered an absent relationship section:\n%s", content)
	}
	if !strings.Contains(content, "Graph Analysis") {
		t.Fatalf("sparse detail omitted the always-rendered graph section:\n%s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		if width := lipgloss.Width(line); width > m.viewport.Width {
			t.Fatalf("narrow detail line width = %d, want <= %d: %q", width, m.viewport.Width, line)
		}
	}
}

func detailSectionOffset(content, section string) int {
	offset := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), section) {
			return offset
		}
		offset += len(line)
	}
	return -1
}
