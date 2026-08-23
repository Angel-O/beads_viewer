package ui

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
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
