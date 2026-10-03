package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newIssueLookupInput(theme Theme) textinput.Model {
	input := textinput.New()
	input.Placeholder = "bead ID"
	input.CharLimit = 200
	input.Width = 40
	input.Prompt = "ID: "
	input.PromptStyle = lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(theme.Base.GetForeground())
	input.Blur()
	return input
}

func (m *Model) openIssueLookup() tea.Cmd {
	if !m.showScopePicker || !m.hubRepositoryMode || m.runtimeServices.Scopes.LookupIssue == nil {
		return nil
	}
	if m.issueLookupInput.Prompt == "" {
		m.issueLookupInput = newIssueLookupInput(m.theme)
	}
	m.issueLookupOrigin = m.focused
	m.issueLookupResult = nil
	m.issueLookupError = ""
	m.issueLookupLoading = false
	m.showIssueLookup = true
	m.focused = focusIssueLookup
	m.issueLookupInput.SetValue("")
	return m.issueLookupInput.Focus()
}

func (m *Model) closeIssueLookup() {
	m.issueLookupInput.Blur()
	m.showIssueLookup = false
	m.issueLookupLoading = false
	m.issueLookupGeneration++
	m.issueLookupResult = nil
	m.issueLookupError = ""
	m.focused = m.issueLookupOrigin
}

func (m *Model) handleIssueLookupKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, m.quitCommand()
	case "esc":
		m.closeIssueLookup()
		return m, nil
	case "enter":
		if m.issueLookupResult != nil {
			if m.issueLookupResult.Scope == nil {
				return m, nil
			}
			result := *m.issueLookupResult
			m.closeIssueLookup()
			return m, m.applyIssueLookupResult(result)
		}
		if m.issueLookupLoading {
			return m, nil
		}
		issueID := strings.TrimSpace(m.issueLookupInput.Value())
		if issueID == "" {
			m.issueLookupError = "Bead ID cannot be empty"
			return m, nil
		}
		m.issueLookupGeneration++
		if m.issueLookupGeneration == 0 {
			m.issueLookupGeneration++
		}
		m.issueLookupLoading = true
		m.issueLookupResult = nil
		m.issueLookupError = ""
		return m, lookupIssueCmd(m.runtimeServices.Scopes, issueID, m.issueLookupGeneration)
	default:
		var cmd tea.Cmd
		m.issueLookupInput, cmd = m.issueLookupInput.Update(msg)
		return m, cmd
	}
}

func validateIssueLookupResult(result IssueLookupResult, issueID string) error {
	if result.Issue.ID != issueID {
		return fmt.Errorf("returned issue %q, want exact issue %q", result.Issue.ID, issueID)
	}
	if result.Scope != nil && (strings.TrimSpace(result.Scope.ID) == "" || strings.TrimSpace(result.Scope.Name) == "") {
		return fmt.Errorf("named_scope must contain a non-empty id and name")
	}
	return nil
}

func (m *Model) applyIssueLookupResult(result IssueLookupResult) tea.Cmd {
	if result.Scope == nil {
		return nil
	}
	scope := *result.Scope
	selectedID := m.scopePicker.SelectedScopeID()
	if selectedID == scope.ID {
		if m.scopePicker.memberScopeID == scope.ID && !m.scopePicker.memberLoading && m.scopePicker.memberError == "" {
			m.scopePicker.SelectMemberByID(result.Issue.ID)
		}
		return nil
	}

	// Keep the result navigable without claiming that it came from the bounded
	// catalog. A later catalog refresh may replace this transient row.
	found := false
	if len(m.scopeCatalog) == 0 && len(m.scopePicker.scopes) > 0 {
		m.scopeCatalog = append([]ScopeInfo(nil), m.scopePicker.scopes...)
	}
	for i := range m.scopeCatalog {
		if m.scopeCatalog[i].ID == scope.ID {
			m.scopeCatalog[i] = mergeScopeInfo(m.scopeCatalog[i], scope)
			found = true
			break
		}
	}
	if !found {
		m.scopeCatalog = append(m.scopeCatalog, scope)
	}
	m.scopePicker.SetScopes(m.scopeCatalog)
	m.scopePicker.SelectScopeByID(scope.ID)
	m.scopePicker.memberFocused = false
	m.focused = focusScopePicker
	if m.runtimeServices.Scopes.QueryMembers != nil {
		return m.startScopeMembersPage("", 0)
	}
	return nil
}

func (m *Model) handleIssueLookupMessage(msg issueLookupMsg) tea.Cmd {
	if !m.showIssueLookup || msg.generation != m.issueLookupGeneration {
		return nil
	}
	m.issueLookupLoading = false
	if msg.err != nil {
		m.issueLookupResult = nil
		m.issueLookupError = msg.err.Error()
		return nil
	}
	if err := validateIssueLookupResult(msg.result, msg.issueID); err != nil {
		m.issueLookupResult = nil
		m.issueLookupError = err.Error()
		return nil
	}
	result := msg.result
	m.issueLookupResult = &result
	m.issueLookupError = ""
	return nil
}

func (m Model) renderIssueLookup() string {
	width := m.mainContentWidth()
	boxWidth := min(maxInt(width-8, 32), 76)
	inputWidth := boxWidth - 10
	if inputWidth < 1 {
		inputWidth = 1
	}
	if m.issueLookupInput.Width > inputWidth {
		m.issueLookupInput.Width = inputWidth
	}

	title := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary).Bold(true).Render("Bead lookup")
	muted := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	content := title + "\n\n" + m.issueLookupInput.View()
	switch {
	case m.issueLookupLoading:
		content += "\n\n" + muted.Render("Loading…")
	case m.issueLookupError != "":
		content += "\n\n" + m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked).Render(truncateRunesHelper(m.issueLookupError, inputWidth, "…"))
	case m.issueLookupResult != nil:
		result := m.issueLookupResult
		scope := "Unscoped (informational; no navigation)"
		if result.Scope != nil {
			scope = fmt.Sprintf("%s (%s)", result.Scope.Name, result.Scope.ID)
		}
		content += "\n\n" + strings.Join([]string{
			"ID: " + result.Issue.ID,
			"Title: " + truncateRunesHelper(result.Issue.Title, inputWidth, "…"),
			fmt.Sprintf("Status: %s · Priority: %d · Type: %s", result.Issue.Status, result.Issue.Priority, result.Issue.IssueType),
			"Scope: " + scope,
		}, "\n")
	}
	hint := "Enter lookup · Esc close"
	if m.issueLookupResult != nil {
		hint = "Esc close"
		if m.issueLookupResult.Scope != nil {
			hint = "Enter navigate · Esc close"
		}
	}
	content += "\n\n" + muted.Render(hint)
	box := m.theme.Renderer.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.theme.Primary).Padding(1, 3).Width(boxWidth)
	return lipgloss.Place(width, maxInt(m.height-1, 1), lipgloss.Center, lipgloss.Center, box.Render(content))
}
