package ui

import (
	"bytes"
	"context"
	"fmt"
	"sort"
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
	EpicID        string
	Label         string
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
	// MutateMatching applies one epic- or label-selected scope operation.
	MutateMatching func(context.Context, ScopeMutation) error
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
	details    ScopeDetails
	scopeID    string
	generation uint64
	err        error
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

func loadScopeDetailsCmd(service ScopeServices, scopeID string, generations ...uint64) tea.Cmd {
	var generation uint64
	if len(generations) > 0 {
		generation = generations[0]
	}
	return func() tea.Msg {
		if service.LoadDetails == nil {
			return scopeDetailsMsg{scopeID: scopeID, generation: generation}
		}
		details, err := service.LoadDetails(context.Background(), scopeID)
		return scopeDetailsMsg{details: details, scopeID: scopeID, generation: generation, err: err}
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

func isBacklogReloadNotice(status string) bool {
	return status == "Refreshing…" || strings.HasPrefix(status, "Reloaded ")
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
	previewOffset int
	width         int
	height        int
	theme         Theme
	delegate      IssueDelegate
	marked        map[string]bool
}

func NewBacklogModel(theme Theme) BacklogModel {
	return BacklogModel{theme: theme, pageCursors: []string{""}, delegate: IssueDelegate{Theme: theme, useFullWidth: true}}
}

func (b *BacklogModel) SetSize(width, height int) {
	b.width, b.height = width, height
}

func (b *BacklogModel) SetPage(page BacklogPage, index int) {
	b.ClearMarks()
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
	b.previewOffset = 0
	b.ClearMarks()
}

func (b *BacklogModel) ResetPagination() {
	b.pageIndex = 0
	b.nextCursor = ""
	b.hasMore = false
	b.pageCursors = []string{""}
}

// ToggleMark marks only the current row on the loaded page. Marks never cross
// page or query boundaries; the owning Model clears them before those changes.
func (b *BacklogModel) ToggleMark() {
	issue := b.CurrentIssue()
	if issue == nil {
		return
	}
	if b.marked == nil {
		b.marked = make(map[string]bool)
	}
	b.marked[issue.ID] = !b.marked[issue.ID]
}

func (b *BacklogModel) ClearMarks() { b.marked = nil }

func (b BacklogModel) MarkedIDs() []string {
	ids := make([]string, 0, len(b.marked))
	for _, item := range b.filteredItems {
		if b.marked[item.Issue.ID] {
			ids = append(ids, item.Issue.ID)
		}
	}
	return ids
}

func (b BacklogModel) MarkCount() int { return len(b.MarkedIDs()) }

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
	b.previewOffset = 0
}

// ScrollPreview moves through the complete selected-issue preview without
// changing the page-local list selection.
func (b *BacklogModel) ScrollPreview(delta int) {
	b.previewOffset += delta
	if b.previewOffset < 0 {
		b.previewOffset = 0
	}
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
	b.previewOffset = 0
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
	wideWidth := maxInt(contentWidth*2/3, 1)
	columns := backlogTableColumnsFor(b.filteredItems, contentWidth)
	naturalTableWidth := backlogTableWidth(columns)
	wide := b.CurrentIssue() != nil && naturalTableWidth <= wideWidth
	listWidth := contentWidth
	if wide {
		// Keep the table at its measured width so the preview gets the rest of
		// the pane, while the existing two-thirds fit decision remains intact.
		listWidth = naturalTableWidth
	}
	columns.width = listWidth

	lines := []string{b.renderBacklogHeader(title, columns)}
	availableHeight := maxInt(b.height-2, 1)
	listRows := availableHeight - 5 // header, preview, page hint, and padding
	if !wide && b.CurrentIssue() == nil {
		listRows++
	}
	if listRows < 1 {
		listRows = 1
	}
	listView := b.renderBacklogList(columns, listWidth, listRows)
	page := b.renderBacklogPage(contentWidth)
	if wide {
		previewWidth := maxInt(contentWidth-listWidth-2, 1)
		preview := b.renderBacklogPreview(previewWidth, listRows)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, listView, "  ", preview))
	} else {
		lines = append(lines, listView)
		if b.CurrentIssue() != nil {
			lines = append(lines, b.renderBacklogPreview(contentWidth, 2))
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

type backlogTableColumns struct {
	width         int
	idWidth       int
	typeWidth     int
	priorityWidth int
	statusWidth   int
	createdWidth  int
}

// backlogTableColumnsFor keeps the bounded backlog projection independent of
// the ordinary List metadata layout.
func backlogTableColumnsFor(items []IssueItem, width int) backlogTableColumns {
	columns := backlogTableColumns{
		width:         maxInt(width, 1),
		idWidth:       len("ID"),
		typeWidth:     len("TYPE"),
		priorityWidth: len("PR"),
		statusWidth:   len("STAT"),
		createdWidth:  len("CREATED_AT"),
	}
	for _, item := range items {
		columns.idWidth = maxInt(columns.idWidth, lipgloss.Width(item.Issue.ID))
		columns.typeWidth = maxInt(columns.typeWidth, lipgloss.Width(string(item.Issue.IssueType)))
		columns.priorityWidth = maxInt(columns.priorityWidth, lipgloss.Width(fmt.Sprintf("P%d", item.Issue.Priority)))
		columns.statusWidth = maxInt(columns.statusWidth, lipgloss.Width(strings.ToUpper(string(item.Issue.Status))))
		columns.createdWidth = maxInt(columns.createdWidth, lipgloss.Width(formatBacklogCreatedAt(item.Issue.CreatedAt)))
	}
	return columns
}

func formatBacklogCreatedAt(createdAt time.Time) string {
	if createdAt.IsZero() {
		return "n/a"
	}
	return createdAt.Format(time.RFC3339Nano)
}

func (b BacklogModel) renderBacklogHeader(title string, columns backlogTableColumns) string {
	width := maxInt(columns.width, 1)
	titleStyle := b.theme.Renderer.NewStyle().Foreground(b.theme.Primary).Bold(true).Width(width).MaxWidth(width)
	// Keep the global-backlog column labels bright against the dark header fill.
	tableStyle := b.theme.Renderer.NewStyle().Background(b.theme.Primary).
		Foreground(ThemeFg("#FFFFFF")).Bold(true).Inline(true).
		Width(width).MaxWidth(width)
	return titleStyle.Render(title) + "\n" + tableStyle.Render(renderBacklogTableHeader(columns))
}

const backlogMarkPrefix = "  "

func renderBacklogTableHeader(columns backlogTableColumns) string {
	return backlogMarkPrefix + strings.Join([]string{
		padRight("ID", columns.idWidth),
		padRight("TYPE", columns.typeWidth),
		padRight("PR", columns.priorityWidth),
		padRight("STAT", columns.statusWidth),
		padRight("CREATED_AT", columns.createdWidth),
	}, " ")
}

func backlogTableWidth(columns backlogTableColumns) int {
	return lipgloss.Width(renderBacklogTableHeader(columns))
}

func (b BacklogModel) renderBacklogList(columns backlogTableColumns, width, rows int) string {
	if len(b.filteredItems) == 0 {
		return b.theme.Renderer.NewStyle().Foreground(b.theme.Subtext).Render("No unscoped beads.")
	}
	start, end := b.visibleRangeFor(rows)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		item := b.filteredItems[index]
		item.Marked = b.marked[item.Issue.ID]
		lines = append(lines, b.renderBacklogRow(item, index == b.selected, columns, width))
	}
	return strings.Join(lines, "\n")
}

func (b BacklogModel) renderBacklogRow(item IssueItem, selected bool, columns backlogTableColumns, width int) string {
	row := strings.Join([]string{
		padRight(item.Issue.ID, columns.idWidth),
		padRight(string(item.Issue.IssueType), columns.typeWidth),
		padRight(fmt.Sprintf("P%d", item.Issue.Priority), columns.priorityWidth),
		padRight(strings.ToUpper(string(item.Issue.Status)), columns.statusWidth),
		padRight(formatBacklogCreatedAt(item.Issue.CreatedAt), columns.createdWidth),
	}, " ")
	marker := backlogMarkPrefix
	if item.Marked {
		marker = "✓ "
	}
	row = marker + row
	if selected {
		return b.theme.Renderer.NewStyle().Background(b.theme.Highlight).Bold(true).Width(width).MaxWidth(width).Render(row)
	}
	return b.theme.Renderer.NewStyle().Width(width).MaxWidth(width).Render(row)
}

func (b BacklogModel) renderBacklogPreview(width int, heights ...int) string {
	issue := b.CurrentIssue()
	if issue == nil {
		return ""
	}
	title := issue.Title
	if title == "" {
		title = "(untitled)"
	}
	description := strings.TrimSpace(issue.Description)
	if description == "" {
		description = "(no description)"
	}
	titleStyle := b.theme.Renderer.NewStyle().Foreground(b.theme.Primary).Bold(true).Width(maxInt(width, 1))
	descriptionStyle := b.theme.Renderer.NewStyle().Foreground(b.theme.Subtext).Width(maxInt(width, 1))
	content := titleStyle.Render("TITLE  "+title) + "\n\n" + descriptionStyle.Render("DESCRIPTION  "+description)
	if len(heights) == 0 || heights[0] <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	height := heights[0]
	if height >= len(lines) {
		return content
	}
	maxOffset := len(lines) - height
	offset := min(maxInt(b.previewOffset, 0), maxOffset)
	return strings.Join(lines[offset:offset+height], "\n")
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
	if count := b.MarkCount(); count > 0 {
		page += fmt.Sprintf(" · %d marked", count)
	}
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

// ScopePickerModel owns the small named-scope catalog and one bounded member
// projection. Member filters only change presentation; details are still
// loaded exactly once per selected scope and are never paginated or cached.
type ScopePickerModel struct {
	scopes     []ScopeInfo
	selected   int
	moveTarget string

	members          []IssueItem
	filteredMembers  []IssueItem
	memberSelected   int
	memberScopeID    string
	memberGeneration uint64
	memberLoading    bool
	memberError      string
	memberFocused    bool

	memberStatusFilter     string
	memberRepositoryFilter string
	memberTypeFilter       model.IssueType
	memberReadyIDs         map[string]bool
	memberMarkedIDs        map[string]bool

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

func newScopeMatchInput(theme Theme) textinput.Model {
	input := textinput.New()
	input.Placeholder = "label:team or epic:epic-1"
	input.CharLimit = 100
	input.Width = 40
	input.Prompt = "Match: "
	input.PromptStyle = lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(theme.Base.GetForeground())
	input.Blur()
	return input
}

func (s *ScopePickerModel) SetSize(width, height int) { s.width, s.height = width, height }
func (s *ScopePickerModel) SetScopes(scopes []ScopeInfo) {
	selectedID := ""
	if selected := s.Selected(); selected != nil {
		selectedID = selected.ID
	}
	s.scopes = append([]ScopeInfo(nil), scopes...)
	s.selected = -1
	selectedFound := false
	if selectedID != "" {
		for i := range s.scopes {
			if s.scopes[i].ID == selectedID {
				s.selected = i
				selectedFound = true
				break
			}
		}
	}
	if s.selected < 0 {
		s.selected = 0
	}
	for i := range s.scopes {
		if !selectedFound && s.scopes[i].Active {
			s.selected = i
			break
		}
	}
	if s.selected >= len(s.scopes) {
		s.selected = maxInt(0, len(s.scopes)-1)
	}
}

// SelectedScopeID returns the catalog identity whose members should be shown.
func (s ScopePickerModel) SelectedScopeID() string {
	if selected := s.Selected(); selected != nil {
		return selected.ID
	}
	return ""
}

// BeginMemberLoad marks a new exact-scope request. The generation makes a
// late response harmless when the catalog cursor has already moved elsewhere.
func (s *ScopePickerModel) BeginMemberLoad(scopeID string) uint64 {
	s.memberGeneration++
	s.memberScopeID = scopeID
	s.memberLoading = true
	s.memberError = ""
	s.members = nil
	s.filteredMembers = nil
	s.memberReadyIDs = nil
	s.memberSelected = 0
	s.ClearMemberMarks()
	return s.memberGeneration
}

func (s ScopePickerModel) acceptsMemberDetails(scopeID string, generation uint64) bool {
	return generation > 0 && generation == s.memberGeneration && scopeID == s.memberScopeID
}

func (s *ScopePickerModel) SetMemberError(scopeID string, generation uint64, err error) bool {
	if !s.acceptsMemberDetails(scopeID, generation) {
		return false
	}
	s.memberLoading = false
	if err != nil {
		s.memberError = err.Error()
	}
	return true
}

// SetMemberReadyIDs supplies the already-known ready projection without
// expanding the picker into an issue graph or detail view.
func (s *ScopePickerModel) SetMemberReadyIDs(ids map[string]bool) {
	s.memberReadyIDs = make(map[string]bool, len(ids))
	for id, ready := range ids {
		if ready {
			s.memberReadyIDs[id] = true
		}
	}
}

// SetMembers replaces the bounded member projection and reapplies only the
// display narrowing owned by this picker.
func (s *ScopePickerModel) SetMembers(items []IssueItem) {
	s.members = append([]IssueItem(nil), items...)
	s.memberLoading = false
	s.memberError = ""
	s.applyMemberFilters()
}

func (s *ScopePickerModel) SetMemberFilters(repository, status string, issueType model.IssueType) {
	s.memberRepositoryFilter = repository
	s.memberStatusFilter = status
	s.memberTypeFilter = issueType
	s.ClearMemberMarks()
	s.applyMemberFilters()
}

func (s ScopePickerModel) MemberFocused() bool { return s.memberFocused }

func (s *ScopePickerModel) MoveMember(delta int) {
	if len(s.filteredMembers) == 0 {
		return
	}
	s.memberSelected = (s.memberSelected + delta + len(s.filteredMembers)) % len(s.filteredMembers)
}

func (s ScopePickerModel) SelectedMember() *IssueItem {
	if s.memberSelected < 0 || s.memberSelected >= len(s.filteredMembers) {
		return nil
	}
	selected := s.filteredMembers[s.memberSelected]
	return &selected
}

func (s *ScopePickerModel) ToggleMemberMark() {
	member := s.SelectedMember()
	if member == nil {
		return
	}
	if s.memberMarkedIDs == nil {
		s.memberMarkedIDs = make(map[string]bool)
	}
	s.memberMarkedIDs[member.Issue.ID] = !s.memberMarkedIDs[member.Issue.ID]
}

func (s *ScopePickerModel) ClearMemberMarks() { s.memberMarkedIDs = nil }

func (s ScopePickerModel) MarkedMemberIDs() []string {
	ids := make([]string, 0, len(s.memberMarkedIDs))
	for _, item := range s.filteredMembers {
		if s.memberMarkedIDs[item.Issue.ID] {
			ids = append(ids, item.Issue.ID)
		}
	}
	return ids
}

func (s ScopePickerModel) MemberMarkCount() int { return len(s.MarkedMemberIDs()) }

func (s *ScopePickerModel) CycleMemberRepository() {
	values := s.memberRepositoryValues()
	s.memberRepositoryFilter = cycleStringFilter(s.memberRepositoryFilter, values)
	s.ClearMemberMarks()
	s.applyMemberFilters()
}

func (s *ScopePickerModel) ToggleMemberStatus(status string) {
	if s.memberStatusFilter == status {
		s.memberStatusFilter = ""
	} else {
		s.memberStatusFilter = status
	}
	s.ClearMemberMarks()
	s.applyMemberFilters()
}

func (s *ScopePickerModel) CycleMemberType() {
	values := make([]string, 0)
	seen := make(map[model.IssueType]bool)
	for _, item := range s.members {
		if item.Issue.IssueType != "" && !seen[item.Issue.IssueType] {
			seen[item.Issue.IssueType] = true
			values = append(values, string(item.Issue.IssueType))
		}
	}
	sort.Strings(values)
	current := string(s.memberTypeFilter)
	next := cycleStringFilter(current, values)
	s.memberTypeFilter = model.IssueType(next)
	s.ClearMemberMarks()
	s.applyMemberFilters()
}

func (s ScopePickerModel) memberRepositoryValues() []string {
	seen := make(map[string]bool)
	for _, item := range s.members {
		if value := memberRepositoryValue(item); value != "" {
			seen[value] = true
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func cycleStringFilter(current string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	if current == "" {
		return values[0]
	}
	for i, value := range values {
		if value == current {
			if i+1 < len(values) {
				return values[i+1]
			}
			return ""
		}
	}
	return values[0]
}

func memberRepositoryValue(item IssueItem) string {
	if item.RepositoryName != "" {
		return item.RepositoryName
	}
	if item.RepositoryID != "" {
		return item.RepositoryID
	}
	if item.RepoPrefix != "" {
		return item.RepoPrefix
	}
	return item.Issue.SourceRepo
}

func (s *ScopePickerModel) applyMemberFilters() {
	s.filteredMembers = s.filteredMembers[:0]
	for _, item := range s.members {
		if s.memberRepositoryFilter != "" && memberRepositoryValue(item) != s.memberRepositoryFilter {
			continue
		}
		if s.memberTypeFilter != "" && item.Issue.IssueType != s.memberTypeFilter {
			continue
		}
		switch s.memberStatusFilter {
		case "open":
			if isClosedLikeStatus(item.Issue.Status) {
				continue
			}
		case "closed":
			if !isClosedLikeStatus(item.Issue.Status) {
				continue
			}
		case "ready":
			ready := s.memberReadyIDs[item.Issue.ID]
			if s.memberReadyIDs == nil {
				ready = isIssueReadyAt(item.Issue, nil, time.Now())
			}
			if !ready {
				continue
			}
		}
		s.filteredMembers = append(s.filteredMembers, item)
	}
	if s.memberSelected >= len(s.filteredMembers) {
		s.memberSelected = maxInt(0, len(s.filteredMembers)-1)
	}
}

func (s *ScopePickerModel) SetDetails(details ScopeDetails) {
	s.SetMembers(scopeDetailItems(details))
}

func scopeDetailItems(details ScopeDetails) []IssueItem {
	issues := details.Issues
	if len(details.MemberIDs) == 0 {
		items := make([]IssueItem, len(issues))
		for i, issue := range issues {
			items[i] = IssueItem{Issue: issue, RepoPrefix: issueRepoKey(issue)}
		}
		return items
	}
	byID := make(map[string]model.Issue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	items := make([]IssueItem, 0, len(details.MemberIDs))
	for _, id := range details.MemberIDs {
		if issue, ok := byID[id]; ok {
			items = append(items, IssueItem{Issue: issue, RepoPrefix: issueRepoKey(issue)})
		}
	}
	return items
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
	width, height := s.width, s.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}
	heading := "Scopes"
	if s.moveTarget != "" {
		heading = "Move: " + s.moveTarget
	}
	contentWidth := maxInt(width-4, 1)
	contentHeight := maxInt(height-4, 3)
	catalogRows := maxInt(3, contentHeight/2)
	memberRows := contentHeight - catalogRows - 2
	if memberRows < 2 {
		memberRows = 2
		catalogRows = maxInt(1, contentHeight-memberRows-2)
	}

	catalog := s.renderCatalog(heading, contentWidth, catalogRows)
	members := s.renderMembers(contentWidth, memberRows)
	view := catalog + "\n\n" + members
	return lipgloss.NewStyle().
		Width(maxInt(width, 1)).
		Height(maxInt(height-2, 1)).
		Padding(1, 2).
		Render(view)
}

func (s ScopePickerModel) renderCatalog(heading string, width, rows int) string {
	title := s.theme.Renderer.NewStyle().Foreground(s.theme.Primary).Bold(true).Render(heading)
	lines := []string{title, ""}
	if len(s.scopes) == 0 {
		lines = append(lines, "No scopes available.")
	} else {
		start := 0
		visible := maxInt(rows-2, 1)
		if s.selected >= visible {
			start = s.selected - visible + 1
		}
		end := min(len(s.scopes), start+visible)
		for i := start; i < end; i++ {
			prefix := "  "
			if i == s.selected {
				prefix = "> "
			}
			active := ""
			if s.scopes[i].Active {
				active = "  (active)"
			}
			lines = append(lines, fmt.Sprintf("%s%s · %s/%d%s", prefix, s.scopes[i].Name, s.scopes[i].CreatedAt.Format("2006-01-02"), s.scopes[i].MemberCount, active))
		}
	}
	return lipgloss.NewStyle().Width(width).Height(maxInt(rows, 1)).Render(strings.Join(lines, "\n"))
}

func (s ScopePickerModel) renderMembers(width, rows int) string {
	selected := s.Selected()
	name := "none"
	if selected != nil {
		name = selected.Name
	}
	header := s.theme.Renderer.NewStyle().Foreground(s.theme.Primary).Bold(true).Render("Members · " + name)
	if s.memberLoading {
		return header + "\n" + s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).Render("Loading members…")
	}
	if s.memberError != "" {
		return header + "\n" + s.theme.Renderer.NewStyle().Foreground(s.theme.Blocked).Render("Members unavailable: "+s.memberError)
	}
	filter := fmt.Sprintf("repository:%s · status:%s · type:%s", memberFilterLabel(s.memberRepositoryFilter), memberFilterLabel(s.memberStatusFilter), memberFilterLabel(string(s.memberTypeFilter)))
	filterLine := s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).Render(filter)
	if len(s.filteredMembers) == 0 {
		return header + "\n" + filterLine + "\n" + s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).Render("No members match.")
	}
	items := make([]list.Item, len(s.filteredMembers))
	showRepositories := false
	workspaceMode := false
	repositoryExtraWidth := 0
	for i, item := range s.filteredMembers {
		item.Marked = s.memberMarkedIDs[item.Issue.ID]
		items[i] = item
		showRepositories = showRepositories || item.HubPresentation
		workspaceMode = workspaceMode || item.RepoPrefix != ""
		if item.RepositoryExtra > 0 {
			repositoryExtraWidth = maxInt(repositoryExtraWidth, lipgloss.Width(fmt.Sprintf("+%d", item.RepositoryExtra)))
		}
	}
	delegate := IssueDelegate{Theme: s.theme, ShowRepositories: showRepositories, WorkspaceMode: workspaceMode, HideAssignee: true, useFullWidth: true, layoutItems: items}
	delegate.RepositoryNameWidth = 12
	delegate.RepositoryExtraWidth = repositoryExtraWidth
	delegate.columns = delegate.issueListColumnsFor(items, width)
	l := list.New(items, delegate, width, maxInt(rows-2, 1))
	l.Select(s.memberSelected)
	lines := []string{header, filterLine}
	visible := maxInt(rows-2, 1)
	start := 0
	if s.memberSelected >= visible {
		start = s.memberSelected - visible + 1
	}
	end := min(len(items), start+visible)
	for i := start; i < end; i++ {
		var row bytes.Buffer
		delegate.Render(&row, l, i, items[i])
		lines = append(lines, row.String())
	}
	return lipgloss.NewStyle().Width(width).Height(maxInt(rows, 1)).Render(strings.Join(lines, "\n"))
}

func memberFilterLabel(value string) string {
	if value == "" {
		return "all"
	}
	return value
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

func (m Model) renderScopeMatchPrompt() string {
	availableWidth := m.mainContentWidth()
	boxStyle := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Primary).
		Padding(1, 3).
		Align(lipgloss.Center)
	muted := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	content := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary).Bold(true).Render("Scope match") + "\n\n" +
		muted.Render("Enter label:name or epic:id") + "\n\n" +
		m.scopeMatchInput.View() + "\n\n" +
		muted.Render("Enter apply · Esc cancel")
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
	m.scopePicker.memberFocused = false
	m.focused = focusScopePicker
	m.scopePicker.SetScopes(m.scopeCatalog)
	if moveIssue == "" {
		status := m.activeStatusFilter()
		issueType := model.IssueType("")
		if len(m.activeIssueTypes) == 1 {
			for value := range m.activeIssueTypes {
				issueType = value
			}
		}
		m.scopePicker.SetMemberFilters("", status, issueType)
	}
	var cmds []tea.Cmd
	if m.runtimeServices.Scopes.Load != nil {
		cmds = append(cmds, loadScopeSnapshotCmd(m.runtimeServices.Scopes))
	}
	if details := m.loadSelectedScopeDetails(); details != nil {
		cmds = append(cmds, details)
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	if len(cmds) > 1 {
		return tea.Batch(cmds...)
	}
	return nil
}

func (m *Model) loadSelectedScopeDetails() tea.Cmd {
	if m.runtimeServices.Scopes.LoadDetails == nil {
		return nil
	}
	scopeID := m.scopePicker.SelectedScopeID()
	if scopeID == "" {
		return nil
	}
	generation := m.scopePicker.BeginMemberLoad(scopeID)
	return loadScopeDetailsCmd(m.runtimeServices.Scopes, scopeID, generation)
}

func (m *Model) applyScopePickerDetails(details ScopeDetails) {
	if len(details.Issues) == 0 && len(details.MemberIDs) > 0 {
		details.Issues = make([]model.Issue, 0, len(details.MemberIDs))
		for _, id := range details.MemberIDs {
			if issue := m.issueMap[id]; issue != nil {
				details.Issues = append(details.Issues, *issue)
			}
		}
		details.MemberIDs = nil
	}
	items := scopeDetailItems(details)
	ready := make(map[string]bool)
	for i := range items {
		m.decorateIssueItem(&items[i])
		ready[items[i].Issue.ID] = isIssueReadyAt(items[i].Issue, m.issueMap, time.Now())
	}
	m.scopePicker.SetMemberReadyIDs(ready)
	m.scopePicker.SetMembers(items)
}

func (m *Model) closeScopePicker() {
	m.showScopePicker = false
	m.scopePickerMoveIssue = ""
	m.scopePicker.SetMoveTarget("")
	m.scopePicker.memberFocused = false
	m.scopePicker.ClearMemberMarks()
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
	m.backlog.ClearMarks()
}

func (m *Model) handleScopePickerKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.closeScopePicker()
		return m, nil
	case "j", "down":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.MoveMember(1)
			break
		}
		before := m.scopePicker.SelectedScopeID()
		m.scopePicker.Move(1)
		if before != m.scopePicker.SelectedScopeID() {
			m.scopePicker.ClearMemberMarks()
			return m, m.loadSelectedScopeDetails()
		}
	case "k", "up":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.MoveMember(-1)
			break
		}
		before := m.scopePicker.SelectedScopeID()
		m.scopePicker.Move(-1)
		if before != m.scopePicker.SelectedScopeID() {
			m.scopePicker.ClearMemberMarks()
			return m, m.loadSelectedScopeDetails()
		}
	case "tab":
		m.scopePicker.memberFocused = !m.scopePicker.memberFocused
	case "w":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.CycleMemberRepository()
		}
	case "o", "c", "r":
		if m.scopePicker.MemberFocused() {
			status := map[string]string{"o": "open", "c": "closed", "r": "ready"}[msg.String()]
			m.scopePicker.ToggleMemberStatus(status)
		}
	case "I":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.CycleMemberType()
		}
	// Bubble Tea reports a physical space as either a space rune or "space".
	case " ", "space":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.ToggleMemberMark()
		}
	case "R":
		if m.scopePicker.MemberFocused() {
			return m, m.startScopeMemberRemove()
		}
	case "M":
		if m.scopePicker.MemberFocused() {
			return m, m.beginScopeMatchMutation("remove")
		}
	case "enter":
		if m.scopePicker.MemberFocused() {
			return m, nil
		}
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
	// Local backlog navigation dismisses only reload feedback so the backlog
	// controls can reappear; action results and errors remain visible.
	if !m.statusIsError && isBacklogReloadNotice(m.statusMsg) {
		m.statusMsg = ""
	}
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
			m.backlog.ClearMarks()
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
	case "pgdown", "ctrl+f":
		m.backlog.ScrollPreview(1)
	case "pgup", "ctrl+b":
		m.backlog.ScrollPreview(-1)
	case "/":
		m.backlog.BeginSearch()
		m.backlog.ClearMarks()
	case " ", "space":
		m.backlog.ToggleMark()
	case "n", "right":
		if cursor := m.backlog.NextPageCursor(); cursor != "" {
			m.backlog.ClearMarks()
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{Filter: m.backlog.Filter(), Cursor: cursor, Limit: backlogPageSize}, m.backlog.PageIndex()+1, m.backlogPageGeneration)
		}
	case "p", "left":
		if m.backlog.PageIndex() > 0 {
			m.backlog.ClearMarks()
			cursor := m.backlog.PreviousPageCursor()
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, BacklogQuery{Filter: m.backlog.Filter(), Cursor: cursor, Limit: backlogPageSize}, m.backlog.PageIndex(), m.backlogPageGeneration)
		}
	case "A":
		return m, m.startScopeMutation("add")
	case "M":
		return m, m.beginScopeMatchMutation("add")
	}
	return m, nil
}

func (m *Model) startScopeMutation(action string) tea.Cmd {
	if m.activeScope == nil {
		m.statusMsg, m.statusIsError = "No active scope; press W to activate one", true
		return nil
	}
	if m.isBacklogView && action == "add" {
		if ids := m.backlog.MarkedIDs(); len(ids) > 0 {
			return m.startScopeMembershipMutation(action, ids, m.activeScope.ID)
		}
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

func (m *Model) startScopeMemberRemove() tea.Cmd {
	scopeID := m.scopePicker.SelectedScopeID()
	if scopeID == "" {
		m.statusMsg, m.statusIsError = "No scope selected", true
		return nil
	}
	ids := m.scopePicker.MarkedMemberIDs()
	if len(ids) == 0 {
		if member := m.scopePicker.SelectedMember(); member != nil {
			ids = []string{member.Issue.ID}
		}
	}
	if len(ids) == 0 {
		m.statusMsg, m.statusIsError = "No member selected", true
		return nil
	}
	return m.startScopeMembershipMutation("remove", ids, scopeID)
}

func (m *Model) startScopeMembershipMutation(action string, ids []string, scopeID string) tea.Cmd {
	service := m.runtimeServices.Scopes
	if service.Mutate == nil {
		if len(ids) > 1 {
			m.statusMsg, m.statusIsError = "Batch scope mutation is unavailable", true
			return nil
		}
		if action == "add" && service.Add == nil || action == "remove" && service.Remove == nil {
			m.statusMsg, m.statusIsError = "Scope "+action+" is unavailable", true
			return nil
		}
	}
	mutation := ScopeMutation{ScopeID: scopeID, IssueIDs: append([]string(nil), ids...)}
	if action == "add" {
		mutation.Kind = ScopeMutationAdd
	} else {
		mutation.Kind = ScopeMutationRemove
	}
	return runScopeMutationCmd(mutation, false, func(ctx context.Context) error {
		if service.Mutate != nil {
			return service.Mutate(ctx, mutation)
		}
		if action == "add" {
			return service.Add(ctx, ids[0], scopeID)
		}
		return service.Remove(ctx, ids[0], scopeID)
	})
}

func (m *Model) beginScopeMatchMutation(action string) tea.Cmd {
	scopeID := ""
	if action == "remove" {
		scopeID = m.scopePicker.SelectedScopeID()
		if scopeID == "" {
			m.statusMsg, m.statusIsError = "No scope selected", true
			return nil
		}
	} else if m.activeScope == nil {
		m.statusMsg, m.statusIsError = "No active scope; press W to activate one", true
		return nil
	} else {
		scopeID = m.activeScope.ID
	}
	if m.runtimeServices.Scopes.MutateMatching == nil {
		m.statusMsg, m.statusIsError = "Semantic scope mutation is unavailable", true
		return nil
	}
	m.scopeMatchAction = action
	m.scopeMatchScopeID = scopeID
	m.scopeMatchOrigin = m.focused
	m.scopeMatchInput.SetValue("")
	m.showScopeMatchPrompt = true
	m.focused = focusScopeCreateInput
	return m.scopeMatchInput.Focus()
}

func parseScopeMatch(value string) (string, string, error) {
	prefix, target, ok := strings.Cut(strings.TrimSpace(value), ":")
	target = strings.TrimSpace(target)
	if !ok || target == "" {
		return "", "", fmt.Errorf("enter label:name or epic:id")
	}
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case "label":
		if strings.HasPrefix(target, "ctx:") || strings.Contains(target, ",") {
			return "", "", fmt.Errorf("enter one ordinary label")
		}
		return "", target, nil
	case "epic":
		return target, "", nil
	default:
		return "", "", fmt.Errorf("match must start with label: or epic:")
	}
}

func (m *Model) handleScopeMatchKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.scopeMatchInput.Blur()
		m.showScopeMatchPrompt = false
		m.focused = m.scopeMatchOrigin
		return m, nil
	case "enter":
		epic, label, err := parseScopeMatch(m.scopeMatchInput.Value())
		if err != nil {
			m.statusMsg, m.statusIsError = err.Error(), true
			return m, nil
		}
		m.scopeMatchInput.Blur()
		m.showScopeMatchPrompt = false
		m.focused = m.scopeMatchOrigin
		mutation := ScopeMutation{Kind: ScopeMutationAdd, ScopeID: m.scopeMatchScopeID, EpicID: epic, Label: label}
		if m.scopeMatchAction == "remove" {
			mutation.Kind = ScopeMutationRemove
		}
		service := m.runtimeServices.Scopes
		return m, runScopeMutationCmd(mutation, false, func(ctx context.Context) error {
			return service.MutateMatching(ctx, mutation)
		})
	default:
		var cmd tea.Cmd
		m.scopeMatchInput, cmd = m.scopeMatchInput.Update(msg)
		return m, cmd
	}
}

func (m *Model) clearScopeActionMarks() {
	m.backlog.ClearMarks()
	m.scopePicker.ClearMemberMarks()
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
		if m.showScopePicker && m.scopePicker.SelectedScopeID() == mutation.ScopeID {
			cmds = append(cmds, m.loadSelectedScopeDetails())
		} else {
			cmds = append(cmds, loadScopeDetailsCmd(m.runtimeServices.Scopes, mutation.ScopeID))
		}
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
