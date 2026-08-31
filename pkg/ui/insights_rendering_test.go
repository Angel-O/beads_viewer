package ui

import (
	"io"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestInsightsPriorityListOuterGeometry(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	theme := DefaultTheme(renderer)
	insights := analysis.Insights{
		Stats: analysis.NewGraphStatsForTest(
			nil, nil, nil, nil, nil, map[string]float64{"issue-1": 1}, nil, nil, nil, 0, nil,
		),
	}
	picks := []analysis.TopPick{{ID: "issue-1", Title: "Issue", Score: 0.4}}
	m := NewInsightsModel(insights, map[string]*model.Issue{
		"issue-1": {ID: "issue-1", Title: "Issue"},
	}, theme)
	m.SetTopPicks(picks)
	m.SetRecommendations(nil, "hash")
	m.SetSize(150, 61)
	m.focusedPanel = PanelPriority

	listView := m.View()
	listTop, detailStart, ok := splitPanelBounds(listView)
	if !ok {
		t.Fatalf("could not find split panel bounds in list view")
	}
	listLines := strings.Split(strings.TrimRight(ansi.Strip(listView), "\n"), "\n")
	if len(listLines) != 61 || listTop != 45 {
		t.Fatalf("list geometry = height %d, Priority top %d; want height 61, top 45", len(listLines), listTop)
	}
	priorityCardTop := firstCornerRowAt(listLines, listTop+1, 0, detailStart, '╭')
	detailViewportTop := firstCornerRowAt(listLines, 1, detailStart+2, -1, '╭')
	priorityCardBottom := firstCornerRowAt(listLines, priorityCardTop, 0, detailStart, '╰')
	detailViewportBottom := firstCornerRowAt(listLines, detailViewportTop, detailStart+2, -1, '╰')
	if priorityCardTop != 47 || detailViewportTop != 1 {
		t.Fatalf("inner tops = Priority %d, detail %d; want 47, 1", priorityCardTop, detailViewportTop)
	}
	if priorityCardBottom != 58 || detailViewportBottom != 58 {
		t.Fatalf("inner bottoms = Priority %d, detail %d; want 58, 58", priorityCardBottom, detailViewportBottom)
	}
	assertSplitPanelGeometry(t, listView, listTop, detailStart)
	m.ToggleHeatmap()
	heatmapView := m.View()
	heatmapTop, heatmapDetailStart, ok := splitPanelBounds(heatmapView)
	if !ok {
		t.Fatalf("could not find split panel bounds in heatmap view")
	}
	if got, want := heatmapTop, listTop; got != want {
		t.Fatalf("heatmap row-4 top = %d, want list top %d", got, want)
	}
	if got, want := heatmapDetailStart, detailStart; got != want {
		t.Fatalf("heatmap detail column = %d, want list detail column %d", got, want)
	}
	if got, want := lipgloss.Height(heatmapView), lipgloss.Height(listView); got != want {
		t.Fatalf("heatmap toggle changed rendered height from %d to %d", want, got)
	}
	assertSplitPanelGeometry(t, heatmapView, heatmapTop, heatmapDetailStart)
	heatmapLines := strings.Split(strings.TrimRight(ansi.Strip(heatmapView), "\n"), "\n")
	if got := firstCornerRowAt(heatmapLines, 1, heatmapDetailStart+2, -1, '╭'); got != 1 {
		t.Fatalf("heatmap detail viewport top = %d, want 1", got)
	}
	if got := firstCornerRowAt(heatmapLines, 1, heatmapDetailStart+2, -1, '╰'); got != 58 {
		t.Fatalf("heatmap detail viewport bottom = %d, want 58", got)
	}
}

func splitPanelBounds(rendered string) (int, int, bool) {
	lines := strings.Split(strings.TrimRight(ansi.Strip(rendered), "\n"), "\n")
	positions := cornerPositions(lines[0], '╭')
	if len(positions) == 0 {
		return 0, 0, false
	}
	detailStart := positions[len(positions)-1]

	for row, line := range lines[1:] {
		positions := cornerPositions(line, '╭')
		if len(positions) == 1 && positions[0] == 0 {
			return row + 1, detailStart, true
		}
	}
	return 0, 0, false
}

func assertSplitPanelGeometry(t *testing.T, rendered string, priorityTop, detailStart int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(ansi.Strip(rendered), "\n"), "\n")
	if got := firstCornerRowAt(lines, 0, detailStart, -1, '╭'); got != 0 {
		t.Fatalf("detail outer border starts on row %d, want 0", got)
	}
	if got := firstCornerRowAt(lines, priorityTop, 0, detailStart, '╭'); got != priorityTop {
		t.Fatalf("priority outer border starts on row %d, want %d", got, priorityTop)
	}

	last := []rune(lines[len(lines)-1])
	if !hasCornerAt(string(last), 0, '╰') || !hasCornerAt(string(last), detailStart, '╰') {
		t.Fatalf("priority and detail bottom borders are not aligned: %q", lines[len(lines)-1])
	}
}

func firstCornerRowAt(lines []string, startRow, minColumn, maxColumn int, corner rune) int {
	if startRow < 0 {
		return -1
	}
	for row := startRow; row < len(lines); row++ {
		for _, column := range cornerPositions(lines[row], corner) {
			if column >= minColumn && (maxColumn < 0 || column < maxColumn) {
				return row
			}
		}
	}
	return -1
}

func cornerPositions(line string, corner rune) []int {
	positions := make([]int, 0, 2)
	runes := []rune(line)
	for column, char := range runes {
		if char == corner {
			positions = append(positions, lipgloss.Width(string(runes[:column])))
		}
	}
	return positions
}

func hasCornerAt(line string, column int, corner rune) bool {
	for _, position := range cornerPositions(line, corner) {
		if position == column {
			return true
		}
	}
	return false
}

func TestRenderHeatmapCellSelectedPopulatedUsesContrast(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	renderer.SetHasDarkBackground(true)
	theme := DefaultTheme(renderer)

	selected := (&InsightsModel{}).renderHeatmapCell(2, 5, 8, true, theme)
	if strings.Contains(selected, "\x1b[7m") {
		t.Fatal("selected populated heatmap cell uses reverse video")
	}
	if !strings.Contains(selected, "48;2;189;147;249") {
		t.Fatalf("selected populated heatmap cell lacks explicit selection background: %q", selected)
	}
}
