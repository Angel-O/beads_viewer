package ui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestRepoPickerCurrentMarkerIsBoldWithoutChangingPlainText(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(renderer))
	m.SetHubScope(model.NewAllItemsHubScope())
	m.SetCurrentRepository("ctx:beta-456")
	m.SetSize(120, 24)

	view := m.View()
	var currentLine, nonCurrentLine string
	for _, line := range strings.Split(view, "\n") {
		plain := ansi.Strip(line)
		switch {
		case strings.Contains(plain, "beta current (3)"):
			currentLine = line
		case strings.Contains(plain, "gamma (0)"):
			nonCurrentLine = line
		}
	}
	if currentLine == "" || nonCurrentLine == "" {
		t.Fatalf("picker rows missing from view:\n%s", ansi.Strip(view))
	}
	if !strings.Contains(currentLine, "\x1b[1m current\x1b[0m") {
		t.Fatalf("current marker was not rendered bold: %q", currentLine)
	}
	if strings.Contains(nonCurrentLine, "\x1b[1m") {
		t.Fatalf("non-current repository row unexpectedly contains bold styling: %q", nonCurrentLine)
	}
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "beta current (3)") || strings.Contains(plain, "*") {
		t.Fatalf("plain picker content changed: %q", plain)
	}
}

func TestRepoPickerCurrentMarkerTruncatesPlainRowBeforeStyling(t *testing.T) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)
	m := NewRepoPickerModel(model.RepositoryCatalog{{
		ID: "ctx:long", Name: ".", BeadCount: 42,
	}}, DefaultTheme(renderer))
	scope, err := model.NewSelectedContextsHubScope([]string{"ctx:long"})
	if err != nil {
		t.Fatal(err)
	}
	m.SetHubScope(scope)
	m.SetCurrentRepository("ctx:long")
	m.MoveDown()

	for _, tc := range []struct {
		name         string
		width        int
		wantRow      string
		contentWidth int
	}{
		{name: "fixed fields consume content", width: 29, wantRow: "▸ [x]  current (42)", contentWidth: 19},
		{name: "one-cell name fits", width: 30, wantRow: "▸ [x] . current (42)", contentWidth: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.SetSize(tc.width, 10)
			view := m.View()
			plainView := ansi.Strip(view)
			var plainRow string
			for _, line := range strings.Split(plainView, "\n") {
				if strings.Contains(line, "current (42)") {
					plainRow = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│"))
					break
				}
			}
			if plainRow != tc.wantRow {
				t.Fatalf("ANSI-stripped current row = %q, want %q; full view:\n%s", plainRow, tc.wantRow, plainView)
			}
			if lipgloss.Width(plainRow) > tc.contentWidth {
				t.Fatalf("current row width = %d, want <= %d: %q", lipgloss.Width(plainRow), tc.contentWidth, plainRow)
			}
			if strings.Contains(plainRow, "...") || strings.Contains(plainRow, "…") {
				t.Fatalf("current row gained unintended ellipsis: %q", plainRow)
			}
			if !strings.Contains(view, "\x1b[1m current\x1b[0m") {
				t.Fatalf("current marker lost bold styling: %q", view)
			}
		})
	}
}

func TestRepoPickerContextlessRowPreservesCountAtNarrowWidth(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetHubScope(model.NewAllItemsHubScope())
	m.SetSize(20, 8)

	view := m.View()
	var row string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "(0)") {
			row = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│"))
			break
		}
	}
	if want := "▸ [x]  (0)"; row != want {
		t.Fatalf("narrow contextless row = %q, want %q; full view:\n%s", row, want, view)
	}
	if strings.Contains(row, "...") || lipgloss.Width(view) > 20 {
		t.Fatalf("narrow contextless row lost fixed fields or overflowed: %q", row)
	}
}

func TestRepoPickerUltraCompactWidth14StaysBounded(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetSize(14, 8)
	if out := m.View(); lipgloss.Width(out) > 14 || lipgloss.Height(out) > 8 {
		t.Fatalf("picker at 14x8 rendered %dx%d:\n%s", lipgloss.Width(out), lipgloss.Height(out), out)
	}
}
