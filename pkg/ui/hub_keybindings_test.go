package ui

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestKeyDispatch_FlowMatrixExcludesContextLabels(t *testing.T) {
	issues := []model.Issue{
		{ID: "backend", Title: "Backend", Labels: []string{"ctx:project-one", "backend"}, Status: model.StatusOpen},
		{ID: "frontend", Title: "Frontend", Labels: []string{"ctx:project-two", "frontend"}, Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "backend", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.runtimeServices.LabelPredicate = func(label string) bool {
		switch label {
		case "ctx:project-one", "ctx:project-two":
			return false
		default:
			return true
		}
	}

	updated, _ := m.Update(keyMsg("f"))
	m = updated.(*Model)

	if got := m.flowMatrix.SelectedLabel(); got != "backend" {
		t.Fatalf("selected label = %q, want highest-ranked regular label backend", got)
	}
	view := m.flowMatrix.View()
	if !strings.Contains(view, "2 labels") {
		t.Fatalf("flow header should count only regular labels; view = %q", view)
	}
	if strings.Contains(view, "ctx:") {
		t.Fatalf("context label appeared in rendered flow list or detail pane; view = %q", view)
	}
	for _, label := range m.flowMatrix.flow.Labels {
		if strings.HasPrefix(label, "ctx:") {
			t.Fatalf("context label %q appeared in flow view model", label)
		}
	}

	updated, _ = m.Update(keyMsg("G"))
	m = updated.(*Model)
	if got := m.flowMatrix.SelectedLabel(); got != "frontend" {
		t.Fatalf("end selection = %q, want frontend", got)
	}
}

func TestKeyDispatch_FlowMatrixContextOnlySelectionSafety(t *testing.T) {
	issues := []model.Issue{
		{ID: "one", Title: "One", Labels: []string{"ctx:project-one"}, Status: model.StatusOpen},
		{ID: "two", Title: "Two", Labels: []string{"ctx:project-two"}, Status: model.StatusOpen, Dependencies: []*model.Dependency{{DependsOnID: "one", Type: model.DepBlocks}}},
	}
	m := NewModel(issues, nil, "")
	m.runtimeServices.CatalogPath = "hub.yaml"
	m.runtimeServices.LabelPredicate = func(label string) bool {
		switch label {
		case "ctx:project-one", "ctx:project-two":
			return false
		default:
			return true
		}
	}

	updated, _ := m.Update(keyMsg("f"))
	m = updated.(*Model)
	for _, key := range []string{"j", "k", "G", "g", "enter"} {
		updated, _ = m.Update(keyMsg(key))
		m = updated.(*Model)
	}

	if got := m.flowMatrix.SelectedLabel(); got != "" {
		t.Fatalf("selected label = %q, want empty for context-only data", got)
	}
	if m.flowMatrix.showDrilldown {
		t.Fatal("empty flow should not open a drilldown")
	}
	if got := m.flowMatrix.View(); !strings.Contains(got, "No open cross-label blocking dependencies") {
		t.Fatalf("context-only flow should render empty state; view = %q", got)
	}
}
