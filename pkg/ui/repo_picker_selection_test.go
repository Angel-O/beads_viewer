package ui

import (
	"slices"
	"testing"

	"github.com/charmbracelet/lipgloss"

	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

func TestRepoPickerSelectionRoundTripsNeutralVariants(t *testing.T) {
	type testCase struct {
		name            string
		selection       repositorypkg.Selection
		wantMode        repositorypkg.SelectionMode
		wantIDs         []string
		wantUnassigned  bool
		wantContextless bool
	}
	tests := []testCase{
		{name: "all", selection: repositorypkg.NewAllSelection(), wantMode: repositorypkg.SelectionAll, wantContextless: true},
		{name: "unassigned", selection: repositorypkg.NewUnassignedSelection(), wantMode: repositorypkg.SelectionUnassigned, wantContextless: true},
	}
	selected, err := repositorypkg.NewSelectedSelection([]string{"ctx:gamma-789", "ctx:alpha-123"})
	if err != nil {
		t.Fatal(err)
	}
	selectedAndUnassigned, err := repositorypkg.NewSelectedAndUnassignedSelection([]string{"ctx:gamma-789", "ctx:alpha-123"})
	if err != nil {
		t.Fatal(err)
	}
	tests = append(tests,
		testCase{name: "selected", selection: selected, wantMode: repositorypkg.SelectionSelected, wantIDs: []string{"ctx:alpha-123", "ctx:gamma-789"}},
		testCase{name: "selected and unassigned", selection: selectedAndUnassigned, wantMode: repositorypkg.SelectionSelected, wantIDs: []string{"ctx:alpha-123", "ctx:gamma-789"}, wantUnassigned: true, wantContextless: true},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			picker := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
			picker.SetRepositorySelection(tt.selection)
			got, err := picker.RepositorySelection()
			if err != nil {
				t.Fatal(err)
			}
			if got.Mode() != tt.wantMode || got.IncludesUnassigned() != tt.wantUnassigned || !slices.Equal(got.IDs(), tt.wantIDs) {
				t.Fatalf("selection = %#v, want mode=%q IDs=%v unassigned=%v", got, tt.wantMode, tt.wantIDs, tt.wantUnassigned)
			}
			if picker.ContextlessSelected() != tt.wantContextless {
				t.Fatalf("contextless selected = %v, want %v", picker.ContextlessSelected(), tt.wantContextless)
			}
		})
	}
}
