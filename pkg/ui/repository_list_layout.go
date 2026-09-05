package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

const repositoryListNameWidthCap = 16

// repositoryListColumnWidths reserves the Hub repository column from the
// catalog, not from whichever scope or filtered rows happen to be visible.
func (m *Model) repositoryListColumnWidths(delegate IssueDelegate) (int, int) {
	issues := m.issues
	if len(issues) == 0 {
		issues = make([]model.Issue, 0, len(m.listItemsBuffer))
		for _, item := range m.listItemsBuffer {
			if issueItem, ok := item.(IssueItem); ok {
				issues = append(issues, issueItem.Issue)
			}
		}
	}
	return m.repositoryListColumnWidthsFor(delegate, issues, m.list.Width())
}

func (m *Model) repositoryListColumnWidthsFor(delegate IssueDelegate, issues []model.Issue, listWidth int) (int, int) {
	if !m.hubRepositoryPresentation() || listWidth <= 45 {
		return 0, 0
	}

	repositories := make(repositorypkg.Catalog, 0, len(m.repositoryCatalog))
	for _, repository := range m.repositoryCatalog {
		if repository.Kind == repositorypkg.IdentityExact {
			repositories = append(repositories, repository)
		}
	}
	nameWidth := 0
	for _, repository := range repositories {
		name := repository.Name
		if name == "" {
			name = repository.ID
		}
		nameWidth = max(nameWidth, lipgloss.Width(name))
	}
	nameWidth = max(nameWidth, lipgloss.Width(contextlessRepositoryID))
	nameWidth = min(nameWidth, repositoryListNameWidthCap)
	if nameWidth == 0 {
		return 0, 0
	}

	knownContexts := make(map[string]struct{}, len(m.repositoryCatalog))
	for _, repository := range m.repositoryCatalog {
		if repository.Kind == repositorypkg.IdentityExact {
			knownContexts[repository.ID] = struct{}{}
		}
	}
	extraWidth := repositoryListExtraWidthFor(knownContexts, issues)

	// Variable row metadata must not steal width from an active repository
	// label. Only the fixed minimum row determines when a narrow terminal
	// requires truncation.
	rowWidth := delegate.rowWidthFor(listWidth)
	minimum := IssueItem{Issue: model.Issue{IssueType: model.TypeTask, Status: model.StatusOpen}}
	minimumReserve := delegate.rowWidthWithoutRepository(minimum, rowWidth)
	availableNameWidth := rowWidth - minimumReserve - extraWidth - 3
	if availableNameWidth < 1 {
		return 0, 0
	}
	return min(nameWidth, availableNameWidth), extraWidth
}

func (m Model) repositoryListExtraWidth() int {
	knownContexts := make(map[string]struct{}, len(m.repositoryCatalog))
	for _, repository := range m.repositoryCatalog {
		if repository.Kind == repositorypkg.IdentityExact {
			knownContexts[repository.ID] = struct{}{}
		}
	}

	issues := m.issues
	if len(issues) == 0 {
		issues = make([]model.Issue, 0, len(m.listItemsBuffer))
		for _, item := range m.listItemsBuffer {
			if issueItem, ok := item.(IssueItem); ok {
				issues = append(issues, issueItem.Issue)
			}
		}
	}
	return repositoryListExtraWidthFor(knownContexts, issues)
}

func repositoryListExtraWidthFor(knownContexts map[string]struct{}, issues []model.Issue) int {
	maxExtra := 0
	for _, issue := range issues {
		seen := make(map[string]struct{})
		for _, label := range issue.Labels {
			if _, known := knownContexts[label]; !known {
				continue
			}
			seen[label] = struct{}{}
		}
		if extra := len(seen) - 1; extra > maxExtra {
			maxExtra = extra
		}
	}
	if maxExtra == 0 {
		return 0
	}
	return lipgloss.Width(fmt.Sprintf("+%d", maxExtra))
}
