package correlation

import (
	"path/filepath"
	"testing"
	"time"

	json "github.com/goccy/go-json"
)

// TestHistoryArtifactRoundTripPreservesBeadID guards a real bug: CorrelatedCommit.BeadID
// is tagged json:"-" (hidden from the public report), but the HEAD-artifact disk cache
// serializes the pre-assembly Commits slice. Without a custom codec, BeadID is dropped on
// round-trip and assembleReport (which groups commits onto beads by BeadID) returns
// commit-less histories on the middle-tier "edit a bead, re-triage" path.
func TestHistoryArtifactRoundTripPreservesBeadID(t *testing.T) {
	in := &historyArtifact{
		Events: []BeadEvent{{BeadID: "bv-9", EventType: EventCreated}},
		Commits: []CorrelatedCommit{
			{SHA: "aaa", BeadID: "bv-9", Method: "explicit", Confidence: 0.9},
			{SHA: "bbb", BeadID: "bv-7"},
			{SHA: "ccc", BeadID: ""}, // unlinked commit: empty must stay empty
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out historyArtifact
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Commits) != 3 {
		t.Fatalf("commit count: got %d want 3", len(out.Commits))
	}
	want := []string{"bv-9", "bv-7", ""}
	for i, w := range want {
		if out.Commits[i].BeadID != w {
			t.Errorf("Commits[%d].BeadID: got %q want %q", i, out.Commits[i].BeadID, w)
		}
	}
	// Other fields must still round-trip (regression guard for the wire struct).
	if out.Commits[0].SHA != "aaa" || out.Commits[0].Method != "explicit" || out.Commits[0].Confidence != 0.9 {
		t.Errorf("non-BeadID fields not preserved: %+v", out.Commits[0])
	}
	if len(out.Events) != 1 || out.Events[0].BeadID != "bv-9" {
		t.Errorf("events not preserved: %+v", out.Events)
	}
}

func TestPersistentCacheFreshnessRejectsFutureTimestamps(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		createdAt time.Time
		wantFresh bool
	}{
		{name: "fresh", createdAt: now.Add(-time.Hour), wantFresh: true},
		{name: "age boundary", createdAt: now.Add(-correlationDiskCacheMaxAge), wantFresh: true},
		{name: "stale", createdAt: now.Add(-correlationDiskCacheMaxAge - time.Nanosecond)},
		{name: "future", createdAt: now.Add(time.Nanosecond)},
		{name: "zero", createdAt: time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheCreatedAtIsFresh(tt.createdAt, now, correlationDiskCacheMaxAge); got != tt.wantFresh {
				t.Fatalf("cacheCreatedAtIsFresh()=%v, want %v", got, tt.wantFresh)
			}
		})
	}

	reportEntries := map[string]correlationDiskCacheEntry{
		"fresh":  {CreatedAt: now.Add(-time.Hour)},
		"future": {CreatedAt: now.Add(time.Hour)},
		"stale":  {CreatedAt: now.Add(-correlationDiskCacheMaxAge - time.Second)},
		"zero":   {},
	}
	pruneCorrelationDiskCacheEntries(now, reportEntries)
	if len(reportEntries) != 1 {
		t.Fatalf("report prune retained %d entries, want only fresh", len(reportEntries))
	}
	if _, ok := reportEntries["fresh"]; !ok {
		t.Fatal("report prune removed fresh entry")
	}

	artifactEntries := map[string]headArtifactCacheEntry{
		"fresh":  {CreatedAt: now.Add(-time.Hour)},
		"future": {CreatedAt: now.Add(time.Hour)},
		"stale":  {CreatedAt: now.Add(-headArtifactCacheMaxAge - time.Second)},
		"zero":   {},
	}
	pruneHeadArtifactCacheEntries(now, artifactEntries)
	if len(artifactEntries) != 1 {
		t.Fatalf("artifact prune retained %d entries, want only fresh", len(artifactEntries))
	}
	if _, ok := artifactEntries["fresh"]; !ok {
		t.Fatal("artifact prune removed fresh entry")
	}
}

// TestAssembleReportGroupsCommitsAfterRoundTrip verifies the end-to-end consequence:
// after a cache round-trip, assembleReport still attaches commits to their beads.
func TestAssembleReportGroupsCommitsAfterRoundTrip(t *testing.T) {
	art := &historyArtifact{
		Commits: []CorrelatedCommit{
			{SHA: "aaa", BeadID: "bv-9", Method: "explicit", Confidence: 0.9},
			{SHA: "bbb", BeadID: "bv-9", Method: "temporal", Confidence: 0.5},
		},
	}
	b, _ := json.Marshal(art)
	var rt historyArtifact
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatal(err)
	}
	c := &Correlator{}
	beads := []BeadInfo{{ID: "bv-9", Title: "t", Status: "open"}}
	report := c.assembleReport(beads, CorrelatorOptions{}, &rt)
	h, ok := report.Histories["bv-9"]
	if !ok {
		t.Fatalf("no history for bv-9")
	}
	if len(h.Commits) != 2 {
		t.Errorf("bv-9 commits after round-trip: got %d want 2 (BeadID grouping broken)", len(h.Commits))
	}
	if len(report.CommitIndex) == 0 {
		t.Errorf("CommitIndex empty after round-trip (reverse lookup broken)")
	}
}

func TestCorrelationCacheNamespaceCanonicalAndIsolated(t *testing.T) {
	repo := t.TempDir()
	relPrimary := filepath.Join(".beads", "issues.jsonl")
	absPrimary := filepath.Join(repo, relPrimary)

	relNamespace := correlationCacheNamespace(repo, relPrimary)
	absNamespace := correlationCacheNamespace(filepath.Join(repo, "."), absPrimary)
	if relNamespace != absNamespace {
		t.Fatalf("equivalent primary paths produced different namespaces: relative=%q absolute=%q", relNamespace, absNamespace)
	}

	legacyNamespace := correlationCacheNamespace(repo, filepath.Join(".beads", "beads.jsonl"))
	if relNamespace == legacyNamespace {
		t.Fatal("different selected Beads histories shared a cache namespace")
	}

	otherRepoNamespace := correlationCacheNamespace(t.TempDir(), relPrimary)
	if relNamespace == otherRepoNamespace {
		t.Fatal("different repositories shared a cache namespace")
	}
}

func TestPersistentCorrelationCachesIsolateNamespaces(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	t.Setenv("BV_NO_CACHE", "")
	t.Setenv("BV_CACHE_DIR", t.TempDir())

	const (
		namespaceA = "repo-a:issues"
		namespaceB = "repo-b:issues"
		headSHA    = "same-head"
		beadsHash  = "same-beads"
		optsHash   = "same-options"
	)

	// The report cache validates entry.Report.DataHash against the beads hash
	// key, so distinguish the two namespaces via GitRange instead.
	putCorrelationDiskCachedReport(namespaceA, headSHA, beadsHash, optsHash, &HistoryReport{DataHash: beadsHash, GitRange: "report-a"})
	putCorrelationDiskCachedReport(namespaceB, headSHA, beadsHash, optsHash, &HistoryReport{DataHash: beadsHash, GitRange: "report-b"})
	for namespace, want := range map[string]string{namespaceA: "report-a", namespaceB: "report-b"} {
		got, ok := getCorrelationDiskCachedReport(namespace, headSHA, beadsHash, optsHash)
		if !ok || got.GitRange != want {
			t.Fatalf("report cache namespace %q = (%+v, %v), want git range %q", namespace, got, ok, want)
		}
	}

	putHeadArtifactCached(namespaceA, headSHA, optsHash, &historyArtifact{Events: []BeadEvent{{BeadID: "artifact-a"}}})
	putHeadArtifactCached(namespaceB, headSHA, optsHash, &historyArtifact{Events: []BeadEvent{{BeadID: "artifact-b"}}})
	for namespace, want := range map[string]string{namespaceA: "artifact-a", namespaceB: "artifact-b"} {
		got, ok := getHeadArtifactCached(namespace, headSHA, optsHash)
		if !ok || len(got.Events) != 1 || got.Events[0].BeadID != want {
			t.Fatalf("artifact cache namespace %q = (%+v, %v), want bead %q", namespace, got, ok, want)
		}
	}
}
