package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

const repositoryListNameWidthCap = 16

// repositoryListColumnWidths keeps the Hub repository column tied to the
// active scope, not to whichever status-filtered rows happen to be visible.
func (m *Model) repositoryListColumnWidths(delegate IssueDelegate) (int, int) {
	if !m.hubRepositoryPresentation() || m.list.Width() <= 45 {
		return 0, 0
	}

	repositories, includeContextless := m.repositoryListScopeCatalog()
	nameWidth := 0
	for _, repository := range repositories {
		name := repository.Name
		if name == "" {
			name = repository.ID
		}
		nameWidth = max(nameWidth, lipgloss.Width(name))
	}
	if includeContextless {
		nameWidth = max(nameWidth, lipgloss.Width(contextlessRepositoryID))
	}
	nameWidth = min(nameWidth, repositoryListNameWidthCap)
	if nameWidth == 0 {
		return 0, 0
	}

	extraWidth := m.repositoryListExtraWidth()

	// Variable row metadata must not steal width from an active repository
	// label. Only the fixed minimum row determines when a narrow terminal
	// requires truncation.
	rowWidth := m.list.Width() - 1
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
		if repository.Kind == model.RepositoryIdentityHubContext {
			knownContexts[repository.ID] = struct{}{}
		}
	}

	maxExtra := 0
	for _, issue := range m.repositoryIssues {
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

func (m Model) repositoryListScopeCatalog() (model.RepositoryCatalog, bool) {
	includeContextless := false
	selected := map[string]bool(nil)
	switch m.hubScope.Mode {
	case model.HubScopeContextless:
		selected = make(map[string]bool)
		includeContextless = true
	case model.HubScopeSelectedContexts:
		selected = make(map[string]bool, len(m.hubScope.Contexts))
		for _, contextID := range m.hubScope.Contexts {
			selected[contextID] = true
		}
		includeContextless = m.hubScope.IncludeContextless
	default:
		includeContextless = true
	}

	repositories := make(model.RepositoryCatalog, 0, len(m.repositoryCatalog))
	for _, repository := range m.repositoryCatalog {
		if repository.Kind != model.RepositoryIdentityHubContext {
			continue
		}
		if selected == nil || selected[repository.ID] {
			repositories = append(repositories, repository)
		}
	}
	return repositories, includeContextless
}
