package ui_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
	"github.com/charmbracelet/colorprofile"
)

func TestRenderSparkline(t *testing.T) {
	tests := []struct {
		name  string
		val   float64
		width int
	}{
		{"Zero", 0.0, 5},
		{"Full", 1.0, 5},
		{"Half", 0.5, 5},
		{"Small", 0.1, 5},
		{"AlmostFull", 0.99, 5},
		{"Overflow", 1.5, 5},
		{"Underflow", -0.5, 5},
		{"Width1", 0.5, 1},
		{"Width0", 0.5, 0}, // Edge case
		{"VerySmall", 0.01, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderSparkline panicked: %v", r)
				}
			}()
			got := ui.RenderSparkline(tt.val, tt.width)
			if len([]rune(got)) != tt.width {
				if tt.width > 0 { // Allow 0 length for 0 width
					t.Errorf("RenderSparkline length mismatch. Want %d, got %d ('%s')", tt.width, len([]rune(got)), got)
				}
			}
			if strings.Count(got, "\n") > 0 {
				t.Errorf("RenderSparkline contains newlines")
			}
			// Verify visibility for non-zero small values
			if tt.name == "VerySmall" && tt.width > 0 {
				if strings.TrimSpace(got) == "" {
					t.Errorf("RenderSparkline should show visible bar for small values, got empty/spaces: '%s'", got)
				}
			}
		})
	}
}

func TestHeatGradientHighEndContrastPreservesLowMidMappings(t *testing.T) {
	previousProfile := ui.TermProfile
	ui.TermProfile = colorprofile.TrueColor
	t.Cleanup(func() { ui.TermProfile = previousProfile })

	hotBg, hotFg := ui.GetHeatGradientColorBg(0.8)
	maxBg, maxFg := ui.GetHeatGradientColorBg(1.0)
	if reflect.DeepEqual(hotBg, maxBg) {
		t.Fatalf("hot and max backgrounds are identical: %v", hotBg)
	}
	if !reflect.DeepEqual(hotFg, ui.ThemeFg("#ffffff")) || !reflect.DeepEqual(maxFg, ui.ThemeFg("#ffffff")) {
		t.Fatalf("high-end foreground contrast changed: hot=%v max=%v", hotFg, maxFg)
	}

	for _, testCase := range []struct {
		intensity float64
		bg        string
	}{
		{0.1, "#16213e"},
		{0.2, "#3282b8"},
		{0.4, "#f7dc6f"},
		{0.6, "#e94560"},
		{0.8, "#ff2e63"},
	} {
		bg, _ := ui.GetHeatGradientColorBg(testCase.intensity)
		if !reflect.DeepEqual(bg, ui.ThemeBg(testCase.bg)) {
			t.Errorf("intensity %.1f background = %v, want %s", testCase.intensity, bg, testCase.bg)
		}
	}
}
