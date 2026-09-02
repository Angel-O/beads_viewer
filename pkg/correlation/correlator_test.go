package correlation

import (
	"strings"
	"testing"
	"time"
)

func TestBuildHistories_Empty(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	histories := c.buildHistories(nil, nil, nil)

	if len(histories) != 0 {
		t.Errorf("expected empty histories, got %d", len(histories))
	}
}

func TestBuildHistories_Basic(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	beads := []BeadInfo{
		{ID: "bv-1", Title: "Task 1", Status: "open"},
		{ID: "bv-2", Title: "Task 2", Status: "closed"},
	}

	now := time.Now()
	events := []BeadEvent{
		{BeadID: "bv-1", EventType: EventCreated, Timestamp: now.Add(-24 * time.Hour), Author: "Alice"},
		{BeadID: "bv-1", EventType: EventClaimed, Timestamp: now.Add(-12 * time.Hour), Author: "Alice"},
		{BeadID: "bv-2", EventType: EventCreated, Timestamp: now.Add(-48 * time.Hour), Author: "Bob"},
		{BeadID: "bv-2", EventType: EventClosed, Timestamp: now.Add(-1 * time.Hour), Author: "Bob"},
	}

	histories := c.buildHistories(beads, events, nil)

	if len(histories) != 2 {
		t.Errorf("expected 2 histories, got %d", len(histories))
	}

	h1 := histories["bv-1"]
	if len(h1.Events) != 2 {
		t.Errorf("expected 2 events for bv-1, got %d", len(h1.Events))
	}
	if h1.Milestones.Created == nil {
		t.Error("expected bv-1 to have created milestone")
	}
	if h1.Milestones.Claimed == nil {
		t.Error("expected bv-1 to have claimed milestone")
	}

	h2 := histories["bv-2"]
	if len(h2.Events) != 2 {
		t.Errorf("expected 2 events for bv-2, got %d", len(h2.Events))
	}
	if h2.CycleTime == nil {
		t.Error("expected bv-2 to have cycle time (closed bead)")
	}
}

func TestBuildCommitIndex(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	histories := map[string]BeadHistory{
		"bv-1": {
			BeadID: "bv-1",
			Commits: []CorrelatedCommit{
				{SHA: "abc123", Method: MethodCoCommitted},
				{SHA: "def456", Method: MethodCoCommitted},
			},
		},
		"bv-2": {
			BeadID: "bv-2",
			Commits: []CorrelatedCommit{
				{SHA: "abc123", Method: MethodCoCommitted}, // Same commit, different bead
				{SHA: "ghi789", Method: MethodCoCommitted},
			},
		},
	}

	index := c.buildCommitIndex(histories)

	if len(index) != 3 {
		t.Errorf("expected 3 unique commits in index, got %d", len(index))
	}

	// abc123 should reference both beads
	if len(index["abc123"]) != 2 {
		t.Errorf("expected abc123 to reference 2 beads, got %d", len(index["abc123"]))
	}
	if got := index["abc123"]; got[0] != "bv-1" || got[1] != "bv-2" {
		t.Fatalf("expected sorted abc123 bead IDs, got %v", got)
	}

	// Duplicate history entries must not produce duplicate reverse links.
	history := histories["bv-2"]
	history.Commits = append(history.Commits, CorrelatedCommit{SHA: "abc123"})
	histories["bv-2"] = history
	if got := BuildCommitIndex(histories)["abc123"]; len(got) != 2 {
		t.Fatalf("expected duplicate commit link to collapse, got %v", got)
	}
}

func TestCalculateStats_Empty(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	stats := c.calculateStats(make(map[string]BeadHistory), nil)

	if stats.TotalBeads != 0 {
		t.Errorf("expected 0 total beads, got %d", stats.TotalBeads)
	}
	if stats.BeadsWithCommits != 0 {
		t.Errorf("expected 0 beads with commits, got %d", stats.BeadsWithCommits)
	}
}

func TestCalculateStats_WithData(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	claimToClose := 24 * time.Hour
	histories := map[string]BeadHistory{
		"bv-1": {
			BeadID: "bv-1",
			Events: []BeadEvent{
				{Author: "Alice"},
			},
			Commits: []CorrelatedCommit{
				{SHA: "abc123", Author: "Alice", Method: MethodCoCommitted},
			},
			CycleTime: &CycleTime{ClaimToClose: &claimToClose},
		},
		"bv-2": {
			BeadID: "bv-2",
			Events: []BeadEvent{
				{Author: "Bob"},
			},
			Commits: []CorrelatedCommit{
				{SHA: "def456", Author: "Bob", Method: MethodExplicitID},
			},
		},
	}

	stats := c.calculateStats(histories, nil)

	if stats.TotalBeads != 2 {
		t.Errorf("expected 2 total beads, got %d", stats.TotalBeads)
	}
	if stats.BeadsWithCommits != 2 {
		t.Errorf("expected 2 beads with commits, got %d", stats.BeadsWithCommits)
	}
	if stats.TotalCommits != 2 {
		t.Errorf("expected 2 total commits, got %d", stats.TotalCommits)
	}
	if stats.UniqueAuthors != 2 {
		t.Errorf("expected 2 unique authors, got %d", stats.UniqueAuthors)
	}
	if stats.MethodDistribution["co_committed"] != 1 {
		t.Errorf("expected 1 co_committed, got %d", stats.MethodDistribution["co_committed"])
	}
	if stats.MethodDistribution["explicit_id"] != 1 {
		t.Errorf("expected 1 explicit_id, got %d", stats.MethodDistribution["explicit_id"])
	}
	if stats.AvgCycleTimeDays == nil {
		t.Error("expected avg cycle time to be set")
	}
}

func TestDescribeGitRange(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	tests := []struct {
		name     string
		opts     CorrelatorOptions
		expected string
	}{
		{
			name:     "no filters",
			opts:     CorrelatorOptions{},
			expected: "all history",
		},
		{
			name:     "with limit",
			opts:     CorrelatorOptions{Limit: 100},
			expected: "limit 100 commits",
		},
		{
			name: "with since",
			opts: func() CorrelatorOptions {
				since := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
				return CorrelatorOptions{Since: &since}
			}(),
			expected: "since 2024-01-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.describeGitRange(tt.opts)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCalculateDataHash(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	beads1 := []BeadInfo{
		{ID: "bv-1", Status: "open"},
		{ID: "bv-2", Status: "closed"},
	}

	beads2 := []BeadInfo{
		{ID: "bv-1", Status: "open"},
		{ID: "bv-2", Status: "open"}, // Different status
	}
	beads3 := []BeadInfo{
		{ID: "bv-2", Status: "closed"},
		{ID: "bv-1", Status: "open"},
	}

	hash1 := c.calculateDataHash(beads1)
	hash2 := c.calculateDataHash(beads2)
	hash3 := c.calculateDataHash(beads3)

	if hash1 == hash2 {
		t.Error("different bead data should produce different hashes")
	}
	if hash1 != hash3 {
		t.Error("equivalent bead data in different order should produce the same hash")
	}

	// Same data should produce same hash
	hash1Again := c.calculateDataHash(beads1)
	if hash1 != hash1Again {
		t.Error("same bead data should produce same hash")
	}
}

func TestDedupCommits(t *testing.T) {
	commits := []CorrelatedCommit{
		{SHA: "abc123", Message: "First"},
		{SHA: "def456", Message: "Second"},
		{SHA: "abc123", Message: "First duplicate"}, // Duplicate SHA
		{SHA: "ghi789", Message: "Third"},
	}

	result := dedupCommits(commits)

	if len(result) != 3 {
		t.Errorf("expected 3 unique commits, got %d", len(result))
	}

	// First occurrence should be kept
	if result[0].Message != "First" {
		t.Errorf("expected first commit message to be 'First', got %s", result[0].Message)
	}
}

func TestNewCorrelator(t *testing.T) {
	c := NewCorrelator("/tmp/test")
	if c.repoPath != "/tmp/test" {
		t.Errorf("repoPath = %s, want /tmp/test", c.repoPath)
	}
	if c.extractor == nil {
		t.Error("extractor should not be nil")
	}
	if c.coCommitter == nil {
		t.Error("coCommitter should not be nil")
	}
}

func TestValidateRepository_NoGitDir(t *testing.T) {
	err := ValidateRepository("/nonexistent/path")
	if err == nil {
		t.Error("ValidateRepository should fail for nonexistent path")
	}
}

func TestValidateRepository_NoBeadsFile(t *testing.T) {
	// Use temp dir that exists but has no beads
	err := ValidateRepository("/tmp")
	if err == nil {
		t.Error("ValidateRepository should fail without beads file")
	}
}

func TestFindLatestCommitSHA_Empty(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	sha := c.findLatestCommitSHA(nil, nil)
	if sha != "" {
		t.Errorf("findLatestCommitSHA with empty inputs should return empty, got %s", sha)
	}
}

func TestFindLatestCommitSHA_FromEvents(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	now := time.Now()
	events := []BeadEvent{
		{CommitSHA: "older", Timestamp: now.Add(-1 * time.Hour)},
		{CommitSHA: "newest", Timestamp: now},
		{CommitSHA: "middle", Timestamp: now.Add(-30 * time.Minute)},
	}

	sha := c.findLatestCommitSHA(events, nil)
	if sha != "newest" {
		t.Errorf("findLatestCommitSHA = %s, want 'newest'", sha)
	}
}

func TestFindLatestCommitSHA_FromCommits(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	now := time.Now()
	commits := []CorrelatedCommit{
		{SHA: "commit_old", Timestamp: now.Add(-1 * time.Hour)},
		{SHA: "commit_newest", Timestamp: now},
	}

	sha := c.findLatestCommitSHA(nil, commits)
	if sha != "commit_newest" {
		t.Errorf("findLatestCommitSHA = %s, want 'commit_newest'", sha)
	}
}

func TestFindLatestCommitSHA_Mixed(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	now := time.Now()
	events := []BeadEvent{
		{CommitSHA: "event_sha", Timestamp: now.Add(-1 * time.Hour)},
	}
	commits := []CorrelatedCommit{
		{SHA: "commit_sha", Timestamp: now}, // This is newer
	}

	sha := c.findLatestCommitSHA(events, commits)
	if sha != "commit_sha" {
		t.Errorf("findLatestCommitSHA = %s, want 'commit_sha' (newer)", sha)
	}
}

func TestBuildHistories_WithCommits(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	beads := []BeadInfo{
		{ID: "bv-1", Title: "Task 1", Status: "in_progress"},
	}

	now := time.Now()
	events := []BeadEvent{
		{BeadID: "bv-1", EventType: EventClaimed, Timestamp: now, CommitSHA: "abc123"},
	}

	commits := []CorrelatedCommit{
		{SHA: "abc123", Author: "Test Author", BeadID: "bv-1"},
	}

	histories := c.buildHistories(beads, events, commits)

	h := histories["bv-1"]
	if len(h.Commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(h.Commits))
	}
	if h.LastAuthor != "Test Author" {
		t.Errorf("LastAuthor = %s, want 'Test Author'", h.LastAuthor)
	}
}

func TestCalculateStats_AvgCommitsPerBead(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	histories := map[string]BeadHistory{
		"bv-1": {
			Commits: []CorrelatedCommit{{SHA: "a1"}, {SHA: "a2"}},
		},
		"bv-2": {
			Commits: []CorrelatedCommit{{SHA: "b1"}},
		},
		"bv-3": {
			Commits: nil, // No commits
		},
	}

	stats := c.calculateStats(histories, nil)

	// 3 commits / 2 beads with commits = 1.5
	if stats.AvgCommitsPerBead != 1.5 {
		t.Errorf("AvgCommitsPerBead = %v, want 1.5", stats.AvgCommitsPerBead)
	}
}

func TestDescribeGitRange_Combined(t *testing.T) {
	c := NewCorrelator("/tmp/test")

	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	opts := CorrelatorOptions{
		Since: &since,
		Until: &until,
		Limit: 100,
	}

	result := c.describeGitRange(opts)

	if result != "since 2024-01-01, until 2024-12-31, limit 100 commits" {
		t.Errorf("unexpected result: %s", result)
	}
}

// TestAssembleReport_AppliesFeedback proves the feedback loop changes report
// output (C4): a rejected (commit, bead) pair disappears from the bead's
// commits and the commit index, a confirmed pair is pinned to confidence 1.0,
// untouched pairs and other beads are unchanged, and the cached artifact the
// report was assembled from is never mutated.
func TestAssembleReport_AppliesFeedback(t *testing.T) {
	store := NewFeedbackStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("load empty store: %v", err)
	}
	if err := store.Reject("aaa", "bv-9", "tester", 0.9, "unrelated refactor"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if err := store.Confirm("bbb", "bv-9", "tester", 0.5, ""); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// Feedback about a pair the report does not contain must be ignored, not
	// invented into the output.
	if err := store.Confirm("zzz", "bv-9", "tester", 0.5, ""); err != nil {
		t.Fatalf("confirm stray: %v", err)
	}

	art := &historyArtifact{
		Commits: []CorrelatedCommit{
			{SHA: "aaa", BeadID: "bv-9", Method: "explicit", Confidence: 0.9, Author: "a"},
			{SHA: "bbb", BeadID: "bv-9", Method: "temporal", Confidence: 0.5, Author: "b"},
			{SHA: "ccc", BeadID: "bv-9", Method: "cocommit", Confidence: 0.7, Author: "c"},
			{SHA: "aaa", BeadID: "bv-10", Method: "explicit", Confidence: 0.9, Author: "a"},
		},
	}
	beads := []BeadInfo{{ID: "bv-9", Title: "nine", Status: "open"}, {ID: "bv-10", Title: "ten", Status: "open"}}

	withStore := (&Correlator{}).WithFeedbackStore(store)
	report := withStore.assembleReport(beads, CorrelatorOptions{}, art)

	nine := report.Histories["bv-9"]
	if len(nine.Commits) != 2 {
		t.Fatalf("bv-9 commits after feedback = %d (%+v); want 2 (aaa rejected)", len(nine.Commits), nine.Commits)
	}
	for _, c := range nine.Commits {
		switch c.SHA {
		case "aaa":
			t.Fatalf("rejected commit aaa still listed for bv-9: %+v", c)
		case "bbb":
			if c.Confidence != 1.0 || !c.Confirmed {
				t.Fatalf("confirmed commit bbb: confidence=%v confirmed=%v; want 1.0/true", c.Confidence, c.Confirmed)
			}
			if !strings.Contains(c.Reason, "confirmed by feedback (tester)") {
				t.Fatalf("confirmed commit reason should say so: %q", c.Reason)
			}
		case "ccc":
			if c.Confidence != 0.7 || c.Confirmed {
				t.Fatalf("untouched commit ccc changed: %+v", c)
			}
		}
	}
	if beadsForAAA := report.CommitIndex["aaa"]; len(beadsForAAA) != 1 || beadsForAAA[0] != "bv-10" {
		t.Fatalf("commit_index[aaa]=%v; want only bv-10 (bv-9 rejected, bv-10 untouched)", beadsForAAA)
	}
	if ten := report.Histories["bv-10"]; len(ten.Commits) != 1 || ten.Commits[0].Confidence != 0.9 {
		t.Fatalf("bv-10 must be unaffected by bv-9 feedback: %+v", ten.Commits)
	}
	if fa := report.Stats.FeedbackApplied; fa == nil || fa.Confirmed != 1 || fa.Rejected != 1 || fa.Ignored != 0 {
		t.Fatalf("stats.feedback_applied=%+v; want confirmed=1 rejected=1 ignored=0", fa)
	}

	// The artifact (what the disk/HEAD caches hold) is untouched, so feedback
	// never needs a cache invalidation of the git walk.
	if len(art.Commits) != 4 || art.Commits[0].Confidence != 0.9 || art.Commits[1].Confidence != 0.5 {
		t.Fatalf("assembleReport mutated the artifact: %+v", art.Commits)
	}

	// Without a store nothing is applied and the payload says so (nil).
	plain := (&Correlator{}).assembleReport(beads, CorrelatorOptions{}, art)
	if plain.Stats.FeedbackApplied != nil {
		t.Fatalf("no store attached: feedback_applied should be nil, got %+v", plain.Stats.FeedbackApplied)
	}
	if len(plain.Histories["bv-9"].Commits) != 3 {
		t.Fatalf("no store attached: bv-9 should keep all 3 commits, got %d", len(plain.Histories["bv-9"].Commits))
	}
}
