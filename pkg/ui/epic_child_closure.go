package ui

import (
	"context"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	tea "github.com/charmbracelet/bubbletea"
)

type epicChildClosureMsg struct {
	parentID string
	children []EpicChild
	err      error
}

type epicChildClosureState struct {
	parentID  string
	children  []EpicChild
	loaded    bool
	attempted bool
}

func (m *Model) displayedIssueID() string {
	if m.showAlertsPanel {
		return ""
	}
	if m.focused != focusList && m.focused != focusDetail {
		return ""
	}
	if m.insightsDetailID != "" {
		return m.insightsDetailID
	}
	if !m.isSplitView && !m.showDetails {
		return ""
	}
	item := m.list.SelectedItem()
	issue, ok := item.(IssueItem)
	if !ok {
		return ""
	}
	return issue.Issue.ID
}

// epicChildClosureCmd fetches only the direct children for the currently
// displayed epic. The result is kept only for that detail render.
func (m *Model) epicChildClosureCmd() tea.Cmd {
	parentID := m.displayedIssueID()
	provider := m.runtimeServices.EpicChildClosureProvider
	if provider == nil || !m.hubRepositoryPresentation() {
		return nil
	}
	issue := m.issueMap[parentID]
	if issue == nil || issue.IssueType != model.TypeEpic {
		m.epicChildren = epicChildClosureState{parentID: parentID}
		return nil
	}
	if m.epicChildren.parentID != parentID {
		m.epicChildren = epicChildClosureState{parentID: parentID}
	}
	if m.epicChildren.attempted {
		return nil
	}
	m.epicChildren.attempted = true
	return func() tea.Msg {
		children, err := provider(context.Background(), parentID)
		return epicChildClosureMsg{parentID: parentID, children: children, err: err}
	}
}

func (m *Model) handleEpicChildClosure(msg epicChildClosureMsg) {
	if msg.parentID != m.displayedIssueID() || msg.parentID != m.epicChildren.parentID {
		return
	}
	if msg.err != nil {
		return
	}
	m.epicChildren.children = msg.children
	m.epicChildren.loaded = true
	m.updateViewportContent()
}
