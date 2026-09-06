package ui

import (
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
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
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

// ScopeCatalogQuery requests one bounded named-scope catalog page. Cursor is
// opaque and must only be sent back to the service that produced it.
type ScopeCatalogQuery struct {
	Cursor string
	Limit  int
}

// ScopeCatalogPage is one bounded named-scope catalog page.
type ScopeCatalogPage struct {
	Scopes     []ScopeInfo
	HasMore    bool
	NextCursor string
}

// ScopeMembersQuery requests one bounded page of members for a named scope.
// Cursor is opaque; Contexts are forwarded as repeated backend filters.
type ScopeMembersQuery struct {
	ScopeID  string
	Cursor   string
	Limit    int
	Status   string
	Type     string
	Contexts []string
}

// ScopeMembersPage is one bounded page of full member issue projections.
type ScopeMembersPage struct {
	Scope      ScopeInfo
	Members    []model.Issue
	HasMore    bool
	NextCursor string
}

// Request/response aliases keep the seam readable to callers that prefer
// protocol terminology over the existing Query/Page naming.
type ScopeCatalogRequest = ScopeCatalogQuery
type ScopeCatalogResponse = ScopeCatalogPage
type ScopeMembersRequest = ScopeMembersQuery
type ScopeMembersResponse = ScopeMembersPage

// BacklogPage is one bounded page of unscoped beads. NextCursor is opaque and
// must only be sent back to the service that produced it.
type BacklogPage struct {
	Issues     []model.Issue
	HasMore    bool
	NextCursor string
}

// BacklogQuery is the complete request for one bounded backlog page. Filter
// searches ID/title; Label and Status are backend-owned exact filters. Contexts
// and IncludeContextless are owned by the backlog picker. Cursor is opaque and
// must not be decoded or edited. An empty Status means all.
type BacklogQuery struct {
	Filter             string
	Label              string
	Status             string
	Contexts           []string
	IncludeContextless bool
	Cursor             string
	Limit              int
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
	// QueryCatalog and QueryMembers are additive paginated seams. The complete
	// Load and LoadDetails fields remain the compatibility path for current UI
	// callers.
	QueryCatalog func(context.Context, ScopeCatalogQuery) (ScopeCatalogPage, error)
	QueryMembers func(context.Context, ScopeMembersQuery) (ScopeMembersPage, error)
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

type scopeCatalogPageMsg struct {
	page       ScopeCatalogPage
	cursor     string
	index      int
	generation uint64
	err        error
}

type scopeMembersPageMsg struct {
	page       ScopeMembersPage
	scopeID    string
	cursor     string
	index      int
	generation uint64
	err        error
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

func loadScopeCatalogPageCmd(service ScopeServices, query ScopeCatalogQuery, index int, generation uint64) tea.Cmd {
	return func() tea.Msg {
		if service.QueryCatalog == nil {
			return scopeCatalogPageMsg{cursor: query.Cursor, index: index, generation: generation}
		}
		page, err := service.QueryCatalog(context.Background(), query)
		return scopeCatalogPageMsg{page: page, cursor: query.Cursor, index: index, generation: generation, err: err}
	}
}

func loadScopeMembersPageCmd(service ScopeServices, query ScopeMembersQuery, index int, generation uint64) tea.Cmd {
	return func() tea.Msg {
		if service.QueryMembers == nil {
			return scopeMembersPageMsg{scopeID: query.ScopeID, cursor: query.Cursor, index: index, generation: generation}
		}
		page, err := service.QueryMembers(context.Background(), query)
		return scopeMembersPageMsg{page: page, scopeID: query.ScopeID, cursor: query.Cursor, index: index, generation: generation, err: err}
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
const scopePageSize = 50

const backlogStatusAll = "all"

var backlogStatuses = [...]string{backlogStatusAll, "open", "in_progress", "blocked", "deferred", "closed"}

// isScopeBacklogGlobalKey leaves global controls and view jumps on the main
// Update path. Search and label input are intentionally excluded so printable
// keys remain input text.
func isScopeBacklogGlobalKey(key string) bool {
	switch key {
	case "ctrl+c", "?", "`", ";", "f2", "ctrl+j", "ctrl+k", "ctrl+r", "f5",
		"w", "W", "B", "a", "b", "g", "h", "i", "E", "f", "[", "]", "f3", "f4":
		return true
	default:
		return false
	}
}

func isScopePickerPagingKey(key string) bool {
	switch key {
	case "n", "p", "left", "right", "[", "]":
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
	issues             []model.Issue
	items              []IssueItem
	filtered           []model.Issue
	filteredItems      []IssueItem
	selected           int
	filter             string
	label              string
	status             string
	searching          bool
	labelEditing       bool
	labelInput         textinput.Model
	hasMore            bool
	nextCursor         string
	pageIndex          int
	pageCursors        []string
	previewOffset      int
	contexts           []string
	contextNames       []string
	includeContextless bool
	width              int
	height             int
	theme              Theme
	delegate           IssueDelegate
	marked             map[string]bool
}

func NewBacklogModel(theme Theme) BacklogModel {
	return BacklogModel{
		theme:       theme,
		status:      backlogStatusAll,
		pageCursors: []string{""},
		delegate:    IssueDelegate{Theme: theme, useFullWidth: true},
		labelInput:  newBacklogLabelInput(theme),
	}
}

func newBacklogLabelInput(theme Theme) textinput.Model {
	input := textinput.New()
	input.Placeholder = "ordinary label"
	input.CharLimit = 100
	input.Width = 30
	input.Prompt = "Label: "
	input.PromptStyle = lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(theme.Base.GetForeground())
	input.Blur()
	return input
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
	b.labelEditing = false
	b.labelInput.Blur()
	b.ClearMarks()
}

func (b *BacklogModel) ResetPagination() {
	b.pageIndex = 0
	b.nextCursor = ""
	b.hasMore = false
	b.pageCursors = []string{""}
}

func (b *BacklogModel) resetCursor() { b.selected, b.previewOffset = 0, 0 }

// SetContextFilter records the backlog-owned Hub context projection. It does
// not touch the generic Model scope or its active issue list.
func (b *BacklogModel) SetContextFilter(contexts []string, includeContextless bool, names []string) {
	b.contexts = append([]string(nil), contexts...)
	b.contextNames = append([]string(nil), names...)
	b.includeContextless = includeContextless
}

func (b BacklogModel) Contexts() []string { return append([]string(nil), b.contexts...) }

func (b BacklogModel) IncludeContextless() bool { return b.includeContextless }

func (b BacklogModel) contextFilterLabel() string {
	labels := append([]string(nil), b.contextNames...)
	if len(labels) != len(b.contexts) {
		labels = append([]string(nil), b.contexts...)
	}
	if b.includeContextless {
		labels = append(labels, contextlessRepositoryID)
	}
	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, ", ")
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
func (b BacklogModel) Label() string      { return b.label }
func (b BacklogModel) Status() string {
	if b.status == "" {
		return backlogStatusAll
	}
	return b.status
}
func (b BacklogModel) LabelEditing() bool { return b.labelEditing }

// BeginLabelEdit opens the single-value exact ordinary-label editor.
func (b *BacklogModel) BeginLabelEdit() tea.Cmd {
	b.labelInput.SetValue(b.label)
	b.labelEditing = true
	return b.labelInput.Focus()
}

func (b *BacklogModel) EndLabelEdit() {
	b.labelEditing = false
	b.labelInput.Blur()
}

func (b *BacklogModel) UpdateLabelInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	b.labelInput, cmd = b.labelInput.Update(msg)
	return cmd
}

func (b BacklogModel) LabelInputValue() string { return b.labelInput.Value() }

func (b *BacklogModel) SetLabel(value string) { b.label = strings.TrimSpace(value) }

func (b *BacklogModel) CancelLabelEdit() {
	b.labelInput.SetValue(b.label)
	b.EndLabelEdit()
}

func (b *BacklogModel) CycleStatus() {
	current := b.Status()
	for i, status := range backlogStatuses {
		if status == current {
			b.status = backlogStatuses[(i+1)%len(backlogStatuses)]
			return
		}
	}
	b.status = backlogStatusAll
}

func isBacklogOrdinaryLabel(value string) bool {
	label := strings.TrimSpace(value)
	return label != "" && !strings.Contains(label, ",") && !strings.HasPrefix(label, "ctx:")
}

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
	filters := []string{}
	if b.searching {
		filters = append(filters, "search: "+b.filter+"_")
	} else if strings.TrimSpace(b.filter) != "" {
		filters = append(filters, "search: "+b.filter)
	}
	if b.labelEditing {
		filters = append(filters, "label: "+b.labelInput.Value()+"_")
	} else if b.label != "" {
		filters = append(filters, "label: "+b.label)
	}
	filters = append(filters, "status: "+b.Status())
	title := "Global backlog"
	if contexts := b.contextFilterLabel(); contexts != "" {
		title += " · contexts: " + contexts
	}
	if len(filters) > 0 {
		title += " · " + strings.Join(filters, " · ")
	}
	return b.renderBacklog(title)
}

func (b BacklogModel) renderBacklog(title string) string {
	contentWidth := maxInt(b.width-4, 1)
	wideWidth := maxInt(contentWidth*2/3, 1)
	columns := backlogTableColumnsFor(b.filteredItems, contentWidth)
	naturalTableWidth := backlogTableWidth(columns)
	naturalTableWidth = maxInt(naturalTableWidth, lipgloss.Width(title))
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
	previewRows := 0
	if !wide && b.CurrentIssue() != nil {
		// Keep one table row, separator, and page hint visible at short heights.
		previewRows = min(2, maxInt(availableHeight-5, 0))
	}
	listRows := availableHeight - 4 - previewRows // header, separator, page hint, and padding
	if listRows < 1 {
		listRows = 1
	}
	listView := b.renderBacklogList(columns, listWidth, listRows)
	page := b.renderBacklogPage(listWidth)
	if wide {
		previewWidth := maxInt(contentWidth-listWidth-2, 1)
		preview := b.renderBacklogPreview(previewWidth, listRows)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, listView, "  ", preview))
	} else {
		lines = append(lines, listView)
		if previewRows > 0 {
			lines = append(lines, b.renderBacklogPreview(contentWidth, previewRows))
		}
	}
	lines = append(lines, "")
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

// Fixed cells keep later backlog columns stable when pages contain different
// ID or type lengths; values are truncated before padding to those cells.
const (
	backlogIDWidth     = 18
	backlogTypeWidth   = 10
	backlogStatusWidth = 11
)

// backlogTableColumnsFor keeps the bounded backlog projection independent of
// the ordinary List metadata layout.
func backlogTableColumnsFor(items []IssueItem, width int) backlogTableColumns {
	columns := backlogTableColumns{
		width:         maxInt(width, 1),
		idWidth:       backlogIDWidth,
		typeWidth:     backlogTypeWidth,
		priorityWidth: len("PR"),
		statusWidth:   backlogStatusWidth,
		createdWidth:  len("CREATED_AT"),
	}
	for _, item := range items {
		columns.priorityWidth = maxInt(columns.priorityWidth, lipgloss.Width(fmt.Sprintf("P%d", item.Issue.Priority)))
		columns.createdWidth = maxInt(columns.createdWidth, lipgloss.Width(formatBacklogCreatedAt(item.Issue.CreatedAt)))
	}
	return columns
}

func formatBacklogCreatedAt(createdAt time.Time) string {
	if createdAt.IsZero() {
		return "n/a"
	}
	// Backlog dates are human-facing local time, not backend/RFC3339 timestamps.
	return createdAt.In(time.Local).Format("Mon 02 Jan - 15:04")
}

func (b BacklogModel) renderBacklogHeader(title string, columns backlogTableColumns) string {
	width := maxInt(columns.width, 1)
	titleStyle := b.theme.Renderer.NewStyle().Foreground(b.theme.Primary).Bold(true).Inline(true).Width(width).MaxWidth(width)
	// Keep the global-backlog column labels bright against the dark header fill.
	tableStyle := b.theme.Renderer.NewStyle().Background(b.theme.Primary).
		Foreground(ThemeFg("#FFFFFF")).Bold(true).Inline(true).
		Width(width).MaxWidth(width)
	return titleStyle.Render(title) + "\n" + tableStyle.Render(renderBacklogTableHeader(columns))
}

const backlogMarkPrefix = "    " // independent two-cell cursor and mark slots

func renderBacklogTableHeader(columns backlogTableColumns) string {
	columns.statusWidth = backlogStatusWidth
	return backlogMarkPrefix + strings.Join([]string{
		padRight("ID", columns.idWidth),
		padRight("TYPE", columns.typeWidth),
		padRight("PR", columns.priorityWidth),
		padRight("STATUS", columns.statusWidth),
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
	columns.statusWidth = backlogStatusWidth
	id := truncateRunesHelper(item.Issue.ID, columns.idWidth, "…")
	issueType := truncateRunesHelper(string(item.Issue.IssueType), columns.typeWidth, "…")
	status := truncateRunesHelper(strings.ToUpper(string(item.Issue.Status)), columns.statusWidth, "…")
	row := strings.Join([]string{
		padRight(id, columns.idWidth),
		padRight(issueType, columns.typeWidth),
		padRight(fmt.Sprintf("P%d", item.Issue.Priority), columns.priorityWidth),
		padRight(status, columns.statusWidth),
		padRight(formatBacklogCreatedAt(item.Issue.CreatedAt), columns.createdWidth),
	}, " ")
	cursor, mark := "  ", "  "
	if selected {
		cursor = "▸ "
	}
	if item.Marked {
		mark = "✓ "
	}
	row = cursor + mark + row
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
	return b.theme.Renderer.NewStyle().Foreground(b.theme.Subtext).
		Width(maxInt(width, 1)).Align(lipgloss.Center).Render(fmt.Sprintf("page %d", b.pageIndex+1))
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

// ScopePickerModel owns the named-scope catalog and one bounded member page.
// Paged mode keeps catalog and member cursors separate; the complete loaders
// still use the same presentation path for standalone callers.
type ScopePickerModel struct {
	scopes     []ScopeInfo
	selected   int
	moveTarget string

	catalogHasMore     bool
	catalogNextCursor  string
	catalogPageIndex   int
	catalogPageCursors []string
	catalogGeneration  uint64
	catalogLoading     bool
	catalogError       string

	members               []IssueItem
	filteredMembers       []IssueItem
	memberSelected        int
	memberScopeID         string
	memberGeneration      uint64
	memberLoading         bool
	memberError           string
	memberFocused         bool
	memberHasMore         bool
	memberNextCursor      string
	memberPageIndex       int
	memberPageCursors     []string
	memberServerFiltering bool

	memberStatusFilter     string
	memberRepositoryFilter string
	memberContextFilter    []string
	memberTypeFilter       model.IssueType
	memberContextCatalog   repositorypkg.Catalog
	memberReadyIDs         map[string]bool
	memberMarkedIDs        map[string]bool

	width, height int
	theme         Theme
}

func NewScopePickerModel(theme Theme) ScopePickerModel {
	return ScopePickerModel{theme: theme, catalogPageCursors: []string{""}, memberPageCursors: []string{""}}
}

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

func (s *ScopePickerModel) BeginCatalogLoad() uint64 {
	s.catalogGeneration++
	s.catalogLoading = true
	s.catalogError = ""
	return s.catalogGeneration
}

func (s ScopePickerModel) acceptsCatalogPage(generation uint64) bool {
	return generation > 0 && generation == s.catalogGeneration
}

func (s *ScopePickerModel) SetCatalogPage(page ScopeCatalogPage, index int, generation uint64) bool {
	if !s.acceptsCatalogPage(generation) {
		return false
	}
	s.catalogLoading = false
	s.catalogError = ""
	s.catalogHasMore = page.HasMore
	s.catalogNextCursor = page.NextCursor
	s.catalogPageIndex = maxInt(0, index)
	if len(s.catalogPageCursors) == 0 {
		s.catalogPageCursors = []string{""}
	}
	if s.catalogPageIndex >= len(s.catalogPageCursors) {
		s.catalogPageCursors = append(s.catalogPageCursors, make([]string, s.catalogPageIndex-len(s.catalogPageCursors)+1)...)
	}
	s.SetScopes(page.Scopes)
	return true
}

func (s *ScopePickerModel) SetCatalogError(generation uint64, err error) bool {
	if !s.acceptsCatalogPage(generation) {
		return false
	}
	s.catalogLoading = false
	if err != nil {
		s.catalogError = err.Error()
	}
	return true
}

func (s *ScopePickerModel) NextCatalogPage() (string, int, bool) {
	if s.catalogLoading || !s.catalogHasMore || s.catalogNextCursor == "" {
		return "", 0, false
	}
	next := s.catalogPageIndex + 1
	if next < len(s.catalogPageCursors) {
		s.catalogPageCursors = s.catalogPageCursors[:next]
	}
	s.catalogPageCursors = append(s.catalogPageCursors, s.catalogNextCursor)
	s.catalogPageIndex = next
	return s.catalogNextCursor, next, true
}

func (s *ScopePickerModel) PreviousCatalogPage() (string, int, bool) {
	if s.catalogLoading || s.catalogPageIndex <= 0 || s.catalogPageIndex >= len(s.catalogPageCursors) {
		return "", 0, false
	}
	s.catalogPageIndex--
	return s.catalogPageCursors[s.catalogPageIndex], s.catalogPageIndex, true
}

func (s ScopePickerModel) CatalogPageIndex() int { return s.catalogPageIndex }
func (s ScopePickerModel) CatalogHasMore() bool  { return s.catalogHasMore && s.catalogNextCursor != "" }
func (s ScopePickerModel) OwnsPagingKey(key string) bool {
	switch key {
	case "[":
		return s.memberFocused && s.memberPageIndex > 0 || !s.memberFocused && s.catalogPageIndex > 0
	case "]", "right":
		return s.memberFocused && s.MemberHasMore() || !s.memberFocused && s.CatalogHasMore()
	case "left":
		return true
	case "n":
		return s.memberFocused && s.MemberHasMore() || !s.memberFocused
	case "p":
		return true
	default:
		return false
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
	s.memberHasMore = false
	s.memberNextCursor = ""
	s.memberPageIndex = 0
	s.memberPageCursors = []string{""}
	return s.memberGeneration
}

func (s *ScopePickerModel) BeginMemberPageLoad(scopeID string) uint64 {
	s.memberGeneration++
	s.memberScopeID = scopeID
	s.memberLoading = true
	s.memberError = ""
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
	selectedID := ""
	if selected := s.SelectedMember(); selected != nil {
		selectedID = selected.Issue.ID
	}
	s.members = append([]IssueItem(nil), items...)
	s.memberLoading = false
	s.memberError = ""
	s.applyMemberFilters()
	if selectedID != "" {
		for i, item := range s.filteredMembers {
			if item.Issue.ID == selectedID {
				s.memberSelected = i
				break
			}
		}
	}
}

func (s ScopePickerModel) acceptsMemberPage(scopeID string, generation uint64) bool {
	return s.acceptsMemberDetails(scopeID, generation)
}

func (s *ScopePickerModel) SetMemberPage(page ScopeMembersPage, items []IssueItem, index int, generation uint64) bool {
	if !s.acceptsMemberPage(page.Scope.ID, generation) {
		return false
	}
	s.memberHasMore = page.HasMore
	s.memberNextCursor = page.NextCursor
	s.memberPageIndex = maxInt(0, index)
	if len(s.memberPageCursors) == 0 {
		s.memberPageCursors = []string{""}
	}
	if s.memberPageIndex >= len(s.memberPageCursors) {
		s.memberPageCursors = append(s.memberPageCursors, make([]string, s.memberPageIndex-len(s.memberPageCursors)+1)...)
	}
	s.memberLoading = false
	s.memberError = ""
	s.ClearMemberMarks()
	s.SetMembers(items)
	return true
}

func (s *ScopePickerModel) SetMemberServerFiltering(enabled bool) { s.memberServerFiltering = enabled }

// SetMemberContextCatalog keeps server-side context choices independent of the
// currently loaded member page.
func (s *ScopePickerModel) SetMemberContextCatalog(contexts repositorypkg.Catalog) {
	s.memberContextCatalog = append(repositorypkg.Catalog(nil), contexts...)
}

func (s ScopePickerModel) MemberFilters() (repository, status string, issueType model.IssueType) {
	return s.memberRepositoryFilter, s.memberStatusFilter, s.memberTypeFilter
}

func (s ScopePickerModel) MemberContexts() []string {
	if len(s.memberContextFilter) > 0 {
		return append([]string(nil), s.memberContextFilter...)
	}
	if s.memberRepositoryFilter == "" {
		return nil
	}
	for _, context := range s.memberContextCatalog {
		if context.Name == s.memberRepositoryFilter || context.ID == s.memberRepositoryFilter {
			return []string{context.ID}
		}
	}
	contexts := make([]string, 0, 1)
	seen := make(map[string]bool)
	for _, item := range s.members {
		if memberRepositoryValue(item) != s.memberRepositoryFilter || item.RepositoryID == "" || seen[item.RepositoryID] {
			continue
		}
		seen[item.RepositoryID] = true
		contexts = append(contexts, item.RepositoryID)
	}
	if len(contexts) == 0 {
		return []string{s.memberRepositoryFilter}
	}
	sort.Strings(contexts)
	return contexts
}

func (s *ScopePickerModel) NextMemberPage() (string, int, bool) {
	if s.memberLoading || !s.memberHasMore || s.memberNextCursor == "" {
		return "", 0, false
	}
	next := s.memberPageIndex + 1
	if next < len(s.memberPageCursors) {
		s.memberPageCursors = s.memberPageCursors[:next]
	}
	s.memberPageCursors = append(s.memberPageCursors, s.memberNextCursor)
	s.memberPageIndex = next
	return s.memberNextCursor, next, true
}

func (s *ScopePickerModel) PreviousMemberPage() (string, int, bool) {
	if s.memberLoading || s.memberPageIndex <= 0 || s.memberPageIndex >= len(s.memberPageCursors) {
		return "", 0, false
	}
	s.memberPageIndex--
	return s.memberPageCursors[s.memberPageIndex], s.memberPageIndex, true
}

func (s ScopePickerModel) MemberPageIndex() int { return s.memberPageIndex }
func (s ScopePickerModel) MemberHasMore() bool  { return s.memberHasMore && s.memberNextCursor != "" }

func (s *ScopePickerModel) ResetPaging() {
	s.catalogGeneration++
	s.catalogLoading = false
	s.catalogHasMore = false
	s.catalogNextCursor = ""
	s.catalogPageIndex = 0
	s.catalogPageCursors = []string{""}
	s.catalogError = ""
	s.memberGeneration++
	s.memberLoading = false
	s.memberHasMore = false
	s.memberNextCursor = ""
	s.memberPageIndex = 0
	s.memberPageCursors = []string{""}
	s.memberScopeID = ""
	s.ClearMemberMarks()
}

func (s *ScopePickerModel) SetMemberFilters(repository, status string, issueType model.IssueType) {
	s.memberRepositoryFilter = repository
	s.memberContextFilter = nil
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
	s.memberContextFilter = nil
	if s.memberServerFiltering && s.memberRepositoryFilter != "" {
		for _, context := range s.memberContextCatalog {
			if context.Name == s.memberRepositoryFilter || context.ID == s.memberRepositoryFilter {
				s.memberContextFilter = []string{context.ID}
				break
			}
		}
		if len(s.memberContextFilter) == 0 {
			for _, item := range s.members {
				if memberRepositoryValue(item) == s.memberRepositoryFilter && item.RepositoryID != "" {
					s.memberContextFilter = append(s.memberContextFilter, item.RepositoryID)
				}
			}
		}
		if len(s.memberContextFilter) == 0 {
			s.memberContextFilter = []string{s.memberRepositoryFilter}
		}
		sort.Strings(s.memberContextFilter)
	}
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
	var values []string
	if s.memberServerFiltering {
		values = []string{
			string(model.TypeBug), string(model.TypeFeature), string(model.TypeTask),
			string(model.TypeEpic), string(model.TypeChore),
		}
	} else {
		seen := make(map[model.IssueType]bool)
		for _, item := range s.members {
			if item.Issue.IssueType != "" && !seen[item.Issue.IssueType] {
				seen[item.Issue.IssueType] = true
				values = append(values, string(item.Issue.IssueType))
			}
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
	if len(s.memberContextCatalog) > 0 {
		seen := make(map[string]bool)
		values := make([]string, 0, len(s.memberContextCatalog))
		for _, context := range s.memberContextCatalog {
			value := context.Name
			if value == "" {
				value = context.ID
			}
			if value != "" && !seen[value] {
				seen[value] = true
				values = append(values, value)
			}
		}
		sort.Strings(values)
		return values
	}
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
	if s.memberServerFiltering {
		s.filteredMembers = append(s.filteredMembers[:0], s.members...)
		if s.memberSelected >= len(s.filteredMembers) {
			s.memberSelected = maxInt(0, len(s.filteredMembers)-1)
		}
		return
	}
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
	// A framed member panel needs six outer rows: borders, heading, filters,
	// column header, and one member row. Below the minimum useful framed layout,
	// keep the picker bounded with its compact presentation.
	if width < 20 || height < 15 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, truncateRunesHelper("Scopes", width, "…"))
	}
	heading := "Scopes"
	if s.moveTarget != "" {
		heading = "Move: " + s.moveTarget
	}
	contentWidth := maxInt(width-4, 1)
	contentHeight := maxInt(height-1, 3)
	catalogRows := maxInt(3, contentHeight/2)
	memberRows := contentHeight - catalogRows - 2
	if memberRows < 6 {
		memberRows = 6
		catalogRows = maxInt(1, contentHeight-memberRows-2)
	}

	catalogStyle, memberStyle := PanelStyle, FocusedPanelStyle
	if !s.memberFocused {
		catalogStyle, memberStyle = FocusedPanelStyle, PanelStyle
	}
	panel := func(style lipgloss.Style, content string, panelWidth, panelHeight int) string {
		innerHeight := maxInt(panelHeight-2, 1)
		return style.Padding(0, 1).Width(maxInt(panelWidth-2, 1)).Height(innerHeight).Render(content)
	}
	catalog := panel(catalogStyle, s.renderCatalog(heading, contentWidth-4, catalogRows-2), contentWidth, catalogRows)
	members := panel(memberStyle, s.renderMembers(contentWidth-4, memberRows-2), contentWidth, memberRows)
	view := catalog + "\n\n" + members
	return lipgloss.NewStyle().
		Width(maxInt(width, 1)).
		Height(maxInt(height, 1)).
		Padding(1, 2).
		Render(view)
}

func (s ScopePickerModel) renderCatalog(heading string, width, rows int) string {
	width = maxInt(width, 1)
	rows = maxInt(rows, 1)
	page := fmt.Sprintf("page %d", s.catalogPageIndex+1)
	if s.catalogHasMore {
		page += "+"
	}
	title := s.theme.Renderer.NewStyle().Foreground(s.theme.Primary).Bold(true).Render(truncateRunesHelper(heading+" · "+page, width, "…"))
	lines := []string{title, ""}
	if s.catalogLoading {
		lines = append(lines, "Loading scopes…")
	} else if s.catalogError != "" {
		lines = append(lines, "Scopes unavailable: "+s.catalogError)
	} else if len(s.scopes) == 0 {
		lines = append(lines, "No scopes available.")
	} else {
		// Match the Context picker: a catalog row gets a muted detail line when
		// the panel can afford two-line entries, otherwise keep the old compact
		// one-line row. Windowing uses that same row height so the panel cannot
		// render past its assigned viewport.
		showDetails := rows >= 5
		rowHeight := 1
		if showDetails {
			rowHeight = 2
		}
		visible := (rows - len(lines)) / rowHeight
		if visible < 0 {
			visible = 0
		}
		start := 0
		if visible > 0 && s.selected >= visible {
			start = s.selected - visible + 1
		}
		end := min(len(s.scopes), start+visible)
		for i := start; i < end; i++ {
			prefix := "  "
			if i == s.selected {
				prefix = "▸ "
			}
			nameStyle := s.theme.Renderer.NewStyle().Foreground(s.theme.Base.GetForeground())
			if i == s.selected {
				nameStyle = nameStyle.Foreground(s.theme.Primary).Bold(true)
			}
			activeStyle := s.theme.Renderer.NewStyle().Foreground(s.theme.Open).Bold(true)
			active := ""
			if s.scopes[i].Active {
				active = "  (active)"
			}
			activeRendered := ""
			if active != "" {
				activeRendered = activeStyle.Render(active)
			}
			date := s.scopes[i].CreatedAt.Format("2006-01-02")
			if !showDetails {
				suffix := fmt.Sprintf(" · %s/%d", date, s.scopes[i].MemberCount)
				// Truncate the complete styled row, not just the name: the
				// date/count and active marker must not escape narrow panels.
				row := prefix + nameStyle.Render(s.scopes[i].Name) + suffix + activeRendered
				lines = append(lines, ansi.Truncate(row, width, "…"))
				continue
			}

			nameWidth := maxInt(width-lipgloss.Width(prefix)-lipgloss.Width(active), 0)
			displayName := truncateRunesHelper(s.scopes[i].Name, nameWidth, "…")
			lines = append(lines, prefix+nameStyle.Render(displayName)+activeRendered)
			detail := fmt.Sprintf("    created: %s · members: %d", date, s.scopes[i].MemberCount)
			lines = append(lines, s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).
				Render(truncateRunesHelper(detail, width, "…")))
		}
	}
	return lipgloss.NewStyle().Width(width).Height(rows).MaxHeight(rows).Render(strings.Join(lines, "\n"))
}

type scopeMemberColumns struct {
	mark, repository, issueType, priority, status, id, age, title, labels int
}

func scopeMemberLabels(item IssueItem) []string {
	labels := item.Issue.Labels
	if item.HubPresentation {
		labels = item.PresentationLabels
	}
	filtered := make([]string, 0, len(labels))
	for _, label := range labels {
		if !isHubContextLabel(label) {
			filtered = append(filtered, label)
		}
	}
	return filtered
}

func scopeMemberRepository(item IssueItem) string {
	repository := memberRepositoryValue(item)
	if repository == "" {
		return ""
	}
	if item.RepositoryExtra > 0 {
		repository += fmt.Sprintf(" +%d", item.RepositoryExtra)
	}
	return repository
}

// scopeMemberContext renders the local member's context without letting a
// long name escape the bounded member row.
func scopeMemberContext(item IssueItem, width int) string {
	repository := memberRepositoryValue(item)
	if repository == "" || width <= 0 {
		return ""
	}
	extra := ""
	if item.RepositoryExtra > 0 {
		extra = fmt.Sprintf(" +%d", item.RepositoryExtra)
	}
	nameWidth := maxInt(width-lipgloss.Width(extra)-2, 1)
	if item.RepositoryID != "" {
		return RenderRepositoryBadgeCompact(item.RepositoryID, repository, nameWidth) + extra
	}
	if item.RepoPrefix != "" {
		return RenderRepoBadge(item.RepoPrefix) + extra
	}
	return lipgloss.NewStyle().Foreground(GetRepoColor(repository)).Bold(true).
		Render(truncateRunesHelper(repository, nameWidth, "…")) + extra
}

func scopeMemberLabel(item IssueItem, width int) string {
	labels := scopeMemberLabels(item)
	if len(labels) == 0 || width < 3 {
		return ""
	}
	labelStyle := itemLabelStyle()
	text := truncateRunesHelper(strings.Join(labels, ","), maxInt(width-2, 1), "…")
	return labelStyle.Render(text)
}

// itemLabelStyle keeps the bounded member label column visually consistent
// with the ordinary list's single accent label pill.
func itemLabelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorPrimary).Background(ColorBgSubtle).Padding(0, 1)
}

func scopeMemberColumnsFor(items []IssueItem, width int) scopeMemberColumns {
	width = maxInt(width, 1)
	columns := scopeMemberColumns{
		mark:       2,
		repository: len("CONTEXT"),
		issueType:  len("TYPE"),
		priority:   lipgloss.Width(RenderPriorityBadge(0)),
		status:     lipgloss.Width(RenderStatusBadge("open")),
		id:         len("ID"),
		age:        4,
	}
	titleWidth := 1
	for _, item := range items {
		columns.repository = maxInt(columns.repository, lipgloss.Width(scopeMemberRepository(item))+2)
		icon := model.IssueTypeIcon(string(item.Issue.IssueType))
		columns.issueType = maxInt(columns.issueType, lipgloss.Width(icon)+1+lipgloss.Width(strings.ToUpper(string(item.Issue.IssueType))))
		columns.id = maxInt(columns.id, lipgloss.Width(item.Issue.ID))
		titleWidth = maxInt(titleWidth, lipgloss.Width(item.Issue.Title))
		if labels := scopeMemberLabels(item); len(labels) > 0 {
			columns.labels = maxInt(columns.labels, lipgloss.Width(itemLabelStyle().Render(strings.Join(labels, ","))))
		}
	}
	columns.repository = min(columns.repository, 16)
	columns.issueType = min(columns.issueType, 10)
	columns.id = min(maxInt(columns.id, len("ID")), 24)
	columns.labels = min(columns.labels, 20)
	if columns.labels > 0 {
		titleWidth = min(titleWidth, maxInt(width-columns.mark-columns.repository-columns.issueType-columns.priority-columns.status-columns.id-columns.age-columns.labels-8, 1))
	}
	separators := 7
	if columns.labels > 0 {
		separators++
	}
	reserved := columns.mark + columns.repository + columns.issueType + columns.priority + columns.status + columns.id + columns.age + columns.labels + separators
	columns.title = min(titleWidth, maxInt(width-reserved, 1))
	total := func() int {
		return columns.mark + columns.repository + columns.issueType + columns.priority + columns.status + columns.id + columns.age + columns.title + columns.labels + separators
	}
	for total() > width {
		switch {
		case columns.labels > 3:
			columns.labels--
		case columns.title > 1:
			columns.title--
		case columns.repository > 1:
			columns.repository--
		case columns.id > 1:
			columns.id--
		case columns.issueType > 1:
			columns.issueType--
		case columns.status > 1:
			columns.status--
		case columns.age > 1:
			columns.age--
		case columns.priority > 1:
			columns.priority--
		default:
			return columns
		}
	}
	return columns
}

func scopeMemberCell(value string, width int) string {
	return padRight(truncateRunesHelper(value, width, "…"), width)
}

func scopeMemberStyledCell(value string, width int) string {
	valueWidth := lipgloss.Width(value)
	if valueWidth > width {
		return ansi.Truncate(value, width, "…")
	}
	return value + strings.Repeat(" ", width-valueWidth)
}

func (s ScopePickerModel) renderMemberRow(item IssueItem, selected bool, columns scopeMemberColumns, width int) string {
	mark := "  "
	if item.Marked {
		mark = s.theme.PrimaryBold.Render("✓ ")
	} else if selected {
		mark = s.theme.PrimaryBold.Render("▸ ")
	}
	icon, iconColor := s.theme.GetTypeIcon(string(item.Issue.IssueType))
	typeText := strings.ToUpper(string(item.Issue.IssueType))
	typeValue := s.theme.Renderer.NewStyle().Foreground(iconColor).Render(icon) + " " + typeText
	idStyle := s.theme.SecondaryText
	if selected {
		idStyle = idStyle.Bold(true)
	}
	titleStyle := s.theme.Renderer.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E8E8E8"})
	if selected {
		titleStyle = titleStyle.Foreground(s.theme.Primary).Bold(true)
	}
	values := []string{
		scopeMemberStyledCell(mark, columns.mark),
		scopeMemberStyledCell(scopeMemberContext(item, columns.repository), columns.repository),
		scopeMemberStyledCell(typeValue, columns.issueType),
		scopeMemberStyledCell(RenderPriorityBadge(item.Issue.Priority), columns.priority),
		scopeMemberStyledCell(RenderStatusBadge(string(item.Issue.Status)), columns.status),
		scopeMemberStyledCell(idStyle.Render(truncateRunesHelper(item.Issue.ID, columns.id, "…")), columns.id),
		scopeMemberStyledCell(s.theme.MutedText.Render(formatIssueListAge(item.Issue.CreatedAt)), columns.age),
		scopeMemberStyledCell(titleStyle.Render(truncateRunesHelper(item.Issue.Title, columns.title, "…")), columns.title),
	}
	if columns.labels > 0 {
		values = append(values, scopeMemberStyledCell(scopeMemberLabel(item, columns.labels), columns.labels))
	}
	row := strings.Join(values, " ")
	if lipgloss.Width(row) > width {
		row = ansi.Truncate(row, maxInt(width, 1), "…")
	}
	style := s.theme.Renderer.NewStyle().Width(maxInt(width, 1)).MaxWidth(maxInt(width, 1))
	if selected {
		style = style.Background(s.theme.Highlight).Bold(true)
	}
	return style.Render(row)
}

func (s ScopePickerModel) renderMembers(width, rows int) string {
	width = maxInt(width, 1)
	rows = maxInt(rows, 1)
	selected := s.Selected()
	name := "none"
	if selected != nil {
		name = selected.Name
	}
	page := fmt.Sprintf("page %d", s.memberPageIndex+1)
	if s.memberHasMore {
		page += "+"
	}
	count := ""
	if selected != nil {
		count = fmt.Sprintf(" · %d members", selected.MemberCount)
	}
	header := s.theme.Renderer.NewStyle().Foreground(s.theme.Primary).Bold(true).Render(truncateRunesHelper("Members · "+name+count+" · "+page, width, "…"))
	if s.memberLoading {
		return header + "\n" + s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).Render(truncateRunesHelper("Loading members…", width, "…"))
	}
	if s.memberError != "" {
		return header + "\n" + s.theme.Renderer.NewStyle().Foreground(s.theme.Blocked).Render(truncateRunesHelper("Members unavailable: "+s.memberError, width, "…"))
	}
	filter := fmt.Sprintf("repository:%s · status:%s · type:%s", memberFilterLabel(s.memberRepositoryFilter), memberFilterLabel(s.memberStatusFilter), memberFilterLabel(string(s.memberTypeFilter)))
	filterLine := s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).Render(truncateRunesHelper(filter, width, "…"))
	if len(s.filteredMembers) == 0 {
		return header + "\n" + filterLine + "\n" + s.theme.Renderer.NewStyle().Foreground(s.theme.Subtext).Render(truncateRunesHelper("No members match.", width, "…"))
	}
	items := append([]IssueItem(nil), s.filteredMembers...)
	for i := range items {
		items[i].Marked = s.memberMarkedIDs[items[i].Issue.ID]
	}
	columns := scopeMemberColumnsFor(items, width)
	headerCells := []string{
		scopeMemberCell("", columns.mark),
		scopeMemberCell("CONTEXT", columns.repository),
		scopeMemberCell("TYPE", columns.issueType),
		scopeMemberCell("PR", columns.priority),
		scopeMemberCell("STAT", columns.status),
		scopeMemberCell("ID", columns.id),
		scopeMemberCell("AGE", columns.age),
		scopeMemberCell("TITLE", columns.title),
	}
	if columns.labels > 0 {
		headerCells = append(headerCells, scopeMemberCell("LABELS", columns.labels))
	}
	headerLine := truncateRunesHelper(strings.Join(headerCells, " "), maxInt(width, 1), "…")
	lines := []string{header, filterLine, headerLine}
	visible := maxInt(rows-3, 1)
	start := 0
	if s.memberSelected >= visible {
		start = s.memberSelected - visible + 1
	}
	end := min(len(items), start+visible)
	for i := start; i < end; i++ {
		lines = append(lines, s.renderMemberRow(items[i], i == s.memberSelected, columns, width))
	}
	return lipgloss.NewStyle().Width(width).Height(maxInt(rows, 1)).MaxHeight(maxInt(rows, 1)).Render(strings.Join(lines, "\n"))
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
	action := "Add matching exact label/epic issues to active scope"
	if m.scopeMatchAction == "remove" {
		action = "Remove matching exact label/epic issues from selected scope"
	}
	content := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary).Bold(true).Render("Scope match") + "\n\n" +
		muted.Render(action) + "\n\n" +
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

func (m *Model) startScopeCatalogPage(cursor string, index int) tea.Cmd {
	if m.runtimeServices.Scopes.QueryCatalog == nil {
		return loadScopeSnapshotCmd(m.runtimeServices.Scopes)
	}
	generation := m.scopePicker.BeginCatalogLoad()
	return loadScopeCatalogPageCmd(m.runtimeServices.Scopes, ScopeCatalogQuery{Cursor: cursor, Limit: scopePageSize}, index, generation)
}

func (m *Model) startScopeMembersPage(cursor string, index int) tea.Cmd {
	if m.runtimeServices.Scopes.QueryMembers == nil {
		return m.loadSelectedScopeDetails()
	}
	scopeID := m.scopePicker.SelectedScopeID()
	if scopeID == "" {
		return nil
	}
	m.scopePicker.SetMemberContextCatalog(m.repositoryCatalog)
	_, status, issueType := m.scopePicker.MemberFilters()
	if status == "closed" {
		status = "completed"
	}
	generation := m.scopePicker.BeginMemberLoad(scopeID)
	m.scopePicker.SetMemberServerFiltering(true)
	return loadScopeMembersPageCmd(m.runtimeServices.Scopes, ScopeMembersQuery{
		ScopeID: scopeID, Cursor: cursor, Limit: scopePageSize, Status: status,
		Type: string(issueType), Contexts: m.scopePicker.MemberContexts(),
	}, index, generation)
}

func (m *Model) startScopeMembersPageAt(cursor string, index int) tea.Cmd {
	if m.runtimeServices.Scopes.QueryMembers == nil {
		return nil
	}
	scopeID := m.scopePicker.SelectedScopeID()
	if scopeID == "" {
		return nil
	}
	m.scopePicker.SetMemberContextCatalog(m.repositoryCatalog)
	_, status, issueType := m.scopePicker.MemberFilters()
	if status == "closed" {
		status = "completed"
	}
	generation := m.scopePicker.BeginMemberPageLoad(scopeID)
	return loadScopeMembersPageCmd(m.runtimeServices.Scopes, ScopeMembersQuery{
		ScopeID: scopeID, Cursor: cursor, Limit: scopePageSize, Status: status,
		Type: string(issueType), Contexts: m.scopePicker.MemberContexts(),
	}, index, generation)
}

func (m *Model) openScopePicker(moveIssue string) tea.Cmd {
	if m.isBacklogView {
		m.closeBacklog()
	}
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
	if m.runtimeServices.Scopes.QueryCatalog != nil {
		cmds = append(cmds, m.startScopeCatalogPage("", 0))
		if m.runtimeServices.Scopes.Load != nil {
			cmds = append(cmds, loadScopeSnapshotCmd(m.runtimeServices.Scopes))
		}
	} else if m.runtimeServices.Scopes.Load != nil {
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
	if m.runtimeServices.Scopes.QueryMembers != nil {
		return m.startScopeMembersPage("", 0)
	}
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
	m.scopePicker.ResetPaging()
	m.focused = m.scopePickerOrigin
}

func (m *Model) openBacklog() tea.Cmd {
	m.isBacklogView = true
	m.isBoardView, m.isGraphView, m.isActionableView, m.isHistoryView = false, false, false, false
	m.focused = focusBacklog
	m.backlog.Reset()
	m.backlogLoading = true
	m.backlogPageGeneration++
	return loadBacklogPageCmd(m.runtimeServices.Scopes, m.backlogQuery(""), 0, m.backlogPageGeneration)
}

func (m *Model) backlogQuery(cursor string) BacklogQuery {
	status := m.backlog.Status()
	if status == backlogStatusAll {
		status = ""
	}
	return BacklogQuery{
		Filter:             m.backlog.Filter(),
		Label:              m.backlog.Label(),
		Status:             status,
		Contexts:           m.backlog.Contexts(),
		IncludeContextless: m.backlog.IncludeContextless(),
		Cursor:             cursor,
		Limit:              backlogPageSize,
	}
}

func (m *Model) reloadBacklogFromFirstPage() tea.Cmd {
	m.backlog.ClearMarks()
	m.backlog.ResetPagination()
	m.backlogLoading = true
	m.backlogPageGeneration++
	return loadBacklogPageCmd(m.runtimeServices.Scopes, m.backlogQuery(""), 0, m.backlogPageGeneration)
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
	case "right", "]":
		if m.scopePicker.MemberFocused() {
			if cursor, index, ok := m.scopePicker.NextMemberPage(); ok {
				return m, m.startScopeMembersPageAt(cursor, index)
			}
		} else if msg.String() != "n" {
			if cursor, index, ok := m.scopePicker.NextCatalogPage(); ok {
				return m, m.startScopeCatalogPage(cursor, index)
			}
		}
	case "p", "left", "[":
		if m.scopePicker.MemberFocused() {
			if cursor, index, ok := m.scopePicker.PreviousMemberPage(); ok {
				return m, m.startScopeMembersPageAt(cursor, index)
			}
		} else if cursor, index, ok := m.scopePicker.PreviousCatalogPage(); ok {
			return m, m.startScopeCatalogPage(cursor, index)
		}
	case "w":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.CycleMemberRepository()
			if m.scopePicker.memberServerFiltering {
				return m, m.startScopeMembersPage("", 0)
			}
		}
	case "o", "c", "r":
		if m.scopePicker.MemberFocused() {
			status := map[string]string{"o": "open", "c": "closed", "r": "ready"}[msg.String()]
			m.scopePicker.ToggleMemberStatus(status)
			if m.scopePicker.memberServerFiltering {
				return m, m.startScopeMembersPage("", 0)
			}
		}
	case "I":
		if m.scopePicker.MemberFocused() {
			m.scopePicker.CycleMemberType()
			if m.scopePicker.memberServerFiltering {
				return m, m.startScopeMembersPage("", 0)
			}
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
		if m.scopePicker.MemberFocused() {
			if cursor, index, ok := m.scopePicker.NextMemberPage(); ok {
				return m, m.startScopeMembersPageAt(cursor, index)
			}
			return m, nil
		}
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
	if m.backlog.LabelEditing() {
		switch msg.String() {
		case "enter":
			label := strings.TrimSpace(m.backlog.LabelInputValue())
			if isBacklogOrdinaryLabel(label) {
				oldLabel := m.backlog.Label()
				m.backlog.EndLabelEdit()
				m.backlog.SetLabel(label)
				if oldLabel != m.backlog.Label() {
					return m, m.reloadBacklogFromFirstPage()
				}
				return m, nil
			}
			if label == "" {
				oldLabel := m.backlog.Label()
				m.backlog.EndLabelEdit()
				m.backlog.SetLabel("")
				if oldLabel != "" {
					return m, m.reloadBacklogFromFirstPage()
				}
				return m, nil
			}
			m.statusMsg = "Enter one ordinary label"
			m.statusIsError = true
			return m, nil
		case "esc":
			m.backlog.CancelLabelEdit()
			return m, nil
		default:
			return m, m.backlog.UpdateLabelInput(msg)
		}
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
			return m, m.reloadBacklogFromFirstPage()
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
	case "l":
		return m, m.backlog.BeginLabelEdit()
	case "s":
		before := m.backlog.Status()
		m.backlog.CycleStatus()
		if before != m.backlog.Status() {
			return m, m.reloadBacklogFromFirstPage()
		}
	case " ", "space":
		m.backlog.ToggleMark()
	case "n", "right":
		if cursor := m.backlog.NextPageCursor(); cursor != "" {
			m.backlog.ClearMarks()
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, m.backlogQuery(cursor), m.backlog.PageIndex()+1, m.backlogPageGeneration)
		}
	case "p", "left":
		if m.backlog.PageIndex() > 0 {
			m.backlog.ClearMarks()
			cursor := m.backlog.PreviousPageCursor()
			m.backlogLoading = true
			m.backlogPageGeneration++
			return m, loadBacklogPageCmd(m.runtimeServices.Scopes, m.backlogQuery(cursor), m.backlog.PageIndex(), m.backlogPageGeneration)
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
	cmds := []tea.Cmd{m.startScopeCatalogPage("", 0)}
	if m.runtimeServices.Scopes.QueryCatalog != nil && m.runtimeServices.Scopes.Load != nil {
		cmds = append(cmds, loadScopeSnapshotCmd(m.runtimeServices.Scopes))
	}
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
		cmds = append(cmds, loadBacklogPageCmd(m.runtimeServices.Scopes,
			m.backlogQuery(m.backlog.CurrentPageCursor()), m.backlog.PageIndex(), m.backlogPageGeneration))
	}
	return tea.Batch(cmds...)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
