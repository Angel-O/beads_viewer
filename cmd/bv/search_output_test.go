package main

import (
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/search"
)

// --search-preset used to be silently ignored unless --search-mode hybrid was
// also given: the flag "worked" while the ranking never changed. A preset now
// implies hybrid mode (text-only implies text), and an explicit text mode with
// a hybrid preset is rejected instead of ignored.
func TestApplySearchConfigOverrides_PresetImpliesHybrid(t *testing.T) {
	base := search.SearchConfig{Mode: search.SearchModeText, Preset: search.PresetDefault}

	cfg, err := applySearchConfigOverrides(base, "", "impact-first", "")
	if err != nil {
		t.Fatalf("preset alone: %v", err)
	}
	if cfg.Mode != search.SearchModeHybrid || cfg.Preset != search.PresetImpactFirst {
		t.Fatalf("preset alone: mode=%q preset=%q, want hybrid/impact-first", cfg.Mode, cfg.Preset)
	}

	cfg, err = applySearchConfigOverrides(base, "", "text-only", "")
	if err != nil {
		t.Fatalf("text-only: %v", err)
	}
	if cfg.Mode != search.SearchModeText {
		t.Fatalf("text-only preset: mode=%q, want text", cfg.Mode)
	}

	cfg, err = applySearchConfigOverrides(base, "hybrid", "bug-hunting", "")
	if err != nil || cfg.Mode != search.SearchModeHybrid || cfg.Preset != search.PresetBugHunting {
		t.Fatalf("explicit hybrid + preset: cfg=%+v err=%v", cfg, err)
	}

	if _, err := applySearchConfigOverrides(base, "text", "sprint-planning", ""); err == nil {
		t.Fatal("--search-mode text with a hybrid preset must be rejected")
	}

	// No preset: mode untouched.
	cfg, err = applySearchConfigOverrides(base, "", "", "")
	if err != nil || cfg.Mode != search.SearchModeText {
		t.Fatalf("no flags: cfg=%+v err=%v", cfg, err)
	}
}
