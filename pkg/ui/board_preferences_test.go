package ui

import (
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestApplyBoardHideEmptyColumnsPreference(t *testing.T) {
	m := &Model{board: NewBoardModel([]model.Issue{
		{ID: "open", Status: model.StatusOpen},
	}, DefaultTheme(nil))}

	if got := m.board.GetEmptyColumnVisibilityMode(); got != "Auto" {
		t.Fatalf("new board mode = %q, want Auto", got)
	}

	m.ApplyBoardHideEmptyColumnsPreference()
	if got := m.board.GetEmptyColumnVisibilityMode(); got != "Hide Empty" {
		t.Fatalf("configured board mode = %q, want Hide Empty", got)
	}
	if got := m.board.HiddenColumnCount(); got != 3 {
		t.Fatalf("configured Status board hid %d columns, want 3", got)
	}

	// The configured override is the same session-only state used by e: cycling
	// still reaches Auto and then Show All without persistence.
	m.board.ToggleEmptyColumns()
	if got := m.board.GetEmptyColumnVisibilityMode(); got != "Auto" {
		t.Fatalf("after e from configured mode = %q, want Auto", got)
	}
	m.board.ToggleEmptyColumns()
	if got := m.board.GetEmptyColumnVisibilityMode(); got != "Show All" {
		t.Fatalf("after e twice from configured mode = %q, want Show All", got)
	}
}
