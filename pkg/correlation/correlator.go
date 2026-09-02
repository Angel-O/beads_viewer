// Package correlation provides the Correlator for building complete bead history reports.
package correlation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	json "github.com/goccy/go-json"
)

// Correlator orchestrates the extraction and correlation of bead history data
type Correlator struct {
	repoPath      string
	extractor     *Extractor
	coCommitter   *CoCommitExtractor
	hubConfigPath string
	historyMode   HistoryMode

	// ctx, when set via WithContext, bounds every git subprocess spawned
	// during report generation (issue #166). nil means context.Background().
	ctx context.Context
}

// HistoryMode selects the history provider independently from issue loading.
type HistoryMode string

const (
	HistoryModeGit      HistoryMode = "git"
	HistoryModeExternal HistoryMode = "external"
	HistoryModeOff      HistoryMode = "off"
)

// WithHistoryMode selects git, external, or disabled history loading.
func (c *Correlator) WithHistoryMode(mode HistoryMode) *Correlator {
	c.historyMode = mode
	return c
}

// WithHubConfig switches correlation to explicit,
// repository-aware history. No Git command is run against repoPath in this mode.
func (c *Correlator) WithHubConfig(path string) *Correlator {
	c.hubConfigPath = path
	return c
}

// NewCorrelator creates a new correlator for the given repository.
// beadsFilePath is optional and forwarded to the extractor so history follows
// the correct beads file; variadic form preserves compatibility with older
// single-argument callers.
func NewCorrelator(repoPath string, beadsFilePath ...string) *Correlator {
	return &Correlator{
		repoPath:    repoPath,
		extractor:   NewExtractor(repoPath, beadsFilePath...),
		coCommitter: NewCoCommitExtractor(repoPath),
	}
}

// WithContext binds ctx to the correlator and its underlying extractors so
// every git subprocess spawned while generating a report is killed as soon as
// ctx is cancelled (issue #166: bounded robot liveness). It mutates and
// returns the receiver for chaining:
//
//	report, err := NewCorrelator(dir).WithContext(ctx).GenerateReportCached(beads, opts)
//
// A nil ctx (or never calling WithContext) preserves the legacy
// run-to-completion behavior.
func (c *Correlator) WithContext(ctx context.Context) *Correlator {
	c.ctx = ctx
	c.extractor.ctx = ctx
	c.coCommitter.ctx = ctx
	return c
}

// CorrelatorOptions controls how the history report is generated
type CorrelatorOptions struct {
	BeadID string     // Filter to single bead ID (empty = all)
	Since  *time.Time // Only events after this time
	Until  *time.Time // Only events before this time
	Limit  int        // Max commits to process (0 = no limit)
}

// historyArtifact holds the purely history-derived (HEAD + options only)
// intermediate products of report generation: the extracted lifecycle events
// and the co-committed-file correlations derived from them. NEITHER depends on
// the passed-in beads slice (bead ID/Title/Status) — Extract reads only
// committed git history, and ExtractAllCoCommits is a pure function of events.
// This is the expensive part of GenerateReport (the 232MB git-blob extraction
// plus the batched co-commit git logs), so it is the unit cached by the
// HEAD-keyed disk cache and reused unchanged across working-tree bead edits.
type historyArtifact struct {
	Events   []BeadEvent        `json:"events"`
	Commits  []CorrelatedCommit `json:"commits"`
	Warnings []HistoryWarning   `json:"warnings,omitempty"`
}

// CorrelatedCommit.BeadID carries the json:"-" tag (it is internal linking state,
// intentionally hidden from the public report JSON). The HEAD-artifact disk cache,
// however, serializes the PRE-assembly Commits slice and MUST round-trip BeadID —
// assembleReport groups commits onto beads by exactly that field. Without this the
// cache would return commit-less reports on the middle-tier (bead-edit) path.
// Custom (Un)MarshalJSON preserves BeadID via a parallel commit_bead_ids array
// without disturbing the public CorrelatedCommit tag.
type historyArtifactWire struct {
	Events        []BeadEvent        `json:"events"`
	Commits       []CorrelatedCommit `json:"commits"`
	CommitBeadIDs []string           `json:"commit_bead_ids,omitempty"`
	Warnings      []HistoryWarning   `json:"warnings,omitempty"`
}

func (a historyArtifact) MarshalJSON() ([]byte, error) {
	ids := make([]string, len(a.Commits))
	for i := range a.Commits {
		ids[i] = a.Commits[i].BeadID
	}
	return json.Marshal(historyArtifactWire{Events: a.Events, Commits: a.Commits, CommitBeadIDs: ids, Warnings: a.Warnings})
}

func (a *historyArtifact) UnmarshalJSON(b []byte) error {
	var w historyArtifactWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	a.Events = w.Events
	a.Commits = w.Commits
	a.Warnings = w.Warnings
	for i := range a.Commits {
		if i < len(w.CommitBeadIDs) {
			a.Commits[i].BeadID = w.CommitBeadIDs[i]
		}
	}
	return nil
}

// extractHistoryArtifact runs ONLY the HEAD/options-dependent extraction steps
// (git history walk + co-commit correlation). It is deterministic given the
// repository HEAD and the extract options; it never reads the working-tree bead
// slice. Split out so the result can be memoized independently of bead edits.
func (c *Correlator) extractHistoryArtifact(opts CorrelatorOptions) (*historyArtifact, error) {
	extractOpts := ExtractOptions{
		Since:  opts.Since,
		Until:  opts.Until,
		Limit:  opts.Limit,
		BeadID: opts.BeadID,
	}

	// Extract lifecycle events from git history
	events, err := c.extractor.Extract(extractOpts)
	if err != nil {
		return nil, fmt.Errorf("extracting events: %w", err)
	}

	// Extract co-committed files
	commits, err := c.coCommitter.ExtractAllCoCommits(events)
	if err != nil {
		return nil, fmt.Errorf("extracting co-commits: %w", err)
	}

	return &historyArtifact{Events: events, Commits: commits}, nil
}

// GenerateReport generates a complete history report
func (c *Correlator) GenerateReport(beads []BeadInfo, opts CorrelatorOptions) (*HistoryReport, error) {
	var art *historyArtifact
	var err error
	if c.historyMode == HistoryModeOff {
		art = &historyArtifact{Events: []BeadEvent{}, Commits: []CorrelatedCommit{}}
	} else if c.historyMode == HistoryModeExternal || c.hubConfigPath != "" {
		if c.hubConfigPath == "" {
			return nil, fmt.Errorf("external history mode requires a hub config")
		}
		art, err = c.extractExternalHistoryArtifact(beads, opts)
	} else {
		art, err = c.extractHistoryArtifact(opts)
	}
	if err != nil {
		return nil, err
	}
	return c.assembleReport(beads, opts, art), nil
}

// assembleReport builds the final HistoryReport from the current bead slice and
// a (possibly cached) history artifact. Every step here is cheap and depends on
// the passed-in beads (title/status enrichment, stats, data hash); the
// expensive history extraction lives in extractHistoryArtifact. Splitting the
// two lets a working-tree bead edit (which flips hashBeads but not HEAD) reuse
// the cached artifact and re-run only this assembly. The output is identical to
// the inline pre-split GenerateReport for the same (beads, opts, artifact).
func (c *Correlator) assembleReport(beads []BeadInfo, opts CorrelatorOptions, art *historyArtifact) *HistoryReport {
	events := art.Events
	commits := art.Commits

	// Build bead histories
	histories := c.buildHistories(beads, events, commits)

	// Apply bead filter if specified
	if opts.BeadID != "" {
		filtered := make(map[string]BeadHistory)
		if h, ok := histories[opts.BeadID]; ok {
			filtered[opts.BeadID] = h
		}
		histories = filtered
	}

	// Build commit index
	commitIndex := c.buildCommitIndex(histories)

	// Calculate stats
	stats := c.calculateStats(histories, commits)

	// Build git range description
	gitRange := c.describeGitRange(opts)

	// Calculate data hash
	dataHash := c.calculateDataHash(beads)

	// Get latest commit SHA for incremental updates
	latestEvents := events
	if c.historyMode == HistoryModeExternal || c.hubConfigPath != "" {
		latestEvents = nil
	}
	latestCommitSHA, latestCommitRepository := c.findLatestCommit(latestEvents, commits)

	return &HistoryReport{
		GeneratedAt:            time.Now().UTC(),
		DataHash:               dataHash,
		GitRange:               gitRange,
		LatestCommitSHA:        latestCommitSHA,
		LatestCommitRepository: latestCommitRepository,
		Stats:                  stats,
		Histories:              histories,
		CommitIndex:            commitIndex,
		Warnings:               art.Warnings,
	}
}

// findLatestCommit finds the most recent commit and its repository identity.
func (c *Correlator) findLatestCommit(events []BeadEvent, commits []CorrelatedCommit) (string, string) {
	var latest time.Time
	var latestSHA string
	var latestRepository string

	// Check events
	for _, e := range events {
		if e.Timestamp.After(latest) {
			latest = e.Timestamp
			latestSHA = e.CommitSHA
		}
	}

	// Check commits
	for _, commit := range commits {
		if commit.Timestamp.After(latest) {
			latest = commit.Timestamp
			latestSHA = commit.SHA
			latestRepository = commit.Repository
		}
	}

	return latestSHA, latestRepository
}

// findLatestCommitSHA preserves the existing internal helper behavior.
func (c *Correlator) findLatestCommitSHA(events []BeadEvent, commits []CorrelatedCommit) string {
	sha, _ := c.findLatestCommit(events, commits)
	return sha
}

// BeadInfo is minimal bead information needed for correlation
type BeadInfo struct {
	ID        string
	Title     string
	IssueType string // Canonical Beads type for History presentation
	Status    string
	Labels    []string
}

// buildHistories constructs BeadHistory for each bead
func (c *Correlator) buildHistories(beads []BeadInfo, events []BeadEvent, commits []CorrelatedCommit) map[string]BeadHistory {
	histories := make(map[string]BeadHistory)

	// Initialize histories from bead list
	for _, bead := range beads {
		histories[bead.ID] = BeadHistory{
			BeadID:    bead.ID,
			Title:     bead.Title,
			IssueType: bead.IssueType,
			Status:    bead.Status,
			Events:    []BeadEvent{},
			Commits:   []CorrelatedCommit{},
		}
	}

	// Group events by bead ID
	eventsByBead := make(map[string][]BeadEvent)
	for _, event := range events {
		eventsByBead[event.BeadID] = append(eventsByBead[event.BeadID], event)
	}

	// Group commits by bead ID
	commitsByBead := make(map[string][]CorrelatedCommit)
	for _, commit := range commits {
		if commit.BeadID != "" {
			commitsByBead[commit.BeadID] = append(commitsByBead[commit.BeadID], commit)
		}
	}

	// Build complete histories
	for beadID, history := range histories {
		if events, ok := eventsByBead[beadID]; ok {
			history.Events = events
		}
		if commits, ok := commitsByBead[beadID]; ok {
			history.Commits = dedupCommits(commits)
			repositories := make(map[string]struct{})
			for _, commit := range history.Commits {
				if commit.Repository != "" {
					repositories[commit.Repository] = struct{}{}
				}
			}
			for repository := range repositories {
				history.Repositories = append(history.Repositories, repository)
			}
			sort.Strings(history.Repositories)
		}

		// Calculate milestones
		history.Milestones = GetBeadMilestones(history.Events)

		// Calculate cycle time
		history.CycleTime = CalculateCycleTime(history.Milestones)

		// Set last author
		if len(history.Commits) > 0 {
			history.LastAuthor = history.Commits[len(history.Commits)-1].Author
		} else if len(history.Events) > 0 {
			history.LastAuthor = history.Events[len(history.Events)-1].Author
		}

		histories[beadID] = history
	}

	return histories
}

// dedupCommits removes duplicate commits by SHA
func dedupCommits(commits []CorrelatedCommit) []CorrelatedCommit {
	seen := make(map[string]bool)
	var result []CorrelatedCommit
	for _, c := range commits {
		identity := CommitIdentity(c.Repository, c.SHA)
		if !seen[identity] {
			seen[identity] = true
			result = append(result, c)
		}
	}
	return result
}

// buildCommitIndex creates a reverse lookup from commit SHA to bead IDs
func (c *Correlator) buildCommitIndex(histories map[string]BeadHistory) CommitIndex {
	index := make(CommitIndex)

	for beadID, history := range histories {
		for _, commit := range history.Commits {
			identity := CommitIdentity(commit.Repository, commit.SHA)
			index[identity] = append(index[identity], beadID)
		}
	}

	return index
}

// calculateStats computes aggregate statistics
func (c *Correlator) calculateStats(histories map[string]BeadHistory, commits []CorrelatedCommit) HistoryStats {
	stats := HistoryStats{
		TotalBeads:         len(histories),
		MethodDistribution: make(map[string]int),
	}

	// Track unique authors and commits
	authors := make(map[string]bool)
	uniqueCommits := make(map[string]bool)

	// Collect cycle times for average
	var cycleTimes []time.Duration

	for _, history := range histories {
		if len(history.Commits) > 0 {
			stats.BeadsWithCommits++
		}

		for _, commit := range history.Commits {
			uniqueCommits[CommitIdentity(commit.Repository, commit.SHA)] = true
			authors[commit.Author] = true
			stats.MethodDistribution[commit.Method.String()]++
		}

		for _, event := range history.Events {
			authors[event.Author] = true
		}

		// Collect cycle time
		if history.CycleTime != nil && history.CycleTime.ClaimToClose != nil {
			cycleTimes = append(cycleTimes, *history.CycleTime.ClaimToClose)
		}
	}

	stats.TotalCommits = len(uniqueCommits)
	stats.UniqueAuthors = len(authors)

	if stats.BeadsWithCommits > 0 {
		stats.AvgCommitsPerBead = float64(stats.TotalCommits) / float64(stats.BeadsWithCommits)
	}

	// Calculate average cycle time
	if len(cycleTimes) > 0 {
		var total time.Duration
		for _, ct := range cycleTimes {
			total += ct
		}
		avgDays := total.Hours() / 24 / float64(len(cycleTimes))
		stats.AvgCycleTimeDays = &avgDays
	}

	return stats
}

// describeGitRange creates a human-readable description of the git range
func (c *Correlator) describeGitRange(opts CorrelatorOptions) string {
	if c.historyMode == HistoryModeOff {
		return "history disabled"
	}
	if c.historyMode == HistoryModeExternal || c.hubConfigPath != "" {
		return "external hub history"
	}
	parts := []string{}

	if opts.Since != nil {
		parts = append(parts, fmt.Sprintf("since %s", opts.Since.Format("2006-01-02")))
	}
	if opts.Until != nil {
		parts = append(parts, fmt.Sprintf("until %s", opts.Until.Format("2006-01-02")))
	}
	if opts.Limit > 0 {
		parts = append(parts, fmt.Sprintf("limit %d commits", opts.Limit))
	}

	if len(parts) == 0 {
		return "all history"
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}
	return result
}

// calculateDataHash creates a hash of the input beads for consistency checking
func (c *Correlator) calculateDataHash(beads []BeadInfo) string {
	return hashBeads(beads)
}

// ValidateRepository checks if the repository is valid for correlation
func ValidateRepository(repoPath string) error {
	// Check if git directory exists
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", repoPath)
	}

	// Check if any beads file exists (multiple possible names)
	beadsFiles := []string{
		filepath.Join(repoPath, ".beads", "issues.jsonl"),
		filepath.Join(repoPath, ".beads", "beads.jsonl"),
		filepath.Join(repoPath, ".beads", "beads.base.jsonl"),
	}

	found := false
	for _, f := range beadsFiles {
		if _, err := os.Stat(f); err == nil {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no beads file found in %s/.beads/", repoPath)
	}

	return nil
}
