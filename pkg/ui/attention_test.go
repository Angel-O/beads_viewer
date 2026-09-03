package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func TestComputeAttentionView_Empty(t *testing.T) {
	out, err := ComputeAttentionView(nil, 80)
	if err != nil {
		t.Fatalf("ComputeAttentionView error: %v", err)
	}
	if out != "No labels available for Attention analysis" {
		t.Fatalf("unexpected empty state: %q", out)
	}
}

func TestComputeAttentionView_IncludesContextLabelsByDefault(t *testing.T) {
	out, err := ComputeAttentionView([]model.Issue{{
		ID:     "context",
		Status: model.StatusOpen,
		Labels: []string{"ctx:project"},
	}}, 80)
	if err != nil {
		t.Fatalf("ComputeAttentionView error: %v", err)
	}
	if !strings.Contains(out, "ctx:project") {
		t.Fatalf("generic attention view omitted context label: %q", out)
	}
}

func TestComputeAttentionView_RespectsWidthWhenWideEnough(t *testing.T) {
	const width = 80
	out, err := ComputeAttentionView([]model.Issue{{
		ID:     "A",
		Status: model.StatusOpen,
		Labels: []string{"backend"},
	}}, width)
	if err != nil {
		t.Fatalf("ComputeAttentionView error: %v", err)
	}

	line := strings.SplitN(out, "\n", 2)[0]
	if got := runewidth.StringWidth(line); got != width {
		t.Fatalf("expected header width %d, got %d:\n%q", width, got, line)
	}
}

func TestComputeAttentionView_SingleLabelFormatting(t *testing.T) {
	now := time.Now().UTC()
	issues := []model.Issue{{
		ID:        "A",
		Title:     "A",
		Status:    model.StatusOpen,
		IssueType: model.TypeTask,
		Priority:  2,
		Labels:    []string{"backend"},
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
	}}

	out, err := ComputeAttentionView(issues, 80)
	if err != nil {
		t.Fatalf("ComputeAttentionView error: %v", err)
	}
	if !strings.Contains(out, "backend") {
		t.Fatalf("expected label in output, got:\n%s", out)
	}
	if !strings.Contains(out, "1.00") {
		t.Fatalf("expected attention score with 2 decimals, got:\n%s", out)
	}
	if !strings.Contains(out, "blocked=0 stale=0 vel=1.0") {
		t.Fatalf("expected reason fields, got:\n%s", out)
	}
}

func TestComputeAttentionView_LimitsToNineInteractiveRowsAndIsDeterministic(t *testing.T) {
	now := time.Now().UTC()

	// Create 11 distinct labels; with identical issue shape they tie on score and
	// should sort by label name (then truncated to the nine numeric choices).
	var issues []model.Issue
	for i := 1; i <= 11; i++ {
		label := "l" + pad2(i)
		issues = append(issues, model.Issue{ID: "ISSUE-" + label, Title: "Issue " + label, Status: model.StatusOpen, IssueType: model.TypeTask, Priority: 2, Labels: []string{label}, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-time.Hour)})
	}
	out, err := ComputeAttentionView(issues, 120)
	if err != nil {
		t.Fatalf("ComputeAttentionView error: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 10 || !strings.Contains(out, "l01") || !strings.Contains(out, "l09") || strings.Contains(out, "l10") || strings.Contains(out, "l11") {
		t.Fatalf("unexpected compatibility rows:\n%s", out)
	}
}

func attentionFixture(n int) analysis.LabelAttentionResult {
	now := time.Now().UTC()
	var issues []model.Issue
	for i := 1; i <= n; i++ {
		label := "l" + pad2(i)
		issues = append(issues, model.Issue{
			ID:        "ISSUE-" + label,
			Title:     "Issue " + label,
			Status:    model.StatusOpen,
			IssueType: model.TypeTask,
			Priority:  2,
			Labels:    []string{label},
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now.Add(-1 * time.Hour),
		})
	}
	return analysis.ComputeLabelAttentionScores(issues, analysis.DefaultLabelHealthConfig(), now)
}

func TestAttentionModel_EmptyRendersHeaderOnly(t *testing.T) {
	m := NewAttentionModel(DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(80, 10)
	out := m.View()
	if !strings.Contains(out, "Rank") || !strings.Contains(out, "Label") || !strings.Contains(out, "Attention") || !strings.Contains(out, "Reason") {
		t.Fatalf("expected header columns, got:\n%s", out)
	}
	if lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n"); len(lines) != 1 {
		t.Fatalf("expected header only, got %d lines:\n%s", len(lines), out)
	}
	if m.SelectedLabel() != "" || m.Len() != 0 {
		t.Fatalf("empty model must have no selection")
	}
	if label, handled := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); label != "" || !handled {
		t.Fatalf("enter on empty view: label=%q handled=%v", label, handled)
	}
}

func TestAttentionModel_HeaderRespectsWidth(t *testing.T) {
	m := NewAttentionModel(DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(80, 10)
	line := strings.Split(m.View(), "\n")[0]
	if got := runewidth.StringWidth(line); got != 80 {
		t.Fatalf("expected header width 80, got %d: %q", got, line)
	}
}

func TestAttentionModel_LimitsToTop10AndIsDeterministic(t *testing.T) {
	m := NewAttentionModel(DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(120, 20)
	m.SetData(attentionFixture(11))
	if m.Len() != 10 {
		t.Fatalf("expected top 10 labels, got %d", m.Len())
	}
	out := m.View()
	if !strings.Contains(out, "l01") || !strings.Contains(out, "l10") || strings.Contains(out, "l11") {
		t.Fatalf("expected l01..l10 and not l11:\n%s", out)
	}
	if !strings.Contains(out, "pr=") || !strings.Contains(out, "closed30=") {
		t.Fatalf("reason column must show the formula components:\n%s", out)
	}
	again := NewAttentionModel(DefaultTheme(lipgloss.NewRenderer(nil)))
	again.SetSize(120, 20)
	again.SetData(attentionFixture(11))
	if again.View() != out {
		t.Fatal("render is not deterministic for the same input")
	}
}

func TestAttentionModel_NavigationAndEnter(t *testing.T) {
	m := NewAttentionModel(DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(100, 4) // header + 3 visible rows forces scrolling
	m.SetData(attentionFixture(6))

	key := func(s string) (string, bool) {
		if s == "enter" {
			return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	}
	if m.Cursor() != 0 {
		t.Fatalf("cursor starts at 0, got %d", m.Cursor())
	}
	key("j")
	key("j")
	if m.Cursor() != 2 {
		t.Fatalf("after j j cursor = %d, want 2", m.Cursor())
	}
	key("k")
	if m.Cursor() != 1 {
		t.Fatalf("after k cursor = %d, want 1", m.Cursor())
	}
	key("G")
	if m.Cursor() != 5 {
		t.Fatalf("after G cursor = %d, want 5", m.Cursor())
	}
	// The selected (last) row must be visible after scrolling.
	if !strings.Contains(m.View(), m.SelectedLabel()) {
		t.Fatalf("selected label %q not rendered after scroll:\n%s", m.SelectedLabel(), m.View())
	}
	key("g")
	if m.Cursor() != 0 {
		t.Fatalf("after g cursor = %d, want 0", m.Cursor())
	}
	label, handled := key("enter")
	if !handled || label != m.LabelAt(0) {
		t.Fatalf("enter must return the selected label: got %q handled=%v", label, handled)
	}
	if _, handled := key("x"); handled {
		t.Fatal("unknown keys must not be consumed")
	}
}

func TestAttentionModel_TruncatesCells(t *testing.T) {
	now := time.Now().UTC()
	longLabel := "this-is-a-very-very-long-label-name"
	issues := []model.Issue{{
		ID: "A", Title: "A", Status: model.StatusOpen, IssueType: model.TypeTask, Priority: 2,
		Labels: []string{longLabel}, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour),
	}}
	m := NewAttentionModel(DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(40, 5)
	m.SetData(analysis.ComputeLabelAttentionScores(issues, analysis.DefaultLabelHealthConfig(), now))
	out := m.View()
	if !strings.Contains(out, truncate(longLabel, 18)) {
		t.Fatalf("expected truncated label in output:\n%s", out)
	}
	if strings.Contains(out, longLabel) {
		t.Fatalf("label must be truncated to its column:\n%s", out)
	}
}

// Model integration: ] opens the attention overlay on Insights, Enter opens
// the label drilldown, 1-9 filter the list by rank, and ]/Esc return to the list.
func TestModel_AttentionViewFocusDrilldownAndFilter(t *testing.T) {
	now := time.Now().UTC()
	issues := []model.Issue{
		{ID: "A", Title: "Alpha", Status: model.StatusOpen, Priority: 1, Labels: []string{"backend"}, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
		{ID: "B", Title: "Beta", Status: model.StatusOpen, Priority: 2, Labels: []string{"frontend"}, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Hour)},
	}
	m := NewModel(issues, nil, "")
	m.width, m.height = 120, 30

	press := func(s string) {
		var msg tea.KeyMsg
		switch s {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		updated, _ := m.Update(msg)
		m = updated.(*Model)
	}

	press("]")
	if m.focused != focusInsights || !m.showAttentionView {
		t.Fatalf("] must open the attention overlay, got focus=%v shown=%v", m.focused, m.showAttentionView)
	}
	if m.attentionView.Len() != 2 {
		t.Fatalf("expected 2 ranked labels, got %d", m.attentionView.Len())
	}
	if !strings.Contains(m.View(), "Rank") {
		t.Fatalf("attention view not rendered:\n%s", m.View())
	}

	press("j")
	selected := m.attentionView.SelectedLabel()
	press("enter")
	if !m.showLabelDrilldown || m.labelDrilldownLabel != selected {
		t.Fatalf("enter must open the drilldown for %q, got show=%v label=%q", selected, m.showLabelDrilldown, m.labelDrilldownLabel)
	}
	m.showLabelDrilldown = false

	press("1")
	if !strings.HasPrefix(m.currentFilter, "label:") {
		t.Fatalf("1 must filter by the top label, got filter %q", m.currentFilter)
	}

	if m.focused != focusList {
		t.Fatalf("rank filter must return to the list, got %v", m.focused)
	}
}

func pad2(i int) string {
	return fmt.Sprintf("%02d", i)
}
