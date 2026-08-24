package correlation

import (
	"os"
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

func TestCorrelationDiskCachesRoundTripAndRetainEntries(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	t.Setenv("BV_NO_CACHE", "")
	t.Setenv("BV_CACHE_DIR", t.TempDir())

	firstArtifact := &historyArtifact{
		Events:  []BeadEvent{{BeadID: "bv-1", EventType: EventCreated}},
		Commits: []CorrelatedCommit{{SHA: "aaa", BeadID: "bv-1"}},
	}
	secondArtifact := &historyArtifact{
		Events:  []BeadEvent{{BeadID: "bv-2", EventType: EventClosed}},
		Commits: []CorrelatedCommit{{SHA: "bbb", BeadID: "bv-2"}},
	}
	putHeadArtifactCached("head-1", "opts", firstArtifact)
	putHeadArtifactCached("head-2", "opts", secondArtifact)

	for _, tc := range []struct {
		head string
		want string
	}{
		{head: "head-1", want: "bv-1"},
		{head: "head-2", want: "bv-2"},
	} {
		got, ok := getHeadArtifactCached(tc.head, "opts")
		if !ok {
			t.Fatalf("head artifact %s missing after second insertion", tc.head)
		}
		if len(got.Commits) != 1 || got.Commits[0].BeadID != tc.want {
			t.Fatalf("head artifact %s round-trip: got %+v, want bead %s", tc.head, got.Commits, tc.want)
		}
	}

	firstReport := &HistoryReport{DataHash: "data-1", GitRange: "range-1"}
	secondReport := &HistoryReport{DataHash: "data-2", GitRange: "range-2"}
	putCorrelationDiskCachedReport("head", "beads-1", "opts", firstReport)
	putCorrelationDiskCachedReport("head", "beads-2", "opts", secondReport)

	for _, tc := range []struct {
		beadsHash string
		want      string
	}{
		{beadsHash: "beads-1", want: "data-1"},
		{beadsHash: "beads-2", want: "data-2"},
	} {
		got, ok := getCorrelationDiskCachedReport("head", tc.beadsHash, "opts")
		if !ok {
			t.Fatalf("report %s missing after second insertion", tc.beadsHash)
		}
		if got.DataHash != tc.want {
			t.Fatalf("report %s round-trip: got data hash %q, want %q", tc.beadsHash, got.DataHash, tc.want)
		}
	}
}

func TestCorrelationCachePutRejectsOversizedEntriesWithoutLosingExistingEntries(t *testing.T) {
	if headArtifactCacheMaxEntrySize != 64<<20 || correlationDiskCacheMaxEntrySize != 64<<20 {
		t.Fatalf("unexpected production cache ceilings: artifact=%d report=%d", headArtifactCacheMaxEntrySize, correlationDiskCacheMaxEntrySize)
	}
	t.Setenv("BV_ROBOT", "1")
	t.Setenv("BV_NO_CACHE", "")
	t.Setenv("BV_CACHE_DIR", t.TempDir())

	validArtifact := &historyArtifact{Events: []BeadEvent{{BeadID: "kept-artifact"}}}
	putHeadArtifactCached("kept-head", "opts", validArtifact)
	oversizedArtifact := &historyArtifact{
		Events: []BeadEvent{{CommitMsg: "entry exceeds the planted test ceiling"}},
	}
	putHeadArtifactCachedWithMaxEntrySize("oversized-head", "opts", oversizedArtifact, 16)
	if _, ok := getHeadArtifactCached("oversized-head", "opts"); ok {
		t.Fatal("oversized head artifact was persisted")
	}
	if got, ok := getHeadArtifactCached("kept-head", "opts"); !ok || len(got.Events) != 1 || got.Events[0].BeadID != "kept-artifact" {
		t.Fatalf("existing head artifact was lost after oversized rejection: ok=%t artifact=%+v", ok, got)
	}

	validReport := &HistoryReport{DataHash: "kept-report"}
	putCorrelationDiskCachedReport("head", "kept-beads", "opts", validReport)
	oversizedReport := &HistoryReport{GitRange: "entry exceeds the planted test ceiling"}
	putCorrelationDiskCachedReportWithMaxEntrySize("head", "oversized-beads", "opts", oversizedReport, 16)
	if _, ok := getCorrelationDiskCachedReport("head", "oversized-beads", "opts"); ok {
		t.Fatal("oversized report was persisted")
	}
	if got, ok := getCorrelationDiskCachedReport("head", "kept-beads", "opts"); !ok || got.DataHash != "kept-report" {
		t.Fatalf("existing report was lost after oversized rejection: ok=%t report=%+v", ok, got)
	}
}

func TestCorrelationDiskCachesIgnoreNullPayloads(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	t.Setenv("BV_NO_CACHE", "")
	cacheDir := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cacheDir)

	now := time.Now().UTC()
	headPath, err := headArtifactCachePath(true)
	if err != nil {
		t.Fatal(err)
	}
	headFile, err := os.OpenFile(headPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	headCache := headArtifactCacheFile{
		Version: headArtifactCacheVersion,
		Entries: map[string]headArtifactCacheEntry{
			headArtifactCacheKey("head", "opts"): {
				CreatedAt: now,
				Artifact:  json.RawMessage("null"),
			},
		},
	}
	if err := writeHeadArtifactCacheLocked(headFile, headCache); err != nil {
		headFile.Close()
		t.Fatal(err)
	}
	if err := headFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := getHeadArtifactCached("head", "opts"); ok {
		t.Fatal("null head artifact payload unexpectedly produced a cache hit")
	}

	reportPath, err := correlationDiskCachePath(true)
	if err != nil {
		t.Fatal(err)
	}
	reportFile, err := os.OpenFile(reportPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	reportCache := correlationDiskCacheFile{
		Version: correlationDiskCacheVersion,
		Entries: map[string]correlationDiskCacheEntry{
			correlationDiskCacheKey("head", "beads", "opts"): {
				CreatedAt: now,
				Report:    json.RawMessage("null"),
			},
		},
	}
	if err := writeCorrelationDiskCacheLocked(reportFile, reportCache); err != nil {
		reportFile.Close()
		t.Fatal(err)
	}
	if err := reportFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := getCorrelationDiskCachedReport("head", "beads", "opts"); ok {
		t.Fatal("null report payload unexpectedly produced a cache hit")
	}
}
