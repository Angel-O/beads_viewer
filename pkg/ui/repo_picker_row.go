package ui

import "github.com/charmbracelet/lipgloss"

// renderRepoPickerRow allocates space to the name only after reserving the
// fixed row metadata, then styles the already bounded current repository name.
func renderRepoPickerRow(theme Theme, nameStyle lipgloss.Style, contentWidth int, prefix, check, name, count string, current bool) string {
	fixed := prefix + check + " " + count
	nameWidth := max(0, contentWidth-lipgloss.Width(fixed))
	displayName := truncateRunesHelper(name, nameWidth, "...")
	plainLine := prefix + check + " " + displayName + count
	if lipgloss.Width(plainLine) > contentWidth {
		return nameStyle.Render(truncateRunesHelper(fixed, contentWidth, "..."))
	}
	if current && displayName != "" {
		currentStyle := theme.Renderer.NewStyle().Foreground(theme.Primary).Underline(true)
		plainLine = prefix + check + " " + currentStyle.Render(displayName) + count
	}
	return nameStyle.Render(plainLine)
}
