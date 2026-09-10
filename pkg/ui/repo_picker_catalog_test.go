package ui

import (
	"testing"

	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
	"github.com/charmbracelet/lipgloss"
)

func TestRepoPickerCatalogRefreshReconcilesDraftByID(t *testing.T) {
	picker := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	picker.SetActiveRepos(map[string]bool{"ctx:alpha-123": true, "ctx:gamma-789": true})
	picker.MoveDown()
	picker.MoveDown()

	picker.SetCatalog(repositorypkg.Catalog{
		{ID: "ctx:gamma-789", Name: "aardvark"},
		{ID: "ctx:alpha-123", Name: "alpha"},
		{ID: "ctx:new-000", Name: "new"},
	})

	if got := picker.currentRepositoryID(); got != "ctx:gamma-789" {
		t.Fatalf("cursor ID = %q, want ctx:gamma-789", got)
	}
	selected := picker.SelectedRepos()
	if len(selected) != 2 || !selected["ctx:alpha-123"] || !selected["ctx:gamma-789"] || selected["ctx:new-000"] {
		t.Fatalf("selected after refresh = %#v", selected)
	}
}
