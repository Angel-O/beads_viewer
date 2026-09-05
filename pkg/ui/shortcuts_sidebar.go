package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ShortcutsSidebar provides a toggleable panel showing context-aware keyboard shortcuts
// Unlike the help overlay, this can remain visible while working (bv-3qi5)
type ShortcutsSidebar struct {
	width        int
	height       int
	scrollOffset int
	theme        Theme
	context      string       // Current context for filtering shortcuts
	keyRegistry  *KeyRegistry // Registry for auto-generated bindings (bv-xl6g)
	focusHint    focus        // Current focus for registry lookup (bv-xl6g)
	splitView    bool         // Whether List focus is the left pane of Split view
}

// shortcutItem represents a single keyboard shortcut
type shortcutItem struct {
	key  string
	desc string
}

// shortcutSection groups shortcuts by category
type shortcutSection struct {
	title    string
	items    []shortcutItem
	contexts []string // Which contexts this section applies to (empty = all)
}

// NewShortcutsSidebar creates a new shortcuts sidebar
func NewShortcutsSidebar(theme Theme) ShortcutsSidebar {
	return ShortcutsSidebar{
		theme:   theme,
		width:   34, // Fixed width for sidebar (increased for readability)
		context: "list",
	}
}

// SetSize updates the sidebar dimensions
func (s *ShortcutsSidebar) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// SetContext updates the current context for filtering shortcuts
func (s *ShortcutsSidebar) SetContext(ctx string) {
	s.context = ctx
}

// SetFocus updates the current focus for registry-based bindings (bv-xl6g)
func (s *ShortcutsSidebar) SetFocus(f focus) {
	s.focusHint = f
	s.context = ContextFromFocus(f)
}

// SetSplitView updates the small set of focus-dependent List bindings.
func (s *ShortcutsSidebar) SetSplitView(split bool) {
	s.splitView = split
}

// SetKeyRegistry sets the key registry for auto-generated bindings (bv-xl6g)
func (s *ShortcutsSidebar) SetKeyRegistry(r *KeyRegistry) {
	s.keyRegistry = r
}

// ScrollUp scrolls the sidebar content up
func (s *ShortcutsSidebar) ScrollUp() {
	if s.scrollOffset > 0 {
		s.scrollOffset--
	}
}

// ScrollDown scrolls the sidebar content down
func (s *ShortcutsSidebar) ScrollDown() {
	s.scrollOffset++
}

// ScrollPageUp scrolls up by a page
func (s *ShortcutsSidebar) ScrollPageUp() {
	s.scrollOffset -= 10
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

// ScrollPageDown scrolls down by a page
func (s *ShortcutsSidebar) ScrollPageDown() {
	s.scrollOffset += 10
}

// ResetScroll resets scroll position to top
func (s *ShortcutsSidebar) ResetScroll() {
	s.scrollOffset = 0
}

// Width returns the fixed width of the sidebar
func (s *ShortcutsSidebar) Width() int {
	return s.width
}

// sectionsFromRegistry builds shortcut sections from the key registry (bv-xl6g).
// Returns nil if registry is nil or has no bindings for current focus.
func (s *ShortcutsSidebar) sectionsFromRegistry() []shortcutSection {
	if s.keyRegistry == nil {
		return nil
	}

	bindings := s.keyRegistry.AllBindingsForFocus(s.focusHint)
	if len(bindings) == 0 {
		return nil
	}

	// Group by Category
	categoryItems := make(map[string][]shortcutItem)
	categoryOrder := []string{} // Preserve order of first appearance

	for _, b := range bindings {
		// Ctrl+j/k are consumed by the open sidebar, so the underlying view's
		// detail-scroll bindings must not be shown alongside the sidebar control.
		if b.Key == "ctrl+j" || b.Key == "ctrl+k" {
			continue
		}
		if s.splitView && s.focusHint == focusList && b.Key == "enter" {
			continue
		}
		if (s.focusHint == focusList || s.focusHint == focusDetail) && !s.splitView && (b.Key == "tab" || b.Key == "<" || b.Key == ">") {
			continue
		}
		if s.focusHint == focusTree && (b.Key == "tab" || b.Key == "<" || b.Key == ">") {
			continue
		}
		if s.focusHint == focusTree && b.Category != "Tree" && b.Category != "Views" && b.Category != "Filters" {
			continue
		}
		cat := b.Category
		if cat == "" {
			cat = "Other"
		}
		if _, exists := categoryItems[cat]; !exists {
			categoryOrder = append(categoryOrder, cat)
		}
		categoryItems[cat] = append(categoryItems[cat], shortcutItem{
			key:  b.Key,
			desc: b.Desc,
		})
	}
	categoryItems["Sidebar"] = []shortcutItem{{key: "ctrl+j/k", desc: "Scroll sidebar"}}
	categoryOrder = append(categoryOrder, "Sidebar")

	// Keep the most useful categories at the top; bindings within each section
	// remain sorted by the registry.
	preferredOrder := []string{"Navigation", "Views", "Filters", "Actions", "Global", "Sidebar"}
	orderedCategories := make([]string, 0, len(categoryOrder))
	seenCategories := make(map[string]struct{}, len(categoryOrder))
	for _, cat := range preferredOrder {
		if _, exists := categoryItems[cat]; exists {
			orderedCategories = append(orderedCategories, cat)
			seenCategories[cat] = struct{}{}
		}
	}
	for _, cat := range categoryOrder {
		if _, seen := seenCategories[cat]; !seen {
			orderedCategories = append(orderedCategories, cat)
		}
	}

	// Build sections in the stable display order.
	sections := make([]shortcutSection, 0, len(orderedCategories))
	for _, cat := range orderedCategories {
		sections = append(sections, shortcutSection{
			title:    cat,
			items:    categoryItems[cat],
			contexts: []string{}, // Registry bindings are already focus-filtered
		})
	}

	return sections
}

// allSections returns all shortcut sections with their contexts.
// Tries registry-based sections first, falls back to hardcoded (bv-xl6g).
func (s *ShortcutsSidebar) allSections() []shortcutSection {
	// Try registry-based sections first
	if sections := s.sectionsFromRegistry(); len(sections) > 0 {
		return sections
	}

	// Fallback to hardcoded sections
	return s.hardcodedSections()
}

// hardcodedSections returns the manually maintained shortcut sections.
// Used as fallback when registry has no bindings (bv-xl6g).
func (s *ShortcutsSidebar) hardcodedSections() []shortcutSection {
	return []shortcutSection{
		{
			title:    "Navigation",
			contexts: []string{}, // All contexts
			items: []shortcutItem{
				{"j/k", "Navigate"},
				{"Esc", "Back/close"},
			},
		},
		{
			title:    "Jumps",
			contexts: []string{"list", "board"},
			items: []shortcutItem{
				{"Home/G", "Start/end"},
				{"^d/^u", "Page down/up"},
			},
		},
		{
			title:    "Views",
			contexts: []string{"list", "detail", "split"},
			items: []shortcutItem{
				{"a", "Actionable"},
				{"b", "Board"},
				{"g", "Graph"},
				{"h", "History"},
				{"i", "Insights"},
				{"?", "Help"},
				{";", "This sidebar"},
				{"p", "Priority hints"},
			},
		},
		{
			title:    "Graph",
			contexts: []string{"graph"},
			items: []shortcutItem{
				{"hjkl", "Navigate"},
				{"/", "Search ID/title"},
				{"n/N", "Next/prev match"},
				{"PgUp/Dn", "Scroll ↑/↓"},
				{"Enter", "Jump to issue"},
				{"Esc", "Clear/back"},
			},
		},
		{
			title:    "Insights",
			contexts: []string{"insights"},
			items: []shortcutItem{
				{"h/l", "Switch panel"},
				{"j/k", "Select item"},
				{"o", "Active work"},
				{"r", "Ready-only"},
				{"^j/^k", "Scroll detail"},
				{"e", "Explanations"},
				{"x", "Calc proof"},
				{"m", "Heatmap"},
				{"Enter", "Open issue / cell"},
				{"] / F4", "Attention view"},
				{"f", "Flow matrix"},
			},
		},
		{
			title:    "Attention",
			contexts: []string{"attention"},
			items: []shortcutItem{
				{"j/k / ↑↓", "Move selection"},
				{"Home/G", "First / last label"},
				{"Enter", "Filter List by label"},
				{"g", "Graph view"},
				{"] / F4", "Close Attention"},
				{"Esc / q", "Return to previous view"},
			},
		},
		{
			title:    "History",
			contexts: []string{"history"},
			items: []shortcutItem{
				{"v", "Git/Bead mode"},
				{"/", "Search"},
				{"j/k", "Navigate ↓/↑"},
				{"J/K", "Detail ↓/↑"},
				{"Tab", "Focus toggle"},
				{"y", "Copy SHA"},
				{"o", "Open in browser"},
				{"f", "File tree"},
				{"g", "Graph view"},
				{"c", "Cycle filter"},
			},
		},
		{
			title:    "Board",
			contexts: []string{"board"},
			items: []shortcutItem{
				{"h/l", "Columns ←/→"},
				{"j/k", "Items ↓/↑"},
				{"Tab", "Toggle detail"},
				{"e", "Empty columns"},
				{"y", "Copy ID"},
				{"^j/^k", "Scroll detail"},
				{"Enter", "Open issue detail"},
			},
		},
		{
			title:    "Tree",
			contexts: []string{"tree"},
			items: []shortcutItem{
				{"j/k", "Move ↓/↑"},
				{"h/l", "Fold/visit parent/child"},
				{"Enter/Space", "Toggle/select"},
				{"+/-", "Expand/collapse all"},
				{"o/c/r", "Open/closed/ready"},
				{"/", "Search"},
				{"n/N", "Next/prev match"},
				{"v", "Search scope"},
				{"E", "Exit Tree"},
			},
		},
		{
			title:    "Filters",
			contexts: []string{"list", "split"},
			items: []shortcutItem{
				{"o", "Open only"},
				{"c", "Closed only"},
				{"r", "Ready (no blocks)"},
				{"l", "Label picker"},
				{"w", "Repository scope"},
				{"/", "Search"},
			},
		},
		{
			title:    "Actions",
			contexts: []string{"list", "detail", "split"},
			items: []shortcutItem{
				{"n", "Add comment"},
				{"t/T", "Choose/Quick diff"},
				{"x", "Export markdown"},
				{"y", "Copy ID"},
				{"C", "Copy full issue"},
				{"O", "Open in $EDITOR"},
				{"'", "Recipe picker"},
				{"U", "Self-update"},
				{"V", "Cass sessions"},
			},
		},
	}
}

// View renders the sidebar
func (s *ShortcutsSidebar) View() string {
	t := s.theme

	// Styles
	titleStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true).
		Align(lipgloss.Center).
		Width(s.width - 4)

	sectionStyle := t.Renderer.NewStyle().
		Foreground(t.Secondary).
		Bold(true).
		MarginTop(1)

	keyStyle := t.Renderer.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#7D56F4", Dark: "#BD93F9"}).
		Bold(true).
		Width(8)

	descStyle := t.Renderer.NewStyle().
		Foreground(t.Base.GetForeground())

	dimStyle := t.Renderer.NewStyle().
		Foreground(ColorFooterHint).
		Italic(true)

	// Build content
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Shortcuts"))
	sb.WriteString("\n")

	// Filter sections by context
	sections := s.allSections()
	for _, section := range sections {
		// Check if this section applies to current context
		if len(section.contexts) > 0 {
			found := false
			for _, ctx := range section.contexts {
				if ctx == s.context {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		sb.WriteString(sectionStyle.Render(section.title))
		sb.WriteString("\n")

		for _, item := range section.items {
			line := keyStyle.Render(item.key) + descStyle.Render(item.desc)
			sb.WriteString(line + "\n")
		}
	}

	// Build content lines for scrolling
	fullContent := sb.String()
	lines := strings.Split(fullContent, "\n")
	totalLines := len(lines)

	// Calculate visible area
	availableHeight := s.height - 4 // Reserve for border/padding and hint
	if availableHeight < 5 {
		availableHeight = 5
	}

	// Clamp scroll
	maxScroll := totalLines - availableHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.scrollOffset > maxScroll {
		s.scrollOffset = maxScroll
	}

	// Extract visible lines
	startLine := s.scrollOffset
	endLine := startLine + availableHeight
	if endLine > totalLines {
		endLine = totalLines
	}
	visibleLines := lines[startLine:endLine]
	visibleContent := strings.Join(visibleLines, "\n")

	// Add scroll hint if needed
	var footer string
	if totalLines > availableHeight {
		scrollPercent := 0
		if maxScroll > 0 {
			scrollPercent = s.scrollOffset * 100 / maxScroll
		}
		footer = dimStyle.Render(fmt.Sprintf("ctrl+j/k scroll %d%%", scrollPercent))
	} else {
		footer = dimStyle.Render("; hide")
	}

	// Combine content and footer
	content := visibleContent + "\n" + footer

	// Create the sidebar box
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Secondary).
		Padding(0, 1).
		Width(s.width).
		Height(s.height - 1).
		MaxHeight(s.height - 1)

	return boxStyle.Render(content)
}

// contextFromFocus returns the context string for the current focus
func ContextFromFocus(f focus) string {
	switch f {
	case focusList:
		return "list"
	case focusDetail:
		return "detail"
	case focusBoard:
		return "board"
	case focusGraph:
		return "graph"
	case focusInsights:
		return "insights"
	case focusAttention:
		return "attention"
	case focusHistory:
		return "history"
	case focusTree:
		return "tree"
	case focusActionable:
		return "actionable"
	case focusLabelDashboard:
		return "label"
	case focusFlowMatrix:
		return "flow"
	case focusSprint:
		return "sprint"
	case focusScopePicker:
		return "scope"
	case focusBacklog:
		return "backlog"
	default:
		return "list"
	}
}
