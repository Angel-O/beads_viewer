package ui

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHubRepositoryPickerShowsContextlessBeadCount(t *testing.T) {
	issues := []model.Issue{
		{ID: "contextless-1"},
		{ID: "contextless-2", Labels: []string{"backend"}},
		{ID: "alpha", Labels: []string{"ctx:alpha"}},
		{ID: "unknown", Labels: []string{"ctx:unknown"}},
	}
	m := NewModel(issues, nil, "")
	m.ready = true
	m.hubRepositoryMode = true
	m.runtimeServices.LabelPredicate = func(label string) bool {
		switch label {
		case "ctx:alpha", "ctx:unknown":
			return false
		default:
			return true
		}
	}
	m.repositoryCatalog = repositorypkg.Catalog{{ID: "ctx:alpha", Name: "alpha", BeadCount: 1}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(*Model)
	if !m.showRepoPicker {
		t.Fatal("repository picker did not open")
	}
	if picker := m.repoPicker.View(); !strings.Contains(picker, "no-context (2)") {
		t.Fatalf("contextless picker row missing bead count:\n%s", picker)
	}
}
