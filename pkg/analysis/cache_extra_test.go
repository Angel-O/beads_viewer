package analysis

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestCacheSetTTLAndHash(t *testing.T) {
	issues := []model.Issue{{ID: "C1", Title: "Cache"}}
	c := NewCache(10 * time.Second)
	stats := &GraphStats{NodeCount: 1}
	c.Set(issues, stats)
	if c.Hash() == "" {
		t.Fatalf("expected hash after Set")
	}

	// Override TTL and ensure GetByHash respects expiry
	c.SetTTL(-1 * time.Second)
	if got, ok := c.Get(issues); got != nil || ok {
		t.Fatalf("expected cache miss after expired TTL")
	}
}

// TestGraphStatsCacheBlob_SoARoundTrip is a regression guard for the SoA
// (struct-of-arrays / dictionary-encoded) on-disk format: GraphStats →
// graphStatsCacheBlob → JSON (compact columnar) → graphStatsCacheBlob must
// reproduce value-identical metric maps, including the sparse and nil cases
// that distinguish absent vs. present-zero and nil vs. empty maps.
func TestGraphStatsCacheBlob_SoARoundTrip(t *testing.T) {
	cases := map[string]graphStatsCacheBlob{
		"dense": {
			OutDegree:        map[string]int{"A": 1, "B": 0, "C": 3},
			InDegree:         map[string]int{"A": 0, "B": 2, "C": 1},
			TopologicalOrder: []string{"A", "B", "C"},
			Density:          0.5,
			NodeCount:        3,
			EdgeCount:        4,
			PageRank:         map[string]float64{"A": 0.1, "B": 0.0, "C": 0.4},
			Betweenness:      map[string]float64{"A": 0, "B": 0, "C": 0},
			Eigenvector:      map[string]float64{"A": 0.7, "B": 0.2, "C": 0.99},
			Hubs:             map[string]float64{"A": 1, "B": 2, "C": 3},
			Authorities:      map[string]float64{"A": 3, "B": 2, "C": 1},
			CriticalPathScore: map[string]float64{
				"A": 5.5, "B": 0, "C": 2.25,
			},
			CoreNumber:   map[string]int{"A": 2, "B": 1, "C": 2},
			Slack:        map[string]float64{"A": 0, "B": 1.5, "C": 0},
			Articulation: []string{"B"},
			Cycles:       [][]string{{"A", "C"}},
		},
		// Sparse: CoreNumber/Slack cover a subset of the node union; this must
		// stay distinct from present-zero after the round trip.
		"sparse_and_nil": {
			NodeCount:   3,
			OutDegree:   map[string]int{"X": 1, "Y": 2, "Z": 0},
			InDegree:    map[string]int{"X": 0, "Y": 0, "Z": 2},
			PageRank:    map[string]float64{"X": 0.3, "Y": 0.3, "Z": 0.4},
			CoreNumber:  map[string]int{"X": 1}, // sparse: only X
			Slack:       map[string]float64{"Z": 9.0},
			Betweenness: nil, // nil must round-trip back to nil
			Hubs:        map[string]float64{},
		},
		"empty": {
			OutDegree: map[string]int{},
			InDegree:  map[string]int{},
		},
	}

	for name, blob := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(blob)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got graphStatsCacheBlob
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			want := blob
			want.decoded = true
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round-trip mismatch:\n want %#v\n  got %#v", want, got)
			}
		})
	}
}

// TestGraphStatsCacheBlob_SoAStoresNodesOnce verifies the columnar layout: the
// serialized payload stores each node ID exactly once (in "nodes") rather than
// repeating it as a key in every per-node metric map.
func TestGraphStatsCacheBlob_SoAStoresNodesOnce(t *testing.T) {
	blob := graphStatsCacheBlob{
		OutDegree:   map[string]int{"NODE-001": 1, "NODE-002": 2},
		InDegree:    map[string]int{"NODE-001": 0, "NODE-002": 1},
		PageRank:    map[string]float64{"NODE-001": 0.5, "NODE-002": 0.5},
		Betweenness: map[string]float64{"NODE-001": 0.1, "NODE-002": 0.2},
	}
	data, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, id := range []string{"NODE-001", "NODE-002"} {
		n := 0
		for i := 0; i+len(id) <= len(s); i++ {
			if s[i:i+len(id)] == id {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("node %q appears %d times in SoA payload, want exactly 1 (columnar)", id, n)
		}
	}
}

// TestRobotDiskCache_VersionGate confirms an entry written by an older layout
// version, or one whose embedded key does not match the lookup (filename
// collision / foreign file), is treated as a miss and reaped — never served.
func TestRobotDiskCache_VersionGate(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	t.Setenv("BV_CACHE_DIR", t.TempDir())
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DB", beadsDir)

	dir, err := robotAnalysisDiskCacheDir(true)
	if err != nil {
		t.Fatal(err)
	}
	const key = "k|c"
	path := filepath.Join(dir, robotAnalysisEntryFileName(key))

	writeEntry := func(e robotAnalysisDiskCacheEntry) {
		t.Helper()
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Older layout version.
	writeEntry(robotAnalysisDiskCacheEntry{Version: robotAnalysisDiskCacheVersion - 1, Key: key, CreatedAt: time.Now().UTC()})
	if _, _, hit := getRobotDiskCachedStats(key); hit {
		t.Fatal("old-version entry must be a miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old-version entry file should be reaped, stat err = %v", err)
	}

	// Key mismatch.
	writeEntry(robotAnalysisDiskCacheEntry{Version: robotAnalysisDiskCacheVersion, Key: "other|key", CreatedAt: time.Now().UTC()})
	if _, _, hit := getRobotDiskCachedStats(key); hit {
		t.Fatal("key-mismatched entry must be a miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key-mismatched entry file should be reaped, stat err = %v", err)
	}
}

// TestExpandFloatIntNegativeIndexNoPanic guards a corrupt/hand-edited cache file
// with a NEGATIVE sparse index: it must degrade (drop the bad entry) rather than
// panic on nodes[-1] and crash the whole bv command.
func TestExpandFloatIntNegativeIndexNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on negative index (must degrade to a miss): %v", r)
		}
	}()
	nodes := []string{"a", "b"}
	fm := expandFloat(true, []int32{-1, 1}, []float64{9.0, 2.0}, nodes)
	if len(fm) != 1 || fm["b"] != 2.0 {
		t.Errorf("expandFloat: expected only the valid index kept, got %v", fm)
	}
	im := expandInt(true, []int32{-3}, []int{7}, nodes)
	if len(im) != 0 {
		t.Errorf("expandInt: expected empty (only negative index), got %v", im)
	}
}

func TestRobotDiskCacheRejectsStructurallyCorruptSoA(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	cacheDir := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cacheDir)
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DB", beadsDir)

	dir, err := robotAnalysisDiskCacheDir(true)
	if err != nil {
		t.Fatal(err)
	}
	const dataHash = "data-hash"
	configHash := ComputeConfigHash(&AnalysisConfig{})
	key := dataHash + "|" + configHash
	path := filepath.Join(dir, robotAnalysisEntryFileName(key))
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)

	tests := []struct {
		name   string
		result string
	}{
		{name: "missing result", result: ""},
		{name: "wrong inner version", result: `{"v":2,"nodes":[],"node_count":0}`},
		{name: "short dense column", result: `{"v":3,"nodes":["a","b"],"node_count":2,"od_set":true,"od_idx":null,"od":[1]}`},
		{name: "mismatched sparse columns", result: `{"v":3,"nodes":["a","b"],"node_count":2,"od_set":true,"od_idx":[0,1],"od":[1]}`},
		{name: "out of range sparse index", result: `{"v":3,"nodes":["a","b"],"node_count":2,"od_set":true,"od_idx":[2],"od":[1]}`},
		{name: "duplicate sparse index", result: `{"v":3,"nodes":["a","b"],"node_count":2,"od_set":true,"od_idx":[0,0],"od":[1,2]}`},
		{name: "unsorted node dictionary", result: `{"v":3,"nodes":["b","a"],"node_count":2}`},
		{name: "missing required phase1 degrees", result: `{"v":3,"nodes":["a"],"node_count":1}`},
		{name: "unknown topological node", result: `{"v":3,"nodes":["a"],"node_count":1,"topological_order":["missing"],"od_set":true,"od_idx":null,"od":[0],"id_set":true,"id_idx":null,"id":[0]}`},
		{name: "result config does not match key", result: `{"v":3,"nodes":[],"node_count":0,"config":{"ComputePageRank":true},"od_set":true,"od_idx":null,"id_set":true,"id_idx":null}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultField := ""
			if tt.result != "" {
				resultField = `,"result":` + tt.result
			}
			raw := fmt.Sprintf(`{"version":%d,"key":%q,"created_at":%q,"data_hash":%q,"config_hash":%q%s}`,
				robotAnalysisDiskCacheVersion, key, createdAt, dataHash, configHash, resultField)
			if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}

			if stats, _, hit := getRobotDiskCachedStats(key); hit || stats != nil {
				t.Fatalf("corrupt cache returned hit=%v stats=%p", hit, stats)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("corrupt cache entry was not reaped: %v", err)
			}
		})
	}
}

func TestRobotDiskCacheRejectsOversizedEntryBeforeDecode(t *testing.T) {
	t.Setenv("BV_ROBOT", "1")
	cacheDir := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cacheDir)
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DB", beadsDir)

	dir, err := robotAnalysisDiskCacheDir(true)
	if err != nil {
		t.Fatal(err)
	}
	const key = "oversized|entry"
	path := filepath.Join(dir, robotAnalysisEntryFileName(key))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(robotAnalysisDiskCacheMaxEntrySize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if stats, _, hit := getRobotDiskCachedStats(key); hit || stats != nil {
		t.Fatalf("oversized cache returned hit=%v stats=%p", hit, stats)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("oversized cache entry was not reaped: %v", err)
	}
}

func TestRemoveRobotDiskCacheEntryIfSamePreservesConcurrentReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entry.json")
	if err := os.WriteFile(path, []byte("old corrupt entry"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	replacement := filepath.Join(dir, "replacement.json")
	if err := os.WriteFile(replacement, []byte("new valid entry"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}

	removeRobotDiskCacheEntryIfSame(path, oldInfo)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("concurrent replacement was removed: %v", err)
	}
	if string(got) != "new valid entry" {
		t.Fatalf("replacement content=%q, want preserved valid entry", got)
	}
}
