package ui

import (
	"strings"
	"unicode"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	statusLegendWidth        = 22
	statusLegendColumnGap    = 3
	statusLegendTreeMinWidth = statusLegendWidth + statusLegendColumnGap + 40
)

type statusLegendGroup struct {
	label    string
	statuses []model.Status
}

func statusLegendGroups() []statusLegendGroup {
	return []statusLegendGroup{
		{label: "open", statuses: []model.Status{model.StatusOpen}},
		{label: "in progress", statuses: []model.Status{model.StatusInProgress}},
		{label: "blocked", statuses: []model.Status{model.StatusBlocked}},
		{label: "deferred/draft", statuses: []model.Status{model.StatusDeferred, model.StatusDraft}},
		{label: "pinned", statuses: []model.Status{model.StatusPinned}},
		{label: "hooked", statuses: []model.Status{model.StatusHooked}},
		{label: "review", statuses: []model.Status{model.StatusReview}},
		{label: "closed/tombstone", statuses: []model.Status{model.StatusClosed, model.StatusTombstone}},
	}
}

func statusLegendEntries() []string {
	groups := statusLegendGroups()
	entries := make([]string, 0, len(groups)+1)
	for _, group := range groups {
		entries = append(entries, model.StatusIcon(string(group.statuses[0]))+" "+group.label)
	}
	entries = append(entries, model.StatusIcon("unknown")+" other")
	return entries
}

func renderStatusLegend(width int, t Theme) string {
	return renderStatusLegendLines(statusLegendLines(statusLegendEntries(), width, false), width, t)
}

func renderCompactStatusLegend(width int, t Theme) string {
	return renderStatusLegendLines(statusLegendLines(statusLegendEntries(), width, true), width, t)
}

func statusLegendLines(entries []string, width int, compact bool) []string {
	if width <= 0 || len(entries) == 0 {
		return nil
	}
	if !compact {
		lines := []string{"STATUS"}
		for _, entry := range entries {
			lines = append(lines, wrapStatusLegendLine(entry, width)...)
		}
		return lines
	}

	lines := []string{"STATUS"}
	current := ""
	for _, entry := range entries {
		for _, wrapped := range wrapStatusLegendLine(entry, width) {
			if current == "" {
				current = wrapped
				continue
			}
			candidate := current + "  " + wrapped
			if lipgloss.Width(candidate) <= width {
				current = candidate
				continue
			}
			lines = append(lines, current)
			current = wrapped
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func renderStatusLegendLines(lines []string, width int, t Theme) string {
	if width <= 0 || len(lines) == 0 {
		return ""
	}
	style := t.Renderer.NewStyle().Foreground(ColorMuted).Width(width).Align(lipgloss.Right)
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		lineStyle := style
		if len(rendered) == 0 {
			lineStyle = lineStyle.Bold(true)
		}
		rendered = append(rendered, ansi.Truncate(lineStyle.Render(line), width, ""))
	}
	return strings.Join(rendered, "\n")
}

func truncateStatusLegendHeight(legend string, maxHeight int) string {
	if maxHeight <= 0 || legend == "" {
		return ""
	}
	lines := strings.Split(legend, "\n")
	if len(lines) <= maxHeight {
		return legend
	}
	if maxHeight == 1 {
		return lines[0]
	}
	return strings.Join(append(lines[:maxHeight-1], lines[len(lines)-1]), "\n")
}

func wrapStatusLegendLine(line string, width int) []string {
	words := make([]string, 0)
	for _, word := range strings.Fields(line) {
		words = append(words, splitStatusLegendToken(word, width)...)
	}
	if len(words) == 0 || width <= 0 {
		return nil
	}

	lines := make([]string, 0, 2)
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
	}
	lines = append(lines, current)
	return lines
}

func splitStatusLegendToken(token string, width int) []string {
	if width <= 0 {
		return nil
	}
	if lipgloss.Width(token) <= width {
		return []string{token}
	}

	var chunks []string
	current := ""
	for _, r := range token {
		part := string(r)
		if unicode.Is(unicode.Mn, r) {
			if current != "" && lipgloss.Width(current+part) <= width {
				current += part
			}
			continue
		}
		if lipgloss.Width(part) > width {
			if current != "" {
				chunks = append(chunks, current)
				current = ""
			}
			chunks = append(chunks, strings.Repeat("?", width))
			continue
		}
		candidate := current + part
		if current != "" && lipgloss.Width(candidate) > width {
			chunks = append(chunks, current)
			current = part
		} else {
			current = candidate
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}
