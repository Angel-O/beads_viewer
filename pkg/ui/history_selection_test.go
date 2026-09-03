package ui

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
)

func TestHistoryModel_PreserveSelection(t *testing.T) {
	report := createTestHistoryReport() // Uses helper from history_test.go
	theme := testTheme()
	h := NewHistoryModel(report, theme)

	// We expect beads sorted by commit count descending:
	// bv-1 (2 commits)
	// bv-3 (2 commits)
	// bv-2 (1 commit)

	// Select "bv-3" (should be index 1)
	h.selectedBead = 1
	selectedID := h.SelectedBeadID()
	if selectedID != "bv-3" {
		t.Fatalf("setup: selectedBead=1 ID=%s, want bv-3. BeadIDs: %v", selectedID, h.beadIDs)
	}

	// Apply filter that keeps bv-3 but removes bv-1
	// bv-1 author: "Dev One"
	// bv-3 author: "Dev Two"
	// Filter by "Dev Two" should keep bv-3 and bv-2
	h.SetAuthorFilter("Dev Two")

	// Verify bv-3 is still selected
	// In the new list:
	// bv-3 (2 commits)
	// bv-2 (1 commit)
	// bv-3 should be index 0 now

	if h.SelectedBeadID() != "bv-3" {
		t.Errorf("selection lost after filter: ID=%s, want bv-3", h.SelectedBeadID())
	}
	if h.selectedBead != 0 {
		t.Errorf("selectedBead index = %d, want 0", h.selectedBead)
	}

	// Now apply filter that removes bv-3
	// Filter by "Dev One" -> only bv-1 remains
	h.SetAuthorFilter("Dev One")

	// Verify selection reset to 0 (bv-1), since bv-3 is gone
	if h.SelectedBeadID() != "bv-1" {
		t.Errorf("selection should reset to valid item: ID=%s, want bv-1", h.SelectedBeadID())
	}
	if h.selectedBead != 0 {
		t.Errorf("selectedBead index = %d, want 0", h.selectedBead)
	}
}

func TestHistoryModel_SetReportPreservesStableStateByIdentity(t *testing.T) {
	now := time.Now()
	report := &correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{
		"item-a": {
			BeadID: "item-a",
			Title:  "Alpha",
			Commits: []correlation.CorrelatedCommit{
				{Repository: "repo-a", SHA: "sha-a1", ShortSHA: "sha-a1", Message: "first", Author: "Dev", Timestamp: now, Confidence: 0.9, Files: []correlation.FileChange{{Repository: "repo-a", Path: "pkg/ui/first.go"}}},
				{Repository: "repo-a", SHA: "sha-a2", ShortSHA: "sha-a2", Message: "second", Author: "Dev", Timestamp: now.Add(-time.Minute), Confidence: 0.8, Files: []correlation.FileChange{{Repository: "repo-a", Path: "pkg/ui/second.go"}}},
			},
		},
		"item-b": {
			BeadID:  "item-b",
			Title:   "Beta",
			Commits: []correlation.CorrelatedCommit{{Repository: "repo-b", SHA: "sha-b", ShortSHA: "sha-b", Message: "other", Author: "Dev", Timestamp: now.Add(time.Minute), Confidence: 0.7}},
		},
	}}
	h := NewHistoryModel(report, testTheme())
	h.SetSize(160, 30)
	for index, beadID := range h.beadIDs {
		if beadID == "item-a" {
			h.selectedBead = index
		}
	}
	h.selectedCommit = 1
	h.focused = historyFocusMiddle
	h.middleScrollOffset = 2
	h.timelineScrollOffset = 3
	h.authorFilter = "Dev"
	h.minConfidence = 0.5
	h.expandedBeads["item-a"] = true
	h.ToggleFileTree()
	for index, node := range h.flatFileList {
		if node.Path == "repo-a:pkg" {
			h.selectedFileIdx = index
			h.ToggleExpandFile()
			break
		}
	}
	for index, node := range h.flatFileList {
		if node.Path == "repo-a:pkg/ui" {
			h.selectedFileIdx = index
			h.ToggleExpandFile()
			break
		}
	}
	for index, node := range h.flatFileList {
		if node.Path == "repo-a:pkg/ui/second.go" {
			h.selectedFileIdx = index
			break
		}
	}
	h.fileTreeScroll = 1
	h.fileTreeFocus = true
	h.StartSearchWithMode(searchModeCommit)
	h.searchInput.SetValue("second")
	h.applySearchFilter()
	h.FinishSearch()
	h.ToggleViewMode()
	for index, commit := range h.GetFilteredCommitList() {
		if commit.Repository == "repo-a" && commit.SHA == "sha-a2" {
			h.selectedGitCommit = index
		}
	}
	h.selectedRelatedBead = 0
	h.scrollOffset = 1
	h.gitScrollOffset = 2

	refreshed := &correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{
		"item-a": report.Histories["item-a"],
		"item-c": {BeadID: "item-c", Title: "Gamma", Commits: []correlation.CorrelatedCommit{{Repository: "repo-c", SHA: "sha-c", Message: "new", Timestamp: now.Add(2 * time.Minute)}}},
	}}
	h.SetReport(refreshed)

	if !h.IsGitMode() || h.focused != historyFocusMiddle || h.SearchQuery() != "second" || h.searchMode != searchModeCommit || h.IsSearchActive() {
		t.Fatalf("refresh changed mode/focus/search state: git=%v focus=%v query=%q mode=%v active=%v", h.IsGitMode(), h.focused, h.SearchQuery(), h.searchMode, h.IsSearchActive())
	}
	if commit := h.SelectedGitCommit(); commit == nil || commit.Repository != "repo-a" || commit.SHA != "sha-a2" {
		t.Fatalf("refresh changed selected git commit: %#v", commit)
	}
	if h.SelectedBeadID() != "item-a" {
		t.Fatalf("refresh changed selected bead: %q", h.SelectedBeadID())
	}
	if commit := h.SelectedCommit(); commit == nil || commit.Repository != "repo-a" || commit.SHA != "sha-a2" {
		t.Fatalf("refresh changed selected bead commit: %#v", commit)
	}
	if h.SelectedRelatedBeadID() != "item-a" || h.gitScrollOffset != 0 {
		t.Fatalf("refresh changed related bead or git scroll: bead=%q scroll=%d", h.SelectedRelatedBeadID(), h.gitScrollOffset)
	}
	if h.scrollOffset != 0 || h.middleScrollOffset != 0 || h.timelineScrollOffset != 0 {
		t.Fatalf("refresh changed pane scroll: list=%d middle=%d timeline=%d", h.scrollOffset, h.middleScrollOffset, h.timelineScrollOffset)
	}
	if h.authorFilter != "Dev" || h.minConfidence != 0.5 || !h.expandedBeads["item-a"] {
		t.Fatalf("refresh changed filters or expansion: author=%q confidence=%v expanded=%v", h.authorFilter, h.minConfidence, h.expandedBeads)
	}
	if file := h.SelectedFileNode(); file == nil || file.Path != "repo-a:pkg/ui/second.go" {
		t.Fatalf("refresh changed selected file: %#v", file)
	}
	if !h.showFileTree || !h.fileTreeFocus || h.fileTreeScroll != 0 {
		t.Fatalf("refresh changed file tree state: visible=%v focus=%v scroll=%d", h.showFileTree, h.fileTreeFocus, h.fileTreeScroll)
	}
	if expanded := expandedHistoryFilePaths(h.fileTree); !expanded["repo-a:pkg"] || !expanded["repo-a:pkg/ui"] {
		t.Fatalf("refresh changed file expansion: %v", expanded)
	}
}

func TestHistoryModel_SetReportFallsBackWhenStableIdentitiesDisappear(t *testing.T) {
	h := NewHistoryModel(createTestHistoryReport(), testTheme())
	h.ToggleViewMode()
	h.selectedGitCommit = len(h.GetFilteredCommitList()) - 1
	h.selectedRelatedBead = 0
	h.gitScrollOffset = 2
	h.middleScrollOffset = 2

	h.SetReport(&correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{
		"replacement": {BeadID: "replacement", Title: "Replacement", Commits: []correlation.CorrelatedCommit{{Repository: "repo", SHA: "replacement", Message: "replacement", Timestamp: time.Now()}}},
	}})

	if commit := h.SelectedGitCommit(); commit == nil || commit.SHA != "replacement" {
		t.Fatalf("missing commit identity did not fall back to first valid commit: %#v", commit)
	}
	if h.selectedGitCommit != 0 || h.selectedRelatedBead != 0 || h.gitScrollOffset != 0 || h.middleScrollOffset != 0 {
		t.Fatalf("missing identities retained stale indexes or scroll: commit=%d bead=%d gitScroll=%d middleScroll=%d", h.selectedGitCommit, h.selectedRelatedBead, h.gitScrollOffset, h.middleScrollOffset)
	}
}
