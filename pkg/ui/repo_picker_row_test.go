package ui

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

var repoPickerSGRPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func newANSIRepoPickerModel(catalog repositorypkg.Catalog) (RepoPickerModel, Theme) {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI)
	theme := DefaultTheme(renderer)
	return NewRepoPickerModel(catalog, theme), theme
}

func repoPickerRenderedLine(view, plainContent string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), plainContent) {
			return line
		}
	}
	return ""
}

func repoPickerLineHasUnderline(line string) bool {
	for _, sequence := range repoPickerSGRPattern.FindAllString(line, -1) {
		parameters := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
		for _, parameter := range strings.Split(parameters, ";") {
			if parameter == "4" {
				return true
			}
		}
	}
	return false
}

func TestRepoPickerCurrentNameIsAccentedAndUnderlinedWithoutChangingPlainText(t *testing.T) {
	m, theme := newANSIRepoPickerModel(testRepositoryCatalog())
	m.SetHubScope(hub.NewAllItemsHubScope())
	m.SetCurrentRepository("ctx:beta-456")
	m.SetSize(120, 24)

	view := m.View()
	currentLine := repoPickerRenderedLine(view, "beta (3)")
	nonCurrentLine := repoPickerRenderedLine(view, "gamma (0)")
	if currentLine == "" || nonCurrentLine == "" {
		t.Fatalf("picker rows missing from view:\n%s", ansi.Strip(view))
	}
	wantCurrentName := theme.Renderer.NewStyle().Foreground(theme.Primary).Underline(true).Render("beta")
	if !strings.Contains(currentLine, wantCurrentName) {
		t.Fatalf("current repository name was not accented and underlined: %q", currentLine)
	}
	if repoPickerLineHasUnderline(nonCurrentLine) {
		t.Fatalf("non-current repository row unexpectedly contains underline styling: %q", nonCurrentLine)
	}
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "beta (3)") || strings.Contains(plain, " current (") {
		t.Fatalf("plain picker content changed: %q", plain)
	}
}

func TestRepoPickerCurrentNameTruncatesBeforeStyling(t *testing.T) {
	m, theme := newANSIRepoPickerModel(repositorypkg.Catalog{{
		ID: "ctx:long", Name: ".", BeadCount: 42,
	}})
	scope, err := hub.NewSelectedContextsHubScope([]string{"ctx:long"})
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
		{name: "fixed fields consume content", width: 21, wantRow: "▸ [x]  (42)", contentWidth: 11},
		{name: "one-cell name fits", width: 22, wantRow: "▸ [x] . (42)", contentWidth: 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.SetSize(tc.width, 10)
			view := m.View()
			plainView := ansi.Strip(view)
			var plainRow string
			for _, line := range strings.Split(plainView, "\n") {
				if strings.Contains(line, "(42)") {
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
			wantStyledName := theme.Renderer.NewStyle().Foreground(theme.Primary).Underline(true).Render(".")
			if tc.width == 21 && strings.Contains(view, wantStyledName) {
				t.Fatalf("current styling survived after the name was omitted: %q", view)
			}
			if tc.width == 22 && !strings.Contains(view, wantStyledName) {
				t.Fatalf("current repository name lost accent and underline styling: %q", view)
			}
		})
	}
}

func TestRepoPickerContextlessRowPreservesCountAtNarrowWidth(t *testing.T) {
	m := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	m.SetHubScope(hub.NewAllItemsHubScope())
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
