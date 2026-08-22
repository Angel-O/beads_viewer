package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// Issue #192: cache validation must stay O(top-level entries). Journals and
// snapshots under .beads/ subdirectories (.br_history/, .br_recovery/, …) can
// hold thousands of files and never feed the graph, so their mtimes must not
// be consulted — and must not be able to invalidate a valid entry.
func TestBeadsTreeModTime_OnlyConsultsTopLevelFiles(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(filepath.Join(beadsDir, ".br_history"), 0o755); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-6 * time.Hour).Truncate(time.Second)
	dataMtime := base.Add(time.Hour)
	journalMtime := base.Add(3 * time.Hour) // newer than everything at top level

	dataFile := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(dataFile, []byte(`{"id":"A","title":"A","status":"open","issue_type":"task"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		p := filepath.Join(beadsDir, ".br_history", fmt.Sprintf("snapshot-%02d.jsonl", i))
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, journalMtime, journalMtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dataFile, dataMtime, dataMtime); err != nil {
		t.Fatal(err)
	}
	// Directory mtimes (including the journal dir) are older than the data file.
	if err := os.Chtimes(filepath.Join(beadsDir, ".br_history"), base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(beadsDir, base, base); err != nil {
		t.Fatal(err)
	}

	got := beadsTreeModTime(beadsDir)
	if !got.Equal(dataMtime) {
		t.Fatalf("beadsTreeModTime = %v, want top-level data mtime %v (journal mtime %v must be ignored)", got, dataMtime, journalMtime)
	}

	// A top-level data file rewritten in place (directory mtime unchanged)
	// still moves the result forward.
	newer := dataMtime.Add(30 * time.Minute)
	if err := os.Chtimes(dataFile, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(beadsDir, base, base); err != nil {
		t.Fatal(err)
	}
	if got := beadsTreeModTime(beadsDir); !got.Equal(newer) {
		t.Fatalf("after in-place rewrite beadsTreeModTime = %v, want %v", got, newer)
	}
}

// Issue #192: a plain cache miss (key absent, nothing expired) must be a pure
// read — it must not rewrite and fsync the whole cache file. The subsequent
// put after recompute is the only write on that path.
func TestRobotDiskCache_MissWithoutPruneDoesNotRewriteFile(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	cacheDir := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cacheDir)
	// Point staleness at an isolated, static .beads so the host cwd cannot
	// influence the check.
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DB", beadsDir)

	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen},
		{ID: "B", Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
	}
	config := ConfigForSize(2, 1)
	an := NewAnalyzer(issues)
	stats := an.AnalyzeAsyncWithConfig(context.Background(), config)
	stats.WaitForPhase2()

	cachePath := filepath.Join(cacheDir, robotAnalysisDiskCacheFileName)
	before, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("expected cache file after first analysis: %v", err)
	}
	// Pin the file's mtime in the past so any rewrite is unambiguous.
	pinned := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(cachePath, pinned, pinned); err != nil {
		t.Fatal(err)
	}

	if _, _, hit := getRobotDiskCachedStats("no-such-data-hash|no-such-config-hash"); hit {
		t.Fatal("unexpected hit for an absent key")
	}

	after, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(pinned) || after.Size() != before.Size() {
		t.Fatalf("plain miss rewrote the cache file: mtime %v→%v size %d→%d", pinned, after.ModTime(), before.Size(), after.Size())
	}

	// The genuine entry still hits (the miss above did not disturb it).
	key := an.DataHash() + "|" + ComputeConfigHash(&config)
	if cached, _, hit := getRobotDiskCachedStats(key); !hit || cached == nil {
		t.Fatalf("expected hit for %q after a miss on another key", key)
	}
}

// Expired entries must still be pruned from disk on the miss path, otherwise a
// repo that never gets a fresh put would carry them until max-entries eviction.
func TestRobotDiskCache_MissWithPruneRewritesFile(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	cacheDir := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cacheDir)
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DB", beadsDir)

	cachePath := filepath.Join(cacheDir, robotAnalysisDiskCacheFileName)
	f, err := os.OpenFile(cachePath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	expired := robotAnalysisDiskCacheFile{
		Version: robotAnalysisDiskCacheVersion,
		Entries: map[string]robotAnalysisDiskCacheEntry{
			"old|key": {CreatedAt: time.Now().Add(-2 * robotAnalysisDiskCacheMaxAge), Result: graphStatsCacheBlob{Status: MetricStatus{}}},
		},
	}
	if err := writeRobotDiskCacheLocked(f, expired); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, hit := getRobotDiskCachedStats("other|key"); hit {
		t.Fatal("unexpected hit")
	}

	g, err := os.Open(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	cf := readRobotDiskCacheLocked(g)
	if len(cf.Entries) != 0 {
		t.Fatalf("expired entry should have been pruned and persisted, got %d entries", len(cf.Entries))
	}
}
