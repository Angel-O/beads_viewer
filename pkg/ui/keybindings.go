package ui

import (
	"sort"
	"strings"
	"sync"
)

// KeyBinding documents one key for one focus context. Bindings feed the
// shortcuts sidebar, the help overlay, and --robot-help; they do not dispatch.
// Runtime dispatch is Model.Update and the per-view handlers (handleListKeys,
// handleBoardKeys, handleTreeKeys, ...), and the tests in keybindings_test.go
// drive those handlers directly for every documented key.
type KeyBinding struct {
	Focus    focus  // Which view/focus context this binding applies to
	Key      string // Key string (e.g., "j", "ctrl+d", "enter")
	Desc     string // Human-readable description for help display
	Category string // Grouping category (e.g., "Navigation", "Actions")
}

// KeyRegistry is the per-focus index of documented key bindings.
type KeyRegistry struct {
	mu       sync.RWMutex
	bindings map[focus][]KeyBinding // focus -> ordered bindings (for help)
}

// NewKeyRegistry creates an empty key registry ready to accept bindings.
func NewKeyRegistry() *KeyRegistry {
	return &KeyRegistry{
		bindings: make(map[focus][]KeyBinding),
	}
}

// RegisterBinding adds a single key binding to the registry.
// If the same key is registered twice for the same focus, the later
// registration replaces the earlier one.
func (r *KeyRegistry) RegisterBinding(b KeyBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Track binding for help generation (replace if exists)
	existingBindings := r.bindings[b.Focus]
	found := false
	for i, existing := range existingBindings {
		if existing.Key == b.Key {
			existingBindings[i] = b
			found = true
			break
		}
	}
	if !found {
		r.bindings[b.Focus] = append(r.bindings[b.Focus], b)
	}
}

// AllBindingsForFocus returns all registered bindings for a specific focus,
// sorted by category then by key. Returns an empty slice if no bindings exist.
func (r *KeyRegistry) AllBindingsForFocus(f focus) []KeyBinding {
	r.mu.RLock()
	bindings := r.bindings[f]
	if len(bindings) == 0 {
		r.mu.RUnlock()
		return []KeyBinding{}
	}

	// Return a sorted copy to avoid exposing internal state
	result := make([]KeyBinding, len(bindings))
	copy(result, bindings)
	r.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].Key < result[j].Key
	})

	return result
}

// AllBindings returns all registered bindings across all focus contexts,
// sorted by focus, then category, then key.
func (r *KeyRegistry) AllBindings() []KeyBinding {
	r.mu.RLock()
	if len(r.bindings) == 0 {
		r.mu.RUnlock()
		return []KeyBinding{}
	}

	var result []KeyBinding
	for _, bindings := range r.bindings {
		result = append(result, bindings...)
	}
	r.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		if result[i].Focus != result[j].Focus {
			return result[i].Focus < result[j].Focus
		}
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].Key < result[j].Key
	})

	return result
}

// HasBinding reports whether a binding is documented for the focus and key.
func (r *KeyRegistry) HasBinding(f focus, key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, b := range r.bindings[f] {
		if b.Key == key {
			return true
		}
	}
	return false
}

// registerKeyBindings populates the KeyRegistry from GetKeyBindingDocs so the
// shortcuts sidebar and help surfaces show exactly the documented keys for
// the focused view. Called from NewModel.
func (m *Model) registerKeyBindings() {
	if m == nil || m.keyRegistry == nil {
		return
	}

	for _, doc := range GetKeyBindingDocs() {
		for _, f := range focusesForBindingDoc(doc) {
			m.keyRegistry.RegisterBinding(KeyBinding{
				Focus:    f,
				Key:      doc.Key,
				Desc:     doc.Desc,
				Category: doc.Category,
			})
		}
	}
}

func focusesForBindingDoc(doc KeyBindingDoc) []focus {
	var focuses []focus
	seen := make(map[focus]struct{})

	addFocus := func(f focus) {
		if _, ok := seen[f]; ok {
			return
		}
		seen[f] = struct{}{}
		focuses = append(focuses, f)
	}

	for _, raw := range strings.Split(doc.Context, ",") {
		switch strings.TrimSpace(raw) {
		case "all":
			for _, f := range allDocumentedFocuses() {
				addFocus(f)
			}
		case "list":
			addFocus(focusList)
		case "detail":
			addFocus(focusDetail)
		case "board":
			addFocus(focusBoard)
		case "graph":
			addFocus(focusGraph)
		case "insights":
			addFocus(focusInsights)
		case "history":
			addFocus(focusHistory)
		case "actionable":
			addFocus(focusActionable)
		case "label", "label-dashboard":
			addFocus(focusLabelDashboard)
		case "tree":
			addFocus(focusTree)
		case "flow", "flow-matrix":
			addFocus(focusFlowMatrix)
		case "sprint":
			addFocus(focusSprint)
		case "attention":
			addFocus(focusAttention)
		}
	}

	return focuses
}

func allDocumentedFocuses() []focus {
	return []focus{
		focusList,
		focusDetail,
		focusBoard,
		focusGraph,
		focusInsights,
		focusHistory,
		focusActionable,
		focusLabelDashboard,
		focusTree,
		focusFlowMatrix,
		focusSprint,
		focusAttention,
	}
}

// KeyBindingDoc represents a key binding for documentation purposes (bv-xl6g).
type KeyBindingDoc struct {
	Key      string
	Desc     string
	Category string
	Context  string // Which view(s) this applies to
}

// GetKeyBindingDocs returns all key bindings for documentation/robot-help (bv-xl6g).
// This is separate from the registry to allow documentation even before handlers
// are registered (bv-3bsx migration).
func GetKeyBindingDocs() []KeyBindingDoc {
	// Authoritative keybinding documentation - update when bindings change
	return []KeyBindingDoc{
		// Shared Navigation. Keep contexts explicit: overlays and search inputs
		// consume these keys differently and are not registry view contexts.
		{"j", "Move down", "Navigation", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint,attention"},
		{"k", "Move up", "Navigation", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint,attention"},
		{"up", "Move up", "Navigation", "list,label-dashboard,attention"},
		{"down", "Move down", "Navigation", "list,label-dashboard,attention"},
		{"left", "Previous page", "Navigation", "list"},
		{"right", "Next page", "Navigation", "list"},
		{"home", "Go to start", "Navigation", "list,board,label-dashboard,flow,attention"},
		{"enter", "Open details", "Navigation", "list"},
		{"G", "Go to end", "Navigation", "list,board,tree,flow,label-dashboard,attention"},
		{"gg", "Go to start", "Navigation", "board,tree"},
		{"ctrl+d", "Page down", "Navigation", "list,detail,board,graph,tree"},
		{"ctrl+u", "Page up", "Navigation", "list,detail,board,graph,tree"},
		{"pgup", "Page up", "Navigation", "tree"},
		{"pgdown", "Page down", "Navigation", "tree"},
		{"esc", "Back/close", "Navigation", "list,detail,board,insights,history,actionable,label-dashboard,flow,sprint"},
		{"q", "Back/quit", "Navigation", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint"},
		{"ctrl+c", "Force quit", "Global", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint,attention"},
		{"`", "Full tutorial", "Global", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint,attention"},
		{"F2/;", "Shortcuts sidebar", "Global", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint,attention"},

		// View Switching
		{"a", "Actionable view", "Views", "list,detail"},
		{"b", "Board view", "Views", "list,detail"},
		{"g", "Graph view", "Views", "list,detail"},
		{"h", "History view", "Views", "list,detail"},
		{"i", "Insights panel", "Views", "list,detail"},
		{"E", "Enter Tree (uppercase)", "Views", "list,detail"},
		{"?", "Help overlay", "Views", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint,attention"},
		{"tab", "Switch panes in Split", "Views", "list,detail"},
		{"p", "Priority hints", "Views", "list,board,graph,tree,insights,history,actionable,flow,sprint"},
		{"b", "Return to List", "Views", "board,graph,tree,insights,history,actionable,flow"},
		{"g", "Return to List", "Views", "graph"},
		{"i", "Return to List", "Views", "insights"},
		{"a", "Return to List", "Views", "actionable"},
		{"f", "Flow matrix", "Views", "list,board,graph,tree,insights,actionable,label-dashboard"},
		{"[", "Label dashboard", "Views", "list,board,graph,tree,insights,history,actionable,flow"},
		{"]", "Attention view", "Views", "list,board,graph,tree,insights,history,actionable,label-dashboard,flow"},
		{"[", "Close dashboard", "Views", "label-dashboard"},
		{"]", "Close Attention", "Views", "attention"},
		{"a", "Actionable view", "Views", "board,graph,insights,history,tree,flow,label-dashboard"},
		{"b", "Board view", "Views", "graph,tree,insights,history,actionable,flow,label-dashboard"},
		{"g", "Graph view", "Views", "insights,actionable,label-dashboard,attention"},
		{"i", "Insights panel", "Views", "board,graph,tree,history,actionable,flow,label-dashboard"},
		{"E", "Enter Tree (uppercase)", "Views", "board,graph,insights,history,actionable,flow,label-dashboard"},
		{"h", "History view", "Views", "actionable,flow"},
		{"P", "Sprint dashboard", "Views", "list,detail"},
		{"[", "Label dashboard", "Views", "detail"},
		{"]", "Attention view", "Views", "detail"},
		// Filters
		{"o", "Open issues only", "Filters", "list,board,tree"},
		{"c", "Closed issues only", "Filters", "list,board,tree"},
		{"r", "Ready (unblocked)", "Filters", "list,board,tree"},
		{"l", "Label picker", "Filters", "list"},
		{"I", "Exact issue-type picker", "Filters", "list"},
		{"w", "Repository scope picker", "Filters", "list"},
		{"/", "Search/filter", "Filters", "list"},
		{"ctrl+s", "Semantic search toggle", "Filters", "list"},
		{"H", "Hybrid search toggle", "Filters", "list"},
		{"alt+h", "Hybrid preset", "Filters", "list"},

		// Actions
		{"t", "Time travel (custom revision)", "Actions", "list,detail"},
		{"T", "Time travel (HEAD~5)", "Actions", "list,detail"},
		{"n", "Next changed issue (time travel)", "Actions", "list"},
		{"N", "Previous changed issue (time travel)", "Actions", "list"},
		{"x", "Export to markdown", "Actions", "list,detail"},
		{"y", "Copy issue ID", "Actions", "list,detail,board"},
		{"C", "Copy full issue", "Actions", "list,detail"},
		{"n", "Add comment", "Actions", "list,detail"},
		{"e", "Edit comment", "Actions", "list,detail"},
		{"d", "Delete comment", "Actions", "list,detail"},
		{"O", "Open in $EDITOR", "Actions", "list,detail"},
		{"'", "Recipe picker", "Actions", "list"},
		{"U", "Self-update check", "Actions", "list,detail"},
		{"V", "Cass sessions", "Actions", "list"},
		{"s", "Cycle sort mode", "Actions", "list"},
		{"S", "Apply triage sort", "Actions", "list"},
		{"!", "Alerts panel", "Actions", "list,board,graph,tree,insights,history,actionable,flow,sprint"},
		{"Ctrl+R/F5", "Force refresh", "Actions", "list,detail,board,graph,tree,insights,history,actionable,label-dashboard,flow,sprint"},
		{"<", "Shrink list pane", "Actions", "list,detail"},
		{">", "Expand list pane", "Actions", "list,detail"},

		// Graph View
		{"hjkl", "Navigate graph", "Graph", "graph"},
		{"pgup/pgdown", "Scroll graph up/down", "Graph", "graph"},
		{"/", "Search ID or title", "Graph", "graph"},
		{"n/N", "Next/previous match", "Graph", "graph"},
		{"enter", "Open selected issue", "Graph", "graph"},
		{"esc", "Clear search or exit", "Graph", "graph"},

		// Tree View
		{"h", "Collapse or visit parent", "Tree", "tree"},
		{"l", "Expand or visit child", "Tree", "tree"},
		{"enter", "Toggle expansion / select match", "Tree", "tree"},
		{"space", "Toggle expansion", "Tree", "tree"},
		{"/", "Search Tree", "Tree", "tree"},
		{"n", "Next search match", "Tree", "tree"},
		{"N", "Previous search match", "Tree", "tree"},
		{"v", "Toggle search scope", "Tree", "tree"},
		{"+", "Expand all", "Tree", "tree"},
		{"-", "Collapse all", "Tree", "tree"},
		{"E", "Exit Tree", "Tree", "tree"},
		{"esc", "Clear search or exit Tree", "Tree", "tree"},

		// Board View
		{"h", "Previous column", "Board", "board"},
		{"l", "Next column", "Board", "board"},
		{"H", "First column", "Board", "board"},
		{"L", "Last column", "Board", "board"},
		{"s", "Cycle swimlane/grouping mode", "Board", "board"},
		{"tab", "Toggle detail", "Board", "board"},

		// Tree View
		{"ctrl+j", "Scroll detail down", "Board", "board"},
		{"ctrl+k", "Scroll detail up", "Board", "board"},
		{"enter", "Open selected issue", "Board", "board"},
		{"n/N", "Next/previous match", "Board", "board"},
		{"y", "Copy issue ID", "Board", "board"},
		{"e", "Cycle empty columns", "Board", "board"},
		{"d", "Expand selected card", "Board", "board"},
		{"1-4", "Jump to column", "Board", "board"},
		{"H/L", "First/last column", "Board", "board"},
		{"0/$", "First/last item", "Board", "board"},
		{"/", "Search cards", "Board", "board"},

		// Insights View
		{"h", "Previous panel", "Insights", "insights"},
		{"l", "Next panel", "Insights", "insights"},
		{"tab", "Next panel", "Insights", "insights"},
		{"ctrl+j", "Scroll detail down", "Insights", "insights"},
		{"ctrl+k", "Scroll detail up", "Insights", "insights"},
		{"o", "Active work", "Filters", "insights"},
		{"r", "Ready-only toggle", "Filters", "insights"},
		{"shift+tab", "Previous panel", "Insights", "insights"},
		{"e", "Toggle explanations", "Insights", "insights"},
		{"x", "Calculation proof", "Insights", "insights"},
		{"m", "Heatmap toggle", "Insights", "insights"},
		{"enter", "Open selected issue", "Insights", "insights"},
		{"] / F4", "Attention view", "Insights", "insights"},

		// History View
		{"v", "Toggle git/bead mode", "History", "history"},
		{"tab", "Toggle focus", "History", "history"},
		{"t", "Toggle timeline pane", "History", "history"},
		{"J", "Detail scroll down", "History", "history"},
		{"K", "Detail scroll up", "History", "history"},
		{"enter", "Open selected bead", "History", "history"},
		{"y", "Copy commit SHA", "History", "history"},
		{"o", "Open in browser", "History", "history"},
		{"f/F", "Toggle file tree", "History", "history"},
		{"g", "Graph view", "History", "history"},
		{"h", "Exit History", "History", "history"},
		{"/", "Search commits/beads", "History", "history"},
		{"c", "Confidence filter", "History", "history"},

		// Other Views
		{"enter", "Apply label filter", "Labels", "label-dashboard"},
		{"h", "Open label detail", "Labels", "label-dashboard"},
		{"d", "Open label drilldown", "Labels", "label-dashboard"},
		{"f3", "Close dashboard", "Labels", "label-dashboard"},
		{"enter", "Open selected issue", "Actionable", "actionable"},
		{"enter", "Drill down / open issue", "Flow", "flow"},
		{"f", "Close Flow or drilldown", "Flow", "flow"},
		{"enter", "Filter List by highlighted label", "Attention", "attention"},
		{"] / F4", "Close Attention", "Attention", "attention"},
		{"esc / q", "Back to previous view", "Attention", "attention"},

		// Sprint Dashboard (P)
		{"P", "Close sprint dashboard", "Sprint", "sprint"},
		{"j", "Next sprint", "Sprint", "sprint"},
		{"k", "Previous sprint", "Sprint", "sprint"},
	}
}
