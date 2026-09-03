package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestToonSize_DocumentedWinsStaySmaller (I6) re-measures TOON against JSON
// on this repository. README documents --robot-graph as the payload where
// TOON is smaller; the test fails if TOON grows to more than 110% of JSON for
// it, and logs the full table so the README numbers can be refreshed. The
// commands where JSON is smaller are logged only: that is the documented
// state, not a failure.
func TestToonSize_DocumentedWinsStaySmaller(t *testing.T) {
	if _, err := exec.LookPath("toon"); err != nil {
		t.Skip("toon CLI not installed; --format toon would fall back to JSON")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".beads", "issues.jsonl")); err != nil {
		t.Skip("repository tracker not present")
	}
	bv := buildBvBinary(t)

	size := func(args ...string) int {
		t.Helper()
		cmd := exec.Command(bv, args...)
		cmd.Dir = repo
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return len(out)
	}

	wins := map[string]bool{"--robot-graph": true}
	for _, command := range []string{"--robot-graph", "--robot-next", "--robot-alerts", "--robot-label-health", "--robot-insights", "--robot-triage", "--robot-plan"} {
		jsonBytes := size(command)
		toonBytes := size(command, "--format", "toon")
		ratio := float64(toonBytes) / float64(jsonBytes)
		t.Logf("%-22s json=%7d toon=%7d ratio=%.2f", command, jsonBytes, toonBytes, ratio)
		if wins[command] && ratio > 1.10 {
			t.Errorf("%s is documented as a TOON win but TOON is %.0f%% of JSON; update README and tests/artifacts/perf/toon_vs_json.md", command, ratio*100)
		}
	}

	// --stats must state the direction honestly.
	cmd := exec.Command(bv, "--robot-triage", "--format", "toon", "--stats")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--stats run failed: %v\n%s", err, out)
	}
	stats := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "[stats]") {
			stats = line
		}
	}
	if stats == "" || !(strings.Contains(stats, "smaller") || strings.Contains(stats, "larger") || strings.Contains(stats, "same size")) {
		t.Fatalf("--stats should say whether TOON is smaller or larger, got %q", stats)
	}
	if strings.Contains(stats, "0% savings") {
		t.Fatalf("--stats still reports the misleading 0%% savings: %q", stats)
	}
}
