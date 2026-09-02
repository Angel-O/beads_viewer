package drift

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/baseline"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestCalculatorNoDrift(t *testing.T) {
	bl := &baseline.Baseline{
		Version:   1,
		CreatedAt: time.Now(),
		Stats: baseline.GraphStats{
			NodeCount:       100,
			EdgeCount:       200,
			Density:         0.02,
			OpenCount:       50,
			ClosedCount:     40,
			BlockedCount:    10,
			CycleCount:      0,
			ActionableCount: 40,
		},
	}

	// Current matches baseline
	current := &baseline.Baseline{
		Version:   1,
		CreatedAt: time.Now(),
		Stats:     bl.Stats,
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	if result.HasDrift {
		t.Errorf("expected no drift, got %d alerts", len(result.Alerts))
	}
}

func TestCalculatorNewCycle(t *testing.T) {
	bl := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10, EdgeCount: 15},
		Cycles: [][]string{},
	}

	current := &baseline.Baseline{
		Stats:  bl.Stats,
		Cycles: [][]string{{"A", "B", "C", "A"}},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	if !result.HasDrift {
		t.Error("expected drift to be detected")
	}

	if result.CriticalCount != 1 {
		t.Errorf("expected 1 critical alert, got %d", result.CriticalCount)
	}

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertNewCycle {
			found = true
			if alert.Severity != SeverityCritical {
				t.Errorf("new cycle should be critical, got %s", alert.Severity)
			}
		}
	}
	if !found {
		t.Error("expected new_cycle alert")
	}
}

func TestCalculatorSetNowPinsAllAlertTimesAndStaleness(t *testing.T) {
	pinned := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	bl := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10, EdgeCount: 10},
		Cycles: [][]string{},
	}
	current := &baseline.Baseline{
		Stats:  bl.Stats,
		Cycles: [][]string{{"A", "B", "A"}},
	}
	calc := NewCalculator(bl, current, nil)
	calc.SetNow(pinned)
	calc.SetIssues([]model.Issue{{
		ID:        "STALE",
		Status:    model.StatusOpen,
		UpdatedAt: pinned.Add(-40 * 24 * time.Hour),
	}})

	result := calc.Calculate()
	if len(result.Alerts) < 2 {
		t.Fatalf("expected cycle and staleness alerts, got %#v", result.Alerts)
	}
	for _, alert := range result.Alerts {
		if !alert.DetectedAt.Equal(pinned) {
			t.Fatalf("alert %s detected_at = %v, want %v", alert.Type, alert.DetectedAt, pinned)
		}
	}
}

func TestCalculatorSetNowAcceptsZeroEpoch(t *testing.T) {
	bl := &baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 1}}
	current := &baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 2}}
	calc := NewCalculator(bl, current, nil)
	calc.SetNow(time.Time{})
	result := calc.Calculate()
	if len(result.Alerts) == 0 {
		t.Fatal("expected node-count drift alert")
	}
	for _, alert := range result.Alerts {
		if !alert.DetectedAt.IsZero() {
			t.Fatalf("alert detected_at=%v, want zero epoch", alert.DetectedAt)
		}
	}
}

func TestCalculatorDensityGrowth(t *testing.T) {
	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			EdgeCount: 200,
			Density:   0.02,
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			EdgeCount: 400,
			Density:   0.04, // 100% increase - definitely above 50% warning threshold
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertDensityGrowth {
			found = true
			if alert.Severity != SeverityWarning {
				t.Errorf("100%% density increase should be warning, got %s", alert.Severity)
			}
		}
	}
	if !found {
		t.Error("expected density_growth alert")
	}
}

func TestCalculatorBlockedIncrease(t *testing.T) {
	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:    100,
			BlockedCount: 5,
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:    100,
			BlockedCount: 15, // +10
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertBlockedIncrease {
			found = true
			if alert.Severity != SeverityWarning {
				t.Errorf("blocked increase should be warning, got %s", alert.Severity)
			}
		}
	}
	if !found {
		t.Error("expected blocked_increase alert")
	}
}

func TestCalculatorPageRankChange(t *testing.T) {
	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{NodeCount: 100},
		TopMetrics: baseline.TopMetrics{
			PageRank: []baseline.MetricItem{
				{ID: "TASK-1", Value: 0.2},
				{ID: "TASK-2", Value: 0.15},
			},
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{NodeCount: 100},
		TopMetrics: baseline.TopMetrics{
			PageRank: []baseline.MetricItem{
				{ID: "TASK-1", Value: 0.35}, // 75% increase
				{ID: "TASK-3", Value: 0.18}, // New entry
			},
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertPageRankChange {
			found = true
		}
	}
	if !found {
		t.Error("expected pagerank_change alert")
	}
}

func TestCalculatorStalenessWarningAndCritical(t *testing.T) {
	now := time.Now().UTC()
	issues := []model.Issue{
		{ID: "OLD-WARN", Status: model.StatusOpen, UpdatedAt: now.Add(-16 * 24 * time.Hour)},
		{ID: "OLD-CRIT", Status: model.StatusOpen, UpdatedAt: now.Add(-35 * 24 * time.Hour)},
		{ID: "INPROG", Status: model.StatusInProgress, UpdatedAt: now.Add(-8 * 24 * time.Hour)},
	}

	bl := &baseline.Baseline{Stats: baseline.GraphStats{}}
	current := &baseline.Baseline{Stats: baseline.GraphStats{}}
	calc := NewCalculator(bl, current, nil)
	calc.SetIssues(issues)

	result := calc.Calculate()

	var warnCount, critCount int
	for _, a := range result.Alerts {
		if a.Type != AlertStaleIssue {
			continue
		}
		if a.Severity == SeverityWarning {
			warnCount++
		}
		if a.Severity == SeverityCritical {
			critCount++
		}
	}

	if warnCount != 2 { // OLD-WARN + INPROG (in_progress threshold tightened)
		t.Fatalf("expected 2 warning staleness alerts, got %d", warnCount)
	}
	if critCount != 1 {
		t.Fatalf("expected 1 critical staleness alert, got %d", critCount)
	}
}

func TestCalculatorBlockingCascade(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Blocker A", Status: model.StatusOpen},
		{ID: "B", Title: "Blocked by A", Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
		{ID: "C", Title: "Also blocked by A", Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
		{ID: "D", Title: "Independent", Status: model.StatusOpen},
	}
	bl := &baseline.Baseline{Stats: baseline.GraphStats{}}
	current := &baseline.Baseline{Stats: baseline.GraphStats{}}
	cfg := DefaultConfig()
	cfg.BlockingCascadeInfo = 2
	cfg.BlockingCascadeWarning = 3

	calc := NewCalculator(bl, current, cfg)
	calc.SetIssues(issues)

	result := calc.Calculate()

	var cascade Alert
	found := false
	for _, a := range result.Alerts {
		if a.Type == AlertBlockingCascade && a.IssueID == "A" {
			found = true
			cascade = a
		}
	}
	if !found {
		t.Fatalf("expected blocking cascade alert for A")
	}
	if cascade.Severity != SeverityInfo {
		t.Fatalf("expected info severity, got %s", cascade.Severity)
	}
	if len(cascade.Details) != 2 {
		t.Fatalf("expected 2 downstream ids, got %d", len(cascade.Details))
	}
	// Verify new fields (bv-165)
	if cascade.UnblocksCount != 2 {
		t.Fatalf("expected UnblocksCount=2, got %d", cascade.UnblocksCount)
	}
}

// TestCalculatorBlockingCascadeWithPriorities verifies the downstream priority sum calculation (bv-165)
func TestCalculatorBlockingCascadeWithPriorities(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Blocker A", Status: model.StatusOpen, Priority: 2},
		{ID: "B", Title: "Blocked by A (P1)", Status: model.StatusOpen, Priority: 1, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
		{ID: "C", Title: "Blocked by A (P3)", Status: model.StatusOpen, Priority: 3, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
		{ID: "D", Title: "Blocked by A (P0 critical)", Status: model.StatusOpen, Priority: 0, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
	}
	bl := &baseline.Baseline{Stats: baseline.GraphStats{}}
	current := &baseline.Baseline{Stats: baseline.GraphStats{}}
	cfg := DefaultConfig()
	cfg.BlockingCascadeInfo = 2
	cfg.BlockingCascadeWarning = 5

	calc := NewCalculator(bl, current, cfg)
	calc.SetIssues(issues)

	result := calc.Calculate()

	var cascade Alert
	found := false
	for _, a := range result.Alerts {
		if a.Type == AlertBlockingCascade && a.IssueID == "A" {
			found = true
			cascade = a
		}
	}
	if !found {
		t.Fatalf("expected blocking cascade alert for A")
	}

	// Verify UnblocksCount
	if cascade.UnblocksCount != 3 {
		t.Fatalf("expected UnblocksCount=3, got %d", cascade.UnblocksCount)
	}

	// Verify DownstreamPrioritySum: P1 + P3 + P0 = 1 + 3 + 0 = 4
	expectedPrioritySum := 4
	if cascade.DownstreamPrioritySum != expectedPrioritySum {
		t.Fatalf("expected DownstreamPrioritySum=%d, got %d", expectedPrioritySum, cascade.DownstreamPrioritySum)
	}
}

func TestResultSummary(t *testing.T) {
	result := &Result{
		HasDrift: true,
		Alerts: []Alert{
			{Type: AlertNewCycle, Severity: SeverityCritical, Message: "New cycle"},
			{Type: AlertDensityGrowth, Severity: SeverityWarning, Message: "Density up"},
		},
		CriticalCount: 1,
		WarningCount:  1,
	}

	summary := result.Summary()

	if !strings.Contains(summary, "CRITICAL") {
		t.Error("summary should mention critical")
	}
	if !strings.Contains(summary, "WARNING") {
		t.Error("summary should mention warning")
	}
}

func TestResultExitCode(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		expected int
	}{
		{"no drift", &Result{}, 0},
		{"info only", &Result{HasDrift: true, InfoCount: 1}, 0},
		{"warning", &Result{HasDrift: true, WarningCount: 1}, 2},
		{"critical", &Result{HasDrift: true, CriticalCount: 1}, 1},
		{"critical and warning", &Result{HasDrift: true, CriticalCount: 1, WarningCount: 1}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.ExitCode(); got != tt.expected {
				t.Errorf("ExitCode() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestResultHasCritical(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		expected bool
	}{
		{"no alerts", &Result{}, false},
		{"info only", &Result{InfoCount: 5}, false},
		{"warning only", &Result{WarningCount: 3}, false},
		{"critical", &Result{CriticalCount: 1}, true},
		{"critical with others", &Result{CriticalCount: 2, WarningCount: 1, InfoCount: 3}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasCritical(); got != tt.expected {
				t.Errorf("HasCritical() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestResultHasWarnings(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		expected bool
	}{
		{"no alerts", &Result{}, false},
		{"info only", &Result{InfoCount: 5}, false},
		{"warning only", &Result{WarningCount: 1}, true},
		{"critical only", &Result{CriticalCount: 1}, true},
		{"warning and info", &Result{WarningCount: 2, InfoCount: 3}, true},
		{"critical and warning", &Result{CriticalCount: 1, WarningCount: 2}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasWarnings(); got != tt.expected {
				t.Errorf("HasWarnings() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExampleConfig(t *testing.T) {
	example := ExampleConfig()

	// Should not be empty
	if example == "" {
		t.Error("ExampleConfig() returned empty string")
	}

	// Should be valid YAML that can be parsed
	var config Config
	if err := yaml.Unmarshal([]byte(example), &config); err != nil {
		t.Errorf("ExampleConfig() returned invalid YAML: %v", err)
	}

	// Should contain expected keys
	expectedKeys := []string{
		"density_warning_pct",
		"density_info_pct",
		"blocked_increase_threshold",
		"pagerank_change_warning_pct",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(example, key) {
			t.Errorf("ExampleConfig() should contain %q", key)
		}
	}

	// Parsed config should have reasonable values
	if config.DensityWarningPct <= 0 {
		t.Error("ExampleConfig() density_warning_pct should be positive")
	}
	if config.BlockedIncreaseThreshold < 0 {
		t.Error("ExampleConfig() blocked_increase_threshold should be non-negative")
	}
}

func TestConfigLoadDefault(t *testing.T) {
	tmpDir := t.TempDir()

	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Should return defaults
	if config.DensityWarningPct != 50 {
		t.Errorf("expected default density_warning_pct=50, got %f", config.DensityWarningPct)
	}
}

func TestConfigLoadCustom(t *testing.T) {
	tmpDir := t.TempDir()
	bvDir := filepath.Join(tmpDir, ".bv")
	if err := os.MkdirAll(bvDir, 0755); err != nil {
		t.Fatal(err)
	}

	configContent := `
density_warning_pct: 75
blocked_increase_threshold: 10
`
	if err := os.WriteFile(filepath.Join(bvDir, "drift.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DensityWarningPct != 75 {
		t.Errorf("expected density_warning_pct=75, got %f", config.DensityWarningPct)
	}
	if config.BlockedIncreaseThreshold != 10 {
		t.Errorf("expected blocked_increase_threshold=10, got %d", config.BlockedIncreaseThreshold)
	}
}

func TestConfigLoadInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	bvDir := filepath.Join(tmpDir, ".bv")
	if err := os.MkdirAll(bvDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Invalid config: negative density warning
	configContent := `density_warning_pct: -50`
	if err := os.WriteFile(filepath.Join(bvDir, "drift.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

// TestConfigLoadInvalidYAML tests loading a file with invalid YAML syntax
func TestConfigLoadInvalidYAML(t *testing.T) {
	t.Log("Testing LoadConfig with invalid YAML syntax")

	tmpDir := t.TempDir()
	bvDir := filepath.Join(tmpDir, ".bv")
	if err := os.MkdirAll(bvDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write invalid YAML (bad indentation/syntax)
	invalidYAML := `density_warning_pct: 50
  bad_indentation: true
    this_is_invalid`
	configPath := filepath.Join(bvDir, "drift.yaml")
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created invalid YAML file at: %s", configPath)

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
		if !strings.Contains(err.Error(), "parsing") {
			t.Errorf("error should mention parsing, got: %v", err)
		}
	}
}

// TestConfigLoadPermissionError tests LoadConfig with an unreadable file
func TestConfigLoadPermissionError(t *testing.T) {
	// Skip on systems where we can't reliably test permissions
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	t.Log("Testing LoadConfig with permission denied")

	tmpDir := t.TempDir()
	bvDir := filepath.Join(tmpDir, ".bv")
	if err := os.MkdirAll(bvDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create file with no read permissions
	configPath := filepath.Join(bvDir, "drift.yaml")
	if err := os.WriteFile(configPath, []byte("density_warning_pct: 50"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created unreadable file at: %s", configPath)

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error for unreadable file, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
		if !strings.Contains(err.Error(), "reading") {
			t.Errorf("error should mention reading, got: %v", err)
		}
	}

	// Cleanup: restore permissions so temp dir can be removed
	os.Chmod(configPath, 0644)
}

// TestConfigSaveInvalidConfig tests SaveConfig with a config that fails validation
func TestConfigSaveInvalidConfig(t *testing.T) {
	t.Log("Testing SaveConfig with invalid config")

	tmpDir := t.TempDir()

	// Create invalid config
	invalidConfig := &Config{
		DensityWarningPct: -100, // Invalid: negative
	}

	err := SaveConfig(tmpDir, invalidConfig)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
		if !strings.Contains(err.Error(), "invalid") {
			t.Errorf("error should mention invalid, got: %v", err)
		}
	}

	// Verify no file was created
	configPath := filepath.Join(tmpDir, ".bv", "drift.yaml")
	if _, err := os.Stat(configPath); err == nil {
		t.Error("config file should not have been created for invalid config")
	}
}

// TestConfigSaveMkdirError tests SaveConfig when .bv cannot be created (file exists)
func TestConfigSaveMkdirError(t *testing.T) {
	t.Log("Testing SaveConfig when .bv is a file instead of directory")

	tmpDir := t.TempDir()
	bvPath := filepath.Join(tmpDir, ".bv")

	// Create a FILE named .bv where a directory is expected
	if err := os.WriteFile(bvPath, []byte("blocking file"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created blocking file at: %s", bvPath)

	config := DefaultConfig()
	err := SaveConfig(tmpDir, config)
	if err == nil {
		t.Error("expected error when .bv is a file, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
		if !strings.Contains(err.Error(), "creating config directory") {
			t.Errorf("error should mention creating config directory, got: %v", err)
		}
	}
}

// TestConfigSavePermissionError tests SaveConfig when directory is not writable
func TestConfigSavePermissionError(t *testing.T) {
	// Skip on systems where we can't reliably test permissions
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	t.Log("Testing SaveConfig with permission denied")

	tmpDir := t.TempDir()
	bvDir := filepath.Join(tmpDir, ".bv")
	if err := os.MkdirAll(bvDir, 0555); err != nil { // Read-only directory
		t.Fatal(err)
	}
	t.Logf("Created read-only directory at: %s", bvDir)

	config := DefaultConfig()
	err := SaveConfig(tmpDir, config)
	if err == nil {
		t.Error("expected error for read-only directory, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
	}

	// Cleanup: restore permissions
	os.Chmod(bvDir, 0755)
}

func TestConfigSave(t *testing.T) {
	tmpDir := t.TempDir()

	config := &Config{
		DensityWarningPct:        80,
		BlockedIncreaseThreshold: 3,
	}

	if err := SaveConfig(tmpDir, config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmpDir, ".bv", "drift.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file should exist: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{"valid default", DefaultConfig(), false},
		{"negative density warning", &Config{DensityWarningPct: -10}, true},
		{"info > warning", &Config{DensityWarningPct: 10, DensityInfoPct: 20}, true},
		{"negative blocked", &Config{DensityWarningPct: 50, BlockedIncreaseThreshold: -1}, true},
		{"negative node growth", &Config{DensityWarningPct: 50, NodeGrowthInfoPct: -5}, true},
		{"negative edge growth", &Config{DensityWarningPct: 50, EdgeGrowthInfoPct: -5}, true},
		{"actionable decrease > 100", &Config{DensityWarningPct: 50, ActionableDecreaseWarningPct: 150}, true},
		{"negative actionable increase", &Config{DensityWarningPct: 50, ActionableIncreaseInfoPct: -10}, true},
		{"negative pagerank change", &Config{DensityWarningPct: 50, PageRankChangeWarningPct: -20}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCycleKey(t *testing.T) {
	// Same cycle represented identically should match
	key1 := cycleKey([]string{"A", "B", "C", "A"})
	key2 := cycleKey([]string{"A", "B", "C", "A"})

	if key1 != key2 {
		t.Errorf("identical cycles should match: %s vs %s", key1, key2)
	}

	// Different cycle should not match
	key3 := cycleKey([]string{"X", "Y", "Z", "X"})
	if key1 == key3 {
		t.Error("different cycles should have different keys")
	}

	// Empty cycle
	key4 := cycleKey([]string{})
	if key4 != "" {
		t.Errorf("empty cycle should have empty key, got %s", key4)
	}
}

// =============================================================================
// Calculator Method Branch Coverage Tests (bv-cam.3)
// =============================================================================

// TestCheckActionable_BaselineZero tests that actionable=0 baseline skips calculation
func TestCheckActionable_BaselineZero(t *testing.T) {
	t.Log("Testing checkActionable when baseline actionable=0 (skip case)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 0, // Zero baseline should skip calculation
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 50, // Huge increase but should be ignored
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	// Should not have actionable alert when baseline is 0
	for _, alert := range result.Alerts {
		if alert.Type == AlertActionableChange {
			t.Errorf("should not generate actionable alert when baseline is 0, got: %s", alert.Message)
		}
	}
	t.Log("Correctly skipped actionable check with zero baseline")
}

// TestCheckActionable_InfoIncrease tests actionable increase triggers info alert
func TestCheckActionable_InfoIncrease(t *testing.T) {
	t.Log("Testing checkActionable with 25% increase (info alert)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 100,
		},
	}

	// 25% increase should trigger info (default threshold is 20%)
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 125,
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertActionableChange {
			found = true
			if alert.Severity != SeverityInfo {
				t.Errorf("25%% increase should be info, got %s", alert.Severity)
			}
			t.Logf("Got expected alert: %s", alert.Message)
		}
	}
	if !found {
		t.Error("expected actionable_change info alert for 25% increase")
	}
}

// TestCheckActionable_DecreaseWarning tests large decrease triggers warning
func TestCheckActionable_DecreaseWarning(t *testing.T) {
	t.Log("Testing checkActionable with 35% decrease (warning alert)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 100,
		},
	}

	// 35% decrease should trigger warning (default threshold is 30%)
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 65,
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertActionableChange {
			found = true
			if alert.Severity != SeverityWarning {
				t.Errorf("35%% decrease should be warning, got %s", alert.Severity)
			}
			t.Logf("Got expected alert: %s", alert.Message)
		}
	}
	if !found {
		t.Error("expected actionable_change warning alert for 35% decrease")
	}
}

// TestCheckActionable_InfoDecrease tests moderate decrease yields info alert (not warning)
func TestCheckActionable_InfoDecrease(t *testing.T) {
	t.Log("Testing checkActionable with 25% decrease (info alert)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 80,
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 60, // 25% decrease (warning threshold is 30%)
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertActionableChange {
			found = true
			if alert.Severity != SeverityInfo {
				t.Errorf("25%% decrease should be info, got %s", alert.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected actionable_change info alert for 25% decrease")
	}
}

// TestCheckCycles_BaselineHasCycles tests that removing cycles doesn't alert
func TestCheckCycles_BaselineHasCycles(t *testing.T) {
	t.Log("Testing checkCycles when baseline has cycles but current removes them")

	bl := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10},
		Cycles: [][]string{{"A", "B", "C", "A"}, {"X", "Y", "X"}},
	}

	// Current has fewer cycles - should NOT alert (only new cycles alert)
	current := &baseline.Baseline{
		Stats:  bl.Stats,
		Cycles: [][]string{{"A", "B", "C", "A"}},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	for _, alert := range result.Alerts {
		if alert.Type == AlertNewCycle {
			t.Errorf("should not alert when cycles are removed, got: %s", alert.Message)
		}
	}
	t.Log("Correctly did not alert when cycles were removed")
}

// TestCheckCycles_NewCycles tests that new cycles trigger critical alerts
func TestCheckCycles_NewCycles(t *testing.T) {
	t.Log("Testing checkCycles when current snapshot adds new cycles")

	bl := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10},
		Cycles: [][]string{}, // No cycles in baseline
	}

	current := &baseline.Baseline{
		Stats:  bl.Stats,
		Cycles: [][]string{{"A", "B", "A"}},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertNewCycle {
			found = true
			if alert.Severity != SeverityCritical {
				t.Errorf("new cycles should be critical, got %s", alert.Severity)
			}
			if alert.Delta != 1 {
				t.Errorf("expected delta 1 new cycle, got %.0f", alert.Delta)
			}
		}
	}
	if !found {
		t.Fatal("expected critical new_cycle alert when cycles are added")
	}
}

// TestCheckCycles_SameCycles tests that identical cycles don't alert
func TestCheckCycles_SameCycles(t *testing.T) {
	t.Log("Testing checkCycles when baseline and current have same cycles")

	cycles := [][]string{{"A", "B", "C", "A"}, {"X", "Y", "X"}}

	bl := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10},
		Cycles: cycles,
	}

	current := &baseline.Baseline{
		Stats:  bl.Stats,
		Cycles: cycles,
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	for _, alert := range result.Alerts {
		if alert.Type == AlertNewCycle {
			t.Errorf("should not alert when cycles are identical, got: %s", alert.Message)
		}
	}
	t.Log("Correctly did not alert when cycles are identical")
}

// TestCheckCycles_BothEmpty tests that empty cycles in both don't alert
func TestCheckCycles_BothEmpty(t *testing.T) {
	t.Log("Testing checkCycles when both baseline and current have no cycles")

	bl := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10},
		Cycles: [][]string{},
	}

	current := &baseline.Baseline{
		Stats:  bl.Stats,
		Cycles: [][]string{},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	for _, alert := range result.Alerts {
		if alert.Type == AlertNewCycle {
			t.Errorf("should not alert when both have empty cycles, got: %s", alert.Message)
		}
	}
	t.Log("Correctly did not alert when both have empty cycles")
}

// TestCheckDensity_InfoLevel tests density increase at info level (not warning)
func TestCheckDensity_InfoLevel(t *testing.T) {
	t.Log("Testing checkDensity with 30% increase (info level, not warning)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.02,
		},
	}

	// 30% increase: above 20% info threshold, below 50% warning threshold
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.026, // 30% increase
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertDensityGrowth {
			found = true
			if alert.Severity != SeverityInfo {
				t.Errorf("30%% density increase should be info, got %s", alert.Severity)
			}
			t.Logf("Got expected alert: %s", alert.Message)
		}
	}
	if !found {
		t.Error("expected density_growth info alert for 30% increase")
	}
}

// TestCheckDensity_Decrease tests that density decrease doesn't alert
func TestCheckDensity_Decrease(t *testing.T) {
	t.Log("Testing checkDensity when density decreases (no alert)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.05,
		},
	}

	// Density decreased - should NOT alert
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.02, // 60% decrease
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	for _, alert := range result.Alerts {
		if alert.Type == AlertDensityGrowth {
			t.Errorf("should not alert when density decreases, got: %s", alert.Message)
		}
	}
	t.Log("Correctly did not alert when density decreased")
}

// TestCheckDensity_WarningLevel tests density increase crossing warning threshold
func TestCheckDensity_WarningLevel(t *testing.T) {
	t.Log("Testing checkDensity with 75% increase (warning level)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.02,
		},
	}

	// 75% increase: above 50% warning threshold
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.035, // (0.035-0.02)/0.02 = 75%
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	found := false
	for _, alert := range result.Alerts {
		if alert.Type == AlertDensityGrowth {
			found = true
			if alert.Severity != SeverityWarning {
				t.Errorf("75%% density increase should be warning, got %s", alert.Severity)
			}
			t.Logf("Got expected warning: %s", alert.Message)
		}
	}
	if !found {
		t.Fatal("expected density_growth warning alert for 75% increase")
	}
}

// TestCheckDensity_BaselineZero tests that zero baseline density skips check
func TestCheckDensity_BaselineZero(t *testing.T) {
	t.Log("Testing checkDensity when baseline density=0 (skip case)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0, // Zero baseline should skip
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.05, // Would be infinite increase but should be skipped
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	for _, alert := range result.Alerts {
		if alert.Type == AlertDensityGrowth {
			t.Errorf("should not alert when baseline density is 0, got: %s", alert.Message)
		}
	}
	t.Log("Correctly skipped density check with zero baseline")
}

// TestCheckActionable_SmallChanges tests that small changes don't trigger alerts
func TestCheckActionable_SmallChanges(t *testing.T) {
	t.Log("Testing checkActionable with small changes (no alert)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 100,
		},
	}

	// 10% increase (threshold 20%) -> No Alert
	currentInc := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 110,
		},
	}

	calcInc := NewCalculator(bl, currentInc, nil)
	resultInc := calcInc.Calculate()
	for _, alert := range resultInc.Alerts {
		if alert.Type == AlertActionableChange {
			t.Errorf("10%% increase should not alert, got: %s", alert.Message)
		}
	}

	// 10% decrease (threshold 20% for info) -> No Alert
	currentDec := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       100,
			ActionableCount: 90,
		},
	}

	calcDec := NewCalculator(bl, currentDec, nil)
	resultDec := calcDec.Calculate()
	for _, alert := range resultDec.Alerts {
		if alert.Type == AlertActionableChange {
			t.Errorf("10%% decrease should not alert, got: %s", alert.Message)
		}
	}
}

// TestCheckDensity_SmallIncrease tests that small density increase doesn't alert
func TestCheckDensity_SmallIncrease(t *testing.T) {
	t.Log("Testing checkDensity with 10% increase (no alert)")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.02,
		},
	}

	// 10% increase (threshold 20%)
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.022,
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	for _, alert := range result.Alerts {
		if alert.Type == AlertDensityGrowth {
			t.Errorf("10%% density increase should not alert, got: %s", alert.Message)
		}
	}
}

func TestCalculatorZeroBaseline(t *testing.T) {
	t.Log("Testing calculator with zero-value baseline")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{}, // All zero
	}

	// Current has some values
	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount:       10,
			EdgeCount:       20,
			Density:         0.1,
			BlockedCount:    2,
			ActionableCount: 8,
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	// Should not crash.
	// With zero baseline, most percentage calc checks should be skipped or handle div-by-zero safely.
	// Node/Edge growth checks check "if blNodes > 0".
	// Density check checks "if blDensity == 0".
	// Actionable check checks "if blAction > 0".
	// Blocked check is absolute difference, so 2 - 0 = 2. Threshold is 5. So no alert.

	if result.HasDrift {
		t.Errorf("expected no drift with zero baseline, got %d alerts", len(result.Alerts))
		for _, a := range result.Alerts {
			t.Logf("Alert: %s", a.Message)
		}
	}
}

func TestCalculatorBoundaryThresholds(t *testing.T) {
	t.Log("Testing exact boundary conditions")

	cfg := &Config{
		DensityWarningPct:        50.0,
		DensityInfoPct:           0.0,  // keep default info enabled for below test
		NodeGrowthInfoPct:        1000, // silence unrelated alerts
		EdgeGrowthInfoPct:        1000, // silence unrelated alerts
		BlockedIncreaseThreshold: 999,  // silence blocked delta alerts
	}

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.50,
		},
	}

	// Case 1: Slightly above 50% increase (~50.2%) to avoid float rounding ambiguity
	currentExact := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.751,
		},
	}

	calcExact := NewCalculator(bl, currentExact, cfg)
	resExact := calcExact.Calculate()

	found := false
	for _, a := range resExact.Alerts {
		if a.Type == AlertDensityGrowth && a.Severity == SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("Exact 50% density increase should trigger warning")
		for _, a := range resExact.Alerts {
			t.Logf("Found alert: [%s] %s (Delta: %f)", a.Severity, a.Message, a.Delta)
		}
	}

	// Case 2: Just below 50% increase (0.10 -> 0.1499)
	// Should NOT trigger warning (assuming no info threshold or low info threshold)
	// Default info is 20%, so it might trigger Info. Let's set Info to 49.9 to be safe or ignore info alerts.
	// Let's explicitly check it does NOT trigger Warning.
	currentBelow := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 100,
			Density:   0.1499,
		},
	}

	calcBelow := NewCalculator(bl, currentBelow, cfg)
	resBelow := calcBelow.Calculate()

	for _, a := range resBelow.Alerts {
		if a.Type == AlertDensityGrowth && a.Severity == SeverityWarning {
			t.Errorf("49.9%% density increase should NOT trigger warning, got: %s", a.Message)
		}
	}
}

func TestCalculatorEmptyMetrics(t *testing.T) {
	t.Log("Testing with empty metric slices")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{NodeCount: 10},
		TopMetrics: baseline.TopMetrics{
			PageRank: []baseline.MetricItem{}, // Empty
		},
		Cycles: [][]string{},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{NodeCount: 10},
		TopMetrics: baseline.TopMetrics{
			PageRank: []baseline.MetricItem{}, // Empty
		},
		Cycles: [][]string{},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	if result.HasDrift {
		t.Error("Empty metrics should not trigger drift")
	}
}

func TestCalculatorLargeValues(t *testing.T) {
	t.Log("Testing with large values to ensure no overflow/panic")

	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 1000000,
			EdgeCount: 5000000,
			Density:   0.5,
		},
	}

	current := &baseline.Baseline{
		Stats: baseline.GraphStats{
			NodeCount: 1000000, // No change
			EdgeCount: 5000000,
			Density:   0.5,
		},
	}

	calc := NewCalculator(bl, current, nil)
	result := calc.Calculate()

	if result.HasDrift {
		t.Error("Large stable values should not trigger drift")
	}
}

func TestCheckBlocked_StrictThresholds(t *testing.T) {
	bl := &baseline.Baseline{Stats: baseline.GraphStats{BlockedCount: 5}}

	tests := []struct {
		name      string
		threshold int
		curCount  int
		wantAlert bool
	}{
		{"threshold 0, delta 0", 0, 5, false}, // Should NOT alert (fixed bug)
		{"threshold 0, delta 1", 0, 6, true},  // Should alert (any increase)
		{"threshold 5, delta 4", 5, 9, false}, // Should not alert (below threshold)
		{"threshold 5, delta 5", 5, 10, true}, // Should alert (at threshold)
		{"threshold 5, delta 0", 5, 5, false}, // Should not alert
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cur := &baseline.Baseline{Stats: baseline.GraphStats{BlockedCount: tt.curCount}}
			cfg := &Config{BlockedIncreaseThreshold: tt.threshold}
			calc := NewCalculator(bl, cur, cfg)
			res := calc.Calculate()

			gotAlert := false
			for _, alert := range res.Alerts {
				if alert.Type == AlertBlockedIncrease {
					gotAlert = true
					break
				}
			}

			if gotAlert != tt.wantAlert {
				t.Errorf("wantAlert %v, got %v", tt.wantAlert, gotAlert)
			}
		})
	}
}

// TestDisabledAlerts verifies that disabled alert types are not emitted (bv-167)
func TestDisabledAlerts(t *testing.T) {
	bl := &baseline.Baseline{
		Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 15},
	}
	current := &baseline.Baseline{
		Stats:  baseline.GraphStats{NodeCount: 10, EdgeCount: 15},
		Cycles: [][]string{{"A", "B", "C", "A"}},
	}

	// Without disabling, we should get a cycle alert
	cfg := DefaultConfig()
	calc := NewCalculator(bl, current, cfg)
	result := calc.Calculate()

	hasCycleAlert := false
	for _, a := range result.Alerts {
		if a.Type == AlertNewCycle {
			hasCycleAlert = true
			break
		}
	}
	if !hasCycleAlert {
		t.Fatal("expected cycle alert without disabling")
	}

	// With disabled_alerts containing new_cycle, no cycle alert
	cfg.DisabledAlerts = []string{"new_cycle"}
	calc = NewCalculator(bl, current, cfg)
	result = calc.Calculate()

	for _, a := range result.Alerts {
		if a.Type == AlertNewCycle {
			t.Error("cycle alert should be disabled")
		}
	}
}

// TestIsAlertDisabled verifies the IsAlertDisabled helper (bv-167)
func TestIsAlertDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.IsAlertDisabled("stale_issue") {
		t.Error("stale_issue should not be disabled by default")
	}

	cfg.DisabledAlerts = []string{"stale_issue", "new_cycle"}
	if !cfg.IsAlertDisabled("stale_issue") {
		t.Error("stale_issue should be disabled")
	}
	if !cfg.IsAlertDisabled("new_cycle") {
		t.Error("new_cycle should be disabled")
	}
	if cfg.IsAlertDisabled("density_growth") {
		t.Error("density_growth should not be disabled")
	}
}

// TestLabelOverrides verifies per-label threshold customization (bv-167)
func TestLabelOverrides(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StaleWarningDays = 14
	cfg.StaleCriticalDays = 30
	cfg.InProgressStaleMultiplier = 0.5

	// Without overrides, defaults are returned
	warn, crit, mult := cfg.GetStalenessThresholds([]string{"some-label"})
	if warn != 14 || crit != 30 || mult != 0.5 {
		t.Errorf("expected defaults without overrides, got warn=%d crit=%d mult=%f", warn, crit, mult)
	}

	// Add label overrides
	cfg.LabelOverrides = map[string]*LabelConfig{
		"urgent": {
			StaleWarningDays:  3,
			StaleCriticalDays: 7,
		},
		"low-priority": {
			StaleWarningDays:  30,
			StaleCriticalDays: 60,
		},
	}

	// Issue with "urgent" label should get tighter thresholds
	warn, crit, _ = cfg.GetStalenessThresholds([]string{"urgent"})
	if warn != 3 || crit != 7 {
		t.Errorf("urgent label should have tighter thresholds, got warn=%d crit=%d", warn, crit)
	}

	// Issue with "low-priority" label gets looser thresholds if explicitly configured
	// We want to allow relaxing thresholds for specific labels (e.g. icebox, backlog)
	warn, crit, _ = cfg.GetStalenessThresholds([]string{"low-priority"})
	if warn != 30 || crit != 60 {
		t.Errorf("low-priority should override with looser thresholds, got warn=%d crit=%d", warn, crit)
	}

	// Issue with multiple labels uses tightest (smallest) values
	warn, crit, _ = cfg.GetStalenessThresholds([]string{"low-priority", "urgent"})
	if warn != 3 || crit != 7 {
		t.Errorf("multiple labels should use tightest, got warn=%d crit=%d", warn, crit)
	}
}

// TestLabelOverridesValidation verifies validation of label overrides (bv-167)
func TestLabelOverridesValidation(t *testing.T) {
	cfg := DefaultConfig()

	// Valid config
	cfg.LabelOverrides = map[string]*LabelConfig{
		"urgent": {StaleWarningDays: 3, StaleCriticalDays: 7},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config should pass validation: %v", err)
	}

	// Invalid: critical < warning
	cfg.LabelOverrides = map[string]*LabelConfig{
		"broken": {StaleWarningDays: 10, StaleCriticalDays: 5},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("critical < warning should fail validation")
	}

	// Invalid: negative days
	cfg.LabelOverrides = map[string]*LabelConfig{
		"broken": {StaleWarningDays: -1},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("negative days should fail validation")
	}
}

// alertFixture builds a calculator whose Calculate output contains at least
// one alert of the named type. Each fixture is minimal and isolated so a
// failing type points at exactly one emitter.
type alertFixture struct {
	baseline *baseline.Baseline
	current  *baseline.Baseline
	issues   []model.Issue
	config   *Config
}

func alertFixtures(now time.Time) map[AlertType]alertFixture {
	quietStats := baseline.GraphStats{NodeCount: 10, EdgeCount: 10, OpenCount: 10, ActionableCount: 5}
	quiet := func() (*baseline.Baseline, *baseline.Baseline) {
		return &baseline.Baseline{Stats: quietStats}, &baseline.Baseline{Stats: quietStats}
	}
	fresh := now.Add(-time.Hour)
	blocksOn := func(id, blocker string) *model.Dependency {
		return &model.Dependency{IssueID: id, DependsOnID: blocker, Type: model.DepBlocks}
	}
	closedAt := func(t time.Time) *time.Time { return &t }

	fixtures := map[AlertType]alertFixture{}

	bl, cur := quiet()
	fixtures[AlertStaleIssue] = alertFixture{bl, cur, []model.Issue{
		{ID: "STALE", Status: model.StatusOpen, Labels: []string{"backend"}, UpdatedAt: now.AddDate(0, 0, -20)},
	}, nil}

	bl, cur = quiet()
	fixtures[AlertBlockingCascade] = alertFixture{bl, cur, []model.Issue{
		{ID: "ROOT", Status: model.StatusOpen, Priority: 2, UpdatedAt: fresh},
		{ID: "D1", Status: model.StatusOpen, Priority: 3, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("D1", "ROOT")}},
		{ID: "D2", Status: model.StatusOpen, Priority: 3, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("D2", "ROOT")}},
		{ID: "D3", Status: model.StatusOpen, Priority: 3, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("D3", "ROOT")}},
	}, nil}

	bl, cur = quiet()
	fixtures[AlertHighImpactUnblock] = alertFixture{bl, cur, []model.Issue{
		{ID: "ROOT", Status: model.StatusOpen, Priority: 2, UpdatedAt: fresh},
		{ID: "P0A", Status: model.StatusOpen, Priority: 0, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("P0A", "ROOT")}},
		{ID: "P1B", Status: model.StatusOpen, Priority: 1, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("P1B", "ROOT")}},
		{ID: "P3C", Status: model.StatusOpen, Priority: 3, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("P3C", "ROOT")}},
	}, nil}

	bl, cur = quiet()
	fixtures[AlertAbandonedClaim] = alertFixture{bl, cur, []model.Issue{
		{ID: "CLAIMED", Status: model.StatusInProgress, Assignee: "agent-7", UpdatedAt: now.AddDate(0, 0, -20)},
	}, nil}

	bl, cur = quiet()
	fixtures[AlertPotentialDuplicate] = alertFixture{bl, cur, []model.Issue{
		{ID: "DUP-1", Title: "Fix login timeout on slow networks", Status: model.StatusOpen, UpdatedAt: fresh},
		{ID: "DUP-2", Title: "Fix login timeout on slow networks", Status: model.StatusOpen, UpdatedAt: fresh},
	}, nil}

	// A P4 hub that everything depends on: the graph says it deserves a
	// far higher priority than it carries.
	bl, cur = quiet()
	mismatch := []model.Issue{{ID: "HUB", Status: model.StatusOpen, Priority: 4, UpdatedAt: fresh}}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("LEAF-%d", i)
		mismatch = append(mismatch, model.Issue{ID: id, Status: model.StatusOpen, Priority: 0, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn(id, "HUB")}})
	}
	fixtures[AlertPriorityMismatch] = alertFixture{bl, cur, mismatch, nil}

	bl, cur = quiet()
	velocity := []model.Issue{}
	for i := 0; i < 6; i++ {
		velocity = append(velocity, model.Issue{ID: fmt.Sprintf("OLD-%d", i), Status: model.StatusClosed, UpdatedAt: fresh, ClosedAt: closedAt(now.AddDate(0, 0, -10))})
	}
	velocity = append(velocity, model.Issue{ID: "NEW-0", Status: model.StatusClosed, UpdatedAt: fresh, ClosedAt: closedAt(now.AddDate(0, 0, -2))})
	fixtures[AlertVelocityDrop] = alertFixture{bl, cur, velocity, nil}

	fixtures[AlertNewCycle] = alertFixture{
		&baseline.Baseline{Stats: quietStats},
		&baseline.Baseline{Stats: quietStats, Cycles: [][]string{{"A", "B", "A"}}},
		nil, nil,
	}
	fixtures[AlertDensityGrowth] = alertFixture{
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, Density: 0.10}},
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 16, Density: 0.16}},
		nil, nil,
	}
	fixtures[AlertNodeCountChange] = alertFixture{
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10}},
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 14, EdgeCount: 10}},
		nil, nil,
	}
	fixtures[AlertEdgeCountChange] = alertFixture{
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10}},
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 14}},
		nil, nil,
	}
	fixtures[AlertScopeCreep] = alertFixture{
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, OpenCount: 10}},
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, OpenCount: 13}},
		nil, nil,
	}
	fixtures[AlertBlockedIncrease] = alertFixture{
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, BlockedCount: 1}},
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, BlockedCount: 7}},
		nil, nil,
	}
	fixtures[AlertActionableChange] = alertFixture{
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, ActionableCount: 10}},
		&baseline.Baseline{Stats: baseline.GraphStats{NodeCount: 10, EdgeCount: 10, ActionableCount: 5}},
		nil, nil,
	}
	fixtures[AlertPageRankChange] = alertFixture{
		&baseline.Baseline{Stats: quietStats, TopMetrics: baseline.TopMetrics{PageRank: []baseline.MetricItem{{ID: "X", Value: 0.10}}}},
		&baseline.Baseline{Stats: quietStats, TopMetrics: baseline.TopMetrics{PageRank: []baseline.MetricItem{{ID: "X", Value: 0.20}}}},
		nil, nil,
	}
	return fixtures
}

func alertsOfType(result *Result, typ AlertType) []Alert {
	var out []Alert
	for _, a := range result.Alerts {
		if a.Type == typ {
			out = append(out, a)
		}
	}
	return out
}

// TestDrift_EveryAlertTypeHasEmitter is the D7 gate: every declared alert
// type fires on its fixture, is silenced by disabled_alerts, carries a
// suggested action, and a quiet fixture yields no alerts at all.
func TestDrift_EveryAlertTypeHasEmitter(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	fixtures := alertFixtures(now)

	declared := AllAlertTypes()
	if len(declared) != len(fixtures) {
		t.Fatalf("AllAlertTypes has %d entries but %d fixtures exist; add a fixture for every type", len(declared), len(fixtures))
	}
	for _, typ := range declared {
		fx, ok := fixtures[typ]
		if !ok {
			t.Fatalf("no fixture for alert type %q", typ)
		}
		t.Run(string(typ), func(t *testing.T) {
			calc := NewCalculator(fx.baseline, fx.current, fx.config)
			calc.SetNow(now)
			calc.SetIssues(fx.issues)
			result := calc.Calculate()
			got := alertsOfType(result, typ)
			t.Logf("%s: %d alert(s): %+v", typ, len(got), got)
			if len(got) == 0 {
				t.Fatalf("fixture for %s produced no %s alert; all alerts: %+v", typ, typ, result.Alerts)
			}
			for _, a := range got {
				if a.SuggestedAction == "" {
					t.Fatalf("%s alert has no suggested_action: %+v", typ, a)
				}
				if a.Message == "" || a.Severity == "" {
					t.Fatalf("%s alert missing message/severity: %+v", typ, a)
				}
				if !a.DetectedAt.Equal(now) {
					t.Fatalf("%s alert detected_at=%s; want the pinned instant %s", typ, a.DetectedAt, now)
				}
			}

			cfg := fx.config
			if cfg == nil {
				cfg = DefaultConfig()
			}
			disabled := *cfg
			disabled.DisabledAlerts = append(append([]string(nil), cfg.DisabledAlerts...), string(typ))
			calc = NewCalculator(fx.baseline, fx.current, &disabled)
			calc.SetNow(now)
			calc.SetIssues(fx.issues)
			if left := alertsOfType(calc.Calculate(), typ); len(left) != 0 {
				t.Fatalf("disabled_alerts=[%s] still produced %d alert(s): %+v", typ, len(left), left)
			}
		})
	}

	t.Run("quiet fixture", func(t *testing.T) {
		stats := baseline.GraphStats{NodeCount: 3, EdgeCount: 2, OpenCount: 3, ActionableCount: 2}
		fresh := now.Add(-time.Hour)
		issues := []model.Issue{
			{ID: "A", Title: "Write the parser", Status: model.StatusOpen, Priority: 2, UpdatedAt: fresh},
			{ID: "B", Title: "Ship the release notes", Status: model.StatusInProgress, Priority: 2, Assignee: "me", UpdatedAt: fresh},
			{ID: "C", Title: "Rotate the signing key", Status: model.StatusOpen, Priority: 2, UpdatedAt: fresh, Dependencies: []*model.Dependency{{IssueID: "C", DependsOnID: "A", Type: model.DepBlocks}}},
		}
		calc := NewCalculator(&baseline.Baseline{Stats: stats}, &baseline.Baseline{Stats: stats}, nil)
		calc.SetNow(now)
		calc.SetIssues(issues)
		result := calc.Calculate()
		if len(result.Alerts) != 0 || result.HasDrift {
			t.Fatalf("healthy project produced alerts: %+v", result.Alerts)
		}
		if result.ExitCode() != 0 {
			t.Fatalf("quiet exit code=%d; want 0", result.ExitCode())
		}
	})
}

func TestDrift_NewEmitterSemantics(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour)
	quiet := baseline.GraphStats{NodeCount: 10, EdgeCount: 10, OpenCount: 10, ActionableCount: 5}
	newCalc := func(cfg *Config, issues []model.Issue) *Result {
		calc := NewCalculator(&baseline.Baseline{Stats: quiet}, &baseline.Baseline{Stats: quiet}, cfg)
		calc.SetNow(now)
		calc.SetIssues(issues)
		return calc.Calculate()
	}
	blocksOn := func(id, blocker string) *model.Dependency {
		return &model.Dependency{IssueID: id, DependsOnID: blocker, Type: model.DepBlocks}
	}
	closedAt := func(t time.Time) *time.Time { return &t }

	t.Run("velocity_drop needs a real baseline window", func(t *testing.T) {
		var issues []model.Issue
		for i := 0; i < 3; i++ { // only 3 closes in the prior window: below VelocityMinBaseline (5)
			issues = append(issues, model.Issue{ID: fmt.Sprintf("OLD-%d", i), Status: model.StatusClosed, UpdatedAt: fresh, ClosedAt: closedAt(now.AddDate(0, 0, -10))})
		}
		if got := alertsOfType(newCalc(nil, issues), AlertVelocityDrop); len(got) != 0 {
			t.Fatalf("3 prior closes must not alarm: %+v", got)
		}
		for i := 3; i < 6; i++ {
			issues = append(issues, model.Issue{ID: fmt.Sprintf("OLD-%d", i), Status: model.StatusClosed, UpdatedAt: fresh, ClosedAt: closedAt(now.AddDate(0, 0, -9))})
		}
		for i := 0; i < 4; i++ { // 4 recent vs 6 prior = -33%, under the 50% default
			issues = append(issues, model.Issue{ID: fmt.Sprintf("NEW-%d", i), Status: model.StatusClosed, UpdatedAt: fresh, ClosedAt: closedAt(now.AddDate(0, 0, -1))})
		}
		if got := alertsOfType(newCalc(nil, issues), AlertVelocityDrop); len(got) != 0 {
			t.Fatalf("a 33%% dip must not alarm at the 50%% default: %+v", got)
		}
		cfg := DefaultConfig()
		cfg.VelocityDropPct = 25
		got := alertsOfType(newCalc(cfg, issues), AlertVelocityDrop)
		if len(got) != 1 || got[0].Severity != SeverityWarning || got[0].BaselineVal != 6 || got[0].CurrentVal != 4 {
			t.Fatalf("with velocity_drop_pct=25 want one warning 6→4, got %+v", got)
		}
	})

	t.Run("high_impact_unblock escalates on two urgent downstream items", func(t *testing.T) {
		issues := []model.Issue{
			{ID: "ROOT", Status: model.StatusOpen, Priority: 2, UpdatedAt: fresh},
			{ID: "P0A", Status: model.StatusOpen, Priority: 0, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("P0A", "ROOT")}},
			{ID: "P3B", Status: model.StatusOpen, Priority: 3, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("P3B", "ROOT")}},
			{ID: "P3C", Status: model.StatusOpen, Priority: 3, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("P3C", "ROOT")}},
		}
		got := alertsOfType(newCalc(nil, issues), AlertHighImpactUnblock)
		if len(got) != 1 || got[0].Severity != SeverityInfo || got[0].IssueID != "ROOT" || got[0].UnblocksCount != 3 {
			t.Fatalf("one P0 downstream: want info for ROOT unblocking 3, got %+v", got)
		}
		if len(got[0].Details) != 1 || got[0].Details[0] != "P0A" {
			t.Fatalf("details should list the urgent items only: %+v", got[0].Details)
		}
		issues[2].Priority = 1
		got = alertsOfType(newCalc(nil, issues), AlertHighImpactUnblock)
		if len(got) != 1 || got[0].Severity != SeverityWarning {
			t.Fatalf("two urgent downstream items: want warning, got %+v", got)
		}
		// Plenty of downstream work but none of it urgent: cascade fires, this does not.
		for i := range issues {
			issues[i].Priority = 3
		}
		result := newCalc(nil, issues)
		if got := alertsOfType(result, AlertHighImpactUnblock); len(got) != 0 {
			t.Fatalf("no urgent downstream: high_impact_unblock must stay silent, got %+v", got)
		}
		if got := alertsOfType(result, AlertBlockingCascade); len(got) != 1 {
			t.Fatalf("blocking_cascade should still fire on 3 downstream items, got %+v", got)
		}
	})

	t.Run("abandoned_claim requires an assignee and honours label overrides", func(t *testing.T) {
		idle := now.AddDate(0, 0, -20) // > 14d default (14 x 0.5 x 2)
		issues := []model.Issue{
			{ID: "NOBODY", Status: model.StatusInProgress, UpdatedAt: idle},
			{ID: "CLAIMED", Status: model.StatusInProgress, Assignee: "agent-7", UpdatedAt: idle},
			{ID: "RECENT", Status: model.StatusInProgress, Assignee: "agent-8", UpdatedAt: now.AddDate(0, 0, -10)},
			{ID: "URGENT", Status: model.StatusInProgress, Assignee: "agent-9", Labels: []string{"urgent"}, UpdatedAt: now.AddDate(0, 0, -5)},
		}
		cfg := DefaultConfig()
		cfg.LabelOverrides = map[string]*LabelConfig{"urgent": {StaleWarningDays: 2, StaleCriticalDays: 4}}
		got := alertsOfType(newCalc(cfg, issues), AlertAbandonedClaim)
		ids := map[string]Alert{}
		for _, a := range got {
			ids[a.IssueID] = a
		}
		if _, ok := ids["NOBODY"]; ok {
			t.Fatalf("in_progress without an assignee is stale, not an abandoned claim: %+v", got)
		}
		if _, ok := ids["RECENT"]; ok {
			t.Fatalf("10 idle days is under the 14-day default: %+v", got)
		}
		claimed, ok := ids["CLAIMED"]
		if !ok || claimed.Severity != SeverityWarning || !strings.Contains(claimed.Message, "agent-7") {
			t.Fatalf("want a warning naming agent-7 for CLAIMED, got %+v", got)
		}
		urgent, ok := ids["URGENT"]
		if !ok {
			t.Fatalf("label override (2d x 0.5 x 2 = 2d) should flag URGENT after 5 idle days: %+v", got)
		}
		if len(urgent.Labels) != 1 || urgent.Labels[0] != "urgent" {
			t.Fatalf("alert should carry the issue labels for --alert-label: %+v", urgent)
		}
	})

	t.Run("potential_duplicate is capped and skips dissimilar titles", func(t *testing.T) {
		var issues []model.Issue
		for i := 0; i < 6; i++ {
			issues = append(issues, model.Issue{ID: fmt.Sprintf("SAME-%d", i), Title: "Migrate billing exports to parquet format", Status: model.StatusOpen, UpdatedAt: fresh})
		}
		issues = append(issues, model.Issue{ID: "OTHER", Title: "Rename the tutorial header", Status: model.StatusOpen, UpdatedAt: fresh})
		// Closed twins are history, not duplicates to consolidate.
		issues = append(issues,
			model.Issue{ID: "DONE-1", Title: "Archive the quarterly invoice batch", Status: model.StatusClosed, UpdatedAt: fresh},
			model.Issue{ID: "DONE-2", Title: "Archive the quarterly invoice batch", Status: model.StatusClosed, UpdatedAt: fresh},
		)
		cfg := DefaultConfig()
		cfg.DuplicateMaxAlerts = 4
		got := alertsOfType(newCalc(cfg, issues), AlertPotentialDuplicate)
		if len(got) != 4 {
			t.Fatalf("duplicate_max_alerts=4 should cap the %d similar pairs to 4, got %d: %+v", 15, len(got), got)
		}
		for _, a := range got {
			if a.IssueID == "OTHER" || a.RelatedIssueID == "OTHER" || a.RelatedIssueID == "" {
				t.Fatalf("dissimilar issue paired, or pair missing related_issue_id: %+v", a)
			}
			if strings.HasPrefix(a.IssueID, "DONE") || strings.HasPrefix(a.RelatedIssueID, "DONE") {
				t.Fatalf("closed issues must not be paired as duplicates: %+v", a)
			}
		}
		cfg.DuplicateMaxAlerts = 100
		for _, a := range alertsOfType(newCalc(cfg, issues), AlertPotentialDuplicate) {
			if strings.HasPrefix(a.IssueID, "DONE") || strings.HasPrefix(a.RelatedIssueID, "DONE") {
				t.Fatalf("closed issues must not be paired as duplicates even uncapped: %+v", a)
			}
		}
	})

	t.Run("priority_mismatch respects the confidence floor", func(t *testing.T) {
		issues := []model.Issue{{ID: "HUB", Status: model.StatusOpen, Priority: 4, UpdatedAt: fresh}}
		for i := 0; i < 8; i++ {
			id := fmt.Sprintf("LEAF-%d", i)
			issues = append(issues, model.Issue{ID: id, Status: model.StatusOpen, Priority: 0, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn(id, "HUB")}})
		}
		got := alertsOfType(newCalc(nil, issues), AlertPriorityMismatch)
		var hub *Alert
		for i := range got {
			if got[i].IssueID == "HUB" {
				hub = &got[i]
			}
		}
		if hub == nil || hub.Severity != SeverityWarning || hub.BaselineVal != 4 || hub.CurrentVal >= 4 {
			t.Fatalf("want a warning that P4 HUB deserves a higher priority, got %+v", got)
		}
		// Find a milder hub whose "increase" recommendation lands between
		// the default floor and certainty, so raising the floor just above
		// it must silence it. Shapes are probed because confidence saturates
		// quickly on tiny graphs.
		var mild []model.Issue
		var conf float64
		var seen []string
	probe:
		for _, hubPriority := range []int{2, 3, 4} {
			for leaves := 1; leaves <= 6 && mild == nil; leaves++ {
				candidate := []model.Issue{{ID: "HUB", Status: model.StatusOpen, Priority: hubPriority, UpdatedAt: fresh}}
				for i := 0; i < leaves; i++ {
					id := fmt.Sprintf("LEAF-%d", i)
					candidate = append(candidate, model.Issue{ID: id, Status: model.StatusOpen, Priority: 1, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn(id, "HUB")}})
				}
				for _, rec := range analysis.NewAnalyzer(candidate).GenerateRecommendations() {
					if rec.IssueID != "HUB" || rec.Direction != "increase" {
						continue
					}
					seen = append(seen, fmt.Sprintf("P%d/%d leaves=%.2f", hubPriority, leaves, rec.Confidence))
					if rec.Confidence >= 0.6 && rec.Confidence < 1.0 {
						mild, conf = candidate, rec.Confidence
						break probe
					}
				}
			}
		}
		if mild == nil {
			t.Fatalf("no probed fixture produced an increase recommendation in [0.6,1.0): %v", seen)
		}
		got = alertsOfType(newCalc(nil, mild), AlertPriorityMismatch)
		if len(got) != 1 || got[0].IssueID != "HUB" {
			t.Fatalf("mild hub fixture should produce exactly one HUB alert at the default floor, got %+v", got)
		}
		cfg := DefaultConfig()
		cfg.PriorityMismatchMinConfidence = conf + 0.01
		if got := alertsOfType(newCalc(cfg, mild), AlertPriorityMismatch); len(got) != 0 {
			t.Fatalf("confidence floor %.2f should silence a %.2f recommendation, got %+v", cfg.PriorityMismatchMinConfidence, conf, got)
		}
		// Downgrade suggestions are not alerts: a leaf that the graph says
		// could be lower stays silent.
		leafOnly := []model.Issue{
			{ID: "A", Title: "one", Status: model.StatusOpen, Priority: 0, UpdatedAt: fresh},
			{ID: "B", Title: "two", Status: model.StatusOpen, Priority: 0, UpdatedAt: fresh, Dependencies: []*model.Dependency{blocksOn("B", "A")}},
		}
		for _, a := range alertsOfType(newCalc(nil, leafOnly), AlertPriorityMismatch) {
			if a.Delta > 0 {
				t.Fatalf("decrease recommendation surfaced as an alert: %+v", a)
			}
		}
	})

	t.Run("scope_creep needs a baseline open count", func(t *testing.T) {
		calc := NewCalculator(&baseline.Baseline{Stats: baseline.GraphStats{OpenCount: 0}}, &baseline.Baseline{Stats: baseline.GraphStats{OpenCount: 30}}, nil)
		calc.SetNow(now)
		if got := alertsOfType(calc.Calculate(), AlertScopeCreep); len(got) != 0 {
			t.Fatalf("no baseline open count: must stay silent, got %+v", got)
		}
		calc = NewCalculator(&baseline.Baseline{Stats: baseline.GraphStats{OpenCount: 10}}, &baseline.Baseline{Stats: baseline.GraphStats{OpenCount: 11}}, nil)
		calc.SetNow(now)
		if got := alertsOfType(calc.Calculate(), AlertScopeCreep); len(got) != 0 {
			t.Fatalf("10%% growth is under the 20%% default, got %+v", got)
		}
	})
}

func TestConfigValidate_ProactiveKeys(t *testing.T) {
	cfg := &Config{DensityWarningPct: 50}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("omitted proactive keys must backfill, got %v", err)
	}
	def := DefaultConfig()
	if cfg.VelocityDropPct != def.VelocityDropPct || cfg.HighImpactUnblockMin != def.HighImpactUnblockMin || cfg.DuplicateJaccardThreshold != def.DuplicateJaccardThreshold || cfg.PriorityMismatchMinConfidence != def.PriorityMismatchMinConfidence || cfg.AbandonedClaimMultiplier != def.AbandonedClaimMultiplier || cfg.ScopeCreepPct != def.ScopeCreepPct {
		t.Fatalf("backfilled config differs from defaults: %+v", cfg)
	}
	bad := map[string]*Config{
		"velocity pct > 100":      {DensityWarningPct: 50, VelocityDropPct: 150},
		"negative window":         {DensityWarningPct: 50, VelocityWindowDays: -1},
		"priority max > 4":        {DensityWarningPct: 50, HighImpactPriorityMax: 9},
		"jaccard > 1":             {DensityWarningPct: 50, DuplicateJaccardThreshold: 1.5},
		"confidence > 1":          {DensityWarningPct: 50, PriorityMismatchMinConfidence: 2},
		"negative duplicate cap":  {DensityWarningPct: 50, DuplicateMaxAlerts: -1},
		"abandoned multiplier 11": {DensityWarningPct: 50, AbandonedClaimMultiplier: 11},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: want validation error", name)
		}
	}
	// The example file must round-trip through the loader with every key present.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(dir), []byte(ExampleConfig()), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("example config failed to load: %v", err)
	}
	if loaded.VelocityWindowDays != 7 || loaded.DuplicateMaxAlerts != 10 || loaded.ScopeCreepPct != 20 || loaded.HighImpactPriorityMax != 1 {
		t.Fatalf("example config keys not honoured: %+v", loaded)
	}
	for _, key := range []string{"scope_creep_pct", "velocity_drop_pct", "velocity_window_days", "velocity_min_baseline", "high_impact_unblock_min", "high_impact_priority_max", "abandoned_claim_multiplier", "duplicate_jaccard_threshold", "duplicate_max_alerts", "priority_mismatch_min_confidence"} {
		if !strings.Contains(ExampleConfig(), key+":") {
			t.Errorf("ExampleConfig missing key %s", key)
		}
	}
}
