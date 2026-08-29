package ui

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/lipgloss"
)

func TestStatusLegendEntriesAreExhaustiveAndCanonical(t *testing.T) {
	want := []string{
		model.StatusIcon(string(model.StatusOpen)) + " open",
		model.StatusIcon(string(model.StatusInProgress)) + " in progress",
		model.StatusIcon(string(model.StatusBlocked)) + " blocked",
		model.StatusIcon(string(model.StatusDeferred)) + " deferred/draft",
		model.StatusIcon(string(model.StatusPinned)) + " pinned",
		model.StatusIcon(string(model.StatusHooked)) + " hooked",
		model.StatusIcon(string(model.StatusReview)) + " review",
		model.StatusIcon(string(model.StatusClosed)) + " closed/tombstone",
		model.StatusIcon("unknown") + " other",
	}

	got := statusLegendEntries()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("status legend entries = %#v, want %#v", got, want)
	}
	for _, group := range statusLegendGroups() {
		want := model.StatusIcon(string(group.statuses[0]))
		for _, status := range group.statuses[1:] {
			if got := model.StatusIcon(string(status)); got != want {
				t.Errorf("grouped status %q = %q, want %q", status, got, want)
			}
		}
	}
}

func TestStatusLegendRenderingFitsWidth(t *testing.T) {
	for _, width := range []int{1, 8, 22, 40} {
		legend := renderStatusLegend(width, createTheme())
		for _, line := range strings.Split(legend, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d rendered legend line at %d: %q", width, got, line)
			}
		}
	}
}

func TestGraphAndTreeUseSharedStatusLegendEntries(t *testing.T) {
	theme := createTheme()
	graph := NewGraphModel([]model.Issue{{ID: "root", Status: model.StatusOpen}}, nil, theme)
	graphMetrics := graph.renderMetricsPanel("root", 100, theme)

	tree := NewTreeModel(theme)
	tree.Build([]model.Issue{{ID: "root", Status: model.StatusOpen}})
	tree.SetSize(100, 20)
	treeView := tree.View()

	for _, entry := range statusLegendEntries() {
		if !strings.Contains(graphMetrics, entry) {
			t.Errorf("Graph legend missing shared entry %q", entry)
		}
		if !strings.Contains(treeView, entry) {
			t.Errorf("Tree legend missing shared entry %q", entry)
		}
	}
}
