package ui

import (
	"context"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/charmbracelet/bubbles/list"
)

func TestEpicChildClosureRequestsOnlyDisplayedEpics(t *testing.T) {
	issues := []model.Issue{
		{ID: "epic", IssueType: model.TypeEpic},
		{ID: "task", IssueType: model.TypeTask},
	}
	var requested []string
	m := NewModel(issues, nil, "", RuntimeServices{
		RepositoryPresentation: true,
		EpicChildClosureProvider: func(_ context.Context, parentID string) ([]EpicChild, error) {
			requested = append(requested, parentID)
			return []EpicChild{{ID: "child"}}, nil
		},
	})
	defer m.Stop()
	m.isSplitView = true
	m.list.SetItems([]list.Item{IssueItem{Issue: issues[0]}})

	cmd := m.epicChildClosureCmd()
	if cmd == nil {
		t.Fatal("displayed epic did not schedule a child-closure query")
	}
	if len(requested) != 0 {
		t.Fatal("child-closure provider ran before its command")
	}
	updated, _ := m.Update(cmd())
	m = updated.(*Model)
	if len(requested) != 1 || requested[0] != "epic" || !m.epicChildren.loaded {
		t.Fatalf("requested=%v state=%#v", requested, m.epicChildren)
	}

	for _, view := range []focus{focusBoard, focusGraph, focusInsights, focusTree, focusActionable} {
		m.epicChildren = epicChildClosureState{}
		m.focused = view
		if cmd := m.epicChildClosureCmd(); cmd != nil {
			t.Fatalf("hidden %v view scheduled a child-closure query", view)
		}
	}

	m.epicChildren = epicChildClosureState{}
	m.focused = focusDetail
	m.showAlertsPanel = true
	if cmd := m.epicChildClosureCmd(); cmd != nil {
		t.Fatal("Alerts panel scheduled a child-closure query over the hidden detail")
	}
	m.showAlertsPanel = false
	if cmd := m.epicChildClosureCmd(); cmd == nil {
		t.Fatal("visible detail stopped scheduling a child-closure query")
	}

	m.list.SetItems([]list.Item{IssueItem{Issue: issues[1]}})
	if cmd := m.epicChildClosureCmd(); cmd != nil {
		t.Fatal("non-epic detail scheduled a child-closure query")
	}
}
