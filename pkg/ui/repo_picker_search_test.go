package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestRepoPickerSearchMatchesRepositoryMetadata(t *testing.T) {
	picker := NewRepoPickerModel(testRepositoryCatalog(), DefaultTheme(lipgloss.NewRenderer(nil)))
	picker.BeginSearch()
	picker.UpdateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("services/beta")})

	if picker.FilteredCount() != 1 || picker.currentRepositoryID() != "ctx:beta-456" {
		t.Fatalf("search result = count %d, ID %q", picker.FilteredCount(), picker.currentRepositoryID())
	}
	picker.ClearSearch()
	if picker.IsSearching() || picker.SearchValue() != "" || picker.FilteredCount() != len(testRepositoryCatalog()) {
		t.Fatalf("cleared search = searching=%v value=%q count=%d", picker.IsSearching(), picker.SearchValue(), picker.FilteredCount())
	}
}
