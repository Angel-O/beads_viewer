package ui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// ScopeInfo is the display projection needed by the Viewer scope chooser.
// Membership itself remains owned by wbd; the UI only retains identity/count.
type ScopeInfo struct {
	ID          string
	Name        string
	CreatedAt   time.Time
	MemberCount int
	Active      bool
}

// ScopeSnapshot is the complete named-scope state needed by the Viewer.
type ScopeSnapshot struct {
	Scopes []ScopeInfo
	Active *ScopeInfo
}

// BacklogPage is one bounded page of unscoped beads. NextCursor is opaque and
// must only be sent back to the service that produced it.
type BacklogPage struct {
	Issues     []model.Issue
	HasMore    bool
	NextCursor string
}

// BacklogQuery is the complete request for one bounded backlog page. Filter
// is a user-facing query; Cursor is opaque and must not be decoded or edited.
type BacklogQuery struct {
	Filter string
	Cursor string
	Limit  int
}

// ScopeDetails is the bounded result for one selected named scope. The
// contract deliberately has no pagination or cache policy; callers that only
// need the scope projection can use Info.
type ScopeDetails struct {
	Info      ScopeInfo
	Issues    []model.Issue
	MemberIDs []string
}

// ScopeMutationKind names the semantic operation requested by the Viewer.
// Keeping this typed prevents command names from leaking into Model seams.
type ScopeMutationKind string

const (
	ScopeMutationCreate     ScopeMutationKind = "create"
	ScopeMutationActivate   ScopeMutationKind = "activate"
	ScopeMutationDeactivate ScopeMutationKind = "deactivate"
	ScopeMutationAdd        ScopeMutationKind = "add"
	ScopeMutationRemove     ScopeMutationKind = "remove"
	ScopeMutationMove       ScopeMutationKind = "move"
)

// ScopeMutation is the semantic scope change accepted by the Viewer service.
// IssueIDs may contain more than one ID for add/remove/move.
type ScopeMutation struct {
	Kind          ScopeMutationKind
	Name          string
	ScopeID       string
	IssueIDs      []string
	SourceScopeID string
	TargetScopeID string
}

// ScopeServices is the narrow CLI composition seam for scope-first UI work.
// A zero value keeps standalone/local Viewer callers unchanged.
type ScopeServices struct {
	Load func(context.Context) (ScopeSnapshot, error)
	// QueryBacklog loads one filtered page. The legacy LoadBacklog field remains
	// as a zero-cost compatibility fallback for local callers.
	QueryBacklog func(context.Context, BacklogQuery) (BacklogPage, error)
	// LoadDetails loads one selected scope without taking ownership of member
	// pagination, caching, or streaming.
	LoadDetails func(context.Context, string) (ScopeDetails, error)
	// Mutate applies one semantic scope operation, including batch membership
	// changes. The legacy mutation fields remain compatibility fallbacks.
	Mutate func(context.Context, ScopeMutation) error
	// Create creates a named scope without activating it.
	Create   func(context.Context, string) error
	Activate func(context.Context, string) error
	// Deactivate clears the active named scope.
	Deactivate  func(context.Context) error
	Add         func(context.Context, string, string) error
	Remove      func(context.Context, string, string) error
	Move        func(context.Context, string, string, string) error
	LoadBacklog func(context.Context, string, int) (BacklogPage, error)
}

type scopeSnapshotMsg struct {
	snapshot ScopeSnapshot
	err      error
}

type backlogPageMsg struct {
	page       BacklogPage
	cursor     string
	index      int
	generation uint64
	err        error
}

type scopeDetailsMsg struct {
	details ScopeDetails
	err     error
}

type scopeMutationMsg struct {
	mutation     ScopeMutation
	action       string // compatibility with older local message producers
	restoreFocus bool
	err          error
}

func loadScopeSnapshotCmd(service ScopeServices) tea.Cmd {
	return func() tea.Msg {
		if service.Load == nil {
			return scopeSnapshotMsg{}
		}
		snapshot, err := service.Load(context.Background())
		return scopeSnapshotMsg{snapshot: snapshot, err: err}
	}
}

func loadScopeDetailsCmd(service ScopeServices, scopeID string) tea.Cmd {
	return func() tea.Msg {
		if service.LoadDetails == nil {
			return scopeDetailsMsg{}
		}
		details, err := service.LoadDetails(context.Background(), scopeID)
		return scopeDetailsMsg{details: details, err: err}
	}
}

func loadBacklogPageCmd(service ScopeServices, query BacklogQuery, index int, generation uint64) tea.Cmd {
	return func() tea.Msg {
		if service.QueryBacklog == nil && service.LoadBacklog == nil {
			return backlogPageMsg{cursor: query.Cursor, index: index, generation: generation}
		}
		var page BacklogPage
		var err error
		if service.QueryBacklog != nil {
			page, err = service.QueryBacklog(context.Background(), query)
		} else {
			page, err = service.LoadBacklog(context.Background(), query.Cursor, query.Limit)
		}
		return backlogPageMsg{page: page, cursor: query.Cursor, index: index, generation: generation, err: err}
	}
}

func runScopeMutationCmd(mutation ScopeMutation, restoreFocus bool, run func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		if run == nil {
			return scopeMutationMsg{mutation: mutation, action: string(mutation.Kind), restoreFocus: restoreFocus}
		}
		return scopeMutationMsg{mutation: mutation, action: string(mutation.Kind), restoreFocus: restoreFocus, err: run(context.Background())}
	}
}

const backlogPageSize = 50

// isScopeBacklogGlobalKey leaves global controls and view jumps on the main
// Update path. Search input is intentionally excluded so printable keys remain
// query text.
func isScopeBacklogGlobalKey(key string) bool {
	switch key {
	case "ctrl+c", "?", "`", ";", "f2", "ctrl+j", "ctrl+k", "ctrl+r", "f5",
		"w", "W", "B", "a", "b", "g", "h", "i", "E", "f", "[", "]", "f3", "f4":
		return true
	default:
		return false
	}
}

// BacklogModel renders the global, unscoped backlog independently of the
// ordinary graph snapshot. It deliberately owns only one page and cursors.
type BacklogModel struct {
	issues        []model.Issue
	items         []IssueItem
	filtered      []model.Issue
	filteredItems []IssueItem
	selected      int
	filter        string
	searching     bool
	hasMore       bool
	nextCursor    string
	pageIndex     int
	pageCursors   []string
	width         int
	height        int
	theme         Theme
	delegate      IssueDelegate
}

func NewBacklogModel(theme Theme) BacklogModel {
	return BacklogModel{theme: theme, pageCursors: []string{""}, delegate: IssueDelegate{Theme: theme, useFullWidth: true}}
}

func (b *BacklogModel) SetSize(width, height int) {
	b.width, b.height = width, height
}

func (b *BacklogModel) SetPage(page BacklogPage, index int) {
	b.issues = append([]model.Issue(nil), page.Issues...)
	b.items = make([]IssueItem, len(page.Issues))
	for i, issue := range page.Issues {
		b.items[i] = IssueItem{Issue: issue, RepoPrefix: issueRepoKey(issue)}
	}
	b.applyFilter()
	b.hasMore, b.nextCursor, b.pageIndex = page.HasMore, page.NextCursor, index
	if b.selected >= len(b.filtered) {
		b.selected = maxInt(0, len(b.filtered)-1)
	}
}

// setPresentation replaces only backlog display decoration; the page and its
// opaque cursor remain owned by BacklogModel.
func (b *BacklogModel) setPresentation(items []IssueItem) {
	b.items = append([]IssueItem(nil), items...)
	b.applyFilter()
}

// setDelegate keeps backlog rows on the existing IssueDelegate layout path.
func (b *BacklogModel) setDelegate(delegate IssueDelegate) { b.delegate = delegate }

func (b *BacklogModel) Reset() {
	b.issues = nil
	b.items = nil
	b.filtered = nil
	b.filteredItems = nil
	b.selected = 0
	b.pageIndex = 0
	b.nextCursor = ""
	b.hasMore = false
	b.pageCursors = []string{""}
}

func (b *BacklogModel) ResetPagination() {
	b.pageIndex = 0
	b.nextCursor = ""
	b.hasMore = false
	b.pageCursors = []string{""}
}

func (b BacklogModel) CurrentIssue() *model.Issue {
	if b.selected < 0 || b.selected >= len(b.filteredItems) {
		return nil
	}
	issue := b.filteredItems[b.selected].Issue
	return &issue
}

func (b BacklogModel) HasMore() bool      { return b.hasMore && b.nextCursor != "" }
func (b BacklogModel) PageIndex() int     { return b.pageIndex }
func (b BacklogModel) NextCursor() string { return b.nextCursor }
func (b BacklogModel) Filter() string     { return b.filter }
func (b BacklogModel) Searching() bool    { return b.searching }

func (b *BacklogModel) NextPageCursor() string {
	if !b.HasMore() {
		return ""
	}
	nextIndex := b.pageIndex + 1
	if nextIndex < len(b.pageCursors) {
		b.pageCursors = b.pageCursors[:nextIndex]
	}
	b.pageCursors = append(b.pageCursors, b.nextCursor)
	return b.nextCursor
}

func (b *BacklogModel) PreviousPageCursor() string {
	if b.pageIndex <= 0 || b.pageIndex >= len(b.pageCursors) {
		return ""
	}
	b.pageIndex--
	return b.pageCursors[b.pageIndex]
}

// CurrentPageCursor returns the opaque cursor used to load the current page.
// It is intentionally not interpreted by the page model.
func (b BacklogModel) CurrentPageCursor() string {
	if b.pageIndex < 0 || b.pageIndex >= len(b.pageCursors) {
		return ""
	}
	return b.pageCursors[b.pageIndex]
}

func (b *BacklogModel) BeginSearch() { b.searching = true }
func (b *BacklogModel) EndSearch()   { b.searching = false }
func (b *BacklogModel) ClearFilter() { b.filter = ""; b.applyFilter() }
func (b *BacklogModel) Backspace() {
	if b.filter != "" {
		b.filter = b.filter[:len(b.filter)-1]
		b.applyFilter()
	}
}
func (b *BacklogModel) AddFilter(value string) {
	b.filter += value
	b.applyFilter()
}
func (b *BacklogModel) Move(delta int) {
	items := len(b.filtered)
	if items == 0 {
		return
	}
	b.selected = (b.selected + delta + items) % items
}

func (b *BacklogModel) applyFilter() {
	b.filteredItems = b.filteredIssueItems()
	b.filtered = make([]model.Issue, len(b.filteredItems))
	for i, item := range b.filteredItems {
		b.filtered[i] = item.Issue
	}
	if b.selected >= len(b.filteredItems) {
		b.selected = maxInt(0, len(b.filteredItems)-1)
	}
}

func (b BacklogModel) filteredIssueItems() []IssueItem {
	if strings.TrimSpace(b.filter) == "" {
		return append([]IssueItem(nil), b.items...)
	}
	term := strings.ToLower(b.filter)
	result := make([]IssueItem, 0, len(b.items))
	for _, item := range b.items {
		if strings.Contains(strings.ToLower(item.Issue.ID), term) || strings.Contains(strings.ToLower(item.Issue.Title), term) {
			result = append(result, item)
		}
	}
	return result
}

func (b BacklogModel) View() string {
	if b.searching {
		return b.renderBacklog("Backlog search: " + b.filter + "_")
	}
	return b.renderBacklog("Global backlog")
}

func (b BacklogModel) renderBacklog(title string) string {
	contentWidth := maxInt(b.width-4, 1)
	wide := contentWidth >= 100 && b.CurrentIssue() != nil
	listWidth := contentWidth
	if wide {
		listWidth = maxInt(contentWidth*2/3, 1)
	}
	delegate := b.delegate
	delegate.useFullWidth = true
	delegate.layoutItems = backlogListItems(b.items)
	delegate.columns = delegate.issueListColumnsFor(delegate.layoutItems, listWidth)

	lines := []string{b.renderBacklogHeader(title, delegate.columns)}
	availableHeight := maxInt(b.height-2, 1)
	listRows := availableHeight - 4 // header, page hint, preview, and padding
	if !wide && b.CurrentIssue() == nil {
		listRows++
	}
	if listRows < 1 {
		listRows = 1
	}
	listView := b.renderBacklogList(delegate, listWidth, listRows)
	page := b.renderBacklogPage(contentWidth)
	if wide {
		previewWidth := maxInt(contentWidth-listWidth-2, 1)
		preview := b.renderBacklogPreview(previewWidth)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, listView, "  ", preview))
	} else {
		lines = append(lines, listView)
		if b.CurrentIssue() != nil {
			lines = append(lines, b.renderBacklogPreview(contentWidth))
		}
	}
	lines = append(lines, page)
	return lipgloss.NewStyle().Width(b.width).Height(b.height).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func backlogListItems(items []IssueItem) []list.Item {
	result := make([]list.Item, len(items))
	for i := range items {
		result[i] = items[i]
	}
	return result
}

func (b BacklogModel) renderBacklogHeader(title string, columns *issueListColumns) string {
	return b.theme.Renderer.NewStyle().Foreground(b.theme.Primary).Bold(true).Render(title) + "\n" +
		b.theme.Renderer.NewStyle().Background(b.theme.Primary).
			Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#282A36"}).Bold(true).Inline(true).
			Render(renderIssueListHeader(columns))
}

func (b BacklogModel) renderBacklogList(delegate IssueDelegate, width, rows int) string {
	if len(b.filteredItems) == 0 {
		return b.theme.Renderer.NewStyle().Foreground(b.theme.Subtext).Render("No unscoped beads.")
	}
	start, end := b.visibleRangeFor(rows)
	items := backlogListItems(b.filteredItems)
	l := list.New(items, delegate, width, rows)
	l.Select(b.selected)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		var row bytes.Buffer
		delegate.Render(&row, l, index, items[index])
		lines = append(lines, row.String())
	}
	return strings.Join(lines, "\n")
}

func (b BacklogModel) renderBacklogPreview(width int) string {
	issue := b.CurrentIssue()
	if issue == nil {
		return ""
	}
	description := strings.Join(strings.Fields(issue.Description), " ")
	if description == "" {
		description = "(no description)"
	}
	return b.theme.Renderer.NewStyle().Foreground(b.theme.Subtext).Render(
		ansi.Truncate("DESCRIPTION  "+description, maxInt(width, 1), "…"))
}

func (b BacklogModel) renderBacklogPage(width int) string {
	page := fmt.Sprintf("page %d", b.pageIndex+1)
	if b.HasMore() {
		page += " · n next"
	}
	if b.pageIndex > 0 {
		page += " · p previous"
	}
	page += " · / filter · A add"
	return b.theme.Renderer.NewStyle().Foreground(b.theme.Subtext).Render(ansi.Truncate(page, maxInt(width, 1), "…"))
}

// visibleRange keeps the selected backlog row on screen while reserving the
// title and the page/filter hint. This is intentionally local to backlog
// rendering; ordinary list pagination has different layout ownership.
func (b BacklogModel) visibleRange() (int, int) {
	return b.visibleRangeFor(b.height - 5)
}

func (b BacklogModel) visibleRangeFor(rows int) (int, int) {
	if rows < 1 {
		rows = 1
	}
	if rows >= len(b.filteredItems) {
		return 0, len(b.filteredItems)
	}
	start := b.selected - rows + 1
	if start < 0 {
		start = 0
	}
	return start, min(start+rows, len(b.filteredItems))
}

// ScopePickerModel is intentionally a plain list: named scopes are a small
// control-plane collection, not another issue graph or repository filter.
type ScopePickerModel struct {
	scopes        []ScopeInfo
	selected      int
	moveTarget    string
	width, height int
	theme         Theme
}

func NewScopePickerModel(theme Theme) ScopePickerModel { return ScopePickerModel{theme: theme} }

func newScopeNameInput(theme Theme) textinput.Model {
	input := textinput.New()
	input.Placeholder = "e.g. Today, Release prep"
	input.CharLimit = 100
	input.Width = 40
	input.Prompt = "Scope name: "
	input.PromptStyle = lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(theme.Base.GetForeground())
	input.Blur()
	return input
}

func (s *ScopePickerModel) SetSize(width, height int) { s.width, s.height = width, height }
func (s *ScopePickerModel) SetScopes(scopes []ScopeInfo) {
	s.scopes = append([]ScopeInfo(nil), scopes...)
	for i := range s.scopes {
		if s.scopes[i].Active {
			s.selected = i
			break
		}
	}
	if s.selected >= len(s.scopes) {
		s.selected = maxInt(0, len(s.scopes)-1)
	}
}

// SetMoveTarget changes the picker from scope activation to moving one named
// bead. An empty title restores the activation-only picker.
func (s *ScopePickerModel) SetMoveTarget(title string) { s.moveTarget = title }
func (s *ScopePickerModel) Move(delta int) {
	if len(s.scopes) == 0 {
		return
	}
	s.selected = (s.selected + delta + len(s.scopes)) % len(s.scopes)
}
func (s ScopePickerModel) Selected() *ScopeInfo {
	if s.selected < 0 || s.selected >= len(s.scopes) {
		return nil
	}
	selected := s.scopes[s.selected]
	return &selected
}
func (s ScopePickerModel) View() string {
	heading := "Scopes"
	if s.moveTarget != "" {
		heading = "Move: " + s.moveTarget
	}
	title := s.theme.Renderer.NewStyle().Foreground(s.theme.Primary).Bold(true).Render(heading)
	lines := []string{title, ""}
	if len(s.scopes) == 0 {
		lines = append(lines, "No scopes available.")
	} else {
		for i, scope := range s.scopes {
			prefix := "  "
			if i == s.selected {
				prefix = "> "
			}
			active := ""
			if scope.Active {
				active = "  (active)"
			}
			lines = append(lines, fmt.Sprintf("%s%s · %s/%d%s", prefix, scope.Name, scope.CreatedAt.Format("2006-01-02"), scope.MemberCount, active))
		}
	}
	// Keep padding inside the assigned viewport before the sidebar is joined.
	return lipgloss.NewStyle().
		Width(maxInt(s.width, 1)).
		Height(maxInt(s.height-2, 1)).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m Model) renderScopeCreatePrompt() string {
	availableWidth := m.mainContentWidth()
	contentWidth := availableWidth - 8
	if contentWidth < 24 {
		contentWidth = 24
	}
	inputWidth := contentWidth - lipgloss.Width(m.scopeCreateInput.Prompt) - 1
	if inputWidth < 1 {
		inputWidth = 1
	}
	if m.scopeCreateInput.Width > inputWidth {
		m.scopeCreateInput.Width = inputWidth
	}

	t := m.theme
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 3).
		Align(lipgloss.Center)
	titleStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	mutedStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	content := titleStyle.Render("Create named scope") + "\n\n" +
		mutedStyle.Render("The new scope stays inactive until you activate it.") + "\n\n" +
		m.scopeCreateInput.View() + "\n\n" +
		mutedStyle.Render("Enter create · Esc cancel")
	return lipgloss.Place(availableWidth, max(1, m.height-1), lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

// renderNoActiveScope is the compact guidance shown inside the List or Detail
// content, rather than a full-screen state that hides the current view.
func (m Model) renderNoActiveScope(width int) string {
	style := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	return style.Width(maxInt(width, 1)).Render("No active scope — press W to choose or create a scope, or B for the global backlog.")
}

// replacePaddedEmptyState replaces a component's empty line before restoring
// its assigned dimensions, so a wrapped hint cannot consume adjacent content.
func replacePaddedEmptyState(view, empty, replacement string, width, height int) string {
	lines := strings.Split(view, "\n")
	for index, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) == empty {
			lines[index] = replacement
			style := lipgloss.NewStyle().Width(maxInt(width, 1)).Height(maxInt(height, 1))
			return style.MaxHeight(maxInt(height, 1)).Render(strings.Join(lines, "\n"))
		}
	}
	return view
}

func (m Model) renderScopeBadge() string {
	if m.runtimeServices.Scopes.Load == nil {
		return ""
	}
	label := "scope none"
	if m.activeScope != nil {
		label = fmt.Sprintf("%s · %d/100", m.activeScope.Name, m.activeScope.MemberCount)
	}
	return lipgloss.NewStyle().Background(ColorBgHighlight).Foreground(ColorInfo).Padding(0, 1).Render(label)
}

func (m *Model) openScopePicker(moveIssue string) tea.Cmd {
	m.showScopePicker = true
	m.scopePickerOrigin = m.focused
	m.scopePickerMoveIssue = moveIssue
	m.scopePicker.SetMoveTarget(m.scopeMoveTargetTitle(moveIssue))
	m.focused = focusScopePicker
	m.scopePicker.SetScopes(m.scopeCatalog)
	if m.runtimeServices.Scopes.Load != nil {
		return loadScopeSnapshotCmd(m.runtimeServices.Scopes)
	}
	return nil
}

func (m *Model) closeScopePicker() {
	m.showScopePicker = false
	m.scopePickerMoveIssue = ""
	m.scopePicker.SetMoveTarget("")
	m.focused = m.scopePickerOrigin
}

func (m *Model) openBacklog() tea.Cmd {
	m.isBacklogView = true
	m.isBoardView, m.isGraphView, m.isActionableView, m.isHistoryView = false, false, false, false
	m.focused = focusBacklog
	m.backlog.Reset()
	m.backlogLoading = true
	m.backlogPageGeneration++
	return loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{Limit: backlogPageSize}, 0, m.backlogPageGeneration)
}

func (m *Model) closeBacklog() {
	m.isBacklogView = false
	m.backlogLoading = false
	m.focused = focusList
}

func (m *Model) handleScopePickerKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.closeScopePicker()
		return m, nil
	case "j", "down":
		m.scopePicker.Move(1)
	case "k", "up":
		m.scopePicker.Move(-1)
	case "enter":
		selected := m.scopePicker.Selected()
		if selected == nil {
			if m.scopePickerMoveIssue != "" {
				m.statusMsg, m.statusIsError = "No destination scope selected", true
			}
			return m, nil
		}
		if m.scopePickerMoveIssue != "" {
			if m.activeScope == nil {
				m.statusMsg, m.statusIsError = "No active scope; press W to activate one", true
				return m, nil
			}
			if m.runtimeServices.Scopes.Mutate == nil && m.runtimeServices.Scopes.Move == nil {
				m.statusMsg, m.statusIsError = "Scope move is unavailable", true
				return m, nil
			}
			issueID, target, source := m.scopePickerMoveIssue, selected.ID, m.activeScope.ID
			mutation := ScopeMutation{Kind: ScopeMutationMove, IssueIDs: []string{issueID}, SourceScopeID: source, TargetScopeID: target}
			return m, runScopeMutationCmd(mutation, true, func(ctx context.Context) error {
				if m.runtimeServices.Scopes.Mutate != nil {
					return m.runtimeServices.Scopes.Mutate(ctx, mutation)
				}
				return m.runtimeServices.Scopes.Move(ctx, issueID, source, target)
			})
		}
		if selected.Active {
			if m.runtimeServices.Scopes.Mutate == nil && m.runtimeServices.Scopes.Deactivate == nil {
				return m, nil
			}
			mutation := ScopeMutation{Kind: ScopeMutationDeactivate}
			return m, runScopeMutationCmd(mutation, true, func(ctx context.Context) error {
				if m.runtimeServices.Scopes.Mutate != nil {
					return m.runtimeServices.Scopes.Mutate(ctx, mutation)
				}
				return m.runtimeServices.Scopes.Deactivate(ctx)
			})
		}
		if m.runtimeServices.Scopes.Mutate == nil && m.runtimeServices.Scopes.Activate == nil {
			return m, nil
		}
		mutation := ScopeMutation{Kind: ScopeMutationActivate, ScopeID: selected.ID}
		return m, runScopeMutationCmd(mutation, true, func(ctx context.Context) error {
			if m.runtimeServices.Scopes.Mutate != nil {
				return m.runtimeServices.Scopes.Mutate(ctx, mutation)
			}
			return m.runtimeServices.Scopes.Activate(ctx, selected.ID)
		})
	case "n":
		if m.scopePickerMoveIssue != "" {
			return m, nil
		}
		if m.runtimeServices.Scopes.Mutate == nil && m.runtimeServices.Scopes.Create == nil {
			m.statusMsg, m.statusIsError = "Scope creation is unavailable", true
			return m, nil
		}
		m.scopeCreateInput.SetValue("")
		focusCmd := m.scopeCreateInput.Focus()
		m.showScopeCreatePrompt = true
		m.focused = focusScopeCreateInput
		return m, focusCmd
	}
	return m, nil
}

func (m *Model) handleScopeCreateKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.scopeCreateInput.Blur()
		m.showScopeCreatePrompt = false
		m.focused = focusScopePicker
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.scopeCreateInput.Value())
		if name == "" {
			m.statusMsg, m.statusIsError = "Scope name cannot be empty", true
			return m, nil
		}
		m.scopeCreateInput.Blur()
		m.showScopeCreatePrompt = false
		m.focused = focusScopePicker
		mutation := ScopeMutation{Kind: ScopeMutationCreate, Name: name}
		return m, runScopeMutationCmd(mutation, false, func(ctx context.Context) error {
			if m.runtimeServices.Scopes.Mutate != nil {
				return m.runtimeServices.Scopes.Mutate(ctx, mutation)
			}
			return m.runtimeServices.Scopes.Create(ctx, name)
		})
	default:
		var cmd tea.Cmd
		m.scopeCreateInput, cmd = m.scopeCreateInput.Update(msg)
		return m, cmd
	}
}

func (m *Model) handleBacklogKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	if m.backlog.Searching() {
		oldFilter := m.backlog.Filter()
		switch msg.String() {
		case "enter":
			m.backlog.EndSearch()
		case "esc":
			m.backlog.EndSearch()
		case "backspace":
			m.backlog.Backspace()
		default:
			if len(msg.Runes) > 0 {
				m.backlog.AddFilter(string(msg.Runes))
			}
		}
		if oldFilter != m.backlog.Filter() {
			m.backlog.ResetPagination()
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{Filter: m.backlog.Filter(), Limit: backlogPageSize}, 0, m.backlogPageGeneration)
		}
		return m, nil
	}
	switch msg.String() {
	case "esc", "q", "B":
		m.closeBacklog()
	case "j", "down":
		m.backlog.Move(1)
	case "k", "up":
		m.backlog.Move(-1)
	case "/":
		m.backlog.BeginSearch()
	case "n", "right":
		if cursor := m.backlog.NextPageCursor(); cursor != "" {
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{Filter: m.backlog.Filter(), Cursor: cursor, Limit: backlogPageSize}, m.backlog.PageIndex()+1, m.backlogPageGeneration)
		}
	case "p", "left":
		if m.backlog.PageIndex() > 0 {
			cursor := m.backlog.PreviousPageCursor()
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{Filter: m.backlog.Filter(), Cursor: cursor, Limit: backlogPageSize}, m.backlog.PageIndex(), m.backlogPageGeneration)
		}
	case "A":
		return m, m.startScopeMutation("add")
	}
	return m, nil
}

func (m *Model) startScopeMutation(action string) tea.Cmd {
	if m.activeScope == nil {
		m.statusMsg, m.statusIsError = "No active scope; press W to activate one", true
		return nil
	}
	issueID := ""
	if m.isBacklogView {
		if issue := m.backlog.CurrentIssue(); issue != nil {
			issueID = issue.ID
		}
	} else if action == "move" {
		if issue, ok := m.selectedVisibleScopeIssue(); ok {
			issueID = issue.ID
		}
	} else {
		issueID = m.selectedListIssueID(m.list.FilterState() != list.Unfiltered, m.list.FilterInput.Value())
	}
	if issueID == "" {
		m.statusMsg, m.statusIsError = "No bead selected", true
		return nil
	}
	service := m.runtimeServices.Scopes
	switch action {
	case "add":
		if service.Mutate == nil && service.Add == nil {
			m.statusMsg, m.statusIsError = "Scope add is unavailable", true
			return nil
		}
		mutation := ScopeMutation{Kind: ScopeMutationAdd, ScopeID: m.activeScope.ID, IssueIDs: []string{issueID}}
		return runScopeMutationCmd(mutation, false, func(ctx context.Context) error {
			if service.Mutate != nil {
				return service.Mutate(ctx, mutation)
			}
			return service.Add(ctx, issueID, m.activeScope.ID)
		})
	case "remove":
		if service.Mutate == nil && service.Remove == nil {
			m.statusMsg, m.statusIsError = "Scope remove is unavailable", true
			return nil
		}
		mutation := ScopeMutation{Kind: ScopeMutationRemove, ScopeID: m.activeScope.ID, IssueIDs: []string{issueID}}
		return runScopeMutationCmd(mutation, false, func(ctx context.Context) error {
			if service.Mutate != nil {
				return service.Mutate(ctx, mutation)
			}
			return service.Remove(ctx, issueID, m.activeScope.ID)
		})
	case "move":
		return m.openScopePicker(issueID)
	}
	return nil
}

// selectedVisibleScopeIssue accepts only the bead currently represented by a
// visible List row (or the bead currently shown in Detail). It intentionally
// does not use selectedListIssueID: that helper can retain a pending async
// filter selection after the row has disappeared.
func (m *Model) selectedVisibleScopeIssue() (model.Issue, bool) {
	if m.focused == focusDetail && m.insightsDetailID != "" {
		issue := m.issueMap[m.insightsDetailID]
		if issue == nil || !m.issueMatchesRepositoryScope(*issue) {
			return model.Issue{}, false
		}
		return *issue, true
	}
	if m.focused != focusList && m.focused != focusDetail {
		return model.Issue{}, false
	}
	selected, ok := m.list.SelectedItem().(IssueItem)
	if !ok || selected.Issue.ID == "" {
		return model.Issue{}, false
	}
	visible := false
	for _, raw := range m.list.VisibleItems() {
		if item, ok := raw.(IssueItem); ok && item.Issue.ID == selected.Issue.ID {
			visible = true
			break
		}
	}
	if !visible || !m.issueMatchesRepositoryScope(selected.Issue) {
		return model.Issue{}, false
	}
	if issue := m.issueMap[selected.Issue.ID]; issue != nil {
		return *issue, true
	}
	return selected.Issue, true
}

func (m *Model) scopeMoveTargetTitle(issueID string) string {
	if issue := m.issueMap[issueID]; issue != nil && issue.Title != "" {
		return issue.Title
	}
	for _, issue := range m.issues {
		if issue.ID == issueID && issue.Title != "" {
			return issue.Title
		}
	}
	return issueID
}

func (m *Model) refreshAfterScopeMutation(mutation ScopeMutation) tea.Cmd {
	cmds := []tea.Cmd{loadScopeSnapshotCmd(m.runtimeServices.Scopes)}
	if mutation.Kind == ScopeMutationRemove && m.runtimeServices.Scopes.LoadDetails != nil && mutation.ScopeID != "" {
		cmds = append(cmds, loadScopeDetailsCmd(m.runtimeServices.Scopes, mutation.ScopeID))
	}
	if m.backgroundWorker != nil {
		m.backgroundWorker.ForceSourceRefresh()
		cmds = append(cmds, WaitForBackgroundWorkerMsgCmd(m.backgroundWorker))
	} else if m.beadsPath != "" {
		cmds = append(cmds, func() tea.Msg { return FileChangedMsg{refreshBDExport: true} })
	}
	if m.isBacklogView {
		m.backlogLoading = true
		m.backlogPageGeneration++
		cmds = append(cmds, loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{
			Filter: m.backlog.Filter(),
			Cursor: m.backlog.CurrentPageCursor(),
			Limit:  backlogPageSize,
		}, m.backlog.PageIndex(), m.backlogPageGeneration))
	}
	return tea.Batch(cmds...)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
