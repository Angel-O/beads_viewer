package main_test

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRobotAlerts_BasicAndFilters(t *testing.T) {
	bv := buildBvBinary(t)
	env := t.TempDir()

	now := time.Now().UTC()
	staleUpdated := now.AddDate(0, 0, -20).Format(time.RFC3339) // warning (default 14d)
	staleCreated := now.AddDate(0, 0, -25).Format(time.RFC3339) // keep valid ordering
	tombstoneUpdated := now.AddDate(0, 0, -20).Format(time.RFC3339)
	tombstoneCreated := now.AddDate(0, 0, -25).Format(time.RFC3339)
	freshTime := now.AddDate(0, 0, -1).Format(time.RFC3339)

	// ROOT unblocks 3 issues => blocking_cascade (info); STALE triggers stale_issue (warning).
	writeBeads(t, env, fmt.Sprintf(
		`{"id":"ROOT","title":"Root","status":"open","priority":1,"issue_type":"task","created_at":"%s","updated_at":"%s"}
{"id":"D1","title":"Dep1","status":"open","priority":2,"issue_type":"task","created_at":"%s","updated_at":"%s","dependencies":[{"issue_id":"D1","depends_on_id":"ROOT","type":"blocks"}]}
{"id":"D2","title":"Dep2","status":"open","priority":2,"issue_type":"task","created_at":"%s","updated_at":"%s","dependencies":[{"issue_id":"D2","depends_on_id":"ROOT","type":"blocks"}]}
{"id":"D3","title":"Dep3","status":"open","priority":2,"issue_type":"task","created_at":"%s","updated_at":"%s","dependencies":[{"issue_id":"D3","depends_on_id":"ROOT","type":"blocks"}]}
{"id":"STALE","title":"Stale issue","status":"open","priority":3,"issue_type":"task","created_at":"%s","updated_at":"%s"}
{"id":"TOMBSTONE","title":"Removed","status":"tombstone","priority":3,"issue_type":"task","created_at":"%s","updated_at":"%s"}`,
		freshTime, freshTime,
		freshTime, freshTime,
		freshTime, freshTime,
		freshTime, freshTime,
		staleCreated, staleUpdated,
		tombstoneCreated, tombstoneUpdated,
	))

	type alert struct {
		Type     string `json:"type"`
		Severity string `json:"severity"`
		IssueID  string `json:"issue_id"`
	}
	type payload struct {
		DataHash string  `json:"data_hash"`
		Alerts   []alert `json:"alerts"`
		Summary  struct {
			Total    int `json:"total"`
			Critical int `json:"critical"`
			Warning  int `json:"warning"`
			Info     int `json:"info"`
		} `json:"summary"`
	}

	run := func(args ...string) payload {
		t.Helper()
		cmd := exec.Command(bv, args...)
		cmd.Dir = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		var p payload
		if err := json.Unmarshal(out, &p); err != nil {
			t.Fatalf("json decode: %v\nout=%s", err, out)
		}
		return p
	}

	// Unfiltered output should include at least one stale and one cascade alert.
	base := run("--robot-alerts")
	if base.DataHash == "" {
		t.Fatalf("missing data_hash")
	}
	if base.Summary.Total != len(base.Alerts) {
		t.Fatalf("summary.total=%d; want %d", base.Summary.Total, len(base.Alerts))
	}
	foundStale := false
	foundCascade := false
	foundTombstone := false
	for _, a := range base.Alerts {
		if a.Type == "stale_issue" && a.Severity == "warning" && a.IssueID == "STALE" {
			foundStale = true
		}
		if a.Type == "stale_issue" && a.IssueID == "TOMBSTONE" {
			foundTombstone = true
		}
		if a.Type == "blocking_cascade" && a.IssueID == "ROOT" {
			foundCascade = true
		}
	}
	if !foundStale {
		t.Fatalf("expected stale_issue warning for STALE, got %+v", base.Alerts)
	}
	if foundTombstone {
		t.Fatalf("did not expect stale_issue for TOMBSTONE, got %+v", base.Alerts)
	}
	if !foundCascade {
		t.Fatalf("expected blocking_cascade for ROOT, got %+v", base.Alerts)
	}

	// Type filter.
	onlyStale := run("--robot-alerts", "--alert-type=stale_issue")
	if len(onlyStale.Alerts) == 0 {
		t.Fatalf("expected stale_issue alerts, got 0")
	}
	for _, a := range onlyStale.Alerts {
		if a.Type != "stale_issue" {
			t.Fatalf("unexpected alert type %q in filtered output: %+v", a.Type, a)
		}
	}

	// Severity filter.
	onlyWarning := run("--robot-alerts", "--severity=warning")
	if len(onlyWarning.Alerts) == 0 {
		t.Fatalf("expected warning alerts, got 0")
	}
	for _, a := range onlyWarning.Alerts {
		if a.Severity != "warning" {
			t.Fatalf("unexpected severity %q in filtered output: %+v", a.Severity, a)
		}
	}
}

func TestRobotAlerts_UsesBaselineWhenPresent(t *testing.T) {
	bv := buildBvBinary(t)
	env := t.TempDir()

	now := time.Now().UTC()
	ts := now.Add(-1 * time.Hour).Format(time.RFC3339) // stable, non-stale timestamp

	// Start with a single issue and save a baseline.
	writeBeads(t, env, fmt.Sprintf(
		`{"id":"A","title":"A","status":"open","priority":1,"issue_type":"task","created_at":"%s","updated_at":"%s"}`,
		ts, ts,
	))
	save := exec.Command(bv, "--save-baseline", "test baseline")
	save.Dir = env
	if out, err := save.CombinedOutput(); err != nil {
		t.Fatalf("save baseline failed: %v\n%s", err, out)
	}

	// Change the graph: add a second issue (node_count_change drift).
	writeBeads(t, env, fmt.Sprintf(
		`{"id":"A","title":"A","status":"open","priority":1,"issue_type":"task","created_at":"%s","updated_at":"%s"}
{"id":"B","title":"B","status":"open","priority":1,"issue_type":"task","created_at":"%s","updated_at":"%s"}`,
		ts, ts, ts, ts,
	))

	type alert struct {
		Type     string `json:"type"`
		Severity string `json:"severity"`
	}
	type payload struct {
		Alerts []alert `json:"alerts"`
	}

	cmd := exec.Command(bv, "--robot-alerts")
	cmd.Dir = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("robot-alerts failed: %v\n%s", err, out)
	}
	var p payload
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("json decode: %v\nout=%s", err, out)
	}

	found := false
	for _, a := range p.Alerts {
		if a.Type == "node_count_change" {
			found = true
			if a.Severity != "info" {
				t.Fatalf("expected node_count_change severity=info, got %q", a.Severity)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected node_count_change in alerts, got %+v", p.Alerts)
	}
}

// TestRobotAlerts_ProactiveTypesAndLabelFilter covers the emitters added for
// the README alert table (D7): every proactive type fires on one fixture,
// --alert-type isolates each, --alert-label keeps only alerts on issues that
// carry the label, and every alert carries a suggested_action.
func TestRobotAlerts_ProactiveTypesAndLabelFilter(t *testing.T) {
	bv := buildBvBinary(t)
	env := t.TempDir()

	now := time.Now().UTC()
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	idle := now.AddDate(0, 0, -20).Format(time.RFC3339)
	created := now.AddDate(0, 0, -40).Format(time.RFC3339)
	priorWindow := now.AddDate(0, 0, -10).Format(time.RFC3339)
	recentWindow := now.AddDate(0, 0, -2).Format(time.RFC3339)

	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	// high_impact_unblock + blocking_cascade: HUB (P4, label backend) unblocks two P0 items and one P3.
	add(`{"id":"HUB","title":"Hub","status":"open","priority":4,"issue_type":"task","labels":["backend"],"created_at":"%s","updated_at":"%s"}`, created, fresh)
	for i, p := range []int{0, 0, 3} {
		add(`{"id":"LEAF-%d","title":"Leaf %d","status":"open","priority":%d,"issue_type":"task","created_at":"%s","updated_at":"%s","dependencies":[{"issue_id":"LEAF-%d","depends_on_id":"HUB","type":"blocks"}]}`, i, i, p, created, fresh, i)
	}
	// abandoned_claim: claimed and idle for 20 days (label ops).
	add(`{"id":"CLAIMED","title":"Claimed and forgotten","status":"in_progress","priority":2,"issue_type":"task","assignee":"agent-7","labels":["ops"],"created_at":"%s","updated_at":"%s"}`, created, idle)
	// potential_duplicate: two near-identical titles.
	add(`{"id":"DUP-A","title":"Fix login timeout on slow networks","status":"open","priority":2,"issue_type":"bug","created_at":"%s","updated_at":"%s"}`, created, fresh)
	add(`{"id":"DUP-B","title":"Fix login timeout on slow networks","status":"open","priority":2,"issue_type":"bug","created_at":"%s","updated_at":"%s"}`, created, fresh)
	// velocity_drop: six closes 10 days ago, one 2 days ago.
	for i := 0; i < 6; i++ {
		add(`{"id":"OLD-%d","title":"Old close %d","status":"closed","priority":2,"issue_type":"task","created_at":"%s","updated_at":"%s","closed_at":"%s"}`, i, i, created, priorWindow, priorWindow)
	}
	add(`{"id":"NEW-0","title":"Recent close","status":"closed","priority":2,"issue_type":"task","created_at":"%s","updated_at":"%s","closed_at":"%s"}`, created, recentWindow, recentWindow)
	writeBeads(t, env, strings.Join(lines, "\n"))

	type alert struct {
		Type            string   `json:"type"`
		Severity        string   `json:"severity"`
		IssueID         string   `json:"issue_id"`
		RelatedIssueID  string   `json:"related_issue_id"`
		Labels          []string `json:"labels"`
		SuggestedAction string   `json:"suggested_action"`
	}
	type payload struct {
		Alerts     []alert  `json:"alerts"`
		UsageHints []string `json:"usage_hints"`
	}
	run := func(args ...string) payload {
		t.Helper()
		cmd := exec.Command(bv, args...)
		cmd.Dir = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		var p payload
		if err := json.Unmarshal(out, &p); err != nil {
			t.Fatalf("json decode: %v\nout=%s", err, out)
		}
		return p
	}

	all := run("--robot-alerts")
	byType := map[string][]alert{}
	for _, a := range all.Alerts {
		byType[a.Type] = append(byType[a.Type], a)
		if a.SuggestedAction == "" {
			t.Errorf("alert without suggested_action: %+v", a)
		}
	}
	t.Logf("alert types: %v", func() []string {
		var ts []string
		for k, v := range byType {
			ts = append(ts, fmt.Sprintf("%s=%d", k, len(v)))
		}
		return ts
	}())

	want := map[string]func(a alert) bool{
		"high_impact_unblock": func(a alert) bool { return a.IssueID == "HUB" && a.Severity == "warning" },
		"blocking_cascade":    func(a alert) bool { return a.IssueID == "HUB" },
		"priority_mismatch":   func(a alert) bool { return a.IssueID == "HUB" && a.Severity == "warning" },
		"abandoned_claim":     func(a alert) bool { return a.IssueID == "CLAIMED" && a.Severity == "warning" },
		"potential_duplicate": func(a alert) bool { return a.IssueID != "" && a.RelatedIssueID != "" && a.Severity == "info" },
		"velocity_drop":       func(a alert) bool { return a.Severity == "warning" },
	}
	for typ, ok := range want {
		matched := false
		for _, a := range byType[typ] {
			if ok(a) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("expected a %s alert matching the fixture, got %+v", typ, byType[typ])
		}
		only := run("--robot-alerts", "--alert-type="+typ)
		if len(only.Alerts) == 0 {
			t.Errorf("--alert-type=%s returned nothing", typ)
		}
		for _, a := range only.Alerts {
			if a.Type != typ {
				t.Errorf("--alert-type=%s leaked %s", typ, a.Type)
			}
		}
	}

	backend := run("--robot-alerts", "--alert-label=backend")
	if len(backend.Alerts) == 0 {
		t.Fatalf("--alert-label=backend should keep HUB's alerts")
	}
	for _, a := range backend.Alerts {
		if a.IssueID != "HUB" {
			t.Errorf("--alert-label=backend leaked an alert on %s (%s): labels=%v", a.IssueID, a.Type, a.Labels)
		}
	}
	ops := run("--robot-alerts", "--alert-label=OPS")
	if len(ops.Alerts) == 0 {
		t.Fatalf("--alert-label match should be case-insensitive")
	}
	for _, a := range ops.Alerts {
		if a.IssueID != "CLAIMED" {
			t.Errorf("--alert-label=ops leaked an alert on %s (%s)", a.IssueID, a.Type)
		}
	}
	if none := run("--robot-alerts", "--alert-label=no-such-label"); len(none.Alerts) != 0 {
		t.Errorf("unknown label should filter everything out, got %+v", none.Alerts)
	}
	hinted := false
	for _, h := range all.UsageHints {
		if strings.Contains(h, "suggested_action") {
			hinted = true
		}
	}
	if !hinted {
		t.Errorf("usage_hints should mention suggested_action: %v", all.UsageHints)
	}
}
