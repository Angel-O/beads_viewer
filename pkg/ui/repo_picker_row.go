package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderRepoPickerRow allocates space to the name only after reserving the
// fixed row metadata, then applies marker styling to the already bounded row.
func renderRepoPickerRow(theme Theme, nameStyle lipgloss.Style, contentWidth int, prefix, check, name, marker, count string) string {
	fixed := prefix + check + " " + marker + count
	nameWidth := max(0, contentWidth-lipgloss.Width(fixed))
	plainLine := prefix + check + " " + truncateRunesHelper(name, nameWidth, "...") + marker + count
	if lipgloss.Width(plainLine) > contentWidth {
		plainLine = truncateRunesHelper(fixed, contentWidth, "...")
	}
	if marker != "" {
		if markerIndex := strings.LastIndex(plainLine, marker); markerIndex >= 0 {
			markerStyle := theme.Renderer.NewStyle().Bold(true)
			plainLine = plainLine[:markerIndex] + markerStyle.Render(marker) + plainLine[markerIndex+len(marker):]
		}
	}
	return nameStyle.Render(plainLine)
}
